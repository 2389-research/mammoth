// ABOUTME: Anthropic provider adapter implementing the ProviderAdapter interface.
// ABOUTME: Translates unified LLM requests to/from the Anthropic Messages API (/v1/messages).

package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/2389-research/mammoth/llm/sse"
)

const (
	anthropicDefaultBaseURL = "https://api.anthropic.com"
	anthropicDefaultVersion = "2023-06-01"
	anthropicDefaultMaxToks = 4096
)

// AnthropicAdapter implements ProviderAdapter for the Anthropic Messages API.
type AnthropicAdapter struct {
	*BaseAdapter
	version string
}

// AnthropicOption is a functional option for configuring an AnthropicAdapter.
type AnthropicOption func(*AnthropicAdapter)

// WithAnthropicBaseURL overrides the default Anthropic API base URL.
func WithAnthropicBaseURL(url string) AnthropicOption {
	return func(a *AnthropicAdapter) {
		a.BaseURL = url
	}
}

// WithAnthropicTimeout sets custom timeout values for the adapter.
func WithAnthropicTimeout(timeout AdapterTimeout) AnthropicOption {
	return func(a *AnthropicAdapter) {
		a.Timeout = timeout
		a.HTTPClient = &http.Client{Timeout: timeout.Request}
	}
}

// WithAnthropicVersion sets the anthropic-version header value.
func WithAnthropicVersion(version string) AnthropicOption {
	return func(a *AnthropicAdapter) {
		a.version = version
	}
}

// NewAnthropicAdapter creates an AnthropicAdapter with the given API key and options.
// Authentication uses x-api-key header instead of Bearer token, so the API key
// is stored in DefaultHeaders rather than BaseAdapter.APIKey.
func NewAnthropicAdapter(apiKey string, opts ...AnthropicOption) *AnthropicAdapter {
	adapter := &AnthropicAdapter{
		BaseAdapter: NewBaseAdapter("", anthropicDefaultBaseURL, DefaultAdapterTimeout()),
		version:     anthropicDefaultVersion,
	}

	// Store API key in DefaultHeaders as x-api-key (not Bearer auth)
	adapter.DefaultHeaders["x-api-key"] = apiKey
	adapter.DefaultHeaders["anthropic-version"] = anthropicDefaultVersion

	for _, opt := range opts {
		opt(adapter)
	}

	// Update version header after options are applied
	adapter.DefaultHeaders["anthropic-version"] = adapter.version

	return adapter
}

// Name returns the provider name "anthropic".
func (a *AnthropicAdapter) Name() string {
	return "anthropic"
}

// Complete sends a synchronous completion request to the Anthropic Messages API.
func (a *AnthropicAdapter) Complete(ctx context.Context, req Request) (*Response, error) {
	body, headers := a.buildRequestBody(req, false)

	resp, err := a.DoRequest(ctx, http.MethodPost, "/v1/messages", body, headers)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, a.parseError(resp.StatusCode, respBody)
	}

	return a.parseResponse(respBody, resp.Header)
}

// Stream sends a streaming completion request to the Anthropic Messages API.
func (a *AnthropicAdapter) Stream(ctx context.Context, req Request) (<-chan StreamEvent, error) {
	body, headers := a.buildRequestBody(req, true)

	resp, err := a.DoRequest(ctx, http.MethodPost, "/v1/messages", body, headers)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, fmt.Errorf("reading error response body: %w", readErr)
		}
		return nil, a.parseError(resp.StatusCode, respBody)
	}

	ch := make(chan StreamEvent, 64)
	go a.processStream(ctx, resp.Body, ch)

	return ch, nil
}

// Close releases any resources held by the adapter.
func (a *AnthropicAdapter) Close() error {
	return nil
}

// buildRequestBody translates a unified Request into an Anthropic API request body.
func (a *AnthropicAdapter) buildRequestBody(req Request, stream bool) (map[string]any, map[string]string) {
	systemText, remaining := ExtractSystemMessages(req.Messages)
	merged := MergeConsecutiveMessages(remaining)

	body := map[string]any{
		"model": req.Model,
	}

	if systemText != "" {
		body["system"] = systemText
	}

	body["messages"] = a.translateMessages(merged)

	// max_tokens is required for Anthropic
	if req.MaxTokens != nil {
		body["max_tokens"] = *req.MaxTokens
	} else {
		body["max_tokens"] = anthropicDefaultMaxToks
	}

	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		body["top_p"] = *req.TopP
	}
	if len(req.StopSequences) > 0 {
		body["stop_sequences"] = req.StopSequences
	}

	if stream {
		body["stream"] = true
	}

	// Tool handling
	a.applyToolConfig(body, req)

	// Per-request headers
	headers := map[string]string{}

	// Provider options
	if req.ProviderOptions != nil {
		if anthropicOpts, ok := req.ProviderOptions["anthropic"]; ok {
			if optsMap, ok := anthropicOpts.(map[string]any); ok {
				// Extract beta header if present
				if beta, ok := optsMap["beta"]; ok {
					headers["anthropic-beta"] = fmt.Sprintf("%v", beta)
					delete(optsMap, "beta")
				}
				// Merge remaining options into body
				for k, v := range optsMap {
					body[k] = v
				}
			}
		}
	}

	return body, headers
}

// translateMessages converts unified messages to the Anthropic message format.
func (a *AnthropicAdapter) translateMessages(messages []Message) []map[string]any {
	result := make([]map[string]any, 0, len(messages))

	for _, msg := range messages {
		translated := a.translateMessage(msg)
		if translated != nil {
			result = append(result, translated)
		}
	}

	return result
}

// translateMessage converts a single unified message to an Anthropic message.
func (a *AnthropicAdapter) translateMessage(msg Message) map[string]any {
	switch msg.Role {
	case RoleTool:
		// Tool results become user messages with tool_result content blocks
		return a.translateToolResultMessage(msg)
	case RoleUser:
		return map[string]any{
			"role":    "user",
			"content": a.translateContentParts(msg.Content, "user"),
		}
	case RoleAssistant:
		return map[string]any{
			"role":    "assistant",
			"content": a.translateContentParts(msg.Content, "assistant"),
		}
	default:
		// System/Developer messages should already be extracted
		return nil
	}
}

// translateToolResultMessage translates a tool result message into an Anthropic
// user-role message with tool_result content blocks.
func (a *AnthropicAdapter) translateToolResultMessage(msg Message) map[string]any {
	content := make([]map[string]any, 0, len(msg.Content))

	for _, part := range msg.Content {
		if part.Kind == ContentToolResult && part.ToolResult != nil {
			block := map[string]any{
				"type":        "tool_result",
				"tool_use_id": part.ToolResult.ToolCallID,
				"content":     part.ToolResult.Content,
			}
			if part.ToolResult.IsError {
				block["is_error"] = true
			}
			content = append(content, block)
		}
	}

	return map[string]any{
		"role":    "user",
		"content": content,
	}
}

// translateContentParts converts unified content parts to Anthropic content blocks.
func (a *AnthropicAdapter) translateContentParts(parts []ContentPart, role string) []map[string]any {
	blocks := make([]map[string]any, 0, len(parts))

	for _, part := range parts {
		switch part.Kind {
		case ContentText:
			blocks = append(blocks, map[string]any{
				"type": "text",
				"text": part.Text,
			})

		case ContentImage:
			if part.Image != nil {
				blocks = append(blocks, a.translateImage(part.Image))
			}

		case ContentToolCall:
			if part.ToolCall != nil {
				block := map[string]any{
					"type": "tool_use",
					"id":   part.ToolCall.ID,
					"name": part.ToolCall.Name,
				}
				var input any
				if len(part.ToolCall.Arguments) > 0 {
					json.Unmarshal(part.ToolCall.Arguments, &input)
				}
				if input == nil {
					input = map[string]any{}
				}
				block["input"] = input
				blocks = append(blocks, block)
			}

		case ContentToolResult:
			if part.ToolResult != nil {
				block := map[string]any{
					"type":        "tool_result",
					"tool_use_id": part.ToolResult.ToolCallID,
					"content":     part.ToolResult.Content,
				}
				if part.ToolResult.IsError {
					block["is_error"] = true
				}
				blocks = append(blocks, block)
			}

		case ContentThinking:
			if part.Thinking != nil {
				block := map[string]any{
					"type":      "thinking",
					"thinking":  part.Thinking.Text,
					"signature": part.Thinking.Signature,
				}
				blocks = append(blocks, block)
			}

		case ContentRedactedThinking:
			if part.Thinking != nil {
				block := map[string]any{
					"type": "redacted_thinking",
					"data": part.Thinking.Text,
				}
				blocks = append(blocks, block)
			}
		}
	}

	return blocks
}

// translateImage converts unified ImageData to an Anthropic image content block.
func (a *AnthropicAdapter) translateImage(img *ImageData) map[string]any {
	if img.URL != "" {
		return map[string]any{
			"type": "image",
			"source": map[string]any{
				"type": "url",
				"url":  img.URL,
			},
		}
	}

	return map[string]any{
		"type": "image",
		"source": map[string]any{
			"type":       "base64",
			"media_type": img.MediaType,
			"data":       base64.StdEncoding.EncodeToString(img.Data),
		},
	}
}

// applyToolConfig adds tool definitions and tool_choice to the request body.
func (a *AnthropicAdapter) applyToolConfig(body map[string]any, req Request) {
	if len(req.Tools) == 0 {
		return
	}

	// Handle tool choice mode "none" - omit tools entirely
	if req.ToolChoice != nil && req.ToolChoice.Mode == ToolChoiceNone {
		return
	}

	// Translate tool definitions
	tools := make([]map[string]any, 0, len(req.Tools))
	for _, tool := range req.Tools {
		t := map[string]any{
			"name":        tool.Name,
			"description": tool.Description,
		}
		if len(tool.Parameters) > 0 {
			var schema any
			json.Unmarshal(tool.Parameters, &schema)
			t["input_schema"] = schema
		}
		tools = append(tools, t)
	}
	body["tools"] = tools

	// Translate tool choice
	if req.ToolChoice != nil {
		switch req.ToolChoice.Mode {
		case ToolChoiceAuto:
			body["tool_choice"] = map[string]any{"type": "auto"}
		case ToolChoiceRequired:
			body["tool_choice"] = map[string]any{"type": "any"}
		case ToolChoiceNamed:
			body["tool_choice"] = map[string]any{
				"type": "tool",
				"name": req.ToolChoice.ToolName,
			}
		}
	}
}

// anthropicResponse represents the raw Anthropic API response.
type anthropicResponse struct {
	ID         string                  `json:"id"`
	Type       string                  `json:"type"`
	Role       string                  `json:"role"`
	Model      string                  `json:"model"`
	Content    []anthropicContentBlock `json:"content"`
	StopReason string                  `json:"stop_reason"`
	Usage      anthropicUsage          `json:"usage"`
}

// anthropicContentBlock represents a content block in the Anthropic response.
type anthropicContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	Signature string          `json:"signature,omitempty"`
	Data      string          `json:"data,omitempty"`
}

// anthropicUsage represents token usage in the Anthropic response.
type anthropicUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}

// parseResponse parses the Anthropic API response into a unified Response.
func (a *AnthropicAdapter) parseResponse(body []byte, headers http.Header) (*Response, error) {
	var raw anthropicResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	resp := &Response{
		ID:       raw.ID,
		Model:    raw.Model,
		Provider: "anthropic",
		Message: Message{
			Role:    RoleAssistant,
			Content: a.parseContentBlocks(raw.Content),
		},
		FinishReason: a.mapStopReason(raw.StopReason),
		Usage: Usage{
			InputTokens:  raw.Usage.InputTokens,
			OutputTokens: raw.Usage.OutputTokens,
			TotalTokens:  raw.Usage.InputTokens + raw.Usage.OutputTokens,
		},
		RateLimit: a.ParseRateLimitHeaders(headers),
	}

	// Cache token accounting
	if raw.Usage.CacheCreationInputTokens > 0 {
		resp.Usage.CacheWriteTokens = IntPtr(raw.Usage.CacheCreationInputTokens)
	}
	if raw.Usage.CacheReadInputTokens > 0 {
		resp.Usage.CacheReadTokens = IntPtr(raw.Usage.CacheReadInputTokens)
	}

	// Store raw response
	rawJSON := json.RawMessage(body)
	resp.Raw = rawJSON

	return resp, nil
}

// parseContentBlocks converts Anthropic content blocks to unified ContentParts.
func (a *AnthropicAdapter) parseContentBlocks(blocks []anthropicContentBlock) []ContentPart {
	parts := make([]ContentPart, 0, len(blocks))

	for _, block := range blocks {
		switch block.Type {
		case "text":
			parts = append(parts, TextPart(block.Text))

		case "tool_use":
			parts = append(parts, ContentPart{
				Kind: ContentToolCall,
				ToolCall: &ToolCallData{
					ID:        block.ID,
					Name:      block.Name,
					Arguments: block.Input,
					Type:      "function",
				},
			})

		case "thinking":
			parts = append(parts, ThinkingPart(block.Thinking, block.Signature))

		case "redacted_thinking":
			parts = append(parts, ContentPart{
				Kind: ContentRedactedThinking,
				Thinking: &ThinkingData{
					Text:     block.Data,
					Redacted: true,
				},
			})
		}
	}

	return parts
}

// mapStopReason converts an Anthropic stop_reason to a unified FinishReason.
func (a *AnthropicAdapter) mapStopReason(reason string) FinishReason {
	var unified string
	switch reason {
	case "end_turn":
		unified = FinishStop
	case "max_tokens":
		unified = FinishLength
	case "tool_use":
		unified = FinishToolCalls
	case "stop_sequence":
		unified = FinishStop
	default:
		unified = FinishOther
	}

	return FinishReason{
		Reason: unified,
		Raw:    reason,
	}
}

// anthropicErrorResponse represents the Anthropic error response format.
type anthropicErrorResponse struct {
	Type  string `json:"type"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// parseError parses an Anthropic error response and returns the appropriate error type.
func (a *AnthropicAdapter) parseError(statusCode int, body []byte) error {
	var errResp anthropicErrorResponse
	if err := json.Unmarshal(body, &errResp); err != nil {
		// If we can't parse the error, use a generic message
		return ErrorFromStatusCode(statusCode, fmt.Sprintf("HTTP %d", statusCode), "anthropic", "", json.RawMessage(body), nil)
	}

	return ErrorFromStatusCode(
		statusCode,
		errResp.Error.Message,
		"anthropic",
		errResp.Error.Type,
		json.RawMessage(body),
		nil,
	)
}

// processStream reads SSE events from the response body and sends StreamEvents
// to the channel. It closes the channel and response body when done.
func (a *AnthropicAdapter) processStream(ctx context.Context, body io.ReadCloser, ch chan<- StreamEvent) {
	defer close(ch)
	defer body.Close()

	parser := sse.NewParser(body)

	// Track the types of content blocks by index for content_block_stop
	blockTypes := map[int]string{}

	for {
		select {
		case <-ctx.Done():
			ch <- StreamEvent{
				Type:  StreamErrorEvt,
				Error: ctx.Err(),
			}
			return
		default:
		}

		event, err := parser.Next()
		if err != nil {
			if err == io.EOF {
				return
			}
			ch <- StreamEvent{
				Type:  StreamErrorEvt,
				Error: err,
			}
			return
		}

		a.handleSSEEvent(event, ch, blockTypes)
	}
}

// handleSSEEvent processes a single SSE event and sends the appropriate
// StreamEvents to the channel.
func (a *AnthropicAdapter) handleSSEEvent(event sse.Event, ch chan<- StreamEvent, blockTypes map[int]string) {
	switch event.Type {
	case "message_start":
		var data struct {
			Message struct {
				ID    string         `json:"id"`
				Model string         `json:"model"`
				Usage anthropicUsage `json:"usage"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(event.Data), &data); err != nil {
			return
		}

		ch <- StreamEvent{
			Type: StreamStart,
			Usage: &Usage{
				InputTokens: data.Message.Usage.InputTokens,
			},
		}

	case "content_block_start":
		var data struct {
			Index        int `json:"index"`
			ContentBlock struct {
				Type string `json:"type"`
				ID   string `json:"id,omitempty"`
				Name string `json:"name,omitempty"`
				Text string `json:"text,omitempty"`
			} `json:"content_block"`
		}
		if err := json.Unmarshal([]byte(event.Data), &data); err != nil {
			return
		}

		blockTypes[data.Index] = data.ContentBlock.Type

		switch data.ContentBlock.Type {
		case "text":
			ch <- StreamEvent{
				Type: StreamTextStart,
			}
		case "tool_use":
			ch <- StreamEvent{
				Type: StreamToolStart,
				ToolCall: &ToolCall{
					ID:   data.ContentBlock.ID,
					Name: data.ContentBlock.Name,
				},
			}
		case "thinking":
			ch <- StreamEvent{
				Type: StreamReasonStart,
			}
		}

	case "content_block_delta":
		var data struct {
			Index int `json:"index"`
			Delta struct {
				Type        string `json:"type"`
				Text        string `json:"text,omitempty"`
				PartialJSON string `json:"partial_json,omitempty"`
				Thinking    string `json:"thinking,omitempty"`
			} `json:"delta"`
		}
		if err := json.Unmarshal([]byte(event.Data), &data); err != nil {
			return
		}

		switch data.Delta.Type {
		case "text_delta":
			ch <- StreamEvent{
				Type:  StreamTextDelta,
				Delta: data.Delta.Text,
			}
		case "input_json_delta":
			ch <- StreamEvent{
				Type:  StreamToolDelta,
				Delta: data.Delta.PartialJSON,
			}
		case "thinking_delta":
			ch <- StreamEvent{
				Type:           StreamReasonDelta,
				ReasoningDelta: data.Delta.Thinking,
			}
		}

	case "content_block_stop":
		var data struct {
			Index int `json:"index"`
		}
		if err := json.Unmarshal([]byte(event.Data), &data); err != nil {
			return
		}

		blockType := blockTypes[data.Index]
		switch blockType {
		case "text":
			ch <- StreamEvent{Type: StreamTextEnd}
		case "tool_use":
			ch <- StreamEvent{Type: StreamToolEnd}
		case "thinking":
			ch <- StreamEvent{Type: StreamReasonEnd}
		}

	case "message_delta":
		var data struct {
			Delta struct {
				StopReason string `json:"stop_reason"`
			} `json:"delta"`
			Usage struct {
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(event.Data), &data); err != nil {
			return
		}

		finishReason := a.mapStopReason(data.Delta.StopReason)
		ch <- StreamEvent{
			Type:         StreamFinish,
			FinishReason: &finishReason,
			Usage: &Usage{
				OutputTokens: data.Usage.OutputTokens,
			},
		}

	case "message_stop":
		// Stream is complete, no additional event needed since we already sent StreamFinish
	}
}
