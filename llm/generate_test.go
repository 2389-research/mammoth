// ABOUTME: Tests for the high-level Generate, StreamGenerate, and GenerateObject API functions.
// ABOUTME: Validates tool loops, parallel execution, stop conditions, streaming accumulation, and structured output.

package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// generateTestAdapter extends testAdapter with support for multiple sequential responses.
// This enables testing multi-step tool loops where each Complete call returns a different response.
type generateTestAdapter struct {
	name          string
	responses     []*Response
	errors        []error
	callIndex     int
	completeCalls []Request
	streamEvents  []StreamEvent
	streamErr     error
	closed        bool
	mu            sync.Mutex
}

func newGenerateTestAdapter(name string) *generateTestAdapter {
	return &generateTestAdapter{
		name: name,
	}
}

func (a *generateTestAdapter) Name() string { return a.name }

func (a *generateTestAdapter) Complete(ctx context.Context, req Request) (*Response, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.completeCalls = append(a.completeCalls, req)

	idx := a.callIndex
	a.callIndex++

	if idx < len(a.errors) && a.errors[idx] != nil {
		return nil, a.errors[idx]
	}

	if idx < len(a.responses) {
		return a.responses[idx], nil
	}

	// Default fallback response
	return &Response{
		ID:           fmt.Sprintf("resp-%s-%d", a.name, idx),
		Model:        "test-model",
		Provider:     a.name,
		Message:      AssistantMessage("default response"),
		FinishReason: FinishReason{Reason: FinishStop},
	}, nil
}

func (a *generateTestAdapter) Stream(ctx context.Context, req Request) (<-chan StreamEvent, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.streamErr != nil {
		return nil, a.streamErr
	}
	ch := make(chan StreamEvent, len(a.streamEvents))
	for _, evt := range a.streamEvents {
		ch <- evt
	}
	close(ch)
	return ch, nil
}

func (a *generateTestAdapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closed = true
	return nil
}

func (a *generateTestAdapter) getCompleteCalls() []Request {
	a.mu.Lock()
	defer a.mu.Unlock()
	result := make([]Request, len(a.completeCalls))
	copy(result, a.completeCalls)
	return result
}

// makeToolCallResponse creates a response that contains tool calls with the given finish reason.
func makeToolCallResponse(id string, toolCalls []ToolCallData, usage Usage) *Response {
	parts := make([]ContentPart, len(toolCalls))
	for i, tc := range toolCalls {
		parts[i] = ContentPart{
			Kind:     ContentToolCall,
			ToolCall: &tc,
		}
	}
	return &Response{
		ID:       id,
		Model:    "test-model",
		Provider: "test",
		Message: Message{
			Role:    RoleAssistant,
			Content: parts,
		},
		FinishReason: FinishReason{Reason: FinishToolCalls},
		Usage:        usage,
	}
}

// makeTextResponse creates a simple text response.
func makeTextResponse(id, text string, usage Usage) *Response {
	return &Response{
		ID:           id,
		Model:        "test-model",
		Provider:     "test",
		Message:      AssistantMessage(text),
		FinishReason: FinishReason{Reason: FinishStop},
		Usage:        usage,
	}
}

// TestGenerateSimpleText verifies basic text generation with a simple prompt.
func TestGenerateSimpleText(t *testing.T) {
	adapter := newGenerateTestAdapter("test")
	adapter.responses = []*Response{
		makeTextResponse("resp-1", "Hello, world!", Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}),
	}

	client := NewClient(WithProvider("test", adapter), WithDefaultProvider("test"))

	result, err := Generate(context.Background(), GenerateOptions{
		Client:   client,
		Model:    "test-model",
		Prompt:   "Say hello",
		Provider: "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Text != "Hello, world!" {
		t.Errorf("expected text 'Hello, world!', got %q", result.Text)
	}
	if result.FinishReason.Reason != FinishStop {
		t.Errorf("expected finish reason 'stop', got %q", result.FinishReason.Reason)
	}
	if result.Usage.InputTokens != 10 {
		t.Errorf("expected 10 input tokens, got %d", result.Usage.InputTokens)
	}
}

// TestGenerateWithPrompt verifies that a Prompt string is converted to a UserMessage.
func TestGenerateWithPrompt(t *testing.T) {
	adapter := newGenerateTestAdapter("test")
	adapter.responses = []*Response{
		makeTextResponse("resp-1", "response", Usage{}),
	}

	client := NewClient(WithProvider("test", adapter), WithDefaultProvider("test"))

	_, err := Generate(context.Background(), GenerateOptions{
		Client:   client,
		Model:    "test-model",
		Prompt:   "test prompt",
		Provider: "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := adapter.getCompleteCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}

	msgs := calls[0].Messages
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Role != RoleUser {
		t.Errorf("expected user role, got %q", msgs[0].Role)
	}
	if msgs[0].TextContent() != "test prompt" {
		t.Errorf("expected 'test prompt', got %q", msgs[0].TextContent())
	}
}

// TestGenerateWithMessages verifies that Messages are passed through directly.
func TestGenerateWithMessages(t *testing.T) {
	adapter := newGenerateTestAdapter("test")
	adapter.responses = []*Response{
		makeTextResponse("resp-1", "response", Usage{}),
	}

	client := NewClient(WithProvider("test", adapter), WithDefaultProvider("test"))

	messages := []Message{
		UserMessage("first"),
		AssistantMessage("response1"),
		UserMessage("second"),
	}

	_, err := Generate(context.Background(), GenerateOptions{
		Client:   client,
		Model:    "test-model",
		Messages: messages,
		Provider: "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := adapter.getCompleteCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if len(calls[0].Messages) != 3 {
		t.Errorf("expected 3 messages, got %d", len(calls[0].Messages))
	}
}

// TestGenerateBothPromptAndMessages verifies that setting both Prompt and Messages returns an error.
func TestGenerateBothPromptAndMessages(t *testing.T) {
	adapter := newGenerateTestAdapter("test")
	client := NewClient(WithProvider("test", adapter), WithDefaultProvider("test"))

	_, err := Generate(context.Background(), GenerateOptions{
		Client:   client,
		Model:    "test-model",
		Prompt:   "hello",
		Messages: []Message{UserMessage("world")},
		Provider: "test",
	})
	if err == nil {
		t.Fatal("expected error when both Prompt and Messages are set")
	}

	var invErr *InvalidRequestError
	if ok := isInvalidRequestError(err); !ok {
		// Check for ConfigurationError too since it could be either
		var cfgErr *ConfigurationError
		if ok2 := isConfigurationError(err); !ok2 {
			t.Errorf("expected InvalidRequestError or ConfigurationError, got %T: %v (invErr=%v, cfgErr=%v)", err, err, invErr, cfgErr)
		}
	}
}

func isInvalidRequestError(err error) bool {
	var e *InvalidRequestError
	return errorAs(err, &e)
}

func isConfigurationError(err error) bool {
	var e *ConfigurationError
	return errorAs(err, &e)
}

func errorAs[T any](err error, target *T) bool {
	// Manual type assertion since errors.As has issues with our embedded types
	switch err.(type) {
	case *InvalidRequestError:
		return true
	case *ConfigurationError:
		return true
	}
	return false
}

// TestGenerateWithSystemMessage verifies that the System option is prepended as a SystemMessage.
func TestGenerateWithSystemMessage(t *testing.T) {
	adapter := newGenerateTestAdapter("test")
	adapter.responses = []*Response{
		makeTextResponse("resp-1", "response", Usage{}),
	}

	client := NewClient(WithProvider("test", adapter), WithDefaultProvider("test"))

	_, err := Generate(context.Background(), GenerateOptions{
		Client:   client,
		Model:    "test-model",
		Prompt:   "hello",
		System:   "You are helpful.",
		Provider: "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := adapter.getCompleteCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}

	msgs := calls[0].Messages
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (system + user), got %d", len(msgs))
	}
	if msgs[0].Role != RoleSystem {
		t.Errorf("expected first message to be system, got %q", msgs[0].Role)
	}
	if msgs[0].TextContent() != "You are helpful." {
		t.Errorf("expected system text 'You are helpful.', got %q", msgs[0].TextContent())
	}
	if msgs[1].Role != RoleUser {
		t.Errorf("expected second message to be user, got %q", msgs[1].Role)
	}
}

// TestGenerateToolLoop verifies the tool execution loop with active tools.
func TestGenerateToolLoop(t *testing.T) {
	adapter := newGenerateTestAdapter("test")

	// First response: model wants to call a tool
	adapter.responses = []*Response{
		makeToolCallResponse("resp-1", []ToolCallData{
			{ID: "call-1", Name: "add", Arguments: json.RawMessage(`{"a": 1, "b": 2}`)},
		}, Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}),
		// Second response: model produces final text after seeing tool result
		makeTextResponse("resp-2", "The sum is 3", Usage{InputTokens: 20, OutputTokens: 8, TotalTokens: 28}),
	}

	client := NewClient(WithProvider("test", adapter), WithDefaultProvider("test"))

	addTool := Tool{
		ToolDefinition: ToolDefinition{
			Name:        "add",
			Description: "Add two numbers",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"a":{"type":"number"},"b":{"type":"number"}}}`),
		},
		Execute: func(args json.RawMessage) (string, error) {
			var params struct {
				A float64 `json:"a"`
				B float64 `json:"b"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return "", err
			}
			return fmt.Sprintf("%.0f", params.A+params.B), nil
		},
	}

	result, err := Generate(context.Background(), GenerateOptions{
		Client:        client,
		Model:         "test-model",
		Prompt:        "What is 1 + 2?",
		Tools:         []Tool{addTool},
		MaxToolRounds: 5,
		Provider:      "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Text != "The sum is 3" {
		t.Errorf("expected final text 'The sum is 3', got %q", result.Text)
	}

	if len(result.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(result.Steps))
	}

	// Verify tool results were collected
	if len(result.Steps[0].ToolCalls) != 1 {
		t.Errorf("expected 1 tool call in step 0, got %d", len(result.Steps[0].ToolCalls))
	}
	if len(result.Steps[0].ToolResults) != 1 {
		t.Errorf("expected 1 tool result in step 0, got %d", len(result.Steps[0].ToolResults))
	}
	if result.Steps[0].ToolResults[0].Content != "3" {
		t.Errorf("expected tool result '3', got %q", result.Steps[0].ToolResults[0].Content)
	}

	// Verify the second call included tool results in messages
	calls := adapter.getCompleteCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 complete calls, got %d", len(calls))
	}
	// Second call should have user msg + assistant tool call msg + tool result msg
	secondCallMsgs := calls[1].Messages
	if len(secondCallMsgs) < 3 {
		t.Errorf("expected at least 3 messages in second call, got %d", len(secondCallMsgs))
	}
}

// TestGenerateParallelToolExecution verifies that multiple tool calls are executed concurrently.
func TestGenerateParallelToolExecution(t *testing.T) {
	adapter := newGenerateTestAdapter("test")

	adapter.responses = []*Response{
		makeToolCallResponse("resp-1", []ToolCallData{
			{ID: "call-1", Name: "slow_op", Arguments: json.RawMessage(`{"id": "first"}`)},
			{ID: "call-2", Name: "slow_op", Arguments: json.RawMessage(`{"id": "second"}`)},
			{ID: "call-3", Name: "slow_op", Arguments: json.RawMessage(`{"id": "third"}`)},
		}, Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}),
		makeTextResponse("resp-2", "All done", Usage{InputTokens: 20, OutputTokens: 5, TotalTokens: 25}),
	}

	client := NewClient(WithProvider("test", adapter), WithDefaultProvider("test"))

	var concurrentCount atomic.Int32
	var maxConcurrent atomic.Int32

	slowTool := Tool{
		ToolDefinition: ToolDefinition{
			Name:        "slow_op",
			Description: "A slow operation",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}}}`),
		},
		Execute: func(args json.RawMessage) (string, error) {
			current := concurrentCount.Add(1)
			// Track max concurrency
			for {
				old := maxConcurrent.Load()
				if current <= old || maxConcurrent.CompareAndSwap(old, current) {
					break
				}
			}
			time.Sleep(50 * time.Millisecond)
			concurrentCount.Add(-1)
			var params struct {
				ID string `json:"id"`
			}
			json.Unmarshal(args, &params)
			return "done-" + params.ID, nil
		},
	}

	result, err := Generate(context.Background(), GenerateOptions{
		Client:        client,
		Model:         "test-model",
		Prompt:        "Run all ops",
		Tools:         []Tool{slowTool},
		MaxToolRounds: 5,
		Provider:      "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify all 3 tool results were collected
	if len(result.Steps[0].ToolResults) != 3 {
		t.Fatalf("expected 3 tool results, got %d", len(result.Steps[0].ToolResults))
	}

	// Verify results are in the correct order (matching tool calls)
	if result.Steps[0].ToolResults[0].ToolCallID != "call-1" {
		t.Errorf("expected first result for call-1, got %s", result.Steps[0].ToolResults[0].ToolCallID)
	}
	if result.Steps[0].ToolResults[1].ToolCallID != "call-2" {
		t.Errorf("expected second result for call-2, got %s", result.Steps[0].ToolResults[1].ToolCallID)
	}
	if result.Steps[0].ToolResults[2].ToolCallID != "call-3" {
		t.Errorf("expected third result for call-3, got %s", result.Steps[0].ToolResults[2].ToolCallID)
	}

	// Verify concurrency happened (at least 2 were running simultaneously)
	if maxConcurrent.Load() < 2 {
		t.Errorf("expected parallel execution (max concurrent >= 2), got %d", maxConcurrent.Load())
	}

	if result.Text != "All done" {
		t.Errorf("expected final text 'All done', got %q", result.Text)
	}
}

// TestGenerateToolError verifies that tool execution errors are handled gracefully
// (is_error=true in the result, don't abort the batch).
func TestGenerateToolError(t *testing.T) {
	adapter := newGenerateTestAdapter("test")

	adapter.responses = []*Response{
		makeToolCallResponse("resp-1", []ToolCallData{
			{ID: "call-1", Name: "failing_tool", Arguments: json.RawMessage(`{}`)},
		}, Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}),
		makeTextResponse("resp-2", "I see the tool failed", Usage{InputTokens: 20, OutputTokens: 8, TotalTokens: 28}),
	}

	client := NewClient(WithProvider("test", adapter), WithDefaultProvider("test"))

	failingTool := Tool{
		ToolDefinition: ToolDefinition{
			Name:        "failing_tool",
			Description: "A tool that fails",
			Parameters:  json.RawMessage(`{"type":"object"}`),
		},
		Execute: func(args json.RawMessage) (string, error) {
			return "", fmt.Errorf("something went wrong")
		},
	}

	result, err := Generate(context.Background(), GenerateOptions{
		Client:        client,
		Model:         "test-model",
		Prompt:        "Use the tool",
		Tools:         []Tool{failingTool},
		MaxToolRounds: 5,
		Provider:      "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The tool result should be marked as an error
	if len(result.Steps[0].ToolResults) != 1 {
		t.Fatalf("expected 1 tool result, got %d", len(result.Steps[0].ToolResults))
	}
	if !result.Steps[0].ToolResults[0].IsError {
		t.Error("expected tool result to be marked as error")
	}
	if result.Steps[0].ToolResults[0].Content != "something went wrong" {
		t.Errorf("expected error message 'something went wrong', got %q", result.Steps[0].ToolResults[0].Content)
	}

	// Should still have completed with text
	if result.Text != "I see the tool failed" {
		t.Errorf("expected final text 'I see the tool failed', got %q", result.Text)
	}
}

// TestGenerateUnknownTool verifies that an unknown tool gets an error result.
func TestGenerateUnknownTool(t *testing.T) {
	adapter := newGenerateTestAdapter("test")

	adapter.responses = []*Response{
		makeToolCallResponse("resp-1", []ToolCallData{
			{ID: "call-1", Name: "nonexistent_tool", Arguments: json.RawMessage(`{}`)},
		}, Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}),
		makeTextResponse("resp-2", "I couldn't find that tool", Usage{InputTokens: 20, OutputTokens: 8, TotalTokens: 28}),
	}

	client := NewClient(WithProvider("test", adapter), WithDefaultProvider("test"))

	result, err := Generate(context.Background(), GenerateOptions{
		Client:        client,
		Model:         "test-model",
		Prompt:        "Use the tool",
		Tools:         []Tool{},
		MaxToolRounds: 5,
		Provider:      "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Steps[0].ToolResults) != 1 {
		t.Fatalf("expected 1 tool result, got %d", len(result.Steps[0].ToolResults))
	}
	if !result.Steps[0].ToolResults[0].IsError {
		t.Error("expected tool result to be marked as error")
	}
	if result.Steps[0].ToolResults[0].Content != "Unknown tool: nonexistent_tool" {
		t.Errorf("expected 'Unknown tool: nonexistent_tool', got %q", result.Steps[0].ToolResults[0].Content)
	}
}

// TestGenerateMaxToolRounds verifies that the loop stops at max_tool_rounds.
func TestGenerateMaxToolRounds(t *testing.T) {
	adapter := newGenerateTestAdapter("test")

	// Return tool calls indefinitely
	for i := 0; i < 10; i++ {
		adapter.responses = append(adapter.responses, makeToolCallResponse(
			fmt.Sprintf("resp-%d", i),
			[]ToolCallData{{ID: fmt.Sprintf("call-%d", i), Name: "echo", Arguments: json.RawMessage(`{"msg":"hi"}`)}},
			Usage{InputTokens: 5, OutputTokens: 3, TotalTokens: 8},
		))
	}

	client := NewClient(WithProvider("test", adapter), WithDefaultProvider("test"))

	echoTool := Tool{
		ToolDefinition: ToolDefinition{
			Name:        "echo",
			Description: "Echo a message",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"msg":{"type":"string"}}}`),
		},
		Execute: func(args json.RawMessage) (string, error) {
			return "echoed", nil
		},
	}

	result, err := Generate(context.Background(), GenerateOptions{
		Client:        client,
		Model:         "test-model",
		Prompt:        "Keep echoing",
		Tools:         []Tool{echoTool},
		MaxToolRounds: 3,
		Provider:      "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have stopped after 3 rounds + initial = 3 steps total (3 tool rounds each producing a step)
	// Actually MaxToolRounds=3 means at most 3 complete() calls that result in tool calls
	if len(result.Steps) != 3 {
		t.Errorf("expected 3 steps (max tool rounds), got %d", len(result.Steps))
	}
}

// TestGenerateStopCondition verifies that a custom StopCondition stops the loop.
func TestGenerateStopCondition(t *testing.T) {
	adapter := newGenerateTestAdapter("test")

	// Return tool calls indefinitely
	for i := 0; i < 10; i++ {
		adapter.responses = append(adapter.responses, makeToolCallResponse(
			fmt.Sprintf("resp-%d", i),
			[]ToolCallData{{ID: fmt.Sprintf("call-%d", i), Name: "counter", Arguments: json.RawMessage(`{}`)}},
			Usage{InputTokens: 5, OutputTokens: 3, TotalTokens: 8},
		))
	}

	client := NewClient(WithProvider("test", adapter), WithDefaultProvider("test"))

	callCount := 0
	counterTool := Tool{
		ToolDefinition: ToolDefinition{
			Name:        "counter",
			Description: "A counter",
			Parameters:  json.RawMessage(`{"type":"object"}`),
		},
		Execute: func(args json.RawMessage) (string, error) {
			callCount++
			return fmt.Sprintf("count=%d", callCount), nil
		},
	}

	// Stop after 2 steps
	stopAfter2 := func(steps []StepResult) bool {
		return len(steps) >= 2
	}

	result, err := Generate(context.Background(), GenerateOptions{
		Client:        client,
		Model:         "test-model",
		Prompt:        "Keep counting",
		Tools:         []Tool{counterTool},
		MaxToolRounds: 10,
		StopWhen:      stopAfter2,
		Provider:      "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Steps) != 2 {
		t.Errorf("expected 2 steps (stop condition), got %d", len(result.Steps))
	}
}

// TestGeneratePassiveTools verifies that tools without Execute don't trigger the loop.
func TestGeneratePassiveTools(t *testing.T) {
	adapter := newGenerateTestAdapter("test")

	adapter.responses = []*Response{
		makeToolCallResponse("resp-1", []ToolCallData{
			{ID: "call-1", Name: "passive_tool", Arguments: json.RawMessage(`{"data":"test"}`)},
		}, Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}),
	}

	client := NewClient(WithProvider("test", adapter), WithDefaultProvider("test"))

	passiveTool := Tool{
		ToolDefinition: ToolDefinition{
			Name:        "passive_tool",
			Description: "A passive tool with no execute handler",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"data":{"type":"string"}}}`),
		},
		// No Execute handler
	}

	result, err := Generate(context.Background(), GenerateOptions{
		Client:        client,
		Model:         "test-model",
		Prompt:        "Use the tool",
		Tools:         []Tool{passiveTool},
		MaxToolRounds: 5,
		Provider:      "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have only 1 step - no looping for passive tools
	if len(result.Steps) != 1 {
		t.Errorf("expected 1 step for passive tool, got %d", len(result.Steps))
	}

	// Tool calls should still be in the result
	if len(result.ToolCalls) != 1 {
		t.Errorf("expected 1 tool call in result, got %d", len(result.ToolCalls))
	}
	if result.ToolCalls[0].Name != "passive_tool" {
		t.Errorf("expected tool call name 'passive_tool', got %q", result.ToolCalls[0].Name)
	}

	// No tool results since it's passive
	if len(result.ToolResults) != 0 {
		t.Errorf("expected 0 tool results for passive tool, got %d", len(result.ToolResults))
	}

	// Should only have made 1 call to the adapter
	calls := adapter.getCompleteCalls()
	if len(calls) != 1 {
		t.Errorf("expected 1 adapter call, got %d", len(calls))
	}
}

// TestGenerateTotalUsage verifies that usage is aggregated across all steps.
func TestGenerateTotalUsage(t *testing.T) {
	adapter := newGenerateTestAdapter("test")

	adapter.responses = []*Response{
		makeToolCallResponse("resp-1", []ToolCallData{
			{ID: "call-1", Name: "add", Arguments: json.RawMessage(`{"a":1,"b":2}`)},
		}, Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}),
		makeTextResponse("resp-2", "Result is 3", Usage{InputTokens: 20, OutputTokens: 8, TotalTokens: 28}),
	}

	client := NewClient(WithProvider("test", adapter), WithDefaultProvider("test"))

	addTool := Tool{
		ToolDefinition: ToolDefinition{
			Name:        "add",
			Description: "Add",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"a":{"type":"number"},"b":{"type":"number"}}}`),
		},
		Execute: func(args json.RawMessage) (string, error) {
			return "3", nil
		},
	}

	result, err := Generate(context.Background(), GenerateOptions{
		Client:        client,
		Model:         "test-model",
		Prompt:        "Add 1+2",
		Tools:         []Tool{addTool},
		MaxToolRounds: 5,
		Provider:      "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.TotalUsage.InputTokens != 30 {
		t.Errorf("expected total input tokens 30, got %d", result.TotalUsage.InputTokens)
	}
	if result.TotalUsage.OutputTokens != 13 {
		t.Errorf("expected total output tokens 13, got %d", result.TotalUsage.OutputTokens)
	}
	if result.TotalUsage.TotalTokens != 43 {
		t.Errorf("expected total tokens 43, got %d", result.TotalUsage.TotalTokens)
	}
}

// TestGenerateDefaultClient verifies that Generate uses GetDefaultClient when no Client is specified.
func TestGenerateDefaultClient(t *testing.T) {
	adapter := newGenerateTestAdapter("test")
	adapter.responses = []*Response{
		makeTextResponse("resp-1", "from default", Usage{}),
	}

	client := NewClient(WithProvider("test", adapter), WithDefaultProvider("test"))
	SetDefaultClient(client)
	defer SetDefaultClient(nil)

	result, err := Generate(context.Background(), GenerateOptions{
		Model:    "test-model",
		Prompt:   "hello",
		Provider: "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Text != "from default" {
		t.Errorf("expected 'from default', got %q", result.Text)
	}
}

// TestStreamAccumulator verifies that StreamAccumulator collects events into a Response.
func TestStreamAccumulator(t *testing.T) {
	acc := NewStreamAccumulator()

	acc.Process(StreamEvent{Type: StreamStart})
	acc.Process(StreamEvent{Type: StreamTextDelta, Delta: "Hello"})
	acc.Process(StreamEvent{Type: StreamTextDelta, Delta: ", "})
	acc.Process(StreamEvent{Type: StreamTextDelta, Delta: "world!"})
	acc.Process(StreamEvent{Type: StreamTextEnd})

	toolCall := &ToolCall{
		ID:        "call-1",
		Name:      "test_tool",
		Arguments: json.RawMessage(`{"key":"value"}`),
	}
	acc.Process(StreamEvent{Type: StreamToolStart, ToolCall: toolCall})
	acc.Process(StreamEvent{Type: StreamToolDelta, ToolCall: &ToolCall{ID: "call-1", Arguments: json.RawMessage(`"more"`)}})
	acc.Process(StreamEvent{Type: StreamToolEnd, ToolCall: toolCall})

	usage := Usage{InputTokens: 10, OutputTokens: 20, TotalTokens: 30}
	finishReason := FinishReason{Reason: FinishStop}
	acc.Process(StreamEvent{
		Type:         StreamFinish,
		Usage:        &usage,
		FinishReason: &finishReason,
	})

	resp := acc.Response()
	if resp == nil {
		t.Fatal("expected non-nil response from accumulator")
	}

	text := resp.TextContent()
	if text != "Hello, world!" {
		t.Errorf("expected accumulated text 'Hello, world!', got %q", text)
	}

	if resp.FinishReason.Reason != FinishStop {
		t.Errorf("expected finish reason 'stop', got %q", resp.FinishReason.Reason)
	}

	if resp.Usage.InputTokens != 10 {
		t.Errorf("expected 10 input tokens, got %d", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 20 {
		t.Errorf("expected 20 output tokens, got %d", resp.Usage.OutputTokens)
	}
}

// TestGenerateObject verifies structured output with JSON parsing.
func TestGenerateObject(t *testing.T) {
	adapter := newGenerateTestAdapter("test")

	adapter.responses = []*Response{
		makeTextResponse("resp-1", `{"name":"Alice","age":30}`, Usage{InputTokens: 10, OutputTokens: 15, TotalTokens: 25}),
	}

	client := NewClient(WithProvider("test", adapter), WithDefaultProvider("test"))

	schema := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"age":{"type":"number"}},"required":["name","age"]}`)

	result, err := GenerateObject(context.Background(), GenerateOptions{
		Client:   client,
		Model:    "test-model",
		Prompt:   "Generate a person",
		Provider: "test",
	}, schema)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify Output was parsed
	output, ok := result.Output.(map[string]any)
	if !ok {
		t.Fatalf("expected Output to be map[string]any, got %T", result.Output)
	}
	if output["name"] != "Alice" {
		t.Errorf("expected name 'Alice', got %v", output["name"])
	}
	if output["age"] != float64(30) {
		t.Errorf("expected age 30, got %v", output["age"])
	}

	// Verify that the request had ResponseFormat set
	calls := adapter.getCompleteCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].ResponseFormat == nil {
		t.Fatal("expected ResponseFormat to be set")
	}
	if calls[0].ResponseFormat.Type != "json_schema" {
		t.Errorf("expected response format type 'json_schema', got %q", calls[0].ResponseFormat.Type)
	}
}

// TestGenerateObjectInvalidJSON verifies that NoObjectGeneratedError is returned on bad JSON.
func TestGenerateObjectInvalidJSON(t *testing.T) {
	adapter := newGenerateTestAdapter("test")

	adapter.responses = []*Response{
		makeTextResponse("resp-1", "this is not json at all", Usage{}),
	}

	client := NewClient(WithProvider("test", adapter), WithDefaultProvider("test"))

	schema := json.RawMessage(`{"type":"object"}`)

	_, err := GenerateObject(context.Background(), GenerateOptions{
		Client:   client,
		Model:    "test-model",
		Prompt:   "Generate something",
		Provider: "test",
	}, schema)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}

	var noObjErr *NoObjectGeneratedError
	switch err.(type) {
	case *NoObjectGeneratedError:
		// expected
	default:
		t.Errorf("expected NoObjectGeneratedError, got %T: %v (target=%v)", err, err, noObjErr)
	}
}

// TestStreamGenerate verifies the basic streaming generation path.
func TestStreamGenerate(t *testing.T) {
	adapter := newGenerateTestAdapter("test")
	adapter.streamEvents = []StreamEvent{
		{Type: StreamStart},
		{Type: StreamTextDelta, Delta: "Hello"},
		{Type: StreamTextDelta, Delta: " there"},
		{Type: StreamFinish, FinishReason: &FinishReason{Reason: FinishStop}, Usage: &Usage{InputTokens: 5, OutputTokens: 3, TotalTokens: 8}},
	}

	client := NewClient(WithProvider("test", adapter), WithDefaultProvider("test"))

	sr, err := StreamGenerate(context.Background(), GenerateOptions{
		Client:   client,
		Model:    "test-model",
		Prompt:   "Say hello",
		Provider: "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var events []StreamEvent
	for evt := range sr.Events {
		events = append(events, evt)
	}

	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d", len(events))
	}
	if events[0].Type != StreamStart {
		t.Errorf("expected StreamStart, got %q", events[0].Type)
	}
	if events[1].Delta != "Hello" {
		t.Errorf("expected delta 'Hello', got %q", events[1].Delta)
	}
}

// TestGenerateNoClient verifies that Generate returns an error when no client is available.
func TestGenerateNoClient(t *testing.T) {
	SetDefaultClient(nil)
	defer SetDefaultClient(nil)

	_, err := Generate(context.Background(), GenerateOptions{
		Model:  "test-model",
		Prompt: "hello",
	})
	if err == nil {
		t.Fatal("expected error when no client is available")
	}
}

// TestGenerateWithReasoningContent verifies that reasoning content is extracted from responses.
func TestGenerateWithReasoningContent(t *testing.T) {
	adapter := newGenerateTestAdapter("test")

	resp := &Response{
		ID:       "resp-1",
		Model:    "test-model",
		Provider: "test",
		Message: Message{
			Role: RoleAssistant,
			Content: []ContentPart{
				ThinkingPart("Let me think about this...", "sig-1"),
				TextPart("The answer is 42"),
			},
		},
		FinishReason: FinishReason{Reason: FinishStop},
		Usage:        Usage{InputTokens: 10, OutputTokens: 15, TotalTokens: 25},
	}
	adapter.responses = []*Response{resp}

	client := NewClient(WithProvider("test", adapter), WithDefaultProvider("test"))

	result, err := Generate(context.Background(), GenerateOptions{
		Client:   client,
		Model:    "test-model",
		Prompt:   "What is the meaning of life?",
		Provider: "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Text != "The answer is 42" {
		t.Errorf("expected text 'The answer is 42', got %q", result.Text)
	}
	if result.Reasoning != "Let me think about this..." {
		t.Errorf("expected reasoning 'Let me think about this...', got %q", result.Reasoning)
	}
}

// TestGenerateOptionsPassthrough verifies that all GenerateOptions fields are properly
// passed through to the underlying Request.
func TestGenerateOptionsPassthrough(t *testing.T) {
	adapter := newGenerateTestAdapter("test")
	adapter.responses = []*Response{
		makeTextResponse("resp-1", "ok", Usage{}),
	}

	client := NewClient(WithProvider("test", adapter), WithDefaultProvider("test"))

	temp := 0.7
	topP := 0.9
	maxTokens := 100

	_, err := Generate(context.Background(), GenerateOptions{
		Client:          client,
		Model:           "test-model",
		Prompt:          "test",
		Provider:        "test",
		Temperature:     &temp,
		TopP:            &topP,
		MaxTokens:       &maxTokens,
		StopSequences:   []string{"STOP"},
		ReasoningEffort: "high",
		ProviderOptions: map[string]any{"custom": true},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := adapter.getCompleteCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	req := calls[0]

	if req.Model != "test-model" {
		t.Errorf("expected model 'test-model', got %q", req.Model)
	}
	if req.Temperature == nil || *req.Temperature != 0.7 {
		t.Errorf("expected temperature 0.7, got %v", req.Temperature)
	}
	if req.TopP == nil || *req.TopP != 0.9 {
		t.Errorf("expected top_p 0.9, got %v", req.TopP)
	}
	if req.MaxTokens == nil || *req.MaxTokens != 100 {
		t.Errorf("expected max_tokens 100, got %v", req.MaxTokens)
	}
	if len(req.StopSequences) != 1 || req.StopSequences[0] != "STOP" {
		t.Errorf("expected stop sequences [STOP], got %v", req.StopSequences)
	}
	if req.ReasoningEffort != "high" {
		t.Errorf("expected reasoning effort 'high', got %q", req.ReasoningEffort)
	}
	if req.ProviderOptions["custom"] != true {
		t.Errorf("expected provider option custom=true, got %v", req.ProviderOptions)
	}
}
