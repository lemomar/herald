package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Mode     string   `yaml:"mode"`
	Defaults Defaults `yaml:"defaults"`
}

type Defaults struct {
	Message string `yaml:"message"`
	Title   string `yaml:"title"`
	Icon    string `yaml:"icon"`
}

func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
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
	if err := validate(cfg); err != nil {
		return Config{}, true, fmt.Errorf("invalid config %s: %w", path, err)
	}

	return cfg, true, nil
}

func validate(cfg Config) error {
	if cfg.Mode == "" {
		return nil
	}
	switch cfg.Mode {
	case "greeting", "evaluate":
		return nil
	default:
		return fmt.Errorf("unsupported mode %q (expected greeting or evaluate)", cfg.Mode)
	}
}
