// ABOUTME: Tests for the shared core tool constructors (read_file, write_file, edit_file, shell, grep, glob).
// ABOUTME: Uses a testEnv implementation of ExecutionEnvironment to verify tool behavior without mocks.

package agent

import (
	"fmt"
	"strings"
	"testing"
)

// testEnv is a real implementation of ExecutionEnvironment backed by in-memory state.
// This is not a mock -- it actually stores and retrieves files, executes commands via
// configurable functions, and tracks state.
type testEnv struct {
	files    map[string]string
	execFn   func(cmd string, timeoutMs int, workDir string, envVars map[string]string) *ExecResult
	grepFn   func(pattern, path string, opts GrepOptions) (string, error)
	globFn   func(pattern, path string) ([]string, error)
	workDir  string
	platform string
}

func newTestEnv() *testEnv {
	return &testEnv{
		files:    make(map[string]string),
		workDir:  "/tmp/test",
		platform: "linux",
	}
}

func (e *testEnv) ReadFile(path string, offset, limit int) (string, error) {
	content, ok := e.files[path]
	if !ok {
		return "", fmt.Errorf("file not found: %s", path)
	}

	lines := strings.Split(content, "\n")

	// Apply offset (1-based in spec, but 0 means start from beginning)
	startLine := 0
	if offset > 0 {
		startLine = offset - 1
	}
	if startLine >= len(lines) {
		return "", nil
	}

	// Apply limit
	endLine := len(lines)
	if limit > 0 && startLine+limit < endLine {
		endLine = startLine + limit
	}

	return strings.Join(lines[startLine:endLine], "\n"), nil
}

func (e *testEnv) WriteFile(path string, content string) error {
	e.files[path] = content
	return nil
}

func (e *testEnv) FileExists(path string) (bool, error) {
	_, ok := e.files[path]
	return ok, nil
}

func (e *testEnv) ListDirectory(path string, depth int) ([]DirEntry, error) {
	return nil, nil
}

func (e *testEnv) ExecCommand(command string, timeoutMs int, workingDir string, envVars map[string]string) (*ExecResult, error) {
	if e.execFn != nil {
		return e.execFn(command, timeoutMs, workingDir, envVars), nil
	}
	return &ExecResult{
		Stdout:     "",
		Stderr:     "",
		ExitCode:   0,
		TimedOut:   false,
		DurationMs: 10,
	}, nil
}

func (e *testEnv) Grep(pattern, path string, opts GrepOptions) (string, error) {
	if e.grepFn != nil {
		return e.grepFn(pattern, path, opts)
	}
	return "", nil
}

func (e *testEnv) Glob(pattern, path string) ([]string, error) {
	if e.globFn != nil {
		return e.globFn(pattern, path)
	}
	return nil, nil
}

func (e *testEnv) Initialize() error {
	return nil
}

func (e *testEnv) Cleanup() error {
	return nil
}

func (e *testEnv) WorkingDirectory() string {
	return e.workDir
}

func (e *testEnv) Platform() string {
	return e.platform
}

func (e *testEnv) OSVersion() string {
	return "test-os-1.0"
}

// --- read_file tests ---

func TestReadFileTool(t *testing.T) {
	env := newTestEnv()
	env.files["/tmp/test/hello.go"] = "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}"

	tool := NewReadFileTool()
	if tool.Definition.Name != "read_file" {
		t.Errorf("expected tool name 'read_file', got %q", tool.Definition.Name)
	}

	result, err := tool.Execute(map[string]any{
		"file_path": "/tmp/test/hello.go",
	}, env)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	// Should contain line numbers
	if !strings.Contains(result, "  1 | package main") {
		t.Errorf("expected line-numbered output, got:\n%s", result)
	}
	if !strings.Contains(result, "  4 | \tprintln(\"hello\")") {
		t.Errorf("expected line 4 content, got:\n%s", result)
	}
}

func TestReadFileToolOffset(t *testing.T) {
	env := newTestEnv()
	env.files["/tmp/test/lines.txt"] = "line1\nline2\nline3\nline4\nline5"

	tool := NewReadFileTool()

	result, err := tool.Execute(map[string]any{
		"file_path": "/tmp/test/lines.txt",
		"offset":    float64(3), // JSON numbers come as float64
	}, env)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	// Should start from line 3
	if !strings.Contains(result, "  3 | line3") {
		t.Errorf("expected line 3, got:\n%s", result)
	}
	// Should not contain line 1
	if strings.Contains(result, "line1") {
		t.Errorf("should not contain line1, got:\n%s", result)
	}
}

func TestReadFileToolLimit(t *testing.T) {
	env := newTestEnv()
	env.files["/tmp/test/lines.txt"] = "line1\nline2\nline3\nline4\nline5"

	tool := NewReadFileTool()

	result, err := tool.Execute(map[string]any{
		"file_path": "/tmp/test/lines.txt",
		"limit":     float64(2),
	}, env)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	// Should only contain first 2 lines
	lines := strings.Split(strings.TrimSpace(result), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d:\n%s", len(lines), result)
	}
}

func TestReadFileToolNotFound(t *testing.T) {
	env := newTestEnv()
	tool := NewReadFileTool()

	_, err := tool.Execute(map[string]any{
		"file_path": "/nonexistent/file.txt",
	}, env)
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
}

// --- write_file tests ---

func TestWriteFileTool(t *testing.T) {
	env := newTestEnv()
	tool := NewWriteFileTool()

	if tool.Definition.Name != "write_file" {
		t.Errorf("expected tool name 'write_file', got %q", tool.Definition.Name)
	}

	result, err := tool.Execute(map[string]any{
		"file_path": "/tmp/test/output.txt",
		"content":   "hello world\n",
	}, env)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	// Should return confirmation
	if !strings.Contains(result, "output.txt") {
		t.Errorf("expected confirmation mentioning file, got: %s", result)
	}

	// Verify file was written
	content, ok := env.files["/tmp/test/output.txt"]
	if !ok {
		t.Fatal("file was not written to env")
	}
	if content != "hello world\n" {
		t.Errorf("expected content 'hello world\\n', got %q", content)
	}
}

// --- edit_file tests ---

func TestEditFileTool(t *testing.T) {
	env := newTestEnv()
	env.files["/tmp/test/edit.txt"] = "hello world\nfoo bar\nbaz qux\n"

	tool := NewEditFileTool()
	if tool.Definition.Name != "edit_file" {
		t.Errorf("expected tool name 'edit_file', got %q", tool.Definition.Name)
	}

	result, err := tool.Execute(map[string]any{
		"file_path":  "/tmp/test/edit.txt",
		"old_string": "foo bar",
		"new_string": "REPLACED",
	}, env)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if !strings.Contains(result, "1") {
		t.Errorf("expected result mentioning 1 replacement, got: %s", result)
	}

	content := env.files["/tmp/test/edit.txt"]
	if !strings.Contains(content, "REPLACED") {
		t.Errorf("expected file to contain 'REPLACED', got:\n%s", content)
	}
	if strings.Contains(content, "foo bar") {
		t.Errorf("expected 'foo bar' to be replaced, still present in:\n%s", content)
	}
}

func TestEditFileToolNotFound(t *testing.T) {
	env := newTestEnv()
	tool := NewEditFileTool()

	_, err := tool.Execute(map[string]any{
		"file_path":  "/nonexistent.txt",
		"old_string": "abc",
		"new_string": "def",
	}, env)
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
}

func TestEditFileToolStringNotFound(t *testing.T) {
	env := newTestEnv()
	env.files["/tmp/test/edit.txt"] = "hello world\n"

	tool := NewEditFileTool()

	_, err := tool.Execute(map[string]any{
		"file_path":  "/tmp/test/edit.txt",
		"old_string": "nonexistent string",
		"new_string": "replacement",
	}, env)
	if err == nil {
		t.Fatal("expected error when old_string not found, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

func TestEditFileToolNotUnique(t *testing.T) {
	env := newTestEnv()
	env.files["/tmp/test/edit.txt"] = "hello world\nhello world\nhello world\n"

	tool := NewEditFileTool()

	_, err := tool.Execute(map[string]any{
		"file_path":  "/tmp/test/edit.txt",
		"old_string": "hello world",
		"new_string": "goodbye",
	}, env)
	if err == nil {
		t.Fatal("expected error when old_string matches multiple locations, got nil")
	}
	if !strings.Contains(err.Error(), "not unique") && !strings.Contains(err.Error(), "multiple") {
		t.Errorf("expected 'not unique' or 'multiple' in error, got: %v", err)
	}
}

func TestEditFileToolReplaceAll(t *testing.T) {
	env := newTestEnv()
	env.files["/tmp/test/edit.txt"] = "aaa bbb aaa ccc aaa\n"

	tool := NewEditFileTool()

	result, err := tool.Execute(map[string]any{
		"file_path":   "/tmp/test/edit.txt",
		"old_string":  "aaa",
		"new_string":  "ZZZ",
		"replace_all": true,
	}, env)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if !strings.Contains(result, "3") {
		t.Errorf("expected result mentioning 3 replacements, got: %s", result)
	}

	content := env.files["/tmp/test/edit.txt"]
	if strings.Contains(content, "aaa") {
		t.Errorf("expected all 'aaa' to be replaced, still present in:\n%s", content)
	}
	expected := "ZZZ bbb ZZZ ccc ZZZ\n"
	if content != expected {
		t.Errorf("expected %q, got %q", expected, content)
	}
}

// --- shell tests ---

func TestShellTool(t *testing.T) {
	env := newTestEnv()
	env.execFn = func(cmd string, timeoutMs int, workDir string, envVars map[string]string) *ExecResult {
		return &ExecResult{
			Stdout:     "hello from shell\n",
			Stderr:     "",
			ExitCode:   0,
			TimedOut:   false,
			DurationMs: 50,
		}
	}

	tool := NewShellTool()
	if tool.Definition.Name != "shell" {
		t.Errorf("expected tool name 'shell', got %q", tool.Definition.Name)
	}

	result, err := tool.Execute(map[string]any{
		"command": "echo hello from shell",
	}, env)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if !strings.Contains(result, "hello from shell") {
		t.Errorf("expected output to contain 'hello from shell', got: %s", result)
	}
	if !strings.Contains(result, "exit code: 0") {
		t.Errorf("expected output to contain 'exit code: 0', got: %s", result)
	}
}

func TestShellToolTimeout(t *testing.T) {
	env := newTestEnv()
	receivedTimeout := 0
	env.execFn = func(cmd string, timeoutMs int, workDir string, envVars map[string]string) *ExecResult {
		receivedTimeout = timeoutMs
		return &ExecResult{
			Stdout:     "done\n",
			Stderr:     "",
			ExitCode:   0,
			TimedOut:   false,
			DurationMs: 100,
		}
	}

	tool := NewShellTool()

	_, err := tool.Execute(map[string]any{
		"command":    "sleep 5",
		"timeout_ms": float64(30000),
	}, env)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if receivedTimeout != 30000 {
		t.Errorf("expected timeout 30000, got %d", receivedTimeout)
	}
}

// --- grep tests ---

func TestGrepTool(t *testing.T) {
	env := newTestEnv()
	env.grepFn = func(pattern, path string, opts GrepOptions) (string, error) {
		return "/tmp/test/main.go:10:func main() {\n/tmp/test/main.go:15:func helper() {\n", nil
	}

	tool := NewGrepTool()
	if tool.Definition.Name != "grep" {
		t.Errorf("expected tool name 'grep', got %q", tool.Definition.Name)
	}

	result, err := tool.Execute(map[string]any{
		"pattern": "func",
	}, env)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if !strings.Contains(result, "func main()") {
		t.Errorf("expected grep results, got: %s", result)
	}
}

// --- glob tests ---

func TestGlobTool(t *testing.T) {
	env := newTestEnv()
	env.globFn = func(pattern, path string) ([]string, error) {
		return []string{
			"/tmp/test/main.go",
			"/tmp/test/utils.go",
			"/tmp/test/pkg/helpers.go",
		}, nil
	}

	tool := NewGlobTool()
	if tool.Definition.Name != "glob" {
		t.Errorf("expected tool name 'glob', got %q", tool.Definition.Name)
	}

	result, err := tool.Execute(map[string]any{
		"pattern": "**/*.go",
	}, env)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if !strings.Contains(result, "main.go") {
		t.Errorf("expected glob results containing main.go, got: %s", result)
	}
	if !strings.Contains(result, "helpers.go") {
		t.Errorf("expected glob results containing helpers.go, got: %s", result)
	}
}

// --- stripLineNumbers tests ---

func TestStripLineNumbers_MammothFormat(t *testing.T) {
	// Mammoth's formatLineNumbers produces "NNN | content" format
	input := "  1 | package main\n  2 | \n  3 | func main() {\n  4 | \tprintln(\"hello\")\n  5 | }"
	want := "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}"

	got := stripLineNumbers(input)
	if got != want {
		t.Errorf("stripLineNumbers mammoth format:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestStripLineNumbers_TabDelimited(t *testing.T) {
	// Tab-delimited line numbers (cat -n style, no leading whitespace)
	input := "1\tpackage main\n2\t\n3\tfunc main() {\n4\t\tprintln(\"hello\")\n5\t}"
	want := "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}"

	got := stripLineNumbers(input)
	if got != want {
		t.Errorf("stripLineNumbers tab-delimited:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestStripLineNumbers_TabDelimitedPadded(t *testing.T) {
	// Tab-delimited with leading whitespace padding, as produced by LocalExecutionEnvironment.ReadFile
	// which uses fmt.Fprintf("%4d\t%s\n", lineNum, line) format.
	input := "   1\tpackage main\n   2\t\n   3\tfunc main() {\n   4\t\tprintln(\"hello\")\n   5\t}\n"
	want := "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n"

	got := stripLineNumbers(input)
	if got != want {
		t.Errorf("stripLineNumbers tab-delimited padded:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestStripLineNumbers_NoLineNumbers(t *testing.T) {
	// Content without line numbers should pass through unchanged
	input := "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}"

	got := stripLineNumbers(input)
	if got != input {
		t.Errorf("stripLineNumbers passthrough:\ngot:  %q\nwant: %q", got, input)
	}
}

func TestStripLineNumbers_BitwiseOR(t *testing.T) {
	// Lines with bitwise OR operators should NOT be stripped.
	// The majority heuristic prevents false positives here because
	// most lines don't start with "digits | ".
	input := "package main\n\nfunc flags() int {\n\tx := 123 | mask\n\treturn x\n}"

	got := stripLineNumbers(input)
	if got != input {
		t.Errorf("stripLineNumbers should not strip bitwise OR:\ngot:  %q\nwant: %q", got, input)
	}
}

func TestStripLineNumbers_MixedMajorityNumbered(t *testing.T) {
	// If the majority of non-empty lines are numbered, strip them.
	// One unnumbered blank line among several numbered lines.
	input := "  1 | package main\n\n  3 | func main() {\n  4 | \tprintln(\"hello\")\n  5 | }"
	want := "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}"

	got := stripLineNumbers(input)
	if got != want {
		t.Errorf("stripLineNumbers mixed majority:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestStripLineNumbers_Empty(t *testing.T) {
	got := stripLineNumbers("")
	if got != "" {
		t.Errorf("stripLineNumbers empty: got %q, want %q", got, "")
	}
}

func TestStripLineNumbers_HighLineNumbers(t *testing.T) {
	// High line numbers (3+ digits) should still be stripped
	input := "100 | func hundredth() {\n101 | \treturn\n102 | }"
	want := "func hundredth() {\n\treturn\n}"

	got := stripLineNumbers(input)
	if got != want {
		t.Errorf("stripLineNumbers high line numbers:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestStripLineNumbers_MinorityNumbered(t *testing.T) {
	// When fewer than half of non-empty lines look numbered, don't strip.
	// Only one of five non-empty lines matches the pattern.
	input := "normal line\nanother line\n  3 | this looks numbered\nyet another\nfinal line"

	got := stripLineNumbers(input)
	if got != input {
		t.Errorf("stripLineNumbers minority numbered should passthrough:\ngot:  %q\nwant: %q", got, input)
	}
}

func TestWriteFileStripsLineNumbers(t *testing.T) {
	// Integration test: write_file tool should strip line numbers from content
	// that was read via read_file (which adds "NNN | " prefixes).
	env := newTestEnv()
	tool := NewWriteFileTool()

	// Simulate content as read_file would return it
	numberedContent := "  1 | package main\n  2 | \n  3 | func main() {\n  4 | \tprintln(\"hello\")\n  5 | }"

	result, err := tool.Execute(map[string]any{
		"file_path": "/tmp/test/output.go",
		"content":   numberedContent,
	}, env)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	// Should return a success message
	if !strings.Contains(result, "output.go") {
		t.Errorf("expected confirmation mentioning file, got: %s", result)
	}

	// The file content should have line numbers stripped
	written, ok := env.files["/tmp/test/output.go"]
	if !ok {
		t.Fatal("file was not written to env")
	}

	expected := "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}"
	if written != expected {
		t.Errorf("write_file should strip line numbers:\ngot:  %q\nwant: %q", written, expected)
	}
}

func TestWriteFilePassthroughNoLineNumbers(t *testing.T) {
	// write_file should not alter content that doesn't have line numbers.
	env := newTestEnv()
	tool := NewWriteFileTool()

	plainContent := "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}"

	_, err := tool.Execute(map[string]any{
		"file_path": "/tmp/test/output.go",
		"content":   plainContent,
	}, env)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	written := env.files["/tmp/test/output.go"]
	if written != plainContent {
		t.Errorf("write_file should not alter non-numbered content:\ngot:  %q\nwant: %q", written, plainContent)
	}
}

// --- RegisterCoreTools tests ---

func TestRegisterCoreTools(t *testing.T) {
	registry := NewToolRegistry()
	RegisterCoreTools(registry)

	expectedTools := []string{"read_file", "write_file", "edit_file", "shell", "grep", "glob", "apply_patch"}

	if registry.Count() != len(expectedTools) {
		t.Errorf("expected %d core tools, got %d", len(expectedTools), registry.Count())
	}

	for _, name := range expectedTools {
		if !registry.Has(name) {
			t.Errorf("expected core tool %q to be registered", name)
		}
		tool := registry.Get(name)
		if tool == nil {
			t.Errorf("expected non-nil tool for %q", name)
			continue
		}
		if tool.Execute == nil {
			t.Errorf("expected non-nil Execute function for tool %q", name)
		}
		if tool.Definition.Description == "" {
			t.Errorf("expected non-empty description for tool %q", name)
		}
	}
}
