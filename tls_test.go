package main

import (
	"crypto/tls"
	"testing"
)

func TestLoadTLSConfigFromFiles(t *testing.T) {
	certFile := "contrib/testdata/certs/proxy.crt"
	keyFile := "contrib/testdata/certs/proxy.key"
	caFile := "contrib/testdata/certs/ca.crt"

	tlsConf, err := LoadTLSConfigFromFiles(certFile, keyFile, caFile, false)
	if err != nil {
		t.Fatalf("LoadTLSConfigFromFiles() error = %v", err)
	}
	if tlsConf == nil {
		t.Fatal("LoadTLSConfigFromFiles() returned nil config")
	}

	// Verify MinVersion is TLS 1.2
	if tlsConf.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %d, want %d (TLS 1.2)", tlsConf.MinVersion, tls.VersionTLS12)
	}

	// CipherSuites should be nil (letting Go choose safe defaults)
	if tlsConf.CipherSuites != nil {
		t.Errorf("CipherSuites = %v, want nil", tlsConf.CipherSuites)
	}

	// Should have loaded certificates
	if len(tlsConf.Certificates) == 0 {
		t.Error("Expected at least one certificate to be loaded")
	}

	// Should have CA pools
	if tlsConf.RootCAs == nil {
		t.Error("RootCAs should not be nil")
	}
	if tlsConf.ClientCAs == nil {
		t.Error("ClientCAs should not be nil")
	}
}

func TestLoadTLSConfigFromFiles_WithSystemRoots(t *testing.T) {
	certFile := "contrib/testdata/certs/proxy.crt"
	keyFile := "contrib/testdata/certs/proxy.key"

	// Load with system roots and no CA file
	tlsConf, err := LoadTLSConfigFromFiles(certFile, keyFile, "", true)
	if err != nil {
		t.Fatalf("LoadTLSConfigFromFiles() error = %v", err)
	}
	if tlsConf == nil {
		t.Fatal("returned nil config")
	}
	if tlsConf.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %d, want %d", tlsConf.MinVersion, tls.VersionTLS12)
	}
}

func TestLoadTLSConfigFromFiles_MissingCert(t *testing.T) {
	_, err := LoadTLSConfigFromFiles("nonexistent.crt", "contrib/testdata/certs/proxy.key", "contrib/testdata/certs/ca.crt", false)
	if err == nil {
		t.Error("Expected error for missing cert file, got nil")
	}
}

func TestLoadTLSConfigFromFiles_MissingKey(t *testing.T) {
	_, err := LoadTLSConfigFromFiles("contrib/testdata/certs/proxy.crt", "nonexistent.key", "contrib/testdata/certs/ca.crt", false)
	if err == nil {
		t.Error("Expected error for missing key file, got nil")
	}
}

func TestLoadTLSConfigFromFiles_MissingBoth(t *testing.T) {
	_, err := LoadTLSConfigFromFiles("nonexistent.crt", "nonexistent.key", "", false)
	if err == nil {
		t.Error("Expected error for missing cert and key files, got nil")
	}
}

func TestLoadTLSConfigFromFiles_NoCASource(t *testing.T) {
	// When no CA file and no system roots - should error
	_, err := LoadTLSConfigFromFiles("contrib/testdata/certs/proxy.crt", "contrib/testdata/certs/proxy.key", "", false)
	if err == nil {
		t.Error("Expected error when no CA source provided, got nil")
	}
}
