package main

import (
	"crypto/x509"
	"fmt"
	"io/ioutil"
)

// StandardCertfiles is a list of common root CA locations on various distros
var StandardCertfiles = []string{
	"/etc/ssl/certs/ca-certificates.crt",     // Debian/Ubuntu/Gentoo etc.
	"/etc/pki/tls/certs/ca-bundle.crt",       // Fedora/RHEL
	"/etc/ssl/ca-bundle.pem",                 // OpenSUSE
	"/etc/pki/tls/cacert.pem",                // OpenELEC
	"/usr/local/share/certs/ca-root-nss.crt", // FreeBSD/DragonFly
	"/etc/ssl/cert.pem",                      // OpenBSD
	"/etc/openssl/certs/ca-certificates.crt", // NetBSD
	"/sys/lib/tls/ca.pem",                    // Plan9
	"/etc/certs/ca-certificates.crt",         // Solaris 11.2+
	"/etc/ssl/cacert.pem",                    // OmniOS
	"/system/etc/security/cacerts",           // Android
}

// loadSysroots for Linux attempts to load all of the StandardCertfiles
func loadSysroots(roots *x509.CertPool) (*x509.CertPool, error) {
	results := roots
	for _, cf := range StandardCertfiles {
		if fileExists(cf) {
			if cfc, err := ioutil.ReadFile(cf); err == nil {
				if !results.AppendCertsFromPEM(cfc) {
					return roots, fmt.Errorf("Could not load certificates from %s: %v", cf, err)
				}
				return results, nil
			}
		}
	}
	return results, nil
}
