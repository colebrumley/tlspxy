package tls

import (
	cryptotls "crypto/tls"
	"fmt"
	"strings"
)

// TLSVersionNames maps human-readable TLS version strings to crypto/tls constants.
var TLSVersionNames = map[string]uint16{
	"1.0": cryptotls.VersionTLS10,
	"1.1": cryptotls.VersionTLS11,
	"1.2": cryptotls.VersionTLS12,
	"1.3": cryptotls.VersionTLS13,
}

// ParseTLSVersion parses a TLS version string (e.g. "1.2") into a crypto/tls constant.
// An empty string returns 0 (meaning "use default").
func ParseTLSVersion(s string) (uint16, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	v, ok := TLSVersionNames[s]
	if !ok {
		return 0, fmt.Errorf("unknown TLS version %q (valid: 1.0, 1.1, 1.2, 1.3)", s)
	}
	return v, nil
}

// ParseCipherSuites parses a comma-separated list of cipher suite names into IDs.
// An empty string returns nil (meaning "use default").
func ParseCipherSuites(s string) ([]uint16, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}

	known := make(map[string]uint16)
	for _, cs := range cryptotls.CipherSuites() {
		known[cs.Name] = cs.ID
	}
	for _, cs := range cryptotls.InsecureCipherSuites() {
		known[cs.Name] = cs.ID
	}

	var ids []uint16
	for _, name := range strings.Split(s, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		id, ok := known[name]
		if !ok {
			return nil, fmt.Errorf("unknown cipher suite %q", name)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// ValidCipherSuiteNames returns all known cipher suite names (secure + insecure).
func ValidCipherSuiteNames() []string {
	var names []string
	for _, cs := range cryptotls.CipherSuites() {
		names = append(names, cs.Name)
	}
	for _, cs := range cryptotls.InsecureCipherSuites() {
		names = append(names, cs.Name)
	}
	return names
}
