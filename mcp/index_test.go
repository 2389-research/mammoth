// ABOUTME: Tests for RunIndex disk-backed persistence.
// ABOUTME: Validates save, load, and round-trip of run metadata.
package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIndexSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	idx := NewRunIndex(dir)
	entry := &IndexEntry{
		RunID:         "abc12345abc12345",
		Source:        "digraph { start -> end }",
		Config:        RunConfig{RetryPolicy: "standard"},
		Status:        string(StatusCompleted),
		CheckpointDir: filepath.Join(dir, "abc12345abc12345", "checkpoint"),
	}
	if err := idx.Save(entry); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := idx.Load("abc12345abc12345")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.RunID != entry.RunID {
		t.Errorf("RunID: got %q, want %q", got.RunID, entry.RunID)
	}
	if got.Source != entry.Source {
		t.Errorf("Source mismatch")
	}
	if got.Config.RetryPolicy != "standard" {
		t.Errorf("Config.RetryPolicy: got %q", got.Config.RetryPolicy)
	}
}

func TestIndexLoadNotFound(t *testing.T) {
	dir := t.TempDir()
	idx := NewRunIndex(dir)
	_, err := idx.Load("deadbeefdeadbeef")
	if err == nil {
		t.Fatal("expected error for missing run")
	}
}

func TestIndexValidateRunID(t *testing.T) {
	dir := t.TempDir()
	idx := NewRunIndex(dir)

	tests := []struct {
		name  string
		runID string
	}{
		{"empty", ""},
		{"path traversal", "../etc/passwd"},
		{"absolute path", "/tmp/evil"},
		{"has slash", "abc/def"},
		{"too short", "abc"},
		{"non-hex chars", "run_name_1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := idx.Save(&IndexEntry{RunID: tt.runID, Source: "digraph { a -> b }"})
			if err == nil {
				t.Errorf("expected error for run ID %q", tt.runID)
			}
			_, err = idx.Load(tt.runID)
			if err == nil {
				t.Errorf("expected error for run ID %q", tt.runID)
			}
		})
	}
}

func TestIndexListEntries(t *testing.T) {
	dir := t.TempDir()
	idx := NewRunIndex(dir)
	if err := idx.Save(&IndexEntry{RunID: "aaaa1111bbbb2222", Source: "digraph { a -> b }"}); err != nil {
		t.Fatalf("save run1: %v", err)
	}
	if err := idx.Save(&IndexEntry{RunID: "cccc3333dddd4444", Source: "digraph { c -> d }"}); err != nil {
		t.Fatalf("save run2: %v", err)
	}
	entries, err := idx.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}
}

func TestIndexSaveCreatesRunDir(t *testing.T) {
	dir := t.TempDir()
	idx := NewRunIndex(dir)
	if err := idx.Save(&IndexEntry{RunID: "aaaa1111bbbb2222", Source: "digraph { a -> b }"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	runDir := filepath.Join(dir, "aaaa1111bbbb2222")
	info, err := os.Stat(runDir)
	if err != nil {
		t.Fatalf("run dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}
	sourceFile := filepath.Join(runDir, "source.dot")
	data, err := os.ReadFile(sourceFile)
	if err != nil {
		t.Fatalf("source.dot not written: %v", err)
	}
	if string(data) != "digraph { a -> b }" {
		t.Errorf("source.dot content mismatch: got %q", string(data))
	}
}
