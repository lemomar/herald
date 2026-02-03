package notify

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateIcon(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "icon.png")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	if err := ValidateIcon(""); err != nil {
		t.Fatalf("expected no error for empty path, got %v", err)
	}
	if err := ValidateIcon(path); err != nil {
		t.Fatalf("expected no error for existing icon, got %v", err)
	}
	if err := ValidateIcon(filepath.Join(tmp, "missing.png")); err == nil {
		t.Fatalf("expected error for missing icon")
	}
}

func TestBuildAppleScriptEscapes(t *testing.T) {
	msg := `Hello "World" \\`
	title := `Title "quoted"`

	script := buildAppleScript(msg, title)
	if script == "" {
		t.Fatalf("expected script to be non-empty")
	}
	if !containsAll(script, []string{"display notification", "with title"}) {
		t.Fatalf("unexpected script: %s", script)
	}
}

func containsAll(haystack string, needles []string) bool {
	for _, n := range needles {
		if !strings.Contains(haystack, n) {
			return false
		}
	}
	return true
}
