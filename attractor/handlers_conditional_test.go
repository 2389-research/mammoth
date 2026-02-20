// ABOUTME: Tests for ConditionalHandler covering both prompt-driven agent execution and pass-through behavior.
// ABOUTME: Validates outcome detection, nil-backend error, config passthrough, and backward-compatible pass-through mode.
package attractor

import (
	"context"
	"strings"
	"testing"
)

func TestConditionalHandlerWithPromptCallsBackend(t *testing.T) {
	backend := &fakeBackend{}
	h := &ConditionalHandler{Backend: backend}

	node := &Node{
		ID: "check_tests",
		Attrs: map[string]string{
			"shape":  "diamond",
			"prompt": "Run the test suite and report whether all tests pass",
		},
	}
	pctx := NewContext()
	pctx.Set("goal", "ensure code quality")
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if outcome.Status != StatusSuccess {
		t.Errorf("expected status success, got %v", outcome.Status)
	}

	// Backend should have been called exactly once
	if len(backend.calls) != 1 {
		t.Fatalf("expected 1 backend call, got %d", len(backend.calls))
	}

	call := backend.calls[0]
	if call.Prompt != "Run the test suite and report whether all tests pass" {
		t.Errorf("expected prompt passed to backend, got %q", call.Prompt)
	}
	if call.NodeID != "check_tests" {
		t.Errorf("expected node ID 'check_tests', got %q", call.NodeID)
	}
	if call.Goal != "ensure code quality" {
		t.Errorf("expected goal 'ensure code quality', got %q", call.Goal)
	}

	// Outcome should be set in context updates
	if outcome.ContextUpdates["outcome"] != "success" {
		t.Errorf("expected outcome='success' in context updates, got %v", outcome.ContextUpdates["outcome"])
	}
}

func TestConditionalHandlerWithPromptDetectsOutcomeFail(t *testing.T) {
	backend := &fakeBackend{
		runAgentFn: func(ctx context.Context, config AgentRunConfig) (*AgentRunResult, error) {
			return &AgentRunResult{
				Output:  "Tests ran. 3 of 10 failed. OUTCOME:FAIL",
				Success: true,
			}, nil
		},
	}
	h := &ConditionalHandler{Backend: backend}

	node := &Node{
		ID: "check_quality",
		Attrs: map[string]string{
			"shape":  "diamond",
			"prompt": "Check code quality",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if outcome.Status != StatusFail {
		t.Errorf("expected status fail from OUTCOME:FAIL marker, got %v", outcome.Status)
	}

	if outcome.ContextUpdates["outcome"] != "fail" {
		t.Errorf("expected outcome='fail' in context updates, got %v", outcome.ContextUpdates["outcome"])
	}
}

func TestConditionalHandlerWithoutPromptPassesThrough(t *testing.T) {
	h := &ConditionalHandler{}

	node := &Node{
		ID: "branch_check",
		Attrs: map[string]string{
			"shape": "diamond",
		},
	}
	pctx := NewContext()
	pctx.Set("outcome", "fail")
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if outcome.Status != StatusFail {
		t.Errorf("expected status fail from pass-through, got %v", outcome.Status)
	}

	// Pass-through should preserve the last_stage context update
	if outcome.ContextUpdates["last_stage"] != "branch_check" {
		t.Errorf("expected last_stage='branch_check', got %v", outcome.ContextUpdates["last_stage"])
	}
}

func TestConditionalHandlerWithPromptNilBackendReturnsFail(t *testing.T) {
	h := &ConditionalHandler{Backend: nil}

	node := &Node{
		ID: "check_no_backend",
		Attrs: map[string]string{
			"shape":  "diamond",
			"prompt": "Evaluate something with LLM",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if outcome.Status != StatusFail {
		t.Errorf("expected status fail when backend is nil, got %v", outcome.Status)
	}
	if !strings.Contains(outcome.FailureReason, "no LLM backend configured") {
		t.Errorf("expected failure reason about no LLM backend, got %q", outcome.FailureReason)
	}
}

func TestConditionalHandlerWithPromptAgentFailure(t *testing.T) {
	backend := &fakeBackend{
		runAgentFn: func(ctx context.Context, config AgentRunConfig) (*AgentRunResult, error) {
			return &AgentRunResult{
				Output:  "agent crashed without an outcome marker",
				Success: false,
			}, nil
		},
	}
	h := &ConditionalHandler{Backend: backend}

	node := &Node{
		ID: "check_agent_fail",
		Attrs: map[string]string{
			"shape":  "diamond",
			"prompt": "Check something",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No outcome marker + Success=false means fail
	if outcome.Status != StatusFail {
		t.Errorf("expected status fail when agent reports !Success, got %v", outcome.Status)
	}

	if outcome.ContextUpdates["outcome"] != "fail" {
		t.Errorf("expected outcome='fail' in context updates, got %v", outcome.ContextUpdates["outcome"])
	}
}

func TestConditionalHandlerWithPromptPassesConfig(t *testing.T) {
	var receivedConfig AgentRunConfig
	backend := &fakeBackend{
		runAgentFn: func(ctx context.Context, config AgentRunConfig) (*AgentRunResult, error) {
			receivedConfig = config
			return &AgentRunResult{
				Output:  "OUTCOME:PASS",
				Success: true,
			}, nil
		},
	}

	h := &ConditionalHandler{
		Backend: backend,
		BaseURL: "https://default.example.com",
	}

	node := &Node{
		ID: "check_config",
		Attrs: map[string]string{
			"shape":        "diamond",
			"prompt":       "Evaluate the code",
			"llm_model":    "claude-sonnet-4-5",
			"llm_provider": "anthropic",
			"max_turns":    "10",
			"base_url":     "https://node.example.com",
		},
	}
	pctx := NewContext()
	pctx.Set("goal", "validate everything")
	store := NewArtifactStore(t.TempDir())

	_, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedConfig.Model != "claude-sonnet-4-5" {
		t.Errorf("expected model 'claude-sonnet-4-5', got %q", receivedConfig.Model)
	}
	if receivedConfig.Provider != "anthropic" {
		t.Errorf("expected provider 'anthropic', got %q", receivedConfig.Provider)
	}
	if receivedConfig.MaxTurns != 10 {
		t.Errorf("expected max_turns 10, got %d", receivedConfig.MaxTurns)
	}
	// Node base_url should override handler default
	if receivedConfig.BaseURL != "https://node.example.com" {
		t.Errorf("expected base_url 'https://node.example.com', got %q", receivedConfig.BaseURL)
	}
	if receivedConfig.Goal != "validate everything" {
		t.Errorf("expected goal 'validate everything', got %q", receivedConfig.Goal)
	}
	if receivedConfig.Prompt != "Evaluate the code" {
		t.Errorf("expected prompt 'Evaluate the code', got %q", receivedConfig.Prompt)
	}
}

func TestConditionalHandlerWithPromptStoresArtifact(t *testing.T) {
	backend := &fakeBackend{
		runAgentFn: func(ctx context.Context, config AgentRunConfig) (*AgentRunResult, error) {
			return &AgentRunResult{
				Output:  "Agent evaluation output: all tests pass. OUTCOME:PASS",
				Success: true,
			}, nil
		},
	}
	h := &ConditionalHandler{Backend: backend}

	node := &Node{
		ID: "check_artifact",
		Attrs: map[string]string{
			"shape":  "diamond",
			"prompt": "Evaluate tests",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	_, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	artifactID := "check_artifact.output"
	data, retrieveErr := store.Retrieve(artifactID)
	if retrieveErr != nil {
		t.Fatalf("failed to retrieve artifact: %v", retrieveErr)
	}
	if !strings.Contains(string(data), "Agent evaluation output") {
		t.Errorf("artifact should contain agent output, got %q", string(data))
	}
}

func TestConditionalHandlerWithoutPromptSuccessPassThrough(t *testing.T) {
	h := &ConditionalHandler{}

	node := &Node{
		ID: "branch_success",
		Attrs: map[string]string{
			"shape": "diamond",
		},
	}
	pctx := NewContext()
	pctx.Set("outcome", "success")
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if outcome.Status != StatusSuccess {
		t.Errorf("expected status success from pass-through, got %v", outcome.Status)
	}
}

func TestConditionalHandlerWithoutPromptDefaultsToSuccess(t *testing.T) {
	h := &ConditionalHandler{}

	node := &Node{
		ID: "branch_default",
		Attrs: map[string]string{
			"shape": "diamond",
		},
	}
	pctx := NewContext()
	// No "outcome" set in context
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if outcome.Status != StatusSuccess {
		t.Errorf("expected default status success when no outcome in context, got %v", outcome.Status)
	}
}

func TestConditionalHandlerRespectsContextCancellation(t *testing.T) {
	h := &ConditionalHandler{}

	node := &Node{
		ID:    "branch_cancel",
		Attrs: map[string]string{"shape": "diamond"},
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

func TestConditionalHandlerWithPromptDefaultMaxTurns(t *testing.T) {
	backend := &fakeBackend{}
	h := &ConditionalHandler{Backend: backend}

	node := &Node{
		ID: "check_default_turns",
		Attrs: map[string]string{
			"shape":  "diamond",
			"prompt": "evaluate something",
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
	if backend.calls[0].MaxTurns != 20 {
		t.Errorf("expected default max turns 20, got %d", backend.calls[0].MaxTurns)
	}
}

func TestConditionalHandlerWithPromptBaseURLFallbackOrder(t *testing.T) {
	tests := []struct {
		name           string
		nodeBaseURL    string
		contextBaseURL string
		handlerBaseURL string
		expected       string
	}{
		{
			name:           "node attr wins over context and handler",
			nodeBaseURL:    "https://node.example.com",
			contextBaseURL: "https://context.example.com",
			handlerBaseURL: "https://handler.example.com",
			expected:       "https://node.example.com",
		},
		{
			name:           "context wins over handler when node empty",
			contextBaseURL: "https://context.example.com",
			handlerBaseURL: "https://handler.example.com",
			expected:       "https://context.example.com",
		},
		{
			name:           "handler default used when node and context empty",
			handlerBaseURL: "https://handler.example.com",
			expected:       "https://handler.example.com",
		},
		{
			name:     "all empty yields empty",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var receivedConfig AgentRunConfig
			backend := &fakeBackend{
				runAgentFn: func(ctx context.Context, config AgentRunConfig) (*AgentRunResult, error) {
					receivedConfig = config
					return &AgentRunResult{Success: true, Output: "OUTCOME:PASS"}, nil
				},
			}

			attrs := map[string]string{
				"shape":  "diamond",
				"prompt": "test",
			}
			if tt.nodeBaseURL != "" {
				attrs["base_url"] = tt.nodeBaseURL
			}

			h := &ConditionalHandler{Backend: backend, BaseURL: tt.handlerBaseURL}
			node := &Node{ID: "check_baseurl_" + tt.name, Attrs: attrs}
			pctx := NewContext()
			if tt.contextBaseURL != "" {
				pctx.Set("base_url", tt.contextBaseURL)
			}
			store := NewArtifactStore(t.TempDir())

			_, err := h.Execute(context.Background(), node, pctx, store)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if receivedConfig.BaseURL != tt.expected {
				t.Errorf("expected BaseURL %q, got %q", tt.expected, receivedConfig.BaseURL)
			}
		})
	}
}

func TestConditionalHandlerWithPromptPassesEventHandler(t *testing.T) {
	var receivedHandler func(EngineEvent)
	backend := &fakeBackend{
		runAgentFn: func(ctx context.Context, config AgentRunConfig) (*AgentRunResult, error) {
			receivedHandler = config.EventHandler
			return &AgentRunResult{Success: true, Output: "OUTCOME:PASS"}, nil
		},
	}

	var events []EngineEvent
	handler := func(evt EngineEvent) {
		events = append(events, evt)
	}

	h := &ConditionalHandler{
		Backend:      backend,
		EventHandler: handler,
	}

	node := &Node{
		ID: "check_events",
		Attrs: map[string]string{
			"shape":  "diamond",
			"prompt": "test events",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	_, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedHandler == nil {
		t.Fatal("expected EventHandler to be passed through to AgentRunConfig")
	}

	receivedHandler(EngineEvent{Type: EventAgentLLMTurn, NodeID: "check_events"})
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
}

func TestEngineWiresBackendIntoConditionalHandler(t *testing.T) {
	registry := DefaultHandlerRegistry()

	// Before wiring, backend should be nil
	condHandler := registry.Get("conditional")
	if condHandler == nil {
		t.Fatal("expected conditional handler in default registry")
	}
	ch, ok := condHandler.(*ConditionalHandler)
	if !ok {
		t.Fatalf("expected *ConditionalHandler, got %T", condHandler)
	}
	if ch.Backend != nil {
		t.Error("expected nil backend before wiring")
	}

	// Simulate engine wiring (same pattern as engine.go does for codergen)
	backend := &fakeBackend{}
	if condHandler := registry.Get("conditional"); condHandler != nil {
		if ch, ok := unwrapHandler(condHandler).(*ConditionalHandler); ok {
			ch.Backend = backend
			ch.BaseURL = "https://test.example.com"
		}
	}

	// After wiring, backend should be set
	ch2 := registry.Get("conditional").(*ConditionalHandler)
	if ch2.Backend == nil {
		t.Error("expected backend to be wired")
	}
	if ch2.BaseURL != "https://test.example.com" {
		t.Errorf("expected base URL to be wired, got %q", ch2.BaseURL)
	}
}

func TestConditionalHandlerWithPromptUsesLabelFallback(t *testing.T) {
	backend := &fakeBackend{}
	h := &ConditionalHandler{Backend: backend}

	node := &Node{
		ID: "check_label_fallback",
		Attrs: map[string]string{
			"shape": "diamond",
			"label": "Is the code ready?",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	_, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// label-only nodes should NOT trigger agent mode (no "prompt" attr)
	// They should use pass-through behavior
	if len(backend.calls) != 0 {
		t.Errorf("expected pass-through (no backend call) when only label is set, got %d calls", len(backend.calls))
	}
}
