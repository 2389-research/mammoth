# AttractorBench Conformance CLI Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a conformance CLI binary (`cmd/mammoth-conformance/`) that wraps mammoth's attractor engine to pass AttractorBench Tier 3 conformance tests.

**Architecture:** Thin CLI binary with 4 subcommands (parse, validate, run, list-handlers). Each subcommand calls existing mammoth engine APIs and translates results to AttractorBench's expected JSON schemas. All translation types live in the binary — no conformance code leaks into core packages.

**Tech Stack:** Go, mammoth `dot/` and `attractor/` packages, standard `encoding/json` and `os` libraries.

**Design doc:** `docs/plans/2026-03-05-attractorbench-conformance-design.md`

---

### Task 1: Conformance JSON Types

**Files:**
- Create: `cmd/mammoth-conformance/types.go`
- Create: `cmd/mammoth-conformance/types_test.go`

**Step 1: Write the failing test**

```go
// cmd/mammoth-conformance/types_test.go
// ABOUTME: Tests for conformance JSON output types.
// ABOUTME: Verifies serialization matches AttractorBench expected schemas.
package main

import (
	"encoding/json"
	"testing"
)

func TestConformanceNodeJSON(t *testing.T) {
	node := ConformanceNode{
		ID:         "start",
		Shape:      "Mdiamond",
		Label:      "Begin",
		Attributes: map[string]string{"goal_gate": "true"},
	}

	data, err := json.Marshal(node)
	if err != nil {
		t.Fatal(err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded["id"] != "start" {
		t.Errorf("expected id=start, got %v", decoded["id"])
	}
	if decoded["shape"] != "Mdiamond" {
		t.Errorf("expected shape=Mdiamond, got %v", decoded["shape"])
	}
	if decoded["label"] != "Begin" {
		t.Errorf("expected label=Begin, got %v", decoded["label"])
	}
	attrs, ok := decoded["attributes"].(map[string]any)
	if !ok {
		t.Fatal("attributes should be a map")
	}
	if attrs["goal_gate"] != "true" {
		t.Errorf("expected goal_gate=true, got %v", attrs["goal_gate"])
	}
}

func TestConformanceEdgeJSON(t *testing.T) {
	edge := ConformanceEdge{
		From:      "start",
		To:        "step_a",
		Label:     "plan",
		Condition: "outcome=success",
		Weight:    5,
	}

	data, err := json.Marshal(edge)
	if err != nil {
		t.Fatal(err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded["from"] != "start" {
		t.Errorf("expected from=start, got %v", decoded["from"])
	}
	if decoded["to"] != "step_a" {
		t.Errorf("expected to=step_a, got %v", decoded["to"])
	}
	if decoded["condition"] != "outcome=success" {
		t.Errorf("expected condition, got %v", decoded["condition"])
	}
	if decoded["weight"].(float64) != 5 {
		t.Errorf("expected weight=5, got %v", decoded["weight"])
	}
}

func TestConformanceDiagnosticJSON(t *testing.T) {
	diag := ConformanceDiagnostic{
		Severity: "error",
		Message:  "Missing start node",
	}

	data, err := json.Marshal(diag)
	if err != nil {
		t.Fatal(err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded["severity"] != "error" {
		t.Errorf("expected severity=error, got %v", decoded["severity"])
	}
	if decoded["message"] != "Missing start node" {
		t.Errorf("expected message, got %v", decoded["message"])
	}
}

func TestConformanceRunResultJSON(t *testing.T) {
	result := ConformanceRunResult{
		Status: "success",
		Context: map[string]any{
			"executed_nodes": []string{"start", "step_a"},
			"final_status":   "success",
		},
		Nodes: []ConformanceNodeResult{
			{ID: "start", Status: "success", Output: "", RetryCount: 0},
			{ID: "step_a", Status: "success", Output: "done", RetryCount: 1},
		},
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded["status"] != "success" {
		t.Errorf("expected status=success, got %v", decoded["status"])
	}

	nodes, ok := decoded["nodes"].([]any)
	if !ok {
		t.Fatal("nodes should be an array")
	}
	if len(nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(nodes))
	}
}

func TestConformanceParseOutputJSON(t *testing.T) {
	output := ConformanceParseOutput{
		Nodes: []ConformanceNode{
			{ID: "start", Shape: "Mdiamond", Attributes: map[string]string{}},
		},
		Edges: []ConformanceEdge{
			{From: "start", To: "exit"},
		},
		Attributes: map[string]string{"goal": "test"},
	}

	data, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if _, ok := decoded["nodes"]; !ok {
		t.Error("missing nodes field")
	}
	if _, ok := decoded["edges"]; !ok {
		t.Error("missing edges field")
	}
	if _, ok := decoded["attributes"]; !ok {
		t.Error("missing attributes field")
	}
}

func TestConformanceErrorJSON(t *testing.T) {
	errOut := ConformanceError{Error: "parse error: unexpected token"}

	data, err := json.Marshal(errOut)
	if err != nil {
		t.Fatal(err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded["error"] != "parse error: unexpected token" {
		t.Errorf("expected error message, got %v", decoded["error"])
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/mammoth-conformance/ -run TestConformance -v`
Expected: FAIL — types not defined yet.

**Step 3: Write minimal implementation**

```go
// cmd/mammoth-conformance/types.go
// ABOUTME: JSON output types for AttractorBench conformance CLI.
// ABOUTME: Translates mammoth internal types to AttractorBench expected schemas.
package main

// ConformanceNode represents a DOT graph node in AttractorBench JSON format.
type ConformanceNode struct {
	ID         string            `json:"id"`
	Shape      string            `json:"shape,omitempty"`
	Label      string            `json:"label,omitempty"`
	Attributes map[string]string `json:"attributes"`
}

// ConformanceEdge represents a DOT graph edge in AttractorBench JSON format.
type ConformanceEdge struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Label     string `json:"label,omitempty"`
	Condition string `json:"condition,omitempty"`
	Weight    int    `json:"weight"`
}

// ConformanceParseOutput is the top-level JSON output for the parse command.
type ConformanceParseOutput struct {
	Nodes      []ConformanceNode `json:"nodes"`
	Edges      []ConformanceEdge `json:"edges"`
	Attributes map[string]string `json:"attributes"`
}

// ConformanceDiagnostic represents a validation finding in AttractorBench format.
type ConformanceDiagnostic struct {
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// ConformanceValidateOutput is the top-level JSON output for the validate command.
type ConformanceValidateOutput struct {
	Diagnostics []ConformanceDiagnostic `json:"diagnostics"`
}

// ConformanceNodeResult represents a single node's execution result.
type ConformanceNodeResult struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	Output     string `json:"output,omitempty"`
	RetryCount int    `json:"retry_count"`
}

// ConformanceRunResult is the top-level JSON output for the run command.
type ConformanceRunResult struct {
	Status  string            `json:"status"`
	Context map[string]any    `json:"context"`
	Nodes   []ConformanceNodeResult `json:"nodes"`
}

// ConformanceError is the JSON output for error cases.
type ConformanceError struct {
	Error string `json:"error"`
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/mammoth-conformance/ -run TestConformance -v`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/mammoth-conformance/types.go cmd/mammoth-conformance/types_test.go
git commit -m "feat(conformance): add JSON output types for AttractorBench"
```

---

### Task 2: Parse Command

**Files:**
- Create: `cmd/mammoth-conformance/parse.go`
- Create: `cmd/mammoth-conformance/parse_test.go`

**Step 1: Write the failing test**

```go
// cmd/mammoth-conformance/parse_test.go
// ABOUTME: Tests for the parse command's DOT-to-JSON translation.
// ABOUTME: Verifies correct mapping from dot.Graph to ConformanceParseOutput.
package main

import (
	"testing"

	"github.com/2389-research/mammoth/dot"
)

func TestTranslateGraphToParseOutput(t *testing.T) {
	source := `digraph Simple {
		graph [goal="Run tests"]
		start [shape=Mdiamond]
		step_a [shape=box label="Do stuff" prompt="Write code" max_retries="2"]
		exit [shape=Msquare]
		start -> step_a [label="plan"]
		step_a -> exit [label="done" condition="outcome=success" weight="5"]
	}`

	graph, err := dot.Parse(source)
	if err != nil {
		t.Fatal(err)
	}

	output := translateGraphToParseOutput(graph)

	// Check nodes
	if len(output.Nodes) < 3 {
		t.Fatalf("expected at least 3 nodes, got %d", len(output.Nodes))
	}

	// Find start node
	var startNode *ConformanceNode
	for i := range output.Nodes {
		if output.Nodes[i].ID == "start" {
			startNode = &output.Nodes[i]
			break
		}
	}
	if startNode == nil {
		t.Fatal("start node not found")
	}
	if startNode.Shape != "Mdiamond" {
		t.Errorf("expected shape=Mdiamond, got %s", startNode.Shape)
	}

	// Find step_a node — should have attributes
	var stepNode *ConformanceNode
	for i := range output.Nodes {
		if output.Nodes[i].ID == "step_a" {
			stepNode = &output.Nodes[i]
			break
		}
	}
	if stepNode == nil {
		t.Fatal("step_a node not found")
	}
	if stepNode.Attributes["prompt"] != "Write code" {
		t.Errorf("expected prompt='Write code', got %s", stepNode.Attributes["prompt"])
	}
	if stepNode.Attributes["max_retries"] != "2" {
		t.Errorf("expected max_retries=2, got %s", stepNode.Attributes["max_retries"])
	}

	// Check edges
	if len(output.Edges) != 2 {
		t.Fatalf("expected 2 edges, got %d", len(output.Edges))
	}

	// Check graph attributes
	if output.Attributes["goal"] != "Run tests" {
		t.Errorf("expected goal='Run tests', got %s", output.Attributes["goal"])
	}
}

func TestTranslateGraphChainedEdges(t *testing.T) {
	source := `digraph Chain {
		a -> b -> c -> d
	}`

	graph, err := dot.Parse(source)
	if err != nil {
		t.Fatal(err)
	}

	output := translateGraphToParseOutput(graph)

	// Chained edges should expand to 3 separate edges
	if len(output.Edges) != 3 {
		t.Fatalf("expected 3 edges from chained a->b->c->d, got %d", len(output.Edges))
	}

	// Verify edge pairs exist
	edgePairs := make(map[string]bool)
	for _, e := range output.Edges {
		edgePairs[e.From+"->"+e.To] = true
	}
	for _, pair := range []string{"a->b", "b->c", "c->d"} {
		if !edgePairs[pair] {
			t.Errorf("missing edge %s", pair)
		}
	}
}

func TestTranslateGraphEmptyAttributes(t *testing.T) {
	source := `digraph Minimal {
		start [shape=Mdiamond]
		exit [shape=Msquare]
		start -> exit
	}`

	graph, err := dot.Parse(source)
	if err != nil {
		t.Fatal(err)
	}

	output := translateGraphToParseOutput(graph)

	// Nodes with no extra attributes should still have non-nil attributes map
	for _, node := range output.Nodes {
		if node.Attributes == nil {
			t.Errorf("node %s has nil attributes map", node.ID)
		}
	}

	// Graph with no attributes should have non-nil map
	if output.Attributes == nil {
		t.Error("graph attributes should not be nil")
	}
}

func TestTranslateEdgeWeight(t *testing.T) {
	source := `digraph Weighted {
		a -> b [weight="10"]
		a -> c [weight="abc"]
		a -> d
	}`

	graph, err := dot.Parse(source)
	if err != nil {
		t.Fatal(err)
	}

	output := translateGraphToParseOutput(graph)

	edgeWeights := make(map[string]int)
	for _, e := range output.Edges {
		edgeWeights[e.From+"->"+e.To] = e.Weight
	}

	if edgeWeights["a->b"] != 10 {
		t.Errorf("expected weight 10 for a->b, got %d", edgeWeights["a->b"])
	}
	// Invalid weight should default to 0
	if edgeWeights["a->c"] != 0 {
		t.Errorf("expected weight 0 for invalid, got %d", edgeWeights["a->c"])
	}
	// Missing weight should default to 0
	if edgeWeights["a->d"] != 0 {
		t.Errorf("expected weight 0 for missing, got %d", edgeWeights["a->d"])
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/mammoth-conformance/ -run TestTranslateGraph -v`
Expected: FAIL — `translateGraphToParseOutput` not defined.

**Step 3: Write minimal implementation**

```go
// cmd/mammoth-conformance/parse.go
// ABOUTME: Parse command for AttractorBench conformance CLI.
// ABOUTME: Translates dot.Parse() output to AttractorBench's expected JSON AST format.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"

	"github.com/2389-research/mammoth/dot"
)

// translateGraphToParseOutput converts a dot.Graph to the conformance JSON schema.
func translateGraphToParseOutput(g *dot.Graph) ConformanceParseOutput {
	// Translate nodes — sorted by ID for deterministic output
	nodeIDs := make([]string, 0, len(g.Nodes))
	for id := range g.Nodes {
		nodeIDs = append(nodeIDs, id)
	}
	sort.Strings(nodeIDs)

	nodes := make([]ConformanceNode, 0, len(nodeIDs))
	for _, id := range nodeIDs {
		n := g.Nodes[id]
		cn := ConformanceNode{
			ID:         n.ID,
			Shape:      n.Attrs["shape"],
			Label:      n.Attrs["label"],
			Attributes: make(map[string]string),
		}
		// Copy all attributes except shape and label (already top-level)
		for k, v := range n.Attrs {
			if k != "shape" && k != "label" {
				cn.Attributes[k] = v
			}
		}
		nodes = append(nodes, cn)
	}

	// Translate edges
	edges := make([]ConformanceEdge, 0, len(g.Edges))
	for _, e := range g.Edges {
		weight := 0
		if w, ok := e.Attrs["weight"]; ok {
			if parsed, err := strconv.Atoi(w); err == nil {
				weight = parsed
			}
		}
		ce := ConformanceEdge{
			From:      e.From,
			To:        e.To,
			Label:     e.Attrs["label"],
			Condition: e.Attrs["condition"],
			Weight:    weight,
		}
		edges = append(edges, ce)
	}

	// Graph-level attributes
	attrs := make(map[string]string)
	for k, v := range g.Attrs {
		attrs[k] = v
	}

	return ConformanceParseOutput{
		Nodes:      nodes,
		Edges:      edges,
		Attributes: attrs,
	}
}

// cmdParse implements the "parse <dotfile>" subcommand.
func cmdParse(dotfile string) int {
	source, err := os.ReadFile(dotfile)
	if err != nil {
		writeError(fmt.Sprintf("read file: %v", err))
		return 1
	}

	graph, err := dot.Parse(string(source))
	if err != nil {
		writeError(fmt.Sprintf("parse error: %v", err))
		return 1
	}

	output := translateGraphToParseOutput(graph)
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(output); err != nil {
		writeError(fmt.Sprintf("encode error: %v", err))
		return 1
	}
	return 0
}

// writeError writes a JSON error object to stdout.
func writeError(msg string) {
	enc := json.NewEncoder(os.Stdout)
	_ = enc.Encode(ConformanceError{Error: msg})
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/mammoth-conformance/ -run TestTranslateGraph -v`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/mammoth-conformance/parse.go cmd/mammoth-conformance/parse_test.go
git commit -m "feat(conformance): add parse command with DOT-to-JSON translation"
```

---

### Task 3: Validate Command

**Files:**
- Create: `cmd/mammoth-conformance/validate.go`
- Create: `cmd/mammoth-conformance/validate_test.go`

**Step 1: Write the failing test**

```go
// cmd/mammoth-conformance/validate_test.go
// ABOUTME: Tests for the validate command's diagnostic translation.
// ABOUTME: Verifies correct mapping from attractor.Diagnostic to conformance JSON.
package main

import (
	"testing"

	"github.com/2389-research/mammoth/attractor"
)

func TestTranslateDiagnostics(t *testing.T) {
	diags := []attractor.Diagnostic{
		{Severity: attractor.SeverityError, Message: "Missing start node", Rule: "start_node"},
		{Severity: attractor.SeverityWarning, Message: "Orphan node 'orphan'", Rule: "reachability", NodeID: "orphan"},
		{Severity: attractor.SeverityInfo, Message: "Added fail edge", Rule: "fail_edge_coverage"},
	}

	output := translateDiagnostics(diags)

	if len(output.Diagnostics) != 3 {
		t.Fatalf("expected 3 diagnostics, got %d", len(output.Diagnostics))
	}

	if output.Diagnostics[0].Severity != "error" {
		t.Errorf("expected severity=error, got %s", output.Diagnostics[0].Severity)
	}
	if output.Diagnostics[0].Message != "Missing start node" {
		t.Errorf("expected message about start node, got %s", output.Diagnostics[0].Message)
	}

	if output.Diagnostics[1].Severity != "warning" {
		t.Errorf("expected severity=warning, got %s", output.Diagnostics[1].Severity)
	}

	if output.Diagnostics[2].Severity != "info" {
		t.Errorf("expected severity=info, got %s", output.Diagnostics[2].Severity)
	}
}

func TestTranslateDiagnosticsEmpty(t *testing.T) {
	output := translateDiagnostics(nil)

	if output.Diagnostics == nil {
		t.Error("diagnostics should not be nil even when empty")
	}
	if len(output.Diagnostics) != 0 {
		t.Errorf("expected 0 diagnostics, got %d", len(output.Diagnostics))
	}
}

func TestHasErrors(t *testing.T) {
	tests := []struct {
		name     string
		diags    []attractor.Diagnostic
		expected bool
	}{
		{
			name:     "no diagnostics",
			diags:    nil,
			expected: false,
		},
		{
			name: "warnings only",
			diags: []attractor.Diagnostic{
				{Severity: attractor.SeverityWarning, Message: "warn"},
			},
			expected: false,
		},
		{
			name: "has error",
			diags: []attractor.Diagnostic{
				{Severity: attractor.SeverityWarning, Message: "warn"},
				{Severity: attractor.SeverityError, Message: "err"},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasErrors(tt.diags)
			if got != tt.expected {
				t.Errorf("hasErrors() = %v, want %v", got, tt.expected)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/mammoth-conformance/ -run TestTranslateDiagnostics -v`
Expected: FAIL — `translateDiagnostics` not defined.

**Step 3: Write minimal implementation**

```go
// cmd/mammoth-conformance/validate.go
// ABOUTME: Validate command for AttractorBench conformance CLI.
// ABOUTME: Translates attractor.Validate() diagnostics to AttractorBench JSON format.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/2389-research/mammoth/attractor"
	"github.com/2389-research/mammoth/dot"
)

// severityString converts attractor.Severity to its string representation.
func severityString(s attractor.Severity) string {
	switch s {
	case attractor.SeverityError:
		return "error"
	case attractor.SeverityWarning:
		return "warning"
	case attractor.SeverityInfo:
		return "info"
	default:
		return "unknown"
	}
}

// translateDiagnostics converts attractor diagnostics to conformance JSON format.
func translateDiagnostics(diags []attractor.Diagnostic) ConformanceValidateOutput {
	result := make([]ConformanceDiagnostic, 0, len(diags))
	for _, d := range diags {
		result = append(result, ConformanceDiagnostic{
			Severity: severityString(d.Severity),
			Message:  d.Message,
		})
	}
	return ConformanceValidateOutput{Diagnostics: result}
}

// hasErrors returns true if any diagnostic has error severity.
func hasErrors(diags []attractor.Diagnostic) bool {
	for _, d := range diags {
		if d.Severity == attractor.SeverityError {
			return true
		}
	}
	return false
}

// cmdValidate implements the "validate <dotfile>" subcommand.
func cmdValidate(dotfile string) int {
	source, err := os.ReadFile(dotfile)
	if err != nil {
		writeError(fmt.Sprintf("read file: %v", err))
		return 1
	}

	graph, err := dot.Parse(string(source))
	if err != nil {
		writeError(fmt.Sprintf("parse error: %v", err))
		return 1
	}

	// Apply default transforms before validation
	transforms := attractor.DefaultTransforms()
	graph = attractor.ApplyTransforms(graph, transforms...)

	diags := attractor.Validate(graph)
	output := translateDiagnostics(diags)

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(output); err != nil {
		writeError(fmt.Sprintf("encode error: %v", err))
		return 1
	}

	if hasErrors(diags) {
		return 1
	}
	return 0
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/mammoth-conformance/ -run "TestTranslateDiagnostics|TestHasErrors" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/mammoth-conformance/validate.go cmd/mammoth-conformance/validate_test.go
git commit -m "feat(conformance): add validate command with diagnostic translation"
```

---

### Task 4: List-Handlers Command

**Files:**
- Create: `cmd/mammoth-conformance/handlers.go`
- Create: `cmd/mammoth-conformance/handlers_test.go`

**Step 1: Write the failing test**

```go
// cmd/mammoth-conformance/handlers_test.go
// ABOUTME: Tests for the list-handlers command.
// ABOUTME: Verifies all expected handler types are present in the output.
package main

import (
	"testing"
)

func TestGetHandlerTypes(t *testing.T) {
	types := getHandlerTypes()

	// AttractorBench expects at least these types
	required := []string{"start", "exit", "codergen"}
	for _, req := range required {
		found := false
		for _, h := range types {
			if h == req {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing required handler type: %s", req)
		}
	}

	// Should have at least 5 handler types
	if len(types) < 5 {
		t.Errorf("expected at least 5 handler types, got %d", len(types))
	}
}

func TestGetHandlerTypesSorted(t *testing.T) {
	types := getHandlerTypes()

	// Verify sorted for deterministic output
	for i := 1; i < len(types); i++ {
		if types[i] < types[i-1] {
			t.Errorf("handler types not sorted: %s comes after %s", types[i], types[i-1])
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/mammoth-conformance/ -run TestGetHandlerTypes -v`
Expected: FAIL — `getHandlerTypes` not defined.

**Step 3: Write minimal implementation**

```go
// cmd/mammoth-conformance/handlers.go
// ABOUTME: List-handlers command for AttractorBench conformance CLI.
// ABOUTME: Returns sorted list of registered handler type names from attractor engine.
package main

import (
	"encoding/json"
	"os"
	"sort"

	"github.com/2389-research/mammoth/attractor"
)

// getHandlerTypes returns a sorted list of all registered handler type names.
func getHandlerTypes() []string {
	registry := attractor.DefaultHandlerRegistry()
	all := registry.All()

	types := make([]string, 0, len(all))
	for typeName := range all {
		types = append(types, typeName)
	}
	sort.Strings(types)
	return types
}

// cmdListHandlers implements the "list-handlers" subcommand.
func cmdListHandlers() int {
	types := getHandlerTypes()

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(types); err != nil {
		writeError("encode error: " + err.Error())
		return 1
	}
	return 0
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/mammoth-conformance/ -run TestGetHandlerTypes -v`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/mammoth-conformance/handlers.go cmd/mammoth-conformance/handlers_test.go
git commit -m "feat(conformance): add list-handlers command"
```

---

### Task 5: Run Command

**Files:**
- Create: `cmd/mammoth-conformance/run.go`
- Create: `cmd/mammoth-conformance/run_test.go`

**Step 1: Write the failing test**

```go
// cmd/mammoth-conformance/run_test.go
// ABOUTME: Tests for the run command's result translation.
// ABOUTME: Verifies correct mapping from attractor.RunResult to conformance JSON.
package main

import (
	"testing"

	"github.com/2389-research/mammoth/attractor"
)

func TestTranslateRunResult(t *testing.T) {
	pctx := attractor.NewContext()
	pctx.Set("some_key", "some_value")

	result := &attractor.RunResult{
		FinalOutcome: &attractor.Outcome{
			Status: attractor.StatusSuccess,
		},
		CompletedNodes: []string{"start", "step_a", "exit"},
		NodeOutcomes: map[string]*attractor.Outcome{
			"start":  {Status: attractor.StatusSuccess, Notes: "started"},
			"step_a": {Status: attractor.StatusSuccess, Notes: "code generated"},
			"exit":   {Status: attractor.StatusSuccess},
		},
		Context: pctx,
	}

	retries := map[string]int{
		"start":  0,
		"step_a": 2,
		"exit":   0,
	}

	output := translateRunResult(result, retries)

	if output.Status != "success" {
		t.Errorf("expected status=success, got %s", output.Status)
	}

	// Check context has executed_nodes
	executedNodes, ok := output.Context["executed_nodes"]
	if !ok {
		t.Fatal("missing executed_nodes in context")
	}
	nodeList, ok := executedNodes.([]string)
	if !ok {
		t.Fatal("executed_nodes should be []string")
	}
	if len(nodeList) != 3 {
		t.Errorf("expected 3 executed nodes, got %d", len(nodeList))
	}

	// Check nodes
	if len(output.Nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(output.Nodes))
	}

	// Find step_a node — should have retry count
	var stepNode *ConformanceNodeResult
	for i := range output.Nodes {
		if output.Nodes[i].ID == "step_a" {
			stepNode = &output.Nodes[i]
			break
		}
	}
	if stepNode == nil {
		t.Fatal("step_a node not found")
	}
	if stepNode.RetryCount != 2 {
		t.Errorf("expected retry_count=2, got %d", stepNode.RetryCount)
	}
	if stepNode.Status != "success" {
		t.Errorf("expected status=success, got %s", stepNode.Status)
	}
}

func TestTranslateRunResultFailure(t *testing.T) {
	result := &attractor.RunResult{
		FinalOutcome: &attractor.Outcome{
			Status:        attractor.StatusFail,
			FailureReason: "node timed out",
		},
		CompletedNodes: []string{"start"},
		NodeOutcomes: map[string]*attractor.Outcome{
			"start": {Status: attractor.StatusSuccess},
		},
		Context: attractor.NewContext(),
	}

	output := translateRunResult(result, nil)

	if output.Status != "fail" {
		t.Errorf("expected status=fail, got %s", output.Status)
	}

	finalStatus, ok := output.Context["final_status"]
	if !ok {
		t.Fatal("missing final_status in context")
	}
	if finalStatus != "fail" {
		t.Errorf("expected final_status=fail, got %v", finalStatus)
	}
}

func TestTranslateRunResultNilOutcome(t *testing.T) {
	result := &attractor.RunResult{
		FinalOutcome:   nil,
		CompletedNodes: nil,
		NodeOutcomes:   nil,
		Context:        attractor.NewContext(),
	}

	output := translateRunResult(result, nil)

	if output.Status != "unknown" {
		t.Errorf("expected status=unknown for nil outcome, got %s", output.Status)
	}
	if output.Nodes == nil {
		t.Error("nodes should not be nil")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/mammoth-conformance/ -run TestTranslateRunResult -v`
Expected: FAIL — `translateRunResult` not defined.

**Step 3: Write minimal implementation**

```go
// cmd/mammoth-conformance/run.go
// ABOUTME: Run command for AttractorBench conformance CLI.
// ABOUTME: Executes pipeline via attractor.Engine and translates result to conformance JSON.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/2389-research/mammoth/attractor"
	"github.com/2389-research/mammoth/dot"
	"github.com/2389-research/mammoth/mcp"
)

// translateRunResult converts an attractor.RunResult to conformance JSON format.
func translateRunResult(result *attractor.RunResult, retries map[string]int) ConformanceRunResult {
	status := "unknown"
	if result.FinalOutcome != nil {
		status = string(result.FinalOutcome.Status)
	}

	// Build context with executed_nodes and final_status
	ctxMap := make(map[string]any)
	if result.Context != nil {
		for k, v := range result.Context.Snapshot() {
			// Skip internal keys
			if len(k) > 0 && k[0] == '_' {
				continue
			}
			ctxMap[k] = v
		}
	}
	completedNodes := make([]string, len(result.CompletedNodes))
	copy(completedNodes, result.CompletedNodes)
	ctxMap["executed_nodes"] = completedNodes
	ctxMap["final_status"] = status

	// Build node results — sorted by completion order
	nodes := make([]ConformanceNodeResult, 0, len(result.CompletedNodes))
	for _, nodeID := range result.CompletedNodes {
		nodeResult := ConformanceNodeResult{
			ID:         nodeID,
			Status:     "unknown",
			RetryCount: 0,
		}
		if outcome, ok := result.NodeOutcomes[nodeID]; ok {
			nodeResult.Status = string(outcome.Status)
			nodeResult.Output = outcome.Notes
		}
		if retries != nil {
			nodeResult.RetryCount = retries[nodeID]
		}
		nodes = append(nodes, nodeResult)
	}

	return ConformanceRunResult{
		Status:  status,
		Context: ctxMap,
		Nodes:   nodes,
	}
}

// cmdRun implements the "run <dotfile>" subcommand.
func cmdRun(dotfile string) int {
	source, err := os.ReadFile(dotfile)
	if err != nil {
		writeError(fmt.Sprintf("read file: %v", err))
		return 1
	}

	graph, err := dot.Parse(string(source))
	if err != nil {
		writeError(fmt.Sprintf("parse error: %v", err))
		return 1
	}

	// Apply transforms
	transforms := attractor.DefaultTransforms()
	graph = attractor.ApplyTransforms(graph, transforms...)

	// Validate before running
	diags := attractor.Validate(graph)
	for _, d := range diags {
		if d.Severity == attractor.SeverityError {
			output := translateDiagnostics(diags)
			enc := json.NewEncoder(os.Stdout)
			_ = enc.Encode(output)
			return 1
		}
	}

	// Create temp artifact directory
	artifactDir, err := os.MkdirTemp("", "mammoth-conformance-*")
	if err != nil {
		writeError(fmt.Sprintf("create temp dir: %v", err))
		return 1
	}
	defer os.RemoveAll(artifactDir)

	// Detect backend from environment
	backend := mcp.DetectBackend("")

	// Track retry counts via event handler
	retries := make(map[string]int)
	eventHandler := func(evt attractor.EngineEvent) {
		if evt.Type == attractor.EventStageRetrying {
			retries[evt.NodeID]++
		}
	}

	// Build engine config
	config := attractor.EngineConfig{
		ArtifactDir:  artifactDir,
		Handlers:     attractor.DefaultHandlerRegistry(),
		Backend:      backend,
		EventHandler: eventHandler,
	}

	engine := attractor.NewEngine(config)

	// Set auto-approve interviewer for human gates
	handler := engine.GetHandler("wait.human")
	if wh, ok := handler.(*attractor.WaitForHumanHandler); ok {
		wh.Interviewer = attractor.NewAutoApproveInterviewer("yes")
	}

	// Run with 60-second timeout
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result, err := engine.RunGraph(ctx, graph)
	if err != nil {
		// Emit partial result if available
		if result != nil {
			output := translateRunResult(result, retries)
			output.Status = "fail"
			output.Context["error"] = err.Error()
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(output)
		} else {
			writeError(fmt.Sprintf("execution error: %v", err))
		}
		return 1
	}

	output := translateRunResult(result, retries)
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(output); err != nil {
		writeError(fmt.Sprintf("encode error: %v", err))
		return 1
	}
	return 0
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/mammoth-conformance/ -run TestTranslateRunResult -v`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/mammoth-conformance/run.go cmd/mammoth-conformance/run_test.go
git commit -m "feat(conformance): add run command with engine execution and result translation"
```

---

### Task 6: CLI Entrypoint

**Files:**
- Create: `cmd/mammoth-conformance/main.go`
- Create: `cmd/mammoth-conformance/main_test.go`

**Step 1: Write the failing test**

```go
// cmd/mammoth-conformance/main_test.go
// ABOUTME: Tests for the CLI entrypoint dispatch logic.
// ABOUTME: Verifies subcommand routing and argument validation.
package main

import (
	"testing"
)

func TestDispatchUnknownCommand(t *testing.T) {
	code := dispatch([]string{"mammoth-conformance", "unknown"})
	if code != 1 {
		t.Errorf("expected exit code 1 for unknown command, got %d", code)
	}
}

func TestDispatchNoArgs(t *testing.T) {
	code := dispatch([]string{"mammoth-conformance"})
	if code != 1 {
		t.Errorf("expected exit code 1 for no args, got %d", code)
	}
}

func TestDispatchParseMissingFile(t *testing.T) {
	code := dispatch([]string{"mammoth-conformance", "parse"})
	if code != 1 {
		t.Errorf("expected exit code 1 for missing file argument, got %d", code)
	}
}

func TestDispatchValidateMissingFile(t *testing.T) {
	code := dispatch([]string{"mammoth-conformance", "validate"})
	if code != 1 {
		t.Errorf("expected exit code 1 for missing file argument, got %d", code)
	}
}

func TestDispatchRunMissingFile(t *testing.T) {
	code := dispatch([]string{"mammoth-conformance", "run"})
	if code != 1 {
		t.Errorf("expected exit code 1 for missing file argument, got %d", code)
	}
}

func TestDispatchParseNonexistentFile(t *testing.T) {
	code := dispatch([]string{"mammoth-conformance", "parse", "/nonexistent/file.dot"})
	if code != 1 {
		t.Errorf("expected exit code 1 for nonexistent file, got %d", code)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/mammoth-conformance/ -run TestDispatch -v`
Expected: FAIL — `dispatch` not defined.

**Step 3: Write minimal implementation**

```go
// cmd/mammoth-conformance/main.go
// ABOUTME: CLI entrypoint for AttractorBench conformance binary.
// ABOUTME: Dispatches to parse, validate, run, or list-handlers subcommands.
package main

import (
	"fmt"
	"os"
)

func main() {
	os.Exit(dispatch(os.Args))
}

// dispatch routes CLI arguments to the appropriate subcommand handler.
func dispatch(args []string) int {
	if len(args) < 2 {
		printUsage()
		return 1
	}

	switch args[1] {
	case "parse":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: mammoth-conformance parse <dotfile>")
			return 1
		}
		return cmdParse(args[2])

	case "validate":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: mammoth-conformance validate <dotfile>")
			return 1
		}
		return cmdValidate(args[2])

	case "run":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: mammoth-conformance run <dotfile>")
			return 1
		}
		return cmdRun(args[2])

	case "list-handlers":
		return cmdListHandlers()

	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", args[1])
		printUsage()
		return 1
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "usage: mammoth-conformance <command> [args]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "commands:")
	fmt.Fprintln(os.Stderr, "  parse <dotfile>       Parse DOT file to JSON AST")
	fmt.Fprintln(os.Stderr, "  validate <dotfile>    Validate DOT file, report diagnostics")
	fmt.Fprintln(os.Stderr, "  run <dotfile>         Execute pipeline, report results")
	fmt.Fprintln(os.Stderr, "  list-handlers         List registered handler types")
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/mammoth-conformance/ -run TestDispatch -v`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/mammoth-conformance/main.go cmd/mammoth-conformance/main_test.go
git commit -m "feat(conformance): add CLI entrypoint with subcommand dispatch"
```

---

### Task 7: Integration Tests with Real DOT Files

**Files:**
- Create: `cmd/mammoth-conformance/integration_test.go`

**Step 1: Write the failing test**

```go
// cmd/mammoth-conformance/integration_test.go
// ABOUTME: Integration tests for the conformance CLI using real DOT files.
// ABOUTME: Verifies parse and validate commands produce correct output against example pipelines.
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/2389-research/mammoth/attractor"
	"github.com/2389-research/mammoth/dot"
)

func TestIntegrationParseExampleFiles(t *testing.T) {
	examplesDir := filepath.Join("..", "..", "examples")
	entries, err := os.ReadDir(examplesDir)
	if err != nil {
		t.Skipf("examples directory not found: %v", err)
	}

	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".dot" {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			dotfile := filepath.Join(examplesDir, entry.Name())
			source, err := os.ReadFile(dotfile)
			if err != nil {
				t.Fatal(err)
			}

			graph, err := dot.Parse(string(source))
			if err != nil {
				t.Fatalf("parse failed: %v", err)
			}

			output := translateGraphToParseOutput(graph)

			// Verify JSON round-trips cleanly
			data, err := json.Marshal(output)
			if err != nil {
				t.Fatalf("JSON marshal failed: %v", err)
			}

			var decoded ConformanceParseOutput
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("JSON unmarshal failed: %v", err)
			}

			if len(decoded.Nodes) != len(output.Nodes) {
				t.Errorf("node count mismatch: %d vs %d", len(decoded.Nodes), len(output.Nodes))
			}
			if len(decoded.Edges) != len(output.Edges) {
				t.Errorf("edge count mismatch: %d vs %d", len(decoded.Edges), len(output.Edges))
			}
		})
	}
}

func TestIntegrationValidateExampleFiles(t *testing.T) {
	examplesDir := filepath.Join("..", "..", "examples")
	entries, err := os.ReadDir(examplesDir)
	if err != nil {
		t.Skipf("examples directory not found: %v", err)
	}

	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".dot" {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			dotfile := filepath.Join(examplesDir, entry.Name())
			source, err := os.ReadFile(dotfile)
			if err != nil {
				t.Fatal(err)
			}

			graph, err := dot.Parse(string(source))
			if err != nil {
				t.Fatalf("parse failed: %v", err)
			}

			transforms := attractor.DefaultTransforms()
			graph = attractor.ApplyTransforms(graph, transforms...)
			diags := attractor.Validate(graph)

			output := translateDiagnostics(diags)

			// Verify JSON is valid
			data, err := json.Marshal(output)
			if err != nil {
				t.Fatalf("JSON marshal failed: %v", err)
			}

			var decoded ConformanceValidateOutput
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("JSON unmarshal failed: %v", err)
			}

			// Log diagnostics for visibility
			for _, d := range decoded.Diagnostics {
				t.Logf("[%s] %s", d.Severity, d.Message)
			}
		})
	}
}

func TestIntegrationListHandlers(t *testing.T) {
	types := getHandlerTypes()

	// Must include the core AttractorBench-expected types
	required := map[string]bool{
		"start":   false,
		"exit":    false,
		"codergen": false,
	}

	for _, h := range types {
		if _, ok := required[h]; ok {
			required[h] = true
		}
	}

	for name, found := range required {
		if !found {
			t.Errorf("missing required handler type: %s", name)
		}
	}

	// Verify output is valid JSON
	data, err := json.Marshal(types)
	if err != nil {
		t.Fatal(err)
	}

	var decoded []string
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if len(decoded) != len(types) {
		t.Errorf("handler count mismatch: %d vs %d", len(decoded), len(types))
	}
}

func TestIntegrationParseMinimalPipeline(t *testing.T) {
	source := `digraph Simple {
		graph [goal="Run tests and report"]
		rankdir=LR
		start [shape=Mdiamond]
		exit [shape=Msquare]

		start -> codergen [label="plan"]
		codergen -> exit
	}`

	graph, err := dot.Parse(source)
	if err != nil {
		t.Fatal(err)
	}

	output := translateGraphToParseOutput(graph)

	// Verify we have start node with correct shape
	var hasStart bool
	for _, n := range output.Nodes {
		if n.ID == "start" && n.Shape == "Mdiamond" {
			hasStart = true
		}
	}
	if !hasStart {
		t.Error("missing start node with shape=Mdiamond")
	}

	// Verify graph attribute
	if output.Attributes["goal"] != "Run tests and report" {
		t.Errorf("expected goal attribute, got %v", output.Attributes["goal"])
	}

	// Verify edges
	if len(output.Edges) != 2 {
		t.Errorf("expected 2 edges, got %d", len(output.Edges))
	}
}
```

**Step 2: Run test to verify it passes**

Run: `go test ./cmd/mammoth-conformance/ -run TestIntegration -v`
Expected: PASS (all dependencies already implemented in Tasks 1-6)

**Step 3: Commit**

```bash
git add cmd/mammoth-conformance/integration_test.go
git commit -m "test(conformance): add integration tests with real DOT files"
```

---

### Task 8: Build Verification and Smoke Test

**Files:**
- None created — this task verifies the binary compiles and runs.

**Step 1: Build the binary**

Run: `go build -o bin/mammoth-conformance ./cmd/mammoth-conformance/`
Expected: Compiles without errors.

**Step 2: Smoke test parse**

Create a minimal test file and run parse:

```bash
cat > /tmp/test_simple.dot << 'EOF'
digraph Simple {
  graph [goal="Run tests"]
  start [shape=Mdiamond]
  codergen [shape=box prompt="Write code"]
  exit [shape=Msquare]
  start -> codergen [label="plan"]
  codergen -> exit [label="done"]
}
EOF
./bin/mammoth-conformance parse /tmp/test_simple.dot
```

Expected: JSON output with 3 nodes, 2 edges, goal attribute.

**Step 3: Smoke test validate**

```bash
./bin/mammoth-conformance validate /tmp/test_simple.dot
echo "Exit code: $?"
```

Expected: JSON diagnostics (possibly empty or warnings only), exit code 0.

**Step 4: Smoke test list-handlers**

```bash
./bin/mammoth-conformance list-handlers
```

Expected: JSON array with handler type names including "start", "exit", "codergen".

**Step 5: Smoke test validate with bad input**

```bash
cat > /tmp/test_no_start.dot << 'EOF'
digraph Bad {
  step_a [shape=box]
  exit [shape=Msquare]
  step_a -> exit
}
EOF
./bin/mammoth-conformance validate /tmp/test_no_start.dot
echo "Exit code: $?"
```

Expected: JSON diagnostics with error about missing start node, exit code 1.

**Step 6: Run all tests with race detector**

Run: `go test -race ./cmd/mammoth-conformance/ -v`
Expected: All tests PASS.

**Step 7: Commit binary build target**

No code changes needed — binary is built from existing code. Clean up temp files:

```bash
rm -f /tmp/test_simple.dot /tmp/test_no_start.dot
```

---

### Task 9: Clone AttractorBench and Run Tier 3

**Files:**
- None in mammoth — this is an external validation step.

**Step 1: Clone AttractorBench**

```bash
cd /tmp
git clone https://github.com/strongdm/attractorbench
cd attractorbench
```

**Step 2: Install AttractorBench**

```bash
uv sync
```

**Step 3: Generate Tier 3 test tasks**

```bash
uv run attractorbench generate --tier 3
```

**Step 4: Copy mammoth conformance binary**

```bash
mkdir -p tasks/tier3/bin/
cp /path/to/mammoth-dev/bin/mammoth-conformance tasks/tier3/bin/conformance
```

**Step 5: Run Tier 3 conformance tests**

```bash
uv run attractorbench run --tier 3
```

**Step 6: Review results**

Check conformance output for pass/fail rates. Document any failures — these are real spec compliance gaps in mammoth that need fixing.

This step provides the actual answer to "do we even have it?"
