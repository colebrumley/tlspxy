package main

import (
	"crypto/x509"
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
	}

	switch runtime.GOOS {
	case "windows":
		return fmt.Errorf("SetSystemCAPool not implemented on Windows.")
	case "darwin":
		cmd := exec.Command("/usr/bin/security", "find-certificate", "-a", "-p", "/System/Library/Keychains/SystemRootCertificates.keychain")
		data, err := cmd.Output()
		if err != nil {
			return err
		}
		if !capool.AppendCertsFromPEM(data) {
			return fmt.Errorf("No certificates could be loaded from SystemRootCertificates.keychain.")
		}
		return nil
	default:
		for _, cf := range certfiles {
			if _, err := os.Stat(cf); err == nil {
				if cfc, err := ioutil.ReadFile(cf); err == nil {
					if !capool.AppendCertsFromPEM(cfc) {
						return fmt.Errorf("Could not load certificates from %s: %v", cf, err)
					}
					return nil
				}
			}
		}
	}
	return fmt.Errorf("Could not find certificates in any of: %v", certfiles)
}
