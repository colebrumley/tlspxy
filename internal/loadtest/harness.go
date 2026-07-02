package loadtest

import (
	"bytes"
	"context"
	cryptotls "crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"sync"

	"github.com/colebrumley/tlspxy/internal/proxy"
	tlsutil "github.com/colebrumley/tlspxy/internal/tls"
)

// quietLog returns a logger that discards all output, used to suppress
// verbose proxy logging during load tests.
var quietLog = slog.New(slog.NewTextHandler(io.Discard, nil))

// HarnessConfig configures a TestHarness.
type HarnessConfig struct {
	Mode         string // "tcp" or "http"
	ServerTLS    bool
	RemoteTLS    bool
	MutualTLS    bool
	MaxConns     int
	HTTP2        bool
	CertDir      string // path to contrib/testdata/certs/
	ResponseSize int    // HTTP mode backend response size (0 = "ok")
}

// TestHarness manages a complete proxy test environment: backend, proxy, and
// client TLS configuration. It supports both TCP and HTTP proxy modes.
type TestHarness struct {
	ProxyAddr     string
	ClientTLSConf *cryptotls.Config
	CertStore     *tlsutil.CertStore

	backendLn net.Listener
	proxyLn   net.Listener
	cancel    context.CancelFunc
	srv       *http.Server
	wg        sync.WaitGroup
}

// NewHarness creates and starts a fully wired proxy test environment.
func NewHarness(cfg HarnessConfig) (*TestHarness, error) {
	h := &TestHarness{}

	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel

	// Build client TLS config (used by callers to connect to the proxy).
	if cfg.ServerTLS {
		clientConf, err := buildClientTLSConf(cfg)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("client TLS config: %w", err)
		}
		h.ClientTLSConf = clientConf
	}

	switch cfg.Mode {
	case "tcp":
		if err := h.setupTCP(ctx, cfg); err != nil {
			cancel()
			return nil, err
		}
	case "http":
		if err := h.setupHTTP(ctx, cfg); err != nil {
			cancel()
			return nil, err
		}
	default:
		cancel()
		return nil, fmt.Errorf("unsupported mode: %q", cfg.Mode)
	}

	return h, nil
}

// Close shuts down the harness: cancels the context, closes listeners, and
// shuts down the HTTP server if present.
func (h *TestHarness) Close() {
	h.cancel()
	if h.proxyLn != nil {
		h.proxyLn.Close()
	}
	if h.backendLn != nil {
		h.backendLn.Close()
	}
	if h.srv != nil {
		h.srv.Close()
	}
	h.wg.Wait()
}

// TriggerReload calls CertStore.ReloadAll to simulate a SIGHUP cert reload.
func (h *TestHarness) TriggerReload() error {
	if h.CertStore == nil {
		return fmt.Errorf("no CertStore configured (ServerTLS not enabled)")
	}
	return h.CertStore.ReloadAll()
}

// setupTCP starts an echo backend and a TCP proxy in front of it.
func (h *TestHarness) setupTCP(ctx context.Context, cfg HarnessConfig) error {
	// 1. Start echo backend.
	backendLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("backend listen: %w", err)
	}
	h.backendLn = backendLn

	// Optionally wrap backend with TLS.
	var backendAcceptLn net.Listener = backendLn
	if cfg.RemoteTLS {
		backendTLSConf, err := buildBackendTLSConf(cfg)
		if err != nil {
			return fmt.Errorf("backend TLS config: %w", err)
		}
		backendAcceptLn = cryptotls.NewListener(backendLn, backendTLSConf)
	}

	// Accept loop for echo backend.
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		for {
			conn, err := backendAcceptLn.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}(conn)
		}
	}()

	// 2. Create proxy listener.
	proxyLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("proxy listen: %w", err)
	}
	h.proxyLn = proxyLn

	var proxyAcceptLn net.Listener = proxyLn
	if cfg.ServerTLS {
		proxyAcceptLn, err = h.wrapWithServerTLS(proxyLn, cfg)
		if err != nil {
			return fmt.Errorf("proxy server TLS: %w", err)
		}
	}
	h.ProxyAddr = proxyLn.Addr().String()

	// Build remote TLS config for the proxy → backend connection.
	var remoteTLSConf *cryptotls.Config
	if cfg.RemoteTLS {
		remoteTLSConf, err = buildRemoteTLSConf(cfg)
		if err != nil {
			return fmt.Errorf("remote TLS config: %w", err)
		}
	}

	// Connection semaphore.
	var sem chan struct{}
	if cfg.MaxConns > 0 {
		sem = make(chan struct{}, cfg.MaxConns)
	}

	counter := &proxy.Counter{}
	connID := 0

	// 3. Accept loop for proxy.
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		for {
			conn, err := proxyAcceptLn.Accept()
			if err != nil {
				return
			}
			if sem != nil {
				select {
				case sem <- struct{}{}:
				default:
					conn.Close()
					continue
				}
			}
			connID++
			p := &proxy.TCPProxy{
				Counter:       counter,
				ServerConn:    conn,
				ServerAddr:    h.ProxyAddr,
				RemoteAddr:    backendLn.Addr().String(),
				RemoteTLSConf: remoteTLSConf,
				ErrorSignal:   make(chan bool, 1),
				Ctx:           ctx,
				ConnID:        connID,
				Log:           quietLog,
			}
			go func() {
				defer func() {
					if sem != nil {
						<-sem
					}
				}()
				p.Start()
			}()
		}
	}()

	return nil
}

// setupHTTP starts an HTTP backend and an HTTP reverse proxy in front of it.
func (h *TestHarness) setupHTTP(ctx context.Context, cfg HarnessConfig) error {
	// 1. Start HTTP backend.
	var responseBody []byte
	if cfg.ResponseSize > 0 {
		responseBody = bytes.Repeat([]byte("x"), cfg.ResponseSize)
	} else {
		responseBody = []byte("ok")
	}

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(responseBody)
	}))
	// Track the backend via a stub listener so Close() can shut it down.
	// httptest.Server manages its own listener, so we record nil for backendLn
	// and shut down via srv/cancel instead. We'll close the backend in a cleanup goroutine.
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		<-ctx.Done()
		backend.Close()
	}()

	backendURL, err := url.Parse(backend.URL)
	if err != nil {
		return fmt.Errorf("parse backend URL: %w", err)
	}

	// 2. Create proxy listener.
	proxyLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("proxy listen: %w", err)
	}
	h.proxyLn = proxyLn

	var serveLn net.Listener = proxyLn
	if cfg.ServerTLS {
		serveLn, err = h.wrapWithServerTLS(proxyLn, cfg)
		if err != nil {
			return fmt.Errorf("proxy server TLS: %w", err)
		}
	}
	h.ProxyAddr = proxyLn.Addr().String()

	// 3. Create reverse proxy with proxy.Transport.
	director := func(req *http.Request) {
		req.Host = backendURL.Host
		req.URL.Scheme = backendURL.Scheme
		req.URL.Host = backendURL.Host
		req.URL.Path = proxy.SingleJoiningSlash(backendURL.Path, req.URL.Path)
		if clientIP, _, err := net.SplitHostPort(req.RemoteAddr); err == nil {
			if prior := req.Header.Get("X-Forwarded-For"); prior != "" {
				clientIP = prior + ", " + clientIP
			}
			req.Header.Set("X-Forwarded-For", clientIP)
			req.Header.Set("X-Real-IP", clientIP)
		}
	}

	pt := &proxy.Transport{
		RoundTripper: &http.Transport{},
	}

	rp := &httputil.ReverseProxy{
		Director:  director,
		Transport: pt,
		ErrorLog:  log.New(io.Discard, "", 0),
	}

	// 4. Create and start HTTP server.
	h.srv = &http.Server{
		Handler:     rp,
		BaseContext: func(_ net.Listener) context.Context { return ctx },
		ErrorLog:    log.New(io.Discard, "", 0),
	}

	// Connection semaphore for HTTP mode.
	if cfg.MaxConns > 0 {
		sem := make(chan struct{}, cfg.MaxConns)
		h.srv.ConnState = func(conn net.Conn, state http.ConnState) {
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
		}
	}

	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		if err := h.srv.Serve(serveLn); err != nil && err != http.ErrServerClosed {
			_ = err // suppress noise during load tests
		}
	}()

	return nil
}

// wrapWithServerTLS creates a CertStore and wraps the listener with TLS.
func (h *TestHarness) wrapWithServerTLS(ln net.Listener, cfg HarnessConfig) (net.Listener, error) {
	certPath := filepath.Join(cfg.CertDir, "proxy.crt")
	keyPath := filepath.Join(cfg.CertDir, "proxy.key")
	caPath := filepath.Join(cfg.CertDir, "ca.crt")

	cs, err := tlsutil.NewCertStore(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("creating CertStore: %w", err)
	}
	h.CertStore = cs

	tlsConf := &cryptotls.Config{
		GetCertificate: cs.GetCertificate,
		MinVersion:     cryptotls.VersionTLS12,
	}

	if cfg.MutualTLS {
		caPool, err := loadCAPool(caPath)
		if err != nil {
			return nil, fmt.Errorf("loading CA for mTLS: %w", err)
		}
		tlsConf.ClientCAs = caPool
		tlsConf.ClientAuth = cryptotls.RequireAndVerifyClientCert
	}

	return cryptotls.NewListener(ln, tlsConf), nil
}

// buildClientTLSConf builds the TLS config clients use to connect to the proxy.
func buildClientTLSConf(cfg HarnessConfig) (*cryptotls.Config, error) {
	caPath := filepath.Join(cfg.CertDir, "ca.crt")
	caPool, err := loadCAPool(caPath)
	if err != nil {
		return nil, err
	}

	conf := &cryptotls.Config{
		RootCAs:    caPool,
		ServerName: "localhost",
		MinVersion: cryptotls.VersionTLS12,
	}

	if cfg.MutualTLS {
		clientCert, err := cryptotls.LoadX509KeyPair(
			filepath.Join(cfg.CertDir, "client.crt"),
			filepath.Join(cfg.CertDir, "client.key"),
		)
		if err != nil {
			return nil, fmt.Errorf("loading client cert: %w", err)
		}
		conf.Certificates = []cryptotls.Certificate{clientCert}
	}

	return conf, nil
}

// buildBackendTLSConf builds the TLS config for the echo backend server.
func buildBackendTLSConf(cfg HarnessConfig) (*cryptotls.Config, error) {
	cert, err := cryptotls.LoadX509KeyPair(
		filepath.Join(cfg.CertDir, "server.crt"),
		filepath.Join(cfg.CertDir, "server.key"),
	)
	if err != nil {
		return nil, fmt.Errorf("loading server cert: %w", err)
	}

	conf := &cryptotls.Config{
		Certificates: []cryptotls.Certificate{cert},
		MinVersion:   cryptotls.VersionTLS12,
	}

	caPool, err := loadCAPool(filepath.Join(cfg.CertDir, "ca.crt"))
	if err != nil {
		return nil, err
	}
	conf.ClientCAs = caPool

	return conf, nil
}

// buildRemoteTLSConf builds the TLS config the proxy uses to connect to the backend.
func buildRemoteTLSConf(cfg HarnessConfig) (*cryptotls.Config, error) {
	caPool, err := loadCAPool(filepath.Join(cfg.CertDir, "ca.crt"))
	if err != nil {
		return nil, err
	}
	return &cryptotls.Config{
		RootCAs:    caPool,
		ServerName: "localhost",
		MinVersion: cryptotls.VersionTLS12,
	}, nil
}

// loadCAPool reads a PEM-encoded CA certificate file and returns a cert pool.
func loadCAPool(caPath string) (*x509.CertPool, error) {
	caPEM, err := os.ReadFile(caPath)
	if err != nil {
		return nil, fmt.Errorf("reading CA file: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("failed to parse CA cert from %s", caPath)
	}
	return pool, nil
}
