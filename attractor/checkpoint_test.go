// ABOUTME: Tests for checkpoint serialization and deserialization.
// ABOUTME: Covers round-trip save/load and error handling for missing files.
package attractor

import (
	"os"
	"path/filepath"
	"testing"
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
