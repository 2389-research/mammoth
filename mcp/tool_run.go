// ABOUTME: run_pipeline MCP tool handler for launching async pipeline execution.
// ABOUTME: Parses DOT, validates, creates a run, and spawns a goroutine to execute the pipeline.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/2389-research/mammoth/attractor"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// RunPipelineInput is the input schema for the run_pipeline tool.
type RunPipelineInput struct {
	Source      string `json:"source,omitempty" jsonschema:"DOT source string to run"`
	File        string `json:"file,omitempty"   jsonschema:"path to a DOT file to run"`
	RetryPolicy string `json:"retry_policy,omitempty" jsonschema:"retry policy name: none, default, aggressive"`
	Backend     string `json:"backend,omitempty" jsonschema:"backend to use: agent, claude-code"`
	BaseURL     string `json:"base_url,omitempty" jsonschema:"API base URL for the backend"`
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

	// Synchronous validation before creating the run.
	graph, err := attractor.Parse(src)
	if err != nil {
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: fmt.Sprintf("parse error: %v", err)}},
			IsError: true,
		}, RunPipelineOutput{}, nil
	}

	graph = attractor.ApplyTransforms(graph, attractor.DefaultTransforms()...)
	if _, valErr := attractor.ValidateOrError(graph); valErr != nil {
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: fmt.Sprintf("validation error: %v", valErr)}},
			IsError: true,
		}, RunPipelineOutput{}, nil
	}

	// Create the run.
	config := RunConfig{
		RetryPolicy: input.RetryPolicy,
		Backend:     input.Backend,
		BaseURL:     input.BaseURL,
	}
	run := s.registry.Create(src, config)

	// Set up directories.
	runDir := filepath.Join(s.dataDir, run.ID)
	checkpointDir := filepath.Join(runDir, "checkpoints")
	artifactDir := filepath.Join(runDir, "artifacts")
	_ = os.MkdirAll(checkpointDir, 0755)
	_ = os.MkdirAll(artifactDir, 0755)

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
	go s.executePipeline(run, graph)

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

// executePipeline runs the attractor engine in a background goroutine
// and updates the run state on completion or failure.
func (s *Server) executePipeline(run *ActiveRun, graph *attractor.Graph) {
	ctx, cancel := context.WithCancel(context.Background())
	run.mu.Lock()
	run.cancel = cancel
	run.mu.Unlock()
	defer cancel()

	// Build engine config.
	handlers := attractor.DefaultHandlerRegistry()
	handlers = wrapRegistryWithInterviewer(handlers, &mcpInterviewer{run: run})

	backend := DetectBackend(run.Config.Backend)

	engineConfig := attractor.EngineConfig{
		CheckpointDir: run.CheckpointDir,
		ArtifactDir:   run.ArtifactDir,
		RunID:         run.ID,
		Handlers:      handlers,
		EventHandler:  newEventHandler(run),
		Backend:       backend,
		BaseURL:       run.Config.BaseURL,
		DefaultRetry:  retryPolicyFromName(run.Config.RetryPolicy),
	}

	engine := attractor.NewEngine(engineConfig)
	result, err := engine.RunGraph(ctx, graph)

	run.mu.Lock()
	if err != nil {
		run.Status = StatusFailed
		run.Error = err.Error()
	} else {
		run.Status = StatusCompleted
		run.Result = result
	}
	run.mu.Unlock()

	// Update disk index.
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
	_ = s.index.Save(entry)
}

// wrapRegistryWithInterviewer creates a new HandlerRegistry wrapping each
// handler from source with an interviewer injector. This allows human gate
// questions to be routed through the MCP answer channel.
func wrapRegistryWithInterviewer(source *attractor.HandlerRegistry, iv attractor.Interviewer) *attractor.HandlerRegistry {
	wrapped := attractor.NewHandlerRegistry()
	for _, handler := range source.All() {
		wrapped.Register(&interviewerWrapper{
			inner:       handler,
			interviewer: iv,
		})
	}
	return wrapped
}

// interviewerWrapper wraps a NodeHandler, injecting an Interviewer into the
// pipeline context before delegating execution.
type interviewerWrapper struct {
	inner       attractor.NodeHandler
	interviewer attractor.Interviewer
}

func (w *interviewerWrapper) Type() string { return w.inner.Type() }

func (w *interviewerWrapper) InnerHandler() attractor.NodeHandler { return w.inner }

func (w *interviewerWrapper) Execute(ctx context.Context, node *attractor.Node, pctx *attractor.Context, store *attractor.ArtifactStore) (*attractor.Outcome, error) {
	pctx.Set("_interviewer", w.interviewer)
	return w.inner.Execute(ctx, node, pctx, store)
}

// retryPolicyFromName converts a named retry policy to the attractor type.
func retryPolicyFromName(name string) attractor.RetryPolicy {
	switch name {
	case "aggressive":
		return attractor.RetryPolicy{
			MaxAttempts: 5,
			Backoff: attractor.BackoffConfig{
				InitialDelay: 200 * time.Millisecond,
				Factor:       2.0,
				MaxDelay:     60 * time.Second,
			},
		}
	case "default":
		return attractor.RetryPolicy{
			MaxAttempts: 3,
			Backoff: attractor.BackoffConfig{
				InitialDelay: 200 * time.Millisecond,
				Factor:       2.0,
				MaxDelay:     30 * time.Second,
			},
		}
	default: // "none" or unset
		return attractor.RetryPolicyNone()
	}
}
