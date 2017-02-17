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
		if tlsConf, err = LoadTLSConfigFromFiles(cert, key, ca, useSysRoots); err != nil {
			return
		}
		log.Debugln("Loading remote TLS config succeeded")
	} else {
		tlsConf = nil
		err = nil
	}

	if doVerify && useSysRoots {
		var capool *x509.CertPool
		// Just load system CAs
		log.Debugf("Loading default remote TLS config [verify: %v, system roots: %v]", doVerify, useSysRoots)
		if tlsConf == nil {
			capool = x509.NewCertPool()
			capool, err = SetSystemCAPool(capool)
			if err != nil {
				return
			}
			tlsConf = &tls.Config{
				RootCAs:   capool,
				ClientCAs: capool,
			}
		} else {
			capool, err = SetSystemCAPool(tlsConf.RootCAs)
			if err != nil {
				return
			}
			capool, err = SetSystemCAPool(tlsConf.ClientCAs)
			if err != nil {
				return
			}
			tlsConf.RootCAs = capool
			tlsConf.ClientCAs = capool
		}
	}

	if !doVerify && tlsConf != nil {
		tlsConf.InsecureSkipVerify = true
	}
	return
}
