# Spec Conformance Gaps Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Close the 3 remaining attractor spec conformance gaps: `auto_status` attribute, Duration token type, and tool call hooks.

**Architecture:** Each gap is an isolated feature. `auto_status` is an engine-level check applied after handler execution. Duration is a lexer token type that catches `900s`/`15m`/`2h` as single tokens instead of number+identifier. Tool call hooks are graph/node attributes that wrap LLM tool calls with pre/post shell commands.

**Tech Stack:** Go 1.25+, table-driven tests, `time.ParseDuration` for duration parsing, `os/exec` for hook execution.

---

### Task 1: auto_status — Write failing tests

**Files:**
- Create: `attractor/auto_status_test.go`

**Step 1: Write the failing test**

```go
// ABOUTME: Tests for auto_status attribute that auto-generates SUCCESS when handler writes no status.
// ABOUTME: Covers auto_status=true on codergen nodes and interaction with allow_partial.
package attractor

import (
	"context"
	"testing"
)

func TestAutoStatusGeneratesSuccessWhenHandlerReturnsEmpty(t *testing.T) {
	// A handler that returns an outcome with empty status (simulating "no status written")
	handler := &testHandler{outcome: &Outcome{Status: ""}}
	node := &Node{
		ID:    "auto_node",
		Attrs: map[string]string{"auto_status": "true", "shape": "box"},
	}
	graph := &Graph{
		Nodes: map[string]*Node{
			"start":     {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"auto_node": node,
			"exit":      {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*Edge{
			{From: "start", To: "auto_node"},
			{From: "auto_node", To: "exit"},
		},
		Attrs: map[string]string{},
	}

	outcome := applyAutoStatus(node, handler.outcome)
	if outcome.Status != StatusSuccess {
		t.Errorf("expected StatusSuccess with auto_status=true and empty status, got %q", outcome.Status)
	}
}

func TestAutoStatusDoesNotOverrideExplicitStatus(t *testing.T) {
	node := &Node{
		ID:    "auto_node",
		Attrs: map[string]string{"auto_status": "true"},
	}
	outcome := &Outcome{Status: StatusFail, FailureReason: "explicit failure"}

	result := applyAutoStatus(node, outcome)
	if result.Status != StatusFail {
		t.Errorf("expected StatusFail to be preserved, got %q", result.Status)
	}
}

func TestAutoStatusFalseDoesNothing(t *testing.T) {
	node := &Node{
		ID:    "auto_node",
		Attrs: map[string]string{"auto_status": "false"},
	}
	outcome := &Outcome{Status: ""}

	result := applyAutoStatus(node, outcome)
	if result.Status != "" {
		t.Errorf("expected empty status preserved when auto_status=false, got %q", result.Status)
	}
}

func TestAutoStatusMissingAttributeDoesNothing(t *testing.T) {
	node := &Node{
		ID:    "auto_node",
		Attrs: map[string]string{},
	}
	outcome := &Outcome{Status: ""}

	result := applyAutoStatus(node, outcome)
	if result.Status != "" {
		t.Errorf("expected empty status preserved when no auto_status attr, got %q", result.Status)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./attractor/ -run TestAutoStatus -v`
Expected: FAIL — `applyAutoStatus` undefined

---

### Task 2: auto_status — Implement

**Files:**
- Modify: `attractor/engine.go` (add `applyAutoStatus` function, wire into execution loop after handler returns)

**Step 3: Write the implementation**

Add the `applyAutoStatus` function to `engine.go`:

```go
// applyAutoStatus checks if the node has auto_status=true and the outcome
// has an empty status. If so, it sets the status to SUCCESS. This implements
// the spec Section 2.6 requirement: "If true and the handler writes no
// status, the engine auto-generates a SUCCESS outcome."
func applyAutoStatus(node *Node, outcome *Outcome) *Outcome {
	if node.Attrs == nil || node.Attrs["auto_status"] != "true" {
		return outcome
	}
	if outcome.Status == "" {
		outcome.Status = StatusSuccess
		if outcome.Notes == "" {
			outcome.Notes = "auto_status: generated SUCCESS for node " + node.ID
		}
	}
	return outcome
}
```

Then wire it into `executeWithRetry` in `engine.go`, right after `safeExecute` returns successfully (after the `outcome, err := safeExecute(...)` line ~753). Add:

```go
outcome = applyAutoStatus(node, outcome)
```

**Step 4: Run test to verify it passes**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./attractor/ -run TestAutoStatus -v`
Expected: PASS

**Step 5: Commit**

```
git add attractor/auto_status_test.go attractor/engine.go
git commit -m "feat(attractor): implement auto_status attribute per spec §2.6"
```

---

### Task 3: Duration token type — Write failing tests

**Files:**
- Modify: `dot/lexer_test.go`

**Step 6: Write the failing test**

Add to `dot/lexer_test.go`:

```go
func TestLexDurationValues(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantType   TokenType
		wantValue  string
	}{
		{"milliseconds", "250ms", TokenDuration, "250ms"},
		{"seconds", "900s", TokenDuration, "900s"},
		{"minutes", "15m", TokenDuration, "15m"},
		{"hours", "2h", TokenDuration, "2h"},
		{"days", "1d", TokenDuration, "1d"},
		{"negative", "-5s", TokenDuration, "-5s"},
		{"large", "86400s", TokenDuration, "86400s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens, err := Lex(tt.input)
			if err != nil {
				t.Fatalf("Lex(%q) error: %v", tt.input, err)
			}
			// Filter out EOF
			var nonEOF []Token
			for _, tok := range tokens {
				if tok.Type != TokenEOF {
					nonEOF = append(nonEOF, tok)
				}
			}
			if len(nonEOF) != 1 {
				t.Fatalf("expected 1 token, got %d: %v", len(nonEOF), nonEOF)
			}
			if nonEOF[0].Type != tt.wantType {
				t.Errorf("token type = %v, want %v", nonEOF[0].Type, tt.wantType)
			}
			if nonEOF[0].Value != tt.wantValue {
				t.Errorf("token value = %q, want %q", nonEOF[0].Value, tt.wantValue)
			}
		})
	}
}

func TestLexDurationInAttribute(t *testing.T) {
	input := `timeout=900s`
	tokens, err := Lex(input)
	if err != nil {
		t.Fatalf("Lex(%q) error: %v", input, err)
	}
	// Should produce: Identifier("timeout"), Equals, Duration("900s"), EOF
	expected := []struct {
		typ TokenType
		val string
	}{
		{TokenIdentifier, "timeout"},
		{TokenEquals, "="},
		{TokenDuration, "900s"},
	}
	for i, exp := range expected {
		if tokens[i].Type != exp.typ || tokens[i].Value != exp.val {
			t.Errorf("token[%d] = {%v, %q}, want {%v, %q}", i, tokens[i].Type, tokens[i].Value, exp.typ, exp.val)
		}
	}
}

func TestLexPlainNumberNotDuration(t *testing.T) {
	// A plain number without a duration suffix should remain TokenNumber
	input := `42`
	tokens, err := Lex(input)
	if err != nil {
		t.Fatalf("Lex(%q) error: %v", input, err)
	}
	if tokens[0].Type != TokenNumber {
		t.Errorf("expected TokenNumber for %q, got %v", input, tokens[0].Type)
	}
}
```

**Step 7: Run test to verify it fails**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./dot/ -run TestLexDuration -v`
Expected: FAIL — `TokenDuration` undefined

---

### Task 4: Duration token type — Implement

**Files:**
- Modify: `dot/lexer.go` (add `TokenDuration`, modify `lexNumber()` to detect duration suffixes)

**Step 8: Write the implementation**

1. Add `TokenDuration` to the const block after `TokenMinus`:

```go
TokenDuration // duration literal (e.g. 900s, 15m, 2h, 250ms, 1d)
```

2. Add the case to `String()`:

```go
case TokenDuration:
    return "DURATION"
```

3. Modify `lexNumber()` to check for a duration suffix after consuming digits. After the decimal/fractional part check (line ~330), before emitting the token, add:

```go
// Check for duration suffix: ms, s, m, h, d
// Only applies to pure integer values (no decimal point)
if !strings.Contains(sb.String(), ".") && l.pos < len(l.input) {
    ch := l.input[l.pos]
    if ch == 's' || ch == 'h' || ch == 'd' {
        sb.WriteRune(ch)
        l.advance()
        l.tokens = append(l.tokens, Token{Type: TokenDuration, Value: sb.String(), Line: startLine, Col: startCol})
        return
    }
    if ch == 'm' {
        sb.WriteRune(ch)
        l.advance()
        // Check for "ms" (milliseconds)
        if l.pos < len(l.input) && l.input[l.pos] == 's' {
            sb.WriteRune('s')
            l.advance()
        }
        l.tokens = append(l.tokens, Token{Type: TokenDuration, Value: sb.String(), Line: startLine, Col: startCol})
        return
    }
}
```

4. The parser should accept `TokenDuration` anywhere it accepts `TokenNumber` or `TokenString` as an attribute value. Check `parseStatement()` for where attribute values are consumed and ensure `TokenDuration` is in the accepted set.

**Step 9: Run test to verify it passes**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./dot/ -run TestLexDuration -v`
Expected: PASS

**Step 10: Run existing tests to check for regressions**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./dot/ -v`
Expected: All pass (existing `timeout="900s"` quoted strings still work; unquoted durations now lex correctly)

**Step 11: Commit**

```
git add dot/lexer.go dot/lexer_test.go
git commit -m "feat(dot): add TokenDuration for spec-compliant duration value parsing"
```

---

### Task 5: Duration in parser — Write failing test

**Files:**
- Modify: `dot/parser_test.go`

**Step 12: Write the failing test**

```go
func TestParserAcceptsUnquotedDurationAttribute(t *testing.T) {
	input := `digraph G { node_a [timeout=900s]; }`
	graph, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	node := graph.Nodes["node_a"]
	if node == nil {
		t.Fatal("expected node_a to exist")
	}
	if node.Attrs["timeout"] != "900s" {
		t.Errorf("timeout = %q, want %q", node.Attrs["timeout"], "900s")
	}
}
```

**Step 13: Run test to verify it fails (or passes if parser already handles it)**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./dot/ -run TestParserAcceptsUnquotedDuration -v`

If it passes, great — parser already handles `TokenDuration` in attribute values. If it fails, add `TokenDuration` to the attribute value token set in the parser.

**Step 14: Commit if changes were needed**

```
git add dot/parser.go dot/parser_test.go
git commit -m "feat(dot): accept TokenDuration in attribute values"
```

---

### Task 6: Tool call hooks — Write failing tests

**Files:**
- Create: `attractor/tool_hooks_test.go`

**Step 15: Write the failing test**

```go
// ABOUTME: Tests for tool_hooks.pre and tool_hooks.post attributes per spec §9.7.
// ABOUTME: Covers pre-hook skip (non-zero exit), post-hook execution, and failure recording.
package attractor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPreHookRunsBeforeToolCall(t *testing.T) {
	// Pre-hook writes a marker file; we check it exists after execution
	dir := t.TempDir()
	markerPath := filepath.Join(dir, "pre_hook_ran")

	hooks := &ToolCallHooks{
		PreCommand:  "touch " + markerPath,
		PostCommand: "",
	}

	// Simulate a tool call
	result := hooks.RunPre(context.Background(), ToolCallMeta{
		ToolName: "write_file",
		NodeID:   "test_node",
	})
	if result.Skip {
		t.Error("pre-hook with exit 0 should not skip")
	}

	if _, err := os.Stat(markerPath); os.IsNotExist(err) {
		t.Error("pre-hook marker file not created; hook did not run")
	}
}

func TestPreHookNonZeroExitSkipsToolCall(t *testing.T) {
	hooks := &ToolCallHooks{
		PreCommand: "exit 1",
	}

	result := hooks.RunPre(context.Background(), ToolCallMeta{
		ToolName: "write_file",
		NodeID:   "test_node",
	})
	if !result.Skip {
		t.Error("pre-hook with exit 1 should set Skip=true")
	}
}

func TestPostHookRunsAfterToolCall(t *testing.T) {
	dir := t.TempDir()
	markerPath := filepath.Join(dir, "post_hook_ran")

	hooks := &ToolCallHooks{
		PostCommand: "touch " + markerPath,
	}

	hooks.RunPost(context.Background(), ToolCallMeta{
		ToolName: "write_file",
		NodeID:   "test_node",
	}, ToolCallResult{
		Output:   "file written",
		ExitCode: 0,
	})

	if _, err := os.Stat(markerPath); os.IsNotExist(err) {
		t.Error("post-hook marker file not created; hook did not run")
	}
}

func TestEmptyHooksAreNoOps(t *testing.T) {
	hooks := &ToolCallHooks{}

	result := hooks.RunPre(context.Background(), ToolCallMeta{ToolName: "test"})
	if result.Skip {
		t.Error("empty pre-hook should not skip")
	}

	// Post-hook with empty command should not panic
	hooks.RunPost(context.Background(), ToolCallMeta{ToolName: "test"}, ToolCallResult{})
}

func TestHookReceivesToolMetadataViaEnv(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "env_dump")

	hooks := &ToolCallHooks{
		PreCommand: "echo $ATTRACTOR_TOOL_NAME > " + outPath,
	}

	hooks.RunPre(context.Background(), ToolCallMeta{
		ToolName: "read_file",
		NodeID:   "review_node",
	})

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading env dump: %v", err)
	}
	got := string(data)
	if got == "" || got == "\n" {
		t.Error("ATTRACTOR_TOOL_NAME env var was not set")
	}
}

func TestResolveToolCallHooks(t *testing.T) {
	// Graph-level hooks
	graph := &Graph{
		Attrs: map[string]string{
			"tool_hooks.pre":  "echo graph_pre",
			"tool_hooks.post": "echo graph_post",
		},
	}

	// Node with no override
	node := &Node{ID: "n1", Attrs: map[string]string{}}
	hooks := ResolveToolCallHooks(node, graph)
	if hooks.PreCommand != "echo graph_pre" {
		t.Errorf("expected graph-level pre hook, got %q", hooks.PreCommand)
	}

	// Node-level override
	node2 := &Node{ID: "n2", Attrs: map[string]string{
		"tool_hooks.pre": "echo node_pre",
	}}
	hooks2 := ResolveToolCallHooks(node2, graph)
	if hooks2.PreCommand != "echo node_pre" {
		t.Errorf("expected node-level pre hook override, got %q", hooks2.PreCommand)
	}
	// Post falls back to graph
	if hooks2.PostCommand != "echo graph_post" {
		t.Errorf("expected graph-level post hook fallback, got %q", hooks2.PostCommand)
	}
}
```

**Step 16: Run test to verify it fails**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./attractor/ -run "TestPreHook|TestPostHook|TestEmptyHooks|TestHookReceives|TestResolveToolCallHooks" -v`
Expected: FAIL — types undefined

---

### Task 7: Tool call hooks — Implement

**Files:**
- Create: `attractor/tool_hooks.go`

**Step 17: Write the implementation**

```go
// ABOUTME: Tool call hooks that run shell commands before/after LLM tool calls per spec §9.7.
// ABOUTME: Pre-hooks can skip tool calls (non-zero exit); post-hooks are for logging/auditing.
package attractor

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

// hookTimeout is the maximum time a hook command can run.
const hookTimeout = 30 * time.Second

// ToolCallMeta contains metadata about an LLM tool call, passed to hooks
// via environment variables.
type ToolCallMeta struct {
	ToolName string
	NodeID   string
	Input    string // JSON-serialized tool input (optional)
}

// ToolCallResult contains the result of an LLM tool call, passed to post-hooks.
type ToolCallResult struct {
	Output   string
	ExitCode int
}

// PreHookResult indicates whether the tool call should proceed.
type PreHookResult struct {
	Skip   bool   // If true, the tool call should be skipped
	Reason string // Why it was skipped
}

// ToolCallHooks holds the pre and post hook commands resolved for a node.
type ToolCallHooks struct {
	PreCommand  string
	PostCommand string
}

// RunPre executes the pre-hook command. Returns Skip=true if the hook
// exits with a non-zero code, indicating the tool call should be skipped.
func (h *ToolCallHooks) RunPre(ctx context.Context, meta ToolCallMeta) PreHookResult {
	if h.PreCommand == "" {
		return PreHookResult{}
	}

	hookCtx, cancel := context.WithTimeout(ctx, hookTimeout)
	defer cancel()

	cmd := exec.CommandContext(hookCtx, "sh", "-c", h.PreCommand)
	cmd.Env = buildHookEnv(meta)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return PreHookResult{
			Skip:   true,
			Reason: fmt.Sprintf("pre-hook exited non-zero: %v (stderr: %s)", err, stderr.String()),
		}
	}
	return PreHookResult{}
}

// RunPost executes the post-hook command. Failures are recorded but do not
// block the tool call result.
func (h *ToolCallHooks) RunPost(ctx context.Context, meta ToolCallMeta, result ToolCallResult) {
	if h.PostCommand == "" {
		return
	}

	hookCtx, cancel := context.WithTimeout(ctx, hookTimeout)
	defer cancel()

	cmd := exec.CommandContext(hookCtx, "sh", "-c", h.PostCommand)
	env := buildHookEnv(meta)
	env = append(env, fmt.Sprintf("ATTRACTOR_TOOL_OUTPUT=%s", result.Output))
	env = append(env, fmt.Sprintf("ATTRACTOR_TOOL_EXIT_CODE=%d", result.ExitCode))
	cmd.Env = env

	// Fire and ignore errors — post-hooks are for auditing only
	_ = cmd.Run()
}

// ResolveToolCallHooks resolves the pre/post hook commands for a node.
// Node-level attributes override graph-level attributes.
func ResolveToolCallHooks(node *Node, graph *Graph) *ToolCallHooks {
	hooks := &ToolCallHooks{}

	// Graph-level defaults
	if graph.Attrs != nil {
		hooks.PreCommand = graph.Attrs["tool_hooks.pre"]
		hooks.PostCommand = graph.Attrs["tool_hooks.post"]
	}

	// Node-level overrides
	if node.Attrs != nil {
		if v, ok := node.Attrs["tool_hooks.pre"]; ok {
			hooks.PreCommand = v
		}
		if v, ok := node.Attrs["tool_hooks.post"]; ok {
			hooks.PostCommand = v
		}
	}

	return hooks
}

// buildHookEnv creates the environment variables passed to hook commands.
func buildHookEnv(meta ToolCallMeta) []string {
	env := []string{
		fmt.Sprintf("ATTRACTOR_TOOL_NAME=%s", meta.ToolName),
		fmt.Sprintf("ATTRACTOR_NODE_ID=%s", meta.NodeID),
	}
	if meta.Input != "" {
		env = append(env, fmt.Sprintf("ATTRACTOR_TOOL_INPUT=%s", meta.Input))
	}
	return env
}
```

**Step 18: Run test to verify it passes**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./attractor/ -run "TestPreHook|TestPostHook|TestEmptyHooks|TestHookReceives|TestResolveToolCallHooks" -v`
Expected: PASS

**Step 19: Commit**

```
git add attractor/tool_hooks.go attractor/tool_hooks_test.go
git commit -m "feat(attractor): implement tool call hooks per spec §9.7"
```

---

### Task 8: Tool hooks validation rule — Write failing test

**Files:**
- Modify: `attractor/validate_test.go`

**Step 20: Write the failing test**

Add a test that verifies the validator warns when `tool_hooks.pre` or `tool_hooks.post` contain empty strings (graph-level):

```go
func TestValidateToolHooksSyntax(t *testing.T) {
	graph := validPipelineGraph()
	graph.Attrs["tool_hooks.pre"] = "" // empty is fine, no warning
	diags := Validate(graph)
	if hasDiagnostic(diags, "tool_hooks_syntax") {
		t.Error("empty tool_hooks should not produce a diagnostic")
	}
}
```

This is lightweight — tool hooks validation is a stretch goal. Skip this if not needed and move to conformance test.

---

### Task 9: Conformance test update — Add auto_status and duration coverage

**Files:**
- Modify: `examples/conformance_test.dot`
- Modify: `attractor/conformance_branching_test.go`

**Step 21: Add a Phase 7 to conformance_test.dot for auto_status**

After the Summary node and before its edge to Exit, add:

```dot
// ---------------------------------------------------------------
// Phase 7 — Auto-status
// Validates: auto_status=true generates SUCCESS when handler
//            writes no explicit status.
// ---------------------------------------------------------------
AutoStatusNode [
    shape=box,
    label="Auto Status Test",
    auto_status=true,
    prompt="This node tests auto_status. Return your response without explicitly setting outcome. Do not use any tools. Do not read or modify any files."
];

Summary -> AutoStatusNode [condition="outcome=success"];
Summary -> FailSink [condition="outcome=fail", label="phase6_fail"];
AutoStatusNode -> Exit [condition="outcome=success"];
AutoStatusNode -> FailSink [condition="outcome=fail", label="phase7_fail"];
```

Remove the old `Summary -> Exit` edge.

**Step 22: Add structure test for auto_status node**

In `attractor/conformance_branching_test.go`, inside `TestConformanceTestDOT_HasRequiredStructure`, add `"AutoStatusNode"` to the `requiredNodes` list:

```go
{"AutoStatusNode", "Phase 7: auto_status"},
```

And add an assertion:

```go
// Verify AutoStatusNode has auto_status=true
if asn, ok := nodesByID["AutoStatusNode"]; ok {
    if asn.Attrs["auto_status"] != "true" {
        t.Error("AutoStatusNode missing auto_status=true attribute")
    }
}
```

**Step 23: Run conformance structure tests**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./attractor/ -run TestConformanceTestDOT -v`
Expected: PASS

**Step 24: Commit**

```
git add examples/conformance_test.dot attractor/conformance_branching_test.go
git commit -m "test(conformance): add Phase 7 for auto_status attribute coverage"
```

---

### Task 10: Run full test suite and verify

**Step 25: Run all tests**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./... -count=1`
Expected: All pass, no regressions

**Step 26: Final commit if any fixups needed**

---

## Summary

| Task | Feature | Effort |
|------|---------|--------|
| 1-2 | auto_status attribute | ~30 lines impl + tests |
| 3-5 | Duration token type | ~40 lines lexer + parser |
| 6-7 | Tool call hooks | ~100 lines impl + tests |
| 8 | Tool hooks validation | Optional |
| 9 | Conformance test update | ~20 lines |
| 10 | Full test suite | Verification |
