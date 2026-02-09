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
	"syscall"
	"time"

	"github.com/colebrumley/tlspxy/internal/config"
	"github.com/colebrumley/tlspxy/internal/health"
	"github.com/colebrumley/tlspxy/internal/logging"
	"github.com/colebrumley/tlspxy/internal/metrics"
	"github.com/colebrumley/tlspxy/internal/proxy"
	sighandler "github.com/colebrumley/tlspxy/internal/signal"
	tlsconfig "github.com/colebrumley/tlspxy/internal/tls"
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

	// Create the SigHandlerMux and configure logging
	shm = sighandler.New()
	go shm.WatchForSignals()
	logging.Configure(k)

	// Initialize metrics if enabled
	if k.Bool("metrics.enable") {
		metrics.Init()
		metrics.StartServer(
			k.String("metrics.addr"),
			k.String("metrics.path"),
		)
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
	listener := tlsconfig.ConfigServer(inner, k)

	// Load the remote config. This will depend on what kind of listener
	// we have configured.
	if remoteTLS, err = tlsconfig.ConfigRemote(k); err != nil {
		slog.Warn(fmt.Sprintf("Skipping client TLS configuration: %v", err))
		remoteTLS = nil
	}

	// Create a root context that is cancelled on SIGINT/SIGTERM.
	rootCtx, rootCancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer rootCancel()

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

		// Parse timeout configuration.
		readTimeout, _ := time.ParseDuration(k.String("server.timeouts.read"))
		writeTimeout, _ := time.ParseDuration(k.String("server.timeouts.write"))
		idleTimeout, _ := time.ParseDuration(k.String("server.timeouts.idle"))

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
				Counter:       &ctr,
				ServerConn:    conn,
				ServerAddr:    serverAddr,
				RemoteAddr:    remoteAddr,
				RemoteTLSConf: remoteTLS,
				ErrorSignal:   make(chan bool, 1),
				Ctx:           rootCtx,
				ConnID:        connID,
				ShowContent:   k.Bool("log.contents"),
				Log:           connLog,
				ReadTimeout:   readTimeout,
				WriteTimeout:  writeTimeout,
				IdleTimeout:   idleTimeout,
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
	case "http", "https":
		var (
			u  *url.URL
			rp *httputil.ReverseProxy
		)
		if u, err = url.Parse(k.String("remote.addr")); err != nil {
			slog.Error("Failed to parse remote address", "error", err)
			os.Exit(1)
		}

		director := func(req *http.Request) {
			oldURL := req.URL.String()
			req.Host = u.Host
			req.URL.Scheme = u.Scheme
			req.URL.Host = u.Host
			req.URL.Path = proxy.SingleJoiningSlash(u.Path, req.URL.Path)
			if clientIP, _, err := net.SplitHostPort(req.RemoteAddr); err == nil {
				req.Header.Set("X-Real-IP", clientIP)
				if prior := req.Header.Get("X-Forwarded-For"); prior != "" {
					clientIP = prior + ", " + clientIP
				}
				req.Header.Set("X-Forwarded-For", clientIP)
			}
			if u.RawQuery == "" || req.URL.RawQuery == "" {
				req.URL.RawQuery = u.RawQuery + req.URL.RawQuery
			} else {
				req.URL.RawQuery = u.RawQuery + "&" + req.URL.RawQuery
			}
			if _, ok := req.Header["User-Agent"]; !ok {
				// explicitly disable User-Agent so it's not set to default value
				req.Header.Set("User-Agent", "")
			}
			slog.Debug("Rewrote request URL", "component", "http", "from", oldURL, "to", req.URL.String())
		}

		pt := &proxy.Transport{
			ShowContent: k.Bool("log.contents"),
			RoundTripper: &http.Transport{
				TLSClientConfig: remoteTLS,
			},
		}
		shm.AddHandler(pt.InterruptHandler, os.Interrupt, syscall.SIGTERM)

		rp = &httputil.ReverseProxy{
			Director:  director,
			Transport: pt,
		}
		var handler http.Handler = rp
		if hcPath := k.String("server.healthcheck"); hcPath != "" {
			handler = health.CheckMiddleware(hcPath, rp)
		}
		srv := &http.Server{
			Handler:     handler,
			BaseContext: func(_ net.Listener) context.Context { return rootCtx },
		}
		if maxConns := k.Int("server.maxconns"); maxConns > 0 {
			sem := make(chan struct{}, maxConns)
			slog.Info("Connection limit configured", "maxconns", maxConns)
			srv.ConnState = func(conn net.Conn, state http.ConnState) {
				switch state {
				case http.StateNew:
					select {
					case sem <- struct{}{}:
					default:
						slog.Warn("Connection limit reached, rejecting", "maxconns", cap(sem))
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
		shm.AddHandler(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			srv.Shutdown(ctx)
		}, os.Interrupt, syscall.SIGTERM)

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
