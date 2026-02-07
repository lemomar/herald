package logs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fakeTempFile struct {
	name     string
	writeErr error
	closeErr error
}

func (f *fakeTempFile) Name() string { return f.name }
func (f *fakeTempFile) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return len(p), nil
}
func (f *fakeTempFile) Close() error { return f.closeErr }

func resetStoreHooks(t *testing.T) {
	t.Helper()
	origUserHomeDirFn := userHomeDirFn
	origReadFileFn := readFileFn
	origMkdirAllFn := mkdirAllFn
	origCreateTempFn := createTempFn
	origRenameFn := renameFn
	origRemoveFn := removeFn
	t.Cleanup(func() {
		userHomeDirFn = origUserHomeDirFn
		readFileFn = origReadFileFn
		mkdirAllFn = origMkdirAllFn
		createTempFn = origCreateTempFn
		renameFn = origRenameFn
		removeFn = origRemoveFn
	})
}

func TestStoreAppendReadRoundTrip(t *testing.T) {
	resetStoreHooks(t)
	path := filepath.Join(t.TempDir(), "logs.yaml")
	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}

	ts := time.Date(2026, 2, 6, 23, 40, 12, 123456000, time.UTC)
	if err := store.Append(Entry{
		Timestamp: ts,
		Source:    "daemon",
		Event:     "notify",
		Title:     "Build",
		Message:   "Done",
		Level:     "info",
	}); err != nil {
		t.Fatalf("append failed: %v", err)
	}

	entries, err := store.ReadAll()
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Title != "Build" {
		t.Fatalf("unexpected title: %q", entries[0].Title)
	}
	if entries[0].Timestamp.Format(time.RFC3339Nano) != ts.Format(time.RFC3339Nano) {
		t.Fatalf("unexpected timestamp: %s", entries[0].Timestamp.Format(time.RFC3339Nano))
	}
}

func TestStoreQueryLast(t *testing.T) {
	resetStoreHooks(t)
	path := filepath.Join(t.TempDir(), "logs.yaml")
	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}

	for i := 1; i <= 3; i++ {
		if err := store.Append(Entry{
			Title:   "T",
			Message: fmt.Sprintf("m%d", i),
			Event:   "notify",
			Source:  "daemon",
			Level:   "info",
		}); err != nil {
			t.Fatalf("append failed: %v", err)
		}
	}

	entries, err := store.Query(2, "")
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Message != "m2" || entries[1].Message != "m3" {
		t.Fatalf("expected tail entries m2,m3, got %q,%q", entries[0].Message, entries[1].Message)
	}
}

func TestStoreQueryFilterCaseInsensitive(t *testing.T) {
	resetStoreHooks(t)
	path := filepath.Join(t.TempDir(), "logs.yaml")
	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}
	if err := store.Append(Entry{Source: "daemon", Event: "notify", Title: "Build", Message: "Task done", Level: "info"}); err != nil {
		t.Fatalf("append failed: %v", err)
	}
	if err := store.Append(Entry{Source: "daemon", Event: "connect", Title: "Socket", Message: "Connected", Level: "info"}); err != nil {
		t.Fatalf("append failed: %v", err)
	}

	entries, err := store.Query(0, "TASK")
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Title != "Build" {
		t.Fatalf("unexpected title: %q", entries[0].Title)
	}
}

func TestStoreReadMissingFile(t *testing.T) {
	resetStoreHooks(t)
	path := filepath.Join(t.TempDir(), "logs.yaml")
	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}

	entries, err := store.ReadAll()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no entries, got %d", len(entries))
	}
}

func TestStoreReadInvalidYAML(t *testing.T) {
	resetStoreHooks(t)
	path := filepath.Join(t.TempDir(), "logs.yaml")
	if err := os.WriteFile(path, []byte("["), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}

	if _, err := store.ReadAll(); err == nil {
		t.Fatalf("expected yaml parse error")
	}
}

func TestStoreReadEmptyFile(t *testing.T) {
	resetStoreHooks(t)
	path := filepath.Join(t.TempDir(), "logs.yaml")
	if err := os.WriteFile(path, []byte("   \n"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}

	entries, err := store.ReadAll()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no entries, got %d", len(entries))
	}
}

func TestStoreQueryMatchesEventField(t *testing.T) {
	resetStoreHooks(t)
	path := filepath.Join(t.TempDir(), "logs.yaml")
	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}
	if err := store.Append(Entry{Source: "daemon", Event: "ws_error", Title: "Socket", Message: "boom", Level: "error"}); err != nil {
		t.Fatalf("append failed: %v", err)
	}

	entries, err := store.Query(0, "WS_ERROR")
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one entry, got %d", len(entries))
	}
}

func TestStoreDefaultPathUsesHome(t *testing.T) {
	resetStoreHooks(t)
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	path, err := DefaultPath()
	if err != nil {
		t.Fatalf("default path failed: %v", err)
	}
	want := filepath.Join(tmp, ".herald", "logs.yaml")
	if path != want {
		t.Fatalf("expected %q, got %q", want, path)
	}
}

func TestNewStoreDefaultAndPathAccessor(t *testing.T) {
	resetStoreHooks(t)
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	store, err := NewStore("")
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}
	want := filepath.Join(tmp, ".herald", "logs.yaml")
	if store.Path() != want {
		t.Fatalf("expected %q, got %q", want, store.Path())
	}
}

func TestDefaultPathErrorBranches(t *testing.T) {
	resetStoreHooks(t)
	userHomeDirFn = func() (string, error) {
		return "", errors.New("no home")
	}

	if _, err := DefaultDir(); err == nil {
		t.Fatalf("expected default dir error")
	}
	if _, err := DefaultPath(); err == nil {
		t.Fatalf("expected default path error")
	}
	if _, err := NewStore(""); err == nil {
		t.Fatalf("expected new store default-path error")
	}
}

func TestStoreWriteAllErrorBranches(t *testing.T) {
	resetStoreHooks(t)
	t.Run("mkdir error", func(t *testing.T) {
		tmp := t.TempDir()
		base := filepath.Join(tmp, "base-file")
		if err := os.WriteFile(base, []byte("x"), 0o644); err != nil {
			t.Fatalf("write failed: %v", err)
		}
		path := filepath.Join(base, "logs.yaml")
		store, err := NewStore(path)
		if err != nil {
			t.Fatalf("new store failed: %v", err)
		}
		if err := store.Append(Entry{Message: "x"}); err == nil {
			t.Fatalf("expected mkdir error")
		}
	})

	t.Run("rename error target is directory", func(t *testing.T) {
		dirPath := filepath.Join(t.TempDir(), "logs.yaml")
		if err := os.MkdirAll(dirPath, 0o755); err != nil {
			t.Fatalf("mkdir failed: %v", err)
		}
		store, err := NewStore(dirPath)
		if err != nil {
			t.Fatalf("new store failed: %v", err)
		}
		if err := store.Append(Entry{Message: "x"}); err == nil {
			t.Fatalf("expected rename error")
		}
	})

	t.Run("create temp failure", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("permission test is unreliable as root")
		}
		tmp := t.TempDir()
		locked := filepath.Join(tmp, "locked")
		if err := os.MkdirAll(locked, 0o500); err != nil {
			t.Fatalf("mkdir failed: %v", err)
		}
		path := filepath.Join(locked, "logs.yaml")
		store, err := NewStore(path)
		if err != nil {
			t.Fatalf("new store failed: %v", err)
		}
		if err := store.Append(Entry{Message: "x"}); err == nil {
			t.Fatalf("expected create temp failure")
		}
	})
}

func TestStoreReadFileErrorForDirectoryPath(t *testing.T) {
	resetStoreHooks(t)
	dirPath := filepath.Join(t.TempDir(), "logsdir")
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	store, err := NewStore(dirPath)
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}
	if _, err := store.ReadAll(); err == nil {
		t.Fatalf("expected read error for directory path")
	}
	if _, err := store.Query(0, "anything"); err == nil {
		t.Fatalf("expected query read error for directory path")
	}
}

func TestStoreWriteAllHookedErrorBranches(t *testing.T) {
	resetStoreHooks(t)
	store, err := NewStore(filepath.Join(t.TempDir(), "logs.yaml"))
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}

	createTempFn = func(dir, pattern string) (tempFile, error) {
		return nil, errors.New("create temp failed")
	}
	if err := store.Append(Entry{Message: "x"}); err == nil {
		t.Fatalf("expected create temp error")
	}

	resetStoreHooks(t)
	store, err = NewStore(filepath.Join(t.TempDir(), "logs.yaml"))
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}
	createTempFn = func(dir, pattern string) (tempFile, error) {
		return &fakeTempFile{name: filepath.Join(dir, "tmp-write"), writeErr: errors.New("write fail")}, nil
	}
	if err := store.Append(Entry{Message: "x"}); err == nil {
		t.Fatalf("expected write error")
	}

	resetStoreHooks(t)
	store, err = NewStore(filepath.Join(t.TempDir(), "logs.yaml"))
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}
	createTempFn = func(dir, pattern string) (tempFile, error) {
		return &fakeTempFile{name: filepath.Join(dir, "tmp-close"), closeErr: errors.New("close fail")}, nil
	}
	if err := store.Append(Entry{Message: "x"}); err == nil {
		t.Fatalf("expected close error")
	}

	resetStoreHooks(t)
	store, err = NewStore(filepath.Join(t.TempDir(), "logs.yaml"))
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}
	createTempFn = func(dir, pattern string) (tempFile, error) {
		return &fakeTempFile{name: filepath.Join(dir, "tmp-rename")}, nil
	}
	renameFn = func(oldpath, newpath string) error { return errors.New("rename fail") }
	if err := store.Append(Entry{Message: "x"}); err == nil {
		t.Fatalf("expected rename error")
	}
}
