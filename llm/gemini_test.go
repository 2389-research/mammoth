// ABOUTME: Tests for the Gemini provider adapter using httptest servers for real HTTP interactions.
// ABOUTME: Validates request translation, response parsing, streaming, auth, tool calls, and error handling.

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

// TestGeminiAdapterName verifies that Name() returns "gemini".
func TestGeminiAdapterName(t *testing.T) {
	adapter := NewGeminiAdapter("test-api-key")
	if adapter.Name() != "gemini" {
		t.Errorf("Name() = %q, want %q", adapter.Name(), "gemini")
	}
}

// TestGeminiRequestTranslation verifies the request body sent to the Gemini API
// contains properly translated messages, generationConfig, and model path.
func TestGeminiRequestTranslation(t *testing.T) {
	var receivedBody map[string]any
	var receivedPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading body: %v", err)
		}
		if err := json.Unmarshal(body, &receivedBody); err != nil {
			t.Errorf("unmarshaling body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{
			"candidates": [{
				"content": {"parts": [{"text": "Hello back!"}], "role": "model"},
				"finishReason": "STOP"
			}],
			"usageMetadata": {"promptTokenCount": 10, "candidatesTokenCount": 5, "totalTokenCount": 15}
		}`
		fmt.Fprint(w, resp)
	}))
	defer server.Close()

	adapter := NewGeminiAdapter("test-key", WithGeminiBaseURL(server.URL))
	ctx := context.Background()

	_, err := adapter.Complete(ctx, Request{
		Model: "gemini-3-pro-preview",
		Messages: []Message{
			UserMessage("Hello"),
		},
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	if !strings.Contains(receivedPath, "gemini-3-pro-preview") {
		t.Errorf("path = %q, should contain model name", receivedPath)
	}
	if !strings.HasSuffix(receivedPath, ":generateContent") {
		t.Errorf("path = %q, should end with :generateContent", receivedPath)
	}

	contents, ok := receivedBody["contents"].([]any)
	if !ok {
		t.Fatalf("expected contents array, got %T", receivedBody["contents"])
	}
	if len(contents) != 1 {
		t.Fatalf("expected 1 content entry, got %d", len(contents))
	}

	content := contents[0].(map[string]any)
	if content["role"] != "user" {
		t.Errorf("role = %v, want user", content["role"])
	}
}

// TestGeminiSystemMessageExtraction verifies that system/developer messages are
// extracted into the systemInstruction field.
func TestGeminiSystemMessageExtraction(t *testing.T) {
	var receivedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedBody)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"candidates": [{
				"content": {"parts": [{"text": "ok"}], "role": "model"},
				"finishReason": "STOP"
			}],
			"usageMetadata": {"promptTokenCount": 5, "candidatesTokenCount": 2, "totalTokenCount": 7}
		}`)
	}))
	defer server.Close()

	adapter := NewGeminiAdapter("test-key", WithGeminiBaseURL(server.URL))
	ctx := context.Background()

	_, err := adapter.Complete(ctx, Request{
		Model: "gemini-3-pro-preview",
		Messages: []Message{
			SystemMessage("You are a helpful assistant."),
			DeveloperMessage("Be concise."),
			UserMessage("Hello"),
		},
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	sysInstr, ok := receivedBody["systemInstruction"].(map[string]any)
	if !ok {
		t.Fatalf("expected systemInstruction object, got %T: %v", receivedBody["systemInstruction"], receivedBody["systemInstruction"])
	}

	parts, ok := sysInstr["parts"].([]any)
	if !ok {
		t.Fatalf("expected parts array in systemInstruction, got %T", sysInstr["parts"])
	}
	if len(parts) != 1 {
		t.Fatalf("expected 1 part in systemInstruction, got %d", len(parts))
	}

	part := parts[0].(map[string]any)
	text := part["text"].(string)
	if !strings.Contains(text, "You are a helpful assistant.") {
		t.Errorf("systemInstruction text = %q, should contain system message", text)
	}
	if !strings.Contains(text, "Be concise.") {
		t.Errorf("systemInstruction text = %q, should contain developer message", text)
	}

	// Verify system messages are not in the contents array
	contents := receivedBody["contents"].([]any)
	for _, c := range contents {
		cm := c.(map[string]any)
		if cm["role"] == "system" || cm["role"] == "developer" {
			t.Errorf("system/developer messages should not appear in contents, found role=%v", cm["role"])
		}
	}
}

// TestGeminiAssistantRoleMapping verifies that assistant messages are mapped to
// the "model" role in Gemini's format.
func TestGeminiAssistantRoleMapping(t *testing.T) {
	var receivedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedBody)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"candidates": [{
				"content": {"parts": [{"text": "ok"}], "role": "model"},
				"finishReason": "STOP"
			}],
			"usageMetadata": {"promptTokenCount": 5, "candidatesTokenCount": 2, "totalTokenCount": 7}
		}`)
	}))
	defer server.Close()

	adapter := NewGeminiAdapter("test-key", WithGeminiBaseURL(server.URL))
	ctx := context.Background()

	_, err := adapter.Complete(ctx, Request{
		Model: "gemini-3-pro-preview",
		Messages: []Message{
			UserMessage("Hello"),
			AssistantMessage("Hi there!"),
			UserMessage("How are you?"),
		},
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	contents := receivedBody["contents"].([]any)
	if len(contents) != 3 {
		t.Fatalf("expected 3 contents, got %d", len(contents))
	}

	// Second message should have role "model" not "assistant"
	assistantContent := contents[1].(map[string]any)
	if assistantContent["role"] != "model" {
		t.Errorf("assistant role mapped to %q, want %q", assistantContent["role"], "model")
	}
}

// TestGeminiToolDefinitionTranslation verifies that tools are translated to
// Gemini's functionDeclarations format.
func TestGeminiToolDefinitionTranslation(t *testing.T) {
	var receivedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedBody)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"candidates": [{
				"content": {"parts": [{"text": "ok"}], "role": "model"},
				"finishReason": "STOP"
			}],
			"usageMetadata": {"promptTokenCount": 5, "candidatesTokenCount": 2, "totalTokenCount": 7}
		}`)
	}))
	defer server.Close()

	adapter := NewGeminiAdapter("test-key", WithGeminiBaseURL(server.URL))
	ctx := context.Background()

	_, err := adapter.Complete(ctx, Request{
		Model: "gemini-3-pro-preview",
		Messages: []Message{
			UserMessage("What's the weather?"),
		},
		Tools: []ToolDefinition{
			{
				Name:        "get_weather",
				Description: "Get the current weather",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"location":{"type":"string"}}}`),
			},
		},
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	tools, ok := receivedBody["tools"].([]any)
	if !ok {
		t.Fatalf("expected tools array, got %T", receivedBody["tools"])
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool group, got %d", len(tools))
	}

	toolGroup := tools[0].(map[string]any)
	funcDecls, ok := toolGroup["functionDeclarations"].([]any)
	if !ok {
		t.Fatalf("expected functionDeclarations array, got %T", toolGroup["functionDeclarations"])
	}
	if len(funcDecls) != 1 {
		t.Fatalf("expected 1 functionDeclaration, got %d", len(funcDecls))
	}

	decl := funcDecls[0].(map[string]any)
	if decl["name"] != "get_weather" {
		t.Errorf("function name = %v, want get_weather", decl["name"])
	}
	if decl["description"] != "Get the current weather" {
		t.Errorf("function description = %v, want 'Get the current weather'", decl["description"])
	}
	if decl["parameters"] == nil {
		t.Error("expected parameters to be present")
	}
}

// TestGeminiToolChoiceTranslation verifies that tool choice modes are translated
// to Gemini's tool_config with function_calling_config.
func TestGeminiToolChoiceTranslation(t *testing.T) {
	tests := []struct {
		name       string
		toolChoice *ToolChoice
		wantMode   string
		wantNames  []string
	}{
		{
			name:       "auto mode",
			toolChoice: &ToolChoice{Mode: ToolChoiceAuto},
			wantMode:   "AUTO",
		},
		{
			name:       "none mode",
			toolChoice: &ToolChoice{Mode: ToolChoiceNone},
			wantMode:   "NONE",
		},
		{
			name:       "required mode",
			toolChoice: &ToolChoice{Mode: ToolChoiceRequired},
			wantMode:   "ANY",
		},
		{
			name:       "named mode",
			toolChoice: &ToolChoice{Mode: ToolChoiceNamed, ToolName: "get_weather"},
			wantMode:   "ANY",
			wantNames:  []string{"get_weather"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var receivedBody map[string]any

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				json.Unmarshal(body, &receivedBody)
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{
					"candidates": [{
						"content": {"parts": [{"text": "ok"}], "role": "model"},
						"finishReason": "STOP"
					}],
					"usageMetadata": {"promptTokenCount": 5, "candidatesTokenCount": 2, "totalTokenCount": 7}
				}`)
			}))
			defer server.Close()

			adapter := NewGeminiAdapter("test-key", WithGeminiBaseURL(server.URL))
			ctx := context.Background()

			_, err := adapter.Complete(ctx, Request{
				Model: "gemini-3-pro-preview",
				Messages: []Message{
					UserMessage("test"),
				},
				Tools: []ToolDefinition{
					{Name: "get_weather", Description: "Get weather", Parameters: json.RawMessage(`{"type":"object"}`)},
				},
				ToolChoice: tt.toolChoice,
			})
			if err != nil {
				t.Fatalf("Complete() error: %v", err)
			}

			toolConfig, ok := receivedBody["tool_config"].(map[string]any)
			if !ok {
				t.Fatalf("expected tool_config object, got %T", receivedBody["tool_config"])
			}

			fcc, ok := toolConfig["function_calling_config"].(map[string]any)
			if !ok {
				t.Fatalf("expected function_calling_config, got %T", toolConfig["function_calling_config"])
			}

			if fcc["mode"] != tt.wantMode {
				t.Errorf("mode = %v, want %q", fcc["mode"], tt.wantMode)
			}

			if tt.wantNames != nil {
				names, ok := fcc["allowed_function_names"].([]any)
				if !ok {
					t.Fatalf("expected allowed_function_names array, got %T", fcc["allowed_function_names"])
				}
				if len(names) != len(tt.wantNames) {
					t.Fatalf("expected %d names, got %d", len(tt.wantNames), len(names))
				}
				for i, wantName := range tt.wantNames {
					if names[i] != wantName {
						t.Errorf("name[%d] = %v, want %q", i, names[i], wantName)
					}
				}
			}
		})
	}
}

// TestGeminiSyntheticToolCallIDs verifies that Gemini responses with functionCall
// parts get synthetic IDs assigned via GenerateCallID, and that the adapter maps
// those IDs to function names.
func TestGeminiSyntheticToolCallIDs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"candidates": [{
				"content": {
					"parts": [{"functionCall": {"name": "get_weather", "args": {"location": "NYC"}}}],
					"role": "model"
				},
				"finishReason": "STOP"
			}],
			"usageMetadata": {"promptTokenCount": 10, "candidatesTokenCount": 5, "totalTokenCount": 15}
		}`)
	}))
	defer server.Close()

	adapter := NewGeminiAdapter("test-key", WithGeminiBaseURL(server.URL))
	ctx := context.Background()

	resp, err := adapter.Complete(ctx, Request{
		Model:    "gemini-3-pro-preview",
		Messages: []Message{UserMessage("What's the weather in NYC?")},
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	toolCalls := resp.ToolCalls()
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(toolCalls))
	}

	tc := toolCalls[0]
	if tc.Name != "get_weather" {
		t.Errorf("tool call name = %q, want get_weather", tc.Name)
	}
	if !strings.HasPrefix(tc.ID, "call_") {
		t.Errorf("tool call ID = %q, should start with 'call_'", tc.ID)
	}

	// Verify the adapter tracks the mapping
	mappedName, ok := adapter.callIDToName[tc.ID]
	if !ok {
		t.Errorf("expected callIDToName to contain %q", tc.ID)
	}
	if mappedName != "get_weather" {
		t.Errorf("mapped name = %q, want get_weather", mappedName)
	}
}

// TestGeminiToolResultTranslation verifies that tool result messages are translated
// to Gemini's functionResponse format using the function name (not the synthetic ID).
func TestGeminiToolResultTranslation(t *testing.T) {
	var receivedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedBody)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"candidates": [{
				"content": {"parts": [{"text": "The weather is sunny"}], "role": "model"},
				"finishReason": "STOP"
			}],
			"usageMetadata": {"promptTokenCount": 15, "candidatesTokenCount": 5, "totalTokenCount": 20}
		}`)
	}))
	defer server.Close()

	adapter := NewGeminiAdapter("test-key", WithGeminiBaseURL(server.URL))

	// Pre-populate the callIDToName mapping as if a previous call returned a tool call
	syntheticID := "call_abc123"
	adapter.callIDToName[syntheticID] = "get_weather"

	ctx := context.Background()
	_, err := adapter.Complete(ctx, Request{
		Model: "gemini-3-pro-preview",
		Messages: []Message{
			UserMessage("What's the weather?"),
			{
				Role: RoleAssistant,
				Content: []ContentPart{
					ToolCallPart(syntheticID, "get_weather", json.RawMessage(`{"location":"NYC"}`)),
				},
			},
			ToolResultMessage(syntheticID, `{"temp": 72, "condition": "sunny"}`, false),
		},
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	contents := receivedBody["contents"].([]any)

	// Find the tool result message (should be from user role with functionResponse)
	var foundFuncResponse bool
	for _, c := range contents {
		cm := c.(map[string]any)
		parts := cm["parts"].([]any)
		for _, p := range parts {
			pm := p.(map[string]any)
			if fr, ok := pm["functionResponse"]; ok {
				foundFuncResponse = true
				frMap := fr.(map[string]any)
				if frMap["name"] != "get_weather" {
					t.Errorf("functionResponse name = %v, want get_weather", frMap["name"])
				}
			}
		}
	}

	if !foundFuncResponse {
		t.Error("expected to find functionResponse in request body")
	}
}

// TestGeminiResponseParsing verifies that Gemini responses are correctly parsed
// into the unified Response type.
func TestGeminiResponseParsing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"candidates": [{
				"content": {
					"parts": [{"text": "Hello! How can I help you?"}],
					"role": "model"
				},
				"finishReason": "STOP"
			}],
			"usageMetadata": {
				"promptTokenCount": 10,
				"candidatesTokenCount": 8,
				"totalTokenCount": 18,
				"thoughtsTokenCount": 5,
				"cachedContentTokenCount": 3
			},
			"modelVersion": "gemini-3-pro-preview"
		}`)
	}))
	defer server.Close()

	adapter := NewGeminiAdapter("test-key", WithGeminiBaseURL(server.URL))
	ctx := context.Background()

	resp, err := adapter.Complete(ctx, Request{
		Model:    "gemini-3-pro-preview",
		Messages: []Message{UserMessage("Hello")},
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	if resp.Provider != "gemini" {
		t.Errorf("Provider = %q, want gemini", resp.Provider)
	}
	if resp.Model != "gemini-3-pro-preview" {
		t.Errorf("Model = %q, want gemini-3-pro-preview", resp.Model)
	}
	if resp.TextContent() != "Hello! How can I help you?" {
		t.Errorf("TextContent() = %q, want 'Hello! How can I help you?'", resp.TextContent())
	}
	if resp.FinishReason.Reason != FinishStop {
		t.Errorf("FinishReason = %q, want stop", resp.FinishReason.Reason)
	}
	if resp.FinishReason.Raw != "STOP" {
		t.Errorf("FinishReason.Raw = %q, want STOP", resp.FinishReason.Raw)
	}
	if resp.Usage.InputTokens != 10 {
		t.Errorf("InputTokens = %d, want 10", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 8 {
		t.Errorf("OutputTokens = %d, want 8", resp.Usage.OutputTokens)
	}
	if resp.Usage.TotalTokens != 18 {
		t.Errorf("TotalTokens = %d, want 18", resp.Usage.TotalTokens)
	}
	if resp.Usage.ReasoningTokens == nil || *resp.Usage.ReasoningTokens != 5 {
		t.Errorf("ReasoningTokens = %v, want 5", resp.Usage.ReasoningTokens)
	}
	if resp.Usage.CacheReadTokens == nil || *resp.Usage.CacheReadTokens != 3 {
		t.Errorf("CacheReadTokens = %v, want 3", resp.Usage.CacheReadTokens)
	}
}

// TestGeminiFinishReasonInference verifies that when the response contains
// functionCall parts, the finish reason is inferred as "tool_calls".
func TestGeminiFinishReasonInference(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"candidates": [{
				"content": {
					"parts": [{"functionCall": {"name": "search", "args": {"q": "test"}}}],
					"role": "model"
				},
				"finishReason": "STOP"
			}],
			"usageMetadata": {"promptTokenCount": 5, "candidatesTokenCount": 5, "totalTokenCount": 10}
		}`)
	}))
	defer server.Close()

	adapter := NewGeminiAdapter("test-key", WithGeminiBaseURL(server.URL))
	ctx := context.Background()

	resp, err := adapter.Complete(ctx, Request{
		Model:    "gemini-3-pro-preview",
		Messages: []Message{UserMessage("search for test")},
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	// Even though Gemini says STOP, we should infer tool_calls since parts contain functionCall
	if resp.FinishReason.Reason != FinishToolCalls {
		t.Errorf("FinishReason = %q, want tool_calls (inferred from functionCall parts)", resp.FinishReason.Reason)
	}
}

// TestGeminiErrorHandling verifies that error responses from Gemini are parsed
// into the appropriate error types.
func TestGeminiErrorHandling(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantType   string
	}{
		{
			name:       "400 bad request",
			statusCode: http.StatusBadRequest,
			body:       `{"error":{"code":400,"message":"Invalid request","status":"INVALID_ARGUMENT"}}`,
			wantType:   "*llm.InvalidRequestError",
		},
		{
			name:       "401 unauthorized",
			statusCode: http.StatusUnauthorized,
			body:       `{"error":{"code":401,"message":"API key not valid","status":"UNAUTHENTICATED"}}`,
			wantType:   "*llm.AuthenticationError",
		},
		{
			name:       "403 forbidden",
			statusCode: http.StatusForbidden,
			body:       `{"error":{"code":403,"message":"Permission denied","status":"PERMISSION_DENIED"}}`,
			wantType:   "*llm.AccessDeniedError",
		},
		{
			name:       "404 not found",
			statusCode: http.StatusNotFound,
			body:       `{"error":{"code":404,"message":"Model not found","status":"NOT_FOUND"}}`,
			wantType:   "*llm.NotFoundError",
		},
		{
			name:       "429 rate limit",
			statusCode: http.StatusTooManyRequests,
			body:       `{"error":{"code":429,"message":"Quota exceeded","status":"RESOURCE_EXHAUSTED"}}`,
			wantType:   "*llm.RateLimitError",
		},
		{
			name:       "500 server error",
			statusCode: http.StatusInternalServerError,
			body:       `{"error":{"code":500,"message":"Internal error","status":"INTERNAL"}}`,
			wantType:   "*llm.ServerError",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				fmt.Fprint(w, tt.body)
			}))
			defer server.Close()

			adapter := NewGeminiAdapter("test-key", WithGeminiBaseURL(server.URL))
			ctx := context.Background()

			_, err := adapter.Complete(ctx, Request{
				Model:    "gemini-3-pro-preview",
				Messages: []Message{UserMessage("test")},
			})
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			gotType := fmt.Sprintf("%T", err)
			if gotType != tt.wantType {
				t.Errorf("error type = %q, want %q (error: %v)", gotType, tt.wantType, err)
			}
		})
	}
}

// TestGeminiStreaming verifies that the streaming endpoint is called correctly and
// SSE events are parsed into StreamEvents.
func TestGeminiStreaming(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify streaming endpoint
		if !strings.Contains(r.URL.Path, ":streamGenerateContent") {
			t.Errorf("streaming path = %q, should contain :streamGenerateContent", r.URL.Path)
		}
		if r.URL.Query().Get("alt") != "sse" {
			t.Errorf("alt param = %q, want sse", r.URL.Query().Get("alt"))
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// Write SSE events
		chunks := []string{
			`data: {"candidates":[{"content":{"parts":[{"text":"Hello"}],"role":"model"}}]}`,
			``,
			`data: {"candidates":[{"content":{"parts":[{"text":" world"}],"role":"model"}}]}`,
			``,
			`data: {"candidates":[{"content":{"parts":[{"text":"!"}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":3,"totalTokenCount":8}}`,
			``,
		}
		for _, chunk := range chunks {
			fmt.Fprintf(w, "%s\n", chunk)
		}
	}))
	defer server.Close()

	adapter := NewGeminiAdapter("test-key", WithGeminiBaseURL(server.URL))
	ctx := context.Background()

	ch, err := adapter.Stream(ctx, Request{
		Model:    "gemini-3-pro-preview",
		Messages: []Message{UserMessage("Hello")},
	})
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}

	var events []StreamEvent
	for evt := range ch {
		events = append(events, evt)
	}

	// Should have at least: text_start, text_delta(s), text_end, finish
	var hasTextStart, hasTextDelta, hasFinish bool
	var textContent string
	for _, evt := range events {
		switch evt.Type {
		case StreamTextStart:
			hasTextStart = true
		case StreamTextDelta:
			hasTextDelta = true
			textContent += evt.Delta
		case StreamFinish:
			hasFinish = true
			if evt.Usage == nil {
				t.Error("StreamFinish should have usage info")
			}
		}
	}

	if !hasTextStart {
		t.Error("expected StreamTextStart event")
	}
	if !hasTextDelta {
		t.Error("expected StreamTextDelta events")
	}
	if !hasFinish {
		t.Error("expected StreamFinish event")
	}
	if textContent != "Hello world!" {
		t.Errorf("streamed text = %q, want 'Hello world!'", textContent)
	}
}

// TestGeminiQueryParamAuth verifies that the API key is passed as a query parameter
// and NOT as a Bearer token in the Authorization header.
func TestGeminiQueryParamAuth(t *testing.T) {
	var receivedQuery string
	var receivedAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedQuery = r.URL.Query().Get("key")
		receivedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"candidates": [{
				"content": {"parts": [{"text": "ok"}], "role": "model"},
				"finishReason": "STOP"
			}],
			"usageMetadata": {"promptTokenCount": 5, "candidatesTokenCount": 2, "totalTokenCount": 7}
		}`)
	}))
	defer server.Close()

	adapter := NewGeminiAdapter("my-secret-api-key", WithGeminiBaseURL(server.URL))
	ctx := context.Background()

	_, err := adapter.Complete(ctx, Request{
		Model:    "gemini-3-pro-preview",
		Messages: []Message{UserMessage("test")},
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	if receivedQuery != "my-secret-api-key" {
		t.Errorf("query param key = %q, want my-secret-api-key", receivedQuery)
	}
	if receivedAuth != "" {
		t.Errorf("Authorization header = %q, should be empty (Gemini uses query param auth)", receivedAuth)
	}
}

// TestGeminiGenerationConfig verifies that temperature, topP, maxOutputTokens,
// and stopSequences are placed in the generationConfig field.
func TestGeminiGenerationConfig(t *testing.T) {
	var receivedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedBody)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"candidates": [{
				"content": {"parts": [{"text": "ok"}], "role": "model"},
				"finishReason": "STOP"
			}],
			"usageMetadata": {"promptTokenCount": 5, "candidatesTokenCount": 2, "totalTokenCount": 7}
		}`)
	}))
	defer server.Close()

	adapter := NewGeminiAdapter("test-key", WithGeminiBaseURL(server.URL))
	ctx := context.Background()

	temp := 0.7
	topP := 0.9
	maxTokens := 1024

	_, err := adapter.Complete(ctx, Request{
		Model:         "gemini-3-pro-preview",
		Messages:      []Message{UserMessage("test")},
		Temperature:   &temp,
		TopP:          &topP,
		MaxTokens:     &maxTokens,
		StopSequences: []string{"END", "STOP"},
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	genConfig, ok := receivedBody["generationConfig"].(map[string]any)
	if !ok {
		t.Fatalf("expected generationConfig object, got %T", receivedBody["generationConfig"])
	}

	if genConfig["temperature"] != 0.7 {
		t.Errorf("temperature = %v, want 0.7", genConfig["temperature"])
	}
	if genConfig["topP"] != 0.9 {
		t.Errorf("topP = %v, want 0.9", genConfig["topP"])
	}
	// JSON numbers are float64, maxOutputTokens is an int in request but float64 after JSON roundtrip
	if genConfig["maxOutputTokens"] != float64(1024) {
		t.Errorf("maxOutputTokens = %v, want 1024", genConfig["maxOutputTokens"])
	}

	stops, ok := genConfig["stopSequences"].([]any)
	if !ok {
		t.Fatalf("expected stopSequences array, got %T", genConfig["stopSequences"])
	}
	if len(stops) != 2 {
		t.Fatalf("expected 2 stop sequences, got %d", len(stops))
	}
	if stops[0] != "END" || stops[1] != "STOP" {
		t.Errorf("stopSequences = %v, want [END STOP]", stops)
	}
}

// TestGeminiClose verifies that Close() returns nil and is safe to call.
func TestGeminiClose(t *testing.T) {
	adapter := NewGeminiAdapter("test-key")
	if err := adapter.Close(); err != nil {
		t.Errorf("Close() error = %v, want nil", err)
	}
}

// TestGeminiFinishReasonMapping verifies the mapping of various Gemini finish reasons.
func TestGeminiFinishReasonMapping(t *testing.T) {
	tests := []struct {
		geminiReason string
		wantReason   string
	}{
		{"STOP", FinishStop},
		{"MAX_TOKENS", FinishLength},
		{"SAFETY", FinishContentFilter},
		{"OTHER", FinishOther},
		{"UNKNOWN_REASON", FinishOther},
	}

	for _, tt := range tests {
		t.Run(tt.geminiReason, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{
					"candidates": [{
						"content": {"parts": [{"text": "ok"}], "role": "model"},
						"finishReason": %q
					}],
					"usageMetadata": {"promptTokenCount": 5, "candidatesTokenCount": 2, "totalTokenCount": 7}
				}`, tt.geminiReason)
			}))
			defer server.Close()

			adapter := NewGeminiAdapter("test-key", WithGeminiBaseURL(server.URL))
			ctx := context.Background()

			resp, err := adapter.Complete(ctx, Request{
				Model:    "gemini-3-pro-preview",
				Messages: []Message{UserMessage("test")},
			})
			if err != nil {
				t.Fatalf("Complete() error: %v", err)
			}
			if resp.FinishReason.Reason != tt.wantReason {
				t.Errorf("FinishReason = %q, want %q", resp.FinishReason.Reason, tt.wantReason)
			}
			if resp.FinishReason.Raw != tt.geminiReason {
				t.Errorf("FinishReason.Raw = %q, want %q", resp.FinishReason.Raw, tt.geminiReason)
			}
		})
	}
}

// TestGeminiImageParts verifies that image content parts are translated correctly.
func TestGeminiImageParts(t *testing.T) {
	var receivedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedBody)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"candidates": [{
				"content": {"parts": [{"text": "I see an image"}], "role": "model"},
				"finishReason": "STOP"
			}],
			"usageMetadata": {"promptTokenCount": 50, "candidatesTokenCount": 5, "totalTokenCount": 55}
		}`)
	}))
	defer server.Close()

	adapter := NewGeminiAdapter("test-key", WithGeminiBaseURL(server.URL))
	ctx := context.Background()

	imgData := []byte("fake-image-data")

	_, err := adapter.Complete(ctx, Request{
		Model: "gemini-3-pro-preview",
		Messages: []Message{
			{
				Role: RoleUser,
				Content: []ContentPart{
					TextPart("What's in this image?"),
					ImageURLPart("https://example.com/image.png"),
					ImageDataPart(imgData, "image/png"),
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	contents := receivedBody["contents"].([]any)
	content := contents[0].(map[string]any)
	parts := content["parts"].([]any)

	if len(parts) != 3 {
		t.Fatalf("expected 3 parts, got %d", len(parts))
	}

	// First part should be text
	p0 := parts[0].(map[string]any)
	if _, ok := p0["text"]; !ok {
		t.Error("part 0 should be text")
	}

	// Second part should be fileData (URL-based image)
	p1 := parts[1].(map[string]any)
	fd, ok := p1["fileData"].(map[string]any)
	if !ok {
		t.Fatalf("part 1 should be fileData, got %v", p1)
	}
	if fd["fileUri"] != "https://example.com/image.png" {
		t.Errorf("fileUri = %v, want https://example.com/image.png", fd["fileUri"])
	}

	// Third part should be inlineData (base64 image)
	p2 := parts[2].(map[string]any)
	id, ok := p2["inlineData"].(map[string]any)
	if !ok {
		t.Fatalf("part 2 should be inlineData, got %v", p2)
	}
	if id["mimeType"] != "image/png" {
		t.Errorf("mimeType = %v, want image/png", id["mimeType"])
	}
	expectedB64 := base64.StdEncoding.EncodeToString(imgData)
	if id["data"] != expectedB64 {
		t.Errorf("data = %v, want %q", id["data"], expectedB64)
	}
}

// TestGeminiProviderOptions verifies that provider_options["gemini"] entries
// are merged into the request body.
func TestGeminiProviderOptions(t *testing.T) {
	var receivedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedBody)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"candidates": [{
				"content": {"parts": [{"text": "ok"}], "role": "model"},
				"finishReason": "STOP"
			}],
			"usageMetadata": {"promptTokenCount": 5, "candidatesTokenCount": 2, "totalTokenCount": 7}
		}`)
	}))
	defer server.Close()

	adapter := NewGeminiAdapter("test-key", WithGeminiBaseURL(server.URL))
	ctx := context.Background()

	_, err := adapter.Complete(ctx, Request{
		Model:    "gemini-3-pro-preview",
		Messages: []Message{UserMessage("test")},
		ProviderOptions: map[string]any{
			"gemini": map[string]any{
				"groundingConfig": map[string]any{
					"source": "google_search",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	gc, ok := receivedBody["groundingConfig"].(map[string]any)
	if !ok {
		t.Fatalf("expected groundingConfig in body, got %T", receivedBody["groundingConfig"])
	}
	if gc["source"] != "google_search" {
		t.Errorf("groundingConfig.source = %v, want google_search", gc["source"])
	}
}

// TestGeminiStreamingToolCalls verifies that streaming responses with functionCall
// parts emit StreamToolStart and StreamToolEnd events.
func TestGeminiStreamingToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		chunks := []string{
			`data: {"candidates":[{"content":{"parts":[{"functionCall":{"name":"get_weather","args":{"location":"NYC"}}}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"totalTokenCount":15}}`,
			``,
		}
		for _, chunk := range chunks {
			fmt.Fprintf(w, "%s\n", chunk)
		}
	}))
	defer server.Close()

	adapter := NewGeminiAdapter("test-key", WithGeminiBaseURL(server.URL))
	ctx := context.Background()

	ch, err := adapter.Stream(ctx, Request{
		Model:    "gemini-3-pro-preview",
		Messages: []Message{UserMessage("What's the weather?")},
	})
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}

	var events []StreamEvent
	for evt := range ch {
		events = append(events, evt)
	}

	var hasToolStart, hasToolEnd bool
	for _, evt := range events {
		switch evt.Type {
		case StreamToolStart:
			hasToolStart = true
			if evt.ToolCall == nil {
				t.Error("StreamToolStart should have ToolCall")
			} else if evt.ToolCall.Name != "get_weather" {
				t.Errorf("ToolCall.Name = %q, want get_weather", evt.ToolCall.Name)
			}
		case StreamToolEnd:
			hasToolEnd = true
		}
	}

	if !hasToolStart {
		t.Error("expected StreamToolStart event")
	}
	if !hasToolEnd {
		t.Error("expected StreamToolEnd event")
	}
}

// TestGeminiStreamingError verifies that streaming handles error responses.
func TestGeminiStreamingError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":{"code":400,"message":"Bad request","status":"INVALID_ARGUMENT"}}`)
	}))
	defer server.Close()

	adapter := NewGeminiAdapter("test-key", WithGeminiBaseURL(server.URL))
	ctx := context.Background()

	_, err := adapter.Stream(ctx, Request{
		Model:    "gemini-3-pro-preview",
		Messages: []Message{UserMessage("test")},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var invReq *InvalidRequestError
	if !errors.As(err, &invReq) {
		t.Errorf("expected InvalidRequestError, got %T: %v", err, err)
	}
}

// TestGeminiWithTimeout verifies that the timeout option is applied.
func TestGeminiWithTimeout(t *testing.T) {
	timeout := AdapterTimeout{
		Connect:    5e9,
		Request:    30e9,
		StreamRead: 10e9,
	}
	adapter := NewGeminiAdapter("test-key", WithGeminiTimeout(timeout))
	if adapter.base.Timeout != timeout {
		t.Errorf("timeout = %v, want %v", adapter.base.Timeout, timeout)
	}
}
