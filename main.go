package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/colebrumley/tlspxy/internal/config"
	"github.com/colebrumley/tlspxy/internal/health"
	"github.com/colebrumley/tlspxy/internal/logging"
	"github.com/colebrumley/tlspxy/internal/metrics"
	"github.com/colebrumley/tlspxy/internal/proxy"
	sighandler "github.com/colebrumley/tlspxy/internal/signal"
	"github.com/colebrumley/tlspxy/internal/sigv4"
	tlsconfig "github.com/colebrumley/tlspxy/internal/tls"
	"golang.org/x/net/http2"
)

// AppVersion is the global application version
var AppVersion string

// CommitID is the current git commit of this build
var CommitID string

func main() {
	// Provide sensible defaults when ldflags are not set.
	if AppVersion == "" {
		AppVersion = "dev"
	}
	if CommitID == "" {
		CommitID = "unknown"
	}

	// Check for -version/--version before anything else
	for _, arg := range os.Args[1:] {
		if arg == "-version" || arg == "--version" {
			fmt.Printf("tlspxy version %s (commit %s)\n", AppVersion, CommitID)
			os.Exit(0)
		}
	}

	var (
		inner                  net.Listener
		serverAddr, remoteAddr string
		serverTCPAddr          *net.TCPAddr
		remoteTLS              *tls.Config
		err                    error
		shm                    *sighandler.SigHandlerMux
	)

	// Pre-parse -config flags from os.Args before loading config.
	// This allows specifying config files/dirs that are loaded
	// before env vars and other flags override them.
	configPaths := config.ParseConfigPaths(os.Args[1:])

	k, err := config.GetConfig(configPaths...)
	if err != nil {
		slog.Error("Failed to load config", "error", err)
		os.Exit(1)
	}

	// Load priority => Files < Env < Flag
	config.LoadEnvVars(k)
	config.LoadFlags(k, AppVersion, CommitID)

	// If no meaningful config was provided (remote.addr still empty and no
	// flags/env/config set), show help instead of a cryptic validation error.
	if k.String("remote.addr") == "" && len(configPaths) == 0 && flag.NFlag() == 0 {
		flag.Usage()
		os.Exit(0)
	}

	if err := config.ValidateConfig(k); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if k.Bool("validate") {
		fmt.Println("Configuration is valid.")
		c, _ := json.MarshalIndent(k.Raw(), "", "  ")
		fmt.Println(string(c))
		os.Exit(0)
	}

	// Create the SigHandlerMux and configure logging
	shm = sighandler.New()
	go shm.WatchForSignals()
	logging.Configure(k)

	// Initialize metrics if enabled
	if k.Bool("metrics.enable") {
		metrics.Init()
		metricsSrv, err := metrics.StartServer(
			k.String("metrics.addr"),
			k.String("metrics.path"),
		)
		if err != nil {
			slog.Error("Failed to start metrics server", "addr", k.String("metrics.addr"), "error", err)
			os.Exit(1)
		}
		shm.AddHandler(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if serr := metricsSrv.Shutdown(ctx); serr != nil {
				slog.Debug("Metrics server shutdown error", "error", serr)
			}
		}, os.Interrupt, syscall.SIGTERM)
	}

	// Print the loaded config if debug is on
	c, _ := json.MarshalIndent(k.Raw(), "", "  ")
	slog.Debug("Loaded config", "config", string(c))

	// Parse the Server listener config
	serverAddr = k.String("server.addr")
	if serverAddr == "" {
		slog.Error("No server address defined!")
		os.Exit(1)
	}
	if serverTCPAddr, err = net.ResolveTCPAddr("tcp", serverAddr); err != nil {
		slog.Error("Failed to resolve server address", "error", err)
		os.Exit(1)
	}

	// Create the base TCP listener. Both TCP and HTTP proxies will
	// be based on this listener.
	if inner, err = net.ListenTCP("tcp", serverTCPAddr); err != nil {
		slog.Error("Failed to listen", "error", err)
		os.Exit(1)
	}

	// Configure the server's TLS settings
	listener, certStore, err := tlsconfig.ConfigServer(inner, k)
	if err != nil {
		slog.Error("Failed to configure server TLS", "error", err)
		inner.Close()
		os.Exit(1)
	}

	if certStore != nil {
		shm.AddHandler(func() {
			if err := certStore.ReloadAll(); err != nil {
				slog.Error("Failed to reload certificates", "error", err)
			} else {
				slog.Info("Certificates reloaded successfully")
			}
		}, syscall.SIGHUP)
		slog.Info("Certificate hot-reload enabled (send SIGHUP to reload)")
	}

	// Load the remote config. This will depend on what kind of listener
	// we have configured.
	if remoteTLS, err = tlsconfig.ConfigRemote(k); err != nil {
		slog.Error("Failed to configure remote TLS", "error", err)
		listener.Close()
		os.Exit(1)
	}

	// Durations were validated by ValidateConfig above, so config.Duration can
	// safely resolve them; empty values yield 0 (unbounded/disabled).
	readTimeout := config.Duration(k, "server.timeouts.read")
	writeTimeout := config.Duration(k, "server.timeouts.write")
	idleTimeout := config.Duration(k, "server.timeouts.idle")
	handshakeTimeout := config.Duration(k, "server.timeouts.handshake")
	dialTimeout := config.Duration(k, "remote.timeouts.dial")

	// Create a root context that is cancelled on SIGINT/SIGTERM.
	rootCtx, rootCancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer rootCancel()

	// Optionally watch cert/key files and reload automatically on change.
	// This complements the SIGHUP handler and is recommended for containerized
	// deployments with mounted secrets. Config asked for it, so treat a failure
	// to establish the watcher as fatal (fail-closed like the rest of startup).
	if certStore != nil && k.Bool("server.tls.autoreload") {
		if err := certStore.Watch(rootCtx); err != nil {
			slog.Error("Failed to start certificate auto-reload watcher", "error", err)
			listener.Close()
			os.Exit(1)
		}
		slog.Info("Certificate auto-reload enabled (watching cert files)")
	}

	switch k.String("server.type") {
	case "tcp":
		// Pull remote.addr out of Config and convert to *net.TCPAddr
		remoteAddr = k.String("remote.addr")
		if remoteAddr == "" {
			slog.Error("No remote address defined!")
			os.Exit(1)
		}
		slog.Info("Opening proxy", "from", serverAddr, "to", remoteAddr)
		ctr := proxy.Counter{}
		shm.AddHandler(ctr.InterruptHandler, os.Interrupt, syscall.SIGTERM)

		// When root context is cancelled, close the listener to break
		// out of the Accept loop.
		go func() {
			<-rootCtx.Done()
			listener.Close()
		}()

		var sem chan struct{}
		if maxConns := k.Int("server.maxconns"); maxConns > 0 {
			sem = make(chan struct{}, maxConns)
			slog.Info("Connection limit configured", "maxconns", maxConns)
		}

		var wg sync.WaitGroup
		connID := 0
		for {
			conn, err := listener.Accept()
			if err != nil {
				// If the context was cancelled, this is a clean shutdown.
				if rootCtx.Err() != nil {
					slog.Info("Listener closed, shutting down")
					break
				}
				slog.Error("Failed to accept connection", "error", err)
				continue
			}
			if sem != nil {
				select {
				case sem <- struct{}{}:
				default:
					slog.Warn("Connection limit reached, rejecting", "maxconns", cap(sem))
					conn.Close()
					continue
				}
			}
			connID++
			connLog := slog.With("component", "tcp", "conn", connID)
			connLog.Info("Accepted connection", "src", conn.RemoteAddr().String())

			p := &proxy.TCPProxy{
				Counter:          &ctr,
				ServerConn:       conn,
				ServerAddr:       serverAddr,
				RemoteAddr:       remoteAddr,
				RemoteTLSConf:    remoteTLS,
				ErrorSignal:      make(chan bool, 1),
				Ctx:              rootCtx,
				ConnID:           connID,
				ShowContent:      k.Bool("log.contents"),
				Log:              connLog,
				ReadTimeout:      readTimeout,
				WriteTimeout:     writeTimeout,
				IdleTimeout:      idleTimeout,
				HandshakeTimeout: handshakeTimeout,
				DialTimeout:      dialTimeout,
				ProxyProto:       k.String("remote.proxyprotocol"),
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() {
					if sem != nil {
						<-sem
					}
				}()
				p.Start()
			}()
		}

		// Wait for in-flight connection goroutines to finish their cleanup,
		// bounded by a 30s timeout to avoid hanging indefinitely on shutdown.
		done := make(chan struct{})
		go func() { wg.Wait(); close(done) }()
		select {
		case <-done:
			slog.Info("All connections drained")
		case <-time.After(30 * time.Second):
			slog.Warn("Drain timeout exceeded, forcing exit")
		}
	case "http", "https":
		var (
			u  *url.URL
			rp *httputil.ReverseProxy
		)
		if u, err = url.Parse(k.String("remote.addr")); err != nil {
			slog.Error("Failed to parse remote address", "error", err)
			os.Exit(1)
		}

		// trustXFF controls whether inbound X-Forwarded-For headers are
		// trusted. As an edge proxy the default is secure: reset XFF to the
		// real peer. Enable server.trustxff only when tlspxy sits
		// behind another trusted proxy.
		trustXFF := k.Bool("server.trustxff")

		pt := &proxy.Transport{
			ShowContent: k.Bool("log.contents"),
			RoundTripper: &http.Transport{
				// Backend connection establishment (TCP dial + TLS handshake) is
				// bounded by the dedicated dial timeout. ResponseHeaderTimeout uses
				// the read timeout ("how long to wait for the backend to start
				// responding") and stays 0/unbounded when read timeout is unset so
				// long-polling backends are not broken.
				DialContext:           (&net.Dialer{Timeout: dialTimeout}).DialContext,
				TLSClientConfig:       remoteTLS,
				TLSHandshakeTimeout:   dialTimeout,
				ResponseHeaderTimeout: readTimeout,
				IdleConnTimeout:       idleTimeout,
			},
		}
		shm.AddHandler(pt.InterruptHandler, os.Interrupt, syscall.SIGTERM)

		rp = &httputil.ReverseProxy{
			Rewrite:   proxy.NewRewrite(u, trustXFF),
			Transport: pt,
		}

		// When the SigV4 gateway is enabled, it replaces the plain reverse
		// proxy: it verifies inbound client signatures, re-signs with mapped
		// AWS credentials, and forwards to the AWS endpoint. Startup is
		// fail-closed — a broken keystore/credential/target config aborts.
		var handler http.Handler = rp
		if k.Bool("sigv4.enable") {
			clockSkew := config.Duration(k, "sigv4.clockskew")
			gw, gwErr := sigv4.Build(rootCtx, k, pt, clockSkew)
			if gwErr != nil {
				slog.Error("Failed to initialize SigV4 gateway", "error", gwErr)
				listener.Close()
				os.Exit(1)
			}
			handler = gw.Handler
			slog.Info("SigV4 credential-translation gateway enabled", "keys", gw.Keystore.Len())

			shm.AddHandler(func() {
				if err := gw.Keystore.ReloadAll(); err != nil {
					slog.Error("Failed to reload SigV4 keystore", "error", err)
				} else {
					slog.Info("SigV4 keystore reloaded successfully", "keys", gw.Keystore.Len())
				}
			}, syscall.SIGHUP)
			slog.Info("SigV4 keystore hot-reload enabled (send SIGHUP to reload)")

			if k.Bool("sigv4.autoreload") {
				if err := gw.Keystore.Watch(rootCtx); err != nil {
					slog.Error("Failed to start SigV4 keystore auto-reload watcher", "error", err)
					listener.Close()
					os.Exit(1)
				}
				slog.Info("SigV4 keystore auto-reload enabled (watching keystore file)")
			}
		}

		if hcPath := k.String("server.healthcheck"); hcPath != "" {
			handler = health.CheckMiddleware(hcPath, handler)
		}
		srv := &http.Server{
			Handler:      handler,
			ReadTimeout:  readTimeout,
			WriteTimeout: writeTimeout,
			IdleTimeout:  idleTimeout,
			BaseContext:  func(_ net.Listener) context.Context { return rootCtx },
		}
		if maxConns := k.Int("server.maxconns"); maxConns > 0 {
			sem := make(chan struct{}, maxConns)
			slog.Info("Connection limit configured", "maxconns", maxConns)
			// Track which connections actually acquired a semaphore slot so
			// that rejected connections (closed at StateNew without a slot) do
			// not release a token belonging to a different live connection.
			var mu sync.Mutex
			acquired := make(map[net.Conn]struct{})
			srv.ConnState = func(conn net.Conn, state http.ConnState) {
				switch state {
				case http.StateNew:
					select {
					case sem <- struct{}{}:
						mu.Lock()
						acquired[conn] = struct{}{}
						mu.Unlock()
					default:
						slog.Warn("Connection limit reached, rejecting", "maxconns", cap(sem))
						conn.Close()
					}
				case http.StateClosed, http.StateHijacked:
					mu.Lock()
					if _, ok := acquired[conn]; ok {
						delete(acquired, conn)
						<-sem
					}
					mu.Unlock()
				}
			}
		}
		shm.AddHandler(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := srv.Shutdown(ctx); err != nil {
				slog.Debug("HTTP server shutdown error", "error", err)
			}
		}, os.Interrupt, syscall.SIGTERM)

		if k.Bool("server.http2") {
			if err := http2.ConfigureServer(srv, &http2.Server{}); err != nil {
				slog.Error("Failed to configure HTTP/2 server", "error", err)
			}
			if baseTransport, ok := pt.RoundTripper.(*http.Transport); ok {
				if err := http2.ConfigureTransport(baseTransport); err != nil {
					slog.Error("Failed to configure HTTP/2 transport", "error", err)
				}
			}
			slog.Info("HTTP/2 enabled")
		}

		slog.Info("Opening proxy", "from", serverTCPAddr.String(), "to", u.String())
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			slog.Error("Server error", "error", err)
			os.Exit(1)
		}
	default:
		slog.Error("Unknown server type requested!")
		os.Exit(1)
	}
}
