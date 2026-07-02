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

func baseServerTLSMap() map[string]interface{} {
	return map[string]interface{}{
		"cert": "", "key": "", "ca": "",
		"require": false, "verify": false,
		"minversion": "", "maxversion": "", "ciphersuites": "", "alpn": "",
		"letsencrypt": map[string]interface{}{"enable": false},
	}
}

func TestGetServerConfig_NoCerts(t *testing.T) {
	k := serverTLSKoanf(t, map[string]interface{}{
		"server": map[string]interface{}{
			"tls": baseServerTLSMap(),
		},
	})
	tlsConf, store, err := GetServerConfig(k)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tlsConf != nil {
		t.Errorf("expected nil tls.Config when no certs configured, got %+v", tlsConf)
	}
	if store != nil {
		t.Errorf("expected nil CertStore when no certs configured")
	}
}

func TestGetServerConfig_WithCerts(t *testing.T) {
	root := projectRoot(t)
	m := baseServerTLSMap()
	m["cert"] = filepath.Join(root, "contrib/testdata/certs/proxy.crt")
	m["key"] = filepath.Join(root, "contrib/testdata/certs/proxy.key")
	m["ca"] = filepath.Join(root, "contrib/testdata/certs/ca.crt")

	k := serverTLSKoanf(t, map[string]interface{}{
		"server": map[string]interface{}{"tls": m},
	})
	tlsConf, store, err := GetServerConfig(k)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tlsConf == nil {
		t.Fatal("expected non-nil tls.Config with cert/key")
	}
	if tlsConf.MinVersion != cryptotls.VersionTLS12 {
		t.Errorf("MinVersion = %d, want %d (TLS 1.2)", tlsConf.MinVersion, cryptotls.VersionTLS12)
	}
	if tlsConf.GetCertificate == nil {
		t.Error("expected GetCertificate to be set")
	}
	if store == nil {
		t.Fatal("expected non-nil CertStore")
	}
	// Verify cert is loadable via GetCertificate
	cert, err := store.GetCertificate(&cryptotls.ClientHelloInfo{})
	if err != nil {
		t.Fatalf("GetCertificate error: %v", err)
	}
	if cert == nil {
		t.Error("expected non-nil certificate from CertStore")
	}
}

func TestGetServerConfig_RequireClientCert(t *testing.T) {
	root := projectRoot(t)
	m := baseServerTLSMap()
	m["cert"] = filepath.Join(root, "contrib/testdata/certs/proxy.crt")
	m["key"] = filepath.Join(root, "contrib/testdata/certs/proxy.key")
	m["ca"] = filepath.Join(root, "contrib/testdata/certs/ca.crt")
	m["require"] = true

	k := serverTLSKoanf(t, map[string]interface{}{
		"server": map[string]interface{}{"tls": m},
	})
	tlsConf, _, err := GetServerConfig(k)
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
	m := baseServerTLSMap()
	m["cert"] = filepath.Join(root, "contrib/testdata/certs/proxy.crt")
	m["key"] = filepath.Join(root, "contrib/testdata/certs/proxy.key")
	m["ca"] = filepath.Join(root, "contrib/testdata/certs/ca.crt")
	m["verify"] = true

	k := serverTLSKoanf(t, map[string]interface{}{
		"server": map[string]interface{}{"tls": m},
	})
	tlsConf, _, err := GetServerConfig(k)
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
	m := baseServerTLSMap()
	m["cert"] = filepath.Join(root, "contrib/testdata/certs/proxy.crt")
	m["key"] = filepath.Join(root, "contrib/testdata/certs/proxy.key")
	m["ca"] = filepath.Join(root, "contrib/testdata/certs/ca.crt")
	m["require"] = true
	m["verify"] = true

	k := serverTLSKoanf(t, map[string]interface{}{
		"server": map[string]interface{}{"tls": m},
	})
	tlsConf, _, err := GetServerConfig(k)
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
	m := baseServerTLSMap()
	m["cert"] = filepath.Join(root, "contrib/testdata/certs/proxy.crt")
	m["key"] = filepath.Join(root, "contrib/testdata/certs/proxy.key")
	m["ca"] = filepath.Join(root, "contrib/testdata/certs/ca.crt")

	k := serverTLSKoanf(t, map[string]interface{}{
		"server": map[string]interface{}{"tls": m},
	})
	inner, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen error: %v", err)
	}
	defer inner.Close()
	wrapped, store, err := ConfigServer(inner, k)
	if err != nil {
		t.Fatalf("ConfigServer() error = %v", err)
	}
	if wrapped == inner {
		t.Error("expected ConfigServer to wrap the listener when TLS is configured")
	}
	if store == nil {
		t.Error("expected non-nil CertStore from ConfigServer")
	}
	wrapped.Close()
}

func TestConfigServer_NoTLS(t *testing.T) {
	k := serverTLSKoanf(t, map[string]interface{}{
		"server": map[string]interface{}{
			"tls": baseServerTLSMap(),
		},
	})
	inner, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen error: %v", err)
	}
	defer inner.Close()
	result, store, err := ConfigServer(inner, k)
	if err != nil {
		t.Fatalf("ConfigServer() error = %v", err)
	}
	if result != inner {
		t.Error("expected ConfigServer to return the original listener when no TLS configured")
	}
	if store != nil {
		t.Error("expected nil CertStore when no TLS configured")
	}
}

func TestGetServerConfig_LetsEncrypt(t *testing.T) {
	m := baseServerTLSMap()
	m["letsencrypt"] = map[string]interface{}{
		"enable": true, "domain": "example.org",
		"email": "test@example.org", "cachedir": "/tmp/le-test",
	}

	k := serverTLSKoanf(t, map[string]interface{}{
		"server": map[string]interface{}{"tls": m},
	})
	tlsConf, store, err := GetServerConfig(k)
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
	if store != nil {
		t.Error("expected nil CertStore for LetsEncrypt (LE manages its own certs)")
	}
}

func TestGetServerConfig_InvalidCerts(t *testing.T) {
	m := baseServerTLSMap()
	m["cert"] = "/nonexistent/cert.pem"
	m["key"] = "/nonexistent/key.pem"

	k := serverTLSKoanf(t, map[string]interface{}{
		"server": map[string]interface{}{"tls": m},
	})
	tlsConf, store, err := GetServerConfig(k)
	if err == nil {
		t.Fatal("expected error for invalid cert paths, got nil")
	}
	if tlsConf != nil {
		t.Errorf("expected nil tls.Config on error, got %+v", tlsConf)
	}
	if store != nil {
		t.Error("expected nil CertStore on error")
	}
}

func TestGetServerConfig_WithTLSVersionOptions(t *testing.T) {
	root := projectRoot(t)
	m := baseServerTLSMap()
	m["cert"] = filepath.Join(root, "contrib/testdata/certs/proxy.crt")
	m["key"] = filepath.Join(root, "contrib/testdata/certs/proxy.key")
	m["ca"] = filepath.Join(root, "contrib/testdata/certs/ca.crt")
	m["minversion"] = "1.3"
	m["maxversion"] = "1.3"

	k := serverTLSKoanf(t, map[string]interface{}{
		"server": map[string]interface{}{"tls": m},
	})
	tlsConf, _, err := GetServerConfig(k)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tlsConf == nil {
		t.Fatal("expected non-nil tls.Config")
	}
	if tlsConf.MinVersion != cryptotls.VersionTLS13 {
		t.Errorf("MinVersion = %d, want %d (TLS 1.3)", tlsConf.MinVersion, cryptotls.VersionTLS13)
	}
	if tlsConf.MaxVersion != cryptotls.VersionTLS13 {
		t.Errorf("MaxVersion = %d, want %d (TLS 1.3)", tlsConf.MaxVersion, cryptotls.VersionTLS13)
	}
}

func TestGetServerConfig_WithALPN(t *testing.T) {
	root := projectRoot(t)
	m := baseServerTLSMap()
	m["cert"] = filepath.Join(root, "contrib/testdata/certs/proxy.crt")
	m["key"] = filepath.Join(root, "contrib/testdata/certs/proxy.key")
	m["ca"] = filepath.Join(root, "contrib/testdata/certs/ca.crt")
	m["alpn"] = "h2,http/1.1"

	k := serverTLSKoanf(t, map[string]interface{}{
		"server": map[string]interface{}{"tls": m},
	})
	tlsConf, _, err := GetServerConfig(k)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tlsConf == nil {
		t.Fatal("expected non-nil tls.Config")
	}
	if len(tlsConf.NextProtos) != 2 {
		t.Fatalf("NextProtos length = %d, want 2", len(tlsConf.NextProtos))
	}
	if tlsConf.NextProtos[0] != "h2" || tlsConf.NextProtos[1] != "http/1.1" {
		t.Errorf("NextProtos = %v, want [h2 http/1.1]", tlsConf.NextProtos)
	}
}
