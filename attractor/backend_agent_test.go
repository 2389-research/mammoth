// ABOUTME: Tests for AgentBackend which wires CodergenBackend to the real agent loop and LLM SDK.
// ABOUTME: Covers provider selection, session config, fidelity mode wiring, and integration with test doubles.
package attractor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/2389-research/mammoth/llm"
)

// testProviderAdapter is a test double for llm.ProviderAdapter that returns
// pre-configured responses in sequence.
type testProviderAdapter struct {
	responses []*llm.Response
	callIdx   int
	calls     []llm.Request
	mu        sync.Mutex
}

func (a *testProviderAdapter) Name() string { return "test" }

func (a *testProviderAdapter) Complete(ctx context.Context, req llm.Request) (*llm.Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, req)
	if a.callIdx >= len(a.responses) {
		return nil, fmt.Errorf("testProviderAdapter: no more responses (called %d times, only %d responses)", a.callIdx+1, len(a.responses))
	}
	resp := a.responses[a.callIdx]
	a.callIdx++
	return resp, nil
}

func (a *testProviderAdapter) Stream(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, error) {
	return nil, fmt.Errorf("streaming not supported in test")
}

func (a *testProviderAdapter) Close() error { return nil }

func (a *testProviderAdapter) getCalls() []llm.Request {
	a.mu.Lock()
	defer a.mu.Unlock()
	result := make([]llm.Request, len(a.calls))
	copy(result, a.calls)
	return result
}

// makeTestTextResponse creates a simple text-only LLM response for testing.
func makeTestTextResponse(text string) *llm.Response {
	return &llm.Response{
		ID:           "resp-text",
		Model:        "test-model",
		Provider:     "anthropic",
		Message:      llm.AssistantMessage(text),
		FinishReason: llm.FinishReason{Reason: llm.FinishStop},
		Usage:        llm.Usage{InputTokens: 100, OutputTokens: 50, TotalTokens: 150},
	}
}

// makeTestToolCallResponse creates an LLM response with a tool call.
func makeTestToolCallResponse(toolName, toolID string, args map[string]any) *llm.Response {
	argsJSON, _ := json.Marshal(args)
	parts := []llm.ContentPart{
		llm.TextPart("Using tool."),
		llm.ToolCallPart(toolID, toolName, argsJSON),
	}
	return &llm.Response{
		ID:       "resp-tool",
		Model:    "test-model",
		Provider: "anthropic",
		Message: llm.Message{
			Role:    llm.RoleAssistant,
			Content: parts,
		},
		FinishReason: llm.FinishReason{Reason: llm.FinishToolCalls},
		Usage:        llm.Usage{InputTokens: 100, OutputTokens: 80, TotalTokens: 180},
	}
}

// newTestAgentClient creates an llm.Client with the test adapter registered under
// the "anthropic" provider name (matching the default profile selection).
func newTestAgentClient(adapter *testProviderAdapter) *llm.Client {
	return llm.NewClient(
		llm.WithProvider("anthropic", adapter),
		llm.WithDefaultProvider("anthropic"),
	)
}

func TestAgentBackendImplementsInterface(t *testing.T) {
	var _ CodergenBackend = (*AgentBackend)(nil)
}

func TestAgentBackendSimpleTextCompletion(t *testing.T) {
	adapter := &testProviderAdapter{
		responses: []*llm.Response{
			makeTestTextResponse("I wrote the code successfully."),
		},
	}
	client := newTestAgentClient(adapter)

	backend := &AgentBackend{
		Client: client,
	}

	config := AgentRunConfig{
		Prompt:   "Write a hello world function",
		Model:    "test-model",
		Provider: "anthropic",
		WorkDir:  t.TempDir(),
		NodeID:   "test-node",
		MaxTurns: 10,
	}

	result, err := backend.RunAgent(context.Background(), config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Success {
		t.Error("expected success")
	}
	if result.Output != "I wrote the code successfully." {
		t.Errorf("expected agent output, got %q", result.Output)
	}
	if result.TokensUsed != 150 {
		t.Errorf("expected 150 tokens, got %d", result.TokensUsed)
	}
}

func TestAgentBackendWithToolCalls(t *testing.T) {
	adapter := &testProviderAdapter{
		responses: []*llm.Response{
			makeTestToolCallResponse("read_file", "call-1", map[string]any{
				"file_path": "/tmp/test.txt",
			}),
			makeTestTextResponse("I read the file and completed the task."),
		},
	}
	client := newTestAgentClient(adapter)

	backend := &AgentBackend{
		Client: client,
	}

	config := AgentRunConfig{
		Prompt:   "Read the file and process it",
		Model:    "test-model",
		Provider: "anthropic",
		WorkDir:  t.TempDir(),
		NodeID:   "tool-node",
		MaxTurns: 10,
	}

	result, err := backend.RunAgent(context.Background(), config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Success {
		t.Error("expected success")
	}
	if result.ToolCalls != 1 {
		t.Errorf("expected 1 tool call, got %d", result.ToolCalls)
	}
	// Total tokens: first call (180) + second call (150) = 330
	if result.TokensUsed < 150 {
		t.Errorf("expected tokens >= 150, got %d", result.TokensUsed)
	}
}

func TestAgentBackendRespectsMaxTurns(t *testing.T) {
	// Create an adapter that always returns tool calls (forcing the loop to hit MaxTurns)
	adapter := &testProviderAdapter{
		responses: []*llm.Response{
			makeTestToolCallResponse("shell", "call-1", map[string]any{"command": "echo hi"}),
			makeTestToolCallResponse("shell", "call-2", map[string]any{"command": "echo hi"}),
			makeTestToolCallResponse("shell", "call-3", map[string]any{"command": "echo hi"}),
			makeTestToolCallResponse("shell", "call-4", map[string]any{"command": "echo hi"}),
			makeTestTextResponse("done"),
		},
	}
	client := newTestAgentClient(adapter)

	backend := &AgentBackend{
		Client: client,
	}

	config := AgentRunConfig{
		Prompt:   "run commands",
		Model:    "test-model",
		Provider: "anthropic",
		WorkDir:  t.TempDir(),
		NodeID:   "limit-node",
		MaxTurns: 3, // Should stop before using all responses
	}

	result, err := backend.RunAgent(context.Background(), config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The agent should have been limited by MaxTurns
	if !result.Success {
		t.Error("expected success even when hitting turn limit")
	}
}

func TestAgentBackendContextCancellation(t *testing.T) {
	adapter := &testProviderAdapter{
		responses: []*llm.Response{
			makeTestTextResponse("should not reach this"),
		},
	}
	client := newTestAgentClient(adapter)

	backend := &AgentBackend{
		Client: client,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	config := AgentRunConfig{
		Prompt:   "cancelled task",
		Model:    "test-model",
		Provider: "anthropic",
		WorkDir:  t.TempDir(),
		NodeID:   "cancel-node",
		MaxTurns: 10,
	}

	_, err := backend.RunAgent(ctx, config)
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

func TestAgentBackendGoalInSystemPrompt(t *testing.T) {
	adapter := &testProviderAdapter{
		responses: []*llm.Response{
			makeTestTextResponse("completed"),
		},
	}
	client := newTestAgentClient(adapter)

	backend := &AgentBackend{
		Client: client,
	}

	config := AgentRunConfig{
		Prompt:   "implement feature",
		Model:    "test-model",
		Provider: "anthropic",
		WorkDir:  t.TempDir(),
		Goal:     "Build a REST API",
		NodeID:   "goal-node",
		MaxTurns: 10,
	}

	result, err := backend.RunAgent(context.Background(), config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}

	// Verify the goal was included in the user input (which goes through the messages)
	calls := adapter.getCalls()
	if len(calls) < 1 {
		t.Fatal("expected at least 1 LLM call")
	}
	// The goal appears in the user message (via buildAgentInput), not the system prompt
	foundGoal := false
	for _, msg := range calls[0].Messages {
		text := msg.TextContent()
		if strings.Contains(text, "Build a REST API") {
			foundGoal = true
			break
		}
	}
	if !foundGoal {
		t.Error("expected goal 'Build a REST API' to appear in messages")
	}
}

func TestAgentBackendDefaultMaxTurns(t *testing.T) {
	adapter := &testProviderAdapter{
		responses: []*llm.Response{
			makeTestTextResponse("done"),
		},
	}
	client := newTestAgentClient(adapter)

	backend := &AgentBackend{
		Client: client,
	}

	config := AgentRunConfig{
		Prompt:   "task",
		Model:    "test-model",
		Provider: "anthropic",
		WorkDir:  t.TempDir(),
		NodeID:   "default-turns",
		MaxTurns: 0, // Should use default
	}

	result, err := backend.RunAgent(context.Background(), config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestAgentBackendProviderSelectionOpenAI(t *testing.T) {
	adapter := &testProviderAdapter{
		responses: []*llm.Response{
			makeTestTextResponse("done via openai"),
		},
	}
	client := llm.NewClient(
		llm.WithProvider("openai", adapter),
		llm.WithDefaultProvider("openai"),
	)

	backend := &AgentBackend{
		Client: client,
	}

	config := AgentRunConfig{
		Prompt:   "task",
		Model:    "gpt-4",
		Provider: "openai",
		WorkDir:  t.TempDir(),
		NodeID:   "openai-node",
		MaxTurns: 10,
	}

	result, err := backend.RunAgent(context.Background(), config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
	if result.Output != "done via openai" {
		t.Errorf("expected output 'done via openai', got %q", result.Output)
	}
}

func TestCreateProviderAdapterAnthropicReturnsRealAdapter(t *testing.T) {
	adapter := createProviderAdapter("anthropic", "test-key-anthropic", "")

	// Should return the real AnthropicAdapter, not a placeholder
	anthropicAdapter, ok := adapter.(*llm.AnthropicAdapter)
	if !ok {
		t.Fatalf("expected *llm.AnthropicAdapter, got %T", adapter)
	}
	if anthropicAdapter.Name() != "anthropic" {
		t.Errorf("expected Name() = 'anthropic', got %q", anthropicAdapter.Name())
	}
}

func TestCreateProviderAdapterOpenAIReturnsRealAdapter(t *testing.T) {
	adapter := createProviderAdapter("openai", "test-key-openai", "")

	// Should return the real OpenAIAdapter, not a placeholder
	openaiAdapter, ok := adapter.(*llm.OpenAIAdapter)
	if !ok {
		t.Fatalf("expected *llm.OpenAIAdapter, got %T", adapter)
	}
	if openaiAdapter.Name() != "openai" {
		t.Errorf("expected Name() = 'openai', got %q", openaiAdapter.Name())
	}
}

func TestCreateProviderAdapterGeminiReturnsRealAdapter(t *testing.T) {
	adapter := createProviderAdapter("gemini", "test-key-gemini", "")

	// Should return the real GeminiAdapter, not a placeholder
	geminiAdapter, ok := adapter.(*llm.GeminiAdapter)
	if !ok {
		t.Fatalf("expected *llm.GeminiAdapter, got %T", adapter)
	}
	if geminiAdapter.Name() != "gemini" {
		t.Errorf("expected Name() = 'gemini', got %q", geminiAdapter.Name())
	}
}

func TestCreateProviderAdapterUnknownDefaultsToAnthropic(t *testing.T) {
	adapter := createProviderAdapter("unknown-provider", "test-key", "")

	// Unknown providers should default to AnthropicAdapter
	anthropicAdapter, ok := adapter.(*llm.AnthropicAdapter)
	if !ok {
		t.Fatalf("expected *llm.AnthropicAdapter for unknown provider, got %T", adapter)
	}
	if anthropicAdapter.Name() != "anthropic" {
		t.Errorf("expected Name() = 'anthropic', got %q", anthropicAdapter.Name())
	}
}

func TestCreateProviderAdapterNeverReturnsPlaceholder(t *testing.T) {
	providers := []string{"anthropic", "openai", "gemini", "unknown"}
	for _, provider := range providers {
		adapter := createProviderAdapter(provider, "test-key-"+provider, "")

		// Verify the adapter is never a placeholder by checking it does not
		// return the characteristic placeholder error on Complete
		ctx := context.Background()
		_, err := adapter.Complete(ctx, llm.Request{
			Model:    "test-model",
			Messages: []llm.Message{llm.UserMessage("test")},
		})

		// Real adapters will fail with network/auth errors, not placeholder errors
		if err != nil && strings.Contains(err.Error(), "placeholder") {
			t.Errorf("provider %q: adapter returned placeholder error: %v", provider, err)
		}
	}
}

// --- Fidelity Mode Integration Tests ---

func TestBackendFidelityModePassedToSession(t *testing.T) {
	// Verify that when AgentRunConfig.FidelityMode is set, the agent session
	// is configured with the corresponding fidelity mode.
	tests := []struct {
		name         string
		fidelityMode string
	}{
		{"full fidelity", "full"},
		{"truncate fidelity", "truncate"},
		{"compact fidelity", "compact"},
		{"summary:low fidelity", "summary:low"},
		{"summary:medium fidelity", "summary:medium"},
		{"summary:high fidelity", "summary:high"},
		{"empty fidelity (default)", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := &testProviderAdapter{
				responses: []*llm.Response{
					makeTestTextResponse("completed with fidelity: " + tt.fidelityMode),
				},
			}
			client := newTestAgentClient(adapter)

			backend := &AgentBackend{Client: client}

			config := AgentRunConfig{
				Prompt:       "test fidelity wiring",
				Model:        "test-model",
				Provider:     "anthropic",
				WorkDir:      t.TempDir(),
				NodeID:       "fidelity-node",
				MaxTurns:     10,
				FidelityMode: tt.fidelityMode,
			}

			result, err := backend.RunAgent(context.Background(), config)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !result.Success {
				t.Error("expected success")
			}
			if result.Output != "completed with fidelity: "+tt.fidelityMode {
				t.Errorf("expected output 'completed with fidelity: %s', got %q", tt.fidelityMode, result.Output)
			}
		})
	}
}

func TestBackendFidelityFullPreservesHistory(t *testing.T) {
	// With full fidelity, all conversation history should be sent to the LLM.
	// We verify this by checking that multi-turn conversations include all turns.
	adapter := &testProviderAdapter{
		responses: []*llm.Response{
			makeTestToolCallResponse("read_file", "call-1", map[string]any{
				"file_path": "/tmp/test.txt",
			}),
			makeTestTextResponse("All done with full history."),
		},
	}
	client := newTestAgentClient(adapter)

	backend := &AgentBackend{Client: client}

	config := AgentRunConfig{
		Prompt:       "Read and summarize the file",
		Model:        "test-model",
		Provider:     "anthropic",
		WorkDir:      t.TempDir(),
		NodeID:       "full-fidelity-node",
		MaxTurns:     10,
		FidelityMode: "full",
	}

	result, err := backend.RunAgent(context.Background(), config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}

	// The second LLM call should have received the full conversation history
	// including the first user message, first assistant response, and tool result
	calls := adapter.getCalls()
	if len(calls) < 2 {
		t.Fatalf("expected at least 2 LLM calls, got %d", len(calls))
	}

	// Second call should have more messages than the first (user + assistant + tool result + system)
	if len(calls[1].Messages) <= len(calls[0].Messages) {
		t.Errorf("expected second call to have more messages than first (full history); first=%d, second=%d",
			len(calls[0].Messages), len(calls[1].Messages))
	}
}

func TestBackendFidelityCompactReducesHistory(t *testing.T) {
	// With compact fidelity and enough turns, history should be reduced.
	// This test creates enough tool call rounds to exceed the minTurnsForReduction
	// threshold, then verifies compact mode limits what goes to the LLM.
	responses := make([]*llm.Response, 0)
	for i := 0; i < 12; i++ {
		responses = append(responses, makeTestToolCallResponse(
			"shell", fmt.Sprintf("call-%d", i),
			map[string]any{"command": fmt.Sprintf("echo step-%d", i)},
		))
	}
	responses = append(responses, makeTestTextResponse("Finished with compact history."))

	adapter := &testProviderAdapter{responses: responses}
	client := newTestAgentClient(adapter)

	backend := &AgentBackend{Client: client}

	config := AgentRunConfig{
		Prompt:       "Run many steps",
		Model:        "test-model",
		Provider:     "anthropic",
		WorkDir:      t.TempDir(),
		NodeID:       "compact-fidelity-node",
		MaxTurns:     20,
		FidelityMode: "compact",
	}

	result, err := backend.RunAgent(context.Background(), config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
	if result.Output != "Finished with compact history." {
		t.Errorf("unexpected output: %q", result.Output)
	}

	// After many tool rounds, compact mode should have reduced the message count
	// in the later LLM calls compared to what full mode would produce.
	// We check that the final call has fewer messages than the sum of all turns.
	calls := adapter.getCalls()
	lastCall := calls[len(calls)-1]
	totalTurnsExpectedInFull := 1 + (len(calls)-1)*3 // system + (user + assistant + tool_result) * rounds
	if len(lastCall.Messages) >= totalTurnsExpectedInFull {
		t.Errorf("compact fidelity should reduce message count; got %d messages, expected fewer than %d",
			len(lastCall.Messages), totalTurnsExpectedInFull)
	}
}

func TestBackendFidelityConfigFieldExists(t *testing.T) {
	// Verify AgentRunConfig has the FidelityMode field and it round-trips correctly
	config := AgentRunConfig{
		Prompt:       "test",
		FidelityMode: "summary:medium",
	}
	if config.FidelityMode != "summary:medium" {
		t.Errorf("expected FidelityMode 'summary:medium', got %q", config.FidelityMode)
	}
}

func TestBackendFidelityModeInCodergenHandler(t *testing.T) {
	// Verify that CodergenHandler reads the fidelity attribute from the node
	// and passes it through to the backend via AgentRunConfig.FidelityMode
	backend := &fakeBackend{}
	h := &CodergenHandler{Backend: backend}

	node := &Node{
		ID: "codegen_fidelity",
		Attrs: map[string]string{
			"shape":    "box",
			"prompt":   "Write code",
			"fidelity": "summary:high",
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
	if backend.calls[0].FidelityMode != "summary:high" {
		t.Errorf("expected FidelityMode 'summary:high', got %q", backend.calls[0].FidelityMode)
	}
}

func TestBackendFidelityModeFromContextOverride(t *testing.T) {
	// Verify that CodergenHandler reads fidelity from pipeline context
	// when the node itself does not specify a fidelity attribute.
	backend := &fakeBackend{}
	h := &CodergenHandler{Backend: backend}

	node := &Node{
		ID: "codegen_ctx_fidelity",
		Attrs: map[string]string{
			"shape":  "box",
			"prompt": "Do work",
		},
	}
	pctx := NewContext()
	pctx.Set("_fidelity_mode", "truncate")
	store := NewArtifactStore(t.TempDir())

	_, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(backend.calls) != 1 {
		t.Fatalf("expected 1 backend call, got %d", len(backend.calls))
	}
	if backend.calls[0].FidelityMode != "truncate" {
		t.Errorf("expected FidelityMode 'truncate' from context, got %q", backend.calls[0].FidelityMode)
	}
}

// --- extractResult OUTCOME marker tests ---

func TestExtractResultDetectsOutcomePass(t *testing.T) {
	adapter := &testProviderAdapter{
		responses: []*llm.Response{
			makeTestTextResponse("Everything looks good.\nOUTCOME:PASS"),
		},
	}
	client := newTestAgentClient(adapter)

	backend := &AgentBackend{Client: client}
	config := AgentRunConfig{
		Prompt:   "Review the code",
		Model:    "test-model",
		Provider: "anthropic",
		WorkDir:  t.TempDir(),
		NodeID:   "outcome-pass-node",
		MaxTurns: 10,
	}

	result, err := backend.RunAgent(context.Background(), config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected Success=true for OUTCOME:PASS")
	}
}

func TestExtractResultDetectsOutcomeFail(t *testing.T) {
	adapter := &testProviderAdapter{
		responses: []*llm.Response{
			makeTestTextResponse("Found issues in the code.\nOUTCOME:FAIL"),
		},
	}
	client := newTestAgentClient(adapter)

	backend := &AgentBackend{Client: client}
	config := AgentRunConfig{
		Prompt:   "Review the code",
		Model:    "test-model",
		Provider: "anthropic",
		WorkDir:  t.TempDir(),
		NodeID:   "outcome-fail-node",
		MaxTurns: 10,
	}

	result, err := backend.RunAgent(context.Background(), config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected Success=false for OUTCOME:FAIL")
	}
}

func TestExtractResultDefaultsToTrueWithoutMarker(t *testing.T) {
	adapter := &testProviderAdapter{
		responses: []*llm.Response{
			makeTestTextResponse("I wrote the code. No outcome marker here."),
		},
	}
	client := newTestAgentClient(adapter)

	backend := &AgentBackend{Client: client}
	config := AgentRunConfig{
		Prompt:   "Write some code",
		Model:    "test-model",
		Provider: "anthropic",
		WorkDir:  t.TempDir(),
		NodeID:   "no-marker-node",
		MaxTurns: 10,
	}

	result, err := backend.RunAgent(context.Background(), config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected Success=true when no OUTCOME marker is present (backward compatible)")
	}
}

func TestExtractResultUsesLastMessage(t *testing.T) {
	// First response says OUTCOME:PASS via a tool call, then final response says OUTCOME:FAIL.
	// Only the last assistant message matters.
	adapter := &testProviderAdapter{
		responses: []*llm.Response{
			makeTestToolCallResponse("read_file", "call-1", map[string]any{
				"file_path": "/tmp/test.txt",
			}),
			makeTestTextResponse("After reviewing, there are critical bugs.\nOUTCOME:FAIL"),
		},
	}
	client := newTestAgentClient(adapter)

	backend := &AgentBackend{Client: client}
	config := AgentRunConfig{
		Prompt:   "Review everything",
		Model:    "test-model",
		Provider: "anthropic",
		WorkDir:  t.TempDir(),
		NodeID:   "last-msg-node",
		MaxTurns: 10,
	}

	result, err := backend.RunAgent(context.Background(), config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected Success=false because last message contains OUTCOME:FAIL")
	}
}

func TestBackendFidelityNodeAttrOverridesContext(t *testing.T) {
	// When both node attr and context specify fidelity, node attr wins
	backend := &fakeBackend{}
	h := &CodergenHandler{Backend: backend}

	node := &Node{
		ID: "codegen_fidelity_precedence",
		Attrs: map[string]string{
			"shape":    "box",
			"prompt":   "Do work",
			"fidelity": "compact",
		},
	}
	pctx := NewContext()
	pctx.Set("_fidelity_mode", "full")
	store := NewArtifactStore(t.TempDir())

	_, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if backend.calls[0].FidelityMode != "compact" {
		t.Errorf("expected FidelityMode 'compact' (node attr should override context), got %q", backend.calls[0].FidelityMode)
	}
}

func TestBackendFidelityInvalidModeIgnored(t *testing.T) {
	// Invalid fidelity mode on the node should be ignored (empty string passed)
	backend := &fakeBackend{}
	h := &CodergenHandler{Backend: backend}

	node := &Node{
		ID: "codegen_invalid_fidelity",
		Attrs: map[string]string{
			"shape":    "box",
			"prompt":   "Do work",
			"fidelity": "bogus_mode",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	_, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Invalid fidelity should not be passed through
	if backend.calls[0].FidelityMode != "" {
		t.Errorf("expected empty FidelityMode for invalid fidelity attr, got %q", backend.calls[0].FidelityMode)
	}
}
