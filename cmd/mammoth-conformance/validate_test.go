// ABOUTME: Tests for the validate command, verifying severity mapping, diagnostic translation, and validation flow.
// ABOUTME: Covers severityString, translateDiagnostics, hasErrors, and cmdValidate with valid and invalid pipelines.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/2389-research/mammoth/attractor"
)

func TestSeverityString(t *testing.T) {
	tests := []struct {
		severity attractor.Severity
		want     string
	}{
		{attractor.SeverityError, "error"},
		{attractor.SeverityWarning, "warning"},
		{attractor.SeverityInfo, "info"},
		{attractor.Severity(99), "info"}, // unknown defaults to info
	}
	for _, tt := range tests {
		got := severityString(tt.severity)
		if got != tt.want {
			t.Errorf("severityString(%d) = %q, want %q", int(tt.severity), got, tt.want)
		}
	}
}

func TestTranslateDiagnostics_NonEmpty(t *testing.T) {
	diags := []attractor.Diagnostic{
		{Rule: "start_node", Severity: attractor.SeverityError, Message: "no start node"},
		{Rule: "type_known", Severity: attractor.SeverityWarning, Message: "unknown type"},
	}
	output := translateDiagnostics(diags)

	if len(output.Diagnostics) != 2 {
		t.Fatalf("got %d diagnostics, want 2", len(output.Diagnostics))
	}
	if output.Diagnostics[0].Severity != "error" {
		t.Errorf("diag[0].severity = %q, want error", output.Diagnostics[0].Severity)
	}
	if output.Diagnostics[0].Message != "no start node" {
		t.Errorf("diag[0].message = %q, want 'no start node'", output.Diagnostics[0].Message)
	}
	if output.Diagnostics[1].Severity != "warning" {
		t.Errorf("diag[1].severity = %q, want warning", output.Diagnostics[1].Severity)
	}
}

func TestTranslateDiagnostics_Empty(t *testing.T) {
	output := translateDiagnostics(nil)

	if output.Diagnostics == nil {
		t.Fatal("diagnostics should not be nil, should be empty slice")
	}
	if len(output.Diagnostics) != 0 {
		t.Errorf("got %d diagnostics, want 0", len(output.Diagnostics))
	}
}

func TestHasErrors_True(t *testing.T) {
	diags := []attractor.Diagnostic{
		{Severity: attractor.SeverityWarning, Message: "warning"},
		{Severity: attractor.SeverityError, Message: "error"},
	}
	if !hasErrors(diags) {
		t.Error("hasErrors should return true when error-severity diagnostic exists")
	}
}

func TestHasErrors_False(t *testing.T) {
	diags := []attractor.Diagnostic{
		{Severity: attractor.SeverityWarning, Message: "warning"},
		{Severity: attractor.SeverityInfo, Message: "info"},
	}
	if hasErrors(diags) {
		t.Error("hasErrors should return false when no error-severity diagnostic exists")
	}
}

func TestHasErrors_Empty(t *testing.T) {
	if hasErrors(nil) {
		t.Error("hasErrors should return false for nil input")
	}
}

func TestCmdValidate_ValidPipeline(t *testing.T) {
	source := `digraph pipeline {
		start [shape=Mdiamond]
		done [shape=Msquare]
		start -> done
	}`
	dir := t.TempDir()
	dotfile := filepath.Join(dir, "valid.dot")
	if err := os.WriteFile(dotfile, []byte(source), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	exitCode := cmdValidate(dotfile)

	w.Close()
	os.Stdout = oldStdout

	if exitCode != 0 {
		buf := make([]byte, 8192)
		n, _ := r.Read(buf)
		t.Errorf("exit code = %d, want 0; output: %s", exitCode, string(buf[:n]))
	}
}

func TestCmdValidate_InvalidPipeline(t *testing.T) {
	// Pipeline with no start node (missing shape=Mdiamond)
	source := `digraph pipeline {
		build [shape=box]
		done [shape=Msquare]
		build -> done
	}`
	dir := t.TempDir()
	dotfile := filepath.Join(dir, "invalid.dot")
	if err := os.WriteFile(dotfile, []byte(source), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	exitCode := cmdValidate(dotfile)

	w.Close()
	os.Stdout = oldStdout

	if exitCode != 1 {
		t.Errorf("exit code = %d, want 1", exitCode)
	}

	buf := make([]byte, 8192)
	n, _ := r.Read(buf)
	output := string(buf[:n])
	if !strings.Contains(output, "diagnostics") {
		t.Errorf("output should contain diagnostics: %s", output)
	}
}

func TestCmdValidate_FileNotFound(t *testing.T) {
	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	exitCode := cmdValidate("/nonexistent/file.dot")

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
