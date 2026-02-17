// ABOUTME: Tests for the core agentic loop that orchestrates LLM calls, tool execution, and session management.
// ABOUTME: Covers ProcessInput, tool execution (sequential/parallel), limits, steering, followup, and events.

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/2389-research/mammoth/llm"
)

// loopTestAdapter is a ProviderAdapter that returns pre-configured responses in sequence.
// It records all requests it receives for later verification.
type loopTestAdapter struct {
	responses []*llm.Response
	callIdx   int
	calls     []llm.Request
	mu        sync.Mutex
}

func (a *loopTestAdapter) Name() string { return "test" }

func (a *loopTestAdapter) Complete(ctx context.Context, req llm.Request) (*llm.Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, req)
	if a.callIdx >= len(a.responses) {
		return nil, fmt.Errorf("loopTestAdapter: no more responses (called %d times, only %d responses configured)", a.callIdx+1, len(a.responses))
	}
	resp := a.responses[a.callIdx]
	a.callIdx++
	return resp, nil
}

func (a *loopTestAdapter) Stream(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, error) {
	return nil, fmt.Errorf("streaming not implemented in test adapter")
}

func (a *loopTestAdapter) Close() error { return nil }

func (a *loopTestAdapter) getCalls() []llm.Request {
	a.mu.Lock()
	defer a.mu.Unlock()
	result := make([]llm.Request, len(a.calls))
	copy(result, a.calls)
	return result
}

// testProfile implements ProviderProfile for testing purposes.
type testProfile struct {
	id            string
	model         string
	systemPrompt  string
	toolDefs      []llm.ToolDefinition
	providerOpts  map[string]any
	registry      *ToolRegistry
	parallelTools bool
}

func (p *testProfile) ID() string    { return p.id }
func (p *testProfile) Model() string { return p.model }
func (p *testProfile) BuildSystemPrompt(env ExecutionEnvironment, projectDocs []string) string {
	return p.systemPrompt
}
func (p *testProfile) Tools() []llm.ToolDefinition     { return p.toolDefs }
func (p *testProfile) ProviderOptions() map[string]any { return p.providerOpts }
func (p *testProfile) ToolRegistry() *ToolRegistry     { return p.registry }
func (p *testProfile) SupportsParallelToolCalls() bool { return p.parallelTools }
func (p *testProfile) SupportsReasoning() bool         { return false }
func (p *testProfile) SupportsStreaming() bool         { return false }
func (p *testProfile) ContextWindowSize() int          { return 200000 }

// loopTestEnv implements ExecutionEnvironment for loop testing purposes.
type loopTestEnv struct {
	workDir string
	files   map[string]string
	mu      sync.Mutex
}

func newLoopTestEnv() *loopTestEnv {
	return &loopTestEnv{
		workDir: "/tmp/test-agent",
		files:   make(map[string]string),
	}
}

func (e *loopTestEnv) ReadFile(path string, offset, limit int) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	content, ok := e.files[path]
	if !ok {
		return "", fmt.Errorf("file not found: %s", path)
	}
	return content, nil
}

func (e *loopTestEnv) WriteFile(path string, content string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.files[path] = content
	return nil
}

func (e *loopTestEnv) FileExists(path string) (bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	_, ok := e.files[path]
	return ok, nil
}

func (e *loopTestEnv) ListDirectory(path string, depth int) ([]DirEntry, error) {
	return nil, nil
}

func (e *loopTestEnv) ExecCommand(command string, timeoutMs int, workingDir string, envVars map[string]string) (*ExecResult, error) {
	return &ExecResult{Stdout: "ok", ExitCode: 0, DurationMs: 10}, nil
}

func (e *loopTestEnv) Grep(pattern, path string, opts GrepOptions) (string, error) {
	return "", nil
}

func (e *loopTestEnv) Glob(pattern, path string) ([]string, error) {
	return nil, nil
}

func (e *loopTestEnv) Initialize() error        { return nil }
func (e *loopTestEnv) Cleanup() error           { return nil }
func (e *loopTestEnv) WorkingDirectory() string { return e.workDir }
func (e *loopTestEnv) Platform() string         { return "test" }
func (e *loopTestEnv) OSVersion() string        { return "1.0" }

// makeTextResponse creates a Response with text content and no tool calls.
func makeTextResponse(text string) *llm.Response {
	return &llm.Response{
		ID:           "resp-text",
		Model:        "test-model",
		Provider:     "test",
		Message:      llm.AssistantMessage(text),
		FinishReason: llm.FinishReason{Reason: llm.FinishStop},
		Usage:        llm.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
	}
}

// makeToolCallResponse creates a Response that includes tool calls.
func makeToolCallResponse(toolCalls ...llm.ToolCallData) *llm.Response {
	parts := make([]llm.ContentPart, 0, len(toolCalls)+1)
	parts = append(parts, llm.TextPart("I'll use some tools."))
	for _, tc := range toolCalls {
		parts = append(parts, llm.ToolCallPart(tc.ID, tc.Name, tc.Arguments))
	}
	return &llm.Response{
		ID:       "resp-tools",
		Model:    "test-model",
		Provider: "test",
		Message: llm.Message{
			Role:    llm.RoleAssistant,
			Content: parts,
		},
		FinishReason: llm.FinishReason{Reason: llm.FinishToolCalls},
		Usage:        llm.Usage{InputTokens: 10, OutputTokens: 20, TotalTokens: 30},
	}
}

// newTestSetup creates a common test setup with profile, env, session, client, and adapter.
func newTestSetup() (*testProfile, *loopTestEnv, *Session, *llm.Client, *loopTestAdapter) {
	registry := NewToolRegistry()
	registry.Register(&RegisteredTool{
		Definition: llm.ToolDefinition{
			Name:        "echo_tool",
			Description: "Echoes the input",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"message":{"type":"string"}},"required":["message"]}`),
		},
		Execute: func(args map[string]any, env ExecutionEnvironment) (string, error) {
			msg, _ := args["message"].(string)
			return "echo: " + msg, nil
		},
	})

	profile := &testProfile{
		id:            "test",
		model:         "test-model",
		systemPrompt:  "You are a test assistant.",
		toolDefs:      registry.Definitions(),
		registry:      registry,
		parallelTools: false,
	}

	env := newLoopTestEnv()
	config := DefaultSessionConfig()
	session := NewSession(config)

	adapter := &loopTestAdapter{}
	client := llm.NewClient(
		llm.WithProvider("test", adapter),
		llm.WithDefaultProvider("test"),
	)

	return profile, env, session, client, adapter
}

func TestProcessInputSimpleText(t *testing.T) {
	profile, env, session, client, adapter := newTestSetup()
	defer session.Close()

	// Model returns a simple text response with no tool calls.
	adapter.responses = []*llm.Response{makeTextResponse("Hello there!")}

	err := ProcessInput(context.Background(), session, profile, env, client, "Hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have a user turn + assistant turn in history
	if session.TurnCount() != 2 {
		t.Fatalf("expected 2 turns, got %d", session.TurnCount())
	}

	// First turn is user
	userTurn, ok := session.History[0].(UserTurn)
	if !ok {
		t.Fatalf("expected UserTurn, got %T", session.History[0])
	}
	if userTurn.Content != "Hi" {
		t.Errorf("expected user content 'Hi', got %q", userTurn.Content)
	}

	// Second turn is assistant
	assistantTurn, ok := session.History[1].(AssistantTurn)
	if !ok {
		t.Fatalf("expected AssistantTurn, got %T", session.History[1])
	}
	if assistantTurn.Content != "Hello there!" {
		t.Errorf("expected assistant content 'Hello there!', got %q", assistantTurn.Content)
	}

	// Session should be IDLE after processing
	if session.State != StateIdle {
		t.Errorf("expected state %s, got %s", StateIdle, session.State)
	}
}

func TestProcessInputToolLoop(t *testing.T) {
	profile, env, session, client, adapter := newTestSetup()
	defer session.Close()

	// First response: model calls echo_tool
	// Second response: model returns text (natural completion)
	args := json.RawMessage(`{"message":"hello"}`)
	adapter.responses = []*llm.Response{
		makeToolCallResponse(llm.ToolCallData{ID: "call-1", Name: "echo_tool", Arguments: args}),
		makeTextResponse("The tool said: echo: hello"),
	}

	err := ProcessInput(context.Background(), session, profile, env, client, "Use the echo tool")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// History should be: user, assistant(tool call), tool_results, assistant(text)
	if session.TurnCount() != 4 {
		t.Fatalf("expected 4 turns, got %d", session.TurnCount())
	}

	// Check tool results turn
	toolResultsTurn, ok := session.History[2].(ToolResultsTurn)
	if !ok {
		t.Fatalf("expected ToolResultsTurn at index 2, got %T", session.History[2])
	}
	if len(toolResultsTurn.Results) != 1 {
		t.Fatalf("expected 1 tool result, got %d", len(toolResultsTurn.Results))
	}
	if !strings.Contains(toolResultsTurn.Results[0].Content, "echo: hello") {
		t.Errorf("expected tool result to contain 'echo: hello', got %q", toolResultsTurn.Results[0].Content)
	}
	if toolResultsTurn.Results[0].IsError {
		t.Error("expected tool result to not be an error")
	}

	// LLM should have been called twice
	calls := adapter.getCalls()
	if len(calls) != 2 {
		t.Errorf("expected 2 LLM calls, got %d", len(calls))
	}
}

func TestProcessInputParallelToolExecution(t *testing.T) {
	registry := NewToolRegistry()

	// Track execution order with an atomic counter to verify concurrency
	var execOrder int64

	registry.Register(&RegisteredTool{
		Definition: llm.ToolDefinition{
			Name:        "slow_tool_a",
			Description: "A slow tool",
			Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		},
		Execute: func(args map[string]any, env ExecutionEnvironment) (string, error) {
			atomic.AddInt64(&execOrder, 1)
			time.Sleep(50 * time.Millisecond)
			return "result_a", nil
		},
	})
	registry.Register(&RegisteredTool{
		Definition: llm.ToolDefinition{
			Name:        "slow_tool_b",
			Description: "Another slow tool",
			Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		},
		Execute: func(args map[string]any, env ExecutionEnvironment) (string, error) {
			atomic.AddInt64(&execOrder, 1)
			time.Sleep(50 * time.Millisecond)
			return "result_b", nil
		},
	})

	profile := &testProfile{
		id:            "test",
		model:         "test-model",
		systemPrompt:  "You are a test assistant.",
		toolDefs:      registry.Definitions(),
		registry:      registry,
		parallelTools: true, // Enable parallel execution
	}

	env := newLoopTestEnv()
	config := DefaultSessionConfig()
	session := NewSession(config)
	defer session.Close()

	adapter := &loopTestAdapter{}
	client := llm.NewClient(
		llm.WithProvider("test", adapter),
		llm.WithDefaultProvider("test"),
	)

	argsA := json.RawMessage(`{}`)
	argsB := json.RawMessage(`{}`)
	adapter.responses = []*llm.Response{
		makeToolCallResponse(
			llm.ToolCallData{ID: "call-a", Name: "slow_tool_a", Arguments: argsA},
			llm.ToolCallData{ID: "call-b", Name: "slow_tool_b", Arguments: argsB},
		),
		makeTextResponse("Both tools finished."),
	}

	start := time.Now()
	err := ProcessInput(context.Background(), session, profile, env, client, "Run both tools")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// If executed in parallel, both 50ms tools should complete in roughly 50-100ms
	// If sequential, it would take ~100ms+. Allow generous threshold.
	if elapsed > 200*time.Millisecond {
		t.Errorf("parallel execution took too long: %v (expected < 200ms)", elapsed)
	}

	// Both tools should have been called
	if atomic.LoadInt64(&execOrder) != 2 {
		t.Errorf("expected both tools to execute, got %d executions", atomic.LoadInt64(&execOrder))
	}

	// Verify tool results turn has both results
	toolResultsTurn, ok := session.History[2].(ToolResultsTurn)
	if !ok {
		t.Fatalf("expected ToolResultsTurn at index 2, got %T", session.History[2])
	}
	if len(toolResultsTurn.Results) != 2 {
		t.Fatalf("expected 2 tool results, got %d", len(toolResultsTurn.Results))
	}
}

func TestProcessInputRoundLimit(t *testing.T) {
	profile, env, session, client, adapter := newTestSetup()
	defer session.Close()

	// Set a low round limit
	session.Config.MaxToolRoundsPerInput = 2

	args := json.RawMessage(`{"message":"loop"}`)

	// Provide enough responses for the round limit plus one more (shouldn't be reached)
	adapter.responses = []*llm.Response{
		makeToolCallResponse(llm.ToolCallData{ID: "c1", Name: "echo_tool", Arguments: args}),
		makeToolCallResponse(llm.ToolCallData{ID: "c2", Name: "echo_tool", Arguments: args}),
		makeTextResponse("should not reach here"),
	}

	err := ProcessInput(context.Background(), session, profile, env, client, "keep calling tools")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have called LLM exactly 2 times (hitting round limit after 2 tool rounds)
	calls := adapter.getCalls()
	if len(calls) != 2 {
		t.Errorf("expected 2 LLM calls (round limit), got %d", len(calls))
	}

	// Session should be IDLE (not closed)
	if session.State != StateIdle {
		t.Errorf("expected state %s after round limit, got %s", StateIdle, session.State)
	}
}

func TestProcessInputTurnLimit(t *testing.T) {
	profile, env, session, client, adapter := newTestSetup()
	defer session.Close()

	// Set max turns to a small number. Each user+assistant pair is 2 turns, tool results is 1 more.
	// With max_turns=3, after the user turn (1) + assistant turn (2) + tool results (3),
	// the next iteration should hit the turn limit.
	session.Config.MaxTurns = 3

	args := json.RawMessage(`{"message":"turn limit"}`)

	adapter.responses = []*llm.Response{
		makeToolCallResponse(llm.ToolCallData{ID: "c1", Name: "echo_tool", Arguments: args}),
		makeTextResponse("should not reach here"),
	}

	err := ProcessInput(context.Background(), session, profile, env, client, "test turn limit")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The loop should have stopped after hitting max_turns
	calls := adapter.getCalls()
	if len(calls) > 1 {
		t.Errorf("expected at most 1 LLM call before turn limit, got %d", len(calls))
	}
}

func TestProcessInputUnknownTool(t *testing.T) {
	profile, env, session, client, adapter := newTestSetup()
	defer session.Close()

	args := json.RawMessage(`{"x":"y"}`)

	// Model calls a tool that doesn't exist
	adapter.responses = []*llm.Response{
		makeToolCallResponse(llm.ToolCallData{ID: "c1", Name: "nonexistent_tool", Arguments: args}),
		makeTextResponse("Got it, that tool doesn't exist."),
	}

	err := ProcessInput(context.Background(), session, profile, env, client, "call unknown tool")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Find the ToolResultsTurn
	var toolResultsTurn ToolResultsTurn
	found := false
	for _, turn := range session.History {
		if tr, ok := turn.(ToolResultsTurn); ok {
			toolResultsTurn = tr
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected a ToolResultsTurn in history")
	}

	if len(toolResultsTurn.Results) != 1 {
		t.Fatalf("expected 1 tool result, got %d", len(toolResultsTurn.Results))
	}

	result := toolResultsTurn.Results[0]
	if !result.IsError {
		t.Error("expected tool result to be an error for unknown tool")
	}
	if !strings.Contains(result.Content, "Unknown tool") {
		t.Errorf("expected error message to contain 'Unknown tool', got %q", result.Content)
	}
	if !strings.Contains(result.Content, "nonexistent_tool") {
		t.Errorf("expected error message to contain tool name 'nonexistent_tool', got %q", result.Content)
	}
}

func TestProcessInputToolError(t *testing.T) {
	registry := NewToolRegistry()
	registry.Register(&RegisteredTool{
		Definition: llm.ToolDefinition{
			Name:        "failing_tool",
			Description: "A tool that always fails",
			Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		},
		Execute: func(args map[string]any, env ExecutionEnvironment) (string, error) {
			return "", fmt.Errorf("something went wrong")
		},
	})

	profile := &testProfile{
		id:            "test",
		model:         "test-model",
		systemPrompt:  "You are a test assistant.",
		toolDefs:      registry.Definitions(),
		registry:      registry,
		parallelTools: false,
	}

	env := newLoopTestEnv()
	config := DefaultSessionConfig()
	session := NewSession(config)
	defer session.Close()

	adapter := &loopTestAdapter{}
	client := llm.NewClient(
		llm.WithProvider("test", adapter),
		llm.WithDefaultProvider("test"),
	)

	args := json.RawMessage(`{}`)
	adapter.responses = []*llm.Response{
		makeToolCallResponse(llm.ToolCallData{ID: "c1", Name: "failing_tool", Arguments: args}),
		makeTextResponse("I see the tool failed."),
	}

	err := ProcessInput(context.Background(), session, profile, env, client, "use the failing tool")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Find the tool result
	var toolResultsTurn ToolResultsTurn
	for _, turn := range session.History {
		if tr, ok := turn.(ToolResultsTurn); ok {
			toolResultsTurn = tr
			break
		}
	}

	if len(toolResultsTurn.Results) != 1 {
		t.Fatalf("expected 1 tool result, got %d", len(toolResultsTurn.Results))
	}

	result := toolResultsTurn.Results[0]
	if !result.IsError {
		t.Error("expected tool result to be an error")
	}
	if !strings.Contains(result.Content, "something went wrong") {
		t.Errorf("expected error message to contain 'something went wrong', got %q", result.Content)
	}
}

func TestProcessInputSteering(t *testing.T) {
	profile, env, session, client, adapter := newTestSetup()
	defer session.Close()

	args := json.RawMessage(`{"message":"first"}`)

	// Inject a steering message before we start
	session.Steer("Focus on testing")

	adapter.responses = []*llm.Response{
		makeToolCallResponse(llm.ToolCallData{ID: "c1", Name: "echo_tool", Arguments: args}),
		makeTextResponse("Done with testing focus."),
	}

	err := ProcessInput(context.Background(), session, profile, env, client, "do something")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check that a SteeringTurn was added to history
	steeringFound := false
	for _, turn := range session.History {
		if st, ok := turn.(SteeringTurn); ok {
			if st.Content == "Focus on testing" {
				steeringFound = true
			}
		}
	}
	if !steeringFound {
		t.Error("expected a SteeringTurn with 'Focus on testing' in history")
	}
}

func TestProcessInputFollowup(t *testing.T) {
	profile, env, session, client, adapter := newTestSetup()
	defer session.Close()

	// Queue a followup before we start
	session.FollowUp("now do step 2")

	adapter.responses = []*llm.Response{
		makeTextResponse("Step 1 done."),
		makeTextResponse("Step 2 done."),
	}

	err := ProcessInput(context.Background(), session, profile, env, client, "do step 1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// LLM should have been called twice (once for original, once for followup)
	calls := adapter.getCalls()
	if len(calls) != 2 {
		t.Errorf("expected 2 LLM calls (original + followup), got %d", len(calls))
	}

	// History should contain both user inputs
	userTurns := 0
	for _, turn := range session.History {
		if _, ok := turn.(UserTurn); ok {
			userTurns++
		}
	}
	if userTurns != 2 {
		t.Errorf("expected 2 user turns (original + followup), got %d", userTurns)
	}
}

func TestProcessInputContextCancellation(t *testing.T) {
	profile, env, session, client, adapter := newTestSetup()
	defer session.Close()

	args := json.RawMessage(`{"message":"cancel"}`)

	// First response returns a tool call, but by then context is cancelled
	adapter.responses = []*llm.Response{
		makeToolCallResponse(llm.ToolCallData{ID: "c1", Name: "echo_tool", Arguments: args}),
		makeTextResponse("should not reach"),
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel context after a brief delay
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	// ProcessInput should respect cancellation
	err := ProcessInput(ctx, session, profile, env, client, "start work")
	// The function may or may not return an error depending on timing,
	// but it should not hang indefinitely. If context was cancelled before
	// the second LLM call, the loop should have exited.
	_ = err

	// The key assertion: the function returned (didn't hang)
	// and the session is not stuck in PROCESSING forever
	if session.State == StateProcessing {
		t.Error("session should not be stuck in PROCESSING after context cancellation")
	}
}

func TestProcessInputLoopDetection(t *testing.T) {
	profile, env, session, client, adapter := newTestSetup()
	defer session.Close()

	// Enable loop detection with a small window
	session.Config.EnableLoopDetection = true
	session.Config.LoopDetectionWindow = 4
	session.Config.MaxToolRoundsPerInput = 10

	// Same tool call every time will trigger loop detection after 4 identical calls
	args := json.RawMessage(`{"message":"same"}`)
	tc := llm.ToolCallData{ID: "c1", Name: "echo_tool", Arguments: args}

	adapter.responses = []*llm.Response{
		makeToolCallResponse(tc),
		makeToolCallResponse(tc),
		makeToolCallResponse(tc),
		makeToolCallResponse(tc),
		makeTextResponse("finally done"), // After loop detection warning
	}

	err := ProcessInput(context.Background(), session, profile, env, client, "loop forever")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check that a loop detection SteeringTurn was injected
	loopDetected := false
	for _, turn := range session.History {
		if st, ok := turn.(SteeringTurn); ok {
			if strings.Contains(st.Content, "Loop detected") {
				loopDetected = true
			}
		}
	}
	if !loopDetected {
		t.Error("expected a loop detection SteeringTurn in history")
	}
}

func TestProcessInputNaturalCompletion(t *testing.T) {
	profile, env, session, client, adapter := newTestSetup()
	defer session.Close()

	// Model responds with text only (no tool calls) on the first response
	adapter.responses = []*llm.Response{makeTextResponse("All done, no tools needed.")}

	err := ProcessInput(context.Background(), session, profile, env, client, "just respond")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only 1 LLM call should have been made
	calls := adapter.getCalls()
	if len(calls) != 1 {
		t.Errorf("expected 1 LLM call for natural completion, got %d", len(calls))
	}

	// History should be: user turn + assistant turn = 2 turns
	if session.TurnCount() != 2 {
		t.Errorf("expected 2 turns for natural completion, got %d", session.TurnCount())
	}

	// Session state should be IDLE
	if session.State != StateIdle {
		t.Errorf("expected state %s, got %s", StateIdle, session.State)
	}
}

func TestProcessInputEventsEmitted(t *testing.T) {
	profile, env, session, client, adapter := newTestSetup()
	defer session.Close()

	// Subscribe to events before processing
	eventCh := session.EventEmitter.Subscribe()

	args := json.RawMessage(`{"message":"event test"}`)
	adapter.responses = []*llm.Response{
		makeToolCallResponse(llm.ToolCallData{ID: "c1", Name: "echo_tool", Arguments: args}),
		makeTextResponse("Done."),
	}

	err := ProcessInput(context.Background(), session, profile, env, client, "test events")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Collect all events
	var events []SessionEvent
	timeout := time.After(1 * time.Second)
	for {
		select {
		case evt, ok := <-eventCh:
			if !ok {
				goto done
			}
			events = append(events, evt)
		case <-timeout:
			goto done
		default:
			// No more events in buffer
			goto done
		}
	}
done:

	// Check that we got the expected event kinds
	eventKinds := make([]EventKind, len(events))
	for i, evt := range events {
		eventKinds[i] = evt.Kind
	}

	// Should have at minimum: USER_INPUT, ASSISTANT_TEXT_END, TOOL_CALL_START, TOOL_CALL_END, ASSISTANT_TEXT_END, SESSION_END
	expectedKinds := []EventKind{
		EventUserInput,
		EventAssistantTextEnd,
		EventToolCallStart,
		EventToolCallEnd,
		EventAssistantTextEnd,
		EventSessionEnd,
	}

	for _, expected := range expectedKinds {
		found := false
		for _, actual := range eventKinds {
			if actual == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected event kind %q not found in events: %v", expected, eventKinds)
		}
	}

	// USER_INPUT should come before ASSISTANT_TEXT_END
	userInputIdx := -1
	firstAssistantEndIdx := -1
	for i, k := range eventKinds {
		if k == EventUserInput && userInputIdx == -1 {
			userInputIdx = i
		}
		if k == EventAssistantTextEnd && firstAssistantEndIdx == -1 {
			firstAssistantEndIdx = i
		}
	}
	if userInputIdx >= firstAssistantEndIdx {
		t.Error("USER_INPUT event should come before first ASSISTANT_TEXT_END")
	}

	// SESSION_END should be last
	lastEventKind := eventKinds[len(eventKinds)-1]
	if lastEventKind != EventSessionEnd {
		t.Errorf("expected last event to be SESSION_END, got %s", lastEventKind)
	}
}

func TestProcessInputUserOverrideAppendsToSystemPrompt(t *testing.T) {
	profile, env, session, client, adapter := newTestSetup()
	defer session.Close()

	// Set user override on session config
	session.Config.UserOverride = "Always write tests first. Never skip TDD."

	adapter.responses = []*llm.Response{makeTextResponse("Understood, TDD it is.")}

	err := ProcessInput(context.Background(), session, profile, env, client, "build feature X")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the system prompt sent to LLM contains the user override
	calls := adapter.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 LLM call, got %d", len(calls))
	}

	// The first message should be the system prompt
	if len(calls[0].Messages) < 1 {
		t.Fatal("expected at least 1 message in the LLM request")
	}

	systemMsg := calls[0].Messages[0]
	systemContent := ""
	for _, part := range systemMsg.Content {
		if part.Kind == llm.ContentText {
			systemContent += part.Text
		}
	}

	if !strings.Contains(systemContent, "Always write tests first") {
		t.Error("expected system prompt to contain user override text")
	}
	if !strings.Contains(systemContent, "User Instructions") {
		t.Error("expected system prompt to contain 'User Instructions' header")
	}
}

func TestProcessInputNoUserOverride(t *testing.T) {
	profile, env, session, client, adapter := newTestSetup()
	defer session.Close()

	// No user override set (empty string)
	adapter.responses = []*llm.Response{makeTextResponse("Hello.")}

	err := ProcessInput(context.Background(), session, profile, env, client, "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := adapter.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 LLM call, got %d", len(calls))
	}

	systemMsg := calls[0].Messages[0]
	systemContent := ""
	for _, part := range systemMsg.Content {
		if part.Kind == llm.ContentText {
			systemContent += part.Text
		}
	}

	// Should NOT contain the user override header when no override is set
	if strings.Contains(systemContent, "User Instructions") {
		t.Error("system prompt should not contain 'User Instructions' when no override is set")
	}
}
