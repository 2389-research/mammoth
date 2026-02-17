// ABOUTME: Tests for the MuxAdapter that bridges mux/llm.Client to mammoth's ProviderAdapter interface.
// ABOUTME: Covers request/response conversion, streaming, tool calls, and type mapping.

package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	muxllm "github.com/2389-research/mux/llm"
)

// stubMuxClient implements muxllm.Client for testing without mocks.
// It records the request and returns a preconfigured response.
type stubMuxClient struct {
	lastRequest  *muxllm.Request
	response     *muxllm.Response
	err          error
	streamEvents []muxllm.StreamEvent
	streamErr    error
}

func (s *stubMuxClient) CreateMessage(ctx context.Context, req *muxllm.Request) (*muxllm.Response, error) {
	s.lastRequest = req
	if s.err != nil {
		return nil, s.err
	}
	return s.response, nil
}

func (s *stubMuxClient) CreateMessageStream(ctx context.Context, req *muxllm.Request) (<-chan muxllm.StreamEvent, error) {
	s.lastRequest = req
	if s.streamErr != nil {
		return nil, s.streamErr
	}
	ch := make(chan muxllm.StreamEvent, len(s.streamEvents))
	for _, evt := range s.streamEvents {
		ch <- evt
	}
	close(ch)
	return ch, nil
}

func TestMuxAdapter_ImplementsProviderAdapter(t *testing.T) {
	stub := &stubMuxClient{}
	adapter := NewMuxAdapter("mux", stub)

	// Compile-time check that MuxAdapter satisfies ProviderAdapter.
	var _ ProviderAdapter = adapter
}

func TestMuxAdapter_Name(t *testing.T) {
	stub := &stubMuxClient{}
	adapter := NewMuxAdapter("anthropic-mux", stub)
	if got := adapter.Name(); got != "anthropic-mux" {
		t.Errorf("Name() = %q, want %q", got, "anthropic-mux")
	}
}

func TestMuxAdapter_Close(t *testing.T) {
	stub := &stubMuxClient{}
	adapter := NewMuxAdapter("mux", stub)
	if err := adapter.Close(); err != nil {
		t.Errorf("Close() returned unexpected error: %v", err)
	}
}

func TestConvertRequest_BasicTextMessages(t *testing.T) {
	req := Request{
		Model: "claude-sonnet-4-20250514",
		Messages: []Message{
			UserMessage("Hello"),
			AssistantMessage("Hi there"),
			UserMessage("How are you?"),
		},
		MaxTokens:   intPtr(1024),
		Temperature: Float64Ptr(0.7),
	}

	muxReq := convertRequest(req)

	if muxReq.Model != "claude-sonnet-4-20250514" {
		t.Errorf("Model = %q, want %q", muxReq.Model, "claude-sonnet-4-20250514")
	}
	if muxReq.MaxTokens != 1024 {
		t.Errorf("MaxTokens = %d, want %d", muxReq.MaxTokens, 1024)
	}
	if muxReq.Temperature == nil || *muxReq.Temperature != 0.7 {
		t.Errorf("Temperature = %v, want 0.7", muxReq.Temperature)
	}
	if muxReq.System != "" {
		t.Errorf("System = %q, want empty", muxReq.System)
	}
	if len(muxReq.Messages) != 3 {
		t.Fatalf("len(Messages) = %d, want 3", len(muxReq.Messages))
	}
	if muxReq.Messages[0].Role != muxllm.RoleUser {
		t.Errorf("Messages[0].Role = %q, want %q", muxReq.Messages[0].Role, muxllm.RoleUser)
	}
	if muxReq.Messages[0].Content != "Hello" {
		t.Errorf("Messages[0].Content = %q, want %q", muxReq.Messages[0].Content, "Hello")
	}
}

func TestConvertRequest_SystemMessageExtraction(t *testing.T) {
	req := Request{
		Model: "test-model",
		Messages: []Message{
			SystemMessage("You are a helpful assistant"),
			DeveloperMessage("Additional system instructions"),
			UserMessage("Hello"),
		},
	}

	muxReq := convertRequest(req)

	if muxReq.System != "You are a helpful assistant\nAdditional system instructions" {
		t.Errorf("System = %q, want concatenated system text", muxReq.System)
	}
	if len(muxReq.Messages) != 1 {
		t.Fatalf("len(Messages) = %d, want 1 (system messages should be extracted)", len(muxReq.Messages))
	}
	if muxReq.Messages[0].Content != "Hello" {
		t.Errorf("Messages[0].Content = %q, want %q", muxReq.Messages[0].Content, "Hello")
	}
}

func TestConvertRequest_ToolResultMessages(t *testing.T) {
	req := Request{
		Model: "test-model",
		Messages: []Message{
			ToolResultMessage("call_123", "file contents here", false),
		},
	}

	muxReq := convertRequest(req)

	if len(muxReq.Messages) != 1 {
		t.Fatalf("len(Messages) = %d, want 1", len(muxReq.Messages))
	}
	msg := muxReq.Messages[0]
	if msg.Role != muxllm.RoleUser {
		t.Errorf("tool result message Role = %q, want %q", msg.Role, muxllm.RoleUser)
	}
	if len(msg.Blocks) != 1 {
		t.Fatalf("len(Blocks) = %d, want 1", len(msg.Blocks))
	}
	block := msg.Blocks[0]
	if block.Type != muxllm.ContentTypeToolResult {
		t.Errorf("block Type = %q, want %q", block.Type, muxllm.ContentTypeToolResult)
	}
	if block.ToolUseID != "call_123" {
		t.Errorf("block ToolUseID = %q, want %q", block.ToolUseID, "call_123")
	}
	if block.Text != "file contents here" {
		t.Errorf("block Text = %q, want %q", block.Text, "file contents here")
	}
	if block.IsError {
		t.Error("block IsError = true, want false")
	}
}

func TestConvertRequest_ToolResultWithError(t *testing.T) {
	req := Request{
		Model: "test-model",
		Messages: []Message{
			ToolResultMessage("call_456", "command failed", true),
		},
	}

	muxReq := convertRequest(req)

	block := muxReq.Messages[0].Blocks[0]
	if !block.IsError {
		t.Error("block IsError = false, want true")
	}
}

func TestConvertRequest_AssistantToolCallMessage(t *testing.T) {
	args := json.RawMessage(`{"path": "/tmp/test.go", "content": "package main"}`)
	req := Request{
		Model: "test-model",
		Messages: []Message{
			{
				Role: RoleAssistant,
				Content: []ContentPart{
					TextPart("Let me write that file."),
					ToolCallPart("call_abc", "write_file", args),
				},
			},
		},
	}

	muxReq := convertRequest(req)

	if len(muxReq.Messages) != 1 {
		t.Fatalf("len(Messages) = %d, want 1", len(muxReq.Messages))
	}
	msg := muxReq.Messages[0]
	if msg.Role != muxllm.RoleAssistant {
		t.Errorf("Role = %q, want %q", msg.Role, muxllm.RoleAssistant)
	}
	if len(msg.Blocks) != 2 {
		t.Fatalf("len(Blocks) = %d, want 2", len(msg.Blocks))
	}
	// Text block
	if msg.Blocks[0].Type != muxllm.ContentTypeText {
		t.Errorf("Blocks[0].Type = %q, want %q", msg.Blocks[0].Type, muxllm.ContentTypeText)
	}
	if msg.Blocks[0].Text != "Let me write that file." {
		t.Errorf("Blocks[0].Text = %q, want %q", msg.Blocks[0].Text, "Let me write that file.")
	}
	// Tool use block
	if msg.Blocks[1].Type != muxllm.ContentTypeToolUse {
		t.Errorf("Blocks[1].Type = %q, want %q", msg.Blocks[1].Type, muxllm.ContentTypeToolUse)
	}
	if msg.Blocks[1].ID != "call_abc" {
		t.Errorf("Blocks[1].ID = %q, want %q", msg.Blocks[1].ID, "call_abc")
	}
	if msg.Blocks[1].Name != "write_file" {
		t.Errorf("Blocks[1].Name = %q, want %q", msg.Blocks[1].Name, "write_file")
	}
	// Check tool arguments were deserialized to map
	if msg.Blocks[1].Input["path"] != "/tmp/test.go" {
		t.Errorf("Blocks[1].Input[path] = %v, want %q", msg.Blocks[1].Input["path"], "/tmp/test.go")
	}
}

func TestConvertRequest_ThinkingAndRedactedDropped(t *testing.T) {
	req := Request{
		Model: "test-model",
		Messages: []Message{
			{
				Role: RoleAssistant,
				Content: []ContentPart{
					ThinkingPart("deep thoughts", "sig123"),
					RedactedThinkingPart("", "sig456"),
					TextPart("Here is my answer"),
				},
			},
		},
	}

	muxReq := convertRequest(req)

	msg := muxReq.Messages[0]
	if len(msg.Blocks) != 1 {
		t.Fatalf("len(Blocks) = %d, want 1 (thinking parts should be dropped)", len(msg.Blocks))
	}
	if msg.Blocks[0].Text != "Here is my answer" {
		t.Errorf("Blocks[0].Text = %q, want %q", msg.Blocks[0].Text, "Here is my answer")
	}
}

func TestConvertRequest_ToolDefinitions(t *testing.T) {
	params := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)
	req := Request{
		Model:    "test-model",
		Messages: []Message{UserMessage("hello")},
		Tools: []ToolDefinition{
			{
				Name:        "read_file",
				Description: "Read a file",
				Parameters:  params,
			},
		},
	}

	muxReq := convertRequest(req)

	if len(muxReq.Tools) != 1 {
		t.Fatalf("len(Tools) = %d, want 1", len(muxReq.Tools))
	}
	tool := muxReq.Tools[0]
	if tool.Name != "read_file" {
		t.Errorf("tool Name = %q, want %q", tool.Name, "read_file")
	}
	if tool.Description != "Read a file" {
		t.Errorf("tool Description = %q, want %q", tool.Description, "Read a file")
	}
	if tool.InputSchema["type"] != "object" {
		t.Errorf("tool InputSchema[type] = %v, want %q", tool.InputSchema["type"], "object")
	}
}

func TestConvertRequest_SimpleTextMessageUsesContentField(t *testing.T) {
	// When a message has a single text part, we should use the Content string field
	// for simpler API payload (not Blocks).
	req := Request{
		Model:    "test-model",
		Messages: []Message{UserMessage("just text")},
	}

	muxReq := convertRequest(req)

	msg := muxReq.Messages[0]
	if msg.Content != "just text" {
		t.Errorf("Content = %q, want %q", msg.Content, "just text")
	}
	if len(msg.Blocks) != 0 {
		t.Errorf("len(Blocks) = %d, want 0 for simple text messages", len(msg.Blocks))
	}
}

func TestConvertResponse_TextOnly(t *testing.T) {
	muxResp := &muxllm.Response{
		ID:    "msg_123",
		Model: "claude-sonnet-4-20250514",
		Content: []muxllm.ContentBlock{
			{Type: muxllm.ContentTypeText, Text: "Hello there!"},
		},
		StopReason: muxllm.StopReasonEndTurn,
		Usage: muxllm.Usage{
			InputTokens:  10,
			OutputTokens: 5,
		},
	}

	resp := convertResponse(muxResp, "mux")

	if resp.ID != "msg_123" {
		t.Errorf("ID = %q, want %q", resp.ID, "msg_123")
	}
	if resp.Model != "claude-sonnet-4-20250514" {
		t.Errorf("Model = %q, want %q", resp.Model, "claude-sonnet-4-20250514")
	}
	if resp.Provider != "mux" {
		t.Errorf("Provider = %q, want %q", resp.Provider, "mux")
	}
	if resp.Message.Role != RoleAssistant {
		t.Errorf("Message.Role = %q, want %q", resp.Message.Role, RoleAssistant)
	}
	if resp.TextContent() != "Hello there!" {
		t.Errorf("TextContent() = %q, want %q", resp.TextContent(), "Hello there!")
	}
	if resp.FinishReason.Reason != FinishStop {
		t.Errorf("FinishReason.Reason = %q, want %q", resp.FinishReason.Reason, FinishStop)
	}
	if resp.FinishReason.Raw != string(muxllm.StopReasonEndTurn) {
		t.Errorf("FinishReason.Raw = %q, want %q", resp.FinishReason.Raw, muxllm.StopReasonEndTurn)
	}
	if resp.Usage.InputTokens != 10 {
		t.Errorf("Usage.InputTokens = %d, want 10", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 5 {
		t.Errorf("Usage.OutputTokens = %d, want 5", resp.Usage.OutputTokens)
	}
	if resp.Usage.TotalTokens != 15 {
		t.Errorf("Usage.TotalTokens = %d, want 15", resp.Usage.TotalTokens)
	}
}

func TestConvertResponse_WithToolCalls(t *testing.T) {
	muxResp := &muxllm.Response{
		ID:    "msg_456",
		Model: "test-model",
		Content: []muxllm.ContentBlock{
			{Type: muxllm.ContentTypeText, Text: "I'll read the file."},
			{
				Type:  muxllm.ContentTypeToolUse,
				ID:    "call_xyz",
				Name:  "read_file",
				Input: map[string]any{"path": "/tmp/test.go"},
			},
		},
		StopReason: muxllm.StopReasonToolUse,
		Usage:      muxllm.Usage{InputTokens: 20, OutputTokens: 15},
	}

	resp := convertResponse(muxResp, "mux")

	if resp.FinishReason.Reason != FinishToolCalls {
		t.Errorf("FinishReason.Reason = %q, want %q", resp.FinishReason.Reason, FinishToolCalls)
	}

	parts := resp.Message.Content
	if len(parts) != 2 {
		t.Fatalf("len(parts) = %d, want 2", len(parts))
	}

	// Text part
	if parts[0].Kind != ContentText {
		t.Errorf("parts[0].Kind = %q, want %q", parts[0].Kind, ContentText)
	}
	if parts[0].Text != "I'll read the file." {
		t.Errorf("parts[0].Text = %q, want %q", parts[0].Text, "I'll read the file.")
	}

	// Tool call part
	if parts[1].Kind != ContentToolCall {
		t.Errorf("parts[1].Kind = %q, want %q", parts[1].Kind, ContentToolCall)
	}
	tc := parts[1].ToolCall
	if tc == nil {
		t.Fatal("parts[1].ToolCall is nil")
	}
	if tc.ID != "call_xyz" {
		t.Errorf("ToolCall.ID = %q, want %q", tc.ID, "call_xyz")
	}
	if tc.Name != "read_file" {
		t.Errorf("ToolCall.Name = %q, want %q", tc.Name, "read_file")
	}

	// Verify arguments were marshaled back to JSON
	var argsMap map[string]any
	if err := json.Unmarshal(tc.Arguments, &argsMap); err != nil {
		t.Fatalf("Unmarshal tool call arguments: %v", err)
	}
	if argsMap["path"] != "/tmp/test.go" {
		t.Errorf("arguments[path] = %v, want %q", argsMap["path"], "/tmp/test.go")
	}
}

func TestConvertResponse_StopReasonMapping(t *testing.T) {
	tests := []struct {
		muxReason  muxllm.StopReason
		wantReason string
		wantRaw    string
	}{
		{muxllm.StopReasonEndTurn, FinishStop, "end_turn"},
		{muxllm.StopReasonToolUse, FinishToolCalls, "tool_use"},
		{muxllm.StopReasonMaxTokens, FinishLength, "max_tokens"},
		{muxllm.StopReason("unknown_reason"), FinishOther, "unknown_reason"},
	}

	for _, tt := range tests {
		t.Run(string(tt.muxReason), func(t *testing.T) {
			muxResp := &muxllm.Response{
				ID:         "msg_test",
				Model:      "test-model",
				Content:    []muxllm.ContentBlock{{Type: muxllm.ContentTypeText, Text: "test"}},
				StopReason: tt.muxReason,
			}

			resp := convertResponse(muxResp, "mux")

			if resp.FinishReason.Reason != tt.wantReason {
				t.Errorf("Reason = %q, want %q", resp.FinishReason.Reason, tt.wantReason)
			}
			if resp.FinishReason.Raw != tt.wantRaw {
				t.Errorf("Raw = %q, want %q", resp.FinishReason.Raw, tt.wantRaw)
			}
		})
	}
}

func TestConvertRequest_MaxTokensZeroWhenNil(t *testing.T) {
	req := Request{
		Model:    "test-model",
		Messages: []Message{UserMessage("hello")},
		// MaxTokens is nil
	}

	muxReq := convertRequest(req)

	if muxReq.MaxTokens != 0 {
		t.Errorf("MaxTokens = %d, want 0 when source is nil", muxReq.MaxTokens)
	}
}

func TestMuxAdapter_Complete_EndToEnd(t *testing.T) {
	stub := &stubMuxClient{
		response: &muxllm.Response{
			ID:    "msg_e2e",
			Model: "claude-sonnet-4-20250514",
			Content: []muxllm.ContentBlock{
				{Type: muxllm.ContentTypeText, Text: "I am working on it."},
				{
					Type:  muxllm.ContentTypeToolUse,
					ID:    "call_001",
					Name:  "bash",
					Input: map[string]any{"command": "ls -la"},
				},
			},
			StopReason: muxllm.StopReasonToolUse,
			Usage:      muxllm.Usage{InputTokens: 100, OutputTokens: 50},
		},
	}
	adapter := NewMuxAdapter("mux", stub)

	params := json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}`)
	req := Request{
		Model: "claude-sonnet-4-20250514",
		Messages: []Message{
			SystemMessage("You are a coding assistant."),
			UserMessage("List the files."),
		},
		Tools: []ToolDefinition{
			{Name: "bash", Description: "Run a bash command", Parameters: params},
		},
		MaxTokens:   intPtr(4096),
		Temperature: Float64Ptr(0.5),
	}

	resp, err := adapter.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	// Verify the request was converted correctly
	if stub.lastRequest == nil {
		t.Fatal("lastRequest is nil, Complete did not call CreateMessage")
	}
	if stub.lastRequest.System != "You are a coding assistant." {
		t.Errorf("request System = %q, want %q", stub.lastRequest.System, "You are a coding assistant.")
	}
	if len(stub.lastRequest.Messages) != 1 {
		t.Errorf("len(request Messages) = %d, want 1 (system extracted)", len(stub.lastRequest.Messages))
	}
	if len(stub.lastRequest.Tools) != 1 {
		t.Errorf("len(request Tools) = %d, want 1", len(stub.lastRequest.Tools))
	}

	// Verify the response was converted correctly
	if resp.ID != "msg_e2e" {
		t.Errorf("resp.ID = %q, want %q", resp.ID, "msg_e2e")
	}
	if resp.Provider != "mux" {
		t.Errorf("resp.Provider = %q, want %q", resp.Provider, "mux")
	}
	if resp.FinishReason.Reason != FinishToolCalls {
		t.Errorf("resp.FinishReason.Reason = %q, want %q", resp.FinishReason.Reason, FinishToolCalls)
	}
	toolCalls := resp.ToolCalls()
	if len(toolCalls) != 1 {
		t.Fatalf("len(ToolCalls) = %d, want 1", len(toolCalls))
	}
	if toolCalls[0].Name != "bash" {
		t.Errorf("ToolCalls[0].Name = %q, want %q", toolCalls[0].Name, "bash")
	}
}

func TestMuxAdapter_Complete_Error(t *testing.T) {
	stub := &stubMuxClient{
		err: fmt.Errorf("connection refused"),
	}
	adapter := NewMuxAdapter("mux", stub)

	_, err := adapter.Complete(context.Background(), Request{
		Model:    "test-model",
		Messages: []Message{UserMessage("hello")},
	})
	if err == nil {
		t.Fatal("Complete() expected error, got nil")
	}
	if err.Error() != "mux adapter complete: connection refused" {
		t.Errorf("error = %q, want wrapped error", err.Error())
	}
}

func TestMuxAdapter_Stream_EndToEnd(t *testing.T) {
	stub := &stubMuxClient{
		streamEvents: []muxllm.StreamEvent{
			{Type: muxllm.EventMessageStart, Response: &muxllm.Response{ID: "msg_stream"}},
			{Type: muxllm.EventContentStart, Index: 0, Block: &muxllm.ContentBlock{Type: muxllm.ContentTypeText}},
			{Type: muxllm.EventContentDelta, Index: 0, Text: "Hello "},
			{Type: muxllm.EventContentDelta, Index: 0, Text: "world"},
			{Type: muxllm.EventContentStop, Index: 0},
			{Type: muxllm.EventMessageStop, Response: &muxllm.Response{
				ID:         "msg_stream",
				Model:      "test-model",
				StopReason: muxllm.StopReasonEndTurn,
				Usage:      muxllm.Usage{InputTokens: 5, OutputTokens: 2},
			}},
		},
	}
	adapter := NewMuxAdapter("mux", stub)

	ch, err := adapter.Stream(context.Background(), Request{
		Model:    "test-model",
		Messages: []Message{UserMessage("say hi")},
	})
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}

	var events []StreamEvent
	for evt := range ch {
		events = append(events, evt)
	}

	// Verify event sequence
	if len(events) == 0 {
		t.Fatal("received 0 events")
	}

	// Check we got a stream_start
	if events[0].Type != StreamStart {
		t.Errorf("events[0].Type = %q, want %q", events[0].Type, StreamStart)
	}

	// Check we got text deltas
	var textContent string
	for _, evt := range events {
		if evt.Type == StreamTextDelta {
			textContent += evt.Delta
		}
	}
	if textContent != "Hello world" {
		t.Errorf("accumulated text = %q, want %q", textContent, "Hello world")
	}

	// Check we got a finish event
	lastEvt := events[len(events)-1]
	if lastEvt.Type != StreamFinish {
		t.Errorf("last event Type = %q, want %q", lastEvt.Type, StreamFinish)
	}
}

func TestMuxAdapter_Stream_WithToolUse(t *testing.T) {
	stub := &stubMuxClient{
		streamEvents: []muxllm.StreamEvent{
			{Type: muxllm.EventMessageStart, Response: &muxllm.Response{ID: "msg_tool_stream"}},
			{Type: muxllm.EventContentStart, Index: 0, Block: &muxllm.ContentBlock{
				Type: muxllm.ContentTypeToolUse,
				ID:   "call_stream_1",
				Name: "read_file",
			}},
			{Type: muxllm.EventContentDelta, Index: 0, Text: `{"path": "/tmp`},
			{Type: muxllm.EventContentDelta, Index: 0, Text: `/file.go"}`},
			{Type: muxllm.EventContentStop, Index: 0},
			{Type: muxllm.EventMessageStop, Response: &muxllm.Response{
				ID:         "msg_tool_stream",
				Model:      "test-model",
				StopReason: muxllm.StopReasonToolUse,
				Usage:      muxllm.Usage{InputTokens: 10, OutputTokens: 8},
			}},
		},
	}
	adapter := NewMuxAdapter("mux", stub)

	ch, err := adapter.Stream(context.Background(), Request{
		Model:    "test-model",
		Messages: []Message{UserMessage("read a file")},
	})
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}

	var events []StreamEvent
	for evt := range ch {
		events = append(events, evt)
	}

	// Find tool_call_start event
	var foundToolStart bool
	var foundToolDelta bool
	var foundToolEnd bool
	for _, evt := range events {
		switch evt.Type {
		case StreamToolStart:
			foundToolStart = true
			if evt.ToolCall == nil {
				t.Error("tool_call_start event has nil ToolCall")
			} else {
				if evt.ToolCall.ID != "call_stream_1" {
					t.Errorf("ToolCall.ID = %q, want %q", evt.ToolCall.ID, "call_stream_1")
				}
				if evt.ToolCall.Name != "read_file" {
					t.Errorf("ToolCall.Name = %q, want %q", evt.ToolCall.Name, "read_file")
				}
			}
		case StreamToolDelta:
			foundToolDelta = true
		case StreamToolEnd:
			foundToolEnd = true
		}
	}
	if !foundToolStart {
		t.Error("did not find tool_call_start event")
	}
	if !foundToolDelta {
		t.Error("did not find tool_call_delta event")
	}
	if !foundToolEnd {
		t.Error("did not find tool_call_end event")
	}
}

func TestMuxAdapter_Stream_Error(t *testing.T) {
	stub := &stubMuxClient{
		streamErr: fmt.Errorf("stream not supported"),
	}
	adapter := NewMuxAdapter("mux", stub)

	_, err := adapter.Stream(context.Background(), Request{
		Model:    "test-model",
		Messages: []Message{UserMessage("hello")},
	})
	if err == nil {
		t.Fatal("Stream() expected error, got nil")
	}
}

func TestMuxAdapter_Stream_ErrorEvent(t *testing.T) {
	stub := &stubMuxClient{
		streamEvents: []muxllm.StreamEvent{
			{Type: muxllm.EventMessageStart, Response: &muxllm.Response{ID: "msg_err"}},
			{Type: muxllm.EventError, Error: fmt.Errorf("overloaded")},
		},
	}
	adapter := NewMuxAdapter("mux", stub)

	ch, err := adapter.Stream(context.Background(), Request{
		Model:    "test-model",
		Messages: []Message{UserMessage("hello")},
	})
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}

	var foundError bool
	for evt := range ch {
		if evt.Type == StreamErrorEvt {
			foundError = true
			if evt.Error == nil {
				t.Error("error event has nil Error")
			}
		}
	}
	if !foundError {
		t.Error("did not find error event in stream")
	}
}

func TestConvertContentPartsToBlocks_MixedContent(t *testing.T) {
	args := json.RawMessage(`{"key":"value"}`)
	parts := []ContentPart{
		TextPart("some text"),
		ToolCallPart("call_1", "tool_a", args),
		ToolResultPart("call_2", "result text", false),
		ThinkingPart("thinking...", "sig"),
		ImageURLPart("http://example.com/img.png"),
	}

	blocks := convertContentPartsToBlocks(parts)

	// Thinking and Image should be dropped
	if len(blocks) != 3 {
		t.Fatalf("len(blocks) = %d, want 3 (thinking/image dropped)", len(blocks))
	}

	if blocks[0].Type != muxllm.ContentTypeText {
		t.Errorf("blocks[0].Type = %q, want %q", blocks[0].Type, muxllm.ContentTypeText)
	}
	if blocks[1].Type != muxllm.ContentTypeToolUse {
		t.Errorf("blocks[1].Type = %q, want %q", blocks[1].Type, muxllm.ContentTypeToolUse)
	}
	if blocks[2].Type != muxllm.ContentTypeToolResult {
		t.Errorf("blocks[2].Type = %q, want %q", blocks[2].Type, muxllm.ContentTypeToolResult)
	}
}

func TestConvertBlocksToContentParts(t *testing.T) {
	blocks := []muxllm.ContentBlock{
		{Type: muxllm.ContentTypeText, Text: "hello"},
		{
			Type:  muxllm.ContentTypeToolUse,
			ID:    "call_x",
			Name:  "my_tool",
			Input: map[string]any{"a": float64(1), "b": "two"},
		},
		{
			Type:      muxllm.ContentTypeToolResult,
			ToolUseID: "call_y",
			Text:      "result",
			IsError:   true,
		},
	}

	parts := convertBlocksToContentParts(blocks)

	if len(parts) != 3 {
		t.Fatalf("len(parts) = %d, want 3", len(parts))
	}

	// Text part
	if parts[0].Kind != ContentText {
		t.Errorf("parts[0].Kind = %q, want %q", parts[0].Kind, ContentText)
	}
	if parts[0].Text != "hello" {
		t.Errorf("parts[0].Text = %q, want %q", parts[0].Text, "hello")
	}

	// Tool call part
	if parts[1].Kind != ContentToolCall {
		t.Errorf("parts[1].Kind = %q, want %q", parts[1].Kind, ContentToolCall)
	}
	tc := parts[1].ToolCall
	if tc.ID != "call_x" {
		t.Errorf("ToolCall.ID = %q, want %q", tc.ID, "call_x")
	}
	var argsMap map[string]any
	if err := json.Unmarshal(tc.Arguments, &argsMap); err != nil {
		t.Fatalf("Unmarshal arguments: %v", err)
	}
	if argsMap["a"] != float64(1) {
		t.Errorf("arguments[a] = %v, want 1", argsMap["a"])
	}

	// Tool result part
	if parts[2].Kind != ContentToolResult {
		t.Errorf("parts[2].Kind = %q, want %q", parts[2].Kind, ContentToolResult)
	}
	tr := parts[2].ToolResult
	if tr.ToolCallID != "call_y" {
		t.Errorf("ToolResult.ToolCallID = %q, want %q", tr.ToolCallID, "call_y")
	}
	if tr.Content != "result" {
		t.Errorf("ToolResult.Content = %q, want %q", tr.Content, "result")
	}
	if !tr.IsError {
		t.Error("ToolResult.IsError = false, want true")
	}
}

func TestConvertStreamEvent(t *testing.T) {
	tests := []struct {
		name       string
		muxEvent   muxllm.StreamEvent
		wantType   StreamEventType
		checkDelta string
	}{
		{
			name:     "message_start",
			muxEvent: muxllm.StreamEvent{Type: muxllm.EventMessageStart, Response: &muxllm.Response{ID: "msg_1"}},
			wantType: StreamStart,
		},
		{
			name: "content_block_start_text",
			muxEvent: muxllm.StreamEvent{
				Type:  muxllm.EventContentStart,
				Block: &muxllm.ContentBlock{Type: muxllm.ContentTypeText},
			},
			wantType: StreamTextStart,
		},
		{
			name: "content_block_start_tool_use",
			muxEvent: muxllm.StreamEvent{
				Type: muxllm.EventContentStart,
				Block: &muxllm.ContentBlock{
					Type: muxllm.ContentTypeToolUse,
					ID:   "call_s1",
					Name: "tool_name",
				},
			},
			wantType: StreamToolStart,
		},
		{
			name:       "content_block_delta_text",
			muxEvent:   muxllm.StreamEvent{Type: muxllm.EventContentDelta, Text: "chunk"},
			wantType:   StreamTextDelta,
			checkDelta: "chunk",
		},
		{
			name:     "content_block_stop",
			muxEvent: muxllm.StreamEvent{Type: muxllm.EventContentStop},
			wantType: StreamTextEnd,
		},
		{
			name: "message_stop",
			muxEvent: muxllm.StreamEvent{
				Type: muxllm.EventMessageStop,
				Response: &muxllm.Response{
					StopReason: muxllm.StopReasonEndTurn,
					Usage:      muxllm.Usage{InputTokens: 5, OutputTokens: 3},
				},
			},
			wantType: StreamFinish,
		},
		{
			name:     "error",
			muxEvent: muxllm.StreamEvent{Type: muxllm.EventError, Error: fmt.Errorf("bad")},
			wantType: StreamErrorEvt,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// For content_block_start/delta/stop with tool context, we need the tracking state.
			// We'll test the simpler conversion function here and the stateful version through Stream().
			evt := convertStreamEvent(tt.muxEvent, nil)
			if evt.Type != tt.wantType {
				t.Errorf("Type = %q, want %q", evt.Type, tt.wantType)
			}
			if tt.checkDelta != "" && evt.Delta != tt.checkDelta {
				t.Errorf("Delta = %q, want %q", evt.Delta, tt.checkDelta)
			}
		})
	}
}

func TestConvertStreamEvent_MessageStartCarriesUsage(t *testing.T) {
	// Anthropic sends input_tokens in message_start. The adapter must forward
	// this usage data on the StreamStart event so the accumulator can capture it.
	evt := convertStreamEvent(muxllm.StreamEvent{
		Type: muxllm.EventMessageStart,
		Response: &muxllm.Response{
			ID:    "msg_abc",
			Usage: muxllm.Usage{InputTokens: 2048, OutputTokens: 0},
		},
	}, nil)

	if evt.Type != StreamStart {
		t.Fatalf("expected StreamStart, got %q", evt.Type)
	}
	if evt.Usage == nil {
		t.Fatal("expected Usage on StreamStart for Anthropic message_start")
	}
	if evt.Usage.InputTokens != 2048 {
		t.Errorf("expected InputTokens=2048, got %d", evt.Usage.InputTokens)
	}
}

func TestConvertStreamEvent_MessageStartNoUsageWhenZero(t *testing.T) {
	// OpenAI/Gemini send empty message_start. Usage should remain nil.
	evt := convertStreamEvent(muxllm.StreamEvent{
		Type: muxllm.EventMessageStart,
	}, nil)

	if evt.Usage != nil {
		t.Errorf("expected nil Usage for message_start without response, got %+v", evt.Usage)
	}
}

// intPtr is a helper for creating *int values in tests.
func intPtr(v int) *int {
	return &v
}
