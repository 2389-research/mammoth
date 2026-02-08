// ABOUTME: Defines the ExecutionEnvironment interface for the coding agent loop.
// ABOUTME: Provides the abstraction layer that decouples tool logic from where tools run.

package agent

// ExecutionEnvironment abstracts all file, command, and search operations
// so that tools are decoupled from the runtime (local, Docker, K8s, WASM, SSH).
type ExecutionEnvironment interface {
	// ReadFile reads a file with line numbers prepended. Offset is 1-based.
	// If limit is 0, a default of 2000 lines is used.
	ReadFile(path string, offset, limit int) (string, error)

	// WriteFile writes content to a file, creating parent directories as needed.
	WriteFile(path string, content string) error

	// FileExists checks whether a file or directory exists at the given path.
	FileExists(path string) (bool, error)

	// ListDirectory returns entries in a directory, optionally recursing to the given depth.
	// A depth of 0 means only the immediate children. A depth of -1 means unlimited.
	ListDirectory(path string, depth int) ([]DirEntry, error)

	// ExecCommand runs a shell command with timeout and environment controls.
	// If workingDir is empty, the environment's working directory is used.
	ExecCommand(command string, timeoutMs int, workingDir string, envVars map[string]string) (*ExecResult, error)

	// Grep searches file contents by regex pattern. Path defaults to the working directory.
	Grep(pattern, path string, opts GrepOptions) (string, error)

	// Glob finds files matching a glob pattern. Path defaults to the working directory.
	Glob(pattern, path string) ([]string, error)

	// Initialize prepares the execution environment (e.g., verifies working directory).
	Initialize() error

	// Cleanup releases resources held by the execution environment.
	Cleanup() error

	// WorkingDirectory returns the root working directory for this environment.
	WorkingDirectory() string

	// Platform returns the OS identifier (e.g., "darwin", "linux", "windows").
	Platform() string

	// OSVersion returns the OS version string (e.g., kernel version from uname -r).
	OSVersion() string
}

// ExecResult holds the outcome of a command execution.
type ExecResult struct {
	Stdout     string
	Stderr     string
	ExitCode   int
	TimedOut   bool
	DurationMs int
}

// DirEntry represents a single entry when listing a directory.
type DirEntry struct {
	Name  string
	IsDir bool
	Size  int64
}

// GrepOptions configures the behavior of a grep search.
type GrepOptions struct {
	GlobFilter      string
	CaseInsensitive bool
	MaxResults      int
}
