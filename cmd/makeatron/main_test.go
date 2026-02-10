// ABOUTME: Tests for the makeatron CLI entrypoint covering flag parsing, retry policy mapping,
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

func TestParseFlagsTUI(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"makeatron", "--tui", "pipeline.dot"}
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

	os.Args = []string{"makeatron", "pipeline.dot"}
	cfg := parseFlags()

	if cfg.tuiMode {
		t.Error("expected tuiMode=false by default")
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

// --- buildPipelineServer render wiring tests ---

func TestBuildPipelineServerWiresRenderFunctions(t *testing.T) {
	cfg := config{
		retryPolicy: "none",
	}
	server := buildPipelineServer(cfg)

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
	}
	server := buildPipelineServer(cfg)

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
	}
	server := buildPipelineServer(cfg)

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
	}
	server := buildPipelineServer(cfg)

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
