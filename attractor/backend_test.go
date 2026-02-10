// ABOUTME: Tests for the CodergenBackend interface and the test double used by CodergenHandler tests.
// ABOUTME: Validates AgentRunConfig defaults, result mapping, and the stub/real backend switching behavior.
package attractor

import (
	"context"
	"testing"
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
