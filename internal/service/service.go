package service

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

var ErrUnavailable = errors.New("service manager unavailable")

const (
	ModeLaunchd = "launchd"
	ModeSystemd = "systemd"
)

type runFunc func(name string, args ...string) (string, error)
type lookPathFunc func(file string) (string, error)
type homeDirFunc func() (string, error)

type Manager struct {
	goos     string
	run      runFunc
	lookPath lookPathFunc
	homeDir  homeDirFunc
	uid      int
}

func NewManager() *Manager {
	return &Manager{
		goos: runtime.GOOS,
		run: func(name string, args ...string) (string, error) {
			cmd := exec.Command(name, args...)
			out, err := cmd.CombinedOutput()
			return strings.TrimSpace(string(out)), err
		},
		lookPath: exec.LookPath,
		homeDir:  os.UserHomeDir,
		uid:      os.Getuid(),
	}
}

func (m *Manager) Start(executablePath, configPath string) (string, error) {
	switch m.goos {
	case "darwin":
		if _, err := m.lookPath("launchctl"); err != nil {
			return "", ErrUnavailable
		}
		if err := m.startLaunchd(executablePath, configPath); err != nil {
			return "", err
		}
		return ModeLaunchd, nil
	case "linux":
		if _, err := m.lookPath("systemctl"); err != nil {
			return "", ErrUnavailable
		}
		if err := m.startSystemd(executablePath, configPath); err != nil {
			return "", err
		}
		return ModeSystemd, nil
	default:
		return "", ErrUnavailable
	}
}

func (m *Manager) Stop() (string, error) {
	switch m.goos {
	case "darwin":
		if _, err := m.lookPath("launchctl"); err != nil {
			return "", ErrUnavailable
		}
		if err := m.stopLaunchd(); err != nil {
			return "", err
		}
		return ModeLaunchd, nil
	case "linux":
		if _, err := m.lookPath("systemctl"); err != nil {
			return "", ErrUnavailable
		}
		if err := m.stopSystemd(); err != nil {
			return "", err
		}
		return ModeSystemd, nil
	default:
		return "", ErrUnavailable
	}
}

func (m *Manager) Status() (bool, string, error) {
	switch m.goos {
	case "darwin":
		if _, err := m.lookPath("launchctl"); err != nil {
			return false, "", ErrUnavailable
		}
		running, err := m.statusLaunchd()
		return running, ModeLaunchd, err
	case "linux":
		if _, err := m.lookPath("systemctl"); err != nil {
			return false, "", ErrUnavailable
		}
		running, err := m.statusSystemd()
		return running, ModeSystemd, err
	default:
		return false, "", ErrUnavailable
	}
}

func RenderLaunchdPlist(executablePath, configPath string) string {
	args := []string{}
	if configPath != "" {
		args = append(args, "--config", configPath)
	}
	args = append(args, "daemon", "run")
	argLines := make([]string, 0, len(args)+1)
	argLines = append(argLines, fmt.Sprintf("\t\t<string>%s</string>", executablePath))
	for _, arg := range args {
		argLines = append(argLines, fmt.Sprintf("\t\t<string>%s</string>", arg))
	}

	return strings.Join([]string{
		"<?xml version=\"1.0\" encoding=\"UTF-8\"?>",
		"<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">",
		"<plist version=\"1.0\">",
		"<dict>",
		"\t<key>Label</key>",
		"\t<string>com.herald.daemon</string>",
		"\t<key>ProgramArguments</key>",
		"\t<array>",
		strings.Join(argLines, "\n"),
		"\t</array>",
		"\t<key>RunAtLoad</key>",
		"\t<true/>",
		"\t<key>KeepAlive</key>",
		"\t<true/>",
		"</dict>",
		"</plist>",
	}, "\n") + "\n"
}

func RenderSystemdUnit(executablePath, configPath string) string {
	execStart := executablePath
	if configPath != "" {
		execStart += " --config " + shellEscape(configPath)
	}
	execStart += " daemon run"

	return strings.Join([]string{
		"[Unit]",
		"Description=Herald daemon",
		"After=network-online.target",
		"Wants=network-online.target",
		"",
		"[Service]",
		"Type=simple",
		"ExecStart=" + execStart,
		"Restart=always",
		"RestartSec=3",
		"",
		"[Install]",
		"WantedBy=default.target",
	}, "\n") + "\n"
}

func (m *Manager) startLaunchd(executablePath, configPath string) error {
	home, err := m.homeDir()
	if err != nil {
		return err
	}
	plistPath := filepath.Join(home, "Library", "LaunchAgents", "com.herald.daemon.plist")
	if err := writeIfChanged(plistPath, RenderLaunchdPlist(executablePath, configPath)); err != nil {
		return err
	}

	domain := fmt.Sprintf("gui/%d", m.uid)
	_, _ = m.run("launchctl", "bootout", domain, "com.herald.daemon")
	if out, err := m.run("launchctl", "bootstrap", domain, plistPath); err != nil {
		return fmt.Errorf("launchctl bootstrap failed: %s", out)
	}
	if out, err := m.run("launchctl", "kickstart", "-k", domain+"/com.herald.daemon"); err != nil {
		return fmt.Errorf("launchctl kickstart failed: %s", out)
	}
	return nil
}

func (m *Manager) stopLaunchd() error {
	domain := fmt.Sprintf("gui/%d", m.uid)
	out, err := m.run("launchctl", "bootout", domain, "com.herald.daemon")
	if err != nil {
		if strings.Contains(strings.ToLower(out), "could not find") || strings.Contains(strings.ToLower(out), "no such process") {
			return nil
		}
		return fmt.Errorf("launchctl bootout failed: %s", out)
	}
	return nil
}

func (m *Manager) statusLaunchd() (bool, error) {
	domain := fmt.Sprintf("gui/%d", m.uid)
	out, err := m.run("launchctl", "print", domain+"/com.herald.daemon")
	if err != nil {
		if strings.Contains(strings.ToLower(out), "could not find") || strings.Contains(strings.ToLower(out), "not found") {
			return false, nil
		}
		return false, fmt.Errorf("launchctl print failed: %s", out)
	}
	return strings.TrimSpace(out) != "", nil
}

func (m *Manager) startSystemd(executablePath, configPath string) error {
	home, err := m.homeDir()
	if err != nil {
		return err
	}
	unitPath := filepath.Join(home, ".config", "systemd", "user", "herald-daemon.service")
	if err := writeIfChanged(unitPath, RenderSystemdUnit(executablePath, configPath)); err != nil {
		return err
	}

	if out, err := m.run("systemctl", "--user", "daemon-reload"); err != nil {
		return fmt.Errorf("systemctl daemon-reload failed: %s", out)
	}
	if out, err := m.run("systemctl", "--user", "start", "herald-daemon.service"); err != nil {
		return fmt.Errorf("systemctl start failed: %s", out)
	}
	return nil
}

func (m *Manager) stopSystemd() error {
	out, err := m.run("systemctl", "--user", "stop", "herald-daemon.service")
	if err != nil {
		if strings.Contains(strings.ToLower(out), "not loaded") || strings.Contains(strings.ToLower(out), "not found") {
			return nil
		}
		return fmt.Errorf("systemctl stop failed: %s", out)
	}
	return nil
}

func (m *Manager) statusSystemd() (bool, error) {
	out, err := m.run("systemctl", "--user", "is-active", "herald-daemon.service")
	if err != nil {
		trimmed := strings.TrimSpace(strings.ToLower(out))
		if trimmed == "inactive" || trimmed == "failed" || trimmed == "unknown" {
			return false, nil
		}
		return false, fmt.Errorf("systemctl is-active failed: %s", out)
	}
	return strings.TrimSpace(out) == "active", nil
}

func writeIfChanged(path, content string) error {
	if existing, err := os.ReadFile(path); err == nil {
		if string(existing) == content {
			return nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create service directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("failed to write service file %s: %w", path, err)
	}
	return nil
}

func shellEscape(input string) string {
	if input == "" {
		return "''"
	}
	if !strings.ContainsAny(input, " \t\n'\"") {
		return input
	}
	return "'" + strings.ReplaceAll(input, "'", "'\\''") + "'"
}
