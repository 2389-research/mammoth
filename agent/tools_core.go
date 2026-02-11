// ABOUTME: Shared core tool constructors for the coding agent loop (read_file, write_file, edit_file, shell, grep, glob).
// ABOUTME: Each constructor returns a RegisteredTool that delegates to the ExecutionEnvironment interface.

package agent

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/2389-research/mammoth/llm"
)

// getStringArg extracts a string argument from a map, returning an error if missing or wrong type.
func getStringArg(args map[string]any, key string, required bool) (string, error) {
	val, ok := args[key]
	if !ok || val == nil {
		if required {
			return "", fmt.Errorf("missing required parameter: %s", key)
		}
		return "", nil
	}
	s, ok := val.(string)
	if !ok {
		return "", fmt.Errorf("parameter %s must be a string, got %T", key, val)
	}
	return s, nil
}

// getIntArg extracts an integer argument from a map, handling JSON float64 encoding.
func getIntArg(args map[string]any, key string, defaultVal int) (int, error) {
	val, ok := args[key]
	if !ok || val == nil {
		return defaultVal, nil
	}
	switch v := val.(type) {
	case float64:
		return int(v), nil
	case int:
		return v, nil
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			return 0, fmt.Errorf("parameter %s must be an integer: %w", key, err)
		}
		return int(n), nil
	default:
		return 0, fmt.Errorf("parameter %s must be a number, got %T", key, val)
	}
}

// getBoolArg extracts a boolean argument from a map.
func getBoolArg(args map[string]any, key string, defaultVal bool) (bool, error) {
	val, ok := args[key]
	if !ok || val == nil {
		return defaultVal, nil
	}
	b, ok := val.(bool)
	if !ok {
		return false, fmt.Errorf("parameter %s must be a boolean, got %T", key, val)
	}
	return b, nil
}

// formatLineNumbers prepends line numbers to content in "NNN | content" format.
// startLine is the 1-based line number for the first line.
func formatLineNumbers(content string, startLine int) string {
	lines := strings.Split(content, "\n")
	var builder strings.Builder
	for i, line := range lines {
		lineNum := startLine + i
		builder.WriteString(fmt.Sprintf("%3d | %s", lineNum, line))
		if i < len(lines)-1 {
			builder.WriteByte('\n')
		}
	}
	return builder.String()
}

// NewReadFileTool creates a RegisteredTool for reading files with line numbers.
func NewReadFileTool() *RegisteredTool {
	return &RegisteredTool{
		Definition: llm.ToolDefinition{
			Name:        "read_file",
			Description: "Read a file from the filesystem. Returns line-numbered content.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"file_path": {
						"type": "string",
						"description": "Absolute path to the file to read"
					},
					"offset": {
						"type": "integer",
						"description": "1-based line number to start reading from (default: 0 = beginning)"
					},
					"limit": {
						"type": "integer",
						"description": "Maximum number of lines to read (default: 2000)"
					}
				},
				"required": ["file_path"]
			}`),
		},
		Description: "Read a file from the filesystem. Returns line-numbered content.",
		Execute: func(args map[string]any, env ExecutionEnvironment) (string, error) {
			filePath, err := getStringArg(args, "file_path", true)
			if err != nil {
				return "", err
			}

			offset, err := getIntArg(args, "offset", 0)
			if err != nil {
				return "", err
			}

			limit, err := getIntArg(args, "limit", 2000)
			if err != nil {
				return "", err
			}

			content, err := env.ReadFile(filePath, offset, limit)
			if err != nil {
				return "", err
			}

			// Determine start line number for formatting
			startLine := 1
			if offset > 0 {
				startLine = offset
			}

			return formatLineNumbers(content, startLine), nil
		},
	}
}

// NewWriteFileTool creates a RegisteredTool for writing files.
func NewWriteFileTool() *RegisteredTool {
	return &RegisteredTool{
		Definition: llm.ToolDefinition{
			Name:        "write_file",
			Description: "Write content to a file. Creates the file and parent directories if needed.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"file_path": {
						"type": "string",
						"description": "Absolute path to the file to write"
					},
					"content": {
						"type": "string",
						"description": "The full file content to write"
					}
				},
				"required": ["file_path", "content"]
			}`),
		},
		Description: "Write content to a file. Creates the file and parent directories if needed.",
		Execute: func(args map[string]any, env ExecutionEnvironment) (string, error) {
			filePath, err := getStringArg(args, "file_path", true)
			if err != nil {
				return "", err
			}

			content, err := getStringArg(args, "content", true)
			if err != nil {
				return "", err
			}

			if err := env.WriteFile(filePath, content); err != nil {
				return "", err
			}

			return fmt.Sprintf("Successfully wrote %d bytes to %s", len(content), filepath.Base(filePath)), nil
		},
	}
}

// NewEditFileTool creates a RegisteredTool for search-and-replace editing of files.
func NewEditFileTool() *RegisteredTool {
	return &RegisteredTool{
		Definition: llm.ToolDefinition{
			Name:        "edit_file",
			Description: "Replace an exact string occurrence in a file.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"file_path": {
						"type": "string",
						"description": "Absolute path to the file to edit"
					},
					"old_string": {
						"type": "string",
						"description": "Exact text to find in the file"
					},
					"new_string": {
						"type": "string",
						"description": "Replacement text"
					},
					"replace_all": {
						"type": "boolean",
						"description": "Replace all occurrences (default: false)"
					}
				},
				"required": ["file_path", "old_string", "new_string"]
			}`),
		},
		Description: "Replace an exact string occurrence in a file.",
		Execute: func(args map[string]any, env ExecutionEnvironment) (string, error) {
			filePath, err := getStringArg(args, "file_path", true)
			if err != nil {
				return "", err
			}

			oldString, err := getStringArg(args, "old_string", true)
			if err != nil {
				return "", err
			}

			newString, err := getStringArg(args, "new_string", true)
			if err != nil {
				return "", err
			}

			replaceAll, err := getBoolArg(args, "replace_all", false)
			if err != nil {
				return "", err
			}

			// Read the current file content (offset=0, limit=0 means read all)
			content, err := env.ReadFile(filePath, 0, 0)
			if err != nil {
				return "", err
			}

			count := strings.Count(content, oldString)
			if count == 0 {
				return "", fmt.Errorf("old_string not found in %s", filePath)
			}

			if !replaceAll && count > 1 {
				return "", fmt.Errorf("old_string is not unique in %s (found %d occurrences). "+
					"Provide more context to make it unique, or set replace_all=true", filePath, count)
			}

			var newContent string
			var replacements int
			if replaceAll {
				newContent = strings.ReplaceAll(content, oldString, newString)
				replacements = count
			} else {
				newContent = strings.Replace(content, oldString, newString, 1)
				replacements = 1
			}

			if err := env.WriteFile(filePath, newContent); err != nil {
				return "", err
			}

			return fmt.Sprintf("Made %d replacement(s) in %s", replacements, filepath.Base(filePath)), nil
		},
	}
}

// NewShellTool creates a RegisteredTool for executing shell commands.
func NewShellTool() *RegisteredTool {
	return &RegisteredTool{
		Definition: llm.ToolDefinition{
			Name:        "shell",
			Description: "Execute a shell command. Returns stdout, stderr, and exit code.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"command": {
						"type": "string",
						"description": "The shell command to run"
					},
					"timeout_ms": {
						"type": "integer",
						"description": "Command timeout in milliseconds (default: 10000)"
					},
					"description": {
						"type": "string",
						"description": "Human-readable description of what this command does"
					}
				},
				"required": ["command"]
			}`),
		},
		Description: "Execute a shell command. Returns stdout, stderr, and exit code.",
		Execute: func(args map[string]any, env ExecutionEnvironment) (string, error) {
			command, err := getStringArg(args, "command", true)
			if err != nil {
				return "", err
			}

			timeoutMs, err := getIntArg(args, "timeout_ms", 10000)
			if err != nil {
				return "", err
			}

			result, err := env.ExecCommand(command, timeoutMs, "", nil)
			if err != nil {
				return "", err
			}

			var output strings.Builder
			if result.Stdout != "" {
				output.WriteString(result.Stdout)
			}
			if result.Stderr != "" {
				if output.Len() > 0 {
					output.WriteByte('\n')
				}
				output.WriteString("[stderr]\n")
				output.WriteString(result.Stderr)
			}

			output.WriteString(fmt.Sprintf("\n[exit code: %d, duration: %dms]", result.ExitCode, result.DurationMs))

			if result.TimedOut {
				output.WriteString(fmt.Sprintf("\n[ERROR: Command timed out after %dms. Partial output is shown above. "+
					"You can retry with a longer timeout by setting the timeout_ms parameter.]", timeoutMs))
			}

			return output.String(), nil
		},
	}
}

// NewGrepTool creates a RegisteredTool for searching file contents by regex.
func NewGrepTool() *RegisteredTool {
	return &RegisteredTool{
		Definition: llm.ToolDefinition{
			Name:        "grep",
			Description: "Search file contents using regex patterns.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"pattern": {
						"type": "string",
						"description": "Regex pattern to search for"
					},
					"path": {
						"type": "string",
						"description": "Directory or file to search (default: working directory)"
					},
					"glob_filter": {
						"type": "string",
						"description": "File pattern filter (e.g., '*.py')"
					},
					"case_insensitive": {
						"type": "boolean",
						"description": "Case insensitive search (default: false)"
					},
					"max_results": {
						"type": "integer",
						"description": "Maximum number of results (default: 100)"
					}
				},
				"required": ["pattern"]
			}`),
		},
		Description: "Search file contents using regex patterns.",
		Execute: func(args map[string]any, env ExecutionEnvironment) (string, error) {
			pattern, err := getStringArg(args, "pattern", true)
			if err != nil {
				return "", err
			}

			path, err := getStringArg(args, "path", false)
			if err != nil {
				return "", err
			}
			if path == "" {
				path = env.WorkingDirectory()
			}

			globFilter, err := getStringArg(args, "glob_filter", false)
			if err != nil {
				return "", err
			}

			caseInsensitive, err := getBoolArg(args, "case_insensitive", false)
			if err != nil {
				return "", err
			}

			maxResults, err := getIntArg(args, "max_results", 100)
			if err != nil {
				return "", err
			}

			opts := GrepOptions{
				GlobFilter:      globFilter,
				CaseInsensitive: caseInsensitive,
				MaxResults:      maxResults,
			}

			result, err := env.Grep(pattern, path, opts)
			if err != nil {
				return "", err
			}

			return result, nil
		},
	}
}

// NewGlobTool creates a RegisteredTool for finding files by glob pattern.
func NewGlobTool() *RegisteredTool {
	return &RegisteredTool{
		Definition: llm.ToolDefinition{
			Name:        "glob",
			Description: "Find files matching a glob pattern.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"pattern": {
						"type": "string",
						"description": "Glob pattern (e.g., '**/*.ts')"
					},
					"path": {
						"type": "string",
						"description": "Base directory (default: working directory)"
					}
				},
				"required": ["pattern"]
			}`),
		},
		Description: "Find files matching a glob pattern.",
		Execute: func(args map[string]any, env ExecutionEnvironment) (string, error) {
			pattern, err := getStringArg(args, "pattern", true)
			if err != nil {
				return "", err
			}

			path, err := getStringArg(args, "path", false)
			if err != nil {
				return "", err
			}
			if path == "" {
				path = env.WorkingDirectory()
			}

			matches, err := env.Glob(pattern, path)
			if err != nil {
				return "", err
			}

			if len(matches) == 0 {
				return "No files matched the pattern.", nil
			}

			return strings.Join(matches, "\n"), nil
		},
	}
}

// NewApplyPatchTool creates a RegisteredTool for applying v4a format patches.
// The v4a format supports creating, deleting, updating, and moving files in a single operation.
func NewApplyPatchTool() *RegisteredTool {
	return &RegisteredTool{
		Definition: llm.ToolDefinition{
			Name:        "apply_patch",
			Description: "Apply code changes using the v4a patch format. Supports creating, deleting, updating, and moving files in a single operation.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"patch": {
						"type": "string",
						"description": "The patch content in v4a format"
					}
				},
				"required": ["patch"]
			}`),
		},
		Description: "Apply code changes using the v4a patch format.",
		Execute: func(args map[string]any, env ExecutionEnvironment) (string, error) {
			patchStr, err := getStringArg(args, "patch", true)
			if err != nil {
				return "", err
			}

			patch, err := ParsePatch(patchStr)
			if err != nil {
				return "", err
			}

			result, err := ApplyPatch(patch, env)
			if err != nil {
				return "", err
			}

			return result.Summary, nil
		},
	}
}

// RegisterCoreTools registers all shared core tools with the given registry.
func RegisterCoreTools(registry *ToolRegistry) {
	registry.Register(NewReadFileTool())
	registry.Register(NewWriteFileTool())
	registry.Register(NewEditFileTool())
	registry.Register(NewShellTool())
	registry.Register(NewGrepTool())
	registry.Register(NewGlobTool())
	registry.Register(NewApplyPatchTool())
}
