// ABOUTME: Append-only JSONL event log for durable event storage.
// ABOUTME: Provides crash-safe append, sequential replay, and repair for truncated files.
package store

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/2389-research/mammoth/spec/core"
)

// JsonlLog is an append-only JSONL event log backed by a file.
// Each line is a single JSON-serialized Event followed by a newline.
type JsonlLog struct {
	path string
	file *os.File
}

// OpenJsonl opens (or creates) a JSONL log file at the given path.
// Creates parent directories if they do not exist.
// The file is opened in append mode.
func OpenJsonl(path string) (*JsonlLog, error) {
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, fmt.Errorf("create parent dirs: %w", err)
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open jsonl file: %w", err)
	}

	return &JsonlLog{path: path, file: file}, nil
}

// Path returns the path to the underlying JSONL file.
func (l *JsonlLog) Path() string {
	return l.path
}

// Append serializes a single event as one JSON line, writes it with a
// trailing newline, and fsyncs to disk.
func (l *JsonlLog) Append(event *core.Event) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	line := append(data, '\n')
	if _, err := l.file.Write(line); err != nil {
		return fmt.Errorf("write event line: %w", err)
	}

	if err := l.file.Sync(); err != nil {
		return fmt.Errorf("fsync: %w", err)
	}

	return nil
}

// Close closes the underlying file.
func (l *JsonlLog) Close() error {
	return l.file.Close()
}

// ReplayJsonl reads all events from a JSONL file, returning them in order.
// Empty lines are skipped. Returns an empty slice for empty files.
func ReplayJsonl(path string) ([]core.Event, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open jsonl for replay: %w", err)
	}
	defer func() { _ = file.Close() }()

	var events []core.Event
	scanner := bufio.NewScanner(file)
	// Increase scanner buffer for potentially large event lines.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event core.Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return nil, fmt.Errorf("parse event line: %w", err)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan jsonl file: %w", err)
	}

	return events, nil
}

// RepairJsonl repairs a potentially corrupted JSONL file by keeping only
// complete, parseable lines and truncating any partial trailing data.
// Uses atomic temp-file + fsync + rename to prevent data loss on crash.
// Returns the count of valid events retained.
func RepairJsonl(path string) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open jsonl for repair: %w", err)
	}

	var validLines []string
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// Only keep lines that parse as valid Event JSON
		var event core.Event
		if json.Unmarshal([]byte(line), &event) == nil {
			validLines = append(validLines, scanner.Text())
		}
	}
	if err := scanner.Err(); err != nil {
		_ = file.Close()
		return 0, fmt.Errorf("scan jsonl for repair: %w", err)
	}
	_ = file.Close()

	count := len(validLines)

	// Write valid lines to a temp file, fsync, then atomically rename
	tmpPath := path + ".tmp"
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		return 0, fmt.Errorf("create temp file: %w", err)
	}

	for _, line := range validLines {
		if _, err := fmt.Fprintln(tmpFile, line); err != nil {
			_ = tmpFile.Close()
			_ = os.Remove(tmpPath)
			return 0, fmt.Errorf("write valid line: %w", err)
		}
	}

	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return 0, fmt.Errorf("fsync temp file: %w", err)
	}
	_ = tmpFile.Close()

	// Atomic rename over the original
	if err := os.Rename(tmpPath, path); err != nil {
		return 0, fmt.Errorf("rename temp to original: %w", err)
	}

	// Fsync the parent directory to ensure the rename metadata is durable.
	parent := filepath.Dir(path)
	if dir, err := os.Open(parent); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}

	return count, nil
}
