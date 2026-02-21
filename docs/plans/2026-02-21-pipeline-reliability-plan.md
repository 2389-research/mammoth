# Pipeline Reliability Fixes Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix 6 systemic bugs that cause mammoth pipelines to report success despite incomplete or broken output: line-number embedding, turn exhaustion defaulting to success, and lack of trustless verification.

**Architecture:** Fix 1-2 are independent P0 bug fixes in `agent/` and `attractor/`. Fixes 3-6 share a `runVerifyCommand` helper extracted from `ToolHandler`'s exec pattern, used by codergen, conditional, fan-in, exit, and a new octagon verify handler.

**Tech Stack:** Go, `os/exec`, `regexp`, existing `fakeBackend` test double, existing `ToolHandler` exec pattern.

---

### Task 1: Test — stripLineNumbers function

**Files:**
- Create: `agent/tools_core_test.go` (if it doesn't exist, add to existing)

**Step 1: Write the failing test**

```go
func TestStripLineNumbers(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "mammoth format",
			input: "  1 | package main\n  2 | \n  3 | func main() {\n  4 | }\n",
			want:  "package main\n\nfunc main() {\n}\n",
		},
		{
			name:  "tab delimited",
			input: "1\tpackage main\n2\tfunc foo() {}\n",
			want:  "package main\nfunc foo() {}\n",
		},
		{
			name:  "no line numbers (passthrough)",
			input: "package main\n\nfunc main() {}\n",
			want:  "package main\n\nfunc main() {}\n",
		},
		{
			name:  "bitwise OR not stripped",
			input: "x := 123 | mask\n",
			want:  "x := 123 | mask\n",
		},
		{
			name:  "mixed lines some numbered some not",
			input: "  1 | line one\nno number here\n  3 | line three\n",
			want:  "line one\nno number here\nline three\n",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "high line numbers",
			input: "100 | func foo() {\n101 |     return 42\n102 | }\n",
			want:  "func foo() {\n    return 42\n}\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripLineNumbers(tt.input)
			if got != tt.want {
				t.Errorf("stripLineNumbers(%q)\n  got:  %q\n  want: %q", tt.input, got, tt.want)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./agent/ -run TestStripLineNumbers -v`
Expected: FAIL — `stripLineNumbers` undefined

---

### Task 2: Implement stripLineNumbers + wire into write_file

**Files:**
- Modify: `agent/tools_core.go`

**Step 1: Implement stripLineNumbers**

Add to `agent/tools_core.go` after `formatLineNumbers`:

```go
// lineNumberPattern matches leading line-number prefixes in two formats:
//   - "  1 | code"  (spaces + digits + space + pipe + space)
//   - "1\tcode"     (digits + tab)
// The pattern requires the number at the start of the line with only optional
// leading whitespace, preventing false positives on code like "x = 123 | mask".
var lineNumberPattern = regexp.MustCompile(`^\s*\d+\s*[|]\s?`)
var lineNumberTabPattern = regexp.MustCompile(`^\d+\t`)

// stripLineNumbers removes leading line-number prefixes that may have been
// embedded by formatLineNumbers when an agent reads a file and writes it back.
func stripLineNumbers(content string) string {
	if content == "" {
		return content
	}
	lines := strings.Split(content, "\n")
	// Only strip if a majority of non-empty lines match the pattern,
	// to avoid stripping content that happens to start with a number.
	matchCount := 0
	nonEmptyCount := 0
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		nonEmptyCount++
		if lineNumberPattern.MatchString(line) || lineNumberTabPattern.MatchString(line) {
			matchCount++
		}
	}
	// If fewer than half of non-empty lines match, don't strip
	if nonEmptyCount == 0 || matchCount*2 < nonEmptyCount {
		return content
	}

	var builder strings.Builder
	for i, line := range lines {
		stripped := lineNumberPattern.ReplaceAllString(line, "")
		if stripped == line {
			stripped = lineNumberTabPattern.ReplaceAllString(line, "")
		}
		builder.WriteString(stripped)
		if i < len(lines)-1 {
			builder.WriteByte('\n')
		}
	}
	return builder.String()
}
```

Add `"regexp"` to the import block.

**Step 2: Wire into write_file handler**

In `NewWriteFileTool`, after extracting content and before calling `env.WriteFile`, add:

```go
// Strip line-number prefixes that may have been embedded by read_file
content = stripLineNumbers(content)
```

**Step 3: Run tests**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./agent/ -run TestStripLineNumbers -v`
Expected: ALL PASS

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./agent/ -count=1`
Expected: ALL PASS

**Step 4: Commit**

```
fix(agent): strip line-number prefixes in write_file to prevent file corruption
```

---

### Task 3: Test — write_file integration with line-number stripping

**Files:**
- Modify: `agent/tools_core_test.go`

**Step 1: Write the integration test**

```go
func TestWriteFileStripsLineNumbers(t *testing.T) {
	env := &testEnv{files: make(map[string]string)}
	tool := NewWriteFileTool()

	// Simulate content from read_file (with line numbers)
	numberedContent := "  1 | package main\n  2 | \n  3 | func main() {\n  4 | \tfmt.Println(\"hello\")\n  5 | }\n"

	result, err := tool.Execute(map[string]any{
		"file_path": "/tmp/test.go",
		"content":   numberedContent,
	}, env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result, "Successfully wrote") {
		t.Errorf("unexpected result: %s", result)
	}

	// The written content should NOT have line numbers
	written := env.files["/tmp/test.go"]
	if strings.Contains(written, "  1 |") {
		t.Errorf("expected line numbers to be stripped, got:\n%s", written)
	}
	if !strings.Contains(written, "package main") {
		t.Errorf("expected content to be preserved, got:\n%s", written)
	}
}
```

NOTE: Check if a `testEnv` already exists in agent test files. If not, you need a minimal one that implements `ExecutionEnvironment` with at least `WriteFile`. Search for existing test helpers in `agent/*_test.go`.

**Step 2: Run test**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./agent/ -run TestWriteFileStripsLineNumbers -v`
Expected: PASS

**Step 3: Commit**

```
test(agent): add integration test for write_file line-number stripping
```

---

### Task 4: Test — Session.HitTurnLimit field

**Files:**
- Modify: `agent/session_test.go` (or create if needed)

**Step 1: Write the failing test**

```go
func TestSessionHitTurnLimitDefaultsFalse(t *testing.T) {
	session := NewSession(DefaultSessionConfig())
	if session.HitTurnLimit {
		t.Error("expected HitTurnLimit to default to false")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./agent/ -run TestSessionHitTurnLimitDefaultsFalse -v`
Expected: FAIL — `session.HitTurnLimit` undefined

---

### Task 5: Implement Session.HitTurnLimit + loop.go wiring

**Files:**
- Modify: `agent/session.go` — add `HitTurnLimit bool` field to `Session`
- Modify: `agent/loop.go` — set `session.HitTurnLimit = true` at line ~38

**Step 1: Add field to Session**

In `agent/session.go`, add to the `Session` struct:

```go
type Session struct {
	ID            string
	Config        SessionConfig
	History       []Turn
	State         SessionState
	EventEmitter  *EventEmitter
	HitTurnLimit  bool          // set when the agent loop breaks due to turn limit
	steeringQueue []string
	followupQueue []string
	mu            sync.Mutex
}
```

**Step 2: Set in loop.go**

In `agent/loop.go` lines 37-40, add `session.HitTurnLimit = true`:

```go
// 2. Check turn limit
if session.Config.MaxTurns > 0 && session.TurnCount() >= session.Config.MaxTurns {
	session.HitTurnLimit = true
	session.Emit(EventTurnLimit, map[string]any{"total_turns": session.TurnCount()})
	break
}
```

Also set it for the round limit at lines 31-34:

```go
// 1. Check round limit
if roundCount >= session.Config.MaxToolRoundsPerInput {
	session.HitTurnLimit = true
	session.Emit(EventTurnLimit, map[string]any{"round": roundCount})
	break
}
```

**Step 3: Run tests**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./agent/ -run TestSessionHitTurnLimit -v`
Expected: PASS

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./agent/ -count=1`
Expected: ALL PASS

**Step 4: Commit**

```
feat(agent): track turn limit exhaustion on Session.HitTurnLimit
```

---

### Task 6: Test — extractResult treats turn exhaustion as failure

**Files:**
- Modify: `attractor/backend_agent_test.go` or `attractor/backend_test.go`

**Step 1: Write the failing test**

```go
func TestExtractResultTurnExhaustedWithoutMarkerFails(t *testing.T) {
	session := agent.NewSession(agent.SessionConfig{MaxTurns: 5})
	session.HitTurnLimit = true

	// Agent produced output but no OUTCOME marker
	session.AppendTurn(agent.AssistantTurn{
		Content:   "I created the first 3 files but ran out of time...",
		Timestamp: time.Now(),
	})

	result := extractResult(session)
	if result.Success {
		t.Error("expected Success=false when turn limit hit without success marker")
	}
}

func TestExtractResultTurnExhaustedWithSuccessMarkerSucceeds(t *testing.T) {
	session := agent.NewSession(agent.SessionConfig{MaxTurns: 5})
	session.HitTurnLimit = true

	// Agent explicitly declared success before hitting limit
	session.AppendTurn(agent.AssistantTurn{
		Content:   "All work complete. OUTCOME:SUCCESS",
		Timestamp: time.Now(),
	})

	result := extractResult(session)
	if !result.Success {
		t.Error("expected Success=true when explicit OUTCOME:SUCCESS marker present despite turn limit")
	}
}

func TestExtractResultNoTurnLimitDefaultsToSuccess(t *testing.T) {
	session := agent.NewSession(agent.DefaultSessionConfig())
	// HitTurnLimit is false (default)

	session.AppendTurn(agent.AssistantTurn{
		Content:   "I finished the work.",
		Timestamp: time.Now(),
	})

	result := extractResult(session)
	if !result.Success {
		t.Error("expected Success=true when no turn limit was hit")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./attractor/ -run TestExtractResultTurnExhausted -v`
Expected: FAIL — `extractResult` signature doesn't take session (or doesn't check HitTurnLimit)

---

### Task 7: Implement turn exhaustion = failure in extractResult

**Files:**
- Modify: `attractor/backend_agent.go`

**Step 1: Update extractResult to accept session and check HitTurnLimit**

Change the `extractResult` function signature to accept `*agent.Session` and check the `HitTurnLimit` field. After the existing OUTCOME marker detection, add:

```go
func extractResult(session *agent.Session) *AgentRunResult {
	result := &AgentRunResult{
		Success: true,
	}

	// ... existing history walking code ...

	// Check for explicit OUTCOME markers
	hasExplicitSuccessMarker := false
	if result.Output != "" {
		if marker, found := DetectOutcomeMarker(result.Output); found {
			result.Success = marker != "fail"
			if marker != "fail" {
				hasExplicitSuccessMarker = true
			}
		}
	}

	// Turn exhaustion without explicit success marker = failure.
	// Agents that complete early can declare OUTCOME:SUCCESS to override.
	if session.HitTurnLimit && !hasExplicitSuccessMarker {
		result.Success = false
	}

	return result
}
```

NOTE: The function already takes `*agent.Session` — just verify. If it only takes session history, refactor to pass the session.

**Step 2: Run tests**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./attractor/ -run TestExtractResult -v`
Expected: ALL PASS

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./attractor/ -count=1`
Expected: ALL PASS (no regressions)

**Step 3: Commit**

```
fix(attractor): treat turn exhaustion without success marker as failure
```

---

### Task 8: Test + implement shared runVerifyCommand helper

**Files:**
- Create: `attractor/verify_command.go`
- Create: `attractor/verify_command_test.go`

**Step 1: Write the failing tests**

```go
// ABOUTME: Tests for the shared runVerifyCommand helper used by codergen, conditional, fan-in, and exit handlers.
// ABOUTME: Covers exit code detection, timeout, working directory, and output capture.
package attractor

import (
	"context"
	"testing"
	"time"
)

func TestRunVerifyCommandSuccess(t *testing.T) {
	result := runVerifyCommand(context.Background(), "echo hello", "", 10*time.Second)
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
	if result.Stdout == "" {
		t.Error("expected stdout to contain output")
	}
	if !result.Success {
		t.Error("expected Success=true for exit code 0")
	}
}

func TestRunVerifyCommandFailure(t *testing.T) {
	result := runVerifyCommand(context.Background(), "exit 1", "", 10*time.Second)
	if result.ExitCode != 1 {
		t.Errorf("expected exit code 1, got %d", result.ExitCode)
	}
	if result.Success {
		t.Error("expected Success=false for exit code 1")
	}
}

func TestRunVerifyCommandCapturesStderr(t *testing.T) {
	result := runVerifyCommand(context.Background(), "echo err >&2", "", 10*time.Second)
	if result.Stderr == "" {
		t.Error("expected stderr to contain output")
	}
}

func TestRunVerifyCommandTimeout(t *testing.T) {
	result := runVerifyCommand(context.Background(), "sleep 60", "", 100*time.Millisecond)
	if result.Success {
		t.Error("expected failure on timeout")
	}
	if !result.TimedOut {
		t.Error("expected TimedOut=true")
	}
}

func TestRunVerifyCommandWorkDir(t *testing.T) {
	dir := t.TempDir()
	result := runVerifyCommand(context.Background(), "pwd", dir, 10*time.Second)
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
}

func TestRunVerifyCommandContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := runVerifyCommand(ctx, "echo hello", "", 10*time.Second)
	if result.Success {
		t.Error("expected failure on cancelled context")
	}
}
```

**Step 2: Write the implementation**

Create `attractor/verify_command.go`:

```go
// ABOUTME: Shared verify_command execution helper for pipeline node handlers.
// ABOUTME: Runs a shell command and returns exit code, stdout, stderr for post-execution verification.
package attractor

import (
	"bytes"
	"context"
	"os/exec"
	"syscall"
	"time"
)

// VerifyResult holds the outcome of a verify_command execution.
type VerifyResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
	Success  bool // true when ExitCode == 0
	TimedOut bool
}

// defaultVerifyTimeout is used when no timeout is specified.
const defaultVerifyTimeout = 60 * time.Second

// runVerifyCommand executes a shell command and returns the result.
// It uses the same process-group management as ToolHandler for clean cleanup.
func runVerifyCommand(ctx context.Context, command, workDir string, timeout time.Duration) VerifyResult {
	if timeout <= 0 {
		timeout = defaultVerifyTimeout
	}

	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "sh", "-c", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			pgid, err := syscall.Getpgid(cmd.Process.Pid)
			if err == nil {
				_ = syscall.Kill(-pgid, syscall.SIGKILL)
			}
			return cmd.Process.Kill()
		}
		return nil
	}
	cmd.WaitDelay = 3 * time.Second

	if workDir != "" {
		cmd.Dir = workDir
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	runErr := cmd.Run()

	result := VerifyResult{
		Stdout: stdoutBuf.String(),
		Stderr: stderrBuf.String(),
	}

	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = 1
		}
		if cmdCtx.Err() == context.DeadlineExceeded {
			result.TimedOut = true
		}
	}

	result.Success = result.ExitCode == 0
	return result
}
```

**Step 3: Run tests**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./attractor/ -run TestRunVerifyCommand -v`
Expected: ALL PASS

**Step 4: Commit**

```
feat(attractor): add shared runVerifyCommand helper for post-execution verification
```

---

### Task 9: Test + implement verify_command on CodergenHandler

**Files:**
- Modify: `attractor/handlers_codergen.go`
- Modify: `attractor/handlers_codergen_test.go`

**Step 1: Write the failing test**

```go
func TestCodergenHandlerVerifyCommandOverridesOnFailure(t *testing.T) {
	backend := &fakeBackend{} // default: Success=true
	h := &CodergenHandler{Backend: backend}

	node := &Node{
		ID: "impl_with_verify",
		Attrs: map[string]string{
			"shape":          "box",
			"prompt":         "Write code",
			"verify_command": "exit 1", // verification fails
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if outcome.Status != StatusFail {
		t.Errorf("expected StatusFail when verify_command fails, got %v", outcome.Status)
	}
}

func TestCodergenHandlerVerifyCommandPassesThrough(t *testing.T) {
	backend := &fakeBackend{}
	h := &CodergenHandler{Backend: backend}

	node := &Node{
		ID: "impl_verify_pass",
		Attrs: map[string]string{
			"shape":          "box",
			"prompt":         "Write code",
			"verify_command": "exit 0", // verification passes
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if outcome.Status != StatusSuccess {
		t.Errorf("expected StatusSuccess when verify_command passes, got %v", outcome.Status)
	}
}

func TestCodergenHandlerNoVerifyCommandIgnored(t *testing.T) {
	backend := &fakeBackend{}
	h := &CodergenHandler{Backend: backend}

	node := &Node{
		ID: "impl_no_verify",
		Attrs: map[string]string{
			"shape":  "box",
			"prompt": "Write code",
			// no verify_command
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if outcome.Status != StatusSuccess {
		t.Errorf("expected StatusSuccess without verify_command, got %v", outcome.Status)
	}
}
```

**Step 2: Implement**

In `CodergenHandler.Execute()`, after the agent returns and the outcome is built (just before the final `return` statements around line 190), add:

```go
// Post-execution verification: if verify_command is set, run it and
// override the outcome on failure regardless of what the agent claimed.
if verifyCmd := attrs["verify_command"]; verifyCmd != "" {
	workDir := config.WorkDir
	vResult := runVerifyCommand(ctx, verifyCmd, workDir, defaultVerifyTimeout)

	// Store verify output as artifact
	if store != nil {
		artifactID := node.ID + ".verify_output"
		verifyOutput := fmt.Sprintf("exit_code=%d\nstdout:\n%s\nstderr:\n%s", vResult.ExitCode, vResult.Stdout, vResult.Stderr)
		_, _ = store.Store(artifactID, "verify_output", []byte(verifyOutput))
	}

	if !vResult.Success {
		return &Outcome{
			Status:         StatusFail,
			FailureReason:  fmt.Sprintf("verify_command failed (exit %d): %s", vResult.ExitCode, vResult.Stderr),
			ContextUpdates: updates,
		}, nil
	}
}
```

This goes AFTER the success path builds the outcome but BEFORE returning it. The verify command only runs when the agent itself succeeded — if the agent already failed, skip verification.

**Step 3: Run tests**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./attractor/ -run TestCodergenHandlerVerifyCommand -v`
Expected: ALL PASS

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./attractor/ -count=1`
Expected: ALL PASS

**Step 4: Commit**

```
feat(attractor): add verify_command support to CodergenHandler
```

---

### Task 10: Add verify_command to ConditionalHandler

**Files:**
- Modify: `attractor/handlers_conditional.go`
- Modify: `attractor/handlers_conditional_test.go`

**Step 1: Write the failing test**

```go
func TestConditionalHandlerVerifyCommandOverridesOnFailure(t *testing.T) {
	backend := &fakeBackend{
		runAgentFn: func(ctx context.Context, config AgentRunConfig) (*AgentRunResult, error) {
			return &AgentRunResult{
				Output:  "All tests pass!\nOUTCOME:PASS",
				Success: true,
			}, nil
		},
	}
	h := &ConditionalHandler{Backend: backend}

	node := &Node{
		ID: "verify_with_cmd",
		Attrs: map[string]string{
			"shape":          "diamond",
			"prompt":         "Run tests",
			"verify_command": "exit 1", // independent verification fails
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if outcome.Status != StatusFail {
		t.Errorf("expected StatusFail when verify_command fails, got %v", outcome.Status)
	}
}
```

**Step 2: Implement**

In `ConditionalHandler.executeWithAgent()`, after `resolveOutcome` and before the final return, add the same `verify_command` pattern used in Task 9.

**Step 3: Run tests**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./attractor/ -run TestConditionalHandlerVerifyCommand -v`
Expected: ALL PASS

**Step 4: Commit**

```
feat(attractor): add verify_command support to ConditionalHandler
```

---

### Task 11: Test + implement VerifyHandler (shape=octagon)

**Files:**
- Modify: `attractor/handlers.go` — add octagon to shapeToType, register VerifyHandler
- Create: `attractor/handlers_verify.go`
- Create: `attractor/handlers_verify_test.go`

**Step 1: Write the failing tests**

```go
// ABOUTME: Tests for VerifyHandler that executes deterministic shell commands without an LLM.
// ABOUTME: Covers exit code routing, timeout, working directory, and outcome context updates.
package attractor

import (
	"context"
	"testing"
)

func TestVerifyHandlerSuccess(t *testing.T) {
	h := &VerifyHandler{}
	node := &Node{
		ID: "verify_tests",
		Attrs: map[string]string{
			"shape":   "octagon",
			"command": "echo all tests pass",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusSuccess {
		t.Errorf("expected StatusSuccess, got %v", outcome.Status)
	}
	if outcome.ContextUpdates["outcome"] != "success" {
		t.Errorf("expected outcome=success in context, got %v", outcome.ContextUpdates["outcome"])
	}
}

func TestVerifyHandlerFailure(t *testing.T) {
	h := &VerifyHandler{}
	node := &Node{
		ID: "verify_tests_fail",
		Attrs: map[string]string{
			"shape":   "octagon",
			"command": "exit 1",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusFail {
		t.Errorf("expected StatusFail, got %v", outcome.Status)
	}
	if outcome.ContextUpdates["outcome"] != "fail" {
		t.Errorf("expected outcome=fail in context, got %v", outcome.ContextUpdates["outcome"])
	}
}

func TestVerifyHandlerNoCommand(t *testing.T) {
	h := &VerifyHandler{}
	node := &Node{
		ID:    "verify_no_cmd",
		Attrs: map[string]string{"shape": "octagon"},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusFail {
		t.Errorf("expected StatusFail when no command, got %v", outcome.Status)
	}
}

func TestVerifyHandlerType(t *testing.T) {
	h := &VerifyHandler{}
	if h.Type() != "verify" {
		t.Errorf("expected type 'verify', got %q", h.Type())
	}
}

func TestVerifyHandlerShapeMapping(t *testing.T) {
	handlerType := ShapeToHandlerType("octagon")
	if handlerType != "verify" {
		t.Errorf("expected octagon to map to 'verify', got %q", handlerType)
	}
}

func TestVerifyHandlerInDefaultRegistry(t *testing.T) {
	reg := DefaultHandlerRegistry()
	h := reg.Get("verify")
	if h == nil {
		t.Fatal("expected verify handler in default registry")
	}
}
```

**Step 2: Implement**

Create `attractor/handlers_verify.go`:

```go
// ABOUTME: Deterministic verify handler that executes shell commands without an LLM.
// ABOUTME: Maps to shape=octagon. Uses exit code for pass/fail, zero token cost.
package attractor

import (
	"context"
	"fmt"
	"time"
)

// VerifyHandler handles deterministic verification nodes (shape=octagon).
// It runs a shell command and uses the exit code for pass/fail routing.
// No LLM is involved — this is pure command execution.
type VerifyHandler struct{}

// Type returns the handler type string "verify".
func (h *VerifyHandler) Type() string {
	return "verify"
}

// Execute runs the command specified in the node's "command" attribute.
// Exit code 0 → StatusSuccess, non-zero → StatusFail.
// Sets "outcome" in ContextUpdates for conditional edge routing.
func (h *VerifyHandler) Execute(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	attrs := node.Attrs
	if attrs == nil {
		attrs = make(map[string]string)
	}

	command := attrs["command"]
	if command == "" {
		return &Outcome{
			Status:        StatusFail,
			FailureReason: "no command attribute specified for verify node: " + node.ID,
			ContextUpdates: map[string]any{
				"outcome":    "fail",
				"last_stage": node.ID,
			},
		}, nil
	}

	// Parse timeout
	timeout := defaultVerifyTimeout
	if timeoutStr := attrs["timeout"]; timeoutStr != "" {
		if parsed, err := time.ParseDuration(timeoutStr); err == nil {
			timeout = parsed
		}
	}

	workDir := attrs["working_dir"]
	if workDir == "" && store != nil && store.BaseDir() != "" {
		workDir = store.BaseDir()
	}

	result := runVerifyCommand(ctx, command, workDir, timeout)

	// Store output as artifact
	if store != nil {
		artifactID := node.ID + ".output"
		output := fmt.Sprintf("exit_code=%d\nstdout:\n%s\nstderr:\n%s", result.ExitCode, result.Stdout, result.Stderr)
		_, _ = store.Store(artifactID, "verify_output", []byte(output))
	}

	status := StatusSuccess
	outcomeStr := "success"
	if !result.Success {
		status = StatusFail
		outcomeStr = "fail"
	}

	failureReason := ""
	if !result.Success {
		failureReason = fmt.Sprintf("verify command failed (exit %d): %s", result.ExitCode, result.Stderr)
		if result.TimedOut {
			failureReason = fmt.Sprintf("verify command timed out after %s", timeout)
		}
	}

	return &Outcome{
		Status:        status,
		Notes:         result.Stdout,
		FailureReason: failureReason,
		ContextUpdates: map[string]any{
			"outcome":    outcomeStr,
			"last_stage": node.ID,
		},
	}, nil
}
```

In `attractor/handlers.go`, add to `shapeToType`:

```go
"octagon": "verify",
```

And register in `DefaultHandlerRegistry()`:

```go
reg.Register(&VerifyHandler{})
```

**Step 3: Run tests**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./attractor/ -run TestVerifyHandler -v`
Expected: ALL PASS

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./attractor/ -count=1`
Expected: ALL PASS

**Step 4: Commit**

```
feat(attractor): add deterministic VerifyHandler (shape=octagon) for trustless verification
```

---

### Task 12: Add verify_command to FanInHandler

**Files:**
- Modify: `attractor/handlers_fanin.go`
- Modify or create: `attractor/handlers_fanin_test.go`

**Step 1: Write the failing test**

```go
func TestFanInHandlerVerifyCommandFailure(t *testing.T) {
	h := &FanInHandler{}
	node := &Node{
		ID: "fan_in_verify",
		Attrs: map[string]string{
			"shape":          "tripleoctagon",
			"verify_command": "exit 1",
		},
	}
	pctx := NewContext()
	pctx.Set("parallel.results", []any{"branch1", "branch2"})
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusFail {
		t.Errorf("expected StatusFail when verify_command fails, got %v", outcome.Status)
	}
}

func TestFanInHandlerVerifyCommandSuccess(t *testing.T) {
	h := &FanInHandler{}
	node := &Node{
		ID: "fan_in_verify_pass",
		Attrs: map[string]string{
			"shape":          "tripleoctagon",
			"verify_command": "exit 0",
		},
	}
	pctx := NewContext()
	pctx.Set("parallel.results", []any{"branch1"})
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusSuccess {
		t.Errorf("expected StatusSuccess, got %v", outcome.Status)
	}
}

func TestFanInHandlerNoVerifyCommand(t *testing.T) {
	h := &FanInHandler{}
	node := &Node{
		ID: "fan_in_no_verify",
		Attrs: map[string]string{"shape": "tripleoctagon"},
	}
	pctx := NewContext()
	pctx.Set("parallel.results", []any{"branch1"})
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusSuccess {
		t.Errorf("expected StatusSuccess without verify_command, got %v", outcome.Status)
	}
}
```

**Step 2: Implement**

In `FanInHandler.Execute()`, after the success return (line ~36), add the verify_command check:

```go
func (h *FanInHandler) Execute(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	results := pctx.Get("parallel.results")
	if results == nil {
		return &Outcome{
			Status:        StatusFail,
			FailureReason: "No parallel results to evaluate for fan-in node: " + node.ID,
		}, nil
	}

	// Post-merge verification
	attrs := node.Attrs
	if attrs == nil {
		attrs = make(map[string]string)
	}
	if verifyCmd := attrs["verify_command"]; verifyCmd != "" {
		workDir := ""
		if store != nil && store.BaseDir() != "" {
			workDir = store.BaseDir()
		}
		vResult := runVerifyCommand(ctx, verifyCmd, workDir, defaultVerifyTimeout)

		if store != nil {
			artifactID := node.ID + ".verify_output"
			output := fmt.Sprintf("exit_code=%d\nstdout:\n%s\nstderr:\n%s", vResult.ExitCode, vResult.Stdout, vResult.Stderr)
			_, _ = store.Store(artifactID, "verify_output", []byte(output))
		}

		if !vResult.Success {
			return &Outcome{
				Status:        StatusFail,
				FailureReason: fmt.Sprintf("fan-in verify_command failed (exit %d): %s", vResult.ExitCode, vResult.Stderr),
				ContextUpdates: map[string]any{
					"last_stage": node.ID,
				},
			}, nil
		}
	}

	return &Outcome{
		Status: StatusSuccess,
		Notes:  "Fan-in merged parallel results at node: " + node.ID,
		ContextUpdates: map[string]any{
			"last_stage":                node.ID,
			"parallel.fan_in.completed": true,
		},
	}, nil
}
```

Add `"fmt"` to imports in `handlers_fanin.go`.

**Step 3: Run tests**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./attractor/ -run TestFanInHandler -v`
Expected: ALL PASS

**Step 4: Commit**

```
feat(attractor): add verify_command support to FanInHandler for post-merge validation
```

---

### Task 13: Add verify_command to ExitHandler

**Files:**
- Modify: `attractor/handlers_exit.go`
- Modify or create: `attractor/handlers_exit_test.go`

**Step 1: Write the failing test**

```go
func TestExitHandlerVerifyCommandFailure(t *testing.T) {
	h := &ExitHandler{}
	node := &Node{
		ID: "exit_verify",
		Attrs: map[string]string{
			"shape":          "Msquare",
			"verify_command": "exit 1",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusFail {
		t.Errorf("expected StatusFail when verify_command fails, got %v", outcome.Status)
	}
}

func TestExitHandlerVerifyCommandSuccess(t *testing.T) {
	h := &ExitHandler{}
	node := &Node{
		ID: "exit_verify_pass",
		Attrs: map[string]string{
			"shape":          "Msquare",
			"verify_command": "exit 0",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusSuccess {
		t.Errorf("expected StatusSuccess, got %v", outcome.Status)
	}
}

func TestExitHandlerNoVerifyCommand(t *testing.T) {
	h := &ExitHandler{}
	node := &Node{
		ID: "exit_no_verify",
		Attrs: map[string]string{"shape": "Msquare"},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusSuccess {
		t.Errorf("expected StatusSuccess without verify_command, got %v", outcome.Status)
	}
}
```

**Step 2: Implement**

Update `ExitHandler.Execute()` to check for `verify_command` before returning success:

```go
func (h *ExitHandler) Execute(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	attrs := node.Attrs
	if attrs == nil {
		attrs = make(map[string]string)
	}

	// Pre-exit verification
	if verifyCmd := attrs["verify_command"]; verifyCmd != "" {
		workDir := ""
		if store != nil && store.BaseDir() != "" {
			workDir = store.BaseDir()
		}
		vResult := runVerifyCommand(ctx, verifyCmd, workDir, defaultVerifyTimeout)

		if store != nil {
			artifactID := node.ID + ".verify_output"
			output := fmt.Sprintf("exit_code=%d\nstdout:\n%s\nstderr:\n%s", vResult.ExitCode, vResult.Stdout, vResult.Stderr)
			_, _ = store.Store(artifactID, "verify_output", []byte(output))
		}

		if !vResult.Success {
			return &Outcome{
				Status:        StatusFail,
				FailureReason: fmt.Sprintf("exit verify_command failed (exit %d): %s", vResult.ExitCode, vResult.Stderr),
				ContextUpdates: map[string]any{
					"_finished_at": time.Now().Format(time.RFC3339Nano),
				},
			}, nil
		}
	}

	return &Outcome{
		Status: StatusSuccess,
		Notes:  "Pipeline exited at node: " + node.ID,
		ContextUpdates: map[string]any{
			"_finished_at": time.Now().Format(time.RFC3339Nano),
		},
	}, nil
}
```

Add `"fmt"` to imports in `handlers_exit.go`.

**Step 3: Run tests**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./attractor/ -run TestExitHandler -v`
Expected: ALL PASS

**Step 4: Commit**

```
feat(attractor): add verify_command support to ExitHandler for pipeline goal verification
```

---

### Task 14: Full test suite + commit design docs

**Step 1: Run all tests**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./... -count=1`
Expected: ALL PASS

**Step 2: Commit design docs**

```
docs: add pipeline reliability design and implementation plan
```

---

### Task 15: Reply to BBS threads

Post replies to the three BBS threads summarizing what was fixed and linking the commits. Update the mammoth topic with the status of each proposed fix.
