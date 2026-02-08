package main

import "crypto/x509"

// SystemCAPool returns a CertPool containing the system's root CA certificates.
func SystemCAPool() (*x509.CertPool, error) {
	return x509.SystemCertPool()
}
