package notify

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type Notifier interface {
	Send(message, title, icon string) error
}

func ForOS(goos string) (Notifier, error) {
	switch goos {
	case "darwin":
		return &macNotifier{}, nil
	case "linux":
		return &linuxNotifier{}, nil
	default:
		return nil, fmt.Errorf("unsupported OS: %s", goos)
	}
}

func ValidateIcon(path string) error {
	if path == "" {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("icon not found: %s", path)
		}
		return fmt.Errorf("failed to access icon %s: %w", path, err)
	}
	return nil
}

type macNotifier struct{}

type linuxNotifier struct{}

func (n *macNotifier) Send(message, title, icon string) error {
	if _, err := exec.LookPath("osascript"); err != nil {
		return errors.New("osascript not found; ensure it is available on macOS")
	}

	script := buildAppleScript(message, title)
	cmd := exec.Command("osascript", "-e", script)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("osascript failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func (n *linuxNotifier) Send(message, title, icon string) error {
	if _, err := exec.LookPath("notify-send"); err != nil {
		return errors.New("notify-send not found; install libnotify-bin or equivalent")
	}

	args := []string{}
	if icon != "" {
		args = append(args, "-i", icon)
	}
	args = append(args, title, message)

	cmd := exec.Command("notify-send", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("notify-send failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func buildAppleScript(message, title string) string {
	escapedMessage := escapeAppleScript(message)
	escapedTitle := escapeAppleScript(title)

	if title == "" {
		return fmt.Sprintf("display notification \"%s\"", escapedMessage)
	}
	return fmt.Sprintf("display notification \"%s\" with title \"%s\"", escapedMessage, escapedTitle)
}

func escapeAppleScript(value string) string {
	escaped := strings.ReplaceAll(value, "\\", "\\\\")
	escaped = strings.ReplaceAll(escaped, "\"", "\\\"")
	return escaped
}
