// ABOUTME: Tests for OpenAI Chat Completions API client conversion functions.
// ABOUTME: Covers convertCompatRequest and convertCompatResponse for correct mapping.

package llm

import (
	"testing"

	muxllm "github.com/2389-research/mux/llm"
	"github.com/openai/openai-go"
)

// TestConvertCompatRequest_StreamOptionsOnlyWhenStreaming verifies that
// StreamOptions.IncludeUsage is set only when stream=true.
func TestConvertCompatRequest_StreamOptionsOnlyWhenStreaming(t *testing.T) {
	req := &muxllm.Request{
		Model:    "gpt-4o",
		Messages: []muxllm.Message{muxllm.NewUserMessage("hello")},
	}

	// Streaming: should have StreamOptions
	streamParams := convertCompatRequest(req, true)
	if !streamParams.StreamOptions.IncludeUsage.Valid() {
		t.Fatal("StreamOptions.IncludeUsage is not set for stream=true")
	}
	if !streamParams.StreamOptions.IncludeUsage.Value {
		t.Errorf("StreamOptions.IncludeUsage.Value = false, want true (stream=true)")
	}

	// Non-streaming: should NOT have StreamOptions
	syncParams := convertCompatRequest(req, false)
	if syncParams.StreamOptions.IncludeUsage.Valid() {
		t.Errorf("StreamOptions.IncludeUsage should not be set for stream=false")
	}
}

// TestConvertCompatResponse_UsagePopulated verifies that convertCompatResponse
// correctly reads non-zero usage data from the OpenAI response.
func TestConvertCompatResponse_UsagePopulated(t *testing.T) {
	comp := openai.ChatCompletion{
		ID:    "chatcmpl-test123",
		Model: "gpt-4o",
		Usage: openai.CompletionUsage{
			PromptTokens:     100,
			CompletionTokens: 50,
		},
		Choices: []openai.ChatCompletionChoice{
			{
				FinishReason: "stop",
				Message: openai.ChatCompletionMessage{
					Content: "",
				},
			},
		},
	}

	resp := convertCompatResponse(&comp)

	if resp.Usage.InputTokens != 100 {
		t.Errorf("Usage.InputTokens = %d, want 100", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 50 {
		t.Errorf("Usage.OutputTokens = %d, want 50", resp.Usage.OutputTokens)
	}
}

// TestConvertCompatResponse_ZeroUsage verifies that zero usage fields are
// handled gracefully without panicking.
func TestConvertCompatResponse_ZeroUsage(t *testing.T) {
	comp := openai.ChatCompletion{
		ID:    "chatcmpl-zero",
		Model: "gpt-4o",
		Usage: openai.CompletionUsage{
			PromptTokens:     0,
			CompletionTokens: 0,
		},
		Choices: []openai.ChatCompletionChoice{},
	}

	resp := convertCompatResponse(&comp)

	if resp.Usage.InputTokens != 0 {
		t.Errorf("Usage.InputTokens = %d, want 0", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 0 {
		t.Errorf("Usage.OutputTokens = %d, want 0", resp.Usage.OutputTokens)
	}
}

// TestConvertCompatRequest_ModelSet verifies that the model field from the
// muxllm.Request is correctly propagated into the OpenAI params.
func TestConvertCompatRequest_ModelSet(t *testing.T) {
	tests := []struct {
		name      string
		model     string
		wantModel string
	}{
		{
			name:      "standard model",
			model:     "gpt-4o",
			wantModel: "gpt-4o",
		},
		{
			name:      "cerebras model",
			model:     "llama-4-scout-17b-16e-instruct",
			wantModel: "llama-4-scout-17b-16e-instruct",
		},
		{
			name:      "openrouter model",
			model:     "openai/gpt-4o",
			wantModel: "openai/gpt-4o",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &muxllm.Request{
				Model:    tt.model,
				Messages: []muxllm.Message{muxllm.NewUserMessage("hello")},
			}

			params := convertCompatRequest(req, false)

			if params.Model != tt.wantModel {
				t.Errorf("Model = %q, want %q", params.Model, tt.wantModel)
			}
		})
	}
}
