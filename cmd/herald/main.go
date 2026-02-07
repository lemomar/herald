package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"herald/internal/cli"
	"herald/internal/config"
	"herald/internal/logcmd"
	"herald/internal/notify"
)

var (
	exitFn = os.Exit

	getenvFn            = os.Getenv
	currentOSFn         = func() string { return runtime.GOOS }
	parseCLIFn          = cli.Parse
	defaultConfigPathFn = config.DefaultPath
	loadConfigFn        = config.Load
	parseExitCodeFn     = cli.ParseExitCodeValue
	validateIconFn      = notify.ValidateIcon
	notifyForOSFn       = notify.ForOS
	executablePathFn    = os.Executable
	userHomeDirFn       = os.UserHomeDir
	openHookFileFn      = os.OpenFile
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "logs" {
		if err := runLogsAlias(os.Args[2:], os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			exitFn(2)
		}
		return
	}

	if len(os.Args) > 1 && os.Args[1] == "hook" {
		if err := printHook(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			exitFn(2)
		}
		return
	}

	if getenvFn("HERALD_HOOK") != "1" {
		fmt.Fprintln(os.Stderr, "tip: install the shell hook for automatic exit codes: `herald hook --shell zsh --install`")
	}

	opts, err := parseCLIFn(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		exitFn(2)
	}

	cfgPath := opts.ConfigPath
	if cfgPath == "" {
		defaultPath, err := defaultConfigPathFn()
		if err != nil {
			fmt.Fprintln(os.Stderr, "failed to resolve config path:", err)
			exitFn(1)
		}
		cfgPath = defaultPath
	}

	cfg, _, err := loadConfigFn(cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		exitFn(1)
	}

	merged := cli.MergeWithConfig(opts, cfg)
	if merged.ExitCode == nil {
		if env := getenvFn("HERALD_EXIT_CODE"); env != "" {
			code, err := parseExitCodeFn(env)
			if err != nil {
				fmt.Fprintln(os.Stderr, err.Error())
				exitFn(2)
			}
			merged.ExitCode = &code
		}
	}
	evaluateActive := merged.Evaluate || cfg.Mode == "evaluate"
	message, explicit := cli.ResolveMessage(merged, cfg, evaluateActive)
	merged.Message = message
	prevCmd := strings.TrimSpace(getenvFn("HERALD_PREV_CMD"))
	prevCmd = sanitizePrevCommand(prevCmd)
	merged.Title = cli.ResolveTitle(merged, cfg, prevCmd, explicit, evaluateActive)

	currentOS := currentOSFn()
	if currentOS == "darwin" && merged.Icon != "" {
		if merged.Verbose {
			fmt.Fprintln(os.Stderr, "warning: macOS notifications do not support custom icons; ignoring --icon")
		}
		merged.Icon = ""
	}

	if merged.Icon != "" {
		if err := validateIconFn(merged.Icon); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			exitFn(1)
		}
	}

	notifier, err := notifyForOSFn(currentOS)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		exitFn(1)
	}

	if err := notifier.Send(merged.Message, merged.Title, merged.Icon); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		exitFn(1)
	}

	if merged.Verbose {
		fmt.Fprintln(os.Stdout, "notification sent")
	}
}

func sanitizePrevCommand(value string) string {
	if value == "" {
		return value
	}
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	parts := strings.Split(trimmed, ";")
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		p := strings.TrimSpace(part)
		if p != "" {
			clean = append(clean, p)
		}
	}
	if len(clean) == 0 {
		return ""
	}
	last := clean[len(clean)-1]
	if strings.Contains(strings.ToLower(last), "herald") {
		if len(clean) < 2 {
			return ""
		}
		last = clean[len(clean)-2]
	}
	fields := strings.Fields(last)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func printHook(args []string) error {
	shell := ""
	install := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--shell" {
			if i+1 >= len(args) {
				return fmt.Errorf("flag --shell requires a value")
			}
			shell = args[i+1]
			i++
			continue
		}
		if strings.HasPrefix(arg, "--shell=") {
			shell = strings.TrimPrefix(arg, "--shell=")
			continue
		}
		if arg == "--install" {
			install = true
			continue
		}
		return fmt.Errorf("unknown flag for hook: %s", arg)
	}

	if shell == "" {
		if env := getenvFn("SHELL"); env != "" {
			if strings.Contains(env, "zsh") {
				shell = "zsh"
			} else if strings.Contains(env, "bash") {
				shell = "bash"
			} else if strings.Contains(env, "fish") {
				shell = "fish"
			}
		}
	}
	if shell == "" {
		shell = "zsh"
	}

	exe, err := executablePathFn()
	if err != nil {
		return fmt.Errorf("failed to resolve executable path: %w", err)
	}

	script, err := hookScript(shell, exe)
	if err != nil {
		return err
	}

	if install {
		return installHook(shell, script)
	}

	fmt.Println(script)
	return nil
}

func runLogsAlias(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	last := fs.Int("last", 0, "Show only the last N entries")
	filter := fs.String("filter", "", "Filter logs by text")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument(s): %s", strings.Join(fs.Args(), " "))
	}

	return logcmd.Run(out, logcmd.Options{
		Last:   *last,
		Filter: *filter,
	})
}

func hookScript(shell, exe string) (string, error) {
	switch shell {
	case "zsh", "bash":
		preexec := strings.Join([]string{
			"__herald_preexec(){",
			"  local cmd=\"$1\"",
			"  if [ -z \"$cmd\" ]; then return; fi",
			"  case \"$cmd\" in",
			"    *herald*)",
			"      if [[ \"$cmd\" == *\";\"* ]]; then",
			"        local before=${cmd%%herald*}",
			"        HERALD_LAST_CMD=\"$before\"",
			"        HERALD_HAS_LAST_CMD=1",
			"      fi",
			"      return ;;",
			"  esac",
			"  HERALD_LAST_CMD=\"$cmd\"",
			"  HERALD_HAS_LAST_CMD=1",
			"}",
		}, "\n")

		if shell == "zsh" {
			preexec = strings.Join([]string{
				"autoload -Uz add-zsh-hook 2>/dev/null || true",
				preexec,
				"HERALD_HAS_LAST_CMD=0",
				"preexec(){ __herald_preexec \"$1\"; }",
				"add-zsh-hook preexec __herald_preexec 2>/dev/null || true",
			}, "\n")
		} else {
			preexec = strings.Join([]string{
				preexec,
				"HERALD_HAS_LAST_CMD=0",
				"__herald_debug(){ if [ \"$BASH_COMMAND\" = \"__herald_debug\" ]; then return; fi; __herald_preexec \"$BASH_COMMAND\"; }",
				"trap '__herald_debug' DEBUG",
			}, "\n")
		}

		wrapper := fmt.Sprintf(
			"herald(){ local code=$?; local prev_cmd=\"\"; "+
				"if [ \"$HERALD_HAS_LAST_CMD\" = \"1\" ]; then "+
				"  prev_cmd=\"$HERALD_LAST_CMD\"; "+
				"  HERALD_HOOK=1 HERALD_EXIT_CODE=\"$code\" HERALD_PREV_CMD=\"$prev_cmd\" command %q \"$@\"; "+
				"else HERALD_HOOK=1 command %q \"$@\"; fi; "+
				"HERALD_LAST_CMD=\"\"; HERALD_HAS_LAST_CMD=0; unset HERALD_PREV_CMD HERALD_EXIT_CODE; }",
			exe,
			exe,
		)
		return preexec + "\n" + wrapper, nil
	case "fish":
		return fmt.Sprintf(
			"function __herald_preexec --on-event fish_preexec\n"+
				"  if not set -q HERALD_HAS_LAST_CMD\n"+
				"    set -g HERALD_HAS_LAST_CMD 0\n"+
				"  end\n"+
				"  set -l cmd $argv\n"+
				"  if test -z \"$cmd\"; return; end\n"+
				"  if string match -rq 'herald' -- \"$cmd\"\n"+
				"    if string match -rq ';' -- \"$cmd\"\n"+
				"      set -l parts (string split -m 1 ';' -- \"$cmd\")\n"+
				"      set -g HERALD_LAST_CMD $parts[1]\n"+
				"      set -g HERALD_HAS_LAST_CMD 1\n"+
				"    end\n"+
				"    return\n"+
				"  end\n"+
				"  set -g HERALD_LAST_CMD \"$cmd\"\n"+
				"  set -g HERALD_HAS_LAST_CMD 1\n"+
				"end\n"+
				"function herald\n"+
				"  set -l code $status\n"+
				"  set -l prev_cmd \"\"\n"+
				"  if test \"$HERALD_HAS_LAST_CMD\" = \"1\"\n"+
				"    set prev_cmd $HERALD_LAST_CMD\n"+
				"    set -x HERALD_HOOK 1\n"+
				"    set -x HERALD_EXIT_CODE $code\n"+
				"    set -x HERALD_PREV_CMD \"$prev_cmd\"\n"+
				"    command %q $argv\n"+
				"  else\n"+
				"    set -x HERALD_HOOK 1\n"+
				"    command %q $argv\n"+
				"  end\n"+
				"  set -g HERALD_LAST_CMD \"\"\n"+
				"  set -g HERALD_HAS_LAST_CMD 0\n"+
				"  set -e HERALD_PREV_CMD\n"+
				"  set -e HERALD_EXIT_CODE\n"+
				"end",
			exe,
			exe,
		), nil
	default:
		return "", fmt.Errorf("unsupported shell for hook: %s", shell)
	}
}

func installHook(shell, script string) error {
	home, err := userHomeDirFn()
	if err != nil {
		return fmt.Errorf("failed to resolve home dir: %w", err)
	}
	var rcPath string
	switch shell {
	case "zsh":
		rcPath = filepath.Join(home, ".zshrc")
	case "bash":
		rcPath = filepath.Join(home, ".bashrc")
	case "fish":
		rcPath = filepath.Join(home, ".config", "fish", "config.fish")
	default:
		return fmt.Errorf("unsupported shell for install: %s", shell)
	}

	startMarker := "# >>> herald hook >>>"
	endMarker := "# <<< herald hook <<<"

	existing, _ := os.ReadFile(rcPath)
	content := string(existing)
	block := fmt.Sprintf("\n%s\n%s\n%s\n", startMarker, script, endMarker)

	if strings.Contains(content, startMarker) && strings.Contains(content, endMarker) {
		before := strings.Split(content, startMarker)[0]
		after := strings.Split(content, endMarker)
		content = before + block + after[1]
	} else {
		content = content + block
	}

	if err := os.MkdirAll(filepath.Dir(rcPath), 0o755); err != nil {
		return fmt.Errorf("failed to create config dir: %w", err)
	}
	f, err := openHookFileFn(rcPath, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("failed to open rc file: %w", err)
	}
	defer f.Close()

	if _, err := f.WriteString(content); err != nil {
		return fmt.Errorf("failed to write hook: %w", err)
	}
	return nil
}
