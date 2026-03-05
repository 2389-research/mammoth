// ABOUTME: Tests for checkpoint serialization and deserialization.
// ABOUTME: Covers round-trip save/load and error handling for missing files.
package attractor

import (
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewCheckpoint(t *testing.T) {
	ctx := NewContext()
	ctx.Set("model", "gpt-4")
	ctx.AppendLog("started")

	completed := []string{"node_a", "node_b"}
	retries := map[string]int{"node_b": 2}

	cp := NewCheckpoint(ctx, "node_c", completed, retries)

	if cp.CurrentNode != "node_c" {
		t.Errorf("expected CurrentNode 'node_c', got %q", cp.CurrentNode)
	}
	if len(cp.CompletedNodes) != 2 {
		t.Errorf("expected 2 completed nodes, got %d", len(cp.CompletedNodes))
	}
	if cp.NodeRetries["node_b"] != 2 {
		t.Errorf("expected node_b retries=2, got %d", cp.NodeRetries["node_b"])
	}
	if cp.ContextValues["model"] != "gpt-4" {
		t.Errorf("expected context model='gpt-4', got %v", cp.ContextValues["model"])
	}
	if len(cp.Logs) != 1 || cp.Logs[0] != "started" {
		t.Errorf("expected logs=['started'], got %v", cp.Logs)
	}
	if cp.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}
}

func TestCheckpointSaveLoad(t *testing.T) {
	ctx := NewContext()
	ctx.Set("temperature", "0.7")
	ctx.Set("max_tokens", "4096")
	ctx.AppendLog("checkpoint test log")

	completed := []string{"start", "process"}
	retries := map[string]int{"process": 1}

	original := NewCheckpoint(ctx, "review", completed, retries)

	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")

	if err := original.Save(path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("checkpoint file was not created")
	}

	loaded, err := LoadCheckpoint(path)
	if err != nil {
		t.Fatalf("LoadCheckpoint failed: %v", err)
	}

	if loaded.CurrentNode != original.CurrentNode {
		t.Errorf("CurrentNode mismatch: got %q, want %q", loaded.CurrentNode, original.CurrentNode)
	}
	if len(loaded.CompletedNodes) != len(original.CompletedNodes) {
		t.Errorf("CompletedNodes length mismatch: got %d, want %d", len(loaded.CompletedNodes), len(original.CompletedNodes))
	}
	for i, node := range original.CompletedNodes {
		if loaded.CompletedNodes[i] != node {
			t.Errorf("CompletedNodes[%d] mismatch: got %q, want %q", i, loaded.CompletedNodes[i], node)
		}
	}
	if loaded.NodeRetries["process"] != 1 {
		t.Errorf("NodeRetries['process'] mismatch: got %d, want 1", loaded.NodeRetries["process"])
	}
	if loaded.ContextValues["temperature"] != "0.7" {
		t.Errorf("ContextValues['temperature'] mismatch: got %v, want '0.7'", loaded.ContextValues["temperature"])
	}
	if len(loaded.Logs) != 1 || loaded.Logs[0] != "checkpoint test log" {
		t.Errorf("Logs mismatch: got %v", loaded.Logs)
	}
	if loaded.Timestamp.IsZero() {
		t.Error("loaded timestamp should not be zero")
	}
}

func TestLoadCheckpointFileNotFound(t *testing.T) {
	_, err := LoadCheckpoint("/nonexistent/path/checkpoint.json")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

func TestCheckpointSaveUsesAtomicRename(t *testing.T) {
	// Verify that Save uses write-to-temp + rename. We detect the transient temp
	// file by polling the directory in a goroutine while Save runs.
	// This test FAILS with os.WriteFile (no temp file ever appears) and PASSES
	// with the atomic rename implementation.
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")

	// Build a checkpoint large enough (~5MB) that the write takes several
	// milliseconds, giving the watcher goroutine many polling windows to observe
	// the temp file before rename completes.
	logs := make([]string, 5000)
	for i := range logs {
		logs[i] = strings.Repeat("log entry padding ", 50)
	}
	cp := &Checkpoint{CurrentNode: "node_x", Logs: logs}

	var tempFileSeen atomic.Bool
	ready := make(chan struct{})
	stop := make(chan struct{})

	go func() {
		defer close(stop)
		close(ready) // signal: goroutine is now actively polling
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			entries, _ := os.ReadDir(dir)
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), ".checkpoint-") && strings.HasSuffix(e.Name(), ".tmp") {
					tempFileSeen.Store(true)
					return
				}
			}
			// No sleep: poll as fast as possible to maximise chances of
			// observing the temp file during the write window.
		}
	}()

	<-ready // ensure watcher goroutine is scheduled before Save starts
	if err := cp.Save(path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	<-stop // wait for goroutine to exit

	if !tempFileSeen.Load() {
		t.Error("atomic write not detected: no temp file (.checkpoint-*.tmp) observed during Save; expected write-to-temp-then-rename pattern")
	}
}

func TestCheckpointSaveNoTempFilesRemain(t *testing.T) {
	// After a successful Save, no temp files must remain in the directory.
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")

	cp := &Checkpoint{CurrentNode: "node_x"}
	if err := cp.Save(path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	if len(entries) != 1 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("expected exactly 1 file in dir after save, got %d: %v", len(entries), names)
	}
}

func TestCheckpointSaveTempFileCleanedUpOnRenameError(t *testing.T) {
	// When the atomic rename fails, the temp file must be cleaned up.
	// We force the rename to fail by making the target path an existing directory.
	dir := t.TempDir()
	targetDir := filepath.Join(dir, "target")
	if err := os.Mkdir(targetDir, 0755); err != nil {
		t.Fatalf("Mkdir failed: %v", err)
	}

	cp := &Checkpoint{CurrentNode: "node_y"}
	err := cp.Save(targetDir) // targetDir is an existing directory; rename will fail
	if err == nil {
		t.Fatal("expected Save to fail when target path is an existing directory")
	}

	// Only "target" should remain; no leftover temp files.
	entries, err2 := os.ReadDir(dir)
	if err2 != nil {
		t.Fatalf("ReadDir failed: %v", err2)
	}
	if len(entries) != 1 || entries[0].Name() != "target" {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("expected only 'target' dir after failed save, got: %v", names)
	}
}
