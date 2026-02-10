// ABOUTME: Integration tests for the agent loop wired to tool execution environment and thread fidelity.
// ABOUTME: Exercises the full path: tool call dispatch -> registry -> ExecutionEnvironment -> result flow back.

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/2389-research/makeatron/llm"
)

// --- Integration Test: Tool Execution via ExecutionEnvironment ---

func TestIntegration_ToolDispatchThroughExecEnv(t *testing.T) {
	// This test verifies the full path: LLM returns a tool call -> agent dispatches
	// to the registry -> tool executes via ExecutionEnvironment -> result flows back
	// to the LLM as a ToolResultsTurn.

	tests := []struct {
		name          string
		toolName      string
		toolArgs      map[string]any
		setupEnv      func(*loopTestEnv)
		wantContains  string
		wantIsError   bool
	}{
		{
			name:     "shell_tool_executes_via_env",
			toolName: "shell",
			toolArgs: map[string]any{"command": "echo hello"},
			setupEnv: func(env *loopTestEnv) {},
			// loopTestEnv.ExecCommand returns "ok" by default
			wantContains: "ok",
			wantIsError:  false,
		},
		{
			name:     "read_file_tool_reads_from_env",
			toolName: "read_file",
			toolArgs: map[string]any{"file_path": "/tmp/test-agent/hello.txt"},
			setupEnv: func(env *loopTestEnv) {
				env.files["/tmp/test-agent/hello.txt"] = "hello world"
			},
			wantContains: "hello world",
			wantIsError:  false,
		},
		{
			name:     "write_file_tool_writes_to_env",
			toolName: "write_file",
			toolArgs: map[string]any{"file_path": "/tmp/test-agent/out.txt", "content": "written content"},
			setupEnv: func(env *loopTestEnv) {},
			wantContains: "Successfully wrote",
			wantIsError:  false,
		},
		{
			name:     "read_file_tool_missing_file_returns_error",
			toolName: "read_file",
			toolArgs: map[string]any{"file_path": "/tmp/test-agent/nonexistent.txt"},
			setupEnv: func(env *loopTestEnv) {},
			wantContains: "Tool error",
			wantIsError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build a registry with all core tools
			registry := NewToolRegistry()
			RegisterCoreTools(registry)

			profile := &testProfile{
				id:           "test",
				model:        "test-model",
				systemPrompt: "You are a test assistant.",
				toolDefs:     registry.Definitions(),
				registry:     registry,
			}

			env := newLoopTestEnv()
			tt.setupEnv(env)

			config := DefaultSessionConfig()
			session := NewSession(config)
			defer session.Close()

			// Marshal tool args
			argsJSON, err := json.Marshal(tt.toolArgs)
			if err != nil {
				t.Fatalf("failed to marshal tool args: %v", err)
			}

			adapter := &loopTestAdapter{
				responses: []*llm.Response{
					makeToolCallResponse(llm.ToolCallData{
						ID:        "call-1",
						Name:      tt.toolName,
						Arguments: argsJSON,
					}),
					makeTextResponse("Done."),
				},
			}

			client := llm.NewClient(
				llm.WithProvider("test", adapter),
				llm.WithDefaultProvider("test"),
			)

			err = ProcessInput(context.Background(), session, profile, env, client, "test tool dispatch")
			if err != nil {
				t.Fatalf("ProcessInput error: %v", err)
			}

			// Find the ToolResultsTurn
			var toolResult llm.ToolResult
			found := false
			for _, turn := range session.History {
				if tr, ok := turn.(ToolResultsTurn); ok {
					if len(tr.Results) > 0 {
						toolResult = tr.Results[0]
						found = true
					}
				}
			}
			if !found {
				t.Fatal("expected a ToolResultsTurn in session history")
			}

			if !strings.Contains(toolResult.Content, tt.wantContains) {
				t.Errorf("expected tool result to contain %q, got %q", tt.wantContains, toolResult.Content)
			}

			if toolResult.IsError != tt.wantIsError {
				t.Errorf("expected IsError=%v, got IsError=%v (content: %q)", tt.wantIsError, toolResult.IsError, toolResult.Content)
			}
		})
	}
}

// TestIntegration_WriteAndReadBackViaEnv tests a multi-turn agent scenario
// where one tool call writes a file, then a second tool call reads it back,
// verifying that the ExecutionEnvironment state persists across tool calls.
func TestIntegration_WriteAndReadBackViaEnv(t *testing.T) {
	registry := NewToolRegistry()
	RegisterCoreTools(registry)

	profile := &testProfile{
		id:           "test",
		model:        "test-model",
		systemPrompt: "You are a test assistant.",
		toolDefs:     registry.Definitions(),
		registry:     registry,
	}

	env := newLoopTestEnv()
	config := DefaultSessionConfig()
	session := NewSession(config)
	defer session.Close()

	writeArgs, _ := json.Marshal(map[string]any{
		"file_path": "/tmp/test-agent/state.txt",
		"content":   "persistent data",
	})
	readArgs, _ := json.Marshal(map[string]any{
		"file_path": "/tmp/test-agent/state.txt",
	})

	adapter := &loopTestAdapter{
		responses: []*llm.Response{
			// Turn 1: write a file
			makeToolCallResponse(llm.ToolCallData{
				ID: "write-1", Name: "write_file", Arguments: writeArgs,
			}),
			// Turn 2: read it back
			makeToolCallResponse(llm.ToolCallData{
				ID: "read-1", Name: "read_file", Arguments: readArgs,
			}),
			// Turn 3: natural completion
			makeTextResponse("The file contained: persistent data"),
		},
	}

	client := llm.NewClient(
		llm.WithProvider("test", adapter),
		llm.WithDefaultProvider("test"),
	)

	err := ProcessInput(context.Background(), session, profile, env, client, "write then read")
	if err != nil {
		t.Fatalf("ProcessInput error: %v", err)
	}

	// Verify the file was written to the env
	content, ok := env.files["/tmp/test-agent/state.txt"]
	if !ok {
		t.Fatal("expected file to be written to env")
	}
	if content != "persistent data" {
		t.Errorf("expected file content 'persistent data', got %q", content)
	}

	// Find the second ToolResultsTurn (read result) and verify it contains the data
	toolResultCount := 0
	for _, turn := range session.History {
		if tr, ok := turn.(ToolResultsTurn); ok {
			toolResultCount++
			if toolResultCount == 2 {
				// This is the read result
				if len(tr.Results) == 0 {
					t.Fatal("expected non-empty tool results for read")
				}
				if !strings.Contains(tr.Results[0].Content, "persistent data") {
					t.Errorf("expected read result to contain 'persistent data', got %q", tr.Results[0].Content)
				}
			}
		}
	}
	if toolResultCount < 2 {
		t.Errorf("expected at least 2 ToolResultsTurns, got %d", toolResultCount)
	}

	// LLM should have been called 3 times
	calls := adapter.getCalls()
	if len(calls) != 3 {
		t.Errorf("expected 3 LLM calls, got %d", len(calls))
	}
}

// --- Integration Test: Thread Fidelity ---

func TestIntegration_FidelityModeInSessionConfig(t *testing.T) {
	// Verify that FidelityMode can be set on SessionConfig
	config := DefaultSessionConfig()

	// Default should be empty (no fidelity mode set)
	if config.FidelityMode != "" {
		t.Errorf("expected default FidelityMode to be empty, got %q", config.FidelityMode)
	}

	// Set each valid mode
	validModes := []string{"full", "truncate", "compact", "summary:low", "summary:medium", "summary:high"}
	for _, mode := range validModes {
		config.FidelityMode = mode
		if config.FidelityMode != mode {
			t.Errorf("expected FidelityMode %q, got %q", mode, config.FidelityMode)
		}
	}
}

func TestIntegration_ApplyFidelityFull(t *testing.T) {
	// "full" mode should preserve all history
	history := buildTestHistory(20)
	result := ApplyFidelity(history, "full", 200000)
	if len(result) != len(history) {
		t.Errorf("full fidelity should preserve all %d turns, got %d", len(history), len(result))
	}
}

func TestIntegration_ApplyFidelityTruncate(t *testing.T) {
	// "truncate" mode should drop older turns from the middle but keep system,
	// first user turn, and recent turns
	history := buildTestHistory(50)
	result := ApplyFidelity(history, "truncate", 200000)

	// Should have fewer turns than original
	if len(result) >= len(history) {
		t.Errorf("truncate fidelity should reduce %d turns, got %d", len(history), len(result))
	}

	// First turn should be preserved (system or user)
	if len(result) > 0 {
		firstType := result[0].TurnType()
		if firstType != "system" && firstType != "user" {
			t.Errorf("expected first preserved turn to be system or user, got %q", firstType)
		}
	}

	// Last turn should be from the end of history
	if len(result) > 0 {
		lastResult := result[len(result)-1]
		lastOriginal := history[len(history)-1]
		if lastResult.TurnTimestamp() != lastOriginal.TurnTimestamp() {
			t.Error("expected last turn in truncated history to match last turn in original")
		}
	}
}

func TestIntegration_ApplyFidelityCompact(t *testing.T) {
	// "compact" mode should aggressively reduce: keep system turns, most recent
	// turns, and tool results only from recent context
	history := buildTestHistory(50)
	result := ApplyFidelity(history, "compact", 200000)

	// Should be significantly smaller
	if len(result) >= len(history) {
		t.Errorf("compact fidelity should significantly reduce %d turns, got %d", len(history), len(result))
	}

	// Should be smaller than truncate
	truncateResult := ApplyFidelity(history, "truncate", 200000)
	if len(result) > len(truncateResult) {
		t.Errorf("compact (%d) should be no larger than truncate (%d)", len(result), len(truncateResult))
	}
}

func TestIntegration_ApplyFidelitySummary(t *testing.T) {
	// summary modes should condense old turns into a summary turn
	history := buildTestHistory(50)

	for _, mode := range []string{"summary:low", "summary:medium", "summary:high"} {
		t.Run(mode, func(t *testing.T) {
			result := ApplyFidelity(history, mode, 200000)

			// Should have fewer turns
			if len(result) >= len(history) {
				t.Errorf("%s should reduce %d turns, got %d", mode, len(history), len(result))
			}

			// Should contain a system turn with summary marker
			hasSummary := false
			for _, turn := range result {
				if st, ok := turn.(SystemTurn); ok {
					if strings.Contains(st.Content, "[Context Summary]") {
						hasSummary = true
					}
				}
			}
			if !hasSummary {
				t.Errorf("%s should inject a summary SystemTurn", mode)
			}
		})
	}
}

func TestIntegration_ApplyFidelityEmptyAndDefault(t *testing.T) {
	history := buildTestHistory(10)

	// Empty mode should behave like "full"
	result := ApplyFidelity(history, "", 200000)
	if len(result) != len(history) {
		t.Errorf("empty fidelity should preserve all turns, got %d of %d", len(result), len(history))
	}

	// Unknown mode should behave like "full"
	result = ApplyFidelity(history, "unknown_mode", 200000)
	if len(result) != len(history) {
		t.Errorf("unknown fidelity should preserve all turns, got %d of %d", len(result), len(history))
	}
}

func TestIntegration_ApplyFidelitySmallHistory(t *testing.T) {
	// Small histories should not be truncated regardless of mode
	history := buildTestHistory(3)

	for _, mode := range []string{"truncate", "compact", "summary:low"} {
		t.Run(mode, func(t *testing.T) {
			result := ApplyFidelity(history, mode, 200000)
			if len(result) != len(history) {
				t.Errorf("%s with small history should preserve all %d turns, got %d", mode, len(history), len(result))
			}
		})
	}
}

// TestIntegration_FidelityAppliedInLoop verifies that when FidelityMode is set on
// SessionConfig, the agent loop applies it when building messages for the LLM.
func TestIntegration_FidelityAppliedInLoop(t *testing.T) {
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
		id:           "test",
		model:        "test-model",
		systemPrompt: "You are a test assistant.",
		toolDefs:     registry.Definitions(),
		registry:     registry,
	}

	env := newLoopTestEnv()
	config := DefaultSessionConfig()
	config.FidelityMode = "compact"
	session := NewSession(config)
	defer session.Close()

	// Pre-populate session with enough history that compact would reduce it
	for i := 0; i < 30; i++ {
		session.AppendTurn(UserTurn{
			Content:   fmt.Sprintf("Old message %d", i),
			Timestamp: time.Now().Add(-time.Duration(30-i) * time.Minute),
		})
		session.AppendTurn(AssistantTurn{
			Content:   fmt.Sprintf("Old response %d", i),
			Timestamp: time.Now().Add(-time.Duration(30-i)*time.Minute + time.Second),
		})
	}

	adapter := &loopTestAdapter{
		responses: []*llm.Response{
			makeTextResponse("Final response."),
		},
	}

	client := llm.NewClient(
		llm.WithProvider("test", adapter),
		llm.WithDefaultProvider("test"),
	)

	err := ProcessInput(context.Background(), session, profile, env, client, "new query after lots of history")
	if err != nil {
		t.Fatalf("ProcessInput error: %v", err)
	}

	// The LLM should have received fewer messages than the full history
	calls := adapter.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 LLM call, got %d", len(calls))
	}

	// Full history would be 60 old turns + 1 new user turn = 61 turns -> lots of messages
	// With compact fidelity, the LLM should receive fewer messages (plus system prompt)
	messagesCount := len(calls[0].Messages)
	fullHistoryCount := 61 + 1 // 61 turns + 1 system message
	if messagesCount >= fullHistoryCount {
		t.Errorf("compact fidelity should reduce messages sent to LLM: got %d, full would be %d",
			messagesCount, fullHistoryCount)
	}
}

// TestIntegration_MultiTurnWithSteeringAndTools exercises a complex multi-turn
// scenario with tool calls, steering injection, and followup processing.
func TestIntegration_MultiTurnWithSteeringAndTools(t *testing.T) {
	registry := NewToolRegistry()
	RegisterCoreTools(registry)

	// Also register a custom counter tool
	callCount := 0
	registry.Register(&RegisteredTool{
		Definition: llm.ToolDefinition{
			Name:        "counter",
			Description: "Increments and returns a counter",
			Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		},
		Execute: func(args map[string]any, env ExecutionEnvironment) (string, error) {
			callCount++
			return fmt.Sprintf("count=%d", callCount), nil
		},
	})

	profile := &testProfile{
		id:           "test",
		model:        "test-model",
		systemPrompt: "You are a test assistant.",
		toolDefs:     registry.Definitions(),
		registry:     registry,
	}

	env := newLoopTestEnv()
	config := DefaultSessionConfig()
	session := NewSession(config)
	defer session.Close()

	// Inject steering
	session.Steer("Remember to count things")
	// Queue a followup
	session.FollowUp("Now count one more time")

	counterArgs, _ := json.Marshal(map[string]any{})

	adapter := &loopTestAdapter{
		responses: []*llm.Response{
			// First input: call counter
			makeToolCallResponse(llm.ToolCallData{ID: "c1", Name: "counter", Arguments: counterArgs}),
			makeTextResponse("Counter is at 1."),
			// Followup: call counter again
			makeToolCallResponse(llm.ToolCallData{ID: "c2", Name: "counter", Arguments: counterArgs}),
			makeTextResponse("Counter is now at 2."),
		},
	}

	client := llm.NewClient(
		llm.WithProvider("test", adapter),
		llm.WithDefaultProvider("test"),
	)

	err := ProcessInput(context.Background(), session, profile, env, client, "start counting")
	if err != nil {
		t.Fatalf("ProcessInput error: %v", err)
	}

	// Counter should have been called twice (once per input)
	if callCount != 2 {
		t.Errorf("expected counter to be called 2 times, got %d", callCount)
	}

	// History should contain steering turn
	steeringFound := false
	for _, turn := range session.History {
		if st, ok := turn.(SteeringTurn); ok {
			if strings.Contains(st.Content, "count things") {
				steeringFound = true
			}
		}
	}
	if !steeringFound {
		t.Error("expected steering turn in history")
	}

	// There should be 2 user turns (original + followup)
	userTurns := 0
	for _, turn := range session.History {
		if _, ok := turn.(UserTurn); ok {
			userTurns++
		}
	}
	if userTurns != 2 {
		t.Errorf("expected 2 user turns (original + followup), got %d", userTurns)
	}

	// LLM should have been called 4 times total
	calls := adapter.getCalls()
	if len(calls) != 4 {
		t.Errorf("expected 4 LLM calls, got %d", len(calls))
	}

	// Session should be idle
	if session.State != StateIdle {
		t.Errorf("expected session state %s, got %s", StateIdle, session.State)
	}
}

// TestIntegration_EventsFlowThroughFullStack verifies that events are emitted
// at each stage of the full integration path.
func TestIntegration_EventsFlowThroughFullStack(t *testing.T) {
	registry := NewToolRegistry()
	RegisterCoreTools(registry)

	profile := &testProfile{
		id:           "test",
		model:        "test-model",
		systemPrompt: "You are a test assistant.",
		toolDefs:     registry.Definitions(),
		registry:     registry,
	}

	env := newLoopTestEnv()
	env.files["/tmp/test-agent/test.txt"] = "test content"

	config := DefaultSessionConfig()
	session := NewSession(config)
	defer session.Close()

	// Subscribe to events
	eventCh := session.EventEmitter.Subscribe()

	readArgs, _ := json.Marshal(map[string]any{"file_path": "/tmp/test-agent/test.txt"})
	adapter := &loopTestAdapter{
		responses: []*llm.Response{
			makeToolCallResponse(llm.ToolCallData{ID: "r1", Name: "read_file", Arguments: readArgs}),
			makeTextResponse("File content was: test content"),
		},
	}

	client := llm.NewClient(
		llm.WithProvider("test", adapter),
		llm.WithDefaultProvider("test"),
	)

	err := ProcessInput(context.Background(), session, profile, env, client, "read the test file")
	if err != nil {
		t.Fatalf("ProcessInput error: %v", err)
	}

	// Collect events
	var events []SessionEvent
	for {
		select {
		case evt, ok := <-eventCh:
			if !ok {
				goto done
			}
			events = append(events, evt)
		default:
			goto done
		}
	}
done:

	// Verify the expected sequence of event kinds
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
		for _, actual := range events {
			if actual.Kind == expected {
				found = true
				break
			}
		}
		if !found {
			kinds := make([]EventKind, len(events))
			for i, e := range events {
				kinds[i] = e.Kind
			}
			t.Errorf("expected event %q not found in events: %v", expected, kinds)
		}
	}

	// The TOOL_CALL_END event should contain the read_file output
	for _, evt := range events {
		if evt.Kind == EventToolCallEnd {
			if output, ok := evt.Data["output"].(string); ok {
				if !strings.Contains(output, "test content") {
					t.Errorf("expected TOOL_CALL_END output to contain 'test content', got %q", output)
				}
			}
		}
	}
}

// --- Helpers ---

// buildTestHistory creates a test history with the given number of user/assistant turn pairs.
func buildTestHistory(pairs int) []Turn {
	var history []Turn
	baseTime := time.Now().Add(-time.Hour)

	for i := 0; i < pairs; i++ {
		history = append(history, UserTurn{
			Content:   fmt.Sprintf("User message %d", i),
			Timestamp: baseTime.Add(time.Duration(i*2) * time.Minute),
		})
		history = append(history, AssistantTurn{
			Content:   fmt.Sprintf("Assistant response %d", i),
			Timestamp: baseTime.Add(time.Duration(i*2+1) * time.Minute),
		})
	}

	return history
}
