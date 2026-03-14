// ABOUTME: resume_pipeline MCP tool handler for resuming a failed pipeline from its last checkpoint.
// ABOUTME: Loads previous run metadata from disk, finds the latest checkpoint, and spawns a new execution.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/2389-research/tracker/pipeline"
	"github.com/2389-research/tracker/pipeline/handlers"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ResumePipelineInput is the input schema for the resume_pipeline tool.
type ResumePipelineInput struct {
	RunID string `json:"run_id" jsonschema:"the run ID of a previous run to resume from"`
}

// ResumePipelineOutput is the output of the resume_pipeline tool.
type ResumePipelineOutput struct {
	RunID          string `json:"run_id"`
	Status         string `json:"status"`
	CheckpointUsed string `json:"checkpoint_used"`
}

// registerResumePipeline registers the resume_pipeline tool on the given MCP server.
func (s *Server) registerResumePipeline(srv *mcpsdk.Server) {
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "resume_pipeline",
		Description: "Resume a previous pipeline run from its last checkpoint. Creates a new run that continues from where the previous run left off.",
	}, s.handleResumePipeline)
}

// handleResumePipeline loads a previous run's checkpoint and spawns a new execution from it.
func (s *Server) handleResumePipeline(_ context.Context, _ *mcpsdk.CallToolRequest, input ResumePipelineInput) (*mcpsdk.CallToolResult, ResumePipelineOutput, error) {
	// Load previous run from disk index.
	entry, err := s.index.Load(input.RunID)
	if err != nil {
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: fmt.Sprintf("run %q not found: %v", input.RunID, err)}},
			IsError: true,
		}, ResumePipelineOutput{}, nil
	}

	// Find latest checkpoint.
	cpPath, err := findLatestCheckpoint(entry.CheckpointDir)
	if err != nil {
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: fmt.Sprintf("no checkpoint found for run %q: %v", input.RunID, err)}},
			IsError: true,
		}, ResumePipelineOutput{}, nil
	}

	// Verify the DOT source can be parsed by tracker.
	if _, parseErr := pipeline.ParseDOT(entry.Source); parseErr != nil {
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: fmt.Sprintf("parse error on saved source: %v", parseErr)}},
			IsError: true,
		}, ResumePipelineOutput{}, nil
	}

	// Create new run in registry.
	run := s.registry.Create(entry.Source, entry.Config)

	// Set up directories for the new run.
	runDir := filepath.Join(s.dataDir, run.ID)
	checkpointDir := filepath.Join(runDir, "checkpoints")
	artifactDir := filepath.Join(runDir, "artifacts")
	if err := os.MkdirAll(checkpointDir, 0755); err != nil {
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: fmt.Sprintf("create checkpoint dir: %v", err)}},
			IsError: true,
		}, ResumePipelineOutput{}, nil
	}
	if err := os.MkdirAll(artifactDir, 0755); err != nil {
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: fmt.Sprintf("create artifact dir: %v", err)}},
			IsError: true,
		}, ResumePipelineOutput{}, nil
	}

	run.mu.Lock()
	run.CheckpointDir = checkpointDir
	run.ArtifactDir = artifactDir
	run.mu.Unlock()

	// Save new run to disk index.
	newEntry := &IndexEntry{
		RunID:         run.ID,
		Source:        entry.Source,
		Config:        entry.Config,
		Status:        string(StatusRunning),
		CheckpointDir: checkpointDir,
		ArtifactDir:   artifactDir,
	}
	if err := s.index.Save(newEntry); err != nil {
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: fmt.Sprintf("save index: %v", err)}},
			IsError: true,
		}, ResumePipelineOutput{}, nil
	}

	// Spawn resume execution.
	go s.resumePipeline(run, cpPath)

	output := ResumePipelineOutput{
		RunID:          run.ID,
		Status:         string(StatusRunning),
		CheckpointUsed: cpPath,
	}
	data, err := json.Marshal(output)
	if err != nil {
		return nil, ResumePipelineOutput{}, fmt.Errorf("marshal output: %w", err)
	}
	return &mcpsdk.CallToolResult{
		Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: string(data)}},
	}, output, nil
}

// resumePipeline runs the engine from a checkpoint in a background goroutine.
func (s *Server) resumePipeline(run *ActiveRun, checkpointPath string) {
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

	// Load the checkpoint state and initialize engine context from it.
	cp, cpErr := pipeline.LoadCheckpoint(checkpointPath)
	if cpErr != nil {
		run.mu.Lock()
		run.Status = StatusFailed
		run.Error = fmt.Sprintf("load checkpoint: %v", cpErr)
		run.mu.Unlock()
		s.updateIndexStatus(run)
		return
	}

	// Build the interviewer with the run's context for cancellation.
	iv := &mcpInterviewer{run: run, ctx: ctx}

	// Build the handler registry with the interviewer wired in.
	registry := handlers.NewDefaultRegistry(graph,
		handlers.WithInterviewer(iv, graph),
		handlers.WithAgentEventHandler(newAgentEventHandler(run)),
	)

	// Build engine options with checkpoint context for resume.
	newCheckpointPath := filepath.Join(run.CheckpointDir, "checkpoint.json")
	opts := []pipeline.EngineOption{
		pipeline.WithPipelineEventHandler(newPipelineEventHandler(run)),
		pipeline.WithCheckpointPath(newCheckpointPath),
		pipeline.WithArtifactDir(run.ArtifactDir),
		pipeline.WithInitialContext(cp.Context),
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

// findLatestCheckpoint lists checkpoint*.json files in the given directory
// and returns the one with the most recent modification time.
func findLatestCheckpoint(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("read checkpoint dir: %w", err)
	}

	var latestName string
	var latestTime int64
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, "checkpoint") || !strings.HasSuffix(name, ".json") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		modTime := info.ModTime().UnixNano()
		if latestName == "" || modTime > latestTime {
			latestName = name
			latestTime = modTime
		}
	}

	if latestName == "" {
		return "", fmt.Errorf("no checkpoint files found in %s", dir)
	}
	return filepath.Join(dir, latestName), nil
}
