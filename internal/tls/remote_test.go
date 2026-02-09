package tls

import (
	"path/filepath"
	"testing"

	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/v2"
)

func remoteTLSKoanf(t *testing.T, m map[string]interface{}) *koanf.Koanf {
	t.Helper()
	k := koanf.New(".")
	if err := k.Load(confmap.Provider(m, "."), nil); err != nil {
		t.Fatalf("koanf load error: %v", err)
	}
	return k
}

func TestConfigRemote_DefaultTLS(t *testing.T) {
	k := remoteTLSKoanf(t, map[string]interface{}{
		"remote": map[string]interface{}{
			"tls": map[string]interface{}{
				"enable": true, "verify": false,
				"cert": "", "key": "", "ca": "", "sysroots": false,
				"minversion": "", "maxversion": "", "ciphersuites": "", "alpn": "",
			},
		},
	})
	tlsConf, err := ConfigRemote(k)
	if err != nil {
		t.Fatalf("ConfigRemote() error = %v", err)
	}
	if tlsConf == nil {
		t.Fatal("expected non-nil tls.Config when enable=true")
	}
	if !tlsConf.InsecureSkipVerify {
		t.Error("expected InsecureSkipVerify=true when verify=false")
	}
}

func TestConfigRemote_WithCertKey(t *testing.T) {
	root := projectRoot(t)
	k := remoteTLSKoanf(t, map[string]interface{}{
		"remote": map[string]interface{}{
			"tls": map[string]interface{}{
				"enable": true, "verify": false,
				"cert": filepath.Join(root, "contrib/testdata/certs/proxy.crt"),
				"key":  filepath.Join(root, "contrib/testdata/certs/proxy.key"),
				"ca":   filepath.Join(root, "contrib/testdata/certs/ca.crt"),
				"sysroots": false,
				"minversion": "", "maxversion": "", "ciphersuites": "", "alpn": "",
			},
		},
	})
	tlsConf, err := ConfigRemote(k)
	if err != nil {
		t.Fatalf("ConfigRemote() error = %v", err)
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

func TestConfigRemote_VerifyWithSysRoots(t *testing.T) {
	k := remoteTLSKoanf(t, map[string]interface{}{
		"remote": map[string]interface{}{
			"tls": map[string]interface{}{
				"enable": true, "verify": true,
				"cert": "", "key": "", "ca": "", "sysroots": true,
				"minversion": "", "maxversion": "", "ciphersuites": "", "alpn": "",
			},
		},
	})
	tlsConf, err := ConfigRemote(k)
	if err != nil {
		t.Fatalf("ConfigRemote() error = %v", err)
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

func TestConfigRemote_Disabled(t *testing.T) {
	k := remoteTLSKoanf(t, map[string]interface{}{
		"remote": map[string]interface{}{
			"tls": map[string]interface{}{
				"enable": false, "verify": false,
				"cert": "", "key": "", "ca": "", "sysroots": false,
				"minversion": "", "maxversion": "", "ciphersuites": "", "alpn": "",
			},
		},
	})
	tlsConf, err := ConfigRemote(k)
	if err != nil {
		t.Fatalf("ConfigRemote() error = %v", err)
	}
	if tlsConf != nil {
		t.Errorf("expected nil tls.Config when enable=false, got %+v", tlsConf)
	}
}

func TestConfigRemote_InsecureSkipVerify(t *testing.T) {
	k := remoteTLSKoanf(t, map[string]interface{}{
		"remote": map[string]interface{}{
			"tls": map[string]interface{}{
				"enable": true, "verify": false,
				"cert": "", "key": "", "ca": "", "sysroots": false,
				"minversion": "", "maxversion": "", "ciphersuites": "", "alpn": "",
			},
		},
	})
	tlsConf, err := ConfigRemote(k)
	if err != nil {
		t.Fatalf("ConfigRemote() error = %v", err)
	}
	if tlsConf == nil {
		t.Fatal("expected non-nil tls.Config")
	}
	if !tlsConf.InsecureSkipVerify {
		t.Error("InsecureSkipVerify should be true when verify=false")
	}
}

func TestConfigRemote_WithCertKeyAndVerify(t *testing.T) {
	root := projectRoot(t)
	k := remoteTLSKoanf(t, map[string]interface{}{
		"remote": map[string]interface{}{
			"tls": map[string]interface{}{
				"enable": true, "verify": true,
				"cert": filepath.Join(root, "contrib/testdata/certs/proxy.crt"),
				"key":  filepath.Join(root, "contrib/testdata/certs/proxy.key"),
				"ca":   filepath.Join(root, "contrib/testdata/certs/ca.crt"),
				"sysroots": true,
				"minversion": "", "maxversion": "", "ciphersuites": "", "alpn": "",
			},
		},
	})
	tlsConf, err := ConfigRemote(k)
	if err != nil {
		t.Fatalf("ConfigRemote() error = %v", err)
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

func TestConfigRemote_WithCertKeyNoVerify(t *testing.T) {
	root := projectRoot(t)
	k := remoteTLSKoanf(t, map[string]interface{}{
		"remote": map[string]interface{}{
			"tls": map[string]interface{}{
				"enable": false, "verify": false,
				"cert": filepath.Join(root, "contrib/testdata/certs/proxy.crt"),
				"key":  filepath.Join(root, "contrib/testdata/certs/proxy.key"),
				"ca":   filepath.Join(root, "contrib/testdata/certs/ca.crt"),
				"sysroots": false,
				"minversion": "", "maxversion": "", "ciphersuites": "", "alpn": "",
			},
		},
	})
	tlsConf, err := ConfigRemote(k)
	if err != nil {
		t.Fatalf("ConfigRemote() error = %v", err)
	}
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

func TestConfigRemote_VerifyNoSysRoots(t *testing.T) {
	k := remoteTLSKoanf(t, map[string]interface{}{
		"remote": map[string]interface{}{
			"tls": map[string]interface{}{
				"enable": true, "verify": true,
				"cert": "", "key": "", "ca": "", "sysroots": false,
				"minversion": "", "maxversion": "", "ciphersuites": "", "alpn": "",
			},
		},
	})
	tlsConf, err := ConfigRemote(k)
	if err != nil {
		t.Fatalf("ConfigRemote() error = %v", err)
	}
	if tlsConf == nil {
		t.Fatal("expected non-nil tls.Config when enable=true")
	}
	if tlsConf.InsecureSkipVerify {
		t.Error("InsecureSkipVerify should be false when verify=true")
	}
}
