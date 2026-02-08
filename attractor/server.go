// ABOUTME: HTTP server for managing pipeline execution via REST API with SSE streaming.
// ABOUTME: Provides endpoints for submitting, querying, cancelling pipelines, and human-in-the-loop Q&A.
package attractor

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// PipelineServer manages HTTP endpoints for running pipelines.
type PipelineServer struct {
	engine    *Engine
	pipelines map[string]*PipelineRun
	mu        sync.RWMutex
	mux       *http.ServeMux
}

// PipelineRun tracks a running pipeline.
type PipelineRun struct {
	ID        string
	Status    string // "running", "completed", "failed", "cancelled"
	Source    string // original DOT source
	Result    *RunResult
	Error     string
	Events    []EngineEvent    // collected events
	Cancel    context.CancelFunc
	Questions   []PendingQuestion      // for human-in-the-loop
	mu          sync.RWMutex
	CreatedAt   time.Time
	answerChans map[string]chan string // qid -> channel for delivering answers

	// interviewer bridges HTTP question/answer with the pipeline's Interviewer.Ask calls
	interviewer *httpInterviewer
}

// PendingQuestion represents a question waiting for a human answer.
type PendingQuestion struct {
	ID       string   `json:"id"`
	Question string   `json:"question"`
	Options  []string `json:"options"`
	Answered bool     `json:"answered"`
	Answer   string   `json:"answer,omitempty"`
}

// PipelineStatus is the JSON response for pipeline status queries.
type PipelineStatus struct {
	ID             string    `json:"id"`
	Status         string    `json:"status"`
	CompletedNodes []string  `json:"completed_nodes,omitempty"`
	Error          string    `json:"error,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// httpInterviewer implements the Interviewer interface by bridging HTTP requests.
// When Ask is called by a pipeline handler, it registers a PendingQuestion and blocks
// until the answer arrives via an HTTP POST endpoint.
type httpInterviewer struct {
	run *PipelineRun
}

// Ask registers a pending question on the pipeline run and blocks until answered.
func (h *httpInterviewer) Ask(ctx context.Context, question string, options []string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	qid := generateID()
	answerCh := make(chan string, 1)

	pq := PendingQuestion{
		ID:       qid,
		Question: question,
		Options:  options,
	}

	h.run.mu.Lock()
	h.run.Questions = append(h.run.Questions, pq)
	h.run.mu.Unlock()

	// Store the answer channel so the HTTP handler can send the answer
	h.run.mu.Lock()
	if h.run.answerChans == nil {
		h.run.answerChans = make(map[string]chan string)
	}
	h.run.answerChans[qid] = answerCh
	h.run.mu.Unlock()

	// Block until we get an answer or context is cancelled
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case answer := <-answerCh:
		return answer, nil
	}
}

// NewPipelineServer creates a new PipelineServer with the given engine.
func NewPipelineServer(engine *Engine) *PipelineServer {
	s := &PipelineServer{
		engine:    engine,
		pipelines: make(map[string]*PipelineRun),
	}
	s.mux = http.NewServeMux()
	s.registerRoutes()
	return s
}

// ServeHTTP delegates to the internal mux.
func (s *PipelineServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// Handler returns the HTTP handler for this server.
func (s *PipelineServer) Handler() http.Handler {
	return s.mux
}

// registerRoutes sets up all HTTP routes using Go 1.22+ ServeMux patterns.
func (s *PipelineServer) registerRoutes() {
	s.mux.HandleFunc("POST /pipelines", s.handleSubmitPipeline)
	s.mux.HandleFunc("GET /pipelines/{id}", s.handleGetPipeline)
	s.mux.HandleFunc("GET /pipelines/{id}/events", s.handleEvents)
	s.mux.HandleFunc("POST /pipelines/{id}/cancel", s.handleCancel)
	s.mux.HandleFunc("GET /pipelines/{id}/questions", s.handleGetQuestions)
	s.mux.HandleFunc("POST /pipelines/{id}/questions/{qid}/answer", s.handleAnswerQuestion)
	s.mux.HandleFunc("GET /pipelines/{id}/context", s.handleGetContext)
}

// handleSubmitPipeline handles POST /pipelines.
func (s *PipelineServer) handleSubmitPipeline(w http.ResponseWriter, r *http.Request) {
	var source string

	contentType := r.Header.Get("Content-Type")
	if contentType == "application/json" {
		var req struct {
			Source string `json:"source"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
			return
		}
		source = req.Source
	} else {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to read body"})
			return
		}
		source = string(body)
	}

	if source == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "empty pipeline source"})
		return
	}

	id := generateID()
	ctx, cancel := context.WithCancel(context.Background())

	run := &PipelineRun{
		ID:          id,
		Status:      "running",
		Source:      source,
		Cancel:      cancel,
		Questions:   make([]PendingQuestion, 0),
		CreatedAt:   time.Now(),
		answerChans: make(map[string]chan string),
	}
	run.interviewer = &httpInterviewer{run: run}

	s.mu.Lock()
	s.pipelines[id] = run
	s.mu.Unlock()

	// Build a per-pipeline engine config that captures events and injects the interviewer
	engineConfig := s.engine.config
	engineConfig.EventHandler = func(evt EngineEvent) {
		run.mu.Lock()
		run.Events = append(run.Events, evt)
		run.mu.Unlock()
	}

	// Create a new handler registry that wraps each handler with interviewer injection.
	sourceRegistry := engineConfig.Handlers
	if sourceRegistry == nil {
		sourceRegistry = DefaultHandlerRegistry()
	}
	wrappedRegistry := wrapRegistryWithInterviewer(sourceRegistry, run.interviewer)
	engineConfig.Handlers = wrappedRegistry
	pipelineEngine := NewEngine(engineConfig)

	// Start pipeline execution in a goroutine
	go func() {
		result, err := pipelineEngine.Run(ctx, source)
		run.mu.Lock()
		defer run.mu.Unlock()
		if err != nil {
			if ctx.Err() != nil {
				run.Status = "cancelled"
			} else {
				run.Status = "failed"
				run.Error = err.Error()
			}
		} else {
			run.Status = "completed"
		}
		run.Result = result
	}()

	writeJSON(w, http.StatusAccepted, map[string]string{
		"id":     id,
		"status": "running",
	})
}

// handleGetPipeline handles GET /pipelines/{id}.
func (s *PipelineServer) handleGetPipeline(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	s.mu.RLock()
	run, ok := s.pipelines[id]
	s.mu.RUnlock()

	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "pipeline not found"})
		return
	}

	run.mu.RLock()
	status := PipelineStatus{
		ID:        run.ID,
		Status:    run.Status,
		Error:     run.Error,
		CreatedAt: run.CreatedAt,
	}
	if run.Result != nil {
		status.CompletedNodes = run.Result.CompletedNodes
	}
	run.mu.RUnlock()

	writeJSON(w, http.StatusOK, status)
}

// handleEvents handles GET /pipelines/{id}/events as an SSE stream.
func (s *PipelineServer) handleEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	s.mu.RLock()
	run, ok := s.pipelines[id]
	s.mu.RUnlock()

	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "pipeline not found"})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	// Track how many events we've already sent
	sentCount := 0

	for {
		run.mu.RLock()
		currentEvents := run.Events
		status := run.Status
		run.mu.RUnlock()

		// Send any new events
		for sentCount < len(currentEvents) {
			evt := currentEvents[sentCount]
			data, _ := json.Marshal(map[string]any{
				"type":    string(evt.Type),
				"node_id": evt.NodeID,
				"data":    evt.Data,
			})
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			sentCount++
		}

		// If the pipeline is done, send final status and close
		if status == "completed" || status == "failed" || status == "cancelled" {
			data, _ := json.Marshal(map[string]string{"status": status})
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			return
		}

		// Check if client disconnected
		select {
		case <-r.Context().Done():
			return
		case <-time.After(100 * time.Millisecond):
			// Poll again
		}
	}
}

// handleCancel handles POST /pipelines/{id}/cancel.
func (s *PipelineServer) handleCancel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	s.mu.RLock()
	run, ok := s.pipelines[id]
	s.mu.RUnlock()

	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "pipeline not found"})
		return
	}

	run.Cancel()

	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

// handleGetQuestions handles GET /pipelines/{id}/questions.
func (s *PipelineServer) handleGetQuestions(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	s.mu.RLock()
	run, ok := s.pipelines[id]
	s.mu.RUnlock()

	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "pipeline not found"})
		return
	}

	run.mu.RLock()
	var pending []PendingQuestion
	for _, q := range run.Questions {
		if !q.Answered {
			pending = append(pending, q)
		}
	}
	run.mu.RUnlock()

	if pending == nil {
		pending = make([]PendingQuestion, 0)
	}

	writeJSON(w, http.StatusOK, pending)
}

// handleAnswerQuestion handles POST /pipelines/{id}/questions/{qid}/answer.
func (s *PipelineServer) handleAnswerQuestion(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	qid := r.PathValue("qid")

	s.mu.RLock()
	run, ok := s.pipelines[id]
	s.mu.RUnlock()

	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "pipeline not found"})
		return
	}

	var req struct {
		Answer string `json:"answer"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}

	run.mu.Lock()
	found := false
	for i := range run.Questions {
		if run.Questions[i].ID == qid {
			run.Questions[i].Answered = true
			run.Questions[i].Answer = req.Answer
			found = true
			break
		}
	}

	// Send the answer to the waiting Ask() call
	var answerCh chan string
	if found {
		answerCh = run.answerChans[qid]
		delete(run.answerChans, qid)
	}
	run.mu.Unlock()

	if !found {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "question not found"})
		return
	}

	if answerCh != nil {
		answerCh <- req.Answer
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "answered"})
}

// handleGetContext handles GET /pipelines/{id}/context.
func (s *PipelineServer) handleGetContext(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	s.mu.RLock()
	run, ok := s.pipelines[id]
	s.mu.RUnlock()

	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "pipeline not found"})
		return
	}

	run.mu.RLock()
	result := run.Result
	run.mu.RUnlock()

	if result == nil || result.Context == nil {
		writeJSON(w, http.StatusOK, map[string]any{})
		return
	}

	writeJSON(w, http.StatusOK, result.Context.Snapshot())
}

// wrapRegistryWithInterviewer creates a new HandlerRegistry where each handler
// is wrapped to inject the given Interviewer into the pipeline context.
func wrapRegistryWithInterviewer(source *HandlerRegistry, interviewer Interviewer) *HandlerRegistry {
	wrapped := NewHandlerRegistry()
	for typeName, handler := range source.handlers {
		wrapped.handlers[typeName] = &interviewerInjectingHandler{
			inner:       handler,
			interviewer: interviewer,
		}
	}
	return wrapped
}

// interviewerInjectingHandler wraps a NodeHandler, injecting an Interviewer
// into the pipeline context before delegating execution.
type interviewerInjectingHandler struct {
	inner       NodeHandler
	interviewer Interviewer
}

func (h *interviewerInjectingHandler) Type() string { return h.inner.Type() }

func (h *interviewerInjectingHandler) Execute(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
	// Inject the interviewer so handlers can use it for human-in-the-loop
	pctx.Set("_interviewer", h.interviewer)
	return h.inner.Execute(ctx, node, pctx, store)
}

// writeJSON encodes v as JSON and writes it with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// generateID creates a random hex ID suitable for pipeline identification.
func generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
