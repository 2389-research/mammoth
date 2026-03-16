// ABOUTME: run_pipeline MCP tool handler for launching async pipeline execution.
// ABOUTME: Validates DOT, creates a run, and spawns a goroutine to execute via tracker.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/2389-research/mammoth/dot"
	"github.com/2389-research/mammoth/dot/validator"
	"github.com/2389-research/tracker/pipeline"
	"github.com/2389-research/tracker/agent/exec"
	"github.com/2389-research/tracker/pipeline/handlers"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// RunPipelineInput is the input schema for the run_pipeline tool.
type RunPipelineInput struct {
	Source      string `json:"source,omitempty" jsonschema:"DOT source string to run"`
	File        string `json:"file,omitempty"   jsonschema:"path to a DOT file to run"`
	RetryPolicy string `json:"retry_policy,omitempty" jsonschema:"retry policy name: none, default, aggressive"`
}

// RunPipelineOutput is the output of the run_pipeline tool.
type RunPipelineOutput struct {
	RunID  string `json:"run_id"`
	Status string `json:"status"`
}

// registerRunPipeline registers the run_pipeline tool on the given MCP server.
func (s *Server) registerRunPipeline(srv *mcpsdk.Server) {
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "run_pipeline",
		Description: "Start a pipeline run from a DOT definition. Returns immediately with a run ID. Use get_run_status to monitor progress.",
	}, s.handleRunPipeline)
}

// handleRunPipeline validates DOT, creates a run, and spawns async execution.
func (s *Server) handleRunPipeline(_ context.Context, _ *mcpsdk.CallToolRequest, input RunPipelineInput) (*mcpsdk.CallToolResult, RunPipelineOutput, error) {
	src, err := resolveSource(input.Source, input.File)
	if err != nil {
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: err.Error()}},
			IsError: true,
		}, RunPipelineOutput{}, nil
	}

	// Pre-validate using mammoth's DOT parser and validator for immediate feedback.
	graph, err := dot.Parse(src)
	if err != nil {
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: fmt.Sprintf("parse error: %v", err)}},
			IsError: true,
		}, RunPipelineOutput{}, nil
	}

	diags := validator.Lint(graph)
	for _, d := range diags {
		if d.Severity == "error" {
			return &mcpsdk.CallToolResult{
				Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: fmt.Sprintf("validation error: [%s] %s: %s", d.Severity, d.Rule, d.Message)}},
				IsError: true,
			}, RunPipelineOutput{}, nil
		}
	}

	// Create the run.
	config := RunConfig{
		RetryPolicy: input.RetryPolicy,
	}
	run := s.registry.Create(src, config)

	// Set up directories.
	runDir := filepath.Join(s.dataDir, run.ID)
	checkpointDir := filepath.Join(runDir, "checkpoints")
	artifactDir := filepath.Join(runDir, "artifacts")
	if err := os.MkdirAll(checkpointDir, 0755); err != nil {
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: fmt.Sprintf("create checkpoint dir: %v", err)}},
			IsError: true,
		}, RunPipelineOutput{}, nil
	}
	if err := os.MkdirAll(artifactDir, 0755); err != nil {
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: fmt.Sprintf("create artifact dir: %v", err)}},
			IsError: true,
		}, RunPipelineOutput{}, nil
	}

	run.mu.Lock()
	run.CheckpointDir = checkpointDir
	run.ArtifactDir = artifactDir
	run.mu.Unlock()

	// Save to disk index.
	entry := &IndexEntry{
		RunID:         run.ID,
		Source:        src,
		Config:        config,
		Status:        string(StatusRunning),
		CheckpointDir: checkpointDir,
		ArtifactDir:   artifactDir,
	}
	if err := s.index.Save(entry); err != nil {
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: fmt.Sprintf("save index: %v", err)}},
			IsError: true,
		}, RunPipelineOutput{}, nil
	}

	// Spawn async execution.
	go s.executePipeline(run)

	output := RunPipelineOutput{
		RunID:  run.ID,
		Status: string(StatusRunning),
	}
	data, err := json.Marshal(output)
	if err != nil {
		return nil, RunPipelineOutput{}, fmt.Errorf("marshal output: %w", err)
	}
	return &mcpsdk.CallToolResult{
		Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: string(data)}},
	}, output, nil
}

// executePipeline runs the tracker pipeline engine in a background goroutine
// and updates the run state on completion or failure.
func (s *Server) executePipeline(run *ActiveRun) {
	ctx, cancel := context.WithCancel(context.Background())
	run.mu.Lock()
	run.cancel = cancel
	run.mu.Unlock()
	defer cancel()

	// Parse the DOT source into a tracker pipeline graph.
	graph, parseErr := pipeline.ParseDOT(run.Source)
	if parseErr != nil {
		run.mu.Lock()
		run.Status = StatusFailed
		run.Error = fmt.Sprintf("parse DOT: %v", parseErr)
		run.mu.Unlock()
		s.updateIndexStatus(run)
		return
	}

	// Build the interviewer with the run's context for cancellation.
	iv := &mcpInterviewer{run: run, ctx: ctx}

	// Build the handler registry with the interviewer and LLM client wired in.
	registryOpts := []handlers.RegistryOption{
		handlers.WithInterviewer(iv, graph),
		handlers.WithAgentEventHandler(newAgentEventHandler(run)),
	}
	if s.llmClient != nil {
		registryOpts = append(registryOpts, handlers.WithLLMClient(s.llmClient, run.ArtifactDir))
		registryOpts = append(registryOpts, handlers.WithExecEnvironment(exec.NewLocalEnvironment(run.ArtifactDir)))
	}
	registry := handlers.NewDefaultRegistry(graph, registryOpts...)

	// Build engine options.
	checkpointPath := filepath.Join(run.CheckpointDir, "checkpoint.json")
	opts := []pipeline.EngineOption{
		pipeline.WithPipelineEventHandler(newPipelineEventHandler(run)),
		pipeline.WithCheckpointPath(checkpointPath),
		pipeline.WithArtifactDir(run.ArtifactDir),
	}

	engine := pipeline.NewEngine(graph, registry, opts...)
	result, err := engine.Run(ctx)

	run.mu.Lock()
	if err != nil {
		run.Status = StatusFailed
		run.Error = err.Error()
	} else {
		run.Status = StatusCompleted
		run.Result = result
	}
	run.mu.Unlock()

	s.updateIndexStatus(run)
}

// updateIndexStatus saves the current run status to the disk index.
func (s *Server) updateIndexStatus(run *ActiveRun) {
	run.mu.RLock()
	status := string(run.Status)
	run.mu.RUnlock()
	entry := &IndexEntry{
		RunID:         run.ID,
		Source:        run.Source,
		Config:        run.Config,
		Status:        status,
		CheckpointDir: run.CheckpointDir,
		ArtifactDir:   run.ArtifactDir,
	}
	if err := s.index.Save(entry); err != nil {
		fmt.Fprintf(os.Stderr, "[mcp] failed to save run index for %s: %v\n", run.ID, err)
	}
}
