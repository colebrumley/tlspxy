package main

import "crypto/x509"

// SetSystemCAPool loads the system's root CA list into the provided CertPool using
// the OS-preferred method. Leans heavily on unexported funcs from crypto/x509.
func SetSystemCAPool(capool *x509.CertPool) (pool *x509.CertPool, err error) {
	pool, err = loadSysroots(capool)
	return
}
