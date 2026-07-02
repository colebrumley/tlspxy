package tls

import (
	cryptotls "crypto/tls"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/v2"
)

func TestNewCertStore(t *testing.T) {
	root := projectRoot(t)
	certFile := filepath.Join(root, "contrib/testdata/certs/proxy.crt")
	keyFile := filepath.Join(root, "contrib/testdata/certs/proxy.key")

	store, err := NewCertStore(certFile, keyFile)
	if err != nil {
		t.Fatalf("NewCertStore() error = %v", err)
	}
	if store == nil {
		t.Fatal("NewCertStore() returned nil")
	}
	if store.defaultCert == nil {
		t.Error("defaultCert should not be nil")
	}
}

func TestNewCertStore_InvalidFiles(t *testing.T) {
	_, err := NewCertStore("/nonexistent/cert.pem", "/nonexistent/key.pem")
	if err == nil {
		t.Error("expected error for nonexistent files")
	}
}

func TestCertStore_GetCertificate(t *testing.T) {
	root := projectRoot(t)
	store, err := NewCertStore(
		filepath.Join(root, "contrib/testdata/certs/proxy.crt"),
		filepath.Join(root, "contrib/testdata/certs/proxy.key"),
	)
	if err != nil {
		t.Fatalf("NewCertStore() error = %v", err)
	}

	cert, err := store.GetCertificate(&cryptotls.ClientHelloInfo{})
	if err != nil {
		t.Fatalf("GetCertificate() error = %v", err)
	}
	if cert == nil {
		t.Error("GetCertificate() returned nil")
	}
}

func TestCertStore_GetCertificate_NilHello(t *testing.T) {
	root := projectRoot(t)
	store, err := NewCertStore(
		filepath.Join(root, "contrib/testdata/certs/proxy.crt"),
		filepath.Join(root, "contrib/testdata/certs/proxy.key"),
	)
	if err != nil {
		t.Fatalf("NewCertStore() error = %v", err)
	}

	cert, err := store.GetCertificate(nil)
	if err != nil {
		t.Fatalf("GetCertificate(nil) error = %v", err)
	}
	if cert == nil {
		t.Error("GetCertificate(nil) returned nil, expected default cert")
	}
}

func TestCertStore_Reload(t *testing.T) {
	root := projectRoot(t)
	certSrc := filepath.Join(root, "contrib/testdata/certs/proxy.crt")
	keySrc := filepath.Join(root, "contrib/testdata/certs/proxy.key")

	// Copy cert/key to temp files so we can overwrite them
	tmpDir, err := os.MkdirTemp("", "certstore-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	tmpCert := filepath.Join(tmpDir, "cert.pem")
	tmpKey := filepath.Join(tmpDir, "key.pem")
	copyFile(t, certSrc, tmpCert)
	copyFile(t, keySrc, tmpKey)

	store, err := NewCertStore(tmpCert, tmpKey)
	if err != nil {
		t.Fatalf("NewCertStore() error = %v", err)
	}

	origCert, _ := store.GetCertificate(&cryptotls.ClientHelloInfo{})

	// Overwrite with a different cert (use server cert)
	copyFile(t, filepath.Join(root, "contrib/testdata/certs/server.crt"), tmpCert)
	copyFile(t, filepath.Join(root, "contrib/testdata/certs/server.key"), tmpKey)

	if err := store.Reload(); err != nil {
		t.Fatalf("Reload() error = %v", err)
	}

	newCert, _ := store.GetCertificate(&cryptotls.ClientHelloInfo{})
	if newCert == origCert {
		t.Error("expected different cert after reload")
	}
}

func TestCertStore_Reload_InvalidCert(t *testing.T) {
	root := projectRoot(t)
	certSrc := filepath.Join(root, "contrib/testdata/certs/proxy.crt")
	keySrc := filepath.Join(root, "contrib/testdata/certs/proxy.key")

	tmpDir, err := os.MkdirTemp("", "certstore-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	tmpCert := filepath.Join(tmpDir, "cert.pem")
	tmpKey := filepath.Join(tmpDir, "key.pem")
	copyFile(t, certSrc, tmpCert)
	copyFile(t, keySrc, tmpKey)

	store, err := NewCertStore(tmpCert, tmpKey)
	if err != nil {
		t.Fatalf("NewCertStore() error = %v", err)
	}

	origCert, _ := store.GetCertificate(&cryptotls.ClientHelloInfo{})

	// Write invalid cert data
	if err := os.WriteFile(tmpCert, []byte("not a cert"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if err := store.Reload(); err == nil {
		t.Error("expected error when reloading invalid cert")
	}

	// Old cert should be preserved
	currentCert, _ := store.GetCertificate(&cryptotls.ClientHelloInfo{})
	if currentCert != origCert {
		t.Error("expected old cert to be preserved after failed reload")
	}
}

func TestCertStore_ConcurrentGetCertificateDuringReload(t *testing.T) {
	root := projectRoot(t)
	certSrc := filepath.Join(root, "contrib/testdata/certs/proxy.crt")
	keySrc := filepath.Join(root, "contrib/testdata/certs/proxy.key")

	tmpDir, err := os.MkdirTemp("", "certstore-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	tmpCert := filepath.Join(tmpDir, "cert.pem")
	tmpKey := filepath.Join(tmpDir, "key.pem")
	copyFile(t, certSrc, tmpCert)
	copyFile(t, keySrc, tmpKey)

	store, err := NewCertStore(tmpCert, tmpKey)
	if err != nil {
		t.Fatalf("NewCertStore() error = %v", err)
	}

	var wg sync.WaitGroup
	// Run concurrent GetCertificate calls while reloading
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cert, err := store.GetCertificate(&cryptotls.ClientHelloInfo{})
			if err != nil {
				t.Errorf("concurrent GetCertificate error: %v", err)
			}
			if cert == nil {
				t.Error("concurrent GetCertificate returned nil")
			}
		}()
	}
	// Reload concurrently
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = store.Reload()
		}()
	}
	wg.Wait()
}

func TestCertStore_SNI(t *testing.T) {
	root := projectRoot(t)
	store, err := NewCertStore(
		filepath.Join(root, "contrib/testdata/certs/proxy.crt"),
		filepath.Join(root, "contrib/testdata/certs/proxy.key"),
	)
	if err != nil {
		t.Fatalf("NewCertStore() error = %v", err)
	}

	// Add an SNI cert
	err = store.AddSNICert("example.com",
		filepath.Join(root, "contrib/testdata/certs/server.crt"),
		filepath.Join(root, "contrib/testdata/certs/server.key"),
	)
	if err != nil {
		t.Fatalf("AddSNICert() error = %v", err)
	}

	// SNI match should return SNI cert
	sniCert, err := store.GetCertificate(&cryptotls.ClientHelloInfo{ServerName: "example.com"})
	if err != nil {
		t.Fatalf("GetCertificate(SNI) error = %v", err)
	}

	// Non-matching should return default
	defaultCert, err := store.GetCertificate(&cryptotls.ClientHelloInfo{ServerName: "other.com"})
	if err != nil {
		t.Fatalf("GetCertificate(other) error = %v", err)
	}

	if sniCert == defaultCert {
		t.Error("expected SNI cert to be different from default cert")
	}
}

func TestCertStore_ReloadAll(t *testing.T) {
	root := projectRoot(t)

	tmpDir, err := os.MkdirTemp("", "certstore-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Copy default cert
	tmpCert := filepath.Join(tmpDir, "default.crt")
	tmpKey := filepath.Join(tmpDir, "default.key")
	copyFile(t, filepath.Join(root, "contrib/testdata/certs/proxy.crt"), tmpCert)
	copyFile(t, filepath.Join(root, "contrib/testdata/certs/proxy.key"), tmpKey)

	// Copy SNI cert
	sniCert := filepath.Join(tmpDir, "sni.crt")
	sniKey := filepath.Join(tmpDir, "sni.key")
	copyFile(t, filepath.Join(root, "contrib/testdata/certs/server.crt"), sniCert)
	copyFile(t, filepath.Join(root, "contrib/testdata/certs/server.key"), sniKey)

	store, err := NewCertStore(tmpCert, tmpKey)
	if err != nil {
		t.Fatalf("NewCertStore() error = %v", err)
	}
	if err := store.AddSNICert("sni.example.com", sniCert, sniKey); err != nil {
		t.Fatalf("AddSNICert() error = %v", err)
	}

	// ReloadAll should succeed
	if err := store.ReloadAll(); err != nil {
		t.Fatalf("ReloadAll() error = %v", err)
	}

	// Both certs should still work
	defaultC, _ := store.GetCertificate(&cryptotls.ClientHelloInfo{})
	if defaultC == nil {
		t.Error("default cert is nil after ReloadAll")
	}
	sniC, _ := store.GetCertificate(&cryptotls.ClientHelloInfo{ServerName: "sni.example.com"})
	if sniC == nil {
		t.Error("SNI cert is nil after ReloadAll")
	}
}

func TestParseSNIConfig_Empty(t *testing.T) {
	k := koanf.New(".")
	if err := k.Load(confmap.Provider(map[string]interface{}{}, "."), nil); err != nil {
		t.Fatalf("k.Load() error = %v", err)
	}

	entries, err := ParseSNIConfig(k)
	if err != nil {
		t.Fatalf("ParseSNIConfig() error = %v", err)
	}
	if entries != nil {
		t.Errorf("expected nil entries, got %v", entries)
	}
}

func TestParseSNIConfig_Valid(t *testing.T) {
	k := koanf.New(".")
	if err := k.Load(confmap.Provider(map[string]interface{}{
		"server": map[string]interface{}{
			"tls": map[string]interface{}{
				"sni": []interface{}{
					map[string]interface{}{
						"hostname": "example.com",
						"cert":     "/path/to/cert.pem",
						"key":      "/path/to/key.pem",
					},
				},
			},
		},
	}, "."), nil); err != nil {
		t.Fatalf("k.Load() error = %v", err)
	}

	entries, err := ParseSNIConfig(k)
	if err != nil {
		t.Fatalf("ParseSNIConfig() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Hostname != "example.com" {
		t.Errorf("hostname = %q, want %q", entries[0].Hostname, "example.com")
	}
}

func TestParseSNIConfig_MissingHostname(t *testing.T) {
	k := koanf.New(".")
	if err := k.Load(confmap.Provider(map[string]interface{}{
		"server": map[string]interface{}{
			"tls": map[string]interface{}{
				"sni": []interface{}{
					map[string]interface{}{
						"cert": "/path/to/cert.pem",
						"key":  "/path/to/key.pem",
					},
				},
			},
		},
	}, "."), nil); err != nil {
		t.Fatalf("k.Load() error = %v", err)
	}

	_, err := ParseSNIConfig(k)
	if err == nil {
		t.Error("expected error for missing hostname")
	}
}

func TestParseSNIConfig_MissingCert(t *testing.T) {
	k := koanf.New(".")
	if err := k.Load(confmap.Provider(map[string]interface{}{
		"server": map[string]interface{}{
			"tls": map[string]interface{}{
				"sni": []interface{}{
					map[string]interface{}{
						"hostname": "example.com",
						"key":      "/path/to/key.pem",
					},
				},
			},
		},
	}, "."), nil); err != nil {
		t.Fatalf("k.Load() error = %v", err)
	}

	_, err := ParseSNIConfig(k)
	if err == nil {
		t.Error("expected error for missing cert")
	}
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("reading %s: %v", src, err)
	}
	if err := os.WriteFile(dst, data, 0644); err != nil {
		t.Fatalf("writing %s: %v", dst, err)
	}
}
