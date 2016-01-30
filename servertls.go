package main

import (
	"crypto/tls"
	log "github.com/Sirupsen/logrus"
	"github.com/olebedev/config"
	"net"
	"os"
)

func configServerTLS(inner net.Listener, cfg *config.Config) net.Listener {
	// Load server TLS config from cert files
	cert, _ := cfg.String("server.tls.cert")
	key, _ := cfg.String("server.tls.key")
	ca, _ := cfg.String("server.tls.ca")

	tlsConf, err := LoadTlsConfigFromFiles(cert, key, ca, false)
	if err != nil {
		// If a cert ro key was actually specified, panic
		if len(cert) > 0 || len(key) > 0 {
			log.Errorln("Failed to load requested TLS config: ", err.Error())
			os.Exit(1)
		}

		// Otherwise, continue with non-TLS server
		log.Warningln("No server TLS config loaded")
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
