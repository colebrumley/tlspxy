package main

import (
	"flag"
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/olebedev/config"
	"net"
)

func init() {
	var err error
	// Load config from local yml/json file, env, or flags
	cfg, err = getConfig()
	if err != nil {
		log.Fatal(err)
	}
	flag.Usage = func() {
		fmt.Println("Description:    TLSpxy - Tiny TLS termination tool")
		fmt.Println("Usage:          tlspxy [OPTIONS]")
		fmt.Println("Options:")
		m, _ := cfg.Map("")
		prettyPrintFlagMap(m, []string{})
		fmt.Println("All options can be set via flags, environment variables, or configuration files.",
			"\n  -> See http://colebrumley.github.io/tlspxy for details.")
	}
	cfg.Env().Flag()
}

func main() {

	configLogging(cfg)

	c, _ := config.RenderYaml(cfg.Root)
	log.Debugln("Loaded config:\n", c)

	// Convert addresses to *net.TCPAddr
	l, _ := cfg.String("server.addr")
	laddr, err := net.ResolveTCPAddr("tcp", l)
	if err != nil {
		log.Error(err)
	}
	r, _ := cfg.String("remote.addr")
	raddr, err := net.ResolveTCPAddr("tcp", r)
	if err != nil {
		log.Error(err)
	}

	// Create the base TCP listener
	inner, err := net.ListenTCP("tcp", laddr)
	if err != nil {
		log.Error(err)
	}

	rTls, err := configRemoteTLS(cfg)
	if err != nil {
		log.Warningf("Skipping client TLS configuration: %v", err)
		rTls = nil
	}

	listener := configServerTLS(inner, cfg)

	connId := 0
	showContent, _ := cfg.Bool("log.contents")

	log.Infof("Opening proxy from %s to %s", l, r)
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Errorf("Failed to accept connection '%s'", err)
			continue
		}
		connId++

		p := &Proxy{
			ServerConn:    conn,
			ServerAddr:    laddr,
			RemoteAddr:    raddr,
			RemoteTlsConf: rTls,
			ErrorState:    false,
			ErrorSignal:   make(chan bool),
			prefix:        fmt.Sprintf("Connection #%03d ", connId),
			showContent:   showContent,
		}
		go p.start()
	}
}
