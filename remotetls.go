package main

import (
	"crypto/tls"
	"crypto/x509"

	log "github.com/Sirupsen/logrus"
	"github.com/olebedev/config"
)

func configRemoteTLS(cfg *config.Config) (tlsConf *tls.Config, err error) {
	cert := cfg.UString("remote.tls.cert")
	key := cfg.UString("remote.tls.key")
	ca := cfg.UString("remote.tls.ca")
	doVerify := cfg.UBool("remote.tls.verify", false)
	useSysRoots := cfg.UBool("remote.tls.sysroots", false)

	if fileExists(cert) && fileExists(key) {
		log.Debugf("Loading remote TLS config: [cert: %s, key: %s, ca: %s, SystemRoots: %v]", cert, key, ca, useSysRoots)
		tlsConf, err = LoadTLSConfigFromFiles(cert, key, ca, useSysRoots)
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
