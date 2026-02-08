package main

import (
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
)

func testLog() *log.Entry {
	return log.WithFields(log.Fields{"component": "tcp", "conn": 0})
}

func TestProxyCounter_Concurrent(t *testing.T) {
	counter := &ProxyCounter{}

	const goroutines = 100
	const opsPerGoroutine = 1000

	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	// Spawn goroutines calling To()
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				counter.To(1)
			}
		}()
	}

	// Spawn goroutines calling From()
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				counter.From(1)
			}
		}()
	}

	wg.Wait()

	to, from := counter.Total()
	expectedTo := uint64(goroutines * opsPerGoroutine)
	expectedFrom := uint64(goroutines * opsPerGoroutine)

	if to != expectedTo {
		t.Errorf("To counter = %d, want %d", to, expectedTo)
	}
	if from != expectedFrom {
		t.Errorf("From counter = %d, want %d", from, expectedFrom)
	}
}

func TestProxyCounter_Total(t *testing.T) {
	counter := &ProxyCounter{}
	counter.To(100)
	counter.To(200)
	counter.From(50)
	counter.From(75)

	to, from := counter.Total()
	if to != 300 {
		t.Errorf("To = %d, want 300", to)
	}
	if from != 125 {
		t.Errorf("From = %d, want 125", from)
	}
}

func TestTCPProxy_Pipe(t *testing.T) {
	// Use net.Pipe to create connected pairs for testing bidirectional copy
	serverClient, serverProxy := net.Pipe()
	remoteProxy, remoteServer := net.Pipe()

	counter := &ProxyCounter{}
	proxy := &TCPProxy{
		Counter:     counter,
		ServerConn:  serverProxy,
		RemoteConn:  remoteProxy,
		ErrorSignal: make(chan bool, 1),
		closeOnce:   sync.Once{},
		log:         testLog(),
	}

	// Start piping in both directions
	go proxy.pipe(proxy.ServerConn, proxy.RemoteConn)
	go proxy.pipe(proxy.RemoteConn, proxy.ServerConn)

	// Send data from "server" (client) side to "remote" side
	testData := []byte("hello from server") // 17 bytes
	go func() {
		serverClient.Write(testData)
	}()

	// Read from "remote" side
	buf := make([]byte, 1024)
	n, err := remoteServer.Read(buf)
	if err != nil {
		t.Fatalf("Read from remote failed: %v", err)
	}
	if string(buf[:n]) != "hello from server" {
		t.Errorf("got %q, want %q", string(buf[:n]), "hello from server")
	}

	// Send data from "remote" side to "server" (client) side
	testData2 := []byte("hello from remote") // 17 bytes
	go func() {
		remoteServer.Write(testData2)
	}()

	buf2 := make([]byte, 1024)
	n, err = serverClient.Read(buf2)
	if err != nil {
		t.Fatalf("Read from server failed: %v", err)
	}
	if string(buf2[:n]) != "hello from remote" {
		t.Errorf("got %q, want %q", string(buf2[:n]), "hello from remote")
	}

	// Wait for counters to reach expected values before closing.
	// The pipe goroutines update counters right after dst.Write returns,
	// but there can be a scheduling gap between Write returning and the
	// atomic add executing.
	deadline := time.After(2 * time.Second)
	for {
		to, from := counter.Total()
		if to == 17 && from == 17 {
			break
		}
		select {
		case <-deadline:
			to, from := counter.Total()
			t.Fatalf("Timed out waiting for counters: to=%d, from=%d (want 17, 17)", to, from)
		default:
			time.Sleep(time.Millisecond)
		}
	}

	// Close both external ends to trigger pipe read errors
	serverClient.Close()
	remoteServer.Close()

	// Wait for ErrorSignal
	select {
	case <-proxy.ErrorSignal:
		// Pipe goroutines detected closed connections
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for ErrorSignal after closing connections")
	}

	// Assert exact byte counts:
	// "hello from server" goes server→remote, counted as To (17 bytes)
	// "hello from remote" goes remote→server, counted as From (17 bytes)
	to, from := counter.Total()
	if to != 17 {
		t.Errorf("To counter = %d, want 17", to)
	}
	if from != 17 {
		t.Errorf("From counter = %d, want 17", from)
	}
}

func TestTCPProxy_ErrorHandling(t *testing.T) {
	// Verify that closing one side of a connection signals via ErrorSignal
	// and that the sync.Once prevents double-signaling.
	serverClient, serverProxy := net.Pipe()
	remoteProxy, remoteServer := net.Pipe()

	counter := &ProxyCounter{}
	errSignal := make(chan bool, 1)
	proxy := &TCPProxy{
		Counter:     counter,
		ServerConn:  serverProxy,
		RemoteConn:  remoteProxy,
		ErrorSignal: errSignal,
		closeOnce:   sync.Once{},
		log:         testLog(),
	}

	// Start piping
	go proxy.pipe(proxy.ServerConn, proxy.RemoteConn)
	go proxy.pipe(proxy.RemoteConn, proxy.ServerConn)

	// Close one end - should trigger error signal
	serverClient.Close()
	remoteServer.Close()

	select {
	case <-errSignal:
		// Got the signal, as expected
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for ErrorSignal")
	}

	// The sync.Once ensures only one signal is sent.
	// Verify the channel has at most one value.
	select {
	case <-errSignal:
		t.Fatal("ErrorSignal received twice - sync.Once is not working")
	default:
		// Channel is empty - correct behavior
	}
}

func TestTCPProxy_DialTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping dial timeout test in short mode")
	}

	// Test that TCPProxy.start() doesn't hang when the remote is unreachable.
	// 10.255.255.1 is a non-routable IP per RFC 5737.
	serverClient, serverProxy := net.Pipe()
	defer serverClient.Close()

	counter := &ProxyCounter{}
	errSignal := make(chan bool, 1)
	proxy := &TCPProxy{
		Counter:     counter,
		RemoteAddr:  "10.255.255.1:12345",
		ServerConn:  serverProxy,
		ErrorSignal: errSignal,
		closeOnce:   sync.Once{},
		log:         testLog(),
	}

	go proxy.start()

	// The proxy uses a 30s dial timeout, but on many systems a non-routable
	// IP produces "no route to host" much faster. Use a generous timeout.
	select {
	case <-errSignal:
		// start() correctly signaled an error when dial failed
	case <-time.After(35 * time.Second):
		t.Fatal("TCPProxy.start() hung - no ErrorSignal received within 35s")
	}
}

func TestProxyCounter_Atomic(t *testing.T) {
	// Verify that To/From use atomics by checking with LoadUint64 directly
	counter := &ProxyCounter{}
	counter.To(42)
	counter.From(99)

	to := atomic.LoadUint64(&counter.to)
	from := atomic.LoadUint64(&counter.from)
	if to != 42 {
		t.Errorf("atomic to = %d, want 42", to)
	}
	if from != 99 {
		t.Errorf("atomic from = %d, want 99", from)
	}
}

// startEchoServer starts a TCP echo server that accepts one connection,
// echoes all data back, then closes. Returns the listener address.
func startEchoServer(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start echo server: %v", err)
	}
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		io.Copy(conn, conn)
	}()
	return ln
}

func TestTCPProxy_Start_HappyPath(t *testing.T) {
	// Start a local TCP echo server
	ln := startEchoServer(t)
	defer ln.Close()

	// Create a pipe: clientEnd is "the client", serverProxy is TCPProxy.ServerConn
	clientEnd, serverProxy := net.Pipe()

	counter := &ProxyCounter{}
	errSignal := make(chan bool, 1)
	proxy := &TCPProxy{
		Counter:     counter,
		RemoteAddr:  ln.Addr().String(),
		ServerConn:  serverProxy,
		ErrorSignal: errSignal,
		closeOnce:   sync.Once{},
		log:         testLog(),
	}

	// start() blocks on ErrorSignal internally, so track when it returns
	startDone := make(chan struct{})
	go func() {
		proxy.start()
		close(startDone)
	}()

	// Write test data through the pipe; the echo server will send it back
	testMsg := []byte("hello echo server")
	go func() {
		clientEnd.Write(testMsg)
	}()

	// Read the echoed data back on the client side
	buf := make([]byte, 1024)
	n, err := clientEnd.Read(buf)
	if err != nil {
		t.Fatalf("Read from client end failed: %v", err)
	}
	if string(buf[:n]) != "hello echo server" {
		t.Errorf("got %q, want %q", string(buf[:n]), "hello echo server")
	}

	// Close the client side to trigger connection teardown
	clientEnd.Close()

	select {
	case <-startDone:
		// start() returned after handling the error
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for start() to return")
	}

	// Verify byte counters: 17 bytes sent (To) and 17 bytes received (From)
	to, from := counter.Total()
	if to != 17 {
		t.Errorf("To counter = %d, want 17", to)
	}
	if from != 17 {
		t.Errorf("From counter = %d, want 17", from)
	}
}

func TestTCPProxy_Start_RemoteRefused(t *testing.T) {
	// Get a port that's definitely closed by listening then immediately closing
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	clientEnd, serverProxy := net.Pipe()
	defer clientEnd.Close()

	counter := &ProxyCounter{}
	errSignal := make(chan bool, 1)
	proxy := &TCPProxy{
		Counter:     counter,
		RemoteAddr:  addr,
		ServerConn:  serverProxy,
		ErrorSignal: errSignal,
		closeOnce:   sync.Once{},
		log:         testLog(),
	}

	go proxy.start()

	select {
	case <-errSignal:
		// Connection refused was handled correctly
	case <-time.After(5 * time.Second):
		t.Fatal("TCPProxy.start() hung on connection refused")
	}
}

func TestTCPProxy_Start_WithTLS(t *testing.T) {
	// Load test CA and server certs
	certFile := "contrib/testdata/certs/server.crt"
	keyFile := "contrib/testdata/certs/server.key"
	caFile := "contrib/testdata/certs/ca.crt"

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		t.Fatalf("Failed to load server cert: %v", err)
	}

	caCert, err := os.ReadFile(caFile)
	if err != nil {
		t.Fatalf("Failed to read CA cert: %v", err)
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caCert) {
		t.Fatal("Failed to add CA cert to pool")
	}

	// Start a TLS echo server
	tlsConf := &tls.Config{
		Certificates: []tls.Certificate{cert},
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", tlsConf)
	if err != nil {
		t.Fatalf("Failed to start TLS listener: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		io.Copy(conn, conn)
	}()

	// Create a pipe for the server side
	clientEnd, serverProxy := net.Pipe()

	counter := &ProxyCounter{}
	errSignal := make(chan bool, 1)
	proxy := &TCPProxy{
		Counter:    counter,
		RemoteAddr: ln.Addr().String(),
		ServerConn: serverProxy,
		RemoteTLSConf: &tls.Config{
			RootCAs:    caPool,
			ServerName: "localhost",
		},
		ErrorSignal: errSignal,
		closeOnce:   sync.Once{},
		log:         testLog(),
	}

	// start() blocks on ErrorSignal internally, so track when it returns
	startDone := make(chan struct{})
	go func() {
		proxy.start()
		close(startDone)
	}()

	// Write test data; the TLS echo server echoes it back
	testMsg := []byte("hello tls server")
	go func() {
		clientEnd.Write(testMsg)
	}()

	buf := make([]byte, 1024)
	n, err := clientEnd.Read(buf)
	if err != nil {
		t.Fatalf("Read from client end failed: %v", err)
	}
	if string(buf[:n]) != "hello tls server" {
		t.Errorf("got %q, want %q", string(buf[:n]), "hello tls server")
	}

	// Close the client side to trigger teardown
	clientEnd.Close()

	select {
	case <-startDone:
		// start() returned after handling the error
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for start() to return")
	}

	// Verify byte counters: 16 bytes each way
	to, from := counter.Total()
	if to != 16 {
		t.Errorf("To counter = %d, want 16", to)
	}
	if from != 16 {
		t.Errorf("From counter = %d, want 16", from)
	}
}

func TestTCPProxy_Pipe_WriteFail(t *testing.T) {
	// Test that pipe() signals ErrorSignal when a write fails.
	// Create connections where the destination is pre-closed.
	serverClient, serverProxy := net.Pipe()
	remoteProxy, remoteServer := net.Pipe()

	counter := &ProxyCounter{}
	errSignal := make(chan bool, 1)
	proxy := &TCPProxy{
		Counter:     counter,
		ServerConn:  serverProxy,
		RemoteConn:  remoteProxy,
		ErrorSignal: errSignal,
		closeOnce:   sync.Once{},
		log:         testLog(),
	}

	// Close the remote end so that writes to remoteProxy will fail
	remoteServer.Close()

	// Start only the server→remote pipe direction
	go proxy.pipe(proxy.ServerConn, proxy.RemoteConn)

	// Write data from the client side; pipe will read it then fail on write
	go func() {
		serverClient.Write([]byte("will fail"))
		serverClient.Close()
	}()

	select {
	case <-errSignal:
		// Write failure correctly signaled
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for ErrorSignal on write failure")
	}
}
