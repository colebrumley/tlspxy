package main

import (
	"crypto/x509"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"runtime"
)

// SetSystemCAPool tries a bunch of heuristics to load the system CA
//  certificates.  This is mostly stolen from crypto/x509 and I have no
//  idea why it is not exported.
func SetSystemCAPool(capool *x509.CertPool) error {
	// Fail immediately if Windows, if Darwin try the magic keychain extractor,
	//  otherwise loop through certfiles until we can open one of them, and
	//  try that one (only).
	var certfiles = []string{
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
		"/ssl/CA/ca-chain.pem",                   // Enteon digicert chain
	}
	goos := runtime.GOOS
	if goos == "windows" {
		return errors.New("SetSystemCAPool not implemented on Windows.")
	}
	if goos == "darwin" {
		cmd := exec.Command("/usr/bin/security", "find-certificate", "-a", "-p", "/System/Library/Keychains/SystemRootCertificates.keychain")
		data, err := cmd.Output()
		if err != nil {
			return err
		}
		didit := capool.AppendCertsFromPEM(data)
		if didit == false {
			return errors.New("No certificates could be loaded from SystemRootCertificates.keychain.")
		}
		return nil
	}
	for _, cf := range certfiles {
		_, err := os.Stat(cf)
		if err == nil {
			cfc, err := ioutil.ReadFile(cf)
			if err == nil {
				if capool.AppendCertsFromPEM(cfc) == false {
					s := fmt.Sprintf("Could not load certificates from %s: %v", cf, err)
					return errors.New(s)
				}
				return nil
			}
		}
	}
	s := fmt.Sprintf("Could not find certificates in any of: %v", certfiles)
	return errors.New(s)
}
