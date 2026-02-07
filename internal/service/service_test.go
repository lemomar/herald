package service

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNewManagerDefaults(t *testing.T) {
	m := NewManager()
	if m == nil {
		t.Fatalf("expected manager")
	}
	if m.goos != runtime.GOOS {
		t.Fatalf("unexpected goos: %q", m.goos)
	}
	if m.run == nil || m.lookPath == nil || m.homeDir == nil {
		t.Fatalf("expected manager funcs to be initialized")
	}
	if _, err := m.run("echo", "ok"); err != nil {
		t.Fatalf("expected default runner to execute echo, got %v", err)
	}
}

func TestRenderLaunchdPlist(t *testing.T) {
	plist := RenderLaunchdPlist("/usr/local/bin/heraldctl", "/tmp/herald.yaml")
	checks := []string{
		"com.herald.daemon",
		"<string>/usr/local/bin/heraldctl</string>",
		"<string>--config</string>",
		"<string>/tmp/herald.yaml</string>",
		"<string>daemon</string>",
		"<string>run</string>",
	}
	for _, check := range checks {
		if !strings.Contains(plist, check) {
			t.Fatalf("plist missing %q", check)
		}
	}

	plistNoConfig := RenderLaunchdPlist("/usr/local/bin/heraldctl", "")
	if strings.Contains(plistNoConfig, "--config") {
		t.Fatalf("did not expect --config in plist without config path")
	}
}

func TestRenderSystemdUnit(t *testing.T) {
	unit := RenderSystemdUnit("/usr/local/bin/heraldctl", "/tmp/herald.yaml")
	if !strings.Contains(unit, "ExecStart=/usr/local/bin/heraldctl --config /tmp/herald.yaml daemon run") {
		t.Fatalf("unit had unexpected ExecStart: %s", unit)
	}

	unitNoConfig := RenderSystemdUnit("/usr/local/bin/heraldctl", "")
	if !strings.Contains(unitNoConfig, "ExecStart=/usr/local/bin/heraldctl daemon run") {
		t.Fatalf("unit had unexpected ExecStart without config: %s", unitNoConfig)
	}
}

func TestManagerStartStopStatusUnavailable(t *testing.T) {
	m := &Manager{goos: "darwin", lookPath: func(file string) (string, error) { return "", errors.New("missing") }}
	if _, err := m.Start("/bin/x", ""); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
	if _, err := m.Stop(); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
	if _, _, err := m.Status(); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}

	m = &Manager{goos: "plan9"}
	if _, err := m.Start("/bin/x", ""); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
	if _, err := m.Stop(); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
	if _, _, err := m.Status(); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
}

func TestStatusSystemdInactive(t *testing.T) {
	m := &Manager{
		goos: "linux",
		lookPath: func(file string) (string, error) {
			return "/bin/systemctl", nil
		},
		run: func(name string, args ...string) (string, error) {
			return "inactive", errors.New("exit status 3")
		},
	}

	running, mode, err := m.Status()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if mode != ModeSystemd {
		t.Fatalf("unexpected mode: %s", mode)
	}
	if running {
		t.Fatalf("expected inactive service")
	}
}

func TestStatusLaunchdRunning(t *testing.T) {
	m := &Manager{
		goos: "darwin",
		lookPath: func(file string) (string, error) {
			return "/bin/launchctl", nil
		},
		run: func(name string, args ...string) (string, error) {
			return "state = running", nil
		},
		uid: 501,
	}

	running, mode, err := m.Status()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if mode != ModeLaunchd {
		t.Fatalf("unexpected mode: %s", mode)
	}
	if !running {
		t.Fatalf("expected running service")
	}
}

func TestLaunchdStartStopAndErrors(t *testing.T) {
	home := t.TempDir()
	calls := []string{}
	m := &Manager{
		goos: "darwin",
		uid:  501,
		homeDir: func() (string, error) {
			return home, nil
		},
		lookPath: func(file string) (string, error) {
			return "/bin/launchctl", nil
		},
		run: func(name string, args ...string) (string, error) {
			calls = append(calls, strings.Join(append([]string{name}, args...), " "))
			if len(args) > 0 && args[0] == "bootout" {
				return "", nil
			}
			if len(args) > 0 && args[0] == "bootstrap" {
				return "", nil
			}
			if len(args) > 0 && args[0] == "kickstart" {
				return "", nil
			}
			return "", nil
		},
	}

	mode, err := m.Start("/usr/local/bin/heraldctl", "/tmp/herald.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mode != ModeLaunchd {
		t.Fatalf("unexpected mode: %s", mode)
	}
	if len(calls) < 3 {
		t.Fatalf("expected launchctl calls, got %v", calls)
	}
	plistPath := filepath.Join(home, "Library", "LaunchAgents", "com.herald.daemon.plist")
	if _, err := os.Stat(plistPath); err != nil {
		t.Fatalf("expected plist to be written: %v", err)
	}

	calls = nil
	mode, err = m.Stop()
	if err != nil {
		t.Fatalf("unexpected stop error: %v", err)
	}
	if mode != ModeLaunchd {
		t.Fatalf("unexpected stop mode: %s", mode)
	}
	if len(calls) == 0 || !strings.Contains(calls[0], "bootout") {
		t.Fatalf("expected bootout call, got %v", calls)
	}

	m.run = func(name string, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "bootstrap" {
			return "bootstrap fail", errors.New("boom")
		}
		return "", nil
	}
	if _, err := m.Start("/usr/local/bin/heraldctl", ""); err == nil || !strings.Contains(err.Error(), "bootstrap failed") {
		t.Fatalf("expected bootstrap error, got %v", err)
	}

	m.run = func(name string, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "kickstart" {
			return "kickstart fail", errors.New("boom")
		}
		return "", nil
	}
	if _, err := m.Start("/usr/local/bin/heraldctl", ""); err == nil || !strings.Contains(err.Error(), "kickstart failed") {
		t.Fatalf("expected kickstart error, got %v", err)
	}

	m.run = func(name string, args ...string) (string, error) {
		return "could not find service", errors.New("exit")
	}
	if _, err := m.Stop(); err != nil {
		t.Fatalf("expected no error for missing service, got %v", err)
	}

	m.run = func(name string, args ...string) (string, error) {
		return "unexpected", errors.New("exit")
	}
	if _, err := m.Stop(); err == nil || !strings.Contains(err.Error(), "bootout failed") {
		t.Fatalf("expected stop error, got %v", err)
	}

	m.run = func(name string, args ...string) (string, error) {
		return "not found", errors.New("exit")
	}
	running, mode, err := m.Status()
	if err != nil {
		t.Fatalf("expected no status error for missing service, got %v", err)
	}
	if running || mode != ModeLaunchd {
		t.Fatalf("expected not running launchd, got running=%v mode=%s", running, mode)
	}

	m.run = func(name string, args ...string) (string, error) {
		return "unexpected error", errors.New("exit")
	}
	if _, _, err := m.Status(); err == nil || !strings.Contains(err.Error(), "print failed") {
		t.Fatalf("expected status error, got %v", err)
	}
}

func TestSystemdStartStopStatusAndErrors(t *testing.T) {
	home := t.TempDir()
	calls := []string{}
	m := &Manager{
		goos: "linux",
		homeDir: func() (string, error) {
			return home, nil
		},
		lookPath: func(file string) (string, error) {
			return "/bin/systemctl", nil
		},
		run: func(name string, args ...string) (string, error) {
			calls = append(calls, strings.Join(append([]string{name}, args...), " "))
			if len(args) >= 2 && args[1] == "is-active" {
				return "active", nil
			}
			return "", nil
		},
	}

	mode, err := m.Start("/usr/local/bin/heraldctl", "/tmp/herald.yaml")
	if err != nil {
		t.Fatalf("unexpected start error: %v", err)
	}
	if mode != ModeSystemd {
		t.Fatalf("unexpected mode: %s", mode)
	}
	unitPath := filepath.Join(home, ".config", "systemd", "user", "herald-daemon.service")
	if _, err := os.Stat(unitPath); err != nil {
		t.Fatalf("expected unit file to be written: %v", err)
	}

	mode, err = m.Stop()
	if err != nil {
		t.Fatalf("unexpected stop error: %v", err)
	}
	if mode != ModeSystemd {
		t.Fatalf("unexpected stop mode: %s", mode)
	}

	running, mode, err := m.Status()
	if err != nil {
		t.Fatalf("unexpected status error: %v", err)
	}
	if !running || mode != ModeSystemd {
		t.Fatalf("expected active systemd status, got running=%v mode=%s", running, mode)
	}
	if len(calls) == 0 {
		t.Fatalf("expected systemctl calls")
	}

	m.run = func(name string, args ...string) (string, error) {
		if len(args) >= 2 && args[1] == "daemon-reload" {
			return "reload failed", errors.New("boom")
		}
		return "", nil
	}
	if _, err := m.Start("/usr/local/bin/heraldctl", ""); err == nil || !strings.Contains(err.Error(), "daemon-reload failed") {
		t.Fatalf("expected daemon-reload error, got %v", err)
	}

	m.run = func(name string, args ...string) (string, error) {
		if len(args) >= 2 && args[1] == "start" {
			return "start failed", errors.New("boom")
		}
		return "", nil
	}
	if _, err := m.Start("/usr/local/bin/heraldctl", ""); err == nil || !strings.Contains(err.Error(), "systemctl start failed") {
		t.Fatalf("expected start error, got %v", err)
	}

	m.run = func(name string, args ...string) (string, error) {
		return "not loaded", errors.New("boom")
	}
	if _, err := m.Stop(); err != nil {
		t.Fatalf("expected no stop error for not loaded, got %v", err)
	}

	m.run = func(name string, args ...string) (string, error) {
		return "hard fail", errors.New("boom")
	}
	if _, err := m.Stop(); err == nil || !strings.Contains(err.Error(), "systemctl stop failed") {
		t.Fatalf("expected stop error, got %v", err)
	}

	m.run = func(name string, args ...string) (string, error) {
		return "inactive", errors.New("exit")
	}
	running, mode, err = m.Status()
	if err != nil {
		t.Fatalf("expected no status error for inactive, got %v", err)
	}
	if running || mode != ModeSystemd {
		t.Fatalf("expected inactive status, got running=%v mode=%s", running, mode)
	}

	m.run = func(name string, args ...string) (string, error) {
		return "weird", errors.New("exit")
	}
	if _, _, err := m.Status(); err == nil || !strings.Contains(err.Error(), "is-active failed") {
		t.Fatalf("expected status error, got %v", err)
	}
}

func TestStartLaunchdAndSystemdHomeOrWriteErrors(t *testing.T) {
	t.Run("launchd home dir error", func(t *testing.T) {
		m := &Manager{
			goos: "darwin",
			uid:  501,
			lookPath: func(file string) (string, error) {
				return "/bin/launchctl", nil
			},
			homeDir: func() (string, error) { return "", errors.New("no home") },
			run: func(name string, args ...string) (string, error) {
				return "", nil
			},
		}
		if _, err := m.Start("/usr/local/bin/heraldctl", ""); err == nil || !strings.Contains(err.Error(), "no home") {
			t.Fatalf("expected home dir error, got %v", err)
		}
	})

	t.Run("launchd write plist error", func(t *testing.T) {
		home := t.TempDir()
		if err := os.MkdirAll(filepath.Join(home, "Library"), 0o755); err != nil {
			t.Fatalf("mkdir failed: %v", err)
		}
		if err := os.WriteFile(filepath.Join(home, "Library", "LaunchAgents"), []byte("x"), 0o644); err != nil {
			t.Fatalf("write failed: %v", err)
		}
		m := &Manager{
			goos: "darwin",
			uid:  501,
			lookPath: func(file string) (string, error) {
				return "/bin/launchctl", nil
			},
			homeDir: func() (string, error) { return home, nil },
			run: func(name string, args ...string) (string, error) {
				return "", nil
			},
		}
		if _, err := m.Start("/usr/local/bin/heraldctl", ""); err == nil {
			t.Fatalf("expected write plist error")
		}
	})

	t.Run("systemd home dir error", func(t *testing.T) {
		m := &Manager{
			goos: "linux",
			lookPath: func(file string) (string, error) {
				return "/bin/systemctl", nil
			},
			homeDir: func() (string, error) { return "", errors.New("no home") },
			run: func(name string, args ...string) (string, error) {
				return "", nil
			},
		}
		if _, err := m.Start("/usr/local/bin/heraldctl", ""); err == nil || !strings.Contains(err.Error(), "no home") {
			t.Fatalf("expected home dir error, got %v", err)
		}
	})

	t.Run("systemd write unit error", func(t *testing.T) {
		home := t.TempDir()
		if err := os.MkdirAll(filepath.Join(home, ".config"), 0o755); err != nil {
			t.Fatalf("mkdir failed: %v", err)
		}
		if err := os.WriteFile(filepath.Join(home, ".config", "systemd"), []byte("x"), 0o644); err != nil {
			t.Fatalf("write failed: %v", err)
		}
		m := &Manager{
			goos: "linux",
			lookPath: func(file string) (string, error) {
				return "/bin/systemctl", nil
			},
			homeDir: func() (string, error) { return home, nil },
			run: func(name string, args ...string) (string, error) {
				return "", nil
			},
		}
		if _, err := m.Start("/usr/local/bin/heraldctl", ""); err == nil {
			t.Fatalf("expected write unit error")
		}
	})
}

func TestWriteIfChangedAndShellEscape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "unit.service")
	if err := writeIfChanged(path, "hello"); err != nil {
		t.Fatalf("writeIfChanged failed: %v", err)
	}
	if err := writeIfChanged(path, "hello"); err != nil {
		t.Fatalf("writeIfChanged same content failed: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("unexpected content: %q", string(data))
	}

	if got := shellEscape(""); got != "''" {
		t.Fatalf("unexpected empty shell escape: %q", got)
	}
	if got := shellEscape("simple"); got != "simple" {
		t.Fatalf("unexpected simple shell escape: %q", got)
	}
	if got := shellEscape("with space"); got != "'with space'" {
		t.Fatalf("unexpected spaced shell escape: %q", got)
	}
	if got := shellEscape("it's"); got != "'it'\\''s'" {
		t.Fatalf("unexpected quote shell escape: %q", got)
	}
}

func TestWriteIfChangedErrorBranches(t *testing.T) {
	t.Run("mkdir failure", func(t *testing.T) {
		tmp := t.TempDir()
		file := filepath.Join(tmp, "file")
		if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
			t.Fatalf("write failed: %v", err)
		}
		target := filepath.Join(file, "child", "unit.service")
		if err := writeIfChanged(target, "hello"); err == nil {
			t.Fatalf("expected mkdir failure")
		}
	})

	t.Run("write failure", func(t *testing.T) {
		tmp := t.TempDir()
		dir := filepath.Join(tmp, "dir")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir failed: %v", err)
		}
		// Make target path a directory so os.WriteFile fails.
		target := filepath.Join(dir, "unit.service")
		if err := os.MkdirAll(target, 0o755); err != nil {
			t.Fatalf("mkdir failed: %v", err)
		}
		if err := writeIfChanged(target, "hello"); err == nil {
			t.Fatalf("expected write failure")
		}
	})
}
