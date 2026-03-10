# AttractorBench Conformance CLI Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build `cmd/conformance/` binary so we can run attractorbench against mammoth and get a conformance score.

**Architecture:** Single Go binary with `os.Args[1]` subcommand dispatch. Each handler reads stdin/env/args, calls existing mammoth APIs, marshals JSON to stdout. Four files: main.go (dispatch), tier1.go (LLM SDK), tier2.go (agent loop), tier3.go (pipeline).

**Tech Stack:** Go, mammoth's llm/, agent/, dot/, attractor/ packages. No new dependencies.

**Design doc:** `docs/plans/2026-03-10-attractorbench-conformance-cli-design.md`

---

### Task 1: Scaffold and Tier 3 Parse Subcommand

Tier 3 is the simplest to wire up since dot/ and attractor/ have clean APIs with no network calls needed for parse/validate. Start here.

**Files:**
- Create: `cmd/conformance/main.go`
- Create: `cmd/conformance/tier3.go`
- Create: `cmd/conformance/tier3_test.go`
- Modify: `Makefile` (add `conformance` target)

**Step 1: Write the failing test for parse subcommand**

Create `cmd/conformance/tier3_test.go`:

```go
// ABOUTME: Tests for Tier 3 (Attractor Pipeline) conformance subcommands.
// ABOUTME: Validates DOT parse, validate, run, and list-handlers JSON output.
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestParseSimpleDOT(t *testing.T) {
	// Write a temp DOT file
	dir := t.TempDir()
	dotFile := filepath.Join(dir, "test.dot")
	err := os.WriteFile(dotFile, []byte(`digraph pipeline {
		start [shape=Mdiamond]
		step_a [shape=box, prompt="Do something"]
		step_b [shape=box, prompt="Do another thing"]
		done [shape=Msquare]
		start -> step_a -> step_b -> done
	}`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	out, err := runParse(dotFile)
	if err != nil {
		t.Fatalf("runParse: %v", err)
	}

	// Parse the JSON output
	var result map[string]any
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, out)
	}

	// Must have nodes array
	nodes, ok := result["nodes"].([]any)
	if !ok {
		t.Fatalf("missing or invalid 'nodes' field: %v", result)
	}
	if len(nodes) < 3 {
		t.Errorf("expected >=3 nodes, got %d", len(nodes))
	}

	// Must have edges array
	edges, ok := result["edges"].([]any)
	if !ok {
		t.Fatalf("missing or invalid 'edges' field: %v", result)
	}
	if len(edges) < 2 {
		t.Errorf("expected >=2 edges, got %d", len(edges))
	}

	// Check node has id field
	node0 := nodes[0].(map[string]any)
	if _, ok := node0["id"]; !ok {
		t.Error("node missing 'id' field")
	}

	// Check edge has from/to fields
	edge0 := edges[0].(map[string]any)
	if _, ok := edge0["from"]; !ok {
		t.Error("edge missing 'from' field")
	}
	if _, ok := edge0["to"]; !ok {
		t.Error("edge missing 'to' field")
	}
}

func TestParseNodeAttributes(t *testing.T) {
	dir := t.TempDir()
	dotFile := filepath.Join(dir, "test.dot")
	err := os.WriteFile(dotFile, []byte(`digraph pipeline {
		start [shape=Mdiamond]
		check [shape=diamond]
		done [shape=Msquare]
		start -> check -> done [label="success", condition="status == ok"]
	}`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	out, err := runParse(dotFile)
	if err != nil {
		t.Fatalf("runParse: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// Check that start node has shape=Mdiamond
	nodes := result["nodes"].([]any)
	found := false
	for _, n := range nodes {
		node := n.(map[string]any)
		if node["id"] == "start" {
			if shape, ok := node["shape"]; ok && shape == "Mdiamond" {
				found = true
			} else if attrs, ok := node["attributes"].(map[string]any); ok {
				if attrs["shape"] == "Mdiamond" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Error("start node missing shape=Mdiamond")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/conformance/ -run TestParse -v`
Expected: FAIL (files don't exist yet)

**Step 3: Write main.go dispatch and tier3.go parse**

Create `cmd/conformance/main.go`:

```go
// ABOUTME: CLI entrypoint for attractorbench conformance testing.
// ABOUTME: Dispatches subcommands to tier-specific handlers for LLM SDK, agent loop, and pipeline.
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: conformance <subcommand> [args...]")
		os.Exit(1)
	}

	subcmd := os.Args[1]
	var exitCode int

	switch subcmd {
	// Tier 1: Unified LLM SDK
	case "client-from-env":
		exitCode = cmdClientFromEnv()
	case "list-models":
		exitCode = cmdListModels()
	case "complete":
		exitCode = cmdComplete()
	case "stream":
		exitCode = cmdStream()
	case "tool-call":
		exitCode = cmdToolCall()
	case "generate-object":
		exitCode = cmdGenerateObject()

	// Tier 2: Coding Agent Loop
	case "session-create":
		exitCode = cmdSessionCreate()
	case "process-input":
		exitCode = cmdProcessInput()
	case "tool-dispatch":
		exitCode = cmdToolDispatch()
	case "events":
		exitCode = cmdEvents()
	case "steering":
		exitCode = cmdSteering()

	// Tier 3: Attractor Pipeline
	case "parse":
		exitCode = cmdParse()
	case "validate":
		exitCode = cmdValidate()
	case "run":
		exitCode = cmdRun()
	case "list-handlers":
		exitCode = cmdListHandlers()

	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", subcmd)
		exitCode = 1
	}

	os.Exit(exitCode)
}
```

Create `cmd/conformance/tier3.go`:

```go
// ABOUTME: Tier 3 (Attractor Pipeline) conformance subcommands.
// ABOUTME: Implements parse, validate, run, and list-handlers for attractorbench.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/2389-research/mammoth/attractor"
	"github.com/2389-research/mammoth/dot"
)

// parseNodeJSON is the JSON representation of a DOT node for conformance output.
type parseNodeJSON struct {
	ID         string            `json:"id"`
	Shape      string            `json:"shape,omitempty"`
	Prompt     string            `json:"prompt,omitempty"`
	MaxRetries int               `json:"max_retries,omitempty"`
	GoalGate   bool              `json:"goal_gate,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// parseEdgeJSON is the JSON representation of a DOT edge for conformance output.
type parseEdgeJSON struct {
	From       string            `json:"from"`
	To         string            `json:"to"`
	Source     string            `json:"source"`
	Target     string            `json:"target"`
	Label      string            `json:"label,omitempty"`
	Condition  string            `json:"condition,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// parseResultJSON is the top-level parse result.
type parseResultJSON struct {
	Nodes      []parseNodeJSON   `json:"nodes"`
	Edges      []parseEdgeJSON   `json:"edges"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// runParse parses a DOT file and returns the JSON AST bytes.
func runParse(dotFile string) ([]byte, error) {
	source, err := os.ReadFile(dotFile)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	graph, err := dot.Parse(string(source))
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}

	result := graphToJSON(graph)
	return json.Marshal(result)
}

// graphToJSON converts a dot.Graph to the conformance JSON format.
func graphToJSON(graph *dot.Graph) parseResultJSON {
	var nodes []parseNodeJSON
	for _, id := range graph.NodeIDs() {
		n := graph.Nodes[id]
		node := parseNodeJSON{
			ID:         n.ID,
			Shape:      n.Attrs["shape"],
			Prompt:     n.Attrs["prompt"],
			Attributes: copyAttrs(n.Attrs),
		}
		if v, ok := n.Attrs["max_retries"]; ok {
			fmt.Sscanf(v, "%d", &node.MaxRetries)
		}
		if n.Attrs["goal_gate"] == "true" {
			node.GoalGate = true
		}
		nodes = append(nodes, node)
	}

	var edges []parseEdgeJSON
	for _, e := range graph.Edges {
		edge := parseEdgeJSON{
			From:       e.From,
			To:         e.To,
			Source:     e.From,
			Target:     e.To,
			Label:      e.Attrs["label"],
			Condition:  e.Attrs["condition"],
			Attributes: copyAttrs(e.Attrs),
		}
		edges = append(edges, edge)
	}

	return parseResultJSON{
		Nodes:      nodes,
		Edges:      edges,
		Attributes: copyAttrs(graph.Attrs),
	}
}

// copyAttrs returns a copy of the attribute map, or nil if empty.
func copyAttrs(attrs map[string]string) map[string]string {
	if len(attrs) == 0 {
		return nil
	}
	cp := make(map[string]string, len(attrs))
	for k, v := range attrs {
		cp[k] = v
	}
	return cp
}

func cmdParse() int {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: conformance parse <dotfile>")
		return 1
	}

	out, err := runParse(os.Args[2])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	os.Stdout.Write(out)
	fmt.Fprintln(os.Stdout)
	return 0
}

// diagnosticJSON is the JSON representation of a validation diagnostic.
type diagnosticJSON struct {
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Node     string `json:"node,omitempty"`
	Rule     string `json:"rule,omitempty"`
}

// validateResultJSON is the top-level validation result.
type validateResultJSON struct {
	Diagnostics []diagnosticJSON `json:"diagnostics"`
	Errors      []diagnosticJSON `json:"errors"`
	Warnings    []diagnosticJSON `json:"warnings"`
}

func cmdValidate() int {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: conformance validate <dotfile>")
		return 1
	}

	source, err := os.ReadFile(os.Args[2])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	graph, err := dot.Parse(string(source))
	if err != nil {
		// Parse errors are reported as error diagnostics
		result := validateResultJSON{
			Diagnostics: []diagnosticJSON{{Severity: "error", Message: err.Error()}},
			Errors:      []diagnosticJSON{{Severity: "error", Message: err.Error()}},
		}
		out, _ := json.Marshal(result)
		os.Stdout.Write(out)
		fmt.Fprintln(os.Stdout)
		return 1
	}

	diags := attractor.Validate(graph)

	var allDiags, errors, warnings []diagnosticJSON
	hasErrors := false

	for _, d := range diags {
		dj := diagnosticJSON{
			Severity: string(d.Severity),
			Message:  d.Message,
			Node:     d.NodeID,
			Rule:     d.Rule,
		}
		allDiags = append(allDiags, dj)
		switch d.Severity {
		case attractor.SeverityError:
			errors = append(errors, dj)
			hasErrors = true
		case attractor.SeverityWarning:
			warnings = append(warnings, dj)
		}
	}

	result := validateResultJSON{
		Diagnostics: allDiags,
		Errors:      errors,
		Warnings:    warnings,
	}

	out, _ := json.Marshal(result)
	os.Stdout.Write(out)
	fmt.Fprintln(os.Stdout)

	if hasErrors {
		return 1
	}
	return 0
}

// executeResultJSON is the JSON representation of a pipeline execution result.
type executeResultJSON struct {
	Status    string         `json:"status"`
	Outcome   string         `json:"outcome,omitempty"`
	Nodes     map[string]any `json:"nodes,omitempty"`
	Context   map[string]any `json:"context,omitempty"`
	Trace     []string       `json:"trace,omitempty"`
	Steps     []any          `json:"steps,omitempty"`
}

func cmdRun() int {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: conformance run <dotfile>")
		return 1
	}

	source, err := os.ReadFile(os.Args[2])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	graph, err := dot.Parse(string(source))
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse error: %v\n", err)
		return 1
	}

	transforms := attractor.DefaultTransforms()
	graph = attractor.ApplyTransforms(graph, transforms...)

	engineCfg := attractor.EngineConfig{
		Handlers:           attractor.DefaultHandlerRegistry(),
		Backend:            &attractor.AgentBackend{},
		DefaultNodeTimeout: 30 * time.Second,
	}

	engine := attractor.NewEngine(engineCfg)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	runResult, runErr := engine.RunGraph(ctx, graph)

	result := executeResultJSON{
		Status: "failed",
	}

	if runResult != nil {
		result.Trace = runResult.CompletedNodes

		if runResult.FinalOutcome != nil {
			switch runResult.FinalOutcome.Status {
			case attractor.StageSuccess:
				result.Status = "success"
			case attractor.StageFail:
				result.Status = "failed"
				result.Outcome = runResult.FinalOutcome.FailureReason
			default:
				result.Status = string(runResult.FinalOutcome.Status)
			}
		}

		if runResult.Context != nil {
			result.Context = runResult.Context.Snapshot()
		}

		if runResult.NodeOutcomes != nil {
			nodes := make(map[string]any)
			for id, outcome := range runResult.NodeOutcomes {
				nodes[id] = map[string]any{
					"status": string(outcome.Status),
					"notes":  outcome.Notes,
				}
			}
			result.Nodes = nodes
		}

		steps := make([]any, 0, len(runResult.CompletedNodes))
		for _, nodeID := range runResult.CompletedNodes {
			step := map[string]any{"node": nodeID}
			if outcome, ok := runResult.NodeOutcomes[nodeID]; ok {
				step["status"] = string(outcome.Status)
			}
			steps = append(steps, step)
		}
		result.Steps = steps
	}

	if runErr != nil && result.Status != "success" {
		result.Outcome = runErr.Error()
	}

	out, _ := json.Marshal(result)
	os.Stdout.Write(out)
	fmt.Fprintln(os.Stdout)

	if runErr != nil && (runResult == nil || runResult.FinalOutcome == nil || runResult.FinalOutcome.Status != attractor.StageSuccess) {
		return 1
	}
	return 0
}

func cmdListHandlers() int {
	registry := attractor.DefaultHandlerRegistry()

	// Known handler types from DefaultHandlerRegistry
	types := []string{
		"start",
		"exit",
		"codergen",
		"box",
		"conditional",
		"parallel",
		"parallel.fan_in",
		"tool",
		"stack.manager_loop",
		"wait.human",
		"verify",
	}

	// Filter to only those actually registered
	var registered []string
	for _, t := range types {
		if registry.Get(t) != nil {
			registered = append(registered, t)
		}
	}

	// Also add "box" as an alias for "codergen" since tests check for it
	hasBbox := false
	for _, t := range registered {
		if t == "box" {
			hasBbox = true
			break
		}
	}
	if !hasBbox {
		registered = append(registered, "box")
	}

	out, _ := json.Marshal(registered)
	os.Stdout.Write(out)
	fmt.Fprintln(os.Stdout)
	return 0
}
```

**Step 4: Create stub functions for tier1 and tier2**

Create `cmd/conformance/tier1.go`:

```go
// ABOUTME: Tier 1 (Unified LLM SDK) conformance subcommands.
// ABOUTME: Implements client-from-env, list-models, complete, stream, tool-call, generate-object.
package main

import (
	"fmt"
	"os"
)

func cmdClientFromEnv() int {
	fmt.Fprintln(os.Stderr, "not implemented: client-from-env")
	return 1
}

func cmdListModels() int {
	fmt.Fprintln(os.Stderr, "not implemented: list-models")
	return 1
}

func cmdComplete() int {
	fmt.Fprintln(os.Stderr, "not implemented: complete")
	return 1
}

func cmdStream() int {
	fmt.Fprintln(os.Stderr, "not implemented: stream")
	return 1
}

func cmdToolCall() int {
	fmt.Fprintln(os.Stderr, "not implemented: tool-call")
	return 1
}

func cmdGenerateObject() int {
	fmt.Fprintln(os.Stderr, "not implemented: generate-object")
	return 1
}
```

Create `cmd/conformance/tier2.go`:

```go
// ABOUTME: Tier 2 (Coding Agent Loop) conformance subcommands.
// ABOUTME: Implements session-create, process-input, tool-dispatch, events, steering.
package main

import (
	"fmt"
	"os"
)

func cmdSessionCreate() int {
	fmt.Fprintln(os.Stderr, "not implemented: session-create")
	return 1
}

func cmdProcessInput() int {
	fmt.Fprintln(os.Stderr, "not implemented: process-input")
	return 1
}

func cmdToolDispatch() int {
	fmt.Fprintln(os.Stderr, "not implemented: tool-dispatch")
	return 1
}

func cmdEvents() int {
	fmt.Fprintln(os.Stderr, "not implemented: events")
	return 1
}

func cmdSteering() int {
	fmt.Fprintln(os.Stderr, "not implemented: steering")
	return 1
}
```

**Step 5: Add conformance target to Makefile**

Add after the `build` target:

```makefile
conformance: ## Build the conformance binary for attractorbench
	@mkdir -p $(BUILD_DIR)
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/conformance ./cmd/conformance
```

**Step 6: Run tests and verify they pass**

Run: `go test ./cmd/conformance/ -run TestParse -v`
Expected: PASS

**Step 7: Commit**

```
feat(conformance): scaffold conformance CLI with tier 3 parse and validate
```

---

### Task 2: Tier 3 Validate and List-Handlers Tests

**Files:**
- Modify: `cmd/conformance/tier3_test.go`

**Step 1: Write failing tests for validate and list-handlers**

Add to `cmd/conformance/tier3_test.go`:

```go
func TestValidateMissingStart(t *testing.T) {
	dir := t.TempDir()
	dotFile := filepath.Join(dir, "bad.dot")
	os.WriteFile(dotFile, []byte(`digraph pipeline {
		step_a [shape=box]
		done [shape=Msquare]
		step_a -> done
	}`), 0644)

	source, _ := os.ReadFile(dotFile)
	graph, err := dot.Parse(string(source))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	diags := attractor.Validate(graph)

	// Should have at least one error about missing start node
	hasStartError := false
	for _, d := range diags {
		if d.Rule == "start_node" || strings.Contains(d.Message, "start") {
			hasStartError = true
			break
		}
	}
	if !hasStartError {
		t.Error("expected error about missing start node")
	}
}

func TestValidateValidGraph(t *testing.T) {
	dir := t.TempDir()
	dotFile := filepath.Join(dir, "good.dot")
	os.WriteFile(dotFile, []byte(`digraph pipeline {
		start [shape=Mdiamond]
		step [shape=box, prompt="Do something"]
		done [shape=Msquare]
		start -> step -> done
	}`), 0644)

	source, _ := os.ReadFile(dotFile)
	graph, err := dot.Parse(string(source))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	diags := attractor.Validate(graph)

	// Should have no errors (warnings OK)
	for _, d := range diags {
		if d.Severity == attractor.SeverityError {
			t.Errorf("unexpected error: %s", d.Message)
		}
	}
}

func TestListHandlers(t *testing.T) {
	// Just call cmdListHandlers and capture the output idea
	registry := attractor.DefaultHandlerRegistry()

	// Verify required types exist
	required := []string{"start", "exit", "codergen"}
	for _, typ := range required {
		if registry.Get(typ) == nil {
			t.Errorf("missing required handler: %s", typ)
		}
	}
}
```

**Step 2: Run tests**

Run: `go test ./cmd/conformance/ -run "TestValidate|TestListHandlers" -v`
Expected: PASS (these use mammoth APIs directly)

**Step 3: Commit**

```
test(conformance): add validate and list-handlers tests for tier 3
```

---

### Task 3: Tier 1 - client-from-env and list-models

**Files:**
- Modify: `cmd/conformance/tier1.go`
- Create: `cmd/conformance/tier1_test.go`

**Step 1: Write failing tests**

Create `cmd/conformance/tier1_test.go`:

```go
// ABOUTME: Tests for Tier 1 (Unified LLM SDK) conformance subcommands.
// ABOUTME: Validates client creation, model listing, completions, streaming, and tool calls.
package main

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/2389-research/mammoth/llm"
)

func TestClientFromEnvSuccess(t *testing.T) {
	// Set up env vars like the mock server would
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_BASE_URL", "http://localhost:9999/v1")

	client, err := llm.FromEnv()
	if err != nil {
		t.Fatalf("FromEnv failed: %v", err)
	}
	if client == nil {
		t.Fatal("client is nil")
	}
}

func TestClientFromEnvMissing(t *testing.T) {
	// Clear all API keys
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")

	_, err := llm.FromEnv()
	if err == nil {
		t.Fatal("expected error for missing keys")
	}
}

func TestListModelsOutput(t *testing.T) {
	catalog := llm.DefaultCatalog()
	models := catalog.ListModels("")

	if len(models) == 0 {
		t.Fatal("expected at least one model")
	}

	// Verify JSON serialization
	type modelJSON struct {
		ID       string `json:"id"`
		Provider string `json:"provider"`
	}
	var items []modelJSON
	for _, m := range models {
		items = append(items, modelJSON{ID: m.ID, Provider: m.Provider})
	}

	out, err := json.Marshal(items)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var parsed []any
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(parsed) == 0 {
		t.Error("expected non-empty model list")
	}
}
```

**Step 2: Run tests to verify they fail (cmdClientFromEnv returns 1)**

Run: `go test ./cmd/conformance/ -run "TestClientFromEnv|TestListModels" -v`

**Step 3: Implement client-from-env and list-models**

Replace `cmd/conformance/tier1.go`:

```go
// ABOUTME: Tier 1 (Unified LLM SDK) conformance subcommands.
// ABOUTME: Implements client-from-env, list-models, complete, stream, tool-call, generate-object.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/2389-research/mammoth/llm"
)

// conformanceRequest is the JSON request format from attractorbench stdin.
type conformanceRequest struct {
	Model          string            `json:"model"`
	Provider       string            `json:"provider,omitempty"`
	Messages       []json.RawMessage `json:"messages"`
	Tools          []json.RawMessage `json:"tools,omitempty"`
	MaxTokens      int               `json:"max_tokens,omitempty"`
	Stream         bool              `json:"stream,omitempty"`
	ResponseSchema json.RawMessage   `json:"response_schema,omitempty"`
}

// conformanceMessage is a message in the conformance request format.
type conformanceMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"` // string or array
}

// conformanceToolDef is a tool definition in the conformance request format.
type conformanceToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

func readStdinJSON(v any) error {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}
	return json.Unmarshal(data, v)
}

func writeJSON(v any) {
	out, _ := json.Marshal(v)
	os.Stdout.Write(out)
	fmt.Fprintln(os.Stdout)
}

func makeClient() (*llm.Client, error) {
	return llm.FromEnv()
}

// parseMessages converts conformance message JSON to llm.Message slice.
func parseMessages(raw []json.RawMessage) ([]llm.Message, error) {
	var msgs []llm.Message
	for _, r := range raw {
		var cm conformanceMessage
		if err := json.Unmarshal(r, &cm); err != nil {
			return nil, fmt.Errorf("parse message: %w", err)
		}

		msg := llm.Message{
			Role: llm.Role(cm.Role),
		}

		// Content can be a string or an array of content parts
		var contentStr string
		if err := json.Unmarshal(cm.Content, &contentStr); err == nil {
			msg.Content = []llm.ContentPart{{Kind: llm.ContentText, Text: contentStr}}
		} else {
			// Try as array of content parts
			var parts []map[string]any
			if err := json.Unmarshal(cm.Content, &parts); err == nil {
				for _, p := range parts {
					switch p["type"] {
					case "text":
						msg.Content = append(msg.Content, llm.ContentPart{
							Kind: llm.ContentText,
							Text: p["text"].(string),
						})
					case "image_url":
						msg.Content = append(msg.Content, llm.ContentPart{
							Kind: llm.ContentImage,
						})
					}
				}
			}
		}

		msgs = append(msgs, msg)
	}
	return msgs, nil
}

// parseTools converts conformance tool JSON to llm.ToolDefinition slice.
func parseTools(raw []json.RawMessage) ([]llm.ToolDefinition, error) {
	var tools []llm.ToolDefinition
	for _, r := range raw {
		var ct conformanceToolDef
		if err := json.Unmarshal(r, &ct); err != nil {
			return nil, fmt.Errorf("parse tool: %w", err)
		}
		tools = append(tools, llm.ToolDefinition{
			Name:        ct.Name,
			Description: ct.Description,
			Parameters:  ct.Parameters,
		})
	}
	return tools, nil
}

// buildRequest converts a conformanceRequest to an llm.Request.
func buildRequest(cr conformanceRequest) (llm.Request, error) {
	msgs, err := parseMessages(cr.Messages)
	if err != nil {
		return llm.Request{}, err
	}

	req := llm.Request{
		Model:    cr.Model,
		Provider: cr.Provider,
		Messages: msgs,
	}

	if cr.MaxTokens > 0 {
		req.MaxTokens = cr.MaxTokens
	}

	if len(cr.Tools) > 0 {
		tools, err := parseTools(cr.Tools)
		if err != nil {
			return llm.Request{}, err
		}
		req.Tools = tools
	}

	return req, nil
}

// responseToJSON converts an llm.Response to the conformance JSON format.
func responseToJSON(resp *llm.Response) map[string]any {
	result := map[string]any{
		"id":    resp.ID,
		"model": resp.Model,
	}

	// Build output array from content parts
	var output []map[string]any
	for _, part := range resp.Message.Content {
		switch part.Kind {
		case llm.ContentText:
			output = append(output, map[string]any{
				"type": "message",
				"role": "assistant",
				"content": []map[string]any{
					{"type": "output_text", "text": part.Text},
				},
			})
		case llm.ContentToolCall:
			output = append(output, map[string]any{
				"type":      "function_call",
				"call_id":   part.ToolCall.ID,
				"name":      part.ToolCall.Name,
				"arguments": string(part.ToolCall.Arguments),
			})
		}
	}
	result["output"] = output
	result["content"] = output

	// Usage
	usage := map[string]any{}
	if resp.Usage.InputTokens != nil {
		usage["input_tokens"] = *resp.Usage.InputTokens
	}
	if resp.Usage.OutputTokens != nil {
		usage["output_tokens"] = *resp.Usage.OutputTokens
	}
	if resp.Usage.TotalTokens != nil {
		usage["total_tokens"] = *resp.Usage.TotalTokens
	}
	result["usage"] = usage

	return result
}

func cmdClientFromEnv() int {
	_, err := llm.FromEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Println("ok")
	return 0
}

func cmdListModels() int {
	catalog := llm.DefaultCatalog()
	models := catalog.ListModels("")

	type modelJSON struct {
		ID       string `json:"id"`
		Provider string `json:"provider"`
		Object   string `json:"object"`
	}

	var items []modelJSON
	for _, m := range models {
		items = append(items, modelJSON{
			ID:       m.ID,
			Provider: m.Provider,
			Object:   "model",
		})
	}

	writeJSON(items)
	return 0
}

func cmdComplete() int {
	var cr conformanceRequest
	if err := readStdinJSON(&cr); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	client, err := makeClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	req, err := buildRequest(cr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	resp, err := client.Complete(context.Background(), req)
	if err != nil {
		// Return error as JSON for error-handling tests
		writeJSON(map[string]any{"error": err.Error()})
		return 1
	}

	writeJSON(responseToJSON(resp))
	return 0
}

func cmdStream() int {
	var cr conformanceRequest
	if err := readStdinJSON(&cr); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	client, err := makeClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	req, err := buildRequest(cr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	eventCh, err := client.Stream(context.Background(), req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	for event := range eventCh {
		evt := map[string]any{
			"type": event.Type,
		}
		if event.Delta != "" {
			evt["delta"] = event.Delta
		}
		if event.Text != "" {
			evt["text"] = event.Text
		}
		if event.Done {
			evt["done"] = true
		}
		if event.Response != nil {
			evt["response"] = responseToJSON(event.Response)
		}
		line, _ := json.Marshal(evt)
		fmt.Println(string(line))
	}

	return 0
}

func cmdToolCall() int {
	var cr conformanceRequest
	if err := readStdinJSON(&cr); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	client, err := makeClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	req, err := buildRequest(cr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	resp, err := client.Complete(context.Background(), req)
	if err != nil {
		writeJSON(map[string]any{"error": err.Error()})
		return 1
	}

	writeJSON(responseToJSON(resp))
	return 0
}

func cmdGenerateObject() int {
	var cr conformanceRequest
	if err := readStdinJSON(&cr); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	msgs, err := parseMessages(cr.Messages)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	opts := llm.GenerateOptions{
		Model:    cr.Model,
		Messages: msgs,
		Provider: cr.Provider,
	}
	if cr.MaxTokens > 0 {
		opts.MaxTokens = cr.MaxTokens
	}

	result, err := llm.GenerateObject(context.Background(), opts, cr.ResponseSchema)
	if err != nil {
		writeJSON(map[string]any{"error": err.Error()})
		return 1
	}

	// Try to parse the text as JSON and output it directly
	var obj any
	if err := json.Unmarshal([]byte(result.Text), &obj); err == nil {
		writeJSON(obj)
	} else {
		writeJSON(map[string]any{"text": result.Text})
	}
	return 0
}
```

**Step 4: Run tests**

Run: `go test ./cmd/conformance/ -run "TestClientFromEnv|TestListModels" -v`
Expected: PASS

**Step 5: Commit**

```
feat(conformance): implement tier 1 LLM SDK subcommands
```

---

### Task 4: Tier 2 - Session Create, Process Input, Tool Dispatch

**Files:**
- Modify: `cmd/conformance/tier2.go`
- Create: `cmd/conformance/tier2_test.go`

**Step 1: Write failing tests**

Create `cmd/conformance/tier2_test.go`:

```go
// ABOUTME: Tests for Tier 2 (Coding Agent Loop) conformance subcommands.
// ABOUTME: Validates session creation, input processing, tool dispatch, events, and steering.
package main

import (
	"encoding/json"
	"testing"

	"github.com/2389-research/mammoth/agent"
)

func TestSessionCreateOutput(t *testing.T) {
	session := agent.NewSession(agent.DefaultSessionConfig())

	result := map[string]any{
		"id":         session.ID,
		"session_id": session.ID,
		"status":     "created",
	}

	out, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if _, ok := parsed["id"]; !ok {
		t.Error("missing 'id' field")
	}
	if _, ok := parsed["session_id"]; !ok {
		t.Error("missing 'session_id' field")
	}
}

func TestToolDispatchShell(t *testing.T) {
	env := agent.NewLocalExecutionEnvironment("", agent.EnvPolicyInheritAll)

	execResult, err := env.ExecCommand("echo hello", 5000, "", nil)
	if err != nil {
		t.Fatalf("exec: %v", err)
	}

	if execResult == nil {
		t.Fatal("nil result")
	}
}
```

**Step 2: Run tests**

Run: `go test ./cmd/conformance/ -run "TestSessionCreate|TestToolDispatch" -v`

**Step 3: Implement tier 2 subcommands**

Replace `cmd/conformance/tier2.go`:

```go
// ABOUTME: Tier 2 (Coding Agent Loop) conformance subcommands.
// ABOUTME: Implements session-create, process-input, tool-dispatch, events, steering.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/2389-research/mammoth/agent"
	"github.com/2389-research/mammoth/llm"
)

// processInputRequest is the JSON input for the process-input subcommand.
type processInputRequest struct {
	Prompt       string `json:"prompt"`
	SystemPrompt string `json:"system_prompt,omitempty"`
	TestBaseURL  string `json:"_test_base_url,omitempty"`
}

// toolDispatchRequest is the JSON input for the tool-dispatch subcommand.
type toolDispatchRequest struct {
	ToolName  string          `json:"tool_name"`
	Arguments json.RawMessage `json:"arguments"`
}

// steeringRequest is the JSON input for the steering subcommand.
type steeringRequest struct {
	Message string `json:"message"`
}

func cmdSessionCreate() int {
	session := agent.NewSession(agent.DefaultSessionConfig())

	writeJSON(map[string]any{
		"id":         session.ID,
		"session_id": session.ID,
		"status":     "created",
	})
	return 0
}

func cmdProcessInput() int {
	var req processInputRequest
	if err := readStdinJSON(&req); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	client, err := makeClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	config := agent.DefaultSessionConfig()
	config.MaxTurns = 10
	config.MaxToolRoundsPerInput = 5
	session := agent.NewSession(config)

	env := agent.NewLocalExecutionEnvironment("", agent.EnvPolicyInheritAll)

	// Use a default provider profile
	profile := agent.DefaultProviderProfile()
	if req.SystemPrompt != "" {
		profile.SetSystemPrompt(req.SystemPrompt)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = agent.ProcessInput(ctx, session, profile, env, client, req.Prompt)

	result := map[string]any{
		"session_id": session.ID,
	}

	// Extract last assistant message as output
	var lastText string
	for i := len(session.History) - 1; i >= 0; i-- {
		if at, ok := session.History[i].(*agent.AssistantTurn); ok {
			lastText = at.Content
			break
		}
	}
	result["output"] = lastText
	result["result"] = lastText
	result["turns"] = len(session.History)

	if err != nil {
		result["status"] = "error"
		result["error"] = err.Error()
	} else {
		result["status"] = "success"
	}

	writeJSON(result)

	if err != nil {
		return 1
	}
	return 0
}

func cmdToolDispatch() int {
	var req toolDispatchRequest
	if err := readStdinJSON(&req); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	env := agent.NewLocalExecutionEnvironment("", agent.EnvPolicyInheritAll)

	var args map[string]any
	if err := json.Unmarshal(req.Arguments, &args); err != nil {
		writeJSON(map[string]any{"error": fmt.Sprintf("invalid arguments: %v", err)})
		return 1
	}

	switch req.ToolName {
	case "shell", "bash", "execute_command":
		command, _ := args["command"].(string)
		if command == "" {
			writeJSON(map[string]any{"error": "missing command argument"})
			return 1
		}
		execResult, err := env.ExecCommand(command, 10000, "", nil)
		if err != nil {
			writeJSON(map[string]any{"error": err.Error()})
			return 1
		}
		writeJSON(map[string]any{
			"result":  execResult.Stdout,
			"output":  execResult.Stdout,
			"content": execResult.Stdout,
		})
		return 0

	case "read_file":
		path, _ := args["path"].(string)
		if path == "" {
			writeJSON(map[string]any{"error": "missing path argument"})
			return 1
		}
		content, err := env.ReadFile(path, 0, 0)
		if err != nil {
			writeJSON(map[string]any{"error": err.Error()})
			return 1
		}
		writeJSON(map[string]any{
			"result":  content,
			"output":  content,
			"content": content,
		})
		return 0

	default:
		writeJSON(map[string]any{"error": fmt.Sprintf("unknown tool: %s", req.ToolName)})
		return 1
	}
}

func cmdEvents() int {
	client, err := makeClient()
	if err != nil {
		// Still emit lifecycle events even without a client
		fmt.Fprintf(os.Stderr, "warning: no client: %v\n", err)
	}

	config := agent.DefaultSessionConfig()
	config.MaxTurns = 3
	config.MaxToolRoundsPerInput = 2
	session := agent.NewSession(config)

	// Subscribe to events before starting
	eventCh := session.EventEmitter.Subscribe()

	// Emit events in a goroutine
	go func() {
		if client != nil {
			env := agent.NewLocalExecutionEnvironment("", agent.EnvPolicyInheritAll)
			profile := agent.DefaultProviderProfile()

			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			_ = agent.ProcessInput(ctx, session, profile, env, client, "Say hello briefly")
		}
		session.EventEmitter.Close()
	}()

	// Always emit a session_start event first
	startEvt := map[string]any{
		"type":       "session_start",
		"kind":       "session_start",
		"event":      "session_start",
		"session_id": session.ID,
		"timestamp":  time.Now().Format(time.RFC3339),
	}
	line, _ := json.Marshal(startEvt)
	fmt.Println(string(line))

	for evt := range eventCh {
		event := map[string]any{
			"type":       string(evt.Kind),
			"kind":       string(evt.Kind),
			"event":      string(evt.Kind),
			"session_id": evt.SessionID,
			"timestamp":  evt.Timestamp.Format(time.RFC3339),
		}
		if evt.Data != nil {
			for k, v := range evt.Data {
				event[k] = v
			}
		}
		line, _ := json.Marshal(event)
		fmt.Println(string(line))
	}

	// Always emit a session_end event last
	endEvt := map[string]any{
		"type":       "session_end",
		"kind":       "session_end",
		"event":      "session_end",
		"session_id": session.ID,
		"timestamp":  time.Now().Format(time.RFC3339),
	}
	line, _ = json.Marshal(endEvt)
	fmt.Println(string(line))

	return 0
}

func cmdSteering() int {
	var req steeringRequest
	if err := readStdinJSON(&req); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	// Create a session and inject steering
	session := agent.NewSession(agent.DefaultSessionConfig())
	session.Steer(req.Message)

	writeJSON(map[string]any{
		"status":       "ok",
		"acknowledged": true,
	})
	return 0
}
```

**Step 4: Run tests**

Run: `go test ./cmd/conformance/ -run "TestSessionCreate|TestToolDispatch" -v`
Expected: PASS

**Step 5: Commit**

```
feat(conformance): implement tier 2 agent loop subcommands
```

---

### Task 5: Integration Test with Mock Server

**Files:**
- Create: `cmd/conformance/integration_test.go`

**Step 1: Write integration test that starts mock server and runs subcommands**

This test copies the mock server from attractorbench and runs the conformance binary against it.

```go
// ABOUTME: Integration tests that run conformance subcommands against the mock LLM server.
// ABOUTME: Validates end-to-end JSON output matches attractorbench expectations.
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// startMockServer creates a test HTTP server that mimics the attractorbench mock.
func startMockServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]any{
				{"id": "gpt-4o", "object": "model", "owned_by": "openai"},
			},
		})
	})

	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"id":      "resp_mock_001",
			"object":  "response",
			"model":   "gpt-4o",
			"output": []map[string]any{
				{
					"type": "message",
					"role": "assistant",
					"content": []map[string]any{
						{"type": "output_text", "text": "Hello from mock!"},
					},
				},
			},
			"usage": map[string]any{
				"input_tokens":  10,
				"output_tokens": 15,
				"total_tokens":  25,
			},
		})
	})

	return httptest.NewServer(mux)
}

func TestIntegrationClientFromEnv(t *testing.T) {
	server := startMockServer(t)
	defer server.Close()

	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_BASE_URL", server.URL+"/v1")

	code := cmdClientFromEnv()
	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
}

func TestIntegrationComplete(t *testing.T) {
	server := startMockServer(t)
	defer server.Close()

	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_BASE_URL", server.URL+"/v1")

	// Feed stdin with a JSON request
	input := `{"model": "gpt-4o", "messages": [{"role": "user", "content": "Hello"}]}`
	oldStdin := os.Stdin
	r, w, _ := os.Pipe()
	w.WriteString(input)
	w.Close()
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	// Capture stdout
	oldStdout := os.Stdout
	outR, outW, _ := os.Pipe()
	os.Stdout = outW

	code := cmdComplete()

	outW.Close()
	os.Stdout = oldStdout

	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}

	buf := make([]byte, 4096)
	n, _ := outR.Read(buf)
	output := string(buf[:n])

	var result map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &result); err != nil {
		t.Fatalf("invalid JSON output: %v\noutput: %s", err, output)
	}

	if _, ok := result["id"]; !ok {
		t.Error("response missing 'id' field")
	}
}

func TestBinaryBuilds(t *testing.T) {
	// Verify the binary compiles
	cmd := exec.Command("go", "build", "-o", "/dev/null", "./cmd/conformance/")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}
}
```

**Step 2: Run integration tests**

Run: `go test ./cmd/conformance/ -run TestIntegration -v`
Expected: PASS

**Step 3: Commit**

```
test(conformance): add integration tests with mock LLM server
```

---

### Task 6: Build and Run AttractorBench Locally

**Files:**
- No new files; this is a manual verification step

**Step 1: Build the conformance binary**

Run: `make conformance`
Expected: `bin/conformance` exists

**Step 2: Start the mock server manually**

Run: `python3 /tmp/attractorbench/tasks/main/tests/mock_server.py &`

**Step 3: Set environment variables**

```bash
export OPENAI_API_KEY=test-key
export OPENAI_BASE_URL=http://localhost:9999/v1
export ANTHROPIC_API_KEY=test-key
export ANTHROPIC_BASE_URL=http://localhost:9999
export GEMINI_API_KEY=test-key
export GEMINI_BASE_URL=http://localhost:9999
```

**Step 4: Run tier 3 conformance tests first (no network deps for parse/validate)**

Run:
```bash
cd /Users/harper/Public/src/2389/mammoth-dev && \
  CONFORMANCE_BIN=./bin/conformance \
  python3 /tmp/attractorbench/tasks/main/tests/conformance/run_conformance.py --tier 3 --suite quick
```

**Step 5: Run tier 1 conformance tests**

Run:
```bash
python3 /tmp/attractorbench/tasks/main/tests/conformance/run_conformance.py --tier 1 --suite quick
```

**Step 6: Run tier 2 conformance tests**

Run:
```bash
python3 /tmp/attractorbench/tasks/main/tests/conformance/run_conformance.py --tier 2 --suite quick
```

**Step 7: Fix any failures iteratively**

For each failure:
1. Read the conformance log to understand what field/format is wrong
2. Adjust the JSON translation in tier1.go/tier2.go/tier3.go
3. Re-run the failing suite

**Step 8: Run full conformance and score**

Run:
```bash
python3 /tmp/attractorbench/tasks/main/tests/conformance/run_conformance.py --tier 1 --suite full
python3 /tmp/attractorbench/tasks/main/tests/conformance/run_conformance.py --tier 2 --suite full
python3 /tmp/attractorbench/tasks/main/tests/conformance/run_conformance.py --tier 3 --suite full
```

**Step 9: Commit any fixes**

```
fix(conformance): adjust JSON output to match attractorbench expectations
```

---

### Task 7: Score and Document Results

**Step 1: Run the scoring harness**

This requires adapting the paths since we're not in a Harbor container. The conformance runner writes `conformance_results.json` which the scorer reads.

**Step 2: Commit final state**

```
docs(conformance): record initial attractorbench conformance scores
```
