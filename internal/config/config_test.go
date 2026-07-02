package config

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/v2"
)

func TestGetConfig(t *testing.T) {
	// GetConfig reads all .yml/.yaml files from the current working directory.
	// We create a temp dir, write a config file, cd into that dir, call
	// GetConfig, then restore cwd.
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	tmpDir, err := os.MkdirTemp("", "tlspxy-config-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Auto-discovered YAML in the working dir must start with the #tlspxy marker.
	cfgContent := "#tlspxy\nremote:\n  addr: example.com:443\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "test.yml"), []byte(cfgContent), 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	gotK, err := GetConfig()
	if err != nil {
		t.Fatalf("GetConfig() returned error: %v", err)
	}
	if gotK == nil {
		t.Fatal("GetConfig() returned nil config")
	}

	// The file config should override defaults for remote.addr
	addr := gotK.String("remote.addr")
	if addr != "example.com:443" {
		t.Errorf("remote.addr = %q, want %q", addr, "example.com:443")
	}

	// Defaults should still be present
	serverAddr := gotK.String("server.addr")
	if serverAddr != ":9898" {
		t.Errorf("server.addr = %q, want %q", serverAddr, ":9898")
	}
}

func TestDefault_AutoreloadFalse(t *testing.T) {
	k := koanf.New(".")
	if err := k.Load(confmap.Provider(DefaultConfig, "."), nil); err != nil {
		t.Fatalf("loading defaults: %v", err)
	}
	if k.Bool("server.tls.autoreload") {
		t.Error("server.tls.autoreload default should be false")
	}
}

func TestGetConfig_NoFiles(t *testing.T) {
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	tmpDir, err := os.MkdirTemp("", "tlspxy-config-empty")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	gotK, err := GetConfig()
	if err != nil {
		t.Fatalf("GetConfig() returned error: %v", err)
	}
	if gotK == nil {
		t.Fatal("GetConfig() returned nil config")
	}

	// Should just have defaults
	serverAddr := gotK.String("server.addr")
	if serverAddr != ":9898" {
		t.Errorf("server.addr = %q, want %q", serverAddr, ":9898")
	}

	// remote.timeouts.dial defaults to 10s.
	if got := gotK.String("remote.timeouts.dial"); got != "10s" {
		t.Errorf("remote.timeouts.dial = %q, want %q", got, "10s")
	}
	if got := Duration(gotK, "remote.timeouts.dial"); got != 10*time.Second {
		t.Errorf("Duration(remote.timeouts.dial) = %v, want %v", got, 10*time.Second)
	}
}

func TestDuration(t *testing.T) {
	k := newTestKoanf(map[string]interface{}{
		"a": "30s",
		"b": "",
		"c": "notaduration",
	})
	if got := Duration(k, "a"); got != 30*time.Second {
		t.Errorf("Duration(a) = %v, want 30s", got)
	}
	// Empty resolves to 0 (use default / disabled).
	if got := Duration(k, "b"); got != 0 {
		t.Errorf("Duration(b) = %v, want 0", got)
	}
	// Unparseable resolves to 0 (validation is the real gate).
	if got := Duration(k, "c"); got != 0 {
		t.Errorf("Duration(c) = %v, want 0", got)
	}
}

func TestGetConfig_NonYAMLIgnored(t *testing.T) {
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	tmpDir, err := os.MkdirTemp("", "tlspxy-config-nonyaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Write a .txt file that should be ignored
	if err := os.WriteFile(filepath.Join(tmpDir, "config.txt"), []byte("remote:\n  addr: bad\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Write a .json file that should also be ignored
	if err := os.WriteFile(filepath.Join(tmpDir, "config.json"), []byte(`{"remote":{"addr":"bad"}}`), 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	gotK, err := GetConfig()
	if err != nil {
		t.Fatalf("GetConfig() returned error: %v", err)
	}

	// remote.addr should still be the default (empty) since no YAML files were loaded
	addr := gotK.String("remote.addr")
	if addr != "" {
		t.Errorf("remote.addr = %q, want empty (non-YAML files should be ignored)", addr)
	}
}

func TestGetConfig_ExtraPaths(t *testing.T) {
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	// Create an empty working directory so no configs are auto-loaded
	emptyDir, err := os.MkdirTemp("", "tlspxy-config-empty")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(emptyDir)

	// Create a directory with a config file to pass via extraPaths
	extraDir, err := os.MkdirTemp("", "tlspxy-config-extra")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(extraDir)

	cfgContent := "remote:\n  addr: extra-host:9999\n"
	cfgPath := filepath.Join(extraDir, "extra.yml")
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.Chdir(emptyDir); err != nil {
		t.Fatal(err)
	}

	// Test passing a file path
	gotK, err := GetConfig(cfgPath)
	if err != nil {
		t.Fatalf("GetConfig(file) returned error: %v", err)
	}
	if addr := gotK.String("remote.addr"); addr != "extra-host:9999" {
		t.Errorf("remote.addr = %q, want %q", addr, "extra-host:9999")
	}

	// Test passing a directory
	gotK2, err := GetConfig(extraDir)
	if err != nil {
		t.Fatalf("GetConfig(dir) returned error: %v", err)
	}
	if addr := gotK2.String("remote.addr"); addr != "extra-host:9999" {
		t.Errorf("remote.addr = %q, want %q", addr, "extra-host:9999")
	}
}

func TestGetConfig_MarkerRequiredForAutoDiscovery(t *testing.T) {
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	tmpDir, err := os.MkdirTemp("", "tlspxy-config-marker")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Unmarked YAML: looks like a stray docker-compose.yml — must be IGNORED.
	unmarked := "remote:\n  addr: stray-host:1111\nserver:\n  type: http\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "docker-compose.yml"), []byte(unmarked), 0644); err != nil {
		t.Fatal(err)
	}
	// Marked YAML: real config — must be LOADED.
	marked := "#tlspxy\nremote:\n  addr: real-host:2222\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "tlspxy.yml"), []byte(marked), 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	gotK, err := GetConfig()
	if err != nil {
		t.Fatalf("GetConfig() returned error: %v", err)
	}

	// The marked file wins; the unmarked stray file must not have been applied.
	if addr := gotK.String("remote.addr"); addr != "real-host:2222" {
		t.Errorf("remote.addr = %q, want %q (unmarked file should be ignored)", addr, "real-host:2222")
	}
	// server.type from the stray file must NOT have leaked in (default is tcp).
	if typ := gotK.String("server.type"); typ != "tcp" {
		t.Errorf("server.type = %q, want %q (unmarked file should be ignored)", typ, "tcp")
	}
}

func TestGetConfig_ExplicitFileNoMarkerRequired(t *testing.T) {
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	// Empty working dir so auto-discovery loads nothing.
	emptyDir, err := os.MkdirTemp("", "tlspxy-config-empty")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(emptyDir)

	extraDir, err := os.MkdirTemp("", "tlspxy-config-explicit")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(extraDir)

	// Unmarked file passed explicitly via -config must still load.
	unmarked := "remote:\n  addr: explicit-host:3333\n"
	cfgPath := filepath.Join(extraDir, "myconfig.yml")
	if err := os.WriteFile(cfgPath, []byte(unmarked), 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.Chdir(emptyDir); err != nil {
		t.Fatal(err)
	}

	// Explicit file path.
	gotK, err := GetConfig(cfgPath)
	if err != nil {
		t.Fatalf("GetConfig(file) returned error: %v", err)
	}
	if addr := gotK.String("remote.addr"); addr != "explicit-host:3333" {
		t.Errorf("remote.addr = %q, want %q (explicit unmarked file should load)", addr, "explicit-host:3333")
	}

	// Explicit directory path must also load unmarked files.
	gotK2, err := GetConfig(extraDir)
	if err != nil {
		t.Fatalf("GetConfig(dir) returned error: %v", err)
	}
	if addr := gotK2.String("remote.addr"); addr != "explicit-host:3333" {
		t.Errorf("remote.addr = %q, want %q (explicit unmarked dir should load)", addr, "explicit-host:3333")
	}
}

func TestPrettyPrintFlagMap(t *testing.T) {
	tests := []struct {
		name     string
		m        map[string]interface{}
		prefix   []string
		wantSubs []string // substrings that should appear in output
	}{
		{
			name: "Simple string value",
			m: map[string]interface{}{
				"addr": "localhost:8080",
			},
			wantSubs: []string{"-addr=localhost:8080"},
		},
		{
			name: "Bool and int values",
			m: map[string]interface{}{
				"verify": true,
				"port":   443,
			},
			wantSubs: []string{"-verify=true", "-port=443"},
		},
		{
			name: "Nested map with prefix",
			m: map[string]interface{}{
				"cert": "/path/to/cert",
			},
			prefix:   []string{"server", "tls"},
			wantSubs: []string{"-server-tls-cert=/path/to/cert"},
		},
		{
			name: "Recursive nested map",
			m: map[string]interface{}{
				"tls": map[string]interface{}{
					"cert": "test.crt",
				},
			},
			wantSubs: []string{"-tls-cert=test.crt"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture stdout
			old := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			PrettyPrintFlagMap(tt.m, tt.prefix...)

			w.Close()
			var buf bytes.Buffer
			io.Copy(&buf, r)
			os.Stdout = old

			output := buf.String()
			for _, sub := range tt.wantSubs {
				if !strings.Contains(output, sub) {
					t.Errorf("output %q does not contain %q", output, sub)
				}
			}
		})
	}
}

// newTestKoanf creates a koanf instance from a map for testing.
func newTestKoanf(m map[string]interface{}) *koanf.Koanf {
	k := koanf.New(".")
	k.Load(confmap.Provider(m, "."), nil)
	return k
}

func TestIsCfgFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tlspxy-iscfg-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test files
	ymlFile := filepath.Join(tmpDir, "config.yml")
	yamlFile := filepath.Join(tmpDir, "config.yaml")
	txtFile := filepath.Join(tmpDir, "config.txt")
	jsonFile := filepath.Join(tmpDir, "config.json")

	for _, f := range []string{ymlFile, yamlFile, txtFile, jsonFile} {
		if err := os.WriteFile(f, []byte("test"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name string
		path string
		want bool
	}{
		{
			name: ".yml extension",
			path: ymlFile,
			want: true,
		},
		{
			name: ".yaml extension",
			path: yamlFile,
			want: true,
		},
		{
			name: ".txt extension",
			path: txtFile,
			want: false,
		},
		{
			name: ".json extension",
			path: jsonFile,
			want: false,
		},
		{
			name: "missing file with .yml extension",
			path: filepath.Join(tmpDir, "nonexistent.yml"),
			want: false,
		},
		{
			name: "missing file with .yaml extension",
			path: filepath.Join(tmpDir, "nonexistent.yaml"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsCfgFile(tt.path); got != tt.want {
				t.Errorf("IsCfgFile(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestValidateConfig(t *testing.T) {
	// helper to build a koanf instance from a map, merged with defaults
	makeConfig := func(overrides map[string]interface{}) *koanf.Koanf {
		base := map[string]interface{}{
			"server": map[string]interface{}{
				"addr": ":9898",
				"type": "tcp",
				"tls": map[string]interface{}{
					"cert": "",
					"key":  "",
					"letsencrypt": map[string]interface{}{
						"enable": false,
						"domain": "",
					},
				},
			},
			"remote": map[string]interface{}{
				"addr": "localhost:8080",
			},
		}
		combined := CombineMaps(base, overrides)
		return newTestKoanf(combined)
	}

	tests := []struct {
		name      string
		overrides map[string]interface{}
		wantErr   string // substring expected in error, empty means no error
	}{
		{
			name:      "valid tcp config",
			overrides: map[string]interface{}{},
			wantErr:   "",
		},
		{
			name: "valid http config",
			overrides: map[string]interface{}{
				"server": map[string]interface{}{
					"type": "http",
				},
				"remote": map[string]interface{}{
					"addr": "http://localhost:8080",
				},
			},
			wantErr: "",
		},
		{
			name: "https config requires server TLS",
			overrides: map[string]interface{}{
				"server": map[string]interface{}{
					"type": "https",
				},
				"remote": map[string]interface{}{
					"addr": "https://example.com",
				},
			},
			wantErr: "server.type https",
		},
		{
			name: "valid https config with server certs",
			overrides: map[string]interface{}{
				"server": map[string]interface{}{
					"type": "https",
					"tls": map[string]interface{}{
						"cert": "/path/to/cert.pem",
						"key":  "/path/to/key.pem",
					},
				},
				"remote": map[string]interface{}{
					"addr": "https://example.com",
				},
			},
			wantErr: "",
		},
		{
			name: "invalid server.type",
			overrides: map[string]interface{}{
				"server": map[string]interface{}{
					"type": "udp",
				},
			},
			wantErr: "server.type",
		},
		{
			name: "empty remote.addr",
			overrides: map[string]interface{}{
				"remote": map[string]interface{}{
					"addr": "",
				},
			},
			wantErr: "remote.addr",
		},
		{
			name: "invalid server.addr",
			overrides: map[string]interface{}{
				"server": map[string]interface{}{
					"addr": "not a valid address::::",
				},
			},
			wantErr: "server.addr",
		},
		{
			name: "cert without key",
			overrides: map[string]interface{}{
				"server": map[string]interface{}{
					"tls": map[string]interface{}{
						"cert": "/path/to/cert.pem",
						"key":  "",
					},
				},
			},
			wantErr: "server.tls.key",
		},
		{
			name: "key without cert",
			overrides: map[string]interface{}{
				"server": map[string]interface{}{
					"tls": map[string]interface{}{
						"cert": "",
						"key":  "/path/to/key.pem",
					},
				},
			},
			wantErr: "server.tls.cert",
		},
		{
			name: "cert and key both set is valid",
			overrides: map[string]interface{}{
				"server": map[string]interface{}{
					"tls": map[string]interface{}{
						"cert": "/path/to/cert.pem",
						"key":  "/path/to/key.pem",
					},
				},
			},
			wantErr: "",
		},
		{
			name: "letsencrypt enabled with empty domain",
			overrides: map[string]interface{}{
				"server": map[string]interface{}{
					"tls": map[string]interface{}{
						"letsencrypt": map[string]interface{}{
							"enable": true,
							"domain": "",
						},
					},
				},
			},
			wantErr: "server.tls.letsencrypt.domain",
		},
		{
			name: "letsencrypt enabled with domain is valid",
			overrides: map[string]interface{}{
				"server": map[string]interface{}{
					"tls": map[string]interface{}{
						"letsencrypt": map[string]interface{}{
							"enable": true,
							"domain": "example.com",
						},
					},
				},
			},
			wantErr: "",
		},
		{
			name: "http type with non-URL remote.addr",
			overrides: map[string]interface{}{
				"server": map[string]interface{}{
					"type": "http",
				},
				"remote": map[string]interface{}{
					"addr": "localhost:8080",
				},
			},
			wantErr: "remote.addr",
		},
		{
			name: "https type with bare host remote.addr",
			overrides: map[string]interface{}{
				"server": map[string]interface{}{
					"type": "https",
					"tls": map[string]interface{}{
						"cert": "/path/to/cert.pem",
						"key":  "/path/to/key.pem",
					},
				},
				"remote": map[string]interface{}{
					"addr": "example.com",
				},
			},
			wantErr: "remote.addr",
		},
		{
			name: "remote cert without key",
			overrides: map[string]interface{}{
				"remote": map[string]interface{}{
					"tls": map[string]interface{}{
						"cert": "/path/to/cert.pem",
						"key":  "",
					},
				},
			},
			wantErr: "remote.tls.key",
		},
		{
			name: "remote key without cert",
			overrides: map[string]interface{}{
				"remote": map[string]interface{}{
					"tls": map[string]interface{}{
						"cert": "",
						"key":  "/path/to/key.pem",
					},
				},
			},
			wantErr: "remote.tls.cert",
		},
		{
			name: "remote TLS material requires TLS enabled",
			overrides: map[string]interface{}{
				"remote": map[string]interface{}{
					"tls": map[string]interface{}{
						"enable": false,
						"ca":     "/path/to/ca.pem",
					},
				},
			},
			wantErr: "remote.tls.enable",
		},
		{
			name: "valid timeout durations",
			overrides: map[string]interface{}{
				"server": map[string]interface{}{
					"timeouts": map[string]interface{}{
						"read":  "30s",
						"write": "10s",
						"idle":  "5m",
					},
				},
			},
			wantErr: "",
		},
		{
			name: "invalid timeout read duration",
			overrides: map[string]interface{}{
				"server": map[string]interface{}{
					"timeouts": map[string]interface{}{
						"read":  "not-a-duration",
						"write": "0s",
						"idle":  "0s",
					},
				},
			},
			wantErr: "server.timeouts.read",
		},
		{
			name: "invalid timeout write duration",
			overrides: map[string]interface{}{
				"server": map[string]interface{}{
					"timeouts": map[string]interface{}{
						"read":  "0s",
						"write": "xyz",
						"idle":  "0s",
					},
				},
			},
			wantErr: "server.timeouts.write",
		},
		{
			name: "invalid timeout idle duration",
			overrides: map[string]interface{}{
				"server": map[string]interface{}{
					"timeouts": map[string]interface{}{
						"read":  "0s",
						"write": "0s",
						"idle":  "abc",
					},
				},
			},
			wantErr: "server.timeouts.idle",
		},
		{
			name: "valid TLS version",
			overrides: map[string]interface{}{
				"server": map[string]interface{}{
					"tls": map[string]interface{}{
						"minversion": "1.2",
						"maxversion": "1.3",
					},
				},
			},
			wantErr: "",
		},
		{
			name: "invalid server TLS minversion",
			overrides: map[string]interface{}{
				"server": map[string]interface{}{
					"tls": map[string]interface{}{
						"minversion": "2.0",
					},
				},
			},
			wantErr: "server.tls.minversion",
		},
		{
			name: "server.http2 with tcp is invalid",
			overrides: map[string]interface{}{
				"server": map[string]interface{}{
					"type":  "tcp",
					"http2": true,
				},
			},
			wantErr: "server.http2",
		},
		{
			name: "server.http2 with http is valid",
			overrides: map[string]interface{}{
				"server": map[string]interface{}{
					"type":  "http",
					"http2": true,
				},
				"remote": map[string]interface{}{
					"addr": "http://localhost:8080",
				},
			},
			wantErr: "",
		},
		{
			name: "invalid remote TLS maxversion",
			overrides: map[string]interface{}{
				"remote": map[string]interface{}{
					"addr": "localhost:8080",
					"tls": map[string]interface{}{
						"maxversion": "bogus",
					},
				},
			},
			wantErr: "remote.tls.maxversion",
		},
		{
			name: "invalid cipher suite",
			overrides: map[string]interface{}{
				"server": map[string]interface{}{
					"tls": map[string]interface{}{
						"ciphersuites": "BOGUS_CIPHER",
					},
				},
			},
			wantErr: "server.tls.ciphersuites",
		},
		{
			name: "min version > max version",
			overrides: map[string]interface{}{
				"server": map[string]interface{}{
					"tls": map[string]interface{}{
						"minversion": "1.3",
						"maxversion": "1.2",
					},
				},
			},
			wantErr: "server.tls.minversion",
		},
		{
			name: "valid proxyprotocol v1 in tcp mode",
			overrides: map[string]interface{}{
				"remote": map[string]interface{}{
					"proxyprotocol": "v1",
				},
			},
			wantErr: "",
		},
		{
			name: "valid proxyprotocol v2 in tcp mode",
			overrides: map[string]interface{}{
				"remote": map[string]interface{}{
					"proxyprotocol": "v2",
				},
			},
			wantErr: "",
		},
		{
			name: "invalid proxyprotocol value",
			overrides: map[string]interface{}{
				"remote": map[string]interface{}{
					"proxyprotocol": "v3",
				},
			},
			wantErr: "remote.proxyprotocol must be one of",
		},
		{
			name: "proxyprotocol rejected in http mode",
			overrides: map[string]interface{}{
				"server": map[string]interface{}{
					"type": "http",
				},
				"remote": map[string]interface{}{
					"addr":          "http://localhost:8080",
					"proxyprotocol": "v1",
				},
			},
			wantErr: "remote.proxyprotocol is only valid with server.type tcp",
		},
		{
			name: "invalid handshake timeout",
			overrides: map[string]interface{}{
				"server": map[string]interface{}{
					"timeouts": map[string]interface{}{
						"handshake": "notaduration",
					},
				},
			},
			wantErr: "server.timeouts.handshake",
		},
		{
			name: "invalid remote dial timeout",
			overrides: map[string]interface{}{
				"remote": map[string]interface{}{
					"timeouts": map[string]interface{}{
						"dial": "30sec",
					},
				},
			},
			wantErr: "remote.timeouts.dial",
		},
		{
			name: "valid remote dial timeout",
			overrides: map[string]interface{}{
				"remote": map[string]interface{}{
					"timeouts": map[string]interface{}{
						"dial": "5s",
					},
				},
			},
			wantErr: "",
		},
		{
			name: "empty remote dial timeout allowed",
			overrides: map[string]interface{}{
				"remote": map[string]interface{}{
					"timeouts": map[string]interface{}{
						"dial": "",
					},
				},
			},
			wantErr: "",
		},
		{
			name: "empty read timeout allowed",
			overrides: map[string]interface{}{
				"server": map[string]interface{}{
					"timeouts": map[string]interface{}{
						"read": "",
					},
				},
			},
			wantErr: "",
		},
		{
			name: "sigv4 rejected in tcp mode",
			overrides: map[string]interface{}{
				"sigv4": map[string]interface{}{
					"enable":   true,
					"keystore": "/etc/tlspxy/keystore.yml",
					"service":  "s3",
					"region":   "us-east-1",
				},
			},
			wantErr: "sigv4.enable is only valid with server.type http or https",
		},
		{
			name: "sigv4 requires keystore",
			overrides: map[string]interface{}{
				"server": map[string]interface{}{"type": "https", "tls": map[string]interface{}{"cert": "/c", "key": "/k"}},
				"remote": map[string]interface{}{"addr": "https://example.com"},
				"sigv4": map[string]interface{}{
					"enable":  true,
					"service": "s3",
					"region":  "us-east-1",
				},
			},
			wantErr: "sigv4.keystore is required",
		},
		{
			name: "sigv4 invalid creds source",
			overrides: map[string]interface{}{
				"server": map[string]interface{}{"type": "https", "tls": map[string]interface{}{"cert": "/c", "key": "/k"}},
				"remote": map[string]interface{}{"addr": "https://example.com"},
				"sigv4": map[string]interface{}{
					"enable":   true,
					"keystore": "/ks.yml",
					"service":  "s3",
					"region":   "us-east-1",
					"creds":    map[string]interface{}{"source": "magic"},
				},
			},
			wantErr: "sigv4.creds.source must be one of",
		},
		{
			name: "sigv4 static creds require keys",
			overrides: map[string]interface{}{
				"server": map[string]interface{}{"type": "https", "tls": map[string]interface{}{"cert": "/c", "key": "/k"}},
				"remote": map[string]interface{}{"addr": "https://example.com"},
				"sigv4": map[string]interface{}{
					"enable":   true,
					"keystore": "/ks.yml",
					"service":  "s3",
					"region":   "us-east-1",
					"creds":    map[string]interface{}{"source": "static"},
				},
			},
			wantErr: "sigv4.creds.accesskey and sigv4.creds.secretkey are required",
		},
		{
			name: "sigv4 requires service/region without hostoverride",
			overrides: map[string]interface{}{
				"server": map[string]interface{}{"type": "https", "tls": map[string]interface{}{"cert": "/c", "key": "/k"}},
				"remote": map[string]interface{}{"addr": "https://example.com"},
				"sigv4": map[string]interface{}{
					"enable":   true,
					"keystore": "/ks.yml",
				},
			},
			wantErr: "sigv4.service is required",
		},
		{
			name: "sigv4 valid https config",
			overrides: map[string]interface{}{
				"server": map[string]interface{}{"type": "https", "tls": map[string]interface{}{"cert": "/c", "key": "/k"}},
				"remote": map[string]interface{}{"addr": "https://example.com"},
				"sigv4": map[string]interface{}{
					"enable":   true,
					"keystore": "/ks.yml",
					"service":  "s3",
					"region":   "us-east-1",
					"creds":    map[string]interface{}{"source": "default"},
				},
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := makeConfig(tt.overrides)
			err := ValidateConfig(cfg)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("ValidateConfig() unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("ValidateConfig() expected error containing %q, got nil", tt.wantErr)
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("ValidateConfig() error = %q, want substring %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

func TestCombineMaps_NonOverlapping(t *testing.T) {
	m1 := map[string]interface{}{
		"a": "1",
		"b": "2",
	}
	m2 := map[string]interface{}{
		"c": "3",
		"d": "4",
	}

	result := CombineMaps(m1, m2)

	if result["a"] != "1" {
		t.Errorf("result[a] = %v, want 1", result["a"])
	}
	if result["b"] != "2" {
		t.Errorf("result[b] = %v, want 2", result["b"])
	}
	if result["c"] != "3" {
		t.Errorf("result[c] = %v, want 3", result["c"])
	}
	if result["d"] != "4" {
		t.Errorf("result[d] = %v, want 4", result["d"])
	}
}

func TestCombineMaps_Overlapping(t *testing.T) {
	m1 := map[string]interface{}{
		"key": "original",
	}
	m2 := map[string]interface{}{
		"key": "override",
	}

	result := CombineMaps(m1, m2)

	if result["key"] != "override" {
		t.Errorf("result[key] = %v, want override", result["key"])
	}
}

func TestCombineMaps_Nested(t *testing.T) {
	m1 := map[string]interface{}{
		"outer": map[string]interface{}{
			"inner1": "value1",
			"inner2": "value2",
		},
	}
	m2 := map[string]interface{}{
		"outer": map[string]interface{}{
			"inner2": "overridden",
			"inner3": "value3",
		},
	}

	result := CombineMaps(m1, m2)

	outer, ok := result["outer"].(map[string]interface{})
	if !ok {
		t.Fatal("outer is not a map")
	}
	if outer["inner1"] != "value1" {
		t.Errorf("outer.inner1 = %v, want value1", outer["inner1"])
	}
	if outer["inner2"] != "overridden" {
		t.Errorf("outer.inner2 = %v, want overridden", outer["inner2"])
	}
	if outer["inner3"] != "value3" {
		t.Errorf("outer.inner3 = %v, want value3", outer["inner3"])
	}
}

func TestCombineMaps_Empty(t *testing.T) {
	result := CombineMaps()
	if len(result) != 0 {
		t.Errorf("expected empty map, got %v", result)
	}
}

func TestCombineMaps_ThreeMaps(t *testing.T) {
	m1 := map[string]interface{}{"a": "1"}
	m2 := map[string]interface{}{"a": "2", "b": "2"}
	m3 := map[string]interface{}{"a": "3", "c": "3"}

	result := CombineMaps(m1, m2, m3)

	if result["a"] != "3" {
		t.Errorf("result[a] = %v, want 3 (last map wins)", result["a"])
	}
	if result["b"] != "2" {
		t.Errorf("result[b] = %v, want 2", result["b"])
	}
	if result["c"] != "3" {
		t.Errorf("result[c] = %v, want 3", result["c"])
	}
}

func TestCombineMaps_TypeMismatch(t *testing.T) {
	// If defaults have a map but user config has a scalar for the same key,
	// the scalar should overwrite the map without panicking.
	defaults := map[string]interface{}{
		"server": map[string]interface{}{
			"addr": "localhost",
			"port": 8080,
		},
	}
	userCfg := map[string]interface{}{
		"server": "just-a-string",
	}

	result := CombineMaps(defaults, userCfg)

	if result["server"] != "just-a-string" {
		t.Errorf("result[server] = %v, want \"just-a-string\"", result["server"])
	}

	// Reverse: user has a map where defaults have a scalar.
	// The map should overwrite the scalar without panicking.
	defaults2 := map[string]interface{}{
		"server": "just-a-string",
	}
	userCfg2 := map[string]interface{}{
		"server": map[string]interface{}{
			"addr": "localhost",
		},
	}

	result2 := CombineMaps(defaults2, userCfg2)

	inner, ok := result2["server"].(map[string]interface{})
	if !ok {
		t.Fatalf("result2[server] should be a map, got %T", result2["server"])
	}
	if inner["addr"] != "localhost" {
		t.Errorf("result2[server][addr] = %v, want \"localhost\"", inner["addr"])
	}
}

func TestLoadEnvVars(t *testing.T) {
	// LoadEnvVars should only pick up TLSPXY_-prefixed vars and strip the prefix.
	k := koanf.New(".")

	// Set a prefixed var that should be loaded.
	t.Setenv("TLSPXY_REMOTE_ADDR", "envhost:1234")

	// Set an un-prefixed var that must NOT leak into config.
	t.Setenv("REMOTE_ADDR", "should-not-appear")

	LoadEnvVars(k)

	got := k.String("remote.addr")
	if got != "envhost:1234" {
		t.Errorf("remote.addr = %q, want %q", got, "envhost:1234")
	}

	// Ensure the un-prefixed REMOTE_ADDR did not sneak in.
	all := k.All()
	for key, val := range all {
		if key == "remote.addr" {
			continue
		}
		// If any key looks like it was derived from REMOTE_ADDR without prefix, fail.
		if val == "should-not-appear" {
			t.Errorf("un-prefixed env var leaked into config key %q", key)
		}
	}
}

func TestParseConfigPaths(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "no config flags",
			args: []string{"-server-addr", ":8080"},
			want: nil,
		},
		{
			name: "single -config with space",
			args: []string{"-config", "/etc/tlspxy.yml"},
			want: []string{"/etc/tlspxy.yml"},
		},
		{
			name: "single --config with space",
			args: []string{"--config", "/etc/tlspxy.yml"},
			want: []string{"/etc/tlspxy.yml"},
		},
		{
			name: "single -config= form",
			args: []string{"-config=/etc/tlspxy.yml"},
			want: []string{"/etc/tlspxy.yml"},
		},
		{
			name: "single --config= form",
			args: []string{"--config=/etc/tlspxy.yml"},
			want: []string{"/etc/tlspxy.yml"},
		},
		{
			name: "multiple -config flags",
			args: []string{"-config", "/etc/a.yml", "-config", "/etc/b.yml"},
			want: []string{"/etc/a.yml", "/etc/b.yml"},
		},
		{
			name: "mixed with other flags",
			args: []string{"-server-addr", ":8080", "-config", "/etc/tlspxy.yml", "-remote-addr", "host:443"},
			want: []string{"/etc/tlspxy.yml"},
		},
		{
			name: "-config at end with no value",
			args: []string{"-config"},
			want: nil,
		},
		{
			name: "empty args",
			args: []string{},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseConfigPaths(tt.args)
			if len(got) != len(tt.want) {
				t.Fatalf("ParseConfigPaths(%v) = %v, want %v", tt.args, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("ParseConfigPaths(%v)[%d] = %q, want %q", tt.args, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestFlagDescCompleteness(t *testing.T) {
	// Verify that every leaf key in DefaultConfig has a corresponding entry
	// in the flagDesc map.
	var walkDefaults func(m map[string]interface{}, prefix string)
	walkDefaults = func(m map[string]interface{}, prefix string) {
		for key, val := range m {
			flagName := key
			if prefix != "" {
				flagName = prefix + "-" + key
			}
			switch v := val.(type) {
			case map[string]interface{}:
				walkDefaults(v, flagName)
			default:
				if _, ok := flagDesc[flagName]; !ok {
					t.Errorf("DefaultConfig leaf key %q has no entry in flagDesc", flagName)
				}
			}
		}
	}
	walkDefaults(DefaultConfig, "")
}

func TestHelpGroupsCoversAllFlags(t *testing.T) {
	// Verify that every flag in flagDesc appears in exactly one help group.
	inGroup := make(map[string]bool)
	for _, group := range helpGroups() {
		for _, name := range group.flags {
			if inGroup[name] {
				t.Errorf("flag %q appears in multiple help groups", name)
			}
			inGroup[name] = true
		}
	}

	for name := range flagDesc {
		if !inGroup[name] {
			t.Errorf("flagDesc entry %q is not in any help group", name)
		}
	}
}

func TestExampleConfigs(t *testing.T) {
	root := projectRoot(t)
	pattern := filepath.Join(root, "contrib/examples/*.yml")
	files, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("Glob(%q) error: %v", pattern, err)
	}
	if len(files) == 0 {
		t.Fatal("no example configs found")
	}

	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			// Verify #tlspxy header
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatalf("ReadFile(%q) error: %v", f, err)
			}
			if !strings.HasPrefix(string(data), "#tlspxy") {
				t.Errorf("example %s does not start with #tlspxy header", filepath.Base(f))
			}

			// Verify it parses without error when merged with defaults
			k := koanf.New(".")
			if err := k.Load(confmap.Provider(DefaultConfig, "."), nil); err != nil {
				t.Fatalf("loading defaults: %v", err)
			}
			if err := loadConfigFile(k, f); err != nil {
				t.Fatalf("loading %s: %v", filepath.Base(f), err)
			}
		})
	}
}

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

// Suppress unused import warning - fmt is used in PrettyPrintFlagMap test
var _ = fmt.Sprintf
