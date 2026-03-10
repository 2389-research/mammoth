// ABOUTME: Tests for the run_pipeline MCP tool handler.
// ABOUTME: Covers run ID return, invalid DOT rejection, and async pipeline completion.
package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// waitForRunCompletion polls the registry until the given run reaches a
// terminal status or the deadline expires. This prevents TempDir cleanup
// from racing with the async executePipeline goroutine.
func waitForRunCompletion(t *testing.T, ms *Server, runID string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		run, ok := ms.registry.Get(runID)
		if !ok {
			t.Fatal("run not found in registry")
		}
		run.mu.RLock()
		status := run.Status
		run.mu.RUnlock()
		if status == StatusCompleted || status == StatusFailed {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Error("pipeline did not complete within timeout")
}

// simplePipeline is a minimal DOT pipeline with start and exit nodes.
const simplePipeline = `digraph pipeline {
	start [shape=Mdiamond]
	end [shape=Msquare]
	start -> end
}`

// connectTestServerWithTools creates an MCP server with run_pipeline and
// get_run_status registered, returning a connected client session.
func connectTestServerWithTools(t *testing.T) (*mcpsdk.ClientSession, *Server) {
	t.Helper()
	ctx := context.Background()
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    "mammoth-test",
		Version: "v0.0.1-test",
	}, nil)

	ms := NewServer(t.TempDir())
	ms.registerValidatePipeline(srv)
	ms.registerRunPipeline(srv)

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

func TestRunPipeline_ReturnsRunID(t *testing.T) {
	cs, ms := connectTestServerWithTools(t)
	ctx := context.Background()

	result, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "run_pipeline",
		Arguments: map[string]any{"source": simplePipeline},
	})
	if err != nil {
		t.Fatalf("CallTool error: %v", err)
	}
	if result.IsError {
		text := result.Content[0].(*mcpsdk.TextContent).Text
		t.Fatalf("unexpected tool error: %s", text)
	}

	var output RunPipelineOutput
	text := result.Content[0].(*mcpsdk.TextContent).Text
	if err := json.Unmarshal([]byte(text), &output); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if output.RunID == "" {
		t.Error("expected non-empty run_id")
	}
	if output.Status != string(StatusRunning) {
		t.Errorf("expected status=%q, got %q", StatusRunning, output.Status)
	}

	// Wait for the async pipeline goroutine to finish so TempDir cleanup
	// doesn't race with in-flight writes.
	waitForRunCompletion(t, ms, output.RunID)
}

func TestRunPipeline_InvalidDOT(t *testing.T) {
	cs, _ := connectTestServerWithTools(t)
	ctx := context.Background()

	result, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "run_pipeline",
		Arguments: map[string]any{"source": "not valid dot at all"},
	})
	if err != nil {
		t.Fatalf("CallTool error: %v", err)
	}
	if !result.IsError {
		t.Error("expected tool error for invalid DOT")
	}
}

func TestRunPipeline_CompletesAsync(t *testing.T) {
	cs, ms := connectTestServerWithTools(t)
	ctx := context.Background()

	result, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "run_pipeline",
		Arguments: map[string]any{"source": simplePipeline},
	})
	if err != nil {
		t.Fatalf("CallTool error: %v", err)
	}

	var output RunPipelineOutput
	text := result.Content[0].(*mcpsdk.TextContent).Text
	if err := json.Unmarshal([]byte(text), &output); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}

	// Wait for the pipeline to complete (simple start->end should be fast).
	waitForRunCompletion(t, ms, output.RunID)
}
