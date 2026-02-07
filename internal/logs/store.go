package logs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type tempFile interface {
	Name() string
	Write(p []byte) (n int, err error)
	Close() error
}

var (
	userHomeDirFn = os.UserHomeDir
	readFileFn    = os.ReadFile
	mkdirAllFn    = os.MkdirAll
	createTempFn  = func(dir, pattern string) (tempFile, error) { return os.CreateTemp(dir, pattern) }
	renameFn      = os.Rename
	removeFn      = os.Remove
)

type Entry struct {
	Timestamp time.Time `yaml:"timestamp"`
	Source    string    `yaml:"source"`
	Event     string    `yaml:"event"`
	Title     string    `yaml:"title"`
	Message   string    `yaml:"message"`
	Level     string    `yaml:"level"`
}

type Store struct {
	path string
}

func DefaultDir() (string, error) {
	home, err := userHomeDirFn()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".herald"), nil
}

func DefaultPath() (string, error) {
	dir, err := DefaultDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "logs.yaml"), nil
}

func NewStore(path string) (*Store, error) {
	resolved := path
	if resolved == "" {
		defaultPath, err := DefaultPath()
		if err != nil {
			return nil, err
		}
		resolved = defaultPath
	}
	return &Store{path: resolved}, nil
}

func (s *Store) Path() string {
	return s.path
}

func (s *Store) Append(entry Entry) error {
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	} else {
		entry.Timestamp = entry.Timestamp.UTC()
	}

	entries, err := s.ReadAll()
	if err != nil {
		return err
	}
	entries = append(entries, entry)
	return s.writeAll(entries)
}

func (s *Store) ReadAll() ([]Entry, error) {
	data, err := readFileFn(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return []Entry{}, nil
		}
		return nil, fmt.Errorf("failed to read logs %s: %w", s.path, err)
	}
	if strings.TrimSpace(string(data)) == "" {
		return []Entry{}, nil
	}

	var entries []Entry
	if err := yaml.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("invalid yaml in %s: %w", s.path, err)
	}
	if entries == nil {
		return []Entry{}, nil
	}
	return entries, nil
}

func (s *Store) Query(last int, filter string) ([]Entry, error) {
	entries, err := s.ReadAll()
	if err != nil {
		return nil, err
	}

	if filter != "" {
		needle := strings.ToLower(filter)
		filtered := make([]Entry, 0, len(entries))
		for _, entry := range entries {
			haystack := strings.ToLower(entry.Title + "\n" + entry.Message + "\n" + entry.Event)
			if strings.Contains(haystack, needle) {
				filtered = append(filtered, entry)
			}
		}
		entries = filtered
	}

	if last > 0 && len(entries) > last {
		entries = entries[len(entries)-last:]
	}

	return entries, nil
}

func (s *Store) writeAll(entries []Entry) error {
	dir := filepath.Dir(s.path)
	if err := mkdirAllFn(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create log dir %s: %w", dir, err)
	}

	data, err := yaml.Marshal(entries)
	if err != nil {
		return fmt.Errorf("failed to marshal logs: %w", err)
	}

	tmpFile, err := createTempFn(dir, "logs-*.yaml")
	if err != nil {
		return fmt.Errorf("failed to create temp log file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer removeFn(tmpPath)

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to write temp log file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp log file: %w", err)
	}
	if err := renameFn(tmpPath, s.path); err != nil {
		return fmt.Errorf("failed to commit logs: %w", err)
	}
	return nil
}
