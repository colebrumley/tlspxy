package main

import (
	"reflect"
	"testing"

	"github.com/olebedev/config"
)

func Test_getConfig(t *testing.T) {
	tests := []struct {
		name    string
		wantCfg *config.Config
		wantErr bool
	}{
	// TODO: Add test cases.
	}
	for _, tt := range tests {
		gotCfg, err := getConfig()
		if (err != nil) != tt.wantErr {
			t.Errorf("%q. getConfig() error = %v, wantErr %v", tt.name, err, tt.wantErr)
			continue
		}
		if !reflect.DeepEqual(gotCfg, tt.wantCfg) {
			t.Errorf("%q. getConfig() = %v, want %v", tt.name, gotCfg, tt.wantCfg)
		}
	}
}

func Test_prettyPrintFlagMap(t *testing.T) {
	type args struct {
		m      map[string]interface{}
		prefix []string
	}
	tests := []struct {
		name string
		args args
	}{
	// TODO: Add test cases.
	}
	for _, tt := range tests {
		prettyPrintFlagMap(tt.args.m, tt.args.prefix...)
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
	for _, tt := range tests {
		if got := isCfgFile(tt.args.path); got != tt.want {
			t.Errorf("%q. isCfgFile() = %v, want %v", tt.name, got, tt.want)
		} else {
			t.Logf("%q. isCfgFile() = %v, want %v", tt.name, got, tt.want)
		}
	}
}
