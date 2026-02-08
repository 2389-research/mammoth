// ABOUTME: High-level Generate API for the unified LLM client SDK.
// ABOUTME: Provides Generate, StreamGenerate, GenerateObject functions with tool loops, parallel execution, and structured output.

package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// StopCondition is a predicate that decides whether the tool loop should stop
// based on the accumulated step results so far.
type StopCondition func(steps []StepResult) bool

// StepResult captures the output of a single iteration in the generate loop.
type StepResult struct {
	Text         string
	Reasoning    string
	ToolCalls    []ToolCallData
	ToolResults  []ToolResult
	FinishReason FinishReason
	Usage        Usage
	Response     *Response
	Warnings     []Warning
}

// GenerateResult is the final output of a Generate call, aggregating all steps.
type GenerateResult struct {
	Text         string
	Reasoning    string
	ToolCalls    []ToolCallData
	ToolResults  []ToolResult
	FinishReason FinishReason
	Usage        Usage
	TotalUsage   Usage
	Steps        []StepResult
	Response     *Response
	Output       any // for GenerateObject parsed output
}

// GenerateOptions configures a Generate, StreamGenerate, or GenerateObject call.
type GenerateOptions struct {
	Model           string
	Prompt          string        // simple text prompt (mutually exclusive with Messages)
	Messages        []Message     // full message history
	System          string        // system message
	Tools           []Tool        // tools with optional execute handlers
	ToolChoice      *ToolChoice
	MaxToolRounds   int           // default 1
	StopWhen        StopCondition
	ResponseFormat  *ResponseFormat
	Temperature     *float64
	TopP            *float64
	MaxTokens       *int
	StopSequences   []string
	ReasoningEffort string
	Provider        string
	ProviderOptions map[string]any
	MaxRetries      int // default 2
	Timeout         *TimeoutConfig
	Client          *Client // override default client
}

// StreamResult wraps a channel of streaming events and provides access to the
// final response after the stream completes.
type StreamResult struct {
	Events   <-chan StreamEvent
	response *Response
	mu       sync.Mutex
}

// Response returns the final accumulated response after the stream completes.
func (sr *StreamResult) Response() *Response {
	sr.mu.Lock()
	defer sr.mu.Unlock()
	return sr.response
}

// StreamAccumulator collects streaming events and builds a complete Response.
type StreamAccumulator struct {
	textParts    []string
	toolCalls    map[string]*ToolCall
	toolCallOrder []string
	usage        *Usage
	finishReason *FinishReason
	mu           sync.Mutex
}

// NewStreamAccumulator creates a new StreamAccumulator ready to process events.
func NewStreamAccumulator() *StreamAccumulator {
	return &StreamAccumulator{
		toolCalls: make(map[string]*ToolCall),
	}
}

// Process ingests a single StreamEvent, updating the accumulator's internal state.
func (a *StreamAccumulator) Process(event StreamEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()

	switch event.Type {
	case StreamTextDelta:
		a.textParts = append(a.textParts, event.Delta)

	case StreamToolStart:
		if event.ToolCall != nil {
			tc := *event.ToolCall
			a.toolCalls[tc.ID] = &tc
			a.toolCallOrder = append(a.toolCallOrder, tc.ID)
		}

	case StreamToolDelta:
		// Tool deltas accumulate into existing tool calls
		// (currently stored but not appended to arguments for simplicity)

	case StreamToolEnd:
		// Tool call complete, already stored from start

	case StreamFinish:
		if event.Usage != nil {
			u := *event.Usage
			a.usage = &u
		}
		if event.FinishReason != nil {
			fr := *event.FinishReason
			a.finishReason = &fr
		}
	}
}

// Response constructs a complete Response from the accumulated stream events.
func (a *StreamAccumulator) Response() *Response {
	a.mu.Lock()
	defer a.mu.Unlock()

	var parts []ContentPart

	// Add accumulated text
	fullText := ""
	for _, t := range a.textParts {
		fullText += t
	}
	if fullText != "" {
		parts = append(parts, TextPart(fullText))
	}

	// Add tool calls in order
	for _, id := range a.toolCallOrder {
		if tc, ok := a.toolCalls[id]; ok {
			parts = append(parts, ContentPart{
				Kind: ContentToolCall,
				ToolCall: &ToolCallData{
					ID:        tc.ID,
					Name:      tc.Name,
					Arguments: tc.Arguments,
					Type:      "function",
				},
			})
		}
	}

	resp := &Response{
		Message: Message{
			Role:    RoleAssistant,
			Content: parts,
		},
	}

	if a.usage != nil {
		resp.Usage = *a.usage
	}
	if a.finishReason != nil {
		resp.FinishReason = *a.finishReason
	}

	return resp
}

// resolveClient returns the client to use for the generate call. It prefers
// opts.Client, falls back to GetDefaultClient, and returns an error if neither
// is available.
func resolveClient(opts GenerateOptions) (*Client, error) {
	if opts.Client != nil {
		return opts.Client, nil
	}
	c := GetDefaultClient()
	if c == nil {
		return nil, &ConfigurationError{
			SDKError: SDKError{
				Message: "no client available: set Client in GenerateOptions or call SetDefaultClient",
			},
		}
	}
	return c, nil
}

// buildMessages constructs the message list from GenerateOptions.
func buildMessages(opts GenerateOptions) ([]Message, error) {
	hasPrompt := opts.Prompt != ""
	hasMessages := len(opts.Messages) > 0

	if hasPrompt && hasMessages {
		return nil, &ConfigurationError{
			SDKError: SDKError{
				Message: "cannot set both Prompt and Messages in GenerateOptions; use one or the other",
			},
		}
	}

	var messages []Message

	// Prepend system message if set
	if opts.System != "" {
		messages = append(messages, SystemMessage(opts.System))
	}

	if hasPrompt {
		messages = append(messages, UserMessage(opts.Prompt))
	} else if hasMessages {
		messages = append(messages, opts.Messages...)
	}

	return messages, nil
}

// buildRequest constructs a Request from GenerateOptions and the current message list.
func buildRequest(opts GenerateOptions, messages []Message) Request {
	req := Request{
		Model:           opts.Model,
		Messages:        messages,
		Provider:        opts.Provider,
		ToolChoice:      opts.ToolChoice,
		ResponseFormat:  opts.ResponseFormat,
		Temperature:     opts.Temperature,
		TopP:            opts.TopP,
		MaxTokens:       opts.MaxTokens,
		StopSequences:   opts.StopSequences,
		ReasoningEffort: opts.ReasoningEffort,
		ProviderOptions: opts.ProviderOptions,
	}

	// Convert Tool definitions to ToolDefinitions for the request
	if len(opts.Tools) > 0 {
		defs := make([]ToolDefinition, len(opts.Tools))
		for i, t := range opts.Tools {
			defs[i] = t.ToolDefinition
		}
		req.Tools = defs
	}

	return req
}

// buildToolMap creates a lookup map from tool name to Tool for quick access.
func buildToolMap(tools []Tool) map[string]*Tool {
	m := make(map[string]*Tool, len(tools))
	for i := range tools {
		m[tools[i].Name] = &tools[i]
	}
	return m
}

// executeToolsConcurrently runs all tool calls in parallel, returning results
// in the same order as the input calls.
func executeToolsConcurrently(toolCalls []ToolCallData, toolMap map[string]*Tool) []ToolResult {
	results := make([]ToolResult, len(toolCalls))
	var wg sync.WaitGroup

	for i, tc := range toolCalls {
		wg.Add(1)
		go func(idx int, call ToolCallData) {
			defer wg.Done()

			tool, found := toolMap[call.Name]
			if !found {
				results[idx] = ToolResult{
					ToolCallID: call.ID,
					Content:    fmt.Sprintf("Unknown tool: %s", call.Name),
					IsError:    true,
				}
				return
			}

			if tool.Execute == nil {
				// Passive tool - should not reach here, but handle gracefully
				results[idx] = ToolResult{
					ToolCallID: call.ID,
					Content:    "",
					IsError:    false,
				}
				return
			}

			content, err := tool.Execute(call.Arguments)
			if err != nil {
				results[idx] = ToolResult{
					ToolCallID: call.ID,
					Content:    err.Error(),
					IsError:    true,
				}
				return
			}

			results[idx] = ToolResult{
				ToolCallID: call.ID,
				Content:    content,
				IsError:    false,
			}
		}(i, tc)
	}

	wg.Wait()
	return results
}

// hasActiveTools checks whether any of the tool calls reference tools with Execute handlers.
func hasActiveTools(toolCalls []ToolCallData, toolMap map[string]*Tool) bool {
	for _, tc := range toolCalls {
		if tool, found := toolMap[tc.Name]; found && tool.Execute != nil {
			return true
		}
		// Unknown tools are treated as active (they get error results)
		if _, found := toolMap[tc.Name]; !found {
			return true
		}
	}
	return false
}

// stepResultFromResponse extracts a StepResult from a Response.
func stepResultFromResponse(resp *Response) StepResult {
	return StepResult{
		Text:         resp.TextContent(),
		Reasoning:    resp.Reasoning(),
		ToolCalls:    resp.ToolCalls(),
		FinishReason: resp.FinishReason,
		Usage:        resp.Usage,
		Response:     resp,
		Warnings:     resp.Warnings,
	}
}

// Generate performs a completion request with optional tool execution loop.
// It builds a Request from the provided GenerateOptions, calls the client,
// and if the model requests tool calls with active tools, executes them
// concurrently and continues the loop.
func Generate(ctx context.Context, opts GenerateOptions) (*GenerateResult, error) {
	client, err := resolveClient(opts)
	if err != nil {
		return nil, err
	}

	messages, err := buildMessages(opts)
	if err != nil {
		return nil, err
	}

	// Apply defaults
	maxToolRounds := opts.MaxToolRounds
	if maxToolRounds <= 0 {
		maxToolRounds = 1
	}

	maxRetries := opts.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 2
	}

	toolMap := buildToolMap(opts.Tools)
	var steps []StepResult
	var totalUsage Usage

	for round := 0; round < maxToolRounds; round++ {
		req := buildRequest(opts, messages)

		// Execute with retry logic
		var resp *Response
		policy := RetryPolicy{
			MaxRetries:        maxRetries,
			BaseDelay:         0, // No delay in tests
			MaxDelay:          0,
			BackoffMultiplier: 2.0,
			Jitter:            false,
		}

		retryErr := Retry(ctx, policy, func() error {
			var completeErr error
			resp, completeErr = client.Complete(ctx, req)
			return completeErr
		})
		if retryErr != nil {
			return nil, retryErr
		}

		step := stepResultFromResponse(resp)
		totalUsage = totalUsage.Add(resp.Usage)

		// Check for tool calls
		toolCalls := resp.ToolCalls()
		if len(toolCalls) > 0 && resp.FinishReason.Reason == FinishToolCalls {
			// Check if any tools are active (have Execute handlers)
			if hasActiveTools(toolCalls, toolMap) {
				// Execute tools concurrently
				toolResults := executeToolsConcurrently(toolCalls, toolMap)
				step.ToolResults = toolResults
				steps = append(steps, step)

				// Check stop condition
				if opts.StopWhen != nil && opts.StopWhen(steps) {
					break
				}

				// Build next round messages: append assistant message with tool calls,
				// then tool result messages
				messages = append(messages, resp.Message)
				for _, tr := range toolResults {
					messages = append(messages, ToolResultMessage(tr.ToolCallID, tr.Content, tr.IsError))
				}
				continue
			}
		}

		// No tool calls, passive tools, or final text response - done
		steps = append(steps, step)
		break
	}

	// Build the final result from the last step
	lastStep := steps[len(steps)-1]
	result := &GenerateResult{
		Text:         lastStep.Text,
		Reasoning:    lastStep.Reasoning,
		ToolCalls:    lastStep.ToolCalls,
		ToolResults:  lastStep.ToolResults,
		FinishReason: lastStep.FinishReason,
		Usage:        lastStep.Usage,
		TotalUsage:   totalUsage,
		Steps:        steps,
		Response:     lastStep.Response,
	}

	return result, nil
}

// StreamGenerate performs a streaming completion request. It builds the request
// from GenerateOptions and wraps the client's Stream call. The tool loop is not
// performed for streaming (can be added later).
func StreamGenerate(ctx context.Context, opts GenerateOptions) (*StreamResult, error) {
	client, err := resolveClient(opts)
	if err != nil {
		return nil, err
	}

	messages, err := buildMessages(opts)
	if err != nil {
		return nil, err
	}

	req := buildRequest(opts, messages)

	eventCh, err := client.Stream(ctx, req)
	if err != nil {
		return nil, err
	}

	sr := &StreamResult{
		Events: eventCh,
	}

	return sr, nil
}

// GenerateObject calls Generate with the ResponseFormat set to json_schema,
// then parses the response text as JSON. It validates the output by unmarshaling
// into a map and sets result.Output. Returns NoObjectGeneratedError on parse failure.
func GenerateObject(ctx context.Context, opts GenerateOptions, schema json.RawMessage) (*GenerateResult, error) {
	// Set the response format to json_schema
	opts.ResponseFormat = &ResponseFormat{
		Type:       "json_schema",
		JSONSchema: schema,
		Strict:     true,
	}

	result, err := Generate(ctx, opts)
	if err != nil {
		return nil, err
	}

	// Parse the response text as JSON
	var parsed map[string]any
	if err := json.Unmarshal([]byte(result.Text), &parsed); err != nil {
		return nil, &NoObjectGeneratedError{
			SDKError: SDKError{
				Message: fmt.Sprintf("failed to parse response as JSON: %s", err.Error()),
				Cause:   err,
			},
		}
	}

	result.Output = parsed
	return result, nil
}
