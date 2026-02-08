// ABOUTME: CLI entrypoint for the makeatron pipeline runner with run, validate, and server modes.
// ABOUTME: Wires together the attractor engine, HTTP server, retry policies, and signal handling.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/2389-research/makeatron/attractor"
)

var version = "dev"

// config holds all CLI configuration parsed from flags and positional arguments.
type config struct {
	serverMode    bool
	port          int
	validateOnly  bool
	checkpointDir string
	artifactDir   string
	retryPolicy   string
	verbose       bool
	showVersion   bool
	pipelineFile  string
}

func main() {
	cfg := parseFlags()

	if cfg.showVersion {
		fmt.Printf("makeatron %s\n", version)
		os.Exit(0)
	}

	os.Exit(run(cfg))
}

// parseFlags parses command-line flags and returns a populated config.
func parseFlags() config {
	var cfg config

	fs := flag.NewFlagSet("makeatron", flag.ContinueOnError)
	fs.BoolVar(&cfg.serverMode, "server", false, "Start HTTP server mode")
	fs.IntVar(&cfg.port, "port", 2389, "Server port (default: 2389)")
	fs.BoolVar(&cfg.validateOnly, "validate", false, "Validate pipeline without executing")
	fs.StringVar(&cfg.checkpointDir, "checkpoint-dir", "", "Directory for checkpoint files")
	fs.StringVar(&cfg.artifactDir, "artifact-dir", "", "Directory for artifact storage")
	fs.StringVar(&cfg.retryPolicy, "retry", "none", "Default retry policy: none, standard, aggressive, linear, patient")
	fs.BoolVar(&cfg.verbose, "verbose", false, "Verbose output")
	fs.BoolVar(&cfg.showVersion, "version", false, "Print version and exit")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: makeatron [options] <pipeline.dot>\n\nOptions:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(os.Args[1:]); err != nil {
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
		fmt.Fprintln(os.Stderr, "error: pipeline file required (use makeatron <pipeline.dot>)")
		return 1
	}

	if cfg.validateOnly {
		return validatePipeline(cfg)
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

	engineCfg := attractor.EngineConfig{
		CheckpointDir: cfg.checkpointDir,
		ArtifactDir:   cfg.artifactDir,
		DefaultRetry:  retryPolicyFromName(cfg.retryPolicy),
		Handlers:      attractor.DefaultHandlerRegistry(),
	}

	if cfg.verbose {
		engineCfg.EventHandler = verboseEventHandler
	}

	engine := attractor.NewEngine(engineCfg)

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

	result, err := engine.Run(ctx, string(source))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
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

// runServer starts the HTTP pipeline server.
func runServer(cfg config) int {
	engineCfg := attractor.EngineConfig{
		CheckpointDir: cfg.checkpointDir,
		ArtifactDir:   cfg.artifactDir,
		DefaultRetry:  retryPolicyFromName(cfg.retryPolicy),
		Handlers:      attractor.DefaultHandlerRegistry(),
	}

	if cfg.verbose {
		engineCfg.EventHandler = verboseEventHandler
	}

	engine := attractor.NewEngine(engineCfg)
	server := attractor.NewPipelineServer(engine)

	addr := fmt.Sprintf(":%d", cfg.port)

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
		fmt.Fprintf(os.Stderr, "[stage] %s failed\n", evt.NodeID)
	case attractor.EventStageRetrying:
		fmt.Fprintf(os.Stderr, "[stage] %s retrying\n", evt.NodeID)
	case attractor.EventPipelineCompleted:
		fmt.Fprintf(os.Stderr, "[pipeline] completed\n")
	case attractor.EventPipelineFailed:
		fmt.Fprintf(os.Stderr, "[pipeline] failed\n")
	case attractor.EventCheckpointSaved:
		fmt.Fprintf(os.Stderr, "[checkpoint] saved at %s\n", evt.NodeID)
	}
}
