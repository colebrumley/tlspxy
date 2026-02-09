package tls

import (
	cryptotls "crypto/tls"
	"net"
	"path/filepath"
	"testing"

	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/v2"
)

func serverTLSKoanf(t *testing.T, m map[string]interface{}) *koanf.Koanf {
	t.Helper()
	k := koanf.New(".")
	if err := k.Load(confmap.Provider(m, "."), nil); err != nil {
		t.Fatalf("koanf load error: %v", err)
	}
	return k
}

func TestGetServerConfig_NoCerts(t *testing.T) {
	k := serverTLSKoanf(t, map[string]interface{}{
		"server": map[string]interface{}{
			"tls": map[string]interface{}{
				"cert": "", "key": "", "ca": "",
				"require": false, "verify": false,
				"letsencrypt": map[string]interface{}{"enable": false},
			},
		},
	})
	tlsConf, err := GetServerConfig(k)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tlsConf != nil {
		t.Errorf("expected nil tls.Config when no certs configured, got %+v", tlsConf)
	}
}

func TestGetServerConfig_WithCerts(t *testing.T) {
	root := projectRoot(t)
	k := serverTLSKoanf(t, map[string]interface{}{
		"server": map[string]interface{}{
			"tls": map[string]interface{}{
				"cert": filepath.Join(root, "contrib/testdata/certs/proxy.crt"),
				"key":  filepath.Join(root, "contrib/testdata/certs/proxy.key"),
				"ca":   filepath.Join(root, "contrib/testdata/certs/ca.crt"),
				"require": false, "verify": false,
				"letsencrypt": map[string]interface{}{"enable": false},
			},
		},
	})
	tlsConf, err := GetServerConfig(k)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tlsConf == nil {
		t.Fatal("expected non-nil tls.Config with cert/key")
	}
	if tlsConf.MinVersion != cryptotls.VersionTLS12 {
		t.Errorf("MinVersion = %d, want %d (TLS 1.2)", tlsConf.MinVersion, cryptotls.VersionTLS12)
	}
	if len(tlsConf.Certificates) == 0 {
		t.Error("expected certificates to be loaded")
	}
}

func TestGetServerConfig_RequireClientCert(t *testing.T) {
	root := projectRoot(t)
	k := serverTLSKoanf(t, map[string]interface{}{
		"server": map[string]interface{}{
			"tls": map[string]interface{}{
				"cert": filepath.Join(root, "contrib/testdata/certs/proxy.crt"),
				"key":  filepath.Join(root, "contrib/testdata/certs/proxy.key"),
				"ca":   filepath.Join(root, "contrib/testdata/certs/ca.crt"),
				"require": true, "verify": false,
				"letsencrypt": map[string]interface{}{"enable": false},
			},
		},
	})
	tlsConf, err := GetServerConfig(k)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tlsConf == nil {
		t.Fatal("expected non-nil tls.Config")
	}
	if tlsConf.ClientAuth != cryptotls.RequireAnyClientCert {
		t.Errorf("ClientAuth = %v, want RequireAnyClientCert", tlsConf.ClientAuth)
	}
}

func TestGetServerConfig_VerifyClientCert(t *testing.T) {
	root := projectRoot(t)
	k := serverTLSKoanf(t, map[string]interface{}{
		"server": map[string]interface{}{
			"tls": map[string]interface{}{
				"cert": filepath.Join(root, "contrib/testdata/certs/proxy.crt"),
				"key":  filepath.Join(root, "contrib/testdata/certs/proxy.key"),
				"ca":   filepath.Join(root, "contrib/testdata/certs/ca.crt"),
				"require": false, "verify": true,
				"letsencrypt": map[string]interface{}{"enable": false},
			},
		},
	})
	tlsConf, err := GetServerConfig(k)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tlsConf == nil {
		t.Fatal("expected non-nil tls.Config")
	}
	if tlsConf.ClientAuth != cryptotls.RequireAndVerifyClientCert {
		t.Errorf("ClientAuth = %v, want RequireAndVerifyClientCert", tlsConf.ClientAuth)
	}
}

func TestGetServerConfig_VerifyOverridesRequire(t *testing.T) {
	root := projectRoot(t)
	k := serverTLSKoanf(t, map[string]interface{}{
		"server": map[string]interface{}{
			"tls": map[string]interface{}{
				"cert": filepath.Join(root, "contrib/testdata/certs/proxy.crt"),
				"key":  filepath.Join(root, "contrib/testdata/certs/proxy.key"),
				"ca":   filepath.Join(root, "contrib/testdata/certs/ca.crt"),
				"require": true, "verify": true,
				"letsencrypt": map[string]interface{}{"enable": false},
			},
		},
	})
	tlsConf, err := GetServerConfig(k)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tlsConf == nil {
		t.Fatal("expected non-nil tls.Config")
	}
	if tlsConf.ClientAuth != cryptotls.RequireAndVerifyClientCert {
		t.Errorf("ClientAuth = %v, want RequireAndVerifyClientCert (verify overrides require)", tlsConf.ClientAuth)
	}
}

func TestConfigServer_WrapsListener(t *testing.T) {
	root := projectRoot(t)
	k := serverTLSKoanf(t, map[string]interface{}{
		"server": map[string]interface{}{
			"tls": map[string]interface{}{
				"cert": filepath.Join(root, "contrib/testdata/certs/proxy.crt"),
				"key":  filepath.Join(root, "contrib/testdata/certs/proxy.key"),
				"ca":   filepath.Join(root, "contrib/testdata/certs/ca.crt"),
				"require": false, "verify": false,
				"letsencrypt": map[string]interface{}{"enable": false},
			},
		},
	})
	inner, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen error: %v", err)
	}
	defer inner.Close()
	wrapped := ConfigServer(inner, k)
	if wrapped == inner {
		t.Error("expected ConfigServer to wrap the listener when TLS is configured")
	}
	wrapped.Close()
}

func TestConfigServer_NoTLS(t *testing.T) {
	k := serverTLSKoanf(t, map[string]interface{}{
		"server": map[string]interface{}{
			"tls": map[string]interface{}{
				"cert": "", "key": "", "ca": "",
				"require": false, "verify": false,
				"letsencrypt": map[string]interface{}{"enable": false},
			},
		},
	})
	inner, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen error: %v", err)
	}
	defer inner.Close()
	result := ConfigServer(inner, k)
	if result != inner {
		t.Error("expected ConfigServer to return the original listener when no TLS configured")
	}
}

func TestGetServerConfig_LetsEncrypt(t *testing.T) {
	k := serverTLSKoanf(t, map[string]interface{}{
		"server": map[string]interface{}{
			"tls": map[string]interface{}{
				"cert": "", "key": "", "ca": "",
				"require": false, "verify": false,
				"letsencrypt": map[string]interface{}{
					"enable": true, "domain": "example.org",
					"email": "test@example.org", "cachedir": "/tmp/le-test",
				},
			},
		},
	})
	tlsConf, err := GetServerConfig(k)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tlsConf == nil {
		t.Fatal("expected non-nil tls.Config with LetsEncrypt enabled")
	}
	if tlsConf.MinVersion != cryptotls.VersionTLS12 {
		t.Errorf("MinVersion = %d, want %d (TLS 1.2)", tlsConf.MinVersion, cryptotls.VersionTLS12)
	}
	if tlsConf.GetCertificate == nil {
		t.Error("expected GetCertificate to be set for LetsEncrypt")
	}
}

func TestGetServerConfig_InvalidCerts(t *testing.T) {
	k := serverTLSKoanf(t, map[string]interface{}{
		"server": map[string]interface{}{
			"tls": map[string]interface{}{
				"cert": "/nonexistent/cert.pem",
				"key":  "/nonexistent/key.pem",
				"ca":   "",
				"require": false, "verify": false,
				"letsencrypt": map[string]interface{}{"enable": false},
			},
		},
	})
	tlsConf, err := GetServerConfig(k)
	if err == nil {
		t.Fatal("expected error for invalid cert paths, got nil")
	}
	if tlsConf != nil {
		t.Errorf("expected nil tls.Config on error, got %+v", tlsConf)
	}
}
