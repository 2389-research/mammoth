// ABOUTME: Tests for pipeline run audit narrative generation.
// ABOUTME: Covers context construction from run data and event summarization.
package attractor

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestBuildAuditContext_FailedRun(t *testing.T) {
	startTime := time.Date(2026, 2, 20, 11, 39, 48, 0, time.UTC)
	endTime := time.Date(2026, 2, 20, 11, 39, 53, 0, time.UTC)

	req := AuditRequest{
		State: &RunState{
			ID:           "ebbe59cd241c09df",
			PipelineFile: "kayabot4.dot",
			Status:       "failed",
			StartedAt:    startTime,
			CompletedAt:  &endTime,
			Error:        `node "setup" visited 3 times (max 3)`,
		},
		Events: []EngineEvent{
			{Type: EventPipelineStarted, Timestamp: startTime, Data: map[string]any{"workdir": "/tmp/test"}},
			{Type: EventStageStarted, NodeID: "start", Timestamp: startTime},
			{Type: EventStageCompleted, NodeID: "start", Timestamp: startTime},
			{Type: EventStageStarted, NodeID: "setup", Timestamp: startTime},
			{Type: EventStageFailed, NodeID: "setup", Timestamp: startTime.Add(2 * time.Second), Data: map[string]any{"reason": "429 rate limit"}},
			{Type: EventPipelineFailed, Timestamp: endTime, Data: map[string]any{"error": "max visits"}},
		},
		Graph: &Graph{
			Nodes: map[string]*Node{
				"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
				"setup": {ID: "setup", Attrs: map[string]string{"shape": "box", "prompt": "set up project"}},
				"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
			},
			Edges: []*Edge{
				{From: "start", To: "setup", Attrs: map[string]string{}},
				{From: "setup", To: "exit", Attrs: map[string]string{}},
			},
		},
		Verbose: false,
	}

	ctx := buildAuditContext(req)

	if ctx == "" {
		t.Fatal("expected non-empty audit context")
	}
	if !strings.Contains(ctx, "kayabot4.dot") {
		t.Error("expected pipeline file name in context")
	}
	if !strings.Contains(ctx, "failed") {
		t.Error("expected status in context")
	}
	if !strings.Contains(ctx, "setup") {
		t.Error("expected node names in context")
	}
	if !strings.Contains(ctx, "429 rate limit") {
		t.Error("expected error details in context")
	}
}

func TestBuildAuditContext_IncludesFlowSummary(t *testing.T) {
	startTime := time.Now()

	req := AuditRequest{
		State: &RunState{
			ID:        "abc123",
			Status:    "completed",
			StartedAt: startTime,
		},
		Events: []EngineEvent{},
		Graph: &Graph{
			Nodes: map[string]*Node{
				"start":  {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
				"build":  {ID: "build", Attrs: map[string]string{"shape": "box"}},
				"verify": {ID: "verify", Attrs: map[string]string{"shape": "box"}},
				"exit":   {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
			},
			Edges: []*Edge{
				{From: "start", To: "build", Attrs: map[string]string{}},
				{From: "build", To: "verify", Attrs: map[string]string{}},
				{From: "verify", To: "exit", Attrs: map[string]string{}},
			},
		},
		Verbose: false,
	}

	ctx := buildAuditContext(req)

	// Assert the full linearized path appears (not just individual node names,
	// which could match elsewhere in the output).
	if !strings.Contains(ctx, "start -> build -> verify -> exit") {
		t.Errorf("expected complete flow path 'start -> build -> verify -> exit' in context:\n%s", ctx)
	}
}

func TestBuildAuditContext_VerboseIncludesToolDetails(t *testing.T) {
	startTime := time.Now()

	req := AuditRequest{
		State: &RunState{
			ID:        "abc123",
			Status:    "completed",
			StartedAt: startTime,
		},
		Events: []EngineEvent{
			{Type: EventAgentToolCallStart, NodeID: "build", Timestamp: startTime, Data: map[string]any{
				"tool_name": "bash_exec",
				"arguments": `{"command": "go build ./..."}`,
			}},
			{Type: EventAgentToolCallEnd, NodeID: "build", Timestamp: startTime.Add(time.Second), Data: map[string]any{
				"tool_name":   "bash_exec",
				"duration_ms": 1200,
			}},
		},
		Graph:   &Graph{Nodes: map[string]*Node{}, Edges: []*Edge{}},
		Verbose: true,
	}

	ctx := buildAuditContext(req)

	if !strings.Contains(ctx, "bash_exec") {
		t.Error("verbose mode should include tool names")
	}
	if !strings.Contains(ctx, "go build") {
		t.Error("verbose mode should include tool arguments")
	}
}

func TestGenerateAudit_RequiresClient(t *testing.T) {
	req := AuditRequest{
		State:  &RunState{ID: "test", Status: "failed", StartedAt: time.Now()},
		Events: []EngineEvent{},
		Graph:  &Graph{Nodes: map[string]*Node{}, Edges: []*Edge{}},
	}

	_, err := GenerateAudit(context.Background(), req, nil)
	if err == nil {
		t.Error("expected error when no LLM client provided")
	}
}
