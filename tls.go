package main

import (
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io/ioutil"
)

func LoadTlsConfigFromFiles(cert, key, ca string, loadSystemRoots bool) (tlsConf *tls.Config, err error) {
	var (
		tlsCert tls.Certificate
		caPool  *x509.CertPool
		caPem   []byte
	)

	// cert and key must be defined
	if !fileExists(cert) || !fileExists(key) {
		err = errors.New("Could not load cert/key, file does not exist")
		return
	}

	tlsCert, err = tls.LoadX509KeyPair(cert, key)
	if err != nil {
		return
	}

	// Make sure we have a CA somewhere
	if len(ca) == 0 && !loadSystemRoots {
		err = errors.New("Must provide a CA source!")
		return
	}

	caPool = x509.NewCertPool()

	if loadSystemRoots {
		if err = SetSystemCAPool(caPool); err != nil {
			return
		}
	}

	if len(ca) > 0 {
		caPem, err = ioutil.ReadFile(ca)
		if err != nil {
			return
		}
		if !caPool.AppendCertsFromPEM(caPem) {
			err = errors.New("Failed to load CA file!")
			return
		}
	}

	tlsConf = &tls.Config{
		ClientCAs:                caPool,
		RootCAs:                  caPool,
		PreferServerCipherSuites: true,
		Rand:         rand.Reader,
		Certificates: []tls.Certificate{tlsCert},
	}
	return
}
