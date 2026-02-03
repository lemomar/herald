package cli

import (
	"fmt"
	"strconv"
	"strings"

	"herald/internal/config"
)

type Options struct {
	Message    string
	Title      string
	Icon       string
	ConfigPath string
	Verbose    bool
	ExitCode   *int
	Evaluate   bool
}

func Parse(args []string) (Options, error) {
	var opts Options
	var messageParts []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			messageParts = append(messageParts, arg)
			continue
		}

		if arg == "--verbose" {
			opts.Verbose = true
			continue
		}
		if arg == "--evaluate" {
			opts.Evaluate = true
			continue
		}

		key, val, hasValue := splitFlag(arg)
		if !hasValue {
			if i+1 >= len(args) {
				return Options{}, fmt.Errorf("flag %s requires a value", key)
			}
			val = args[i+1]
			i++
		}

		switch key {
		case "--title":
			opts.Title = val
		case "--icon":
			opts.Icon = val
		case "--config":
			opts.ConfigPath = val
		case "--exit-code":
			code, err := ParseExitCodeValue(val)
			if err != nil {
				return Options{}, err
			}
			opts.ExitCode = &code
		default:
			return Options{}, fmt.Errorf("unknown flag: %s", key)
		}
	}

	if len(messageParts) > 0 {
		opts.Message = strings.Join(messageParts, " ")
	}

	return opts, nil
}

func splitFlag(arg string) (string, string, bool) {
	if eq := strings.IndexByte(arg, '='); eq >= 0 {
		return arg[:eq], arg[eq+1:], true
	}
	return arg, "", false
}

func ParseExitCodeValue(val string) (int, error) {
	if val == "" {
		return 0, fmt.Errorf("flag --exit-code requires a value")
	}
	code, err := strconv.Atoi(val)
	if err != nil {
		return 0, fmt.Errorf("invalid --exit-code %q (must be integer)", val)
	}
	return code, nil
}

func MergeWithConfig(opts Options, cfg config.Config) Options {
	merged := opts

	if merged.Message == "" {
		merged.Message = cfg.Defaults.Message
	}
	if merged.Title == "" {
		merged.Title = cfg.Defaults.Title
	}
	if merged.Icon == "" {
		merged.Icon = cfg.Defaults.Icon
	}

	return merged
}

func ResolveMessage(opts Options, cfg config.Config, evaluateActive bool) (string, bool) {
	if opts.Message != "" {
		return opts.Message, true
	}
	if cfg.Defaults.Message != "" {
		return cfg.Defaults.Message, true
	}
	if evaluateActive && opts.ExitCode != nil {
		return derivedMessage(opts.ExitCode), false
	}
	return "Hello from Herald", false
}

func ResolveTitle(opts Options, cfg config.Config, prevCmd string, messageExplicit bool, evaluateActive bool) string {
	if opts.Title != "" {
		return opts.Title
	}
	if cfg.Defaults.Title != "" {
		return cfg.Defaults.Title
	}
	if messageExplicit {
		return "Herald"
	}
	if evaluateActive && prevCmd != "" {
		return fmt.Sprintf("Command: %s", prevCmd)
	}
	return "Herald"
}

func derivedMessage(exitCode *int) string {
	if exitCode == nil {
		return "Hello from Herald"
	}
	if *exitCode == 0 {
		return "Task succeeded"
	}
	return fmt.Sprintf("Task failed with code %d", *exitCode)
}
