package proxy

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	testTimeout = 5 * time.Second
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
				_, _ = io.Copy(c, c)
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
				_, _ = io.Copy(c, c)
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

func certDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(projectRoot(t), "contrib/testdata/certs")
}

func TestIntegration_PlaintextPassthrough(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	echoAddr, echoCleanup := integrationEchoServer(t)
	t.Cleanup(echoCleanup)

	proxyLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { proxyLn.Close() })
	proxyAddr := proxyLn.Addr().String()

	counter := &Counter{}

	accepted := make(chan struct{})
	proxyDone := make(chan struct{})
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
			Ctx:         context.Background(),
			ConnID:      1,
			Log:         slog.With("component", "tcp", "conn", 1),
		}
		p.Start()
		close(proxyDone)
	}()

	conn, err := net.DialTimeout("tcp", proxyAddr, testTimeout)
	if err != nil {
		t.Fatalf("Failed to connect to proxy: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	select {
	case <-accepted:
	case <-time.After(testTimeout):
		t.Fatal("Timed out waiting for proxy to accept connection")
	}

	testData := "Hello plaintext passthrough"
	_ = conn.SetDeadline(time.Now().Add(testTimeout))
	if _, err := conn.Write([]byte(testData)); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	got := string(buf[:n])
	if got != testData {
		t.Errorf("Echo mismatch: got %q, want %q", got, testData)
	}

	conn.Close()
	select {
	case <-proxyDone:
	case <-time.After(testTimeout):
		t.Fatal("Timed out waiting for proxy goroutine to finish")
	}

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

	echoAddr, echoCleanup := integrationEchoServer(t)
	t.Cleanup(echoCleanup)

	cd := certDir(t)
	proxyCert := filepath.Join(cd, "proxy.crt")
	proxyKey := filepath.Join(cd, "proxy.key")
	caFile := filepath.Join(cd, "ca.crt")

	serverTLSConf := loadTestTLSConfig(t, proxyCert, proxyKey, caFile)

	innerLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	tlsLn := tls.NewListener(innerLn, serverTLSConf)
	t.Cleanup(func() { tlsLn.Close() })
	proxyAddr := tlsLn.Addr().String()

	counter := &Counter{}

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
			Ctx:         context.Background(),
			ConnID:      1,
			Log:         slog.With("component", "tcp", "conn", 1),
		}
		p.Start()
	}()

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

	select {
	case <-accepted:
	case <-time.After(testTimeout):
		t.Fatal("Timed out waiting for proxy to accept TLS connection")
	}

	testData := "Hello through TLS termination proxy"
	_ = conn.SetDeadline(time.Now().Add(testTimeout))
	if _, err := conn.Write([]byte(testData)); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	got := string(buf[:n])
	if got != testData {
		t.Errorf("Echo mismatch: got %q, want %q", got, testData)
	}

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

	var (
		mu             sync.Mutex
		capturedBody   string
		capturedXFF    string
		capturedPath   string
		capturedMethod string
	)

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

	backendURL, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatal(err)
	}

	cd := certDir(t)
	proxyCert := filepath.Join(cd, "proxy.crt")
	proxyKey := filepath.Join(cd, "proxy.key")
	caFile := filepath.Join(cd, "ca.crt")

	serverTLSConf := loadTestTLSConfig(t, proxyCert, proxyKey, caFile)

	innerLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	tlsLn := tls.NewListener(innerLn, serverTLSConf)
	t.Cleanup(func() { tlsLn.Close() })
	proxyAddr := tlsLn.Addr().String()

	director := func(req *http.Request) {
		req.Host = backendURL.Host
		req.URL.Scheme = backendURL.Scheme
		req.URL.Host = backendURL.Host
		req.URL.Path = SingleJoiningSlash(backendURL.Path, req.URL.Path)
		if clientIP, _, err := net.SplitHostPort(req.RemoteAddr); err == nil {
			if prior := req.Header.Get("X-Forwarded-For"); prior != "" {
				clientIP = prior + ", " + clientIP
			}
			req.Header.Set("X-Forwarded-For", clientIP)
			req.Header.Set("X-Real-IP", clientIP)
		}
	}

	proxyTransport := &Transport{
		RoundTripper: &http.Transport{},
	}

	rp := &httputil.ReverseProxy{
		Director:  director,
		Transport: proxyTransport,
	}

	srv := &http.Server{Handler: rp}
	go func() { _ = srv.Serve(tlsLn) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})

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

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	if string(respBody) != "backend-response-ok" {
		t.Errorf("Response body = %q, want %q", string(respBody), "backend-response-ok")
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Status code = %d, want %d", resp.StatusCode, http.StatusOK)
	}

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

	cd := certDir(t)
	serverCert := filepath.Join(cd, "server.crt")
	serverKey := filepath.Join(cd, "server.key")
	caFile := filepath.Join(cd, "ca.crt")

	echoAddr, echoCleanup := integrationTLSEchoServer(t, serverCert, serverKey, caFile)
	t.Cleanup(echoCleanup)

	proxyCert := filepath.Join(cd, "proxy.crt")
	proxyKey := filepath.Join(cd, "proxy.key")

	serverTLSConf := loadTestTLSConfig(t, proxyCert, proxyKey, caFile)

	innerLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	tlsLn := tls.NewListener(innerLn, serverTLSConf)
	t.Cleanup(func() { tlsLn.Close() })
	proxyAddr := tlsLn.Addr().String()

	counter := &Counter{}

	caPool := loadTestCAPool(t, caFile)
	remoteTLSConf := &tls.Config{
		RootCAs:    caPool,
		ServerName: "localhost",
		MinVersion: tls.VersionTLS12,
	}

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
			Ctx:           context.Background(),
			ConnID:        1,
			Log:           slog.With("component", "tcp", "conn", 1),
		}
		p.Start()
	}()

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

	select {
	case <-accepted:
	case <-time.After(testTimeout):
		t.Fatal("Timed out waiting for proxy to accept TLS connection")
	}

	testData := "Hello through double TLS proxy"
	_ = conn.SetDeadline(time.Now().Add(testTimeout))
	if _, err := conn.Write([]byte(testData)); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	got := string(buf[:n])
	if got != testData {
		t.Errorf("Echo mismatch: got %q, want %q", got, testData)
	}

	state := conn.ConnectionState()
	if !state.HandshakeComplete {
		t.Error("TLS handshake not completed")
	}

	conn.Close()
}
