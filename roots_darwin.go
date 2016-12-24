package main

import (
	"crypto/x509"
	"fmt"
	"os/exec"
)

// loadSysroots for Linux uses OSX's security tool to pull the system roots. This takes a while...
func loadSysroots(roots *x509.CertPool) (*x509.CertPool, error) {
	cmd := exec.Command("/usr/bin/security", "find-certificate", "-a", "-p", "/System/Library/Keychains/SystemRootCertificates.keychain")
	data, err := cmd.Output()
	if err != nil {
		return roots, err
	}
	if !roots.AppendCertsFromPEM(data) {
		return roots, fmt.Errorf("No certificates could be loaded from SystemRootCertificates.keychain.")
	}
	return roots, nil
}
