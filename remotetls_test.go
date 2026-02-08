package main

import (
	"testing"

	"github.com/olebedev/config"
)

func TestConfigRemoteTLS_DefaultTLS(t *testing.T) {
	cfg, err := config.ParseYaml(`
remote:
  tls:
    enable: true
    verify: false
    cert: ""
    key: ""
    ca: ""
    sysroots: false
`)
	if err != nil {
		t.Fatalf("ParseYaml error: %v", err)
	}

	tlsConf, err := configRemoteTLS(cfg)
	if err != nil {
		t.Fatalf("configRemoteTLS() error = %v", err)
	}
	if tlsConf == nil {
		t.Fatal("expected non-nil tls.Config when enable=true")
	}
	if !tlsConf.InsecureSkipVerify {
		t.Error("expected InsecureSkipVerify=true when verify=false")
	}
}

func TestConfigRemoteTLS_WithCertKey(t *testing.T) {
	cfg, err := config.ParseYaml(`
remote:
  tls:
    enable: true
    verify: false
    cert: "contrib/testdata/certs/proxy.crt"
    key: "contrib/testdata/certs/proxy.key"
    ca: "contrib/testdata/certs/ca.crt"
    sysroots: false
`)
	if err != nil {
		t.Fatalf("ParseYaml error: %v", err)
	}

	tlsConf, err := configRemoteTLS(cfg)
	if err != nil {
		t.Fatalf("configRemoteTLS() error = %v", err)
	}
	if tlsConf == nil {
		t.Fatal("expected non-nil tls.Config with cert/key")
	}
	if len(tlsConf.Certificates) == 0 {
		t.Error("expected certificates to be loaded")
	}
	if tlsConf.RootCAs == nil {
		t.Error("expected RootCAs to be set")
	}
}

func TestConfigRemoteTLS_VerifyWithSysRoots(t *testing.T) {
	cfg, err := config.ParseYaml(`
remote:
  tls:
    enable: true
    verify: true
    cert: ""
    key: ""
    ca: ""
    sysroots: true
`)
	if err != nil {
		t.Fatalf("ParseYaml error: %v", err)
	}

	tlsConf, err := configRemoteTLS(cfg)
	if err != nil {
		t.Fatalf("configRemoteTLS() error = %v", err)
	}
	if tlsConf == nil {
		t.Fatal("expected non-nil tls.Config with verify+sysroots")
	}
	if tlsConf.InsecureSkipVerify {
		t.Error("expected InsecureSkipVerify=false when verify=true")
	}
	if tlsConf.RootCAs == nil {
		t.Error("expected RootCAs to be set with system roots")
	}
}

func TestConfigRemoteTLS_Disabled(t *testing.T) {
	cfg, err := config.ParseYaml(`
remote:
  tls:
    enable: false
    verify: false
    cert: ""
    key: ""
    ca: ""
    sysroots: false
`)
	if err != nil {
		t.Fatalf("ParseYaml error: %v", err)
	}

	tlsConf, err := configRemoteTLS(cfg)
	if err != nil {
		t.Fatalf("configRemoteTLS() error = %v", err)
	}
	if tlsConf != nil {
		t.Errorf("expected nil tls.Config when enable=false and no cert/key, got %+v", tlsConf)
	}
}

func TestConfigRemoteTLS_InsecureSkipVerify(t *testing.T) {
	cfg, err := config.ParseYaml(`
remote:
  tls:
    enable: true
    verify: false
    cert: ""
    key: ""
    ca: ""
    sysroots: false
`)
	if err != nil {
		t.Fatalf("ParseYaml error: %v", err)
	}

	tlsConf, err := configRemoteTLS(cfg)
	if err != nil {
		t.Fatalf("configRemoteTLS() error = %v", err)
	}
	if tlsConf == nil {
		t.Fatal("expected non-nil tls.Config")
	}
	if !tlsConf.InsecureSkipVerify {
		t.Error("InsecureSkipVerify should be true when verify=false")
	}
}

func TestConfigRemoteTLS_WithCertKeyAndVerify(t *testing.T) {
	cfg, err := config.ParseYaml(`
remote:
  tls:
    enable: true
    verify: true
    cert: "contrib/testdata/certs/proxy.crt"
    key: "contrib/testdata/certs/proxy.key"
    ca: "contrib/testdata/certs/ca.crt"
    sysroots: true
`)
	if err != nil {
		t.Fatalf("ParseYaml error: %v", err)
	}

	tlsConf, err := configRemoteTLS(cfg)
	if err != nil {
		t.Fatalf("configRemoteTLS() error = %v", err)
	}
	if tlsConf == nil {
		t.Fatal("expected non-nil tls.Config")
	}
	if len(tlsConf.Certificates) == 0 {
		t.Error("expected certificates to be loaded")
	}
	if tlsConf.InsecureSkipVerify {
		t.Error("InsecureSkipVerify should be false when verify=true")
	}
	if tlsConf.RootCAs == nil {
		t.Error("expected RootCAs to include system roots")
	}
}

func TestConfigRemoteTLS_WithCertKeyNoVerify(t *testing.T) {
	cfg, err := config.ParseYaml(`
remote:
  tls:
    enable: false
    verify: false
    cert: "contrib/testdata/certs/proxy.crt"
    key: "contrib/testdata/certs/proxy.key"
    ca: "contrib/testdata/certs/ca.crt"
    sysroots: false
`)
	if err != nil {
		t.Fatalf("ParseYaml error: %v", err)
	}

	tlsConf, err := configRemoteTLS(cfg)
	if err != nil {
		t.Fatalf("configRemoteTLS() error = %v", err)
	}
	// Even with enable=false, cert/key files exist so the first branch is taken
	if tlsConf == nil {
		t.Fatal("expected non-nil tls.Config when cert/key files exist")
	}
	if len(tlsConf.Certificates) == 0 {
		t.Error("expected certificates to be loaded")
	}
	if !tlsConf.InsecureSkipVerify {
		t.Error("InsecureSkipVerify should be true when verify=false")
	}
}

func TestConfigRemoteTLS_VerifyNoSysRoots(t *testing.T) {
	// verify=true but sysroots=false, enable=true, no cert/key
	// The doVerify && useSysRoots block is skipped, and since doVerify is true
	// the !doVerify block is also skipped. So InsecureSkipVerify stays false (default).
	cfg, err := config.ParseYaml(`
remote:
  tls:
    enable: true
    verify: true
    cert: ""
    key: ""
    ca: ""
    sysroots: false
`)
	if err != nil {
		t.Fatalf("ParseYaml error: %v", err)
	}

	tlsConf, err := configRemoteTLS(cfg)
	if err != nil {
		t.Fatalf("configRemoteTLS() error = %v", err)
	}
	if tlsConf == nil {
		t.Fatal("expected non-nil tls.Config when enable=true")
	}
	if tlsConf.InsecureSkipVerify {
		t.Error("InsecureSkipVerify should be false when verify=true")
	}
}
