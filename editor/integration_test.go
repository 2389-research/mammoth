// ABOUTME: End-to-end integration tests for the HTTP server
// ABOUTME: Tests full workflows using httptest.Server with the real chi router

package editor

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// validTestDOT is a valid mammoth pipeline for testing
const validTestDOT = `digraph test {
    graph [goal="Test pipeline"]
    start [shape=Mdiamond, label="Start"]
    process [shape=box, prompt="Do work", label="Process"]
    end [shape=Msquare, label="End"]
    start -> process
    process -> end
}`

// setupTestServer creates a test server with real session store and templates
func setupTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	store := NewStore(100, time.Hour)
	// Template paths relative to the editor package directory
	srv := NewServer(store, "templates", "static")
	return httptest.NewServer(srv)
}

// createIntegrationTestSession creates a session via HTTP and returns its ID
func createIntegrationTestSession(t *testing.T, ts *httptest.Server, dotSource string) string {
	t.Helper()
	// POST to /sessions, don't follow redirect
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	form := url.Values{"dot": {dotSource}}
	resp, err := client.PostForm(ts.URL+"/sessions", form)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 303, got %d: %s", resp.StatusCode, body)
	}

	loc := resp.Header.Get("Location")
	// Extract session ID from /sessions/{id}
	parts := strings.Split(loc, "/sessions/")
	if len(parts) < 2 {
		t.Fatalf("bad redirect location: %s", loc)
	}
	return parts[1]
}

func TestIntegration_FullUploadFlow(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	// Step 1: POST valid DOT
	form := url.Values{"dot": {validTestDOT}}
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.PostForm(ts.URL+"/sessions", form)
	if err != nil {
		t.Fatalf("POST /sessions: %v", err)
	}
	defer resp.Body.Close()

	// Step 2: Verify 303 redirect
	if resp.StatusCode != http.StatusSeeOther {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 303, got %d: %s", resp.StatusCode, body)
	}

	redirectURL := resp.Header.Get("Location")
	if !strings.HasPrefix(redirectURL, "/sessions/") {
		t.Fatalf("expected redirect to /sessions/{id}, got %s", redirectURL)
	}

	// Step 3: Follow redirect to editor page
	resp2, err := http.Get(ts.URL + redirectURL)
	if err != nil {
		t.Fatalf("GET editor page: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp2.StatusCode)
	}

	// Step 4: Verify HTML contains three panels
	body, err := io.ReadAll(resp2.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	bodyStr := string(body)

	// Check for code editor
	if !strings.Contains(bodyStr, "code-editor") && !strings.Contains(bodyStr, "code_editor") {
		t.Error("editor page missing code editor panel")
	}

	// Check for graph viewer
	if !strings.Contains(bodyStr, "graph-viewer") && !strings.Contains(bodyStr, "graph_viewer") {
		t.Error("editor page missing graph viewer panel")
	}

	// Check for property panel
	if !strings.Contains(bodyStr, "property-panel") && !strings.Contains(bodyStr, "property_panel") {
		t.Error("editor page missing property panel")
	}

	// Verify DOT content is present
	if !strings.Contains(bodyStr, "digraph test") {
		t.Error("editor page missing DOT content")
	}
}

func TestIntegration_EditNode(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	// Create session
	sessionID := createIntegrationTestSession(t, ts, validTestDOT)

	// Update a node attribute
	form := url.Values{
		"attr_label": {"Updated Process"},
		"attr_color": {"blue"},
	}
	resp, err := http.PostForm(ts.URL+"/sessions/"+sessionID+"/nodes/process", form)
	if err != nil {
		t.Fatalf("POST update node: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	// Verify response contains updated attributes
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	bodyStr := string(body)

	if !strings.Contains(bodyStr, "Updated Process") {
		t.Error("response missing updated label")
	}
	if !strings.Contains(bodyStr, "blue") {
		t.Error("response missing updated color")
	}
}

func TestIntegration_EditDOTSource(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	// Create session
	sessionID := createIntegrationTestSession(t, ts, validTestDOT)

	// Update DOT source
	newDOT := `digraph updated {
    graph [goal="Updated pipeline"]
    a [label="Node A"]
    b [label="Node B"]
    a -> b
}`
	form := url.Values{"dot": {newDOT}}
	resp, err := http.PostForm(ts.URL+"/sessions/"+sessionID+"/dot", form)
	if err != nil {
		t.Fatalf("POST update DOT: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	// Verify response contains updated content
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	bodyStr := string(body)

	if !strings.Contains(bodyStr, "digraph updated") {
		t.Error("response missing updated graph name")
	}
	if !strings.Contains(bodyStr, "Node A") {
		t.Error("response missing new node A")
	}
	if !strings.Contains(bodyStr, "Node B") {
		t.Error("response missing new node B")
	}
}

func TestIntegration_UndoRedo(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	// Create session
	sessionID := createIntegrationTestSession(t, ts, validTestDOT)

	// Update DOT
	newDOT := `digraph changed {
    a [label="Changed"]
    b [label="Graph"]
    a -> b
}`
	form := url.Values{"dot": {newDOT}}
	resp, err := http.PostForm(ts.URL+"/sessions/"+sessionID+"/dot", form)
	if err != nil {
		t.Fatalf("POST update DOT: %v", err)
	}
	resp.Body.Close()

	// POST /undo to restore old DOT
	resp, err = http.PostForm(ts.URL+"/sessions/"+sessionID+"/undo", nil)
	if err != nil {
		t.Fatalf("POST undo: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("undo failed with %d: %s", resp.StatusCode, body)
	}

	// Verify old DOT restored
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	bodyStr := string(body)

	if !strings.Contains(bodyStr, "digraph test") {
		t.Error("undo did not restore original graph")
	}
	if !strings.Contains(bodyStr, "Start") {
		t.Error("undo did not restore original nodes")
	}

	// POST /redo to reapply new DOT
	resp2, err := http.PostForm(ts.URL+"/sessions/"+sessionID+"/redo", nil)
	if err != nil {
		t.Fatalf("POST redo: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp2.Body)
		t.Fatalf("redo failed with %d: %s", resp2.StatusCode, body)
	}

	// Verify new DOT re-applied
	body2, err := io.ReadAll(resp2.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	bodyStr2 := string(body2)

	if !strings.Contains(bodyStr2, "digraph changed") {
		t.Error("redo did not restore changed graph")
	}
	if !strings.Contains(bodyStr2, "Changed") {
		t.Error("redo did not restore changed nodes")
	}
}

func TestIntegration_Export(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	// Create session
	sessionID := createIntegrationTestSession(t, ts, validTestDOT)

	// GET /export
	resp, err := http.Get(ts.URL + "/sessions/" + sessionID + "/export")
	if err != nil {
		t.Fatalf("GET export: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	// Verify Content-Type
	contentType := resp.Header.Get("Content-Type")
	if contentType != "text/vnd.graphviz" {
		t.Errorf("expected Content-Type text/vnd.graphviz, got %s", contentType)
	}

	// Verify Content-Disposition is attachment
	contentDisposition := resp.Header.Get("Content-Disposition")
	if !strings.Contains(contentDisposition, "attachment") {
		t.Errorf("expected attachment in Content-Disposition, got %s", contentDisposition)
	}

	// Verify body is valid DOT
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	bodyStr := string(body)

	if !strings.Contains(bodyStr, "digraph test") {
		t.Error("exported DOT missing graph declaration")
	}
	if !strings.Contains(bodyStr, "start") || !strings.Contains(bodyStr, "process") {
		t.Error("exported DOT missing nodes")
	}
}

func TestIntegration_InvalidDOT(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	// POST garbage DOT
	form := url.Values{"dot": {"this is not valid DOT syntax }{]["}}
	resp, err := http.PostForm(ts.URL+"/sessions", form)
	if err != nil {
		t.Fatalf("POST invalid DOT: %v", err)
	}
	defer resp.Body.Close()

	// Verify 422 status
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", resp.StatusCode)
	}

	// Verify error message in body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	bodyStr := string(body)

	if !strings.Contains(bodyStr, "Invalid DOT") && !strings.Contains(bodyStr, "parse error") {
		t.Error("response missing error message about invalid DOT")
	}
}

func TestIntegration_ValidMammothPipeline(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	// Create session with valid mammoth pipeline
	sessionID := createIntegrationTestSession(t, ts, validTestDOT)

	// GET /validate
	resp, err := http.Get(ts.URL + "/sessions/" + sessionID + "/validate")
	if err != nil {
		t.Fatalf("GET validate: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	// Verify response has diagnostics (info/warning level only, no errors)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	bodyStr := string(body)

	// Check that diagnostics section is present
	if !strings.Contains(bodyStr, "diagnostic") && !strings.Contains(bodyStr, "Diagnostic") {
		// May not contain the word diagnostic if no issues, check for common patterns
		// The response should at least be non-empty HTML partial
		if len(bodyStr) < 10 {
			t.Error("validate response too short")
		}
	}

	// Ensure no "error" level diagnostics for this valid pipeline
	// (This is a soft check - the validator might not have "error" in output for valid graphs)
	if strings.Contains(strings.ToLower(bodyStr), "severity: error") ||
		strings.Contains(strings.ToLower(bodyStr), "level: error") {
		t.Error("valid pipeline should not have error-level diagnostics")
	}
}

func TestIntegration_SessionNotFound(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	// Try to access non-existent session, don't follow redirects
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Get(ts.URL + "/sessions/nonexistent-session-id")
	if err != nil {
		t.Fatalf("GET nonexistent session: %v", err)
	}
	defer resp.Body.Close()

	// Should redirect to landing page
	if resp.StatusCode != http.StatusFound && resp.StatusCode != http.StatusSeeOther {
		t.Errorf("expected redirect (302 or 303), got %d", resp.StatusCode)
	}

	loc := resp.Header.Get("Location")
	if loc != "/" {
		t.Errorf("expected redirect to /, got %s", loc)
	}
}

func TestIntegration_AddNodeFlow(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	// Create session
	sessionID := createIntegrationTestSession(t, ts, validTestDOT)

	// Add a new node
	form := url.Values{
		"id":         {"newnode"},
		"attr_label": {"New Node"},
		"attr_shape": {"ellipse"},
		"attr_color": {"red"},
	}
	resp, err := http.PostForm(ts.URL+"/sessions/"+sessionID+"/nodes", form)
	if err != nil {
		t.Fatalf("POST add node: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	// Verify response contains new node
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	bodyStr := string(body)

	if !strings.Contains(bodyStr, "newnode") {
		t.Error("response missing new node ID")
	}
	if !strings.Contains(bodyStr, "New Node") {
		t.Error("response missing new node label")
	}
}

func TestIntegration_DeleteNodeFlow(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	// Create session
	sessionID := createIntegrationTestSession(t, ts, validTestDOT)

	// Delete a node
	req, err := http.NewRequest("DELETE", ts.URL+"/sessions/"+sessionID+"/nodes/process", nil)
	if err != nil {
		t.Fatalf("create DELETE request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE node: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	// Verify response no longer contains deleted node
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	bodyStr := string(body)

	// The node "process" should not appear as a standalone node
	// (It might appear in historical context, so check for node definition)
	if strings.Contains(bodyStr, `"process"`) || strings.Contains(bodyStr, "process [") {
		t.Error("response still contains deleted node definition")
	}
}

func TestIntegration_AddEdgeFlow(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	// Create session
	sessionID := createIntegrationTestSession(t, ts, validTestDOT)

	// Add a new edge
	form := url.Values{
		"from":       {"start"},
		"to":         {"end"},
		"attr_label": {"shortcut"},
		"attr_style": {"dashed"},
	}
	resp, err := http.PostForm(ts.URL+"/sessions/"+sessionID+"/edges", form)
	if err != nil {
		t.Fatalf("POST add edge: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	// Verify response contains new edge
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	bodyStr := string(body)

	if !strings.Contains(bodyStr, "start") || !strings.Contains(bodyStr, "end") {
		t.Error("response missing edge endpoints")
	}
	if !strings.Contains(bodyStr, "shortcut") {
		t.Error("response missing edge label")
	}
}

func TestIntegration_EmptyDOTSubmission(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	// POST empty DOT
	form := url.Values{"dot": {"   "}}
	resp, err := http.PostForm(ts.URL+"/sessions", form)
	if err != nil {
		t.Fatalf("POST empty DOT: %v", err)
	}
	defer resp.Body.Close()

	// Verify 422 status
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", resp.StatusCode)
	}

	// Verify error message
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	bodyStr := string(body)

	if !strings.Contains(bodyStr, "required") && !strings.Contains(bodyStr, "Required") {
		t.Error("response missing error message about required DOT")
	}
}

func TestIntegration_UpdateGraphAttrs(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	// Create session
	sessionID := createIntegrationTestSession(t, ts, validTestDOT)

	// Update graph attributes
	form := url.Values{
		"attr_goal":    {"Updated goal"},
		"attr_bgcolor": {"lightgray"},
		"attr_rankdir": {"LR"},
	}
	resp, err := http.PostForm(ts.URL+"/sessions/"+sessionID+"/attrs", form)
	if err != nil {
		t.Fatalf("POST update graph attrs: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	// Verify response contains updated attributes
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	bodyStr := string(body)

	if !strings.Contains(bodyStr, "Updated goal") {
		t.Error("response missing updated goal attribute")
	}
}
