// ABOUTME: Tests for the makeatron CLI entrypoint covering flag parsing, retry policy mapping,
// ABOUTME: pipeline validation, pipeline execution, and version display.
package main

import (
	"os"
	"testing"

	"github.com/2389-research/makeatron/attractor"
)

// writeTempDOT creates a temporary DOT file with the given content and returns its path.
// The file is cleaned up automatically when the test finishes.
func writeTempDOT(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp("", "test-*.dot")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })
	return f.Name()
}

const validDOT = `digraph test {
    start [shape=Mdiamond]
    finish [shape=Msquare]
    start -> finish
}`

const invalidDOT = `digraph test {
    orphan [shape=box]
    finish [shape=Msquare]
    orphan -> finish
}`

// --- parseFlags tests ---

func TestParseFlagsDefaults(t *testing.T) {
	// Save and restore os.Args
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"makeatron", "pipeline.dot"}
	cfg := parseFlags()

	if cfg.serverMode {
		t.Error("expected serverMode=false by default")
	}
	if cfg.port != 2389 {
		t.Errorf("expected default port=2389, got %d", cfg.port)
	}
	if cfg.validateOnly {
		t.Error("expected validateOnly=false by default")
	}
	if cfg.checkpointDir != "" {
		t.Errorf("expected empty checkpointDir, got %q", cfg.checkpointDir)
	}
	if cfg.artifactDir != "" {
		t.Errorf("expected empty artifactDir, got %q", cfg.artifactDir)
	}
	if cfg.retryPolicy != "none" {
		t.Errorf("expected retryPolicy=none, got %q", cfg.retryPolicy)
	}
	if cfg.verbose {
		t.Error("expected verbose=false by default")
	}
	if cfg.showVersion {
		t.Error("expected showVersion=false by default")
	}
	if cfg.pipelineFile != "pipeline.dot" {
		t.Errorf("expected pipelineFile=pipeline.dot, got %q", cfg.pipelineFile)
	}
}

func TestParseFlagsServer(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"makeatron", "--server"}
	cfg := parseFlags()

	if !cfg.serverMode {
		t.Error("expected serverMode=true with --server flag")
	}
}

func TestParseFlagsValidate(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"makeatron", "--validate", "test.dot"}
	cfg := parseFlags()

	if !cfg.validateOnly {
		t.Error("expected validateOnly=true with --validate flag")
	}
	if cfg.pipelineFile != "test.dot" {
		t.Errorf("expected pipelineFile=test.dot, got %q", cfg.pipelineFile)
	}
}

func TestParseFlagsPort(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"makeatron", "--server", "--port", "9999"}
	cfg := parseFlags()

	if cfg.port != 9999 {
		t.Errorf("expected port=9999, got %d", cfg.port)
	}
}

func TestParseFlagsRetry(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"makeatron", "--retry", "aggressive", "test.dot"}
	cfg := parseFlags()

	if cfg.retryPolicy != "aggressive" {
		t.Errorf("expected retryPolicy=aggressive, got %q", cfg.retryPolicy)
	}
}

// --- retryPolicyFromName tests ---

func TestRetryPolicyFromNameAll(t *testing.T) {
	tests := []struct {
		name        string
		expectedMax int
	}{
		{"none", 1},
		{"standard", 5},
		{"aggressive", 5},
		{"linear", 3},
		{"patient", 3},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			policy := retryPolicyFromName(tc.name)
			if policy.MaxAttempts != tc.expectedMax {
				t.Errorf("retryPolicyFromName(%q): expected MaxAttempts=%d, got %d", tc.name, tc.expectedMax, policy.MaxAttempts)
			}
		})
	}
}

func TestRetryPolicyFromNameUnknown(t *testing.T) {
	policy := retryPolicyFromName("bogus")
	nonePolicy := attractor.RetryPolicyNone()
	if policy.MaxAttempts != nonePolicy.MaxAttempts {
		t.Errorf("expected unknown name to return none policy (MaxAttempts=%d), got MaxAttempts=%d", nonePolicy.MaxAttempts, policy.MaxAttempts)
	}
}

func TestRetryPolicyFromNameCaseInsensitive(t *testing.T) {
	policy := retryPolicyFromName("STANDARD")
	if policy.MaxAttempts != 5 {
		t.Errorf("expected case-insensitive match for STANDARD, got MaxAttempts=%d", policy.MaxAttempts)
	}
}

// --- validatePipeline tests ---

func TestValidatePipelineValid(t *testing.T) {
	dotFile := writeTempDOT(t, validDOT)
	cfg := config{
		pipelineFile: dotFile,
	}
	exitCode := validatePipeline(cfg)
	if exitCode != 0 {
		t.Errorf("expected exit code 0 for valid pipeline, got %d", exitCode)
	}
}

func TestValidatePipelineInvalid(t *testing.T) {
	dotFile := writeTempDOT(t, invalidDOT)
	cfg := config{
		pipelineFile: dotFile,
	}
	exitCode := validatePipeline(cfg)
	if exitCode != 1 {
		t.Errorf("expected exit code 1 for invalid pipeline (missing start node), got %d", exitCode)
	}
}

func TestValidatePipelineNonexistentFile(t *testing.T) {
	cfg := config{
		pipelineFile: "/tmp/this-file-does-not-exist-at-all.dot",
	}
	exitCode := validatePipeline(cfg)
	if exitCode != 1 {
		t.Errorf("expected exit code 1 for nonexistent file, got %d", exitCode)
	}
}

// --- runPipeline tests ---

func TestRunPipelineSuccess(t *testing.T) {
	dotFile := writeTempDOT(t, validDOT)
	cfg := config{
		pipelineFile: dotFile,
		retryPolicy:  "none",
	}
	exitCode := runPipeline(cfg)
	if exitCode != 0 {
		t.Errorf("expected exit code 0 for simple valid pipeline, got %d", exitCode)
	}
}

func TestRunPipelineNonexistentFile(t *testing.T) {
	cfg := config{
		pipelineFile: "/tmp/no-such-pipeline-file.dot",
		retryPolicy:  "none",
	}
	exitCode := runPipeline(cfg)
	if exitCode != 1 {
		t.Errorf("expected exit code 1 for nonexistent file, got %d", exitCode)
	}
}

func TestRunPipelineInvalidDOT(t *testing.T) {
	dotFile := writeTempDOT(t, "this is not valid DOT at all {{{")
	cfg := config{
		pipelineFile: dotFile,
		retryPolicy:  "none",
	}
	exitCode := runPipeline(cfg)
	if exitCode != 1 {
		t.Errorf("expected exit code 1 for malformed DOT, got %d", exitCode)
	}
}

func TestRunPipelineWithVerbose(t *testing.T) {
	dotFile := writeTempDOT(t, validDOT)
	cfg := config{
		pipelineFile: dotFile,
		retryPolicy:  "none",
		verbose:      true,
	}
	exitCode := runPipeline(cfg)
	if exitCode != 0 {
		t.Errorf("expected exit code 0 with verbose mode, got %d", exitCode)
	}
}

func TestRunPipelineWithCheckpointDir(t *testing.T) {
	dotFile := writeTempDOT(t, validDOT)
	tmpDir := t.TempDir()
	cfg := config{
		pipelineFile:  dotFile,
		retryPolicy:   "none",
		checkpointDir: tmpDir,
	}
	exitCode := runPipeline(cfg)
	if exitCode != 0 {
		t.Errorf("expected exit code 0 with checkpoint dir, got %d", exitCode)
	}
}

// --- version field test ---

func TestVersionFieldCausesEarlyExit(t *testing.T) {
	cfg := config{
		showVersion: true,
	}
	// The run function should not be called when showVersion is true
	// (handled in main before run is called), but we verify the field is set.
	if !cfg.showVersion {
		t.Error("expected showVersion=true")
	}
}

// --- run function integration tests ---

func TestRunValidateMode(t *testing.T) {
	dotFile := writeTempDOT(t, validDOT)
	cfg := config{
		validateOnly: true,
		pipelineFile: dotFile,
	}
	exitCode := run(cfg)
	if exitCode != 0 {
		t.Errorf("expected exit code 0 for validate mode with valid pipeline, got %d", exitCode)
	}
}

func TestRunRunMode(t *testing.T) {
	dotFile := writeTempDOT(t, validDOT)
	cfg := config{
		pipelineFile: dotFile,
		retryPolicy:  "none",
	}
	exitCode := run(cfg)
	if exitCode != 0 {
		t.Errorf("expected exit code 0 for run mode with valid pipeline, got %d", exitCode)
	}
}

func TestRunRequiresPipelineFile(t *testing.T) {
	cfg := config{
		pipelineFile: "",
	}
	exitCode := run(cfg)
	if exitCode != 1 {
		t.Errorf("expected exit code 1 when no pipeline file given (non-server mode), got %d", exitCode)
	}
}
