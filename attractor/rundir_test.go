// ABOUTME: Tests for RunDirectory, which manages the per-run directory layout.
// ABOUTME: Covers directory creation, node artifact I/O, checkpoint roundtrip, and convenience methods.
package attractor

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestNewRunDirectory_CreatesStructure(t *testing.T) {
	base := t.TempDir()
	runID := "run-abc-123"

	rd, err := NewRunDirectory(base, runID)
	if err != nil {
		t.Fatalf("NewRunDirectory failed: %v", err)
	}

	if rd.BaseDir != filepath.Join(base, runID) {
		t.Errorf("BaseDir = %q, want %q", rd.BaseDir, filepath.Join(base, runID))
	}
	if rd.RunID != runID {
		t.Errorf("RunID = %q, want %q", rd.RunID, runID)
	}

	// The run directory should exist
	info, err := os.Stat(rd.BaseDir)
	if err != nil {
		t.Fatalf("run directory does not exist: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("run directory is not a directory")
	}

	// The nodes subdirectory should exist
	nodesDir := filepath.Join(rd.BaseDir, "nodes")
	info, err = os.Stat(nodesDir)
	if err != nil {
		t.Fatalf("nodes directory does not exist: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("nodes directory is not a directory")
	}
}

func TestNewRunDirectory_EmptyRunID(t *testing.T) {
	base := t.TempDir()
	_, err := NewRunDirectory(base, "")
	if err == nil {
		t.Fatal("expected error for empty runID, got nil")
	}
}

func TestNewRunDirectory_EmptyBaseDir(t *testing.T) {
	_, err := NewRunDirectory("", "run-123")
	if err == nil {
		t.Fatal("expected error for empty baseDir, got nil")
	}
}

func TestNodeDir_ReturnsCorrectPath(t *testing.T) {
	base := t.TempDir()
	rd, err := NewRunDirectory(base, "run-1")
	if err != nil {
		t.Fatalf("NewRunDirectory failed: %v", err)
	}

	got := rd.NodeDir("planner")
	want := filepath.Join(base, "run-1", "nodes", "planner")
	if got != want {
		t.Errorf("NodeDir = %q, want %q", got, want)
	}
}

func TestEnsureNodeDir_CreatesDirectory(t *testing.T) {
	base := t.TempDir()
	rd, err := NewRunDirectory(base, "run-2")
	if err != nil {
		t.Fatalf("NewRunDirectory failed: %v", err)
	}

	if err := rd.EnsureNodeDir("architect"); err != nil {
		t.Fatalf("EnsureNodeDir failed: %v", err)
	}

	nodeDir := rd.NodeDir("architect")
	info, err := os.Stat(nodeDir)
	if err != nil {
		t.Fatalf("node directory does not exist: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("node directory is not a directory")
	}

	// Calling again should be idempotent
	if err := rd.EnsureNodeDir("architect"); err != nil {
		t.Fatalf("second EnsureNodeDir failed: %v", err)
	}
}

func TestEnsureNodeDir_EmptyNodeID(t *testing.T) {
	base := t.TempDir()
	rd, err := NewRunDirectory(base, "run-3")
	if err != nil {
		t.Fatalf("NewRunDirectory failed: %v", err)
	}

	err = rd.EnsureNodeDir("")
	if err == nil {
		t.Fatal("expected error for empty nodeID, got nil")
	}
}

func TestWriteReadNodeArtifact_Roundtrip(t *testing.T) {
	base := t.TempDir()
	rd, err := NewRunDirectory(base, "run-4")
	if err != nil {
		t.Fatalf("NewRunDirectory failed: %v", err)
	}

	nodeID := "coder"
	filename := "output.go"
	data := []byte("package main\n\nfunc main() {}\n")

	if err := rd.WriteNodeArtifact(nodeID, filename, data); err != nil {
		t.Fatalf("WriteNodeArtifact failed: %v", err)
	}

	got, err := rd.ReadNodeArtifact(nodeID, filename)
	if err != nil {
		t.Fatalf("ReadNodeArtifact failed: %v", err)
	}

	if string(got) != string(data) {
		t.Errorf("ReadNodeArtifact returned %q, want %q", string(got), string(data))
	}
}

func TestWriteNodeArtifact_EmptyNodeID(t *testing.T) {
	base := t.TempDir()
	rd, err := NewRunDirectory(base, "run-5")
	if err != nil {
		t.Fatalf("NewRunDirectory failed: %v", err)
	}

	err = rd.WriteNodeArtifact("", "file.txt", []byte("data"))
	if err == nil {
		t.Fatal("expected error for empty nodeID, got nil")
	}
}

func TestWriteNodeArtifact_EmptyFilename(t *testing.T) {
	base := t.TempDir()
	rd, err := NewRunDirectory(base, "run-6")
	if err != nil {
		t.Fatalf("NewRunDirectory failed: %v", err)
	}

	err = rd.WriteNodeArtifact("node1", "", []byte("data"))
	if err == nil {
		t.Fatal("expected error for empty filename, got nil")
	}
}

func TestReadNodeArtifact_MissingFile(t *testing.T) {
	base := t.TempDir()
	rd, err := NewRunDirectory(base, "run-7")
	if err != nil {
		t.Fatalf("NewRunDirectory failed: %v", err)
	}

	_, err = rd.ReadNodeArtifact("node1", "nonexistent.txt")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestListNodeArtifacts(t *testing.T) {
	base := t.TempDir()
	rd, err := NewRunDirectory(base, "run-8")
	if err != nil {
		t.Fatalf("NewRunDirectory failed: %v", err)
	}

	nodeID := "builder"

	// Write several artifacts
	files := map[string][]byte{
		"plan.md":     []byte("# Plan\nStep 1"),
		"output.go":   []byte("package main"),
		"response.md": []byte("Done"),
	}
	for name, content := range files {
		if err := rd.WriteNodeArtifact(nodeID, name, content); err != nil {
			t.Fatalf("WriteNodeArtifact(%q) failed: %v", name, err)
		}
	}

	artifacts, err := rd.ListNodeArtifacts(nodeID)
	if err != nil {
		t.Fatalf("ListNodeArtifacts failed: %v", err)
	}

	sort.Strings(artifacts)
	want := []string{"output.go", "plan.md", "response.md"}
	if len(artifacts) != len(want) {
		t.Fatalf("ListNodeArtifacts returned %d items, want %d", len(artifacts), len(want))
	}
	for i, name := range want {
		if artifacts[i] != name {
			t.Errorf("artifacts[%d] = %q, want %q", i, artifacts[i], name)
		}
	}
}

func TestListNodeArtifacts_EmptyDir(t *testing.T) {
	base := t.TempDir()
	rd, err := NewRunDirectory(base, "run-9")
	if err != nil {
		t.Fatalf("NewRunDirectory failed: %v", err)
	}

	// Node dir does not exist yet
	artifacts, err := rd.ListNodeArtifacts("ghost-node")
	if err != nil {
		t.Fatalf("ListNodeArtifacts should not error for missing dir, got: %v", err)
	}
	if len(artifacts) != 0 {
		t.Errorf("expected empty list, got %v", artifacts)
	}
}

func TestSaveLoadCheckpoint_Roundtrip(t *testing.T) {
	base := t.TempDir()
	rd, err := NewRunDirectory(base, "run-10")
	if err != nil {
		t.Fatalf("NewRunDirectory failed: %v", err)
	}

	ctx := NewContext()
	ctx.Set("model", "claude-opus-4")
	ctx.AppendLog("started pipeline")

	original := NewCheckpoint(ctx, "review", []string{"start", "code"}, map[string]int{"code": 1})

	if err := rd.SaveCheckpoint(original); err != nil {
		t.Fatalf("SaveCheckpoint failed: %v", err)
	}

	// Verify the checkpoint file exists at the expected path
	cpPath := filepath.Join(rd.BaseDir, "checkpoint.json")
	if _, err := os.Stat(cpPath); os.IsNotExist(err) {
		t.Fatal("checkpoint.json was not created in run directory")
	}

	loaded, err := rd.LoadCheckpoint()
	if err != nil {
		t.Fatalf("LoadCheckpoint failed: %v", err)
	}

	if loaded.CurrentNode != original.CurrentNode {
		t.Errorf("CurrentNode = %q, want %q", loaded.CurrentNode, original.CurrentNode)
	}
	if len(loaded.CompletedNodes) != len(original.CompletedNodes) {
		t.Fatalf("CompletedNodes len = %d, want %d", len(loaded.CompletedNodes), len(original.CompletedNodes))
	}
	for i, node := range original.CompletedNodes {
		if loaded.CompletedNodes[i] != node {
			t.Errorf("CompletedNodes[%d] = %q, want %q", i, loaded.CompletedNodes[i], node)
		}
	}
	if loaded.NodeRetries["code"] != 1 {
		t.Errorf("NodeRetries['code'] = %d, want 1", loaded.NodeRetries["code"])
	}
	if loaded.ContextValues["model"] != "claude-opus-4" {
		t.Errorf("ContextValues['model'] = %v, want 'claude-opus-4'", loaded.ContextValues["model"])
	}
	if loaded.Timestamp.IsZero() {
		t.Error("loaded checkpoint should have a non-zero timestamp")
	}
}

func TestSaveCheckpoint_Overwrites(t *testing.T) {
	base := t.TempDir()
	rd, err := NewRunDirectory(base, "run-11")
	if err != nil {
		t.Fatalf("NewRunDirectory failed: %v", err)
	}

	ctx := NewContext()
	cp1 := NewCheckpoint(ctx, "node_a", []string{"start"}, nil)
	if err := rd.SaveCheckpoint(cp1); err != nil {
		t.Fatalf("first SaveCheckpoint failed: %v", err)
	}

	cp2 := NewCheckpoint(ctx, "node_b", []string{"start", "node_a"}, nil)
	if err := rd.SaveCheckpoint(cp2); err != nil {
		t.Fatalf("second SaveCheckpoint failed: %v", err)
	}

	loaded, err := rd.LoadCheckpoint()
	if err != nil {
		t.Fatalf("LoadCheckpoint failed: %v", err)
	}

	if loaded.CurrentNode != "node_b" {
		t.Errorf("CurrentNode = %q, want 'node_b' (should be overwritten)", loaded.CurrentNode)
	}
	if len(loaded.CompletedNodes) != 2 {
		t.Errorf("CompletedNodes len = %d, want 2", len(loaded.CompletedNodes))
	}
}

func TestLoadCheckpoint_NoCheckpoint(t *testing.T) {
	base := t.TempDir()
	rd, err := NewRunDirectory(base, "run-12")
	if err != nil {
		t.Fatalf("NewRunDirectory failed: %v", err)
	}

	_, err = rd.LoadCheckpoint()
	if err == nil {
		t.Fatal("expected error when no checkpoint exists, got nil")
	}
}

func TestWritePrompt(t *testing.T) {
	base := t.TempDir()
	rd, err := NewRunDirectory(base, "run-13")
	if err != nil {
		t.Fatalf("NewRunDirectory failed: %v", err)
	}

	prompt := "You are a software architect. Design a REST API for user management."
	if err := rd.WritePrompt("architect", prompt); err != nil {
		t.Fatalf("WritePrompt failed: %v", err)
	}

	got, err := rd.ReadNodeArtifact("architect", "prompt.md")
	if err != nil {
		t.Fatalf("ReadNodeArtifact for prompt.md failed: %v", err)
	}
	if string(got) != prompt {
		t.Errorf("prompt.md content = %q, want %q", string(got), prompt)
	}
}

func TestWriteResponse(t *testing.T) {
	base := t.TempDir()
	rd, err := NewRunDirectory(base, "run-14")
	if err != nil {
		t.Fatalf("NewRunDirectory failed: %v", err)
	}

	response := "Here is the API design:\n\n## Endpoints\n- GET /users\n- POST /users"
	if err := rd.WriteResponse("architect", response); err != nil {
		t.Fatalf("WriteResponse failed: %v", err)
	}

	got, err := rd.ReadNodeArtifact("architect", "response.md")
	if err != nil {
		t.Fatalf("ReadNodeArtifact for response.md failed: %v", err)
	}
	if string(got) != response {
		t.Errorf("response.md content = %q, want %q", string(got), response)
	}
}

func TestWritePrompt_EmptyNodeID(t *testing.T) {
	base := t.TempDir()
	rd, err := NewRunDirectory(base, "run-15")
	if err != nil {
		t.Fatalf("NewRunDirectory failed: %v", err)
	}

	err = rd.WritePrompt("", "some prompt")
	if err == nil {
		t.Fatal("expected error for empty nodeID, got nil")
	}
}

func TestWriteResponse_EmptyNodeID(t *testing.T) {
	base := t.TempDir()
	rd, err := NewRunDirectory(base, "run-16")
	if err != nil {
		t.Fatalf("NewRunDirectory failed: %v", err)
	}

	err = rd.WriteResponse("", "some response")
	if err == nil {
		t.Fatal("expected error for empty nodeID, got nil")
	}
}

func TestMultipleNodesArtifacts(t *testing.T) {
	base := t.TempDir()
	rd, err := NewRunDirectory(base, "run-17")
	if err != nil {
		t.Fatalf("NewRunDirectory failed: %v", err)
	}

	// Write artifacts for multiple nodes
	nodes := []string{"planner", "coder", "reviewer"}
	for _, nodeID := range nodes {
		if err := rd.WritePrompt(nodeID, "prompt for "+nodeID); err != nil {
			t.Fatalf("WritePrompt(%q) failed: %v", nodeID, err)
		}
		if err := rd.WriteResponse(nodeID, "response from "+nodeID); err != nil {
			t.Fatalf("WriteResponse(%q) failed: %v", nodeID, err)
		}
	}

	// Verify each node's artifacts are isolated
	for _, nodeID := range nodes {
		artifacts, err := rd.ListNodeArtifacts(nodeID)
		if err != nil {
			t.Fatalf("ListNodeArtifacts(%q) failed: %v", nodeID, err)
		}
		if len(artifacts) != 2 {
			t.Errorf("node %q has %d artifacts, want 2", nodeID, len(artifacts))
		}

		prompt, err := rd.ReadNodeArtifact(nodeID, "prompt.md")
		if err != nil {
			t.Fatalf("ReadNodeArtifact(%q, prompt.md) failed: %v", nodeID, err)
		}
		if string(prompt) != "prompt for "+nodeID {
			t.Errorf("node %q prompt = %q, want %q", nodeID, string(prompt), "prompt for "+nodeID)
		}
	}
}
