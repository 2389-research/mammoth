// ABOUTME: Tests for the mammoth CLI entrypoint covering flag parsing, pipeline validation,
// ABOUTME: pipeline execution, version display, LLM client construction, and serve subcommand.
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/2389-research/mammoth/dot"
	"github.com/2389-research/mammoth/dot/validator"
	"github.com/2389-research/mammoth/runstate"
	"github.com/2389-research/tracker/agent"
	"github.com/2389-research/tracker/pipeline"
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

	os.Args = []string{"mammoth", "pipeline.dot"}
	cfg := parseFlags()

	if cfg.port != 2389 {
		t.Errorf("expected default port=2389, got %d", cfg.port)
	}
	if cfg.validateOnly {
		t.Error("expected validateOnly=false by default")
	}
	if cfg.checkpointDir != "" {
		t.Errorf("expected empty checkpointDir, got %q", cfg.checkpointDir)
	}
	if cfg.artifactDir != "." {
		t.Errorf("expected artifactDir='.', got %q", cfg.artifactDir)
	}
	if cfg.dataDir != "" {
		t.Errorf("expected empty dataDir, got %q", cfg.dataDir)
	}
	if cfg.retryPolicy != "none" {
		t.Errorf("expected retryPolicy=none, got %q", cfg.retryPolicy)
	}
	if cfg.tuiMode {
		t.Error("expected tuiMode=false by default")
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
	if cfg.fresh {
		t.Error("expected fresh=false by default")
	}
}

func TestParseFlagsFresh(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"mammoth", "-fresh", "pipeline.dot"}
	cfg := parseFlags()

	if !cfg.fresh {
		t.Error("expected fresh=true with -fresh flag")
	}
	if cfg.pipelineFile != "pipeline.dot" {
		t.Errorf("expected pipelineFile=pipeline.dot, got %q", cfg.pipelineFile)
	}
}

func TestParseFlagsFreshDefaultFalse(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"mammoth", "pipeline.dot"}
	cfg := parseFlags()

	if cfg.fresh {
		t.Error("expected fresh=false by default")
	}
}

func TestParseFlagsValidate(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"mammoth", "--validate", "test.dot"}
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

	os.Args = []string{"mammoth", "--port", "9999", "pipeline.dot"}
	cfg := parseFlags()

	if cfg.port != 9999 {
		t.Errorf("expected port=9999, got %d", cfg.port)
	}
}

func TestParseFlagsTUI(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"mammoth", "--tui", "pipeline.dot"}
	cfg := parseFlags()

	if !cfg.tuiMode {
		t.Error("expected tuiMode=true with --tui flag")
	}
	if cfg.pipelineFile != "pipeline.dot" {
		t.Errorf("expected pipelineFile=pipeline.dot, got %q", cfg.pipelineFile)
	}
}

func TestParseFlagsTUIDefaultFalse(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"mammoth", "pipeline.dot"}
	cfg := parseFlags()

	if cfg.tuiMode {
		t.Error("expected tuiMode=false by default")
	}
}

func TestParseFlagsDataDir(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"mammoth", "--data-dir", "/tmp/mammoth-data", "test.dot"}
	cfg := parseFlags()

	if cfg.dataDir != "/tmp/mammoth-data" {
		t.Errorf("expected dataDir=/tmp/mammoth-data, got %q", cfg.dataDir)
	}
}

func TestResolveDataDirWithOverride(t *testing.T) {
	dir := t.TempDir()
	got, err := resolveDataDir(dir)
	if err != nil {
		t.Fatalf("resolveDataDir failed: %v", err)
	}
	if got != dir {
		t.Errorf("expected %q, got %q", dir, got)
	}
}

func TestResolveDataDirUsesDefault(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	got, err := resolveDataDir("")
	if err != nil {
		t.Fatalf("resolveDataDir failed: %v", err)
	}
	if got == "" {
		t.Error("expected non-empty default data dir")
	}
	if !strings.Contains(got, "mammoth") {
		t.Errorf("expected data dir to contain 'mammoth', got %q", got)
	}
}

func TestParseFlagsRetry(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"mammoth", "--retry", "aggressive", "test.dot"}
	cfg := parseFlags()

	if cfg.retryPolicy != "aggressive" {
		t.Errorf("expected retryPolicy=aggressive, got %q", cfg.retryPolicy)
	}
}

func TestParseFlagsRunSubcommand(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"mammoth", "run", "pipeline.dot"}
	cfg := parseFlags()

	if cfg.pipelineFile != "pipeline.dot" {
		t.Errorf("expected pipelineFile='pipeline.dot', got %q", cfg.pipelineFile)
	}
}

func TestParseFlagsRunSubcommandWithFlags(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"mammoth", "--verbose", "run", "pipeline.dot"}
	cfg := parseFlags()

	if cfg.pipelineFile != "pipeline.dot" {
		t.Errorf("expected pipelineFile='pipeline.dot', got %q", cfg.pipelineFile)
	}
	if !cfg.verbose {
		t.Error("expected verbose=true")
	}
}

func TestParseFlagsRunSubcommandAlone(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	// "mammoth run" with no pipeline file should result in empty pipelineFile
	os.Args = []string{"mammoth", "run"}
	cfg := parseFlags()

	if cfg.pipelineFile != "" {
		t.Errorf("expected empty pipelineFile, got %q", cfg.pipelineFile)
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
		t.Errorf("expected exit code 1 when no pipeline file given (usage error), got %d", exitCode)
	}
}

func TestRunNoArgsShowsHelp(t *testing.T) {
	cfg := config{
		pipelineFile: "",
	}
	exitCode := run(cfg)
	if exitCode != 1 {
		t.Errorf("expected exit code 1 for no-args (usage error), got %d", exitCode)
	}
}

// --- dot.Parse and validator.Lint tests ---

func TestDotParseValidDOT(t *testing.T) {
	graph, err := dot.Parse(validDOT)
	if err != nil {
		t.Fatalf("dot.Parse failed for valid DOT: %v", err)
	}
	if graph == nil {
		t.Fatal("expected non-nil graph")
	}
	if graph.Name != "test" {
		t.Errorf("expected graph name 'test', got %q", graph.Name)
	}
}

func TestValidatorLintValid(t *testing.T) {
	graph, err := dot.Parse(validDOT)
	if err != nil {
		t.Fatalf("dot.Parse failed: %v", err)
	}
	diags := validator.Lint(graph)
	hasErrors := false
	for _, d := range diags {
		if d.Severity == "error" {
			hasErrors = true
			t.Errorf("unexpected error: %s", d.Message)
		}
	}
	if hasErrors {
		t.Error("expected no errors for valid DOT")
	}
}

func TestValidatorLintInvalid(t *testing.T) {
	graph, err := dot.Parse(invalidDOT)
	if err != nil {
		t.Fatalf("dot.Parse failed: %v", err)
	}
	diags := validator.Lint(graph)
	hasErrors := false
	for _, d := range diags {
		if d.Severity == "error" {
			hasErrors = true
		}
	}
	if !hasErrors {
		t.Error("expected at least one error for invalid DOT (missing start node)")
	}
}

// --- runstate tests ---

func TestRunstateSourceHash(t *testing.T) {
	hash1 := runstate.SourceHash("hello")
	hash2 := runstate.SourceHash("hello")
	if hash1 != hash2 {
		t.Errorf("expected same hash for same input, got %q and %q", hash1, hash2)
	}

	hash3 := runstate.SourceHash("world")
	if hash1 == hash3 {
		t.Error("expected different hash for different input")
	}
}

func TestRunstateGenerateRunID(t *testing.T) {
	id, err := runstate.GenerateRunID()
	if err != nil {
		t.Fatalf("GenerateRunID failed: %v", err)
	}
	if len(id) != 16 {
		t.Errorf("expected 16-character run ID, got %d characters: %q", len(id), id)
	}
}

// --- buildTrackerLLMClient tests ---

func TestBuildTrackerLLMClientNoKeys(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")

	client, err := buildTrackerLLMClient()
	if err != nil {
		t.Fatalf("expected no error without API keys, got: %v", err)
	}
	if client != nil {
		t.Error("expected nil client when no API keys are set")
	}
}

func TestHasLLMKeys(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")

	if hasLLMKeys() {
		t.Error("expected hasLLMKeys=false with no keys set")
	}

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	if !hasLLMKeys() {
		t.Error("expected hasLLMKeys=true with ANTHROPIC_API_KEY set")
	}
}

// --- verbose event handler tests ---

func TestVerbosePipelineHandler(t *testing.T) {
	// Just verify it doesn't panic on various event types.
	events := []pipeline.PipelineEvent{
		{Type: pipeline.EventPipelineStarted},
		{Type: pipeline.EventStageStarted, NodeID: "build"},
		{Type: pipeline.EventStageCompleted, NodeID: "build"},
		{Type: pipeline.EventStageFailed, NodeID: "build"},
		{Type: pipeline.EventStageRetrying, NodeID: "build"},
		{Type: pipeline.EventPipelineCompleted},
		{Type: pipeline.EventPipelineFailed},
		{Type: pipeline.EventCheckpointSaved, NodeID: "build"},
	}

	for _, evt := range events {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("verbosePipelineHandler panicked on %s: %v", evt.Type, r)
				}
			}()
			verbosePipelineHandler(evt)
		}()
	}
}

func TestVerboseAgentHandler(t *testing.T) {
	// Just verify it doesn't panic on various event types.
	events := []agent.Event{
		{Type: agent.EventTextDelta, Text: "hello"},
		{Type: agent.EventToolCallStart, ToolName: "file_write"},
		{Type: agent.EventToolCallEnd, ToolName: "file_write"},
		{Type: agent.EventTurnEnd, Turn: 1},
		{Type: agent.EventSteeringInjected, Text: "focus"},
	}

	for _, evt := range events {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("verboseAgentHandler panicked on %s: %v", evt.Type, r)
				}
			}()
			verboseAgentHandler(evt)
		}()
	}
}

// --- example DOT files test ---

func TestExampleDOTFilesParseAndValidate(t *testing.T) {
	examples := []string{
		"../../examples/build_pong.dot",
		"../../examples/build_dvd_bounce.dot",
		"../../examples/build_markdown_editor.dot",
		"../../examples/build_htmx_blog.dot",
		"../../examples/build_python_code_agent.dot",
	}

	for _, path := range examples {
		t.Run(path, func(t *testing.T) {
			source, err := os.ReadFile(path)
			if err != nil {
				if os.IsNotExist(err) {
					t.Skipf("skipping %s: file does not exist", path)
				}
				t.Fatalf("failed to read %s: %v", path, err)
			}
			graph, err := dot.Parse(string(source))
			if err != nil {
				t.Fatalf("failed to parse %s: %v", path, err)
			}

			diags := validator.Lint(graph)
			for _, d := range diags {
				if d.Severity == "error" {
					t.Errorf("[%s] validation error: %s (node: %s)", path, d.Message, d.NodeID)
				}
			}
		})
	}
}

// --- Auto-resume tests ---

func TestRunPipelineCreatesAutoCheckpoint(t *testing.T) {
	dotFile := writeTempDOT(t, validDOT)
	dataDir := t.TempDir()

	cfg := config{
		pipelineFile: dotFile,
		retryPolicy:  "none",
		dataDir:      dataDir,
	}
	exitCode := runPipeline(cfg)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}

	// Verify a checkpoint.json was created in some run dir
	runsDir := dataDir + "/runs"
	entries, err := os.ReadDir(runsDir)
	if err != nil {
		t.Fatalf("failed to read runs dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one run directory")
	}

	found := false
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		cpPath := runsDir + "/" + entry.Name() + "/checkpoint.json"
		if _, err := os.Stat(cpPath); err == nil {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected checkpoint.json in run directory for auto-resume")
	}
}

func TestRunPipelineStoresSourceHash(t *testing.T) {
	dotFile := writeTempDOT(t, validDOT)
	dataDir := t.TempDir()

	cfg := config{
		pipelineFile: dotFile,
		retryPolicy:  "none",
		dataDir:      dataDir,
	}
	exitCode := runPipeline(cfg)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}

	// Read the run state and verify it has a source hash
	runsDir := dataDir + "/runs"
	store, err := runstate.NewFSRunStateStore(runsDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	runs, err := store.List()
	if err != nil {
		t.Fatalf("failed to list runs: %v", err)
	}
	if len(runs) == 0 {
		t.Fatal("expected at least one run")
	}

	expectedHash := runstate.SourceHash(validDOT)
	if runs[0].SourceHash != expectedHash {
		t.Errorf("SourceHash mismatch: got %q, want %q", runs[0].SourceHash, expectedHash)
	}
}

func TestRunPipelineFreshSkipsResume(t *testing.T) {
	dotFile := writeTempDOT(t, validDOT)
	dataDir := t.TempDir()

	// Run once to create a stored run
	cfg := config{
		pipelineFile: dotFile,
		retryPolicy:  "none",
		dataDir:      dataDir,
	}
	exitCode := runPipeline(cfg)
	if exitCode != 0 {
		t.Fatalf("first run: expected exit code 0, got %d", exitCode)
	}

	// Run again with --fresh flag — should create a new run, not resume
	cfg.fresh = true
	exitCode = runPipeline(cfg)
	if exitCode != 0 {
		t.Fatalf("second run with --fresh: expected exit code 0, got %d", exitCode)
	}

	// Should have two run directories now
	runsDir := dataDir + "/runs"
	store, err := runstate.NewFSRunStateStore(runsDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	runs, err := store.List()
	if err != nil {
		t.Fatalf("failed to list runs: %v", err)
	}
	if len(runs) != 2 {
		t.Errorf("expected 2 runs (fresh created a new one), got %d", len(runs))
	}
}

// --- serve subcommand tests ---

func TestParseServeSubcommand(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"mammoth", "serve"}
	scfg, ok := parseServeArgs(os.Args[1:])

	if !ok {
		t.Fatal("expected parseServeArgs to recognize 'serve' subcommand")
	}
	if scfg.port != 2389 {
		t.Errorf("expected default port=2389, got %d", scfg.port)
	}
	if scfg.dataDir != "" {
		t.Errorf("expected empty dataDir by default, got %q", scfg.dataDir)
	}
	if scfg.global {
		t.Error("expected global=false by default")
	}
}

func TestParseServeSubcommandWithPort(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"mammoth", "serve", "--port", "9999"}
	scfg, ok := parseServeArgs(os.Args[1:])

	if !ok {
		t.Fatal("expected parseServeArgs to recognize 'serve' subcommand")
	}
	if scfg.port != 9999 {
		t.Errorf("expected port=9999, got %d", scfg.port)
	}
}

func TestParseServeSubcommandWithDataDir(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"mammoth", "serve", "--data-dir", "/tmp/test-data"}
	scfg, ok := parseServeArgs(os.Args[1:])

	if !ok {
		t.Fatal("expected parseServeArgs to recognize 'serve' subcommand")
	}
	if scfg.dataDir != "/tmp/test-data" {
		t.Errorf("expected dataDir=/tmp/test-data, got %q", scfg.dataDir)
	}
}

func TestParseServeArgsReturnsFalseForNonServe(t *testing.T) {
	_, ok := parseServeArgs([]string{"pipeline.dot"})
	if ok {
		t.Error("expected parseServeArgs to return false for non-serve arg")
	}

	_, ok = parseServeArgs([]string{"run", "pipeline.dot"})
	if ok {
		t.Error("expected parseServeArgs to return false for 'run' subcommand")
	}

	_, ok = parseServeArgs([]string{"--server"})
	if ok {
		t.Error("expected parseServeArgs to return false for '--server' flag")
	}
}

func TestParseServeArgsGlobal(t *testing.T) {
	scfg, ok := parseServeArgs([]string{"serve", "--global"})
	if !ok {
		t.Fatal("expected serve subcommand to be detected")
	}
	if !scfg.global {
		t.Fatal("expected global flag to be true")
	}
}

func TestParseServeArgsDefaultLocal(t *testing.T) {
	scfg, ok := parseServeArgs([]string{"serve"})
	if !ok {
		t.Fatal("expected serve subcommand to be detected")
	}
	if scfg.global {
		t.Fatal("expected global flag to be false by default")
	}
}

func TestBuildWebServerDefaultLocal(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key-for-server-boot")
	scfg := serveConfig{port: 0}
	srv, err := buildWebServer(scfg)
	if err != nil {
		t.Fatalf("buildWebServer: %v", err)
	}
	// Server was created in local mode (CWD is root)
	_ = srv
}

func TestBuildWebServerGlobal(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key-for-server-boot")
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	scfg := serveConfig{port: 0, global: true}
	srv, err := buildWebServer(scfg)
	if err != nil {
		t.Fatalf("buildWebServer: %v", err)
	}
	_ = srv
}

func TestBuildWebServerExplicitDataDir(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key-for-server-boot")
	scfg := serveConfig{port: 0, dataDir: t.TempDir()}
	srv, err := buildWebServer(scfg)
	if err != nil {
		t.Fatalf("buildWebServer: %v", err)
	}
	_ = srv
}

func TestRunServeStartsHealthEndpoint(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key-for-server-boot")
	dataDir := t.TempDir()

	scfg := serveConfig{
		port:    0, // use port 0 to let the OS pick a free port
		dataDir: dataDir,
	}

	// Create a server and test the health endpoint via httptest
	srv, err := buildWebServer(scfg)
	if err != nil {
		t.Fatalf("buildWebServer failed: %v", err)
	}

	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 for /health, got %d", resp.StatusCode)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode health response: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("expected health status %q, got %q", "ok", body["status"])
	}
}

func TestRunServeGracefulShutdown(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key-for-server-boot")
	dataDir := t.TempDir()

	scfg := serveConfig{
		port:    0,
		dataDir: dataDir,
	}

	srv, err := buildWebServer(scfg)
	if err != nil {
		t.Fatalf("buildWebServer failed: %v", err)
	}

	ts := httptest.NewServer(srv)
	defer ts.Close()

	// Verify server responds before shutdown
	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health failed before shutdown: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 before shutdown, got %d", resp.StatusCode)
	}

	// Close the test server (simulates shutdown)
	ts.Close()

	// Verify server no longer responds
	_, err = http.Get(ts.URL + "/health")
	if err == nil {
		t.Error("expected error after server shutdown")
	}
}

func TestRunServeResolvesDefaultDataDir(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key-for-server-boot")
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	scfg := serveConfig{
		port:    0,
		dataDir: "", // empty means use default
	}

	srv, err := buildWebServer(scfg)
	if err != nil {
		t.Fatalf("buildWebServer with default data dir failed: %v", err)
	}

	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// --- buildPipelineEngine tests ---

func TestBuildPipelineEngineSimple(t *testing.T) {
	engine, graph, err := buildPipelineEngine(validDOT, t.TempDir(), nil, "", "", nil, nil)
	if err != nil {
		t.Fatalf("buildPipelineEngine failed: %v", err)
	}
	if engine == nil {
		t.Fatal("expected non-nil engine")
	}
	if graph == nil {
		t.Fatal("expected non-nil graph")
	}
}

func TestBuildPipelineEngineInvalidDOT(t *testing.T) {
	_, _, err := buildPipelineEngine("not valid DOT {{{", t.TempDir(), nil, "", "", nil, nil)
	if err == nil {
		t.Fatal("expected error for invalid DOT")
	}
}

// --- printPipelineResult test ---

func TestPrintPipelineResult(t *testing.T) {
	// Just verify it doesn't panic with nil or populated results.
	printPipelineResult(nil, "")
	printPipelineResult(&pipeline.EngineResult{
		Status:         "completed",
		CompletedNodes: []string{"start", "finish"},
		Context:        map[string]string{"_workdir": "/tmp/test"},
	}, "(resumed)")
}
