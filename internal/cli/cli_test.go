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
	if merged.Title != "Herald" {
		t.Fatalf("expected default title Herald, got %q", merged.Title)
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
