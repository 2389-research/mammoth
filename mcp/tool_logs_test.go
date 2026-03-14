// ABOUTME: Tests for the get_run_logs MCP tool handler.
// ABOUTME: Covers all logs, tail, node filter, and not-found error cases.
package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// connectLogsServer creates an MCP server with get_run_logs registered.
func connectLogsServer(t *testing.T) (*mcpsdk.ClientSession, *Server) {
	t.Helper()
	ctx := context.Background()
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    "mammoth-test",
		Version: "v0.0.1-test",
	}, nil)

	ms := NewServer(t.TempDir())
	ms.registerGetRunLogs(srv)

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

func TestGetRunLogs_AllLogs(t *testing.T) {
	cs, ms := connectLogsServer(t)
	ctx := context.Background()

	run := ms.registry.Create(simplePipeline, RunConfig{})
	now := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	run.mu.Lock()
	run.EventBuffer = []RunEvent{
		{Type: "stage_started", NodeID: "start", Timestamp: now},
		{Type: "stage_completed", NodeID: "start", Timestamp: now.Add(1 * time.Second)},
		{Type: "stage_started", NodeID: "build", Timestamp: now.Add(2 * time.Second)},
	}
	run.mu.Unlock()

	result, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "get_run_logs",
		Arguments: map[string]any{"run_id": run.ID},
	})
	if err != nil {
		t.Fatalf("CallTool error: %v", err)
	}

	var output GetRunLogsOutput
	text := result.Content[0].(*mcpsdk.TextContent).Text
	if err := json.Unmarshal([]byte(text), &output); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(output.Lines) != 3 {
		t.Errorf("expected 3 log lines, got %d", len(output.Lines))
	}
}

func TestGetRunLogs_Tail(t *testing.T) {
	cs, ms := connectLogsServer(t)
	ctx := context.Background()

	run := ms.registry.Create(simplePipeline, RunConfig{})
	now := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	run.mu.Lock()
	run.EventBuffer = []RunEvent{
		{Type: "stage_started", NodeID: "start", Timestamp: now},
		{Type: "stage_completed", NodeID: "start", Timestamp: now.Add(1 * time.Second)},
		{Type: "stage_started", NodeID: "build", Timestamp: now.Add(2 * time.Second)},
		{Type: "stage_completed", NodeID: "build", Timestamp: now.Add(3 * time.Second)},
	}
	run.mu.Unlock()

	result, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "get_run_logs",
		Arguments: map[string]any{
			"run_id": run.ID,
			"tail":   float64(2),
		},
	})
	if err != nil {
		t.Fatalf("CallTool error: %v", err)
	}

	var output GetRunLogsOutput
	text := result.Content[0].(*mcpsdk.TextContent).Text
	if err := json.Unmarshal([]byte(text), &output); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(output.Lines) != 2 {
		t.Errorf("expected 2 log lines with tail=2, got %d", len(output.Lines))
	}
}

func TestGetRunLogs_NodeFilter(t *testing.T) {
	cs, ms := connectLogsServer(t)
	ctx := context.Background()

	run := ms.registry.Create(simplePipeline, RunConfig{})
	now := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	run.mu.Lock()
	run.EventBuffer = []RunEvent{
		{Type: "stage_started", NodeID: "start", Timestamp: now},
		{Type: "stage_completed", NodeID: "start", Timestamp: now.Add(1 * time.Second)},
		{Type: "stage_started", NodeID: "build", Timestamp: now.Add(2 * time.Second)},
	}
	run.mu.Unlock()

	result, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "get_run_logs",
		Arguments: map[string]any{
			"run_id":  run.ID,
			"node_id": "build",
		},
	})
	if err != nil {
		t.Fatalf("CallTool error: %v", err)
	}

	var output GetRunLogsOutput
	text := result.Content[0].(*mcpsdk.TextContent).Text
	if err := json.Unmarshal([]byte(text), &output); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(output.Lines) != 1 {
		t.Errorf("expected 1 log line for node_id=build, got %d", len(output.Lines))
	}
}

func TestGetRunLogs_NotFound(t *testing.T) {
	cs, _ := connectLogsServer(t)
	ctx := context.Background()

	result, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "get_run_logs",
		Arguments: map[string]any{"run_id": "nonexistent"},
	})
	if err != nil {
		t.Fatalf("CallTool error: %v", err)
	}
	if !result.IsError {
		t.Error("expected tool error for not-found run")
	}
}
