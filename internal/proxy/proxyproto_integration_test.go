package proxy

import (
	"context"
	"crypto/tls"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"
)

// TestTCPProxy_SendsProxyProtocolV1 runs a TCPProxy against a fake backend and
// asserts the backend receives a valid PROXY protocol v1 header followed by the
// proxied payload.
func TestTCPProxy_SendsProxyProtocolV1(t *testing.T) {
	// Fake backend that records the first bytes it receives.
	backend, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("backend listen: %v", err)
	}
	defer backend.Close()

	received := make(chan []byte, 1)
	go func() {
		conn, err := backend.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		var acc []byte
		buf := make([]byte, 512)
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		for {
			n, err := conn.Read(buf)
			acc = append(acc, buf[:n]...)
			if err != nil {
				break
			}
			if len(acc) >= len("PROXY TCP4 127.0.0.1 127.0.0.1 00000 00000\r\nPAYLOAD") {
				break
			}
		}
		received <- acc
	}()

	// Real TCP connection for the server side so RemoteAddr/LocalAddr are
	// *net.TCPAddr.
	serverLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("server listen: %v", err)
	}
	defer serverLn.Close()

	clientConn, err := net.Dial("tcp", serverLn.Addr().String())
	if err != nil {
		t.Fatalf("client dial: %v", err)
	}
	defer clientConn.Close()

	serverConn, err := serverLn.Accept()
	if err != nil {
		t.Fatalf("server accept: %v", err)
	}

	proxy := &TCPProxy{
		Counter:     &Counter{},
		RemoteAddr:  backend.Addr().String(),
		ServerConn:  serverConn,
		ErrorSignal: make(chan bool, 1),
		Ctx:         context.Background(),
		CloseOnce:   sync.Once{},
		Log:         testLog(),
		ProxyProto:  "v1",
	}
	go proxy.Start()

	// Client sends a payload after connection is proxied.
	go func() {
		time.Sleep(50 * time.Millisecond)
		_, _ = clientConn.Write([]byte("PAYLOAD"))
	}()

	select {
	case got := <-received:
		s := string(got)
		clientAddr := clientConn.LocalAddr().(*net.TCPAddr)
		srvAddr := serverLn.Addr().(*net.TCPAddr)
		want := "PROXY TCP4 127.0.0.1 127.0.0.1 " +
			strconv.Itoa(clientAddr.Port) + " " + strconv.Itoa(srvAddr.Port) + "\r\nPAYLOAD"
		if s != want {
			t.Errorf("backend received %q, want %q", s, want)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for backend to receive data")
	}
}

// TestTCPProxy_HandshakeGate_NoBackendDial verifies that a plain-TCP client
// connecting to a TLS listener (never completing a handshake) does NOT cause a
// backend dial, and the connection is closed after the handshake timeout.
func TestTCPProxy_HandshakeGate_NoBackendDial(t *testing.T) {
	// Backend that fails the test if it ever receives a connection.
	backend, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("backend listen: %v", err)
	}
	defer backend.Close()
	go func() {
		conn, err := backend.Accept()
		if err != nil {
			return
		}
		conn.Close()
		t.Errorf("backend was dialed but should not have been")
	}()

	// TLS listener for the server side.
	root := projectRoot(t)
	cert, err := tls.LoadX509KeyPair(
		root+"/contrib/testdata/certs/server.crt",
		root+"/contrib/testdata/certs/server.key",
	)
	if err != nil {
		t.Fatalf("load cert: %v", err)
	}
	tlsLn, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}})
	if err != nil {
		t.Fatalf("tls listen: %v", err)
	}
	defer tlsLn.Close()

	// Plain-TCP client connecting to the TLS listener; sends garbage, never
	// completes a TLS handshake.
	client, err := net.Dial("tcp", tlsLn.Addr().String())
	if err != nil {
		t.Fatalf("client dial: %v", err)
	}
	defer client.Close()
	_, _ = client.Write([]byte("this is not a tls handshake"))

	serverConn, err := tlsLn.Accept()
	if err != nil {
		t.Fatalf("tls accept: %v", err)
	}

	proxy := &TCPProxy{
		Counter:          &Counter{},
		RemoteAddr:       backend.Addr().String(),
		ServerConn:       serverConn,
		ErrorSignal:      make(chan bool, 1),
		Ctx:              context.Background(),
		CloseOnce:        sync.Once{},
		Log:              testLog(),
		HandshakeTimeout: 200 * time.Millisecond,
	}

	startDone := make(chan struct{})
	go func() {
		proxy.Start()
		close(startDone)
	}()

	select {
	case <-startDone:
		// Good: Start returned without dialing the backend.
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not return after handshake timeout")
	}

	// Give any erroneous backend dial a moment to surface.
	time.Sleep(100 * time.Millisecond)
}
