// ABOUTME: Tests for the run command, verifying RunResult-to-conformance translation and pipeline execution.
// ABOUTME: Covers translateRunResult with nil/non-nil outcomes, retry counts, context filtering, and cmdRun.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/2389-research/mammoth/attractor"
)

func TestTranslateRunResult_BasicSuccess(t *testing.T) {
	result := &attractor.RunResult{
		FinalOutcome: &attractor.Outcome{
			Status: attractor.StatusSuccess,
			Notes:  "pipeline complete",
		},
		CompletedNodes: []string{"start", "build", "done"},
		NodeOutcomes: map[string]*attractor.Outcome{
			"start": {Status: attractor.StatusSuccess},
			"build": {Status: attractor.StatusSuccess, Notes: "code generated"},
			"done":  {Status: attractor.StatusSuccess},
		},
		Context: attractor.NewContext(),
	}
	result.Context.Set("outcome", "success")
	result.Context.Set("_graph", "internal") // should be skipped

	retries := map[string]int{"build": 2}

	output := translateRunResult(result, retries)

	if output.Status != "success" {
		t.Errorf("status = %q, want success", output.Status)
	}
	if output.Context["final_status"] != "success" {
		t.Errorf("context.final_status = %v, want success", output.Context["final_status"])
	}
	// Internal key should be filtered
	if _, ok := output.Context["_graph"]; ok {
		t.Error("internal key _graph should be filtered from context")
	}
	// executed_nodes should be present
	executedNodes, ok := output.Context["executed_nodes"].([]string)
	if !ok {
		t.Fatalf("executed_nodes not []string: %T", output.Context["executed_nodes"])
	}
	if len(executedNodes) != 3 {
		t.Errorf("executed_nodes has %d items, want 3", len(executedNodes))
	}

	// Node results
	if len(output.Nodes) != 3 {
		t.Fatalf("got %d nodes, want 3", len(output.Nodes))
	}
	// Check order matches CompletedNodes
	if output.Nodes[0].ID != "start" {
		t.Errorf("nodes[0].id = %q, want start", output.Nodes[0].ID)
	}
	if output.Nodes[1].ID != "build" {
		t.Errorf("nodes[1].id = %q, want build", output.Nodes[1].ID)
	}
	if output.Nodes[1].RetryCount != 2 {
		t.Errorf("nodes[1].retry_count = %d, want 2", output.Nodes[1].RetryCount)
	}
	if output.Nodes[1].Output != "code generated" {
		t.Errorf("nodes[1].output = %q, want 'code generated'", output.Nodes[1].Output)
	}
	if output.Nodes[2].ID != "done" {
		t.Errorf("nodes[2].id = %q, want done", output.Nodes[2].ID)
	}
}

func TestTranslateRunResult_NilOutcome(t *testing.T) {
	result := &attractor.RunResult{
		FinalOutcome:   nil,
		CompletedNodes: []string{},
		NodeOutcomes:   map[string]*attractor.Outcome{},
		Context:        attractor.NewContext(),
	}
	output := translateRunResult(result, nil)

	if output.Status != "unknown" {
		t.Errorf("status = %q, want unknown for nil outcome", output.Status)
	}
}

func TestTranslateRunResult_DuplicateCompletedNodes(t *testing.T) {
	// A node can appear multiple times in CompletedNodes due to retries
	result := &attractor.RunResult{
		FinalOutcome:   &attractor.Outcome{Status: attractor.StatusSuccess},
		CompletedNodes: []string{"start", "build", "build", "done"},
		NodeOutcomes: map[string]*attractor.Outcome{
			"start": {Status: attractor.StatusSuccess},
			"build": {Status: attractor.StatusSuccess},
			"done":  {Status: attractor.StatusSuccess},
		},
		Context: attractor.NewContext(),
	}

	output := translateRunResult(result, nil)

	// Should deduplicate nodes
	if len(output.Nodes) != 3 {
		t.Errorf("got %d nodes, want 3 (deduplicated)", len(output.Nodes))
	}
}

func TestTranslateRunResult_NilContext(t *testing.T) {
	result := &attractor.RunResult{
		FinalOutcome:   &attractor.Outcome{Status: attractor.StatusFail},
		CompletedNodes: []string{"start"},
		NodeOutcomes: map[string]*attractor.Outcome{
			"start": {Status: attractor.StatusSuccess},
		},
		Context: nil,
	}

	output := translateRunResult(result, nil)

	if output.Status != "fail" {
		t.Errorf("status = %q, want fail", output.Status)
	}
	// Context should still have executed_nodes and final_status
	if output.Context["final_status"] != "fail" {
		t.Errorf("context.final_status = %v, want fail", output.Context["final_status"])
	}
}

func TestCmdRun_FileNotFound(t *testing.T) {
	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	exitCode := cmdRun("/nonexistent/pipeline.dot")

	w.Close()
	os.Stdout = oldStdout

	if exitCode != 1 {
		t.Errorf("exit code = %d, want 1", exitCode)
	}

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	output := string(buf[:n])
	if !strings.Contains(output, "error") {
		t.Errorf("output should contain error: %s", output)
	}
}

func TestCmdRun_InvalidDOT(t *testing.T) {
	dir := t.TempDir()
	dotfile := filepath.Join(dir, "bad.dot")
	if err := os.WriteFile(dotfile, []byte("not valid dot"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	exitCode := cmdRun(dotfile)

	w.Close()
	os.Stdout = oldStdout

	if exitCode != 1 {
		t.Errorf("exit code = %d, want 1", exitCode)
	}

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	output := string(buf[:n])
	if !strings.Contains(output, "error") {
		t.Errorf("output should contain error: %s", output)
	}
}

func TestCmdRun_ValidationFailure(t *testing.T) {
	// Valid DOT syntax but invalid pipeline (no start node)
	source := `digraph pipeline {
		build [shape=box]
		done [shape=Msquare]
		build -> done
	}`
	dir := t.TempDir()
	dotfile := filepath.Join(dir, "nostart.dot")
	if err := os.WriteFile(dotfile, []byte(source), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	exitCode := cmdRun(dotfile)

	w.Close()
	os.Stdout = oldStdout

	if exitCode != 1 {
		t.Errorf("exit code = %d, want 1", exitCode)
	}

	buf := make([]byte, 8192)
	n, _ := r.Read(buf)
	output := string(buf[:n])
	if !strings.Contains(output, "diagnostics") {
		t.Errorf("output should contain diagnostics for validation failure: %s", output)
	}
}

func TestCmdRun_SimplePipeline(t *testing.T) {
	// A minimal valid pipeline: start -> exit
	source := `digraph pipeline {
		start [shape=Mdiamond]
		done [shape=Msquare]
		start -> done
	}`
	dir := t.TempDir()
	dotfile := filepath.Join(dir, "simple.dot")
	if err := os.WriteFile(dotfile, []byte(source), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	exitCode := cmdRun(dotfile)

	w.Close()
	os.Stdout = oldStdout

	buf := make([]byte, 8192)
	n, _ := r.Read(buf)
	output := string(buf[:n])

	if exitCode != 0 {
		t.Errorf("exit code = %d, want 0; output: %s", exitCode, output)
	}

	if !strings.Contains(output, "success") {
		t.Errorf("output should contain success status: %s", output)
	}
}
