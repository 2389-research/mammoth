// ABOUTME: Pipeline generation handler that runs the embedded meta-pipeline to produce DOT from specs.
// ABOUTME: Exports SpecState to markdown, launches tracker build, stores result in project DOT field.
package web

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/2389-research/mammoth/spec/core"
	"github.com/2389-research/mammoth/spec/export"
	"github.com/2389-research/tracker/agent"
	"github.com/2389-research/tracker/agent/exec"
	"github.com/2389-research/tracker/pipeline"
	"github.com/2389-research/tracker/pipeline/handlers"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/oklog/ulid/v2"
)

//go:embed pipeline_from_spec.dot
var metaPipelineDOT string

// startGenerationBuild creates in-memory run tracking and launches the tracker
// pipeline engine using the embedded meta-pipeline. The meta-pipeline reads
// spec.md from the working directory and produces pipeline.dot as output.
// On success, the generated DOT is stored in the project's DOT field.
func (s *Server) startGenerationBuild(projectID, specMarkdown string) string {
	runID := uuid.New().String()
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan SSEEvent, 100)
	now := time.Now()
	state := &RunState{
		ID:             runID,
		Status:         "running",
		StartedAt:      now,
		CompletedNodes: []string{},
	}

	run := &BuildRun{
		State:  state,
		Events: events,
		Cancel: cancel,
		Ctx:    ctx,
	}
	run.EnsureFanoutStarted()

	s.buildsMu.Lock()
	s.builds[projectID] = run
	s.buildsMu.Unlock()

	workDir := s.workspace.ArtifactDir(projectID, runID)
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		log.Printf("component=web.generate action=create_workdir_failed project_id=%s run_id=%s err=%v", projectID, runID, err)
		s.buildsMu.Lock()
		completedAt := time.Now()
		state.CompletedAt = &completedAt
		state.Status = "failed"
		state.Error = fmt.Sprintf("create working directory: %v", err)
		s.buildsMu.Unlock()
		close(events)
		cancel()
		return runID
	}

	// Write the spec markdown to the working directory for the meta-pipeline.
	specPath := filepath.Join(workDir, "spec.md")
	if err := os.WriteFile(specPath, []byte(specMarkdown), 0o644); err != nil {
		log.Printf("component=web.generate action=write_spec_failed project_id=%s run_id=%s err=%v", projectID, runID, err)
		s.buildsMu.Lock()
		completedAt := time.Now()
		state.CompletedAt = &completedAt
		state.Status = "failed"
		state.Error = fmt.Sprintf("write spec.md: %v", err)
		s.buildsMu.Unlock()
		close(events)
		cancel()
		return runID
	}

	// Create the broadcast function for events.
	broadcastEvent := func(be BuildEvent) {
		sseEvt := buildEventToSSE(be)
		select {
		case events <- sseEvt:
		default:
			log.Printf("component=web.generate action=drop_sse_event project_id=%s run_id=%s reason=channel_full", projectID, runID)
		}
	}

	// Create the interviewer for human gates.
	interviewer := newBuildInterviewer(ctx, broadcastEvent)

	// Pipeline event handler bridges tracker events to SSE.
	pipelineHandler := pipeline.PipelineEventHandlerFunc(func(evt pipeline.PipelineEvent) {
		be := buildEventFromPipeline(evt)

		s.buildsMu.Lock()
		if evt.NodeID != "" {
			state.CurrentNode = evt.NodeID
		}
		if evt.Type == pipeline.EventStageCompleted {
			state.CompletedNodes = append(state.CompletedNodes, evt.NodeID)
		}
		s.buildsMu.Unlock()

		broadcastEvent(be)
	})

	// Agent event handler bridges tracker agent events to SSE.
	agentHandler := agent.EventHandlerFunc(func(evt agent.Event) {
		be := buildEventFromAgent(evt)
		if be.Type != "" {
			broadcastEvent(be)
		}
	})

	go func() {
		defer close(events)
		defer cancel()
		defer func() {
			if rec := recover(); rec != nil {
				s.buildsMu.Lock()
				completedAt := time.Now()
				state.CompletedAt = &completedAt
				state.Status = "failed"
				state.Error = fmt.Sprintf("panic: %v", rec)
				s.buildsMu.Unlock()
				s.persistBuildOutcome(projectID, state)
				log.Printf("component=web.generate action=panic_recovered project_id=%s run_id=%s recovered=%v", projectID, runID, rec)
			}
		}()

		// Parse the embedded meta-pipeline DOT.
		graph, parseErr := pipeline.ParseDOT(metaPipelineDOT)
		if parseErr != nil {
			s.buildsMu.Lock()
			completedAt := time.Now()
			state.CompletedAt = &completedAt
			state.Status = "failed"
			state.Error = fmt.Sprintf("parse meta-pipeline DOT: %v", parseErr)
			s.buildsMu.Unlock()
			s.persistBuildOutcome(projectID, state)
			return
		}

		// Build engine options (no checkpoint for generation builds).
		opts := []pipeline.EngineOption{
			pipeline.WithPipelineEventHandler(pipelineHandler),
			pipeline.WithArtifactDir(workDir),
		}

		registryOpts := []handlers.RegistryOption{
			handlers.WithInterviewer(interviewer, graph),
		}
		if s.llmClient != nil {
			registryOpts = append(registryOpts, handlers.WithLLMClient(s.llmClient, workDir))
			registryOpts = append(registryOpts, handlers.WithExecEnvironment(exec.NewLocalEnvironment(workDir)))
			registryOpts = append(registryOpts, handlers.WithAgentEventHandler(agentHandler))
		}
		registry := handlers.NewDefaultRegistry(graph, registryOpts...)
		engine := pipeline.NewEngine(graph, registry, opts...)

		_, runErr := engine.Run(ctx)

		s.buildsMu.Lock()
		completedAt := time.Now()
		state.CompletedAt = &completedAt
		if runErr != nil {
			if ctx.Err() != nil {
				state.Status = "cancelled"
			} else {
				state.Status = "failed"
				state.Error = runErr.Error()
			}
		} else {
			state.Status = "completed"
		}
		s.buildsMu.Unlock()
		s.persistBuildOutcome(projectID, state)

		// On success, read the generated pipeline.dot and store it in the project.
		// Re-read the project from the store to avoid overwriting concurrent changes.
		if runErr == nil {
			dotPath := filepath.Join(workDir, "pipeline.dot")
			dotBytes, readErr := os.ReadFile(dotPath)
			if readErr != nil {
				log.Printf("component=web.generate action=read_pipeline_dot_failed project_id=%s run_id=%s err=%v", projectID, runID, readErr)
				return
			}
			fresh, ok := s.store.Get(projectID)
			if !ok {
				log.Printf("component=web.generate action=project_not_found project_id=%s run_id=%s", projectID, runID)
				return
			}
			fresh.DOT = string(dotBytes)
			if err := s.store.Update(fresh); err != nil {
				log.Printf("component=web.generate action=update_project_dot_failed project_id=%s run_id=%s err=%v", projectID, runID, err)
			}
		}
	}()

	return runID
}

// handleGeneratePipeline triggers pipeline generation from the project's spec.
// It exports the spec to markdown, then runs the embedded meta-pipeline to
// produce a DOT pipeline file.
func (s *Server) handleGeneratePipeline(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	p, ok := s.store.Get(projectID)
	if !ok {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}

	// Concurrency guard: only block if there is an actively running build.
	s.buildsMu.RLock()
	existingRun, hasRun := s.builds[projectID]
	runningNow := hasRun && existingRun != nil && existingRun.State != nil && existingRun.State.Status == "running"
	s.buildsMu.RUnlock()
	if runningNow {
		http.Error(w, "a build is already running for this project", http.StatusConflict)
		return
	}

	// Resolve the spec actor and export markdown.
	if strings.TrimSpace(p.SpecID) == "" {
		http.Error(w, "project has no spec", http.StatusBadRequest)
		return
	}
	specID, err := ulid.Parse(p.SpecID)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid spec ID: %v", err), http.StatusBadRequest)
		return
	}
	handle := s.specState.GetActor(specID)
	if handle == nil {
		http.Error(w, "spec actor not found", http.StatusNotFound)
		return
	}

	var specMarkdown string
	handle.ReadState(func(st *core.SpecState) {
		if st != nil {
			specMarkdown = export.ExportMarkdown(st)
		}
	})
	if strings.TrimSpace(specMarkdown) == "" {
		http.Error(w, "spec is empty, nothing to generate from", http.StatusBadRequest)
		return
	}

	runID := s.startGenerationBuild(projectID, specMarkdown)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"run_id":     runID,
		"project_id": projectID,
	})
}
