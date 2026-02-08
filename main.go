package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/olebedev/config"
)

// AppVersion is the global application version
var AppVersion string

// CommitID is the current git commit of this build
var CommitID string

func main() {
	var (
		inner                  net.Listener
		serverAddr, remoteAddr string
		serverTCPAddr          *net.TCPAddr
		remoteTLS              *tls.Config
		err                    error
		shm                    *SigHandlerMux
	)

	// Pre-parse -config flags from os.Args before loading config.
	// This allows specifying config files/dirs that are loaded
	// before env vars and other flags override them.
	configPaths := parseConfigPaths(os.Args[1:])

	cfg, err = getConfig(configPaths...)
	if err != nil {
		log.Fatal(err)
	}
	flag.Usage = func() {
		fmt.Println("Version:       ", AppVersion, "| Commit", CommitID)
		fmt.Println("Description:    TLSpxy - Tiny TLS termination tool")
		fmt.Println("Usage:          tlspxy [OPTIONS]")
		fmt.Println("Options:")
		m, _ := cfg.Map("")
		prettyPrintFlagMap(m)
		fmt.Println("All options can be set via flags, environment variables, or configuration files.",
			"\n  -> See https://github.com/colebrumley/tlspxy/wiki/Configuration for details.")
	}
	// Load priority => Files < Env < Flag
	cfg.Env().Flag()

	// Create the SigHandlerMux and configure logging
	shm = &SigHandlerMux{
		do: map[os.Signal][]func(){},
	}
	go shm.WatchForSignals()
	configLogging(cfg)

	// Print the loaded config if debug is on
	c, _ := config.RenderYaml(cfg.Root)
	log.Debugln("Loaded config:\n", c)

	// Parse the Server listener config
	if serverAddr, err = cfg.String("server.addr"); err != nil {
		log.Error("No server address defined!")
		os.Exit(1)
	}
	if serverTCPAddr, err = net.ResolveTCPAddr("tcp", serverAddr); err != nil {
		log.Error(err)
		os.Exit(1)
	}

	// Create the base TCP listener. Both TCP and HTTP proxies will
	// be based on this listener.
	if inner, err = net.ListenTCP("tcp", serverTCPAddr); err != nil {
		log.Error(err)
	}

	// Configure the server's TLS settings
	listener := configServerTLS(inner, cfg)

	// Load the remote config. This will depend on what kind of listener
	// we have configured.
	if remoteTLS, err = configRemoteTLS(cfg); err != nil {
		log.Warningf("Skipping client TLS configuration: %v", err)
		remoteTLS = nil
	}

	switch cfg.UString("server.type", "tcp") {
	case "tcp":
		// Pull remote.addr out of Config and convert to *net.TCPAddr
		if remoteAddr, err = cfg.String("remote.addr"); err != nil {
			log.Error("No remote address defined!")
			os.Exit(1)
		}
		log.Infof("Opening proxy from %s to %s", serverAddr, remoteAddr)
		ctr := ProxyCounter{}
		shm.AddHandler(ctr.InterruptHandler, os.Interrupt, syscall.SIGTERM)
		connID := 0
		for {
			conn, err := listener.Accept()
			if err != nil {
				log.Errorf("Failed to accept connection '%s'", err)
				continue
			}
			connID++
			connLog := log.WithFields(log.Fields{
				"component": "tcp",
				"conn":      connID,
			})
			connLog.WithField("src", conn.RemoteAddr().String()).Info("Accepted connection")

			p := &TCPProxy{
				Counter:       &ctr,
				ServerConn:    conn,
				ServerAddr:    serverAddr,
				RemoteAddr:    remoteAddr,
				RemoteTLSConf: remoteTLS,
				ErrorSignal:   make(chan bool),
				connID:        connID,
				showContent:   cfg.UBool("log.contents", false),
				log:           connLog,
			}
			go p.start()
		}
	case "http", "https":
		var (
			u  *url.URL
			rp *httputil.ReverseProxy
		)
		if u, err = url.Parse(cfg.UString("remote.addr")); err != nil {
			log.Error(err)
		}

		director := func(req *http.Request) {
			oldURL := req.URL.String()
			req.Host = u.Host
			req.URL.Scheme = u.Scheme
			req.URL.Host = u.Host
			req.URL.Path = singleJoiningSlash(u.Path, req.URL.Path)
			if clientIP, _, err := net.SplitHostPort(req.RemoteAddr); err == nil {
				if prior := req.Header.Get("X-Forwarded-For"); prior != "" {
					clientIP = prior + ", " + clientIP
				}
				req.Header.Set("X-Forwarded-For", clientIP)
				req.Header.Set("X-Real-IP", clientIP)
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
			httpLog.WithFields(log.Fields{
				"from": oldURL,
				"to":   req.URL.String(),
			}).Debug("Rewrote request URL")
		}

		proxy := &ProxyTransport{
			ShowContent: cfg.UBool("log.contents", false),
			RoundTripper: &http.Transport{
				TLSClientConfig: remoteTLS,
			},
		}
		shm.AddHandler(proxy.InterruptHandler, os.Interrupt, syscall.SIGTERM)

		rp = &httputil.ReverseProxy{
			Director:  director,
			Transport: proxy,
		}
		srv := &http.Server{Handler: rp}
		shm.AddHandler(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			srv.Shutdown(ctx)
		}, os.Interrupt, syscall.SIGTERM)

		log.Infof("Opening proxy from %s to %s", serverTCPAddr.String(), u.String())
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	default:
		log.Errorln("Unknown server type requested!")
		os.Exit(1)
	}
}
