// ABOUTME: Tests for enhanced system prompt construction, git context, tool descriptions, and project doc filtering.
// ABOUTME: Uses steeringTestEnv to verify BuildGitContext, BuildEnvironmentBlock, BuildToolDescriptions, FilterProjectDocs, DiscoverProjectDocsWalk, and BuildFullSystemPrompt.

package agent

import (
	"fmt"
	"strings"
	"testing"
)

// steeringTestEnv is a real implementation of ExecutionEnvironment for steering tests.
// It stores files in-memory and allows injecting command results for git operations.
type steeringTestEnv struct {
	files          map[string]string
	workDir        string
	platform       string
	osVersion      string
	commandResults map[string]*ExecResult
}

func newSteeringTestEnv() *steeringTestEnv {
	return &steeringTestEnv{
		files:          make(map[string]string),
		workDir:        "/tmp/steering-test",
		platform:       "linux",
		osVersion:      "6.1.0-test",
		commandResults: make(map[string]*ExecResult),
	}
}

func (e *steeringTestEnv) ReadFile(path string, offset, limit int) (string, error) {
	content, ok := e.files[path]
	if !ok {
		return "", fmt.Errorf("file not found: %s", path)
	}
	lines := strings.Split(content, "\n")
	startLine := 0
	if offset > 0 {
		startLine = offset - 1
	}
	if startLine >= len(lines) {
		return "", nil
	}
	endLine := len(lines)
	if limit > 0 && startLine+limit < endLine {
		endLine = startLine + limit
	}
	return strings.Join(lines[startLine:endLine], "\n"), nil
}

func (e *steeringTestEnv) WriteFile(path string, content string) error {
	e.files[path] = content
	return nil
}

func (e *steeringTestEnv) FileExists(path string) (bool, error) {
	_, ok := e.files[path]
	return ok, nil
}

func (e *steeringTestEnv) ListDirectory(path string, depth int) ([]DirEntry, error) {
	return nil, nil
}

func (e *steeringTestEnv) ExecCommand(command string, timeoutMs int, workingDir string, envVars map[string]string) (*ExecResult, error) {
	if result, ok := e.commandResults[command]; ok {
		return result, nil
	}
	// Default: command not found / fails
	return &ExecResult{
		Stdout:     "",
		Stderr:     "command not configured",
		ExitCode:   1,
		DurationMs: 1,
	}, nil
}

func (e *steeringTestEnv) Grep(pattern, path string, opts GrepOptions) (string, error) {
	return "", nil
}

func (e *steeringTestEnv) Glob(pattern, path string) ([]string, error) {
	return nil, nil
}

func (e *steeringTestEnv) Initialize() error        { return nil }
func (e *steeringTestEnv) Cleanup() error           { return nil }
func (e *steeringTestEnv) WorkingDirectory() string { return e.workDir }
func (e *steeringTestEnv) Platform() string         { return e.platform }
func (e *steeringTestEnv) OSVersion() string        { return e.osVersion }

// --- BuildGitContext Tests ---

func TestBuildGitContext_InRepo(t *testing.T) {
	env := newSteeringTestEnv()
	env.commandResults["git rev-parse --is-inside-work-tree"] = &ExecResult{
		Stdout: "true\n", ExitCode: 0, DurationMs: 5,
	}
	env.commandResults["git branch --show-current"] = &ExecResult{
		Stdout: "main\n", ExitCode: 0, DurationMs: 5,
	}
	env.commandResults["git status --short"] = &ExecResult{
		Stdout: " M file.go\n?? new.txt\n", ExitCode: 0, DurationMs: 5,
	}
	env.commandResults["git log --oneline -5"] = &ExecResult{
		Stdout: "abc1234 fix: resolve bug\ndef5678 feat: add feature\n", ExitCode: 0, DurationMs: 5,
	}

	result := BuildGitContext(env)

	if result == "" {
		t.Fatal("expected non-empty git context for a repo")
	}
	if !strings.Contains(result, "main") {
		t.Errorf("expected git context to contain branch name 'main', got:\n%s", result)
	}
	if !strings.Contains(result, "file.go") {
		t.Errorf("expected git context to contain status info, got:\n%s", result)
	}
	if !strings.Contains(result, "abc1234") {
		t.Errorf("expected git context to contain recent commits, got:\n%s", result)
	}
	if !strings.Contains(result, "Is git repo: true") {
		t.Errorf("expected git context to contain 'Is git repo: true', got:\n%s", result)
	}
}

func TestBuildGitContext_NotRepo(t *testing.T) {
	env := newSteeringTestEnv()
	// git rev-parse fails because not in a repo
	env.commandResults["git rev-parse --is-inside-work-tree"] = &ExecResult{
		Stdout: "", Stderr: "fatal: not a git repository", ExitCode: 128, DurationMs: 5,
	}

	result := BuildGitContext(env)

	if result != "" {
		t.Errorf("expected empty git context for non-repo, got:\n%s", result)
	}
}

// --- BuildEnvironmentBlock Tests ---

func TestBuildEnvironmentBlock(t *testing.T) {
	env := newSteeringTestEnv()
	env.workDir = "/home/user/project"
	env.platform = "darwin"
	env.osVersion = "Darwin 24.0.0"

	// Set up git context
	env.commandResults["git rev-parse --is-inside-work-tree"] = &ExecResult{
		Stdout: "true\n", ExitCode: 0, DurationMs: 5,
	}
	env.commandResults["git branch --show-current"] = &ExecResult{
		Stdout: "feature/cool\n", ExitCode: 0, DurationMs: 5,
	}
	env.commandResults["git status --short"] = &ExecResult{
		Stdout: "", ExitCode: 0, DurationMs: 5,
	}
	env.commandResults["git log --oneline -5"] = &ExecResult{
		Stdout: "aaa1111 initial commit\n", ExitCode: 0, DurationMs: 5,
	}

	result := BuildEnvironmentBlock(env, "claude-sonnet-4-5", "April 2025")

	if !strings.Contains(result, "/home/user/project") {
		t.Errorf("expected block to contain working directory, got:\n%s", result)
	}
	if !strings.Contains(result, "darwin") {
		t.Errorf("expected block to contain platform, got:\n%s", result)
	}
	if !strings.Contains(result, "Darwin 24.0.0") {
		t.Errorf("expected block to contain OS version, got:\n%s", result)
	}
	if !strings.Contains(result, "claude-sonnet-4-5") {
		t.Errorf("expected block to contain model name, got:\n%s", result)
	}
	if !strings.Contains(result, "April 2025") {
		t.Errorf("expected block to contain knowledge cutoff, got:\n%s", result)
	}
	if !strings.Contains(result, "feature/cool") {
		t.Errorf("expected block to contain git branch, got:\n%s", result)
	}
}

// --- BuildToolDescriptions Tests ---

func TestBuildToolDescriptions(t *testing.T) {
	registry := NewToolRegistry()
	registry.Register(&RegisteredTool{
		Definition:  newToolDef("read_file", "Read a file from the filesystem"),
		Description: "Read a file from the filesystem",
		Execute: func(args map[string]any, env ExecutionEnvironment) (string, error) {
			return "", nil
		},
	})
	registry.Register(&RegisteredTool{
		Definition:  newToolDef("shell", "Execute a shell command"),
		Description: "Execute a shell command",
		Execute: func(args map[string]any, env ExecutionEnvironment) (string, error) {
			return "", nil
		},
	})

	result := BuildToolDescriptions(registry)

	if result == "" {
		t.Fatal("expected non-empty tool descriptions")
	}
	if !strings.Contains(result, "read_file") {
		t.Errorf("expected tool descriptions to contain 'read_file', got:\n%s", result)
	}
	if !strings.Contains(result, "shell") {
		t.Errorf("expected tool descriptions to contain 'shell', got:\n%s", result)
	}
	if !strings.Contains(result, "Read a file") {
		t.Errorf("expected tool descriptions to contain tool description text, got:\n%s", result)
	}
}

// --- FilterProjectDocs Tests ---

func TestFilterProjectDocs_Anthropic(t *testing.T) {
	docs := map[string]string{
		"AGENTS.md":    "# Agent instructions",
		"CLAUDE.md":    "# Claude-specific rules",
		"GEMINI.md":    "# Gemini-specific rules",
		"README.md":    "# Project readme",
		".cursorrules": "cursor rules here",
	}

	result := FilterProjectDocs(docs, "anthropic")

	joined := strings.Join(result, "\n")

	if !strings.Contains(joined, "Agent instructions") {
		t.Error("expected AGENTS.md content to be included for anthropic")
	}
	if !strings.Contains(joined, "Claude-specific rules") {
		t.Error("expected CLAUDE.md content to be included for anthropic")
	}
	if strings.Contains(joined, "Gemini-specific rules") {
		t.Error("expected GEMINI.md content to be excluded for anthropic")
	}
	if !strings.Contains(joined, "Project readme") {
		t.Error("expected README.md content to be included for anthropic")
	}
	if !strings.Contains(joined, "cursor rules here") {
		t.Error("expected .cursorrules content to be included for anthropic")
	}
}

func TestFilterProjectDocs_OpenAI(t *testing.T) {
	docs := map[string]string{
		"AGENTS.md":              "# Agent instructions",
		"CLAUDE.md":              "# Claude-specific rules",
		".codex/instructions.md": "# Codex instructions",
		"README.md":              "# Project readme",
		".cursorrules":           "cursor rules here",
	}

	result := FilterProjectDocs(docs, "openai")

	joined := strings.Join(result, "\n")

	if !strings.Contains(joined, "Agent instructions") {
		t.Error("expected AGENTS.md content to be included for openai")
	}
	if !strings.Contains(joined, "Codex instructions") {
		t.Error("expected .codex/instructions.md content to be included for openai")
	}
	if strings.Contains(joined, "Claude-specific rules") {
		t.Error("expected CLAUDE.md content to be excluded for openai")
	}
	if !strings.Contains(joined, "Project readme") {
		t.Error("expected README.md content to be included for openai")
	}
	if !strings.Contains(joined, "cursor rules here") {
		t.Error("expected .cursorrules content to be included for openai")
	}
}

func TestFilterProjectDocs_Gemini(t *testing.T) {
	docs := map[string]string{
		"AGENTS.md":    "# Agent instructions",
		"CLAUDE.md":    "# Claude-specific rules",
		"GEMINI.md":    "# Gemini-specific rules",
		"README.md":    "# Project readme",
		".cursorrules": "cursor rules here",
	}

	result := FilterProjectDocs(docs, "gemini")

	joined := strings.Join(result, "\n")

	if !strings.Contains(joined, "Agent instructions") {
		t.Error("expected AGENTS.md content to be included for gemini")
	}
	if !strings.Contains(joined, "Gemini-specific rules") {
		t.Error("expected GEMINI.md content to be included for gemini")
	}
	if strings.Contains(joined, "Claude-specific rules") {
		t.Error("expected CLAUDE.md content to be excluded for gemini")
	}
	if !strings.Contains(joined, "Project readme") {
		t.Error("expected README.md content to be included for gemini")
	}
	if !strings.Contains(joined, "cursor rules here") {
		t.Error("expected .cursorrules content to be included for gemini")
	}
}

func TestFilterProjectDocs_BudgetLimit(t *testing.T) {
	// Create a doc that exceeds 32KB
	bigContent := strings.Repeat("A", 40000)
	docs := map[string]string{
		"AGENTS.md": bigContent,
	}

	result := FilterProjectDocs(docs, "anthropic")

	joined := strings.Join(result, "\n")
	totalSize := len(joined)

	// Total should not exceed 32KB + truncation marker
	if totalSize > 33000 {
		t.Errorf("expected filtered docs to be within budget, got %d bytes", totalSize)
	}
	if !strings.Contains(joined, "[TRUNCATED") {
		t.Errorf("expected truncation marker when exceeding budget, got:\n%s", joined[len(joined)-100:])
	}
}

// --- DiscoverProjectDocsWalk Tests ---

func TestDiscoverProjectDocsWalk(t *testing.T) {
	env := newSteeringTestEnv()
	env.workDir = "/home/user/project/subdir"

	// Git root is the parent of workDir
	env.commandResults["git rev-parse --show-toplevel"] = &ExecResult{
		Stdout: "/home/user/project\n", ExitCode: 0, DurationMs: 5,
	}

	// Files at git root level
	env.files["/home/user/project/AGENTS.md"] = "# Root agent instructions"
	env.files["/home/user/project/README.md"] = "# Root readme"

	// Files at working directory level (deeper = higher precedence)
	env.files["/home/user/project/subdir/CLAUDE.md"] = "# Subdir claude rules"

	result := DiscoverProjectDocsWalk(env)

	if result == nil {
		t.Fatal("expected non-nil result from DiscoverProjectDocsWalk")
	}

	if _, ok := result["AGENTS.md"]; !ok {
		t.Error("expected to find AGENTS.md from git root")
	}
	if _, ok := result["README.md"]; !ok {
		t.Error("expected to find README.md from git root")
	}
	if content, ok := result["CLAUDE.md"]; !ok {
		t.Error("expected to find CLAUDE.md from subdir")
	} else if !strings.Contains(content, "Subdir claude rules") {
		t.Errorf("expected CLAUDE.md from subdir to have deeper content, got: %s", content)
	}
}

// --- BuildFullSystemPrompt Tests ---

func TestBuildFullSystemPrompt(t *testing.T) {
	env := newSteeringTestEnv()
	env.workDir = "/home/user/project"
	env.platform = "darwin"
	env.osVersion = "Darwin 24.0.0"

	// Set up git context
	env.commandResults["git rev-parse --is-inside-work-tree"] = &ExecResult{
		Stdout: "true\n", ExitCode: 0, DurationMs: 5,
	}
	env.commandResults["git branch --show-current"] = &ExecResult{
		Stdout: "main\n", ExitCode: 0, DurationMs: 5,
	}
	env.commandResults["git status --short"] = &ExecResult{
		Stdout: "", ExitCode: 0, DurationMs: 5,
	}
	env.commandResults["git log --oneline -5"] = &ExecResult{
		Stdout: "abc1234 initial commit\n", ExitCode: 0, DurationMs: 5,
	}
	env.commandResults["git rev-parse --show-toplevel"] = &ExecResult{
		Stdout: "/home/user/project\n", ExitCode: 0, DurationMs: 5,
	}

	// Add project docs
	env.files["/home/user/project/CLAUDE.md"] = "# Claude instructions\nFollow these rules."
	env.files["/home/user/project/README.md"] = "# My Project\nA cool project."

	profile := NewAnthropicProfile("")
	userOverride := "Always be concise."

	result := BuildFullSystemPrompt(profile, env, userOverride)

	if result == "" {
		t.Fatal("expected non-empty full system prompt")
	}

	// Layer 1: Provider-specific base instructions
	if !strings.Contains(result, "coding assistant") {
		t.Error("expected full prompt to contain provider base instructions")
	}

	// Layer 2: Environment context with git info
	if !strings.Contains(result, "/home/user/project") {
		t.Error("expected full prompt to contain working directory")
	}
	if !strings.Contains(result, "darwin") {
		t.Error("expected full prompt to contain platform")
	}

	// Layer 3: Tool descriptions
	if !strings.Contains(result, "read_file") {
		t.Error("expected full prompt to contain tool descriptions")
	}

	// Layer 4: Project-specific instructions
	if !strings.Contains(result, "Claude instructions") {
		t.Error("expected full prompt to contain project docs")
	}

	// Layer 5: User override
	if !strings.Contains(result, "Always be concise.") {
		t.Error("expected full prompt to contain user override")
	}
}
