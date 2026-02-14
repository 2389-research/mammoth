// ABOUTME: Unified mammoth HTTP server providing the wizard flow for Spec Builder,
// ABOUTME: DOT Editor, and Attractor Pipeline Runner behind a single chi router.
package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/2389-research/mammoth/attractor"
	"github.com/2389-research/mammoth/editor"
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

	// specInitMu serializes lazy spec initialization and recovery to prevent
	// duplicate actors when concurrent requests hit the same uninitialized project.
	specInitMu sync.Mutex

	// Editor server and project->session mapping for the unified /projects/{id}/editor flow.
	editorServer *editor.Server
	editorStore  *editor.Store
	editorMu     sync.Mutex
	editorByProj map[string]string

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
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating data directory: %w", err)
	}
	if err := store.LoadAll(); err != nil {
		return nil, fmt.Errorf("loading projects: %w", err)
	}

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

	editorTemplateDir, editorStaticDir, err := resolveEditorAssetDirs()
	if err != nil {
		return nil, fmt.Errorf("resolving editor assets: %w", err)
	}
	editorStore := editor.NewStore(200, 24*time.Hour)
	editorServer := editor.NewServer(editorStore, editorTemplateDir, editorStaticDir)

	s := &Server{
		store:        store,
		templates:    tmpl,
		addr:         cfg.Addr,
		dataDir:      cfg.DataDir,
		specState:    specState,
		specRenderer: specRenderer,
		editorServer: editorServer,
		editorStore:  editorStore,
		editorByProj: make(map[string]string),
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
			r.Post("/spec/continue", s.handleSpecContinueToEditor)

			// DOT editor phase (delegates to editor handlers)
			r.Route("/editor", s.editorRouter)

			// Build runner phase
			r.Post("/build/start", s.handleBuildStart)
			r.Get("/build", s.handleBuildView)
			r.Get("/build/events", s.handleBuildEvents)
			r.Get("/build/state", s.handleBuildState)
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

// handleProjectCreate creates a new project from a prompt or uploaded file.
// DOT uploads go directly to edit mode; all other content seeds the spec transcript.
func (s *Server) handleProjectCreate(w http.ResponseWriter, r *http.Request) {
	// Cap request body at 1MB to prevent oversized payloads.
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	contentType := r.Header.Get("Content-Type")
	var parseErr error
	if strings.HasPrefix(contentType, "multipart/form-data") {
		parseErr = r.ParseMultipartForm(1 << 20)
	} else {
		parseErr = r.ParseForm()
	}
	if parseErr != nil {
		if isMaxBytesError(parseErr) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	prompt := strings.TrimSpace(r.FormValue("prompt"))
	dotSrc := strings.TrimSpace(r.FormValue("dot")) // backward compatibility for existing clients
	legacyName := strings.TrimSpace(r.FormValue("name"))

	fileName := ""
	fileContent := ""
	if f, h, err := r.FormFile("import_file"); err == nil {
		defer f.Close()
		b, readErr := io.ReadAll(f)
		if readErr != nil {
			http.Error(w, "failed to read upload", http.StatusBadRequest)
			return
		}
		fileName = h.Filename
		fileContent = strings.TrimSpace(string(b))
	}

	if prompt == "" && fileContent == "" && dotSrc == "" && legacyName == "" {
		http.Error(w, "provide a spec prompt or upload a file", http.StatusBadRequest)
		return
	}

	name := legacyName
	if name == "" {
		name = projectNameFromInputs(prompt, fileName, dotSrc)
	}
	p, err := s.store.Create(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	uploadedDot := dotSrc
	if uploadedDot == "" && isDOTFile(fileName, fileContent) {
		uploadedDot = fileContent
	}

	if strings.TrimSpace(uploadedDot) != "" {
		p.DOT = uploadedDot
		p.Phase = PhaseEdit
		if err := s.store.Update(p); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/projects/"+p.ID, http.StatusSeeOther)
		return
	}

	seedText := prompt
	if seedText == "" {
		seedText = fileContent
	}
	if seedText != "" {
		sourceHint := sourceHintFromFilename(fileName)
		if err := s.importProjectSpecFromContent(p.ID, seedText, sourceHint); err != nil {
			log.Printf("failed to import/seed spec content: %v", err)
			http.Error(w, "failed to initialize spec from input", http.StatusInternalServerError)
			return
		}
		updated, ok := s.store.Get(p.ID)
		if ok {
			p = updated
		}
	}

	http.Redirect(w, r, "/projects/"+p.ID, http.StatusSeeOther)
}

func projectNameFromInputs(prompt, fileName, dot string) string {
	if fileName != "" {
		base := strings.TrimSpace(path.Base(fileName))
		ext := path.Ext(base)
		name := strings.TrimSpace(strings.TrimSuffix(base, ext))
		if name != "" {
			return truncateProjectName(name)
		}
	}
	if prompt != "" {
		name := firstLine(prompt)
		if name != "" {
			return truncateProjectName(name)
		}
	}
	if dot != "" {
		return "Imported DOT"
	}
	return "Untitled Project"
}

func truncateProjectName(name string) string {
	r := []rune(strings.TrimSpace(name))
	if len(r) == 0 {
		return "Untitled Project"
	}
	if len(r) <= 48 {
		return string(r)
	}
	return string(r[:48])
}

func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func isDOTFile(fileName, content string) bool {
	ext := strings.ToLower(path.Ext(fileName))
	if ext == ".dot" || ext == ".gv" {
		return true
	}
	c := strings.ToLower(content)
	return strings.Contains(c, "digraph ") || strings.HasPrefix(c, "digraph")
}

func sourceHintFromFilename(fileName string) string {
	ext := strings.ToLower(path.Ext(fileName))
	switch ext {
	case ".dot", ".gv":
		return "dot"
	case ".yaml", ".yml":
		return "yaml"
	case ".md", ".markdown":
		return "markdown"
	case ".json":
		return "json"
	case ".txt", ".spec":
		return "text"
	default:
		return ""
	}
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

// handleSpecContinueToEditor exports the current spec state to DOT, stores it
// on the project, and redirects to the project-scoped editor route.
func (s *Server) handleSpecContinueToEditor(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	p, ok := s.store.Get(projectID)
	if !ok {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}

	if err := s.syncProjectFromSpec(projectID, p); err != nil {
		log.Printf("spec continue: project=%s err=%v", projectID, err)
		http.Error(w, "failed to export spec to DOT", http.StatusBadRequest)
		return
	}

	http.Redirect(w, r, projectEditorBasePath(projectID), http.StatusSeeOther)
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

	// Prevent overlapping runs for the same project.
	s.buildsMu.RLock()
	if existing, exists := s.builds[projectID]; exists && existing.State != nil && existing.State.Status == "running" {
		s.buildsMu.RUnlock()
		http.Redirect(w, r, "/projects/"+projectID+"/build", http.StatusSeeOther)
		return
	}
	s.buildsMu.RUnlock()

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
	p.Diagnostics = nil
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
	run.EnsureFanoutStarted()

	s.buildsMu.Lock()
	s.builds[projectID] = run
	s.buildsMu.Unlock()

	// Create the attractor engine and start it in a background goroutine.
	artifactDir := filepath.Join(s.dataDir, projectID, "artifacts", runID)
	engine := attractor.NewEngine(attractor.EngineConfig{
		ArtifactDir: artifactDir,
		RunID:       runID,
		Backend:     detectBackendFromEnv(false),
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
		defer func() {
			if rec := recover(); rec != nil {
				s.buildsMu.Lock()
				completedAt := time.Now()
				state.CompletedAt = &completedAt
				state.Status = "failed"
				state.Error = fmt.Sprintf("panic: %v", rec)
				s.buildsMu.Unlock()
				s.persistBuildOutcome(projectID, state)
				log.Printf("build panic: project=%s run=%s recovered=%v", projectID, runID, rec)
			}
		}()

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
		s.persistBuildOutcome(projectID, state)
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
	run.EnsureFanoutStarted()
	history, eventsCh, unsubscribe := run.SubscribeWithHistory()
	defer unsubscribe()

	// Set SSE headers.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, canFlush := w.(http.Flusher)
	for _, evt := range history {
		fmt.Fprint(w, evt.Format())
	}
	if canFlush {
		flusher.Flush()
	}

	// Stream events until the channel is closed or the client disconnects.
	for {
		select {
		case evt, ok := <-eventsCh:
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

func (s *Server) handleBuildState(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	p, ok := s.store.Get(projectID)
	if !ok {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}

	type buildStateResponse struct {
		ProjectID   string     `json:"project_id"`
		RunID       string     `json:"run_id,omitempty"`
		Phase       string     `json:"phase"`
		Active      bool       `json:"active"`
		Status      string     `json:"status"`
		Diagnostics []string   `json:"diagnostics,omitempty"`
		RunState    *RunState  `json:"run_state,omitempty"`
		Recent      []SSEEvent `json:"recent_events,omitempty"`
	}

	resp := buildStateResponse{
		ProjectID:   projectID,
		RunID:       p.RunID,
		Phase:       string(p.Phase),
		Active:      false,
		Status:      "idle",
		Diagnostics: p.Diagnostics,
	}

	s.buildsMu.RLock()
	run, exists := s.builds[projectID]
	if exists && run != nil && run.State != nil {
		stateCopy := *run.State
		resp.Active = stateCopy.Status == "running"
		resp.Status = stateCopy.Status
		resp.RunState = &stateCopy
		resp.Recent = run.HistorySnapshot()
	}
	s.buildsMu.RUnlock()

	if resp.RunState == nil {
		switch p.Phase {
		case PhaseDone:
			resp.Status = "completed"
		case PhaseBuild:
			if len(p.Diagnostics) > 0 {
				resp.Status = "failed"
			} else {
				resp.Status = "running"
			}
		case PhaseEdit:
			resp.Status = "idle"
		default:
			resp.Status = "idle"
		}
	}

	writeSpecJSON(w, http.StatusOK, resp)
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
	var persistState *RunState
	if exists {
		run.Cancel()
		now := time.Now()
		run.State.Status = "cancelled"
		run.State.CompletedAt = &now
		run.State.Error = "build cancelled by user"
		copyState := *run.State
		persistState = &copyState
	}
	s.buildsMu.Unlock()
	if persistState != nil {
		s.persistBuildOutcome(projectID, persistState)
	}

	http.Redirect(w, r, "/projects/"+projectID, http.StatusSeeOther)
}

func (s *Server) persistBuildOutcome(projectID string, runState *RunState) {
	p, ok := s.store.Get(projectID)
	if !ok {
		return
	}

	p.RunID = runState.ID
	switch runState.Status {
	case "completed":
		p.Phase = PhaseDone
		p.Diagnostics = nil
	case "cancelled":
		p.Phase = PhaseBuild
		p.Diagnostics = []string{"Build cancelled."}
	case "failed":
		p.Phase = PhaseBuild
		if runState.Error != "" {
			p.Diagnostics = []string{"Build failed: " + runState.Error}
		} else {
			p.Diagnostics = []string{"Build failed."}
		}
	}

	if err := s.store.Update(p); err != nil {
		log.Printf("persist build outcome: project=%s err=%v", projectID, err)
	}
}
