package main

import (
	"crypto/tls"
	"net"
	"os"

	"golang.org/x/crypto/acme/autocert"

	log "github.com/Sirupsen/logrus"
	"github.com/olebedev/config"
)

func configServerTLS(inner net.Listener, cfg *config.Config) net.Listener {
	// Load server TLS config from cert files
	cert, _ := cfg.String("server.tls.cert")
	key, _ := cfg.String("server.tls.key")
	ca, _ := cfg.String("server.tls.ca")
	useLetsencrypt := cfg.UBool("server.tls.letsencrypt.enable", false)

	r := inner

	// Check for whether server.tls.letsencrypt.enable is true,
	// and load a LetsEncrypt cert if so.
	if useLetsencrypt {
		m := *getCertManager(
			cfg.UString("server.tls.letsencrypt.domain"),
			cfg.UString("server.tls.letsencrypt.cachedir"),
			cfg.UString("server.tls.letsencrypt.email"),
		)

		r = tls.NewListener(inner, &tls.Config{GetCertificate: m.GetCertificate})
	} else {
		tlsConf, err := LoadTLSConfigFromFiles(cert, key, ca, false)
		if err != nil {
			// If a cert ro key was actually specified, panic
			if len(cert) > 0 || len(key) > 0 {
				log.Errorln("Failed to load requested TLS config: ", err.Error())
				os.Exit(1)
			}

			// Otherwise, continue with non-TLS server
			log.Warningln("No server TLS config loaded")
			log.Infoln("Proceeding with non-TLS server")
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
			r = tls.NewListener(inner, tlsConf)
		}
	}
	return r
}

func getCertManager(domain, cachepath, email string) *autocert.Manager {
	m := autocert.Manager{
		Prompt: autocert.AcceptTOS,
	}

	if len(domain) > 0 {
		m.HostPolicy = autocert.HostWhitelist(domain)
	}

	if len(email) > 0 {
		m.Email = email
	}

	if len(cachepath) > 0 {
		m.Cache = autocert.DirCache(cachepath)
	}
	return &m
}
