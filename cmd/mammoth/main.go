// ABOUTME: CLI entrypoint for the mammoth pipeline runner with run, validate, server, and serve modes.
// ABOUTME: Wires together the attractor engine, HTTP server, web UI, retry policies, and signal handling.
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
	"github.com/2389-research/mammoth/web"

	tea "github.com/charmbracelet/bubbletea"
)

var version = "dev"

// config holds all CLI configuration parsed from flags and positional arguments.
type config struct {
	serverMode    bool
	port          int
	validateOnly  bool
	tuiMode       bool
	fresh         bool
	checkpointDir string
	artifactDir   string
	dataDir       string
	retryPolicy   string
	baseURL       string
	backendType   string
	verbose       bool
	showVersion   bool
	pipelineFile  string
}

// serveConfig holds configuration for the "mammoth serve" subcommand.
type serveConfig struct {
	port    int
	dataDir string
}

func main() {
	loadDotEnvAuto()

	// Check for subcommands before regular flag parsing, since they use
	// their own flag sets and don't share flags with pipeline mode.
	if len(os.Args) > 1 {
		if scfg, ok := parseServeArgs(os.Args[1:]); ok {
			os.Exit(runServe(scfg))
		}
		if scfg, ok := parseSetupArgs(os.Args[1:]); ok {
			os.Exit(runSetup(scfg))
		}
	}

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
	fs.StringVar(&cfg.backendType, "backend", "", "Agent backend: agent (default), claude-code")
	fs.BoolVar(&cfg.tuiMode, "tui", false, "Run with interactive terminal UI")
	fs.BoolVar(&cfg.fresh, "fresh", false, "Force a fresh run, skip auto-resume")
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

	// Accept optional "run" subcommand: `mammoth run pipeline.dot` is equivalent
	// to `mammoth pipeline.dot`.
	argIdx := 0
	if fs.NArg() > 0 && fs.Arg(0) == "run" {
		argIdx = 1
	}
	if fs.NArg() > argIdx {
		cfg.pipelineFile = fs.Arg(argIdx)
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
		return 1
	}

	if cfg.validateOnly {
		return validatePipeline(cfg)
	}

	// Any mode that actually executes a pipeline needs an LLM backend.
	// Check for API keys before doing anything else.
	if detectBackend(false, cfg.backendType) == nil {
		fmt.Fprintln(os.Stderr, "error: no LLM API key found")
		fmt.Fprintln(os.Stderr, "Set one of: ANTHROPIC_API_KEY, OPENAI_API_KEY, or GEMINI_API_KEY")
		fmt.Fprintln(os.Stderr, "Or use --backend claude-code to use the Claude Code CLI")
		return 1
	}

	if cfg.tuiMode {
		return runPipelineWithTUI(cfg)
	}

	return runPipeline(cfg)
}

// runPipeline reads a DOT file and executes the pipeline. When a TTY is
// available, it uses an inline Bubble Tea progress display. Otherwise it
// falls back to direct execution with optional verbose event logging.
//
// Supports auto-resume: if the same DOT file was previously run and failed,
// the pipeline automatically resumes from the last checkpoint. Use -fresh
// to force a new run.
func runPipeline(cfg config) int {
	source, err := os.ReadFile(cfg.pipelineFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	// Parse the graph so we can display the node list in the inline TUI.
	graph, err := attractor.Parse(string(source))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	transforms := attractor.DefaultTransforms()
	graph = attractor.ApplyTransforms(graph, transforms...)

	// Compute content hash for auto-resume matching
	sourceHash := attractor.SourceHash(string(source))

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

	// Auto-resume: check for a previous failed/interrupted run with the same source hash
	if store != nil && !cfg.fresh {
		resumeState, findErr := store.FindResumable(sourceHash)
		if findErr != nil {
			fmt.Fprintf(os.Stderr, "warning: could not check for resumable runs: %v\n", findErr)
		}
		if resumeState != nil {
			return runPipelineResume(cfg, graph, store, resumeState, string(source), sourceHash)
		}
	}

	return runPipelineFresh(cfg, graph, store, string(source), sourceHash)
}

// runPipelineResume resumes a previously failed/interrupted pipeline run from its checkpoint.
func runPipelineResume(
	cfg config,
	graph *attractor.Graph,
	store *attractor.FSRunStateStore,
	resumeState *attractor.RunState,
	source string,
	sourceHash string,
) int {
	cpPath := store.CheckpointPath(resumeState.ID)

	engineCfg := attractor.EngineConfig{
		CheckpointDir:      cfg.checkpointDir,
		AutoCheckpointPath: cpPath,
		ArtifactDir:        cfg.artifactDir,
		DefaultRetry:       retryPolicyFromName(cfg.retryPolicy),
		Handlers:           attractor.DefaultHandlerRegistry(),
		Backend:            detectBackend(cfg.verbose, cfg.backendType),
		BaseURL:            cfg.baseURL,
		RunID:              resumeState.ID,
	}

	engine := attractor.NewEngine(engineCfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Update the existing run state to "running" and clear any previous error
	resumeState.Status = "running"
	resumeState.Error = ""
	if err := store.Update(resumeState); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not update run state: %v\n", err)
	}

	var result *attractor.RunResult
	var runErr error

	if isTerminal() {
		result, runErr = runPipelineResumeWithStream(cfg, graph, engine, ctx, cpPath, resumeState)
	} else {
		result, runErr = runPipelineResumeDirect(cfg, engine, ctx, graph, cpPath)
	}

	// Persist final run state
	now := time.Now()
	resumeState.CompletedAt = &now
	resumeState.SourceHash = sourceHash
	if runErr != nil {
		if errors.Is(runErr, context.Canceled) {
			resumeState.Status = "cancelled"
		} else {
			resumeState.Status = "failed"
		}
		resumeState.Error = runErr.Error()
	} else {
		resumeState.Status = "completed"
		if result != nil {
			resumeState.CompletedNodes = result.CompletedNodes
			if result.Context != nil {
				resumeState.Context = result.Context.Snapshot()
			}
		}
	}
	if err := store.Update(resumeState); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not persist final state: %v\n", err)
	}

	if runErr != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", runErr)
		return 1
	}

	return 0
}

// runPipelineResumeWithStream resumes pipeline execution using the inline Bubble Tea display.
func runPipelineResumeWithStream(
	cfg config,
	graph *attractor.Graph,
	engine *attractor.Engine,
	ctx context.Context,
	cpPath string,
	resumeState *attractor.RunState,
) (*attractor.RunResult, error) {
	// Load checkpoint to find which node we're resuming from
	cp, err := attractor.LoadCheckpoint(cpPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load checkpoint: %w", err)
	}

	resumeInfo := &tui.ResumeInfo{
		ResumedFrom:   cp.CurrentNode,
		PreviousNodes: cp.CompletedNodes,
	}

	model := tui.NewStreamModel(graph, engine, cfg.pipelineFile, ctx, cfg.verbose, tui.WithResumeInfo(resumeInfo))

	p := tea.NewProgram(model)

	bridge := tui.NewEventBridge(p.Send)
	engine.SetEventHandler(bridge.HandleEvent)

	tui.WireHumanGate(engine, model.HumanGate())

	// Replace the pipeline command with a resume command
	model.SetResumeCmd(func() tea.Cmd {
		return tui.ResumeFromCheckpointCmd(ctx, engine, graph, cpPath)
	})

	if _, err := p.Run(); err != nil {
		return nil, err
	}

	select {
	case pipelineResult := <-model.ResultCh():
		return pipelineResult.Result, pipelineResult.Err
	default:
		return nil, context.Canceled
	}
}

// runPipelineResumeDirect resumes pipeline execution with direct output (no TUI).
func runPipelineResumeDirect(
	cfg config,
	engine *attractor.Engine,
	ctx context.Context,
	graph *attractor.Graph,
	cpPath string,
) (*attractor.RunResult, error) {
	if cfg.verbose {
		engine.SetEventHandler(verboseEventHandler)
	}

	wireInterviewer(engine)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Fprintln(os.Stderr, "\nInterrupted, shutting down...")
		cancel()
	}()

	fmt.Fprintf(os.Stderr, "Resuming pipeline from checkpoint...\n")
	result, runErr := engine.ResumeFromCheckpoint(ctx, graph, cpPath)
	signal.Stop(sigChan)

	if runErr != nil {
		return result, runErr
	}

	fmt.Printf("Pipeline completed successfully (resumed).\n")
	fmt.Printf("Completed nodes: %v\n", result.CompletedNodes)
	if result.FinalOutcome != nil {
		fmt.Printf("Final status: %s\n", result.FinalOutcome.Status)
	}
	if result.Context != nil {
		if wd := result.Context.GetString("_workdir", ""); wd != "" {
			fmt.Printf("Output directory: %s\n", wd)
		}
	}

	return result, nil
}

// runPipelineFresh starts a new pipeline run with auto-checkpoint enabled.
func runPipelineFresh(
	cfg config,
	graph *attractor.Graph,
	store *attractor.FSRunStateStore,
	source string,
	sourceHash string,
) int {
	// Generate a run ID for tracking
	runID, err := attractor.GenerateRunID()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	// Determine auto-checkpoint path
	var autoCheckpointPath string
	if store != nil {
		autoCheckpointPath = store.CheckpointPath(runID)
	}

	engineCfg := attractor.EngineConfig{
		CheckpointDir:      cfg.checkpointDir,
		AutoCheckpointPath: autoCheckpointPath,
		ArtifactDir:        cfg.artifactDir,
		DefaultRetry:       retryPolicyFromName(cfg.retryPolicy),
		Handlers:           attractor.DefaultHandlerRegistry(),
		Backend:            detectBackend(cfg.verbose, cfg.backendType),
		BaseURL:            cfg.baseURL,
		RunID:              runID,
	}

	engine := attractor.NewEngine(engineCfg)

	// Create a cancellable context.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Persist initial run state
	startTime := time.Now()
	if store != nil {
		initialState := &attractor.RunState{
			ID:             runID,
			PipelineFile:   cfg.pipelineFile,
			Status:         "running",
			Source:         source,
			SourceHash:     sourceHash,
			StartedAt:      startTime,
			CompletedNodes: []string{},
			Context:        map[string]any{},
			Events:         []attractor.EngineEvent{},
		}
		if err := store.Create(initialState); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not persist initial state: %v\n", err)
		}
	}

	// Choose between inline streaming TUI (interactive) and direct execution
	// (non-interactive: tests, CI, piped output).
	var result *attractor.RunResult
	var runErr error

	if isTerminal() {
		result, runErr = runPipelineWithStream(cfg, graph, engine, ctx, source)
	} else {
		result, runErr = runPipelineDirect(cfg, engine, ctx, source)
	}

	// Persist final run state
	if store != nil {
		now := time.Now()
		finalState := &attractor.RunState{
			ID:           runID,
			PipelineFile: cfg.pipelineFile,
			StartedAt:    startTime,
			CompletedAt:  &now,
			Source:       source,
			SourceHash:   sourceHash,
			Context:      map[string]any{},
			Events:       []attractor.EngineEvent{},
		}
		if runErr != nil {
			if errors.Is(runErr, context.Canceled) {
				finalState.Status = "cancelled"
			} else {
				finalState.Status = "failed"
			}
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

	return 0
}

// runPipelineWithStream executes the pipeline using the inline Bubble Tea
// streaming progress display. Returns the pipeline result and error.
func runPipelineWithStream(
	cfg config,
	graph *attractor.Graph,
	engine *attractor.Engine,
	ctx context.Context,
	source string,
) (*attractor.RunResult, error) {
	model := tui.NewStreamModel(graph, engine, cfg.pipelineFile, ctx, cfg.verbose)

	p := tea.NewProgram(model)

	bridge := tui.NewEventBridge(p.Send)
	engine.SetEventHandler(bridge.HandleEvent)

	tui.WireHumanGate(engine, model.HumanGate())

	if _, err := p.Run(); err != nil {
		return nil, err
	}

	// Read the pipeline result from the model's channel.
	select {
	case pipelineResult := <-model.ResultCh():
		return pipelineResult.Result, pipelineResult.Err
	default:
		return nil, context.Canceled
	}
}

// runPipelineDirect executes the pipeline with simple signal handling and
// optional verbose logging (used when no TTY is available).
func runPipelineDirect(
	cfg config,
	engine *attractor.Engine,
	ctx context.Context,
	source string,
) (*attractor.RunResult, error) {
	if cfg.verbose {
		engine.SetEventHandler(verboseEventHandler)
	}

	// Wire CLI interviewer for human gate nodes
	wireInterviewer(engine)

	// Set up signal handling for graceful cancellation.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Fprintln(os.Stderr, "\nInterrupted, shutting down...")
		cancel()
	}()

	result, runErr := engine.Run(ctx, source)
	signal.Stop(sigChan)

	if runErr != nil {
		return result, runErr
	}

	fmt.Printf("Pipeline completed successfully.\n")
	fmt.Printf("Completed nodes: %v\n", result.CompletedNodes)
	if result.FinalOutcome != nil {
		fmt.Printf("Final status: %s\n", result.FinalOutcome.Status)
	}
	if result.Context != nil {
		if wd := result.Context.GetString("_workdir", ""); wd != "" {
			fmt.Printf("Output directory: %s\n", wd)
		}
	}

	return result, nil
}

// isTerminal returns true if stdout is connected to a terminal (TTY).
// Returns false in tests, CI, or when output is piped.
func isTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
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
		Backend:       detectBackend(cfg.verbose, cfg.backendType),
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
		CheckpointDir: cfg.checkpointDir,
		ArtifactDir:   cfg.artifactDir,
		DefaultRetry:  retryPolicyFromName(cfg.retryPolicy),
		Handlers:      attractor.DefaultHandlerRegistry(),
		Backend:       detectBackend(cfg.verbose, cfg.backendType),
		BaseURL:       cfg.baseURL,
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
	if detectBackend(false, cfg.backendType) == nil {
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

// parseServeArgs checks whether args starts with the "serve" subcommand and,
// if so, parses serve-specific flags. Returns the serve config and true if
// "serve" was detected, or a zero value and false otherwise.
func parseServeArgs(args []string) (serveConfig, bool) {
	if len(args) == 0 || args[0] != "serve" {
		return serveConfig{}, false
	}

	var scfg serveConfig
	fs := flag.NewFlagSet("mammoth serve", flag.ContinueOnError)
	fs.IntVar(&scfg.port, "port", 2389, "Server port (default: 2389)")
	fs.StringVar(&scfg.dataDir, "data-dir", "", "Data directory for projects (default: $XDG_DATA_HOME/mammoth)")

	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: mammoth serve [flags]")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Start the unified web server for the mammoth wizard flow.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Flags:")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		os.Exit(2)
	}

	return scfg, true
}

// buildWebServer creates a web.Server for the "mammoth serve" subcommand,
// resolving the data directory and configuring the listen address.
func buildWebServer(scfg serveConfig) (*web.Server, error) {
	dataDir := scfg.dataDir
	if dataDir == "" {
		resolved, err := resolveDataDir("")
		if err != nil {
			return nil, fmt.Errorf("resolve data dir: %w", err)
		}
		dataDir = resolved
	}

	addr := fmt.Sprintf("127.0.0.1:%d", scfg.port)
	srv, err := web.NewServer(web.ServerConfig{
		Addr:      addr,
		Workspace: web.NewGlobalWorkspace(dataDir),
	})
	if err != nil {
		return nil, fmt.Errorf("create web server: %w", err)
	}
	return srv, nil
}

// runServe starts the unified web server for the mammoth wizard flow. It
// listens on the configured port and blocks until SIGINT or SIGTERM.
func runServe(scfg serveConfig) int {
	srv, err := buildWebServer(scfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	addr := fmt.Sprintf("127.0.0.1:%d", scfg.port)

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
		Handler: srv,
	}

	go func() {
		<-ctx.Done()
		httpServer.Close()
	}()

	fmt.Fprintf(os.Stderr, "mammoth web UI: http://%s\n", addr)
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

// detectBackend selects the agent backend based on the --backend flag,
// MAMMOTH_BACKEND env var, or auto-detection from API keys.
func detectBackend(verbose bool, backendType string) attractor.CodergenBackend {
	// Check env var fallback when no explicit flag
	if backendType == "" {
		backendType = os.Getenv("MAMMOTH_BACKEND")
	}

	if backendType == "claude-code" {
		backend, err := attractor.NewClaudeCodeBackend()
		if err != nil {
			fmt.Fprintf(os.Stderr, "[backend] claude-code: %v, falling back to agent\n", err)
		} else {
			if verbose {
				fmt.Fprintf(os.Stderr, "[backend] using ClaudeCodeBackend (%s)\n", backend.BinaryPath)
			}
			return backend
		}
	}

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
