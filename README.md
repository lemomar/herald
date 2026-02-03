# Herald (MVP)

`herald` is a minimal cross-platform CLI to send local desktop notifications.

## Features (v0.1)
- Local notifications
- macOS via `osascript`
- Linux via `notify-send`
- Simple CLI with optional title/icon
- YAML config at `~/.heraldrc`

## Install
Build from source:

```bash
# from repo root
go build -o herald ./cmd/herald
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
```

## Config
Default config path: `~/.heraldrc`

Example:

```yaml
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
- Message: (none)
- Icon: (none)

## Exit Codes
- `0` on success
- `>0` on error (message printed to stderr)

## Notes
- macOS notifications are sent using `osascript`.
- Linux notifications are sent using `notify-send` (install `libnotify-bin` or equivalent).
- Windows is not supported in v0.1.
- macOS notifications do not support custom icons; `--icon` is ignored on macOS.
