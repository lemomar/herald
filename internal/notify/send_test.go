package notify

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinuxNotifierSendSuccessAndFailure(t *testing.T) {
	tmp := t.TempDir()
	argsPath := filepath.Join(tmp, "linux_args.txt")
	scriptPath := filepath.Join(tmp, "notify-send")
	script := "#!/bin/sh\necho \"$@\" > \"" + argsPath + "\"\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write script failed: %v", err)
	}
	t.Setenv("PATH", tmp)

	n := &linuxNotifier{}
	if err := n.Send("Build complete", "CI", "/tmp/icon.png"); err != nil {
		t.Fatalf("unexpected send error: %v", err)
	}
	data, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args failed: %v", err)
	}
	got := strings.TrimSpace(string(data))
	if !strings.Contains(got, "-i /tmp/icon.png") || !strings.Contains(got, "CI Build complete") {
		t.Fatalf("unexpected args: %q", got)
	}

	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\necho boom\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("write failing script failed: %v", err)
	}
	if err := n.Send("msg", "title", ""); err == nil {
		t.Fatalf("expected notify-send failure")
	}
}

func TestMacNotifierSendSuccessAndFailure(t *testing.T) {
	tmp := t.TempDir()
	argsPath := filepath.Join(tmp, "mac_args.txt")
	scriptPath := filepath.Join(tmp, "osascript")
	script := "#!/bin/sh\necho \"$@\" > \"" + argsPath + "\"\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write script failed: %v", err)
	}
	t.Setenv("PATH", tmp)

	n := &macNotifier{}
	if err := n.Send("Build complete", "CI", ""); err != nil {
		t.Fatalf("unexpected send error: %v", err)
	}
	data, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args failed: %v", err)
	}
	got := strings.TrimSpace(string(data))
	if !strings.Contains(got, "display notification") || !strings.Contains(got, "with title") {
		t.Fatalf("unexpected script args: %q", got)
	}

	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\necho fail\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("write failing script failed: %v", err)
	}
	if err := n.Send("msg", "title", ""); err == nil {
		t.Fatalf("expected osascript failure")
	}
}

func TestNotifierBinaryMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if err := (&linuxNotifier{}).Send("msg", "title", ""); err == nil {
		t.Fatalf("expected linux lookpath error")
	}
	if err := (&macNotifier{}).Send("msg", "title", ""); err == nil {
		t.Fatalf("expected mac lookpath error")
	}
}

func TestForOSAndAppleScriptHelpers(t *testing.T) {
	if _, err := ForOS("linux"); err != nil {
		t.Fatalf("expected linux notifier, got %v", err)
	}
	if _, err := ForOS("darwin"); err != nil {
		t.Fatalf("expected darwin notifier, got %v", err)
	}
	if _, err := ForOS("plan9"); err == nil {
		t.Fatalf("expected unsupported os error")
	}

	script := buildAppleScript("hello", "")
	if !strings.Contains(script, "display notification \"hello\"") || strings.Contains(script, "with title") {
		t.Fatalf("unexpected script without title: %q", script)
	}
}
