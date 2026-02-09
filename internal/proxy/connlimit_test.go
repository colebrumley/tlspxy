package proxy

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"sync"
	"testing"
	"time"
)

func TestIntegration_TCP_MaxConns_RejectsExcess(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	const maxConns = 2

	echoAddr, echoCleanup := integrationEchoServer(t)
	t.Cleanup(echoCleanup)

	proxyLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { proxyLn.Close() })
	proxyAddr := proxyLn.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	counter := &Counter{}
	sem := make(chan struct{}, maxConns)

	go func() {
		connID := 0
		for {
			conn, err := proxyLn.Accept()
			if err != nil {
				return
			}
			select {
			case sem <- struct{}{}:
			default:
				conn.Close()
				continue
			}
			connID++
			p := &TCPProxy{
				Counter:     counter,
				ServerConn:  conn,
				ServerAddr:  proxyAddr,
				RemoteAddr:  echoAddr,
				ErrorSignal: make(chan bool, 1),
				Ctx:         ctx,
				ConnID:      connID,
				Log:         slog.With("component", "tcp", "conn", connID),
			}
			go func() {
				defer func() { <-sem }()
				p.Start()
			}()
		}
	}()

	var conns []net.Conn
	for i := 0; i < maxConns; i++ {
		c, err := net.DialTimeout("tcp", proxyAddr, testTimeout)
		if err != nil {
			t.Fatalf("Failed to connect (conn %d): %v", i, err)
		}
		conns = append(conns, c)
		t.Cleanup(func() { c.Close() })
	}

	time.Sleep(50 * time.Millisecond)

	excess, err := net.DialTimeout("tcp", proxyAddr, testTimeout)
	if err != nil {
		return
	}
	defer excess.Close()

	excess.SetDeadline(time.Now().Add(500 * time.Millisecond))
	_, err = excess.Write([]byte("hello"))
	if err != nil {
		return
	}
	buf := make([]byte, 1024)
	_, err = excess.Read(buf)
	if err == nil {
		t.Error("Expected excess connection to be rejected, but it succeeded")
	}
}

func TestIntegration_TCP_MaxConns_Zero_Unlimited(t *testing.T) {
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

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	counter := &Counter{}

	go func() {
		connID := 0
		for {
			conn, err := proxyLn.Accept()
			if err != nil {
				return
			}
			connID++
			p := &TCPProxy{
				Counter:     counter,
				ServerConn:  conn,
				ServerAddr:  proxyAddr,
				RemoteAddr:  echoAddr,
				ErrorSignal: make(chan bool, 1),
				Ctx:         ctx,
				ConnID:      connID,
				Log:         slog.With("component", "tcp", "conn", connID),
			}
			go p.Start()
		}
	}()

	const numConns = 10
	var conns []net.Conn
	for i := 0; i < numConns; i++ {
		c, err := net.DialTimeout("tcp", proxyAddr, testTimeout)
		if err != nil {
			t.Fatalf("Failed to connect (conn %d): %v", i, err)
		}
		conns = append(conns, c)
		t.Cleanup(func() { c.Close() })
	}

	for i, c := range conns {
		testData := fmt.Sprintf("hello-%d", i)
		c.SetDeadline(time.Now().Add(testTimeout))
		if _, err := c.Write([]byte(testData)); err != nil {
			t.Fatalf("Write failed on conn %d: %v", i, err)
		}
		buf := make([]byte, 1024)
		n, err := c.Read(buf)
		if err != nil {
			t.Fatalf("Read failed on conn %d: %v", i, err)
		}
		if got := string(buf[:n]); got != testData {
			t.Errorf("conn %d: got %q, want %q", i, got, testData)
		}
	}
}

func TestIntegration_TCP_MaxConns_ReleasesSlot(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	const maxConns = 1

	echoAddr, echoCleanup := integrationEchoServer(t)
	t.Cleanup(echoCleanup)

	proxyLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { proxyLn.Close() })
	proxyAddr := proxyLn.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	counter := &Counter{}
	sem := make(chan struct{}, maxConns)

	go func() {
		connID := 0
		for {
			conn, err := proxyLn.Accept()
			if err != nil {
				return
			}
			select {
			case sem <- struct{}{}:
			default:
				conn.Close()
				continue
			}
			connID++
			p := &TCPProxy{
				Counter:     counter,
				ServerConn:  conn,
				ServerAddr:  proxyAddr,
				RemoteAddr:  echoAddr,
				ErrorSignal: make(chan bool, 1),
				Ctx:         ctx,
				ConnID:      connID,
				Log:         slog.With("component", "tcp", "conn", connID),
			}
			go func() {
				defer func() { <-sem }()
				p.Start()
			}()
		}
	}()

	c1, err := net.DialTimeout("tcp", proxyAddr, testTimeout)
	if err != nil {
		t.Fatalf("Failed to connect first: %v", err)
	}

	c1.SetDeadline(time.Now().Add(testTimeout))
	if _, err := c1.Write([]byte("first")); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	buf := make([]byte, 1024)
	n, err := c1.Read(buf)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if got := string(buf[:n]); got != "first" {
		t.Errorf("got %q, want %q", got, "first")
	}

	c1.Close()
	time.Sleep(100 * time.Millisecond)

	c2, err := net.DialTimeout("tcp", proxyAddr, testTimeout)
	if err != nil {
		t.Fatalf("Failed to connect second: %v", err)
	}
	defer c2.Close()

	c2.SetDeadline(time.Now().Add(testTimeout))
	if _, err := c2.Write([]byte("second")); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	n, err = c2.Read(buf)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if got := string(buf[:n]); got != "second" {
		t.Errorf("got %q, want %q", got, "second")
	}
}

func TestIntegration_HTTP_MaxConns_RejectsExcess(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	const maxConns = 2

	var mu sync.Mutex
	requestCount := 0
	holdOpen := make(chan struct{})

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount++
		mu.Unlock()
		<-holdOpen
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	}))
	t.Cleanup(func() {
		close(holdOpen)
		backend.Close()
	})

	backendURL, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatal(err)
	}

	proxyLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { proxyLn.Close() })
	proxyAddr := proxyLn.Addr().String()

	director := func(req *http.Request) {
		req.Host = backendURL.Host
		req.URL.Scheme = backendURL.Scheme
		req.URL.Host = backendURL.Host
		req.URL.Path = SingleJoiningSlash(backendURL.Path, req.URL.Path)
	}

	rp := &httputil.ReverseProxy{
		Director:  director,
		Transport: &http.Transport{},
	}

	sem := make(chan struct{}, maxConns)
	srv := &http.Server{
		Handler: rp,
		ConnState: func(conn net.Conn, state http.ConnState) {
			switch state {
			case http.StateNew:
				select {
				case sem <- struct{}{}:
				default:
					conn.Close()
				}
			case http.StateClosed, http.StateHijacked:
				select {
				case <-sem:
				default:
				}
			}
		},
	}

	go srv.Serve(proxyLn)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	})

	var rawConns []net.Conn
	for i := 0; i < maxConns; i++ {
		c, err := net.DialTimeout("tcp", proxyAddr, testTimeout)
		if err != nil {
			t.Fatalf("Failed to dial proxy (conn %d): %v", i, err)
		}
		rawConns = append(rawConns, c)
		t.Cleanup(func() { c.Close() })
	}

	time.Sleep(50 * time.Millisecond)

	excess, err := net.DialTimeout("tcp", proxyAddr, testTimeout)
	if err != nil {
		return
	}
	defer excess.Close()

	excess.SetDeadline(time.Now().Add(500 * time.Millisecond))
	_, err = excess.Write([]byte("GET / HTTP/1.1\r\nHost: localhost\r\n\r\n"))
	if err != nil {
		return
	}
	buf := make([]byte, 1024)
	_, err = excess.Read(buf)
	if err == nil {
		t.Error("Expected excess HTTP connection to be rejected, but got a response")
	}

	for _, c := range rawConns {
		c.Close()
	}
}

func TestIntegration_HTTP_MaxConns_Zero_Unlimited(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	}))
	t.Cleanup(backend.Close)

	backendURL, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatal(err)
	}

	proxyLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { proxyLn.Close() })
	proxyAddr := proxyLn.Addr().String()

	director := func(req *http.Request) {
		req.Host = backendURL.Host
		req.URL.Scheme = backendURL.Scheme
		req.URL.Host = backendURL.Host
		req.URL.Path = SingleJoiningSlash(backendURL.Path, req.URL.Path)
	}

	rp := &httputil.ReverseProxy{
		Director:  director,
		Transport: &http.Transport{},
	}

	srv := &http.Server{Handler: rp}
	go srv.Serve(proxyLn)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	})

	httpClient := &http.Client{Timeout: testTimeout}
	for i := 0; i < 10; i++ {
		resp, err := httpClient.Get(fmt.Sprintf("http://%s/test-%d", proxyAddr, i))
		if err != nil {
			t.Fatalf("Request %d failed: %v", i, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if string(body) != "ok" {
			t.Errorf("Request %d: got body %q, want %q", i, string(body), "ok")
		}
	}
}
