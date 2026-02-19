// ABOUTME: Unified mammoth HTTP server providing the wizard flow for Spec Builder,
// ABOUTME: DOT Editor, and Attractor Pipeline Runner behind a single chi router.
package web

import (
	"bufio"
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
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/2389-research/mammoth/attractor"
	"github.com/2389-research/mammoth/editor"
	"github.com/2389-research/mammoth/llm"
	"github.com/2389-research/mammoth/spec/core"
	"github.com/2389-research/mammoth/spec/server"
	specweb "github.com/2389-research/mammoth/spec/web"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/oklog/ulid/v2"
)

// Server is the unified mammoth HTTP server that provides the wizard flow:
// Spec Builder -> DOT Editor -> Attractor Pipeline Runner.
type Server struct {
	store     *ProjectStore
	templates *TemplateEngine
	router    chi.Router
	addr      string
	workspace Workspace

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

	// dotFixer repairs invalid DOT graphs using an LLM backend.
	// It is injectable for tests.
	dotFixer func(ctx context.Context, p *Project) (string, error)
}

// ServerConfig holds the configuration for the unified web server.
type ServerConfig struct {
	Addr      string    // listen address (default: "127.0.0.1:2389")
	Workspace Workspace // workspace for path resolution
}

// NewServer creates a new Server with the given configuration. It initializes
// the project store and sets up routing.
func NewServer(cfg ServerConfig) (*Server, error) {
	if cfg.Addr == "" {
		cfg.Addr = "127.0.0.1:2389"
	}
	if cfg.Workspace.StateDir == "" {
		return nil, fmt.Errorf("workspace state dir must not be empty")
	}

	store := NewProjectStore(cfg.Workspace.ProjectStoreDir())
	if err := os.MkdirAll(cfg.Workspace.StateDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating state directory: %w", err)
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
	specState := server.NewAppState(cfg.Workspace.StateDir, providerStatus)
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
	editorStore.StartCleanup(15 * time.Minute)

	// Build model options from catalog for editor dropdown.
	catalog := llm.DefaultCatalog()
	var modelOpts []editor.ModelOption
	for _, m := range catalog.ListModels("") {
		modelOpts = append(modelOpts, editor.ModelOption{
			ID:          m.ID,
			DisplayName: m.DisplayName,
			Provider:    m.Provider,
		})
	}
	editorServer := editor.NewServer(editorStore, editorTemplateDir, editorStaticDir, editor.WithModelOptions(modelOpts))

	s := &Server{
		store:        store,
		templates:    tmpl,
		addr:         cfg.Addr,
		workspace:    cfg.Workspace,
		specState:    specState,
		specRenderer: specRenderer,
		editorServer: editorServer,
		editorStore:  editorStore,
		editorByProj: make(map[string]string),
		builds:       make(map[string]*BuildRun),
	}
	s.dotFixer = s.fixDOTWithAgent

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
	r.Use(webRequestLogger)
	r.Use(middleware.Recoverer)

	// Top-level routes
	r.Get("/", s.handleProjectList)
	r.Get("/health", s.handleHealth)

	// Spec builder static assets served from embedded filesystem.
	specStaticFS, err := fs.Sub(specweb.ContentFS, "static")
	if err != nil {
		log.Printf("component=web.server action=init_spec_static_failed err=%v", err)
	} else {
		r.Handle("/spec-static/*", http.StripPrefix("/spec-static/", http.FileServer(http.FS(specStaticFS))))
	}

	// Shared web static assets (tokens, base, layout CSS + viz-render JS).
	webStaticFS, err := fs.Sub(StaticFS, "static")
	if err != nil {
		log.Printf("component=web.server action=init_web_static_failed err=%v", err)
	} else {
		r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(webStaticFS))))
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
			r.Post("/dot/fix", s.handleDOTFix)

			// Build runner phase
			r.Post("/build/start", s.handleBuildStart)
			r.Get("/build", s.handleBuildView)
			r.Get("/build/events", s.handleBuildEvents)
			r.Get("/build/state", s.handleBuildState)
			r.Post("/build/stop", s.handleBuildStop)
			r.Get("/final", s.handleFinalView)
			r.Get("/final/timeline", s.handleFinalTimeline)
			r.Get("/artifacts/list", s.handleArtifactList)
			r.Get("/artifacts/file", s.handleArtifactFile)
		})
	})

	return r
}

// handleHealth returns a JSON health check response.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleProjectList returns all projects as JSON for API clients, or renders
// the project list page as HTML when the browser requests text/html.
func (s *Server) handleProjectList(w http.ResponseWriter, r *http.Request) {
	projects := s.store.List()
	if wantsJSON(r) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(projects)
		return
	}
	ws := s.workspace
	data := PageData{
		Title:     "Projects",
		Projects:  projects,
		Workspace: &ws,
	}
	if err := s.templates.Render(w, "home.html", data); err != nil {
		log.Printf("component=web.server action=render_failed view=home err=%v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
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
		log.Printf("component=web.server action=render_failed view=project_new err=%v", err)
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
	opts := parseSpecBuilderOptions(r.FormValue)

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
			log.Printf("component=web.server action=import_spec_failed project_id=%s err=%v", p.ID, err)
			http.Error(w, "failed to initialize spec from input", http.StatusInternalServerError)
			return
		}
		updated, ok := s.store.Get(p.ID)
		if ok {
			p = updated
		}
	}
	if err := s.applySpecBuilderOptions(p.ID, opts); err != nil {
		log.Printf("component=web.server action=apply_spec_options_failed project_id=%s err=%v", p.ID, err)
	}

	http.Redirect(w, r, "/projects/"+p.ID, http.StatusSeeOther)
}

func (s *Server) applySpecBuilderOptions(projectID string, opts specBuilderOptions) error {
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
		return fmt.Errorf("parse spec id: %w", err)
	}
	handle := s.specState.GetActor(specID)
	if handle == nil {
		return fmt.Errorf("spec actor not found")
	}

	existingConstraints := ""
	handle.ReadState(func(st *core.SpecState) {
		if st != nil && st.Core != nil && st.Core.Constraints != nil {
			existingConstraints = strings.TrimSpace(*st.Core.Constraints)
		}
	})
	constraints := mergeSpecOptionConstraints(existingConstraints, opts)
	constraints = strings.TrimSpace(constraints)
	if constraints == "" {
		return nil
	}

	if _, err := handle.SendCommand(core.UpdateSpecCoreCommand{Constraints: &constraints}); err != nil {
		return fmt.Errorf("update spec constraints: %w", err)
	}
	return nil
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
		Diagnostics: classifyDiagnostics(p.Diagnostics),
	}
	if err := s.templates.Render(w, "project_overview.html", data); err != nil {
		log.Printf("component=web.server action=render_failed view=project_overview project_id=%s err=%v", projectID, err)
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
		log.Printf("component=web.server action=spec_continue_failed project_id=%s err=%v", projectID, err)
		http.Error(w, "failed to export spec to DOT", http.StatusBadRequest)
		return
	}
	s.stopProjectSpecSwarm(p)

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
		log.Printf("component=web.build action=validate_dot_failed project_id=%s err=%v", projectID, err)
		if updateErr := s.store.Update(p); updateErr != nil {
			log.Printf("component=web.build action=update_project_failed project_id=%s phase=edit err=%v", projectID, updateErr)
		}
		http.Redirect(w, r, "/projects/"+projectID, http.StatusSeeOther)
		return
	}

	// Leaving spec/edit for an active build should stop background spec agents.
	s.stopProjectSpecSwarm(p)

	// Generate a run ID and set up run state.
	runID, err := attractor.GenerateRunID()
	if err != nil {
		log.Printf("component=web.build action=generate_run_id_failed project_id=%s err=%v", projectID, err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	p.RunID = runID
	p.Diagnostics = nil
	if updateErr := s.store.Update(p); updateErr != nil {
		log.Printf("component=web.build action=update_project_failed project_id=%s phase=build err=%v", projectID, updateErr)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	s.startBuildExecution(projectID, p, runID, false)

	http.Redirect(w, r, "/projects/"+projectID+"/build", http.StatusSeeOther)
}

// stopProjectSpecSwarm stops the project's spec swarm if one is running.
func (s *Server) stopProjectSpecSwarm(p *Project) {
	if p == nil || p.SpecID == "" {
		return
	}
	specID, err := ulid.Parse(p.SpecID)
	if err != nil {
		log.Printf("component=web.spec action=stop_swarm_invalid_spec_id spec_id=%q err=%v", p.SpecID, err)
		return
	}
	s.specState.StopSwarm(specID)
}

// maybeResumeBuild restarts a pending build-phase project after process restart.
// A project is considered pending when it is in build phase, has a run ID, and
// has no terminal diagnostics persisted yet.
func (s *Server) maybeResumeBuild(projectID string, p *Project) {
	if p == nil || p.Phase != PhaseBuild || p.RunID == "" || len(p.Diagnostics) > 0 {
		return
	}
	s.buildsMu.RLock()
	existing, exists := s.builds[projectID]
	s.buildsMu.RUnlock()
	if exists && existing != nil && existing.State != nil && existing.State.Status == "running" {
		return
	}
	log.Printf("component=web.build action=resume_pending project_id=%s run_id=%s", projectID, p.RunID)
	s.startBuildExecution(projectID, p, p.RunID, true)
}

// startBuildExecution creates in-memory run tracking and launches the engine.
// When resumeFromCheckpoint is true, it attempts ResumeFromCheckpoint using the
// run's checkpoint file and falls back to a fresh Run when unavailable.
func (s *Server) startBuildExecution(projectID string, p *Project, runID string, resumeFromCheckpoint bool) {
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

	artifactDir := s.workspace.ArtifactDir(projectID, runID)
	checkpointDir := s.workspace.CheckpointDir(projectID, runID)
	checkpointPath := filepath.Join(checkpointDir, "checkpoint.json")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		log.Printf("component=web.build action=create_artifact_dir_failed project_id=%s run_id=%s err=%v", projectID, runID, err)
	}
	if err := os.MkdirAll(checkpointDir, 0o755); err != nil {
		log.Printf("component=web.build action=create_checkpoint_dir_failed project_id=%s run_id=%s err=%v", projectID, runID, err)
	}
	progressLogDir := s.workspace.ProgressLogDir(projectID, runID)
	var progressLogger *attractor.ProgressLogger
	var progressErr error
	if os.Getenv("MAMMOTH_DISABLE_PROGRESS_LOG") != "1" {
		progressLogger, progressErr = attractor.NewProgressLogger(progressLogDir)
		if progressErr != nil {
			log.Printf("component=web.build action=init_progress_logger_failed project_id=%s run_id=%s err=%v", projectID, runID, progressErr)
		}
	}
	handlers := attractor.DefaultHandlerRegistry()
	configureBuildInterviewer(handlers)
	engine := attractor.NewEngine(attractor.EngineConfig{
		ArtifactDir:        artifactDir,
		RunID:              runID,
		AutoCheckpointPath: checkpointPath,
		Backend:            detectBackendFromEnv(false),
		Handlers:           handlers,
		EventHandler: func(evt attractor.EngineEvent) {
			sseEvt := engineEventToSSE(evt)

			s.buildsMu.Lock()
			if evt.NodeID != "" {
				state.CurrentNode = evt.NodeID
			}
			if evt.Type == attractor.EventStageCompleted {
				state.CompletedNodes = append(state.CompletedNodes, evt.NodeID)
			}
			s.buildsMu.Unlock()
			if progressLogger != nil {
				progressLogger.HandleEvent(evt)
			}

			select {
			case events <- sseEvt:
			default:
				log.Printf("component=web.build action=drop_sse_event project_id=%s run_id=%s reason=channel_full", projectID, runID)
			}
		},
	})

	go func() {
		defer close(events)
		if progressLogger != nil {
			defer progressLogger.Close()
		}
		defer func() {
			if rec := recover(); rec != nil {
				s.buildsMu.Lock()
				completedAt := time.Now()
				state.CompletedAt = &completedAt
				state.Status = "failed"
				state.Error = fmt.Sprintf("panic: %v", rec)
				s.buildsMu.Unlock()
				s.persistBuildOutcome(projectID, state)
				log.Printf("component=web.build action=panic_recovered project_id=%s run_id=%s recovered=%v", projectID, runID, rec)
			}
		}()

		var runErr error
		if resumeFromCheckpoint {
			if _, err := os.Stat(checkpointPath); err == nil {
				graph, parseErr := attractor.Parse(p.DOT)
				if parseErr != nil {
					runErr = fmt.Errorf("parse DOT for resume: %w", parseErr)
				} else {
					_, runErr = engine.ResumeFromCheckpoint(ctx, graph, checkpointPath)
				}
			} else {
				log.Printf("component=web.build action=resume_checkpoint_missing project_id=%s run_id=%s fallback=fresh_run", projectID, runID)
				_, runErr = engine.Run(ctx, p.DOT)
			}
		} else {
			_, runErr = engine.Run(ctx, p.DOT)
		}

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
}

// handleBuildView renders the build progress page for a project.
func (s *Server) handleBuildView(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	p, ok := s.store.Get(projectID)
	if !ok {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}
	s.maybeResumeBuild(projectID, p)

	data := PageData{
		Title:       p.Name + " - Build",
		Project:     p,
		ActivePhase: "build",
	}
	if err := s.templates.Render(w, "build_view.html", data); err != nil {
		log.Printf("component=web.server action=render_failed view=build_view project_id=%s err=%v", projectID, err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}

// handleFinalView renders the post-build summary page with graph and artifacts.
func (s *Server) handleFinalView(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	p, ok := s.store.Get(projectID)
	if !ok {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}

	data := PageData{
		Title:       p.Name + " - Final",
		Project:     p,
		ActivePhase: "done",
	}
	if err := s.templates.Render(w, "final_view.html", data); err != nil {
		log.Printf("component=web.server action=render_failed view=final_view project_id=%s err=%v", projectID, err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}

type finalTimelineStep struct {
	NodeID      string                   `json:"node_id"`
	Status      string                   `json:"status"`
	StartedAt   string                   `json:"started_at,omitempty"`
	CompletedAt string                   `json:"completed_at,omitempty"`
	DurationMS  int64                    `json:"duration_ms,omitempty"`
	Attempt     int                      `json:"attempt,omitempty"`
	Error       string                   `json:"error,omitempty"`
	Operations  []finalTimelineOperation `json:"operations,omitempty"`
}

type finalTimelineOperation struct {
	Timestamp string         `json:"timestamp"`
	Type      string         `json:"type"`
	Summary   string         `json:"summary"`
	Data      map[string]any `json:"data,omitempty"`
}

func (s *Server) handleFinalTimeline(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	p, ok := s.store.Get(projectID)
	if !ok {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}
	if p.RunID == "" {
		writeSpecJSON(w, http.StatusOK, map[string]any{"steps": []finalTimelineStep{}})
		return
	}

	progressPath := filepath.Join(s.workspace.ProgressLogDir(projectID, p.RunID), "progress.ndjson")
	f, err := os.Open(progressPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeSpecJSON(w, http.StatusOK, map[string]any{"steps": []finalTimelineStep{}})
			return
		}
		http.Error(w, "failed to open timeline", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	type progressEntry struct {
		Timestamp string         `json:"timestamp"`
		Type      string         `json:"type"`
		NodeID    string         `json:"node_id"`
		Data      map[string]any `json:"data"`
	}
	var steps []finalTimelineStep
	lastByNode := map[string]int{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var evt progressEntry
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			continue
		}
		switch evt.Type {
		case "stage.started":
			step := finalTimelineStep{
				NodeID:    evt.NodeID,
				Status:    "running",
				StartedAt: evt.Timestamp,
				Attempt:   len(steps) + 1,
			}
			steps = append(steps, step)
			idx := len(steps) - 1
			lastByNode[evt.NodeID] = idx
			appendTimelineOperation(&steps[idx], evt.Timestamp, evt.Type, evt.NodeID, evt.Data)
		case "stage.completed":
			idx, ok := lastByNode[evt.NodeID]
			if !ok {
				steps = append(steps, finalTimelineStep{
					NodeID:      evt.NodeID,
					Status:      "completed",
					CompletedAt: evt.Timestamp,
				})
				appendTimelineOperation(&steps[len(steps)-1], evt.Timestamp, evt.Type, evt.NodeID, evt.Data)
				continue
			}
			steps[idx].Status = "completed"
			steps[idx].CompletedAt = evt.Timestamp
			if started, end := parseRFC3339(steps[idx].StartedAt), parseRFC3339(evt.Timestamp); !started.IsZero() && !end.IsZero() {
				steps[idx].DurationMS = end.Sub(started).Milliseconds()
			}
			appendTimelineOperation(&steps[idx], evt.Timestamp, evt.Type, evt.NodeID, evt.Data)
			delete(lastByNode, evt.NodeID)
		case "stage.failed":
			idx, ok := lastByNode[evt.NodeID]
			if !ok {
				steps = append(steps, finalTimelineStep{
					NodeID:      evt.NodeID,
					Status:      "failed",
					CompletedAt: evt.Timestamp,
					Error:       strFromMap(evt.Data, "reason", "error"),
				})
				appendTimelineOperation(&steps[len(steps)-1], evt.Timestamp, evt.Type, evt.NodeID, evt.Data)
				continue
			}
			steps[idx].Status = "failed"
			steps[idx].CompletedAt = evt.Timestamp
			steps[idx].Error = strFromMap(evt.Data, "reason", "error")
			if started, end := parseRFC3339(steps[idx].StartedAt), parseRFC3339(evt.Timestamp); !started.IsZero() && !end.IsZero() {
				steps[idx].DurationMS = end.Sub(started).Milliseconds()
			}
			appendTimelineOperation(&steps[idx], evt.Timestamp, evt.Type, evt.NodeID, evt.Data)
			delete(lastByNode, evt.NodeID)
		case "stage.retrying":
			idx, ok := lastByNode[evt.NodeID]
			if ok {
				steps[idx].Status = "retrying"
				appendTimelineOperation(&steps[idx], evt.Timestamp, evt.Type, evt.NodeID, evt.Data)
			}
		default:
			if idx, ok := lastByNode[evt.NodeID]; ok {
				appendTimelineOperation(&steps[idx], evt.Timestamp, evt.Type, evt.NodeID, evt.Data)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		http.Error(w, "failed to read timeline", http.StatusInternalServerError)
		return
	}
	writeSpecJSON(w, http.StatusOK, map[string]any{"steps": steps})
}

func appendTimelineOperation(step *finalTimelineStep, ts, typ, nodeID string, data map[string]any) {
	if step == nil {
		return
	}
	op := finalTimelineOperation{
		Timestamp: ts,
		Type:      typ,
		Summary:   timelineOperationSummary(typ, nodeID, data),
		Data:      data,
	}
	step.Operations = append(step.Operations, op)
}

func timelineOperationSummary(typ, nodeID string, data map[string]any) string {
	switch typ {
	case "stage.started":
		if nodeID != "" {
			return "Stage started: " + nodeID
		}
		return "Stage started"
	case "stage.completed":
		if nodeID != "" {
			return "Stage completed: " + nodeID
		}
		return "Stage completed"
	case "stage.failed":
		reason := strFromMap(data, "reason", "error")
		if reason != "" && nodeID != "" {
			return "Stage failed: " + nodeID + " - " + reason
		}
		if reason != "" {
			return "Stage failed: " + reason
		}
		if nodeID != "" {
			return "Stage failed: " + nodeID
		}
		return "Stage failed"
	case "stage.retrying":
		attempt := strFromMap(data, "attempt")
		if nodeID != "" && attempt != "" {
			return "Stage retrying: " + nodeID + " (attempt " + attempt + ")"
		}
		if nodeID != "" {
			return "Stage retrying: " + nodeID
		}
		return "Stage retrying"
	case "agent.tool_call.start":
		tool := strFromMap(data, "tool_name")
		if tool != "" && nodeID != "" {
			return "Tool start: " + tool + " @ " + nodeID
		}
		if tool != "" {
			return "Tool start: " + tool
		}
		return "Tool start"
	case "agent.tool_call.end":
		tool := strFromMap(data, "tool_name")
		dur := strFromMap(data, "duration_ms")
		if tool != "" && dur != "" {
			return "Tool done: " + tool + " (" + dur + "ms)"
		}
		if tool != "" {
			return "Tool done: " + tool
		}
		return "Tool done"
	case "agent.llm_turn":
		total := strFromMap(data, "total_tokens")
		if total != "" {
			return "LLM turn: " + total + " tokens"
		}
		return "LLM turn"
	case "checkpoint.saved":
		return "Checkpoint saved"
	default:
		return typ
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
	s.maybeResumeBuild(projectID, p)

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
				// Build phase can outlive in-memory run state after restarts.
				// Without an active run object, report idle to avoid phantom running UI.
				resp.Status = "idle"
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
		log.Printf("component=web.build action=persist_outcome_failed project_id=%s run_id=%s err=%v", projectID, runState.ID, err)
	}
}

func (s *Server) handleArtifactList(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	p, ok := s.store.Get(projectID)
	if !ok {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}
	if p.RunID == "" {
		writeSpecJSON(w, http.StatusOK, map[string]any{
			"base_path": "",
			"dir":       "",
			"entries":   []map[string]any{},
			"files":     []string{},
		})
		return
	}

	baseDir := s.workspace.ArtifactDir(projectID, p.RunID)
	dirParam := strings.TrimSpace(r.URL.Query().Get("dir"))
	dirParam = strings.TrimPrefix(filepath.ToSlash(path.Clean("/"+dirParam)), "/")
	if dirParam == "." {
		dirParam = ""
	}
	targetDir := filepath.Join(baseDir, filepath.FromSlash(dirParam))

	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		http.Error(w, "invalid artifact base", http.StatusInternalServerError)
		return
	}
	absTarget, err := filepath.Abs(targetDir)
	if err != nil {
		http.Error(w, "invalid artifact directory", http.StatusBadRequest)
		return
	}
	if absTarget != absBase && !strings.HasPrefix(absTarget, absBase+string(filepath.Separator)) {
		http.Error(w, "invalid directory", http.StatusBadRequest)
		return
	}

	dirEntries, err := os.ReadDir(absTarget)
	if err != nil {
		if os.IsNotExist(err) {
			writeSpecJSON(w, http.StatusOK, map[string]any{
				"base_path": absBase,
				"dir":       dirParam,
				"entries":   []map[string]any{},
				"files":     []string{},
			})
			return
		}
		http.Error(w, "failed to list artifacts", http.StatusInternalServerError)
		return
	}

	type entryRow struct {
		Name  string
		Path  string
		IsDir bool
		Size  int64
	}
	rows := make([]entryRow, 0, len(dirEntries))
	for _, ent := range dirEntries {
		name := ent.Name()
		rel := filepath.ToSlash(filepath.Join(dirParam, name))
		if rel == "" || strings.HasPrefix(rel, "..") {
			continue
		}
		info, infoErr := ent.Info()
		if infoErr != nil {
			continue
		}
		rows = append(rows, entryRow{
			Name:  name,
			Path:  rel,
			IsDir: ent.IsDir(),
			Size:  info.Size(),
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].IsDir != rows[j].IsDir {
			return rows[i].IsDir
		}
		return rows[i].Name < rows[j].Name
	})

	entries := make([]map[string]any, 0, len(rows))
	files := make([]string, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, map[string]any{
			"name":   row.Name,
			"path":   row.Path,
			"is_dir": row.IsDir,
			"size":   row.Size,
		})
		if !row.IsDir {
			files = append(files, row.Path)
		}
	}

	writeSpecJSON(w, http.StatusOK, map[string]any{
		"base_path": absBase,
		"dir":       dirParam,
		"entries":   entries,
		"files":     files,
	})
}

func (s *Server) handleArtifactFile(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	p, ok := s.store.Get(projectID)
	if !ok {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}
	if p.RunID == "" {
		http.Error(w, "no run artifacts", http.StatusNotFound)
		return
	}

	relPath := strings.TrimSpace(r.URL.Query().Get("path"))
	if relPath == "" {
		http.Error(w, "missing path", http.StatusBadRequest)
		return
	}
	relPath = strings.TrimPrefix(filepath.ToSlash(path.Clean("/"+relPath)), "/")
	if relPath == "" || strings.HasPrefix(relPath, "..") {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	baseDir := s.workspace.ArtifactDir(projectID, p.RunID)
	filePath := filepath.Join(baseDir, filepath.FromSlash(relPath))
	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		http.Error(w, "invalid artifact base", http.StatusInternalServerError)
		return
	}
	absFile, err := filepath.Abs(filePath)
	if err != nil {
		http.Error(w, "invalid artifact file", http.StatusBadRequest)
		return
	}
	if absFile != absBase && !strings.HasPrefix(absFile, absBase+string(filepath.Separator)) {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	info, err := os.Stat(absFile)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "artifact not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to stat artifact", http.StatusInternalServerError)
		return
	}
	if info.IsDir() {
		http.Error(w, "path is a directory", http.StatusBadRequest)
		return
	}
	if info.Size() > 2<<20 {
		http.Error(w, "artifact too large to display (>2MB)", http.StatusRequestEntityTooLarge)
		return
	}

	content, err := os.ReadFile(absFile)
	if err != nil {
		http.Error(w, "failed to read artifact", http.StatusInternalServerError)
		return
	}

	writeSpecJSON(w, http.StatusOK, map[string]any{
		"path":    relPath,
		"content": string(content),
	})
}

func parseRFC3339(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

func strFromMap(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if m == nil {
			return ""
		}
		if v, ok := m[key]; ok {
			switch s := v.(type) {
			case string:
				return s
			default:
				return fmt.Sprintf("%v", s)
			}
		}
	}
	return ""
}
