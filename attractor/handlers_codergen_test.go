// ABOUTME: Tests for CodergenHandler wired to a CodergenBackend for real agent execution.
// ABOUTME: Covers backend integration, nil-backend error, error handling, config passthrough, and artifact storage.
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

func TestCodergenHandlerWithNilBackendReturnsError(t *testing.T) {
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

	if outcome.Status != StatusFail {
		t.Errorf("expected status fail from nil backend, got %v", outcome.Status)
	}
	if !strings.Contains(outcome.FailureReason, "no LLM backend configured") {
		t.Errorf("expected failure reason about no LLM backend, got %q", outcome.FailureReason)
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

func TestCodergenHandlerEventHandlerFieldPassedToConfig(t *testing.T) {
	var receivedHandler func(EngineEvent)
	backend := &fakeBackend{
		runAgentFn: func(ctx context.Context, config AgentRunConfig) (*AgentRunResult, error) {
			receivedHandler = config.EventHandler
			return &AgentRunResult{Success: true}, nil
		},
	}

	var events []EngineEvent
	handler := func(evt EngineEvent) {
		events = append(events, evt)
	}

	h := &CodergenHandler{
		Backend:      backend,
		EventHandler: handler,
	}

	node := &Node{
		ID:    "codegen_event_test",
		Attrs: map[string]string{"shape": "box", "prompt": "test"},
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

	// Call the handler to verify it's the same function
	receivedHandler(EngineEvent{Type: EventAgentLLMTurn, NodeID: "codegen_event_test"})
	if len(events) != 1 {
		t.Fatalf("expected 1 event from passed handler, got %d", len(events))
	}
	if events[0].Type != EventAgentLLMTurn {
		t.Errorf("expected EventAgentLLMTurn, got %q", events[0].Type)
	}
}

func TestCodergenHandlerNilEventHandlerPassedThrough(t *testing.T) {
	var receivedConfig AgentRunConfig
	backend := &fakeBackend{
		runAgentFn: func(ctx context.Context, config AgentRunConfig) (*AgentRunResult, error) {
			receivedConfig = config
			return &AgentRunResult{Success: true}, nil
		},
	}

	h := &CodergenHandler{
		Backend: backend,
		// EventHandler deliberately nil
	}

	node := &Node{
		ID:    "codegen_nil_event",
		Attrs: map[string]string{"shape": "box", "prompt": "test"},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	_, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedConfig.EventHandler != nil {
		t.Error("expected nil EventHandler to be passed through as nil")
	}
}

func TestCodergenHandlerRecordsGranularTokenUsageInContext(t *testing.T) {
	backend := &fakeBackend{
		runAgentFn: func(ctx context.Context, config AgentRunConfig) (*AgentRunResult, error) {
			return &AgentRunResult{
				Output:     "done",
				ToolCalls:  5,
				TokensUsed: 1500,
				Success:    true,
				TurnCount:  3,
				Usage: TokenUsage{
					InputTokens:      1000,
					OutputTokens:     500,
					TotalTokens:      1500,
					ReasoningTokens:  200,
					CacheReadTokens:  150,
					CacheWriteTokens: 75,
				},
			}, nil
		},
	}
	h := &CodergenHandler{Backend: backend}

	node := &Node{
		ID:    "codegen_token_usage",
		Attrs: map[string]string{"shape": "box", "prompt": "test"},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if outcome.ContextUpdates["codergen.input_tokens"] != 1000 {
		t.Errorf("expected codergen.input_tokens=1000, got %v", outcome.ContextUpdates["codergen.input_tokens"])
	}
	if outcome.ContextUpdates["codergen.output_tokens"] != 500 {
		t.Errorf("expected codergen.output_tokens=500, got %v", outcome.ContextUpdates["codergen.output_tokens"])
	}
	if outcome.ContextUpdates["codergen.reasoning_tokens"] != 200 {
		t.Errorf("expected codergen.reasoning_tokens=200, got %v", outcome.ContextUpdates["codergen.reasoning_tokens"])
	}
	if outcome.ContextUpdates["codergen.cache_read_tokens"] != 150 {
		t.Errorf("expected codergen.cache_read_tokens=150, got %v", outcome.ContextUpdates["codergen.cache_read_tokens"])
	}
	if outcome.ContextUpdates["codergen.cache_write_tokens"] != 75 {
		t.Errorf("expected codergen.cache_write_tokens=75, got %v", outcome.ContextUpdates["codergen.cache_write_tokens"])
	}
}

func TestCodergenHandlerRecordsTurnCountInContext(t *testing.T) {
	backend := &fakeBackend{
		runAgentFn: func(ctx context.Context, config AgentRunConfig) (*AgentRunResult, error) {
			return &AgentRunResult{
				Output:     "done",
				ToolCalls:  5,
				TokensUsed: 1000,
				Success:    true,
				TurnCount:  7,
			}, nil
		},
	}
	h := &CodergenHandler{Backend: backend}

	node := &Node{
		ID:    "codegen_turns",
		Attrs: map[string]string{"shape": "box", "prompt": "test"},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if outcome.ContextUpdates["codergen.turn_count"] != 7 {
		t.Errorf("expected codergen.turn_count = 7, got %v", outcome.ContextUpdates["codergen.turn_count"])
	}
}

func TestCodergenHandlerPassesBaseURLToConfig(t *testing.T) {
	var receivedConfig AgentRunConfig
	backend := &fakeBackend{
		runAgentFn: func(ctx context.Context, config AgentRunConfig) (*AgentRunResult, error) {
			receivedConfig = config
			return &AgentRunResult{Success: true}, nil
		},
	}

	h := &CodergenHandler{Backend: backend}

	node := &Node{
		ID: "codegen_baseurl",
		Attrs: map[string]string{
			"shape":    "box",
			"prompt":   "test",
			"base_url": "https://custom.api.example.com",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	_, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedConfig.BaseURL != "https://custom.api.example.com" {
		t.Errorf("expected BaseURL 'https://custom.api.example.com', got %q", receivedConfig.BaseURL)
	}
}

func TestCodergenHandlerEmptyBaseURLByDefault(t *testing.T) {
	var receivedConfig AgentRunConfig
	backend := &fakeBackend{
		runAgentFn: func(ctx context.Context, config AgentRunConfig) (*AgentRunResult, error) {
			receivedConfig = config
			return &AgentRunResult{Success: true}, nil
		},
	}

	h := &CodergenHandler{Backend: backend}

	node := &Node{
		ID:    "codegen_no_baseurl",
		Attrs: map[string]string{"shape": "box", "prompt": "test"},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	_, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedConfig.BaseURL != "" {
		t.Errorf("expected empty BaseURL by default, got %q", receivedConfig.BaseURL)
	}
}

func TestCodergenHandlerBaseURLFallsBackToHandlerDefault(t *testing.T) {
	var receivedConfig AgentRunConfig
	backend := &fakeBackend{
		runAgentFn: func(ctx context.Context, config AgentRunConfig) (*AgentRunResult, error) {
			receivedConfig = config
			return &AgentRunResult{Success: true}, nil
		},
	}

	h := &CodergenHandler{
		Backend: backend,
		BaseURL: "https://default.api.example.com",
	}

	node := &Node{
		ID:    "codegen_default_baseurl",
		Attrs: map[string]string{"shape": "box", "prompt": "test"},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	_, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedConfig.BaseURL != "https://default.api.example.com" {
		t.Errorf("expected handler default BaseURL, got %q", receivedConfig.BaseURL)
	}
}

func TestCodergenHandlerNodeBaseURLOverridesDefault(t *testing.T) {
	var receivedConfig AgentRunConfig
	backend := &fakeBackend{
		runAgentFn: func(ctx context.Context, config AgentRunConfig) (*AgentRunResult, error) {
			receivedConfig = config
			return &AgentRunResult{Success: true}, nil
		},
	}

	h := &CodergenHandler{
		Backend: backend,
		BaseURL: "https://default.api.example.com",
	}

	node := &Node{
		ID: "codegen_override_baseurl",
		Attrs: map[string]string{
			"shape":    "box",
			"prompt":   "test",
			"base_url": "https://override.api.example.com",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	_, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedConfig.BaseURL != "https://override.api.example.com" {
		t.Errorf("expected node base_url to override handler default, got %q", receivedConfig.BaseURL)
	}
}

func TestEngineWiresBaseURLToCodergenHandler(t *testing.T) {
	var receivedConfig AgentRunConfig
	backend := &stubCodergenBackend{
		runFn: func(ctx context.Context, config AgentRunConfig) (*AgentRunResult, error) {
			receivedConfig = config
			return &AgentRunResult{Success: true}, nil
		},
	}

	g := &Graph{
		Name:         "baseurl_wiring",
		Nodes:        make(map[string]*Node),
		Edges:        make([]*Edge, 0),
		Attrs:        make(map[string]string),
		NodeDefaults: make(map[string]string),
		EdgeDefaults: make(map[string]string),
	}
	g.Nodes["start"] = &Node{ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}}
	g.Nodes["code_task"] = &Node{ID: "code_task", Attrs: map[string]string{"shape": "box", "label": "Code"}}
	g.Nodes["exit"] = &Node{ID: "exit", Attrs: map[string]string{"shape": "Msquare"}}
	g.Edges = append(g.Edges,
		&Edge{From: "start", To: "code_task", Attrs: map[string]string{}},
		&Edge{From: "code_task", To: "exit", Attrs: map[string]string{}},
	)

	engine := NewEngine(EngineConfig{
		Backend:      backend,
		DefaultRetry: RetryPolicyNone(),
		BaseURL:      "https://engine-level.api.example.com",
	})

	_, err := engine.RunGraph(context.Background(), g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedConfig.BaseURL != "https://engine-level.api.example.com" {
		t.Errorf("expected engine-level BaseURL to be wired through, got %q", receivedConfig.BaseURL)
	}
}

func TestEngineWiresEventHandlerToCodergenHandler(t *testing.T) {
	var events []EngineEvent
	backend := &stubCodergenBackend{
		runFn: func(ctx context.Context, config AgentRunConfig) (*AgentRunResult, error) {
			// Simulate the backend emitting an event through the handler
			if config.EventHandler != nil {
				config.EventHandler(EngineEvent{
					Type:   EventAgentLLMTurn,
					NodeID: "code_task",
					Data:   map[string]any{"tokens": 42},
				})
			}
			return &AgentRunResult{Success: true, TurnCount: 1}, nil
		},
	}

	g := &Graph{
		Name:         "event_wiring",
		Nodes:        make(map[string]*Node),
		Edges:        make([]*Edge, 0),
		Attrs:        make(map[string]string),
		NodeDefaults: make(map[string]string),
		EdgeDefaults: make(map[string]string),
	}
	g.Nodes["start"] = &Node{ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}}
	g.Nodes["code_task"] = &Node{ID: "code_task", Attrs: map[string]string{"shape": "box", "label": "Code"}}
	g.Nodes["exit"] = &Node{ID: "exit", Attrs: map[string]string{"shape": "Msquare"}}
	g.Edges = append(g.Edges,
		&Edge{From: "start", To: "code_task", Attrs: map[string]string{}},
		&Edge{From: "code_task", To: "exit", Attrs: map[string]string{}},
	)

	engine := NewEngine(EngineConfig{
		Backend:      backend,
		DefaultRetry: RetryPolicyNone(),
		EventHandler: func(evt EngineEvent) {
			events = append(events, evt)
		},
	})

	_, err := engine.RunGraph(context.Background(), g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Look for the agent.llm_turn event among all emitted events
	found := false
	for _, evt := range events {
		if evt.Type == EventAgentLLMTurn && evt.NodeID == "code_task" {
			found = true
			if evt.Data["tokens"] != 42 {
				t.Errorf("expected tokens=42 in agent event, got %v", evt.Data["tokens"])
			}
		}
	}
	if !found {
		t.Error("expected agent.llm_turn event to be emitted through engine event handler")
	}
}
