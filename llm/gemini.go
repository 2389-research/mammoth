// ABOUTME: Gemini provider adapter for the unified LLM client SDK using the native Gemini API.
// ABOUTME: Translates between unified Request/Response types and Gemini's generateContent/streamGenerateContent endpoints.

package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/2389-research/mammoth/llm/sse"
)

// GeminiAdapter implements ProviderAdapter for Google's Gemini API.
// It uses query-parameter authentication and translates between the unified
// SDK types and Gemini's native generateContent request/response format.
type GeminiAdapter struct {
	apiKey       string
	base         *BaseAdapter
	callIDToName map[string]string
	mu           sync.Mutex
}

// GeminiOption is a functional option for configuring a GeminiAdapter.
type GeminiOption func(*GeminiAdapter)

// WithGeminiBaseURL sets the base URL for the Gemini API.
// Default is "https://generativelanguage.googleapis.com".
func WithGeminiBaseURL(url string) GeminiOption {
	return func(a *GeminiAdapter) {
		a.base.BaseURL = url
	}
}

// WithGeminiTimeout sets the timeout configuration for the adapter.
func WithGeminiTimeout(timeout AdapterTimeout) GeminiOption {
	return func(a *GeminiAdapter) {
		a.base.Timeout = timeout
		a.base.HTTPClient = &http.Client{
			Timeout: timeout.Request,
		}
	}
}

// NewGeminiAdapter creates a GeminiAdapter with the given API key and options.
// The BaseAdapter APIKey is set to empty so DoRequest will not add a Bearer token;
// authentication is handled via query parameter instead.
func NewGeminiAdapter(apiKey string, opts ...GeminiOption) *GeminiAdapter {
	adapter := &GeminiAdapter{
		apiKey:       apiKey,
		base:         NewBaseAdapter("", "https://generativelanguage.googleapis.com", DefaultAdapterTimeout()),
		callIDToName: make(map[string]string),
	}
	for _, opt := range opts {
		opt(adapter)
	}
	return adapter
}

// Name returns the provider name "gemini".
func (a *GeminiAdapter) Name() string {
	return "gemini"
}

// Close releases any resources held by the adapter.
func (a *GeminiAdapter) Close() error {
	return nil
}

// Complete sends a non-streaming completion request to the Gemini API and returns
// a unified Response.
func (a *GeminiAdapter) Complete(ctx context.Context, req Request) (*Response, error) {
	body := a.buildRequestBody(req)
	path := fmt.Sprintf("/v1beta/models/%s:generateContent?key=%s", req.Model, a.apiKey)

	httpResp, err := a.base.DoRequest(ctx, http.MethodPost, path, body, nil)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return nil, a.parseErrorResponse(httpResp.StatusCode, respBody)
	}

	return a.parseResponse(req.Model, respBody)
}

// Stream sends a streaming request to the Gemini API and returns a channel of StreamEvents.
func (a *GeminiAdapter) Stream(ctx context.Context, req Request) (<-chan StreamEvent, error) {
	body := a.buildRequestBody(req)
	path := fmt.Sprintf("/v1beta/models/%s:streamGenerateContent?alt=sse&key=%s", req.Model, a.apiKey)

	httpResp, err := a.base.DoRequest(ctx, http.MethodPost, path, body, nil)
	if err != nil {
		return nil, err
	}

	if httpResp.StatusCode != http.StatusOK {
		defer httpResp.Body.Close()
		respBody, readErr := io.ReadAll(httpResp.Body)
		if readErr != nil {
			return nil, fmt.Errorf("reading error response: %w", readErr)
		}
		return nil, a.parseErrorResponse(httpResp.StatusCode, respBody)
	}

	ch := make(chan StreamEvent, 64)
	go a.processSSEStream(ctx, httpResp.Body, ch)
	return ch, nil
}

// buildRequestBody translates a unified Request into a Gemini API request body map.
func (a *GeminiAdapter) buildRequestBody(req Request) map[string]any {
	body := make(map[string]any)

	// Extract system messages into systemInstruction
	systemText, remaining := ExtractSystemMessages(req.Messages)
	if systemText != "" {
		body["systemInstruction"] = map[string]any{
			"parts": []map[string]any{
				{"text": systemText},
			},
		}
	}

	// Translate messages to Gemini contents format
	var contents []map[string]any
	for _, msg := range remaining {
		content := a.translateMessage(msg)
		if content != nil {
			contents = append(contents, content)
		}
	}
	body["contents"] = contents

	// Translate tools to functionDeclarations
	if len(req.Tools) > 0 {
		var funcDecls []map[string]any
		for _, tool := range req.Tools {
			decl := map[string]any{
				"name":        tool.Name,
				"description": tool.Description,
			}
			if tool.Parameters != nil {
				var params any
				json.Unmarshal(tool.Parameters, &params)
				decl["parameters"] = params
			}
			funcDecls = append(funcDecls, decl)
		}
		body["tools"] = []map[string]any{
			{"functionDeclarations": funcDecls},
		}
	}

	// Translate tool choice to tool_config
	if req.ToolChoice != nil {
		fcc := make(map[string]any)
		switch req.ToolChoice.Mode {
		case ToolChoiceAuto:
			fcc["mode"] = "AUTO"
		case ToolChoiceNone:
			fcc["mode"] = "NONE"
		case ToolChoiceRequired:
			fcc["mode"] = "ANY"
		case ToolChoiceNamed:
			fcc["mode"] = "ANY"
			fcc["allowed_function_names"] = []string{req.ToolChoice.ToolName}
		}
		body["tool_config"] = map[string]any{
			"function_calling_config": fcc,
		}
	}

	// Build generationConfig
	genConfig := make(map[string]any)
	hasGenConfig := false

	if req.Temperature != nil {
		genConfig["temperature"] = *req.Temperature
		hasGenConfig = true
	}
	if req.TopP != nil {
		genConfig["topP"] = *req.TopP
		hasGenConfig = true
	}
	if req.MaxTokens != nil {
		genConfig["maxOutputTokens"] = *req.MaxTokens
		hasGenConfig = true
	}
	if len(req.StopSequences) > 0 {
		genConfig["stopSequences"] = req.StopSequences
		hasGenConfig = true
	}

	if hasGenConfig {
		body["generationConfig"] = genConfig
	}

	// Merge provider options from ProviderOptions["gemini"]
	if opts, ok := req.ProviderOptions["gemini"]; ok {
		if geminiOpts, ok := opts.(map[string]any); ok {
			for k, v := range geminiOpts {
				body[k] = v
			}
		}
	}

	return body
}

// translateMessage converts a unified Message into a Gemini content map.
func (a *GeminiAdapter) translateMessage(msg Message) map[string]any {
	role := a.translateRole(msg.Role)

	var parts []map[string]any
	for _, cp := range msg.Content {
		part := a.translateContentPart(msg.Role, cp)
		if part != nil {
			parts = append(parts, part)
		}
	}

	if len(parts) == 0 {
		return nil
	}

	return map[string]any{
		"role":  role,
		"parts": parts,
	}
}

// translateRole maps unified roles to Gemini roles.
func (a *GeminiAdapter) translateRole(role Role) string {
	switch role {
	case RoleUser, RoleTool:
		return "user"
	case RoleAssistant:
		return "model"
	default:
		return "user"
	}
}

// translateContentPart converts a unified ContentPart into a Gemini part map.
func (a *GeminiAdapter) translateContentPart(role Role, cp ContentPart) map[string]any {
	switch cp.Kind {
	case ContentText:
		return map[string]any{"text": cp.Text}

	case ContentImage:
		if cp.Image == nil {
			return nil
		}
		if cp.Image.URL != "" {
			mimeType := cp.Image.MediaType
			if mimeType == "" {
				mimeType = "image/png"
			}
			return map[string]any{
				"fileData": map[string]any{
					"mimeType": mimeType,
					"fileUri":  cp.Image.URL,
				},
			}
		}
		if len(cp.Image.Data) > 0 {
			mimeType := cp.Image.MediaType
			if mimeType == "" {
				mimeType = "image/png"
			}
			return map[string]any{
				"inlineData": map[string]any{
					"mimeType": mimeType,
					"data":     base64.StdEncoding.EncodeToString(cp.Image.Data),
				},
			}
		}
		return nil

	case ContentToolCall:
		if cp.ToolCall == nil {
			return nil
		}
		var args any
		if cp.ToolCall.Arguments != nil {
			json.Unmarshal(cp.ToolCall.Arguments, &args)
		}
		return map[string]any{
			"functionCall": map[string]any{
				"name": cp.ToolCall.Name,
				"args": args,
			},
		}

	case ContentToolResult:
		if cp.ToolResult == nil {
			return nil
		}
		// Look up the function name from the synthetic ID mapping
		funcName := a.lookupFunctionName(cp.ToolResult.ToolCallID)
		var result any
		if err := json.Unmarshal([]byte(cp.ToolResult.Content), &result); err != nil {
			// If content is not valid JSON, wrap it as a string result
			result = map[string]any{"result": cp.ToolResult.Content}
		}
		return map[string]any{
			"functionResponse": map[string]any{
				"name":     funcName,
				"response": result,
			},
		}

	default:
		return nil
	}
}

// lookupFunctionName retrieves the function name for a synthetic tool call ID.
// If the ID is not found in the mapping, it returns the ID itself as a fallback.
func (a *GeminiAdapter) lookupFunctionName(toolCallID string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if name, ok := a.callIDToName[toolCallID]; ok {
		return name
	}
	return toolCallID
}

// parseResponse translates a Gemini API response into the unified Response type.
func (a *GeminiAdapter) parseResponse(model string, respBody []byte) (*Response, error) {
	var geminiResp geminiResponse
	if err := json.Unmarshal(respBody, &geminiResp); err != nil {
		return nil, fmt.Errorf("parsing Gemini response: %w", err)
	}

	resp := &Response{
		Provider: "gemini",
		Model:    model,
		Message: Message{
			Role: RoleAssistant,
		},
	}

	// Store raw response
	raw := json.RawMessage(respBody)
	resp.Raw = raw

	// Use modelVersion if available
	if geminiResp.ModelVersion != "" {
		resp.Model = geminiResp.ModelVersion
	}

	// Parse candidates
	hasToolCalls := false
	if len(geminiResp.Candidates) > 0 {
		candidate := geminiResp.Candidates[0]

		for _, part := range candidate.Content.Parts {
			if part.Text != "" {
				resp.Message.Content = append(resp.Message.Content, TextPart(part.Text))
			}
			if part.FunctionCall != nil {
				hasToolCalls = true
				callID := GenerateCallID()
				argsJSON, _ := json.Marshal(part.FunctionCall.Args)

				a.mu.Lock()
				a.callIDToName[callID] = part.FunctionCall.Name
				a.mu.Unlock()

				resp.Message.Content = append(resp.Message.Content, ToolCallPart(callID, part.FunctionCall.Name, argsJSON))
			}
		}

		// Map finish reason
		resp.FinishReason = a.mapFinishReason(candidate.FinishReason, hasToolCalls)
	}

	// Parse usage
	if geminiResp.UsageMetadata != nil {
		resp.Usage = Usage{
			InputTokens:  geminiResp.UsageMetadata.PromptTokenCount,
			OutputTokens: geminiResp.UsageMetadata.CandidatesTokenCount,
			TotalTokens:  geminiResp.UsageMetadata.TotalTokenCount,
		}
		if geminiResp.UsageMetadata.ThoughtsTokenCount > 0 {
			resp.Usage.ReasoningTokens = IntPtr(geminiResp.UsageMetadata.ThoughtsTokenCount)
		}
		if geminiResp.UsageMetadata.CachedContentTokenCount > 0 {
			resp.Usage.CacheReadTokens = IntPtr(geminiResp.UsageMetadata.CachedContentTokenCount)
		}
	}

	return resp, nil
}

// mapFinishReason translates a Gemini finish reason string to a unified FinishReason.
// If the response contains tool calls, the finish reason is overridden to "tool_calls".
func (a *GeminiAdapter) mapFinishReason(geminiReason string, hasToolCalls bool) FinishReason {
	if hasToolCalls {
		return FinishReason{Reason: FinishToolCalls, Raw: geminiReason}
	}

	var reason string
	switch geminiReason {
	case "STOP":
		reason = FinishStop
	case "MAX_TOKENS":
		reason = FinishLength
	case "SAFETY":
		reason = FinishContentFilter
	default:
		reason = FinishOther
	}

	return FinishReason{Reason: reason, Raw: geminiReason}
}

// parseErrorResponse parses a Gemini error response and returns the appropriate error type.
func (a *GeminiAdapter) parseErrorResponse(statusCode int, respBody []byte) error {
	var errResp geminiErrorResponse
	if err := json.Unmarshal(respBody, &errResp); err != nil {
		return ErrorFromStatusCode(statusCode, fmt.Sprintf("HTTP %d (unparseable body)", statusCode), "gemini", "", json.RawMessage(respBody), nil)
	}

	return ErrorFromStatusCode(
		statusCode,
		errResp.Error.Message,
		"gemini",
		errResp.Error.Status,
		json.RawMessage(respBody),
		nil,
	)
}

// processSSEStream reads SSE events from the response body and sends StreamEvents to the channel.
func (a *GeminiAdapter) processSSEStream(ctx context.Context, body io.ReadCloser, ch chan<- StreamEvent) {
	defer close(ch)
	defer body.Close()

	parser := sse.NewParser(body)
	textStarted := false
	var lastUsage *Usage

	for {
		select {
		case <-ctx.Done():
			ch <- StreamEvent{Type: StreamErrorEvt, Error: ctx.Err()}
			return
		default:
		}

		event, err := parser.Next()
		if err != nil {
			if err == io.EOF {
				// Emit text end if we were in a text block
				if textStarted {
					ch <- StreamEvent{Type: StreamTextEnd}
				}
				// Emit finish event
				finishEvt := StreamEvent{
					Type:         StreamFinish,
					FinishReason: &FinishReason{Reason: FinishStop},
				}
				if lastUsage != nil {
					finishEvt.Usage = lastUsage
				}
				ch <- finishEvt
				return
			}
			ch <- StreamEvent{Type: StreamErrorEvt, Error: err}
			return
		}

		if event.Data == "" {
			continue
		}

		var chunk geminiResponse
		if err := json.Unmarshal([]byte(event.Data), &chunk); err != nil {
			ch <- StreamEvent{Type: StreamErrorEvt, Error: fmt.Errorf("parsing SSE data: %w", err)}
			continue
		}

		// Track usage from the chunk
		if chunk.UsageMetadata != nil {
			usage := Usage{
				InputTokens:  chunk.UsageMetadata.PromptTokenCount,
				OutputTokens: chunk.UsageMetadata.CandidatesTokenCount,
				TotalTokens:  chunk.UsageMetadata.TotalTokenCount,
			}
			if chunk.UsageMetadata.ThoughtsTokenCount > 0 {
				usage.ReasoningTokens = IntPtr(chunk.UsageMetadata.ThoughtsTokenCount)
			}
			if chunk.UsageMetadata.CachedContentTokenCount > 0 {
				usage.CacheReadTokens = IntPtr(chunk.UsageMetadata.CachedContentTokenCount)
			}
			lastUsage = &usage
		}

		// Process candidate parts
		if len(chunk.Candidates) > 0 {
			candidate := chunk.Candidates[0]

			for _, part := range candidate.Content.Parts {
				if part.Text != "" {
					if !textStarted {
						ch <- StreamEvent{Type: StreamTextStart}
						textStarted = true
					}
					ch <- StreamEvent{Type: StreamTextDelta, Delta: part.Text}
				}

				if part.FunctionCall != nil {
					callID := GenerateCallID()
					argsJSON, _ := json.Marshal(part.FunctionCall.Args)

					a.mu.Lock()
					a.callIDToName[callID] = part.FunctionCall.Name
					a.mu.Unlock()

					ch <- StreamEvent{
						Type: StreamToolStart,
						ToolCall: &ToolCall{
							ID:        callID,
							Name:      part.FunctionCall.Name,
							Arguments: argsJSON,
						},
					}
					ch <- StreamEvent{
						Type: StreamToolEnd,
						ToolCall: &ToolCall{
							ID:        callID,
							Name:      part.FunctionCall.Name,
							Arguments: argsJSON,
						},
					}
				}
			}

			// Check for finish reason in this chunk
			if candidate.FinishReason != "" {
				if textStarted {
					ch <- StreamEvent{Type: StreamTextEnd}
					textStarted = false
				}

				hasToolCalls := false
				for _, part := range candidate.Content.Parts {
					if part.FunctionCall != nil {
						hasToolCalls = true
						break
					}
				}

				fr := a.mapFinishReason(candidate.FinishReason, hasToolCalls)
				finishEvt := StreamEvent{
					Type:         StreamFinish,
					FinishReason: &fr,
				}
				if lastUsage != nil {
					finishEvt.Usage = lastUsage
				}
				ch <- finishEvt
				return
			}
		}
	}
}

// geminiResponse represents the top-level JSON response from the Gemini API.
type geminiResponse struct {
	Candidates    []geminiCandidate `json:"candidates"`
	UsageMetadata *geminiUsage      `json:"usageMetadata"`
	ModelVersion  string            `json:"modelVersion"`
}

// geminiCandidate represents a single candidate in the Gemini response.
type geminiCandidate struct {
	Content      geminiContent `json:"content"`
	FinishReason string        `json:"finishReason"`
}

// geminiContent represents the content of a Gemini message.
type geminiContent struct {
	Parts []geminiPart `json:"parts"`
	Role  string       `json:"role"`
}

// geminiPart represents a single part in a Gemini content block.
type geminiPart struct {
	Text         string              `json:"text,omitempty"`
	FunctionCall *geminiFunctionCall `json:"functionCall,omitempty"`
}

// geminiFunctionCall represents a function call in a Gemini response.
type geminiFunctionCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

// geminiUsage represents token usage metadata from Gemini.
type geminiUsage struct {
	PromptTokenCount        int `json:"promptTokenCount"`
	CandidatesTokenCount    int `json:"candidatesTokenCount"`
	TotalTokenCount         int `json:"totalTokenCount"`
	ThoughtsTokenCount      int `json:"thoughtsTokenCount"`
	CachedContentTokenCount int `json:"cachedContentTokenCount"`
}

// geminiErrorResponse represents the error response format from Gemini.
type geminiErrorResponse struct {
	Error geminiErrorDetail `json:"error"`
}

// geminiErrorDetail holds the details of a Gemini API error.
type geminiErrorDetail struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

// Ensure GeminiAdapter implements ProviderAdapter at compile time.
var _ ProviderAdapter = (*GeminiAdapter)(nil)
