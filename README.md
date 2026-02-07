# Herald

`herald` provides local desktop notifications, and `heraldctl` manages a background daemon that receives notification events over WebSocket.

License: GPL-3.0

## Binaries
- `herald`: one-shot local notifications + shell hook helpers + `logs` alias
- `heraldctl`: daemon lifecycle commands (`start`, `stop`, `status`, `restart`, `logs`)

## Build
```bash
# dev
make build

# release artifacts in repo root
make release
```

## Install shell hook (`herald`)
```bash
# zsh
eval "$(herald hook --shell zsh)"

# bash
eval "$(herald hook --shell bash)"

# fish
herald hook --shell fish | source
```

Or install permanently:
```bash
herald hook --shell zsh --install
herald hook --shell bash --install
herald hook --shell fish --install
```

## Usage

### One-shot notification
```bash
herald "Build complete"
herald "Build complete" --title "CI"
herald "Backup finished" --icon /path/to/icon.png
```

### Daemon control
```bash
heraldctl start
heraldctl status
heraldctl restart
heraldctl stop
```

### Log history
```bash
# primary
heraldctl logs --last 50 --filter build

# compatibility alias
herald logs --last 50 --filter build
```

## Config (`~/.heraldrc`)
```yaml
mode: greeting # or evaluate
defaults:
  message: "Hello from herald"
  title: "Herald"
  icon: "/path/to/icon.png"

daemon:
  server_url: "wss://example/ws"
  token: "secret"
  reconnect_sec: 5
```

### Daemon config rules
- `daemon.server_url` is required when starting daemon
- supported URL schemes: `ws`, `wss`
- `daemon.reconnect_sec <= 0` defaults to `5`
- if `daemon.token` is set, daemon sends `Authorization: Bearer <token>` during WS handshake

## WebSocket event contract
Daemon expects JSON text frames like:
```json
{"title":"Build","message":"Done","icon":"/optional/icon.png"}
```

Rules:
- `message` is required
- `title` and `icon` are optional
- unknown fields are ignored
- invalid payloads are skipped and logged as errors

## Service integration
`heraldctl start` is service-first and uses user services when available.

- macOS: `~/Library/LaunchAgents/com.herald.daemon.plist` via `launchctl`
- Linux: `~/.config/systemd/user/herald-daemon.service` via `systemctl --user`
- Fallback: background process with PID file when service manager is unavailable

## Logs and PID files
- History: `~/.herald/logs.yaml`
- PID file: `~/.herald/daemon.pid`

Log records are append-only YAML entries with:
- `timestamp` (`RFC3339Nano`, UTC)
- `source`
- `event`
- `title`
- `message`
- `level`

## Notes
- macOS notifications use `osascript`
- Linux notifications use `notify-send` (install `libnotify-bin` or equivalent)
- macOS ignores custom icons
- Windows is not supported
