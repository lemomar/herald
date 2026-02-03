package cli

import (
	"fmt"
	"strings"

	"herald/internal/config"
)

type Options struct {
	Message    string
	Title      string
	Icon       string
	ConfigPath string
	Verbose    bool
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

func MergeWithConfig(opts Options, cfg config.Config) Options {
	merged := opts

	if merged.Message == "" {
		merged.Message = cfg.Defaults.Message
	}
	if merged.Title == "" {
		merged.Title = cfg.Defaults.Title
		if merged.Title == "" {
			merged.Title = "Herald"
		}
	}
	if merged.Icon == "" {
		merged.Icon = cfg.Defaults.Icon
	 }

	return merged
}
