package notify

import "testing"

func TestForOS(t *testing.T) {
	if _, err := ForOS("darwin"); err != nil {
		t.Fatalf("expected darwin notifier, got error: %v", err)
	}
	if _, err := ForOS("linux"); err != nil {
		t.Fatalf("expected linux notifier, got error: %v", err)
	}
	if _, err := ForOS("windows"); err == nil {
		t.Fatalf("expected error for unsupported OS")
	}
}
