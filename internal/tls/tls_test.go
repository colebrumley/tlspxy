package tls

import (
	cryptotls "crypto/tls"
	"os"
	"path/filepath"
	"testing"
)

// projectRoot finds the project root by looking for go.mod.
func projectRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find project root")
		}
		dir = parent
	}
}

func TestLoadConfigFromFiles(t *testing.T) {
	root := projectRoot(t)
	certFile := filepath.Join(root, "contrib/testdata/certs/proxy.crt")
	keyFile := filepath.Join(root, "contrib/testdata/certs/proxy.key")
	caFile := filepath.Join(root, "contrib/testdata/certs/ca.crt")

	tlsConf, err := LoadConfigFromFiles(certFile, keyFile, caFile, false)
	if err != nil {
		t.Fatalf("LoadConfigFromFiles() error = %v", err)
	}
	if tlsConf == nil {
		t.Fatal("LoadConfigFromFiles() returned nil config")
	}

	// Verify MinVersion is TLS 1.2
	if tlsConf.MinVersion != cryptotls.VersionTLS12 {
		t.Errorf("MinVersion = %d, want %d (TLS 1.2)", tlsConf.MinVersion, cryptotls.VersionTLS12)
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

func TestLoadConfigFromFiles_WithSystemRoots(t *testing.T) {
	root := projectRoot(t)
	certFile := filepath.Join(root, "contrib/testdata/certs/proxy.crt")
	keyFile := filepath.Join(root, "contrib/testdata/certs/proxy.key")

	// Load with system roots and no CA file
	tlsConf, err := LoadConfigFromFiles(certFile, keyFile, "", true)
	if err != nil {
		t.Fatalf("LoadConfigFromFiles() error = %v", err)
	}
	if tlsConf == nil {
		t.Fatal("returned nil config")
	}
	if tlsConf.MinVersion != cryptotls.VersionTLS12 {
		t.Errorf("MinVersion = %d, want %d", tlsConf.MinVersion, cryptotls.VersionTLS12)
	}
}

func TestLoadConfigFromFiles_MissingCert(t *testing.T) {
	root := projectRoot(t)
	_, err := LoadConfigFromFiles("nonexistent.crt", filepath.Join(root, "contrib/testdata/certs/proxy.key"), filepath.Join(root, "contrib/testdata/certs/ca.crt"), false)
	if err == nil {
		t.Error("Expected error for missing cert file, got nil")
	}
}

func TestLoadConfigFromFiles_MissingKey(t *testing.T) {
	root := projectRoot(t)
	_, err := LoadConfigFromFiles(filepath.Join(root, "contrib/testdata/certs/proxy.crt"), "nonexistent.key", filepath.Join(root, "contrib/testdata/certs/ca.crt"), false)
	if err == nil {
		t.Error("Expected error for missing key file, got nil")
	}
}

func TestLoadConfigFromFiles_MissingBoth(t *testing.T) {
	_, err := LoadConfigFromFiles("nonexistent.crt", "nonexistent.key", "", false)
	if err == nil {
		t.Error("Expected error for missing cert and key files, got nil")
	}
}

func TestLoadConfigFromFiles_NoCASource(t *testing.T) {
	root := projectRoot(t)
	// When no CA file and no system roots - should error
	_, err := LoadConfigFromFiles(filepath.Join(root, "contrib/testdata/certs/proxy.crt"), filepath.Join(root, "contrib/testdata/certs/proxy.key"), "", false)
	if err == nil {
		t.Error("Expected error when no CA source provided, got nil")
	}
}

func BenchmarkSystemCAPool(b *testing.B) {
	for n := 0; n < b.N; n++ {
		if _, err := SystemCAPool(); err != nil {
			b.Error("Failed to load system root CA pool")
		}
	}
}

func TestFileExists(t *testing.T) {
	// Test with an existing file
	tmpFile, err := os.CreateTemp("", "tlspxy-test")
	if err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	if !FileExists(tmpFile.Name()) {
		t.Errorf("FileExists(%q) = false, want true", tmpFile.Name())
	}

	// Test with a non-existing file
	if FileExists(filepath.Join(os.TempDir(), "nonexistent-file-tlspxy-test-12345")) {
		t.Error("FileExists(nonexistent) = true, want false")
	}
}

func TestFileExists_Directory(t *testing.T) {
	// FileExists should return true for directories too (os.Stat works on dirs)
	tmpDir, err := os.MkdirTemp("", "tlspxy-test-dir")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	if !FileExists(tmpDir) {
		t.Errorf("FileExists(%q) = false for directory, want true", tmpDir)
	}
}
