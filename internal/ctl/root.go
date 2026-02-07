package ctl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"herald/internal/config"
	"herald/internal/daemon"
	"herald/internal/logcmd"
	"herald/internal/service"
)

type rootOptions struct {
	configPath string
}

type serviceController interface {
	Start(executablePath, configPath string) (string, error)
	Stop() (string, error)
	Status() (bool, string, error)
}

type daemonRunner interface {
	Run(ctx context.Context) error
}

var (
	newServiceManagerFn = func() serviceController {
		return service.NewManager()
	}
	loadConfigFn        = loadConfig
	validateDaemonFn    = config.ValidateDaemon
	osExecutableFn      = os.Executable
	defaultConfigPathFn = config.DefaultPath
	readConfigFn        = config.Load
	daemonIsRunningFn   = daemon.IsRunning
	daemonStopFn        = daemon.StopProcess
	startBackgroundFn   = startBackgroundProcess
	newRunnerFn         = func(opts daemon.Options) (daemonRunner, error) {
		return daemon.NewRunner(opts)
	}
	notifyContextFn = signal.NotifyContext
	openFileFn      = os.OpenFile
	execCommandFn   = exec.Command
	nowFn           = time.Now
	sleepFn         = time.Sleep

	backgroundWaitTimeout = 2 * time.Second
	backgroundPollDelay   = 100 * time.Millisecond
)

func NewRootCommand() *cobra.Command {
	opts := &rootOptions{}

	root := &cobra.Command{
		Use:          "heraldctl",
		Short:        "Manage the Herald daemon",
		SilenceUsage: true,
	}
	root.PersistentFlags().StringVar(&opts.configPath, "config", "", "Path to Herald config file")

	root.AddCommand(newStartCmd(opts))
	root.AddCommand(newStopCmd(opts))
	root.AddCommand(newStatusCmd(opts))
	root.AddCommand(newRestartCmd(opts))
	root.AddCommand(newLogsCmd())
	root.AddCommand(newDaemonCmd(opts))

	return root
}

func newStartCmd(opts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start Herald daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, cfgPath, err := loadConfigFn(opts.configPath)
			if err != nil {
				return err
			}
			if err := validateDaemonFn(cfg.Daemon); err != nil {
				return err
			}

			svc := newServiceManagerFn()
			running, mode, err := svc.Status()
			if err == nil && running {
				fmt.Fprintf(cmd.OutOrStdout(), "running (%s)\n", mode)
				return nil
			}
			if err != nil && !errors.Is(err, service.ErrUnavailable) {
				return err
			}

			exe, err := osExecutableFn()
			if err != nil {
				return fmt.Errorf("failed to resolve executable path: %w", err)
			}

			mode, err = svc.Start(exe, cfgPath)
			if err == nil {
				fmt.Fprintf(cmd.OutOrStdout(), "started (%s)\n", mode)
				return nil
			}
			if !errors.Is(err, service.ErrUnavailable) {
				return err
			}

			running, _, err = daemonIsRunningFn("")
			if err != nil {
				return err
			}
			if running {
				fmt.Fprintln(cmd.OutOrStdout(), "running (pid)")
				return nil
			}

			if err := startBackgroundFn(exe, cfgPath); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "started (pid)")
			return nil
		},
	}
}

func newStopCmd(opts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop Herald daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := newServiceManagerFn()
			running, mode, err := svc.Status()
			if err == nil {
				if running {
					if _, err := svc.Stop(); err != nil {
						return err
					}
					fmt.Fprintf(cmd.OutOrStdout(), "stopped (%s)\n", mode)
					return nil
				}
				fmt.Fprintf(cmd.OutOrStdout(), "stopped (%s)\n", mode)
				return nil
			}
			if !errors.Is(err, service.ErrUnavailable) {
				return err
			}

			running, pid, err := daemonIsRunningFn("")
			if err != nil {
				return err
			}
			if !running {
				fmt.Fprintln(cmd.OutOrStdout(), "stopped")
				return nil
			}

			if err := daemonStopFn(""); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "stopped (pid:%d)\n", pid)
			return nil
		},
	}
}

func newStatusCmd(opts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show Herald daemon status",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := newServiceManagerFn()
			if running, mode, err := svc.Status(); err == nil {
				if running {
					fmt.Fprintf(cmd.OutOrStdout(), "running (%s)\n", mode)
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "stopped (%s)\n", mode)
				}
				return nil
			} else if !errors.Is(err, service.ErrUnavailable) {
				return err
			}

			running, pid, err := daemonIsRunningFn("")
			if err != nil {
				return err
			}
			if running {
				fmt.Fprintf(cmd.OutOrStdout(), "running (pid:%d)\n", pid)
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "stopped")
			}
			return nil
		},
	}
}

func newRestartCmd(opts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "restart",
		Short: "Restart Herald daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := newStopCmd(opts).RunE(cmd, args); err != nil {
				return err
			}
			return newStartCmd(opts).RunE(cmd, args)
		},
	}
}

func newLogsCmd() *cobra.Command {
	var last int
	var filter string

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Show Herald log history",
		RunE: func(cmd *cobra.Command, args []string) error {
			return logcmd.Run(cmd.OutOrStdout(), logcmd.Options{
				Last:   last,
				Filter: filter,
			})
		},
	}
	cmd.Flags().IntVar(&last, "last", 0, "Show only the last N entries")
	cmd.Flags().StringVar(&filter, "filter", "", "Filter logs by text (case-insensitive)")
	return cmd
}

func newDaemonCmd(opts *rootOptions) *cobra.Command {
	daemonCmd := &cobra.Command{
		Use:    "daemon",
		Hidden: true,
	}

	runCmd := &cobra.Command{
		Use:    "run",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := loadConfigFn(opts.configPath)
			if err != nil {
				return err
			}
			if err := validateDaemonFn(cfg.Daemon); err != nil {
				return err
			}

			runner, err := newRunnerFn(daemon.Options{
				DaemonConfig: cfg.Daemon,
			})
			if err != nil {
				return err
			}

			ctx, stop := notifyContextFn(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return runner.Run(ctx)
		},
	}

	daemonCmd.AddCommand(runCmd)
	return daemonCmd
}

func loadConfig(explicitPath string) (config.Config, string, error) {
	cfgPath := explicitPath
	if cfgPath == "" {
		defaultPath, err := defaultConfigPathFn()
		if err != nil {
			return config.Config{}, "", err
		}
		cfgPath = defaultPath
	}

	cfg, _, err := readConfigFn(cfgPath)
	if err != nil {
		return config.Config{}, "", err
	}
	return cfg, cfgPath, nil
}

func startBackgroundProcess(executablePath, configPath string) error {
	args := []string{}
	if configPath != "" {
		args = append(args, "--config", configPath)
	}
	args = append(args, "daemon", "run")

	cmd := execCommandFn(executablePath, args...)
	devNull, err := openFileFn(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer devNull.Close()

	cmd.Stdout = devNull
	cmd.Stderr = devNull
	cmd.Stdin = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start daemon process: %w", err)
	}
	if err := cmd.Process.Release(); err != nil {
		return fmt.Errorf("failed to release daemon process: %w", err)
	}

	deadline := nowFn().Add(backgroundWaitTimeout)
	for nowFn().Before(deadline) {
		running, _, err := daemonIsRunningFn("")
		if err == nil && running {
			return nil
		}
		sleepFn(backgroundPollDelay)
	}
	return fmt.Errorf("daemon process did not create pid file")
}
