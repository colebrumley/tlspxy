package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"

	log "github.com/Sirupsen/logrus"
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

	cfg, err = getConfig()
	if err != nil {
		log.Fatal(err)
		os.Exit(1)
	}
	flag.Usage = func() {
		fmt.Println("Version:       ", AppVersion, "| Commit", CommitID)
		fmt.Println("Description:    TLSpxy - Tiny TLS termination tool")
		fmt.Println("Usage:          tlspxy [OPTIONS]")
		fmt.Println("Options:")
		m, _ := cfg.Map("")
		prettyPrintFlagMap(m)
		fmt.Println("All options can be set via flags, environment variables, or configuration files.",
			"\n  -> See http://colebrumley.github.io/tlspxy for details.")
	}
	// Load priority => Files < Env < Flag
	cfg.Env().Flag()

	shm = &SigHandlerMux{
		do: map[os.Signal][]func(){},
	}
	go shm.WatchForSignals()
	configLogging(cfg)

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

	// Load the remote config. This will depend on what kind of listener
	// we have configured.
	if remoteTLS, err = configRemoteTLS(cfg); err != nil {
		log.Warningf("Skipping client TLS configuration: %v", err)
		remoteTLS = nil
	}

	switch cfg.UString("server.type", "tcp") {
	case "tcp":
		var (
			remoteTCPAddr *net.TCPAddr
		)
		// Pull remote.addr out of Config and convert to *net.TCPAddr
		if remoteAddr, err = cfg.String("remote.addr"); err != nil {
			log.Error("No remote address defined!")
			os.Exit(1)
		}
		if remoteTCPAddr, err = net.ResolveTCPAddr("tcp", remoteAddr); err != nil {
			log.Error(err)
			os.Exit(1)
		}
		listener := configServerTLS(inner, cfg)
		log.Infof("Opening proxy from %s to %s", serverTCPAddr.String(), remoteTCPAddr.String())
		serveTCP(listener, serverTCPAddr, remoteTCPAddr, cfg.UBool("log.contents", false), remoteTLS)
	case "http", "https":
		var (
			u  *url.URL
			rp *httputil.ReverseProxy
		)
		if u, err = url.Parse(cfg.UString("remote.addr")); err != nil {
			log.Error(err)
		}

		myIP := GetOutboundIP()
		director := func(req *http.Request) {
			oldURL := req.URL.String()
			req.Host = u.Host
			req.URL.Scheme = u.Scheme
			req.URL.Host = u.Host
			req.URL.Path = singleJoiningSlash(u.Path, req.URL.Path)
			req.RemoteAddr = myIP
			if u.RawQuery == "" || req.URL.RawQuery == "" {
				req.URL.RawQuery = u.RawQuery + req.URL.RawQuery
			} else {
				req.URL.RawQuery = u.RawQuery + "&" + req.URL.RawQuery
			}
			if _, ok := req.Header["User-Agent"]; !ok {
				// explicitly disable User-Agent so it's not set to default value
				req.Header.Set("User-Agent", "")
			}
			log.Debugf("Rewrote request URL %s to %s", oldURL, req.URL.String())
		}

		proxy := &ProxyTransport{
			ShowContent: cfg.UBool("log.contents", false),
			RoundTripper: &http.Transport{
				TLSClientConfig: getServerTLSConfig(cfg),
			},
		}
		shm.AddHandler(proxy.InterruptHandler, os.Interrupt, os.Kill)

		rp = &httputil.ReverseProxy{
			Director:  director,
			Transport: proxy,
		}
		log.Infof("Opening proxy from %s to %s", serverTCPAddr.String(), u.String())
		http.Serve(inner, rp)
	default:
		log.Errorln("Unknown server type requested!")
		os.Exit(1)
	}
}
