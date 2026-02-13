// ABOUTME: Tests for the mammoth CLI help display covering content, formatting, and env detection.
// ABOUTME: TDD tests written before implementation to drive the help.go design.
package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestPrintHelpContainsASCIIArt(t *testing.T) {
	var buf bytes.Buffer
	printHelp(&buf, "dev")
	out := buf.String()

	// The ASCII elephant has distinctive features we can check for.
	if !strings.Contains(out, "~~-+-+~~") {
		t.Error("expected help output to contain ASCII mammoth art")
	}
	if !strings.Contains(out, ".o8)") {
		t.Error("expected help output to contain ASCII mammoth eye")
	}
}

func TestPrintHelpContainsProjectName(t *testing.T) {
	var buf bytes.Buffer
	printHelp(&buf, "1.2.3")
	out := buf.String()

	if !strings.Contains(out, "mammoth") {
		t.Error("expected help output to contain project name 'mammoth'")
	}
	if !strings.Contains(out, "1.2.3") {
		t.Error("expected help output to contain version '1.2.3'")
	}
}

func TestPrintHelpContainsUsagePatterns(t *testing.T) {
	var buf bytes.Buffer
	printHelp(&buf, "dev")
	out := buf.String()

	patterns := []string{
		"mammoth [run] <pipeline.dot>",
		"mammoth -validate <pipeline.dot>",
		"mammoth -server",
		"mammoth serve",
	}
	for _, p := range patterns {
		if !strings.Contains(out, p) {
			t.Errorf("expected help to contain usage pattern %q", p)
		}
	}
}

func TestPrintHelpContainsAllFlags(t *testing.T) {
	var buf bytes.Buffer
	printHelp(&buf, "dev")
	out := buf.String()

	flags := []string{
		"-retry",
		"-checkpoint-dir",
		"-artifact-dir",
		"-data-dir",
		"-base-url",
		"-tui",
		"-verbose",
		"-server",
		"-port",
		"-validate",
		"-version",
		"-help",
	}
	for _, f := range flags {
		if !strings.Contains(out, f) {
			t.Errorf("expected help to contain flag %q", f)
		}
	}
}

func TestPrintHelpContainsExamples(t *testing.T) {
	var buf bytes.Buffer
	printHelp(&buf, "dev")
	out := buf.String()

	if !strings.Contains(out, "Examples:") {
		t.Error("expected help to contain 'Examples:' section header")
	}

	examples := []string{
		"mammoth examples/simple.dot",
		"mammoth -validate",
		"mammoth -tui",
		"mammoth -server",
		"mammoth -retry aggressive",
	}
	for _, e := range examples {
		if !strings.Contains(out, e) {
			t.Errorf("expected help to contain example %q", e)
		}
	}
}

func TestPrintHelpShowsEnvVarStatus(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test-key")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")

	var buf bytes.Buffer
	printHelp(&buf, "dev")
	out := buf.String()

	// ANTHROPIC_API_KEY should show [set]
	lines := strings.Split(out, "\n")
	foundSet := false
	foundNotSet := false
	for _, line := range lines {
		if strings.Contains(line, "ANTHROPIC_API_KEY") && strings.Contains(line, "[set]") && !strings.Contains(line, "[not set]") {
			foundSet = true
		}
		if strings.Contains(line, "OPENAI_API_KEY") && strings.Contains(line, "[not set]") {
			foundNotSet = true
		}
	}
	if !foundSet {
		t.Error("expected ANTHROPIC_API_KEY to show [set] when env var is present")
	}
	if !foundNotSet {
		t.Error("expected OPENAI_API_KEY to show [not set] when env var is empty")
	}
}

func TestPrintHelpShowsAllEnvKeysNotSet(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")

	var buf bytes.Buffer
	printHelp(&buf, "dev")
	out := buf.String()

	count := strings.Count(out, "[not set]")
	if count < 3 {
		t.Errorf("expected at least 3 '[not set]' markers when no keys are configured, got %d", count)
	}
}

func TestPrintHelpContainsDocsLink(t *testing.T) {
	var buf bytes.Buffer
	printHelp(&buf, "dev")
	out := buf.String()

	if !strings.Contains(out, "https://github.com/2389-research/mammoth") {
		t.Error("expected help to contain docs link")
	}
}

func TestPrintHelpWritesToWriter(t *testing.T) {
	var buf bytes.Buffer
	printHelp(&buf, "dev")

	if buf.Len() == 0 {
		t.Error("expected printHelp to write to the provided writer")
	}
}

func TestEnvStatus(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		value    string
		expected string
	}{
		{"set key", "TEST_KEY_SET", "some-value", "[set]"},
		{"empty key", "TEST_KEY_EMPTY", "", "[not set]"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			got := envStatus(tc.key)
			if got != tc.expected {
				t.Errorf("envStatus(%q) = %q, want %q", tc.key, got, tc.expected)
			}
		})
	}
}

func TestPrintHelpFlagGrouping(t *testing.T) {
	var buf bytes.Buffer
	printHelp(&buf, "dev")
	out := buf.String()

	sections := []string{
		"Pipeline Flags:",
		"Server Flags:",
		"Other:",
	}
	for _, s := range sections {
		if !strings.Contains(out, s) {
			t.Errorf("expected help to contain section header %q", s)
		}
	}
}
