package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"herald/internal/config"
	"herald/internal/logs"
	"herald/internal/notify"
)

type NotifySender interface {
	Send(message, title, icon string) error
}

type wsConn interface {
	ReadMessage() (messageType int, p []byte, err error)
	Close() error
}

type DialContextFunc func(ctx context.Context, urlStr string, requestHeader http.Header) (wsConn, *http.Response, error)

type Options struct {
	DaemonConfig config.DaemonConfig
	Notifier     NotifySender
	StorePath    string
	PIDPath      string
	Store        *logs.Store
	DialContext  DialContextFunc
	Sleep        func(time.Duration)
	Now          func() time.Time
	OS           string
}

type Runner struct {
	daemonConfig config.DaemonConfig
	notifier     NotifySender
	store        *logs.Store
	pidPath      string
	dialContext  DialContextFunc
	sleep        func(time.Duration)
	now          func() time.Time
	goos         string
}

type notifyPayload struct {
	Title   string `json:"title"`
	Message string `json:"message"`
	Icon    string `json:"icon"`
}

func NewRunner(opts Options) (*Runner, error) {
	daemonCfg := opts.DaemonConfig
	if daemonCfg.ReconnectSec <= 0 {
		daemonCfg.ReconnectSec = 5
	}
	if err := config.ValidateDaemon(daemonCfg); err != nil {
		return nil, err
	}

	notifier := opts.Notifier
	if notifier == nil {
		defaultNotifier, err := notify.ForOS(runtime.GOOS)
		if err != nil {
			return nil, err
		}
		notifier = defaultNotifier
	}

	store := opts.Store
	if store == nil {
		defaultStore, err := logs.NewStore(opts.StorePath)
		if err != nil {
			return nil, err
		}
		store = defaultStore
	}

	pidPath, err := ResolvePIDPath(opts.PIDPath)
	if err != nil {
		return nil, err
	}

	dialContext := opts.DialContext
	if dialContext == nil {
		dialer := websocket.Dialer{}
		dialContext = func(ctx context.Context, urlStr string, requestHeader http.Header) (wsConn, *http.Response, error) {
			return dialer.DialContext(ctx, urlStr, requestHeader)
		}
	}

	sleep := opts.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}

	now := opts.Now
	if now == nil {
		now = time.Now
	}

	goos := opts.OS
	if goos == "" {
		goos = runtime.GOOS
	}

	return &Runner{
		daemonConfig: daemonCfg,
		notifier:     notifier,
		store:        store,
		pidPath:      pidPath,
		dialContext:  dialContext,
		sleep:        sleep,
		now:          now,
		goos:         goos,
	}, nil
}

func (r *Runner) Run(ctx context.Context) error {
	if err := EnsureSingleInstance(r.pidPath); err != nil {
		return err
	}
	defer CleanupPID(r.pidPath)

	r.appendLog("lifecycle", "daemon_start", "Herald daemon started", "", "info")
	defer r.appendLog("lifecycle", "daemon_stop", "Herald daemon stopped", "", "info")

	for {
		if ctx.Err() != nil {
			return nil
		}

		err := r.consumeConnection(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			r.appendLog("connection", "ws_error", err.Error(), "", "error")
		}

		r.appendLog("connection", "ws_reconnect", "Reconnecting websocket", "", "info")
		if !r.sleepWithContext(ctx, time.Duration(r.daemonConfig.ReconnectSec)*time.Second) {
			return nil
		}
	}
}

func (r *Runner) consumeConnection(ctx context.Context) error {
	headers := make(http.Header)
	if token := strings.TrimSpace(r.daemonConfig.Token); token != "" {
		headers.Set("Authorization", "Bearer "+token)
	}

	conn, _, err := r.dialContext(ctx, r.daemonConfig.ServerURL, headers)
	if err != nil {
		return fmt.Errorf("websocket dial failed: %w", err)
	}
	defer conn.Close()

	r.appendLog("connection", "ws_connected", "Websocket connected", "", "info")

	for {
		if ctx.Err() != nil {
			return nil
		}

		_, payload, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("websocket read failed: %w", err)
		}
		r.handlePayload(payload)
	}
}

func (r *Runner) handlePayload(payload []byte) {
	var incoming notifyPayload
	if err := json.Unmarshal(payload, &incoming); err != nil {
		r.appendLog("message", "payload_invalid", "Invalid websocket payload", string(payload), "error")
		return
	}

	incoming.Message = strings.TrimSpace(incoming.Message)
	if incoming.Message == "" {
		r.appendLog("message", "payload_invalid", "Missing required message field", string(payload), "error")
		return
	}

	title := strings.TrimSpace(incoming.Title)
	if title == "" {
		title = "Herald"
	}
	icon := strings.TrimSpace(incoming.Icon)
	if r.goos == "darwin" {
		icon = ""
	}

	if err := r.notifier.Send(incoming.Message, title, icon); err != nil {
		r.appendLog("notify", "notify_failed", err.Error(), incoming.Message, "error")
		return
	}

	r.appendLog("notify", "notify", incoming.Message, title, "info")
}

func (r *Runner) appendLog(source, event, message, title, level string) {
	_ = r.store.Append(logs.Entry{
		Timestamp: r.now().UTC(),
		Source:    source,
		Event:     event,
		Title:     title,
		Message:   message,
		Level:     level,
	})
}

func (r *Runner) sleepWithContext(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		return true
	}
	done := make(chan struct{})
	go func() {
		r.sleep(delay)
		close(done)
	}()
	select {
	case <-ctx.Done():
		return false
	case <-done:
		return true
	}
}
