package main

import (
	"crypto/tls"

	log "github.com/sirupsen/logrus"
	"github.com/olebedev/config"
)

func configRemoteTLS(cfg *config.Config) (tlsConf *tls.Config, err error) {
	isTLS := cfg.UBool("remote.tls.enable", true)
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
	} else if isTLS {
		tlsConf = &tls.Config{}
	}

	if doVerify && useSysRoots && tlsConf != nil {
		log.Debugf("Loading default remote TLS config [verify: %v, system roots: %v]", doVerify, useSysRoots)
		capool, poolErr := SystemCAPool()
		if poolErr != nil {
			err = poolErr
			return
		}
		tlsConf.RootCAs = capool
		tlsConf.ClientCAs = capool
	}

	if !doVerify && tlsConf != nil {
		tlsConf.InsecureSkipVerify = true
	}
	return
}
