// ABOUTME: Tests for the unified mammoth HTTP server and chi router.
// ABOUTME: Covers health, home, project CRUD, build lifecycle, and http.Handler compliance.
package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
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

	form := url.Values{"name": {"test-project"}}
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
	if projects[0].Name != "test-project" {
		t.Errorf("expected project name %q, got %q", "test-project", projects[0].Name)
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

// newTestServer creates a Server with a temporary data directory for testing.
func newTestServer(t *testing.T) *Server {
	t.Helper()
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
