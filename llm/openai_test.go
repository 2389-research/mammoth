// ABOUTME: Tests for the OpenAI Responses API provider adapter.
// ABOUTME: Validates request translation, response parsing, streaming, error handling, and option configuration.

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

func TestOpenAIAdapterName(t *testing.T) {
	adapter := NewOpenAIAdapter("sk-test")
	if got := adapter.Name(); got != "openai" {
		t.Errorf("Name() = %q, want %q", got, "openai")
	}
}

func TestOpenAIRequestTranslation(t *testing.T) {
	var receivedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading body: %v", err)
			return
		}
		if err := json.Unmarshal(body, &receivedBody); err != nil {
			t.Errorf("unmarshalling body: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{
			"id": "resp_123",
			"model": "gpt-5.2",
			"status": "completed",
			"output": [
				{
					"type": "message",
					"role": "assistant",
					"content": [{"type": "output_text", "text": "Hello back!"}]
				}
			],
			"usage": {
				"input_tokens": 10,
				"output_tokens": 5,
				"total_tokens": 15
			}
		}`
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	adapter := NewOpenAIAdapter("sk-test", WithOpenAIBaseURL(server.URL))

	req := Request{
		Model: "gpt-5.2",
		Messages: []Message{
			UserMessage("Hello"),
			AssistantMessage("Hi there"),
			UserMessage("How are you?"),
		},
		Temperature: Float64Ptr(0.7),
		MaxTokens:   IntPtr(100),
		TopP:        Float64Ptr(0.9),
	}

	_, err := adapter.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	// Check model was set
	if model, ok := receivedBody["model"].(string); !ok || model != "gpt-5.2" {
		t.Errorf("model = %v, want %q", receivedBody["model"], "gpt-5.2")
	}

	// Check temperature
	if temp, ok := receivedBody["temperature"].(float64); !ok || temp != 0.7 {
		t.Errorf("temperature = %v, want 0.7", receivedBody["temperature"])
	}

	// Check max_output_tokens
	if maxTok, ok := receivedBody["max_output_tokens"].(float64); !ok || int(maxTok) != 100 {
		t.Errorf("max_output_tokens = %v, want 100", receivedBody["max_output_tokens"])
	}

	// Check top_p
	if topP, ok := receivedBody["top_p"].(float64); !ok || topP != 0.9 {
		t.Errorf("top_p = %v, want 0.9", receivedBody["top_p"])
	}

	// Check input items
	input, ok := receivedBody["input"].([]any)
	if !ok {
		t.Fatalf("input is not an array: %T", receivedBody["input"])
	}
	if len(input) != 3 {
		t.Fatalf("input has %d items, want 3", len(input))
	}

	// First input: user message
	item0 := input[0].(map[string]any)
	if item0["type"] != "message" {
		t.Errorf("input[0].type = %v, want %q", item0["type"], "message")
	}
	if item0["role"] != "user" {
		t.Errorf("input[0].role = %v, want %q", item0["role"], "user")
	}
	content0 := item0["content"].([]any)
	part0 := content0[0].(map[string]any)
	if part0["type"] != "input_text" {
		t.Errorf("input[0].content[0].type = %v, want %q", part0["type"], "input_text")
	}
	if part0["text"] != "Hello" {
		t.Errorf("input[0].content[0].text = %v, want %q", part0["text"], "Hello")
	}

	// Second input: assistant message
	item1 := input[1].(map[string]any)
	if item1["type"] != "message" {
		t.Errorf("input[1].type = %v, want %q", item1["type"], "message")
	}
	if item1["role"] != "assistant" {
		t.Errorf("input[1].role = %v, want %q", item1["role"], "assistant")
	}
	content1 := item1["content"].([]any)
	part1 := content1[0].(map[string]any)
	if part1["type"] != "output_text" {
		t.Errorf("input[1].content[0].type = %v, want %q", part1["type"], "output_text")
	}
}

func TestOpenAISystemMessageExtraction(t *testing.T) {
	var receivedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading body: %v", err)
			return
		}
		if err := json.Unmarshal(body, &receivedBody); err != nil {
			t.Errorf("unmarshalling body: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{
			"id": "resp_123",
			"model": "gpt-5.2",
			"status": "completed",
			"output": [
				{
					"type": "message",
					"role": "assistant",
					"content": [{"type": "output_text", "text": "OK"}]
				}
			],
			"usage": {"input_tokens": 10, "output_tokens": 5, "total_tokens": 15}
		}`
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	adapter := NewOpenAIAdapter("sk-test", WithOpenAIBaseURL(server.URL))

	req := Request{
		Model: "gpt-5.2",
		Messages: []Message{
			SystemMessage("You are a helpful assistant."),
			DeveloperMessage("Be concise."),
			UserMessage("Hello"),
		},
	}

	_, err := adapter.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	// instructions should contain system + developer messages
	instructions, ok := receivedBody["instructions"].(string)
	if !ok {
		t.Fatalf("instructions is not a string: %T", receivedBody["instructions"])
	}
	if instructions != "You are a helpful assistant.\nBe concise." {
		t.Errorf("instructions = %q, want %q", instructions, "You are a helpful assistant.\nBe concise.")
	}

	// input should only have the user message
	input, ok := receivedBody["input"].([]any)
	if !ok {
		t.Fatalf("input is not an array: %T", receivedBody["input"])
	}
	if len(input) != 1 {
		t.Fatalf("input has %d items, want 1 (system messages should be extracted)", len(input))
	}
}

func TestOpenAIToolDefinitionTranslation(t *testing.T) {
	var receivedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading body: %v", err)
			return
		}
		if err := json.Unmarshal(body, &receivedBody); err != nil {
			t.Errorf("unmarshalling body: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{
			"id": "resp_123",
			"model": "gpt-5.2",
			"status": "completed",
			"output": [
				{
					"type": "message",
					"role": "assistant",
					"content": [{"type": "output_text", "text": "OK"}]
				}
			],
			"usage": {"input_tokens": 10, "output_tokens": 5, "total_tokens": 15}
		}`
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	adapter := NewOpenAIAdapter("sk-test", WithOpenAIBaseURL(server.URL))

	params := json.RawMessage(`{"type":"object","properties":{"location":{"type":"string"}},"required":["location"]}`)
	req := Request{
		Model:    "gpt-5.2",
		Messages: []Message{UserMessage("What's the weather?")},
		Tools: []ToolDefinition{
			{
				Name:        "get_weather",
				Description: "Get the current weather",
				Parameters:  params,
			},
		},
	}

	_, err := adapter.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	tools, ok := receivedBody["tools"].([]any)
	if !ok {
		t.Fatalf("tools is not an array: %T", receivedBody["tools"])
	}
	if len(tools) != 1 {
		t.Fatalf("tools has %d items, want 1", len(tools))
	}

	tool := tools[0].(map[string]any)
	if tool["type"] != "function" {
		t.Errorf("tool.type = %v, want %q", tool["type"], "function")
	}
	if tool["name"] != "get_weather" {
		t.Errorf("tool.name = %v, want %q", tool["name"], "get_weather")
	}
	if tool["description"] != "Get the current weather" {
		t.Errorf("tool.description = %v, want %q", tool["description"], "Get the current weather")
	}
	if tool["parameters"] == nil {
		t.Error("tool.parameters should not be nil")
	}
}

func TestOpenAIToolChoiceTranslation(t *testing.T) {
	tests := []struct {
		name       string
		toolChoice *ToolChoice
		wantValue  any // string or map depending on mode
		wantAbsent bool
	}{
		{
			name:       "auto",
			toolChoice: &ToolChoice{Mode: ToolChoiceAuto},
			wantValue:  "auto",
		},
		{
			name:       "none",
			toolChoice: &ToolChoice{Mode: ToolChoiceNone},
			wantValue:  "none",
		},
		{
			name:       "required",
			toolChoice: &ToolChoice{Mode: ToolChoiceRequired},
			wantValue:  "required",
		},
		{
			name:       "named",
			toolChoice: &ToolChoice{Mode: ToolChoiceNamed, ToolName: "get_weather"},
			wantValue:  map[string]any{"type": "function", "name": "get_weather"},
		},
		{
			name:       "nil",
			toolChoice: nil,
			wantAbsent: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var receivedBody map[string]any

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("reading body: %v", err)
					return
				}
				if err := json.Unmarshal(body, &receivedBody); err != nil {
					t.Errorf("unmarshalling body: %v", err)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				resp := `{
					"id": "resp_123",
					"model": "gpt-5.2",
					"status": "completed",
					"output": [
						{
							"type": "message",
							"role": "assistant",
							"content": [{"type": "output_text", "text": "OK"}]
						}
					],
					"usage": {"input_tokens": 10, "output_tokens": 5, "total_tokens": 15}
				}`
				_, _ = w.Write([]byte(resp))
			}))
			defer server.Close()

			adapter := NewOpenAIAdapter("sk-test", WithOpenAIBaseURL(server.URL))

			params := json.RawMessage(`{"type":"object","properties":{}}`)
			req := Request{
				Model:    "gpt-5.2",
				Messages: []Message{UserMessage("test")},
				Tools: []ToolDefinition{
					{Name: "get_weather", Description: "Get weather", Parameters: params},
				},
				ToolChoice: tc.toolChoice,
			}

			_, err := adapter.Complete(context.Background(), req)
			if err != nil {
				t.Fatalf("Complete() error: %v", err)
			}

			if tc.wantAbsent {
				if _, exists := receivedBody["tool_choice"]; exists {
					t.Errorf("tool_choice should be absent, got %v", receivedBody["tool_choice"])
				}
				return
			}

			gotChoice := receivedBody["tool_choice"]
			if gotChoice == nil {
				t.Fatalf("tool_choice is nil, want %v", tc.wantValue)
			}

			// For string values
			if wantStr, ok := tc.wantValue.(string); ok {
				gotStr, ok := gotChoice.(string)
				if !ok {
					t.Fatalf("tool_choice is %T, want string", gotChoice)
				}
				if gotStr != wantStr {
					t.Errorf("tool_choice = %q, want %q", gotStr, wantStr)
				}
				return
			}

			// For named tool choice (map)
			if wantMap, ok := tc.wantValue.(map[string]any); ok {
				gotMap, ok := gotChoice.(map[string]any)
				if !ok {
					t.Fatalf("tool_choice is %T, want map", gotChoice)
				}
				for key, wantVal := range wantMap {
					if gotMap[key] != wantVal {
						t.Errorf("tool_choice.%s = %v, want %v", key, gotMap[key], wantVal)
					}
				}
			}
		})
	}
}

func TestOpenAIResponseParsing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{
			"id": "resp_abc123",
			"model": "gpt-5.2",
			"status": "completed",
			"output": [
				{
					"type": "message",
					"role": "assistant",
					"content": [
						{"type": "output_text", "text": "The answer is 42."}
					]
				}
			],
			"usage": {
				"input_tokens": 25,
				"output_tokens": 10,
				"total_tokens": 35,
				"output_tokens_details": {
					"reasoning_tokens": 3
				},
				"prompt_tokens_details": {
					"cached_tokens": 5
				}
			}
		}`
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	adapter := NewOpenAIAdapter("sk-test", WithOpenAIBaseURL(server.URL))

	req := Request{
		Model:    "gpt-5.2",
		Messages: []Message{UserMessage("What is the meaning of life?")},
	}

	resp, err := adapter.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	if resp.ID != "resp_abc123" {
		t.Errorf("ID = %q, want %q", resp.ID, "resp_abc123")
	}
	if resp.Model != "gpt-5.2" {
		t.Errorf("Model = %q, want %q", resp.Model, "gpt-5.2")
	}
	if resp.Provider != "openai" {
		t.Errorf("Provider = %q, want %q", resp.Provider, "openai")
	}

	// Check message content
	if resp.TextContent() != "The answer is 42." {
		t.Errorf("TextContent() = %q, want %q", resp.TextContent(), "The answer is 42.")
	}
	if resp.Message.Role != RoleAssistant {
		t.Errorf("Message.Role = %q, want %q", resp.Message.Role, RoleAssistant)
	}

	// Check finish reason
	if resp.FinishReason.Reason != FinishStop {
		t.Errorf("FinishReason.Reason = %q, want %q", resp.FinishReason.Reason, FinishStop)
	}

	// Check usage
	if resp.Usage.InputTokens != 25 {
		t.Errorf("Usage.InputTokens = %d, want 25", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 10 {
		t.Errorf("Usage.OutputTokens = %d, want 10", resp.Usage.OutputTokens)
	}
	if resp.Usage.TotalTokens != 35 {
		t.Errorf("Usage.TotalTokens = %d, want 35", resp.Usage.TotalTokens)
	}

	// Check reasoning tokens
	if resp.Usage.ReasoningTokens == nil {
		t.Fatal("Usage.ReasoningTokens should not be nil")
	}
	if *resp.Usage.ReasoningTokens != 3 {
		t.Errorf("Usage.ReasoningTokens = %d, want 3", *resp.Usage.ReasoningTokens)
	}

	// Check cache read tokens
	if resp.Usage.CacheReadTokens == nil {
		t.Fatal("Usage.CacheReadTokens should not be nil")
	}
	if *resp.Usage.CacheReadTokens != 5 {
		t.Errorf("Usage.CacheReadTokens = %d, want 5", *resp.Usage.CacheReadTokens)
	}
}

func TestOpenAIResponseParsingToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{
			"id": "resp_tools",
			"model": "gpt-5.2",
			"status": "completed",
			"output": [
				{
					"type": "function_call",
					"id": "call_123",
					"name": "get_weather",
					"arguments": "{\"location\":\"London\"}"
				}
			],
			"usage": {"input_tokens": 10, "output_tokens": 15, "total_tokens": 25}
		}`
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	adapter := NewOpenAIAdapter("sk-test", WithOpenAIBaseURL(server.URL))

	req := Request{
		Model:    "gpt-5.2",
		Messages: []Message{UserMessage("What's the weather in London?")},
	}

	resp, err := adapter.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	if resp.FinishReason.Reason != FinishToolCalls {
		t.Errorf("FinishReason.Reason = %q, want %q", resp.FinishReason.Reason, FinishToolCalls)
	}

	toolCalls := resp.ToolCalls()
	if len(toolCalls) != 1 {
		t.Fatalf("got %d tool calls, want 1", len(toolCalls))
	}
	if toolCalls[0].ID != "call_123" {
		t.Errorf("tool call ID = %q, want %q", toolCalls[0].ID, "call_123")
	}
	if toolCalls[0].Name != "get_weather" {
		t.Errorf("tool call name = %q, want %q", toolCalls[0].Name, "get_weather")
	}

	argsMap, err := toolCalls[0].ArgumentsMap()
	if err != nil {
		t.Fatalf("ArgumentsMap error: %v", err)
	}
	if argsMap["location"] != "London" {
		t.Errorf("location = %v, want %q", argsMap["location"], "London")
	}
}

func TestOpenAIResponseParsingMaxTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{
			"id": "resp_length",
			"model": "gpt-5.2",
			"status": "incomplete",
			"incomplete_details": {"reason": "max_output_tokens"},
			"output": [
				{
					"type": "message",
					"role": "assistant",
					"content": [{"type": "output_text", "text": "The answer is..."}]
				}
			],
			"usage": {"input_tokens": 10, "output_tokens": 100, "total_tokens": 110}
		}`
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	adapter := NewOpenAIAdapter("sk-test", WithOpenAIBaseURL(server.URL))

	req := Request{
		Model:    "gpt-5.2",
		Messages: []Message{UserMessage("Tell me a long story")},
	}

	resp, err := adapter.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	if resp.FinishReason.Reason != FinishLength {
		t.Errorf("FinishReason.Reason = %q, want %q", resp.FinishReason.Reason, FinishLength)
	}
}

func TestOpenAIErrorHandling(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantErr    any
	}{
		{
			name:       "401 unauthorized",
			statusCode: http.StatusUnauthorized,
			body:       `{"error":{"message":"Invalid API key","type":"invalid_api_key","code":"invalid_api_key"}}`,
			wantErr:    &AuthenticationError{},
		},
		{
			name:       "403 forbidden",
			statusCode: http.StatusForbidden,
			body:       `{"error":{"message":"Access denied","type":"access_denied"}}`,
			wantErr:    &AccessDeniedError{},
		},
		{
			name:       "404 not found",
			statusCode: http.StatusNotFound,
			body:       `{"error":{"message":"Model not found","type":"not_found"}}`,
			wantErr:    &NotFoundError{},
		},
		{
			name:       "429 rate limited",
			statusCode: http.StatusTooManyRequests,
			body:       `{"error":{"message":"Rate limit exceeded","type":"rate_limit_exceeded"}}`,
			wantErr:    &RateLimitError{},
		},
		{
			name:       "500 server error",
			statusCode: http.StatusInternalServerError,
			body:       `{"error":{"message":"Internal server error","type":"server_error"}}`,
			wantErr:    &ServerError{},
		},
		{
			name:       "400 bad request",
			statusCode: http.StatusBadRequest,
			body:       `{"error":{"message":"Invalid request","type":"invalid_request_error"}}`,
			wantErr:    &InvalidRequestError{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.statusCode)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			adapter := NewOpenAIAdapter("sk-test", WithOpenAIBaseURL(server.URL))

			req := Request{
				Model:    "gpt-5.2",
				Messages: []Message{UserMessage("Hello")},
			}

			_, err := adapter.Complete(context.Background(), req)
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			// Check error type
			switch tc.wantErr.(type) {
			case *AuthenticationError:
				var target *AuthenticationError
				if !errors.As(err, &target) {
					t.Errorf("error type = %T, want *AuthenticationError", err)
				}
			case *AccessDeniedError:
				var target *AccessDeniedError
				if !errors.As(err, &target) {
					t.Errorf("error type = %T, want *AccessDeniedError", err)
				}
			case *NotFoundError:
				var target *NotFoundError
				if !errors.As(err, &target) {
					t.Errorf("error type = %T, want *NotFoundError", err)
				}
			case *RateLimitError:
				var target *RateLimitError
				if !errors.As(err, &target) {
					t.Errorf("error type = %T, want *RateLimitError", err)
				}
			case *ServerError:
				var target *ServerError
				if !errors.As(err, &target) {
					t.Errorf("error type = %T, want *ServerError", err)
				}
			case *InvalidRequestError:
				var target *InvalidRequestError
				if !errors.As(err, &target) {
					t.Errorf("error type = %T, want *InvalidRequestError", err)
				}
			}
		})
	}
}

func TestOpenAIStreaming(t *testing.T) {
	sseData := strings.Join([]string{
		"event: response.created",
		`data: {"type":"response.created","response":{"id":"resp_stream","model":"gpt-5.2","status":"in_progress"}}`,
		"",
		"event: response.output_item.added",
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message","role":"assistant","content":[]}}`,
		"",
		"event: response.content_part.added",
		`data: {"type":"response.content_part.added","output_index":0,"content_index":0,"part":{"type":"output_text","text":""}}`,
		"",
		"event: response.output_text.delta",
		`data: {"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"Hello"}`,
		"",
		"event: response.output_text.delta",
		`data: {"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":" world"}`,
		"",
		"event: response.output_text.done",
		`data: {"type":"response.output_text.done","output_index":0,"content_index":0,"text":"Hello world"}`,
		"",
		"event: response.output_item.done",
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Hello world"}]}}`,
		"",
		"event: response.completed",
		`data: {"type":"response.completed","response":{"id":"resp_stream","model":"gpt-5.2","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Hello world"}]}],"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}}`,
		"",
	}, "\n")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify stream:true was sent
		body, _ := io.ReadAll(r.Body)
		var reqBody map[string]any
		_ = json.Unmarshal(body, &reqBody)
		if reqBody["stream"] != true {
			t.Error("stream should be true in request body")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseData))
	}))
	defer server.Close()

	adapter := NewOpenAIAdapter("sk-test", WithOpenAIBaseURL(server.URL))

	req := Request{
		Model:    "gpt-5.2",
		Messages: []Message{UserMessage("Hello")},
	}

	ch, err := adapter.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}

	var events []StreamEvent
	for evt := range ch {
		events = append(events, evt)
	}

	if len(events) == 0 {
		t.Fatal("received no events")
	}

	// Check that we get text delta events
	var textDeltas []string
	var gotTextStart, gotTextEnd, gotFinish bool
	for _, evt := range events {
		switch evt.Type {
		case StreamTextStart:
			gotTextStart = true
		case StreamTextDelta:
			textDeltas = append(textDeltas, evt.Delta)
		case StreamTextEnd:
			gotTextEnd = true
		case StreamFinish:
			gotFinish = true
			if evt.Usage == nil {
				t.Error("StreamFinish should have usage")
			} else {
				if evt.Usage.InputTokens != 10 {
					t.Errorf("usage.InputTokens = %d, want 10", evt.Usage.InputTokens)
				}
				if evt.Usage.OutputTokens != 5 {
					t.Errorf("usage.OutputTokens = %d, want 5", evt.Usage.OutputTokens)
				}
			}
		}
	}

	if !gotTextStart {
		t.Error("missing StreamTextStart event")
	}
	if !gotTextEnd {
		t.Error("missing StreamTextEnd event")
	}
	if !gotFinish {
		t.Error("missing StreamFinish event")
	}

	combinedText := strings.Join(textDeltas, "")
	if combinedText != "Hello world" {
		t.Errorf("combined text deltas = %q, want %q", combinedText, "Hello world")
	}
}

func TestOpenAIStreamingToolCalls(t *testing.T) {
	sseData := strings.Join([]string{
		"event: response.output_item.added",
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"call_abc","name":"get_weather","arguments":""}}`,
		"",
		"event: response.function_call_arguments.delta",
		`data: {"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"loc"}`,
		"",
		"event: response.function_call_arguments.delta",
		`data: {"type":"response.function_call_arguments.delta","output_index":0,"delta":"ation\":\"London\"}"}`,
		"",
		"event: response.function_call_arguments.done",
		`data: {"type":"response.function_call_arguments.done","output_index":0,"arguments":"{\"location\":\"London\"}"}`,
		"",
		"event: response.output_item.done",
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"call_abc","name":"get_weather","arguments":"{\"location\":\"London\"}"}}`,
		"",
		"event: response.completed",
		`data: {"type":"response.completed","response":{"id":"resp_tc","model":"gpt-5.2","status":"completed","output":[{"type":"function_call","id":"call_abc","name":"get_weather","arguments":"{\"location\":\"London\"}"}],"usage":{"input_tokens":20,"output_tokens":10,"total_tokens":30}}}`,
		"",
	}, "\n")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseData))
	}))
	defer server.Close()

	adapter := NewOpenAIAdapter("sk-test", WithOpenAIBaseURL(server.URL))

	req := Request{
		Model:    "gpt-5.2",
		Messages: []Message{UserMessage("Weather?")},
	}

	ch, err := adapter.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}

	var gotToolStart, gotToolEnd bool
	var toolDeltas []string
	for evt := range ch {
		switch evt.Type {
		case StreamToolStart:
			gotToolStart = true
			if evt.ToolCall == nil {
				t.Error("StreamToolStart should have ToolCall")
			} else {
				if evt.ToolCall.Name != "get_weather" {
					t.Errorf("tool name = %q, want %q", evt.ToolCall.Name, "get_weather")
				}
				if evt.ToolCall.ID != "call_abc" {
					t.Errorf("tool ID = %q, want %q", evt.ToolCall.ID, "call_abc")
				}
			}
		case StreamToolDelta:
			toolDeltas = append(toolDeltas, evt.Delta)
		case StreamToolEnd:
			gotToolEnd = true
		}
	}

	if !gotToolStart {
		t.Error("missing StreamToolStart event")
	}
	if !gotToolEnd {
		t.Error("missing StreamToolEnd event")
	}

	combinedArgs := strings.Join(toolDeltas, "")
	if combinedArgs != `{"location":"London"}` {
		t.Errorf("combined tool deltas = %q, want %q", combinedArgs, `{"location":"London"}`)
	}
}

func TestOpenAIReasoningEffort(t *testing.T) {
	var receivedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading body: %v", err)
			return
		}
		if err := json.Unmarshal(body, &receivedBody); err != nil {
			t.Errorf("unmarshalling body: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{
			"id": "resp_123",
			"model": "gpt-5.2",
			"status": "completed",
			"output": [
				{
					"type": "message",
					"role": "assistant",
					"content": [{"type": "output_text", "text": "OK"}]
				}
			],
			"usage": {"input_tokens": 10, "output_tokens": 5, "total_tokens": 15}
		}`
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	adapter := NewOpenAIAdapter("sk-test", WithOpenAIBaseURL(server.URL))

	req := Request{
		Model:           "gpt-5.2",
		Messages:        []Message{UserMessage("Think hard about this")},
		ReasoningEffort: "high",
	}

	_, err := adapter.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	reasoning, ok := receivedBody["reasoning"].(map[string]any)
	if !ok {
		t.Fatalf("reasoning is not a map: %T (%v)", receivedBody["reasoning"], receivedBody["reasoning"])
	}
	if reasoning["effort"] != "high" {
		t.Errorf("reasoning.effort = %v, want %q", reasoning["effort"], "high")
	}
}

func TestOpenAIReasoningEffortNotSetWhenEmpty(t *testing.T) {
	var receivedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading body: %v", err)
			return
		}
		if err := json.Unmarshal(body, &receivedBody); err != nil {
			t.Errorf("unmarshalling body: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{
			"id": "resp_123",
			"model": "gpt-5.2",
			"status": "completed",
			"output": [
				{
					"type": "message",
					"role": "assistant",
					"content": [{"type": "output_text", "text": "OK"}]
				}
			],
			"usage": {"input_tokens": 10, "output_tokens": 5, "total_tokens": 15}
		}`
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	adapter := NewOpenAIAdapter("sk-test", WithOpenAIBaseURL(server.URL))

	req := Request{
		Model:    "gpt-5.2",
		Messages: []Message{UserMessage("Hello")},
	}

	_, err := adapter.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	if _, exists := receivedBody["reasoning"]; exists {
		t.Error("reasoning should not be set when ReasoningEffort is empty")
	}
}

func TestOpenAIProviderOptions(t *testing.T) {
	var receivedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading body: %v", err)
			return
		}
		if err := json.Unmarshal(body, &receivedBody); err != nil {
			t.Errorf("unmarshalling body: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{
			"id": "resp_123",
			"model": "gpt-5.2",
			"status": "completed",
			"output": [
				{
					"type": "message",
					"role": "assistant",
					"content": [{"type": "output_text", "text": "OK"}]
				}
			],
			"usage": {"input_tokens": 10, "output_tokens": 5, "total_tokens": 15}
		}`
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	adapter := NewOpenAIAdapter("sk-test", WithOpenAIBaseURL(server.URL))

	req := Request{
		Model:    "gpt-5.2",
		Messages: []Message{UserMessage("Hello")},
		ProviderOptions: map[string]any{
			"openai": map[string]any{
				"store":                true,
				"previous_response_id": "resp_prev",
			},
		},
	}

	_, err := adapter.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	if receivedBody["store"] != true {
		t.Errorf("store = %v, want true", receivedBody["store"])
	}
	if receivedBody["previous_response_id"] != "resp_prev" {
		t.Errorf("previous_response_id = %v, want %q", receivedBody["previous_response_id"], "resp_prev")
	}
}

func TestOpenAIAuthorizationHeader(t *testing.T) {
	var receivedAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{
			"id": "resp_123",
			"model": "gpt-5.2",
			"status": "completed",
			"output": [
				{
					"type": "message",
					"role": "assistant",
					"content": [{"type": "output_text", "text": "OK"}]
				}
			],
			"usage": {"input_tokens": 10, "output_tokens": 5, "total_tokens": 15}
		}`
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	adapter := NewOpenAIAdapter("sk-my-secret-key", WithOpenAIBaseURL(server.URL))
	req := Request{
		Model:    "gpt-5.2",
		Messages: []Message{UserMessage("Hello")},
	}
	_, err := adapter.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	if receivedAuth != "Bearer sk-my-secret-key" {
		t.Errorf("Authorization = %q, want %q", receivedAuth, "Bearer sk-my-secret-key")
	}
}

func TestOpenAIOrganizationAndProjectHeaders(t *testing.T) {
	var receivedOrg, receivedProject string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedOrg = r.Header.Get("OpenAI-Organization")
		receivedProject = r.Header.Get("OpenAI-Project")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{
			"id": "resp_123",
			"model": "gpt-5.2",
			"status": "completed",
			"output": [
				{
					"type": "message",
					"role": "assistant",
					"content": [{"type": "output_text", "text": "OK"}]
				}
			],
			"usage": {"input_tokens": 10, "output_tokens": 5, "total_tokens": 15}
		}`
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	adapter := NewOpenAIAdapter("sk-test",
		WithOpenAIBaseURL(server.URL),
		WithOpenAIOrganization("org-abc123"),
		WithOpenAIProject("proj-xyz789"),
	)

	req := Request{
		Model:    "gpt-5.2",
		Messages: []Message{UserMessage("Hello")},
	}
	_, err := adapter.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	if receivedOrg != "org-abc123" {
		t.Errorf("OpenAI-Organization = %q, want %q", receivedOrg, "org-abc123")
	}
	if receivedProject != "proj-xyz789" {
		t.Errorf("OpenAI-Project = %q, want %q", receivedProject, "proj-xyz789")
	}
}

func TestOpenAIToolResultTranslation(t *testing.T) {
	var receivedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading body: %v", err)
			return
		}
		if err := json.Unmarshal(body, &receivedBody); err != nil {
			t.Errorf("unmarshalling body: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{
			"id": "resp_123",
			"model": "gpt-5.2",
			"status": "completed",
			"output": [
				{
					"type": "message",
					"role": "assistant",
					"content": [{"type": "output_text", "text": "The weather is sunny."}]
				}
			],
			"usage": {"input_tokens": 10, "output_tokens": 5, "total_tokens": 15}
		}`
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	adapter := NewOpenAIAdapter("sk-test", WithOpenAIBaseURL(server.URL))

	req := Request{
		Model: "gpt-5.2",
		Messages: []Message{
			UserMessage("What's the weather?"),
			{
				Role: RoleAssistant,
				Content: []ContentPart{
					ToolCallPart("call_123", "get_weather", json.RawMessage(`{"location":"London"}`)),
				},
			},
			ToolResultMessage("call_123", `{"temp":20,"condition":"sunny"}`, false),
		},
	}

	_, err := adapter.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	input, ok := receivedBody["input"].([]any)
	if !ok {
		t.Fatalf("input is not an array: %T", receivedBody["input"])
	}

	// Should have: user message, function_call, function_call_output
	if len(input) != 3 {
		t.Fatalf("input has %d items, want 3", len(input))
	}

	// Check tool call item
	tcItem := input[1].(map[string]any)
	if tcItem["type"] != "function_call" {
		t.Errorf("input[1].type = %v, want %q", tcItem["type"], "function_call")
	}
	if tcItem["id"] != "call_123" {
		t.Errorf("input[1].id = %v, want %q", tcItem["id"], "call_123")
	}
	if tcItem["name"] != "get_weather" {
		t.Errorf("input[1].name = %v, want %q", tcItem["name"], "get_weather")
	}

	// Check tool result item
	trItem := input[2].(map[string]any)
	if trItem["type"] != "function_call_output" {
		t.Errorf("input[2].type = %v, want %q", trItem["type"], "function_call_output")
	}
	if trItem["call_id"] != "call_123" {
		t.Errorf("input[2].call_id = %v, want %q", trItem["call_id"], "call_123")
	}
	if trItem["output"] != `{"temp":20,"condition":"sunny"}` {
		t.Errorf("input[2].output = %v, want JSON string", trItem["output"])
	}
}

func TestOpenAIImageURLTranslation(t *testing.T) {
	var receivedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading body: %v", err)
			return
		}
		if err := json.Unmarshal(body, &receivedBody); err != nil {
			t.Errorf("unmarshalling body: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{
			"id": "resp_123",
			"model": "gpt-5.2",
			"status": "completed",
			"output": [
				{
					"type": "message",
					"role": "assistant",
					"content": [{"type": "output_text", "text": "I see an image."}]
				}
			],
			"usage": {"input_tokens": 50, "output_tokens": 5, "total_tokens": 55}
		}`
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	adapter := NewOpenAIAdapter("sk-test", WithOpenAIBaseURL(server.URL))

	req := Request{
		Model: "gpt-5.2",
		Messages: []Message{
			UserMessageWithParts(
				TextPart("What's in this image?"),
				ImageURLPart("https://example.com/cat.jpg"),
			),
		},
	}

	_, err := adapter.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	input := receivedBody["input"].([]any)
	item0 := input[0].(map[string]any)
	content := item0["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("content has %d parts, want 2", len(content))
	}

	imgPart := content[1].(map[string]any)
	if imgPart["type"] != "input_image" {
		t.Errorf("image part type = %v, want %q", imgPart["type"], "input_image")
	}
	if imgPart["image_url"] != "https://example.com/cat.jpg" {
		t.Errorf("image_url = %v, want %q", imgPart["image_url"], "https://example.com/cat.jpg")
	}
}

func TestOpenAIImageDataTranslation(t *testing.T) {
	var receivedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading body: %v", err)
			return
		}
		if err := json.Unmarshal(body, &receivedBody); err != nil {
			t.Errorf("unmarshalling body: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{
			"id": "resp_123",
			"model": "gpt-5.2",
			"status": "completed",
			"output": [
				{
					"type": "message",
					"role": "assistant",
					"content": [{"type": "output_text", "text": "OK"}]
				}
			],
			"usage": {"input_tokens": 50, "output_tokens": 5, "total_tokens": 55}
		}`
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	adapter := NewOpenAIAdapter("sk-test", WithOpenAIBaseURL(server.URL))

	imgData := []byte{0x89, 0x50, 0x4e, 0x47} // PNG magic bytes
	req := Request{
		Model: "gpt-5.2",
		Messages: []Message{
			UserMessageWithParts(
				TextPart("What's this?"),
				ImageDataPart(imgData, "image/png"),
			),
		},
	}

	_, err := adapter.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	input := receivedBody["input"].([]any)
	item0 := input[0].(map[string]any)
	content := item0["content"].([]any)
	imgPart := content[1].(map[string]any)

	expectedURL := fmt.Sprintf("data:image/png;base64,%s", base64.StdEncoding.EncodeToString(imgData))
	if imgPart["image_url"] != expectedURL {
		t.Errorf("image_url = %v, want %q", imgPart["image_url"], expectedURL)
	}
}

func TestOpenAIClose(t *testing.T) {
	adapter := NewOpenAIAdapter("sk-test")
	if err := adapter.Close(); err != nil {
		t.Errorf("Close() error: %v", err)
	}
}

func TestOpenAIStopSequences(t *testing.T) {
	var receivedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading body: %v", err)
			return
		}
		if err := json.Unmarshal(body, &receivedBody); err != nil {
			t.Errorf("unmarshalling body: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{
			"id": "resp_123",
			"model": "gpt-5.2",
			"status": "completed",
			"output": [{"type": "message", "role": "assistant", "content": [{"type": "output_text", "text": "OK"}]}],
			"usage": {"input_tokens": 10, "output_tokens": 5, "total_tokens": 15}
		}`
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	adapter := NewOpenAIAdapter("sk-test", WithOpenAIBaseURL(server.URL))
	req := Request{
		Model:         "gpt-5.2",
		Messages:      []Message{UserMessage("Hello")},
		StopSequences: []string{"END", "STOP"},
	}

	_, err := adapter.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	stopSeqs, ok := receivedBody["stop"].([]any)
	if !ok {
		t.Fatalf("stop is not an array: %T", receivedBody["stop"])
	}
	if len(stopSeqs) != 2 {
		t.Fatalf("stop has %d items, want 2", len(stopSeqs))
	}
	if stopSeqs[0] != "END" || stopSeqs[1] != "STOP" {
		t.Errorf("stop = %v, want [END, STOP]", stopSeqs)
	}
}

func TestOpenAIStreamingErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Invalid API key","type":"invalid_api_key"}}`))
	}))
	defer server.Close()

	adapter := NewOpenAIAdapter("bad-key", WithOpenAIBaseURL(server.URL))

	req := Request{
		Model:    "gpt-5.2",
		Messages: []Message{UserMessage("Hello")},
	}

	_, err := adapter.Stream(context.Background(), req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var authErr *AuthenticationError
	if !errors.As(err, &authErr) {
		t.Errorf("error type = %T, want *AuthenticationError", err)
	}
}
