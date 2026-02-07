package logcmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"herald/internal/logs"
)

func resetLogcmdHooks(t *testing.T) {
	t.Helper()
	origNewStoreFn := newStoreFn
	t.Cleanup(func() {
		newStoreFn = origNewStoreFn
	})
}

func TestRunFormatsAndFilters(t *testing.T) {
	resetLogcmdHooks(t)
	tmp := t.TempDir()
	path := filepath.Join(tmp, "logs.yaml")
	store, err := logs.NewStore(path)
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}
	if err := store.Append(logs.Entry{Timestamp: time.Date(2026, 2, 6, 23, 40, 12, 0, time.UTC), Source: "daemon", Event: "notify", Title: "Build", Message: "Done", Level: "info"}); err != nil {
		t.Fatalf("append failed: %v", err)
	}
	if err := store.Append(logs.Entry{Timestamp: time.Date(2026, 2, 6, 23, 41, 12, 0, time.UTC), Source: "daemon", Event: "notify", Title: "", Message: "  Trim me  ", Level: ""}); err != nil {
		t.Fatalf("append failed: %v", err)
	}

	var out bytes.Buffer
	if err := Run(&out, Options{Path: path, Last: 1}); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	line := strings.TrimSpace(out.String())
	if !strings.Contains(line, "[info] -: Trim me") {
		t.Fatalf("unexpected formatted output: %q", line)
	}

	out.Reset()
	if err := Run(&out, Options{Path: path, Filter: "done"}); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if !strings.Contains(out.String(), "Build: Done") || strings.Contains(out.String(), "Trim me") {
		t.Fatalf("unexpected filtered output: %q", out.String())
	}
}

func TestRunErrorBranches(t *testing.T) {
	resetLogcmdHooks(t)
	if err := Run(&bytes.Buffer{}, Options{Last: -1}); err == nil {
		t.Fatalf("expected negative --last error")
	}

	tmp := t.TempDir()
	path := filepath.Join(tmp, "logs.yaml")
	if err := os.WriteFile(path, []byte("["), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if err := Run(&bytes.Buffer{}, Options{Path: path}); err == nil {
		t.Fatalf("expected yaml parse error")
	}

	newStoreFn = func(path string) (*logs.Store, error) {
		return nil, errors.New("store init failed")
	}
	if err := Run(&bytes.Buffer{}, Options{Path: path}); err == nil || !strings.Contains(err.Error(), "store init failed") {
		t.Fatalf("expected store init failure, got %v", err)
	}
}
