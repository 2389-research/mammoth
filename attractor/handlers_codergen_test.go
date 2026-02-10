// ABOUTME: Tests for CodergenHandler wired to a CodergenBackend for real agent execution.
// ABOUTME: Covers backend integration, stub fallback, error handling, config passthrough, and artifact storage.
package attractor

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestCodergenHandlerWithBackendCallsRunAgent(t *testing.T) {
	backend := &fakeBackend{}
	h := &CodergenHandler{Backend: backend}

	node := &Node{
		ID: "codegen_node",
		Attrs: map[string]string{
			"shape":        "box",
			"prompt":       "Write a hello world function",
			"label":        "Hello World",
			"llm_model":    "claude-sonnet-4-5",
			"llm_provider": "anthropic",
		},
	}
	pctx := NewContext()
	pctx.Set("goal", "build a greeting app")
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if outcome.Status != StatusSuccess {
		t.Errorf("expected status success, got %v", outcome.Status)
	}

	// Backend should have been called
	if len(backend.calls) != 1 {
		t.Fatalf("expected 1 backend call, got %d", len(backend.calls))
	}

	call := backend.calls[0]
	if call.Prompt != "Write a hello world function" {
		t.Errorf("expected prompt 'Write a hello world function', got %q", call.Prompt)
	}
	if call.Model != "claude-sonnet-4-5" {
		t.Errorf("expected model 'claude-sonnet-4-5', got %q", call.Model)
	}
	if call.Provider != "anthropic" {
		t.Errorf("expected provider 'anthropic', got %q", call.Provider)
	}
	if call.NodeID != "codegen_node" {
		t.Errorf("expected node ID 'codegen_node', got %q", call.NodeID)
	}
	if call.Goal != "build a greeting app" {
		t.Errorf("expected goal 'build a greeting app', got %q", call.Goal)
	}
}

func TestCodergenHandlerWithBackendPassesMaxTurns(t *testing.T) {
	backend := &fakeBackend{}
	h := &CodergenHandler{Backend: backend}

	node := &Node{
		ID: "codegen_turns",
		Attrs: map[string]string{
			"shape":     "box",
			"prompt":    "Do work",
			"max_turns": "50",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	_, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(backend.calls) != 1 {
		t.Fatalf("expected 1 backend call, got %d", len(backend.calls))
	}

	if backend.calls[0].MaxTurns != 50 {
		t.Errorf("expected max turns 50, got %d", backend.calls[0].MaxTurns)
	}
}

func TestCodergenHandlerWithBackendDefaultMaxTurns(t *testing.T) {
	backend := &fakeBackend{}
	h := &CodergenHandler{Backend: backend}

	node := &Node{
		ID: "codegen_default",
		Attrs: map[string]string{
			"shape":  "box",
			"prompt": "Do work",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	_, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Default max turns should be 20 when not specified
	if backend.calls[0].MaxTurns != 20 {
		t.Errorf("expected default max turns 20, got %d", backend.calls[0].MaxTurns)
	}
}

func TestCodergenHandlerWithBackendFailure(t *testing.T) {
	backend := &fakeBackend{
		runAgentFn: func(ctx context.Context, config AgentRunConfig) (*AgentRunResult, error) {
			return &AgentRunResult{
				Output:  "failed to complete task",
				Success: false,
			}, nil
		},
	}
	h := &CodergenHandler{Backend: backend}

	node := &Node{
		ID: "codegen_fail",
		Attrs: map[string]string{
			"shape":  "box",
			"prompt": "impossible task",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if outcome.Status != StatusFail {
		t.Errorf("expected status fail, got %v", outcome.Status)
	}
	if outcome.FailureReason == "" {
		t.Error("expected failure reason to be set")
	}
}

func TestCodergenHandlerWithBackendError(t *testing.T) {
	backend := &fakeBackend{
		runAgentFn: func(ctx context.Context, config AgentRunConfig) (*AgentRunResult, error) {
			return nil, fmt.Errorf("API key missing")
		},
	}
	h := &CodergenHandler{Backend: backend}

	node := &Node{
		ID: "codegen_error",
		Attrs: map[string]string{
			"shape":  "box",
			"prompt": "do something",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if outcome.Status != StatusFail {
		t.Errorf("expected status fail on backend error, got %v", outcome.Status)
	}
	if !strings.Contains(outcome.FailureReason, "API key missing") {
		t.Errorf("expected failure reason to mention API key, got %q", outcome.FailureReason)
	}
}

func TestCodergenHandlerWithNilBackendFallsBackToStub(t *testing.T) {
	h := &CodergenHandler{Backend: nil}

	node := &Node{
		ID: "codegen_stub",
		Attrs: map[string]string{
			"shape":  "box",
			"prompt": "stub task",
			"label":  "Stub Label",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if outcome.Status != StatusSuccess {
		t.Errorf("expected status success from stub, got %v", outcome.Status)
	}
	if !strings.Contains(outcome.Notes, "stub") {
		t.Errorf("expected stub notes, got %q", outcome.Notes)
	}
}

func TestCodergenHandlerWithBackendStoresArtifact(t *testing.T) {
	backend := &fakeBackend{
		runAgentFn: func(ctx context.Context, config AgentRunConfig) (*AgentRunResult, error) {
			return &AgentRunResult{
				Output:     "here is the generated code\nfunc hello() {}",
				ToolCalls:  5,
				TokensUsed: 1500,
				Success:    true,
			}, nil
		},
	}
	h := &CodergenHandler{Backend: backend}

	node := &Node{
		ID: "codegen_artifact",
		Attrs: map[string]string{
			"shape":  "box",
			"prompt": "generate code",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusSuccess {
		t.Errorf("expected status success, got %v", outcome.Status)
	}

	// Verify the output was stored as an artifact
	artifactID := "codegen_artifact.output"
	data, retrieveErr := store.Retrieve(artifactID)
	if retrieveErr != nil {
		t.Fatalf("failed to retrieve artifact: %v", retrieveErr)
	}
	if !strings.Contains(string(data), "here is the generated code") {
		t.Errorf("artifact should contain agent output, got %q", string(data))
	}
}

func TestCodergenHandlerWithBackendRecordsContextUpdates(t *testing.T) {
	backend := &fakeBackend{
		runAgentFn: func(ctx context.Context, config AgentRunConfig) (*AgentRunResult, error) {
			return &AgentRunResult{
				Output:     "done",
				ToolCalls:  7,
				TokensUsed: 2000,
				Success:    true,
			}, nil
		},
	}
	h := &CodergenHandler{Backend: backend}

	node := &Node{
		ID: "codegen_ctx",
		Attrs: map[string]string{
			"shape":        "box",
			"prompt":       "build feature",
			"llm_model":    "gpt-4",
			"llm_provider": "openai",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if outcome.ContextUpdates["last_stage"] != "codegen_ctx" {
		t.Errorf("expected last_stage = codegen_ctx, got %v", outcome.ContextUpdates["last_stage"])
	}
	if outcome.ContextUpdates["codergen.model"] != "gpt-4" {
		t.Errorf("expected codergen.model = gpt-4, got %v", outcome.ContextUpdates["codergen.model"])
	}
	if outcome.ContextUpdates["codergen.provider"] != "openai" {
		t.Errorf("expected codergen.provider = openai, got %v", outcome.ContextUpdates["codergen.provider"])
	}
	if outcome.ContextUpdates["codergen.tool_calls"] != 7 {
		t.Errorf("expected codergen.tool_calls = 7, got %v", outcome.ContextUpdates["codergen.tool_calls"])
	}
	if outcome.ContextUpdates["codergen.tokens_used"] != 2000 {
		t.Errorf("expected codergen.tokens_used = 2000, got %v", outcome.ContextUpdates["codergen.tokens_used"])
	}
}

func TestCodergenHandlerWithBackendUsesLabelFallback(t *testing.T) {
	backend := &fakeBackend{}
	h := &CodergenHandler{Backend: backend}

	node := &Node{
		ID: "codegen_label",
		Attrs: map[string]string{
			"shape": "box",
			"label": "My Label As Prompt",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	_, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(backend.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(backend.calls))
	}
	if backend.calls[0].Prompt != "My Label As Prompt" {
		t.Errorf("expected prompt to fall back to label, got %q", backend.calls[0].Prompt)
	}
}

func TestCodergenHandlerWithBackendUsesNodeIDFallback(t *testing.T) {
	backend := &fakeBackend{}
	h := &CodergenHandler{Backend: backend}

	node := &Node{
		ID:    "codegen_nolabel",
		Attrs: map[string]string{"shape": "box"},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	_, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if backend.calls[0].Prompt != "codegen_nolabel" {
		t.Errorf("expected prompt to fall back to node ID, got %q", backend.calls[0].Prompt)
	}
}

func TestCodergenHandlerWithBackendPassesWorkDir(t *testing.T) {
	backend := &fakeBackend{}
	h := &CodergenHandler{Backend: backend}

	node := &Node{
		ID: "codegen_workdir",
		Attrs: map[string]string{
			"shape":   "box",
			"prompt":  "do work",
			"workdir": "/custom/work/dir",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	_, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if backend.calls[0].WorkDir != "/custom/work/dir" {
		t.Errorf("expected workdir '/custom/work/dir', got %q", backend.calls[0].WorkDir)
	}
}

func TestCodergenHandlerWithBackendRespectsContextCancellation(t *testing.T) {
	h := &CodergenHandler{Backend: &fakeBackend{}}

	node := &Node{
		ID:    "codegen_cancel",
		Attrs: map[string]string{"shape": "box", "prompt": "cancelled"},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := h.Execute(ctx, node, pctx, store)
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}
