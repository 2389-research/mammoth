// ABOUTME: Tests for LocalExecutionEnvironment, the default local implementation.
// ABOUTME: Covers file ops, command execution, env filtering, grep, glob, and lifecycle.

package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLocalExecEnvReadFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "hello.txt")
	content := "line one\nline two\nline three\n"
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	env := NewLocalExecutionEnvironment(dir)
	result, err := env.ReadFile(filePath, 0, 0)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	// Should contain line numbers
	if !strings.Contains(result, "1\t") {
		t.Error("expected line number 1 in output")
	}
	if !strings.Contains(result, "line one") {
		t.Error("expected 'line one' in output")
	}
	if !strings.Contains(result, "3\t") {
		t.Error("expected line number 3 in output")
	}
	if !strings.Contains(result, "line three") {
		t.Error("expected 'line three' in output")
	}
}

func TestLocalExecEnvReadFileOffset(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "lines.txt")
	var lines []string
	for i := 1; i <= 10; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	if err := os.WriteFile(filePath, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	env := NewLocalExecutionEnvironment(dir)

	// Read starting at line 3, limit 2
	result, err := env.ReadFile(filePath, 3, 2)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	if !strings.Contains(result, "line 3") {
		t.Error("expected 'line 3' in output")
	}
	if !strings.Contains(result, "line 4") {
		t.Error("expected 'line 4' in output")
	}
	if strings.Contains(result, "line 2") {
		t.Error("should not contain 'line 2' (before offset)")
	}
	if strings.Contains(result, "line 5") {
		t.Error("should not contain 'line 5' (past limit)")
	}
}

func TestLocalExecEnvReadFileNotFound(t *testing.T) {
	dir := t.TempDir()
	env := NewLocalExecutionEnvironment(dir)

	_, err := env.ReadFile(filepath.Join(dir, "nonexistent.txt"), 0, 0)
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
}

func TestLocalExecEnvWriteFile(t *testing.T) {
	dir := t.TempDir()
	env := NewLocalExecutionEnvironment(dir)

	filePath := filepath.Join(dir, "output.txt")
	content := "hello world\n"
	if err := env.WriteFile(filePath, content); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("os.ReadFile returned error: %v", err)
	}
	if string(data) != content {
		t.Errorf("expected %q, got %q", content, string(data))
	}
}

func TestLocalExecEnvWriteFileCreateDirs(t *testing.T) {
	dir := t.TempDir()
	env := NewLocalExecutionEnvironment(dir)

	filePath := filepath.Join(dir, "a", "b", "c", "deep.txt")
	content := "deep content\n"
	if err := env.WriteFile(filePath, content); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("os.ReadFile returned error: %v", err)
	}
	if string(data) != content {
		t.Errorf("expected %q, got %q", content, string(data))
	}
}

func TestLocalExecEnvFileExists(t *testing.T) {
	dir := t.TempDir()
	env := NewLocalExecutionEnvironment(dir)

	// File does not exist
	exists, err := env.FileExists(filepath.Join(dir, "nope.txt"))
	if err != nil {
		t.Fatalf("FileExists returned error: %v", err)
	}
	if exists {
		t.Error("expected false for nonexistent file")
	}

	// Create a file
	filePath := filepath.Join(dir, "yep.txt")
	if err := os.WriteFile(filePath, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	exists, err = env.FileExists(filePath)
	if err != nil {
		t.Fatalf("FileExists returned error: %v", err)
	}
	if !exists {
		t.Error("expected true for existing file")
	}
}

func TestLocalExecEnvListDirectory(t *testing.T) {
	dir := t.TempDir()

	// Create files and a subdirectory
	if err := os.WriteFile(filepath.Join(dir, "file1.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file2.txt"), []byte("world!"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0755); err != nil {
		t.Fatal(err)
	}

	env := NewLocalExecutionEnvironment(dir)
	entries, err := env.ListDirectory(dir, 0)
	if err != nil {
		t.Fatalf("ListDirectory returned error: %v", err)
	}

	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	// Build a map for easier checking
	nameMap := make(map[string]DirEntry)
	for _, e := range entries {
		nameMap[e.Name] = e
	}

	if e, ok := nameMap["file1.txt"]; !ok {
		t.Error("missing file1.txt")
	} else {
		if e.IsDir {
			t.Error("file1.txt should not be a directory")
		}
		if e.Size != 5 {
			t.Errorf("file1.txt expected size 5, got %d", e.Size)
		}
	}

	if e, ok := nameMap["subdir"]; !ok {
		t.Error("missing subdir")
	} else if !e.IsDir {
		t.Error("subdir should be a directory")
	}
}

func TestLocalExecEnvExecCommand(t *testing.T) {
	dir := t.TempDir()
	env := NewLocalExecutionEnvironment(dir)

	result, err := env.ExecCommand("echo hello", 10000, "", nil)
	if err != nil {
		t.Fatalf("ExecCommand returned error: %v", err)
	}

	if !strings.Contains(result.Stdout, "hello") {
		t.Errorf("expected stdout to contain 'hello', got %q", result.Stdout)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
	if result.TimedOut {
		t.Error("command should not have timed out")
	}
	if result.DurationMs < 0 {
		t.Error("duration should be non-negative")
	}
}

func TestLocalExecEnvExecCommandTimeout(t *testing.T) {
	dir := t.TempDir()
	env := NewLocalExecutionEnvironment(dir)

	// Sleep for 30 seconds with a 500ms timeout -- should time out
	result, err := env.ExecCommand("sleep 30", 500, "", nil)
	if err != nil {
		t.Fatalf("ExecCommand returned error: %v", err)
	}

	if !result.TimedOut {
		t.Error("expected command to time out")
	}
}

func TestLocalExecEnvExecCommandExitCode(t *testing.T) {
	dir := t.TempDir()
	env := NewLocalExecutionEnvironment(dir)

	result, err := env.ExecCommand("exit 42", 10000, "", nil)
	if err != nil {
		t.Fatalf("ExecCommand returned error: %v", err)
	}

	if result.ExitCode != 42 {
		t.Errorf("expected exit code 42, got %d", result.ExitCode)
	}
}

func TestLocalExecEnvExecCommandWorkingDir(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "subwork")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	env := NewLocalExecutionEnvironment(dir)

	result, err := env.ExecCommand("pwd", 10000, subDir, nil)
	if err != nil {
		t.Fatalf("ExecCommand returned error: %v", err)
	}

	got := strings.TrimSpace(result.Stdout)
	// Resolve symlinks for comparison (macOS /tmp -> /private/tmp)
	resolvedSubDir, _ := filepath.EvalSymlinks(subDir)
	resolvedGot, _ := filepath.EvalSymlinks(got)

	if resolvedGot != resolvedSubDir {
		t.Errorf("expected working dir %q, got %q", resolvedSubDir, resolvedGot)
	}
}

func TestLocalExecEnvEnvFiltering(t *testing.T) {
	dir := t.TempDir()
	env := NewLocalExecutionEnvironment(dir)

	// Pass sensitive and safe environment variables
	envVars := map[string]string{
		"MY_API_KEY":     "secret123",
		"DATABASE_TOKEN": "dbtoken",
		"SAFE_VAR":       "safe_value",
	}

	// Default policy should filter out API_KEY and TOKEN patterns
	result, err := env.ExecCommand("env", 10000, "", envVars)
	if err != nil {
		t.Fatalf("ExecCommand returned error: %v", err)
	}

	output := result.Stdout + result.Stderr

	if strings.Contains(output, "secret123") {
		t.Error("sensitive API key value should be filtered out")
	}
	if strings.Contains(output, "dbtoken") {
		t.Error("sensitive token value should be filtered out")
	}
	if !strings.Contains(output, "safe_value") {
		t.Error("non-sensitive variable should be present")
	}
}

func TestLocalExecEnvEnvPolicyAll(t *testing.T) {
	dir := t.TempDir()
	env := NewLocalExecutionEnvironment(dir, WithEnvPolicy(EnvPolicyInheritAll))

	envVars := map[string]string{
		"MY_API_KEY": "secret123",
		"SAFE_VAR":   "safe_value",
	}

	result, err := env.ExecCommand("env", 10000, "", envVars)
	if err != nil {
		t.Fatalf("ExecCommand returned error: %v", err)
	}

	output := result.Stdout + result.Stderr

	// InheritAll should pass everything through
	if !strings.Contains(output, "secret123") {
		t.Error("InheritAll policy should include API key value")
	}
	if !strings.Contains(output, "safe_value") {
		t.Error("InheritAll policy should include safe variable")
	}
}

func TestLocalExecEnvEnvPolicyNone(t *testing.T) {
	dir := t.TempDir()
	env := NewLocalExecutionEnvironment(dir, WithEnvPolicy(EnvPolicyInheritNone))

	envVars := map[string]string{
		"CUSTOM_VAR": "custom_value",
	}

	result, err := env.ExecCommand("env", 10000, "", envVars)
	if err != nil {
		t.Fatalf("ExecCommand returned error: %v", err)
	}

	output := result.Stdout + result.Stderr

	// InheritNone should only have explicit envVars
	if !strings.Contains(output, "custom_value") {
		t.Error("InheritNone should include explicitly passed variables")
	}

	// Should not have inherited HOME (unless it was explicitly passed)
	// Check that the output is relatively sparse (only explicit vars + minimal system vars)
	lines := strings.Split(strings.TrimSpace(output), "\n")
	// In a truly clean environment, there should be very few variables
	// We pass 1 var, but the shell itself may inject a few (PWD, SHLVL, _)
	if len(lines) > 10 {
		t.Errorf("InheritNone should have very few env vars, got %d lines", len(lines))
	}
}

func TestLocalExecEnvGrep(t *testing.T) {
	dir := t.TempDir()

	// Create test files
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("Hello World\nfoo bar\nHello Again\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "other.txt"), []byte("no match here\n"), 0644); err != nil {
		t.Fatal(err)
	}

	env := NewLocalExecutionEnvironment(dir)

	result, err := env.Grep("Hello", dir, GrepOptions{})
	if err != nil {
		t.Fatalf("Grep returned error: %v", err)
	}

	if !strings.Contains(result, "Hello World") {
		t.Error("expected grep result to contain 'Hello World'")
	}
	if !strings.Contains(result, "Hello Again") {
		t.Error("expected grep result to contain 'Hello Again'")
	}
	if strings.Contains(result, "no match here") {
		t.Error("grep should not match 'no match here'")
	}
}

func TestLocalExecEnvGlob(t *testing.T) {
	dir := t.TempDir()

	// Create test files
	for _, name := range []string{"a.txt", "b.txt", "c.go", "d.go"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("content"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	env := NewLocalExecutionEnvironment(dir)

	matches, err := env.Glob("*.txt", dir)
	if err != nil {
		t.Fatalf("Glob returned error: %v", err)
	}

	if len(matches) != 2 {
		t.Fatalf("expected 2 matches for *.txt, got %d: %v", len(matches), matches)
	}

	for _, m := range matches {
		if !strings.HasSuffix(m, ".txt") {
			t.Errorf("expected .txt file, got %s", m)
		}
	}
}

func TestLocalExecEnvInitialize(t *testing.T) {
	dir := t.TempDir()
	newDir := filepath.Join(dir, "newworkdir")

	env := NewLocalExecutionEnvironment(newDir)
	if err := env.Initialize(); err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}

	info, err := os.Stat(newDir)
	if err != nil {
		t.Fatalf("work dir should exist after Initialize: %v", err)
	}
	if !info.IsDir() {
		t.Error("work dir should be a directory")
	}
}

func TestLocalExecEnvPlatform(t *testing.T) {
	dir := t.TempDir()
	env := NewLocalExecutionEnvironment(dir)

	platform := env.Platform()
	if platform != runtime.GOOS {
		t.Errorf("expected platform %q, got %q", runtime.GOOS, platform)
	}
}

func TestLocalExecEnvWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	env := NewLocalExecutionEnvironment(dir)

	if env.WorkingDirectory() != dir {
		t.Errorf("expected working directory %q, got %q", dir, env.WorkingDirectory())
	}
}
