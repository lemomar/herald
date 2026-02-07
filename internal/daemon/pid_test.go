package daemon

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func resetPIDHooks(t *testing.T) {
	t.Helper()
	origKillFn := killFn
	origNowFn := timeNowFn
	origSleepFn := timeSleep
	origDefaultDirFn := defaultDirFn
	origReadFileFn := pidReadFileFn
	origMkdirAllFn := pidMkdirAllFn
	origWriteFileFn := pidWriteFileFn
	origRemoveFn := pidRemoveFn
	t.Cleanup(func() {
		killFn = origKillFn
		timeNowFn = origNowFn
		timeSleep = origSleepFn
		defaultDirFn = origDefaultDirFn
		pidReadFileFn = origReadFileFn
		pidMkdirAllFn = origMkdirAllFn
		pidWriteFileFn = origWriteFileFn
		pidRemoveFn = origRemoveFn
	})
}

func TestEnsureSingleInstanceRemovesStalePID(t *testing.T) {
	resetPIDHooks(t)
	pidPath := filepath.Join(t.TempDir(), "daemon.pid")
	if err := os.WriteFile(pidPath, []byte("999999\n"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	if err := EnsureSingleInstance(pidPath); err != nil {
		t.Fatalf("ensure single instance failed: %v", err)
	}
	defer CleanupPID(pidPath)

	data, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("invalid pid file: %v", err)
	}
	if pid != os.Getpid() {
		t.Fatalf("expected pid %d, got %d", os.Getpid(), pid)
	}
}

func TestEnsureSingleInstanceFailsWhenAlreadyRunning(t *testing.T) {
	resetPIDHooks(t)
	pidPath := filepath.Join(t.TempDir(), "daemon.pid")
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	err := EnsureSingleInstance(pidPath)
	if err == nil {
		t.Fatalf("expected already running error")
	}
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("expected ErrAlreadyRunning, got %v", err)
	}
}

func TestEnsureSingleInstanceInvalidPIDFile(t *testing.T) {
	resetPIDHooks(t)
	pidPath := filepath.Join(t.TempDir(), "daemon.pid")
	if err := os.WriteFile(pidPath, []byte("not-a-pid"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	if err := EnsureSingleInstance(pidPath); err == nil {
		t.Fatalf("expected invalid pid error")
	}
}

func TestIsRunningWithMissingAndInvalidPID(t *testing.T) {
	resetPIDHooks(t)
	pidPath := filepath.Join(t.TempDir(), "daemon.pid")

	running, pid, err := IsRunning(pidPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if running || pid != 0 {
		t.Fatalf("expected not running, got running=%v pid=%d", running, pid)
	}

	if err := os.WriteFile(pidPath, []byte("abc\n"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if _, _, err := IsRunning(pidPath); err == nil {
		t.Fatalf("expected invalid pid error")
	}
}

func TestStopProcessStalePIDAndCleanup(t *testing.T) {
	resetPIDHooks(t)
	pidPath := filepath.Join(t.TempDir(), "daemon.pid")
	if err := os.WriteFile(pidPath, []byte("999999\n"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	if err := StopProcess(pidPath); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("expected pid file removed, got err=%v", err)
	}

	// CleanupPID should not fail when file does not exist.
	if err := CleanupPID(pidPath); err != nil {
		t.Fatalf("unexpected cleanup error: %v", err)
	}
}

func TestCleanupPIDErrorForNonEmptyDir(t *testing.T) {
	resetPIDHooks(t)
	pidPath := filepath.Join(t.TempDir(), "daemon.pid")
	if err := os.MkdirAll(pidPath, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pidPath, "file"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if err := CleanupPID(pidPath); err == nil {
		t.Fatalf("expected cleanup error for non-empty directory pid path")
	}
}

func TestProcessRunningHandlesNonPositivePID(t *testing.T) {
	resetPIDHooks(t)
	if processRunning(0) {
		t.Fatalf("expected pid 0 to be not running")
	}
}

func TestStopProcessRunningPID(t *testing.T) {
	resetPIDHooks(t)
	out, err := exec.Command("sh", "-c", "sleep 10 >/dev/null 2>&1 & echo $!").Output()
	if err != nil {
		t.Fatalf("failed to spawn sleep process: %v", err)
	}
	pidText := strings.TrimSpace(string(out))
	pid, err := strconv.Atoi(pidText)
	if err != nil {
		t.Fatalf("failed to parse pid %q: %v", pidText, err)
	}
	defer func() {
		_ = exec.Command("kill", "-9", strconv.Itoa(pid)).Run()
	}()

	pidPath := filepath.Join(t.TempDir(), "daemon.pid")
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(pid)+"\n"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	if err := StopProcess(pidPath); err != nil {
		t.Fatalf("unexpected stop error: %v", err)
	}
}

func TestStopProcessKillErrorAndTimeout(t *testing.T) {
	resetPIDHooks(t)
	pidPath := filepath.Join(t.TempDir(), "daemon.pid")
	if err := os.WriteFile(pidPath, []byte("1234\n"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	killFn = func(pid int, sig syscall.Signal) error {
		if sig == 0 {
			return nil
		}
		return errors.New("kill failed")
	}
	if err := StopProcess(pidPath); err == nil || !strings.Contains(err.Error(), "failed to signal daemon process") {
		t.Fatalf("expected kill error, got %v", err)
	}

	resetPIDHooks(t)
	if err := os.WriteFile(pidPath, []byte("1234\n"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	killFn = func(pid int, sig syscall.Signal) error {
		if sig == 0 {
			return nil
		}
		return nil
	}
	now := time.Now()
	timeNowFn = func() time.Time {
		now = now.Add(6 * time.Second)
		return now
	}
	timeSleep = func(time.Duration) {}
	if err := StopProcess(pidPath); err == nil || !strings.Contains(err.Error(), "did not exit in time") {
		t.Fatalf("expected timeout error, got %v", err)
	}
}

func TestResolvePIDPathExplicitAndDefault(t *testing.T) {
	resetPIDHooks(t)
	if path, err := ResolvePIDPath("/tmp/custom.pid"); err != nil || path != "/tmp/custom.pid" {
		t.Fatalf("unexpected explicit path result path=%q err=%v", path, err)
	}
	if path, err := ResolvePIDPath(""); err != nil || !strings.HasSuffix(path, "/.herald/daemon.pid") {
		t.Fatalf("unexpected default path result path=%q err=%v", path, err)
	}

	defaultDirFn = func() (string, error) { return "", errors.New("no home") }
	if _, err := ResolvePIDPath(""); err == nil {
		t.Fatalf("expected default dir error")
	}
}

func TestEnsureSingleInstanceStaleRemoveFailure(t *testing.T) {
	resetPIDHooks(t)
	if os.Geteuid() == 0 {
		t.Skip("permission test is unreliable as root")
	}
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "locked")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	pidPath := filepath.Join(dir, "daemon.pid")
	if err := os.WriteFile(pidPath, []byte("999999\n"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod failed: %v", err)
	}
	defer os.Chmod(dir, 0o755)
	if err := EnsureSingleInstance(pidPath); err == nil {
		t.Fatalf("expected stale pid remove failure")
	}
}

func TestPIDFunctionsResolvePathError(t *testing.T) {
	resetPIDHooks(t)
	defaultDirFn = func() (string, error) { return "", errors.New("no home") }

	if _, _, err := IsRunning(""); err == nil {
		t.Fatalf("expected IsRunning path resolution error")
	}
	if err := StopProcess(""); err == nil {
		t.Fatalf("expected StopProcess path resolution error")
	}
	if err := CleanupPID(""); err == nil {
		t.Fatalf("expected CleanupPID path resolution error")
	}

	if err := EnsureSingleInstance(""); err == nil {
		t.Fatalf("expected EnsureSingleInstance path resolution error")
	}
}

func TestEnsureSingleInstanceHookedMkdirAndWriteErrors(t *testing.T) {
	t.Run("mkdir error", func(t *testing.T) {
		resetPIDHooks(t)
		pidPath := filepath.Join(t.TempDir(), "daemon.pid")

		pidMkdirAllFn = func(path string, perm os.FileMode) error {
			return errors.New("mkdir fail")
		}
		if err := EnsureSingleInstance(pidPath); err == nil || !strings.Contains(err.Error(), "failed to create pid dir") {
			t.Fatalf("expected mkdir error, got %v", err)
		}
	})

	t.Run("write error", func(t *testing.T) {
		resetPIDHooks(t)
		pidPath := filepath.Join(t.TempDir(), "daemon.pid")

		pidWriteFileFn = func(name string, data []byte, perm os.FileMode) error {
			return errors.New("write fail")
		}
		if err := EnsureSingleInstance(pidPath); err == nil || !strings.Contains(err.Error(), "failed to write pid file") {
			t.Fatalf("expected write error, got %v", err)
		}
	})
}

func TestPIDReadAndRemoveHookedErrors(t *testing.T) {
	t.Run("read pid error", func(t *testing.T) {
		resetPIDHooks(t)
		pidPath := filepath.Join(t.TempDir(), "daemon.pid")

		pidReadFileFn = func(name string) ([]byte, error) {
			return nil, errors.New("read fail")
		}
		if _, _, err := IsRunning(pidPath); err == nil || !strings.Contains(err.Error(), "failed to read pid file") {
			t.Fatalf("expected read error from IsRunning, got %v", err)
		}
	})

	t.Run("stale remove error", func(t *testing.T) {
		resetPIDHooks(t)
		pidPath := filepath.Join(t.TempDir(), "daemon.pid")
		if err := os.WriteFile(pidPath, []byte("999999\n"), 0o644); err != nil {
			t.Fatalf("write failed: %v", err)
		}
		pidRemoveFn = func(name string) error { return errors.New("remove fail") }
		if err := EnsureSingleInstance(pidPath); err == nil || !strings.Contains(err.Error(), "failed to remove stale pid file") {
			t.Fatalf("expected stale remove error, got %v", err)
		}
	})

	t.Run("stop stale remove ignored", func(t *testing.T) {
		resetPIDHooks(t)
		pidPath := filepath.Join(t.TempDir(), "daemon.pid")
		if err := os.WriteFile(pidPath, []byte("123\n"), 0o644); err != nil {
			t.Fatalf("write failed: %v", err)
		}
		killFn = func(pid int, sig syscall.Signal) error {
			if sig == 0 {
				return errors.New("not running")
			}
			return nil
		}
		pidRemoveFn = func(name string) error { return errors.New("remove fail") }
		if err := StopProcess(pidPath); err != nil {
			t.Fatalf("expected remove error in stale stop path to be ignored, got %v", err)
		}
	})

	t.Run("cleanup remove error", func(t *testing.T) {
		resetPIDHooks(t)
		pidPath := filepath.Join(t.TempDir(), "daemon.pid")
		pidRemoveFn = func(name string) error { return errors.New("cleanup fail") }
		if err := CleanupPID(pidPath); err == nil || !strings.Contains(err.Error(), "cleanup fail") {
			t.Fatalf("expected cleanup remove error, got %v", err)
		}
	})
}

func TestIsRunningReturnsTrueForLivePID(t *testing.T) {
	resetPIDHooks(t)
	pidPath := filepath.Join(t.TempDir(), "daemon.pid")
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	running, pid, err := IsRunning(pidPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !running || pid != os.Getpid() {
		t.Fatalf("expected running pid %d, got running=%v pid=%d", os.Getpid(), running, pid)
	}
}
