package main

import (
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	log "github.com/Sirupsen/logrus"
	syslogrus "github.com/Sirupsen/logrus/hooks/syslog"
	"github.com/olebedev/config"
	"io/ioutil"
	"log/syslog"
	"net"
	"os"
	"strings"
)

var cfg *config.Config

func init() {
	var err error
	// Load config from local yml/json file, env, or flags
	cfg, err = getConfig()
	if err != nil {
		log.Fatal(err)
	}
	flag.Usage = func() {
		fmt.Println("Description:    TLSpxy - Tiny TLS termination tool")
		fmt.Println("Usage:          tlspxy [OPTIONS] (see docs/configuration.md)\nOptions:")
		m, _ := cfg.Map("")
		prettyPrintFlagMap(m, []string{})
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
	log.Infof("Opening proxy from %s to %s", l, r)
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

func configRemoteTLS(cfg *config.Config) (tlsConf *tls.Config, err error) {
	cert, _ := cfg.String("remote.tls.cert")
	key, _ := cfg.String("remote.tls.key")
	ca, _ := cfg.String("remote.tls.ca")
	doVerify, _ := cfg.Bool("remote.tls.verify")
	useSysRoots, _ := cfg.Bool("remote.tls.sysroots")

	if fileExists(cert) && fileExists(key) {
		log.Debugf("Loading remote TLS config: [cert: %s, key: %s, ca: %s, SystemRoots: %v]", cert, key, ca, useSysRoots)
		tlsConf, err = LoadTlsConfigFromFiles(cert, key, ca, useSysRoots)
		if err != nil {
			return
		}
		log.Debugln("Loading remote TLS config succeeded")
	} else if doVerify || useSysRoots {
		// Just load system CAs
		log.Debugf("Loading default remote TLS config [verify: %v, system roots: %v]", doVerify, useSysRoots)
		capool := x509.NewCertPool()
		SetSystemCAPool(capool)
		tlsConf = &tls.Config{
			RootCAs:   capool,
			ClientCAs: capool,
		}
	} else {
		tlsConf = nil
		err = nil
		return
	}

	if !doVerify {
		tlsConf.InsecureSkipVerify = true
	}
	return
}

func configServerTLS(inner net.Listener, cfg *config.Config) net.Listener {
	// Load server TLS config from cert files
	cert, _ := cfg.String("server.tls.cert")
	key, _ := cfg.String("server.tls.key")
	ca, _ := cfg.String("server.tls.ca")

	tlsConf, err := LoadTlsConfigFromFiles(cert, key, ca, false)
	if err != nil {
		log.Warningln("Could not load server TLS config: ", err.Error())
		log.Infoln("Proceeding with non-TLS server")
		return inner
	} else {
		log.Debugf("Loaded TLS config: [cert: %s, key: %s, ca: %s]", cert, key, ca)
		// Parse the other TLS options.
		//   'verify' overrides 'require'
		if v, _ := cfg.Bool("server.tls.require"); v && tlsConf != nil {
			log.Debugf("Setting server.tls.require -> %v", v)
			tlsConf.ClientAuth = tls.RequireAnyClientCert
		}
		if v, _ := cfg.Bool("server.tls.verify"); v && tlsConf != nil {
			log.Debugf("Setting server.tls.verify -> %v", v)
			tlsConf.ClientAuth = tls.RequireAndVerifyClientCert
		}
	}

	// Wrap it with our TLS config & return
	return tls.NewListener(inner, tlsConf)
}

func configLogging(cfg *config.Config) {
	// Set verbosity
	verbosity, _ := cfg.String("log.level")
	switch verbosity {
	case "debug":
		log.SetLevel(log.DebugLevel)
	case "warning":
		log.SetLevel(log.WarnLevel)
	case "error":
		log.SetLevel(log.ErrorLevel)
	default:
		verbosity = "info"
		log.SetLevel(log.InfoLevel)
	}

	log.SetOutput(os.Stdout)

	logDest, _ := cfg.String("log.destination")
	if len(logDest) == 0 {
		logDest = "stdout"
	}

	if strings.HasPrefix(logDest, "syslog://") {
		addr := strings.TrimPrefix(logDest, "syslog://")
		hook, err := syslogrus.NewSyslogHook("udp", addr, syslog.LOG_INFO, "tlspxy")
		if err != nil {
			log.Error("Unable to connect to local syslog daemon")
		} else {
			log.AddHook(hook)
		}
		log.SetOutput(ioutil.Discard)
		return
	}

	log.Debugf("Log Settings: [level: %s, dest: %s]", strings.ToUpper(verbosity), logDest)
}
