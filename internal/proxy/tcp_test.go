package proxy

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testLog() *slog.Logger {
	return slog.With("component", "tcp", "conn", 0)
}

// projectRoot finds the project root by looking for go.mod.
func projectRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find project root")
		}
		dir = parent
	}
}

func TestCounter_Concurrent(t *testing.T) {
	counter := &Counter{}

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

func TestCounter_Total(t *testing.T) {
	counter := &Counter{}
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

	counter := &Counter{}
	proxy := &TCPProxy{
		Counter:     counter,
		ServerConn:  serverProxy,
		RemoteConn:  remoteProxy,
		ErrorSignal: make(chan bool, 1),
		Ctx:         context.Background(),
		CloseOnce:   sync.Once{},
		Log:         testLog(),
	}

	// Start piping in both directions
	go proxy.Pipe(proxy.ServerConn, proxy.RemoteConn)
	go proxy.Pipe(proxy.RemoteConn, proxy.ServerConn)

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

	// Assert exact byte counts
	to, from := counter.Total()
	if to != 17 {
		t.Errorf("To counter = %d, want 17", to)
	}
	if from != 17 {
		t.Errorf("From counter = %d, want 17", from)
	}
}

func TestTCPProxy_ErrorHandling(t *testing.T) {
	serverClient, serverProxy := net.Pipe()
	remoteProxy, remoteServer := net.Pipe()

	counter := &Counter{}
	errSignal := make(chan bool, 1)
	proxy := &TCPProxy{
		Counter:     counter,
		ServerConn:  serverProxy,
		RemoteConn:  remoteProxy,
		ErrorSignal: errSignal,
		Ctx:         context.Background(),
		CloseOnce:   sync.Once{},
		Log:         testLog(),
	}

	go proxy.Pipe(proxy.ServerConn, proxy.RemoteConn)
	go proxy.Pipe(proxy.RemoteConn, proxy.ServerConn)

	serverClient.Close()
	remoteServer.Close()

	select {
	case <-errSignal:
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for ErrorSignal")
	}

	select {
	case <-errSignal:
		t.Fatal("ErrorSignal received twice - sync.Once is not working")
	default:
	}
}

func TestTCPProxy_DialTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping dial timeout test in short mode")
	}

	serverClient, serverProxy := net.Pipe()
	defer serverClient.Close()

	counter := &Counter{}
	errSignal := make(chan bool, 1)
	proxy := &TCPProxy{
		Counter:     counter,
		RemoteAddr:  "10.255.255.1:12345",
		ServerConn:  serverProxy,
		ErrorSignal: errSignal,
		Ctx:         context.Background(),
		CloseOnce:   sync.Once{},
		Log:         testLog(),
	}

	go proxy.Start()

	select {
	case <-errSignal:
	case <-time.After(35 * time.Second):
		t.Fatal("TCPProxy.Start() hung - no ErrorSignal received within 35s")
	}
}

func TestCounter_Atomic(t *testing.T) {
	counter := &Counter{}
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
	ln := startEchoServer(t)
	defer ln.Close()

	clientEnd, serverProxy := net.Pipe()

	counter := &Counter{}
	errSignal := make(chan bool, 1)
	proxy := &TCPProxy{
		Counter:     counter,
		RemoteAddr:  ln.Addr().String(),
		ServerConn:  serverProxy,
		ErrorSignal: errSignal,
		Ctx:         context.Background(),
		CloseOnce:   sync.Once{},
		Log:         testLog(),
	}

	startDone := make(chan struct{})
	go func() {
		proxy.Start()
		close(startDone)
	}()

	testMsg := []byte("hello echo server")
	go func() {
		clientEnd.Write(testMsg)
	}()

	buf := make([]byte, 1024)
	n, err := clientEnd.Read(buf)
	if err != nil {
		t.Fatalf("Read from client end failed: %v", err)
	}
	if string(buf[:n]) != "hello echo server" {
		t.Errorf("got %q, want %q", string(buf[:n]), "hello echo server")
	}

	deadline := time.After(5 * time.Second)
	for {
		to, from := counter.Total()
		if to == 17 && from == 17 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("Timed out waiting for counters: to=%d, from=%d (want 17, 17)", to, from)
		default:
			time.Sleep(time.Millisecond)
		}
	}

	clientEnd.Close()

	select {
	case <-startDone:
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for Start() to return")
	}
}

func TestTCPProxy_Start_RemoteRefused(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	clientEnd, serverProxy := net.Pipe()
	defer clientEnd.Close()

	counter := &Counter{}
	errSignal := make(chan bool, 1)
	proxy := &TCPProxy{
		Counter:     counter,
		RemoteAddr:  addr,
		ServerConn:  serverProxy,
		ErrorSignal: errSignal,
		Ctx:         context.Background(),
		CloseOnce:   sync.Once{},
		Log:         testLog(),
	}

	go proxy.Start()

	select {
	case <-errSignal:
	case <-time.After(5 * time.Second):
		t.Fatal("TCPProxy.Start() hung on connection refused")
	}
}

func TestTCPProxy_Start_WithTLS(t *testing.T) {
	root := projectRoot(t)
	certFile := filepath.Join(root, "contrib/testdata/certs/server.crt")
	keyFile := filepath.Join(root, "contrib/testdata/certs/server.key")
	caFile := filepath.Join(root, "contrib/testdata/certs/ca.crt")

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

	clientEnd, serverProxy := net.Pipe()

	counter := &Counter{}
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
		Ctx:         context.Background(),
		CloseOnce:   sync.Once{},
		Log:         testLog(),
	}

	startDone := make(chan struct{})
	go func() {
		proxy.Start()
		close(startDone)
	}()

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

	deadline := time.After(5 * time.Second)
	for {
		to, from := counter.Total()
		if to == 16 && from == 16 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("Timed out waiting for counters: to=%d, from=%d (want 16, 16)", to, from)
		default:
			time.Sleep(time.Millisecond)
		}
	}

	clientEnd.Close()

	select {
	case <-startDone:
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for Start() to return")
	}
}

func TestTCPProxy_Pipe_WriteFail(t *testing.T) {
	serverClient, serverProxy := net.Pipe()
	remoteProxy, remoteServer := net.Pipe()

	counter := &Counter{}
	errSignal := make(chan bool, 1)
	proxy := &TCPProxy{
		Counter:     counter,
		ServerConn:  serverProxy,
		RemoteConn:  remoteProxy,
		ErrorSignal: errSignal,
		Ctx:         context.Background(),
		CloseOnce:   sync.Once{},
		Log:         testLog(),
	}

	remoteServer.Close()

	go proxy.Pipe(proxy.ServerConn, proxy.RemoteConn)

	go func() {
		serverClient.Write([]byte("will fail"))
		serverClient.Close()
	}()

	select {
	case <-errSignal:
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for ErrorSignal on write failure")
	}
}

// panicConn is a net.Conn wrapper that panics on Read.
type panicConn struct {
	net.Conn
}

func (c *panicConn) Read(b []byte) (int, error) {
	panic("intentional test panic in Read")
}

func TestTCPProxy_PipeRecovery(t *testing.T) {
	_, serverProxy := net.Pipe()
	remoteProxy, remoteServer := net.Pipe()
	defer remoteServer.Close()

	counter := &Counter{}
	errSignal := make(chan bool, 1)
	proxy := &TCPProxy{
		Counter:     counter,
		ServerConn:  serverProxy,
		RemoteConn:  remoteProxy,
		ErrorSignal: errSignal,
		Ctx:         context.Background(),
		CloseOnce:   sync.Once{},
		Log:         testLog(),
	}

	go proxy.Pipe(&panicConn{Conn: serverProxy}, proxy.RemoteConn)

	select {
	case <-errSignal:
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for ErrorSignal after panic in pipe")
	}
}

func TestTCPProxy_IdleTimeout(t *testing.T) {
	ln := startEchoServer(t)
	defer ln.Close()

	serverLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	defer serverLn.Close()

	clientConn, err := net.Dial("tcp", serverLn.Addr().String())
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer clientConn.Close()

	serverConn, err := serverLn.Accept()
	if err != nil {
		t.Fatalf("Failed to accept: %v", err)
	}
	defer serverConn.Close()

	counter := &Counter{}
	errSignal := make(chan bool, 1)
	proxy := &TCPProxy{
		Counter:     counter,
		RemoteAddr:  ln.Addr().String(),
		ServerConn:  serverConn,
		ErrorSignal: errSignal,
		Ctx:         context.Background(),
		CloseOnce:   sync.Once{},
		Log:         testLog(),
		IdleTimeout: 100 * time.Millisecond,
	}

	startDone := make(chan struct{})
	go func() {
		proxy.Start()
		close(startDone)
	}()

	select {
	case <-startDone:
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for idle timeout to close connection")
	}
}

func TestTCPProxy_Pipe_ZeroTimeouts(t *testing.T) {
	serverClient, serverProxy := net.Pipe()
	remoteProxy, remoteServer := net.Pipe()

	counter := &Counter{}
	proxy := &TCPProxy{
		Counter:      counter,
		ServerConn:   serverProxy,
		RemoteConn:   remoteProxy,
		ErrorSignal:  make(chan bool, 1),
		Ctx:          context.Background(),
		CloseOnce:    sync.Once{},
		Log:          testLog(),
		ReadTimeout:  0,
		WriteTimeout: 0,
		IdleTimeout:  0,
	}

	go proxy.Pipe(proxy.ServerConn, proxy.RemoteConn)
	go proxy.Pipe(proxy.RemoteConn, proxy.ServerConn)

	testData := []byte("hello zero timeouts")
	go func() {
		serverClient.Write(testData)
	}()

	buf := make([]byte, 1024)
	n, err := remoteServer.Read(buf)
	if err != nil {
		t.Fatalf("Read from remote failed: %v", err)
	}
	if string(buf[:n]) != "hello zero timeouts" {
		t.Errorf("got %q, want %q", string(buf[:n]), "hello zero timeouts")
	}

	testData2 := []byte("reply zero timeouts")
	go func() {
		remoteServer.Write(testData2)
	}()

	buf2 := make([]byte, 1024)
	n, err = serverClient.Read(buf2)
	if err != nil {
		t.Fatalf("Read from server failed: %v", err)
	}
	if string(buf2[:n]) != "reply zero timeouts" {
		t.Errorf("got %q, want %q", string(buf2[:n]), "reply zero timeouts")
	}

	deadline := time.After(2 * time.Second)
	for {
		to, from := counter.Total()
		if to == 19 && from == 19 {
			break
		}
		select {
		case <-deadline:
			to, from := counter.Total()
			t.Fatalf("Timed out waiting for counters: to=%d, from=%d (want 19, 19)", to, from)
		default:
			time.Sleep(time.Millisecond)
		}
	}

	serverClient.Close()
	remoteServer.Close()

	select {
	case <-proxy.ErrorSignal:
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for ErrorSignal")
	}
}

func TestSingleJoiningSlash(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want string
	}{
		{
			name: "Both have slash",
			a:    "http://example.com/",
			b:    "/path",
			want: "http://example.com/path",
		},
		{
			name: "Neither has slash",
			a:    "http://example.com",
			b:    "path",
			want: "http://example.com/path",
		},
		{
			name: "Only a has slash",
			a:    "http://example.com/",
			b:    "path",
			want: "http://example.com/path",
		},
		{
			name: "Only b has slash",
			a:    "http://example.com",
			b:    "/path",
			want: "http://example.com/path",
		},
		{
			name: "Empty b",
			a:    "http://example.com/",
			b:    "",
			want: "http://example.com/",
		},
		{
			name: "Empty a",
			a:    "",
			b:    "/path",
			want: "/path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SingleJoiningSlash(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("SingleJoiningSlash(%q, %q) = %q, want %q", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
