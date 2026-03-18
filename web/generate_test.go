// ABOUTME: Tests for the embedded meta-pipeline DOT and the pipeline generation handler.
// ABOUTME: Verifies parsing, validation, build creation, HTTP endpoint, and failure preservation.
package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/2389-research/mammoth/dot"
	"github.com/2389-research/mammoth/dot/validator"
)

func TestEmbeddedMetaPipelineParses(t *testing.T) {
	if metaPipelineDOT == "" {
		t.Fatal("metaPipelineDOT is empty")
	}
	g, err := dot.Parse(metaPipelineDOT)
	if err != nil {
		t.Fatalf("embedded meta-pipeline failed to parse: %v", err)
	}
	if g == nil {
		t.Fatal("parsed graph is nil")
	}
}

func TestEmbeddedMetaPipelineValidates(t *testing.T) {
	g, err := dot.Parse(metaPipelineDOT)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	diags := validator.Lint(g)
	for _, d := range diags {
		if d.Severity == "error" {
			t.Errorf("validation error: %s (node=%s)", d.Message, d.NodeID)
		}
	}
}

func TestStartGenerationBuild_CreatesBuildRun(t *testing.T) {
	srv := newTestServer(t)

	p, err := srv.store.Create("gen-build-test")
	if err != nil {
		t.Fatalf("unexpected error creating project: %v", err)
	}

	runID := srv.startGenerationBuild(p.ID, "# Test Spec\nSome spec content")

	if runID == "" {
		t.Fatal("expected non-empty runID from startGenerationBuild")
	}

	// Verify a build run is tracked in memory.
	srv.buildsMu.RLock()
	run, exists := srv.builds[p.ID]
	var runStatus string
	if exists && run != nil && run.State != nil {
		runStatus = run.State.Status
	}
	srv.buildsMu.RUnlock()

	if !exists {
		t.Fatal("expected build run to be tracked on server")
	}
	switch runStatus {
	case "running", "completed", "failed", "cancelled":
		// Build goroutine can transition quickly; assert it entered lifecycle.
	default:
		t.Errorf("expected run status to be a lifecycle state, got %q", runStatus)
	}

	waitForBuildToSettle(t, srv, p.ID, 2*time.Second)
}

func TestHandleGeneratePipeline_Conflict(t *testing.T) {
	srv := newTestServer(t)

	p, err := srv.store.Create("gen-conflict-test")
	if err != nil {
		t.Fatalf("unexpected error creating project: %v", err)
	}

	// Register an actively running build.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan SSEEvent, 10)
	srv.buildsMu.Lock()
	srv.builds[p.ID] = &BuildRun{
		State: &RunState{
			ID:     "existing-run-1",
			Status: "running",
		},
		Events: events,
		Cancel: cancel,
		Ctx:    ctx,
	}
	srv.buildsMu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/projects/"+p.ID+"/spec/generate-pipeline", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected status 409, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleGeneratePipeline_CompletedBuildDoesNotBlock(t *testing.T) {
	srv := newTestServer(t)

	p, err := srv.store.Create("gen-completed-test")
	if err != nil {
		t.Fatalf("unexpected error creating project: %v", err)
	}

	// Register a completed build (should NOT block new generation).
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan SSEEvent, 10)
	completedAt := time.Now()
	srv.buildsMu.Lock()
	srv.builds[p.ID] = &BuildRun{
		State: &RunState{
			ID:          "old-run-1",
			Status:      "completed",
			CompletedAt: &completedAt,
		},
		Events: events,
		Cancel: cancel,
		Ctx:    ctx,
	}
	srv.buildsMu.Unlock()

	// This should NOT return 409 (it will return 400 because no spec is set,
	// but that proves the concurrency guard passed).
	req := httptest.NewRequest(http.MethodPost, "/projects/"+p.ID+"/spec/generate-pipeline", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code == http.StatusConflict {
		t.Fatalf("completed build should not block new generation, got 409")
	}
	// Expect 400 because project has no SpecID.
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 (no spec), got %d; body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleGeneratePipeline_NoSpec(t *testing.T) {
	srv := newTestServer(t)

	p, err := srv.store.Create("gen-no-spec")
	if err != nil {
		t.Fatalf("unexpected error creating project: %v", err)
	}
	// No SpecID set on project.

	req := httptest.NewRequest(http.MethodPost, "/projects/"+p.ID+"/spec/generate-pipeline", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

func TestGenerationPreservesDOTOnFailure(t *testing.T) {
	srv := newTestServer(t)
	p, err := srv.store.Create("test-preserve")
	if err != nil {
		t.Fatalf("unexpected error creating project: %v", err)
	}
	p.DOT = "digraph old { a -> b; }"
	if err := srv.store.Update(p); err != nil {
		t.Fatalf("unexpected error updating project: %v", err)
	}

	// Start generation with empty spec — will fail quickly without LLM.
	runID := srv.startGenerationBuild(p.ID, "")
	if runID == "" {
		t.Fatal("expected run ID")
	}

	// Wait for build to complete via subscription.
	srv.buildsMu.RLock()
	run := srv.builds[p.ID]
	srv.buildsMu.RUnlock()
	if run != nil {
		ch, unsub := run.Subscribe()
		defer unsub()
		timeout := time.After(10 * time.Second)
		for done := false; !done; {
			select {
			case _, ok := <-ch:
				if !ok {
					done = true
				}
			case <-timeout:
				t.Fatal("timed out waiting for build to complete")
			}
		}
	}

	// Verify old DOT is preserved (generation failed, so DOT should not change).
	updated, ok := srv.store.Get(p.ID)
	if !ok {
		t.Fatal("project not found after generation")
	}
	if updated.DOT != "digraph old { a -> b; }" {
		t.Errorf("expected old DOT preserved, got %q", updated.DOT)
	}
}

func TestHandleGeneratePipeline_ProjectNotFound(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/projects/nonexistent/spec/generate-pipeline", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d; body: %s", rec.Code, rec.Body.String())
	}
}
