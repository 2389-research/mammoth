// ABOUTME: Tests for the Anthropic provider adapter using httptest servers.
// ABOUTME: Validates request translation, response parsing, streaming, error handling, and header management.

package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAnthropicAdapterName verifies the adapter returns "anthropic" as its name.
func TestAnthropicAdapterName(t *testing.T) {
	adapter := NewAnthropicAdapter("test-key")
	if adapter.Name() != "anthropic" {
		t.Errorf("Name() = %q, want %q", adapter.Name(), "anthropic")
	}
}

// TestAnthropicRequestTranslation verifies that a unified Request is correctly
// translated into the Anthropic Messages API request body format.
func TestAnthropicRequestTranslation(t *testing.T) {
	var receivedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading body: %v", err)
		}
		if err := json.Unmarshal(body, &receivedBody); err != nil {
			t.Errorf("unmarshal body: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{
			"id": "msg_test",
			"type": "message",
			"role": "assistant",
			"model": "claude-sonnet-4-20250514",
			"content": [{"type": "text", "text": "Hello!"}],
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 10, "output_tokens": 5}
		}`
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	adapter := NewAnthropicAdapter("test-key", WithAnthropicBaseURL(server.URL))
	temp := 0.7
	topP := 0.9

	req := Request{
		Model: "claude-sonnet-4-20250514",
		Messages: []Message{
			UserMessage("Hello"),
		},
		Temperature:   &temp,
		TopP:          &topP,
		MaxTokens:     IntPtr(1000),
		StopSequences: []string{"STOP"},
	}

	_, err := adapter.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedBody["model"] != "claude-sonnet-4-20250514" {
		t.Errorf("model = %v, want %q", receivedBody["model"], "claude-sonnet-4-20250514")
	}
	if receivedBody["max_tokens"] != float64(1000) {
		t.Errorf("max_tokens = %v, want 1000", receivedBody["max_tokens"])
	}
	if receivedBody["temperature"] != 0.7 {
		t.Errorf("temperature = %v, want 0.7", receivedBody["temperature"])
	}
	if receivedBody["top_p"] != 0.9 {
		t.Errorf("top_p = %v, want 0.9", receivedBody["top_p"])
	}

	stopSeqs, ok := receivedBody["stop_sequences"].([]any)
	if !ok || len(stopSeqs) != 1 || stopSeqs[0] != "STOP" {
		t.Errorf("stop_sequences = %v, want [STOP]", receivedBody["stop_sequences"])
	}

	msgs, ok := receivedBody["messages"].([]any)
	if !ok || len(msgs) != 1 {
		t.Fatalf("messages = %v, want 1 message", receivedBody["messages"])
	}
	msg := msgs[0].(map[string]any)
	if msg["role"] != "user" {
		t.Errorf("message role = %v, want user", msg["role"])
	}
	content := msg["content"].([]any)
	block := content[0].(map[string]any)
	if block["type"] != "text" || block["text"] != "Hello" {
		t.Errorf("content block = %v, want text:Hello", block)
	}
}

// TestAnthropicSystemMessageExtraction verifies that system and developer messages
// are extracted to the top-level "system" parameter.
func TestAnthropicSystemMessageExtraction(t *testing.T) {
	var receivedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedBody)

		w.Header().Set("Content-Type", "application/json")
		resp := `{
			"id": "msg_test",
			"type": "message",
			"role": "assistant",
			"model": "claude-sonnet-4-20250514",
			"content": [{"type": "text", "text": "Hi"}],
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 10, "output_tokens": 5}
		}`
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	adapter := NewAnthropicAdapter("test-key", WithAnthropicBaseURL(server.URL))

	req := Request{
		Model: "claude-sonnet-4-20250514",
		Messages: []Message{
			SystemMessage("You are a helpful assistant."),
			DeveloperMessage("Be concise."),
			UserMessage("Hello"),
		},
	}

	_, err := adapter.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// System messages should be extracted to "system" field
	systemText, ok := receivedBody["system"].(string)
	if !ok {
		t.Fatalf("system field not found or not a string, got %T: %v", receivedBody["system"], receivedBody["system"])
	}
	if !strings.Contains(systemText, "You are a helpful assistant.") {
		t.Errorf("system text should contain system message, got %q", systemText)
	}
	if !strings.Contains(systemText, "Be concise.") {
		t.Errorf("system text should contain developer message, got %q", systemText)
	}

	// Only user message should remain in messages array
	msgs := receivedBody["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message after system extraction, got %d", len(msgs))
	}
	msg := msgs[0].(map[string]any)
	if msg["role"] != "user" {
		t.Errorf("remaining message role = %v, want user", msg["role"])
	}
}

// TestAnthropicStrictAlternation verifies that consecutive same-role messages
// are merged to satisfy Anthropic's strict alternation requirement.
func TestAnthropicStrictAlternation(t *testing.T) {
	var receivedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedBody)

		w.Header().Set("Content-Type", "application/json")
		resp := `{
			"id": "msg_test",
			"type": "message",
			"role": "assistant",
			"model": "claude-sonnet-4-20250514",
			"content": [{"type": "text", "text": "Response"}],
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 10, "output_tokens": 5}
		}`
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	adapter := NewAnthropicAdapter("test-key", WithAnthropicBaseURL(server.URL))

	req := Request{
		Model: "claude-sonnet-4-20250514",
		Messages: []Message{
			UserMessage("Hello"),
			UserMessage("How are you?"),
			AssistantMessage("I'm fine"),
			AssistantMessage("Thanks"),
			UserMessage("Great"),
		},
	}

	_, err := adapter.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs := receivedBody["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("expected 3 merged messages, got %d", len(msgs))
	}

	// First merged user message should have 2 content blocks
	firstMsg := msgs[0].(map[string]any)
	if firstMsg["role"] != "user" {
		t.Errorf("first message role = %v, want user", firstMsg["role"])
	}
	firstContent := firstMsg["content"].([]any)
	if len(firstContent) != 2 {
		t.Errorf("first message has %d content blocks, want 2", len(firstContent))
	}

	// Merged assistant message should have 2 content blocks
	secondMsg := msgs[1].(map[string]any)
	if secondMsg["role"] != "assistant" {
		t.Errorf("second message role = %v, want assistant", secondMsg["role"])
	}
	secondContent := secondMsg["content"].([]any)
	if len(secondContent) != 2 {
		t.Errorf("second message has %d content blocks, want 2", len(secondContent))
	}
}

// TestAnthropicToolDefinitionTranslation verifies that tool definitions are
// translated to Anthropic's input_schema format.
func TestAnthropicToolDefinitionTranslation(t *testing.T) {
	var receivedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedBody)

		w.Header().Set("Content-Type", "application/json")
		resp := `{
			"id": "msg_test",
			"type": "message",
			"role": "assistant",
			"model": "claude-sonnet-4-20250514",
			"content": [{"type": "text", "text": "Hi"}],
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 10, "output_tokens": 5}
		}`
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	adapter := NewAnthropicAdapter("test-key", WithAnthropicBaseURL(server.URL))

	schema := json.RawMessage(`{"type":"object","properties":{"location":{"type":"string"}},"required":["location"]}`)
	req := Request{
		Model:    "claude-sonnet-4-20250514",
		Messages: []Message{UserMessage("What's the weather?")},
		Tools: []ToolDefinition{
			{
				Name:        "get_weather",
				Description: "Get the weather for a location",
				Parameters:  schema,
			},
		},
	}

	_, err := adapter.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tools, ok := receivedBody["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %v", receivedBody["tools"])
	}

	tool := tools[0].(map[string]any)
	if tool["name"] != "get_weather" {
		t.Errorf("tool name = %v, want get_weather", tool["name"])
	}
	if tool["description"] != "Get the weather for a location" {
		t.Errorf("tool description = %v, want correct description", tool["description"])
	}

	// Anthropic uses "input_schema" not "parameters"
	inputSchema, ok := tool["input_schema"].(map[string]any)
	if !ok {
		t.Fatalf("input_schema not found or not a map, got %T: %v", tool["input_schema"], tool["input_schema"])
	}
	if inputSchema["type"] != "object" {
		t.Errorf("input_schema.type = %v, want object", inputSchema["type"])
	}
}

// TestAnthropicToolChoiceTranslation verifies all four tool choice modes
// are translated correctly for Anthropic.
func TestAnthropicToolChoiceTranslation(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{}}`)
	tools := []ToolDefinition{{Name: "test_tool", Description: "A test tool", Parameters: schema}}

	tests := []struct {
		name     string
		choice   *ToolChoice
		wantKey  string // key to check in body ("tool_choice" or absence)
		wantType string // expected type value
		wantName string // expected tool name (for named mode)
		wantTool bool   // whether tools should be in request
	}{
		{
			name:     "auto",
			choice:   &ToolChoice{Mode: ToolChoiceAuto},
			wantKey:  "tool_choice",
			wantType: "auto",
			wantTool: true,
		},
		{
			name:     "none",
			choice:   &ToolChoice{Mode: ToolChoiceNone},
			wantTool: false,
		},
		{
			name:     "required",
			choice:   &ToolChoice{Mode: ToolChoiceRequired},
			wantKey:  "tool_choice",
			wantType: "any",
			wantTool: true,
		},
		{
			name:     "named",
			choice:   &ToolChoice{Mode: ToolChoiceNamed, ToolName: "test_tool"},
			wantKey:  "tool_choice",
			wantType: "tool",
			wantName: "test_tool",
			wantTool: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var receivedBody map[string]any

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				json.Unmarshal(body, &receivedBody)

				w.Header().Set("Content-Type", "application/json")
				resp := `{
					"id": "msg_test",
					"type": "message",
					"role": "assistant",
					"model": "claude-sonnet-4-20250514",
					"content": [{"type": "text", "text": "Hi"}],
					"stop_reason": "end_turn",
					"usage": {"input_tokens": 10, "output_tokens": 5}
				}`
				_, _ = w.Write([]byte(resp))
			}))
			defer server.Close()

			adapter := NewAnthropicAdapter("test-key", WithAnthropicBaseURL(server.URL))

			req := Request{
				Model:      "claude-sonnet-4-20250514",
				Messages:   []Message{UserMessage("Hi")},
				Tools:      tools,
				ToolChoice: tt.choice,
			}

			_, err := adapter.Complete(context.Background(), req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantKey != "" {
				tc, ok := receivedBody["tool_choice"].(map[string]any)
				if !ok {
					t.Fatalf("tool_choice not found, body keys: %v", receivedBody)
				}
				if tc["type"] != tt.wantType {
					t.Errorf("tool_choice.type = %v, want %q", tc["type"], tt.wantType)
				}
				if tt.wantName != "" && tc["name"] != tt.wantName {
					t.Errorf("tool_choice.name = %v, want %q", tc["name"], tt.wantName)
				}
			}

			if tt.wantTool {
				if _, ok := receivedBody["tools"]; !ok {
					t.Error("expected tools in request body")
				}
			} else {
				if _, ok := receivedBody["tools"]; ok {
					t.Error("expected no tools in request body for none mode")
				}
			}
		})
	}
}

// TestAnthropicToolResultInUserMessage verifies that tool results from the
// unified format (RoleTool messages) are translated into user-role messages
// with tool_result content blocks for Anthropic.
func TestAnthropicToolResultInUserMessage(t *testing.T) {
	var receivedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedBody)

		w.Header().Set("Content-Type", "application/json")
		resp := `{
			"id": "msg_test",
			"type": "message",
			"role": "assistant",
			"model": "claude-sonnet-4-20250514",
			"content": [{"type": "text", "text": "The weather is sunny."}],
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 20, "output_tokens": 10}
		}`
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	adapter := NewAnthropicAdapter("test-key", WithAnthropicBaseURL(server.URL))

	req := Request{
		Model: "claude-sonnet-4-20250514",
		Messages: []Message{
			UserMessage("What's the weather?"),
			{
				Role: RoleAssistant,
				Content: []ContentPart{
					ToolCallPart("call_123", "get_weather", json.RawMessage(`{"location":"NYC"}`)),
				},
			},
			ToolResultMessage("call_123", "Sunny, 72F", false),
		},
	}

	_, err := adapter.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs := receivedBody["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}

	// The tool result message should be translated to a user role message
	toolResultMsg := msgs[2].(map[string]any)
	if toolResultMsg["role"] != "user" {
		t.Errorf("tool result message role = %v, want user", toolResultMsg["role"])
	}

	content := toolResultMsg["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(content))
	}

	block := content[0].(map[string]any)
	if block["type"] != "tool_result" {
		t.Errorf("content type = %v, want tool_result", block["type"])
	}
	if block["tool_use_id"] != "call_123" {
		t.Errorf("tool_use_id = %v, want call_123", block["tool_use_id"])
	}
	if block["content"] != "Sunny, 72F" {
		t.Errorf("content = %v, want 'Sunny, 72F'", block["content"])
	}
}

// TestAnthropicResponseParsing verifies that Anthropic API responses are
// correctly parsed into the unified Response type.
func TestAnthropicResponseParsing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := `{
			"id": "msg_abc123",
			"type": "message",
			"role": "assistant",
			"model": "claude-sonnet-4-20250514",
			"content": [
				{"type": "text", "text": "Here is the answer."},
				{"type": "tool_use", "id": "toolu_456", "name": "calculator", "input": {"expression": "2+2"}}
			],
			"stop_reason": "tool_use",
			"usage": {
				"input_tokens": 100,
				"output_tokens": 50,
				"cache_creation_input_tokens": 200,
				"cache_read_input_tokens": 150
			}
		}`
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	adapter := NewAnthropicAdapter("test-key", WithAnthropicBaseURL(server.URL))

	resp, err := adapter.Complete(context.Background(), Request{
		Model:    "claude-sonnet-4-20250514",
		Messages: []Message{UserMessage("What is 2+2?")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.ID != "msg_abc123" {
		t.Errorf("ID = %q, want %q", resp.ID, "msg_abc123")
	}
	if resp.Model != "claude-sonnet-4-20250514" {
		t.Errorf("Model = %q, want %q", resp.Model, "claude-sonnet-4-20250514")
	}
	if resp.Provider != "anthropic" {
		t.Errorf("Provider = %q, want %q", resp.Provider, "anthropic")
	}

	// Check finish reason mapping: tool_use -> tool_calls
	if resp.FinishReason.Reason != FinishToolCalls {
		t.Errorf("FinishReason.Reason = %q, want %q", resp.FinishReason.Reason, FinishToolCalls)
	}
	if resp.FinishReason.Raw != "tool_use" {
		t.Errorf("FinishReason.Raw = %q, want %q", resp.FinishReason.Raw, "tool_use")
	}

	// Check usage
	if resp.Usage.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want 100", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 50 {
		t.Errorf("OutputTokens = %d, want 50", resp.Usage.OutputTokens)
	}
	if resp.Usage.TotalTokens != 150 {
		t.Errorf("TotalTokens = %d, want 150", resp.Usage.TotalTokens)
	}

	// Check cache tokens
	if resp.Usage.CacheWriteTokens == nil || *resp.Usage.CacheWriteTokens != 200 {
		t.Errorf("CacheWriteTokens = %v, want 200", resp.Usage.CacheWriteTokens)
	}
	if resp.Usage.CacheReadTokens == nil || *resp.Usage.CacheReadTokens != 150 {
		t.Errorf("CacheReadTokens = %v, want 150", resp.Usage.CacheReadTokens)
	}

	// Check message content
	if resp.Message.Role != RoleAssistant {
		t.Errorf("Message.Role = %q, want assistant", resp.Message.Role)
	}
	if len(resp.Message.Content) != 2 {
		t.Fatalf("expected 2 content parts, got %d", len(resp.Message.Content))
	}
	if resp.Message.Content[0].Kind != ContentText {
		t.Errorf("content[0].Kind = %q, want text", resp.Message.Content[0].Kind)
	}
	if resp.Message.Content[0].Text != "Here is the answer." {
		t.Errorf("content[0].Text = %q, want 'Here is the answer.'", resp.Message.Content[0].Text)
	}
	if resp.Message.Content[1].Kind != ContentToolCall {
		t.Errorf("content[1].Kind = %q, want tool_call", resp.Message.Content[1].Kind)
	}
	if resp.Message.Content[1].ToolCall.ID != "toolu_456" {
		t.Errorf("tool call ID = %q, want toolu_456", resp.Message.Content[1].ToolCall.ID)
	}
	if resp.Message.Content[1].ToolCall.Name != "calculator" {
		t.Errorf("tool call name = %q, want calculator", resp.Message.Content[1].ToolCall.Name)
	}
}

// TestAnthropicThinkingBlocks verifies that thinking and redacted_thinking
// blocks are properly preserved in request translation and response parsing.
func TestAnthropicThinkingBlocks(t *testing.T) {
	t.Run("response parsing", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			resp := `{
				"id": "msg_think",
				"type": "message",
				"role": "assistant",
				"model": "claude-sonnet-4-20250514",
				"content": [
					{"type": "thinking", "thinking": "Let me reason about this...", "signature": "sig123"},
					{"type": "redacted_thinking", "data": "cmVkYWN0ZWQ="},
					{"type": "text", "text": "The answer is 4."}
				],
				"stop_reason": "end_turn",
				"usage": {"input_tokens": 10, "output_tokens": 20}
			}`
			_, _ = w.Write([]byte(resp))
		}))
		defer server.Close()

		adapter := NewAnthropicAdapter("test-key", WithAnthropicBaseURL(server.URL))
		resp, err := adapter.Complete(context.Background(), Request{
			Model:    "claude-sonnet-4-20250514",
			Messages: []Message{UserMessage("What is 2+2?")},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(resp.Message.Content) != 3 {
			t.Fatalf("expected 3 content parts, got %d", len(resp.Message.Content))
		}

		// Thinking block
		if resp.Message.Content[0].Kind != ContentThinking {
			t.Errorf("content[0].Kind = %q, want thinking", resp.Message.Content[0].Kind)
		}
		if resp.Message.Content[0].Thinking.Text != "Let me reason about this..." {
			t.Errorf("thinking text = %q", resp.Message.Content[0].Thinking.Text)
		}
		if resp.Message.Content[0].Thinking.Signature != "sig123" {
			t.Errorf("thinking signature = %q, want sig123", resp.Message.Content[0].Thinking.Signature)
		}

		// Redacted thinking block
		if resp.Message.Content[1].Kind != ContentRedactedThinking {
			t.Errorf("content[1].Kind = %q, want redacted_thinking", resp.Message.Content[1].Kind)
		}
		if resp.Message.Content[1].Thinking.Redacted != true {
			t.Error("expected redacted=true")
		}

		// Text block
		if resp.Message.Content[2].Kind != ContentText {
			t.Errorf("content[2].Kind = %q, want text", resp.Message.Content[2].Kind)
		}
	})

	t.Run("round trip in request", func(t *testing.T) {
		var receivedBody map[string]any

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			json.Unmarshal(body, &receivedBody)

			w.Header().Set("Content-Type", "application/json")
			resp := `{
				"id": "msg_rt",
				"type": "message",
				"role": "assistant",
				"model": "claude-sonnet-4-20250514",
				"content": [{"type": "text", "text": "OK"}],
				"stop_reason": "end_turn",
				"usage": {"input_tokens": 10, "output_tokens": 5}
			}`
			_, _ = w.Write([]byte(resp))
		}))
		defer server.Close()

		adapter := NewAnthropicAdapter("test-key", WithAnthropicBaseURL(server.URL))

		req := Request{
			Model: "claude-sonnet-4-20250514",
			Messages: []Message{
				UserMessage("What is 2+2?"),
				{
					Role: RoleAssistant,
					Content: []ContentPart{
						ThinkingPart("Reasoning here", "sig456"),
						RedactedThinkingPart("", "sig789"),
						TextPart("The answer is 4."),
					},
				},
				UserMessage("Thanks"),
			},
		}

		_, err := adapter.Complete(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		msgs := receivedBody["messages"].([]any)
		if len(msgs) != 3 {
			t.Fatalf("expected 3 messages, got %d", len(msgs))
		}

		assistantMsg := msgs[1].(map[string]any)
		content := assistantMsg["content"].([]any)
		if len(content) != 3 {
			t.Fatalf("expected 3 content blocks in assistant message, got %d", len(content))
		}

		thinkingBlock := content[0].(map[string]any)
		if thinkingBlock["type"] != "thinking" {
			t.Errorf("block[0].type = %v, want thinking", thinkingBlock["type"])
		}
		if thinkingBlock["thinking"] != "Reasoning here" {
			t.Errorf("block[0].thinking = %v", thinkingBlock["thinking"])
		}
		if thinkingBlock["signature"] != "sig456" {
			t.Errorf("block[0].signature = %v, want sig456", thinkingBlock["signature"])
		}

		redactedBlock := content[1].(map[string]any)
		if redactedBlock["type"] != "redacted_thinking" {
			t.Errorf("block[1].type = %v, want redacted_thinking", redactedBlock["type"])
		}
	})
}

// TestAnthropicMaxTokensDefault verifies that max_tokens defaults to 4096
// when not specified in the request.
func TestAnthropicMaxTokensDefault(t *testing.T) {
	var receivedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedBody)

		w.Header().Set("Content-Type", "application/json")
		resp := `{
			"id": "msg_test",
			"type": "message",
			"role": "assistant",
			"model": "claude-sonnet-4-20250514",
			"content": [{"type": "text", "text": "Hi"}],
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 10, "output_tokens": 5}
		}`
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	adapter := NewAnthropicAdapter("test-key", WithAnthropicBaseURL(server.URL))

	// No MaxTokens set
	req := Request{
		Model:    "claude-sonnet-4-20250514",
		Messages: []Message{UserMessage("Hi")},
	}

	_, err := adapter.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedBody["max_tokens"] != float64(4096) {
		t.Errorf("max_tokens = %v, want 4096 (default)", receivedBody["max_tokens"])
	}
}

// TestAnthropicErrorHandling verifies that API error responses are correctly
// parsed and mapped to the appropriate error types.
func TestAnthropicErrorHandling(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		errorType  string
	}{
		{
			name:       "authentication error",
			statusCode: 401,
			body:       `{"type":"error","error":{"type":"authentication_error","message":"Invalid API key"}}`,
			errorType:  "authentication",
		},
		{
			name:       "rate limit error",
			statusCode: 429,
			body:       `{"type":"error","error":{"type":"rate_limit_error","message":"Rate limit exceeded"}}`,
			errorType:  "rate_limit",
		},
		{
			name:       "server error",
			statusCode: 500,
			body:       `{"type":"error","error":{"type":"api_error","message":"Internal server error"}}`,
			errorType:  "server",
		},
		{
			name:       "invalid request",
			statusCode: 400,
			body:       `{"type":"error","error":{"type":"invalid_request_error","message":"Invalid model"}}`,
			errorType:  "invalid_request",
		},
		{
			name:       "not found",
			statusCode: 404,
			body:       `{"type":"error","error":{"type":"not_found_error","message":"Model not found"}}`,
			errorType:  "not_found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			adapter := NewAnthropicAdapter("test-key", WithAnthropicBaseURL(server.URL))

			_, err := adapter.Complete(context.Background(), Request{
				Model:    "claude-sonnet-4-20250514",
				Messages: []Message{UserMessage("Hi")},
			})

			if err == nil {
				t.Fatal("expected error, got nil")
			}

			switch tt.errorType {
			case "authentication":
				var authErr *AuthenticationError
				if !errors.As(err, &authErr) {
					t.Errorf("expected AuthenticationError, got %T: %v", err, err)
				}
			case "rate_limit":
				var rlErr *RateLimitError
				if !errors.As(err, &rlErr) {
					t.Errorf("expected RateLimitError, got %T: %v", err, err)
				}
			case "server":
				var srvErr *ServerError
				if !errors.As(err, &srvErr) {
					t.Errorf("expected ServerError, got %T: %v", err, err)
				}
			case "invalid_request":
				var irErr *InvalidRequestError
				if !errors.As(err, &irErr) {
					t.Errorf("expected InvalidRequestError, got %T: %v", err, err)
				}
			case "not_found":
				var nfErr *NotFoundError
				if !errors.As(err, &nfErr) {
					t.Errorf("expected NotFoundError, got %T: %v", err, err)
				}
			}
		})
	}
}

// TestAnthropicStreaming verifies that SSE streaming responses are correctly
// parsed into StreamEvent channel events.
func TestAnthropicStreaming(t *testing.T) {
	sseData := strings.Join([]string{
		"event: message_start",
		`data: {"type":"message_start","message":{"id":"msg_stream","type":"message","role":"assistant","model":"claude-sonnet-4-20250514","content":[],"stop_reason":null,"usage":{"input_tokens":25,"output_tokens":0}}}`,
		"",
		"event: content_block_start",
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		"",
		"event: content_block_delta",
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
		"",
		"event: content_block_delta",
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}`,
		"",
		"event: content_block_stop",
		`data: {"type":"content_block_stop","index":0}`,
		"",
		"event: message_delta",
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":10}}`,
		"",
		"event: message_stop",
		`data: {"type":"message_stop"}`,
		"",
	}, "\n")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify stream: true was sent
		body, _ := io.ReadAll(r.Body)
		var reqBody map[string]any
		json.Unmarshal(body, &reqBody)
		if reqBody["stream"] != true {
			t.Errorf("expected stream: true in request body")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseData))
	}))
	defer server.Close()

	adapter := NewAnthropicAdapter("test-key", WithAnthropicBaseURL(server.URL))

	ch, err := adapter.Stream(context.Background(), Request{
		Model:    "claude-sonnet-4-20250514",
		Messages: []Message{UserMessage("Hi")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var events []StreamEvent
	for evt := range ch {
		events = append(events, evt)
	}

	// We should get: StreamStart, StreamTextStart, StreamTextDelta("Hello"),
	// StreamTextDelta(" world"), StreamTextEnd, StreamFinish
	if len(events) < 4 {
		t.Fatalf("expected at least 4 events, got %d", len(events))
	}

	// Check for text deltas
	var textContent string
	for _, evt := range events {
		if evt.Type == StreamTextDelta {
			textContent += evt.Delta
		}
	}
	if textContent != "Hello world" {
		t.Errorf("concatenated text = %q, want %q", textContent, "Hello world")
	}

	// Check for finish event
	var hasFinish bool
	for _, evt := range events {
		if evt.Type == StreamFinish {
			hasFinish = true
			if evt.FinishReason == nil || evt.FinishReason.Reason != FinishStop {
				t.Errorf("expected finish reason 'stop', got %v", evt.FinishReason)
			}
		}
	}
	if !hasFinish {
		t.Error("expected StreamFinish event")
	}
}

// TestAnthropicStreamingToolUse verifies that tool use blocks are streamed correctly.
func TestAnthropicStreamingToolUse(t *testing.T) {
	sseData := strings.Join([]string{
		"event: message_start",
		`data: {"type":"message_start","message":{"id":"msg_tool","type":"message","role":"assistant","model":"claude-sonnet-4-20250514","content":[],"stop_reason":null,"usage":{"input_tokens":25,"output_tokens":0}}}`,
		"",
		"event: content_block_start",
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_abc","name":"get_weather"}}`,
		"",
		"event: content_block_delta",
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"loc"}}`,
		"",
		"event: content_block_delta",
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"ation\":\"NYC\"}"}}`,
		"",
		"event: content_block_stop",
		`data: {"type":"content_block_stop","index":0}`,
		"",
		"event: message_delta",
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":15}}`,
		"",
		"event: message_stop",
		`data: {"type":"message_stop"}`,
		"",
	}, "\n")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseData))
	}))
	defer server.Close()

	adapter := NewAnthropicAdapter("test-key", WithAnthropicBaseURL(server.URL))

	ch, err := adapter.Stream(context.Background(), Request{
		Model:    "claude-sonnet-4-20250514",
		Messages: []Message{UserMessage("Weather?")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var events []StreamEvent
	for evt := range ch {
		events = append(events, evt)
	}

	// Check for tool call start
	var hasToolStart bool
	for _, evt := range events {
		if evt.Type == StreamToolStart {
			hasToolStart = true
			if evt.ToolCall == nil {
				t.Error("expected ToolCall in StreamToolStart event")
			} else {
				if evt.ToolCall.ID != "toolu_abc" {
					t.Errorf("tool call ID = %q, want toolu_abc", evt.ToolCall.ID)
				}
				if evt.ToolCall.Name != "get_weather" {
					t.Errorf("tool call name = %q, want get_weather", evt.ToolCall.Name)
				}
			}
		}
	}
	if !hasToolStart {
		t.Error("expected StreamToolStart event")
	}

	// Check for tool deltas
	var jsonContent string
	for _, evt := range events {
		if evt.Type == StreamToolDelta {
			jsonContent += evt.Delta
		}
	}
	if jsonContent != `{"location":"NYC"}` {
		t.Errorf("concatenated tool JSON = %q, want {\"location\":\"NYC\"}", jsonContent)
	}
}

// TestAnthropicHeaders verifies that the correct headers are sent with requests,
// specifically x-api-key and anthropic-version.
func TestAnthropicHeaders(t *testing.T) {
	var receivedHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header

		w.Header().Set("Content-Type", "application/json")
		resp := `{
			"id": "msg_test",
			"type": "message",
			"role": "assistant",
			"model": "claude-sonnet-4-20250514",
			"content": [{"type": "text", "text": "Hi"}],
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 10, "output_tokens": 5}
		}`
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	adapter := NewAnthropicAdapter("sk-ant-test-key-123",
		WithAnthropicBaseURL(server.URL),
		WithAnthropicVersion("2023-06-01"),
	)

	_, err := adapter.Complete(context.Background(), Request{
		Model:    "claude-sonnet-4-20250514",
		Messages: []Message{UserMessage("Hi")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check x-api-key header
	apiKey := receivedHeaders.Get("X-Api-Key")
	if apiKey != "sk-ant-test-key-123" {
		t.Errorf("x-api-key = %q, want %q", apiKey, "sk-ant-test-key-123")
	}

	// Check anthropic-version header
	version := receivedHeaders.Get("Anthropic-Version")
	if version != "2023-06-01" {
		t.Errorf("anthropic-version = %q, want %q", version, "2023-06-01")
	}

	// Check Content-Type header
	ct := receivedHeaders.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	// Should NOT have Bearer auth header
	auth := receivedHeaders.Get("Authorization")
	if auth != "" {
		t.Errorf("Authorization header should be empty for Anthropic, got %q", auth)
	}
}

// TestAnthropicStopReasonMapping verifies all stop reason mappings from Anthropic to unified format.
func TestAnthropicStopReasonMapping(t *testing.T) {
	tests := []struct {
		anthropicReason string
		wantReason      string
	}{
		{"end_turn", FinishStop},
		{"max_tokens", FinishLength},
		{"tool_use", FinishToolCalls},
		{"unknown_reason", FinishOther},
	}

	for _, tt := range tests {
		t.Run(tt.anthropicReason, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				resp := fmt.Sprintf(`{
					"id": "msg_test",
					"type": "message",
					"role": "assistant",
					"model": "claude-sonnet-4-20250514",
					"content": [{"type": "text", "text": "Hi"}],
					"stop_reason": %q,
					"usage": {"input_tokens": 10, "output_tokens": 5}
				}`, tt.anthropicReason)
				_, _ = w.Write([]byte(resp))
			}))
			defer server.Close()

			adapter := NewAnthropicAdapter("test-key", WithAnthropicBaseURL(server.URL))

			resp, err := adapter.Complete(context.Background(), Request{
				Model:    "claude-sonnet-4-20250514",
				Messages: []Message{UserMessage("Hi")},
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if resp.FinishReason.Reason != tt.wantReason {
				t.Errorf("FinishReason.Reason = %q, want %q", resp.FinishReason.Reason, tt.wantReason)
			}
			if resp.FinishReason.Raw != tt.anthropicReason {
				t.Errorf("FinishReason.Raw = %q, want %q", resp.FinishReason.Raw, tt.anthropicReason)
			}
		})
	}
}

// TestAnthropicProviderOptions verifies that provider_options["anthropic"]
// are merged into the request body.
func TestAnthropicProviderOptions(t *testing.T) {
	var receivedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedBody)

		w.Header().Set("Content-Type", "application/json")
		resp := `{
			"id": "msg_test",
			"type": "message",
			"role": "assistant",
			"model": "claude-sonnet-4-20250514",
			"content": [{"type": "text", "text": "Hi"}],
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 10, "output_tokens": 5}
		}`
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	adapter := NewAnthropicAdapter("test-key", WithAnthropicBaseURL(server.URL))

	req := Request{
		Model:    "claude-sonnet-4-20250514",
		Messages: []Message{UserMessage("Hi")},
		ProviderOptions: map[string]any{
			"anthropic": map[string]any{
				"metadata": map[string]any{
					"user_id": "user123",
				},
			},
		},
	}

	_, err := adapter.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	metadata, ok := receivedBody["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata not found in request body, body = %v", receivedBody)
	}
	if metadata["user_id"] != "user123" {
		t.Errorf("metadata.user_id = %v, want user123", metadata["user_id"])
	}
}

// TestAnthropicBetaHeader verifies that beta headers are added when specified
// in provider options.
func TestAnthropicBetaHeader(t *testing.T) {
	var receivedHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header

		w.Header().Set("Content-Type", "application/json")
		resp := `{
			"id": "msg_test",
			"type": "message",
			"role": "assistant",
			"model": "claude-sonnet-4-20250514",
			"content": [{"type": "text", "text": "Hi"}],
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 10, "output_tokens": 5}
		}`
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	adapter := NewAnthropicAdapter("test-key", WithAnthropicBaseURL(server.URL))

	req := Request{
		Model:    "claude-sonnet-4-20250514",
		Messages: []Message{UserMessage("Hi")},
		ProviderOptions: map[string]any{
			"anthropic": map[string]any{
				"beta": "prompt-caching-2024-07-31",
			},
		},
	}

	_, err := adapter.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	beta := receivedHeaders.Get("Anthropic-Beta")
	if beta != "prompt-caching-2024-07-31" {
		t.Errorf("anthropic-beta = %q, want %q", beta, "prompt-caching-2024-07-31")
	}
}

// TestAnthropicImageTranslation verifies that images (both URL and data)
// are correctly translated to Anthropic's format.
func TestAnthropicImageTranslation(t *testing.T) {
	var receivedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedBody)

		w.Header().Set("Content-Type", "application/json")
		resp := `{
			"id": "msg_test",
			"type": "message",
			"role": "assistant",
			"model": "claude-sonnet-4-20250514",
			"content": [{"type": "text", "text": "I see an image."}],
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 100, "output_tokens": 10}
		}`
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	adapter := NewAnthropicAdapter("test-key", WithAnthropicBaseURL(server.URL))

	imgData := []byte("fake-png-data")
	req := Request{
		Model: "claude-sonnet-4-20250514",
		Messages: []Message{
			UserMessageWithParts(
				TextPart("Look at these images:"),
				ImageURLPart("https://example.com/cat.jpg"),
				ImageDataPart(imgData, "image/png"),
			),
		},
	}

	_, err := adapter.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs := receivedBody["messages"].([]any)
	msg := msgs[0].(map[string]any)
	content := msg["content"].([]any)
	if len(content) != 3 {
		t.Fatalf("expected 3 content blocks, got %d", len(content))
	}

	// Text block
	if content[0].(map[string]any)["type"] != "text" {
		t.Errorf("block[0].type = %v, want text", content[0].(map[string]any)["type"])
	}

	// URL image block
	urlBlock := content[1].(map[string]any)
	if urlBlock["type"] != "image" {
		t.Errorf("block[1].type = %v, want image", urlBlock["type"])
	}
	source := urlBlock["source"].(map[string]any)
	if source["type"] != "url" {
		t.Errorf("source.type = %v, want url", source["type"])
	}
	if source["url"] != "https://example.com/cat.jpg" {
		t.Errorf("source.url = %v", source["url"])
	}

	// Base64 image block
	dataBlock := content[2].(map[string]any)
	if dataBlock["type"] != "image" {
		t.Errorf("block[2].type = %v, want image", dataBlock["type"])
	}
	dataSource := dataBlock["source"].(map[string]any)
	if dataSource["type"] != "base64" {
		t.Errorf("source.type = %v, want base64", dataSource["type"])
	}
	if dataSource["media_type"] != "image/png" {
		t.Errorf("source.media_type = %v, want image/png", dataSource["media_type"])
	}
	expectedB64 := base64.StdEncoding.EncodeToString(imgData)
	if dataSource["data"] != expectedB64 {
		t.Errorf("source.data = %v, want %v", dataSource["data"], expectedB64)
	}
}

// TestAnthropicStreamingThinking verifies that thinking blocks are correctly
// streamed in SSE events.
func TestAnthropicStreamingThinking(t *testing.T) {
	sseData := strings.Join([]string{
		"event: message_start",
		`data: {"type":"message_start","message":{"id":"msg_think","type":"message","role":"assistant","model":"claude-sonnet-4-20250514","content":[],"stop_reason":null,"usage":{"input_tokens":25,"output_tokens":0}}}`,
		"",
		"event: content_block_start",
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
		"",
		"event: content_block_delta",
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Let me think"}}`,
		"",
		"event: content_block_stop",
		`data: {"type":"content_block_stop","index":0}`,
		"",
		"event: content_block_start",
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
		"",
		"event: content_block_delta",
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Answer"}}`,
		"",
		"event: content_block_stop",
		`data: {"type":"content_block_stop","index":1}`,
		"",
		"event: message_delta",
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":20}}`,
		"",
		"event: message_stop",
		`data: {"type":"message_stop"}`,
		"",
	}, "\n")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseData))
	}))
	defer server.Close()

	adapter := NewAnthropicAdapter("test-key", WithAnthropicBaseURL(server.URL))

	ch, err := adapter.Stream(context.Background(), Request{
		Model:    "claude-sonnet-4-20250514",
		Messages: []Message{UserMessage("Think about this")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var events []StreamEvent
	for evt := range ch {
		events = append(events, evt)
	}

	// Check for reasoning events
	var hasReasonStart, hasReasonDelta bool
	var reasonContent string
	for _, evt := range events {
		if evt.Type == StreamReasonStart {
			hasReasonStart = true
		}
		if evt.Type == StreamReasonDelta {
			hasReasonDelta = true
			reasonContent += evt.ReasoningDelta
		}
	}
	if !hasReasonStart {
		t.Error("expected StreamReasonStart event")
	}
	if !hasReasonDelta {
		t.Error("expected StreamReasonDelta event")
	}
	if reasonContent != "Let me think" {
		t.Errorf("reasoning content = %q, want %q", reasonContent, "Let me think")
	}
}

// TestAnthropicClose verifies the adapter can be closed without error.
func TestAnthropicClose(t *testing.T) {
	adapter := NewAnthropicAdapter("test-key")
	err := adapter.Close()
	if err != nil {
		t.Errorf("unexpected error from Close: %v", err)
	}
}

// TestAnthropicCustomVersion verifies that a custom API version can be set.
func TestAnthropicCustomVersion(t *testing.T) {
	var receivedHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header

		w.Header().Set("Content-Type", "application/json")
		resp := `{
			"id": "msg_test",
			"type": "message",
			"role": "assistant",
			"model": "claude-sonnet-4-20250514",
			"content": [{"type": "text", "text": "Hi"}],
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 10, "output_tokens": 5}
		}`
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	adapter := NewAnthropicAdapter("test-key",
		WithAnthropicBaseURL(server.URL),
		WithAnthropicVersion("2024-01-01"),
	)

	_, err := adapter.Complete(context.Background(), Request{
		Model:    "claude-sonnet-4-20250514",
		Messages: []Message{UserMessage("Hi")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	version := receivedHeaders.Get("Anthropic-Version")
	if version != "2024-01-01" {
		t.Errorf("anthropic-version = %q, want %q", version, "2024-01-01")
	}
}

// TestAnthropicToolResultWithError verifies that tool results marked as errors
// include the is_error field.
func TestAnthropicToolResultWithError(t *testing.T) {
	var receivedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedBody)

		w.Header().Set("Content-Type", "application/json")
		resp := `{
			"id": "msg_test",
			"type": "message",
			"role": "assistant",
			"model": "claude-sonnet-4-20250514",
			"content": [{"type": "text", "text": "Sorry"}],
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 10, "output_tokens": 5}
		}`
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	adapter := NewAnthropicAdapter("test-key", WithAnthropicBaseURL(server.URL))

	req := Request{
		Model: "claude-sonnet-4-20250514",
		Messages: []Message{
			UserMessage("Do something"),
			{
				Role: RoleAssistant,
				Content: []ContentPart{
					ToolCallPart("call_err", "failing_tool", json.RawMessage(`{}`)),
				},
			},
			ToolResultMessage("call_err", "Tool execution failed: timeout", true),
		},
	}

	_, err := adapter.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs := receivedBody["messages"].([]any)
	toolResultMsg := msgs[2].(map[string]any)
	content := toolResultMsg["content"].([]any)
	block := content[0].(map[string]any)
	if block["is_error"] != true {
		t.Errorf("is_error = %v, want true", block["is_error"])
	}
}

// TestAnthropicStreamingError verifies that streaming errors from the server
// are properly handled.
func TestAnthropicStreamingError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"rate_limit_error","message":"Too many requests"}}`))
	}))
	defer server.Close()

	adapter := NewAnthropicAdapter("test-key", WithAnthropicBaseURL(server.URL))

	_, err := adapter.Stream(context.Background(), Request{
		Model:    "claude-sonnet-4-20250514",
		Messages: []Message{UserMessage("Hi")},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var rlErr *RateLimitError
	if !errors.As(err, &rlErr) {
		t.Errorf("expected RateLimitError, got %T: %v", err, err)
	}
}
