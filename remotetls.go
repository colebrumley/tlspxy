package main

import (
	"crypto/tls"
	"crypto/x509"
	log "github.com/Sirupsen/logrus"
	"github.com/olebedev/config"
)

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
