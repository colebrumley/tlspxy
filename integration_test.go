package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	testTimeout = 5 * time.Second
	certDir     = "contrib/testdata/certs"
)

// integrationEchoServer starts a TCP server that echoes back whatever it receives.
func integrationEchoServer(t *testing.T) (addr string, cleanup func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				io.Copy(c, c)
			}(conn)
		}
	}()
	return ln.Addr().String(), func() { ln.Close() }
}

// integrationTLSEchoServer starts a TLS echo server using the given cert/key/ca files.
func integrationTLSEchoServer(t *testing.T, certFile, keyFile, caFile string) (addr string, cleanup func()) {
	t.Helper()

	tlsCfg := loadTestTLSConfig(t, certFile, keyFile, caFile)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	tlsLn := tls.NewListener(ln, tlsCfg)
	t.Cleanup(func() { tlsLn.Close() })

	go func() {
		for {
			conn, err := tlsLn.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				io.Copy(c, c)
			}(conn)
		}
	}()
	return tlsLn.Addr().String(), func() { tlsLn.Close() }
}

// loadTestTLSConfig loads test certs and returns a server tls.Config.
func loadTestTLSConfig(t *testing.T, certFile, keyFile, caFile string) *tls.Config {
	t.Helper()

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		t.Fatalf("Failed to load key pair %s/%s: %v", certFile, keyFile, err)
	}

	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		t.Fatalf("Failed to read CA file %s: %v", caFile, err)
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caPEM) {
		t.Fatalf("Failed to parse CA cert from %s", caFile)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientCAs:    caPool,
		RootCAs:      caPool,
		MinVersion:   tls.VersionTLS12,
	}
}

// loadTestCAPool loads the CA cert and returns a cert pool for client use.
func loadTestCAPool(t *testing.T, caFile string) *x509.CertPool {
	t.Helper()
	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		t.Fatalf("Failed to read CA file %s: %v", caFile, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		t.Fatalf("Failed to parse CA cert from %s", caFile)
	}
	return pool
}

func TestIntegration_PlaintextPassthrough(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Start a plain TCP echo server as the backend
	echoAddr, echoCleanup := integrationEchoServer(t)
	t.Cleanup(echoCleanup)

	// Start a plain TCP listener for the proxy
	proxyLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { proxyLn.Close() })
	proxyAddr := proxyLn.Addr().String()

	counter := &ProxyCounter{}

	// Accept one connection and proxy it
	accepted := make(chan struct{})
	go func() {
		conn, err := proxyLn.Accept()
		if err != nil {
			return
		}
		close(accepted)
		p := &TCPProxy{
			Counter:     counter,
			ServerConn:  conn,
			ServerAddr:  proxyAddr,
			RemoteAddr:  echoAddr,
			ErrorSignal: make(chan bool, 1),
			prefix:      "test ",
		}
		p.start()
	}()

	// Connect a plain TCP client to the proxy
	conn, err := net.DialTimeout("tcp", proxyAddr, testTimeout)
	if err != nil {
		t.Fatalf("Failed to connect to proxy: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	// Wait for the proxy to accept
	select {
	case <-accepted:
	case <-time.After(testTimeout):
		t.Fatal("Timed out waiting for proxy to accept connection")
	}

	// Send test data
	testData := "Hello plaintext passthrough"
	conn.SetDeadline(time.Now().Add(testTimeout))
	if _, err := conn.Write([]byte(testData)); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Read the echo
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	got := string(buf[:n])
	if got != testData {
		t.Errorf("Echo mismatch: got %q, want %q", got, testData)
	}

	// Close client connection so the proxy goroutine finishes
	conn.Close()
	time.Sleep(100 * time.Millisecond)

	// Verify byte counters were updated
	to, from := counter.Total()
	if to == 0 {
		t.Error("Expected to counter > 0")
	}
	if from == 0 {
		t.Error("Expected from counter > 0")
	}
}

func TestIntegration_TLSTermination_TCP(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Start a plain TCP echo server as the backend (no TLS)
	echoAddr, echoCleanup := integrationEchoServer(t)
	t.Cleanup(echoCleanup)

	// Load proxy server TLS config
	proxyCert := certDir + "/proxy.crt"
	proxyKey := certDir + "/proxy.key"
	caFile := certDir + "/ca.crt"

	serverTLSConf := loadTestTLSConfig(t, proxyCert, proxyKey, caFile)

	// Start a TCP listener and wrap with TLS for the proxy's server side
	innerLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	tlsLn := tls.NewListener(innerLn, serverTLSConf)
	t.Cleanup(func() { tlsLn.Close() })
	proxyAddr := tlsLn.Addr().String()

	counter := &ProxyCounter{}

	// Accept connections and proxy them to the plain echo server
	accepted := make(chan struct{}, 1)
	go func() {
		conn, err := tlsLn.Accept()
		if err != nil {
			return
		}
		select {
		case accepted <- struct{}{}:
		default:
		}
		p := &TCPProxy{
			Counter:     counter,
			ServerConn:  conn,
			ServerAddr:  proxyAddr,
			RemoteAddr:  echoAddr,
			ErrorSignal: make(chan bool, 1),
			prefix:      "tls-test ",
		}
		// No RemoteTLSConf — backend is plain TCP
		p.start()
	}()

	// Connect as a TLS client to the proxy
	caPool := loadTestCAPool(t, caFile)
	clientTLSConf := &tls.Config{
		RootCAs:    caPool,
		ServerName: "localhost",
		MinVersion: tls.VersionTLS12,
	}

	conn, err := tls.DialWithDialer(
		&net.Dialer{Timeout: testTimeout},
		"tcp", proxyAddr, clientTLSConf,
	)
	if err != nil {
		t.Fatalf("TLS dial to proxy failed: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	// Wait for the proxy to accept
	select {
	case <-accepted:
	case <-time.After(testTimeout):
		t.Fatal("Timed out waiting for proxy to accept TLS connection")
	}

	// Send data through the TLS connection
	testData := "Hello through TLS termination proxy"
	conn.SetDeadline(time.Now().Add(testTimeout))
	if _, err := conn.Write([]byte(testData)); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Read the echoed response
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	got := string(buf[:n])
	if got != testData {
		t.Errorf("Echo mismatch: got %q, want %q", got, testData)
	}

	// Verify the TLS connection state
	state := conn.ConnectionState()
	if !state.HandshakeComplete {
		t.Error("TLS handshake not completed")
	}
	if state.Version < tls.VersionTLS12 {
		t.Error("TLS version below 1.2")
	}

	conn.Close()
}

func TestIntegration_TLSTermination_HTTP(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Capture request details from the backend
	var (
		mu              sync.Mutex
		capturedBody    string
		capturedXFF     string
		capturedPath    string
		capturedMethod  string
	)

	// Start a plain HTTP backend
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		capturedXFF = r.Header.Get("X-Forwarded-For")
		capturedPath = r.URL.Path
		capturedMethod = r.Method
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "backend-response-ok")
	}))
	t.Cleanup(backend.Close)

	// Parse backend URL
	backendURL, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatal(err)
	}

	// Load proxy server TLS config
	proxyCert := certDir + "/proxy.crt"
	proxyKey := certDir + "/proxy.key"
	caFile := certDir + "/ca.crt"

	serverTLSConf := loadTestTLSConfig(t, proxyCert, proxyKey, caFile)

	// Create TLS listener for the proxy's server side
	innerLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	tlsLn := tls.NewListener(innerLn, serverTLSConf)
	t.Cleanup(func() { tlsLn.Close() })
	proxyAddr := tlsLn.Addr().String()

	// Set up the reverse proxy exactly like main.go's HTTP mode
	director := func(req *http.Request) {
		req.Host = backendURL.Host
		req.URL.Scheme = backendURL.Scheme
		req.URL.Host = backendURL.Host
		req.URL.Path = singleJoiningSlash(backendURL.Path, req.URL.Path)
		if clientIP, _, err := net.SplitHostPort(req.RemoteAddr); err == nil {
			if prior := req.Header.Get("X-Forwarded-For"); prior != "" {
				clientIP = prior + ", " + clientIP
			}
			req.Header.Set("X-Forwarded-For", clientIP)
			req.Header.Set("X-Real-IP", clientIP)
		}
	}

	proxy := &ProxyTransport{
		RoundTripper: &http.Transport{},
	}

	rp := &httputil.ReverseProxy{
		Director:  director,
		Transport: proxy,
	}

	srv := &http.Server{Handler: rp}
	go func() {
		if err := srv.Serve(tlsLn); err != nil && err != http.ErrServerClosed {
			// Server stopped
		}
	}()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	})

	// Create an HTTPS client that trusts our CA
	caPool := loadTestCAPool(t, caFile)
	httpClient := &http.Client{
		Timeout: testTimeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:    caPool,
				ServerName: "localhost",
				MinVersion: tls.VersionTLS12,
			},
		},
	}

	// Send a POST request through the TLS proxy to the plain HTTP backend
	reqBody := "integration-test-request-body"
	reqURL := fmt.Sprintf("https://%s/test-path", proxyAddr)
	req, err := http.NewRequest("POST", reqURL, strings.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "text/plain")

	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("HTTPS request to proxy failed: %v", err)
	}
	defer resp.Body.Close()

	// Read response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	// Verify response body matches what the backend sent
	if string(respBody) != "backend-response-ok" {
		t.Errorf("Response body = %q, want %q", string(respBody), "backend-response-ok")
	}

	// Verify status code
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Status code = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// Verify backend captured the request correctly
	mu.Lock()
	defer mu.Unlock()

	if capturedMethod != "POST" {
		t.Errorf("Backend received method %q, want POST", capturedMethod)
	}
	if capturedBody != reqBody {
		t.Errorf("Backend received body %q, want %q", capturedBody, reqBody)
	}
	if capturedPath != "/test-path" {
		t.Errorf("Backend received path %q, want /test-path", capturedPath)
	}
	if capturedXFF == "" {
		t.Error("Backend did not receive X-Forwarded-For header")
	}
}

func TestIntegration_TLSBothSides(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Start a TLS echo server as the backend
	serverCert := certDir + "/server.crt"
	serverKey := certDir + "/server.key"
	caFile := certDir + "/ca.crt"

	echoAddr, echoCleanup := integrationTLSEchoServer(t, serverCert, serverKey, caFile)
	t.Cleanup(echoCleanup)

	// Load proxy server TLS config (proxy-facing side)
	proxyCert := certDir + "/proxy.crt"
	proxyKey := certDir + "/proxy.key"

	serverTLSConf := loadTestTLSConfig(t, proxyCert, proxyKey, caFile)

	// Start a TLS listener for the proxy's server side
	innerLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	tlsLn := tls.NewListener(innerLn, serverTLSConf)
	t.Cleanup(func() { tlsLn.Close() })
	proxyAddr := tlsLn.Addr().String()

	counter := &ProxyCounter{}

	// Remote TLS config for the proxy → backend connection
	caPool := loadTestCAPool(t, caFile)
	remoteTLSConf := &tls.Config{
		RootCAs:    caPool,
		ServerName: "localhost",
		MinVersion: tls.VersionTLS12,
	}

	// Accept connections and proxy them to the TLS echo server
	accepted := make(chan struct{}, 1)
	go func() {
		conn, err := tlsLn.Accept()
		if err != nil {
			return
		}
		select {
		case accepted <- struct{}{}:
		default:
		}
		p := &TCPProxy{
			Counter:       counter,
			ServerConn:    conn,
			ServerAddr:    proxyAddr,
			RemoteAddr:    echoAddr,
			RemoteTLSConf: remoteTLSConf,
			ErrorSignal:   make(chan bool, 1),
			prefix:        "tls-both-test ",
		}
		p.start()
	}()

	// Connect as a TLS client to the proxy
	clientTLSConf := &tls.Config{
		RootCAs:    caPool,
		ServerName: "localhost",
		MinVersion: tls.VersionTLS12,
	}

	conn, err := tls.DialWithDialer(
		&net.Dialer{Timeout: testTimeout},
		"tcp", proxyAddr, clientTLSConf,
	)
	if err != nil {
		t.Fatalf("TLS dial to proxy failed: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	// Wait for the proxy to accept
	select {
	case <-accepted:
	case <-time.After(testTimeout):
		t.Fatal("Timed out waiting for proxy to accept TLS connection")
	}

	// Give the proxy a moment to establish the backend TLS connection
	time.Sleep(100 * time.Millisecond)

	// Send data through both TLS layers
	testData := "Hello through double TLS proxy"
	conn.SetDeadline(time.Now().Add(testTimeout))
	if _, err := conn.Write([]byte(testData)); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Read the echoed response
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	got := string(buf[:n])
	if got != testData {
		t.Errorf("Echo mismatch: got %q, want %q", got, testData)
	}

	// Verify the client-proxy TLS connection state
	state := conn.ConnectionState()
	if !state.HandshakeComplete {
		t.Error("TLS handshake not completed")
	}

	conn.Close()
}
