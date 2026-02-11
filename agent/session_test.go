// ABOUTME: Tests for the session management, turn types, config, loop detection, and history conversion.
// ABOUTME: Covers Session lifecycle, steering/followup queues, and ConvertHistoryToMessages.

package agent

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/2389-research/mammoth/llm"
)

func TestNewSession(t *testing.T) {
	config := DefaultSessionConfig()
	session := NewSession(config)
	defer session.Close()

	if session.ID == "" {
		t.Error("expected non-empty session ID")
	}

	// UUID format: 8-4-4-4-12 hex chars
	if len(session.ID) != 36 {
		t.Errorf("expected UUID length 36, got %d: %s", len(session.ID), session.ID)
	}

	if session.State != StateIdle {
		t.Errorf("expected initial state %s, got %s", StateIdle, session.State)
	}

	if session.EventEmitter == nil {
		t.Error("expected non-nil EventEmitter")
	}

	if len(session.History) != 0 {
		t.Errorf("expected empty history, got %d turns", len(session.History))
	}
}

func TestSessionConfig(t *testing.T) {
	config := DefaultSessionConfig()

	if config.MaxTurns != 0 {
		t.Errorf("expected MaxTurns 0, got %d", config.MaxTurns)
	}
	if config.MaxToolRoundsPerInput != 200 {
		t.Errorf("expected MaxToolRoundsPerInput 200, got %d", config.MaxToolRoundsPerInput)
	}
	if config.DefaultCommandTimeoutMs != 10000 {
		t.Errorf("expected DefaultCommandTimeoutMs 10000, got %d", config.DefaultCommandTimeoutMs)
	}
	if config.MaxCommandTimeoutMs != 600000 {
		t.Errorf("expected MaxCommandTimeoutMs 600000, got %d", config.MaxCommandTimeoutMs)
	}
	if config.ReasoningEffort != "" {
		t.Errorf("expected empty ReasoningEffort, got %s", config.ReasoningEffort)
	}
	if config.ToolOutputLimits == nil {
		t.Error("expected non-nil ToolOutputLimits map")
	}
	if !config.EnableLoopDetection {
		t.Error("expected EnableLoopDetection to be true")
	}
	if config.LoopDetectionWindow != 10 {
		t.Errorf("expected LoopDetectionWindow 10, got %d", config.LoopDetectionWindow)
	}
	if config.MaxSubagentDepth != 1 {
		t.Errorf("expected MaxSubagentDepth 1, got %d", config.MaxSubagentDepth)
	}
}

func TestSessionStateTransitions(t *testing.T) {
	config := DefaultSessionConfig()
	session := NewSession(config)
	defer session.Close()

	// Initial state
	if session.State != StateIdle {
		t.Fatalf("expected initial state %s, got %s", StateIdle, session.State)
	}

	// IDLE -> PROCESSING
	session.SetState(StateProcessing)
	if session.State != StateProcessing {
		t.Errorf("expected state %s, got %s", StateProcessing, session.State)
	}

	// PROCESSING -> AWAITING_INPUT
	session.SetState(StateAwaitingInput)
	if session.State != StateAwaitingInput {
		t.Errorf("expected state %s, got %s", StateAwaitingInput, session.State)
	}

	// AWAITING_INPUT -> PROCESSING
	session.SetState(StateProcessing)
	if session.State != StateProcessing {
		t.Errorf("expected state %s, got %s", StateProcessing, session.State)
	}

	// PROCESSING -> IDLE
	session.SetState(StateIdle)
	if session.State != StateIdle {
		t.Errorf("expected state %s, got %s", StateIdle, session.State)
	}

	// IDLE -> CLOSED
	session.SetState(StateClosed)
	if session.State != StateClosed {
		t.Errorf("expected state %s, got %s", StateClosed, session.State)
	}
}

func TestUserTurn(t *testing.T) {
	now := time.Now()
	turn := UserTurn{
		Content:   "Hello, agent!",
		Timestamp: now,
	}

	if turn.TurnType() != "user" {
		t.Errorf("expected turn type 'user', got %s", turn.TurnType())
	}

	if !turn.TurnTimestamp().Equal(now) {
		t.Errorf("expected timestamp %v, got %v", now, turn.TurnTimestamp())
	}
}

func TestAssistantTurn(t *testing.T) {
	now := time.Now()
	args := json.RawMessage(`{"file": "test.go"}`)
	turn := AssistantTurn{
		Content: "I'll read the file.",
		ToolCalls: []llm.ToolCallData{
			{ID: "call-1", Name: "read_file", Arguments: args},
		},
		Reasoning:  "thinking about reading the file",
		Usage:      llm.Usage{InputTokens: 100, OutputTokens: 50, TotalTokens: 150},
		ResponseID: "resp-123",
		Timestamp:  now,
	}

	if turn.TurnType() != "assistant" {
		t.Errorf("expected turn type 'assistant', got %s", turn.TurnType())
	}

	if !turn.TurnTimestamp().Equal(now) {
		t.Errorf("expected timestamp %v, got %v", now, turn.TurnTimestamp())
	}

	if len(turn.ToolCalls) != 1 {
		t.Errorf("expected 1 tool call, got %d", len(turn.ToolCalls))
	}

	if turn.ToolCalls[0].Name != "read_file" {
		t.Errorf("expected tool call name 'read_file', got %s", turn.ToolCalls[0].Name)
	}
}

func TestToolResultsTurn(t *testing.T) {
	now := time.Now()
	turn := ToolResultsTurn{
		Results: []llm.ToolResult{
			{ToolCallID: "call-1", Content: "file contents here", IsError: false},
			{ToolCallID: "call-2", Content: "error: not found", IsError: true},
		},
		Timestamp: now,
	}

	if turn.TurnType() != "tool_results" {
		t.Errorf("expected turn type 'tool_results', got %s", turn.TurnType())
	}

	if !turn.TurnTimestamp().Equal(now) {
		t.Errorf("expected timestamp %v, got %v", now, turn.TurnTimestamp())
	}

	if len(turn.Results) != 2 {
		t.Errorf("expected 2 results, got %d", len(turn.Results))
	}
}

func TestSteeringQueue(t *testing.T) {
	config := DefaultSessionConfig()
	session := NewSession(config)
	defer session.Close()

	// Empty drain returns empty slice
	drained := session.DrainSteering()
	if len(drained) != 0 {
		t.Errorf("expected empty drain, got %d messages", len(drained))
	}

	// Add steering messages
	session.Steer("focus on tests")
	session.Steer("use table-driven tests")
	session.Steer("add benchmarks")

	// Drain should return all messages in order
	drained = session.DrainSteering()
	if len(drained) != 3 {
		t.Fatalf("expected 3 drained messages, got %d", len(drained))
	}
	if drained[0] != "focus on tests" {
		t.Errorf("expected first message 'focus on tests', got %s", drained[0])
	}
	if drained[1] != "use table-driven tests" {
		t.Errorf("expected second message 'use table-driven tests', got %s", drained[1])
	}
	if drained[2] != "add benchmarks" {
		t.Errorf("expected third message 'add benchmarks', got %s", drained[2])
	}

	// Drain again should be empty
	drained = session.DrainSteering()
	if len(drained) != 0 {
		t.Errorf("expected empty drain after first drain, got %d", len(drained))
	}

	// Test concurrent safety
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			session.Steer("concurrent message")
		}(i)
	}
	wg.Wait()

	drained = session.DrainSteering()
	if len(drained) != 100 {
		t.Errorf("expected 100 concurrent messages, got %d", len(drained))
	}
}

func TestFollowupQueue(t *testing.T) {
	config := DefaultSessionConfig()
	session := NewSession(config)
	defer session.Close()

	// Empty drain returns empty string
	msg := session.DrainFollowup()
	if msg != "" {
		t.Errorf("expected empty followup, got %s", msg)
	}

	// Add followup messages
	session.FollowUp("now run the tests")
	session.FollowUp("then commit")

	// Drain should return first message
	msg = session.DrainFollowup()
	if msg != "now run the tests" {
		t.Errorf("expected 'now run the tests', got %s", msg)
	}

	// Drain again should return second message
	msg = session.DrainFollowup()
	if msg != "then commit" {
		t.Errorf("expected 'then commit', got %s", msg)
	}

	// Drain again should be empty
	msg = session.DrainFollowup()
	if msg != "" {
		t.Errorf("expected empty followup, got %s", msg)
	}
}

func TestAppendTurn(t *testing.T) {
	config := DefaultSessionConfig()
	session := NewSession(config)
	defer session.Close()

	session.AppendTurn(UserTurn{Content: "hello", Timestamp: time.Now()})
	session.AppendTurn(AssistantTurn{Content: "hi there", Timestamp: time.Now()})
	session.AppendTurn(ToolResultsTurn{
		Results:   []llm.ToolResult{{ToolCallID: "c1", Content: "ok"}},
		Timestamp: time.Now(),
	})
	session.AppendTurn(SystemTurn{Content: "system info", Timestamp: time.Now()})
	session.AppendTurn(SteeringTurn{Content: "steer left", Timestamp: time.Now()})

	if session.TurnCount() != 5 {
		t.Errorf("expected 5 turns, got %d", session.TurnCount())
	}

	if session.History[0].TurnType() != "user" {
		t.Errorf("expected first turn to be 'user', got %s", session.History[0].TurnType())
	}
	if session.History[1].TurnType() != "assistant" {
		t.Errorf("expected second turn to be 'assistant', got %s", session.History[1].TurnType())
	}
	if session.History[2].TurnType() != "tool_results" {
		t.Errorf("expected third turn to be 'tool_results', got %s", session.History[2].TurnType())
	}
	if session.History[3].TurnType() != "system" {
		t.Errorf("expected fourth turn to be 'system', got %s", session.History[3].TurnType())
	}
	if session.History[4].TurnType() != "steering" {
		t.Errorf("expected fifth turn to be 'steering', got %s", session.History[4].TurnType())
	}
}

func TestConvertHistoryToMessages(t *testing.T) {
	args := json.RawMessage(`{"file_path": "/tmp/test.go"}`)
	history := []Turn{
		SystemTurn{Content: "You are a coding assistant.", Timestamp: time.Now()},
		UserTurn{Content: "Read the file.", Timestamp: time.Now()},
		AssistantTurn{
			Content: "I'll read it.",
			ToolCalls: []llm.ToolCallData{
				{ID: "call-1", Name: "read_file", Arguments: args},
			},
			Timestamp: time.Now(),
		},
		ToolResultsTurn{
			Results:   []llm.ToolResult{{ToolCallID: "call-1", Content: "file data"}},
			Timestamp: time.Now(),
		},
		SteeringTurn{Content: "Focus on the main function.", Timestamp: time.Now()},
	}

	messages := ConvertHistoryToMessages(history)

	if len(messages) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(messages))
	}

	// System turn -> system role
	if messages[0].Role != llm.RoleSystem {
		t.Errorf("expected message 0 role %s, got %s", llm.RoleSystem, messages[0].Role)
	}
	if messages[0].TextContent() != "You are a coding assistant." {
		t.Errorf("expected system message text, got %s", messages[0].TextContent())
	}

	// User turn -> user role
	if messages[1].Role != llm.RoleUser {
		t.Errorf("expected message 1 role %s, got %s", llm.RoleUser, messages[1].Role)
	}
	if messages[1].TextContent() != "Read the file." {
		t.Errorf("expected user message text, got %s", messages[1].TextContent())
	}

	// Assistant turn -> assistant role with text + tool calls
	if messages[2].Role != llm.RoleAssistant {
		t.Errorf("expected message 2 role %s, got %s", llm.RoleAssistant, messages[2].Role)
	}
	if messages[2].TextContent() != "I'll read it." {
		t.Errorf("expected assistant text, got %s", messages[2].TextContent())
	}
	toolCalls := messages[2].ToolCalls()
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call in assistant message, got %d", len(toolCalls))
	}
	if toolCalls[0].Name != "read_file" {
		t.Errorf("expected tool call name 'read_file', got %s", toolCalls[0].Name)
	}

	// Tool results turn -> tool role messages
	if messages[3].Role != llm.RoleTool {
		t.Errorf("expected message 3 role %s, got %s", llm.RoleTool, messages[3].Role)
	}

	// Steering turn -> user role
	if messages[4].Role != llm.RoleUser {
		t.Errorf("expected message 4 role %s, got %s", llm.RoleUser, messages[4].Role)
	}
	if messages[4].TextContent() != "Focus on the main function." {
		t.Errorf("expected steering message text, got %s", messages[4].TextContent())
	}
}

func TestDetectLoopRepeatingPattern(t *testing.T) {
	// Pattern of length 1: same tool call repeated 10 times
	t.Run("pattern_length_1", func(t *testing.T) {
		args := json.RawMessage(`{"file": "test.go"}`)
		var history []Turn
		for i := 0; i < 10; i++ {
			history = append(history,
				AssistantTurn{
					ToolCalls: []llm.ToolCallData{{ID: "c", Name: "read_file", Arguments: args}},
					Timestamp: time.Now(),
				},
				ToolResultsTurn{
					Results:   []llm.ToolResult{{ToolCallID: "c", Content: "data"}},
					Timestamp: time.Now(),
				},
			)
		}
		if !DetectLoop(history, 10) {
			t.Error("expected loop detection for pattern length 1")
		}
	})

	// Pattern of length 2: alternating tool calls repeated 5 times
	t.Run("pattern_length_2", func(t *testing.T) {
		argsRead := json.RawMessage(`{"file": "test.go"}`)
		argsWrite := json.RawMessage(`{"file": "test.go", "content": "hello"}`)
		var history []Turn
		for i := 0; i < 5; i++ {
			history = append(history,
				AssistantTurn{
					ToolCalls: []llm.ToolCallData{{ID: "c1", Name: "read_file", Arguments: argsRead}},
					Timestamp: time.Now(),
				},
				ToolResultsTurn{
					Results:   []llm.ToolResult{{ToolCallID: "c1", Content: "data"}},
					Timestamp: time.Now(),
				},
				AssistantTurn{
					ToolCalls: []llm.ToolCallData{{ID: "c2", Name: "write_file", Arguments: argsWrite}},
					Timestamp: time.Now(),
				},
				ToolResultsTurn{
					Results:   []llm.ToolResult{{ToolCallID: "c2", Content: "ok"}},
					Timestamp: time.Now(),
				},
			)
		}
		if !DetectLoop(history, 10) {
			t.Error("expected loop detection for pattern length 2")
		}
	})

	// Pattern of length 3: three tool calls repeated
	t.Run("pattern_length_3", func(t *testing.T) {
		argsA := json.RawMessage(`{"a": 1}`)
		argsB := json.RawMessage(`{"b": 2}`)
		argsC := json.RawMessage(`{"c": 3}`)
		var history []Turn
		// Need 9 tool calls (3 repeats of pattern length 3) with window=9
		for i := 0; i < 3; i++ {
			history = append(history,
				AssistantTurn{
					ToolCalls: []llm.ToolCallData{{ID: "ca", Name: "tool_a", Arguments: argsA}},
					Timestamp: time.Now(),
				},
				ToolResultsTurn{
					Results:   []llm.ToolResult{{ToolCallID: "ca", Content: "ra"}},
					Timestamp: time.Now(),
				},
				AssistantTurn{
					ToolCalls: []llm.ToolCallData{{ID: "cb", Name: "tool_b", Arguments: argsB}},
					Timestamp: time.Now(),
				},
				ToolResultsTurn{
					Results:   []llm.ToolResult{{ToolCallID: "cb", Content: "rb"}},
					Timestamp: time.Now(),
				},
				AssistantTurn{
					ToolCalls: []llm.ToolCallData{{ID: "cc", Name: "tool_c", Arguments: argsC}},
					Timestamp: time.Now(),
				},
				ToolResultsTurn{
					Results:   []llm.ToolResult{{ToolCallID: "cc", Content: "rc"}},
					Timestamp: time.Now(),
				},
			)
		}
		if !DetectLoop(history, 9) {
			t.Error("expected loop detection for pattern length 3")
		}
	})
}

func TestDetectLoopNoPattern(t *testing.T) {
	var history []Turn
	for i := 0; i < 10; i++ {
		args := json.RawMessage(`{"file": "file` + string(rune('a'+i)) + `.go"}`)
		history = append(history,
			AssistantTurn{
				ToolCalls: []llm.ToolCallData{{ID: "c", Name: "read_file", Arguments: args}},
				Timestamp: time.Now(),
			},
			ToolResultsTurn{
				Results:   []llm.ToolResult{{ToolCallID: "c", Content: "data"}},
				Timestamp: time.Now(),
			},
		)
	}
	if DetectLoop(history, 10) {
		t.Error("expected no loop detection when calls vary")
	}
}

func TestDetectLoopInsufficientHistory(t *testing.T) {
	args := json.RawMessage(`{"file": "test.go"}`)
	history := []Turn{
		AssistantTurn{
			ToolCalls: []llm.ToolCallData{{ID: "c", Name: "read_file", Arguments: args}},
			Timestamp: time.Now(),
		},
		ToolResultsTurn{
			Results:   []llm.ToolResult{{ToolCallID: "c", Content: "data"}},
			Timestamp: time.Now(),
		},
	}

	if DetectLoop(history, 10) {
		t.Error("expected no loop detection with insufficient history")
	}
}

func TestSessionClose(t *testing.T) {
	config := DefaultSessionConfig()
	session := NewSession(config)

	if session.State != StateIdle {
		t.Fatalf("expected initial state %s, got %s", StateIdle, session.State)
	}

	session.Close()

	if session.State != StateClosed {
		t.Errorf("expected state %s after close, got %s", StateClosed, session.State)
	}
}
