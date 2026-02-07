package ctl

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"herald/internal/config"
	"herald/internal/daemon"
	"herald/internal/service"
)

type fakeService struct {
	statusSeq []statusResult
	startErr  error
	startMode string
	stopErr   error
	stopMode  string

	statusCalls int
	startCalls  int
	stopCalls   int
}

type statusResult struct {
	running bool
	mode    string
	err     error
}

func (f *fakeService) Start(executablePath, configPath string) (string, error) {
	f.startCalls++
	if f.startMode == "" {
		f.startMode = service.ModeSystemd
	}
	return f.startMode, f.startErr
}

func (f *fakeService) Stop() (string, error) {
	f.stopCalls++
	if f.stopMode == "" {
		f.stopMode = service.ModeSystemd
	}
	return f.stopMode, f.stopErr
}

func (f *fakeService) Status() (bool, string, error) {
	f.statusCalls++
	if len(f.statusSeq) == 0 {
		return false, "", nil
	}
	res := f.statusSeq[0]
	f.statusSeq = f.statusSeq[1:]
	return res.running, res.mode, res.err
}

type fakeRunner struct {
	runCalls int
	runErr   error
}

func (f *fakeRunner) Run(ctx context.Context) error {
	f.runCalls++
	return f.runErr
}

func resetHooks(t *testing.T) {
	t.Helper()

	origNewServiceManagerFn := newServiceManagerFn
	origLoadConfigFn := loadConfigFn
	origValidateDaemonFn := validateDaemonFn
	origOsExecutableFn := osExecutableFn
	origDefaultConfigPathFn := defaultConfigPathFn
	origReadConfigFn := readConfigFn
	origDaemonIsRunningFn := daemonIsRunningFn
	origDaemonStopFn := daemonStopFn
	origStartBackgroundFn := startBackgroundFn
	origNewRunnerFn := newRunnerFn
	origNotifyContextFn := notifyContextFn
	origOpenFileFn := openFileFn
	origExecCommandFn := execCommandFn
	origNowFn := nowFn
	origSleepFn := sleepFn
	origBackgroundWaitTimeout := backgroundWaitTimeout
	origBackgroundPollDelay := backgroundPollDelay

	t.Cleanup(func() {
		newServiceManagerFn = origNewServiceManagerFn
		loadConfigFn = origLoadConfigFn
		validateDaemonFn = origValidateDaemonFn
		osExecutableFn = origOsExecutableFn
		defaultConfigPathFn = origDefaultConfigPathFn
		readConfigFn = origReadConfigFn
		daemonIsRunningFn = origDaemonIsRunningFn
		daemonStopFn = origDaemonStopFn
		startBackgroundFn = origStartBackgroundFn
		newRunnerFn = origNewRunnerFn
		notifyContextFn = origNotifyContextFn
		openFileFn = origOpenFileFn
		execCommandFn = origExecCommandFn
		nowFn = origNowFn
		sleepFn = origSleepFn
		backgroundWaitTimeout = origBackgroundWaitTimeout
		backgroundPollDelay = origBackgroundPollDelay
	})
}

func defaultDaemonConfig() config.Config {
	return config.Config{Daemon: config.DaemonConfig{ServerURL: "ws://example.test/ws", ReconnectSec: 1}}
}

func TestStartCommand_ServiceAlreadyRunning(t *testing.T) {
	resetHooks(t)
	svc := &fakeService{statusSeq: []statusResult{{running: true, mode: service.ModeLaunchd}}}
	newServiceManagerFn = func() serviceController { return svc }
	loadConfigFn = func(string) (config.Config, string, error) { return defaultDaemonConfig(), "/tmp/cfg", nil }
	validateDaemonFn = func(config.DaemonConfig) error { return nil }

	cmd := newStartCmd(&rootOptions{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.String() != "running (launchd)\n" {
		t.Fatalf("unexpected output: %q", out.String())
	}
}

func TestNewRootCommandContainsExpectedCommands(t *testing.T) {
	root := NewRootCommand()
	if root.Use != "heraldctl" {
		t.Fatalf("unexpected root use: %q", root.Use)
	}
	names := map[string]bool{}
	for _, c := range root.Commands() {
		names[c.Name()] = true
	}
	for _, want := range []string{"start", "stop", "status", "restart", "logs", "daemon"} {
		if !names[want] {
			t.Fatalf("missing command %q", want)
		}
	}
}

func TestStartCommand_ServiceStartSuccess(t *testing.T) {
	resetHooks(t)
	svc := &fakeService{
		statusSeq: []statusResult{{running: false, mode: service.ModeSystemd}},
		startMode: service.ModeSystemd,
	}
	newServiceManagerFn = func() serviceController { return svc }
	loadConfigFn = func(string) (config.Config, string, error) { return defaultDaemonConfig(), "/tmp/cfg", nil }
	validateDaemonFn = func(config.DaemonConfig) error { return nil }
	osExecutableFn = func() (string, error) { return "/usr/bin/heraldctl", nil }

	cmd := newStartCmd(&rootOptions{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.String() != "started (systemd)\n" {
		t.Fatalf("unexpected output: %q", out.String())
	}
}

func TestStartCommand_FallbackBackgroundSuccess(t *testing.T) {
	resetHooks(t)
	svc := &fakeService{
		statusSeq: []statusResult{{running: false, mode: service.ModeSystemd}},
		startErr:  service.ErrUnavailable,
	}
	newServiceManagerFn = func() serviceController { return svc }
	loadConfigFn = func(string) (config.Config, string, error) { return defaultDaemonConfig(), "/tmp/cfg", nil }
	validateDaemonFn = func(config.DaemonConfig) error { return nil }
	osExecutableFn = func() (string, error) { return "/usr/bin/heraldctl", nil }
	daemonIsRunningFn = func(string) (bool, int, error) { return false, 0, nil }
	called := false
	startBackgroundFn = func(exe, cfg string) error {
		called = true
		if exe != "/usr/bin/heraldctl" || cfg != "/tmp/cfg" {
			return fmt.Errorf("unexpected args exe=%q cfg=%q", exe, cfg)
		}
		return nil
	}

	cmd := newStartCmd(&rootOptions{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatalf("expected background start")
	}
	if out.String() != "started (pid)\n" {
		t.Fatalf("unexpected output: %q", out.String())
	}
}

func TestStartCommand_FallbackAlreadyRunningPID(t *testing.T) {
	resetHooks(t)
	svc := &fakeService{statusSeq: []statusResult{{running: false}}, startErr: service.ErrUnavailable}
	newServiceManagerFn = func() serviceController { return svc }
	loadConfigFn = func(string) (config.Config, string, error) { return defaultDaemonConfig(), "/tmp/cfg", nil }
	validateDaemonFn = func(config.DaemonConfig) error { return nil }
	osExecutableFn = func() (string, error) { return "/usr/bin/heraldctl", nil }
	daemonIsRunningFn = func(string) (bool, int, error) { return true, 123, nil }

	cmd := newStartCmd(&rootOptions{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.String() != "running (pid)\n" {
		t.Fatalf("unexpected output: %q", out.String())
	}
}

func TestStartCommand_ErrorBranches(t *testing.T) {
	cases := []struct {
		name    string
		setup   func()
		errLike string
	}{
		{
			name: "load config fails",
			setup: func() {
				loadConfigFn = func(string) (config.Config, string, error) { return config.Config{}, "", errors.New("load failed") }
			},
			errLike: "load failed",
		},
		{
			name: "validate fails",
			setup: func() {
				loadConfigFn = func(string) (config.Config, string, error) { return defaultDaemonConfig(), "/tmp/cfg", nil }
				validateDaemonFn = func(config.DaemonConfig) error { return errors.New("bad daemon") }
			},
			errLike: "bad daemon",
		},
		{
			name: "resolve executable fails",
			setup: func() {
				svc := &fakeService{statusSeq: []statusResult{{running: false}}}
				newServiceManagerFn = func() serviceController { return svc }
				loadConfigFn = func(string) (config.Config, string, error) { return defaultDaemonConfig(), "/tmp/cfg", nil }
				validateDaemonFn = func(config.DaemonConfig) error { return nil }
				osExecutableFn = func() (string, error) { return "", errors.New("no executable") }
			},
			errLike: "failed to resolve executable path",
		},
		{
			name: "service status returns non-unavailable error",
			setup: func() {
				svc := &fakeService{statusSeq: []statusResult{{running: false, err: errors.New("status failed")}}}
				newServiceManagerFn = func() serviceController { return svc }
				loadConfigFn = func(string) (config.Config, string, error) { return defaultDaemonConfig(), "/tmp/cfg", nil }
				validateDaemonFn = func(config.DaemonConfig) error { return nil }
			},
			errLike: "status failed",
		},
		{
			name: "service start returns non-unavailable error",
			setup: func() {
				svc := &fakeService{statusSeq: []statusResult{{running: false}}, startErr: errors.New("start failed")}
				newServiceManagerFn = func() serviceController { return svc }
				loadConfigFn = func(string) (config.Config, string, error) { return defaultDaemonConfig(), "/tmp/cfg", nil }
				validateDaemonFn = func(config.DaemonConfig) error { return nil }
				osExecutableFn = func() (string, error) { return "/usr/bin/heraldctl", nil }
			},
			errLike: "start failed",
		},
		{
			name: "fallback pid lookup fails",
			setup: func() {
				svc := &fakeService{statusSeq: []statusResult{{running: false}}, startErr: service.ErrUnavailable}
				newServiceManagerFn = func() serviceController { return svc }
				loadConfigFn = func(string) (config.Config, string, error) { return defaultDaemonConfig(), "/tmp/cfg", nil }
				validateDaemonFn = func(config.DaemonConfig) error { return nil }
				osExecutableFn = func() (string, error) { return "/usr/bin/heraldctl", nil }
				daemonIsRunningFn = func(string) (bool, int, error) { return false, 0, errors.New("pid lookup failed") }
			},
			errLike: "pid lookup failed",
		},
		{
			name: "fallback background start fails",
			setup: func() {
				svc := &fakeService{statusSeq: []statusResult{{running: false}}, startErr: service.ErrUnavailable}
				newServiceManagerFn = func() serviceController { return svc }
				loadConfigFn = func(string) (config.Config, string, error) { return defaultDaemonConfig(), "/tmp/cfg", nil }
				validateDaemonFn = func(config.DaemonConfig) error { return nil }
				osExecutableFn = func() (string, error) { return "/usr/bin/heraldctl", nil }
				daemonIsRunningFn = func(string) (bool, int, error) { return false, 0, nil }
				startBackgroundFn = func(string, string) error { return errors.New("spawn failed") }
			},
			errLike: "spawn failed",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetHooks(t)
			tc.setup()
			cmd := newStartCmd(&rootOptions{})
			err := cmd.RunE(cmd, nil)
			if err == nil || !strings.Contains(err.Error(), tc.errLike) {
				t.Fatalf("expected error containing %q, got %v", tc.errLike, err)
			}
		})
	}
}

func TestStopCommand_CoversBranches(t *testing.T) {
	resetHooks(t)

	t.Run("service mode stop", func(t *testing.T) {
		resetHooks(t)
		svc := &fakeService{statusSeq: []statusResult{{running: true, mode: service.ModeLaunchd}}, stopMode: service.ModeLaunchd}
		newServiceManagerFn = func() serviceController { return svc }

		cmd := newStopCmd(&rootOptions{})
		var out bytes.Buffer
		cmd.SetOut(&out)
		if err := cmd.RunE(cmd, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out.String() != "stopped (launchd)\n" {
			t.Fatalf("unexpected output: %q", out.String())
		}
	})

	t.Run("service stop error", func(t *testing.T) {
		resetHooks(t)
		svc := &fakeService{statusSeq: []statusResult{{running: true, mode: service.ModeLaunchd}}, stopErr: errors.New("stop failed")}
		newServiceManagerFn = func() serviceController { return svc }
		cmd := newStopCmd(&rootOptions{})
		err := cmd.RunE(cmd, nil)
		if err == nil || !strings.Contains(err.Error(), "stop failed") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("pid fallback stopped", func(t *testing.T) {
		resetHooks(t)
		svc := &fakeService{statusSeq: []statusResult{{running: false, mode: service.ModeSystemd}}}
		newServiceManagerFn = func() serviceController { return svc }
		daemonIsRunningFn = func(string) (bool, int, error) { return false, 0, nil }
		cmd := newStopCmd(&rootOptions{})
		var out bytes.Buffer
		cmd.SetOut(&out)
		if err := cmd.RunE(cmd, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out.String() != "stopped (systemd)\n" {
			t.Fatalf("unexpected output: %q", out.String())
		}
	})

	t.Run("pid fallback running", func(t *testing.T) {
		resetHooks(t)
		svc := &fakeService{statusSeq: []statusResult{{running: false, err: service.ErrUnavailable}}}
		newServiceManagerFn = func() serviceController { return svc }
		daemonIsRunningFn = func(string) (bool, int, error) { return true, 88, nil }
		daemonStopFn = func(string) error { return nil }
		cmd := newStopCmd(&rootOptions{})
		var out bytes.Buffer
		cmd.SetOut(&out)
		if err := cmd.RunE(cmd, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out.String() != "stopped (pid:88)\n" {
			t.Fatalf("unexpected output: %q", out.String())
		}
	})

	t.Run("pid fallback errors", func(t *testing.T) {
		resetHooks(t)
		svc := &fakeService{statusSeq: []statusResult{{running: false, err: service.ErrUnavailable}}}
		newServiceManagerFn = func() serviceController { return svc }
		daemonIsRunningFn = func(string) (bool, int, error) { return false, 0, errors.New("pid fail") }
		cmd := newStopCmd(&rootOptions{})
		if err := cmd.RunE(cmd, nil); err == nil || !strings.Contains(err.Error(), "pid fail") {
			t.Fatalf("unexpected error: %v", err)
		}

		resetHooks(t)
		svc2 := &fakeService{statusSeq: []statusResult{{running: false, err: service.ErrUnavailable}}}
		newServiceManagerFn = func() serviceController { return svc2 }
		daemonIsRunningFn = func(string) (bool, int, error) { return true, 90, nil }
		daemonStopFn = func(string) error { return errors.New("kill fail") }
		cmd = newStopCmd(&rootOptions{})
		if err := cmd.RunE(cmd, nil); err == nil || !strings.Contains(err.Error(), "kill fail") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("status error is not masked", func(t *testing.T) {
		resetHooks(t)
		svc := &fakeService{statusSeq: []statusResult{{running: false, err: errors.New("status failed")}}}
		newServiceManagerFn = func() serviceController { return svc }
		cmd := newStopCmd(&rootOptions{})
		if err := cmd.RunE(cmd, nil); err == nil || !strings.Contains(err.Error(), "status failed") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("pid fallback stopped when service unavailable", func(t *testing.T) {
		resetHooks(t)
		svc := &fakeService{statusSeq: []statusResult{{running: false, err: service.ErrUnavailable}}}
		newServiceManagerFn = func() serviceController { return svc }
		daemonIsRunningFn = func(string) (bool, int, error) { return false, 0, nil }
		cmd := newStopCmd(&rootOptions{})
		var out bytes.Buffer
		cmd.SetOut(&out)
		if err := cmd.RunE(cmd, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out.String() != "stopped\n" {
			t.Fatalf("unexpected output: %q", out.String())
		}
	})
}

func TestStatusCommand_CoversBranches(t *testing.T) {
	resetHooks(t)

	t.Run("service running/stopped", func(t *testing.T) {
		resetHooks(t)
		svc := &fakeService{statusSeq: []statusResult{{running: true, mode: service.ModeSystemd}, {running: false, mode: service.ModeSystemd}}}
		newServiceManagerFn = func() serviceController { return svc }
		cmd := newStatusCmd(&rootOptions{})
		var out bytes.Buffer
		cmd.SetOut(&out)
		if err := cmd.RunE(cmd, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out.String() != "running (systemd)\n" {
			t.Fatalf("unexpected output: %q", out.String())
		}

		cmd = newStatusCmd(&rootOptions{})
		out.Reset()
		cmd.SetOut(&out)
		if err := cmd.RunE(cmd, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out.String() != "stopped (systemd)\n" {
			t.Fatalf("unexpected output: %q", out.String())
		}
	})

	t.Run("service status non-unavailable error", func(t *testing.T) {
		resetHooks(t)
		svc := &fakeService{statusSeq: []statusResult{{running: false, err: errors.New("status fail")}}}
		newServiceManagerFn = func() serviceController { return svc }
		cmd := newStatusCmd(&rootOptions{})
		if err := cmd.RunE(cmd, nil); err == nil || !strings.Contains(err.Error(), "status fail") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("fallback branches", func(t *testing.T) {
		resetHooks(t)
		svc := &fakeService{statusSeq: []statusResult{{running: false, err: service.ErrUnavailable}}}
		newServiceManagerFn = func() serviceController { return svc }
		daemonIsRunningFn = func(string) (bool, int, error) { return true, 44, nil }
		cmd := newStatusCmd(&rootOptions{})
		var out bytes.Buffer
		cmd.SetOut(&out)
		if err := cmd.RunE(cmd, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out.String() != "running (pid:44)\n" {
			t.Fatalf("unexpected output: %q", out.String())
		}

		resetHooks(t)
		svc = &fakeService{statusSeq: []statusResult{{running: false, err: service.ErrUnavailable}}}
		newServiceManagerFn = func() serviceController { return svc }
		daemonIsRunningFn = func(string) (bool, int, error) { return false, 0, nil }
		cmd = newStatusCmd(&rootOptions{})
		out.Reset()
		cmd.SetOut(&out)
		if err := cmd.RunE(cmd, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out.String() != "stopped\n" {
			t.Fatalf("unexpected output: %q", out.String())
		}

		resetHooks(t)
		svc = &fakeService{statusSeq: []statusResult{{running: false, err: service.ErrUnavailable}}}
		newServiceManagerFn = func() serviceController { return svc }
		daemonIsRunningFn = func(string) (bool, int, error) { return false, 0, errors.New("pid fail") }
		cmd = newStatusCmd(&rootOptions{})
		if err := cmd.RunE(cmd, nil); err == nil || !strings.Contains(err.Error(), "pid fail") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestRestartCommand_StopThenStartAndStopError(t *testing.T) {
	resetHooks(t)
	svc := &fakeService{
		statusSeq: []statusResult{{running: false}, {running: false}},
		startMode: service.ModeSystemd,
	}
	newServiceManagerFn = func() serviceController { return svc }
	loadConfigFn = func(string) (config.Config, string, error) { return defaultDaemonConfig(), "/tmp/cfg", nil }
	validateDaemonFn = func(config.DaemonConfig) error { return nil }
	osExecutableFn = func() (string, error) { return "/usr/bin/heraldctl", nil }
	daemonIsRunningFn = func(string) (bool, int, error) { return false, 0, nil }

	cmd := newRestartCmd(&rootOptions{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "stopped") || !strings.Contains(out.String(), "started") {
		t.Fatalf("expected stop+start output, got %q", out.String())
	}

	resetHooks(t)
	svc = &fakeService{statusSeq: []statusResult{{running: true, mode: service.ModeSystemd}}, stopErr: errors.New("stop failed")}
	newServiceManagerFn = func() serviceController { return svc }
	cmd = newRestartCmd(&rootOptions{})
	if err := cmd.RunE(cmd, nil); err == nil || !strings.Contains(err.Error(), "stop failed") {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc.startCalls != 0 {
		t.Fatalf("did not expect start after stop failure")
	}
}

func TestLogsCommand_Run(t *testing.T) {
	resetHooks(t)
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	logPath := filepath.Join(tmp, ".herald", "logs.yaml")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(logPath, []byte("- timestamp: 2026-02-06T23:40:12Z\n  source: daemon\n  event: notify\n  title: Build\n  message: Done\n  level: info\n"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	cmd := newLogsCmd()
	if err := cmd.Flags().Set("filter", "done"); err != nil {
		t.Fatalf("set filter failed: %v", err)
	}
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "Build: Done") {
		t.Fatalf("unexpected output: %q", out.String())
	}
}

func TestDaemonRunCommand_CoversBranches(t *testing.T) {
	resetHooks(t)
	runner := &fakeRunner{}
	loadConfigFn = func(string) (config.Config, string, error) { return defaultDaemonConfig(), "/tmp/cfg", nil }
	validateDaemonFn = func(config.DaemonConfig) error { return nil }
	newRunnerFn = func(opts daemon.Options) (daemonRunner, error) { return runner, nil }
	notifyContextFn = func(parent context.Context, _ ...os.Signal) (context.Context, context.CancelFunc) {
		return context.WithCancel(parent)
	}

	daemonCmd := newDaemonCmd(&rootOptions{})
	runCmd := daemonCmd.Commands()[0]
	if err := runCmd.RunE(runCmd, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if runner.runCalls != 1 {
		t.Fatalf("expected runner called once, got %d", runner.runCalls)
	}

	resetHooks(t)
	loadConfigFn = func(string) (config.Config, string, error) { return config.Config{}, "", errors.New("load fail") }
	runCmd = newDaemonCmd(&rootOptions{}).Commands()[0]
	if err := runCmd.RunE(runCmd, nil); err == nil || !strings.Contains(err.Error(), "load fail") {
		t.Fatalf("unexpected error: %v", err)
	}

	resetHooks(t)
	loadConfigFn = func(string) (config.Config, string, error) { return defaultDaemonConfig(), "/tmp/cfg", nil }
	validateDaemonFn = func(config.DaemonConfig) error { return errors.New("bad daemon") }
	runCmd = newDaemonCmd(&rootOptions{}).Commands()[0]
	if err := runCmd.RunE(runCmd, nil); err == nil || !strings.Contains(err.Error(), "bad daemon") {
		t.Fatalf("unexpected error: %v", err)
	}

	resetHooks(t)
	loadConfigFn = func(string) (config.Config, string, error) { return defaultDaemonConfig(), "/tmp/cfg", nil }
	validateDaemonFn = func(config.DaemonConfig) error { return nil }
	newRunnerFn = func(opts daemon.Options) (daemonRunner, error) { return nil, errors.New("runner create fail") }
	runCmd = newDaemonCmd(&rootOptions{}).Commands()[0]
	if err := runCmd.RunE(runCmd, nil); err == nil || !strings.Contains(err.Error(), "runner create fail") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfig_ExplicitAndDefault(t *testing.T) {
	tmp := t.TempDir()

	explicitPath := filepath.Join(tmp, "explicit.yaml")
	if err := os.WriteFile(explicitPath, []byte("mode: greeting\n"), 0o644); err != nil {
		t.Fatalf("write explicit failed: %v", err)
	}
	_, gotPath, err := loadConfig(explicitPath)
	if err != nil {
		t.Fatalf("unexpected explicit error: %v", err)
	}
	if gotPath != explicitPath {
		t.Fatalf("unexpected explicit path: %q", gotPath)
	}

	t.Setenv("HOME", tmp)
	defaultPath := filepath.Join(tmp, ".heraldrc")
	if err := os.WriteFile(defaultPath, []byte("mode: greeting\n"), 0o644); err != nil {
		t.Fatalf("write default failed: %v", err)
	}
	_, gotPath, err = loadConfig("")
	if err != nil {
		t.Fatalf("unexpected default error: %v", err)
	}
	if gotPath != defaultPath {
		t.Fatalf("unexpected default path: %q", gotPath)
	}
}

func TestLoadConfigErrorBranchesWithHooks(t *testing.T) {
	resetHooks(t)
	defaultConfigPathFn = func() (string, error) { return "", errors.New("no default config") }
	if _, _, err := loadConfig(""); err == nil || !strings.Contains(err.Error(), "no default config") {
		t.Fatalf("unexpected error: %v", err)
	}

	resetHooks(t)
	readConfigFn = func(string) (config.Config, bool, error) {
		return config.Config{}, false, errors.New("read config failed")
	}
	if _, _, err := loadConfig("/tmp/custom.yaml"); err == nil || !strings.Contains(err.Error(), "read config failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStartBackgroundProcess_CoversBranches(t *testing.T) {
	resetHooks(t)

	t.Run("open devnull fails", func(t *testing.T) {
		resetHooks(t)
		openFileFn = func(string, int, os.FileMode) (*os.File, error) {
			return nil, errors.New("open failed")
		}
		err := startBackgroundProcess("true", "")
		if err == nil || !strings.Contains(err.Error(), "open failed") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("start command fails", func(t *testing.T) {
		resetHooks(t)
		err := startBackgroundProcess("/path/does/not/exist", "")
		if err == nil || !strings.Contains(err.Error(), "failed to start daemon process") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("success when pid observed", func(t *testing.T) {
		resetHooks(t)
		daemonIsRunningFn = func(string) (bool, int, error) { return true, 1, nil }
		if err := startBackgroundProcess("true", ""); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("timeout when pid never appears", func(t *testing.T) {
		resetHooks(t)
		daemonIsRunningFn = func(string) (bool, int, error) { return false, 0, nil }
		backgroundWaitTimeout = 5 * time.Millisecond
		backgroundPollDelay = 1 * time.Millisecond
		now := time.Now()
		nowFn = func() time.Time {
			now = now.Add(3 * time.Millisecond)
			return now
		}
		sleepFn = func(time.Duration) {}
		err := startBackgroundProcess("true", "")
		if err == nil || !strings.Contains(err.Error(), "did not create pid file") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("includes config arg when provided", func(t *testing.T) {
		resetHooks(t)
		var gotName string
		var gotArgs []string
		execCommandFn = func(name string, args ...string) *exec.Cmd {
			gotName = name
			gotArgs = append([]string(nil), args...)
			return exec.Command("true")
		}
		daemonIsRunningFn = func(string) (bool, int, error) { return true, 1, nil }
		if err := startBackgroundProcess("/usr/bin/heraldctl", "/tmp/herald.yaml"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if gotName != "/usr/bin/heraldctl" {
			t.Fatalf("unexpected executable: %q", gotName)
		}
		if strings.Join(gotArgs, " ") != "--config /tmp/herald.yaml daemon run" {
			t.Fatalf("unexpected args: %q", strings.Join(gotArgs, " "))
		}
	})
}

func TestDaemonRunUsesSignalSet(t *testing.T) {
	resetHooks(t)
	runner := &fakeRunner{}
	loadConfigFn = func(string) (config.Config, string, error) { return defaultDaemonConfig(), "/tmp/cfg", nil }
	validateDaemonFn = func(config.DaemonConfig) error { return nil }
	newRunnerFn = func(opts daemon.Options) (daemonRunner, error) { return runner, nil }

	captured := []os.Signal{}
	notifyContextFn = func(parent context.Context, sig ...os.Signal) (context.Context, context.CancelFunc) {
		captured = append(captured, sig...)
		return context.Background(), func() {}
	}

	runCmd := newDaemonCmd(&rootOptions{}).Commands()[0]
	if err := runCmd.RunE(runCmd, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(captured) != 2 || captured[0] != os.Interrupt || captured[1] != syscall.SIGTERM {
		t.Fatalf("unexpected signal set: %+v", captured)
	}
}

func TestDefaultFactoryHooks(t *testing.T) {
	resetHooks(t)
	if svc := newServiceManagerFn(); svc == nil {
		t.Fatalf("expected default service manager")
	}
	runner, err := newRunnerFn(daemon.Options{DaemonConfig: defaultDaemonConfig().Daemon})
	if err != nil {
		t.Fatalf("expected default runner creation, got %v", err)
	}
	if runner == nil {
		t.Fatalf("expected non-nil runner")
	}
}
