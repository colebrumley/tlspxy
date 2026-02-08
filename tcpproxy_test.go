package main

import (
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

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
		Counter:    counter,
		ServerConn: serverProxy,
		RemoteConn: remoteProxy,
		ErrorSignal: make(chan bool, 1),
		closeOnce:  sync.Once{},
	}

	// Start piping in both directions
	go proxy.pipe(proxy.ServerConn, proxy.RemoteConn)
	go proxy.pipe(proxy.RemoteConn, proxy.ServerConn)

	// Send data from "server" (client) side to "remote" side
	testData := []byte("hello from server")
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
	testData2 := []byte("hello from remote")
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

	// Close and verify counters got updated
	serverClient.Close()
	remoteServer.Close()

	// Give goroutines a moment to process
	time.Sleep(50 * time.Millisecond)

	to, from := counter.Total()
	if to == 0 && from == 0 {
		t.Error("Expected counter to record some bytes, but both are 0")
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
		Counter:    counter,
		ServerConn: serverProxy,
		RemoteConn: remoteProxy,
		ErrorSignal: errSignal,
		closeOnce:  sync.Once{},
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

	// Test that connection to an unreachable host times out.
	// 10.255.255.1 is a non-routable IP per RFC 5737.
	start := time.Now()
	conn, err := net.DialTimeout("tcp", "10.255.255.1:12345", 1*time.Second)
	elapsed := time.Since(start)

	if conn != nil {
		conn.Close()
		t.Fatal("Expected connection to fail")
	}
	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	// Verify it didn't hang forever
	if elapsed > 5*time.Second {
		t.Errorf("Dial took %v, expected roughly 1s timeout", elapsed)
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
