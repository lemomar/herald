package cli

import (
	"testing"

	"herald/internal/config"
)

func TestMergeWithConfig(t *testing.T) {
	cfg := config.Config{
		Defaults: config.Defaults{
			Message: "from-config",
			Title:   "config-title",
			Icon:    "/tmp/icon.png",
		},
	}

	opts := Options{Message: "", Title: "", Icon: "", ConfigPath: ""}
	merged := MergeWithConfig(opts, cfg)

	if merged.Message != "from-config" {
		t.Fatalf("expected message from config, got %q", merged.Message)
	}
	if merged.Title != "config-title" {
		t.Fatalf("expected title from config, got %q", merged.Title)
	}
	if merged.Icon != "/tmp/icon.png" {
		t.Fatalf("expected icon from config, got %q", merged.Icon)
	}

	override := Options{Message: "cli", Title: "cli-title", Icon: "/cli/icon"}
	merged = MergeWithConfig(override, cfg)

	if merged.Message != "cli" {
		t.Fatalf("expected message from cli, got %q", merged.Message)
	}
	if merged.Title != "cli-title" {
		t.Fatalf("expected title from cli, got %q", merged.Title)
	}
	if merged.Icon != "/cli/icon" {
		t.Fatalf("expected icon from cli, got %q", merged.Icon)
	}

	emptyCfg := config.Config{}
	merged = MergeWithConfig(Options{}, emptyCfg)
	if merged.Title != "" {
		t.Fatalf("expected empty title before resolution, got %q", merged.Title)
	}
}

func TestParseFlagsAfterMessage(t *testing.T) {
	opts, err := Parse([]string{"Hello", "world", "--title", "Yo"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.Message != "Hello world" {
		t.Fatalf("expected message to be parsed, got %q", opts.Message)
	}
	if opts.Title != "Yo" {
		t.Fatalf("expected title to be parsed, got %q", opts.Title)
	}
}

func TestParseExitCode(t *testing.T) {
	opts, err := Parse([]string{"--exit-code", "0"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.ExitCode == nil || *opts.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %v", opts.ExitCode)
	}

	opts, err = Parse([]string{"--exit-code=2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.ExitCode == nil || *opts.ExitCode != 2 {
		t.Fatalf("expected exit code 2, got %v", opts.ExitCode)
	}

	if _, err := Parse([]string{"--exit-code", "nope"}); err == nil {
		t.Fatalf("expected error for invalid exit code")
	}
}

func TestParseExitCodeValue(t *testing.T) {
	if _, err := ParseExitCodeValue(""); err == nil {
		t.Fatalf("expected error for empty value")
	}
	if _, err := ParseExitCodeValue("nope"); err == nil {
		t.Fatalf("expected error for non-integer")
	}
	code, err := ParseExitCodeValue("3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 3 {
		t.Fatalf("expected code 3, got %d", code)
	}
}

func TestResolveMessage(t *testing.T) {
	cfg := config.Config{Defaults: config.Defaults{Message: "from-config"}}
	if msg, explicit := ResolveMessage(Options{Message: "cli"}, cfg, false); msg != "cli" || !explicit {
		t.Fatalf("expected cli message, got %q", msg)
	}
	if msg, explicit := ResolveMessage(Options{}, cfg, false); msg != "from-config" || !explicit {
		t.Fatalf("expected config message, got %q", msg)
	}

	code := 0
	if msg, explicit := ResolveMessage(Options{ExitCode: &code}, config.Config{}, true); msg != "Task succeeded" || explicit {
		t.Fatalf("expected success message, got %q", msg)
	}
	code = 9
	if msg, explicit := ResolveMessage(Options{ExitCode: &code}, config.Config{}, true); msg != "Task failed with code 9" || explicit {
		t.Fatalf("expected failure message, got %q", msg)
	}

	if msg, explicit := ResolveMessage(Options{}, config.Config{}, false); msg != "Hello from Herald" || explicit {
		t.Fatalf("expected default hello message, got %q", msg)
	}

	code = 0
	if msg, explicit := ResolveMessage(Options{Evaluate: true, ExitCode: &code, Message: "cli"}, cfg, true); msg != "cli" || !explicit {
		t.Fatalf("expected cli message to win, got %q", msg)
	}

	if msg, explicit := ResolveMessage(Options{Evaluate: true}, config.Config{}, true); msg != "Hello from Herald" || explicit {
		t.Fatalf("expected greeting when evaluate active without exit code, got %q", msg)
	}
}

func TestResolveTitle(t *testing.T) {
	cfg := config.Config{Defaults: config.Defaults{Title: "from-config"}}
	if title := ResolveTitle(Options{Title: "cli"}, cfg, "ls", true, false); title != "cli" {
		t.Fatalf("expected cli title, got %q", title)
	}
	if title := ResolveTitle(Options{}, cfg, "ls", false, false); title != "from-config" {
		t.Fatalf("expected config title, got %q", title)
	}
	code := 0
	if title := ResolveTitle(Options{ExitCode: &code}, config.Config{}, "make build", false, true); title != "Command: make build" {
		t.Fatalf("expected prev cmd title, got %q", title)
	}
	if title := ResolveTitle(Options{Message: "custom"}, config.Config{}, "make build", true, true); title != "Herald" {
		t.Fatalf("expected Herald title when message provided, got %q", title)
	}
	if title := ResolveTitle(Options{}, config.Config{}, "", false, false); title != "Herald" {
		t.Fatalf("expected default title Herald, got %q", title)
	}
}
