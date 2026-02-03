package main

import (
	"fmt"
	"os"
	"runtime"

	"herald/internal/cli"
	"herald/internal/config"
	"herald/internal/notify"
)

func main() {
	opts, err := cli.Parse(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(2)
	}

	cfgPath := opts.ConfigPath
	if cfgPath == "" {
		defaultPath, err := config.DefaultPath()
		if err != nil {
			fmt.Fprintln(os.Stderr, "failed to resolve config path:", err)
			os.Exit(1)
		}
		cfgPath = defaultPath
	}

	cfg, _, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}

	merged := cli.MergeWithConfig(opts, cfg)
	if merged.Message == "" {
		fmt.Fprintln(os.Stderr, "message is required (or set defaults.message in ~/.heraldrc)")
		os.Exit(2)
	}

	currentOS := runtime.GOOS
	if currentOS == "darwin" && merged.Icon != "" {
		if merged.Verbose {
			fmt.Fprintln(os.Stderr, "warning: macOS notifications do not support custom icons; ignoring --icon")
		}
		merged.Icon = ""
	}

	if merged.Icon != "" {
		if err := notify.ValidateIcon(merged.Icon); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(1)
		}
	}

	notifier, err := notify.ForOS(currentOS)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}

	if err := notifier.Send(merged.Message, merged.Title, merged.Icon); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}

	if merged.Verbose {
		fmt.Fprintln(os.Stdout, "notification sent")
	}
}
