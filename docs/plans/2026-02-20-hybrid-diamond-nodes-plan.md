# Hybrid Diamond Nodes Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make diamond nodes (shape=diamond) execute their prompt via an LLM agent when a prompt attribute is present, then use the agent's outcome for conditional edge routing. Diamond nodes without prompts keep the current pass-through behavior.

**Architecture:** Add `Backend`, `BaseURL`, and `EventHandler` fields to `ConditionalHandler`. When `prompt` is present, delegate to the backend, parse the agent's output with `DetectOutcomeMarker`, and return the detected status. Wire the backend in `engine.go` at the same two sites where `CodergenHandler` is wired.

**Tech Stack:** Go, existing `CodergenBackend` interface, existing `DetectOutcomeMarker` function, existing `fakeBackend` test double.

---

### Task 1: Test — Diamond with prompt runs agent

**Files:**
- Create: `attractor/handlers_conditional_test.go`

**Step 1: Write the failing test**

Create `attractor/handlers_conditional_test.go`:

```go
// ABOUTME: Tests for ConditionalHandler covering pass-through, prompt-driven agent execution, and outcome detection.
// ABOUTME: Validates that diamond nodes with prompts dispatch to the backend and route based on agent output.
package attractor

import (
	"context"
	"testing"
)

func TestConditionalHandlerWithPromptCallsBackend(t *testing.T) {
	backend := &fakeBackend{}
	h := &ConditionalHandler{Backend: backend}

	node := &Node{
		ID: "verify_red",
		Attrs: map[string]string{
			"shape":  "diamond",
			"prompt": "Run go test and report OUTCOME:PASS or OUTCOME:FAIL",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(backend.calls) != 1 {
		t.Fatalf("expected 1 backend call, got %d", len(backend.calls))
	}
	if backend.calls[0].Prompt != "Run go test and report OUTCOME:PASS or OUTCOME:FAIL" {
		t.Errorf("wrong prompt: %q", backend.calls[0].Prompt)
	}
	if outcome.Status != StatusSuccess {
		t.Errorf("expected StatusSuccess, got %v", outcome.Status)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./attractor/ -run TestConditionalHandlerWithPromptCallsBackend -v`
Expected: FAIL — `ConditionalHandler` has no `Backend` field

**Step 3: Commit**

```
feat(attractor): add failing test for prompt-driven diamond nodes
```

---

### Task 2: Test — Diamond with prompt detects OUTCOME:FAIL

**Files:**
- Modify: `attractor/handlers_conditional_test.go`

**Step 1: Write the failing test**

Add to `attractor/handlers_conditional_test.go`:

```go
func TestConditionalHandlerWithPromptDetectsOutcomeFail(t *testing.T) {
	backend := &fakeBackend{
		runAgentFn: func(ctx context.Context, config AgentRunConfig) (*AgentRunResult, error) {
			return &AgentRunResult{
				Output:  "Tests failed.\nOUTCOME:FAIL",
				Success: true,
			}, nil
		},
	}
	h := &ConditionalHandler{Backend: backend}

	node := &Node{
		ID: "verify_green",
		Attrs: map[string]string{
			"shape":  "diamond",
			"prompt": "Run tests, report outcome",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if outcome.Status != StatusFail {
		t.Errorf("expected StatusFail from OUTCOME:FAIL marker, got %v", outcome.Status)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./attractor/ -run TestConditionalHandlerWithPromptDetectsOutcomeFail -v`
Expected: FAIL — same compile error, `Backend` field doesn't exist yet

**Step 3: Commit**

```
test(attractor): add test for OUTCOME:FAIL detection in diamond nodes
```

---

### Task 3: Test — Diamond without prompt stays pass-through

**Files:**
- Modify: `attractor/handlers_conditional_test.go`

**Step 1: Write the failing test**

Add to `attractor/handlers_conditional_test.go`:

```go
func TestConditionalHandlerWithoutPromptPassesThrough(t *testing.T) {
	h := &ConditionalHandler{Backend: &fakeBackend{}}

	node := &Node{
		ID:    "route_check",
		Attrs: map[string]string{"shape": "diamond"},
	}
	pctx := NewContext()
	pctx.Set("outcome", "fail")
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No prompt → should pass through the prior outcome
	if outcome.Status != StatusFail {
		t.Errorf("expected StatusFail from pass-through, got %v", outcome.Status)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./attractor/ -run TestConditionalHandlerWithoutPromptPassesThrough -v`
Expected: FAIL — compile error

**Step 3: Commit**

```
test(attractor): add test for diamond pass-through without prompt
```

---

### Task 4: Test — Diamond with prompt but nil backend returns fail

**Files:**
- Modify: `attractor/handlers_conditional_test.go`

**Step 1: Write the failing test**

Add to `attractor/handlers_conditional_test.go`:

```go
func TestConditionalHandlerWithPromptNilBackendReturnsFail(t *testing.T) {
	h := &ConditionalHandler{Backend: nil}

	node := &Node{
		ID: "verify_no_backend",
		Attrs: map[string]string{
			"shape":  "diamond",
			"prompt": "Run tests",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if outcome.Status != StatusFail {
		t.Errorf("expected StatusFail with nil backend, got %v", outcome.Status)
	}
	if outcome.FailureReason == "" {
		t.Error("expected a failure reason when backend is nil")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./attractor/ -run TestConditionalHandlerWithPromptNilBackendReturnsFail -v`
Expected: FAIL — compile error

**Step 3: Commit**

```
test(attractor): add test for nil backend on prompt-driven diamond
```

---

### Task 5: Test — Diamond with prompt and agent failure (Success: false)

**Files:**
- Modify: `attractor/handlers_conditional_test.go`

**Step 1: Write the failing test**

Add to `attractor/handlers_conditional_test.go`:

```go
func TestConditionalHandlerWithPromptAgentFailure(t *testing.T) {
	backend := &fakeBackend{
		runAgentFn: func(ctx context.Context, config AgentRunConfig) (*AgentRunResult, error) {
			return &AgentRunResult{
				Output:  "agent crashed",
				Success: false,
			}, nil
		},
	}
	h := &ConditionalHandler{Backend: backend}

	node := &Node{
		ID: "verify_agent_fail",
		Attrs: map[string]string{
			"shape":  "diamond",
			"prompt": "Run tests",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if outcome.Status != StatusFail {
		t.Errorf("expected StatusFail from agent failure, got %v", outcome.Status)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./attractor/ -run TestConditionalHandlerWithPromptAgentFailure -v`
Expected: FAIL — compile error

**Step 3: Commit**

```
test(attractor): add test for agent failure on prompt-driven diamond
```

---

### Task 6: Test — Diamond with prompt passes model/provider/max_turns

**Files:**
- Modify: `attractor/handlers_conditional_test.go`

**Step 1: Write the failing test**

Add to `attractor/handlers_conditional_test.go`:

```go
func TestConditionalHandlerWithPromptPassesConfig(t *testing.T) {
	backend := &fakeBackend{}
	h := &ConditionalHandler{Backend: backend, BaseURL: "https://default.example.com"}

	node := &Node{
		ID: "verify_config",
		Attrs: map[string]string{
			"shape":        "diamond",
			"prompt":       "Run verification",
			"llm_model":    "gpt-4o",
			"llm_provider": "openai",
			"max_turns":    "5",
			"base_url":     "https://custom.example.com",
		},
	}
	pctx := NewContext()
	pctx.Set("goal", "build app")
	store := NewArtifactStore(t.TempDir())

	_, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(backend.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(backend.calls))
	}
	call := backend.calls[0]
	if call.Model != "gpt-4o" {
		t.Errorf("expected model gpt-4o, got %q", call.Model)
	}
	if call.Provider != "openai" {
		t.Errorf("expected provider openai, got %q", call.Provider)
	}
	if call.MaxTurns != 5 {
		t.Errorf("expected max_turns 5, got %d", call.MaxTurns)
	}
	if call.BaseURL != "https://custom.example.com" {
		t.Errorf("expected base_url from node attr, got %q", call.BaseURL)
	}
	if call.Goal != "build app" {
		t.Errorf("expected goal from context, got %q", call.Goal)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./attractor/ -run TestConditionalHandlerWithPromptPassesConfig -v`
Expected: FAIL — compile error

**Step 3: Commit**

```
test(attractor): add test for config passthrough in prompt-driven diamond
```

---

### Task 7: Implement hybrid ConditionalHandler

**Files:**
- Modify: `attractor/handlers_conditional.go`

**Step 1: Implement the hybrid handler**

Replace the contents of `attractor/handlers_conditional.go` with the hybrid implementation. Key changes:
- Add `Backend CodergenBackend`, `BaseURL string`, `EventHandler func(EngineEvent)` fields
- In `Execute`: check for `prompt` attr. If empty, do existing pass-through. If present, run backend agent, detect outcome marker, return appropriate status.
- Config building mirrors `CodergenHandler` (prompt, label, model, provider, max_turns, goal, fidelity, workdir, base_url, system_prompt).
- Outcome detection: `DetectOutcomeMarker(result.Output)` first, then `result.Success` fallback.
- Set `outcome` in ContextUpdates so downstream nodes can read it.

```go
// ABOUTME: Hybrid conditional handler for the attractor pipeline runner.
// ABOUTME: Runs an agent when a prompt is present, otherwise passes through prior outcome for edge routing.
package attractor

import (
	"context"
	"fmt"
	"strconv"
)

// ConditionalHandler handles conditional routing nodes (shape=diamond).
// When a node has a prompt attribute, it runs an agent via the Backend and
// uses the agent's reported outcome (via DetectOutcomeMarker) for edge routing.
// When no prompt is present, it passes through the prior node's outcome from
// the pipeline context so edge conditions evaluate correctly.
type ConditionalHandler struct {
	// Backend is the agent execution backend. When nil and a prompt is present,
	// the handler returns StatusFail indicating no LLM backend is configured.
	Backend CodergenBackend

	// BaseURL is the default API base URL for the LLM provider.
	BaseURL string

	// EventHandler receives agent-level observability events.
	EventHandler func(EngineEvent)
}

// Type returns the handler type string "conditional".
func (h *ConditionalHandler) Type() string {
	return "conditional"
}

// Execute handles a diamond node. If the node has a prompt attribute, it runs
// an agent and determines the outcome from the agent's output. If no prompt,
// it passes through the prior node's outcome from the pipeline context.
func (h *ConditionalHandler) Execute(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	attrs := node.Attrs
	if attrs == nil {
		attrs = make(map[string]string)
	}

	prompt := attrs["prompt"]

	// No prompt → pass-through mode (original behavior)
	if prompt == "" {
		return h.passThrough(node, pctx)
	}

	// Prompt present → run agent
	return h.runAgent(ctx, node, attrs, prompt, pctx, store)
}

// passThrough reads the outcome status from the pipeline context (set by the
// preceding node) and returns it as this node's status.
func (h *ConditionalHandler) passThrough(node *Node, pctx *Context) (*Outcome, error) {
	status := StatusSuccess
	if prev, ok := pctx.Get("outcome").(string); ok && prev != "" {
		status = StageStatus(prev)
	}

	return &Outcome{
		Status: status,
		Notes:  "Conditional node evaluated: " + node.ID,
		ContextUpdates: map[string]any{
			"last_stage": node.ID,
		},
	}, nil
}

// runAgent dispatches to the Backend and determines outcome from the agent output.
func (h *ConditionalHandler) runAgent(ctx context.Context, node *Node, attrs map[string]string, prompt string, pctx *Context, store *ArtifactStore) (*Outcome, error) {
	if h.Backend == nil {
		return &Outcome{
			Status:        StatusFail,
			FailureReason: "no LLM backend configured: set ANTHROPIC_API_KEY, OPENAI_API_KEY, or GEMINI_API_KEY",
		}, nil
	}

	label := attrs["label"]
	if label == "" {
		label = node.ID
	}

	maxTurns := 20
	if maxTurnsStr := attrs["max_turns"]; maxTurnsStr != "" {
		if parsed, err := strconv.Atoi(maxTurnsStr); err == nil && parsed > 0 {
			maxTurns = parsed
		}
	}

	goal := ""
	if goalVal := pctx.Get("goal"); goalVal != nil {
		if goalStr, ok := goalVal.(string); ok {
			goal = goalStr
		}
	}

	fidelityMode := ""
	if f := attrs["fidelity"]; f != "" && IsValidFidelity(f) {
		fidelityMode = f
	} else if fVal := pctx.Get("_fidelity_mode"); fVal != nil {
		if fStr, ok := fVal.(string); ok && IsValidFidelity(fStr) {
			fidelityMode = fStr
		}
	}

	workDir := attrs["workdir"]
	if workDir == "" && store != nil && store.BaseDir() != "" {
		workDir = store.BaseDir()
	}

	baseURL := attrs["base_url"]
	if baseURL == "" {
		if val := pctx.Get("base_url"); val != nil {
			if s, ok := val.(string); ok {
				baseURL = s
			}
		}
	}
	if baseURL == "" {
		baseURL = h.BaseURL
	}

	systemPrompt := attrs["system_prompt"]
	if systemPrompt == "" {
		if spVal := pctx.Get("system_prompt"); spVal != nil {
			if spStr, ok := spVal.(string); ok {
				systemPrompt = spStr
			}
		}
	}

	config := AgentRunConfig{
		Prompt:       prompt,
		Model:        attrs["llm_model"],
		Provider:     attrs["llm_provider"],
		BaseURL:      baseURL,
		WorkDir:      workDir,
		Goal:         goal,
		NodeID:       node.ID,
		MaxTurns:     maxTurns,
		FidelityMode: fidelityMode,
		SystemPrompt: systemPrompt,
		EventHandler: h.EventHandler,
	}

	result, err := h.Backend.RunAgent(ctx, config)
	if err != nil {
		return &Outcome{
			Status:        StatusFail,
			FailureReason: fmt.Sprintf("agent backend error: %v", err),
			ContextUpdates: map[string]any{
				"last_stage": node.ID,
				"outcome":    "fail",
			},
		}, nil
	}

	// Determine outcome: explicit marker takes precedence over Success field
	status := StatusSuccess
	if marker, found := DetectOutcomeMarker(result.Output); found {
		if marker == "fail" {
			status = StatusFail
		}
	} else if !result.Success {
		status = StatusFail
	}

	updates := map[string]any{
		"last_stage": node.ID,
		"outcome":    string(status),
	}

	notes := fmt.Sprintf("Conditional agent completed: %s", label)
	failureReason := ""
	if status == StatusFail {
		failureReason = fmt.Sprintf("verification failed: %s", result.Output)
	}

	// Store agent output as artifact
	if result.Output != "" && store != nil {
		artifactID := node.ID + ".output"
		if _, storeErr := store.Store(artifactID, "agent_output", []byte(result.Output)); storeErr != nil {
			pctx.AppendLog(fmt.Sprintf("warning: failed to store agent output artifact: %v", storeErr))
		}
	}

	return &Outcome{
		Status:         status,
		Notes:          notes,
		FailureReason:  failureReason,
		ContextUpdates: updates,
	}, nil
}
```

**Step 2: Run all conditional handler tests**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./attractor/ -run TestConditionalHandler -v`
Expected: ALL PASS

**Step 3: Run full attractor test suite**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./attractor/ -v -count=1`
Expected: ALL PASS — existing tests should not break since pass-through behavior is preserved

**Step 4: Commit**

```
feat(attractor): implement hybrid ConditionalHandler with prompt-driven agent execution
```

---

### Task 8: Test — Engine wires backend into ConditionalHandler

**Files:**
- Modify: `attractor/handlers_conditional_test.go` (or find existing engine wiring test file)

**Step 1: Write the failing test**

Add to `attractor/handlers_conditional_test.go`:

```go
func TestEngineWiresBackendIntoConditionalHandler(t *testing.T) {
	registry := DefaultHandlerRegistry()

	// Before wiring, backend should be nil
	condHandler := registry.Get("conditional")
	if condHandler == nil {
		t.Fatal("expected conditional handler in default registry")
	}
	ch, ok := condHandler.(*ConditionalHandler)
	if !ok {
		t.Fatalf("expected *ConditionalHandler, got %T", condHandler)
	}
	if ch.Backend != nil {
		t.Error("expected nil backend before wiring")
	}

	// Simulate engine wiring (same pattern as engine.go does for codergen)
	backend := &fakeBackend{}
	if condHandler := registry.Get("conditional"); condHandler != nil {
		if ch, ok := unwrapHandler(condHandler).(*ConditionalHandler); ok {
			ch.Backend = backend
			ch.BaseURL = "https://test.example.com"
		}
	}

	// After wiring, backend should be set
	ch2 := registry.Get("conditional").(*ConditionalHandler)
	if ch2.Backend == nil {
		t.Error("expected backend to be wired")
	}
	if ch2.BaseURL != "https://test.example.com" {
		t.Errorf("expected base URL to be wired, got %q", ch2.BaseURL)
	}
}
```

**Step 2: Run test to verify it passes** (this just tests the wiring pattern works, before modifying engine.go)

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./attractor/ -run TestEngineWiresBackendIntoConditionalHandler -v`
Expected: PASS — the struct already has the fields from Task 7

**Step 3: Commit**

```
test(attractor): add test for engine backend wiring into ConditionalHandler
```

---

### Task 9: Wire backend into ConditionalHandler in engine.go

**Files:**
- Modify: `attractor/engine.go` (two locations: Run ~line 170 and resume ~line 347)

**Step 1: Add wiring at both sites**

At each location where the engine wires `CodergenHandler`, add the same pattern for `ConditionalHandler` immediately after:

```go
// Wire backend into conditional handler for prompt-driven diamond nodes
if condHandler := registry.Get("conditional"); condHandler != nil {
	if ch, ok := unwrapHandler(condHandler).(*ConditionalHandler); ok {
		ch.Backend = e.config.Backend
		ch.BaseURL = e.config.BaseURL
		ch.EventHandler = e.emitEvent
	}
}
```

There are exactly two wiring sites:
1. In `Run()` around line 170, after the codergen wiring block
2. In the resume path around line 347, after the codergen wiring block

**Step 2: Run full attractor test suite**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./attractor/ -v -count=1`
Expected: ALL PASS

**Step 3: Commit**

```
feat(attractor): wire backend into ConditionalHandler in engine
```

---

### Task 10: Integration test — End-to-end diamond with conditional routing

**Files:**
- Modify: `attractor/handlers_conditional_test.go`

**Step 1: Write the integration test**

This tests the full flow: diamond node with prompt → agent runs → OUTCOME:FAIL detected → edge selection picks the fail branch.

```go
func TestConditionalHandlerOutcomeAffectsEdgeSelection(t *testing.T) {
	// Build a simple graph: start → verify (diamond with prompt) → pass_node / fail_node
	graph := &Graph{
		Nodes: []*Node{
			{ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			{ID: "verify", Attrs: map[string]string{
				"shape":  "diamond",
				"prompt": "Run tests",
			}},
			{ID: "pass_node", Attrs: map[string]string{"shape": "box"}},
			{ID: "fail_node", Attrs: map[string]string{"shape": "box"}},
		},
		Edges: []*Edge{
			{From: "start", To: "verify"},
			{From: "verify", To: "pass_node", Attrs: map[string]string{"condition": "outcome = success"}},
			{From: "verify", To: "fail_node", Attrs: map[string]string{"condition": "outcome = fail"}},
		},
	}

	// Agent reports OUTCOME:FAIL
	backend := &fakeBackend{
		runAgentFn: func(ctx context.Context, config AgentRunConfig) (*AgentRunResult, error) {
			return &AgentRunResult{
				Output:  "Tests failed: 2 errors\nOUTCOME:FAIL",
				Success: true,
			}, nil
		},
	}
	h := &ConditionalHandler{Backend: backend}

	verifyNode := graph.Nodes[1]
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), verifyNode, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if outcome.Status != StatusFail {
		t.Fatalf("expected StatusFail, got %v", outcome.Status)
	}

	// Edge selection should pick the fail_node edge
	edge := SelectEdge(verifyNode, outcome, pctx, graph)
	if edge == nil {
		t.Fatal("expected an edge to be selected")
	}
	if edge.To != "fail_node" {
		t.Errorf("expected edge to fail_node, got %q", edge.To)
	}
}
```

**Step 2: Run test**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./attractor/ -run TestConditionalHandlerOutcomeAffectsEdgeSelection -v`
Expected: PASS

**Step 3: Commit**

```
test(attractor): add integration test for diamond agent outcome + edge selection
```

---

### Task 11: Run full test suite and verify

**Step 1: Run all tests**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./... -count=1`
Expected: ALL PASS

**Step 2: Commit design doc**

```
docs: add hybrid diamond nodes design and implementation plan
```
