package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCombineMaps_NonOverlapping(t *testing.T) {
	m1 := map[string]interface{}{
		"a": "1",
		"b": "2",
	}
	m2 := map[string]interface{}{
		"c": "3",
		"d": "4",
	}

	result := combineMaps(m1, m2)

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

	result := combineMaps(m1, m2)

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

	result := combineMaps(m1, m2)

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
	result := combineMaps()
	if len(result) != 0 {
		t.Errorf("expected empty map, got %v", result)
	}
}

func TestCombineMaps_ThreeMaps(t *testing.T) {
	m1 := map[string]interface{}{"a": "1"}
	m2 := map[string]interface{}{"a": "2", "b": "2"}
	m3 := map[string]interface{}{"a": "3", "c": "3"}

	result := combineMaps(m1, m2, m3)

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

func TestSingleJoiningSlash(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want string
	}{
		{
			name: "Both have slash",
			a:    "http://example.com/",
			b:    "/path",
			want: "http://example.com/path",
		},
		{
			name: "Neither has slash",
			a:    "http://example.com",
			b:    "path",
			want: "http://example.com/path",
		},
		{
			name: "Only a has slash",
			a:    "http://example.com/",
			b:    "path",
			want: "http://example.com/path",
		},
		{
			name: "Only b has slash",
			a:    "http://example.com",
			b:    "/path",
			want: "http://example.com/path",
		},
		{
			name: "Empty b",
			a:    "http://example.com/",
			b:    "",
			want: "http://example.com/",
		},
		{
			name: "Empty a",
			a:    "",
			b:    "/path",
			want: "/path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := singleJoiningSlash(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("singleJoiningSlash(%q, %q) = %q, want %q", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestFileExists(t *testing.T) {
	// Test with an existing file
	tmpFile, err := os.CreateTemp("", "tlspxy-test")
	if err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	if !fileExists(tmpFile.Name()) {
		t.Errorf("fileExists(%q) = false, want true", tmpFile.Name())
	}

	// Test with a non-existing file
	if fileExists(filepath.Join(os.TempDir(), "nonexistent-file-tlspxy-test-12345")) {
		t.Error("fileExists(nonexistent) = true, want false")
	}
}

func TestFileExists_Directory(t *testing.T) {
	// fileExists should return true for directories too (os.Stat works on dirs)
	tmpDir, err := os.MkdirTemp("", "tlspxy-test-dir")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	if !fileExists(tmpDir) {
		t.Errorf("fileExists(%q) = false for directory, want true", tmpDir)
	}
}
