package main

import (
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"os"
)

// LoadTLSConfigFromFiles takes paths to cert files and loads a Go *tls.Config object
func LoadTLSConfigFromFiles(cert, key, ca string, loadSystemRoots bool) (tlsConf *tls.Config, err error) {
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
		var pool *x509.CertPool
		if pool, err = SetSystemCAPool(caPool); err != nil {
			return
		}
		caPool = pool
	}

	if len(ca) > 0 {
		caPem, err = os.ReadFile(ca)
		if err != nil {
			return
		}
		if !caPool.AppendCertsFromPEM(caPem) {
			err = errors.New("Failed to load CA file!")
			return
		}
	}

	tlsConf = &tls.Config{
		ClientCAs:    caPool,
		RootCAs:      caPool,
		Rand:         rand.Reader,
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{tlsCert},
	}
	return
}
