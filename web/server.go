// ABOUTME: Unified mammoth HTTP server providing the wizard flow for Spec Builder,
// ABOUTME: DOT Editor, and Attractor Pipeline Runner behind a single chi router.
package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/2389-research/mammoth/attractor"
	"github.com/2389-research/mammoth/spec/server"
	specweb "github.com/2389-research/mammoth/spec/web"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Server is the unified mammoth HTTP server that provides the wizard flow:
// Spec Builder -> DOT Editor -> Attractor Pipeline Runner.
type Server struct {
	store     *ProjectStore
	templates *TemplateEngine
	router    chi.Router
	addr      string
	dataDir   string

	// Spec builder state and renderer, initialized from embedded FS.
	specState    *server.AppState
	specRenderer *specweb.TemplateRenderer

	// buildsMu protects the builds map for concurrent access from handler
	// goroutines and background engine goroutines.
	buildsMu sync.RWMutex
	builds   map[string]*BuildRun
}

// ServerConfig holds the configuration for the unified web server.
type ServerConfig struct {
	Addr    string // listen address (default: "127.0.0.1:2389")
	DataDir string // data directory for projects
}

// NewServer creates a new Server with the given configuration. It initializes
// the project store and sets up routing.
func NewServer(cfg ServerConfig) (*Server, error) {
	if cfg.Addr == "" {
		cfg.Addr = "127.0.0.1:2389"
	}
	if cfg.DataDir == "" {
		return nil, fmt.Errorf("DataDir must not be empty")
	}

	store := NewProjectStore(cfg.DataDir)

	tmpl, err := NewTemplateEngine()
	if err != nil {
		return nil, fmt.Errorf("initializing templates: %w", err)
	}

	// Initialize spec builder state with provider detection.
	providerStatus := server.DetectProviders()
	specState := server.NewAppState(cfg.DataDir, providerStatus)
	setupSpecLLMClient(specState, providerStatus)

	specRenderer, err := specweb.NewTemplateRendererFromFS(specweb.ContentFS)
	if err != nil {
		return nil, fmt.Errorf("initializing spec templates: %w", err)
	}

	s := &Server{
		store:        store,
		templates:    tmpl,
		addr:         cfg.Addr,
		dataDir:      cfg.DataDir,
		specState:    specState,
		specRenderer: specRenderer,
		builds:       make(map[string]*BuildRun),
	}

	s.router = s.buildRouter()
	return s, nil
}

// ServeHTTP delegates to the chi router, satisfying http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

// ListenAndServe starts the HTTP server on the configured address with
// appropriate timeouts to prevent resource exhaustion from slow clients.
func (s *Server) ListenAndServe() error {
	srv := &http.Server{
		Addr:              s.addr,
		Handler:           s,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}
	return srv.ListenAndServe()
}

// buildRouter constructs the chi router with all routes and middleware.
func (s *Server) buildRouter() chi.Router {
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// Top-level routes
	r.Get("/", s.handleHome)
	r.Get("/health", s.handleHealth)

	// Spec builder static assets served from embedded filesystem.
	specStaticFS, err := fs.Sub(specweb.ContentFS, "static")
	if err != nil {
		log.Printf("WARNING: failed to create spec static sub-FS: %v", err)
	} else {
		r.Handle("/spec-static/*", http.StripPrefix("/spec-static/", http.FileServer(http.FS(specStaticFS))))
	}

	// Project routes
	r.Route("/projects", func(r chi.Router) {
		r.Get("/", s.handleProjectList)
		r.Get("/new", s.handleProjectNew)
		r.Post("/", s.handleProjectCreate)

		r.Route("/{projectID}", func(r chi.Router) {
			r.Get("/", s.handleProjectOverview)
			r.Get("/validate", s.handleValidate)

			// Spec builder phase (delegates to spec/web handlers via adapter middleware)
			r.Route("/spec", s.specRouter)

			// DOT editor phase (delegates to editor handlers)
			// r.Mount("/editor", s.editorRouter())  -- stub for now

			// Build runner phase
			r.Post("/build/start", s.handleBuildStart)
			r.Get("/build", s.handleBuildView)
			r.Get("/build/events", s.handleBuildEvents)
			r.Post("/build/stop", s.handleBuildStop)
		})
	})

	return r
}

// handleHome renders the landing page with entry paths and recent projects.
func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	data := PageData{
		Title:    "Home",
		Projects: s.store.List(),
	}
	if err := s.templates.Render(w, "home.html", data); err != nil {
		log.Printf("error rendering home: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}

// handleHealth returns a JSON health check response.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleProjectList returns all projects as a JSON array.
func (s *Server) handleProjectList(w http.ResponseWriter, r *http.Request) {
	projects := s.store.List()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(projects)
}

// handleProjectNew renders the new project form. Supports mode=idea (default) and mode=dot.
func (s *Server) handleProjectNew(w http.ResponseWriter, r *http.Request) {
	mode := r.URL.Query().Get("mode")
	if mode == "" {
		mode = "idea"
	}
	data := PageData{
		Title: "New Project",
		Mode:  mode,
	}
	if err := s.templates.Render(w, "project_new.html", data); err != nil {
		log.Printf("error rendering project_new: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}

// handleProjectCreate parses a project name from the form body, creates the
// project, and redirects to the project overview.
func (s *Server) handleProjectCreate(w http.ResponseWriter, r *http.Request) {
	// Cap request body at 1MB to prevent oversized payloads.
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	if err := r.ParseForm(); err != nil {
		if isMaxBytesError(err) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	name := r.FormValue("name")
	if name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	p, err := s.store.Create(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/projects/"+p.ID, http.StatusSeeOther)
}

// wantsJSON returns true if the request prefers JSON over HTML based on
// the Accept header. Defaults to JSON when no Accept header is set or when
// Accept contains */* (wildcard), to preserve backward compatibility with
// API clients.
func wantsJSON(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	if accept == "" {
		return true
	}
	if strings.Contains(accept, "text/html") {
		return false
	}
	return strings.Contains(accept, "application/json") || strings.Contains(accept, "*/*")
}

// isMaxBytesError reports whether err (or any error in its chain) is an
// *http.MaxBytesError, indicating the request body exceeded the size limit.
func isMaxBytesError(err error) bool {
	var maxErr *http.MaxBytesError
	return errors.As(err, &maxErr)
}

// handleProjectOverview renders the project overview page as HTML, or returns
// JSON if the client requests it via the Accept header (for API compatibility).
func (s *Server) handleProjectOverview(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	p, ok := s.store.Get(projectID)
	if !ok {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}

	// Support JSON for API clients that set Accept: application/json or send no Accept header.
	if wantsJSON(r) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(p)
		return
	}

	data := PageData{
		Title:       p.Name,
		Project:     p,
		ActivePhase: string(p.Phase),
	}
	if err := s.templates.Render(w, "project_overview.html", data); err != nil {
		log.Printf("error rendering project_overview: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}

// handleValidate returns a validation stub for the project.
func (s *Server) handleValidate(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	_, ok := s.store.Get(projectID)
	if !ok {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"valid":       true,
		"diagnostics": []string{},
	})
}

// handleBuildStart validates the project DOT, creates an attractor engine,
// starts it in a background goroutine, and redirects to the build view.
func (s *Server) handleBuildStart(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	p, ok := s.store.Get(projectID)
	if !ok {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}

	// Validate the DOT via the transition logic. If validation fails,
	// the project stays in edit phase with diagnostics populated.
	if err := TransitionEditorToBuild(p); err != nil {
		log.Printf("build start: DOT validation failed for project %s: %v", projectID, err)
		if updateErr := s.store.Update(p); updateErr != nil {
			log.Printf("build start: failed to update project %s: %v", projectID, updateErr)
		}
		http.Redirect(w, r, "/projects/"+projectID, http.StatusSeeOther)
		return
	}

	// Generate a run ID and set up run state.
	runID, err := attractor.GenerateRunID()
	if err != nil {
		log.Printf("build start: failed to generate run ID: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	p.RunID = runID
	if updateErr := s.store.Update(p); updateErr != nil {
		log.Printf("build start: failed to update project %s: %v", projectID, updateErr)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// Create the build run with a cancellable context.
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

	s.buildsMu.Lock()
	s.builds[projectID] = run
	s.buildsMu.Unlock()

	// Create the attractor engine and start it in a background goroutine.
	artifactDir := filepath.Join(s.dataDir, projectID, "artifacts", runID)
	engine := attractor.NewEngine(attractor.EngineConfig{
		ArtifactDir: artifactDir,
		RunID:       runID,
		EventHandler: func(evt attractor.EngineEvent) {
			sseEvt := engineEventToSSE(evt)

			// Update run state based on event type.
			s.buildsMu.Lock()
			if evt.NodeID != "" {
				state.CurrentNode = evt.NodeID
			}
			if evt.Type == attractor.EventStageCompleted {
				state.CompletedNodes = append(state.CompletedNodes, evt.NodeID)
			}
			s.buildsMu.Unlock()

			// Send to channel; drop if channel is full to avoid blocking the engine.
			select {
			case events <- sseEvt:
			default:
				log.Printf("build: dropped SSE event for project %s (channel full)", projectID)
			}
		},
	})

	go func() {
		defer close(events)

		_, runErr := engine.Run(ctx, p.DOT)

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
	}()

	http.Redirect(w, r, "/projects/"+projectID+"/build", http.StatusSeeOther)
}

// handleBuildView renders the build progress page for a project.
func (s *Server) handleBuildView(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	p, ok := s.store.Get(projectID)
	if !ok {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}

	data := PageData{
		Title:       p.Name + " - Build",
		Project:     p,
		ActivePhase: "build",
	}
	if err := s.templates.Render(w, "build_view.html", data); err != nil {
		log.Printf("error rendering build_view: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}

// handleBuildEvents streams server-sent events for an active build.
// It sets the appropriate SSE headers and writes events as they arrive
// from the build's event channel.
func (s *Server) handleBuildEvents(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	if _, ok := s.store.Get(projectID); !ok {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}

	s.buildsMu.RLock()
	run, exists := s.builds[projectID]
	s.buildsMu.RUnlock()

	if !exists {
		http.Error(w, "no active build", http.StatusNotFound)
		return
	}

	// Set SSE headers.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, canFlush := w.(http.Flusher)

	// Stream events until the channel is closed or the client disconnects.
	for {
		select {
		case evt, ok := <-run.Events:
			if !ok {
				// Channel closed; build is done.
				return
			}
			fmt.Fprint(w, evt.Format())
			if canFlush {
				flusher.Flush()
			}
		case <-r.Context().Done():
			// Client disconnected.
			return
		}
	}
}

// handleBuildStop cancels an active build and redirects to the project overview.
func (s *Server) handleBuildStop(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	_, ok := s.store.Get(projectID)
	if !ok {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}

	s.buildsMu.Lock()
	run, exists := s.builds[projectID]
	if exists {
		run.Cancel()
		now := time.Now()
		run.State.Status = "cancelled"
		run.State.CompletedAt = &now
	}
	s.buildsMu.Unlock()

	http.Redirect(w, r, "/projects/"+projectID, http.StatusSeeOther)
}
