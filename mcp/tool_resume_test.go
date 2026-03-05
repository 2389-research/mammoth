// ABOUTME: Tests for the resume_pipeline MCP tool handler.
// ABOUTME: Covers no checkpoint error, not-found error, and checkpoint-based resume.
package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/2389-research/mammoth/attractor"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// connectResumeServer creates an MCP server with resume_pipeline registered.
func connectResumeServer(t *testing.T) (*mcpsdk.ClientSession, *Server) {
	t.Helper()
	ctx := context.Background()
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    "mammoth-test",
		Version: "v0.0.1-test",
	}, nil)

	ms := NewServer(t.TempDir())
	ms.registerResumePipeline(srv)

	t1, t2 := mcpsdk.NewInMemoryTransports()
	if _, err := srv.Connect(ctx, t1, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcpsdk.NewClient(&mcpsdk.Implementation{
		Name:    "test-client",
		Version: "v0.0.1-test",
	}, nil)
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs, ms
}

func TestResumePipeline_NotFound(t *testing.T) {
	cs, _ := connectResumeServer(t)
	ctx := context.Background()

	result, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "resume_pipeline",
		Arguments: map[string]any{"run_id": "nonexistent"},
	})
	if err != nil {
		t.Fatalf("CallTool error: %v", err)
	}
	if !result.IsError {
		t.Error("expected tool error for not-found run")
	}
}

func TestResumePipeline_NoCheckpoint(t *testing.T) {
	cs, ms := connectResumeServer(t)
	ctx := context.Background()

	// Create a previous run in the disk index with an empty checkpoint dir.
	checkpointDir := filepath.Join(ms.dataDir, "prev-run", "checkpoints")
	_ = os.MkdirAll(checkpointDir, 0755)
	entry := &IndexEntry{
		RunID:         "prev-run",
		Source:        simplePipeline,
		Config:        RunConfig{},
		Status:        string(StatusFailed),
		CheckpointDir: checkpointDir,
	}
	if err := ms.index.Save(entry); err != nil {
		t.Fatalf("save index: %v", err)
	}

	result, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "resume_pipeline",
		Arguments: map[string]any{"run_id": "prev-run"},
	})
	if err != nil {
		t.Fatalf("CallTool error: %v", err)
	}
	if !result.IsError {
		t.Error("expected tool error for no checkpoint")
	}
}

func TestResumePipeline_WithCheckpoint(t *testing.T) {
	cs, ms := connectResumeServer(t)
	ctx := context.Background()

	// Create a previous run with a real checkpoint.
	checkpointDir := filepath.Join(ms.dataDir, "prev-run", "checkpoints")
	_ = os.MkdirAll(checkpointDir, 0755)

	pctx := attractor.NewContext()
	cp := attractor.NewCheckpoint(pctx, "start", []string{"start"}, map[string]int{})
	cpPath := filepath.Join(checkpointDir, "checkpoint_001.json")
	if err := cp.Save(cpPath); err != nil {
		t.Fatalf("save checkpoint: %v", err)
	}

	entry := &IndexEntry{
		RunID:         "prev-run",
		Source:        simplePipeline,
		Config:        RunConfig{},
		Status:        string(StatusFailed),
		CheckpointDir: checkpointDir,
	}
	if err := ms.index.Save(entry); err != nil {
		t.Fatalf("save index: %v", err)
	}

	result, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "resume_pipeline",
		Arguments: map[string]any{"run_id": "prev-run"},
	})
	if err != nil {
		t.Fatalf("CallTool error: %v", err)
	}
	if result.IsError {
		text := result.Content[0].(*mcpsdk.TextContent).Text
		t.Fatalf("unexpected tool error: %s", text)
	}

	var output ResumePipelineOutput
	text := result.Content[0].(*mcpsdk.TextContent).Text
	if err := json.Unmarshal([]byte(text), &output); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if output.RunID == "" {
		t.Error("expected non-empty run_id")
	}
	if output.RunID == "prev-run" {
		t.Error("expected new run_id, got same as previous")
	}
}
