package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/olebedev/config"
)

func Test_getConfig(t *testing.T) {
	// getConfig reads YAML files from the current working directory that
	// have "#tlspxy" as the first line. We create a temp dir, write a
	// config file, cd into that dir, call getConfig, then restore cwd.
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

	gotCfg, err := getConfig()
	if err != nil {
		t.Fatalf("getConfig() returned error: %v", err)
	}
	if gotCfg == nil {
		t.Fatal("getConfig() returned nil config")
	}

	// The file config should override defaults for remote.addr
	addr, err := gotCfg.String("remote.addr")
	if err != nil {
		t.Fatalf("could not get remote.addr: %v", err)
	}
	if addr != "example.com:443" {
		t.Errorf("remote.addr = %q, want %q", addr, "example.com:443")
	}

	// Defaults should still be present
	serverAddr, err := gotCfg.String("server.addr")
	if err != nil {
		t.Fatalf("could not get server.addr: %v", err)
	}
	if serverAddr != ":9898" {
		t.Errorf("server.addr = %q, want %q", serverAddr, ":9898")
	}
}

func Test_getConfig_NoFiles(t *testing.T) {
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

	gotCfg, err := getConfig()
	if err != nil {
		t.Fatalf("getConfig() returned error: %v", err)
	}
	if gotCfg == nil {
		t.Fatal("getConfig() returned nil config")
	}

	// Should just have defaults
	serverAddr, err := gotCfg.String("server.addr")
	if err != nil {
		t.Fatalf("could not get server.addr: %v", err)
	}
	if serverAddr != ":9898" {
		t.Errorf("server.addr = %q, want %q", serverAddr, ":9898")
	}
}

func Test_prettyPrintFlagMap(t *testing.T) {
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

			prettyPrintFlagMap(tt.m, tt.prefix...)

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

func Test_combineConfigs(t *testing.T) {
	type args struct {
		cfgs []*config.Config
	}
	tests := []struct {
		name  string
		args  args
		wantR *config.Config
	}{
		{
			name: "Combine non-overlapping configs",
			args: args{cfgs: []*config.Config{
				{
					Root: map[string]interface{}{
						"log": map[string]interface{}{
							"level": "debug",
						},
					},
				},
				{
					Root: map[string]interface{}{
						"remote": map[string]interface{}{
							"addr": "google.com:443",
						},
						"log": map[string]interface{}{
							"contents": true,
						},
					},
				},
			}},
			wantR: &config.Config{
				Root: map[string]interface{}{
					"remote": map[string]interface{}{
						"addr": "google.com:443",
					},
					"log": map[string]interface{}{
						"level":    "debug",
						"contents": true,
					},
				},
			},
		},
		{
			name: "Combine overlapping configs",
			args: args{cfgs: []*config.Config{
				{
					Root: map[string]interface{}{
						"log": map[string]interface{}{
							"level": "debug",
						},
					},
				},
				{
					Root: map[string]interface{}{
						"log": map[string]interface{}{
							"level": "error",
						},
					},
				},
			}},
			wantR: &config.Config{
				Root: map[string]interface{}{
					"log": map[string]interface{}{
						"level": "error",
					},
				},
			},
		},
	}
	for _, tt := range tests {
		if gotR := combineConfigs(tt.args.cfgs...); !reflect.DeepEqual(gotR, tt.wantR) {
			t.Errorf("%q. combineConfigs() = %v, want %v", tt.name, gotR, tt.wantR)
		} else {
			t.Logf("%q. combineConfigs() = %v, want %v", tt.name, gotR, tt.wantR)
		}
	}
}

func Test_isCfgFile(t *testing.T) {
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

	// isCfgFile opens files relative to cwd, so make sure we're in the project root
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
		if got := isCfgFile(tt.args.path); got != tt.want {
			t.Errorf("%q. isCfgFile() = %v, want %v", tt.name, got, tt.want)
		} else {
			t.Logf("%q. isCfgFile() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// Suppress unused import warning - fmt is used in prettyPrintFlagMap test
var _ = fmt.Sprintf
