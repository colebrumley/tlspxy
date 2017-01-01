package main

import (
	"crypto/x509"
	"testing"
)

// SetSystemCAPool can take a long time on OSX since it's calling out to
// /usr/bin/security and parsing the output. On Windows, it's using the
// native syscalls that are not exported from the x509 package for some
// reason. This func is just making sure we know what kind of delay each
// option causes.
func BenchmarkSetSystemCAPool(b *testing.B) {
	for n := 0; n < b.N; n++ {
		if _, err := SetSystemCAPool(x509.NewCertPool()); err != nil {
			b.Error("Failed to set the system root ca pool")
		}
	}
}
