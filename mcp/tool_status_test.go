// ABOUTME: Tests for the get_run_status MCP tool handler.
// ABOUTME: Covers running state, paused state, and not-found error cases.
package mcp

import (
	"context"
	"encoding/json"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// connectStatusServer creates an MCP server with get_run_status registered.
func connectStatusServer(t *testing.T) (*mcpsdk.ClientSession, *Server) {
	t.Helper()
	ctx := context.Background()
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    "mammoth-test",
		Version: "v0.0.1-test",
	}, nil)

	ms := NewServer(t.TempDir())
	ms.registerGetRunStatus(srv)

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

func TestGetRunStatus_Running(t *testing.T) {
	cs, ms := connectStatusServer(t)
	ctx := context.Background()

	// Create a run in the registry.
	run := ms.registry.Create(simplePipeline, RunConfig{})
	run.mu.Lock()
	run.CurrentNode = "build_step"
	run.CurrentActivity = "executing node: build_step"
	run.CompletedNodes = []string{"start"}
	run.mu.Unlock()

	result, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "get_run_status",
		Arguments: map[string]any{"run_id": run.ID},
	})
	if err != nil {
		t.Fatalf("CallTool error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error")
	}

	var output GetRunStatusOutput
	text := result.Content[0].(*mcpsdk.TextContent).Text
	if err := json.Unmarshal([]byte(text), &output); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if output.Status != string(StatusRunning) {
		t.Errorf("expected status=running, got %s", output.Status)
	}
	if output.CurrentNode != "build_step" {
		t.Errorf("expected current_node=build_step, got %s", output.CurrentNode)
	}
	if len(output.CompletedNodes) != 1 || output.CompletedNodes[0] != "start" {
		t.Errorf("expected completed_nodes=[start], got %v", output.CompletedNodes)
	}
}

func TestGetRunStatus_Paused(t *testing.T) {
	cs, ms := connectStatusServer(t)
	ctx := context.Background()

	run := ms.registry.Create(simplePipeline, RunConfig{})
	run.mu.Lock()
	run.Status = StatusPaused
	run.PendingQuestion = &PendingQuestion{
		ID:   "q1",
		Text: "Continue?",
	}
	run.mu.Unlock()

	result, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "get_run_status",
		Arguments: map[string]any{"run_id": run.ID},
	})
	if err != nil {
		t.Fatalf("CallTool error: %v", err)
	}

	var output GetRunStatusOutput
	text := result.Content[0].(*mcpsdk.TextContent).Text
	if err := json.Unmarshal([]byte(text), &output); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if output.Status != string(StatusPaused) {
		t.Errorf("expected status=paused, got %s", output.Status)
	}
	if output.PendingQuestion == nil {
		t.Fatal("expected pending_question, got nil")
	}
	if output.PendingQuestion.Text != "Continue?" {
		t.Errorf("expected question text=Continue?, got %s", output.PendingQuestion.Text)
	}
}

func TestGetRunStatus_NotFound(t *testing.T) {
	cs, _ := connectStatusServer(t)
	ctx := context.Background()

	result, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "get_run_status",
		Arguments: map[string]any{"run_id": "nonexistent"},
	})
	if err != nil {
		t.Fatalf("CallTool error: %v", err)
	}
	if !result.IsError {
		t.Error("expected tool error for not-found run")
	}
}
