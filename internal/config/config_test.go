package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMissingFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "missing.yaml")

	cfg, exists, err := Load(path)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if exists {
		t.Fatalf("expected exists=false for missing file")
	}
	if cfg != (Config{}) {
		t.Fatalf("expected empty config, got %+v", cfg)
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "bad.yaml")
	if err := os.WriteFile(path, []byte("defaults: ["), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	_, exists, err := Load(path)
	if err == nil {
		t.Fatalf("expected error for invalid yaml")
	}
	if !exists {
		t.Fatalf("expected exists=true for invalid yaml")
	}
	if !strings.Contains(err.Error(), path) {
		t.Fatalf("expected error to mention path, got %v", err)
	}
}

func TestLoadValidYAML(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "ok.yaml")
	data := []byte("mode: greeting\ndefaults:\n  message: hello\n  title: world\n  icon: /tmp/icon.png\n")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	cfg, exists, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Fatalf("expected exists=true for valid file")
	}
	if cfg.Defaults.Message != "hello" {
		t.Fatalf("unexpected message: %q", cfg.Defaults.Message)
	}
	if cfg.Defaults.Title != "world" {
		t.Fatalf("unexpected title: %q", cfg.Defaults.Title)
	}
	if cfg.Defaults.Icon != "/tmp/icon.png" {
		t.Fatalf("unexpected icon: %q", cfg.Defaults.Icon)
	}
}

func TestLoadInvalidMode(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "bad.yaml")
	data := []byte("mode: nope\n")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	_, exists, err := Load(path)
	if err == nil {
		t.Fatalf("expected error for invalid mode")
	}
	if !exists {
		t.Fatalf("expected exists=true for invalid mode")
	}
}
