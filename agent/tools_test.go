// ABOUTME: Tests for the ToolRegistry that manages tool registration, lookup, and output truncation.
// ABOUTME: Covers register/unregister/get/definitions/has/names/count, truncation modes, and concurrency.

package agent

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/2389-research/mammoth/llm"
)

func TestNewToolRegistry(t *testing.T) {
	registry := NewToolRegistry()
	if registry == nil {
		t.Fatal("NewToolRegistry returned nil")
	}
	if registry.Count() != 0 {
		t.Errorf("expected empty registry, got count %d", registry.Count())
	}
}

func TestRegisterTool(t *testing.T) {
	registry := NewToolRegistry()

	tool := &RegisteredTool{
		Definition: llm.ToolDefinition{
			Name:        "test_tool",
			Description: "A test tool",
			Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		},
		Execute: func(args map[string]any, env ExecutionEnvironment) (string, error) {
			return "ok", nil
		},
		Description: "A test tool",
	}

	err := registry.Register(tool)
	if err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	got := registry.Get("test_tool")
	if got == nil {
		t.Fatal("Get returned nil after registering")
	}
	if got.Definition.Name != "test_tool" {
		t.Errorf("expected name 'test_tool', got %q", got.Definition.Name)
	}
	if got.Definition.Description != "A test tool" {
		t.Errorf("expected description 'A test tool', got %q", got.Definition.Description)
	}
}

func TestRegisterToolEmptyName(t *testing.T) {
	registry := NewToolRegistry()

	tool := &RegisteredTool{
		Definition: llm.ToolDefinition{
			Name:        "",
			Description: "No name tool",
			Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		},
		Execute: func(args map[string]any, env ExecutionEnvironment) (string, error) {
			return "", nil
		},
	}

	err := registry.Register(tool)
	if err == nil {
		t.Fatal("expected error for empty name, got nil")
	}
}

func TestUnregisterTool(t *testing.T) {
	registry := NewToolRegistry()

	tool := &RegisteredTool{
		Definition: llm.ToolDefinition{
			Name:        "removable",
			Description: "Will be removed",
			Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		},
		Execute: func(args map[string]any, env ExecutionEnvironment) (string, error) {
			return "", nil
		},
	}

	registry.Register(tool)

	removed := registry.Unregister("removable")
	if !removed {
		t.Error("Unregister returned false for existing tool")
	}

	if registry.Get("removable") != nil {
		t.Error("tool still exists after Unregister")
	}

	removed = registry.Unregister("nonexistent")
	if removed {
		t.Error("Unregister returned true for nonexistent tool")
	}
}

func TestGetTool(t *testing.T) {
	registry := NewToolRegistry()

	tool := &RegisteredTool{
		Definition: llm.ToolDefinition{
			Name:        "findme",
			Description: "Find me",
			Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		},
		Execute: func(args map[string]any, env ExecutionEnvironment) (string, error) {
			return "found", nil
		},
	}

	registry.Register(tool)

	// Found case
	got := registry.Get("findme")
	if got == nil {
		t.Fatal("Get returned nil for existing tool")
	}

	// Not found case
	got = registry.Get("missing")
	if got != nil {
		t.Errorf("Get returned non-nil for missing tool: %+v", got)
	}
}

func TestDefinitions(t *testing.T) {
	registry := NewToolRegistry()

	tools := []*RegisteredTool{
		{
			Definition: llm.ToolDefinition{
				Name:        "alpha",
				Description: "First tool",
				Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
			},
			Execute: func(args map[string]any, env ExecutionEnvironment) (string, error) {
				return "", nil
			},
		},
		{
			Definition: llm.ToolDefinition{
				Name:        "beta",
				Description: "Second tool",
				Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
			},
			Execute: func(args map[string]any, env ExecutionEnvironment) (string, error) {
				return "", nil
			},
		},
	}

	for _, tool := range tools {
		registry.Register(tool)
	}

	defs := registry.Definitions()
	if len(defs) != 2 {
		t.Fatalf("expected 2 definitions, got %d", len(defs))
	}

	// Verify both tools are present (order may vary)
	names := make(map[string]bool)
	for _, d := range defs {
		names[d.Name] = true
	}
	if !names["alpha"] || !names["beta"] {
		t.Errorf("expected definitions for alpha and beta, got %v", names)
	}
}

func TestHas(t *testing.T) {
	registry := NewToolRegistry()

	tool := &RegisteredTool{
		Definition: llm.ToolDefinition{
			Name:        "exists",
			Description: "Exists",
			Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		},
		Execute: func(args map[string]any, env ExecutionEnvironment) (string, error) {
			return "", nil
		},
	}

	registry.Register(tool)

	if !registry.Has("exists") {
		t.Error("Has returned false for existing tool")
	}
	if registry.Has("nope") {
		t.Error("Has returned true for nonexistent tool")
	}
}

func TestNames(t *testing.T) {
	registry := NewToolRegistry()

	toolNames := []string{"gamma", "delta", "epsilon"}
	for _, name := range toolNames {
		registry.Register(&RegisteredTool{
			Definition: llm.ToolDefinition{
				Name:        name,
				Description: "Tool " + name,
				Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
			},
			Execute: func(args map[string]any, env ExecutionEnvironment) (string, error) {
				return "", nil
			},
		})
	}

	names := registry.Names()
	if len(names) != 3 {
		t.Fatalf("expected 3 names, got %d", len(names))
	}

	sort.Strings(names)
	sort.Strings(toolNames)
	for i, name := range names {
		if name != toolNames[i] {
			t.Errorf("expected name %q at index %d, got %q", toolNames[i], i, name)
		}
	}
}

func TestCount(t *testing.T) {
	registry := NewToolRegistry()

	if registry.Count() != 0 {
		t.Errorf("expected count 0, got %d", registry.Count())
	}

	for i := 0; i < 5; i++ {
		registry.Register(&RegisteredTool{
			Definition: llm.ToolDefinition{
				Name:        strings.Repeat("t", i+1),
				Description: "Tool",
				Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
			},
			Execute: func(args map[string]any, env ExecutionEnvironment) (string, error) {
				return "", nil
			},
		})
	}

	if registry.Count() != 5 {
		t.Errorf("expected count 5, got %d", registry.Count())
	}
}

func TestTruncateOutputHeadTail(t *testing.T) {
	// Create a string longer than the limit
	input := strings.Repeat("A", 500) + strings.Repeat("B", 500)
	result := TruncateOutput(input, 200, "head_tail")

	// Should contain the beginning (A's)
	if !strings.HasPrefix(result, "AAAA") {
		t.Error("head_tail result should start with content from the beginning")
	}

	// Should contain the end (B's)
	if !strings.HasSuffix(result, "BBBB") {
		t.Error("head_tail result should end with content from the end")
	}

	// Should contain truncation warning
	if !strings.Contains(result, "WARNING") {
		t.Error("head_tail result should contain truncation WARNING")
	}

	if !strings.Contains(result, "characters were removed") {
		t.Error("head_tail result should mention characters removed")
	}
}

func TestTruncateOutputTail(t *testing.T) {
	input := strings.Repeat("A", 500) + strings.Repeat("B", 500)
	result := TruncateOutput(input, 200, "tail")

	// Should contain the end (B's)
	if !strings.HasSuffix(result, "BBBB") {
		t.Error("tail result should end with content from the end")
	}

	// Should contain truncation warning at beginning
	if !strings.HasPrefix(result, "[WARNING") {
		t.Error("tail result should start with truncation WARNING")
	}

	if !strings.Contains(result, "characters were removed") {
		t.Error("tail result should mention characters removed")
	}
}

func TestTruncateOutputNoTruncation(t *testing.T) {
	input := "short string"
	result := TruncateOutput(input, 1000, "head_tail")

	if result != input {
		t.Errorf("expected unchanged output for short string, got %q", result)
	}
}

func TestTruncateToolOutput(t *testing.T) {
	// Test that per-tool defaults apply
	longOutput := strings.Repeat("X", 60000)

	// read_file default is 50000 chars, head_tail mode
	result := TruncateToolOutput(longOutput, "read_file", nil)
	if !strings.Contains(result, "WARNING") {
		t.Error("read_file output exceeding 50000 chars should be truncated")
	}

	// write_file default is 1000 chars, tail mode
	result = TruncateToolOutput(longOutput, "write_file", nil)
	if !strings.Contains(result, "WARNING") {
		t.Error("write_file output exceeding 1000 chars should be truncated")
	}

	// Short output should not be truncated
	shortOutput := "ok"
	result = TruncateToolOutput(shortOutput, "read_file", nil)
	if result != shortOutput {
		t.Errorf("expected unchanged output for short string, got %q", result)
	}

	// Custom limit overrides default
	customLimits := map[string]int{"read_file": 100}
	mediumOutput := strings.Repeat("Y", 200)
	result = TruncateToolOutput(mediumOutput, "read_file", customLimits)
	if !strings.Contains(result, "WARNING") {
		t.Error("output exceeding custom limit should be truncated")
	}

	// Unknown tool should use a sensible default (30000)
	result = TruncateToolOutput(longOutput, "unknown_tool", nil)
	if !strings.Contains(result, "WARNING") {
		t.Error("unknown tool with long output should still be truncated")
	}
}

func TestTruncateLines(t *testing.T) {
	// Build output with 20 lines
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = fmt.Sprintf("line-%02d", i+1)
	}
	input := strings.Join(lines, "\n")

	result := TruncateLines(input, 10)

	// Should keep first 5 lines (10/2)
	if !strings.HasPrefix(result, "line-01\n") {
		t.Error("truncated output should start with the first line")
	}

	// Should keep last 5 lines (10 - 5)
	if !strings.HasSuffix(result, "line-20") {
		t.Error("truncated output should end with the last line")
	}

	// Should contain the omission marker
	if !strings.Contains(result, "lines omitted") {
		t.Error("truncated output should contain 'lines omitted' marker")
	}

	// Should report 10 lines omitted (20 total - 5 head - 5 tail)
	if !strings.Contains(result, "10 lines omitted") {
		t.Errorf("expected '10 lines omitted' in result, got:\n%s", result)
	}

	// Should NOT contain lines from the middle
	if strings.Contains(result, "line-08") {
		t.Error("truncated output should not contain middle lines")
	}
}

func TestTruncateLinesNoTruncation(t *testing.T) {
	// Build output with 5 lines
	lines := make([]string, 5)
	for i := range lines {
		lines[i] = fmt.Sprintf("line-%02d", i+1)
	}
	input := strings.Join(lines, "\n")

	// maxLines=10 is greater than 5 lines, so no truncation
	result := TruncateLines(input, 10)
	if result != input {
		t.Errorf("expected unchanged output when under line limit, got %q", result)
	}

	// maxLines=0 means unlimited, so no truncation
	result = TruncateLines(input, 0)
	if result != input {
		t.Errorf("expected unchanged output when maxLines is 0 (unlimited), got %q", result)
	}
}

func TestTruncateToolOutputWithLineLimit(t *testing.T) {
	// Create output that is within the character limit but exceeds the line limit for shell.
	// shell has default char limit of 30000 and line limit of 256.
	// Build 300 short lines (well under 30000 chars, but over 256 lines).
	lines := make([]string, 300)
	for i := range lines {
		lines[i] = fmt.Sprintf("output-line-%03d", i+1)
	}
	input := strings.Join(lines, "\n")

	result := TruncateToolOutput(input, "shell", nil)

	// Character truncation should NOT trigger (input is ~5100 chars, limit is 30000)
	if strings.Contains(result, "characters were removed") {
		t.Error("character truncation should not trigger for this input size")
	}

	// Line truncation SHOULD trigger (300 lines > 256 limit)
	if !strings.Contains(result, "lines omitted") {
		t.Error("line truncation should trigger when output exceeds the line limit")
	}

	// Verify the output preserves the first and last lines
	if !strings.HasPrefix(result, "output-line-001") {
		t.Error("line-truncated output should start with the first line")
	}
	if !strings.HasSuffix(result, "output-line-300") {
		t.Error("line-truncated output should end with the last line")
	}
}

func TestTruncateToolOutputShellLineLimit(t *testing.T) {
	// Verify shell specifically gets a 256 line limit.
	// Build exactly 256 lines -- should NOT be truncated.
	lines256 := make([]string, 256)
	for i := range lines256 {
		lines256[i] = fmt.Sprintf("sh-%03d", i+1)
	}
	input256 := strings.Join(lines256, "\n")

	result := TruncateToolOutput(input256, "shell", nil)
	if strings.Contains(result, "lines omitted") {
		t.Error("shell output of exactly 256 lines should not be line-truncated")
	}

	// Build 257 lines -- should be truncated.
	lines257 := make([]string, 257)
	for i := range lines257 {
		lines257[i] = fmt.Sprintf("sh-%03d", i+1)
	}
	input257 := strings.Join(lines257, "\n")

	result = TruncateToolOutput(input257, "shell", nil)
	if !strings.Contains(result, "lines omitted") {
		t.Error("shell output of 257 lines should be line-truncated (limit is 256)")
	}
}

func TestTruncateToolOutputGrepLineLimit(t *testing.T) {
	// Verify grep specifically gets a 200 line limit.
	// Build exactly 200 lines -- should NOT be truncated.
	lines200 := make([]string, 200)
	for i := range lines200 {
		lines200[i] = fmt.Sprintf("match-%03d", i+1)
	}
	input200 := strings.Join(lines200, "\n")

	result := TruncateToolOutput(input200, "grep", nil)
	if strings.Contains(result, "lines omitted") {
		t.Error("grep output of exactly 200 lines should not be line-truncated")
	}

	// Build 201 lines -- should be truncated.
	lines201 := make([]string, 201)
	for i := range lines201 {
		lines201[i] = fmt.Sprintf("match-%03d", i+1)
	}
	input201 := strings.Join(lines201, "\n")

	result = TruncateToolOutput(input201, "grep", nil)
	if !strings.Contains(result, "lines omitted") {
		t.Error("grep output of 201 lines should be line-truncated (limit is 200)")
	}
}

func TestRegistryConcurrency(t *testing.T) {
	registry := NewToolRegistry()

	var wg sync.WaitGroup
	concurrency := 100

	// Concurrent registers
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			name := strings.Repeat("x", idx%10+1)
			registry.Register(&RegisteredTool{
				Definition: llm.ToolDefinition{
					Name:        name,
					Description: "Concurrent tool",
					Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
				},
				Execute: func(args map[string]any, env ExecutionEnvironment) (string, error) {
					return "", nil
				},
			})
		}(i)
	}

	// Concurrent reads
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			registry.Get("xxx")
			registry.Has("xxx")
			registry.Names()
			registry.Definitions()
			registry.Count()
		}()
	}

	wg.Wait()

	// Just verify no panics occurred and registry is in consistent state
	if registry.Count() < 1 {
		t.Error("registry should have at least 1 tool after concurrent registration")
	}
}
