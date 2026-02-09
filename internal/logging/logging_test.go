package logging

import (
	"log/slog"
	"testing"

	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/v2"
)

func testKoanf(t *testing.T, m map[string]interface{}) *koanf.Koanf {
	t.Helper()
	k := koanf.New(".")
	if err := k.Load(confmap.Provider(m, "."), nil); err != nil {
		t.Fatalf("koanf load error: %v", err)
	}
	return k
}

func TestConfigure_DebugLevel(t *testing.T) {
	k := testKoanf(t, map[string]interface{}{
		"log": map[string]interface{}{"level": "debug", "destination": "stdout", "contents": false},
	})
	Configure(k)
	if Level.Level() != slog.LevelDebug {
		t.Errorf("log level = %v, want LevelDebug", Level.Level())
	}
}

func TestConfigure_WarningLevel(t *testing.T) {
	k := testKoanf(t, map[string]interface{}{
		"log": map[string]interface{}{"level": "warning", "destination": "stdout", "contents": false},
	})
	Configure(k)
	if Level.Level() != slog.LevelWarn {
		t.Errorf("log level = %v, want LevelWarn", Level.Level())
	}
}

func TestConfigure_ErrorLevel(t *testing.T) {
	k := testKoanf(t, map[string]interface{}{
		"log": map[string]interface{}{"level": "error", "destination": "stdout", "contents": false},
	})
	Configure(k)
	if Level.Level() != slog.LevelError {
		t.Errorf("log level = %v, want LevelError", Level.Level())
	}
}

func TestConfigure_DefaultInfoLevel(t *testing.T) {
	k := testKoanf(t, map[string]interface{}{
		"log": map[string]interface{}{"level": "info", "destination": "stdout", "contents": false},
	})
	Configure(k)
	if Level.Level() != slog.LevelInfo {
		t.Errorf("log level = %v, want LevelInfo", Level.Level())
	}
}

func TestConfigure_InvalidLevel(t *testing.T) {
	k := testKoanf(t, map[string]interface{}{
		"log": map[string]interface{}{"level": "nonsense", "destination": "stdout", "contents": false},
	})
	Configure(k)
	if Level.Level() != slog.LevelInfo {
		t.Errorf("log level = %v, want LevelInfo for invalid input", Level.Level())
	}
}

func TestConfigure_EmptyLevel(t *testing.T) {
	k := testKoanf(t, map[string]interface{}{
		"log": map[string]interface{}{"level": "", "destination": "stdout", "contents": false},
	})
	Configure(k)
	if Level.Level() != slog.LevelInfo {
		t.Errorf("log level = %v, want LevelInfo for empty input", Level.Level())
	}
}

func TestConfigure_StdoutDestination(t *testing.T) {
	k := testKoanf(t, map[string]interface{}{
		"log": map[string]interface{}{"level": "info", "destination": "stdout", "contents": false},
	})
	Configure(k)
}
