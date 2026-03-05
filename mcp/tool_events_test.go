// ABOUTME: Tests for the get_run_events MCP tool handler.
// ABOUTME: Covers all events, type filter, since filter, and not-found error cases.
package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/2389-research/mammoth/attractor"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// connectEventsServer creates an MCP server with get_run_events registered.
func connectEventsServer(t *testing.T) (*mcpsdk.ClientSession, *Server) {
	t.Helper()
	ctx := context.Background()
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    "mammoth-test",
		Version: "v0.0.1-test",
	}, nil)

	ms := NewServer(t.TempDir())
	ms.registerGetRunEvents(srv)

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

func TestGetRunEvents_AllEvents(t *testing.T) {
	cs, ms := connectEventsServer(t)
	ctx := context.Background()

	run := ms.registry.Create(simplePipeline, RunConfig{})
	now := time.Now()
	run.mu.Lock()
	run.EventBuffer = []attractor.EngineEvent{
		{Type: attractor.EventStageStarted, NodeID: "start", Timestamp: now.Add(-2 * time.Second)},
		{Type: attractor.EventStageCompleted, NodeID: "start", Timestamp: now.Add(-1 * time.Second)},
		{Type: attractor.EventStageStarted, NodeID: "build", Timestamp: now},
	}
	run.mu.Unlock()

	result, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "get_run_events",
		Arguments: map[string]any{"run_id": run.ID},
	})
	if err != nil {
		t.Fatalf("CallTool error: %v", err)
	}

	var output GetRunEventsOutput
	text := result.Content[0].(*mcpsdk.TextContent).Text
	if err := json.Unmarshal([]byte(text), &output); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(output.Events) != 3 {
		t.Errorf("expected 3 events, got %d", len(output.Events))
	}
}

func TestGetRunEvents_TypeFilter(t *testing.T) {
	cs, ms := connectEventsServer(t)
	ctx := context.Background()

	run := ms.registry.Create(simplePipeline, RunConfig{})
	now := time.Now()
	run.mu.Lock()
	run.EventBuffer = []attractor.EngineEvent{
		{Type: attractor.EventStageStarted, NodeID: "start", Timestamp: now.Add(-2 * time.Second)},
		{Type: attractor.EventStageCompleted, NodeID: "start", Timestamp: now.Add(-1 * time.Second)},
		{Type: attractor.EventStageStarted, NodeID: "build", Timestamp: now},
	}
	run.mu.Unlock()

	result, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "get_run_events",
		Arguments: map[string]any{
			"run_id": run.ID,
			"types":  []any{"stage.started"},
		},
	})
	if err != nil {
		t.Fatalf("CallTool error: %v", err)
	}

	var output GetRunEventsOutput
	text := result.Content[0].(*mcpsdk.TextContent).Text
	if err := json.Unmarshal([]byte(text), &output); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(output.Events) != 2 {
		t.Errorf("expected 2 stage.started events, got %d", len(output.Events))
	}
}

func TestGetRunEvents_SinceFilter(t *testing.T) {
	cs, ms := connectEventsServer(t)
	ctx := context.Background()

	run := ms.registry.Create(simplePipeline, RunConfig{})
	now := time.Now()
	run.mu.Lock()
	run.EventBuffer = []attractor.EngineEvent{
		{Type: attractor.EventStageStarted, NodeID: "start", Timestamp: now.Add(-10 * time.Second)},
		{Type: attractor.EventStageCompleted, NodeID: "start", Timestamp: now.Add(-5 * time.Second)},
		{Type: attractor.EventStageStarted, NodeID: "build", Timestamp: now},
	}
	run.mu.Unlock()

	sinceStr := now.Add(-6 * time.Second).Format(time.RFC3339Nano)
	result, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "get_run_events",
		Arguments: map[string]any{
			"run_id": run.ID,
			"since":  sinceStr,
		},
	})
	if err != nil {
		t.Fatalf("CallTool error: %v", err)
	}

	var output GetRunEventsOutput
	text := result.Content[0].(*mcpsdk.TextContent).Text
	if err := json.Unmarshal([]byte(text), &output); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(output.Events) != 2 {
		t.Errorf("expected 2 events after since, got %d", len(output.Events))
	}
}

func TestGetRunEvents_NotFound(t *testing.T) {
	cs, _ := connectEventsServer(t)
	ctx := context.Background()

	result, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "get_run_events",
		Arguments: map[string]any{"run_id": "nonexistent"},
	})
	if err != nil {
		t.Fatalf("CallTool error: %v", err)
	}
	if !result.IsError {
		t.Error("expected tool error for not-found run")
	}
}
