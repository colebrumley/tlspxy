package main

import (
	"crypto/x509"
)

// loadSysroots for Linux attempts to load all of the StandardCertfiles
func loadSysroots(roots *x509.CertPool) (*x509.CertPool, error) {
	return x509.SystemCertPool()
}
