package main

import (
	"testing"

	"github.com/olebedev/config"
	log "github.com/sirupsen/logrus"
)

func TestConfigLogging_DebugLevel(t *testing.T) {
	cfg, err := config.ParseYaml(`log: { level: "debug", destination: "stdout", contents: false }`)
	if err != nil {
		t.Fatalf("ParseYaml error: %v", err)
	}
	configLogging(cfg)
	if log.GetLevel() != log.DebugLevel {
		t.Errorf("log level = %v, want DebugLevel", log.GetLevel())
	}
}

func TestConfigLogging_WarningLevel(t *testing.T) {
	cfg, err := config.ParseYaml(`log: { level: "warning", destination: "stdout", contents: false }`)
	if err != nil {
		t.Fatalf("ParseYaml error: %v", err)
	}
	configLogging(cfg)
	if log.GetLevel() != log.WarnLevel {
		t.Errorf("log level = %v, want WarnLevel", log.GetLevel())
	}
}

func TestConfigLogging_ErrorLevel(t *testing.T) {
	cfg, err := config.ParseYaml(`log: { level: "error", destination: "stdout", contents: false }`)
	if err != nil {
		t.Fatalf("ParseYaml error: %v", err)
	}
	configLogging(cfg)
	if log.GetLevel() != log.ErrorLevel {
		t.Errorf("log level = %v, want ErrorLevel", log.GetLevel())
	}
}

func TestConfigLogging_DefaultInfoLevel(t *testing.T) {
	cfg, err := config.ParseYaml(`log: { level: "info", destination: "stdout", contents: false }`)
	if err != nil {
		t.Fatalf("ParseYaml error: %v", err)
	}
	configLogging(cfg)
	if log.GetLevel() != log.InfoLevel {
		t.Errorf("log level = %v, want InfoLevel", log.GetLevel())
	}
}

func TestConfigLogging_InvalidLevel(t *testing.T) {
	cfg, err := config.ParseYaml(`log: { level: "nonsense", destination: "stdout", contents: false }`)
	if err != nil {
		t.Fatalf("ParseYaml error: %v", err)
	}
	configLogging(cfg)
	if log.GetLevel() != log.InfoLevel {
		t.Errorf("log level = %v, want InfoLevel for invalid input", log.GetLevel())
	}
}

func TestConfigLogging_EmptyLevel(t *testing.T) {
	cfg, err := config.ParseYaml(`log: { level: "", destination: "stdout", contents: false }`)
	if err != nil {
		t.Fatalf("ParseYaml error: %v", err)
	}
	configLogging(cfg)
	if log.GetLevel() != log.InfoLevel {
		t.Errorf("log level = %v, want InfoLevel for empty input", log.GetLevel())
	}
}

func TestConfigLogging_StdoutDestination(t *testing.T) {
	cfg, err := config.ParseYaml(`log: { level: "info", destination: "stdout", contents: false }`)
	if err != nil {
		t.Fatalf("ParseYaml error: %v", err)
	}
	// Should not panic or error
	configLogging(cfg)
}
