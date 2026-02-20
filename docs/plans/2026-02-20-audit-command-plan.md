# Audit Command Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add `mammoth audit [runID]` subcommand that loads pipeline run data and uses an LLM to generate a narrative diagnosis of what happened, especially for failures.

**Architecture:** New `attractor/audit.go` with `GenerateAudit()` that builds a structured context blob from RunState/events/graph, sends it to an LLM via `llm.Generate()`, and returns the narrative. CLI wires it as a subcommand via the existing `parseXxxArgs` pattern.

**Tech Stack:** Go, `llm.FromEnv()` for provider auto-detection, `FSRunStateStore` for run data, `attractor.Parse()` for graph flow summary.

---

### Task 1: Write buildAuditContext tests

**Files:**
- Create: `attractor/audit_test.go`

**Step 1: Write the failing test**

```go
// ABOUTME: Tests for pipeline run audit narrative generation.
// ABOUTME: Covers context construction from run data and event summarization.
package attractor

import (
	"strings"
	"testing"
	"time"
)

func TestBuildAuditContext_FailedRun(t *testing.T) {
	startTime := time.Date(2026, 2, 20, 11, 39, 48, 0, time.UTC)
	endTime := time.Date(2026, 2, 20, 11, 39, 53, 0, time.UTC)

	req := AuditRequest{
		State: &RunState{
			ID:           "ebbe59cd241c09df",
			PipelineFile: "kayabot4.dot",
			Status:       "failed",
			StartedAt:    startTime,
			CompletedAt:  &endTime,
			Error:        `node "setup" visited 3 times (max 3)`,
		},
		Events: []EngineEvent{
			{Type: EventPipelineStarted, Timestamp: startTime, Data: map[string]any{"workdir": "/tmp/test"}},
			{Type: EventStageStarted, NodeID: "start", Timestamp: startTime},
			{Type: EventStageCompleted, NodeID: "start", Timestamp: startTime},
			{Type: EventStageStarted, NodeID: "setup", Timestamp: startTime},
			{Type: EventStageFailed, NodeID: "setup", Timestamp: startTime.Add(2 * time.Second), Data: map[string]any{"reason": "429 rate limit"}},
			{Type: EventPipelineFailed, Timestamp: endTime, Data: map[string]any{"error": "max visits"}},
		},
		Graph: &Graph{
			Nodes: map[string]*Node{
				"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
				"setup": {ID: "setup", Attrs: map[string]string{"shape": "box", "prompt": "set up project"}},
				"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
			},
			Edges: []*Edge{
				{From: "start", To: "setup", Attrs: map[string]string{}},
				{From: "setup", To: "exit", Attrs: map[string]string{}},
			},
		},
		Verbose: false,
	}

	ctx := buildAuditContext(req)

	if ctx == "" {
		t.Fatal("expected non-empty audit context")
	}
	if !strings.Contains(ctx, "kayabot4.dot") {
		t.Error("expected pipeline file name in context")
	}
	if !strings.Contains(ctx, "failed") {
		t.Error("expected status in context")
	}
	if !strings.Contains(ctx, "setup") {
		t.Error("expected node names in context")
	}
	if !strings.Contains(ctx, "429 rate limit") {
		t.Error("expected error details in context")
	}
}

func TestBuildAuditContext_IncludesFlowSummary(t *testing.T) {
	startTime := time.Now()

	req := AuditRequest{
		State: &RunState{
			ID:        "abc123",
			Status:    "completed",
			StartedAt: startTime,
		},
		Events: []EngineEvent{},
		Graph: &Graph{
			Nodes: map[string]*Node{
				"start":  {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
				"build":  {ID: "build", Attrs: map[string]string{"shape": "box"}},
				"verify": {ID: "verify", Attrs: map[string]string{"shape": "box"}},
				"exit":   {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
			},
			Edges: []*Edge{
				{From: "start", To: "build", Attrs: map[string]string{}},
				{From: "build", To: "verify", Attrs: map[string]string{}},
				{From: "verify", To: "exit", Attrs: map[string]string{}},
			},
		},
		Verbose: false,
	}

	ctx := buildAuditContext(req)

	// Should contain flow path from start to exit.
	if !strings.Contains(ctx, "start") || !strings.Contains(ctx, "exit") {
		t.Error("expected flow summary with start and exit nodes")
	}
}

func TestBuildAuditContext_VerboseIncludesToolDetails(t *testing.T) {
	startTime := time.Now()

	req := AuditRequest{
		State: &RunState{
			ID:        "abc123",
			Status:    "completed",
			StartedAt: startTime,
		},
		Events: []EngineEvent{
			{Type: EventAgentToolCallStart, NodeID: "build", Timestamp: startTime, Data: map[string]any{
				"tool_name": "bash_exec",
				"arguments": `{"command": "go build ./..."}`,
			}},
			{Type: EventAgentToolCallEnd, NodeID: "build", Timestamp: startTime.Add(time.Second), Data: map[string]any{
				"tool_name":   "bash_exec",
				"duration_ms": 1200,
			}},
		},
		Graph:   &Graph{Nodes: map[string]*Node{}, Edges: []*Edge{}},
		Verbose: true,
	}

	ctx := buildAuditContext(req)

	if !strings.Contains(ctx, "bash_exec") {
		t.Error("verbose mode should include tool names")
	}
	if !strings.Contains(ctx, "go build") {
		t.Error("verbose mode should include tool arguments")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./attractor/ -run TestBuildAuditContext -v`
Expected: FAIL — `undefined: AuditRequest`, `undefined: buildAuditContext`

**Step 3: Commit (test-only)**

```bash
git add attractor/audit_test.go
git commit -m "test(attractor): add audit context builder tests"
```

---

### Task 2: Implement AuditRequest, AuditReport, buildAuditContext

**Files:**
- Create: `attractor/audit.go`

**Step 1: Implement the types and context builder**

```go
// ABOUTME: Pipeline run audit narrative generator using LLM analysis.
// ABOUTME: Builds structured context from run events and generates human-readable diagnostic reports.
package attractor

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/2389-research/mammoth/llm"
)

// AuditRequest holds all the data needed to generate an audit narrative.
type AuditRequest struct {
	State   *RunState
	Events  []EngineEvent
	Graph   *Graph
	Verbose bool
}

// AuditReport holds the generated audit narrative.
type AuditReport struct {
	Narrative string
}

// buildAuditContext transforms run data into a structured text blob for the LLM.
func buildAuditContext(req AuditRequest) string {
	var b strings.Builder

	// Run metadata
	b.WriteString("## Run Metadata\n")
	b.WriteString(fmt.Sprintf("Run ID: %s\n", req.State.ID))
	b.WriteString(fmt.Sprintf("Pipeline: %s\n", req.State.PipelineFile))
	b.WriteString(fmt.Sprintf("Status: %s\n", req.State.Status))

	duration := "unknown"
	if req.State.CompletedAt != nil {
		d := req.State.CompletedAt.Sub(req.State.StartedAt)
		duration = d.Round(100 * time.Millisecond).String()
	}
	b.WriteString(fmt.Sprintf("Duration: %s\n", duration))

	if req.State.Error != "" {
		b.WriteString(fmt.Sprintf("Error: %s\n", req.State.Error))
	}

	// Pipeline flow
	if req.Graph != nil {
		b.WriteString("\n## Pipeline Flow\n")
		flow := linearizeGraph(req.Graph)
		b.WriteString(flow + "\n")
	}

	// Event timeline
	b.WriteString("\n## Event Timeline\n")
	var baseTime time.Time
	for _, evt := range req.Events {
		if baseTime.IsZero() {
			baseTime = evt.Timestamp
		}
		offset := evt.Timestamp.Sub(baseTime).Round(100 * time.Millisecond)
		line := fmt.Sprintf("+%s  [%s]", offset, evt.Type)
		if evt.NodeID != "" {
			line += fmt.Sprintf(" node=%s", evt.NodeID)
		}

		// Include event data based on verbosity
		if evt.Data != nil {
			switch evt.Type {
			case EventStageFailed, EventPipelineFailed:
				// Always include failure reasons
				if reason, ok := evt.Data["reason"]; ok {
					line += fmt.Sprintf(" reason=%v", reason)
				}
				if errMsg, ok := evt.Data["error"]; ok {
					line += fmt.Sprintf(" error=%v", errMsg)
				}
			case EventAgentToolCallStart:
				if req.Verbose {
					if name, ok := evt.Data["tool_name"]; ok {
						line += fmt.Sprintf(" tool=%v", name)
					}
					if args, ok := evt.Data["arguments"]; ok {
						line += fmt.Sprintf(" args=%v", args)
					}
				}
			case EventAgentToolCallEnd:
				if req.Verbose {
					if name, ok := evt.Data["tool_name"]; ok {
						line += fmt.Sprintf(" tool=%v", name)
					}
					if dur, ok := evt.Data["duration_ms"]; ok {
						line += fmt.Sprintf(" duration=%vms", dur)
					}
				}
			case EventAgentLLMTurn:
				if req.Verbose {
					if tokens, ok := evt.Data["total_tokens"]; ok {
						line += fmt.Sprintf(" tokens=%v", tokens)
					}
				}
			}
		}

		b.WriteString(line + "\n")
	}

	// Summarize tool usage when not verbose
	if !req.Verbose {
		toolCounts := map[string]int{}
		llmTurns := 0
		for _, evt := range req.Events {
			switch evt.Type {
			case EventAgentToolCallStart:
				if name, ok := evt.Data["tool_name"].(string); ok {
					toolCounts[name]++
				}
			case EventAgentLLMTurn:
				llmTurns++
			}
		}
		if len(toolCounts) > 0 || llmTurns > 0 {
			b.WriteString("\n## Agent Activity Summary\n")
			b.WriteString(fmt.Sprintf("LLM turns: %d\n", llmTurns))
			for tool, count := range toolCounts {
				b.WriteString(fmt.Sprintf("Tool %s: %d call(s)\n", tool, count))
			}
		}
	}

	return b.String()
}

// linearizeGraph walks the graph from start to exit via BFS and returns
// a human-readable flow string like "start → build → verify → exit".
func linearizeGraph(g *Graph) string {
	start := g.FindStartNode()
	if start == nil {
		return "(no start node found)"
	}

	visited := map[string]bool{}
	var path []string
	queue := []string{start.ID}
	visited[start.ID] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		path = append(path, current)

		for _, e := range g.OutgoingEdges(current) {
			if !visited[e.To] {
				visited[e.To] = true
				queue = append(queue, e.To)
			}
		}
	}

	return strings.Join(path, " → ")
}
```

**Step 2: Run tests to verify they pass**

Run: `go test ./attractor/ -run TestBuildAuditContext -v`
Expected: All 3 tests PASS

**Step 3: Commit**

```bash
git add attractor/audit.go attractor/audit_test.go
git commit -m "feat(attractor): add audit context builder with tests"
```

---

### Task 3: Implement GenerateAudit (LLM call)

**Files:**
- Modify: `attractor/audit.go` — add GenerateAudit function

**Step 1: Write a test for GenerateAudit call structure**

Add to `attractor/audit_test.go`:

```go
func TestGenerateAudit_RequiresClient(t *testing.T) {
	req := AuditRequest{
		State:  &RunState{ID: "test", Status: "failed", StartedAt: time.Now()},
		Events: []EngineEvent{},
		Graph:  &Graph{Nodes: map[string]*Node{}, Edges: []*Edge{}},
	}

	_, err := GenerateAudit(context.Background(), req, nil)
	if err == nil {
		t.Error("expected error when no LLM client provided")
	}
}
```

**Step 2: Implement GenerateAudit**

Add to `attractor/audit.go`:

```go
// auditSystemPrompt instructs the LLM to produce a structured audit narrative.
const auditSystemPrompt = `You are a pipeline execution analyst for "mammoth", a DOT-based AI pipeline runner.

Given the run metadata, pipeline graph, and event timeline, produce a concise audit report.

Report format (use plain text, not markdown):

SUMMARY
One paragraph: what pipeline ran, what happened, how it ended.

TIMELINE
Chronological list of key events with relative timestamps (+0.0s format).
Group repeated failures. Show each node's outcome (passed/failed/skipped).

DIAGNOSIS
Root cause analysis. Identify patterns:
- Rate limits (429 errors) — transient, suggest retry policy
- Retry loops — identify which node is looping and why
- Agent errors — tool failures, LLM errors
- Validation errors — graph structure issues
- Context cancellation — user interrupted

SUGGESTIONS
2-4 actionable next steps. Reference specific mammoth flags when applicable
(e.g. -retry patient, -fix, max_node_visits, goal_gate).

Keep the report concise. Use plain language. No markdown headers — use ALL CAPS section names.`

// GenerateAudit sends run data to an LLM and returns a narrative audit report.
// The client parameter must be a configured *llm.Client (use llm.FromEnv()).
func GenerateAudit(ctx context.Context, req AuditRequest, client *llm.Client) (*AuditReport, error) {
	if client == nil {
		return nil, fmt.Errorf("audit requires an LLM client — set ANTHROPIC_API_KEY, OPENAI_API_KEY, or GEMINI_API_KEY")
	}

	auditCtx := buildAuditContext(req)

	result, err := llm.Generate(ctx, llm.GenerateOptions{
		System: auditSystemPrompt,
		Prompt: auditCtx,
		Client: client,
	})
	if err != nil {
		return nil, fmt.Errorf("LLM audit generation failed: %w", err)
	}

	return &AuditReport{
		Narrative: result.Text,
	}, nil
}
```

**Step 3: Run tests**

Run: `go test ./attractor/ -run TestGenerateAudit -v`
Expected: PASS (the nil-client test)

Run: `go build ./...`
Expected: Clean build

**Step 4: Commit**

```bash
git add attractor/audit.go attractor/audit_test.go
git commit -m "feat(attractor): add LLM-powered GenerateAudit function"
```

---

### Task 4: Wire `mammoth audit` subcommand

**Files:**
- Modify: `cmd/mammoth/main.go` — add parseAuditArgs, runAudit
- Modify: `cmd/mammoth/help.go` — add audit to usage/examples

**Step 1: Add auditConfig and parseAuditArgs**

Add to `cmd/mammoth/main.go`:

```go
// auditConfig holds configuration for the "mammoth audit" subcommand.
type auditConfig struct {
	runID   string
	verbose bool
	dataDir string
}

// parseAuditArgs checks whether args starts with the "audit" subcommand and,
// if so, parses audit-specific flags. Returns the config and true if "audit"
// was detected, or a zero value and false otherwise.
func parseAuditArgs(args []string) (auditConfig, bool) {
	if len(args) == 0 || args[0] != "audit" {
		return auditConfig{}, false
	}

	var cfg auditConfig
	fs := flag.NewFlagSet("mammoth audit", flag.ContinueOnError)
	fs.BoolVar(&cfg.verbose, "verbose", false, "Include full tool call details")
	fs.StringVar(&cfg.dataDir, "data-dir", "", "Data directory (default: .mammoth/ in CWD)")

	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: mammoth audit [flags] [runID]")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Generate a narrative audit of a pipeline run.")
		fmt.Fprintln(os.Stderr, "With no runID, audits the most recent run.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Flags:")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		os.Exit(2)
	}

	if fs.NArg() > 0 {
		cfg.runID = fs.Arg(0)
	}

	return cfg, true
}
```

**Step 2: Add runAudit function**

```go
// runAudit loads a pipeline run and generates an LLM-powered audit narrative.
func runAudit(cfg auditConfig) int {
	// Resolve data directory.
	dataDir := cfg.dataDir
	if dataDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		dataDir = filepath.Join(cwd, ".mammoth")
	}

	runsDir := filepath.Join(dataDir, "runs")
	store, err := attractor.NewFSRunStateStore(runsDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: could not open run store: %v\n", err)
		return 1
	}

	// Find the target run.
	runID := cfg.runID
	if runID == "" {
		// Find most recent run.
		states, err := store.List()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		if len(states) == 0 {
			fmt.Fprintln(os.Stderr, "error: no runs found in", runsDir)
			return 1
		}
		// Pick the most recent by start time.
		latest := states[0]
		for _, s := range states[1:] {
			if s.StartedAt.After(latest.StartedAt) {
				latest = s
			}
		}
		runID = latest.ID
	}

	state, err := store.Get(runID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: could not load run %s: %v\n", runID, err)
		return 1
	}

	// Parse graph from stored source for flow summary.
	var graph *attractor.Graph
	if state.Source != "" {
		g, parseErr := attractor.Parse(state.Source)
		if parseErr == nil {
			transforms := attractor.DefaultTransforms()
			graph = attractor.ApplyTransforms(g, transforms...)
		}
	}

	// Create LLM client.
	client, err := llm.FromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: audit requires an LLM API key")
		fmt.Fprintln(os.Stderr, "Set one of: ANTHROPIC_API_KEY, OPENAI_API_KEY, or GEMINI_API_KEY")
		return 1
	}

	req := attractor.AuditRequest{
		State:   state,
		Events:  state.Events,
		Graph:   graph,
		Verbose: cfg.verbose,
	}

	report, err := attractor.GenerateAudit(context.Background(), req, client)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	fmt.Println(report.Narrative)
	return 0
}
```

**Step 3: Wire into main() dispatch**

In `main()`, add audit detection alongside serve and setup (before regular flag parsing):

```go
if len(os.Args) > 1 {
	if scfg, ok := parseServeArgs(os.Args[1:]); ok {
		os.Exit(runServe(scfg))
	}
	if scfg, ok := parseSetupArgs(os.Args[1:]); ok {
		os.Exit(runSetup(scfg))
	}
	if acfg, ok := parseAuditArgs(os.Args[1:]); ok {
		os.Exit(runAudit(acfg))
	}
}
```

**Step 4: Add `llm` import to main.go**

Add `"github.com/2389-research/mammoth/llm"` to the import block.

**Step 5: Update help.go**

Add to usage section:
```
mammoth audit [runID]             Audit a pipeline run
```

Add to Other section:
```
-verbose              Include full tool call details (audit)
```

Add to examples:
```
mammoth audit
mammoth audit --verbose ebbe59cd241c09df
```

**Step 6: Verify build**

Run: `go build ./...`
Expected: Clean build

**Step 7: Commit**

```bash
git add cmd/mammoth/main.go cmd/mammoth/help.go
git commit -m "feat(cmd): wire mammoth audit subcommand with LLM narrative"
```

---

### Task 5: Full verification

**Step 1: Run full test suite**

Run: `go test -count=1 ./...`
Expected: All packages pass

**Step 2: Build binary**

Run: `go build -o bin/mammoth ./cmd/mammoth`
Expected: Binary created

**Step 3: End-to-end test with real run**

Run from the kaya-bot/try7 directory (which has a failed run):
```bash
cd /Users/harper/workspace/2389/kaya-bot/try7
/Users/harper/Public/src/2389/mammoth-dev/bin/mammoth audit
```
Expected: LLM generates a narrative about the rate-limit retry loop failure.

**Step 4: Test verbose mode**

```bash
/Users/harper/Public/src/2389/mammoth-dev/bin/mammoth audit --verbose
```
Expected: Same narrative but with more detail about tool calls.

**Step 5: Test specific run ID**

```bash
/Users/harper/Public/src/2389/mammoth-dev/bin/mammoth audit ebbe59cd241c09df
```
Expected: Same narrative (only one run exists).

**Step 6: Test error cases**

```bash
# No runs directory
cd /tmp && /Users/harper/Public/src/2389/mammoth-dev/bin/mammoth audit
# Expected: "no runs found" error

# Bad run ID
cd /Users/harper/workspace/2389/kaya-bot/try7
/Users/harper/Public/src/2389/mammoth-dev/bin/mammoth audit nonexistent
# Expected: "could not load run" error
```
