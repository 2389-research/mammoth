// ABOUTME: Tests for provider profiles (OpenAI, Anthropic, Gemini) and profile options.
// ABOUTME: Verifies default models, tool registration, system prompts, and DiscoverProjectDocs.

package agent

import (
	"strings"
	"testing"
)

// --- OpenAI Profile Tests ---

func TestOpenAIProfileDefaults(t *testing.T) {
	profile := NewOpenAIProfile("")

	if profile.ID() != "openai" {
		t.Errorf("expected ID 'openai', got %q", profile.ID())
	}
	if profile.Model() != "gpt-5.2-codex" {
		t.Errorf("expected default model 'gpt-5.2-codex', got %q", profile.Model())
	}
	if !profile.SupportsParallelToolCalls() {
		t.Error("expected SupportsParallelToolCalls to be true")
	}
	if !profile.SupportsReasoning() {
		t.Error("expected SupportsReasoning to be true")
	}
	if !profile.SupportsStreaming() {
		t.Error("expected SupportsStreaming to be true")
	}
	if profile.ContextWindowSize() != 200000 {
		t.Errorf("expected ContextWindowSize 200000, got %d", profile.ContextWindowSize())
	}
}

func TestOpenAIProfileTools(t *testing.T) {
	profile := NewOpenAIProfile("")
	registry := profile.ToolRegistry()

	expectedTools := []string{"read_file", "write_file", "shell", "grep", "glob", "apply_patch"}
	for _, name := range expectedTools {
		if !registry.Has(name) {
			t.Errorf("expected OpenAI profile to have tool %q", name)
		}
	}

	// OpenAI profile should NOT have edit_file (uses apply_patch instead)
	if registry.Has("edit_file") {
		t.Error("OpenAI profile should not have edit_file tool (uses apply_patch instead)")
	}

	// Verify tools are also returned via Tools()
	tools := profile.Tools()
	toolNames := make(map[string]bool)
	for _, td := range tools {
		toolNames[td.Name] = true
	}
	for _, name := range expectedTools {
		if !toolNames[name] {
			t.Errorf("expected Tools() to include %q", name)
		}
	}
}

func TestOpenAIProfileSystemPrompt(t *testing.T) {
	env := newTestEnv()
	profile := NewOpenAIProfile("")

	prompt := profile.BuildSystemPrompt(env, nil)

	if prompt == "" {
		t.Fatal("expected non-empty system prompt")
	}
	if !strings.Contains(prompt, "apply_patch") {
		t.Error("expected OpenAI system prompt to mention apply_patch")
	}
	if !strings.Contains(prompt, "coding") {
		t.Error("expected OpenAI system prompt to mention coding")
	}
}

// --- Anthropic Profile Tests ---

func TestAnthropicProfileDefaults(t *testing.T) {
	profile := NewAnthropicProfile("")

	if profile.ID() != "anthropic" {
		t.Errorf("expected ID 'anthropic', got %q", profile.ID())
	}
	if profile.Model() != "claude-sonnet-4-5" {
		t.Errorf("expected default model 'claude-sonnet-4-5', got %q", profile.Model())
	}
	if !profile.SupportsParallelToolCalls() {
		t.Error("expected SupportsParallelToolCalls to be true")
	}
	if !profile.SupportsReasoning() {
		t.Error("expected SupportsReasoning to be true")
	}
	if !profile.SupportsStreaming() {
		t.Error("expected SupportsStreaming to be true")
	}
	if profile.ContextWindowSize() != 200000 {
		t.Errorf("expected ContextWindowSize 200000, got %d", profile.ContextWindowSize())
	}
}

func TestAnthropicProfileTools(t *testing.T) {
	profile := NewAnthropicProfile("")
	registry := profile.ToolRegistry()

	expectedTools := []string{"read_file", "write_file", "edit_file", "shell", "grep", "glob", "apply_patch"}
	for _, name := range expectedTools {
		if !registry.Has(name) {
			t.Errorf("expected Anthropic profile to have tool %q", name)
		}
	}
}

func TestAnthropicProfileSystemPrompt(t *testing.T) {
	env := newTestEnv()
	profile := NewAnthropicProfile("")

	prompt := profile.BuildSystemPrompt(env, nil)

	if prompt == "" {
		t.Fatal("expected non-empty system prompt")
	}
	if !strings.Contains(prompt, "edit_file") {
		t.Error("expected Anthropic system prompt to mention edit_file")
	}
	if !strings.Contains(prompt, "old_string") {
		t.Error("expected Anthropic system prompt to mention old_string")
	}
	if !strings.Contains(prompt, "coding") {
		t.Error("expected Anthropic system prompt to mention coding")
	}
}

// --- Gemini Profile Tests ---

func TestGeminiProfileDefaults(t *testing.T) {
	profile := NewGeminiProfile("")

	if profile.ID() != "gemini" {
		t.Errorf("expected ID 'gemini', got %q", profile.ID())
	}
	if profile.Model() != "gemini-3-flash-preview" {
		t.Errorf("expected default model 'gemini-3-flash-preview', got %q", profile.Model())
	}
	if profile.SupportsParallelToolCalls() {
		t.Error("expected SupportsParallelToolCalls to be false for Gemini")
	}
	if !profile.SupportsReasoning() {
		t.Error("expected SupportsReasoning to be true")
	}
	if !profile.SupportsStreaming() {
		t.Error("expected SupportsStreaming to be true")
	}
	if profile.ContextWindowSize() != 1000000 {
		t.Errorf("expected ContextWindowSize 1000000, got %d", profile.ContextWindowSize())
	}
}

func TestGeminiProfileTools(t *testing.T) {
	profile := NewGeminiProfile("")
	registry := profile.ToolRegistry()

	expectedTools := []string{"read_file", "write_file", "edit_file", "shell", "grep", "glob", "apply_patch"}
	for _, name := range expectedTools {
		if !registry.Has(name) {
			t.Errorf("expected Gemini profile to have tool %q", name)
		}
	}
}

func TestGeminiProfileSystemPrompt(t *testing.T) {
	env := newTestEnv()
	profile := NewGeminiProfile("")

	prompt := profile.BuildSystemPrompt(env, nil)

	if prompt == "" {
		t.Fatal("expected non-empty system prompt")
	}
	if !strings.Contains(prompt, "GEMINI.md") {
		t.Error("expected Gemini system prompt to mention GEMINI.md")
	}
	if !strings.Contains(prompt, "coding") {
		t.Error("expected Gemini system prompt to mention coding")
	}
}

// --- Profile Options Tests ---

func TestProfileCustomModel(t *testing.T) {
	profile := NewOpenAIProfile("", WithProfileModel("gpt-5.2"))

	if profile.Model() != "gpt-5.2" {
		t.Errorf("expected custom model 'gpt-5.2', got %q", profile.Model())
	}
}

func TestProfileProviderOptions(t *testing.T) {
	customOpts := map[string]any{
		"reasoning": map[string]any{
			"effort": "high",
		},
	}
	profile := NewOpenAIProfile("", WithProfileProviderOptions(customOpts))

	opts := profile.ProviderOptions()
	if opts == nil {
		t.Fatal("expected non-nil ProviderOptions")
	}
	reasoning, ok := opts["reasoning"]
	if !ok {
		t.Fatal("expected 'reasoning' key in provider options")
	}
	reasoningMap, ok := reasoning.(map[string]any)
	if !ok {
		t.Fatal("expected reasoning to be a map")
	}
	if reasoningMap["effort"] != "high" {
		t.Errorf("expected reasoning.effort 'high', got %v", reasoningMap["effort"])
	}
}

// --- Tool Registry Tests ---

func TestProfileToolRegistry(t *testing.T) {
	profile := NewAnthropicProfile("")
	registry := profile.ToolRegistry()

	if registry == nil {
		t.Fatal("expected non-nil ToolRegistry")
	}

	// Registry should have tools
	if registry.Count() == 0 {
		t.Error("expected registry to have tools registered")
	}

	// All tools in the registry should have execute functions
	for _, name := range registry.Names() {
		tool := registry.Get(name)
		if tool == nil {
			t.Errorf("Get(%q) returned nil", name)
			continue
		}
		if tool.Execute == nil {
			t.Errorf("tool %q has nil Execute function", name)
		}
		if tool.Definition.Name == "" {
			t.Errorf("tool %q has empty definition name", name)
		}
		if tool.Definition.Description == "" {
			t.Errorf("tool %q has empty description", name)
		}
	}
}

// --- DiscoverProjectDocs Tests ---

func TestDiscoverProjectDocs(t *testing.T) {
	env := newTestEnv()
	env.files["/tmp/test/CLAUDE.md"] = "# Claude Instructions\nDo things properly."
	env.files["/tmp/test/README.md"] = "# My Project\nA cool project."

	docs := DiscoverProjectDocs(env)

	if len(docs) == 0 {
		t.Fatal("expected DiscoverProjectDocs to find project docs")
	}

	foundClaude := false
	foundReadme := false
	for _, doc := range docs {
		if strings.Contains(doc, "Claude Instructions") {
			foundClaude = true
		}
		if strings.Contains(doc, "My Project") {
			foundReadme = true
		}
	}

	if !foundClaude {
		t.Error("expected to find CLAUDE.md content")
	}
	if !foundReadme {
		t.Error("expected to find README.md content")
	}
}

func TestDiscoverProjectDocsEmpty(t *testing.T) {
	env := newTestEnv()
	// No doc files present

	docs := DiscoverProjectDocs(env)

	if len(docs) != 0 {
		t.Errorf("expected empty docs for env with no project files, got %d", len(docs))
	}
}

// --- ApplyPatch Tool Tests ---

func TestApplyPatchTool(t *testing.T) {
	env := newTestEnv()
	env.files["/tmp/test/main.py"] = "def main():\n    print(\"Hello\")\n    return 0\n"

	tool := NewApplyPatchTool()
	if tool.Definition.Name != "apply_patch" {
		t.Errorf("expected tool name 'apply_patch', got %q", tool.Definition.Name)
	}

	patch := `*** Begin Patch
*** Update File: /tmp/test/main.py
@@ def main():
     print("Hello")
-    return 0
+    print("World")
+    return 1
*** End Patch`

	result, err := tool.Execute(map[string]any{
		"patch": patch,
	}, env)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if !strings.Contains(result, "main.py") {
		t.Errorf("expected result to mention main.py, got: %s", result)
	}

	content := env.files["/tmp/test/main.py"]
	if !strings.Contains(content, "print(\"World\")") {
		t.Errorf("expected patched file to contain print(\"World\"), got:\n%s", content)
	}
	if !strings.Contains(content, "return 1") {
		t.Errorf("expected patched file to contain 'return 1', got:\n%s", content)
	}
	if strings.Contains(content, "return 0") {
		t.Errorf("expected 'return 0' to be removed, still present in:\n%s", content)
	}
}

func TestApplyPatchToolAddFile(t *testing.T) {
	env := newTestEnv()

	tool := NewApplyPatchTool()

	patch := `*** Begin Patch
*** Add File: /tmp/test/newfile.py
+def greet(name):
+    return f"Hello, {name}!"
*** End Patch`

	result, err := tool.Execute(map[string]any{
		"patch": patch,
	}, env)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if !strings.Contains(result, "newfile.py") {
		t.Errorf("expected result to mention newfile.py, got: %s", result)
	}

	content, ok := env.files["/tmp/test/newfile.py"]
	if !ok {
		t.Fatal("expected newfile.py to be created")
	}
	if !strings.Contains(content, "def greet(name):") {
		t.Errorf("expected file to contain 'def greet(name):', got:\n%s", content)
	}
}

func TestApplyPatchToolDeleteFile(t *testing.T) {
	env := newTestEnv()
	env.files["/tmp/test/old.py"] = "old content"

	tool := NewApplyPatchTool()

	patch := `*** Begin Patch
*** Delete File: /tmp/test/old.py
*** End Patch`

	result, err := tool.Execute(map[string]any{
		"patch": patch,
	}, env)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if !strings.Contains(result, "old.py") {
		t.Errorf("expected result to mention old.py, got: %s", result)
	}

	// The ExecutionEnvironment interface does not have a Delete method,
	// so deletion is implemented by writing empty content.
	content := env.files["/tmp/test/old.py"]
	if content != "" {
		t.Errorf("expected old.py content to be empty after deletion, got: %q", content)
	}

	if !strings.Contains(result, "Deleted") {
		t.Error("expected result to indicate file was deleted")
	}
}

// --- System prompt includes project docs ---

func TestSystemPromptIncludesProjectDocs(t *testing.T) {
	env := newTestEnv()
	docs := []string{"# Project Rules\nAlways write tests."}

	profile := NewAnthropicProfile("")
	prompt := profile.BuildSystemPrompt(env, docs)

	if !strings.Contains(prompt, "Project Rules") {
		t.Error("expected system prompt to include project doc content")
	}
	if !strings.Contains(prompt, "Always write tests") {
		t.Error("expected system prompt to include project doc details")
	}
}

// --- System prompt includes environment context ---

func TestSystemPromptIncludesEnvironmentContext(t *testing.T) {
	env := newTestEnv()
	env.workDir = "/home/user/myproject"
	env.platform = "darwin"

	profile := NewAnthropicProfile("")
	prompt := profile.BuildSystemPrompt(env, nil)

	if !strings.Contains(prompt, "/home/user/myproject") {
		t.Error("expected system prompt to include working directory")
	}
	if !strings.Contains(prompt, "darwin") {
		t.Error("expected system prompt to include platform")
	}
}

// --- Custom tool registration on profile ---

func TestProfileCustomToolRegistration(t *testing.T) {
	profile := NewAnthropicProfile("")
	registry := profile.ToolRegistry()

	// Register a custom tool
	customTool := &RegisteredTool{
		Definition: newToolDef("custom_tool", "A custom test tool"),
		Execute: func(args map[string]any, env ExecutionEnvironment) (string, error) {
			return "custom result", nil
		},
	}
	err := registry.Register(customTool)
	if err != nil {
		t.Fatalf("failed to register custom tool: %v", err)
	}

	if !registry.Has("custom_tool") {
		t.Error("expected registry to have custom_tool after registration")
	}

	// Original tools should still be there
	if !registry.Has("read_file") {
		t.Error("expected registry to still have read_file after custom registration")
	}
}
