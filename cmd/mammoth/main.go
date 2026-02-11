// ABOUTME: CLI entrypoint for the mammoth pipeline runner with run, validate, and server modes.
// ABOUTME: Wires together the attractor engine, HTTP server, retry policies, and signal handling.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/2389-research/mammoth/attractor"
	"github.com/2389-research/mammoth/render"
	"github.com/2389-research/mammoth/tui"

	tea "github.com/charmbracelet/bubbletea"
)

var version = "dev"

// config holds all CLI configuration parsed from flags and positional arguments.
type config struct {
	serverMode    bool
	port          int
	validateOnly  bool
	tuiMode       bool
	checkpointDir string
	artifactDir   string
	dataDir       string
	retryPolicy   string
	baseURL       string
	verbose       bool
	showVersion   bool
	pipelineFile  string
}

func main() {
	loadDotEnv(".env")

	cfg := parseFlags()

	if cfg.showVersion {
		fmt.Printf("mammoth %s\n", version)
		os.Exit(0)
	}

	os.Exit(run(cfg))
}

// parseFlags parses command-line flags and returns a populated config.
func parseFlags() config {
	var cfg config

	fs := flag.NewFlagSet("mammoth", flag.ContinueOnError)
	fs.BoolVar(&cfg.serverMode, "server", false, "Start HTTP server mode")
	fs.IntVar(&cfg.port, "port", 2389, "Server port (default: 2389)")
	fs.BoolVar(&cfg.validateOnly, "validate", false, "Validate pipeline without executing")
	fs.StringVar(&cfg.checkpointDir, "checkpoint-dir", "", "Directory for checkpoint files")
	fs.StringVar(&cfg.artifactDir, "artifact-dir", "", "Directory for artifact storage")
	fs.StringVar(&cfg.dataDir, "data-dir", "", "Data directory for persistent state (default: $XDG_DATA_HOME/mammoth)")
	fs.StringVar(&cfg.retryPolicy, "retry", "none", "Default retry policy: none, standard, aggressive, linear, patient")
	fs.StringVar(&cfg.baseURL, "base-url", "", "Custom API base URL for the LLM provider")
	fs.BoolVar(&cfg.tuiMode, "tui", false, "Run with interactive terminal UI")
	fs.BoolVar(&cfg.verbose, "verbose", false, "Verbose output")
	fs.BoolVar(&cfg.showVersion, "version", false, "Print version and exit")

	fs.Usage = func() {
		printHelp(os.Stderr, version)
	}

	if err := fs.Parse(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		os.Exit(2)
	}

	if fs.NArg() > 0 {
		cfg.pipelineFile = fs.Arg(0)
	}

	return cfg
}

// run dispatches to the appropriate mode based on the config.
// Returns an exit code: 0 for success, 1 for failure.
func run(cfg config) int {
	if cfg.serverMode {
		return runServer(cfg)
	}

	if cfg.pipelineFile == "" {
		printHelp(os.Stderr, version)
		return 0
	}

	if cfg.validateOnly {
		return validatePipeline(cfg)
	}

	// Any mode that actually executes a pipeline needs an LLM backend.
	// Check for API keys before doing anything else.
	if detectBackend(false) == nil {
		fmt.Fprintln(os.Stderr, "error: no LLM API key found")
		fmt.Fprintln(os.Stderr, "Set one of: ANTHROPIC_API_KEY, OPENAI_API_KEY, or GEMINI_API_KEY")
		return 1
	}

	if cfg.tuiMode {
		return runPipelineWithTUI(cfg)
	}

	return runPipeline(cfg)
}

// runPipeline reads a DOT file and executes the pipeline through the engine.
func runPipeline(cfg config) int {
	source, err := os.ReadFile(cfg.pipelineFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	// Resolve data directory for persistent state
	dataDir, err := resolveDataDir(cfg.dataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not resolve data dir: %v\n", err)
	}

	// Set up persistent run state store
	var store *attractor.FSRunStateStore
	if dataDir != "" {
		runsDir := dataDir + "/runs"
		store, err = attractor.NewFSRunStateStore(runsDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not create run state store: %v\n", err)
		}
	}

	// Generate a run ID for tracking
	runID, err := attractor.GenerateRunID()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	engineCfg := attractor.EngineConfig{
		CheckpointDir: cfg.checkpointDir,
		ArtifactDir:   cfg.artifactDir,
		DefaultRetry:  retryPolicyFromName(cfg.retryPolicy),
		Handlers:      attractor.DefaultHandlerRegistry(),
		Backend:       detectBackend(cfg.verbose),
		BaseURL:       cfg.baseURL,
		RunID:         runID,
	}

	if cfg.verbose {
		engineCfg.EventHandler = verboseEventHandler
	}

	engine := attractor.NewEngine(engineCfg)

	// Wire CLI interviewer for human gate nodes
	wireInterviewer(engine)

	// Persist initial run state
	startTime := time.Now()
	if store != nil {
		initialState := &attractor.RunState{
			ID:             runID,
			PipelineFile:   cfg.pipelineFile,
			Status:         "running",
			Source:         string(source),
			StartedAt:      startTime,
			CompletedNodes: []string{},
			Context:        map[string]any{},
			Events:         []attractor.EngineEvent{},
		}
		if err := store.Create(initialState); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not persist initial state: %v\n", err)
		}
	}

	// Set up context with signal handling for graceful cancellation.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Fprintln(os.Stderr, "\nInterrupted, shutting down...")
		cancel()
	}()

	result, runErr := engine.Run(ctx, string(source))

	// Persist final run state
	if store != nil {
		now := time.Now()
		finalState := &attractor.RunState{
			ID:           runID,
			PipelineFile: cfg.pipelineFile,
			StartedAt:    startTime,
			CompletedAt:  &now,
			Source:       string(source),
			Context:      map[string]any{},
			Events:       []attractor.EngineEvent{},
		}
		if runErr != nil {
			finalState.Status = "failed"
			finalState.Error = runErr.Error()
		} else {
			finalState.Status = "completed"
			if result != nil {
				finalState.CompletedNodes = result.CompletedNodes
				if result.Context != nil {
					finalState.Context = result.Context.Snapshot()
				}
			}
		}
		if err := store.Update(finalState); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not persist final state: %v\n", err)
		}
	}

	if runErr != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", runErr)
		return 1
	}

	// Print results to stdout.
	fmt.Printf("Pipeline completed successfully.\n")
	fmt.Printf("Completed nodes: %v\n", result.CompletedNodes)
	if result.FinalOutcome != nil {
		fmt.Printf("Final status: %s\n", result.FinalOutcome.Status)
	}

	return 0
}

// runPipelineWithTUI reads a DOT file and executes the pipeline through the
// Bubble Tea TUI, providing an interactive terminal dashboard with live DAG
// visualization, event log, node details, and human gate input.
func runPipelineWithTUI(cfg config) int {
	source, err := os.ReadFile(cfg.pipelineFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	// Parse the graph early so we can display the DAG structure in the TUI.
	graph, err := attractor.Parse(string(source))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	// Apply transforms for the TUI display (same as engine does internally).
	transforms := attractor.DefaultTransforms()
	graph = attractor.ApplyTransforms(graph, transforms...)

	engineCfg := attractor.EngineConfig{
		CheckpointDir: cfg.checkpointDir,
		ArtifactDir:   cfg.artifactDir,
		DefaultRetry:  retryPolicyFromName(cfg.retryPolicy),
		Handlers:      attractor.DefaultHandlerRegistry(),
		Backend:       detectBackend(cfg.verbose),
		BaseURL:       cfg.baseURL,
	}

	engine := attractor.NewEngine(engineCfg)

	// Create a cancellable context so quitting the TUI stops the engine.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create the TUI app model.
	model := tui.NewAppModel(graph, engine, string(source), ctx)

	// Create the Bubble Tea program with alt-screen mode.
	p := tea.NewProgram(model, tea.WithAltScreen())

	// Wire the event bridge so engine events reach the TUI.
	bridge := tui.NewEventBridge(p.Send)
	engine.SetEventHandler(bridge.HandleEvent)

	// Wire the human gate interviewer for interactive human-in-the-loop nodes.
	tui.WireHumanGate(engine, model.HumanGate())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	return 0
}

// resolveDataDir returns the data directory to use, preferring an explicit
// override and falling back to the XDG-based default.
func resolveDataDir(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	return defaultDataDir()
}

// buildPipelineServer creates a PipelineServer with the render functions and
// persistent state store wired in.
func buildPipelineServer(cfg config) (*attractor.PipelineServer, error) {
	dataDir, err := resolveDataDir(cfg.dataDir)
	if err != nil {
		return nil, fmt.Errorf("resolve data dir: %w", err)
	}

	engineCfg := attractor.EngineConfig{
		CheckpointDir:   cfg.checkpointDir,
		ArtifactDir:     cfg.artifactDir,
		DefaultRetry:    retryPolicyFromName(cfg.retryPolicy),
		Handlers:        attractor.DefaultHandlerRegistry(),
		Backend:         detectBackend(cfg.verbose),
		BaseURL:         cfg.baseURL,
	}

	if cfg.verbose {
		engineCfg.EventHandler = verboseEventHandler
	}

	engine := attractor.NewEngine(engineCfg)
	server := attractor.NewPipelineServer(engine)

	// Wire render functions into the server for graph visualization endpoints.
	server.ToDOT = render.ToDOT
	server.ToDOTWithStatus = render.ToDOTWithStatus
	server.RenderDOTSource = render.RenderDOTSource

	// Wire persistent run state store
	runsDir := dataDir + "/runs"
	store, err := attractor.NewFSRunStateStore(runsDir)
	if err != nil {
		return nil, fmt.Errorf("create run state store: %w", err)
	}
	server.SetRunStateStore(store)

	if err := server.LoadPersistedRuns(); err != nil {
		return nil, fmt.Errorf("load persisted runs: %w", err)
	}

	return server, nil
}

// runServer starts the HTTP pipeline server.
func runServer(cfg config) int {
	if detectBackend(false) == nil {
		fmt.Fprintln(os.Stderr, "warning: no LLM API key found — pipelines with codergen nodes will fail")
		fmt.Fprintln(os.Stderr, "Set one of: ANTHROPIC_API_KEY, OPENAI_API_KEY, or GEMINI_API_KEY")
	}

	server, err := buildPipelineServer(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	addr := fmt.Sprintf("127.0.0.1:%d", cfg.port)

	// Set up context with signal handling for graceful shutdown.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Fprintln(os.Stderr, "\nInterrupted, shutting down...")
		cancel()
	}()

	httpServer := &http.Server{
		Addr:    addr,
		Handler: server.Handler(),
	}

	go func() {
		<-ctx.Done()
		httpServer.Close()
	}()

	fmt.Fprintf(os.Stderr, "listening on %s\n", addr)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	return 0
}

// validatePipeline parses and validates a DOT file without executing it.
func validatePipeline(cfg config) int {
	source, err := os.ReadFile(cfg.pipelineFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	graph, err := attractor.Parse(string(source))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	transforms := attractor.DefaultTransforms()
	graph = attractor.ApplyTransforms(graph, transforms...)

	diags := attractor.Validate(graph)

	hasErrors := false
	for _, d := range diags {
		fmt.Fprintf(os.Stderr, "[%s] %s", d.Severity, d.Message)
		if d.NodeID != "" {
			fmt.Fprintf(os.Stderr, " (node: %s)", d.NodeID)
		}
		if d.Fix != "" {
			fmt.Fprintf(os.Stderr, " -- fix: %s", d.Fix)
		}
		fmt.Fprintln(os.Stderr)

		if d.Severity == attractor.SeverityError {
			hasErrors = true
		}
	}

	if hasErrors {
		fmt.Fprintf(os.Stderr, "Validation failed.\n")
		return 1
	}

	fmt.Println("Pipeline is valid.")
	return 0
}

// retryPolicyFromName maps a CLI retry policy name to an attractor RetryPolicy preset.
func retryPolicyFromName(name string) attractor.RetryPolicy {
	switch strings.ToLower(name) {
	case "none":
		return attractor.RetryPolicyNone()
	case "standard":
		return attractor.RetryPolicyStandard()
	case "aggressive":
		return attractor.RetryPolicyAggressive()
	case "linear":
		return attractor.RetryPolicyLinear()
	case "patient":
		return attractor.RetryPolicyPatient()
	default:
		return attractor.RetryPolicyNone()
	}
}

// detectBackend checks for LLM API keys in the environment and returns
// an AgentBackend if any are found, or nil for stub mode.
func detectBackend(verbose bool) attractor.CodergenBackend {
	keys := []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GEMINI_API_KEY"}
	for _, k := range keys {
		if os.Getenv(k) != "" {
			if verbose {
				fmt.Fprintf(os.Stderr, "[backend] using AgentBackend (%s detected)\n", k)
			}
			return &attractor.AgentBackend{}
		}
	}
	if verbose {
		fmt.Fprintln(os.Stderr, "[backend] no API keys found, using stub mode")
	}
	return nil
}

// wireInterviewer attaches a ConsoleInterviewer to the WaitForHumanHandler
// so human gate nodes work interactively in CLI mode.
func wireInterviewer(engine *attractor.Engine) {
	handler := engine.GetHandler("wait.human")
	if handler == nil {
		return
	}
	if hh, ok := handler.(*attractor.WaitForHumanHandler); ok {
		hh.Interviewer = attractor.NewConsoleInterviewer()
	}
}

// verboseEventHandler prints engine lifecycle events to stderr.
func verboseEventHandler(evt attractor.EngineEvent) {
	switch evt.Type {
	case attractor.EventPipelineStarted:
		fmt.Fprintf(os.Stderr, "[pipeline] started\n")
	case attractor.EventStageStarted:
		fmt.Fprintf(os.Stderr, "[stage] %s started\n", evt.NodeID)
	case attractor.EventStageCompleted:
		fmt.Fprintf(os.Stderr, "[stage] %s completed\n", evt.NodeID)
	case attractor.EventStageFailed:
		if reason, ok := evt.Data["reason"]; ok {
			fmt.Fprintf(os.Stderr, "[stage] %s failed: %v\n", evt.NodeID, reason)
		} else {
			fmt.Fprintf(os.Stderr, "[stage] %s failed\n", evt.NodeID)
		}
	case attractor.EventStageRetrying:
		fmt.Fprintf(os.Stderr, "[stage] %s retrying\n", evt.NodeID)
	case attractor.EventPipelineCompleted:
		fmt.Fprintf(os.Stderr, "[pipeline] completed\n")
	case attractor.EventPipelineFailed:
		if errVal, ok := evt.Data["error"]; ok {
			fmt.Fprintf(os.Stderr, "[pipeline] failed: %v\n", errVal)
		} else {
			fmt.Fprintf(os.Stderr, "[pipeline] failed\n")
		}
	case attractor.EventCheckpointSaved:
		fmt.Fprintf(os.Stderr, "[checkpoint] saved at %s\n", evt.NodeID)
	case attractor.EventAgentToolCallStart:
		fmt.Fprintf(os.Stderr, "[agent] %s: tool %v\n", evt.NodeID, evt.Data["tool_name"])
	case attractor.EventAgentToolCallEnd:
		fmt.Fprintf(os.Stderr, "[agent] %s: tool %v done (%vms)\n", evt.NodeID, evt.Data["tool_name"], evt.Data["duration_ms"])
	case attractor.EventAgentLLMTurn:
		if inputTok, ok := evt.Data["input_tokens"]; ok {
			fmt.Fprintf(os.Stderr, "[agent] %s: llm turn (in:%v out:%v total:%v)\n", evt.NodeID, inputTok, evt.Data["output_tokens"], evt.Data["total_tokens"])
		} else {
			fmt.Fprintf(os.Stderr, "[agent] %s: llm turn (%v tokens)\n", evt.NodeID, evt.Data["tokens"])
		}
	case attractor.EventAgentSteering:
		fmt.Fprintf(os.Stderr, "[agent] %s: steering: %v\n", evt.NodeID, evt.Data["message"])
	case attractor.EventAgentLoopDetected:
		fmt.Fprintf(os.Stderr, "[agent] %s: loop detected: %v\n", evt.NodeID, evt.Data["message"])
	}
}
