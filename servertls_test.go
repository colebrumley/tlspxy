package main

import (
	"crypto/tls"
	"net"
	"testing"

	"github.com/olebedev/config"
)

func TestGetServerTLSConfig_NoCerts(t *testing.T) {
	cfg, err := config.ParseYaml(`
server:
  tls:
    cert: ""
    key: ""
    ca: ""
    require: false
    verify: false
    letsencrypt:
      enable: false
`)
	if err != nil {
		t.Fatalf("ParseYaml error: %v", err)
	}

	tlsConf := getServerTLSConfig(cfg)
	if tlsConf != nil {
		t.Errorf("expected nil tls.Config when no certs configured, got %+v", tlsConf)
	}
}

func TestGetServerTLSConfig_WithCerts(t *testing.T) {
	cfg, err := config.ParseYaml(`
server:
  tls:
    cert: "contrib/testdata/certs/proxy.crt"
    key: "contrib/testdata/certs/proxy.key"
    ca: "contrib/testdata/certs/ca.crt"
    require: false
    verify: false
    letsencrypt:
      enable: false
`)
	if err != nil {
		t.Fatalf("ParseYaml error: %v", err)
	}

	tlsConf := getServerTLSConfig(cfg)
	if tlsConf == nil {
		t.Fatal("expected non-nil tls.Config with cert/key")
	}
	if tlsConf.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %d, want %d (TLS 1.2)", tlsConf.MinVersion, tls.VersionTLS12)
	}
	if len(tlsConf.Certificates) == 0 {
		t.Error("expected certificates to be loaded")
	}
	if tlsConf.RootCAs == nil {
		t.Error("expected RootCAs to be set")
	}
	if tlsConf.ClientCAs == nil {
		t.Error("expected ClientCAs to be set")
	}
}

func TestGetServerTLSConfig_RequireClientCert(t *testing.T) {
	cfg, err := config.ParseYaml(`
server:
  tls:
    cert: "contrib/testdata/certs/proxy.crt"
    key: "contrib/testdata/certs/proxy.key"
    ca: "contrib/testdata/certs/ca.crt"
    require: true
    verify: false
    letsencrypt:
      enable: false
`)
	if err != nil {
		t.Fatalf("ParseYaml error: %v", err)
	}

	tlsConf := getServerTLSConfig(cfg)
	if tlsConf == nil {
		t.Fatal("expected non-nil tls.Config")
	}
	if tlsConf.ClientAuth != tls.RequireAnyClientCert {
		t.Errorf("ClientAuth = %v, want RequireAnyClientCert (%v)", tlsConf.ClientAuth, tls.RequireAnyClientCert)
	}
}

func TestGetServerTLSConfig_VerifyClientCert(t *testing.T) {
	cfg, err := config.ParseYaml(`
server:
  tls:
    cert: "contrib/testdata/certs/proxy.crt"
    key: "contrib/testdata/certs/proxy.key"
    ca: "contrib/testdata/certs/ca.crt"
    require: false
    verify: true
    letsencrypt:
      enable: false
`)
	if err != nil {
		t.Fatalf("ParseYaml error: %v", err)
	}

	tlsConf := getServerTLSConfig(cfg)
	if tlsConf == nil {
		t.Fatal("expected non-nil tls.Config")
	}
	if tlsConf.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Errorf("ClientAuth = %v, want RequireAndVerifyClientCert (%v)", tlsConf.ClientAuth, tls.RequireAndVerifyClientCert)
	}
}

func TestGetServerTLSConfig_VerifyOverridesRequire(t *testing.T) {
	cfg, err := config.ParseYaml(`
server:
  tls:
    cert: "contrib/testdata/certs/proxy.crt"
    key: "contrib/testdata/certs/proxy.key"
    ca: "contrib/testdata/certs/ca.crt"
    require: true
    verify: true
    letsencrypt:
      enable: false
`)
	if err != nil {
		t.Fatalf("ParseYaml error: %v", err)
	}

	tlsConf := getServerTLSConfig(cfg)
	if tlsConf == nil {
		t.Fatal("expected non-nil tls.Config")
	}
	// verify overrides require
	if tlsConf.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Errorf("ClientAuth = %v, want RequireAndVerifyClientCert (%v) (verify should override require)", tlsConf.ClientAuth, tls.RequireAndVerifyClientCert)
	}
}

func TestConfigServerTLS_WrapsListener(t *testing.T) {
	// With TLS configured, the returned listener should be different (wrapped)
	cfg, err := config.ParseYaml(`
server:
  tls:
    cert: "contrib/testdata/certs/proxy.crt"
    key: "contrib/testdata/certs/proxy.key"
    ca: "contrib/testdata/certs/ca.crt"
    require: false
    verify: false
    letsencrypt:
      enable: false
`)
	if err != nil {
		t.Fatalf("ParseYaml error: %v", err)
	}

	inner, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen error: %v", err)
	}
	defer inner.Close()

	wrapped := configServerTLS(inner, cfg)
	if wrapped == inner {
		t.Error("expected configServerTLS to wrap the listener when TLS is configured")
	}
	wrapped.Close()
}

func TestConfigServerTLS_NoTLS(t *testing.T) {
	// Without TLS, the returned listener should be the same as the input
	cfg, err := config.ParseYaml(`
server:
  tls:
    cert: ""
    key: ""
    ca: ""
    require: false
    verify: false
    letsencrypt:
      enable: false
`)
	if err != nil {
		t.Fatalf("ParseYaml error: %v", err)
	}

	inner, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen error: %v", err)
	}
	defer inner.Close()

	result := configServerTLS(inner, cfg)
	if result != inner {
		t.Error("expected configServerTLS to return the original listener when no TLS configured")
	}
}

func TestGetServerTLSConfig_LetsEncrypt(t *testing.T) {
	cfg, err := config.ParseYaml(`
server:
  tls:
    cert: ""
    key: ""
    ca: ""
    require: false
    verify: false
    letsencrypt:
      enable: true
      domain: "example.org"
      email: "test@example.org"
      cachedir: "/tmp/le-test"
`)
	if err != nil {
		t.Fatalf("ParseYaml error: %v", err)
	}

	tlsConf := getServerTLSConfig(cfg)
	if tlsConf == nil {
		t.Fatal("expected non-nil tls.Config with LetsEncrypt enabled")
	}
	if tlsConf.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %d, want %d (TLS 1.2)", tlsConf.MinVersion, tls.VersionTLS12)
	}
	if tlsConf.GetCertificate == nil {
		t.Error("expected GetCertificate to be set for LetsEncrypt")
	}
}
