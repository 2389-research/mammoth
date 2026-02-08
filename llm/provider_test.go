// ABOUTME: Tests for the ProviderAdapter interface and base adapter utilities.
// ABOUTME: Validates HTTP request building, header parsing, message manipulation, and ID generation.

package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewBaseAdapter(t *testing.T) {
	timeout := AdapterTimeout{
		Connect:    5 * time.Second,
		Request:    60 * time.Second,
		StreamRead: 15 * time.Second,
	}
	ba := NewBaseAdapter("sk-test-key", "https://api.example.com", timeout)

	if ba.APIKey != "sk-test-key" {
		t.Errorf("APIKey = %q, want %q", ba.APIKey, "sk-test-key")
	}
	if ba.BaseURL != "https://api.example.com" {
		t.Errorf("BaseURL = %q, want %q", ba.BaseURL, "https://api.example.com")
	}
	if ba.Timeout != timeout {
		t.Errorf("Timeout = %v, want %v", ba.Timeout, timeout)
	}
	if ba.HTTPClient == nil {
		t.Error("HTTPClient should not be nil")
	}
	if ba.DefaultHeaders == nil {
		t.Error("DefaultHeaders should not be nil")
	}
}

func TestNewBaseAdapterDefaultTimeout(t *testing.T) {
	ba := NewBaseAdapter("key", "https://api.example.com", AdapterTimeout{})

	if ba.HTTPClient == nil {
		t.Error("HTTPClient should not be nil")
	}
}

func TestBaseAdapterDoRequest(t *testing.T) {
	type reqBody struct {
		Model   string `json:"model"`
		Message string `json:"message"`
	}

	var receivedMethod string
	var receivedPath string
	var receivedBody []byte
	var receivedHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedPath = r.URL.Path
		receivedHeaders = r.Header
		var err error
		receivedBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	ba := NewBaseAdapter("sk-test-key-123", server.URL, DefaultAdapterTimeout())
	ba.DefaultHeaders["X-Custom-Default"] = "default-value"

	body := reqBody{Model: "gpt-4", Message: "hello"}
	perRequestHeaders := map[string]string{
		"X-Request-ID": "req-42",
	}

	resp, err := ba.DoRequest(context.Background(), http.MethodPost, "/v1/chat", body, perRequestHeaders)
	if err != nil {
		t.Fatalf("DoRequest error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if receivedMethod != http.MethodPost {
		t.Errorf("method = %q, want %q", receivedMethod, http.MethodPost)
	}
	if receivedPath != "/v1/chat" {
		t.Errorf("path = %q, want %q", receivedPath, "/v1/chat")
	}

	// Check JSON body was encoded correctly
	var decoded reqBody
	if err := json.Unmarshal(receivedBody, &decoded); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if decoded.Model != "gpt-4" || decoded.Message != "hello" {
		t.Errorf("body = %+v, want model=gpt-4, message=hello", decoded)
	}

	// Check Content-Type header
	if ct := receivedHeaders.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	// Check Authorization header
	if auth := receivedHeaders.Get("Authorization"); auth != "Bearer sk-test-key-123" {
		t.Errorf("Authorization = %q, want %q", auth, "Bearer sk-test-key-123")
	}

	// Check default headers are set
	if dh := receivedHeaders.Get("X-Custom-Default"); dh != "default-value" {
		t.Errorf("X-Custom-Default = %q, want %q", dh, "default-value")
	}

	// Check per-request headers are set
	if rh := receivedHeaders.Get("X-Request-ID"); rh != "req-42" {
		t.Errorf("X-Request-ID = %q, want %q", rh, "req-42")
	}
}

func TestBaseAdapterDoRequestPerRequestHeadersOverrideDefaults(t *testing.T) {
	var receivedHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ba := NewBaseAdapter("key", server.URL, DefaultAdapterTimeout())
	ba.DefaultHeaders["X-Version"] = "v1"

	resp, err := ba.DoRequest(context.Background(), http.MethodGet, "/test", nil, map[string]string{
		"X-Version": "v2-override",
	})
	if err != nil {
		t.Fatalf("DoRequest error: %v", err)
	}
	defer resp.Body.Close()

	if got := receivedHeaders.Get("X-Version"); got != "v2-override" {
		t.Errorf("X-Version = %q, want %q (per-request should override default)", got, "v2-override")
	}
}

func TestBaseAdapterDoRequestNilBody(t *testing.T) {
	var receivedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		receivedBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ba := NewBaseAdapter("key", server.URL, DefaultAdapterTimeout())
	resp, err := ba.DoRequest(context.Background(), http.MethodGet, "/test", nil, nil)
	if err != nil {
		t.Fatalf("DoRequest error: %v", err)
	}
	defer resp.Body.Close()

	if len(receivedBody) != 0 {
		t.Errorf("expected empty body for nil input, got %q", string(receivedBody))
	}
}

func TestBaseAdapterDoRequestContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ba := NewBaseAdapter("key", server.URL, DefaultAdapterTimeout())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := ba.DoRequest(ctx, http.MethodGet, "/slow", nil, nil)
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

func TestParseRateLimitHeadersFull(t *testing.T) {
	ba := NewBaseAdapter("key", "https://api.example.com", DefaultAdapterTimeout())

	headers := http.Header{}
	headers.Set("x-ratelimit-remaining-requests", "95")
	headers.Set("x-ratelimit-limit-requests", "100")
	headers.Set("x-ratelimit-remaining-tokens", "45000")
	headers.Set("x-ratelimit-limit-tokens", "50000")
	headers.Set("retry-after", "30")

	info := ba.ParseRateLimitHeaders(headers)

	if info == nil {
		t.Fatal("expected non-nil RateLimitInfo")
	}
	if info.RequestsRemaining == nil || *info.RequestsRemaining != 95 {
		t.Errorf("RequestsRemaining = %v, want 95", info.RequestsRemaining)
	}
	if info.RequestsLimit == nil || *info.RequestsLimit != 100 {
		t.Errorf("RequestsLimit = %v, want 100", info.RequestsLimit)
	}
	if info.TokensRemaining == nil || *info.TokensRemaining != 45000 {
		t.Errorf("TokensRemaining = %v, want 45000", info.TokensRemaining)
	}
	if info.TokensLimit == nil || *info.TokensLimit != 50000 {
		t.Errorf("TokensLimit = %v, want 50000", info.TokensLimit)
	}
	if info.ResetAt == nil {
		t.Fatal("expected non-nil ResetAt")
	}
}

func TestParseRateLimitHeadersPartial(t *testing.T) {
	ba := NewBaseAdapter("key", "https://api.example.com", DefaultAdapterTimeout())

	headers := http.Header{}
	headers.Set("x-ratelimit-remaining-requests", "10")

	info := ba.ParseRateLimitHeaders(headers)

	if info == nil {
		t.Fatal("expected non-nil RateLimitInfo")
	}
	if info.RequestsRemaining == nil || *info.RequestsRemaining != 10 {
		t.Errorf("RequestsRemaining = %v, want 10", info.RequestsRemaining)
	}
	if info.RequestsLimit != nil {
		t.Errorf("RequestsLimit should be nil, got %v", info.RequestsLimit)
	}
	if info.TokensRemaining != nil {
		t.Errorf("TokensRemaining should be nil, got %v", info.TokensRemaining)
	}
	if info.TokensLimit != nil {
		t.Errorf("TokensLimit should be nil, got %v", info.TokensLimit)
	}
	if info.ResetAt != nil {
		t.Errorf("ResetAt should be nil, got %v", info.ResetAt)
	}
}

func TestParseRateLimitHeadersEmpty(t *testing.T) {
	ba := NewBaseAdapter("key", "https://api.example.com", DefaultAdapterTimeout())

	info := ba.ParseRateLimitHeaders(http.Header{})

	if info != nil {
		t.Errorf("expected nil for empty headers, got %+v", info)
	}
}

func TestParseRateLimitHeadersInvalidValues(t *testing.T) {
	ba := NewBaseAdapter("key", "https://api.example.com", DefaultAdapterTimeout())

	headers := http.Header{}
	headers.Set("x-ratelimit-remaining-requests", "not-a-number")
	headers.Set("x-ratelimit-limit-tokens", "50000")

	info := ba.ParseRateLimitHeaders(headers)

	if info == nil {
		t.Fatal("expected non-nil RateLimitInfo (valid token header present)")
	}
	// Invalid header should be ignored
	if info.RequestsRemaining != nil {
		t.Errorf("RequestsRemaining should be nil for invalid value, got %v", *info.RequestsRemaining)
	}
	// Valid header should still be parsed
	if info.TokensLimit == nil || *info.TokensLimit != 50000 {
		t.Errorf("TokensLimit = %v, want 50000", info.TokensLimit)
	}
}

func TestExtractSystemMessages(t *testing.T) {
	messages := []Message{
		SystemMessage("You are a helpful assistant."),
		DeveloperMessage("Be concise."),
		UserMessage("Hello"),
		AssistantMessage("Hi there!"),
		SystemMessage("Additional instructions."),
		UserMessage("What is 2+2?"),
	}

	systemText, remaining := ExtractSystemMessages(messages)

	wantSystem := "You are a helpful assistant.\nBe concise.\nAdditional instructions."
	if systemText != wantSystem {
		t.Errorf("systemText = %q, want %q", systemText, wantSystem)
	}

	if len(remaining) != 3 {
		t.Fatalf("remaining has %d messages, want 3", len(remaining))
	}

	expectedRoles := []Role{RoleUser, RoleAssistant, RoleUser}
	for i, msg := range remaining {
		if msg.Role != expectedRoles[i] {
			t.Errorf("remaining[%d].Role = %q, want %q", i, msg.Role, expectedRoles[i])
		}
	}
}

func TestExtractSystemMessagesNoSystem(t *testing.T) {
	messages := []Message{
		UserMessage("Hello"),
		AssistantMessage("Hi"),
	}

	systemText, remaining := ExtractSystemMessages(messages)

	if systemText != "" {
		t.Errorf("systemText = %q, want empty", systemText)
	}
	if len(remaining) != 2 {
		t.Errorf("remaining has %d messages, want 2", len(remaining))
	}
}

func TestExtractSystemMessagesAllSystem(t *testing.T) {
	messages := []Message{
		SystemMessage("First"),
		DeveloperMessage("Second"),
	}

	systemText, remaining := ExtractSystemMessages(messages)

	if systemText != "First\nSecond" {
		t.Errorf("systemText = %q, want %q", systemText, "First\nSecond")
	}
	if len(remaining) != 0 {
		t.Errorf("remaining has %d messages, want 0", len(remaining))
	}
}

func TestExtractSystemMessagesEmpty(t *testing.T) {
	systemText, remaining := ExtractSystemMessages(nil)

	if systemText != "" {
		t.Errorf("systemText = %q, want empty", systemText)
	}
	if len(remaining) != 0 {
		t.Errorf("remaining has %d messages, want 0", len(remaining))
	}
}

func TestMergeConsecutiveMessagesBasic(t *testing.T) {
	messages := []Message{
		UserMessage("Hello"),
		UserMessage("How are you?"),
		AssistantMessage("I'm fine."),
		AssistantMessage("Thanks for asking!"),
		UserMessage("Great"),
	}

	merged := MergeConsecutiveMessages(messages)

	if len(merged) != 3 {
		t.Fatalf("merged has %d messages, want 3", len(merged))
	}

	// First merged user message should have 2 content parts
	if merged[0].Role != RoleUser {
		t.Errorf("merged[0].Role = %q, want %q", merged[0].Role, RoleUser)
	}
	if len(merged[0].Content) != 2 {
		t.Errorf("merged[0] has %d parts, want 2", len(merged[0].Content))
	}
	if merged[0].Content[0].Text != "Hello" {
		t.Errorf("merged[0].Content[0].Text = %q, want %q", merged[0].Content[0].Text, "Hello")
	}
	if merged[0].Content[1].Text != "How are you?" {
		t.Errorf("merged[0].Content[1].Text = %q, want %q", merged[0].Content[1].Text, "How are you?")
	}

	// Merged assistant message should have 2 content parts
	if merged[1].Role != RoleAssistant {
		t.Errorf("merged[1].Role = %q, want %q", merged[1].Role, RoleAssistant)
	}
	if len(merged[1].Content) != 2 {
		t.Errorf("merged[1] has %d parts, want 2", len(merged[1].Content))
	}

	// Last user message should be unchanged
	if merged[2].Role != RoleUser {
		t.Errorf("merged[2].Role = %q, want %q", merged[2].Role, RoleUser)
	}
	if len(merged[2].Content) != 1 {
		t.Errorf("merged[2] has %d parts, want 1", len(merged[2].Content))
	}
}

func TestMergeConsecutiveMessagesAlreadyAlternating(t *testing.T) {
	messages := []Message{
		UserMessage("Hello"),
		AssistantMessage("Hi"),
		UserMessage("Bye"),
	}

	merged := MergeConsecutiveMessages(messages)

	if len(merged) != 3 {
		t.Fatalf("merged has %d messages, want 3 (no-op)", len(merged))
	}

	for i, msg := range messages {
		if merged[i].Role != msg.Role {
			t.Errorf("merged[%d].Role = %q, want %q", i, merged[i].Role, msg.Role)
		}
		if len(merged[i].Content) != len(msg.Content) {
			t.Errorf("merged[%d] content length changed", i)
		}
	}
}

func TestMergeConsecutiveMessagesEmpty(t *testing.T) {
	merged := MergeConsecutiveMessages(nil)
	if len(merged) != 0 {
		t.Errorf("merged has %d messages, want 0", len(merged))
	}
}

func TestMergeConsecutiveMessagesSingle(t *testing.T) {
	messages := []Message{
		UserMessage("Hello"),
	}

	merged := MergeConsecutiveMessages(messages)

	if len(merged) != 1 {
		t.Fatalf("merged has %d messages, want 1", len(merged))
	}
	if merged[0].TextContent() != "Hello" {
		t.Errorf("text = %q, want %q", merged[0].TextContent(), "Hello")
	}
}

func TestMergeConsecutiveMessagesMultipleConsecutive(t *testing.T) {
	messages := []Message{
		UserMessage("A"),
		UserMessage("B"),
		UserMessage("C"),
	}

	merged := MergeConsecutiveMessages(messages)

	if len(merged) != 1 {
		t.Fatalf("merged has %d messages, want 1", len(merged))
	}
	if len(merged[0].Content) != 3 {
		t.Errorf("merged[0] has %d parts, want 3", len(merged[0].Content))
	}
	if merged[0].Content[0].Text != "A" {
		t.Errorf("part 0 text = %q, want %q", merged[0].Content[0].Text, "A")
	}
	if merged[0].Content[1].Text != "B" {
		t.Errorf("part 1 text = %q, want %q", merged[0].Content[1].Text, "B")
	}
	if merged[0].Content[2].Text != "C" {
		t.Errorf("part 2 text = %q, want %q", merged[0].Content[2].Text, "C")
	}
}

func TestMergeConsecutiveMessagesPreservesMultiPartContent(t *testing.T) {
	msg1 := UserMessageWithParts(
		TextPart("Look at this"),
		ImageURLPart("https://example.com/img.png"),
	)
	msg2 := UserMessage("What do you think?")

	merged := MergeConsecutiveMessages([]Message{msg1, msg2})

	if len(merged) != 1 {
		t.Fatalf("merged has %d messages, want 1", len(merged))
	}
	if len(merged[0].Content) != 3 {
		t.Errorf("merged[0] has %d parts, want 3", len(merged[0].Content))
	}
	if merged[0].Content[0].Kind != ContentText {
		t.Errorf("part 0 kind = %q, want text", merged[0].Content[0].Kind)
	}
	if merged[0].Content[1].Kind != ContentImage {
		t.Errorf("part 1 kind = %q, want image", merged[0].Content[1].Kind)
	}
	if merged[0].Content[2].Kind != ContentText {
		t.Errorf("part 2 kind = %q, want text", merged[0].Content[2].Kind)
	}
}

func TestGenerateCallID(t *testing.T) {
	id := GenerateCallID()

	if !strings.HasPrefix(id, "call_") {
		t.Errorf("GenerateCallID() = %q, should start with %q", id, "call_")
	}

	// Should be "call_" + UUID, so at least 10 chars
	if len(id) < 10 {
		t.Errorf("GenerateCallID() = %q, too short (len=%d)", id, len(id))
	}

	// Generate multiple and check uniqueness
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		newID := GenerateCallID()
		if ids[newID] {
			t.Errorf("GenerateCallID() produced duplicate: %q", newID)
		}
		ids[newID] = true
	}
}

func TestGenerateCallIDFormat(t *testing.T) {
	id := GenerateCallID()

	// Should only contain "call_" prefix followed by hex chars and dashes
	suffix := strings.TrimPrefix(id, "call_")
	for _, c := range suffix {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || c == '-') {
			t.Errorf("GenerateCallID() suffix contains unexpected char %q in %q", string(c), id)
		}
	}
}

func TestParseRateLimitHeadersRetryAfterSeconds(t *testing.T) {
	ba := NewBaseAdapter("key", "https://api.example.com", DefaultAdapterTimeout())

	headers := http.Header{}
	headers.Set("retry-after", "60")

	info := ba.ParseRateLimitHeaders(headers)
	if info == nil {
		t.Fatal("expected non-nil RateLimitInfo")
	}
	if info.ResetAt == nil {
		t.Fatal("expected non-nil ResetAt")
	}

	// ResetAt should be approximately now + 60 seconds
	expectedMin := time.Now().Add(59 * time.Second)
	expectedMax := time.Now().Add(61 * time.Second)
	if info.ResetAt.Before(expectedMin) || info.ResetAt.After(expectedMax) {
		t.Errorf("ResetAt = %v, expected between %v and %v", info.ResetAt, expectedMin, expectedMax)
	}
}

func TestBaseAdapterDoRequestResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"success","count":42}`))
	}))
	defer server.Close()

	ba := NewBaseAdapter("key", server.URL, DefaultAdapterTimeout())
	resp, err := ba.DoRequest(context.Background(), http.MethodGet, "/test", nil, nil)
	if err != nil {
		t.Fatalf("DoRequest error: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading response body: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if result["status"] != "success" {
		t.Errorf("status = %v, want %q", result["status"], "success")
	}
}
