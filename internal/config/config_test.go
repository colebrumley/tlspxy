package config

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/v2"
)

func TestGetConfig(t *testing.T) {
	// GetConfig reads YAML files from the current working directory that
	// have "#tlspxy" as the first line. We create a temp dir, write a
	// config file, cd into that dir, call GetConfig, then restore cwd.
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

	// Write a valid config file with #tlspxy header
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
	type args struct {
		path string
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "Valid header",
			args: args{path: "contrib/testdata/config/isCfgFile_true.yml"},
			want: true,
		},
		{
			name: "No header",
			args: args{path: "contrib/testdata/config/isCfgFile_false.yml"},
			want: false,
		},
		{
			name: "Invalid header",
			args: args{path: "contrib/testdata/config/isCfgFile_invalid.yml"},
			want: false,
		},
		{
			name: "Missing file",
			args: args{path: "contrib/testdata/config/isCfgFile.yml"},
			want: false,
		},
	}

	// IsCfgFile opens files relative to cwd, so make sure we're in the project root
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	// Find project root by looking for go.mod
	dir := origDir
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find project root")
		}
		dir = parent
	}
	os.Chdir(dir)

	for _, tt := range tests {
		if got := IsCfgFile(tt.args.path); got != tt.want {
			t.Errorf("%q. IsCfgFile() = %v, want %v", tt.name, got, tt.want)
		} else {
			t.Logf("%q. IsCfgFile() = %v, want %v", tt.name, got, tt.want)
		}
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
						"domain": "example.org",
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
			name: "valid https config",
			overrides: map[string]interface{}{
				"server": map[string]interface{}{
					"type": "https",
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
				},
				"remote": map[string]interface{}{
					"addr": "example.com",
				},
			},
			wantErr: "remote.addr",
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

// Suppress unused import warning - fmt is used in PrettyPrintFlagMap test
var _ = fmt.Sprintf
