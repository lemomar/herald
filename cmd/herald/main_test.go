package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"herald/internal/cli"
	"herald/internal/config"
	"herald/internal/notify"
)

type exitPanic struct {
	code int
}

type fakeMainNotifier struct {
	err     error
	calls   int
	message string
	title   string
	icon    string
}

func (f *fakeMainNotifier) Send(message, title, icon string) error {
	f.calls++
	f.message = message
	f.title = title
	f.icon = icon
	return f.err
}

func resetMainHooks(t *testing.T) {
	t.Helper()

	origExitFn := exitFn
	origGetenvFn := getenvFn
	origCurrentOSFn := currentOSFn
	origParseCLIFn := parseCLIFn
	origDefaultConfigPathFn := defaultConfigPathFn
	origLoadConfigFn := loadConfigFn
	origParseExitCodeFn := parseExitCodeFn
	origValidateIconFn := validateIconFn
	origNotifyForOSFn := notifyForOSFn
	origExecutablePathFn := executablePathFn
	origUserHomeDirFn := userHomeDirFn
	origOpenHookFileFn := openHookFileFn

	t.Cleanup(func() {
		exitFn = origExitFn
		getenvFn = origGetenvFn
		currentOSFn = origCurrentOSFn
		parseCLIFn = origParseCLIFn
		defaultConfigPathFn = origDefaultConfigPathFn
		loadConfigFn = origLoadConfigFn
		parseExitCodeFn = origParseExitCodeFn
		validateIconFn = origValidateIconFn
		notifyForOSFn = origNotifyForOSFn
		executablePathFn = origExecutablePathFn
		userHomeDirFn = origUserHomeDirFn
		openHookFileFn = origOpenHookFileFn
	})
}

func runMainCaptureExit(t *testing.T) (code int) {
	t.Helper()
	code = 0
	exitFn = func(c int) { panic(exitPanic{code: c}) }
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(exitPanic); ok {
				code = e.code
				return
			}
			panic(r)
		}
	}()
	main()
	return
}

func TestSanitizePrevCommand(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"false", "false"},
		{"echo hey", "echo"},
		{"false; herald", "false"},
		{"echo hey; false; herald", "false"},
		{"echo hey; false", "false"},
		{"./herald", ""},
		{"herald --evaluate", ""},
		{"", ""},
		{"   ", ""},
		{" ; ; ", ""},
	}

	for _, tc := range cases {
		if got := sanitizePrevCommand(tc.in); got != tc.want {
			t.Fatalf("sanitizePrevCommand(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRunLogsAlias(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	logPath := filepath.Join(tmp, ".herald", "logs.yaml")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	content := "- timestamp: 2026-02-06T23:40:12Z\n  source: daemon\n  event: notify\n  title: Build\n  message: Done\n  level: info\n"
	if err := os.WriteFile(logPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	var out bytes.Buffer
	if err := runLogsAlias([]string{"--filter", "done"}, &out); err != nil {
		t.Fatalf("runLogsAlias returned error: %v", err)
	}
	if !strings.Contains(out.String(), "Build: Done") {
		t.Fatalf("expected logs output, got %q", out.String())
	}
}

func TestRunLogsAliasErrorBranches(t *testing.T) {
	var out bytes.Buffer

	if err := runLogsAlias([]string{"--last", "-1"}, &out); err == nil {
		t.Fatalf("expected error for negative --last")
	}
	if err := runLogsAlias([]string{"--unknown"}, &out); err == nil {
		t.Fatalf("expected error for unknown flag")
	}
	if err := runLogsAlias([]string{"extra"}, &out); err == nil {
		t.Fatalf("expected error for positional argument")
	}
}

func TestHookScriptAndPrintHook(t *testing.T) {
	resetMainHooks(t)
	if _, err := hookScript("nope", "/tmp/herald"); err == nil {
		t.Fatalf("expected unsupported shell error")
	}

	zsh, err := hookScript("zsh", "/tmp/herald")
	if err != nil {
		t.Fatalf("unexpected zsh hook error: %v", err)
	}
	if !strings.Contains(zsh, "add-zsh-hook") || !strings.Contains(zsh, "HERALD_HOOK=1") {
		t.Fatalf("unexpected zsh hook: %q", zsh)
	}

	fish, err := hookScript("fish", "/tmp/herald")
	if err != nil {
		t.Fatalf("unexpected fish hook error: %v", err)
	}
	if !strings.Contains(fish, "fish_preexec") || !strings.Contains(fish, "function herald") {
		t.Fatalf("unexpected fish hook: %q", fish)
	}

	if err := printHook([]string{"--shell"}); err == nil {
		t.Fatalf("expected missing shell value error")
	}
	if err := printHook([]string{"--bad"}); err == nil {
		t.Fatalf("expected unknown flag error")
	}
	if err := printHook([]string{"--shell", "nope"}); err == nil {
		t.Fatalf("expected unsupported shell error")
	}

	stdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe failed: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = stdout }()

	if err := printHook([]string{"--shell", "bash"}); err != nil {
		t.Fatalf("unexpected printHook error: %v", err)
	}
	_ = w.Close()
	data, _ := io.ReadAll(r)
	if !strings.Contains(string(data), "HERALD_HOOK=1") {
		t.Fatalf("expected hook output, got %q", string(data))
	}

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	if err := printHook([]string{"--shell", "zsh", "--install"}); err != nil {
		t.Fatalf("expected install hook path to succeed, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, ".zshrc")); err != nil {
		t.Fatalf("expected .zshrc to be created: %v", err)
	}

	executablePathFn = func() (string, error) { return "", errors.New("no executable") }
	if err := printHook([]string{"--shell", "zsh"}); err == nil {
		t.Fatalf("expected executable lookup error")
	}
}

func TestInstallHookWritesAndUpdatesBlock(t *testing.T) {
	resetMainHooks(t)
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	script1 := "echo one"
	if err := installHook("zsh", script1); err != nil {
		t.Fatalf("install hook failed: %v", err)
	}
	rcPath := filepath.Join(tmp, ".zshrc")
	data, err := os.ReadFile(rcPath)
	if err != nil {
		t.Fatalf("read rc failed: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, script1) {
		t.Fatalf("expected script in rc: %q", content)
	}

	script2 := "echo two"
	if err := installHook("zsh", script2); err != nil {
		t.Fatalf("install hook update failed: %v", err)
	}
	data, err = os.ReadFile(rcPath)
	if err != nil {
		t.Fatalf("read rc failed: %v", err)
	}
	content = string(data)
	if strings.Contains(content, script1) || !strings.Contains(content, script2) {
		t.Fatalf("expected updated hook block, got %q", content)
	}

	if err := installHook("unknown", "x"); err == nil {
		t.Fatalf("expected unsupported shell error")
	}
}

func TestInstallHookBashFishAndErrorBranches(t *testing.T) {
	resetMainHooks(t)
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	if err := installHook("bash", "echo bash"); err != nil {
		t.Fatalf("bash install failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, ".bashrc")); err != nil {
		t.Fatalf("expected .bashrc: %v", err)
	}

	if err := installHook("fish", "echo fish"); err != nil {
		t.Fatalf("fish install failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, ".config", "fish", "config.fish")); err != nil {
		t.Fatalf("expected fish config: %v", err)
	}

	// mkdir failure: ~/.config is a file, so ~/.config/fish cannot be created.
	tmp2 := t.TempDir()
	t.Setenv("HOME", tmp2)
	if err := os.WriteFile(filepath.Join(tmp2, ".config"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if err := installHook("fish", "echo fish"); err == nil {
		t.Fatalf("expected mkdir failure for fish hook")
	}

	// open failure: ~/.zshrc is a directory.
	tmp3 := t.TempDir()
	t.Setenv("HOME", tmp3)
	if err := os.MkdirAll(filepath.Join(tmp3, ".zshrc"), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := installHook("zsh", "echo zsh"); err == nil {
		t.Fatalf("expected open failure when rc path is directory")
	}

	userHomeDirFn = func() (string, error) { return "", errors.New("no home") }
	if err := installHook("zsh", "echo zsh"); err == nil {
		t.Fatalf("expected user home dir error")
	}

	tmp4 := t.TempDir()
	t.Setenv("HOME", tmp4)
	userHomeDirFn = os.UserHomeDir
	readonly := filepath.Join(tmp4, "readonly")
	if err := os.WriteFile(readonly, []byte("x"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	openHookFileFn = func(name string, flag int, perm os.FileMode) (*os.File, error) {
		return os.Open(readonly)
	}
	if err := installHook("zsh", "echo zsh"); err == nil {
		t.Fatalf("expected write failure for read-only handle")
	}
}

func TestPrintHookShellAutoDetection(t *testing.T) {
	resetMainHooks(t)

	t.Setenv("SHELL", "/bin/zsh")
	if err := printHook([]string{}); err != nil {
		t.Fatalf("expected print hook success with zsh autodetect, got %v", err)
	}

	t.Setenv("SHELL", "/bin/bash")
	if err := printHook([]string{}); err != nil {
		t.Fatalf("expected print hook success with shell autodetect, got %v", err)
	}

	t.Setenv("SHELL", "/usr/bin/fish")
	if err := printHook([]string{}); err != nil {
		t.Fatalf("expected print hook success with fish autodetect, got %v", err)
	}

	t.Setenv("SHELL", "")
	if err := printHook([]string{}); err != nil {
		t.Fatalf("expected print hook success with default zsh shell, got %v", err)
	}

	if err := printHook([]string{"--shell=bash"}); err != nil {
		t.Fatalf("expected --shell=bash style to work, got %v", err)
	}
}

func TestMainSuccessPathsDirect(t *testing.T) {
	resetMainHooks(t)
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	// logs subcommand path
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	logPath := filepath.Join(tmp, ".herald", "logs.yaml")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(logPath, []byte("- timestamp: 2026-02-06T23:40:12Z\n  source: daemon\n  event: notify\n  title: Build\n  message: Done\n  level: info\n"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	os.Args = []string{"herald", "logs", "--last", "1"}
	main()

	// hook subcommand path
	os.Args = []string{"herald", "hook", "--shell", "zsh"}
	main()

	// notify path
	binDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if runtime.GOOS == "darwin" {
		script := "#!/bin/sh\necho \"$@\" > \"" + filepath.Join(tmp, "osascript_args.txt") + "\"\n"
		if err := os.WriteFile(filepath.Join(binDir, "osascript"), []byte(script), 0o755); err != nil {
			t.Fatalf("write script failed: %v", err)
		}
	} else if runtime.GOOS == "linux" {
		script := "#!/bin/sh\necho \"$@\" > \"" + filepath.Join(tmp, "notify_args.txt") + "\"\n"
		if err := os.WriteFile(filepath.Join(binDir, "notify-send"), []byte(script), 0o755); err != nil {
			t.Fatalf("write script failed: %v", err)
		}
	} else {
		t.Skip("unsupported OS for direct notify path test")
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HERALD_HOOK", "1")
	os.Args = []string{"herald", "Build complete", "--title", "CI"}
	main()
}

func TestMainErrorBranchesWithHooks(t *testing.T) {
	t.Run("logs branch error", func(t *testing.T) {
		resetMainHooks(t)
		origArgs := os.Args
		defer func() { os.Args = origArgs }()
		os.Args = []string{"herald", "logs", "--last", "-1"}
		if code := runMainCaptureExit(t); code != 2 {
			t.Fatalf("expected exit 2, got %d", code)
		}
	})

	t.Run("hook branch error", func(t *testing.T) {
		resetMainHooks(t)
		origArgs := os.Args
		defer func() { os.Args = origArgs }()
		os.Args = []string{"herald", "hook", "--unknown"}
		if code := runMainCaptureExit(t); code != 2 {
			t.Fatalf("expected exit 2, got %d", code)
		}
	})

	t.Run("parse error", func(t *testing.T) {
		resetMainHooks(t)
		origArgs := os.Args
		defer func() { os.Args = origArgs }()
		os.Args = []string{"herald", "hello"}
		parseCLIFn = func([]string) (cli.Options, error) { return cli.Options{}, errors.New("parse failed") }
		if code := runMainCaptureExit(t); code != 2 {
			t.Fatalf("expected exit 2, got %d", code)
		}
	})

	t.Run("default config path error", func(t *testing.T) {
		resetMainHooks(t)
		origArgs := os.Args
		defer func() { os.Args = origArgs }()
		os.Args = []string{"herald", "hello"}
		defaultConfigPathFn = func() (string, error) { return "", errors.New("no home") }
		if code := runMainCaptureExit(t); code != 1 {
			t.Fatalf("expected exit 1, got %d", code)
		}
	})

	t.Run("load config error", func(t *testing.T) {
		resetMainHooks(t)
		origArgs := os.Args
		defer func() { os.Args = origArgs }()
		os.Args = []string{"herald", "--config", "/tmp/bad"}
		loadConfigFn = func(string) (config.Config, bool, error) { return config.Config{}, true, errors.New("bad config") }
		if code := runMainCaptureExit(t); code != 1 {
			t.Fatalf("expected exit 1, got %d", code)
		}
	})

	t.Run("bad HERALD_EXIT_CODE", func(t *testing.T) {
		resetMainHooks(t)
		origArgs := os.Args
		defer func() { os.Args = origArgs }()
		os.Args = []string{"herald", "hello"}
		getenvFn = func(key string) string {
			if key == "HERALD_HOOK" {
				return "1"
			}
			if key == "HERALD_EXIT_CODE" {
				return "oops"
			}
			return ""
		}
		loadConfigFn = func(string) (config.Config, bool, error) { return config.Config{}, false, nil }
		if code := runMainCaptureExit(t); code != 2 {
			t.Fatalf("expected exit 2, got %d", code)
		}
	})

	t.Run("icon validation error", func(t *testing.T) {
		resetMainHooks(t)
		origArgs := os.Args
		defer func() { os.Args = origArgs }()
		os.Args = []string{"herald", "--icon", "/tmp/x", "hello"}
		getenvFn = func(key string) string {
			if key == "HERALD_HOOK" {
				return "1"
			}
			return ""
		}
		currentOSFn = func() string { return "linux" }
		loadConfigFn = func(string) (config.Config, bool, error) { return config.Config{}, false, nil }
		validateIconFn = func(string) error { return errors.New("bad icon") }
		if code := runMainCaptureExit(t); code != 1 {
			t.Fatalf("expected exit 1, got %d", code)
		}
	})

	t.Run("notify for os error", func(t *testing.T) {
		resetMainHooks(t)
		origArgs := os.Args
		defer func() { os.Args = origArgs }()
		os.Args = []string{"herald", "hello"}
		getenvFn = func(key string) string {
			if key == "HERALD_HOOK" {
				return "1"
			}
			return ""
		}
		currentOSFn = func() string { return "linux" }
		loadConfigFn = func(string) (config.Config, bool, error) { return config.Config{}, false, nil }
		validateIconFn = func(string) error { return nil }
		notifyForOSFn = func(string) (notify.Notifier, error) { return nil, errors.New("unsupported") }
		if code := runMainCaptureExit(t); code != 1 {
			t.Fatalf("expected exit 1, got %d", code)
		}
	})

	t.Run("notifier send error", func(t *testing.T) {
		resetMainHooks(t)
		origArgs := os.Args
		defer func() { os.Args = origArgs }()
		notifier := &fakeMainNotifier{err: errors.New("send fail")}
		os.Args = []string{"herald", "hello"}
		getenvFn = func(key string) string {
			if key == "HERALD_HOOK" {
				return "1"
			}
			return ""
		}
		currentOSFn = func() string { return "linux" }
		loadConfigFn = func(string) (config.Config, bool, error) { return config.Config{}, false, nil }
		validateIconFn = func(string) error { return nil }
		notifyForOSFn = func(string) (notify.Notifier, error) { return notifier, nil }
		if code := runMainCaptureExit(t); code != 1 {
			t.Fatalf("expected exit 1, got %d", code)
		}
	})
}

func TestMainSuccessEnvExitCodeAndDarwinIconDrop(t *testing.T) {
	resetMainHooks(t)
	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{"herald"}

	notifier := &fakeMainNotifier{}
	parseCLIFn = func([]string) (cli.Options, error) {
		return cli.Options{Evaluate: true, Verbose: true, Icon: "/tmp/icon.png"}, nil
	}
	getenvFn = func(key string) string {
		switch key {
		case "HERALD_HOOK":
			return "1"
		case "HERALD_EXIT_CODE":
			return "0"
		default:
			return ""
		}
	}
	loadConfigFn = func(string) (config.Config, bool, error) { return config.Config{}, false, nil }
	currentOSFn = func() string { return "darwin" }
	validateIconFn = func(string) error {
		t.Fatalf("validate icon should not be called after darwin icon drop")
		return nil
	}
	notifyForOSFn = func(string) (notify.Notifier, error) { return notifier, nil }

	stdout := os.Stdout
	stderr := os.Stderr
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe failed: %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe failed: %v", err)
	}
	os.Stdout = outW
	os.Stderr = errW
	defer func() {
		os.Stdout = stdout
		os.Stderr = stderr
	}()

	if code := runMainCaptureExit(t); code != 0 {
		t.Fatalf("expected success exit code 0, got %d", code)
	}
	_ = outW.Close()
	_ = errW.Close()
	outData, _ := io.ReadAll(outR)
	errData, _ := io.ReadAll(errR)

	if notifier.calls != 1 {
		t.Fatalf("expected notifier call, got %d", notifier.calls)
	}
	if notifier.message != "Task succeeded" {
		t.Fatalf("unexpected notifier message: %q", notifier.message)
	}
	if notifier.icon != "" {
		t.Fatalf("expected empty icon on darwin, got %q", notifier.icon)
	}
	if !strings.Contains(string(outData), "notification sent") {
		t.Fatalf("expected verbose success output, got %q", string(outData))
	}
	if !strings.Contains(string(errData), "macOS notifications do not support custom icons") {
		t.Fatalf("expected darwin icon warning, got %q", string(errData))
	}
}

func TestMainIntegrationLogsCommand(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, ".herald", "logs.yaml")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	content := "- timestamp: 2026-02-06T23:40:12Z\n  source: daemon\n  event: notify\n  title: Build\n  message: Done\n  level: info\n"
	if err := os.WriteFile(logPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	out, code := runMainHelper(t, tmp, map[string]string{
		"HERALD_HOOK": "1",
	}, []string{"logs", "--last", "1"})
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d output=%q", code, out)
	}
	if !strings.Contains(out, "Build: Done") {
		t.Fatalf("expected logs output, got %q", out)
	}
}

func TestMainIntegrationHookCommandError(t *testing.T) {
	out, code := runMainHelper(t, t.TempDir(), map[string]string{}, []string{"hook", "--unknown"})
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d output=%q", code, out)
	}
	if !strings.Contains(out, "unknown flag") {
		t.Fatalf("expected unknown flag error output, got %q", out)
	}
}

func TestMainIntegrationNotifyPath(t *testing.T) {
	tmp := t.TempDir()
	binDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	if runtime.GOOS == "darwin" {
		script := "#!/bin/sh\necho \"$@\" > \"" + filepath.Join(tmp, "osascript_args.txt") + "\"\n"
		if err := os.WriteFile(filepath.Join(binDir, "osascript"), []byte(script), 0o755); err != nil {
			t.Fatalf("write script failed: %v", err)
		}
	} else if runtime.GOOS == "linux" {
		script := "#!/bin/sh\necho \"$@\" > \"" + filepath.Join(tmp, "notify_args.txt") + "\"\n"
		if err := os.WriteFile(filepath.Join(binDir, "notify-send"), []byte(script), 0o755); err != nil {
			t.Fatalf("write script failed: %v", err)
		}
	} else {
		t.Skip("unsupported OS for notify integration test")
	}

	out, code := runMainHelper(t, tmp, map[string]string{
		"HERALD_HOOK": "1",
		"PATH":        binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
	}, []string{"Build complete", "--title", "CI"})
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d output=%q", code, out)
	}
}

func TestHelperMainProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	idx := -1
	for i, arg := range os.Args {
		if arg == "--" {
			idx = i
			break
		}
	}
	if idx < 0 {
		os.Exit(11)
	}
	os.Args = append([]string{"herald"}, os.Args[idx+1:]...)
	main()
	os.Exit(0)
}

func runMainHelper(t *testing.T, home string, extraEnv map[string]string, args []string) (string, int) {
	t.Helper()
	cmdArgs := append([]string{"-test.run=TestHelperMainProcess", "--"}, args...)
	cmd := exec.Command(os.Args[0], cmdArgs...)
	env := append(os.Environ(), "GO_WANT_HELPER_PROCESS=1", "HOME="+home)
	for k, v := range extraEnv {
		env = append(env, k+"="+v)
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err == nil {
		return string(out), 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return string(out), exitErr.ExitCode()
	}
	t.Fatalf("helper command failed unexpectedly: %v", err)
	return "", -1
}
