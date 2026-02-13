// ABOUTME: Tool registry for the coding agent loop, managing registration, lookup, and output truncation.
// ABOUTME: Provides ToolRegistry, RegisteredTool, TruncateOutput, and TruncateToolOutput functions.

package agent

import (
	"fmt"
	"strings"
	"sync"

	"github.com/2389-research/mammoth/llm"
)

// RegisteredTool pairs a tool definition with its execute function.
type RegisteredTool struct {
	Definition  llm.ToolDefinition
	Execute     func(args map[string]any, env ExecutionEnvironment) (string, error)
	Description string
}

// ToolRegistry manages a thread-safe collection of registered tools.
type ToolRegistry struct {
	tools map[string]*RegisteredTool
	mu    sync.RWMutex
}

// NewToolRegistry creates an empty ToolRegistry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]*RegisteredTool),
	}
}

// Register adds or replaces a tool in the registry. Returns an error if
// the tool's definition has an empty name.
func (r *ToolRegistry) Register(tool *RegisteredTool) error {
	if tool.Definition.Name == "" {
		return fmt.Errorf("tool name must not be empty")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[tool.Definition.Name] = tool
	return nil
}

// Unregister removes a tool by name. Returns true if the tool existed.
func (r *ToolRegistry) Unregister(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.tools[name]; ok {
		delete(r.tools, name)
		return true
	}
	return false
}

// Get returns the registered tool with the given name, or nil if not found.
func (r *ToolRegistry) Get(name string) *RegisteredTool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tools[name]
}

// Definitions returns all tool definitions from registered tools.
func (r *ToolRegistry) Definitions() []llm.ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	defs := make([]llm.ToolDefinition, 0, len(r.tools))
	for _, tool := range r.tools {
		defs = append(defs, tool.Definition)
	}
	return defs
}

// Has returns true if a tool with the given name is registered.
func (r *ToolRegistry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.tools[name]
	return ok
}

// Names returns the names of all registered tools.
func (r *ToolRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return names
}

// Count returns the number of registered tools.
func (r *ToolRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools)
}

// defaultToolLimits maps tool names to their default character limits.
var defaultToolLimits = map[string]int{
	"read_file":  50000,
	"shell":      30000,
	"grep":       20000,
	"glob":       20000,
	"edit_file":  10000,
	"write_file": 1000,
}

// defaultToolModes maps tool names to their truncation mode ("head_tail" or "tail").
var defaultToolModes = map[string]string{
	"read_file":  "head_tail",
	"shell":      "head_tail",
	"grep":       "tail",
	"glob":       "tail",
	"edit_file":  "tail",
	"write_file": "tail",
}

// defaultCharLimit is used for tools not listed in defaultToolLimits.
const defaultCharLimit = 30000

// DefaultLineLimits maps tool names to their default line-count limits.
// A value of 0 means unlimited (no line-based truncation).
var DefaultLineLimits = map[string]int{
	"shell": 256,
	"grep":  200,
	"glob":  500,
}

// TruncateLines truncates output that exceeds maxLines using a head/tail split.
// If maxLines is 0 or the output has fewer lines than maxLines, the output is
// returned unchanged. Otherwise the first half and last half of lines are kept
// with an omission marker in between.
func TruncateLines(output string, maxLines int) string {
	if maxLines <= 0 {
		return output
	}

	lines := strings.Split(output, "\n")
	if len(lines) <= maxLines {
		return output
	}

	headCount := maxLines / 2
	tailCount := maxLines - headCount
	omitted := len(lines) - headCount - tailCount

	return strings.Join(lines[:headCount], "\n") +
		fmt.Sprintf("\n[... %d lines omitted ...]\n", omitted) +
		strings.Join(lines[len(lines)-tailCount:], "\n")
}

// TruncateOutput truncates output that exceeds maxChars using the given mode.
// Supported modes: "head_tail" (keep first half + last half) and "tail" (keep last N chars).
// A truncation warning is inserted at the truncation point.
func TruncateOutput(output string, maxChars int, mode string) string {
	if len(output) <= maxChars {
		return output
	}

	removed := len(output) - maxChars

	if mode == "head_tail" {
		half := maxChars / 2
		return output[:half] +
			fmt.Sprintf("\n\n[WARNING: Tool output was truncated. %d characters were removed from the middle. "+
				"The full output is available in the event stream. "+
				"If you need to see specific parts, re-run the tool with more targeted parameters.]\n\n", removed) +
			output[len(output)-half:]
	}

	// Default to "tail" mode
	return fmt.Sprintf("[WARNING: Tool output was truncated. First %d characters were removed. "+
		"The full output is available in the event stream.]\n\n", removed) +
		output[len(output)-maxChars:]
}

// TruncateToolOutput truncates tool output using per-tool defaults, optionally
// overridden by the limits map. Tools not found in defaults or overrides use
// defaultCharLimit with "tail" mode. Character truncation runs first, then
// line-based truncation is applied for tools that have a configured line limit.
func TruncateToolOutput(output, toolName string, limits map[string]int) string {
	// Determine the character limit: override -> default -> fallback
	maxChars := defaultCharLimit
	if defaultLimit, ok := defaultToolLimits[toolName]; ok {
		maxChars = defaultLimit
	}
	if limits != nil {
		if override, ok := limits[toolName]; ok {
			maxChars = override
		}
	}

	// Determine truncation mode
	mode := "tail"
	if m, ok := defaultToolModes[toolName]; ok {
		mode = m
	}

	// Step 1: Character-based truncation (always runs first)
	result := TruncateOutput(output, maxChars, mode)

	// Step 2: Line-based truncation (runs second for tools with a configured limit)
	if maxLines, ok := DefaultLineLimits[toolName]; ok && maxLines > 0 {
		result = TruncateLines(result, maxLines)
	}

	return result
}
