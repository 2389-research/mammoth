// ABOUTME: ProviderAdapter interface and base adapter utilities for the unified LLM client SDK.
// ABOUTME: Provides shared HTTP functionality, header parsing, message manipulation, and ID generation.

package llm

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ProviderAdapter is the interface that all LLM provider adapters must implement.
// It provides a uniform way to send completion and streaming requests to different
// LLM providers (OpenAI, Anthropic, Gemini, etc.).
type ProviderAdapter interface {
	Name() string
	Complete(ctx context.Context, req Request) (*Response, error)
	Stream(ctx context.Context, req Request) (<-chan StreamEvent, error)
	Close() error
}

// Initializer is an optional interface that adapters may implement to perform
// one-time setup (validating credentials, warming caches, etc.).
type Initializer interface {
	Initialize() error
}

// ToolChoiceChecker is an optional interface that adapters may implement to
// indicate which tool choice modes they support.
type ToolChoiceChecker interface {
	SupportsToolChoice(mode string) bool
}

// BaseAdapter provides common HTTP functionality shared across all provider adapters.
// Provider-specific adapters embed BaseAdapter to reuse request building, header management,
// and rate limit parsing.
type BaseAdapter struct {
	APIKey         string
	BaseURL        string
	DefaultHeaders map[string]string
	Timeout        AdapterTimeout
	HTTPClient     *http.Client
}

// NewBaseAdapter creates a BaseAdapter with the given API key, base URL, and timeout config.
// It initializes the HTTP client and default headers map.
func NewBaseAdapter(apiKey, baseURL string, timeout AdapterTimeout) *BaseAdapter {
	return &BaseAdapter{
		APIKey:         apiKey,
		BaseURL:        baseURL,
		DefaultHeaders: make(map[string]string),
		Timeout:        timeout,
		HTTPClient: &http.Client{
			Timeout: timeout.Request,
		},
	}
}

// DoRequest builds and executes an HTTP request against the provider's API.
// It JSON-encodes the body (if non-nil), sets authorization and content type headers,
// applies default headers, and then applies per-request header overrides.
// The request respects the provided context for timeout and cancellation.
func (b *BaseAdapter) DoRequest(ctx context.Context, method, path string, body any, headers map[string]string) (*http.Response, error) {
	url := b.BaseURL + path

	var reqBody *bytes.Buffer
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encoding request body: %w", err)
		}
		reqBody = bytes.NewBuffer(encoded)
	}

	var httpReq *http.Request
	var err error
	if reqBody != nil {
		httpReq, err = http.NewRequestWithContext(ctx, method, url, reqBody)
	} else {
		httpReq, err = http.NewRequestWithContext(ctx, method, url, nil)
	}
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	// Set authorization header
	if b.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+b.APIKey)
	}

	// Set content type for requests with a body
	if body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	// Apply default headers
	for k, v := range b.DefaultHeaders {
		httpReq.Header.Set(k, v)
	}

	// Apply per-request headers (overrides defaults)
	for k, v := range headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := b.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}

	return resp, nil
}

// ParseRateLimitHeaders extracts rate limit information from provider response headers.
// It parses the standard x-ratelimit-* headers and the retry-after header.
// Returns nil if no rate limit headers are present.
func (b *BaseAdapter) ParseRateLimitHeaders(headers http.Header) *RateLimitInfo {
	info := &RateLimitInfo{}
	found := false

	if v := headers.Get("x-ratelimit-remaining-requests"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			info.RequestsRemaining = &n
			found = true
		}
	}

	if v := headers.Get("x-ratelimit-limit-requests"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			info.RequestsLimit = &n
			found = true
		}
	}

	if v := headers.Get("x-ratelimit-remaining-tokens"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			info.TokensRemaining = &n
			found = true
		}
	}

	if v := headers.Get("x-ratelimit-limit-tokens"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			info.TokensLimit = &n
			found = true
		}
	}

	if v := headers.Get("retry-after"); v != "" {
		if seconds, err := strconv.Atoi(v); err == nil {
			resetAt := time.Now().Add(time.Duration(seconds) * time.Second)
			info.ResetAt = &resetAt
			found = true
		}
	}

	if !found {
		return nil
	}
	return info
}

// ExtractSystemMessages separates system and developer role messages from the rest.
// It concatenates the text content of all system/developer messages (joined by newlines)
// and returns them along with the remaining non-system messages.
func ExtractSystemMessages(messages []Message) (systemText string, remaining []Message) {
	var systemParts []string

	for _, msg := range messages {
		if msg.Role == RoleSystem || msg.Role == RoleDeveloper {
			text := msg.TextContent()
			if text != "" {
				systemParts = append(systemParts, text)
			}
		} else {
			remaining = append(remaining, msg)
		}
	}

	systemText = strings.Join(systemParts, "\n")
	return systemText, remaining
}

// MergeConsecutiveMessages combines consecutive messages with the same role by
// appending their content arrays. This is required for providers like Anthropic
// that enforce strict message role alternation.
func MergeConsecutiveMessages(messages []Message) []Message {
	if len(messages) == 0 {
		return nil
	}

	result := []Message{
		{
			Role:    messages[0].Role,
			Content: append([]ContentPart(nil), messages[0].Content...),
			Name:    messages[0].Name,
		},
	}

	for i := 1; i < len(messages); i++ {
		last := &result[len(result)-1]
		if messages[i].Role == last.Role {
			last.Content = append(last.Content, messages[i].Content...)
		} else {
			result = append(result, Message{
				Role:    messages[i].Role,
				Content: append([]ContentPart(nil), messages[i].Content...),
				Name:    messages[i].Name,
			})
		}
	}

	return result
}

// GenerateCallID produces a unique identifier for tool calls, prefixed with "call_".
// This is used for providers like Gemini that do not assign their own tool call IDs.
func GenerateCallID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("call_%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
