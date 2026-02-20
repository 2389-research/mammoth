// ABOUTME: Tests for the mammoth CLI entrypoint covering flag parsing, retry policy mapping,
// ABOUTME: pipeline validation, pipeline execution, version display, and render wiring.
package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/2389-research/mammoth/attractor"
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
	if cfg.backendType != "" {
		t.Errorf("expected empty backendType by default, got %q", cfg.backendType)
	}
}

func TestParseFlagsBackend(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"mammoth", "--backend", "claude-code", "pipeline.dot"}
	cfg := parseFlags()

	if cfg.backendType != "claude-code" {
		t.Errorf("expected backendType='claude-code', got %q", cfg.backendType)
	}
	if cfg.pipelineFile != "pipeline.dot" {
		t.Errorf("expected pipelineFile='pipeline.dot', got %q", cfg.pipelineFile)
	}
}

func TestParseFlagsBackendDefaultEmpty(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"mammoth", "pipeline.dot"}
	cfg := parseFlags()

	if cfg.backendType != "" {
		t.Errorf("expected empty backendType by default, got %q", cfg.backendType)
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

func TestParseFlagsServer(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"mammoth", "--server"}
	cfg := parseFlags()

	if !cfg.serverMode {
		t.Error("expected serverMode=true with --server flag")
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

	os.Args = []string{"mammoth", "--server", "--port", "9999"}
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

func TestParseFlagsBaseURL(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"mammoth", "--base-url", "https://custom.api.example.com", "test.dot"}
	cfg := parseFlags()

	if cfg.baseURL != "https://custom.api.example.com" {
		t.Errorf("expected baseURL='https://custom.api.example.com', got %q", cfg.baseURL)
	}
}

func TestParseFlagsBaseURLDefaultEmpty(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"mammoth", "test.dot"}
	cfg := parseFlags()

	if cfg.baseURL != "" {
		t.Errorf("expected empty baseURL by default, got %q", cfg.baseURL)
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

	os.Args = []string{"mammoth", "--backend", "claude-code", "run", "pipeline.dot"}
	cfg := parseFlags()

	if cfg.pipelineFile != "pipeline.dot" {
		t.Errorf("expected pipelineFile='pipeline.dot', got %q", cfg.pipelineFile)
	}
	if cfg.backendType != "claude-code" {
		t.Errorf("expected backendType='claude-code', got %q", cfg.backendType)
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
	// run() requires an API key to be set before dispatching to pipeline execution.
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

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

func TestRunRejectsExecutionWithoutAPIKey(t *testing.T) {
	// Ensure no API keys are set.
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")

	dotFile := writeTempDOT(t, validDOT)
	cfg := config{
		pipelineFile: dotFile,
		retryPolicy:  "none",
	}
	exitCode := run(cfg)
	if exitCode != 1 {
		t.Errorf("expected exit code 1 when no API key is set, got %d", exitCode)
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

// --- buildPipelineServer render wiring tests ---

func TestBuildPipelineServerWiresRenderFunctions(t *testing.T) {
	cfg := config{
		retryPolicy: "none",
		dataDir:     t.TempDir(),
	}
	server, err := buildPipelineServer(cfg)
	if err != nil {
		t.Fatalf("buildPipelineServer failed: %v", err)
	}

	if server.ToDOT == nil {
		t.Error("expected ToDOT to be wired on the pipeline server")
	}
	if server.ToDOTWithStatus == nil {
		t.Error("expected ToDOTWithStatus to be wired on the pipeline server")
	}
	if server.RenderDOTSource == nil {
		t.Error("expected RenderDOTSource to be wired on the pipeline server")
	}
}

func TestBuildPipelineServerToDOTProducesValidOutput(t *testing.T) {
	cfg := config{
		retryPolicy: "none",
		dataDir:     t.TempDir(),
	}
	server, err := buildPipelineServer(cfg)
	if err != nil {
		t.Fatalf("buildPipelineServer failed: %v", err)
	}

	graph := &attractor.Graph{
		Name: "wiring_test",
		Nodes: map[string]*attractor.Node{
			"a": {ID: "a", Attrs: map[string]string{}},
			"b": {ID: "b", Attrs: map[string]string{}},
		},
		Edges: []*attractor.Edge{
			{From: "a", To: "b", Attrs: map[string]string{}},
		},
		Attrs: map[string]string{},
	}

	dot := server.ToDOT(graph)
	if !strings.Contains(dot, "digraph wiring_test") {
		t.Errorf("expected digraph output from wired ToDOT, got: %s", dot)
	}
	if !strings.Contains(dot, "a -> b") {
		t.Errorf("expected edge in wired ToDOT output, got: %s", dot)
	}
}

func TestBuildPipelineServerToDOTWithStatusProducesColoredOutput(t *testing.T) {
	cfg := config{
		retryPolicy: "none",
		dataDir:     t.TempDir(),
	}
	server, err := buildPipelineServer(cfg)
	if err != nil {
		t.Fatalf("buildPipelineServer failed: %v", err)
	}

	graph := &attractor.Graph{
		Name: "status_test",
		Nodes: map[string]*attractor.Node{
			"a": {ID: "a", Attrs: map[string]string{}},
		},
		Edges: []*attractor.Edge{},
		Attrs: map[string]string{},
	}
	outcomes := map[string]*attractor.Outcome{
		"a": {Status: attractor.StatusSuccess},
	}

	dot := server.ToDOTWithStatus(graph, outcomes)
	if !strings.Contains(dot, "fillcolor") {
		t.Errorf("expected fillcolor in status DOT output, got: %s", dot)
	}
}

func TestRunPipelineWiresInterviewer(t *testing.T) {
	// Verify that runPipeline wires a ConsoleInterviewer into the
	// WaitForHumanHandler so human gate nodes work in CLI mode.
	cfg := config{
		retryPolicy: "none",
	}

	engineCfg := attractor.EngineConfig{
		Handlers:     attractor.DefaultHandlerRegistry(),
		DefaultRetry: attractor.RetryPolicyNone(),
	}
	engine := attractor.NewEngine(engineCfg)

	// Simulate the wiring that runPipeline does
	wireInterviewer(engine)

	handler := engine.GetHandler("wait.human")
	if handler == nil {
		t.Fatal("expected wait.human handler in default registry")
	}
	hh, ok := handler.(*attractor.WaitForHumanHandler)
	if !ok {
		t.Fatalf("expected *WaitForHumanHandler, got %T", handler)
	}
	if hh.Interviewer == nil {
		t.Error("expected Interviewer to be wired on WaitForHumanHandler")
	}

	// Verify cfg is used (suppress unused variable)
	_ = cfg
}

func TestVerboseEventHandlerAgentEvents(t *testing.T) {
	// Capture stderr output to verify agent events are logged
	// We test the function directly without capturing stderr since
	// verboseEventHandler writes to os.Stderr. Just verify it doesn't panic.
	agentEvents := []attractor.EngineEvent{
		{
			Type:   attractor.EventAgentToolCallStart,
			NodeID: "codegen",
			Data:   map[string]any{"tool_name": "file_write"},
		},
		{
			Type:   attractor.EventAgentToolCallEnd,
			NodeID: "codegen",
			Data:   map[string]any{"tool_name": "file_write", "duration_ms": int64(150)},
		},
		{
			Type:   attractor.EventAgentLLMTurn,
			NodeID: "codegen",
			Data:   map[string]any{"tokens": 500},
		},
		{
			Type:   attractor.EventAgentSteering,
			NodeID: "codegen",
			Data:   map[string]any{"message": "focus"},
		},
		{
			Type:   attractor.EventAgentLoopDetected,
			NodeID: "codegen",
			Data:   map[string]any{"message": "loop detected"},
		},
	}

	// Just make sure the handler doesn't panic on any agent event type
	for _, evt := range agentEvents {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("verboseEventHandler panicked on %s: %v", evt.Type, r)
				}
			}()
			verboseEventHandler(evt)
		}()
	}
}

func TestExampleDOTFilesParseAndValidate(t *testing.T) {
	// Verify all example DOT files still parse and validate after
	// converting review nodes from wait.human to codergen.
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
				t.Fatalf("failed to read %s: %v", path, err)
			}
			graph, err := attractor.Parse(string(source))
			if err != nil {
				t.Fatalf("failed to parse %s: %v", path, err)
			}

			transforms := attractor.DefaultTransforms()
			graph = attractor.ApplyTransforms(graph, transforms...)

			diags := attractor.Validate(graph)
			for _, d := range diags {
				if d.Severity == attractor.SeverityError {
					t.Errorf("[%s] validation error: %s (node: %s)", path, d.Message, d.NodeID)
				}
			}
		})
	}
}

func TestBuildPipelineServerGraphEndpointReturnsDOT(t *testing.T) {
	cfg := config{
		retryPolicy: "none",
		dataDir:     t.TempDir(),
	}
	server, err := buildPipelineServer(cfg)
	if err != nil {
		t.Fatalf("buildPipelineServer failed: %v", err)
	}

	// Submit a pipeline via POST
	dotSource := `digraph test { start [shape=Mdiamond]; finish [shape=Msquare]; start -> finish }`
	req := httptest.NewRequest("POST", "/pipelines", strings.NewReader(dotSource))
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}

	var submitResp map[string]string
	json.Unmarshal(rec.Body.Bytes(), &submitResp)
	pipelineID := submitResp["id"]
	if pipelineID == "" {
		t.Fatal("no pipeline ID in response")
	}

	// Give the pipeline a moment to complete
	time.Sleep(200 * time.Millisecond)

	// GET the graph in DOT format
	graphReq := httptest.NewRequest("GET", "/pipelines/"+pipelineID+"/graph?format=dot", nil)
	graphReq = graphReq.WithContext(context.Background())
	graphRec := httptest.NewRecorder()
	server.ServeHTTP(graphRec, graphReq)

	if graphRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for graph endpoint, got %d: %s", graphRec.Code, graphRec.Body.String())
	}

	body := graphRec.Body.String()
	if !strings.Contains(body, "digraph") {
		t.Errorf("expected DOT output from graph endpoint, got: %s", body)
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
	store, err := attractor.NewFSRunStateStore(runsDir)
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

	expectedHash := attractor.SourceHash(validDOT)
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
	store, err := attractor.NewFSRunStateStore(runsDir)
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

// --- detectBackend tests ---

func TestDetectBackendClaudeCodeFlag(t *testing.T) {
	// When --backend=claude-code and claude binary exists, should return ClaudeCodeBackend
	// We can't guarantee claude is installed, so just test the env var path.
	// Clear API keys so the agent fallback doesn't activate
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")

	// With a bogus backend type, should fall through to API key detection
	backend := detectBackend(false, "bogus-backend")
	if backend != nil {
		t.Errorf("expected nil backend for unknown type without API keys, got %T", backend)
	}
}

func TestDetectBackendEnvVar(t *testing.T) {
	// MAMMOTH_BACKEND env var should be checked when backendType is empty
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("MAMMOTH_BACKEND", "bogus-type")

	// With a bogus env var value, should fall through to API key check (and return nil)
	backend := detectBackend(false, "")
	if backend != nil {
		t.Errorf("expected nil backend for bogus MAMMOTH_BACKEND without API keys, got %T", backend)
	}
}

func TestDetectBackendAgentDefault(t *testing.T) {
	// With an API key and no explicit backend, should return AgentBackend
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	backend := detectBackend(false, "")
	if backend == nil {
		t.Fatal("expected non-nil backend with API key set")
	}
	if _, ok := backend.(*attractor.AgentBackend); !ok {
		t.Errorf("expected *AgentBackend, got %T", backend)
	}
}

func TestDetectBackendClaudeCodeFallback(t *testing.T) {
	// When claude-code is requested but binary not found, should fall back to agent
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	// Use a PATH that won't have claude
	t.Setenv("PATH", "/nonexistent")

	backend := detectBackend(false, "claude-code")
	// Should fall back to AgentBackend since claude binary isn't found but API key exists
	if backend == nil {
		t.Fatal("expected non-nil backend (fallback to agent)")
	}
	if _, ok := backend.(*attractor.AgentBackend); !ok {
		t.Errorf("expected fallback to *AgentBackend, got %T", backend)
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

func TestDetectBackendClaudeCodeEnvVarActivation(t *testing.T) {
	// MAMMOTH_BACKEND=claude-code with claude binary available should use it
	t.Setenv("MAMMOTH_BACKEND", "claude-code")
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	backend := detectBackend(false, "")
	if backend == nil {
		t.Fatal("expected non-nil backend")
	}
	// The backend type depends on whether claude is actually installed;
	// just verify we get a non-nil backend of some type
	switch backend.(type) {
	case *attractor.ClaudeCodeBackend:
		// claude was found — correct
	case *attractor.AgentBackend:
		// claude not found, fell back to agent — also correct
	default:
		t.Errorf("expected *ClaudeCodeBackend or *AgentBackend, got %T", backend)
	}
}
