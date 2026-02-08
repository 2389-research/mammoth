// ABOUTME: Core data model types for the unified LLM client SDK.
// ABOUTME: Defines Message, ContentPart, Request, Response, and all supporting types.

package llm

import (
	"encoding/json"
	"time"
)

// Role represents who produced a message in a conversation.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
	RoleDeveloper Role = "developer"
)

// ContentKind discriminates the type of content in a ContentPart.
type ContentKind string

const (
	ContentText             ContentKind = "text"
	ContentImage            ContentKind = "image"
	ContentAudio            ContentKind = "audio"
	ContentDocument         ContentKind = "document"
	ContentToolCall         ContentKind = "tool_call"
	ContentToolResult       ContentKind = "tool_result"
	ContentThinking         ContentKind = "thinking"
	ContentRedactedThinking ContentKind = "redacted_thinking"
)

// ImageData holds image content as URL, raw bytes, or both.
type ImageData struct {
	URL       string `json:"url,omitempty"`
	Data      []byte `json:"data,omitempty"`
	MediaType string `json:"media_type,omitempty"`
	Detail    string `json:"detail,omitempty"` // "auto", "low", "high"
}

// AudioData holds audio content.
type AudioData struct {
	URL       string `json:"url,omitempty"`
	Data      []byte `json:"data,omitempty"`
	MediaType string `json:"media_type,omitempty"`
}

// DocumentData holds document content (PDF, etc.).
type DocumentData struct {
	URL       string `json:"url,omitempty"`
	Data      []byte `json:"data,omitempty"`
	MediaType string `json:"media_type,omitempty"`
	FileName  string `json:"file_name,omitempty"`
}

// ToolCallData represents a model-initiated tool invocation.
type ToolCallData struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
	Type      string          `json:"type,omitempty"` // "function" (default) or "custom"
}

// ArgumentsMap parses the raw JSON arguments into a map.
func (tc *ToolCallData) ArgumentsMap() (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal(tc.Arguments, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// ToolResultData represents the result of executing a tool call.
type ToolResultData struct {
	ToolCallID     string `json:"tool_call_id"`
	Content        string `json:"content"`
	IsError        bool   `json:"is_error"`
	ImageData      []byte `json:"image_data,omitempty"`
	ImageMediaType string `json:"image_media_type,omitempty"`
}

// ThinkingData holds model reasoning/thinking content.
type ThinkingData struct {
	Text      string `json:"text"`
	Signature string `json:"signature,omitempty"`
	Redacted  bool   `json:"redacted"`
}

// ContentPart is a single piece of content within a message.
// It uses a tagged-union pattern: the Kind field determines which data field is populated.
type ContentPart struct {
	Kind       ContentKind   `json:"kind"`
	Text       string        `json:"text,omitempty"`
	Image      *ImageData    `json:"image,omitempty"`
	Audio      *AudioData    `json:"audio,omitempty"`
	Document   *DocumentData `json:"document,omitempty"`
	ToolCall   *ToolCallData `json:"tool_call,omitempty"`
	ToolResult *ToolResultData `json:"tool_result,omitempty"`
	Thinking   *ThinkingData `json:"thinking,omitempty"`
}

// TextPart creates a text ContentPart.
func TextPart(text string) ContentPart {
	return ContentPart{Kind: ContentText, Text: text}
}

// ImageURLPart creates an image ContentPart from a URL.
func ImageURLPart(url string) ContentPart {
	return ContentPart{Kind: ContentImage, Image: &ImageData{URL: url}}
}

// ImageDataPart creates an image ContentPart from raw bytes.
func ImageDataPart(data []byte, mediaType string) ContentPart {
	return ContentPart{Kind: ContentImage, Image: &ImageData{Data: data, MediaType: mediaType}}
}

// ToolCallPart creates a tool call ContentPart.
func ToolCallPart(id, name string, args json.RawMessage) ContentPart {
	return ContentPart{
		Kind: ContentToolCall,
		ToolCall: &ToolCallData{
			ID:        id,
			Name:      name,
			Arguments: args,
			Type:      "function",
		},
	}
}

// ToolResultPart creates a tool result ContentPart.
func ToolResultPart(toolCallID, content string, isError bool) ContentPart {
	return ContentPart{
		Kind: ContentToolResult,
		ToolResult: &ToolResultData{
			ToolCallID: toolCallID,
			Content:    content,
			IsError:    isError,
		},
	}
}

// ThinkingPart creates a thinking ContentPart.
func ThinkingPart(text, signature string) ContentPart {
	return ContentPart{
		Kind: ContentThinking,
		Thinking: &ThinkingData{
			Text:      text,
			Signature: signature,
		},
	}
}

// RedactedThinkingPart creates a redacted thinking ContentPart.
func RedactedThinkingPart(text, signature string) ContentPart {
	return ContentPart{
		Kind: ContentRedactedThinking,
		Thinking: &ThinkingData{
			Text:      text,
			Signature: signature,
			Redacted:  true,
		},
	}
}

// Message is the fundamental unit of conversation.
type Message struct {
	Role       Role          `json:"role"`
	Content    []ContentPart `json:"content"`
	Name       string        `json:"name,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
}

// TextContent returns concatenated text from all text content parts.
func (m *Message) TextContent() string {
	var result string
	for _, part := range m.Content {
		if part.Kind == ContentText {
			result += part.Text
		}
	}
	return result
}

// ToolCalls extracts all tool call data from the message.
func (m *Message) ToolCalls() []ToolCallData {
	var calls []ToolCallData
	for _, part := range m.Content {
		if part.Kind == ContentToolCall && part.ToolCall != nil {
			calls = append(calls, *part.ToolCall)
		}
	}
	return calls
}

// ReasoningContent returns concatenated text from all thinking content parts.
func (m *Message) ReasoningContent() string {
	var result string
	for _, part := range m.Content {
		if part.Kind == ContentThinking && part.Thinking != nil {
			result += part.Thinking.Text
		}
	}
	return result
}

// Convenience constructors for common message types.

// SystemMessage creates a system role message.
func SystemMessage(text string) Message {
	return Message{Role: RoleSystem, Content: []ContentPart{TextPart(text)}}
}

// UserMessage creates a user role message with text.
func UserMessage(text string) Message {
	return Message{Role: RoleUser, Content: []ContentPart{TextPart(text)}}
}

// UserMessageWithParts creates a user role message with multiple content parts.
func UserMessageWithParts(parts ...ContentPart) Message {
	return Message{Role: RoleUser, Content: parts}
}

// AssistantMessage creates an assistant role message with text.
func AssistantMessage(text string) Message {
	return Message{Role: RoleAssistant, Content: []ContentPart{TextPart(text)}}
}

// ToolResultMessage creates a tool role message with a result.
func ToolResultMessage(toolCallID, content string, isError bool) Message {
	return Message{
		Role:       RoleTool,
		Content:    []ContentPart{ToolResultPart(toolCallID, content, isError)},
		ToolCallID: toolCallID,
	}
}

// DeveloperMessage creates a developer role message.
func DeveloperMessage(text string) Message {
	return Message{Role: RoleDeveloper, Content: []ContentPart{TextPart(text)}}
}

// FinishReason indicates why generation stopped, with both unified and raw values.
type FinishReason struct {
	Reason string `json:"reason"` // unified: stop, length, tool_calls, content_filter, error, other
	Raw    string `json:"raw,omitempty"`
}

const (
	FinishStop          = "stop"
	FinishLength        = "length"
	FinishToolCalls     = "tool_calls"
	FinishContentFilter = "content_filter"
	FinishError         = "error"
	FinishOther         = "other"
)

// Usage tracks token consumption for a single LLM call.
type Usage struct {
	InputTokens      int              `json:"input_tokens"`
	OutputTokens     int              `json:"output_tokens"`
	TotalTokens      int              `json:"total_tokens"`
	ReasoningTokens  *int             `json:"reasoning_tokens,omitempty"`
	CacheReadTokens  *int             `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens *int             `json:"cache_write_tokens,omitempty"`
	Raw              *json.RawMessage `json:"raw,omitempty"`
}

// Add combines two Usage values, summing all fields.
func (u Usage) Add(other Usage) Usage {
	result := Usage{
		InputTokens:  u.InputTokens + other.InputTokens,
		OutputTokens: u.OutputTokens + other.OutputTokens,
		TotalTokens:  u.TotalTokens + other.TotalTokens,
	}
	result.ReasoningTokens = addOptionalInt(u.ReasoningTokens, other.ReasoningTokens)
	result.CacheReadTokens = addOptionalInt(u.CacheReadTokens, other.CacheReadTokens)
	result.CacheWriteTokens = addOptionalInt(u.CacheWriteTokens, other.CacheWriteTokens)
	return result
}

func addOptionalInt(a, b *int) *int {
	if a == nil && b == nil {
		return nil
	}
	val := 0
	if a != nil {
		val += *a
	}
	if b != nil {
		val += *b
	}
	return &val
}

// IntPtr returns a pointer to an int value.
func IntPtr(v int) *int {
	return &v
}

// Warning represents a non-fatal issue in a response.
type Warning struct {
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

// RateLimitInfo contains rate limit metadata from provider response headers.
type RateLimitInfo struct {
	RequestsRemaining *int       `json:"requests_remaining,omitempty"`
	RequestsLimit     *int       `json:"requests_limit,omitempty"`
	TokensRemaining   *int       `json:"tokens_remaining,omitempty"`
	TokensLimit       *int       `json:"tokens_limit,omitempty"`
	ResetAt           *time.Time `json:"reset_at,omitempty"`
}

// ResponseFormat specifies the desired output format.
type ResponseFormat struct {
	Type       string          `json:"type"` // "text", "json", or "json_schema"
	JSONSchema json.RawMessage `json:"json_schema,omitempty"`
	Strict     bool            `json:"strict,omitempty"`
}

// ToolChoice controls whether and how the model uses tools.
type ToolChoice struct {
	Mode     string `json:"mode"`               // "auto", "none", "required", "named"
	ToolName string `json:"tool_name,omitempty"` // required when mode is "named"
}

// ToolChoice mode constants.
const (
	ToolChoiceAuto     = "auto"
	ToolChoiceNone     = "none"
	ToolChoiceRequired = "required"
	ToolChoiceNamed    = "named"
)

// ToolDefinition describes a tool available to the model.
type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"` // JSON Schema with root "type": "object"
}

// Tool is a ToolDefinition with an optional execute handler.
type Tool struct {
	ToolDefinition
	Execute func(args json.RawMessage) (string, error) `json:"-"`
}

// IsActive returns true if the tool has an execute handler.
func (t *Tool) IsActive() bool {
	return t.Execute != nil
}

// ToolCall represents a tool invocation extracted from a response.
type ToolCall struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Arguments    json.RawMessage `json:"arguments"`
	RawArguments string          `json:"raw_arguments,omitempty"`
}

// ToolResult represents the output of a tool execution.
type ToolResult struct {
	ToolCallID string `json:"tool_call_id"`
	Content    string `json:"content"`
	IsError    bool   `json:"is_error"`
}

// Request is the unified input type for both complete() and stream().
type Request struct {
	Model           string            `json:"model"`
	Messages        []Message         `json:"messages"`
	Provider        string            `json:"provider,omitempty"`
	Tools           []ToolDefinition  `json:"tools,omitempty"`
	ToolChoice      *ToolChoice       `json:"tool_choice,omitempty"`
	ResponseFormat  *ResponseFormat   `json:"response_format,omitempty"`
	Temperature     *float64          `json:"temperature,omitempty"`
	TopP            *float64          `json:"top_p,omitempty"`
	MaxTokens       *int              `json:"max_tokens,omitempty"`
	StopSequences   []string          `json:"stop_sequences,omitempty"`
	ReasoningEffort string            `json:"reasoning_effort,omitempty"` // "none", "low", "medium", "high"
	Metadata        map[string]string `json:"metadata,omitempty"`
	ProviderOptions map[string]any    `json:"provider_options,omitempty"`
}

// Float64Ptr returns a pointer to a float64 value.
func Float64Ptr(v float64) *float64 {
	return &v
}

// Response is the unified output from a complete() call.
type Response struct {
	ID           string         `json:"id"`
	Model        string         `json:"model"`
	Provider     string         `json:"provider"`
	Message      Message        `json:"message"`
	FinishReason FinishReason   `json:"finish_reason"`
	Usage        Usage          `json:"usage"`
	Raw          json.RawMessage `json:"raw,omitempty"`
	Warnings     []Warning      `json:"warnings,omitempty"`
	RateLimit    *RateLimitInfo `json:"rate_limit,omitempty"`
}

// TextContent returns the concatenated text from the response message.
func (r *Response) TextContent() string {
	return r.Message.TextContent()
}

// ToolCalls returns tool calls from the response message.
func (r *Response) ToolCalls() []ToolCallData {
	return r.Message.ToolCalls()
}

// Reasoning returns concatenated reasoning text from the response.
func (r *Response) Reasoning() string {
	return r.Message.ReasoningContent()
}

// StreamEventType discriminates the type of streaming event.
type StreamEventType string

const (
	StreamStart        StreamEventType = "stream_start"
	StreamTextStart    StreamEventType = "text_start"
	StreamTextDelta    StreamEventType = "text_delta"
	StreamTextEnd      StreamEventType = "text_end"
	StreamReasonStart  StreamEventType = "reasoning_start"
	StreamReasonDelta  StreamEventType = "reasoning_delta"
	StreamReasonEnd    StreamEventType = "reasoning_end"
	StreamToolStart    StreamEventType = "tool_call_start"
	StreamToolDelta    StreamEventType = "tool_call_delta"
	StreamToolEnd      StreamEventType = "tool_call_end"
	StreamFinish       StreamEventType = "finish"
	StreamErrorEvt     StreamEventType = "error"
	StreamProviderEvt  StreamEventType = "provider_event"
)

// StreamEvent represents a single event in a streaming response.
type StreamEvent struct {
	Type           StreamEventType  `json:"type"`
	Delta          string           `json:"delta,omitempty"`
	TextID         string           `json:"text_id,omitempty"`
	ReasoningDelta string           `json:"reasoning_delta,omitempty"`
	ToolCall       *ToolCall        `json:"tool_call,omitempty"`
	FinishReason   *FinishReason    `json:"finish_reason,omitempty"`
	Usage          *Usage           `json:"usage,omitempty"`
	Response       *Response        `json:"response,omitempty"`
	Error          error            `json:"-"`
	Raw            *json.RawMessage `json:"raw,omitempty"`
}

// TimeoutConfig specifies timeout durations for generation.
type TimeoutConfig struct {
	Total   time.Duration `json:"total,omitempty"`
	PerStep time.Duration `json:"per_step,omitempty"`
}

// AdapterTimeout specifies timeout durations at the adapter level.
type AdapterTimeout struct {
	Connect    time.Duration `json:"connect"`
	Request    time.Duration `json:"request"`
	StreamRead time.Duration `json:"stream_read"`
}

// DefaultAdapterTimeout returns sensible defaults for adapter timeouts.
func DefaultAdapterTimeout() AdapterTimeout {
	return AdapterTimeout{
		Connect:    10 * time.Second,
		Request:    120 * time.Second,
		StreamRead: 30 * time.Second,
	}
}
