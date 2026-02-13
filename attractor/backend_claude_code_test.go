// ABOUTME: Tests for ClaudeCodeBackend which shells out to the claude CLI for codergen nodes.
// ABOUTME: Covers JSONL event parsing, argument building, constructor validation, and interface conformance.
package attractor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// --- Compile-time interface check ---

func TestClaudeCodeBackendImplementsInterface(t *testing.T) {
	var _ CodergenBackend = (*ClaudeCodeBackend)(nil)
}

// --- JSONL event parsing tests ---

func TestParseClaudeStreamEvent_Result(t *testing.T) {
	line := `{"type":"result","subtype":"success","result":"Task completed successfully.","is_error":false,"num_turns":3,"usage":{"input_tokens":1000,"output_tokens":500,"cache_read_input_tokens":200,"cache_creation_input_tokens":50,"thinking_tokens":100}}`

	evt, err := parseClaudeStreamEvent([]byte(line))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if evt.Type != "result" {
		t.Errorf("expected type 'result', got %q", evt.Type)
	}
	if evt.Subtype != "success" {
		t.Errorf("expected subtype 'success', got %q", evt.Subtype)
	}
	if evt.Result != "Task completed successfully." {
		t.Errorf("expected result text, got %q", evt.Result)
	}
	if evt.IsError {
		t.Error("expected is_error=false")
	}
	if evt.NumTurns != 3 {
		t.Errorf("expected num_turns=3, got %d", evt.NumTurns)
	}
	if evt.Usage == nil {
		t.Fatal("expected usage to be non-nil")
	}
	if evt.Usage.InputTokens != 1000 {
		t.Errorf("expected input_tokens=1000, got %d", evt.Usage.InputTokens)
	}
	if evt.Usage.OutputTokens != 500 {
		t.Errorf("expected output_tokens=500, got %d", evt.Usage.OutputTokens)
	}
	if evt.Usage.CacheReadTokens != 200 {
		t.Errorf("expected cache_read_tokens=200, got %d", evt.Usage.CacheReadTokens)
	}
	if evt.Usage.CacheCreationTokens != 50 {
		t.Errorf("expected cache_creation_tokens=50, got %d", evt.Usage.CacheCreationTokens)
	}
	if evt.Usage.ThinkingTokens != 100 {
		t.Errorf("expected thinking_tokens=100, got %d", evt.Usage.ThinkingTokens)
	}
}

func TestParseClaudeStreamEvent_Error(t *testing.T) {
	line := `{"type":"result","subtype":"error","result":"Something went wrong","is_error":true,"num_turns":1,"usage":{"input_tokens":100,"output_tokens":10}}`

	evt, err := parseClaudeStreamEvent([]byte(line))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if evt.Type != "result" {
		t.Errorf("expected type 'result', got %q", evt.Type)
	}
	if !evt.IsError {
		t.Error("expected is_error=true")
	}
	if evt.Result != "Something went wrong" {
		t.Errorf("expected error result text, got %q", evt.Result)
	}
}

func TestParseClaudeStreamEvent_InvalidJSON(t *testing.T) {
	line := `{not valid json at all`

	_, err := parseClaudeStreamEvent([]byte(line))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseClaudeStreamEvent_SystemInit(t *testing.T) {
	line := `{"type":"system","subtype":"init","session_id":"sess-abc123"}`

	evt, err := parseClaudeStreamEvent([]byte(line))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if evt.Type != "system" {
		t.Errorf("expected type 'system', got %q", evt.Type)
	}
	if evt.Subtype != "init" {
		t.Errorf("expected subtype 'init', got %q", evt.Subtype)
	}
	if evt.SessionID != "sess-abc123" {
		t.Errorf("expected session_id 'sess-abc123', got %q", evt.SessionID)
	}
}

func TestParseClaudeStreamEvent_AssistantText(t *testing.T) {
	line := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Hello world"}]}}`

	evt, err := parseClaudeStreamEvent([]byte(line))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if evt.Type != "assistant" {
		t.Errorf("expected type 'assistant', got %q", evt.Type)
	}
}

func TestParseClaudeStreamEvent_CostUSD(t *testing.T) {
	line := `{"type":"result","subtype":"success","result":"done","is_error":false,"num_turns":1,"total_cost_usd":0.1464}`

	evt, err := parseClaudeStreamEvent([]byte(line))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if evt.CostUSD != 0.1464 {
		t.Errorf("expected total_cost_usd=0.1464, got %f", evt.CostUSD)
	}
}

func TestParseClaudeStreamEvent_OutcomeMarkers(t *testing.T) {
	tests := []struct {
		name     string
		result   string
		wantPass bool
		wantFail bool
	}{
		{
			name:     "OUTCOME:PASS marker",
			result:   "All tests pass.\nOUTCOME:PASS",
			wantPass: true,
		},
		{
			name:     "OUTCOME:FAIL marker",
			result:   "Tests failed.\nOUTCOME:FAIL",
			wantFail: true,
		},
		{
			name:     "no marker",
			result:   "Just some text output.",
			wantPass: false,
			wantFail: false,
		},
		{
			name:     "both markers - FAIL wins",
			result:   "OUTCOME:PASS but then OUTCOME:FAIL",
			wantPass: true,
			wantFail: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			success := claudeResultToSuccess(tt.result, false)
			if tt.wantFail {
				if success {
					t.Error("expected success=false when OUTCOME:FAIL is present")
				}
			} else if tt.wantPass || (!tt.wantPass && !tt.wantFail) {
				// No FAIL marker: success should be true (default or explicit PASS)
				if !success {
					t.Error("expected success=true")
				}
			}
		})
	}
}

func TestParseClaudeStreamEvent_Usage(t *testing.T) {
	line := `{"type":"result","subtype":"success","result":"done","is_error":false,"num_turns":5,"usage":{"input_tokens":2000,"output_tokens":800,"cache_read_input_tokens":500,"cache_creation_input_tokens":100,"thinking_tokens":300}}`

	evt, err := parseClaudeStreamEvent([]byte(line))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	usage := claudeUsageToTokenUsage(evt.Usage)
	if usage.InputTokens != 2000 {
		t.Errorf("expected InputTokens=2000, got %d", usage.InputTokens)
	}
	if usage.OutputTokens != 800 {
		t.Errorf("expected OutputTokens=800, got %d", usage.OutputTokens)
	}
	expectedTotal := 2000 + 800 + 300
	if usage.TotalTokens != expectedTotal {
		t.Errorf("expected TotalTokens=%d, got %d", expectedTotal, usage.TotalTokens)
	}
	if usage.ReasoningTokens != 300 {
		t.Errorf("expected ReasoningTokens=300, got %d", usage.ReasoningTokens)
	}
	if usage.CacheReadTokens != 500 {
		t.Errorf("expected CacheReadTokens=500, got %d", usage.CacheReadTokens)
	}
	if usage.CacheWriteTokens != 100 {
		t.Errorf("expected CacheWriteTokens=100, got %d", usage.CacheWriteTokens)
	}
}

func TestParseClaudeStreamEvent_NilUsage(t *testing.T) {
	usage := claudeUsageToTokenUsage(nil)
	if usage.TotalTokens != 0 {
		t.Errorf("expected zero usage for nil input, got TotalTokens=%d", usage.TotalTokens)
	}
}

// --- Argument building tests ---

func TestBuildClaudeArgs_Defaults(t *testing.T) {
	b := &ClaudeCodeBackend{
		BinaryPath:      "/usr/local/bin/claude",
		SkipPermissions: true,
	}

	args := b.buildArgs("Write hello world", AgentRunConfig{
		Prompt: "Write hello world",
		NodeID: "test-node",
	})

	assertContains(t, args, "--print")
	assertContains(t, args, "--verbose")
	assertContains(t, args, "--output-format")
	assertContains(t, args, "stream-json")
	assertContains(t, args, "--dangerously-skip-permissions")
	assertContains(t, args, "--no-session-persistence")

	// Should NOT have --model when no model is set
	assertNotContains(t, args, "--model")
	// Should NOT have --max-budget-usd when zero
	assertNotContains(t, args, "--max-budget-usd")

	// User input should be the last argument
	last := args[len(args)-1]
	if last != "Write hello world" {
		t.Errorf("expected prompt as last arg, got %q", last)
	}
}

func TestBuildClaudeArgs_WithModel(t *testing.T) {
	b := &ClaudeCodeBackend{
		BinaryPath:      "/usr/local/bin/claude",
		SkipPermissions: true,
	}

	args := b.buildArgs("task", AgentRunConfig{
		Prompt: "task",
		Model:  "claude-sonnet-4-5",
	})

	assertContains(t, args, "--model")
	assertContains(t, args, "claude-sonnet-4-5")
}

func TestBuildClaudeArgs_WithMaxBudget(t *testing.T) {
	b := &ClaudeCodeBackend{
		BinaryPath:      "/usr/local/bin/claude",
		SkipPermissions: true,
		MaxBudgetUSD:    5.00,
	}

	args := b.buildArgs("task", AgentRunConfig{
		Prompt: "task",
	})

	assertContains(t, args, "--max-budget-usd")
	assertContains(t, args, "5.00")
}

func TestBuildClaudeArgs_MaxTurnsIgnored(t *testing.T) {
	// The claude CLI does not support --max-turns. Verify it is NOT passed.
	b := &ClaudeCodeBackend{
		BinaryPath:      "/usr/local/bin/claude",
		SkipPermissions: true,
	}

	args := b.buildArgs("task", AgentRunConfig{
		Prompt:   "task",
		MaxTurns: 15,
	})

	assertNotContains(t, args, "--max-turns")
	assertNotContains(t, args, "15")
}

func TestBuildClaudeArgs_WithCustomTools(t *testing.T) {
	b := &ClaudeCodeBackend{
		BinaryPath:      "/usr/local/bin/claude",
		AllowedTools:    []string{"Bash", "Read", "Write"},
		SkipPermissions: true,
	}

	args := b.buildArgs("task", AgentRunConfig{
		Prompt: "task",
	})

	assertContains(t, args, "--allowedTools")
	assertContains(t, args, "Bash,Read,Write")
}

func TestBuildClaudeArgs_WithAppendSystemPrompt(t *testing.T) {
	b := &ClaudeCodeBackend{
		BinaryPath:         "/usr/local/bin/claude",
		SkipPermissions:    true,
		AppendSystemPrompt: "Always write tests first.",
	}

	args := b.buildArgs("task", AgentRunConfig{
		Prompt: "task",
	})

	assertContains(t, args, "--append-system-prompt")
	assertContains(t, args, "Always write tests first.")
}

func TestBuildClaudeArgs_SkipPermissionsFalse(t *testing.T) {
	b := &ClaudeCodeBackend{
		BinaryPath:      "/usr/local/bin/claude",
		SkipPermissions: false,
	}

	args := b.buildArgs("task", AgentRunConfig{
		Prompt: "task",
	})

	assertNotContains(t, args, "--dangerously-skip-permissions")
}

func TestBuildClaudeArgs_DefaultModel(t *testing.T) {
	b := &ClaudeCodeBackend{
		BinaryPath:      "/usr/local/bin/claude",
		DefaultModel:    "claude-haiku-3-5",
		SkipPermissions: true,
	}

	// When config has no model, default should be used
	args := b.buildArgs("task", AgentRunConfig{
		Prompt: "task",
	})
	assertContains(t, args, "--model")
	assertContains(t, args, "claude-haiku-3-5")

	// When config has a model, it should override the default
	args = b.buildArgs("task", AgentRunConfig{
		Prompt: "task",
		Model:  "claude-opus-4-6",
	})
	assertContains(t, args, "claude-opus-4-6")
	assertNotContains(t, args, "claude-haiku-3-5")
}

// --- Constructor tests ---

func TestNewClaudeCodeBackend_BinaryNotFound(t *testing.T) {
	// Use a binary path that definitely doesn't exist
	_, err := NewClaudeCodeBackend(WithClaudeBinaryPath("/nonexistent/path/to/claude-binary-xxx"))
	if err == nil {
		t.Error("expected error when binary not found")
	}
}

func TestNewClaudeCodeBackend_CustomPath(t *testing.T) {
	// Use the go binary as a stand-in since we just need something that exists.
	testBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go binary not found in PATH")
	}

	backend, err := NewClaudeCodeBackend(WithClaudeBinaryPath(testBin))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if backend.BinaryPath != testBin {
		t.Errorf("expected BinaryPath=%q, got %q", testBin, backend.BinaryPath)
	}
}

func TestNewClaudeCodeBackend_DefaultSkipPermissions(t *testing.T) {
	testBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go binary not found in PATH")
	}

	backend, err := NewClaudeCodeBackend(WithClaudeBinaryPath(testBin))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !backend.SkipPermissions {
		t.Error("expected SkipPermissions=true by default")
	}
}

func TestNewClaudeCodeBackend_FunctionalOptions(t *testing.T) {
	testBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go binary not found in PATH")
	}

	backend, err := NewClaudeCodeBackend(
		WithClaudeBinaryPath(testBin),
		WithClaudeModel("claude-sonnet-4-5"),
		WithClaudeAllowedTools([]string{"Bash", "Read"}),
		WithClaudeSkipPermissions(false),
		WithClaudeAppendSystemPrompt("Be concise."),
		WithClaudeMaxBudgetUSD(10.0),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if backend.DefaultModel != "claude-sonnet-4-5" {
		t.Errorf("expected DefaultModel='claude-sonnet-4-5', got %q", backend.DefaultModel)
	}
	if len(backend.AllowedTools) != 2 || backend.AllowedTools[0] != "Bash" {
		t.Errorf("expected AllowedTools=[Bash,Read], got %v", backend.AllowedTools)
	}
	if backend.SkipPermissions {
		t.Error("expected SkipPermissions=false")
	}
	if backend.AppendSystemPrompt != "Be concise." {
		t.Errorf("expected AppendSystemPrompt='Be concise.', got %q", backend.AppendSystemPrompt)
	}
	if backend.MaxBudgetUSD != 10.0 {
		t.Errorf("expected MaxBudgetUSD=10.0, got %f", backend.MaxBudgetUSD)
	}
}

// --- claudeResultToSuccess tests ---

func TestClaudeResultToSuccess_ErrorFlag(t *testing.T) {
	// When is_error is true, success should be false regardless of text
	success := claudeResultToSuccess("Everything is fine", true)
	if success {
		t.Error("expected success=false when is_error=true")
	}
}

func TestClaudeResultToSuccess_NoMarkerNoError(t *testing.T) {
	success := claudeResultToSuccess("Just finished the task.", false)
	if !success {
		t.Error("expected success=true when no markers and no error")
	}
}

// --- RunAgent context cancellation test ---

func TestClaudeCodeBackend_RunAgentCancelledContext(t *testing.T) {
	testBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go binary not found in PATH")
	}

	backend, err := NewClaudeCodeBackend(WithClaudeBinaryPath(testBin))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, runErr := backend.RunAgent(ctx, AgentRunConfig{
		Prompt: "test",
		NodeID: "cancel-node",
	})
	if runErr == nil {
		t.Error("expected error from cancelled context")
	}
}

// --- Mock subprocess test ---

func TestClaudeCodeBackend_RunAgentWithMockScript(t *testing.T) {
	// Create a shell script that outputs known JSONL, exercising the full
	// RunAgent pipeline (stdout pipe, JSONL parsing, result extraction)
	// without requiring the real claude binary or API access.
	scriptDir := t.TempDir()
	scriptPath := filepath.Join(scriptDir, "mock-claude")

	script := `#!/bin/sh
printf '%s\n' '{"type":"system","subtype":"init","session_id":"test-session-123"}'
printf '%s\n' '{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Working on it..."},{"type":"tool_use","id":"tc_1","name":"Bash","input":{"command":"echo hi"}}]}}'
printf '%s\n' '{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"All done."}]}}'
printf '%s\n' '{"type":"result","subtype":"success","result":"Task completed successfully.\\nOUTCOME:PASS","is_error":false,"num_turns":2,"total_cost_usd":0.05,"usage":{"input_tokens":500,"output_tokens":200,"cache_read_input_tokens":100,"cache_creation_input_tokens":25,"thinking_tokens":50}}'
`
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write mock script: %v", err)
	}

	backend, err := NewClaudeCodeBackend(WithClaudeBinaryPath(scriptPath))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Track events emitted by RunAgent
	var events []EngineEvent
	handler := func(evt EngineEvent) {
		events = append(events, evt)
	}

	result, err := backend.RunAgent(context.Background(), AgentRunConfig{
		Prompt:       "test task",
		WorkDir:      t.TempDir(),
		NodeID:       "mock-node",
		EventHandler: handler,
	})
	if err != nil {
		t.Fatalf("RunAgent failed: %v", err)
	}

	// Verify result fields
	if !result.Success {
		t.Error("expected success=true")
	}
	if result.TurnCount != 2 {
		t.Errorf("expected TurnCount=2, got %d", result.TurnCount)
	}
	if result.ToolCalls != 1 {
		t.Errorf("expected ToolCalls=1, got %d", result.ToolCalls)
	}
	if result.Usage.InputTokens != 500 {
		t.Errorf("expected InputTokens=500, got %d", result.Usage.InputTokens)
	}
	if result.Usage.OutputTokens != 200 {
		t.Errorf("expected OutputTokens=200, got %d", result.Usage.OutputTokens)
	}
	if result.Usage.ReasoningTokens != 50 {
		t.Errorf("expected ReasoningTokens=50, got %d", result.Usage.ReasoningTokens)
	}
	expectedTotal := 500 + 200 + 50
	if result.TokensUsed != expectedTotal {
		t.Errorf("expected TokensUsed=%d, got %d", expectedTotal, result.TokensUsed)
	}

	// Verify events were emitted
	foundSessionStart := false
	foundToolCallStart := false
	foundLLMTurn := false
	for _, evt := range events {
		switch evt.Type {
		case EventStageStarted:
			if sid, ok := evt.Data["claude_session_id"]; ok && sid == "test-session-123" {
				foundSessionStart = true
			}
		case EventAgentToolCallStart:
			if name, ok := evt.Data["tool_name"]; ok && name == "Bash" {
				foundToolCallStart = true
			}
		case EventAgentLLMTurn:
			foundLLMTurn = true
		}
	}
	if !foundSessionStart {
		t.Error("expected EventStageStarted with session ID")
	}
	if !foundToolCallStart {
		t.Error("expected EventAgentToolCallStart for Bash tool")
	}
	if !foundLLMTurn {
		t.Error("expected EventAgentLLMTurn for assistant message")
	}
}

func TestClaudeCodeBackend_RunAgentWithMockError(t *testing.T) {
	// Test that an error result is correctly parsed
	scriptDir := t.TempDir()
	scriptPath := filepath.Join(scriptDir, "mock-claude-error")

	script := `#!/bin/sh
echo '{"type":"result","subtype":"error","result":"Something went wrong","is_error":true,"num_turns":0,"usage":{"input_tokens":100,"output_tokens":10}}'
`
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write mock script: %v", err)
	}

	backend, err := NewClaudeCodeBackend(WithClaudeBinaryPath(scriptPath))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := backend.RunAgent(context.Background(), AgentRunConfig{
		Prompt:  "fail task",
		WorkDir: t.TempDir(),
		NodeID:  "error-node",
	})
	if err != nil {
		t.Fatalf("RunAgent should not return error for parsed result: %v", err)
	}

	if result.Success {
		t.Error("expected success=false for error result")
	}
	if result.Output != "Something went wrong" {
		t.Errorf("expected error output, got %q", result.Output)
	}
}

func TestClaudeCodeBackend_RunAgentWithOutcomeFail(t *testing.T) {
	// Test that OUTCOME:FAIL marker in result text is detected
	scriptDir := t.TempDir()
	scriptPath := filepath.Join(scriptDir, "mock-claude-fail")

	script := `#!/bin/sh
echo '{"type":"result","subtype":"success","result":"Tests failed.\nOUTCOME:FAIL","is_error":false,"num_turns":1,"usage":{"input_tokens":100,"output_tokens":50}}'
`
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write mock script: %v", err)
	}

	backend, err := NewClaudeCodeBackend(WithClaudeBinaryPath(scriptPath))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := backend.RunAgent(context.Background(), AgentRunConfig{
		Prompt:  "review task",
		WorkDir: t.TempDir(),
		NodeID:  "fail-node",
	})
	if err != nil {
		t.Fatalf("RunAgent should not return error for parsed result: %v", err)
	}

	if result.Success {
		t.Error("expected success=false for OUTCOME:FAIL result")
	}
}

// --- Integration test (gated on claude binary) ---

func TestClaudeCodeBackend_RealExecution(t *testing.T) {
	if os.Getenv("CLAUDECODE") != "" {
		t.Skip("skipping: cannot run nested Claude Code sessions")
	}
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		t.Skip("claude binary not found in PATH, skipping integration test")
	}

	backend, err := NewClaudeCodeBackend(WithClaudeBinaryPath(claudePath))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result, err := backend.RunAgent(ctx, AgentRunConfig{
		Prompt:  "Reply with exactly: hello mammoth",
		WorkDir: t.TempDir(),
		NodeID:  "integration-test",
	})
	if err != nil {
		t.Fatalf("RunAgent failed: %v", err)
	}

	if !result.Success {
		t.Errorf("expected success, got failure with output: %q", result.Output)
	}
	if result.Output == "" {
		t.Error("expected non-empty output")
	}
	if result.TurnCount < 1 {
		t.Errorf("expected at least 1 turn, got %d", result.TurnCount)
	}
}

// --- test helpers ---

func assertContains(t *testing.T, slice []string, val string) {
	t.Helper()
	for _, s := range slice {
		if s == val {
			return
		}
	}
	t.Errorf("expected args to contain %q, got %v", val, slice)
}

func assertNotContains(t *testing.T, slice []string, val string) {
	t.Helper()
	for _, s := range slice {
		if s == val {
			t.Errorf("expected args NOT to contain %q, got %v", val, slice)
			return
		}
	}
}
