package main

import (
	"crypto/x509"
)

// loadSysroots for Linux uses OSX's security tool to pull the system roots. This takes a while...
func loadSysroots(roots *x509.CertPool) (*x509.CertPool, error) {
	return x509.SystemCertPool()
}
