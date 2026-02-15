// ABOUTME: Tests for the unified mammoth HTTP server and chi router.
// ABOUTME: Covers health, home, project CRUD, build lifecycle, and http.Handler compliance.
package web

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/2389-research/mammoth/spec/core"
	specserver "github.com/2389-research/mammoth/spec/server"
)

func TestServerHealth(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("expected status %q, got %q", "ok", body["status"])
	}
}

func TestServerHome(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "mammoth") {
		t.Errorf("expected body to contain %q, got %q", "mammoth", body)
	}
}

func TestServerProjectCreate(t *testing.T) {
	srv := newTestServer(t)

	form := url.Values{"prompt": {"Build a feedback triage pipeline"}}
	req := httptest.NewRequest(http.MethodPost, "/projects", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", rec.Code)
	}

	loc := rec.Header().Get("Location")
	if loc == "" {
		t.Fatal("expected Location header on redirect")
	}
	if !strings.HasPrefix(loc, "/projects/") {
		t.Errorf("expected Location to start with /projects/, got %q", loc)
	}

	// Verify the project was actually created in the store.
	projects := srv.store.List()
	if len(projects) != 1 {
		t.Fatalf("expected 1 project in store, got %d", len(projects))
	}
	if !strings.Contains(projects[0].Name, "Build a feedback triage pipeline") {
		t.Errorf("expected prompt-derived project name, got %q", projects[0].Name)
	}
	if projects[0].SpecID == "" {
		t.Errorf("expected spec to be initialized from prompt")
	}
}

func TestServerProjectCreateDotMode(t *testing.T) {
	srv := newTestServer(t)

	form := url.Values{
		"name": {"dot-project"},
		"mode": {"dot"},
		"dot":  {"digraph x { start -> done }"},
	}
	req := httptest.NewRequest(http.MethodPost, "/projects", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", rec.Code)
	}

	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/projects/") {
		t.Fatalf("expected redirect to project, got %q", loc)
	}

	projectID := strings.TrimPrefix(loc, "/projects/")
	p, ok := srv.store.Get(projectID)
	if !ok {
		t.Fatalf("project %q not found", projectID)
	}
	if p.Phase != PhaseEdit {
		t.Fatalf("expected phase %q, got %q", PhaseEdit, p.Phase)
	}
	if p.DOT != "digraph x { start -> done }" {
		t.Fatalf("unexpected DOT value: %q", p.DOT)
	}
}

func TestServerProjectCreateUploadedDOT(t *testing.T) {
	srv := newTestServer(t)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("prompt", ""); err != nil {
		t.Fatalf("write prompt field: %v", err)
	}
	part, err := writer.CreateFormFile("import_file", "flow.dot")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write([]byte("digraph x { start -> done }")); err != nil {
		t.Fatalf("write file content: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/projects", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", rec.Code)
	}

	loc := rec.Header().Get("Location")
	projectID := strings.TrimPrefix(loc, "/projects/")
	p, ok := srv.store.Get(projectID)
	if !ok {
		t.Fatalf("project %q not found", projectID)
	}
	if p.Phase != PhaseEdit {
		t.Fatalf("expected phase %q, got %q", PhaseEdit, p.Phase)
	}
	if p.DOT != "digraph x { start -> done }" {
		t.Fatalf("unexpected DOT value: %q", p.DOT)
	}
}

func TestServerProjectList(t *testing.T) {
	srv := newTestServer(t)

	// Seed some projects.
	if _, err := srv.store.Create("alpha"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := srv.store.Create("beta"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/projects", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var projects []Project
	if err := json.NewDecoder(rec.Body).Decode(&projects); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(projects) != 2 {
		t.Errorf("expected 2 projects, got %d", len(projects))
	}
}

func TestServerProjectGet(t *testing.T) {
	srv := newTestServer(t)

	p, err := srv.store.Create("existing-project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/projects/"+p.ID, nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var got Project
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if got.ID != p.ID {
		t.Errorf("expected project ID %q, got %q", p.ID, got.ID)
	}
	if got.Name != "existing-project" {
		t.Errorf("expected project name %q, got %q", "existing-project", got.Name)
	}
}

func TestServerProjectGetNotFound(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/projects/nonexistent-id", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", rec.Code)
	}
}

func TestServerBuildStart(t *testing.T) {
	srv := newTestServer(t)

	p, err := srv.store.Create("build-project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Set valid DOT so the build can actually start.
	p.Phase = PhaseEdit
	p.DOT = `digraph test {
		graph [goal="Test pipeline"]
		start [shape=Mdiamond]
		work [label="Do work", prompt="Execute task"]
		done [shape=Msquare]
		start -> work -> done
	}`
	if err := srv.store.Update(p); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/projects/"+p.ID+"/build/start", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", rec.Code)
	}

	loc := rec.Header().Get("Location")
	expected := "/projects/" + p.ID + "/build"
	if loc != expected {
		t.Errorf("expected Location %q, got %q", expected, loc)
	}
}

func TestServerBuildView(t *testing.T) {
	srv := newTestServer(t)

	p, err := srv.store.Create("build-view-project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/projects/"+p.ID+"/build", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "build") {
		t.Errorf("expected body to contain %q, got %q", "build", body)
	}
}

func TestServerServeHTTP(t *testing.T) {
	srv := newTestServer(t)

	// Verify that *Server satisfies the http.Handler interface.
	var handler http.Handler = srv
	_ = handler

	// Use the server with httptest.Server to prove it works as an http.Handler.
	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
}

func TestServerProjectEditorRoute(t *testing.T) {
	srv := newTestServer(t)

	p, err := srv.store.Create("editor-project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	p.DOT = `digraph x { start -> done }`
	p.Phase = PhaseEdit
	if err := srv.store.Update(p); err != nil {
		t.Fatalf("update project: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/projects/"+p.ID+"/editor", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", rec.Code)
	}

	loc := rec.Header().Get("Location")
	prefix := "/projects/" + p.ID + "/editor/sessions/"
	if !strings.HasPrefix(loc, prefix) {
		t.Fatalf("expected redirect prefix %q, got %q", prefix, loc)
	}

	req2 := httptest.NewRequest(http.MethodGet, loc, nil)
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec2.Code)
	}
	if !strings.Contains(rec2.Body.String(), "mammoth-dot-editor") {
		t.Fatalf("expected editor page content")
	}
	if !strings.Contains(rec2.Body.String(), "Continue to Build") {
		t.Fatalf("expected editor toolbar to include continue action")
	}
	if !strings.Contains(rec2.Body.String(), "/projects/"+p.ID+"/build/start") {
		t.Fatalf("expected editor toolbar to target project build route")
	}
}

func TestServerSpecContinueToEditor(t *testing.T) {
	srv := newTestServer(t)

	p, err := srv.store.Create("spec-continue")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	specID, err := srv.initializeSpec(p.ID)
	if err != nil {
		t.Fatalf("initializeSpec: %v", err)
	}
	handle := srv.specState.GetActor(specID)
	if handle == nil {
		t.Fatal("expected spec actor handle")
	}
	if _, err := handle.SendCommand(core.CreateSpecCommand{
		Title:    "Spec Continue",
		OneLiner: "Build a pipeline",
		Goal:     "Generate DOT and edit it",
	}); err != nil {
		t.Fatalf("create spec command: %v", err)
	}
	cancelled := false
	srv.specState.SetSwarm(specID, &specserver.SwarmHandle{
		Cancel: func() { cancelled = true },
	})

	req := httptest.NewRequest(http.MethodPost, "/projects/"+p.ID+"/spec/continue", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/projects/"+p.ID+"/editor" {
		t.Fatalf("expected redirect to editor, got %q", got)
	}

	updated, ok := srv.store.Get(p.ID)
	if !ok {
		t.Fatal("expected updated project in store")
	}
	if updated.Phase != PhaseEdit {
		t.Fatalf("expected phase %q, got %q", PhaseEdit, updated.Phase)
	}
	if !strings.HasPrefix(updated.DOT, "digraph") {
		t.Fatalf("expected generated DOT, got %q", updated.DOT)
	}
	if !cancelled {
		t.Fatalf("expected spec swarm to be cancelled on continue")
	}
	if srv.specState.GetSwarm(specID) != nil {
		t.Fatalf("expected spec swarm to be removed after continue")
	}
}

func TestServerStartSpecInitializesCore(t *testing.T) {
	srv := newTestServer(t)

	p, err := srv.store.Create("start-spec-project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/projects/"+p.ID+"/spec", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "Spec has no core data.") {
		t.Fatalf("expected spec to initialize core on first open")
	}
	if !strings.Contains(body, "spec-compositor") {
		t.Fatalf("expected spec compositor HTML")
	}

	updated, ok := srv.store.Get(p.ID)
	if !ok {
		t.Fatalf("project not found after start spec")
	}
	if updated.SpecID == "" {
		t.Fatalf("expected project.SpecID to be set after start spec")
	}
}

// newTestServer creates a Server with a temporary data directory for testing.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	t.Setenv("MAMMOTH_BACKEND", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	cfg := ServerConfig{
		Addr:    "127.0.0.1:0",
		DataDir: t.TempDir(),
	}
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("unexpected error creating server: %v", err)
	}
	return srv
}
