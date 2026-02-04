# Herald (MVP)

`herald` is a minimal cross-platform CLI to send local desktop notifications.

License: GPL-3.0

## Features (v0.1)
- Local notifications
- macOS via `osascript`
- Linux via `notify-send` (experimental)
- Simple CLI with optional title/icon
- YAML config at `~/.heraldrc`

## Install
Build from source:

```bash
# from repo root
go build -o heraldev ./cmd/herald
```

Or using the Makefile:

```bash
# dev build (heraldev)
make build

# release build (herald)
make release
```

### Homebrew
Once you publish a tagged release (e.g., `v0.1.0`), you can install via:

```bash
brew tap lemomar/herald https://github.com/lemomar/herald
brew install herald
```

After installation, enable the shell hook so `herald` can capture the previous command's exit code:

```bash
# zsh
eval "$(herald hook --shell zsh)"

# bash
eval "$(herald hook --shell bash)"

# fish
herald hook --shell fish | source
```

## Usage

```bash
# basic
./herald "Build complete"

# with title
./herald "Build complete" --title "CI"

# verbose output
./herald "Build complete" --verbose

# with icon (Linux only)
./herald "Backup finished" --icon /path/to/icon.png

# install shell hook to use previous command's exit code automatically
# zsh/bash (use the full path or ./herald if not in PATH)
eval "$(./herald hook --shell zsh)"
# fish
./herald hook --shell fish | source

# or install permanently into your shell config
./herald hook --shell zsh --install
./herald hook --shell bash --install
./herald hook --shell fish --install

# after hook: herald uses previous exit code via HERALD_EXIT_CODE
# and previous command string via HERALD_PREV_CMD for the title fallback
command; herald
command; herald "Custom message"

# force derived success/failure message (requires exit code via hook or env)
command; herald --evaluate
```

## Config
Default config path: `~/.heraldrc`

Example:

```yaml
mode: greeting # or "evaluate"
defaults:
  message: "Hello from herald"
  title: "Herald"
  icon: "/path/to/icon.png"
```

Config precedence:
1. CLI flags
2. Config defaults
3. Built-in defaults

Built-in defaults:
- Title: `Herald`
- Message: `Hello from Herald`
- Icon: (none)

Flags:
- `--exit-code N` pass an exit code to derive a message
- `--evaluate` use success/failure derived message when exit code is available

Config:
- `mode: greeting|evaluate` (default: `greeting`)

Environment:
- `HERALD_EXIT_CODE` set by the hook to pass the previous command status
- `HERALD_PREV_CMD` set by the hook to pass the previous command string for title fallback
- `HERALD_HOOK=1` set by the hook so `herald` knows the hook is installed

## Exit Codes
- `0` on success
- `>0` on error (message printed to stderr)

## Notes
- macOS notifications are sent using `osascript`.
- Linux notifications are sent using `notify-send` (install `libnotify-bin` or equivalent).
- Windows is not supported in v0.1.
- macOS notifications do not support custom icons; `--icon` is ignored on macOS.
