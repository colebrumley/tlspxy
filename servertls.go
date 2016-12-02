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
	cert := cfg.UString("server.tls.cert")
	key := cfg.UString("server.tls.key")
	ca := cfg.UString("server.tls.ca")
	useLetsencrypt := cfg.UBool("server.tls.letsencrypt.enable", false)

	var (
		tlsConf *tls.Config
		err     error
	)

	r := inner

	// Check for whether server.tls.letsencrypt.enable is true,
	// and load a LetsEncrypt cert if so.
	if useLetsencrypt {
		log.Debug("Enabling LetsEncrypt on Server connection")
		m := *getCertManager(
			cfg.UString("server.tls.letsencrypt.domain"),
			cfg.UString("server.tls.letsencrypt.cachedir"),
			cfg.UString("server.tls.letsencrypt.email"),
		)
		tlsConf = &tls.Config{GetCertificate: m.GetCertificate}
		// See if a cert or key was specified, load a TLS config from it if so
	} else if len(cert) > 0 || len(key) > 0 {
		if tlsConf, err = LoadTLSConfigFromFiles(cert, key, ca, false); err != nil {
			log.Errorln("Failed to load requested TLS config: ", err.Error())
			os.Exit(1)
		} else {
			log.Debugf("Loaded Server TLS config: [cert: %s, key: %s, ca: %s]", cert, key, ca)
		}
		// Otherwise don't load a TLS config
	} else {
		log.Warningln("No server TLS config loaded")
		log.Infoln("Proceeding with non-TLS server")
	}

	// Parse the other TLS options.
	//   'verify' overrides 'require'
	if v := cfg.UBool("server.tls.require", false); v && tlsConf != nil {
		log.Debugf("Setting server.tls.require -> %v", v)
		tlsConf.ClientAuth = tls.RequireAnyClientCert
	}
	if v := cfg.UBool("server.tls.verify"); v && tlsConf != nil {
		log.Debugf("Setting server.tls.verify -> %v", v)
		tlsConf.ClientAuth = tls.RequireAndVerifyClientCert
	}

	if tlsConf != nil {
		r = tls.NewListener(inner, tlsConf)
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
