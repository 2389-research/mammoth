// ABOUTME: Provider profiles for the coding agent loop (OpenAI, Anthropic, Gemini).
// ABOUTME: Each profile aligns tools and system prompts to the provider's native agent conventions.

package agent

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/2389-research/makeatron/llm"
)

// ProviderProfile defines the interface for provider-specific tool and prompt configurations.
// Each profile aligns its tools and system prompts to how the provider's models work best.
type ProviderProfile interface {
	ID() string
	Model() string
	BuildSystemPrompt(env ExecutionEnvironment, projectDocs []string) string
	Tools() []llm.ToolDefinition
	ProviderOptions() map[string]any
	ToolRegistry() *ToolRegistry
	SupportsParallelToolCalls() bool
	SupportsReasoning() bool
	SupportsStreaming() bool
	ContextWindowSize() int
}

// BaseProfile provides shared implementation for all provider profiles.
type BaseProfile struct {
	id                        string
	model                     string
	registry                  *ToolRegistry
	supportsParallelToolCalls bool
	supportsReasoning         bool
	supportsStreaming          bool
	contextWindowSize         int
	providerOpts              map[string]any
}

func (b *BaseProfile) ID() string                    { return b.id }
func (b *BaseProfile) Model() string                 { return b.model }
func (b *BaseProfile) SupportsParallelToolCalls() bool { return b.supportsParallelToolCalls }
func (b *BaseProfile) SupportsReasoning() bool       { return b.supportsReasoning }
func (b *BaseProfile) SupportsStreaming() bool        { return b.supportsStreaming }
func (b *BaseProfile) ContextWindowSize() int         { return b.contextWindowSize }
func (b *BaseProfile) ToolRegistry() *ToolRegistry   { return b.registry }

// Tools returns all tool definitions from the profile's registry.
func (b *BaseProfile) Tools() []llm.ToolDefinition {
	return b.registry.Definitions()
}

// ProviderOptions returns provider-specific options for the LLM request.
func (b *BaseProfile) ProviderOptions() map[string]any {
	return b.providerOpts
}

// ProfileOption configures a BaseProfile during construction.
type ProfileOption func(*BaseProfile)

// WithProfileModel overrides the default model for a profile.
func WithProfileModel(model string) ProfileOption {
	return func(b *BaseProfile) {
		b.model = model
	}
}

// WithProfileProviderOptions sets provider-specific options on the profile.
func WithProfileProviderOptions(opts map[string]any) ProfileOption {
	return func(b *BaseProfile) {
		b.providerOpts = opts
	}
}

// buildEnvironmentContext produces the <environment> block for system prompts.
func buildEnvironmentContext(env ExecutionEnvironment) string {
	var b strings.Builder
	b.WriteString("<environment>\n")
	b.WriteString(fmt.Sprintf("Working directory: %s\n", env.WorkingDirectory()))
	b.WriteString(fmt.Sprintf("Platform: %s\n", env.Platform()))
	b.WriteString(fmt.Sprintf("OS version: %s\n", env.OSVersion()))
	b.WriteString(fmt.Sprintf("Today's date: %s\n", time.Now().Format("2006-01-02")))
	b.WriteString("</environment>\n")
	return b.String()
}

// buildProjectDocsSection formats project documentation for inclusion in the system prompt.
func buildProjectDocsSection(docs []string) string {
	if len(docs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n## Project Instructions\n\n")
	for _, doc := range docs {
		b.WriteString(doc)
		b.WriteString("\n\n")
	}
	return b.String()
}

// DiscoverProjectDocs searches the working directory for recognized project documentation files
// and returns their contents. Recognized files: CLAUDE.md, README.md, .cursorrules, GEMINI.md, AGENTS.md.
func DiscoverProjectDocs(env ExecutionEnvironment) []string {
	docFiles := []string{
		"AGENTS.md",
		"CLAUDE.md",
		"README.md",
		".cursorrules",
		"GEMINI.md",
	}

	var docs []string
	for _, name := range docFiles {
		fullPath := filepath.Join(env.WorkingDirectory(), name)
		exists, err := env.FileExists(fullPath)
		if err != nil || !exists {
			continue
		}
		content, err := env.ReadFile(fullPath, 0, 0)
		if err != nil {
			continue
		}
		if content != "" {
			docs = append(docs, content)
		}
	}
	return docs
}

// --- OpenAI Profile ---

// OpenAIProfile is a ProviderProfile aligned to OpenAI's codex-rs conventions.
type OpenAIProfile struct {
	BaseProfile
}

// NewOpenAIProfile creates an OpenAI-aligned provider profile.
// If model is empty, defaults to "gpt-5.2-codex".
func NewOpenAIProfile(model string, opts ...ProfileOption) *OpenAIProfile {
	if model == "" {
		model = "gpt-5.2-codex"
	}

	registry := NewToolRegistry()
	// Register core tools except edit_file (OpenAI uses apply_patch instead)
	registry.Register(NewReadFileTool())
	registry.Register(NewWriteFileTool())
	registry.Register(NewShellTool())
	registry.Register(NewGrepTool())
	registry.Register(NewGlobTool())
	// Register apply_patch (OpenAI-specific)
	registry.Register(NewApplyPatchTool())

	profile := &OpenAIProfile{
		BaseProfile: BaseProfile{
			id:                        "openai",
			model:                     model,
			registry:                  registry,
			supportsParallelToolCalls: true,
			supportsReasoning:         true,
			supportsStreaming:          true,
			contextWindowSize:         200000,
			providerOpts:              make(map[string]any),
		},
	}

	for _, opt := range opts {
		opt(&profile.BaseProfile)
	}

	return profile
}

// BuildSystemPrompt constructs the system prompt for OpenAI models, mirroring codex-rs conventions.
func (p *OpenAIProfile) BuildSystemPrompt(env ExecutionEnvironment, projectDocs []string) string {
	var b strings.Builder

	b.WriteString("You are a coding assistant powered by " + p.model + ". ")
	b.WriteString("You help users write, debug, and modify code by reading files, applying patches, ")
	b.WriteString("running shell commands, and searching codebases.\n\n")

	b.WriteString("## Tool Usage\n\n")
	b.WriteString("- Use `read_file` to read file contents before making changes.\n")
	b.WriteString("- Use `apply_patch` to modify existing files using the v4a patch format. ")
	b.WriteString("The patch format supports creating, deleting, and updating files.\n")
	b.WriteString("- Use `write_file` to create new files.\n")
	b.WriteString("- Use `shell` to run commands. Default timeout is 10 seconds.\n")
	b.WriteString("- Use `grep` and `glob` to search file contents and find files by pattern.\n\n")

	b.WriteString("## apply_patch Format\n\n")
	b.WriteString("Patches use the v4a format with context lines for matching:\n")
	b.WriteString("```\n")
	b.WriteString("*** Begin Patch\n")
	b.WriteString("*** Update File: path/to/file\n")
	b.WriteString("@@ context_hint\n")
	b.WriteString(" context line (space prefix)\n")
	b.WriteString("-removed line (minus prefix)\n")
	b.WriteString("+added line (plus prefix)\n")
	b.WriteString("*** End Patch\n")
	b.WriteString("```\n\n")

	b.WriteString("## Coding Best Practices\n\n")
	b.WriteString("- Read files before editing to understand existing code.\n")
	b.WriteString("- Make targeted changes; avoid rewriting entire files when small edits suffice.\n")
	b.WriteString("- Run tests after making changes to verify correctness.\n")
	b.WriteString("- Follow existing code style and conventions.\n\n")

	b.WriteString(buildEnvironmentContext(env))
	b.WriteString(buildProjectDocsSection(projectDocs))

	return b.String()
}

// Compile-time interface check
var _ ProviderProfile = (*OpenAIProfile)(nil)

// --- Anthropic Profile ---

// AnthropicProfile is a ProviderProfile aligned to Claude Code conventions.
type AnthropicProfile struct {
	BaseProfile
}

// NewAnthropicProfile creates an Anthropic-aligned provider profile.
// If model is empty, defaults to "claude-sonnet-4-5".
func NewAnthropicProfile(model string, opts ...ProfileOption) *AnthropicProfile {
	if model == "" {
		model = "claude-sonnet-4-5"
	}

	registry := NewToolRegistry()
	// Register all 6 core tools (Anthropic uses edit_file natively)
	RegisterCoreTools(registry)

	profile := &AnthropicProfile{
		BaseProfile: BaseProfile{
			id:                        "anthropic",
			model:                     model,
			registry:                  registry,
			supportsParallelToolCalls: true,
			supportsReasoning:         true,
			supportsStreaming:          true,
			contextWindowSize:         200000,
			providerOpts:              make(map[string]any),
		},
	}

	for _, opt := range opts {
		opt(&profile.BaseProfile)
	}

	return profile
}

// BuildSystemPrompt constructs the system prompt for Anthropic models, mirroring Claude Code conventions.
func (p *AnthropicProfile) BuildSystemPrompt(env ExecutionEnvironment, projectDocs []string) string {
	var b strings.Builder

	b.WriteString("You are a coding assistant powered by " + p.model + ". ")
	b.WriteString("You help users write, debug, and modify code by reading files, editing them, ")
	b.WriteString("running shell commands, and searching codebases.\n\n")

	b.WriteString("## Tool Usage\n\n")
	b.WriteString("- Use `read_file` to examine file contents before making changes.\n")
	b.WriteString("- Use `edit_file` with `old_string` and `new_string` to make targeted edits. ")
	b.WriteString("The `old_string` must be unique within the file; if it is not unique, ")
	b.WriteString("provide more surrounding context to make it unique, or use `replace_all`.\n")
	b.WriteString("- Use `write_file` to create new files. Prefer editing existing files over creating new ones.\n")
	b.WriteString("- Use `shell` to execute commands. Default timeout is 120 seconds (120000ms).\n")
	b.WriteString("- Use `grep` and `glob` to search file contents and find files by pattern.\n\n")

	b.WriteString("## Coding Best Practices\n\n")
	b.WriteString("- Always read a file before editing it to understand the existing code.\n")
	b.WriteString("- Make targeted, minimal changes rather than rewriting entire files.\n")
	b.WriteString("- Prefer editing existing files over creating new ones.\n")
	b.WriteString("- Run tests after making changes to verify correctness.\n")
	b.WriteString("- Follow existing code style and conventions in the project.\n\n")

	b.WriteString(buildEnvironmentContext(env))
	b.WriteString(buildProjectDocsSection(projectDocs))

	return b.String()
}

// Compile-time interface check
var _ ProviderProfile = (*AnthropicProfile)(nil)

// --- Gemini Profile ---

// GeminiProfile is a ProviderProfile aligned to gemini-cli conventions.
type GeminiProfile struct {
	BaseProfile
}

// NewGeminiProfile creates a Gemini-aligned provider profile.
// If model is empty, defaults to "gemini-3-flash-preview".
func NewGeminiProfile(model string, opts ...ProfileOption) *GeminiProfile {
	if model == "" {
		model = "gemini-3-flash-preview"
	}

	registry := NewToolRegistry()
	// Register all 6 core tools (Gemini uses edit_file like gemini-cli)
	RegisterCoreTools(registry)

	profile := &GeminiProfile{
		BaseProfile: BaseProfile{
			id:                        "gemini",
			model:                     model,
			registry:                  registry,
			supportsParallelToolCalls: false,
			supportsReasoning:         true,
			supportsStreaming:          true,
			contextWindowSize:         1000000,
			providerOpts:              make(map[string]any),
		},
	}

	for _, opt := range opts {
		opt(&profile.BaseProfile)
	}

	return profile
}

// BuildSystemPrompt constructs the system prompt for Gemini models, mirroring gemini-cli conventions.
func (p *GeminiProfile) BuildSystemPrompt(env ExecutionEnvironment, projectDocs []string) string {
	var b strings.Builder

	b.WriteString("You are a coding assistant powered by " + p.model + ". ")
	b.WriteString("You help users write, debug, and modify code by reading files, editing them, ")
	b.WriteString("running shell commands, and searching codebases.\n\n")

	b.WriteString("## Tool Usage\n\n")
	b.WriteString("- Use `read_file` to examine file contents before making changes.\n")
	b.WriteString("- Use `edit_file` with `old_string` and `new_string` to make targeted edits.\n")
	b.WriteString("- Use `write_file` to create new files.\n")
	b.WriteString("- Use `shell` to execute commands. Default timeout is 10 seconds.\n")
	b.WriteString("- Use `grep` and `glob` to search file contents and find files by pattern.\n\n")

	b.WriteString("## Project Configuration\n\n")
	b.WriteString("- Check for a GEMINI.md file in the project root for project-specific instructions.\n")
	b.WriteString("- GEMINI.md may contain coding conventions, architecture notes, or task-specific guidance.\n\n")

	b.WriteString("## Coding Best Practices\n\n")
	b.WriteString("- Read files before editing to understand existing code.\n")
	b.WriteString("- Make targeted changes; avoid rewriting entire files when small edits suffice.\n")
	b.WriteString("- Run tests after making changes to verify correctness.\n")
	b.WriteString("- Follow existing code style and conventions.\n\n")

	b.WriteString(buildEnvironmentContext(env))
	b.WriteString(buildProjectDocsSection(projectDocs))

	return b.String()
}

// Compile-time interface check
var _ ProviderProfile = (*GeminiProfile)(nil)

// newToolDef is a helper to create a minimal ToolDefinition for testing and custom tools.
func newToolDef(name, description string) llm.ToolDefinition {
	return llm.ToolDefinition{
		Name:        name,
		Description: description,
		Parameters:  json.RawMessage(`{"type": "object", "properties": {}}`),
	}
}
