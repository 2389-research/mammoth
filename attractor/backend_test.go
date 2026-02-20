// ABOUTME: Tests for the CodergenBackend interface and the test double used by CodergenHandler tests.
// ABOUTME: Validates AgentRunConfig defaults, result mapping, and the stub/real backend switching behavior.
package attractor

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/2389-research/mammoth/llm"
)

// fakeBackend is a test double that implements CodergenBackend with configurable behavior.
type fakeBackend struct {
	runAgentFn func(ctx context.Context, config AgentRunConfig) (*AgentRunResult, error)
	calls      []AgentRunConfig
}

func (f *fakeBackend) RunAgent(ctx context.Context, config AgentRunConfig) (*AgentRunResult, error) {
	f.calls = append(f.calls, config)
	if f.runAgentFn != nil {
		return f.runAgentFn(ctx, config)
	}
	return &AgentRunResult{
		Output:     "fake agent output for: " + config.Prompt,
		ToolCalls:  3,
		TokensUsed: 500,
		Success:    true,
	}, nil
}

func TestCodergenBackendInterfaceCompliance(t *testing.T) {
	// Verify that fakeBackend satisfies the CodergenBackend interface.
	var _ CodergenBackend = (*fakeBackend)(nil)
}

func TestAgentRunConfigDefaults(t *testing.T) {
	config := AgentRunConfig{
		Prompt: "write tests",
	}

	if config.MaxTurns != 0 {
		t.Errorf("expected zero-value MaxTurns (0), got %d", config.MaxTurns)
	}
	if config.Model != "" {
		t.Errorf("expected empty model, got %q", config.Model)
	}
	if config.Provider != "" {
		t.Errorf("expected empty provider, got %q", config.Provider)
	}
	if config.WorkDir != "" {
		t.Errorf("expected empty work dir, got %q", config.WorkDir)
	}
}

func TestAgentRunResultFields(t *testing.T) {
	result := &AgentRunResult{
		Output:     "generated code",
		ToolCalls:  5,
		TokensUsed: 1000,
		Success:    true,
	}

	if result.Output != "generated code" {
		t.Errorf("expected output 'generated code', got %q", result.Output)
	}
	if result.ToolCalls != 5 {
		t.Errorf("expected 5 tool calls, got %d", result.ToolCalls)
	}
	if result.TokensUsed != 1000 {
		t.Errorf("expected 1000 tokens, got %d", result.TokensUsed)
	}
	if !result.Success {
		t.Error("expected success to be true")
	}
}

func TestFakeBackendRecordsCalls(t *testing.T) {
	backend := &fakeBackend{}

	config := AgentRunConfig{
		Prompt:   "test prompt",
		Model:    "test-model",
		Provider: "test-provider",
		NodeID:   "node-1",
	}

	result, err := backend.RunAgent(context.Background(), config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(backend.calls) != 1 {
		t.Fatalf("expected 1 call recorded, got %d", len(backend.calls))
	}
	if backend.calls[0].Prompt != "test prompt" {
		t.Errorf("expected recorded prompt 'test prompt', got %q", backend.calls[0].Prompt)
	}
	if !result.Success {
		t.Error("expected success from fake backend")
	}
}

func TestFakeBackendCustomFunction(t *testing.T) {
	backend := &fakeBackend{
		runAgentFn: func(ctx context.Context, config AgentRunConfig) (*AgentRunResult, error) {
			return &AgentRunResult{
				Output:  "custom output",
				Success: false,
			}, nil
		},
	}

	result, err := backend.RunAgent(context.Background(), AgentRunConfig{Prompt: "custom"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "custom output" {
		t.Errorf("expected 'custom output', got %q", result.Output)
	}
	if result.Success {
		t.Error("expected success=false from custom function")
	}
}

func TestToolCallEntryFields(t *testing.T) {
	entry := ToolCallEntry{
		ToolName: "file_write",
		CallID:   "call_123",
		Duration: 250 * time.Millisecond,
		Output:   "wrote 42 bytes",
	}

	if entry.ToolName != "file_write" {
		t.Errorf("expected tool_name 'file_write', got %q", entry.ToolName)
	}
	if entry.CallID != "call_123" {
		t.Errorf("expected call_id 'call_123', got %q", entry.CallID)
	}
	if entry.Duration != 250*time.Millisecond {
		t.Errorf("expected duration 250ms, got %v", entry.Duration)
	}
	if entry.Output != "wrote 42 bytes" {
		t.Errorf("expected output 'wrote 42 bytes', got %q", entry.Output)
	}
}

func TestToolCallEntryJSONSerialization(t *testing.T) {
	entry := ToolCallEntry{
		ToolName: "bash",
		CallID:   "tc_42",
		Duration: 1 * time.Second,
		Output:   "success",
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("failed to marshal ToolCallEntry: %v", err)
	}

	jsonStr := string(data)
	if !strings.Contains(jsonStr, `"tool_name":"bash"`) {
		t.Errorf("expected JSON to contain tool_name, got: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"call_id":"tc_42"`) {
		t.Errorf("expected JSON to contain call_id, got: %s", jsonStr)
	}
}

func TestAgentRunConfigEventHandlerField(t *testing.T) {
	var received []EngineEvent
	handler := func(evt EngineEvent) {
		received = append(received, evt)
	}

	config := AgentRunConfig{
		Prompt:       "test",
		EventHandler: handler,
	}

	if config.EventHandler == nil {
		t.Fatal("expected EventHandler to be set")
	}

	config.EventHandler(EngineEvent{Type: EventAgentLLMTurn, NodeID: "test"})
	if len(received) != 1 {
		t.Fatalf("expected 1 event received, got %d", len(received))
	}
	if received[0].Type != EventAgentLLMTurn {
		t.Errorf("expected EventAgentLLMTurn, got %q", received[0].Type)
	}
}

func TestAgentRunResultEnrichedFields(t *testing.T) {
	result := &AgentRunResult{
		Output:     "done",
		ToolCalls:  5,
		TokensUsed: 1000,
		Success:    true,
		ToolCallLog: []ToolCallEntry{
			{ToolName: "file_read", CallID: "c1", Duration: 100 * time.Millisecond, Output: "contents"},
			{ToolName: "bash", CallID: "c2", Duration: 200 * time.Millisecond, Output: "ok"},
		},
		TurnCount: 3,
	}

	if len(result.ToolCallLog) != 2 {
		t.Errorf("expected 2 tool call log entries, got %d", len(result.ToolCallLog))
	}
	if result.TurnCount != 3 {
		t.Errorf("expected turn count 3, got %d", result.TurnCount)
	}
	if result.ToolCallLog[0].ToolName != "file_read" {
		t.Errorf("expected first tool call to be 'file_read', got %q", result.ToolCallLog[0].ToolName)
	}
}

func TestTokenUsageFields(t *testing.T) {
	usage := TokenUsage{
		InputTokens:      1000,
		OutputTokens:     500,
		TotalTokens:      1500,
		ReasoningTokens:  200,
		CacheReadTokens:  300,
		CacheWriteTokens: 100,
	}

	if usage.InputTokens != 1000 {
		t.Errorf("expected InputTokens=1000, got %d", usage.InputTokens)
	}
	if usage.OutputTokens != 500 {
		t.Errorf("expected OutputTokens=500, got %d", usage.OutputTokens)
	}
	if usage.TotalTokens != 1500 {
		t.Errorf("expected TotalTokens=1500, got %d", usage.TotalTokens)
	}
	if usage.ReasoningTokens != 200 {
		t.Errorf("expected ReasoningTokens=200, got %d", usage.ReasoningTokens)
	}
	if usage.CacheReadTokens != 300 {
		t.Errorf("expected CacheReadTokens=300, got %d", usage.CacheReadTokens)
	}
	if usage.CacheWriteTokens != 100 {
		t.Errorf("expected CacheWriteTokens=100, got %d", usage.CacheWriteTokens)
	}
}

func TestTokenUsageJSONSerialization(t *testing.T) {
	usage := TokenUsage{
		InputTokens:      800,
		OutputTokens:     400,
		TotalTokens:      1200,
		ReasoningTokens:  50,
		CacheReadTokens:  150,
		CacheWriteTokens: 75,
	}

	data, err := json.Marshal(usage)
	if err != nil {
		t.Fatalf("failed to marshal TokenUsage: %v", err)
	}

	jsonStr := string(data)
	if !strings.Contains(jsonStr, `"input_tokens":800`) {
		t.Errorf("expected JSON to contain input_tokens, got: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"output_tokens":400`) {
		t.Errorf("expected JSON to contain output_tokens, got: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"total_tokens":1200`) {
		t.Errorf("expected JSON to contain total_tokens, got: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"reasoning_tokens":50`) {
		t.Errorf("expected JSON to contain reasoning_tokens, got: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"cache_read_tokens":150`) {
		t.Errorf("expected JSON to contain cache_read_tokens, got: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"cache_write_tokens":75`) {
		t.Errorf("expected JSON to contain cache_write_tokens, got: %s", jsonStr)
	}

	// Round-trip
	var decoded TokenUsage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal TokenUsage: %v", err)
	}
	if decoded != usage {
		t.Errorf("round-trip mismatch: got %+v, want %+v", decoded, usage)
	}
}

func TestTokenUsageAddCombinesValues(t *testing.T) {
	a := TokenUsage{
		InputTokens:      100,
		OutputTokens:     50,
		TotalTokens:      150,
		ReasoningTokens:  10,
		CacheReadTokens:  20,
		CacheWriteTokens: 5,
	}
	b := TokenUsage{
		InputTokens:      200,
		OutputTokens:     100,
		TotalTokens:      300,
		ReasoningTokens:  30,
		CacheReadTokens:  40,
		CacheWriteTokens: 15,
	}

	combined := a.Add(b)
	if combined.InputTokens != 300 {
		t.Errorf("expected InputTokens=300, got %d", combined.InputTokens)
	}
	if combined.OutputTokens != 150 {
		t.Errorf("expected OutputTokens=150, got %d", combined.OutputTokens)
	}
	if combined.TotalTokens != 450 {
		t.Errorf("expected TotalTokens=450, got %d", combined.TotalTokens)
	}
	if combined.ReasoningTokens != 40 {
		t.Errorf("expected ReasoningTokens=40, got %d", combined.ReasoningTokens)
	}
	if combined.CacheReadTokens != 60 {
		t.Errorf("expected CacheReadTokens=60, got %d", combined.CacheReadTokens)
	}
	if combined.CacheWriteTokens != 20 {
		t.Errorf("expected CacheWriteTokens=20, got %d", combined.CacheWriteTokens)
	}
}

func TestAgentRunResultTokenUsageField(t *testing.T) {
	result := &AgentRunResult{
		Output:     "done",
		ToolCalls:  5,
		TokensUsed: 1500,
		Success:    true,
		Usage: TokenUsage{
			InputTokens:  1000,
			OutputTokens: 500,
			TotalTokens:  1500,
		},
	}

	if result.Usage.InputTokens != 1000 {
		t.Errorf("expected Usage.InputTokens=1000, got %d", result.Usage.InputTokens)
	}
	if result.Usage.OutputTokens != 500 {
		t.Errorf("expected Usage.OutputTokens=500, got %d", result.Usage.OutputTokens)
	}
	if result.Usage.TotalTokens != result.TokensUsed {
		t.Errorf("expected Usage.TotalTokens to match TokensUsed, got %d vs %d", result.Usage.TotalTokens, result.TokensUsed)
	}
}

func TestAgentRunConfigBaseURLField(t *testing.T) {
	config := AgentRunConfig{
		Prompt:  "test",
		BaseURL: "https://custom.api.example.com",
	}

	if config.BaseURL != "https://custom.api.example.com" {
		t.Errorf("expected BaseURL to be set, got %q", config.BaseURL)
	}
}

func TestAgentRunConfigBaseURLDefaultEmpty(t *testing.T) {
	config := AgentRunConfig{
		Prompt: "test",
	}

	if config.BaseURL != "" {
		t.Errorf("expected empty BaseURL by default, got %q", config.BaseURL)
	}
}

func TestCreateProviderAdapterWithBaseURL(t *testing.T) {
	tests := []struct {
		name      string
		provider  string
		expectMux bool // OpenAI uses MuxAdapter with compat client; others use legacy
	}{
		{"anthropic", "anthropic", false},
		{"openai", "openai", true},
		{"gemini", "gemini", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := createProviderAdapter(context.Background(), tt.provider, "test-key", "https://custom.example.com")
			if adapter == nil {
				t.Error("expected non-nil adapter")
			}
			_, isMux := adapter.(*llm.MuxAdapter)
			if tt.expectMux && !isMux {
				t.Errorf("expected MuxAdapter (compat client) for %s with base URL, got %T", tt.provider, adapter)
			}
			if !tt.expectMux && isMux {
				t.Errorf("expected legacy adapter for %s with base URL, got *llm.MuxAdapter", tt.provider)
			}
		})
	}
}

func TestCreateProviderAdapterWithEmptyBaseURL(t *testing.T) {
	// Empty base URL should use mux adapter
	adapter := createProviderAdapter(context.Background(), "anthropic", "test-key", "")
	if adapter == nil {
		t.Error("expected non-nil adapter with empty base URL")
	}
	// Without a base URL, should use the MuxAdapter
	if _, ok := adapter.(*llm.MuxAdapter); !ok {
		t.Errorf("expected *llm.MuxAdapter with empty base URL, got %T", adapter)
	}
}

func TestFakeBackendRespectsContextCancellation(t *testing.T) {
	backend := &fakeBackend{
		runAgentFn: func(ctx context.Context, config AgentRunConfig) (*AgentRunResult, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return &AgentRunResult{Success: true}, nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := backend.RunAgent(ctx, AgentRunConfig{Prompt: "cancelled"})
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}
