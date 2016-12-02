package main

import (
	"flag"
	"fmt"
	"net"

	"os"

	log "github.com/Sirupsen/logrus"
	"github.com/olebedev/config"
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
	// Load priority => Files < Env < Flag
	cfg.Env().Flag()
}

func main() {

	configLogging(cfg)

	c, _ := config.RenderYaml(cfg.Root)
	log.Debugln("Loaded config:\n", c)

	// Convert addresses to *net.TCPAddr
	l := cfg.UString("server.addr")
	if len(l) <= 0 {
		log.Error("No server address defined!")
		os.Exit(1)
	}
	laddr, err := net.ResolveTCPAddr("tcp", l)
	if err != nil {
		log.Error(err)
	}

	r := cfg.UString("remote.addr")
	if len(r) <= 0 {
		log.Error("No remote address defined!")
		os.Exit(1)
	}
	raddr, err := net.ResolveTCPAddr("tcp", r)
	if err != nil {
		log.Error(err)
	}

	// Create the base TCP listener
	inner, err := net.ListenTCP("tcp", laddr)
	if err != nil {
		log.Error(err)
	}

	rTLS, err := configRemoteTLS(cfg)
	if err != nil {
		log.Warningf("Skipping client TLS configuration: %v", err)
		rTLS = nil
	}

	listener := configServerTLS(inner, cfg)

	showContent := cfg.UBool("log.contents", false)

	log.Infof("Opening proxy from %s to %s", l, r)
	connID := 0
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Errorf("Failed to accept connection '%s'", err)
			continue
		}
		connID++
		log.Infof("Accepted connection #%v from %s", connID, conn.RemoteAddr().String())

		p := &Proxy{
			ServerConn:    conn,
			ServerAddr:    laddr,
			RemoteAddr:    raddr,
			RemoteTLSConf: rTLS,
			ErrorState:    false,
			ErrorSignal:   make(chan bool),
			prefix:        fmt.Sprintf("Connection #%03d ", connID),
			showContent:   showContent,
		}
		go p.start()
	}
}
