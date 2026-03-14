// ABOUTME: CLI entrypoint for the mammoth pipeline runner with run, validate, serve, and audit modes.
// ABOUTME: Wires together the tracker pipeline engine, dot parser/validator, runstate store, and signal handling.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/2389-research/mammoth/dot"
	"github.com/2389-research/mammoth/dot/validator"
	"github.com/2389-research/mammoth/llm"
	"github.com/2389-research/mammoth/runstate"
	"github.com/2389-research/mammoth/tui"
	"github.com/2389-research/mammoth/web"
	"github.com/2389-research/tracker/agent"
	"github.com/2389-research/tracker/agent/exec"
	trackerllm "github.com/2389-research/tracker/llm"
	"github.com/2389-research/tracker/llm/anthropic"
	"github.com/2389-research/tracker/llm/google"
	"github.com/2389-research/tracker/llm/openai"
	"github.com/2389-research/tracker/pipeline"
	"github.com/2389-research/tracker/pipeline/handlers"

	tea "github.com/charmbracelet/bubbletea"
)

var version = "dev"

// config holds all CLI configuration parsed from flags and positional arguments.
type config struct {
	port          int
	validateOnly  bool
	fixMode       bool
	tuiMode       bool
	fresh         bool
	artifactDir string
	dataDir       string
	retryPolicy   string
	verbose       bool
	showVersion   bool
	pipelineFile  string
}

// serveConfig holds configuration for the "mammoth serve" subcommand.
type serveConfig struct {
	port    int
	dataDir string
	global  bool
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
		if acfg, ok := parseAuditArgs(os.Args[1:]); ok {
			os.Exit(runAudit(acfg))
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
	fs.IntVar(&cfg.port, "port", 2389, "Server port (default: 2389)")
	fs.BoolVar(&cfg.validateOnly, "validate", false, "Validate pipeline without executing")
	fs.BoolVar(&cfg.fixMode, "fix", false, "Auto-fix validation warnings (use with -validate)")
	fs.StringVar(&cfg.artifactDir, "artifact-dir", ".", "Directory for artifact storage (default: current directory)")
	fs.StringVar(&cfg.dataDir, "data-dir", "", "Data directory for persistent state (default: .mammoth/ in CWD)")
	fs.StringVar(&cfg.retryPolicy, "retry", "none", "Default retry policy: none, standard, aggressive, linear, patient")
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
	if cfg.pipelineFile == "" {
		printHelp(os.Stderr, version)
		return 1
	}

	if cfg.validateOnly {
		return validatePipeline(cfg)
	}

	if cfg.tuiMode {
		return runPipelineWithTUI(cfg)
	}

	return runPipeline(cfg)
}

// buildTrackerLLMClient constructs a tracker LLM client from environment variables.
// Returns nil, nil when no API keys are set (rather than an error).
func buildTrackerLLMClient() (*trackerllm.Client, error) {
	constructors := map[string]func(string) (trackerllm.ProviderAdapter, error){
		"anthropic": func(key string) (trackerllm.ProviderAdapter, error) {
			var opts []anthropic.Option
			if base := os.Getenv("ANTHROPIC_BASE_URL"); base != "" {
				opts = append(opts, anthropic.WithBaseURL(base))
			}
			return anthropic.New(key, opts...), nil
		},
		"openai": func(key string) (trackerllm.ProviderAdapter, error) {
			var opts []openai.Option
			if base := os.Getenv("OPENAI_BASE_URL"); base != "" {
				opts = append(opts, openai.WithBaseURL(base))
			}
			return openai.New(key, opts...), nil
		},
		"gemini": func(key string) (trackerllm.ProviderAdapter, error) {
			var opts []google.Option
			if base := os.Getenv("GEMINI_BASE_URL"); base != "" {
				opts = append(opts, google.WithBaseURL(base))
			}
			return google.New(key, opts...), nil
		},
	}

	client, err := trackerllm.NewClientFromEnv(constructors)
	if err != nil {
		// If no API keys are configured, return nil client (not an error).
		// The caller decides whether a nil client is acceptable.
		if !hasLLMKeys() {
			return nil, nil
		}
		return nil, err
	}

	// Wire infra-level retry middleware for transient provider errors.
	if client != nil {
		client.AddMiddleware(trackerllm.NewRetryMiddleware(
			trackerllm.WithMaxRetries(3),
			trackerllm.WithBaseDelay(2*time.Second),
		))
	}

	return client, nil
}

// hasLLMKeys returns true if at least one LLM API key is set in the environment.
func hasLLMKeys() bool {
	for _, k := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GEMINI_API_KEY"} {
		if os.Getenv(k) != "" {
			return true
		}
	}
	return false
}

// buildPipelineEngine constructs a tracker pipeline.Engine from DOT source, wiring
// the handler registry with LLM client, execution environment, and event handlers.
func buildPipelineEngine(
	source string,
	workDir string,
	llmClient agent.Completer,
	checkpointPath string,
	artifactDir string,
	pipelineHandler pipeline.PipelineEventHandler,
	agentHandler agent.EventHandler,
) (*pipeline.Engine, *pipeline.Graph, error) {
	trackerGraph, err := pipeline.ParseDOT(source)
	if err != nil {
		return nil, nil, fmt.Errorf("parse pipeline: %w", err)
	}

	var registryOpts []handlers.RegistryOption
	if llmClient != nil {
		registryOpts = append(registryOpts, handlers.WithLLMClient(llmClient, workDir))
		registryOpts = append(registryOpts, handlers.WithExecEnvironment(exec.NewLocalEnvironment(workDir)))
	}
	if agentHandler != nil {
		registryOpts = append(registryOpts, handlers.WithAgentEventHandler(agentHandler))
	}

	registry := handlers.NewDefaultRegistry(trackerGraph, registryOpts...)

	var engineOpts []pipeline.EngineOption
	if checkpointPath != "" {
		engineOpts = append(engineOpts, pipeline.WithCheckpointPath(checkpointPath))
	}
	if artifactDir != "" {
		engineOpts = append(engineOpts, pipeline.WithArtifactDir(artifactDir))
	}
	if pipelineHandler != nil {
		engineOpts = append(engineOpts, pipeline.WithPipelineEventHandler(pipelineHandler))
	}

	engine := pipeline.NewEngine(trackerGraph, registry, engineOpts...)
	return engine, trackerGraph, nil
}

// runPipeline reads a DOT file and executes the pipeline. When a TTY is
// available, it uses an inline Bubble Tea progress display. Otherwise it
// falls back to direct execution with optional verbose event logging.
//
// Supports auto-resume: if the same DOT file was previously run and failed,
// the pipeline automatically resumes from the last checkpoint. Use -fresh
// to force a new run.
func runPipeline(cfg config) int {
	// Resolve artifact dir to absolute path so the agent backend and LLM
	// always work with a concrete directory, not a relative ".".
	if cfg.artifactDir != "" {
		abs, err := filepath.Abs(cfg.artifactDir)
		if err == nil {
			cfg.artifactDir = abs
		}
	}

	source, err := os.ReadFile(cfg.pipelineFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	// Parse the graph with mammoth's dot parser for display in the inline TUI.
	graph, err := dot.Parse(string(source))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	// Compute content hash for auto-resume matching
	sourceHash := runstate.SourceHash(string(source))

	// Resolve data directory for persistent state.
	// CLI pipeline mode defaults to .mammoth/ in CWD (matching web local mode),
	// not the XDG data dir. Use -data-dir to override.
	dataDir := cfg.dataDir
	if dataDir == "" {
		cwd, cwdErr := os.Getwd()
		if cwdErr != nil {
			fmt.Fprintf(os.Stderr, "warning: could not get working directory: %v\n", cwdErr)
		} else {
			dataDir = filepath.Join(cwd, ".mammoth")
		}
	}

	// Set up persistent run state store
	var store *runstate.FSRunStateStore
	if dataDir != "" {
		runsDir := filepath.Join(dataDir, "runs")
		store, err = runstate.NewFSRunStateStore(runsDir)
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
	graph *dot.Graph,
	store *runstate.FSRunStateStore,
	resumeState *runstate.RunState,
	source string,
	sourceHash string,
) int {
	cpPath := store.CheckpointPath(resumeState.ID)

	// Build the LLM client from environment
	llmClient, err := buildTrackerLLMClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	workDir := cfg.artifactDir
	if workDir == "" {
		workDir, _ = os.Getwd()
	}

	// Build event handlers. A deferred relay is included so TUI bridge
	// handlers can be wired after the tea.Program is created.
	relay := &deferredEventRelay{}
	persistHandler := buildPersistenceHandler(store, resumeState.ID)
	var verboseHandler pipeline.PipelineEventHandlerFunc
	if cfg.verbose {
		verboseHandler = verbosePipelineHandler
	}
	pipelineHandler := combinePipelineHandlers(persistHandler, verboseHandler, relay.PipelineHandler())

	var verboseAgentFn agent.EventHandlerFunc
	if cfg.verbose {
		verboseAgentFn = verboseAgentHandler
	}
	agentEvtHandler := combineAgentHandlers(verboseAgentFn, relay.AgentHandler())

	engine, _, err := buildPipelineEngine(source, workDir, llmClient, cpPath, cfg.artifactDir, pipelineHandler, agentEvtHandler)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Update the existing run state to "running" and clear any previous error
	resumeState.Status = "running"
	resumeState.Error = ""
	if err := store.Update(resumeState); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not update run state: %v\n", err)
	}

	var result *pipeline.EngineResult
	var runErr error

	if isTerminal() {
		result, runErr = runPipelineResumeWithStream(cfg, graph, engine, ctx, cpPath, resumeState, relay)
	} else {
		result, runErr = runPipelineResumeDirect(cfg, engine, ctx, cpPath)
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
			resumeState.Context = result.Context
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
	graph *dot.Graph,
	engine *pipeline.Engine,
	ctx context.Context,
	cpPath string,
	resumeState *runstate.RunState,
	relay *deferredEventRelay,
) (*pipeline.EngineResult, error) {
	// Load checkpoint to find which node we're resuming from
	cp, err := pipeline.LoadCheckpoint(cpPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load checkpoint: %w", err)
	}

	resumeInfo := &tui.ResumeInfo{
		ResumedFrom:   cp.CurrentNode,
		PreviousNodes: cp.CompletedNodes,
	}

	model := tui.NewStreamModel(graph, engine, cfg.pipelineFile, ctx, cfg.verbose, tui.WithResumeInfo(resumeInfo))

	p := tea.NewProgram(model)

	// Wire the event bridge so engine events reach the TUI via the relay
	// that was included in the engine's handler chain at construction time.
	bridge := tui.NewEventBridge(p.Send)
	relay.SetBridge(bridge)

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
	engine *pipeline.Engine,
	ctx context.Context,
	cpPath string,
) (*pipeline.EngineResult, error) {
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
	result, runErr := engine.Run(ctx)
	signal.Stop(sigChan)

	if runErr != nil {
		return result, runErr
	}

	printPipelineResult(result, "(resumed)")
	return result, nil
}

// runPipelineFresh starts a new pipeline run with auto-checkpoint enabled.
func runPipelineFresh(
	cfg config,
	graph *dot.Graph,
	store *runstate.FSRunStateStore,
	source string,
	sourceHash string,
) int {
	// Generate a run ID for tracking
	runID, err := runstate.GenerateRunID()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	// Determine auto-checkpoint path
	var autoCheckpointPath string
	if store != nil {
		autoCheckpointPath = store.CheckpointPath(runID)
	}

	// Build the LLM client from environment
	llmClient, llmErr := buildTrackerLLMClient()
	if llmErr != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", llmErr)
		return 1
	}

	workDir := cfg.artifactDir
	if workDir == "" {
		workDir, _ = os.Getwd()
	}

	// Build event handlers. A deferred relay is included so TUI bridge
	// handlers can be wired after the tea.Program is created.
	relay := &deferredEventRelay{}
	persistHandler := buildPersistenceHandler(store, runID)
	var verboseHandler pipeline.PipelineEventHandlerFunc
	if cfg.verbose {
		verboseHandler = verbosePipelineHandler
	}
	pipelineHandler := combinePipelineHandlers(persistHandler, verboseHandler, relay.PipelineHandler())

	var verboseAgentFn agent.EventHandlerFunc
	if cfg.verbose {
		verboseAgentFn = verboseAgentHandler
	}
	agentEvtHandler := combineAgentHandlers(verboseAgentFn, relay.AgentHandler())

	engine, _, err := buildPipelineEngine(source, workDir, llmClient, autoCheckpointPath, cfg.artifactDir, pipelineHandler, agentEvtHandler)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	// Create a cancellable context.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Persist initial run state
	startTime := time.Now()
	if store != nil {
		initialState := &runstate.RunState{
			ID:             runID,
			PipelineFile:   cfg.pipelineFile,
			Status:         "running",
			Source:         source,
			SourceHash:     sourceHash,
			StartedAt:      startTime,
			CompletedNodes: []string{},
			Context:        map[string]string{},
			Events:         []runstate.RunEvent{},
		}
		if err := store.Create(initialState); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not persist initial state: %v\n", err)
		}
	}

	// Choose between inline streaming TUI (interactive) and direct execution
	// (non-interactive: tests, CI, piped output).
	var result *pipeline.EngineResult
	var runErr error

	if isTerminal() {
		result, runErr = runPipelineWithStream(cfg, graph, engine, ctx, source, relay)
	} else {
		result, runErr = runPipelineDirect(cfg, engine, ctx, source)
	}

	// Persist final run state
	if store != nil {
		now := time.Now()
		finalState := &runstate.RunState{
			ID:           runID,
			PipelineFile: cfg.pipelineFile,
			StartedAt:    startTime,
			CompletedAt:  &now,
			Source:       source,
			SourceHash:   sourceHash,
			Context:      map[string]string{},
			Events:       []runstate.RunEvent{},
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
				finalState.Context = result.Context
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
	graph *dot.Graph,
	engine *pipeline.Engine,
	ctx context.Context,
	source string,
	relay *deferredEventRelay,
) (*pipeline.EngineResult, error) {
	model := tui.NewStreamModel(graph, engine, cfg.pipelineFile, ctx, cfg.verbose)

	p := tea.NewProgram(model)

	// Wire the event bridge so engine events reach the TUI via the relay
	// that was included in the engine's handler chain at construction time.
	bridge := tui.NewEventBridge(p.Send)
	relay.SetBridge(bridge)

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
	engine *pipeline.Engine,
	ctx context.Context,
	source string,
) (*pipeline.EngineResult, error) {
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

	result, runErr := engine.Run(ctx)
	signal.Stop(sigChan)

	if runErr != nil {
		return result, runErr
	}

	printPipelineResult(result, "")
	return result, nil
}

// printPipelineResult prints a summary of the completed pipeline run.
func printPipelineResult(result *pipeline.EngineResult, suffix string) {
	if suffix != "" {
		fmt.Printf("Pipeline completed successfully %s.\n", suffix)
	} else {
		fmt.Printf("Pipeline completed successfully.\n")
	}
	if result != nil {
		fmt.Printf("Completed nodes: %v\n", result.CompletedNodes)
		if result.Status != "" {
			fmt.Printf("Final status: %s\n", result.Status)
		}
		if wd, ok := result.Context["_workdir"]; ok && wd != "" {
			fmt.Printf("Output directory: %s\n", wd)
		}
	}
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

	// Parse the graph with mammoth's dot parser for the TUI display.
	graph, err := dot.Parse(string(source))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	// Build the LLM client from environment
	llmClient, llmErr := buildTrackerLLMClient()
	if llmErr != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", llmErr)
		return 1
	}

	workDir := cfg.artifactDir
	if workDir == "" {
		workDir, _ = os.Getwd()
	}

	// Create a deferred relay so bridge handlers can be wired after the
	// tea.Program is created (which requires the model, which requires the engine).
	relay := &deferredEventRelay{}
	engine, _, err := buildPipelineEngine(string(source), workDir, llmClient, "", cfg.artifactDir, relay.PipelineHandler(), relay.AgentHandler())
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	// Create a cancellable context so quitting the TUI stops the engine.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create the TUI app model.
	model := tui.NewAppModel(graph, engine, ctx)

	// Create the Bubble Tea program with alt-screen mode.
	p := tea.NewProgram(model, tea.WithAltScreen())

	// Wire the event bridge so engine events reach the TUI.
	bridge := tui.NewEventBridge(p.Send)
	relay.SetBridge(bridge)

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
	fs.StringVar(&scfg.dataDir, "data-dir", "", "Data directory for projects (overrides --global)")
	fs.BoolVar(&scfg.global, "global", false, "Use global data directory (~/.local/share/mammoth) instead of local .mammoth/")

	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: mammoth serve [flags]")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Start the unified web server for the mammoth wizard flow.")
		fmt.Fprintln(os.Stderr, "By default, uses current directory as project root (.mammoth/ for state).")
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
// choosing between local and global workspace modes based on flags.
// Priority: --data-dir > --global > default (local mode, CWD is project root).
func buildWebServer(scfg serveConfig) (*web.Server, error) {
	var ws web.Workspace

	if scfg.dataDir != "" {
		// Explicit --data-dir: use it as a global workspace (backward compat).
		ws = web.NewGlobalWorkspace(scfg.dataDir)
	} else if scfg.global {
		// --global: use XDG data dir as a global workspace.
		resolved, err := resolveDataDir("")
		if err != nil {
			return nil, fmt.Errorf("resolve data dir: %w", err)
		}
		ws = web.NewGlobalWorkspace(resolved)
	} else {
		// Default: local mode, CWD is root, state in .mammoth/.
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("get working directory: %w", err)
		}
		ws = web.NewLocalWorkspace(cwd)
	}

	// Build tracker LLM client for pipeline execution in the web server.
	llmClient, _ := buildTrackerLLMClient()

	addr := fmt.Sprintf("127.0.0.1:%d", scfg.port)
	srv, err := web.NewServer(web.ServerConfig{
		Addr:      addr,
		Workspace: ws,
		LLMClient: llmClient,
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

	// Display startup message indicating mode and data location.
	if scfg.dataDir != "" {
		fmt.Fprintf(os.Stderr, "mammoth web UI: http://%s (global: %s)\n", addr, scfg.dataDir)
	} else if scfg.global {
		resolved, _ := resolveDataDir("")
		fmt.Fprintf(os.Stderr, "mammoth web UI: http://%s (global: %s)\n", addr, resolved)
	} else {
		cwd, _ := os.Getwd()
		fmt.Fprintf(os.Stderr, "mammoth web UI: http://%s (local: %s)\n", addr, cwd)
	}
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	return 0
}

// validatePipeline parses and validates a DOT file without executing it.
// Uses the dot/ parser and dot/validator Lint function for structural checks.
func validatePipeline(cfg config) int {
	if cfg.fixMode {
		fmt.Fprintln(os.Stderr, "warning: -fix is not yet supported with the tracker pipeline runner")
	}

	source, err := os.ReadFile(cfg.pipelineFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	graph, err := dot.Parse(string(source))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	diags := validator.Lint(graph)

	hasErrors := false
	for _, d := range diags {
		fmt.Fprintf(os.Stderr, "[%s] %s", d.Severity, d.Message)
		if d.NodeID != "" {
			fmt.Fprintf(os.Stderr, " (node: %s)", d.NodeID)
		}
		fmt.Fprintln(os.Stderr)

		if d.Severity == "error" {
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

// buildPersistenceHandler creates a pipeline event handler that persists events
// to the run state store's events.jsonl file.
func buildPersistenceHandler(store *runstate.FSRunStateStore, runID string) pipeline.PipelineEventHandlerFunc {
	if store == nil || runID == "" {
		return nil
	}
	return func(evt pipeline.PipelineEvent) {
		event := runstate.RunEvent{
			Type:      string(evt.Type),
			NodeID:    evt.NodeID,
			Timestamp: evt.Timestamp,
		}
		if evt.Message != "" {
			event.Data = map[string]any{"message": evt.Message}
		}
		if err := store.AddEvent(runID, event); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not persist event: %v\n", err)
		}
	}
}

// combinePipelineHandlers merges multiple pipeline event handlers into one.
// Nil handlers are safely skipped.
func combinePipelineHandlers(handlers ...pipeline.PipelineEventHandlerFunc) pipeline.PipelineEventHandler {
	var active []pipeline.PipelineEventHandler
	for _, h := range handlers {
		if h != nil {
			active = append(active, h)
		}
	}
	if len(active) == 0 {
		return pipeline.PipelineNoopHandler
	}
	return pipeline.PipelineMultiHandler(active...)
}

// verbosePipelineHandler prints pipeline lifecycle events to stderr.
func verbosePipelineHandler(evt pipeline.PipelineEvent) {
	switch evt.Type {
	case pipeline.EventPipelineStarted:
		fmt.Fprintf(os.Stderr, "[pipeline] started\n")
	case pipeline.EventStageStarted:
		fmt.Fprintf(os.Stderr, "[stage] %s started\n", evt.NodeID)
	case pipeline.EventStageCompleted:
		fmt.Fprintf(os.Stderr, "[stage] %s completed\n", evt.NodeID)
	case pipeline.EventStageFailed:
		if evt.Err != nil {
			fmt.Fprintf(os.Stderr, "[stage] %s failed: %v\n", evt.NodeID, evt.Err)
		} else {
			fmt.Fprintf(os.Stderr, "[stage] %s failed\n", evt.NodeID)
		}
	case pipeline.EventStageRetrying:
		fmt.Fprintf(os.Stderr, "[stage] %s retrying\n", evt.NodeID)
	case pipeline.EventPipelineCompleted:
		fmt.Fprintf(os.Stderr, "[pipeline] completed\n")
	case pipeline.EventPipelineFailed:
		if evt.Err != nil {
			fmt.Fprintf(os.Stderr, "[pipeline] failed: %v\n", evt.Err)
		} else {
			fmt.Fprintf(os.Stderr, "[pipeline] failed\n")
		}
	case pipeline.EventCheckpointSaved:
		fmt.Fprintf(os.Stderr, "[checkpoint] saved at %s\n", evt.NodeID)
	}
}

// verboseAgentHandler prints agent session events to stderr.
func verboseAgentHandler(evt agent.Event) {
	switch evt.Type {
	case agent.EventTextDelta:
		if evt.Text != "" {
			fmt.Fprint(os.Stderr, evt.Text)
		}
	case agent.EventToolCallStart:
		if evt.ToolInput != "" {
			fmt.Fprintf(os.Stderr, "\n[agent] tool %s(%s)\n", evt.ToolName, evt.ToolInput)
		} else {
			fmt.Fprintf(os.Stderr, "\n[agent] tool %s\n", evt.ToolName)
		}
	case agent.EventToolCallEnd:
		fmt.Fprintf(os.Stderr, "[agent] tool %s done\n", evt.ToolName)
	case agent.EventTurnEnd:
		fmt.Fprintf(os.Stderr, "[agent] turn %d complete (in:%d out:%d)\n", evt.Turn, evt.Usage.InputTokens, evt.Usage.OutputTokens)
	case agent.EventSteeringInjected:
		fmt.Fprintf(os.Stderr, "[agent] steering: %v\n", evt.Text)
	}
}

// deferredEventRelay provides pipeline and agent event handlers that forward
// to underlying handlers set after construction. This breaks the circular
// dependency between engine construction (needs handlers) and TUI bridge
// creation (needs tea.Program which needs model which needs engine).
type deferredEventRelay struct {
	mu         sync.Mutex
	pipelineFn pipeline.PipelineEventHandlerFunc
	agentFn    agent.EventHandlerFunc
}

// PipelineHandler returns a handler that forwards events to the relay target.
func (r *deferredEventRelay) PipelineHandler() pipeline.PipelineEventHandlerFunc {
	return func(evt pipeline.PipelineEvent) {
		r.mu.Lock()
		fn := r.pipelineFn
		r.mu.Unlock()
		if fn != nil {
			fn(evt)
		}
	}
}

// AgentHandler returns a handler that forwards events to the relay target.
func (r *deferredEventRelay) AgentHandler() agent.EventHandlerFunc {
	return func(evt agent.Event) {
		r.mu.Lock()
		fn := r.agentFn
		r.mu.Unlock()
		if fn != nil {
			fn(evt)
		}
	}
}

// SetBridge wires the relay to forward events through the given TUI bridge.
func (r *deferredEventRelay) SetBridge(bridge *tui.EventBridge) {
	r.mu.Lock()
	r.pipelineFn = bridge.PipelineHandler()
	r.agentFn = bridge.AgentHandler()
	r.mu.Unlock()
}

// combineAgentHandlers merges multiple agent event handlers into one.
// Nil handlers are safely skipped.
func combineAgentHandlers(fns ...agent.EventHandlerFunc) agent.EventHandler {
	var active []agent.EventHandlerFunc
	for _, f := range fns {
		if f != nil {
			active = append(active, f)
		}
	}
	if len(active) == 0 {
		return nil
	}
	if len(active) == 1 {
		return active[0]
	}
	return agent.EventHandlerFunc(func(evt agent.Event) {
		for _, f := range active {
			f(evt)
		}
	})
}

// auditConfig holds configuration for the "mammoth audit" subcommand.
type auditConfig struct {
	runID   string
	verbose bool
	dataDir string
}

// parseAuditArgs checks whether args starts with the "audit" subcommand and,
// if so, parses audit-specific flags. Returns the config and true if "audit"
// was detected, or a zero value and false otherwise.
func parseAuditArgs(args []string) (auditConfig, bool) {
	if len(args) == 0 || args[0] != "audit" {
		return auditConfig{}, false
	}

	var cfg auditConfig
	fs := flag.NewFlagSet("mammoth audit", flag.ContinueOnError)
	fs.BoolVar(&cfg.verbose, "verbose", false, "Include full tool call details")
	fs.StringVar(&cfg.dataDir, "data-dir", "", "Data directory (default: .mammoth/ in CWD)")

	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: mammoth audit [flags] [runID]")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Generate a narrative audit of a pipeline run.")
		fmt.Fprintln(os.Stderr, "With no runID, audits the most recent run.")
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

	if fs.NArg() > 0 {
		cfg.runID = fs.Arg(0)
	}

	return cfg, true
}

// runAudit loads a pipeline run and generates an LLM-powered audit narrative.
func runAudit(cfg auditConfig) int {
	// Resolve data directory.
	dataDir := cfg.dataDir
	if dataDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		dataDir = filepath.Join(cwd, ".mammoth")
	}

	runsDir := filepath.Join(dataDir, "runs")
	store, err := runstate.NewFSRunStateStore(runsDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: could not open run store: %v\n", err)
		return 1
	}

	// Find the target run.
	runID := cfg.runID
	if runID == "" {
		// Find most recent run.
		states, err := store.List()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		if len(states) == 0 {
			fmt.Fprintln(os.Stderr, "error: no runs found in", runsDir)
			return 1
		}
		// Pick the most recent by start time.
		latest := states[0]
		for _, s := range states[1:] {
			if s.StartedAt.After(latest.StartedAt) {
				latest = s
			}
		}
		runID = latest.ID
	}

	state, err := store.Get(runID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: could not load run %s: %v\n", runID, err)
		return 1
	}

	// Parse graph from stored source for flow summary.
	var graph *dot.Graph
	if state.Source != "" {
		g, parseErr := dot.Parse(state.Source)
		if parseErr == nil {
			graph = g
		}
	}

	// Create LLM client (mammoth's llm package for the audit).
	client, err := llm.FromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: audit requires an LLM API key")
		fmt.Fprintln(os.Stderr, "Set one of: ANTHROPIC_API_KEY, OPENAI_API_KEY, or GEMINI_API_KEY")
		return 1
	}

	report, err := generateAudit(context.Background(), state, state.Events, graph, cfg.verbose, client)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	fmt.Println(report.Narrative)
	return 0
}

