// ABOUTME: Tests for core data model types in the unified LLM client SDK.
// ABOUTME: Validates message construction, content parts, usage arithmetic, and type behavior.

package llm

import (
	"encoding/json"
	"testing"
)

func TestMessageConstructors(t *testing.T) {
	tests := []struct {
		name     string
		msg      Message
		wantRole Role
		wantText string
	}{
		{"SystemMessage", SystemMessage("be helpful"), RoleSystem, "be helpful"},
		{"UserMessage", UserMessage("hello"), RoleUser, "hello"},
		{"AssistantMessage", AssistantMessage("hi there"), RoleAssistant, "hi there"},
		{"DeveloperMessage", DeveloperMessage("priority instructions"), RoleDeveloper, "priority instructions"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.msg.Role != tt.wantRole {
				t.Errorf("got role %q, want %q", tt.msg.Role, tt.wantRole)
			}
			if tt.msg.TextContent() != tt.wantText {
				t.Errorf("got text %q, want %q", tt.msg.TextContent(), tt.wantText)
			}
		})
	}
}

func TestToolResultMessage(t *testing.T) {
	msg := ToolResultMessage("call_123", "72F and sunny", false)
	if msg.Role != RoleTool {
		t.Errorf("got role %q, want %q", msg.Role, RoleTool)
	}
	if msg.ToolCallID != "call_123" {
		t.Errorf("got tool_call_id %q, want %q", msg.ToolCallID, "call_123")
	}
	if len(msg.Content) != 1 {
		t.Fatalf("got %d content parts, want 1", len(msg.Content))
	}
	part := msg.Content[0]
	if part.Kind != ContentToolResult {
		t.Errorf("got kind %q, want %q", part.Kind, ContentToolResult)
	}
	if part.ToolResult.Content != "72F and sunny" {
		t.Errorf("got content %q, want %q", part.ToolResult.Content, "72F and sunny")
	}
	if part.ToolResult.IsError {
		t.Error("expected is_error to be false")
	}
}

func TestUserMessageWithParts(t *testing.T) {
	msg := UserMessageWithParts(
		TextPart("What do you see?"),
		ImageURLPart("https://example.com/photo.jpg"),
	)
	if msg.Role != RoleUser {
		t.Errorf("got role %q, want %q", msg.Role, RoleUser)
	}
	if len(msg.Content) != 2 {
		t.Fatalf("got %d content parts, want 2", len(msg.Content))
	}
	if msg.Content[0].Kind != ContentText {
		t.Errorf("part 0: got kind %q, want %q", msg.Content[0].Kind, ContentText)
	}
	if msg.Content[1].Kind != ContentImage {
		t.Errorf("part 1: got kind %q, want %q", msg.Content[1].Kind, ContentImage)
	}
	if msg.Content[1].Image.URL != "https://example.com/photo.jpg" {
		t.Errorf("part 1: got url %q, want %q", msg.Content[1].Image.URL, "https://example.com/photo.jpg")
	}
}

func TestMessageTextContent(t *testing.T) {
	msg := Message{
		Role: RoleAssistant,
		Content: []ContentPart{
			ThinkingPart("let me think...", "sig_abc"),
			TextPart("The answer is "),
			TextPart("42."),
		},
	}
	got := msg.TextContent()
	want := "The answer is 42."
	if got != want {
		t.Errorf("TextContent() = %q, want %q", got, want)
	}
}

func TestMessageReasoningContent(t *testing.T) {
	msg := Message{
		Role: RoleAssistant,
		Content: []ContentPart{
			ThinkingPart("step 1: ", "sig_1"),
			TextPart("result"),
			ThinkingPart("step 2: ", "sig_2"),
		},
	}
	got := msg.ReasoningContent()
	want := "step 1: step 2: "
	if got != want {
		t.Errorf("ReasoningContent() = %q, want %q", got, want)
	}
}

func TestMessageToolCalls(t *testing.T) {
	args := json.RawMessage(`{"location":"SF"}`)
	msg := Message{
		Role: RoleAssistant,
		Content: []ContentPart{
			TextPart("Let me check the weather."),
			ToolCallPart("call_1", "get_weather", args),
			ToolCallPart("call_2", "get_time", json.RawMessage(`{}`)),
		},
	}
	calls := msg.ToolCalls()
	if len(calls) != 2 {
		t.Fatalf("got %d tool calls, want 2", len(calls))
	}
	if calls[0].Name != "get_weather" {
		t.Errorf("call 0: got name %q, want %q", calls[0].Name, "get_weather")
	}
	if calls[1].Name != "get_time" {
		t.Errorf("call 1: got name %q, want %q", calls[1].Name, "get_time")
	}
}

func TestToolCallDataArgumentsMap(t *testing.T) {
	tc := &ToolCallData{
		ID:        "call_1",
		Name:      "get_weather",
		Arguments: json.RawMessage(`{"location":"San Francisco","unit":"celsius"}`),
	}
	m, err := tc.ArgumentsMap()
	if err != nil {
		t.Fatalf("ArgumentsMap() error: %v", err)
	}
	if m["location"] != "San Francisco" {
		t.Errorf("location = %v, want San Francisco", m["location"])
	}
	if m["unit"] != "celsius" {
		t.Errorf("unit = %v, want celsius", m["unit"])
	}
}

func TestToolCallDataArgumentsMapInvalid(t *testing.T) {
	tc := &ToolCallData{
		Arguments: json.RawMessage(`not json`),
	}
	_, err := tc.ArgumentsMap()
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestUsageAdd(t *testing.T) {
	a := Usage{
		InputTokens:     100,
		OutputTokens:    50,
		TotalTokens:     150,
		ReasoningTokens: IntPtr(20),
		CacheReadTokens: IntPtr(30),
	}
	b := Usage{
		InputTokens:      200,
		OutputTokens:     80,
		TotalTokens:      280,
		ReasoningTokens:  IntPtr(40),
		CacheWriteTokens: IntPtr(10),
	}
	result := a.Add(b)

	if result.InputTokens != 300 {
		t.Errorf("InputTokens = %d, want 300", result.InputTokens)
	}
	if result.OutputTokens != 130 {
		t.Errorf("OutputTokens = %d, want 130", result.OutputTokens)
	}
	if result.TotalTokens != 430 {
		t.Errorf("TotalTokens = %d, want 430", result.TotalTokens)
	}
	if result.ReasoningTokens == nil || *result.ReasoningTokens != 60 {
		t.Errorf("ReasoningTokens = %v, want 60", result.ReasoningTokens)
	}
	if result.CacheReadTokens == nil || *result.CacheReadTokens != 30 {
		t.Errorf("CacheReadTokens = %v, want 30", result.CacheReadTokens)
	}
	if result.CacheWriteTokens == nil || *result.CacheWriteTokens != 10 {
		t.Errorf("CacheWriteTokens = %v, want 10", result.CacheWriteTokens)
	}
}

func TestUsageAddBothNil(t *testing.T) {
	a := Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}
	b := Usage{InputTokens: 20, OutputTokens: 10, TotalTokens: 30}
	result := a.Add(b)

	if result.ReasoningTokens != nil {
		t.Errorf("ReasoningTokens should be nil when both are nil, got %v", result.ReasoningTokens)
	}
	if result.CacheReadTokens != nil {
		t.Errorf("CacheReadTokens should be nil when both are nil, got %v", result.CacheReadTokens)
	}
}

func TestContentPartConstructors(t *testing.T) {
	t.Run("TextPart", func(t *testing.T) {
		p := TextPart("hello")
		if p.Kind != ContentText || p.Text != "hello" {
			t.Errorf("unexpected: kind=%q text=%q", p.Kind, p.Text)
		}
	})

	t.Run("ImageURLPart", func(t *testing.T) {
		p := ImageURLPart("https://example.com/img.png")
		if p.Kind != ContentImage || p.Image == nil || p.Image.URL != "https://example.com/img.png" {
			t.Error("unexpected image URL part")
		}
	})

	t.Run("ImageDataPart", func(t *testing.T) {
		p := ImageDataPart([]byte{0x89, 0x50, 0x4E, 0x47}, "image/png")
		if p.Kind != ContentImage || p.Image == nil || p.Image.MediaType != "image/png" {
			t.Error("unexpected image data part")
		}
		if len(p.Image.Data) != 4 {
			t.Errorf("image data len = %d, want 4", len(p.Image.Data))
		}
	})

	t.Run("ThinkingPart", func(t *testing.T) {
		p := ThinkingPart("reasoning text", "sig_abc")
		if p.Kind != ContentThinking || p.Thinking == nil {
			t.Error("unexpected thinking part")
		}
		if p.Thinking.Text != "reasoning text" || p.Thinking.Signature != "sig_abc" {
			t.Error("thinking data mismatch")
		}
		if p.Thinking.Redacted {
			t.Error("thinking should not be redacted")
		}
	})

	t.Run("RedactedThinkingPart", func(t *testing.T) {
		p := RedactedThinkingPart("opaque", "sig_xyz")
		if p.Kind != ContentRedactedThinking || p.Thinking == nil {
			t.Error("unexpected redacted thinking part")
		}
		if !p.Thinking.Redacted {
			t.Error("should be redacted")
		}
	})
}

func TestFinishReasonConstants(t *testing.T) {
	fr := FinishReason{Reason: FinishStop, Raw: "end_turn"}
	if fr.Reason != "stop" {
		t.Errorf("got %q, want %q", fr.Reason, "stop")
	}
	if fr.Raw != "end_turn" {
		t.Errorf("got raw %q, want %q", fr.Raw, "end_turn")
	}
}

func TestToolIsActive(t *testing.T) {
	passive := Tool{ToolDefinition: ToolDefinition{Name: "read"}}
	if passive.IsActive() {
		t.Error("passive tool should not be active")
	}

	active := Tool{
		ToolDefinition: ToolDefinition{Name: "read"},
		Execute:        func(args json.RawMessage) (string, error) { return "", nil },
	}
	if !active.IsActive() {
		t.Error("active tool should be active")
	}
}

func TestToolChoiceConstants(t *testing.T) {
	tc := ToolChoice{Mode: ToolChoiceNamed, ToolName: "get_weather"}
	if tc.Mode != "named" {
		t.Errorf("got mode %q, want %q", tc.Mode, "named")
	}
	if tc.ToolName != "get_weather" {
		t.Errorf("got tool_name %q, want %q", tc.ToolName, "get_weather")
	}
}

func TestResponseAccessors(t *testing.T) {
	args := json.RawMessage(`{}`)
	resp := Response{
		ID:       "resp_1",
		Model:    "claude-opus-4-6",
		Provider: "anthropic",
		Message: Message{
			Role: RoleAssistant,
			Content: []ContentPart{
				ThinkingPart("hmm...", "sig_1"),
				TextPart("The answer is 42."),
				ToolCallPart("call_1", "calculate", args),
			},
		},
		FinishReason: FinishReason{Reason: FinishToolCalls},
		Usage:        Usage{InputTokens: 10, OutputTokens: 20, TotalTokens: 30},
	}

	if resp.TextContent() != "The answer is 42." {
		t.Errorf("TextContent() = %q", resp.TextContent())
	}
	if resp.Reasoning() != "hmm..." {
		t.Errorf("Reasoning() = %q", resp.Reasoning())
	}
	calls := resp.ToolCalls()
	if len(calls) != 1 || calls[0].Name != "calculate" {
		t.Error("unexpected tool calls")
	}
}

func TestDefaultAdapterTimeout(t *testing.T) {
	at := DefaultAdapterTimeout()
	if at.Connect.Seconds() != 10 {
		t.Errorf("Connect = %v, want 10s", at.Connect)
	}
	if at.Request.Seconds() != 120 {
		t.Errorf("Request = %v, want 120s", at.Request)
	}
	if at.StreamRead.Seconds() != 30 {
		t.Errorf("StreamRead = %v, want 30s", at.StreamRead)
	}
}

func TestTypesJSONRoundTrip(t *testing.T) {
	msg := UserMessage("hello world")
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded.Role != RoleUser {
		t.Errorf("decoded role = %q, want %q", decoded.Role, RoleUser)
	}
	if decoded.TextContent() != "hello world" {
		t.Errorf("decoded text = %q, want %q", decoded.TextContent(), "hello world")
	}
}
