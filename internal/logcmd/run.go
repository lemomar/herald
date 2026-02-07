package logcmd

import (
	"fmt"
	"io"
	"strings"
	"time"

	"herald/internal/logs"
)

type Options struct {
	Last   int
	Filter string
	Path   string
}

var newStoreFn = logs.NewStore

func Run(out io.Writer, opts Options) error {
	if opts.Last < 0 {
		return fmt.Errorf("--last must be >= 0")
	}

	store, err := newStoreFn(opts.Path)
	if err != nil {
		return err
	}

	entries, err := store.Query(opts.Last, opts.Filter)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		timestamp := entry.Timestamp.UTC().Format(time.RFC3339Nano)
		title := strings.TrimSpace(entry.Title)
		if title == "" {
			title = "-"
		}
		level := strings.TrimSpace(entry.Level)
		if level == "" {
			level = "info"
		}
		message := strings.TrimSpace(entry.Message)
		fmt.Fprintf(out, "%s [%s] %s: %s\n", timestamp, level, title, message)
	}
	return nil
}
