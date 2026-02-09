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

	// Write a valid YAML config file (no #tlspxy header required)
	cfgContent := "remote:\n  addr: example.com:443\n"
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

// Suppress unused import warning - fmt is used in PrettyPrintFlagMap test
var _ = fmt.Sprintf
