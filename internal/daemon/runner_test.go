package daemon

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"herald/internal/config"
	"herald/internal/logs"
)

type fakeNotifier struct {
	mu    sync.Mutex
	calls []notifyCall
	ch    chan struct{}
	err   error
}

type notifyCall struct {
	message string
	title   string
	icon    string
}

func (f *fakeNotifier) Send(message, title, icon string) error {
	f.mu.Lock()
	f.calls = append(f.calls, notifyCall{message: message, title: title, icon: icon})
	f.mu.Unlock()
	if f.ch != nil {
		select {
		case f.ch <- struct{}{}:
		default:
		}
	}
	return f.err
}

func (f *fakeNotifier) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeNotifier) lastCall() (notifyCall, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return notifyCall{}, false
	}
	return f.calls[len(f.calls)-1], true
}

type failingConn struct{}

func (f *failingConn) ReadMessage() (int, []byte, error) {
	return websocket.TextMessage, nil, errors.New("read failed")
}

func (f *failingConn) Close() error {
	return nil
}

type scriptedConn struct {
	messages [][]byte
	index    int
}

func (s *scriptedConn) ReadMessage() (int, []byte, error) {
	if s.index >= len(s.messages) {
		return websocket.TextMessage, nil, errors.New("eof")
	}
	msg := s.messages[s.index]
	s.index++
	return websocket.TextMessage, msg, nil
}

func (s *scriptedConn) Close() error {
	return nil
}

func TestRunnerValidMessageAuthAndLog(t *testing.T) {
	upgrader := websocket.Upgrader{}
	authSeen := make(chan string, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authSeen <- r.Header.Get("Authorization")
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"title":"Build","message":"Done"}`))
		time.Sleep(100 * time.Millisecond)
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	storePath := filepath.Join(t.TempDir(), "logs.yaml")
	pidPath := filepath.Join(t.TempDir(), "daemon.pid")
	notifyCh := make(chan struct{}, 1)
	notifier := &fakeNotifier{ch: notifyCh}

	runner, err := NewRunner(Options{
		DaemonConfig: config.DaemonConfig{ServerURL: wsURL, Token: "secret", ReconnectSec: 1},
		Notifier:     notifier,
		StorePath:    storePath,
		PIDPath:      pidPath,
	})
	if err != nil {
		t.Fatalf("new runner failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runner.Run(ctx)
	}()

	select {
	case <-notifyCh:
		cancel()
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatalf("timed out waiting for notify")
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runner returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for runner exit")
	}

	if notifier.count() == 0 {
		t.Fatalf("expected notifier call")
	}

	select {
	case auth := <-authSeen:
		if auth != "Bearer secret" {
			t.Fatalf("expected bearer header, got %q", auth)
		}
	default:
		t.Fatalf("expected auth header")
	}

	store, err := logs.NewStore(storePath)
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}
	entries, err := store.Query(0, "")
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	found := false
	for _, entry := range entries {
		if entry.Event == "notify" && entry.Message == "Done" && entry.Title == "Build" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected notify entry in logs")
	}
}

func TestRunnerMalformedPayloadLogsError(t *testing.T) {
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{bad`))
		time.Sleep(100 * time.Millisecond)
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	storePath := filepath.Join(t.TempDir(), "logs.yaml")
	pidPath := filepath.Join(t.TempDir(), "daemon.pid")
	notifier := &fakeNotifier{}

	runner, err := NewRunner(Options{
		DaemonConfig: config.DaemonConfig{ServerURL: wsURL, ReconnectSec: 1},
		Notifier:     notifier,
		StorePath:    storePath,
		PIDPath:      pidPath,
	})
	if err != nil {
		t.Fatalf("new runner failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if err := runner.Run(ctx); err != nil {
		t.Fatalf("runner returned error: %v", err)
	}

	if notifier.count() != 0 {
		t.Fatalf("expected no notifier calls for malformed payload")
	}

	store, err := logs.NewStore(storePath)
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}
	entries, err := store.Query(0, "payload_invalid")
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("expected payload_invalid entry")
	}
}

func TestRunnerReconnectAttempts(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "logs.yaml")
	pidPath := filepath.Join(t.TempDir(), "daemon.pid")
	notifier := &fakeNotifier{}

	attempts := 0
	ctx, cancel := context.WithCancel(context.Background())
	runner, err := NewRunner(Options{
		DaemonConfig: config.DaemonConfig{ServerURL: "ws://example.test/ws", ReconnectSec: 1},
		Notifier:     notifier,
		StorePath:    storePath,
		PIDPath:      pidPath,
		Sleep:        func(_ time.Duration) {},
		DialContext: func(ctx context.Context, urlStr string, requestHeader http.Header) (wsConn, *http.Response, error) {
			attempts++
			if attempts == 1 {
				return nil, nil, errors.New("first dial failure")
			}
			cancel()
			return &failingConn{}, nil, nil
		},
	})
	if err != nil {
		t.Fatalf("new runner failed: %v", err)
	}

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("runner returned error: %v", err)
	}
	if attempts < 2 {
		t.Fatalf("expected reconnect attempts, got %d", attempts)
	}
}

func TestNewRunnerValidationAndDefaults(t *testing.T) {
	if _, err := NewRunner(Options{DaemonConfig: config.DaemonConfig{ServerURL: "http://bad"}}); err == nil {
		t.Fatalf("expected invalid scheme error")
	}

	runner, err := NewRunner(Options{DaemonConfig: config.DaemonConfig{ServerURL: "ws://example.test/ws", ReconnectSec: 0}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if runner.daemonConfig.ReconnectSec != 5 {
		t.Fatalf("expected default reconnect=5, got %d", runner.daemonConfig.ReconnectSec)
	}
}

func TestHandlePayloadMissingMessage(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "logs.yaml")
	pidPath := filepath.Join(t.TempDir(), "daemon.pid")
	notifier := &fakeNotifier{}
	runner, err := NewRunner(Options{
		DaemonConfig: config.DaemonConfig{ServerURL: "ws://example.test/ws", ReconnectSec: 1},
		Notifier:     notifier,
		StorePath:    storePath,
		PIDPath:      pidPath,
	})
	if err != nil {
		t.Fatalf("new runner failed: %v", err)
	}

	runner.handlePayload([]byte(`{"title":"x","message":"   "}`))
	if notifier.count() != 0 {
		t.Fatalf("expected no notifier call")
	}

	store, err := logs.NewStore(storePath)
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}
	entries, err := store.Query(0, "payload_invalid")
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("expected payload_invalid log")
	}
}

func TestHandlePayloadNotifyFailureLogged(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "logs.yaml")
	pidPath := filepath.Join(t.TempDir(), "daemon.pid")
	notifier := &fakeNotifier{err: errors.New("notify exploded")}
	runner, err := NewRunner(Options{
		DaemonConfig: config.DaemonConfig{ServerURL: "ws://example.test/ws", ReconnectSec: 1},
		Notifier:     notifier,
		StorePath:    storePath,
		PIDPath:      pidPath,
	})
	if err != nil {
		t.Fatalf("new runner failed: %v", err)
	}

	runner.handlePayload([]byte(`{"title":"Build","message":"Done"}`))
	store, err := logs.NewStore(storePath)
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}
	entries, err := store.Query(0, "notify_failed")
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("expected notify_failed log")
	}
}

func TestHandlePayloadDefaultTitleAndDarwinIconDrop(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "logs.yaml")
	pidPath := filepath.Join(t.TempDir(), "daemon.pid")
	notifier := &fakeNotifier{}
	runner, err := NewRunner(Options{
		DaemonConfig: config.DaemonConfig{ServerURL: "ws://example.test/ws", ReconnectSec: 1},
		Notifier:     notifier,
		StorePath:    storePath,
		PIDPath:      pidPath,
		OS:           "darwin",
	})
	if err != nil {
		t.Fatalf("new runner failed: %v", err)
	}

	runner.handlePayload([]byte(`{"message":"Done","icon":"/tmp/icon.png"}`))
	call, ok := notifier.lastCall()
	if !ok {
		t.Fatalf("expected notifier call")
	}
	if call.title != "Herald" {
		t.Fatalf("expected default title, got %q", call.title)
	}
	if call.icon != "" {
		t.Fatalf("expected darwin icon to be dropped, got %q", call.icon)
	}
}

func TestConsumeConnectionNoTokenHeader(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "logs.yaml")
	pidPath := filepath.Join(t.TempDir(), "daemon.pid")
	notifier := &fakeNotifier{}
	headersSeen := make(http.Header)
	runner, err := NewRunner(Options{
		DaemonConfig: config.DaemonConfig{ServerURL: "ws://example.test/ws", ReconnectSec: 1, Token: "   "},
		Notifier:     notifier,
		StorePath:    storePath,
		PIDPath:      pidPath,
		DialContext: func(ctx context.Context, urlStr string, requestHeader http.Header) (wsConn, *http.Response, error) {
			headersSeen = requestHeader
			return &scriptedConn{messages: [][]byte{[]byte(`{"message":"done"}`)}}, nil, nil
		},
	})
	if err != nil {
		t.Fatalf("new runner failed: %v", err)
	}

	err = runner.consumeConnection(context.Background())
	if err == nil || !strings.Contains(err.Error(), "websocket read failed") {
		t.Fatalf("expected read error, got %v", err)
	}
	if got := headersSeen.Get("Authorization"); got != "" {
		t.Fatalf("expected no auth header, got %q", got)
	}
}

func TestRunReturnsAlreadyRunning(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "daemon.pid")
	if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o644); err != nil {
		t.Fatalf("write pid failed: %v", err)
	}

	runner, err := NewRunner(Options{
		DaemonConfig: config.DaemonConfig{ServerURL: "ws://example.test/ws", ReconnectSec: 1},
		Notifier:     &fakeNotifier{},
		StorePath:    filepath.Join(t.TempDir(), "logs.yaml"),
		PIDPath:      pidPath,
		DialContext: func(ctx context.Context, urlStr string, requestHeader http.Header) (wsConn, *http.Response, error) {
			return &failingConn{}, nil, nil
		},
	})
	if err != nil {
		t.Fatalf("new runner failed: %v", err)
	}

	err = runner.Run(context.Background())
	if err == nil || !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("expected ErrAlreadyRunning, got %v", err)
	}
}

func TestSleepWithContextCancelled(t *testing.T) {
	runner, err := NewRunner(Options{
		DaemonConfig: config.DaemonConfig{ServerURL: "ws://example.test/ws", ReconnectSec: 1},
		Notifier:     &fakeNotifier{},
		StorePath:    filepath.Join(t.TempDir(), "logs.yaml"),
		PIDPath:      filepath.Join(t.TempDir(), "daemon.pid"),
		Sleep:        func(time.Duration) { time.Sleep(50 * time.Millisecond) },
	})
	if err != nil {
		t.Fatalf("new runner failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if runner.sleepWithContext(ctx, 100*time.Millisecond) {
		t.Fatalf("expected sleepWithContext to stop on canceled context")
	}
	if !runner.sleepWithContext(context.Background(), 0) {
		t.Fatalf("expected zero-delay sleep to succeed")
	}
}
