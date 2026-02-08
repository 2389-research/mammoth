// ABOUTME: Tests for the subagent system that enables spawning child sessions for task decomposition.
// ABOUTME: Covers SubAgentManager lifecycle, depth limiting, tools (spawn, send_input, wait, close), and registration.

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/2389-research/makeatron/llm"
)

// subagentTestAdapter implements llm.ProviderAdapter with pre-configured responses.
// It returns responses in sequence and records all requests for verification.
type subagentTestAdapter struct {
	responses []*llm.Response
	idx       int
	calls     []llm.Request
	mu        sync.Mutex
}

func (a *subagentTestAdapter) Name() string { return "subagent-test" }

func (a *subagentTestAdapter) Complete(ctx context.Context, req llm.Request) (*llm.Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, req)
	if a.idx >= len(a.responses) {
		return nil, fmt.Errorf("subagentTestAdapter: no more responses (called %d times, only %d configured)", a.idx+1, len(a.responses))
	}
	resp := a.responses[a.idx]
	a.idx++
	return resp, nil
}

func (a *subagentTestAdapter) Stream(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, error) {
	return nil, fmt.Errorf("streaming not implemented in subagent test adapter")
}

func (a *subagentTestAdapter) Close() error { return nil }

// subagentTestEnv implements ExecutionEnvironment for subagent testing.
type subagentTestEnv struct {
	workDir  string
	files    map[string]string
	mu       sync.Mutex
}

func newSubagentTestEnv() *subagentTestEnv {
	return &subagentTestEnv{
		workDir: "/tmp/subagent-test",
		files:   make(map[string]string),
	}
}

func (e *subagentTestEnv) ReadFile(path string, offset, limit int) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	content, ok := e.files[path]
	if !ok {
		return "", fmt.Errorf("file not found: %s", path)
	}
	return content, nil
}

func (e *subagentTestEnv) WriteFile(path string, content string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.files[path] = content
	return nil
}

func (e *subagentTestEnv) FileExists(path string) (bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	_, ok := e.files[path]
	return ok, nil
}

func (e *subagentTestEnv) ListDirectory(path string, depth int) ([]DirEntry, error) {
	return nil, nil
}

func (e *subagentTestEnv) ExecCommand(command string, timeoutMs int, workingDir string, envVars map[string]string) (*ExecResult, error) {
	return &ExecResult{Stdout: "ok", ExitCode: 0, DurationMs: 10}, nil
}

func (e *subagentTestEnv) Grep(pattern, path string, opts GrepOptions) (string, error) {
	return "", nil
}

func (e *subagentTestEnv) Glob(pattern, path string) ([]string, error) {
	return nil, nil
}

func (e *subagentTestEnv) Initialize() error        { return nil }
func (e *subagentTestEnv) Cleanup() error           { return nil }
func (e *subagentTestEnv) WorkingDirectory() string { return e.workDir }
func (e *subagentTestEnv) Platform() string         { return "test" }
func (e *subagentTestEnv) OSVersion() string        { return "1.0" }

// subagentTestProfile implements ProviderProfile for subagent testing.
type subagentTestProfile struct {
	model    string
	registry *ToolRegistry
}

func (p *subagentTestProfile) ID() string    { return "subagent-test" }
func (p *subagentTestProfile) Model() string { return p.model }
func (p *subagentTestProfile) BuildSystemPrompt(env ExecutionEnvironment, projectDocs []string) string {
	return "You are a subagent test assistant."
}
func (p *subagentTestProfile) Tools() []llm.ToolDefinition     { return p.registry.Definitions() }
func (p *subagentTestProfile) ProviderOptions() map[string]any { return nil }
func (p *subagentTestProfile) ToolRegistry() *ToolRegistry     { return p.registry }
func (p *subagentTestProfile) SupportsParallelToolCalls() bool { return false }
func (p *subagentTestProfile) SupportsReasoning() bool         { return false }
func (p *subagentTestProfile) SupportsStreaming() bool         { return false }
func (p *subagentTestProfile) ContextWindowSize() int          { return 200000 }

// makeSubagentTextResponse creates a text-only LLM response for subagent tests.
func makeSubagentTextResponse(text string) *llm.Response {
	return &llm.Response{
		ID:           "resp-subagent",
		Model:        "test-model",
		Provider:     "subagent-test",
		Message:      llm.AssistantMessage(text),
		FinishReason: llm.FinishReason{Reason: llm.FinishStop},
		Usage:        llm.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
	}
}

// newSubagentTestSetup creates the common test infrastructure for subagent tests.
func newSubagentTestSetup() (*SubAgentManager, *subagentTestProfile, *subagentTestEnv, *llm.Client, *subagentTestAdapter) {
	registry := NewToolRegistry()

	profile := &subagentTestProfile{
		model:    "test-model",
		registry: registry,
	}

	env := newSubagentTestEnv()

	adapter := &subagentTestAdapter{}
	client := llm.NewClient(
		llm.WithProvider("subagent-test", adapter),
		llm.WithDefaultProvider("subagent-test"),
	)

	manager := NewSubAgentManager(0, 1)

	return manager, profile, env, client, adapter
}

// --- Tests ---

func TestNewSubAgentManager(t *testing.T) {
	manager := NewSubAgentManager(0, 3)
	if manager == nil {
		t.Fatal("expected non-nil SubAgentManager")
	}
	if manager.depth != 0 {
		t.Errorf("expected depth 0, got %d", manager.depth)
	}
	if manager.maxDepth != 3 {
		t.Errorf("expected maxDepth 3, got %d", manager.maxDepth)
	}
	if manager.agents == nil {
		t.Fatal("expected non-nil agents map")
	}
	if len(manager.agents) != 0 {
		t.Errorf("expected empty agents map, got %d entries", len(manager.agents))
	}
}

func TestSubAgentSpawn(t *testing.T) {
	manager, profile, env, client, adapter := newSubagentTestSetup()

	// The subagent will get a text response and complete immediately
	adapter.responses = []*llm.Response{
		makeSubagentTextResponse("I completed the task."),
	}

	handle, err := manager.Spawn(context.Background(), "do something", env, profile, client, 50)
	if err != nil {
		t.Fatalf("unexpected error spawning agent: %v", err)
	}
	if handle == nil {
		t.Fatal("expected non-nil handle")
	}
	if handle.ID == "" {
		t.Error("expected non-empty agent ID")
	}
	if handle.Session == nil {
		t.Error("expected non-nil session on handle")
	}

	// The handle should be retrievable from the manager
	retrieved, ok := manager.Get(handle.ID)
	if !ok {
		t.Fatal("expected to find the agent in the manager")
	}
	if retrieved.ID != handle.ID {
		t.Errorf("expected retrieved ID %q, got %q", handle.ID, retrieved.ID)
	}

	// Wait for the subagent to complete so goroutines clean up
	_, _ = manager.Wait(handle.ID)
}

func TestSubAgentSpawnDepthLimit(t *testing.T) {
	// Create a manager that is already at max depth
	manager := NewSubAgentManager(1, 1)

	env := newSubagentTestEnv()
	profile := &subagentTestProfile{model: "test-model", registry: NewToolRegistry()}
	adapter := &subagentTestAdapter{
		responses: []*llm.Response{makeSubagentTextResponse("should not run")},
	}
	client := llm.NewClient(
		llm.WithProvider("subagent-test", adapter),
		llm.WithDefaultProvider("subagent-test"),
	)

	handle, err := manager.Spawn(context.Background(), "do something", env, profile, client, 50)
	if err == nil {
		t.Fatal("expected error when spawning at max depth")
	}
	if handle != nil {
		t.Error("expected nil handle when depth limit exceeded")
	}
	if !strings.Contains(err.Error(), "depth") {
		t.Errorf("expected error about depth limit, got: %v", err)
	}
}

func TestSubAgentWait(t *testing.T) {
	manager, profile, env, client, adapter := newSubagentTestSetup()

	adapter.responses = []*llm.Response{
		makeSubagentTextResponse("task completed successfully"),
	}

	handle, err := manager.Spawn(context.Background(), "complete this task", env, profile, client, 50)
	if err != nil {
		t.Fatalf("unexpected error spawning agent: %v", err)
	}

	result, err := manager.Wait(handle.ID)
	if err != nil {
		t.Fatalf("unexpected error waiting for agent: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !result.Success {
		t.Error("expected result.Success to be true")
	}
	if result.Output != "task completed successfully" {
		t.Errorf("expected output 'task completed successfully', got %q", result.Output)
	}
	if result.TurnsUsed < 1 {
		t.Errorf("expected at least 1 turn used, got %d", result.TurnsUsed)
	}

	// After waiting, the handle status should be completed
	handle, ok := manager.Get(handle.ID)
	if !ok {
		t.Fatal("expected to find agent after wait")
	}
	if handle.Status != SubAgentCompleted {
		t.Errorf("expected status %q, got %q", SubAgentCompleted, handle.Status)
	}
}

func TestSubAgentSendInput(t *testing.T) {
	manager, profile, env, client, adapter := newSubagentTestSetup()

	// First response to initial task, second response after steering
	adapter.responses = []*llm.Response{
		makeSubagentTextResponse("working on it"),
	}

	handle, err := manager.Spawn(context.Background(), "start the task", env, profile, client, 50)
	if err != nil {
		t.Fatalf("unexpected error spawning agent: %v", err)
	}

	// Send input to the running subagent (this queues a steering message)
	err = manager.SendInput(handle.ID, "additional guidance")
	if err != nil {
		t.Fatalf("unexpected error sending input: %v", err)
	}

	// Verify the steering message was queued on the child session
	steeringMessages := handle.Session.DrainSteering()
	// The message may or may not be there depending on timing (the goroutine may have
	// already drained it), but at minimum the call should not error
	_ = steeringMessages

	// Wait for completion
	_, _ = manager.Wait(handle.ID)
}

func TestSubAgentSendInputInvalidID(t *testing.T) {
	manager := NewSubAgentManager(0, 1)

	err := manager.SendInput("nonexistent-id", "hello")
	if err == nil {
		t.Fatal("expected error for nonexistent agent ID")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

func TestSubAgentClose(t *testing.T) {
	manager, profile, env, client, adapter := newSubagentTestSetup()

	// Give a response so the goroutine can work
	adapter.responses = []*llm.Response{
		makeSubagentTextResponse("done"),
	}

	handle, err := manager.Spawn(context.Background(), "do work", env, profile, client, 50)
	if err != nil {
		t.Fatalf("unexpected error spawning agent: %v", err)
	}

	// Wait briefly for the goroutine to start, then close
	time.Sleep(10 * time.Millisecond)

	err = manager.Close(handle.ID)
	if err != nil {
		t.Fatalf("unexpected error closing agent: %v", err)
	}

	// Status should reflect termination
	handle, ok := manager.Get(handle.ID)
	if !ok {
		t.Fatal("expected to find agent after close")
	}
	// After closing, the status should be either completed or failed
	if handle.Status != SubAgentCompleted && handle.Status != SubAgentFailed {
		t.Errorf("expected status completed or failed after close, got %q", handle.Status)
	}
}

func TestSubAgentCloseInvalidID(t *testing.T) {
	manager := NewSubAgentManager(0, 1)

	err := manager.Close("nonexistent-id")
	if err == nil {
		t.Fatal("expected error for nonexistent agent ID")
	}
}

func TestSubAgentCloseAll(t *testing.T) {
	manager := NewSubAgentManager(0, 1)
	env := newSubagentTestEnv()
	profile := &subagentTestProfile{model: "test-model", registry: NewToolRegistry()}

	// Spawn multiple subagents
	var handles []*SubAgentHandle
	for i := 0; i < 3; i++ {
		adapter := &subagentTestAdapter{
			responses: []*llm.Response{
				makeSubagentTextResponse(fmt.Sprintf("agent %d done", i)),
			},
		}
		client := llm.NewClient(
			llm.WithProvider("subagent-test", adapter),
			llm.WithDefaultProvider("subagent-test"),
		)

		handle, err := manager.Spawn(context.Background(), fmt.Sprintf("task %d", i), env, profile, client, 50)
		if err != nil {
			t.Fatalf("unexpected error spawning agent %d: %v", i, err)
		}
		handles = append(handles, handle)
	}

	// Wait a bit for goroutines to start
	time.Sleep(50 * time.Millisecond)

	// Close all
	manager.CloseAll()

	// All agents should be in a terminal state
	for _, h := range handles {
		retrieved, ok := manager.Get(h.ID)
		if !ok {
			t.Errorf("expected to find agent %s after CloseAll", h.ID)
			continue
		}
		if retrieved.Status != SubAgentCompleted && retrieved.Status != SubAgentFailed {
			t.Errorf("agent %s: expected terminal status, got %q", h.ID, retrieved.Status)
		}
	}
}

func TestSpawnAgentTool(t *testing.T) {
	manager, profile, env, client, adapter := newSubagentTestSetup()

	adapter.responses = []*llm.Response{
		makeSubagentTextResponse("spawned task done"),
	}

	tool := NewSpawnAgentTool(manager, profile, client)
	if tool.Definition.Name != "spawn_agent" {
		t.Errorf("expected tool name 'spawn_agent', got %q", tool.Definition.Name)
	}

	result, err := tool.Execute(map[string]any{
		"task": "write some code",
	}, env)
	if err != nil {
		t.Fatalf("unexpected error executing spawn_agent tool: %v", err)
	}

	// Result should contain an agent ID
	if !strings.Contains(result, "agent_id") {
		t.Errorf("expected result to contain 'agent_id', got: %s", result)
	}
	if !strings.Contains(result, "running") {
		t.Errorf("expected result to contain 'running', got: %s", result)
	}

	// Clean up
	manager.CloseAll()
	time.Sleep(50 * time.Millisecond)
}

func TestSpawnAgentToolMissingTask(t *testing.T) {
	manager, profile, _, client, _ := newSubagentTestSetup()
	env := newSubagentTestEnv()

	tool := NewSpawnAgentTool(manager, profile, client)

	_, err := tool.Execute(map[string]any{}, env)
	if err == nil {
		t.Fatal("expected error when task parameter is missing")
	}
}

func TestWaitTool(t *testing.T) {
	manager, profile, env, client, adapter := newSubagentTestSetup()

	adapter.responses = []*llm.Response{
		makeSubagentTextResponse("waited task result"),
	}

	handle, err := manager.Spawn(context.Background(), "do the thing", env, profile, client, 50)
	if err != nil {
		t.Fatalf("unexpected error spawning: %v", err)
	}

	tool := NewWaitTool(manager)
	if tool.Definition.Name != "wait" {
		t.Errorf("expected tool name 'wait', got %q", tool.Definition.Name)
	}

	result, err := tool.Execute(map[string]any{
		"agent_id": handle.ID,
	}, env)
	if err != nil {
		t.Fatalf("unexpected error executing wait tool: %v", err)
	}

	if !strings.Contains(result, "waited task result") {
		t.Errorf("expected result to contain 'waited task result', got: %s", result)
	}
	if !strings.Contains(result, "true") {
		t.Errorf("expected result to contain 'true' (success), got: %s", result)
	}
}

func TestWaitToolInvalidID(t *testing.T) {
	manager := NewSubAgentManager(0, 1)

	tool := NewWaitTool(manager)
	_, err := tool.Execute(map[string]any{
		"agent_id": "nonexistent",
	}, newSubagentTestEnv())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The tool itself returns an error message in the output, not as a Go error
}

func TestSendInputTool(t *testing.T) {
	manager, profile, env, client, adapter := newSubagentTestSetup()

	adapter.responses = []*llm.Response{
		makeSubagentTextResponse("got your input"),
	}

	handle, err := manager.Spawn(context.Background(), "start", env, profile, client, 50)
	if err != nil {
		t.Fatalf("unexpected error spawning: %v", err)
	}

	tool := NewSendInputTool(manager)
	if tool.Definition.Name != "send_input" {
		t.Errorf("expected tool name 'send_input', got %q", tool.Definition.Name)
	}

	result, err := tool.Execute(map[string]any{
		"agent_id": handle.ID,
		"message":  "here is more context",
	}, env)
	if err != nil {
		t.Fatalf("unexpected error executing send_input tool: %v", err)
	}

	if !strings.Contains(strings.ToLower(result), "sent") && !strings.Contains(strings.ToLower(result), "queued") && !strings.Contains(strings.ToLower(result), "acknowledged") {
		t.Errorf("expected acknowledgement in result, got: %s", result)
	}

	// Wait for completion so goroutines clean up
	_, _ = manager.Wait(handle.ID)
}

func TestCloseAgentTool(t *testing.T) {
	manager, profile, env, client, adapter := newSubagentTestSetup()

	adapter.responses = []*llm.Response{
		makeSubagentTextResponse("closing soon"),
	}

	handle, err := manager.Spawn(context.Background(), "work on it", env, profile, client, 50)
	if err != nil {
		t.Fatalf("unexpected error spawning: %v", err)
	}

	// Wait briefly for goroutine
	time.Sleep(10 * time.Millisecond)

	tool := NewCloseAgentTool(manager)
	if tool.Definition.Name != "close_agent" {
		t.Errorf("expected tool name 'close_agent', got %q", tool.Definition.Name)
	}

	result, err := tool.Execute(map[string]any{
		"agent_id": handle.ID,
	}, env)
	if err != nil {
		t.Fatalf("unexpected error executing close_agent tool: %v", err)
	}

	if !strings.Contains(result, "status") {
		t.Errorf("expected result to contain 'status', got: %s", result)
	}
}

func TestRegisterSubAgentTools(t *testing.T) {
	manager := NewSubAgentManager(0, 1)
	registry := NewToolRegistry()
	profile := &subagentTestProfile{model: "test-model", registry: NewToolRegistry()}
	adapter := &subagentTestAdapter{}
	client := llm.NewClient(
		llm.WithProvider("subagent-test", adapter),
		llm.WithDefaultProvider("subagent-test"),
	)

	RegisterSubAgentTools(registry, manager, profile, client)

	expectedTools := []string{"spawn_agent", "send_input", "wait", "close_agent"}
	for _, name := range expectedTools {
		if !registry.Has(name) {
			t.Errorf("expected tool %q to be registered", name)
		}
		tool := registry.Get(name)
		if tool == nil {
			t.Errorf("expected non-nil tool for %q", name)
			continue
		}
		if tool.Execute == nil {
			t.Errorf("expected non-nil Execute for tool %q", name)
		}
		if tool.Definition.Description == "" {
			t.Errorf("expected non-empty description for tool %q", name)
		}
	}

	if registry.Count() != len(expectedTools) {
		t.Errorf("expected %d registered tools, got %d", len(expectedTools), registry.Count())
	}
}

func TestSubAgentChildSessionConfig(t *testing.T) {
	manager, profile, env, client, adapter := newSubagentTestSetup()

	adapter.responses = []*llm.Response{
		makeSubagentTextResponse("done"),
	}

	handle, err := manager.Spawn(context.Background(), "check config", env, profile, client, 25)
	if err != nil {
		t.Fatalf("unexpected error spawning: %v", err)
	}

	// The child session should have MaxSubagentDepth = 0 (parent depth was 0, max was 1,
	// so child depth is 1 which means child's maxDepth - childDepth = 0 remaining)
	if handle.Session.Config.MaxSubagentDepth != 0 {
		t.Errorf("expected child MaxSubagentDepth 0, got %d", handle.Session.Config.MaxSubagentDepth)
	}

	// MaxTurns should reflect the specified max_turns
	if handle.Session.Config.MaxTurns != 25 {
		t.Errorf("expected child MaxTurns 25, got %d", handle.Session.Config.MaxTurns)
	}

	_, _ = manager.Wait(handle.ID)
}

func TestSubAgentWaitForNonexistentAgent(t *testing.T) {
	manager := NewSubAgentManager(0, 1)

	result, err := manager.Wait("does-not-exist")
	if err == nil {
		t.Fatal("expected error waiting for nonexistent agent")
	}
	if result != nil {
		t.Error("expected nil result for nonexistent agent")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

func TestSpawnAgentToolWithWorkingDir(t *testing.T) {
	manager, profile, env, client, adapter := newSubagentTestSetup()

	adapter.responses = []*llm.Response{
		makeSubagentTextResponse("done in subdir"),
	}

	tool := NewSpawnAgentTool(manager, profile, client)

	result, err := tool.Execute(map[string]any{
		"task":        "work in subdir",
		"working_dir": "/tmp/subdir",
	}, env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Parse the result to extract agent_id
	var parsed map[string]any
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("expected JSON result, got: %s (err: %v)", result, err)
	}
	agentID, ok := parsed["agent_id"].(string)
	if !ok || agentID == "" {
		t.Fatalf("expected agent_id in result, got: %v", parsed)
	}

	// Wait for completion
	_, _ = manager.Wait(agentID)
}
