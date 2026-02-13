// ABOUTME: Enhanced system prompt construction with 5-layer assembly for the coding agent loop.
// ABOUTME: Provides git context, environment blocks, tool descriptions, project doc filtering, and full prompt building.

package agent

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// maxProjectDocsBudget is the maximum total byte size for project documentation
// included in the system prompt. Content exceeding this budget is truncated.
const maxProjectDocsBudget = 32 * 1024

// recognizedDocFiles lists the filenames that are recognized as project documentation.
var recognizedDocFiles = []string{
	"AGENTS.md",
	"CLAUDE.md",
	"README.md",
	".cursorrules",
	"GEMINI.md",
	".codex/instructions.md",
}

// providerDocFiles maps provider IDs to their provider-specific doc files.
// Files not listed here are either universal (AGENTS.md, README.md, .cursorrules)
// or belong to a different provider and should be excluded.
var providerDocFiles = map[string][]string{
	"anthropic": {"CLAUDE.md"},
	"openai":    {".codex/instructions.md"},
	"gemini":    {"GEMINI.md"},
}

// universalDocFiles are always included regardless of provider.
var universalDocFiles = []string{
	"AGENTS.md",
	"README.md",
	".cursorrules",
}

// BuildGitContext returns a git context block with branch, is_repo flag, status summary,
// and recent commits. Uses the ExecutionEnvironment to run git commands.
// Returns empty string if not in a git repo.
func BuildGitContext(env ExecutionEnvironment) string {
	// Check if we're inside a git repo
	result, err := env.ExecCommand("git rev-parse --is-inside-work-tree", 5000, "", nil)
	if err != nil || result.ExitCode != 0 {
		return ""
	}
	isRepo := strings.TrimSpace(result.Stdout)
	if isRepo != "true" {
		return ""
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Is git repo: %s\n", isRepo))

	// Get current branch
	branchResult, err := env.ExecCommand("git branch --show-current", 5000, "", nil)
	if err == nil && branchResult.ExitCode == 0 {
		branch := strings.TrimSpace(branchResult.Stdout)
		if branch != "" {
			b.WriteString(fmt.Sprintf("Git branch: %s\n", branch))
		}
	}

	// Get status summary
	statusResult, err := env.ExecCommand("git status --short", 5000, "", nil)
	if err == nil && statusResult.ExitCode == 0 {
		status := strings.TrimSpace(statusResult.Stdout)
		if status != "" {
			b.WriteString(fmt.Sprintf("Git status:\n%s\n", status))
		}
	}

	// Get recent commits
	logResult, err := env.ExecCommand("git log --oneline -5", 5000, "", nil)
	if err == nil && logResult.ExitCode == 0 {
		log := strings.TrimSpace(logResult.Stdout)
		if log != "" {
			b.WriteString(fmt.Sprintf("Recent commits:\n%s\n", log))
		}
	}

	return b.String()
}

// BuildEnvironmentBlock builds a complete <environment> block including git context.
// This is the enhanced version that includes git info, model name, and knowledge cutoff.
func BuildEnvironmentBlock(env ExecutionEnvironment, modelName string, knowledgeCutoff string) string {
	var b strings.Builder
	b.WriteString("<environment>\n")
	b.WriteString(fmt.Sprintf("Working directory: %s\n", env.WorkingDirectory()))
	b.WriteString(fmt.Sprintf("Platform: %s\n", env.Platform()))
	b.WriteString(fmt.Sprintf("OS version: %s\n", env.OSVersion()))
	b.WriteString(fmt.Sprintf("Today's date: %s\n", time.Now().Format("2006-01-02")))

	if modelName != "" {
		b.WriteString(fmt.Sprintf("Model: %s\n", modelName))
	}
	if knowledgeCutoff != "" {
		b.WriteString(fmt.Sprintf("Knowledge cutoff: %s\n", knowledgeCutoff))
	}

	gitContext := BuildGitContext(env)
	if gitContext != "" {
		b.WriteString(gitContext)
	}

	b.WriteString("</environment>\n")
	return b.String()
}

// BuildToolDescriptions returns a formatted summary of available tools for the system prompt.
func BuildToolDescriptions(registry *ToolRegistry) string {
	if registry == nil || registry.Count() == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("## Available Tools\n\n")

	// Get tool names and sort for deterministic output
	names := registry.Names()
	sort.Strings(names)

	for _, name := range names {
		tool := registry.Get(name)
		if tool == nil {
			continue
		}
		desc := tool.Description
		if desc == "" {
			desc = tool.Definition.Description
		}
		b.WriteString(fmt.Sprintf("- `%s`: %s\n", name, desc))
	}
	b.WriteString("\n")

	return b.String()
}

// FilterProjectDocs filters discovered project docs based on the provider profile ID.
// AGENTS.md is always included. Provider-specific files:
// - "openai": includes .codex/instructions.md
// - "anthropic": includes CLAUDE.md
// - "gemini": includes GEMINI.md
// All providers include README.md and .cursorrules.
// Applies 32KB total byte budget with truncation marker.
func FilterProjectDocs(docs map[string]string, providerID string) []string {
	if len(docs) == 0 {
		return nil
	}

	// Build the set of allowed filenames for this provider
	allowed := make(map[string]bool)
	for _, f := range universalDocFiles {
		allowed[f] = true
	}
	if providerFiles, ok := providerDocFiles[providerID]; ok {
		for _, f := range providerFiles {
			allowed[f] = true
		}
	}

	// Collect docs in a deterministic order: universal first, then provider-specific
	var orderedKeys []string
	for _, f := range universalDocFiles {
		if _, exists := docs[f]; exists && allowed[f] {
			orderedKeys = append(orderedKeys, f)
		}
	}
	if providerFiles, ok := providerDocFiles[providerID]; ok {
		for _, f := range providerFiles {
			if _, exists := docs[f]; exists {
				orderedKeys = append(orderedKeys, f)
			}
		}
	}

	// Assemble filtered content with budget enforcement
	var result []string
	totalSize := 0

	for _, key := range orderedKeys {
		content := docs[key]
		contentSize := len(content)

		if totalSize+contentSize > maxProjectDocsBudget {
			// Truncate this doc to fit within budget
			remaining := maxProjectDocsBudget - totalSize
			if remaining > 0 {
				truncated := content[:remaining]
				truncated += "\n[TRUNCATED: Content exceeded 32KB budget]"
				result = append(result, truncated)
			}
			break
		}

		result = append(result, content)
		totalSize += contentSize
	}

	return result
}

// DiscoverProjectDocsWalk searches from gitRoot (or working directory) down to cwd,
// collecting recognized instruction files at each level. Deeper files have higher precedence.
// Returns a map of filename->content for all discovered files.
func DiscoverProjectDocsWalk(env ExecutionEnvironment) map[string]string {
	docs := make(map[string]string)
	workDir := env.WorkingDirectory()

	// Determine git root
	gitRoot := workDir
	result, err := env.ExecCommand("git rev-parse --show-toplevel", 5000, "", nil)
	if err == nil && result.ExitCode == 0 {
		trimmed := strings.TrimSpace(result.Stdout)
		if trimmed != "" {
			gitRoot = trimmed
		}
	}

	// Build the list of directories to walk: from gitRoot to workDir
	dirs := buildDirPath(gitRoot, workDir)

	// Walk each directory level, deeper entries overwrite shallower ones
	for _, dir := range dirs {
		for _, docFile := range recognizedDocFiles {
			fullPath := filepath.Join(dir, docFile)
			exists, err := env.FileExists(fullPath)
			if err != nil || !exists {
				continue
			}
			content, err := env.ReadFile(fullPath, 0, 0)
			if err != nil {
				continue
			}
			if content != "" {
				docs[docFile] = content
			}
		}
	}

	return docs
}

// buildDirPath returns a slice of directories from root to target (inclusive).
// If target is not under root, returns just the target directory.
func buildDirPath(root, target string) []string {
	// Clean and normalize paths
	root = filepath.Clean(root)
	target = filepath.Clean(target)

	if root == target {
		return []string{root}
	}

	// Check that target is under root
	if !strings.HasPrefix(target, root+string(filepath.Separator)) {
		return []string{target}
	}

	// Build path from root to target
	var dirs []string
	dirs = append(dirs, root)

	relative := strings.TrimPrefix(target, root+string(filepath.Separator))
	parts := strings.Split(relative, string(filepath.Separator))
	current := root
	for _, part := range parts {
		current = filepath.Join(current, part)
		dirs = append(dirs, current)
	}

	return dirs
}

// BuildFullSystemPrompt assembles the complete 5-layer system prompt.
// It takes the profile's base prompt and enhances it with git context, tool descriptions,
// filtered project docs, and user overrides.
//
// The 5 layers are:
//  1. Provider-specific base instructions (from profile.BuildSystemPrompt)
//  2. Environment context with git info, model name, and knowledge cutoff
//  3. Tool descriptions from the registry
//  4. Project-specific instructions (filtered by provider)
//  5. User instructions override
func BuildFullSystemPrompt(profile ProviderProfile, env ExecutionEnvironment, userOverride string) string {
	// Layer 4: Discover and filter project docs
	rawDocs := DiscoverProjectDocsWalk(env)
	filteredDocs := FilterProjectDocs(rawDocs, profile.ID())

	// Layer 1: Provider-specific base instructions (includes basic environment context
	// and project docs via the profile's own BuildSystemPrompt)
	var b strings.Builder
	basePrompt := profile.BuildSystemPrompt(env, filteredDocs)
	b.WriteString(basePrompt)

	// Layer 3: Tool descriptions
	toolDescriptions := BuildToolDescriptions(profile.ToolRegistry())
	if toolDescriptions != "" {
		b.WriteString(toolDescriptions)
	}

	// Layer 5: User override
	if userOverride != "" {
		b.WriteString("\n## User Instructions\n\n")
		b.WriteString(userOverride)
		b.WriteString("\n")
	}

	return b.String()
}
