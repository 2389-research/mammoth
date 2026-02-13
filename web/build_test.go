// ABOUTME: Tests for the editor-to-build transition wiring, SSE event streaming, and build lifecycle.
// ABOUTME: Covers build start with valid/invalid/empty DOT, build view rendering, SSE headers, and build stop.
package web

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// validTestDOT is a minimal valid DOT pipeline with start and exit nodes.
const validTestDOT = `digraph test {
	graph [goal="Test pipeline"]
	start [shape=Mdiamond]
	work [label="Do work", prompt="Execute task"]
	done [shape=Msquare]
	start -> work -> done
}`

func TestBuildStartValidDOT(t *testing.T) {
	srv := newTestServer(t)

	// Create a project with valid DOT in the edit phase.
	p, err := srv.store.Create("build-test")
	if err != nil {
		t.Fatalf("unexpected error creating project: %v", err)
	}
	p.Phase = PhaseEdit
	p.DOT = validTestDOT
	if err := srv.store.Update(p); err != nil {
		t.Fatalf("unexpected error updating project: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/projects/"+p.ID+"/build/start", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	// Should redirect to the build view.
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d; body: %s", rec.Code, rec.Body.String())
	}

	loc := rec.Header().Get("Location")
	expected := "/projects/" + p.ID + "/build"
	if loc != expected {
		t.Errorf("expected Location %q, got %q", expected, loc)
	}

	// Project should now be in build phase.
	updated, ok := srv.store.Get(p.ID)
	if !ok {
		t.Fatal("project not found after build start")
	}
	if updated.Phase != PhaseBuild {
		t.Errorf("expected phase %q, got %q", PhaseBuild, updated.Phase)
	}

	// RunID should be set.
	if updated.RunID == "" {
		t.Error("expected RunID to be set after build start")
	}

	// A build run should be tracked on the server.
	srv.buildsMu.RLock()
	run, exists := srv.builds[p.ID]
	srv.buildsMu.RUnlock()
	if !exists {
		t.Fatal("expected build run to be tracked on server")
	}
	if run.State.Status != "running" {
		t.Errorf("expected run status %q, got %q", "running", run.State.Status)
	}
}

func TestBuildStartEmptyDOT(t *testing.T) {
	srv := newTestServer(t)

	// Create a project with no DOT.
	p, err := srv.store.Create("empty-dot")
	if err != nil {
		t.Fatalf("unexpected error creating project: %v", err)
	}
	p.Phase = PhaseEdit
	p.DOT = ""
	if err := srv.store.Update(p); err != nil {
		t.Fatalf("unexpected error updating project: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/projects/"+p.ID+"/build/start", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	// Should redirect back (not crash).
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected status 303 redirect, got %d; body: %s", rec.Code, rec.Body.String())
	}

	// Project should stay in edit phase.
	updated, ok := srv.store.Get(p.ID)
	if !ok {
		t.Fatal("project not found after build start with empty DOT")
	}
	if updated.Phase != PhaseEdit {
		t.Errorf("expected phase %q for empty DOT, got %q", PhaseEdit, updated.Phase)
	}
}

func TestBuildStartInvalidDOT(t *testing.T) {
	srv := newTestServer(t)

	// Create a project with invalid DOT.
	p, err := srv.store.Create("invalid-dot")
	if err != nil {
		t.Fatalf("unexpected error creating project: %v", err)
	}
	p.Phase = PhaseEdit
	p.DOT = "this is not valid DOT syntax"
	if err := srv.store.Update(p); err != nil {
		t.Fatalf("unexpected error updating project: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/projects/"+p.ID+"/build/start", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	// Should redirect back (not crash).
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected status 303 redirect, got %d; body: %s", rec.Code, rec.Body.String())
	}

	// Project should stay in edit phase with diagnostics.
	updated, ok := srv.store.Get(p.ID)
	if !ok {
		t.Fatal("project not found after build start with invalid DOT")
	}
	if updated.Phase != PhaseEdit {
		t.Errorf("expected phase %q for invalid DOT, got %q", PhaseEdit, updated.Phase)
	}
	if len(updated.Diagnostics) == 0 {
		t.Error("expected diagnostics to be populated for invalid DOT")
	}
}

func TestBuildStartProjectNotFound(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/projects/nonexistent/build/start", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", rec.Code)
	}
}

func TestBuildView(t *testing.T) {
	srv := newTestServer(t)

	// Create project and start a build so the view has something to show.
	p, err := srv.store.Create("build-view-test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	p.Phase = PhaseBuild
	p.DOT = validTestDOT
	p.RunID = "test-run-123"
	if err := srv.store.Update(p); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/projects/"+p.ID+"/build", nil)
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()

	// Should contain build-related content.
	if !strings.Contains(body, "build") && !strings.Contains(body, "Build") {
		t.Errorf("expected body to contain build content, got: %s", body[:min(200, len(body))])
	}

	// Should contain the project name.
	if !strings.Contains(body, "build-view-test") {
		t.Errorf("expected body to contain project name")
	}
}

func TestBuildViewProjectNotFound(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/projects/nonexistent/build", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", rec.Code)
	}
}

func TestBuildEventsSSEHeaders(t *testing.T) {
	srv := newTestServer(t)

	// Create project with an active build run.
	p, err := srv.store.Create("sse-test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	p.Phase = PhaseBuild
	p.DOT = validTestDOT
	p.RunID = "sse-run-1"
	if err := srv.store.Update(p); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Register a build run so the SSE endpoint has something to stream.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan SSEEvent, 10)
	srv.buildsMu.Lock()
	srv.builds[p.ID] = &BuildRun{
		State: &RunState{
			ID:     "sse-run-1",
			Status: "running",
		},
		Events: events,
		Cancel: cancel,
		Ctx:    ctx,
	}
	srv.buildsMu.Unlock()

	// Send an event and close to end the stream.
	events <- SSEEvent{
		Event: "stage.started",
		Data:  `{"node_id":"work"}`,
	}
	close(events)

	req := httptest.NewRequest(http.MethodGet, "/projects/"+p.ID+"/build/events", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	// Verify SSE content type.
	ct := rec.Header().Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("expected Content-Type %q, got %q", "text/event-stream", ct)
	}

	// Verify Cache-Control header for SSE.
	cc := rec.Header().Get("Cache-Control")
	if cc != "no-cache" {
		t.Errorf("expected Cache-Control %q, got %q", "no-cache", cc)
	}

	// Verify the body contains SSE formatted data.
	body := rec.Body.String()
	if !strings.Contains(body, "event: stage.started") {
		t.Errorf("expected SSE event line, got: %s", body)
	}
	if !strings.Contains(body, "data: ") {
		t.Errorf("expected SSE data line, got: %s", body)
	}
}

func TestBuildEventsNoBuild(t *testing.T) {
	srv := newTestServer(t)

	// Create a project but don't start a build.
	p, err := srv.store.Create("no-build-sse")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/projects/"+p.ID+"/build/events", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	// Should return 404 when there's no active build.
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status 404 for no active build, got %d", rec.Code)
	}
}

func TestBuildStop(t *testing.T) {
	srv := newTestServer(t)

	// Create project and register a build run.
	p, err := srv.store.Create("stop-test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	p.Phase = PhaseBuild
	p.RunID = "stop-run-1"
	if err := srv.store.Update(p); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan SSEEvent, 10)
	srv.buildsMu.Lock()
	srv.builds[p.ID] = &BuildRun{
		State: &RunState{
			ID:     "stop-run-1",
			Status: "running",
		},
		Events: events,
		Cancel: cancel,
		Ctx:    ctx,
	}
	srv.buildsMu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/projects/"+p.ID+"/build/stop", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	// Should redirect to the project page.
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", rec.Code)
	}

	loc := rec.Header().Get("Location")
	expected := "/projects/" + p.ID
	if loc != expected {
		t.Errorf("expected Location %q, got %q", expected, loc)
	}

	// Context should be cancelled.
	select {
	case <-ctx.Done():
		// Expected - context was cancelled.
	default:
		t.Error("expected build context to be cancelled after stop")
	}

	// Run state should be updated.
	srv.buildsMu.RLock()
	run, exists := srv.builds[p.ID]
	srv.buildsMu.RUnlock()
	if !exists {
		t.Fatal("expected build run to still exist after stop")
	}
	if run.State.Status != "cancelled" {
		t.Errorf("expected run status %q, got %q", "cancelled", run.State.Status)
	}
}

func TestBuildStopNoActiveBuild(t *testing.T) {
	srv := newTestServer(t)

	// Create project without an active build.
	p, err := srv.store.Create("no-build-stop")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/projects/"+p.ID+"/build/stop", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	// Should still redirect gracefully.
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", rec.Code)
	}
}

func TestBuildStopProjectNotFound(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/projects/nonexistent/build/stop", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", rec.Code)
	}
}

func TestBuildSSEEventFormat(t *testing.T) {
	// Verify SSE event formatting is correct.
	evt := SSEEvent{
		Event: "pipeline.started",
		Data:  `{"workdir":"/tmp/test"}`,
	}

	formatted := evt.Format()
	if !strings.HasPrefix(formatted, "event: pipeline.started\n") {
		t.Errorf("expected event line, got: %s", formatted)
	}
	if !strings.Contains(formatted, "data: {\"workdir\":\"/tmp/test\"}\n\n") {
		t.Errorf("expected data line with trailing double newline, got: %s", formatted)
	}
}

func TestBuildRunStateJSON(t *testing.T) {
	// Verify RunState serializes correctly.
	now := time.Now()
	state := &RunState{
		ID:             "test-123",
		Status:         "running",
		StartedAt:      now,
		CurrentNode:    "work",
		CompletedNodes: []string{"start"},
	}

	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("unexpected error marshaling RunState: %v", err)
	}

	var decoded RunState
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unexpected error unmarshaling RunState: %v", err)
	}

	if decoded.ID != "test-123" {
		t.Errorf("expected ID %q, got %q", "test-123", decoded.ID)
	}
	if decoded.Status != "running" {
		t.Errorf("expected status %q, got %q", "running", decoded.Status)
	}
	if decoded.CurrentNode != "work" {
		t.Errorf("expected CurrentNode %q, got %q", "work", decoded.CurrentNode)
	}
}

func TestBuildEventsStreamMultiple(t *testing.T) {
	srv := newTestServer(t)

	p, err := srv.store.Create("multi-event-test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	p.Phase = PhaseBuild
	p.RunID = "multi-run-1"
	if err := srv.store.Update(p); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan SSEEvent, 10)
	srv.buildsMu.Lock()
	srv.builds[p.ID] = &BuildRun{
		State: &RunState{
			ID:     "multi-run-1",
			Status: "running",
		},
		Events: events,
		Cancel: cancel,
		Ctx:    ctx,
	}
	srv.buildsMu.Unlock()

	// Send multiple events.
	events <- SSEEvent{Event: "pipeline.started", Data: `{}`}
	events <- SSEEvent{Event: "stage.started", Data: `{"node_id":"start"}`}
	events <- SSEEvent{Event: "stage.completed", Data: `{"node_id":"start"}`}
	events <- SSEEvent{Event: "pipeline.completed", Data: `{}`}
	close(events)

	req := httptest.NewRequest(http.MethodGet, "/projects/"+p.ID+"/build/events", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	// Count the SSE events in the body.
	body := rec.Body.String()
	scanner := bufio.NewScanner(strings.NewReader(body))
	eventCount := 0
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			eventCount++
		}
	}

	if eventCount != 4 {
		t.Errorf("expected 4 SSE events, got %d; body:\n%s", eventCount, body)
	}
}
