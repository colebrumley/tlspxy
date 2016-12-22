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
	var tlsConf *tls.Config

	r := inner
	tlsConf = getServerTLSConfig(cfg)

	if tlsConf != nil {
		r = tls.NewListener(inner, tlsConf)
	}
	return r
}

func getServerTLSConfig(cfg *config.Config) *tls.Config {
	var (
		tlsConf *tls.Config
		err     error
	)
	// Load server TLS config from cert files
	cert := cfg.UString("server.tls.cert")
	key := cfg.UString("server.tls.key")
	ca := cfg.UString("server.tls.ca")
	useLetsencrypt := cfg.UBool("server.tls.letsencrypt.enable", false)

	// Check for whether server.tls.letsencrypt.enable is true,
	// and load a LetsEncrypt cert if so.
	if useLetsencrypt {
		log.Debug("Enabling LetsEncrypt on Server connection")
		m := autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(cfg.UString("server.tls.letsencrypt.domain")),
			Email:      cfg.UString("server.tls.letsencrypt.email"),
			Cache:      autocert.DirCache(cfg.UString("server.tls.letsencrypt.cachedir")),
		}
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

	return tlsConf
}
