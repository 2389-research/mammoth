// ABOUTME: Tests for conformance JSON output types, verifying correct serialization to match AttractorBench schemas.
// ABOUTME: Covers all conformance structs: ParseOutput, ValidateOutput, RunResult, Error, and NodeResult.
package main

import (
	"encoding/json"
	"testing"
)

func TestConformanceNodeJSON(t *testing.T) {
	node := ConformanceNode{
		ID:    "build",
		Shape: "box",
		Label: "Build Code",
		Attributes: map[string]string{
			"prompt": "write some code",
		},
	}
	data, err := json.Marshal(node)
	if err != nil {
		t.Fatalf("marshal ConformanceNode: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded["id"] != "build" {
		t.Errorf("id = %v, want build", decoded["id"])
	}
	if decoded["shape"] != "box" {
		t.Errorf("shape = %v, want box", decoded["shape"])
	}
	if decoded["label"] != "Build Code" {
		t.Errorf("label = %v, want Build Code", decoded["label"])
	}
	attrs, ok := decoded["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes not a map: %T", decoded["attributes"])
	}
	if attrs["prompt"] != "write some code" {
		t.Errorf("attributes.prompt = %v, want 'write some code'", attrs["prompt"])
	}
}

func TestConformanceNodeOmitEmpty(t *testing.T) {
	node := ConformanceNode{
		ID:         "start",
		Attributes: map[string]string{},
	}
	data, err := json.Marshal(node)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, exists := decoded["shape"]; exists {
		t.Error("shape should be omitted when empty")
	}
	if _, exists := decoded["label"]; exists {
		t.Error("label should be omitted when empty")
	}
}

func TestConformanceEdgeJSON(t *testing.T) {
	edge := ConformanceEdge{
		From:      "start",
		To:        "build",
		Label:     "Begin",
		Condition: "outcome=SUCCESS",
		Weight:    5,
	}
	data, err := json.Marshal(edge)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded["from"] != "start" {
		t.Errorf("from = %v, want start", decoded["from"])
	}
	if decoded["to"] != "build" {
		t.Errorf("to = %v, want build", decoded["to"])
	}
	if decoded["label"] != "Begin" {
		t.Errorf("label = %v, want Begin", decoded["label"])
	}
	if decoded["condition"] != "outcome=SUCCESS" {
		t.Errorf("condition = %v, want outcome=SUCCESS", decoded["condition"])
	}
	// JSON numbers decode as float64
	if decoded["weight"] != float64(5) {
		t.Errorf("weight = %v, want 5", decoded["weight"])
	}
}

func TestConformanceEdgeOmitEmpty(t *testing.T) {
	edge := ConformanceEdge{
		From: "a",
		To:   "b",
	}
	data, err := json.Marshal(edge)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, exists := decoded["label"]; exists {
		t.Error("label should be omitted when empty")
	}
	if _, exists := decoded["condition"]; exists {
		t.Error("condition should be omitted when empty")
	}
}

func TestConformanceParseOutputJSON(t *testing.T) {
	output := ConformanceParseOutput{
		Nodes: []ConformanceNode{
			{ID: "start", Shape: "Mdiamond", Attributes: map[string]string{}},
		},
		Edges: []ConformanceEdge{
			{From: "start", To: "build"},
		},
		Attributes: map[string]string{
			"goal": "build something",
		},
	}
	data, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	nodes, ok := decoded["nodes"].([]any)
	if !ok || len(nodes) != 1 {
		t.Fatalf("nodes = %v, want 1 element", decoded["nodes"])
	}
	edges, ok := decoded["edges"].([]any)
	if !ok || len(edges) != 1 {
		t.Fatalf("edges = %v, want 1 element", decoded["edges"])
	}
	attrs, ok := decoded["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes not a map: %T", decoded["attributes"])
	}
	if attrs["goal"] != "build something" {
		t.Errorf("attributes.goal = %v, want 'build something'", attrs["goal"])
	}
}

func TestConformanceDiagnosticJSON(t *testing.T) {
	diag := ConformanceDiagnostic{
		Severity: "error",
		Message:  "no start node found",
	}
	data, err := json.Marshal(diag)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded["severity"] != "error" {
		t.Errorf("severity = %v, want error", decoded["severity"])
	}
	if decoded["message"] != "no start node found" {
		t.Errorf("message = %v, want 'no start node found'", decoded["message"])
	}
}

func TestConformanceValidateOutputJSON(t *testing.T) {
	output := ConformanceValidateOutput{
		Diagnostics: []ConformanceDiagnostic{
			{Severity: "error", Message: "missing start"},
			{Severity: "warning", Message: "unknown type"},
		},
	}
	data, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	diags, ok := decoded["diagnostics"].([]any)
	if !ok || len(diags) != 2 {
		t.Fatalf("diagnostics = %v, want 2 elements", decoded["diagnostics"])
	}
}

func TestConformanceValidateOutputEmptyDiagnostics(t *testing.T) {
	output := ConformanceValidateOutput{
		Diagnostics: []ConformanceDiagnostic{},
	}
	data, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Empty slice should serialize as [] not null
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	diags, ok := decoded["diagnostics"].([]any)
	if !ok {
		t.Fatalf("diagnostics should be an array, got %T", decoded["diagnostics"])
	}
	if len(diags) != 0 {
		t.Errorf("diagnostics should be empty, got %d", len(diags))
	}
}

func TestConformanceNodeResultJSON(t *testing.T) {
	nr := ConformanceNodeResult{
		ID:         "build",
		Status:     "success",
		Output:     "code generated",
		RetryCount: 2,
	}
	data, err := json.Marshal(nr)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded["id"] != "build" {
		t.Errorf("id = %v, want build", decoded["id"])
	}
	if decoded["status"] != "success" {
		t.Errorf("status = %v, want success", decoded["status"])
	}
	if decoded["output"] != "code generated" {
		t.Errorf("output = %v, want 'code generated'", decoded["output"])
	}
	if decoded["retry_count"] != float64(2) {
		t.Errorf("retry_count = %v, want 2", decoded["retry_count"])
	}
}

func TestConformanceNodeResultOmitEmptyOutput(t *testing.T) {
	nr := ConformanceNodeResult{
		ID:     "start",
		Status: "success",
	}
	data, err := json.Marshal(nr)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, exists := decoded["output"]; exists {
		t.Error("output should be omitted when empty")
	}
}

func TestConformanceRunResultJSON(t *testing.T) {
	rr := ConformanceRunResult{
		Status: "success",
		Context: map[string]any{
			"executed_nodes": []string{"start", "build", "end"},
			"final_status":   "success",
		},
		Nodes: []ConformanceNodeResult{
			{ID: "start", Status: "success"},
			{ID: "build", Status: "success", Output: "done", RetryCount: 1},
		},
	}
	data, err := json.Marshal(rr)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded["status"] != "success" {
		t.Errorf("status = %v, want success", decoded["status"])
	}
	ctx, ok := decoded["context"].(map[string]any)
	if !ok {
		t.Fatalf("context not a map: %T", decoded["context"])
	}
	if ctx["final_status"] != "success" {
		t.Errorf("context.final_status = %v, want success", ctx["final_status"])
	}
	nodes, ok := decoded["nodes"].([]any)
	if !ok || len(nodes) != 2 {
		t.Fatalf("nodes = %v, want 2 elements", decoded["nodes"])
	}
}

func TestConformanceErrorJSON(t *testing.T) {
	ce := ConformanceError{Error: "file not found"}
	data, err := json.Marshal(ce)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded["error"] != "file not found" {
		t.Errorf("error = %v, want 'file not found'", decoded["error"])
	}
}
