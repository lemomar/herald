package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"herald/internal/logs"
)

var ErrAlreadyRunning = errors.New("daemon is already running")

var (
	killFn         = syscall.Kill
	timeNowFn      = time.Now
	timeSleep      = time.Sleep
	defaultDirFn   = logs.DefaultDir
	pidReadFileFn  = os.ReadFile
	pidMkdirAllFn  = os.MkdirAll
	pidWriteFileFn = os.WriteFile
	pidRemoveFn    = os.Remove
)

func ResolvePIDPath(path string) (string, error) {
	if path != "" {
		return path, nil
	}
	dir, err := defaultDirFn()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "daemon.pid"), nil
}

func EnsureSingleInstance(path string) error {
	resolved, err := ResolvePIDPath(path)
	if err != nil {
		return err
	}

	existingPID, err := readPID(resolved)
	if err != nil {
		return err
	}
	if existingPID > 0 {
		if processRunning(existingPID) {
			return fmt.Errorf("%w (pid=%d)", ErrAlreadyRunning, existingPID)
		}
		if err := pidRemoveFn(resolved); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove stale pid file: %w", err)
		}
	}

	if err := pidMkdirAllFn(filepath.Dir(resolved), 0o755); err != nil {
		return fmt.Errorf("failed to create pid dir: %w", err)
	}
	if err := pidWriteFileFn(resolved, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
		return fmt.Errorf("failed to write pid file: %w", err)
	}
	return nil
}

func IsRunning(path string) (bool, int, error) {
	resolved, err := ResolvePIDPath(path)
	if err != nil {
		return false, 0, err
	}
	pid, err := readPID(resolved)
	if err != nil {
		return false, 0, err
	}
	if pid <= 0 {
		return false, 0, nil
	}
	return processRunning(pid), pid, nil
}

func StopProcess(path string) error {
	resolved, err := ResolvePIDPath(path)
	if err != nil {
		return err
	}
	pid, err := readPID(resolved)
	if err != nil {
		return err
	}
	if pid <= 0 {
		return nil
	}

	if !processRunning(pid) {
		_ = pidRemoveFn(resolved)
		return nil
	}
	if err := killFn(pid, syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to signal daemon process %d: %w", pid, err)
	}

	deadline := timeNowFn().Add(5 * time.Second)
	for timeNowFn().Before(deadline) {
		if !processRunning(pid) {
			_ = pidRemoveFn(resolved)
			return nil
		}
		timeSleep(100 * time.Millisecond)
	}
	return fmt.Errorf("daemon process %d did not exit in time", pid)
}

func CleanupPID(path string) error {
	resolved, err := ResolvePIDPath(path)
	if err != nil {
		return err
	}
	if err := pidRemoveFn(resolved); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func readPID(path string) (int, error) {
	data, err := pidReadFileFn(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to read pid file %s: %w", path, err)
	}
	text := strings.TrimSpace(string(data))
	if text == "" {
		return 0, nil
	}
	pid, err := strconv.Atoi(text)
	if err != nil {
		return 0, fmt.Errorf("invalid pid file %s: %w", path, err)
	}
	return pid, nil
}

func processRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := killFn(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
