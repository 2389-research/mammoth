// ABOUTME: Integration tests exercising the three user flows end-to-end via real HTTP requests.
// ABOUTME: Flow A (Idea->Spec->Edit->Build), Flow B (Upload DOT->Edit->Build), Flow C (Upload DOT->Build).
package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/2389-research/mammoth/spec/core"
)

// noFollowClient returns an *http.Client that does not follow redirects,
// allowing tests to inspect redirect responses directly.
func noFollowClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// createProjectViaHTTP posts a form to /projects and returns the project ID
// extracted from the Location redirect header.
func createProjectViaHTTP(t *testing.T, client *http.Client, baseURL, name string) string {
	t.Helper()

	form := url.Values{"name": {name}}
	resp, err := client.Post(
		baseURL+"/projects",
		"application/x-www-form-urlencoded",
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		t.Fatalf("POST /projects: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST /projects: expected 303, got %d; body: %s", resp.StatusCode, body)
	}

	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/projects/") {
		t.Fatalf("POST /projects: unexpected Location %q", loc)
	}
	return strings.TrimPrefix(loc, "/projects/")
}

// getProjectJSON fetches a project via GET /projects/{id} with Accept: application/json
// and decodes the response into a Project.
func getProjectJSON(t *testing.T, client *http.Client, baseURL, projectID string) Project {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, baseURL+"/projects/"+projectID, nil)
	if err != nil {
		t.Fatalf("GET /projects/%s: %v", projectID, err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /projects/%s: %v", projectID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET /projects/%s: expected 200, got %d; body: %s", projectID, resp.StatusCode, body)
	}

	var p Project
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		t.Fatalf("GET /projects/%s: decode: %v", projectID, err)
	}
	return p
}

// TestIntegrationFlowA exercises the Idea -> Spec -> Edit -> Build user flow.
//
// Steps:
//  1. POST /projects to create a project (starts in spec phase)
//  2. Verify the project is in the spec phase
//  3. Transition spec to editor via TransitionSpecToEditor (simulating spec completion)
//  4. Verify the project is in the edit phase with DOT populated
//  5. POST /projects/{id}/build/start to start the build
//  6. GET /projects/{id}/build to verify the build view renders
func TestIntegrationFlowA(t *testing.T) {
	srv := newTestServer(t)
	ts := httptest.NewServer(srv)
	defer ts.Close()
	client := noFollowClient()

	// Step 1: Create the project via HTTP.
	projectID := createProjectViaHTTP(t, client, ts.URL, "flow-a-idea")

	// Step 2: Verify the project starts in spec phase.
	p := getProjectJSON(t, client, ts.URL, projectID)
	if p.Phase != PhaseSpec {
		t.Fatalf("step 2: expected phase %q, got %q", PhaseSpec, p.Phase)
	}
	if p.Name != "flow-a-idea" {
		t.Fatalf("step 2: expected name %q, got %q", "flow-a-idea", p.Name)
	}

	// Step 3: Build a test spec state and transition to editor.
	// We access the server store directly because the spec HTTP endpoints are stubs.
	specState := makeTestSpecState()
	storeProject, ok := srv.store.Get(projectID)
	if !ok {
		t.Fatal("step 3: project not found in store")
	}

	if err := TransitionSpecToEditor(storeProject, specState); err != nil {
		t.Fatalf("step 3: TransitionSpecToEditor: %v", err)
	}
	if err := srv.store.Update(storeProject); err != nil {
		t.Fatalf("step 3: store.Update: %v", err)
	}

	// Step 4: Verify the project is in edit phase with DOT populated via HTTP.
	p = getProjectJSON(t, client, ts.URL, projectID)
	if p.Phase != PhaseEdit {
		t.Fatalf("step 4: expected phase %q, got %q", PhaseEdit, p.Phase)
	}
	if p.DOT == "" {
		t.Fatal("step 4: expected DOT to be populated")
	}
	if !strings.HasPrefix(p.DOT, "digraph") {
		t.Fatalf("step 4: expected DOT to start with 'digraph', got prefix: %s", p.DOT[:min(30, len(p.DOT))])
	}

	// Step 5: Start the build via HTTP.
	// Defer a stop request so the build goroutine doesn't leak past test teardown.
	defer func() {
		stopResp, stopErr := client.Post(ts.URL+"/projects/"+projectID+"/build/stop", "", nil)
		if stopErr == nil {
			stopResp.Body.Close()
		}
	}()

	resp, err := client.Post(ts.URL+"/projects/"+projectID+"/build/start", "", nil)
	if err != nil {
		t.Fatalf("step 5: POST build/start: %v", err)
	}
	resp.Body.Close()

	// Stop the build when the test exits to avoid leaking goroutines.
	defer func() {
		stopResp, stopErr := client.Post(ts.URL+"/projects/"+projectID+"/build/stop", "", nil)
		if stopErr == nil {
			stopResp.Body.Close()
		}
	}()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("step 5: expected 303, got %d", resp.StatusCode)
	}
	expectedLoc := "/projects/" + projectID + "/build"
	if got := resp.Header.Get("Location"); got != expectedLoc {
		t.Fatalf("step 5: expected Location %q, got %q", expectedLoc, got)
	}

	// Verify the project transitioned to build phase.
	p = getProjectJSON(t, client, ts.URL, projectID)
	if p.Phase != PhaseBuild {
		t.Fatalf("step 5: expected phase %q after build start, got %q", PhaseBuild, p.Phase)
	}
	if p.RunID == "" {
		t.Fatal("step 5: expected RunID to be set after build start")
	}

	// Step 6: Verify the build view renders.
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/projects/"+projectID+"/build", nil)
	if err != nil {
		t.Fatalf("step 6: creating request: %v", err)
	}
	req.Header.Set("Accept", "text/html")

	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("step 6: GET build: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("step 6: expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "flow-a-idea") {
		t.Error("step 6: expected build view to contain project name")
	}

	// Verify SSE endpoint is available for the active build.
	srv.buildsMu.RLock()
	_, buildExists := srv.builds[projectID]
	srv.buildsMu.RUnlock()
	if !buildExists {
		t.Error("step 6: expected active build run to be tracked on server")
	}
}

// TestIntegrationFlowB exercises the Upload DOT -> Edit -> Build user flow.
//
// Steps:
//  1. POST /projects to create a project
//  2. Set DOT on the project and phase to edit (simulating DOT upload)
//  3. GET /projects/{id} to verify edit phase
//  4. POST /projects/{id}/build/start to start the build
//  5. Verify build view and SSE headers
func TestIntegrationFlowB(t *testing.T) {
	srv := newTestServer(t)
	ts := httptest.NewServer(srv)
	defer ts.Close()
	client := noFollowClient()

	// Step 1: Create the project via HTTP.
	projectID := createProjectViaHTTP(t, client, ts.URL, "flow-b-dot-upload")

	// Step 2: Simulate DOT upload by setting DOT directly on the project.
	storeProject, ok := srv.store.Get(projectID)
	if !ok {
		t.Fatal("step 2: project not found in store")
	}
	storeProject.DOT = validTestDOT
	storeProject.Phase = PhaseEdit
	if err := srv.store.Update(storeProject); err != nil {
		t.Fatalf("step 2: store.Update: %v", err)
	}

	// Step 3: Verify the project is in edit phase with DOT via HTTP.
	p := getProjectJSON(t, client, ts.URL, projectID)
	if p.Phase != PhaseEdit {
		t.Fatalf("step 3: expected phase %q, got %q", PhaseEdit, p.Phase)
	}
	if p.DOT == "" {
		t.Fatal("step 3: expected DOT to be populated")
	}

	// Step 4: Start the build via HTTP.
	resp, err := client.Post(ts.URL+"/projects/"+projectID+"/build/start", "", nil)
	if err != nil {
		t.Fatalf("step 4: POST build/start: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("step 4: expected 303, got %d", resp.StatusCode)
	}
	expectedLoc := "/projects/" + projectID + "/build"
	if got := resp.Header.Get("Location"); got != expectedLoc {
		t.Fatalf("step 4: expected Location %q, got %q", expectedLoc, got)
	}

	// Verify project transitioned to build.
	p = getProjectJSON(t, client, ts.URL, projectID)
	if p.Phase != PhaseBuild {
		t.Fatalf("step 4: expected phase %q, got %q", PhaseBuild, p.Phase)
	}
	if p.RunID == "" {
		t.Fatal("step 4: expected RunID to be set")
	}

	// Step 5: Verify build view renders.
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/projects/"+projectID+"/build", nil)
	if err != nil {
		t.Fatalf("step 5: creating request: %v", err)
	}
	req.Header.Set("Accept", "text/html")

	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("step 5: GET build: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("step 5: expected 200, got %d", resp.StatusCode)
	}

	// Verify SSE headers by checking the events endpoint.
	srv.buildsMu.RLock()
	_, buildExists := srv.builds[projectID]
	srv.buildsMu.RUnlock()
	if !buildExists {
		t.Error("step 5: expected active build run to be tracked on server")
	}

	// Check SSE content-type header with a timeout to avoid hanging indefinitely.
	sseCtx, sseCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer sseCancel()
	sseReq, err := http.NewRequestWithContext(sseCtx, http.MethodGet, ts.URL+"/projects/"+projectID+"/build/events", nil)
	if err != nil {
		t.Fatalf("step 5: creating SSE request: %v", err)
	}
	sseResp, err := client.Do(sseReq)
	if err != nil {
		t.Fatalf("step 5: GET build/events: %v", err)
	}
	defer sseResp.Body.Close()

	if ct := sseResp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("step 5: expected Content-Type starting with %q, got %q", "text/event-stream", ct)
	}

	// Stop the build so the SSE goroutine doesn't leak.
	stopResp, err := client.Post(ts.URL+"/projects/"+projectID+"/build/stop", "", nil)
	if err != nil {
		t.Fatalf("step 5: POST build/stop: %v", err)
	}
	stopResp.Body.Close()
}

// TestIntegrationFlowC exercises the Upload DOT -> Build (skip editor) user flow.
//
// Steps:
//  1. POST /projects to create a project
//  2. Set DOT and phase to build on the project (simulating DOT upload with skip editor)
//  3. POST /projects/{id}/build/start to validate and run
//  4. Verify build started or redirected appropriately
func TestIntegrationFlowC(t *testing.T) {
	srv := newTestServer(t)
	ts := httptest.NewServer(srv)
	defer ts.Close()
	client := noFollowClient()

	// Step 1: Create the project via HTTP.
	projectID := createProjectViaHTTP(t, client, ts.URL, "flow-c-skip-editor")

	// Step 2: Set DOT and phase directly (simulating upload-and-build-now).
	// The build/start handler calls TransitionEditorToBuild which re-validates,
	// so we set phase to edit to let the handler handle the transition.
	storeProject, ok := srv.store.Get(projectID)
	if !ok {
		t.Fatal("step 2: project not found in store")
	}
	storeProject.DOT = validTestDOT
	storeProject.Phase = PhaseEdit
	if err := srv.store.Update(storeProject); err != nil {
		t.Fatalf("step 2: store.Update: %v", err)
	}

	// Step 3: Start build directly (skipping editor interaction).
	resp, err := client.Post(ts.URL+"/projects/"+projectID+"/build/start", "", nil)
	if err != nil {
		t.Fatalf("step 3: POST build/start: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("step 3: expected 303, got %d", resp.StatusCode)
	}
	expectedLoc := "/projects/" + projectID + "/build"
	if got := resp.Header.Get("Location"); got != expectedLoc {
		t.Fatalf("step 3: expected Location %q, got %q", expectedLoc, got)
	}

	// Verify project is in build phase with RunID set.
	p := getProjectJSON(t, client, ts.URL, projectID)
	if p.Phase != PhaseBuild {
		t.Fatalf("step 3: expected phase %q, got %q", PhaseBuild, p.Phase)
	}
	if p.RunID == "" {
		t.Fatal("step 3: expected RunID to be set")
	}

	// Step 4: Verify the build is tracked and the build view works.
	// The engine may finish (fail) very quickly since there's no real LLM backend,
	// so we accept "running", "failed", or "completed" as valid build statuses.
	srv.buildsMu.RLock()
	run, buildExists := srv.builds[projectID]
	var runStatus string
	if buildExists {
		runStatus = run.State.Status
	}
	srv.buildsMu.RUnlock()
	if !buildExists {
		t.Fatal("step 4: expected active build run")
	}
	validStatuses := map[string]bool{"running": true, "failed": true, "completed": true}
	if !validStatuses[runStatus] {
		t.Errorf("step 4: expected run status to be running/failed/completed, got %q", runStatus)
	}

	// Verify build view renders.
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/projects/"+projectID+"/build", nil)
	if err != nil {
		t.Fatalf("step 4: creating request: %v", err)
	}
	req.Header.Set("Accept", "text/html")

	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("step 4: GET build: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("step 4: expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "flow-c-skip-editor") {
		t.Error("step 4: expected build view to contain project name")
	}

	// Clean up: stop the build.
	stopResp, err := client.Post(ts.URL+"/projects/"+projectID+"/build/stop", "", nil)
	if err != nil {
		t.Fatalf("cleanup: POST build/stop: %v", err)
	}
	stopResp.Body.Close()
}

// TestIntegrationBuildStopCancellation verifies that stopping a build via HTTP
// cancels the build context and updates the run state across the full HTTP stack.
func TestIntegrationBuildStopCancellation(t *testing.T) {
	srv := newTestServer(t)
	ts := httptest.NewServer(srv)
	defer ts.Close()
	client := noFollowClient()

	// Create a project and start a build.
	projectID := createProjectViaHTTP(t, client, ts.URL, "stop-integration")

	storeProject, ok := srv.store.Get(projectID)
	if !ok {
		t.Fatal("project not found in store")
	}
	storeProject.DOT = validTestDOT
	storeProject.Phase = PhaseEdit
	if err := srv.store.Update(storeProject); err != nil {
		t.Fatalf("store.Update: %v", err)
	}

	// Start the build.
	resp, err := client.Post(ts.URL+"/projects/"+projectID+"/build/start", "", nil)
	if err != nil {
		t.Fatalf("POST build/start: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp.StatusCode)
	}

	// Capture the build context before stopping.
	srv.buildsMu.RLock()
	run, exists := srv.builds[projectID]
	srv.buildsMu.RUnlock()
	if !exists {
		t.Fatal("expected build run to exist")
	}
	buildCtx := run.Ctx

	// Stop the build via HTTP.
	resp, err = client.Post(ts.URL+"/projects/"+projectID+"/build/stop", "", nil)
	if err != nil {
		t.Fatalf("POST build/stop: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp.StatusCode)
	}

	// Verify context was cancelled.
	select {
	case <-buildCtx.Done():
		// Expected.
	case <-time.After(2 * time.Second):
		t.Fatal("expected build context to be cancelled within 2 seconds")
	}

	// Verify run state is cancelled.
	srv.buildsMu.RLock()
	stoppedRun := srv.builds[projectID]
	var stoppedStatus string
	var hasCompletedAt bool
	if stoppedRun != nil {
		stoppedStatus = stoppedRun.State.Status
		hasCompletedAt = stoppedRun.State.CompletedAt != nil
	}
	srv.buildsMu.RUnlock()
	if stoppedStatus != "cancelled" {
		t.Errorf("expected run status %q, got %q", "cancelled", stoppedStatus)
	}
	if !hasCompletedAt {
		t.Error("expected CompletedAt to be set after stop")
	}
}

// TestIntegrationInvalidDOTStaysInEdit verifies that attempting to build with
// invalid DOT keeps the project in edit phase and populates diagnostics, all
// through the full HTTP stack.
func TestIntegrationInvalidDOTStaysInEdit(t *testing.T) {
	srv := newTestServer(t)
	ts := httptest.NewServer(srv)
	defer ts.Close()
	client := noFollowClient()

	projectID := createProjectViaHTTP(t, client, ts.URL, "invalid-dot-flow")

	// Set invalid DOT on the project.
	storeProject, ok := srv.store.Get(projectID)
	if !ok {
		t.Fatal("project not found in store")
	}
	storeProject.DOT = "this is not valid DOT at all"
	storeProject.Phase = PhaseEdit
	if err := srv.store.Update(storeProject); err != nil {
		t.Fatalf("store.Update: %v", err)
	}

	// Attempt to start build.
	resp, err := client.Post(ts.URL+"/projects/"+projectID+"/build/start", "", nil)
	if err != nil {
		t.Fatalf("POST build/start: %v", err)
	}
	resp.Body.Close()

	// Should redirect back to the project overview (not the build page).
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if loc != "/projects/"+projectID {
		t.Errorf("expected redirect to project overview, got %q", loc)
	}

	// Verify the project stayed in edit phase with diagnostics.
	p := getProjectJSON(t, client, ts.URL, projectID)
	if p.Phase != PhaseEdit {
		t.Errorf("expected phase %q, got %q", PhaseEdit, p.Phase)
	}
	if len(p.Diagnostics) == 0 {
		t.Error("expected diagnostics to be populated for invalid DOT")
	}

	// Verify no build run was created.
	srv.buildsMu.RLock()
	_, buildExists := srv.builds[projectID]
	srv.buildsMu.RUnlock()
	if buildExists {
		t.Error("expected no build run for invalid DOT")
	}
}

// makeTestSpecState creates a realistic SpecState with core metadata and a task
// card in the Plan lane, suitable for exercising the spec-to-editor transition.
func makeTestSpecState() *core.SpecState {
	sc := core.NewSpecCore(
		"Integration Test Pipeline",
		"End-to-end integration test",
		"Verify the full wizard flow from idea to build",
	)
	state := core.NewSpecState()
	state.Core = &sc

	now := time.Now().UTC()
	task := core.Card{
		CardID:    core.NewULID(),
		CardType:  "task",
		Title:     "Implement feature",
		Lane:      "Plan",
		Order:     1.0,
		Refs:      []string{},
		CreatedAt: now,
		UpdatedAt: now,
		CreatedBy: "test",
		UpdatedBy: "test",
	}
	body := "Build the main feature with proper error handling"
	task.Body = &body
	state.Cards.Set(task.CardID, task)

	return state
}
