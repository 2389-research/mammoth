// ABOUTME: Adapter layer connecting spec/web handlers to the unified mammoth HTTP server.
// ABOUTME: Provides middleware for URL translation, lazy spec initialization, and API handlers.
package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	muxllm "github.com/2389-research/mux/llm"
	"github.com/go-chi/chi/v5"
	"github.com/oklog/ulid/v2"

	"github.com/2389-research/mammoth/spec/core"
	"github.com/2389-research/mammoth/spec/server"
	specweb "github.com/2389-research/mammoth/spec/web"
)

// Context keys for passing project and spec IDs through the middleware chain.
type specCtxKey string

const (
	ctxKeyProjectID specCtxKey = "specProjectID"
	ctxKeySpecID    specCtxKey = "specSpecID"
)

// specRouter builds the chi sub-router for all spec builder routes. It applies
// middleware to resolve projectID → specID and rewrite URLs in HTML responses.
func (s *Server) specRouter(r chi.Router) {
	r.Use(s.specContextMiddleware)
	r.Use(s.specURLRewriteMiddleware)

	state := s.specState
	renderer := s.specRenderer

	// Main views
	r.Get("/", specweb.SpecView(state, renderer))
	r.Post("/", specweb.CreateSpec(state, renderer))
	r.Get("/board", specweb.Board(state, renderer))
	r.Get("/document", specweb.Document(state, renderer))
	r.Get("/activity", specweb.Activity(state, renderer))
	r.Get("/activity/transcript", specweb.ActivityTranscript(state, renderer))
	r.Get("/chat-panel", specweb.ChatPanel(state, renderer))
	r.Get("/diagram", specweb.Diagram(state, renderer))
	r.Get("/artifacts", specweb.Artifacts(state, renderer))
	r.Get("/ticker", specweb.Ticker(state, renderer))
	r.Get("/provider-status", specweb.ProviderStatus(state, renderer))

	// Card management
	r.Get("/cards/new", specweb.CreateCardForm(state, renderer))
	r.Post("/cards", specweb.CreateCard(state, renderer))
	r.Get("/cards/{card_id}/edit", specweb.EditCardForm(state, renderer))
	r.Put("/cards/{card_id}", specweb.UpdateCard(state, renderer))
	r.Delete("/cards/{card_id}", specweb.DeleteCard(state, renderer))

	// Export
	r.Get("/export/markdown", specweb.ExportMarkdown(state))
	r.Get("/export/yaml", specweb.ExportYAML(state))
	r.Get("/export/dot", specweb.ExportDOT(state))

	// Agent management
	r.Post("/agents/start", specweb.StartAgents(state, renderer))
	r.Post("/agents/pause", specweb.PauseAgents(state, renderer))
	r.Post("/agents/resume", specweb.ResumeAgents(state, renderer))
	r.Get("/agents/status", specweb.AgentStatus(state, renderer))
	r.Get("/agents/leds", specweb.AgentLEDs(state, renderer))

	// Actions
	r.Post("/chat", specweb.Chat(state, renderer))
	r.Post("/answer", specweb.AnswerQuestion(state, renderer))
	r.Post("/undo", specweb.Undo(state, renderer))
	r.Post("/regenerate", specweb.Regenerate(state))

	// JSON API (board drag-and-drop commands + SSE event stream)
	r.Route("/api", func(r chi.Router) {
		r.Post("/commands", s.handleSpecCommands)
		r.Get("/events/stream", s.handleSpecEventStream)
	})
}

// specContextMiddleware resolves the projectID from the parent route, looks up
// or lazily creates the associated spec actor, and injects the spec ID as a chi
// "id" URL parameter so all spec/web handlers work unchanged.
//
// Uses double-checked locking via specInitMu to prevent duplicate actors when
// concurrent requests hit the same uninitialized project.
func (s *Server) specContextMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		projectID := chi.URLParam(r, "projectID")
		p, ok := s.store.Get(projectID)
		if !ok {
			http.Error(w, "project not found", http.StatusNotFound)
			return
		}

		specIDStr, err := s.ensureSpecActor(projectID, p.SpecID)
		if err != nil {
			log.Printf("spec middleware: %v", err)
			http.Error(w, "failed to initialize spec", http.StatusInternalServerError)
			return
		}

		// Inject spec ID as chi "id" param for spec/web handlers.
		rctx := chi.RouteContext(r.Context())
		rctx.URLParams.Add("id", specIDStr)

		ctx := context.WithValue(r.Context(), ctxKeyProjectID, projectID)
		ctx = context.WithValue(ctx, ctxKeySpecID, specIDStr)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ensureSpecActor guarantees that the project has a spec actor loaded in memory.
// It uses double-checked locking: first a fast-path check without the mutex,
// then a slow path that serializes initialization to prevent duplicates.
func (s *Server) ensureSpecActor(projectID, specIDStr string) (string, error) {
	// Fast path: spec already exists and actor is loaded.
	if specIDStr != "" {
		specID, err := ulid.Parse(specIDStr)
		if err != nil {
			return "", fmt.Errorf("invalid spec ID %q on project %s: %w", specIDStr, projectID, err)
		}
		if s.specState.GetActor(specID) != nil {
			return specIDStr, nil
		}
	}

	// Slow path: serialize init/recovery to prevent duplicate actors.
	s.specInitMu.Lock()
	defer s.specInitMu.Unlock()

	// Re-read project under lock in case another goroutine just initialized it.
	p, ok := s.store.Get(projectID)
	if !ok {
		return "", fmt.Errorf("project %s not found", projectID)
	}
	specIDStr = p.SpecID

	if specIDStr == "" {
		newSpecID, err := s.initializeSpec(projectID)
		if err != nil {
			return "", fmt.Errorf("init spec for project %s: %w", projectID, err)
		}
		return newSpecID.String(), nil
	}

	specID, err := ulid.Parse(specIDStr)
	if err != nil {
		return "", fmt.Errorf("invalid spec ID %q on project %s: %w", specIDStr, projectID, err)
	}

	// Double-check: another request may have recovered this actor while we waited.
	if s.specState.GetActor(specID) != nil {
		return specIDStr, nil
	}

	if err := s.recoverSpec(specID); err != nil {
		return "", fmt.Errorf("recover spec %s: %w", specIDStr, err)
	}
	return specIDStr, nil
}

// specURLRewriteMiddleware intercepts HTML responses and rewrites URLs from the
// spec builder's native /web/specs/{specID} namespace to the unified server's
// /projects/{projectID}/spec namespace. Non-HTML responses (JSON, SSE, file
// downloads) pass through unmodified.
func (s *Server) specURLRewriteMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		projectID, _ := r.Context().Value(ctxKeyProjectID).(string)
		specID, _ := r.Context().Value(ctxKeySpecID).(string)

		if projectID == "" || specID == "" {
			next.ServeHTTP(w, r)
			return
		}

		rw := &specURLRewriter{
			ResponseWriter: w,
			specID:         specID,
			projectID:      projectID,
			code:           http.StatusOK,
		}

		next.ServeHTTP(rw, r)
		rw.finish()
	})
}

// specURLRewriter is an http.ResponseWriter wrapper that buffers HTML responses
// for URL rewriting. Non-HTML responses are written through to the underlying
// writer immediately.
type specURLRewriter struct {
	http.ResponseWriter
	buf         bytes.Buffer
	specID      string
	projectID   string
	code        int
	passthrough bool // true once we know this is not HTML
	decided     bool // true once we've checked Content-Type
	finished    bool // true after finish() has been called
}

func (rw *specURLRewriter) WriteHeader(code int) {
	rw.code = code
	if !rw.decided {
		rw.decided = true
		ct := rw.Header().Get("Content-Type")
		if !strings.HasPrefix(ct, "text/html") {
			rw.passthrough = true
			rw.ResponseWriter.WriteHeader(code)
		}
	}
}

func (rw *specURLRewriter) Write(b []byte) (int, error) {
	if !rw.decided {
		rw.decided = true
		ct := rw.Header().Get("Content-Type")
		if !strings.HasPrefix(ct, "text/html") {
			rw.passthrough = true
			rw.ResponseWriter.WriteHeader(rw.code)
		}
	}
	if rw.passthrough {
		return rw.ResponseWriter.Write(b)
	}
	return rw.buf.Write(b)
}

// Flush implements http.Flusher for SSE and streaming responses.
func (rw *specURLRewriter) Flush() {
	if rw.passthrough {
		if f, ok := rw.ResponseWriter.(http.Flusher); ok {
			f.Flush()
		}
	}
}

// finish writes the buffered HTML with URL rewrites to the underlying writer.
// Called by the middleware after the handler returns. No-op for passthrough.
func (rw *specURLRewriter) finish() {
	if rw.passthrough || rw.finished {
		return
	}
	rw.finished = true

	body := rw.buf.Bytes()
	projectPrefix := "/projects/" + rw.projectID + "/spec"

	// Rewrite URLs in the HTML body.
	if len(body) > 0 {
		bodyStr := string(body)
		bodyStr = strings.ReplaceAll(bodyStr, "/web/specs/"+rw.specID, projectPrefix)
		bodyStr = strings.ReplaceAll(bodyStr, "/api/specs/"+rw.specID, projectPrefix+"/api")
		body = []byte(bodyStr)
	}

	// Rewrite the HX-Push-Url header if present.
	if pushURL := rw.Header().Get("HX-Push-Url"); pushURL != "" {
		pushURL = strings.ReplaceAll(pushURL, "/web/specs/"+rw.specID, projectPrefix)
		rw.Header().Set("HX-Push-Url", pushURL)
	}

	// Content-Length may have changed due to URL length differences.
	rw.Header().Del("Content-Length")

	rw.ResponseWriter.WriteHeader(rw.code)
	_, _ = rw.ResponseWriter.Write(body)
}

// handleSpecCommands handles POST .../api/commands for board drag-and-drop.
// Decodes a JSON command, dispatches it to the spec actor, and returns events.
func (s *Server) handleSpecCommands(w http.ResponseWriter, r *http.Request) {
	specIDStr := chi.URLParam(r, "id")
	specID, err := ulid.Parse(specIDStr)
	if err != nil {
		writeSpecJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid spec ID"})
		return
	}

	handle := s.specState.GetActor(specID)
	if handle == nil {
		writeSpecJSON(w, http.StatusNotFound, map[string]string{"error": "spec not found"})
		return
	}

	var rawJSON json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&rawJSON); err != nil {
		writeSpecJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	cmd, err := core.UnmarshalCommand(rawJSON)
	if err != nil {
		writeSpecJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid command: %v", err)})
		return
	}

	events, err := handle.SendCommand(cmd)
	if err != nil {
		writeSpecJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeSpecJSON(w, http.StatusOK, map[string]any{"events": events})
}

// sseHeartbeatInterval is how often the SSE handler sends keep-alive comments.
const sseHeartbeatInterval = 15 * time.Second

// handleSpecEventStream handles GET .../api/events/stream for SSE updates.
// Subscribes to the spec actor's broadcast channel and converts events to
// text/event-stream format with heartbeats.
func (s *Server) handleSpecEventStream(w http.ResponseWriter, r *http.Request) {
	specIDStr := chi.URLParam(r, "id")
	specID, err := ulid.Parse(specIDStr)
	if err != nil {
		http.Error(w, "invalid spec ID", http.StatusBadRequest)
		return
	}

	handle := s.specState.GetActor(specID)
	if handle == nil {
		http.Error(w, "spec not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	ch := handle.Subscribe()
	defer handle.Unsubscribe(ch)
	ctx := r.Context()

	_, _ = fmt.Fprint(w, ":ok\n\n")
	flusher.Flush()

	heartbeat := time.NewTicker(sseHeartbeatInterval)
	defer heartbeat.Stop()

	for {
		select {
		case event, open := <-ch:
			if !open {
				return
			}
			eventType := camelToSnake(event.Payload.EventPayloadType())
			data, marshalErr := json.Marshal(event)
			if marshalErr != nil {
				continue
			}
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, data)
			flusher.Flush()

		case <-heartbeat.C:
			_, _ = fmt.Fprint(w, ":heartbeat\n\n")
			flusher.Flush()

		case <-ctx.Done():
			return
		}
	}
}

// initializeSpec creates a new spec actor for a project. It spawns the actor,
// registers it in the AppState, starts an event persister, and updates the
// project's SpecID field.
func (s *Server) initializeSpec(projectID string) (ulid.ULID, error) {
	specID := core.NewULID()

	specDir := filepath.Join(s.specState.MammothHome, "specs", specID.String())
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		return ulid.ULID{}, fmt.Errorf("create spec directory: %w", err)
	}

	handle := core.SpawnActor(specID, core.NewSpecState())

	server.SpawnEventPersister(s.specState, specID, handle)
	s.specState.SetActor(specID, handle)

	// Auto-start agents if an LLM provider is configured.
	s.specState.TryStartAgents(specID)

	p, ok := s.store.Get(projectID)
	if !ok {
		return ulid.ULID{}, fmt.Errorf("project %s not found", projectID)
	}
	p.SpecID = specID.String()
	if err := s.store.Update(p); err != nil {
		return ulid.ULID{}, fmt.Errorf("update project spec ID: %w", err)
	}

	log.Printf("initialized spec %s for project %s", specID, projectID)
	return specID, nil
}

// recoverSpec loads an existing spec from its JSONL event log and spawns an
// actor. Called when a project references a spec that is not yet in memory.
func (s *Server) recoverSpec(specID ulid.ULID) error {
	specDir := filepath.Join(s.specState.MammothHome, "specs", specID.String())

	// Ensure the spec directory exists so the event persister can write to it.
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		return fmt.Errorf("create spec directory: %w", err)
	}

	logPath := filepath.Join(specDir, "events.jsonl")
	state := core.NewSpecState()

	// Replay events if the log file exists.
	var eventCount int
	data, err := os.ReadFile(logPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read event log: %w", err)
	}
	if err == nil && len(data) > 0 {
		for _, line := range bytes.Split(data, []byte("\n")) {
			line = bytes.TrimSpace(line)
			if len(line) == 0 {
				continue
			}
			var evt core.Event
			if jsonErr := json.Unmarshal(line, &evt); jsonErr != nil {
				log.Printf("spec recovery: skipping malformed event: %v", jsonErr)
				continue
			}
			state.Apply(&evt)
			eventCount++
		}
	}

	handle := core.SpawnActor(specID, state)
	server.SpawnEventPersister(s.specState, specID, handle)
	s.specState.SetActor(specID, handle)
	s.specState.TryStartAgents(specID)

	log.Printf("recovered spec %s (%d events replayed)", specID, eventCount)
	return nil
}

// setupSpecLLMClient detects available LLM providers and configures the mux/llm
// client on the AppState. Called once during server initialization.
func setupSpecLLMClient(state *server.AppState, ps server.ProviderStatus) {
	if !ps.AnyAvailable {
		return
	}

	// Try the default provider first.
	for _, p := range ps.Providers {
		if p.Name == ps.DefaultProvider && p.HasAPIKey {
			apiKey := os.Getenv(strings.ToUpper(p.Name) + "_API_KEY")
			client := createSpecMuxClient(p.Name, apiKey, p.Model)
			if client != nil {
				state.LLMClient = client
				state.LLMModel = p.Model
				log.Printf("spec LLM: using %s (%s)", p.Name, p.Model)
			}
			return
		}
	}

	// Fall back to first available provider.
	for _, p := range ps.Providers {
		if p.HasAPIKey {
			apiKey := os.Getenv(strings.ToUpper(p.Name) + "_API_KEY")
			client := createSpecMuxClient(p.Name, apiKey, p.Model)
			if client != nil {
				state.LLMClient = client
				state.LLMModel = p.Model
				log.Printf("spec LLM: using %s (%s) as fallback", p.Name, p.Model)
			}
			return
		}
	}
}

// createSpecMuxClient creates a mux/llm Client for the given provider.
func createSpecMuxClient(name, apiKey, model string) muxllm.Client {
	switch name {
	case "anthropic":
		return muxllm.NewAnthropicClient(apiKey, model)
	case "openai":
		return muxllm.NewOpenAIClient(apiKey, model)
	case "gemini":
		client, err := muxllm.NewGeminiClient(context.Background(), apiKey, model)
		if err != nil {
			log.Printf("failed to create Gemini mux client: %v", err)
			return nil
		}
		return client
	default:
		return nil
	}
}

// writeSpecJSON writes a JSON response with the given status code.
func writeSpecJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// camelToSnake converts a CamelCase string to snake_case.
// Used to convert EventPayloadType names to SSE event type names.
func camelToSnake(s string) string {
	var result strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				result.WriteByte('_')
			}
			result.WriteRune(unicode.ToLower(r))
		} else {
			result.WriteRune(r)
		}
	}
	return result.String()
}
