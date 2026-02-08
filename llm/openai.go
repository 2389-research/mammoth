// ABOUTME: OpenAI Responses API provider adapter for the unified LLM client SDK.
// ABOUTME: Translates unified Request/Response types to OpenAI's /v1/responses endpoint format, supporting streaming via SSE.

package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/2389-research/makeatron/llm/sse"
)

// OpenAIAdapter implements ProviderAdapter for the OpenAI Responses API.
type OpenAIAdapter struct {
	*BaseAdapter
	organization string
	project      string
}

// OpenAIOption is a functional option for configuring an OpenAIAdapter.
type OpenAIOption func(*OpenAIAdapter)

// WithOpenAIBaseURL sets the base URL for OpenAI API requests.
func WithOpenAIBaseURL(url string) OpenAIOption {
	return func(a *OpenAIAdapter) {
		a.BaseURL = url
	}
}

// WithOpenAITimeout sets the timeout configuration for OpenAI API requests.
func WithOpenAITimeout(timeout AdapterTimeout) OpenAIOption {
	return func(a *OpenAIAdapter) {
		a.Timeout = timeout
		a.HTTPClient = &http.Client{Timeout: timeout.Request}
	}
}

// WithOpenAIOrganization sets the OpenAI-Organization header for API requests.
func WithOpenAIOrganization(org string) OpenAIOption {
	return func(a *OpenAIAdapter) {
		a.organization = org
	}
}

// WithOpenAIProject sets the OpenAI-Project header for API requests.
func WithOpenAIProject(project string) OpenAIOption {
	return func(a *OpenAIAdapter) {
		a.project = project
	}
}

// NewOpenAIAdapter creates a new OpenAIAdapter with the given API key and options.
func NewOpenAIAdapter(apiKey string, opts ...OpenAIOption) *OpenAIAdapter {
	adapter := &OpenAIAdapter{
		BaseAdapter: NewBaseAdapter(apiKey, "https://api.openai.com", DefaultAdapterTimeout()),
	}
	for _, opt := range opts {
		opt(adapter)
	}

	// Set persistent headers for org and project
	if adapter.organization != "" {
		adapter.DefaultHeaders["OpenAI-Organization"] = adapter.organization
	}
	if adapter.project != "" {
		adapter.DefaultHeaders["OpenAI-Project"] = adapter.project
	}

	return adapter
}

// Name returns the provider name for this adapter.
func (a *OpenAIAdapter) Name() string {
	return "openai"
}

// Close releases resources held by the adapter.
func (a *OpenAIAdapter) Close() error {
	return nil
}

// Complete sends a synchronous completion request to the OpenAI Responses API.
func (a *OpenAIAdapter) Complete(ctx context.Context, req Request) (*Response, error) {
	body := a.buildRequestBody(req)

	resp, err := a.DoRequest(ctx, http.MethodPost, "/v1/responses", body, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, a.handleErrorResponse(resp)
	}

	return a.parseResponse(resp)
}

// Stream sends a streaming request to the OpenAI Responses API and returns a channel of events.
func (a *OpenAIAdapter) Stream(ctx context.Context, req Request) (<-chan StreamEvent, error) {
	body := a.buildRequestBody(req)
	body["stream"] = true

	resp, err := a.DoRequest(ctx, http.MethodPost, "/v1/responses", body, nil)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, a.handleErrorResponse(resp)
	}

	ch := make(chan StreamEvent)
	go a.processSSEStream(ctx, resp.Body, ch)
	return ch, nil
}

// buildRequestBody translates a unified Request into the OpenAI Responses API request format.
func (a *OpenAIAdapter) buildRequestBody(req Request) map[string]any {
	body := map[string]any{
		"model": req.Model,
	}

	// Extract system/developer messages into instructions param
	systemText, remaining := ExtractSystemMessages(req.Messages)
	if systemText != "" {
		body["instructions"] = systemText
	}

	// Translate remaining messages into input items
	input := a.translateMessages(remaining)
	body["input"] = input

	// Optional params
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	if req.MaxTokens != nil {
		body["max_output_tokens"] = *req.MaxTokens
	}
	if req.TopP != nil {
		body["top_p"] = *req.TopP
	}
	if len(req.StopSequences) > 0 {
		body["stop"] = req.StopSequences
	}

	// Reasoning effort
	if req.ReasoningEffort != "" {
		body["reasoning"] = map[string]any{
			"effort": req.ReasoningEffort,
		}
	}

	// Tools
	if len(req.Tools) > 0 {
		body["tools"] = a.translateTools(req.Tools)
	}

	// Tool choice
	if req.ToolChoice != nil {
		tc := a.translateToolChoice(req.ToolChoice)
		if tc != nil {
			body["tool_choice"] = tc
		}
	}

	// Response format
	if req.ResponseFormat != nil {
		body["text"] = map[string]any{
			"format": a.translateResponseFormat(req.ResponseFormat),
		}
	}

	// Merge provider-specific options
	if opts, ok := req.ProviderOptions["openai"]; ok {
		if optsMap, ok := opts.(map[string]any); ok {
			for k, v := range optsMap {
				body[k] = v
			}
		}
	}

	return body
}

// translateMessages converts unified Messages into OpenAI Responses API input items.
func (a *OpenAIAdapter) translateMessages(messages []Message) []map[string]any {
	var input []map[string]any

	for _, msg := range messages {
		items := a.translateMessage(msg)
		input = append(input, items...)
	}

	return input
}

// translateMessage converts a single unified Message into one or more OpenAI input items.
func (a *OpenAIAdapter) translateMessage(msg Message) []map[string]any {
	var items []map[string]any

	switch msg.Role {
	case RoleUser:
		content := a.translateContentParts(msg.Content, "user")
		items = append(items, map[string]any{
			"type":    "message",
			"role":    "user",
			"content": content,
		})

	case RoleAssistant:
		// Assistant messages can contain text and/or tool calls.
		// Tool calls become separate function_call items.
		var textContent []map[string]any
		for _, part := range msg.Content {
			switch part.Kind {
			case ContentText:
				textContent = append(textContent, map[string]any{
					"type": "output_text",
					"text": part.Text,
				})
			case ContentToolCall:
				if part.ToolCall != nil {
					items = append(items, map[string]any{
						"type":      "function_call",
						"id":        part.ToolCall.ID,
						"name":      part.ToolCall.Name,
						"arguments": string(part.ToolCall.Arguments),
					})
				}
			}
		}
		// If there's text content, prepend it as a message
		if len(textContent) > 0 {
			msgItem := map[string]any{
				"type":    "message",
				"role":    "assistant",
				"content": textContent,
			}
			// Prepend text message before any tool call items
			items = append([]map[string]any{msgItem}, items...)
		}

	case RoleTool:
		// Tool results become function_call_output items
		for _, part := range msg.Content {
			if part.Kind == ContentToolResult && part.ToolResult != nil {
				items = append(items, map[string]any{
					"type":    "function_call_output",
					"call_id": part.ToolResult.ToolCallID,
					"output":  part.ToolResult.Content,
				})
			}
		}
	}

	return items
}

// translateContentParts converts unified ContentParts into OpenAI content items for a given role.
func (a *OpenAIAdapter) translateContentParts(parts []ContentPart, role string) []map[string]any {
	var content []map[string]any

	for _, part := range parts {
		switch part.Kind {
		case ContentText:
			if role == "user" {
				content = append(content, map[string]any{
					"type": "input_text",
					"text": part.Text,
				})
			} else {
				content = append(content, map[string]any{
					"type": "output_text",
					"text": part.Text,
				})
			}

		case ContentImage:
			if part.Image != nil {
				imgItem := map[string]any{
					"type": "input_image",
				}
				if part.Image.URL != "" {
					imgItem["image_url"] = part.Image.URL
				} else if len(part.Image.Data) > 0 {
					b64 := base64.StdEncoding.EncodeToString(part.Image.Data)
					mediaType := part.Image.MediaType
					if mediaType == "" {
						mediaType = "image/png"
					}
					imgItem["image_url"] = fmt.Sprintf("data:%s;base64,%s", mediaType, b64)
				}
				if part.Image.Detail != "" {
					imgItem["detail"] = part.Image.Detail
				}
				content = append(content, imgItem)
			}
		}
	}

	return content
}

// translateTools converts unified ToolDefinitions into OpenAI tool format.
func (a *OpenAIAdapter) translateTools(tools []ToolDefinition) []map[string]any {
	var result []map[string]any
	for _, tool := range tools {
		t := map[string]any{
			"type":        "function",
			"name":        tool.Name,
			"description": tool.Description,
		}
		if tool.Parameters != nil {
			var params any
			if err := json.Unmarshal(tool.Parameters, &params); err == nil {
				t["parameters"] = params
			}
		}
		result = append(result, t)
	}
	return result
}

// translateToolChoice converts a unified ToolChoice into the OpenAI tool_choice format.
func (a *OpenAIAdapter) translateToolChoice(tc *ToolChoice) any {
	switch tc.Mode {
	case ToolChoiceAuto:
		return "auto"
	case ToolChoiceNone:
		return "none"
	case ToolChoiceRequired:
		return "required"
	case ToolChoiceNamed:
		return map[string]any{
			"type": "function",
			"name": tc.ToolName,
		}
	default:
		return nil
	}
}

// translateResponseFormat converts a unified ResponseFormat into OpenAI format.
func (a *OpenAIAdapter) translateResponseFormat(rf *ResponseFormat) map[string]any {
	result := map[string]any{
		"type": rf.Type,
	}
	if rf.JSONSchema != nil {
		result["json_schema"] = rf.JSONSchema
	}
	return result
}

// openaiResponseBody represents the structure of an OpenAI Responses API response.
type openaiResponseBody struct {
	ID                string              `json:"id"`
	Model             string              `json:"model"`
	Status            string              `json:"status"`
	Output            []openaiOutputItem  `json:"output"`
	Usage             openaiUsage         `json:"usage"`
	IncompleteDetails *openaiIncomplete   `json:"incomplete_details,omitempty"`
}

type openaiOutputItem struct {
	Type      string               `json:"type"`
	ID        string               `json:"id,omitempty"`
	Role      string               `json:"role,omitempty"`
	Content   []openaiContentItem  `json:"content,omitempty"`
	Name      string               `json:"name,omitempty"`
	Arguments string               `json:"arguments,omitempty"`
}

type openaiContentItem struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type openaiUsage struct {
	InputTokens        int                    `json:"input_tokens"`
	OutputTokens       int                    `json:"output_tokens"`
	TotalTokens        int                    `json:"total_tokens"`
	OutputTokensDetail *openaiOutputDetail    `json:"output_tokens_details,omitempty"`
	PromptTokensDetail *openaiPromptDetail    `json:"prompt_tokens_details,omitempty"`
}

type openaiOutputDetail struct {
	ReasoningTokens int `json:"reasoning_tokens"`
}

type openaiPromptDetail struct {
	CachedTokens int `json:"cached_tokens"`
}

type openaiIncomplete struct {
	Reason string `json:"reason"`
}

// parseResponse converts an HTTP response into a unified Response.
func (a *OpenAIAdapter) parseResponse(httpResp *http.Response) (*Response, error) {
	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	var oaiResp openaiResponseBody
	if err := json.Unmarshal(body, &oaiResp); err != nil {
		return nil, fmt.Errorf("parsing response body: %w", err)
	}

	resp := &Response{
		ID:       oaiResp.ID,
		Model:    oaiResp.Model,
		Provider: "openai",
		Message: Message{
			Role: RoleAssistant,
		},
		Raw: json.RawMessage(body),
	}

	// Parse output items into message content
	var hasToolCalls bool
	for _, item := range oaiResp.Output {
		switch item.Type {
		case "message":
			for _, ci := range item.Content {
				if ci.Type == "output_text" {
					resp.Message.Content = append(resp.Message.Content, TextPart(ci.Text))
				}
			}
		case "function_call":
			hasToolCalls = true
			resp.Message.Content = append(resp.Message.Content, ToolCallPart(
				item.ID,
				item.Name,
				json.RawMessage(item.Arguments),
			))
		}
	}

	// Determine finish reason
	resp.FinishReason = a.mapFinishReason(oaiResp.Status, oaiResp.IncompleteDetails, hasToolCalls)

	// Parse usage
	resp.Usage = Usage{
		InputTokens:  oaiResp.Usage.InputTokens,
		OutputTokens: oaiResp.Usage.OutputTokens,
		TotalTokens:  oaiResp.Usage.TotalTokens,
	}
	if oaiResp.Usage.OutputTokensDetail != nil && oaiResp.Usage.OutputTokensDetail.ReasoningTokens > 0 {
		resp.Usage.ReasoningTokens = IntPtr(oaiResp.Usage.OutputTokensDetail.ReasoningTokens)
	}
	if oaiResp.Usage.PromptTokensDetail != nil && oaiResp.Usage.PromptTokensDetail.CachedTokens > 0 {
		resp.Usage.CacheReadTokens = IntPtr(oaiResp.Usage.PromptTokensDetail.CachedTokens)
	}

	// Parse rate limit headers
	resp.RateLimit = a.ParseRateLimitHeaders(httpResp.Header)

	return resp, nil
}

// mapFinishReason translates OpenAI response status to a unified FinishReason.
func (a *OpenAIAdapter) mapFinishReason(status string, incomplete *openaiIncomplete, hasToolCalls bool) FinishReason {
	if hasToolCalls {
		return FinishReason{Reason: FinishToolCalls, Raw: status}
	}

	if status == "incomplete" && incomplete != nil {
		switch incomplete.Reason {
		case "max_output_tokens":
			return FinishReason{Reason: FinishLength, Raw: "max_output_tokens"}
		case "content_filter":
			return FinishReason{Reason: FinishContentFilter, Raw: "content_filter"}
		default:
			return FinishReason{Reason: FinishOther, Raw: incomplete.Reason}
		}
	}

	switch status {
	case "completed":
		return FinishReason{Reason: FinishStop, Raw: status}
	case "failed":
		return FinishReason{Reason: FinishError, Raw: status}
	default:
		return FinishReason{Reason: FinishOther, Raw: status}
	}
}

// handleErrorResponse parses an HTTP error response and returns an appropriate error type.
func (a *OpenAIAdapter) handleErrorResponse(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading error response: %w", err)
	}

	var errResp struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
		} `json:"error"`
	}

	message := fmt.Sprintf("openai API error (status %d)", resp.StatusCode)
	var errorCode string

	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error.Message != "" {
		message = errResp.Error.Message
		errorCode = errResp.Error.Code
		if errorCode == "" {
			errorCode = errResp.Error.Type
		}
	}

	return ErrorFromStatusCode(resp.StatusCode, message, "openai", errorCode, json.RawMessage(body), nil)
}

// processSSEStream reads SSE events from the response body and emits unified StreamEvents.
func (a *OpenAIAdapter) processSSEStream(ctx context.Context, body io.ReadCloser, ch chan<- StreamEvent) {
	defer close(ch)
	defer body.Close()

	parser := sse.NewParser(body)

	// Track state for emitting start events
	textStarted := make(map[int]bool)
	toolStarted := make(map[int]bool)

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

		a.handleSSEEvent(event, ch, textStarted, toolStarted)
	}
}

// handleSSEEvent processes a single SSE event and emits the corresponding stream events.
func (a *OpenAIAdapter) handleSSEEvent(event sse.Event, ch chan<- StreamEvent, textStarted, toolStarted map[int]bool) {
	switch event.Type {
	case "response.output_text.delta":
		var delta struct {
			OutputIndex  int    `json:"output_index"`
			ContentIndex int    `json:"content_index"`
			Delta        string `json:"delta"`
		}
		if err := json.Unmarshal([]byte(event.Data), &delta); err != nil {
			return
		}

		// Emit text start on first delta for this output index
		if !textStarted[delta.OutputIndex] {
			textStarted[delta.OutputIndex] = true
			ch <- StreamEvent{
				Type: StreamTextStart,
			}
		}

		ch <- StreamEvent{
			Type:  StreamTextDelta,
			Delta: delta.Delta,
		}

	case "response.output_text.done":
		var done struct {
			OutputIndex  int    `json:"output_index"`
			ContentIndex int    `json:"content_index"`
			Text         string `json:"text"`
		}
		if err := json.Unmarshal([]byte(event.Data), &done); err != nil {
			return
		}
		ch <- StreamEvent{
			Type: StreamTextEnd,
		}

	case "response.output_item.added":
		var added struct {
			OutputIndex int `json:"output_index"`
			Item        struct {
				Type      string `json:"type"`
				ID        string `json:"id"`
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"item"`
		}
		if err := json.Unmarshal([]byte(event.Data), &added); err != nil {
			return
		}

		// Emit tool start for function_call items
		if added.Item.Type == "function_call" {
			toolStarted[added.OutputIndex] = true
			ch <- StreamEvent{
				Type: StreamToolStart,
				ToolCall: &ToolCall{
					ID:   added.Item.ID,
					Name: added.Item.Name,
				},
			}
		}

	case "response.function_call_arguments.delta":
		var delta struct {
			OutputIndex int    `json:"output_index"`
			Delta       string `json:"delta"`
		}
		if err := json.Unmarshal([]byte(event.Data), &delta); err != nil {
			return
		}

		ch <- StreamEvent{
			Type:  StreamToolDelta,
			Delta: delta.Delta,
		}

	case "response.output_item.done":
		var done struct {
			OutputIndex int `json:"output_index"`
			Item        struct {
				Type string `json:"type"`
			} `json:"item"`
		}
		if err := json.Unmarshal([]byte(event.Data), &done); err != nil {
			return
		}

		// Emit tool end for function_call items
		if done.Item.Type == "function_call" {
			ch <- StreamEvent{
				Type: StreamToolEnd,
			}
		}

	case "response.completed":
		var completed struct {
			Response openaiResponseBody `json:"response"`
		}
		if err := json.Unmarshal([]byte(event.Data), &completed); err != nil {
			return
		}

		usage := &Usage{
			InputTokens:  completed.Response.Usage.InputTokens,
			OutputTokens: completed.Response.Usage.OutputTokens,
			TotalTokens:  completed.Response.Usage.TotalTokens,
		}
		if completed.Response.Usage.OutputTokensDetail != nil && completed.Response.Usage.OutputTokensDetail.ReasoningTokens > 0 {
			usage.ReasoningTokens = IntPtr(completed.Response.Usage.OutputTokensDetail.ReasoningTokens)
		}
		if completed.Response.Usage.PromptTokensDetail != nil && completed.Response.Usage.PromptTokensDetail.CachedTokens > 0 {
			usage.CacheReadTokens = IntPtr(completed.Response.Usage.PromptTokensDetail.CachedTokens)
		}

		hasToolCalls := false
		for _, item := range completed.Response.Output {
			if item.Type == "function_call" {
				hasToolCalls = true
				break
			}
		}
		finishReason := a.mapFinishReason(completed.Response.Status, completed.Response.IncompleteDetails, hasToolCalls)

		ch <- StreamEvent{
			Type:         StreamFinish,
			Usage:        usage,
			FinishReason: &finishReason,
		}
	}
}
