package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

var userHomeDirFn = os.UserHomeDir

type Config struct {
	Mode     string       `yaml:"mode"`
	Defaults Defaults     `yaml:"defaults"`
	Daemon   DaemonConfig `yaml:"daemon"`
}

type Defaults struct {
	Message string `yaml:"message"`
	Title   string `yaml:"title"`
	Icon    string `yaml:"icon"`
}

type DaemonConfig struct {
	ServerURL    string `yaml:"server_url"`
	Token        string `yaml:"token"`
	ReconnectSec int    `yaml:"reconnect_sec"`
}

func DefaultPath() (string, error) {
	home, err := userHomeDirFn()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".heraldrc"), nil
}

func Load(path string) (Config, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, false, nil
		}
		return Config{}, false, fmt.Errorf("failed to read config %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, true, fmt.Errorf("invalid yaml in %s: %w", path, err)
	}
	cfg.applyDefaults()
	if err := validate(cfg); err != nil {
		return Config{}, true, fmt.Errorf("invalid config %s: %w", path, err)
	}

	return cfg, true, nil
}

func validate(cfg Config) error {
	if cfg.Mode == "" {
		return validateDaemonDefaults(cfg.Daemon)
	}
	switch cfg.Mode {
	case "greeting", "evaluate":
		return validateDaemonDefaults(cfg.Daemon)
	default:
		return fmt.Errorf("unsupported mode %q (expected greeting or evaluate)", cfg.Mode)
	}
}

func (cfg *Config) applyDefaults() {
	if cfg.Daemon.ReconnectSec <= 0 {
		cfg.Daemon.ReconnectSec = 5
	}
}

func validateDaemonDefaults(daemonCfg DaemonConfig) error {
	if daemonCfg.ServerURL == "" {
		return nil
	}
	return ValidateDaemon(daemonCfg)
}

func ValidateDaemon(daemonCfg DaemonConfig) error {
	if daemonCfg.ServerURL == "" {
		return fmt.Errorf("daemon.server_url is required")
	}

	parsed, err := url.Parse(daemonCfg.ServerURL)
	if err != nil {
		return fmt.Errorf("invalid daemon.server_url: %w", err)
	}
	switch parsed.Scheme {
	case "ws", "wss":
	default:
		return fmt.Errorf("daemon.server_url must use ws or wss scheme")
	}
	if parsed.Host == "" {
		return fmt.Errorf("daemon.server_url must include a host")
	}
	return nil
}
