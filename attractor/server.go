// ABOUTME: HTTP server for managing pipeline execution via REST API with SSE streaming.
// ABOUTME: Provides endpoints for submitting, querying, cancelling pipelines, and human-in-the-loop Q&A.
package attractor

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// GraphDOTFunc converts a Graph to DOT text. Used by PipelineServer for graph rendering.
type GraphDOTFunc func(g *Graph) string

// GraphDOTWithStatusFunc converts a Graph to DOT text with execution status color overlays.
type GraphDOTWithStatusFunc func(g *Graph, outcomes map[string]*Outcome) string

// DOTRenderFunc renders raw DOT text to the specified format (svg, png).
type DOTRenderFunc func(ctx context.Context, dotText string, format string) ([]byte, error)

// PipelineServer manages HTTP endpoints for running pipelines.
type PipelineServer struct {
	engine     *Engine
	pipelines  map[string]*PipelineRun
	mu         sync.RWMutex
	mux        *http.ServeMux
	eventQuery EventQuery // optional backing store for event query endpoints

	// ToDOT converts a Graph to DOT text. If nil, handleGetGraph uses a minimal fallback.
	ToDOT GraphDOTFunc

	// ToDOTWithStatus converts a Graph to DOT text with status color overlays.
	// If nil, falls back to ToDOT.
	ToDOTWithStatus GraphDOTWithStatusFunc

	// RenderDOTSource renders raw DOT text to svg/png. If nil, only DOT format is available.
	RenderDOTSource DOTRenderFunc
}

// PipelineRun tracks a running pipeline.
type PipelineRun struct {
	ID          string
	Status      string // "running", "completed", "failed", "cancelled"
	Source      string // original DOT source
	Result      *RunResult
	Error       string
	ArtifactDir string        // path to the run's artifact directory
	Events      []EngineEvent // collected events
	Cancel      context.CancelFunc
	Questions   []PendingQuestion // for human-in-the-loop
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
	ArtifactDir    string    `json:"artifact_dir,omitempty"`
	Error          string    `json:"error,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// EventQueryResponse is the JSON response for event query endpoints.
type EventQueryResponse struct {
	Events []EngineEvent `json:"events"`
	Total  int           `json:"total"`
}

// EventTailResponse is the JSON response for the event tail endpoint.
type EventTailResponse struct {
	Events []EngineEvent `json:"events"`
}

// EventSummaryResponse is the JSON response for the event summary endpoint.
type EventSummaryResponse struct {
	TotalEvents int            `json:"total_events"`
	ByType      map[string]int `json:"by_type"`
	ByNode      map[string]int `json:"by_node"`
	FirstEvent  string         `json:"first_event,omitempty"`
	LastEvent   string         `json:"last_event,omitempty"`
}

// SetEventQuery configures the EventQuery backing store for the event query REST endpoints.
func (s *PipelineServer) SetEventQuery(eq EventQuery) {
	s.eventQuery = eq
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

	// Register the answer channel and question atomically in a single critical section.
	// The channel must be set before the question becomes visible to prevent a race
	// where handleAnswerQuestion finds the question but no channel to deliver the answer.
	h.run.mu.Lock()
	if h.run.answerChans == nil {
		h.run.answerChans = make(map[string]chan string)
	}
	h.run.answerChans[qid] = answerCh
	h.run.Questions = append(h.run.Questions, pq)
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

// Handler returns the HTTP handler for this server, wrapped with request logging.
func (s *PipelineServer) Handler() http.Handler {
	return &loggingHandler{inner: s.mux}
}

// loggingHandler wraps an http.Handler with request/response logging.
type loggingHandler struct {
	inner http.Handler
}

func (h *loggingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	lw := &statusWriter{ResponseWriter: w, status: 200}
	h.inner.ServeHTTP(lw, r)
	log.Printf("%s %s %d %s\n", r.Method, r.URL.Path, lw.status, time.Since(start).Round(time.Millisecond))
}

// statusWriter captures the HTTP status code from WriteHeader.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *statusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// registerRoutes sets up all HTTP routes using Go 1.22+ ServeMux patterns.
func (s *PipelineServer) registerRoutes() {
	// JSON API routes
	s.mux.HandleFunc("POST /pipelines", s.handleSubmitPipeline)
	s.mux.HandleFunc("GET /pipelines/{id}", s.handleGetPipeline)
	s.mux.HandleFunc("GET /pipelines/{id}/events/query", s.handleQueryEvents)
	s.mux.HandleFunc("GET /pipelines/{id}/events/tail", s.handleTailEvents)
	s.mux.HandleFunc("GET /pipelines/{id}/events/summary", s.handleSummaryEvents)
	s.mux.HandleFunc("GET /pipelines/{id}/events", s.handleEvents)
	s.mux.HandleFunc("POST /pipelines/{id}/cancel", s.handleCancel)
	s.mux.HandleFunc("GET /pipelines/{id}/questions", s.handleGetQuestions)
	s.mux.HandleFunc("POST /pipelines/{id}/questions/{qid}/answer", s.handleAnswerQuestion)
	s.mux.HandleFunc("GET /pipelines/{id}/context", s.handleGetContext)
	s.mux.HandleFunc("GET /pipelines/{id}/graph", s.handleGetGraph)

	// HTMX web frontend routes
	s.registerUIRoutes()
}

// handleSubmitPipeline handles POST /pipelines.
func (s *PipelineServer) handleSubmitPipeline(w http.ResponseWriter, r *http.Request) {
	var source string

	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaType == "application/json" {
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

	// Pre-validate: parse and validate the pipeline before accepting it.
	// Catches syntax errors and structural problems immediately instead of
	// creating a pipeline run that fails asynchronously.
	graph, parseErr := Parse(source)
	if parseErr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("parse error: %v", parseErr)})
		return
	}
	transforms := DefaultTransforms()
	graph = ApplyTransforms(graph, transforms...)
	if _, validErr := ValidateOrError(graph); validErr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("validation failed: %v", validErr)})
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

	log.Printf("[pipeline %s] submitted (%d bytes)\n", id, len(source))

	// Build a per-pipeline engine config that captures events and injects the interviewer.
	// Each pipeline gets its own run directory under the artifacts base, keyed by pipeline ID.
	// Chain the base event handler (e.g. verbose logging) with the per-pipeline handler
	// that stores events for the web UI.
	engineConfig := s.engine.config
	engineConfig.RunID = id
	baseHandler := engineConfig.EventHandler
	engineConfig.EventHandler = func(evt EngineEvent) {
		run.mu.Lock()
		run.Events = append(run.Events, evt)
		run.mu.Unlock()
		if baseHandler != nil {
			baseHandler(evt)
		}
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
				log.Printf("[pipeline %s] cancelled\n", id)
			} else {
				run.Status = "failed"
				run.Error = err.Error()
				log.Printf("[pipeline %s] failed: %s\n", id, err.Error())
			}
		} else {
			run.Status = "completed"
			completedCount := 0
			if result != nil {
				completedCount = len(result.CompletedNodes)
				if workDir := result.Context.GetString("_workdir", ""); workDir != "" {
					run.ArtifactDir = workDir
				}
			}
			log.Printf("[pipeline %s] completed (%d nodes)\n", id, completedCount)
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
		ID:          run.ID,
		Status:      run.Status,
		Error:       run.Error,
		ArtifactDir: run.ArtifactDir,
		CreatedAt:   run.CreatedAt,
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
		currentEvents := make([]EngineEvent, len(run.Events))
		copy(currentEvents, run.Events)
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

// handleQueryEvents handles GET /pipelines/{id}/events/query.
// Supports query params: type, node, since, until (RFC3339), limit, offset.
func (s *PipelineServer) handleQueryEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	s.mu.RLock()
	_, ok := s.pipelines[id]
	s.mu.RUnlock()

	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "pipeline not found"})
		return
	}

	if s.eventQuery == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "event query not configured"})
		return
	}

	q := r.URL.Query()

	var filter EventFilter

	if typeParam := q.Get("type"); typeParam != "" {
		filter.Types = []EngineEventType{EngineEventType(typeParam)}
	}

	if nodeParam := q.Get("node"); nodeParam != "" {
		filter.NodeID = nodeParam
	}

	if sinceParam := q.Get("since"); sinceParam != "" {
		t, err := time.Parse(time.RFC3339, sinceParam)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid since parameter: " + err.Error()})
			return
		}
		filter.Since = &t
	}

	if untilParam := q.Get("until"); untilParam != "" {
		t, err := time.Parse(time.RFC3339, untilParam)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid until parameter: " + err.Error()})
			return
		}
		filter.Until = &t
	}

	if limitParam := q.Get("limit"); limitParam != "" {
		v, err := strconv.Atoi(limitParam)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid limit parameter: " + err.Error()})
			return
		}
		filter.Limit = v
	}

	if offsetParam := q.Get("offset"); offsetParam != "" {
		v, err := strconv.Atoi(offsetParam)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid offset parameter: " + err.Error()})
			return
		}
		filter.Offset = v
	}

	// Get total count using a filter without pagination
	countFilter := EventFilter{
		Types:  filter.Types,
		NodeID: filter.NodeID,
		Since:  filter.Since,
		Until:  filter.Until,
	}
	total, err := s.eventQuery.CountEvents(id, countFilter)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "count events: " + err.Error()})
		return
	}

	events, err := s.eventQuery.QueryEvents(id, filter)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "query events: " + err.Error()})
		return
	}

	if events == nil {
		events = []EngineEvent{}
	}

	writeJSON(w, http.StatusOK, EventQueryResponse{
		Events: events,
		Total:  total,
	})
}

// handleTailEvents handles GET /pipelines/{id}/events/tail.
// Supports query param: n (default 10).
func (s *PipelineServer) handleTailEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	s.mu.RLock()
	_, ok := s.pipelines[id]
	s.mu.RUnlock()

	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "pipeline not found"})
		return
	}

	if s.eventQuery == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "event query not configured"})
		return
	}

	n := 10
	if nParam := r.URL.Query().Get("n"); nParam != "" {
		v, err := strconv.Atoi(nParam)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid n parameter: " + err.Error()})
			return
		}
		n = v
	}

	events, err := s.eventQuery.TailEvents(id, n)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "tail events: " + err.Error()})
		return
	}

	if events == nil {
		events = []EngineEvent{}
	}

	writeJSON(w, http.StatusOK, EventTailResponse{
		Events: events,
	})
}

// handleSummaryEvents handles GET /pipelines/{id}/events/summary.
func (s *PipelineServer) handleSummaryEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	s.mu.RLock()
	_, ok := s.pipelines[id]
	s.mu.RUnlock()

	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "pipeline not found"})
		return
	}

	if s.eventQuery == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "event query not configured"})
		return
	}

	summary, err := s.eventQuery.SummarizeEvents(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "summarize events: " + err.Error()})
		return
	}

	// Convert EngineEventType keys to strings for JSON serialization
	byType := make(map[string]int, len(summary.ByType))
	for k, v := range summary.ByType {
		byType[string(k)] = v
	}

	resp := EventSummaryResponse{
		TotalEvents: summary.TotalEvents,
		ByType:      byType,
		ByNode:      summary.ByNode,
	}

	if summary.FirstEvent != nil {
		resp.FirstEvent = summary.FirstEvent.Format(time.RFC3339Nano)
	}
	if summary.LastEvent != nil {
		resp.LastEvent = summary.LastEvent.Format(time.RFC3339Nano)
	}

	writeJSON(w, http.StatusOK, resp)
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
// Supports both HTMX form-encoded requests (detected via HX-Request header)
// and JSON API requests. HTMX requests return updated questions HTML;
// API requests return JSON.
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

	isHTMX := r.Header.Get("HX-Request") != ""

	// Extract answer from form data (HTMX) or JSON body (API)
	var answer string
	if isHTMX {
		r.ParseForm()
		answer = r.FormValue("answer")
	} else {
		var payload struct {
			Answer string `json:"answer"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		answer = payload.Answer
	}

	if answer == "" {
		http.Error(w, "answer is required", http.StatusBadRequest)
		return
	}

	run.mu.Lock()
	found := false
	for i := range run.Questions {
		if run.Questions[i].ID == qid {
			run.Questions[i].Answered = true
			run.Questions[i].Answer = answer
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
		answerCh <- answer
	}

	if isHTMX {
		// Return updated questions HTML for HTMX swap
		run.mu.RLock()
		questions := make([]PendingQuestion, len(run.Questions))
		copy(questions, run.Questions)
		run.mu.RUnlock()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(renderQuestionsFragmentHTML(id, questions)))
	} else {
		writeJSON(w, http.StatusOK, map[string]string{"status": "answered"})
	}
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

// handleGetGraph handles GET /pipelines/{id}/graph.
// Returns the pipeline graph rendered in the requested format (dot, svg, png).
// Accepts ?format=dot|svg|png query param (default: svg).
// If the pipeline has execution results, the graph includes status color overlays.
func (s *PipelineServer) handleGetGraph(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	s.mu.RLock()
	run, ok := s.pipelines[id]
	s.mu.RUnlock()

	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "pipeline not found"})
		return
	}

	run.mu.RLock()
	source := run.Source
	result := run.Result
	run.mu.RUnlock()

	if source == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "pipeline has no source"})
		return
	}

	graph, err := Parse(source)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to parse pipeline source: " + err.Error()})
		return
	}

	format := r.URL.Query().Get("format")
	if format == "" {
		format = "svg"
	}

	// Generate DOT text: use status overlay if we have execution results
	var dotText string
	if result != nil && result.NodeOutcomes != nil && s.ToDOTWithStatus != nil {
		dotText = s.ToDOTWithStatus(graph, result.NodeOutcomes)
	} else if s.ToDOT != nil {
		dotText = s.ToDOT(graph)
	} else {
		// Minimal fallback: return the original source as DOT text
		dotText = source
	}

	switch format {
	case "dot":
		w.Header().Set("Content-Type", "text/vnd.graphviz")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(dotText))
	case "svg":
		if s.RenderDOTSource == nil {
			// No renderer configured: fallback to DOT text
			w.Header().Set("Content-Type", "text/vnd.graphviz")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(dotText))
			return
		}
		data, renderErr := s.RenderDOTSource(r.Context(), dotText, "svg")
		if renderErr != nil {
			// Graphviz not available: fallback to DOT text
			w.Header().Set("Content-Type", "text/vnd.graphviz")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(dotText))
			return
		}
		w.Header().Set("Content-Type", "image/svg+xml")
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	case "png":
		if s.RenderDOTSource == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "render not configured"})
			return
		}
		data, renderErr := s.RenderDOTSource(r.Context(), dotText, "png")
		if renderErr != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "graphviz not available: " + renderErr.Error()})
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported format: " + format + " (supported: dot, svg, png)"})
	}
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

func (h *interviewerInjectingHandler) Type() string              { return h.inner.Type() }
func (h *interviewerInjectingHandler) InnerHandler() NodeHandler { return h.inner }

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
// Falls back to a timestamp-based ID if the random source fails.
func generateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback: use current time as a unique-enough ID
		return fmt.Sprintf("t%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
