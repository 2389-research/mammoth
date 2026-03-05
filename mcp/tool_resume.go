// ABOUTME: resume_pipeline MCP tool handler for resuming a failed pipeline from its last checkpoint.
// ABOUTME: Loads previous run metadata from disk, finds the latest checkpoint, and spawns a new execution.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/2389-research/mammoth/attractor"
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

	// Parse the original DOT source.
	graph, err := attractor.Parse(entry.Source)
	if err != nil {
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: fmt.Sprintf("parse error on saved source: %v", err)}},
			IsError: true,
		}, ResumePipelineOutput{}, nil
	}

	// Create new run in registry.
	run := s.registry.Create(entry.Source, entry.Config)

	// Set up directories for the new run.
	runDir := filepath.Join(s.dataDir, run.ID)
	checkpointDir := filepath.Join(runDir, "checkpoints")
	artifactDir := filepath.Join(runDir, "artifacts")
	_ = os.MkdirAll(checkpointDir, 0755)
	_ = os.MkdirAll(artifactDir, 0755)

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
	go s.resumePipeline(run, graph, cpPath)

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
func (s *Server) resumePipeline(run *ActiveRun, graph *attractor.Graph, checkpointPath string) {
	ctx, cancel := context.WithCancel(context.Background())
	run.mu.Lock()
	run.cancel = cancel
	run.mu.Unlock()
	defer cancel()

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
	result, err := engine.ResumeFromCheckpoint(ctx, graph, checkpointPath)

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

// findLatestCheckpoint lists checkpoint*.json files in the given directory,
// sorts them, and returns the last (most recent) one.
func findLatestCheckpoint(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("read checkpoint dir: %w", err)
	}

	var checkpoints []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, "checkpoint") && strings.HasSuffix(name, ".json") {
			checkpoints = append(checkpoints, name)
		}
	}

	if len(checkpoints) == 0 {
		return "", fmt.Errorf("no checkpoint files found in %s", dir)
	}

	sort.Strings(checkpoints)
	return filepath.Join(dir, checkpoints[len(checkpoints)-1]), nil
}
