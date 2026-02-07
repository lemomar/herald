package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func resetConfigHooks(t *testing.T) {
	t.Helper()
	origUserHomeDirFn := userHomeDirFn
	t.Cleanup(func() {
		userHomeDirFn = origUserHomeDirFn
	})
}

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
	data := []byte("mode: greeting\ndefaults:\n  message: hello\n  title: world\n  icon: /tmp/icon.png\ndaemon:\n  server_url: wss://example.com/events\n  token: abc\n  reconnect_sec: 7\n")
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
	if cfg.Daemon.ServerURL != "wss://example.com/events" {
		t.Fatalf("unexpected server url: %q", cfg.Daemon.ServerURL)
	}
	if cfg.Daemon.Token != "abc" {
		t.Fatalf("unexpected token: %q", cfg.Daemon.Token)
	}
	if cfg.Daemon.ReconnectSec != 7 {
		t.Fatalf("unexpected reconnect sec: %d", cfg.Daemon.ReconnectSec)
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

func TestLoadInvalidDaemonURLScheme(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "bad.yaml")
	data := []byte("daemon:\n  server_url: http://example.com/events\n")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	_, exists, err := Load(path)
	if err == nil {
		t.Fatalf("expected error for invalid daemon url scheme")
	}
	if !exists {
		t.Fatalf("expected exists=true for invalid daemon url scheme")
	}
}

func TestLoadDaemonReconnectDefault(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "ok.yaml")
	data := []byte("daemon:\n  server_url: wss://example.com/events\n  reconnect_sec: 0\n")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	cfg, exists, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Fatalf("expected exists=true for valid daemon config")
	}
	if cfg.Daemon.ReconnectSec != 5 {
		t.Fatalf("expected default reconnect_sec=5, got %d", cfg.Daemon.ReconnectSec)
	}
}

func TestValidateDaemonRequiresServerURL(t *testing.T) {
	err := ValidateDaemon(DaemonConfig{})
	if err == nil {
		t.Fatalf("expected error when daemon.server_url is missing")
	}
}

func TestValidateDaemonHostRequired(t *testing.T) {
	err := ValidateDaemon(DaemonConfig{ServerURL: "ws:///missing-host"})
	if err == nil {
		t.Fatalf("expected error when daemon.server_url has no host")
	}
}

func TestDefaultPathUsesHome(t *testing.T) {
	resetConfigHooks(t)
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	path, err := DefaultPath()
	if err != nil {
		t.Fatalf("default path failed: %v", err)
	}
	want := filepath.Join(tmp, ".heraldrc")
	if path != want {
		t.Fatalf("expected %q, got %q", want, path)
	}
}

func TestDefaultPathHomeError(t *testing.T) {
	resetConfigHooks(t)
	userHomeDirFn = func() (string, error) {
		return "", errors.New("no home")
	}

	if _, err := DefaultPath(); err == nil {
		t.Fatalf("expected home dir resolution error")
	}
}

func TestValidateDaemonDefaultsHelper(t *testing.T) {
	if err := validateDaemonDefaults(DaemonConfig{}); err != nil {
		t.Fatalf("expected empty daemon defaults to pass, got %v", err)
	}
	if err := validateDaemonDefaults(DaemonConfig{ServerURL: "ws://example.com/ws"}); err != nil {
		t.Fatalf("expected valid daemon defaults to pass, got %v", err)
	}
}

func TestValidateDaemonParseError(t *testing.T) {
	err := ValidateDaemon(DaemonConfig{ServerURL: "http://[::1"})
	if err == nil || !strings.Contains(err.Error(), "invalid daemon.server_url") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

func TestLoadReadError(t *testing.T) {
	path := t.TempDir()
	_, exists, err := Load(path)
	if err == nil {
		t.Fatalf("expected read error for directory path")
	}
	if exists {
		t.Fatalf("expected exists=false for read error")
	}
}
