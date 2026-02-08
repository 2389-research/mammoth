// ABOUTME: LocalExecutionEnvironment runs tools on the local machine.
// ABOUTME: Handles file ops, command execution with process groups, env filtering, grep, and glob.

package agent

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// EnvPolicy controls how environment variables are inherited by child processes.
type EnvPolicy string

const (
	// EnvPolicyInheritCore inherits only safe environment variables (default).
	EnvPolicyInheritCore EnvPolicy = "inherit_core"
	// EnvPolicyInheritAll inherits all environment variables without filtering.
	EnvPolicyInheritAll EnvPolicy = "inherit_all"
	// EnvPolicyInheritNone starts with a clean environment, only explicit vars.
	EnvPolicyInheritNone EnvPolicy = "inherit_none"
)

// sensitivePatterns are env var name suffixes that should be excluded under InheritCore.
var sensitivePatterns = []string{
	"_API_KEY",
	"_SECRET",
	"_TOKEN",
	"_PASSWORD",
	"_CREDENTIAL",
}

// safeVarNames are environment variables that are always included under InheritCore.
var safeVarNames = map[string]bool{
	"PATH":       true,
	"HOME":       true,
	"USER":       true,
	"SHELL":      true,
	"LANG":       true,
	"TERM":       true,
	"TMPDIR":     true,
	"GOPATH":     true,
	"GOROOT":     true,
	"CARGO_HOME": true,
	"NVM_DIR":    true,
	"EDITOR":     true,
}

// LocalExecOption configures a LocalExecutionEnvironment.
type LocalExecOption func(*LocalExecutionEnvironment)

// WithEnvPolicy sets the environment variable inheritance policy.
func WithEnvPolicy(policy EnvPolicy) LocalExecOption {
	return func(e *LocalExecutionEnvironment) {
		e.envPolicy = policy
	}
}

// LocalExecutionEnvironment implements ExecutionEnvironment for the local machine.
type LocalExecutionEnvironment struct {
	workDir     string
	envPolicy   EnvPolicy
	initialized bool
}

// NewLocalExecutionEnvironment creates a new local execution environment rooted at workDir.
func NewLocalExecutionEnvironment(workDir string, opts ...LocalExecOption) *LocalExecutionEnvironment {
	env := &LocalExecutionEnvironment{
		workDir:   workDir,
		envPolicy: EnvPolicyInheritCore,
	}
	for _, opt := range opts {
		opt(env)
	}
	return env
}

// ReadFile reads a file and prepends line numbers. Offset is 1-based; limit of 0 defaults to 2000.
func (e *LocalExecutionEnvironment) ReadFile(path string, offset, limit int) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read file %s: %w", path, err)
	}

	lines := strings.Split(string(data), "\n")
	// Remove trailing empty line from final newline
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	if limit == 0 {
		limit = 2000
	}

	// offset is 1-based; convert to 0-based index
	startIdx := 0
	if offset > 0 {
		startIdx = offset - 1
	}
	if startIdx > len(lines) {
		startIdx = len(lines)
	}

	endIdx := startIdx + limit
	if endIdx > len(lines) {
		endIdx = len(lines)
	}

	var buf strings.Builder
	for i := startIdx; i < endIdx; i++ {
		lineNum := i + 1 // 1-based line number
		fmt.Fprintf(&buf, "%4d\t%s\n", lineNum, lines[i])
	}

	return buf.String(), nil
}

// WriteFile writes content to a file, creating parent directories as needed.
func (e *LocalExecutionEnvironment) WriteFile(path string, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create directories for %s: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("write file %s: %w", path, err)
	}
	return nil
}

// FileExists checks if a file or directory exists at the given path.
func (e *LocalExecutionEnvironment) FileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// ListDirectory returns entries in a directory. Depth 0 means immediate children only.
func (e *LocalExecutionEnvironment) ListDirectory(path string, depth int) ([]DirEntry, error) {
	if depth == 0 {
		return e.listDirectoryFlat(path)
	}
	return e.listDirectoryRecursive(path, depth, 0)
}

func (e *LocalExecutionEnvironment) listDirectoryFlat(path string) ([]DirEntry, error) {
	dirEntries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("list directory %s: %w", path, err)
	}

	var result []DirEntry
	for _, de := range dirEntries {
		info, err := de.Info()
		if err != nil {
			continue
		}
		result = append(result, DirEntry{
			Name:  de.Name(),
			IsDir: de.IsDir(),
			Size:  info.Size(),
		})
	}
	return result, nil
}

func (e *LocalExecutionEnvironment) listDirectoryRecursive(path string, maxDepth, currentDepth int) ([]DirEntry, error) {
	dirEntries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("list directory %s: %w", path, err)
	}

	var result []DirEntry
	for _, de := range dirEntries {
		info, err := de.Info()
		if err != nil {
			continue
		}
		entry := DirEntry{
			Name:  de.Name(),
			IsDir: de.IsDir(),
			Size:  info.Size(),
		}
		result = append(result, entry)

		if de.IsDir() && (maxDepth == -1 || currentDepth < maxDepth) {
			subPath := filepath.Join(path, de.Name())
			subEntries, err := e.listDirectoryRecursive(subPath, maxDepth, currentDepth+1)
			if err != nil {
				continue
			}
			for _, se := range subEntries {
				se.Name = filepath.Join(de.Name(), se.Name)
				result = append(result, se)
			}
		}
	}
	return result, nil
}

// ExecCommand runs a shell command with timeout enforcement and environment filtering.
func (e *LocalExecutionEnvironment) ExecCommand(command string, timeoutMs int, workingDir string, envVars map[string]string) (*ExecResult, error) {
	if timeoutMs <= 0 {
		timeoutMs = 10000
	}

	timeout := time.Duration(timeoutMs) * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "/bin/bash", "-c", command)

	// Set process group so we can kill the entire group on timeout
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Set working directory
	if workingDir != "" {
		cmd.Dir = workingDir
	} else {
		cmd.Dir = e.workDir
	}

	// Build environment
	cmd.Env = e.buildEnv(envVars)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Start()
	if err != nil {
		return nil, fmt.Errorf("start command: %w", err)
	}

	// Wait for the command to finish
	waitErr := cmd.Wait()
	durationMs := int(time.Since(start).Milliseconds())

	timedOut := ctx.Err() == context.DeadlineExceeded

	if timedOut {
		// Attempt graceful shutdown: SIGTERM to process group
		if cmd.Process != nil {
			pgid, pgErr := syscall.Getpgid(cmd.Process.Pid)
			if pgErr == nil {
				_ = syscall.Kill(-pgid, syscall.SIGTERM)
			}
			// Wait 2 seconds, then SIGKILL
			time.Sleep(2 * time.Second)
			if pgErr == nil {
				_ = syscall.Kill(-pgid, syscall.SIGKILL)
			}
		}
	}

	exitCode := 0
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else if !timedOut {
			// If it's not an ExitError and not a timeout, it's a real error
			exitCode = -1
		}
	}

	return &ExecResult{
		Stdout:     stdout.String(),
		Stderr:     stderr.String(),
		ExitCode:   exitCode,
		TimedOut:   timedOut,
		DurationMs: durationMs,
	}, nil
}

func (e *LocalExecutionEnvironment) buildEnv(explicitVars map[string]string) []string {
	switch e.envPolicy {
	case EnvPolicyInheritAll:
		return e.buildEnvAll(explicitVars)
	case EnvPolicyInheritNone:
		return e.buildEnvNone(explicitVars)
	default:
		return e.buildEnvCore(explicitVars)
	}
}

func (e *LocalExecutionEnvironment) buildEnvAll(explicitVars map[string]string) []string {
	env := os.Environ()
	for k, v := range explicitVars {
		env = append(env, k+"="+v)
	}
	return env
}

func (e *LocalExecutionEnvironment) buildEnvNone(explicitVars map[string]string) []string {
	var env []string
	for k, v := range explicitVars {
		env = append(env, k+"="+v)
	}
	return env
}

func (e *LocalExecutionEnvironment) buildEnvCore(explicitVars map[string]string) []string {
	var env []string

	// Inherit only safe variables from the current environment
	for _, entry := range os.Environ() {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 {
			continue
		}
		name := parts[0]
		if safeVarNames[name] {
			env = append(env, entry)
		} else if isSensitiveVar(name) {
			continue
		} else {
			env = append(env, entry)
		}
	}

	// Add explicit vars, filtering sensitive ones
	for k, v := range explicitVars {
		if isSensitiveVar(k) {
			continue
		}
		env = append(env, k+"="+v)
	}

	return env
}

// isSensitiveVar checks if a variable name matches sensitive patterns (case-insensitive).
func isSensitiveVar(name string) bool {
	upper := strings.ToUpper(name)
	for _, pattern := range sensitivePatterns {
		if strings.HasSuffix(upper, pattern) {
			return true
		}
	}
	return false
}

// Grep searches file contents by regex pattern.
func (e *LocalExecutionEnvironment) Grep(pattern, path string, opts GrepOptions) (string, error) {
	if path == "" {
		path = e.workDir
	}

	// Try ripgrep first
	if rgPath, err := exec.LookPath("rg"); err == nil {
		return e.grepWithRipgrep(rgPath, pattern, path, opts)
	}

	// Fall back to Go regex
	return e.grepWithRegex(pattern, path, opts)
}

func (e *LocalExecutionEnvironment) grepWithRipgrep(rgPath, pattern, path string, opts GrepOptions) (string, error) {
	args := []string{pattern}

	if opts.CaseInsensitive {
		args = append(args, "-i")
	}
	if opts.MaxResults > 0 {
		args = append(args, fmt.Sprintf("--max-count=%d", opts.MaxResults))
	}
	if opts.GlobFilter != "" {
		args = append(args, "--glob", opts.GlobFilter)
	}
	args = append(args, "-n") // line numbers
	args = append(args, path)

	cmd := exec.Command(rgPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		// ripgrep exits 1 when no matches found -- that's not an error
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return "", nil
		}
		return "", fmt.Errorf("ripgrep error: %s", stderr.String())
	}

	return stdout.String(), nil
}

func (e *LocalExecutionEnvironment) grepWithRegex(pattern, path string, opts GrepOptions) (string, error) {
	flags := ""
	if opts.CaseInsensitive {
		flags = "(?i)"
	}
	re, err := regexp.Compile(flags + pattern)
	if err != nil {
		return "", fmt.Errorf("invalid regex pattern %q: %w", pattern, err)
	}

	var buf strings.Builder
	matchCount := 0
	maxResults := opts.MaxResults
	if maxResults <= 0 {
		maxResults = 100
	}

	walkErr := filepath.WalkDir(path, func(fpath string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip entries we can't access
		}
		if d.IsDir() {
			return nil
		}

		// Apply glob filter
		if opts.GlobFilter != "" {
			matched, matchErr := filepath.Match(opts.GlobFilter, d.Name())
			if matchErr != nil || !matched {
				return nil
			}
		}

		file, openErr := os.Open(fpath)
		if openErr != nil {
			return nil
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			if re.MatchString(line) {
				fmt.Fprintf(&buf, "%s:%d:%s\n", fpath, lineNum, line)
				matchCount++
				if matchCount >= maxResults {
					return filepath.SkipAll
				}
			}
		}
		return nil
	})

	if walkErr != nil {
		return "", fmt.Errorf("grep walk error: %w", walkErr)
	}

	return buf.String(), nil
}

// Glob finds files matching a glob pattern relative to the given path.
func (e *LocalExecutionEnvironment) Glob(pattern, path string) ([]string, error) {
	if path == "" {
		path = e.workDir
	}

	// Check if pattern contains ** for recursive matching
	if strings.Contains(pattern, "**") {
		return e.globRecursive(pattern, path)
	}

	// Simple glob
	fullPattern := filepath.Join(path, pattern)
	matches, err := filepath.Glob(fullPattern)
	if err != nil {
		return nil, fmt.Errorf("glob pattern %q: %w", pattern, err)
	}
	return matches, nil
}

func (e *LocalExecutionEnvironment) globRecursive(pattern, basePath string) ([]string, error) {
	// For ** patterns, walk the directory tree and match each file
	// Split pattern into parts at "**"
	parts := strings.SplitN(pattern, "**", 2)
	prefix := strings.TrimRight(parts[0], string(filepath.Separator))
	suffix := ""
	if len(parts) > 1 {
		suffix = strings.TrimLeft(parts[1], string(filepath.Separator))
	}

	startDir := basePath
	if prefix != "" {
		startDir = filepath.Join(basePath, prefix)
	}

	var matches []string
	walkErr := filepath.WalkDir(startDir, func(fpath string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}

		if suffix == "" {
			matches = append(matches, fpath)
			return nil
		}

		matched, matchErr := filepath.Match(suffix, d.Name())
		if matchErr == nil && matched {
			matches = append(matches, fpath)
		}
		return nil
	})

	if walkErr != nil {
		return nil, fmt.Errorf("recursive glob error: %w", walkErr)
	}
	return matches, nil
}

// Initialize verifies the working directory exists, creating it if needed.
func (e *LocalExecutionEnvironment) Initialize() error {
	if err := os.MkdirAll(e.workDir, 0755); err != nil {
		return fmt.Errorf("initialize work dir %s: %w", e.workDir, err)
	}
	e.initialized = true
	return nil
}

// Cleanup is a no-op for the local environment (placeholder for future use).
func (e *LocalExecutionEnvironment) Cleanup() error {
	return nil
}

// WorkingDirectory returns the configured root working directory.
func (e *LocalExecutionEnvironment) WorkingDirectory() string {
	return e.workDir
}

// Platform returns the operating system identifier.
func (e *LocalExecutionEnvironment) Platform() string {
	return runtime.GOOS
}

// OSVersion returns the OS version string from uname -r.
func (e *LocalExecutionEnvironment) OSVersion() string {
	cmd := exec.Command("uname", "-r")
	out, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

// Compile-time interface check
var _ ExecutionEnvironment = (*LocalExecutionEnvironment)(nil)
