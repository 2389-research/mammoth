// ABOUTME: Adapter layer connecting spec/web handlers to the unified mammoth HTTP server.
// ABOUTME: Provides middleware for URL translation, lazy spec initialization, and API handlers.
package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
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

	specagents "github.com/2389-research/mammoth/spec/agents"
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
	r.Get("/agents/state", s.handleSpecAgentState)
	r.Get("/agents/leds", specweb.AgentLEDs(state, renderer))

	// Actions
	r.Post("/chat", specweb.Chat(state, renderer))
	r.Post("/answer", specweb.AnswerQuestion(state, renderer))
	r.Post("/undo", specweb.Undo(state, renderer))
	r.Post("/regenerate", specweb.Regenerate(state))
	r.Get("/options", s.handleSpecOptionsGet)
	r.Post("/options", s.handleSpecOptionsUpdate)

	// JSON API (board drag-and-drop commands + SSE event stream)
	r.Route("/api", func(r chi.Router) {
		r.Post("/commands", s.handleSpecCommands)
		r.Get("/events/stream", s.handleSpecEventStream)
	})
}

func (s *Server) handleSpecOptionsGet(w http.ResponseWriter, r *http.Request) {
	projectID, _ := r.Context().Value(ctxKeyProjectID).(string)
	if projectID == "" {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}
	opts, err := s.readProjectSpecOptions(projectID)
	if err != nil {
		writeSpecJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeSpecJSON(w, http.StatusOK, map[string]any{
		"human_review":     opts.HumanReview,
		"scenario_testing": opts.ScenarioTesting,
		"tdd":              opts.TDD,
	})
}

func (s *Server) handleSpecOptionsUpdate(w http.ResponseWriter, r *http.Request) {
	projectID, _ := r.Context().Value(ctxKeyProjectID).(string)
	if projectID == "" {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	opts := parseSpecBuilderOptions(r.FormValue)
	if err := s.applySpecBuilderOptions(projectID, opts); err != nil {
		writeSpecJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeSpecJSON(w, http.StatusOK, map[string]any{
		"ok":               true,
		"human_review":     opts.HumanReview,
		"scenario_testing": opts.ScenarioTesting,
		"tdd":              opts.TDD,
	})
}

func (s *Server) handleSpecAgentState(w http.ResponseWriter, r *http.Request) {
	projectID, _ := r.Context().Value(ctxKeyProjectID).(string)
	specIDStr, _ := r.Context().Value(ctxKeySpecID).(string)
	if projectID == "" || specIDStr == "" {
		http.Error(w, "spec not found", http.StatusNotFound)
		return
	}
	specID, err := ulid.Parse(specIDStr)
	if err != nil {
		http.Error(w, "invalid spec id", http.StatusBadRequest)
		return
	}
	handle := s.specState.GetActor(specID)
	if handle == nil {
		http.Error(w, "spec not found", http.StatusNotFound)
		return
	}

	sh := s.specState.GetSwarm(specID)
	started := sh != nil
	running := false
	if sh != nil {
		running = !sh.Orchestrator.IsPaused()
	}

	recommendStart := false
	handle.ReadState(func(st *core.SpecState) {
		if st == nil || started {
			return
		}
		if st.Cards != nil && st.Cards.Len() > 0 {
			return
		}
		if st.PendingQuestion != nil {
			return
		}
		for _, msg := range st.Transcript {
			if strings.TrimSpace(msg.Sender) != "human" {
				return
			}
		}
		recommendStart = true
	})

	writeSpecJSON(w, http.StatusOK, map[string]any{
		"started":         started,
		"running":         running,
		"recommend_start": recommendStart,
	})
}

func (s *Server) readProjectSpecOptions(projectID string) (specBuilderOptions, error) {
	opts := specBuilderOptions{HumanReview: true, ScenarioTesting: true, TDD: true}
	p, ok := s.store.Get(projectID)
	if !ok {
		return opts, fmt.Errorf("project %s not found", projectID)
	}
	specIDStr, err := s.ensureSpecActor(projectID, p.SpecID)
	if err != nil {
		return opts, fmt.Errorf("ensure spec actor: %w", err)
	}
	specID, err := ulid.Parse(specIDStr)
	if err != nil {
		return opts, fmt.Errorf("parse spec id: %w", err)
	}
	handle := s.specState.GetActor(specID)
	if handle == nil {
		return opts, fmt.Errorf("spec actor not found")
	}
	var constraints string
	handle.ReadState(func(st *core.SpecState) {
		if st != nil && st.Core != nil && st.Core.Constraints != nil {
			constraints = *st.Core.Constraints
		}
	})
	return optionsFromConstraints(constraints), nil
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
			log.Printf("component=web.spec action=middleware_ensure_actor_failed project_id=%s err=%v", projectID, err)
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

// syncProjectFromSpec exports the project's current spec actor state to DOT and
// updates the project for the editor phase.
func (s *Server) syncProjectFromSpec(projectID string, p *Project) error {
	specIDStr, err := s.ensureSpecActor(projectID, p.SpecID)
	if err != nil {
		return fmt.Errorf("ensure spec actor: %w", err)
	}

	specID, err := ulid.Parse(specIDStr)
	if err != nil {
		return fmt.Errorf("parse spec ID: %w", err)
	}
	handle := s.specState.GetActor(specID)
	if handle == nil {
		return fmt.Errorf("spec actor not found")
	}

	var transitionErr error
	handle.ReadState(func(st *core.SpecState) {
		transitionErr = TransitionSpecToEditor(p, st)
	})
	if transitionErr != nil {
		return transitionErr
	}

	if p.SpecID == "" {
		p.SpecID = specIDStr
	}
	if err := s.store.Update(p); err != nil {
		return fmt.Errorf("update project: %w", err)
	}
	return nil
}

// seedProjectSpecFromPrompt ensures the project's spec actor exists and appends
// the provided text as a human transcript message so agents can parse it.
func (s *Server) seedProjectSpecFromPrompt(projectID, prompt string) error {
	trimmed := strings.TrimSpace(prompt)
	if trimmed == "" {
		return nil
	}

	p, ok := s.store.Get(projectID)
	if !ok {
		return fmt.Errorf("project %s not found", projectID)
	}

	specIDStr, err := s.ensureSpecActor(projectID, p.SpecID)
	if err != nil {
		return fmt.Errorf("ensure spec actor: %w", err)
	}
	specID, err := ulid.Parse(specIDStr)
	if err != nil {
		return fmt.Errorf("parse spec ID: %w", err)
	}

	handle := s.specState.GetActor(specID)
	if handle == nil {
		return fmt.Errorf("spec actor not found")
	}

	if _, err := handle.SendCommand(core.AppendTranscriptCommand{
		Sender:  "human",
		Content: trimmed,
	}); err != nil {
		return fmt.Errorf("append transcript: %w", err)
	}
	return nil
}

// importProjectSpecFromContent imports structured spec data from freeform
// content using the spec importer. If no LLM client is configured, it falls
// back to transcript-only seeding.
func (s *Server) importProjectSpecFromContent(projectID, content, sourceHint string) error {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return nil
	}

	if err := s.seedProjectSpecFromPrompt(projectID, trimmed); err != nil {
		return err
	}

	if s.specState.LLMClient == nil {
		return nil
	}

	p, ok := s.store.Get(projectID)
	if !ok {
		return fmt.Errorf("project %s not found", projectID)
	}
	specIDStr, err := s.ensureSpecActor(projectID, p.SpecID)
	if err != nil {
		return fmt.Errorf("ensure spec actor: %w", err)
	}
	specID, err := ulid.Parse(specIDStr)
	if err != nil {
		return fmt.Errorf("parse spec ID: %w", err)
	}
	handle := s.specState.GetActor(specID)
	if handle == nil {
		return fmt.Errorf("spec actor not found")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	result, err := specagents.ParseWithLLM(ctx, trimmed, sourceHint, s.specState.LLMClient, s.specState.LLMModel)
	if err != nil {
		return fmt.Errorf("import parse failed: %w", err)
	}

	update := core.UpdateSpecCoreCommand{}
	hasUpdate := false
	if t := strings.TrimSpace(result.Spec.Title); t != "" {
		update.Title = &t
		hasUpdate = true
	}
	if l := strings.TrimSpace(result.Spec.OneLiner); l != "" {
		update.OneLiner = &l
		hasUpdate = true
	}
	if g := strings.TrimSpace(result.Spec.Goal); g != "" {
		update.Goal = &g
		hasUpdate = true
	}
	if result.Update != nil {
		if result.Update.Description != nil && strings.TrimSpace(*result.Update.Description) != "" {
			v := strings.TrimSpace(*result.Update.Description)
			update.Description = &v
			hasUpdate = true
		}
		if result.Update.Constraints != nil && strings.TrimSpace(*result.Update.Constraints) != "" {
			v := strings.TrimSpace(*result.Update.Constraints)
			update.Constraints = &v
			hasUpdate = true
		}
		if result.Update.SuccessCriteria != nil && strings.TrimSpace(*result.Update.SuccessCriteria) != "" {
			v := strings.TrimSpace(*result.Update.SuccessCriteria)
			update.SuccessCriteria = &v
			hasUpdate = true
		}
		if result.Update.Risks != nil && strings.TrimSpace(*result.Update.Risks) != "" {
			v := strings.TrimSpace(*result.Update.Risks)
			update.Risks = &v
			hasUpdate = true
		}
		if result.Update.Notes != nil && strings.TrimSpace(*result.Update.Notes) != "" {
			v := strings.TrimSpace(*result.Update.Notes)
			update.Notes = &v
			hasUpdate = true
		}
	}

	if hasUpdate {
		if _, err := handle.SendCommand(update); err != nil {
			return fmt.Errorf("import update core: %w", err)
		}
	}

	for _, c := range result.Cards {
		title := strings.TrimSpace(c.Title)
		cardType := strings.TrimSpace(c.CardType)
		if title == "" || cardType == "" {
			continue
		}
		var body *string
		if c.Body != nil && strings.TrimSpace(*c.Body) != "" {
			v := strings.TrimSpace(*c.Body)
			body = &v
		}
		var lane *string
		if c.Lane != nil && strings.TrimSpace(*c.Lane) != "" {
			v := strings.TrimSpace(*c.Lane)
			lane = &v
		}
		if _, err := handle.SendCommand(core.CreateCardCommand{
			CardType:  cardType,
			Title:     title,
			Body:      body,
			Lane:      lane,
			CreatedBy: "import",
		}); err != nil {
			return fmt.Errorf("import create card: %w", err)
		}
	}

	return nil
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
			wrapDocument:   r.Method == http.MethodGet && r.Header.Get("HX-Request") == "",
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
	buf          bytes.Buffer
	specID       string
	projectID    string
	code         int
	passthrough  bool // true once we know this is not HTML
	decided      bool // true once we've checked Content-Type
	finished     bool // true after finish() has been called
	wrapDocument bool
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
		if rw.wrapDocument && !strings.Contains(strings.ToLower(bodyStr), "<html") {
			bodyStr = wrapSpecHTML(bodyStr, rw.projectID)
		}
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

func wrapSpecHTML(inner, projectID string) string {
	safeProjectID := html.EscapeString(projectID)
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>mammoth spec</title>
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=DM+Sans:ital,opsz,wght@0,9..40,300;0,9..40,400;0,9..40,500;0,9..40,600;1,9..40,400&family=DM+Serif+Display&display=swap" rel="stylesheet">
<link rel="stylesheet" href="/static/css/tokens.css">
<link rel="stylesheet" href="/static/css/base.css">
<link rel="stylesheet" href="/spec-static/style.css">
<style>
body{margin:0}
.web-rail-links{
    padding:0 12px;display:flex;flex-direction:column;gap:4px
}
.web-rail-link{
    display:block;border-radius:var(--radius-xl);padding:10px 12px;font-size:13px;
    color:var(--text-primary);text-decoration:none
}
.web-rail-link:hover{background:var(--bg-secondary)}
.web-rail-link.active{background:var(--text-primary);color:var(--bg-card)}
.spec-shell-top{
    display:flex;align-items:center;justify-content:space-between;gap:12px;
    padding:12px 20px;background:var(--bg-card);border-bottom:1px solid var(--border)
}
.spec-shell-brand{
    font-family:var(--font-display);font-size:18px;color:var(--text-primary);
    letter-spacing:.01em;text-decoration:none
}
.spec-shell-link{
    display:inline-flex;align-items:center;gap:6px;border-radius:999px;
    border:1px solid var(--border);padding:7px 12px;background:var(--bg-secondary);
    color:var(--text-primary);text-decoration:none;font-size:12px;font-weight:700
}
.spec-shell-link:hover{background:var(--accent-hover)}
.spec-shell-next,.spec-shell-opts{
    display:flex;align-items:center;justify-content:space-between;gap:10px;flex-wrap:wrap;
    padding:8px 20px;border-bottom:1px solid var(--border);background:var(--bg-card)
}
.spec-shell-next-label,.spec-shell-opts-title{
    font-size:11px;letter-spacing:.08em;text-transform:uppercase;color:var(--text-muted);font-weight:700
}
.spec-shell-next-action,.spec-shell-opt-save{
    display:inline-flex;align-items:center;gap:6px;border-radius:999px;
    border:1px solid var(--border);padding:6px 11px;background:var(--bg-secondary);
    color:var(--text-primary);font-size:11px;font-weight:700;text-decoration:none;cursor:pointer
}
.spec-shell-next-action:hover,.spec-shell-opt-save:hover{background:var(--accent-hover)}
.spec-shell-opt{display:inline-flex;align-items:center;gap:6px;font-size:12px;color:var(--text-primary)}
.spec-shell-opt-status{font-size:11px;color:var(--text-muted);min-height:14px}
#canvas.spec-canvas-loading{opacity:.45;transform:translateY(2px);transition:all .2s ease}
#canvas.spec-canvas-enter{animation:specCanvasIn .22s ease}
.spec-lane-empty{margin:8px 0 0 0;border:1px dashed #cddff2;border-radius:12px;background:#f8fbff;padding:10px}
.spec-lane-empty-title{margin:0;font-size:12px;font-weight:700;color:#334155}
.spec-lane-empty-hint{margin:4px 0 0 0;font-size:11px;color:#64748b}
@keyframes specCanvasIn{from{opacity:.45;transform:translateY(2px)}to{opacity:1;transform:translateY(0)}}
</style>
<meta name="htmx-config" content='{"allowScriptTags":true}'>
<script src="https://unpkg.com/htmx.org@2.0.4"></script>
<script src="https://unpkg.com/htmx-ext-sse@2.2.2/sse.js"></script>
<script src="https://cdn.jsdelivr.net/npm/sortablejs@1.15.6/Sortable.min.js"></script>
<script src="https://cdn.jsdelivr.net/npm/@viz-js/viz@3.11.0/lib/viz-standalone.js"></script>
</head>
<body>
<div class="app-layout">
<nav class="nav-rail">
    <div class="rail-header">
        <span>mammoth</span>
    </div>
    <div class="web-rail-links">
        <a href="/" class="web-rail-link">Home</a>
        <a href="/projects/new" class="web-rail-link">Start Project</a>
        <a href="/projects/%[1]s" class="web-rail-link">Project Overview</a>
        <a href="/projects/%[1]s/spec" class="web-rail-link active">Spec</a>
        <a href="/projects/%[1]s/editor" class="web-rail-link">Edit</a>
        <a href="/projects/%[1]s/build" class="web-rail-link">Build</a>
    </div>
    <div class="rail-footer"></div>
</nav>
<div class="workspace">
<div class="spec-shell-top">
    <a href="/projects/%[1]s/spec" class="spec-shell-brand">mammoth spec</a>
    <a href="/projects/%[1]s" class="spec-shell-link">&larr; Project</a>
</div>
<div class="spec-shell-next">
    <span class="spec-shell-next-label">Suggested next step</span>
    <a id="spec-shell-next-action" class="spec-shell-next-action" href="/projects/%[1]s">Open project overview</a>
</div>
<div class="spec-shell-opts">
    <span class="spec-shell-opts-title">Pipeline Options</span>
    <label class="spec-shell-opt"><input id="spec-opt-human" type="checkbox"> Human review</label>
    <label class="spec-shell-opt"><input id="spec-opt-scenario" type="checkbox"> Scenario testing</label>
    <label class="spec-shell-opt"><input id="spec-opt-tdd" type="checkbox"> TDD</label>
    <button id="spec-opt-save" type="button" class="spec-shell-opt-save">Save options</button>
    <span id="spec-opt-status" class="spec-shell-opt-status"></span>
</div>
%[2]s
</div>
</div>
<script>(function(){
var pid='%[1]s';var el=document.getElementById('spec-shell-next-action');if(!el)return;
var humanEl=document.getElementById('spec-opt-human');var scenEl=document.getElementById('spec-opt-scenario');var tddEl=document.getElementById('spec-opt-tdd');var saveBtn=document.getElementById('spec-opt-save');var statusEl=document.getElementById('spec-opt-status');
function setAction(text,href,method,nextURL){el.textContent=text;el.setAttribute('href',href||'#');if(method==='POST'){el.onclick=function(e){e.preventDefault();fetch(href,{method:'POST'}).then(function(resp){if(resp&&resp.ok){if(nextURL){window.location.href=nextURL;return;}window.location.reload();return;}window.location.href='/projects/'+pid;}).catch(function(){window.location.href='/projects/'+pid;});};}else{el.onclick=null;}}
function setStatus(msg){if(statusEl){statusEl.textContent=msg||'';}}
function loadOpts(){fetch('/projects/'+pid+'/spec/options',{headers:{'Accept':'application/json'}}).then(function(r){return r.json();}).then(function(o){if(humanEl)humanEl.checked=!!o.human_review;if(scenEl)scenEl.checked=!!o.scenario_testing;if(tddEl)tddEl.checked=!!o.tdd;setStatus('');}).catch(function(){setStatus('Could not load options');});}
if(saveBtn){saveBtn.addEventListener('click',function(){var body='opt_human_review='+(humanEl&&humanEl.checked?'1':'0')+'&opt_scenario_testing='+(scenEl&&scenEl.checked?'1':'0')+'&opt_tdd='+(tddEl&&tddEl.checked?'1':'0');setStatus('Saving...');fetch('/projects/'+pid+'/spec/options',{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body:body}).then(function(r){if(!r.ok)throw new Error('save failed');return r.json();}).then(function(){setStatus('Saved. Regenerate DOT when ready.');}).catch(function(){setStatus('Save failed');});});}
loadOpts();
fetch('/projects/'+pid+'/spec/agents/state',{headers:{'Accept':'application/json'}}).then(function(r){if(!r.ok){throw new Error('state failed');}return r.json();}).then(function(st){
if(st&&st.recommend_start){setAction('Start agents', '/projects/'+pid+'/spec/agents/start','POST','/projects/'+pid+'/spec');return;}
fetch('/projects/'+pid,{headers:{'Accept':'application/json'}}).then(function(r){return r.json();}).then(function(p){if(!p||!p.phase){return;}
if(p.phase==='spec'){setAction('Generate DOT and continue', '/projects/'+pid+'/spec/continue','POST','/projects/'+pid+'/editor');return;}
if(p.phase==='edit'){setAction('Open DOT editor', '/projects/'+pid+'/editor','GET');return;}
if(p.phase==='build'){setAction('Resume build view', '/projects/'+pid+'/build','GET');return;}
if(p.phase==='done'){setAction('Open final report', '/projects/'+pid+'/final','GET');return;}
setAction('Open project overview','/projects/'+pid,'GET');
}).catch(function(){setAction('Open project overview','/projects/'+pid,'GET');});
}).catch(function(){
fetch('/projects/'+pid,{headers:{'Accept':'application/json'}}).then(function(r){return r.json();}).then(function(p){if(!p||!p.phase){return;}
if(p.phase==='spec'){setAction('Generate DOT and continue', '/projects/'+pid+'/spec/continue','POST','/projects/'+pid+'/editor');return;}
if(p.phase==='edit'){setAction('Open DOT editor', '/projects/'+pid+'/editor','GET');return;}
if(p.phase==='build'){setAction('Resume build view', '/projects/'+pid+'/build','GET');return;}
if(p.phase==='done'){setAction('Open final report', '/projects/'+pid+'/final','GET');return;}
setAction('Open project overview','/projects/'+pid,'GET');
}).catch(function(){setAction('Open project overview','/projects/'+pid,'GET');});
});})();
</script>
</body>
</html>`, safeProjectID, inner)
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

	p, ok := s.store.Get(projectID)
	if !ok {
		return ulid.ULID{}, fmt.Errorf("project %s not found", projectID)
	}

	specDir := filepath.Join(s.specState.MammothHome, "specs", specID.String())
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		return ulid.ULID{}, fmt.Errorf("create spec directory: %w", err)
	}

	handle := core.SpawnActor(specID, core.NewSpecState())

	// Initialize core data immediately so opening /spec renders a working view
	// instead of "Spec has no core data."
	title := strings.TrimSpace(p.Name)
	if title == "" {
		title = "Untitled Spec"
	}
	initialEvents, err := handle.SendCommand(core.CreateSpecCommand{
		Title:    title,
		OneLiner: "",
		Goal:     "",
	})
	if err != nil {
		return ulid.ULID{}, fmt.Errorf("initialize spec core: %w", err)
	}
	server.PersistEvents(specDir, initialEvents)

	server.SpawnEventPersister(s.specState, specID, handle)
	s.specState.SetActor(specID, handle)

	p.SpecID = specID.String()
	if err := s.store.Update(p); err != nil {
		return ulid.ULID{}, fmt.Errorf("update project spec ID: %w", err)
	}

	log.Printf("component=web.spec action=initialized project_id=%s spec_id=%s", projectID, specID)
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
				log.Printf("component=web.spec action=recover_skip_malformed_event spec_id=%s err=%v", specID, jsonErr)
				continue
			}
			state.Apply(&evt)
			eventCount++
		}
	}

	handle := core.SpawnActor(specID, state)
	server.SpawnEventPersister(s.specState, specID, handle)
	s.specState.SetActor(specID, handle)

	log.Printf("component=web.spec action=recovered spec_id=%s replayed_events=%d", specID, eventCount)
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
				log.Printf("component=web.spec action=llm_configured provider=%s model=%s fallback=false", p.Name, p.Model)
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
				log.Printf("component=web.spec action=llm_configured provider=%s model=%s fallback=true", p.Name, p.Model)
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
			log.Printf("component=web.spec action=create_llm_client_failed provider=gemini model=%s err=%v", model, err)
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
