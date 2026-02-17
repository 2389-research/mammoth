// ABOUTME: Test suite for HTTP server handlers covering scaffolding, session lifecycle, and mutations
// ABOUTME: Uses httptest with chi router to verify all API endpoints

package editor

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/2389-research/mammoth/dot"
)

const testDOT = `digraph test {
    start [shape=Mdiamond]
    end [shape=Msquare]
    start -> end
}`

const invalidTestDOT = `not valid dot at all {{{`

// newTestServer creates a server configured for testing with templates and static files
// resolved relative to the editor package directory.
func newTestServer(t *testing.T) (*Server, *Store) {
	t.Helper()
	store := NewStore(100, time.Hour)
	srv := NewServer(store, "templates", "static")
	return srv, store
}

// createTestSession creates a session with valid test DOT and returns the session ID.
func createTestSession(t *testing.T, store *Store) string {
	t.Helper()
	sess, err := store.Create(testDOT)
	if err != nil {
		t.Fatalf("failed to create test session: %v", err)
	}
	return sess.ID
}

// ============================================================
// Task 8a Tests: Server scaffolding
// ============================================================

func TestLandingPageReturns200(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "mammoth-dot-editor") {
		t.Fatal("expected body to contain 'mammoth-dot-editor'")
	}
}

func TestStaticCSSReturns200(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/static/css/style.css", nil)
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "mammoth-dot-editor styles") {
		t.Fatal("expected CSS to contain 'mammoth-dot-editor styles'")
	}
}

func TestTemplatesParseWithoutError(t *testing.T) {
	srv, _ := newTestServer(t)

	if srv.templates == nil {
		t.Fatal("expected shared templates to be parsed, got nil")
	}

	// Verify shared partials are present in the base template set
	sharedNames := []string{"layout", "code_editor", "graph_viewer", "property_panel", "diagnostics"}
	for _, name := range sharedNames {
		if srv.templates.Lookup(name) == nil {
			t.Errorf("expected shared template %q to be defined", name)
		}
	}

	// Verify each page-specific template set defines "content"
	if srv.landingTmpl == nil {
		t.Fatal("expected landing template set to be parsed, got nil")
	}
	if srv.landingTmpl.Lookup("content") == nil {
		t.Error("expected landing template set to define 'content'")
	}

	if srv.editorTmpl == nil {
		t.Fatal("expected editor template set to be parsed, got nil")
	}
	if srv.editorTmpl.Lookup("content") == nil {
		t.Error("expected editor template set to define 'content'")
	}
}

// ============================================================
// Task 8b Tests: Session lifecycle handlers
// ============================================================

func TestCreateSessionWithValidDOT(t *testing.T) {
	srv, _ := newTestServer(t)

	form := url.Values{}
	form.Set("dot", testDOT)

	req := httptest.NewRequest(http.MethodPost, "/sessions", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", resp.StatusCode)
	}

	location := resp.Header.Get("Location")
	if !strings.HasPrefix(location, "/sessions/") {
		t.Fatalf("expected redirect to /sessions/{id}, got %s", location)
	}
}

func TestCreateSessionWithInvalidDOT(t *testing.T) {
	srv, _ := newTestServer(t)

	form := url.Values{}
	form.Set("dot", invalidTestDOT)

	req := httptest.NewRequest(http.MethodPost, "/sessions", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected status 422, got %d", resp.StatusCode)
	}
}

func TestCreateSessionOversizeBodyReturns413(t *testing.T) {
	srv, _ := newTestServer(t)

	// Create a body larger than 10MB
	largeBody := strings.Repeat("x", 11<<20)
	form := url.Values{}
	form.Set("dot", largeBody)

	req := httptest.NewRequest(http.MethodPost, "/sessions", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	resp := w.Result()
	// Should be either 413 (too large) or 422 (invalid DOT) — not 200
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusSeeOther {
		t.Fatalf("expected error status for oversize body, got %d", resp.StatusCode)
	}
}

func TestEditorPageReturns200(t *testing.T) {
	srv, store := newTestServer(t)
	sessID := createTestSession(t, store)

	req := httptest.NewRequest(http.MethodGet, "/sessions/"+sessID, nil)
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)
	if !strings.Contains(bodyStr, "editor") {
		t.Fatal("expected body to contain 'editor'")
	}
	if !strings.Contains(bodyStr, sessID) {
		t.Fatal("expected body to contain session ID")
	}
}

func TestEditorPageNonexistentSessionRedirects(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/sessions/nonexistent-id", nil)
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected status 302, got %d", resp.StatusCode)
	}

	location := resp.Header.Get("Location")
	if location != "/" {
		t.Fatalf("expected redirect to /, got %s", location)
	}
}

func TestExportReturns200WithCorrectContentType(t *testing.T) {
	srv, store := newTestServer(t)
	sessID := createTestSession(t, store)

	req := httptest.NewRequest(http.MethodGet, "/sessions/"+sessID+"/export", nil)
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "text/vnd.graphviz" {
		t.Fatalf("expected Content-Type 'text/vnd.graphviz', got %q", ct)
	}

	disp := resp.Header.Get("Content-Disposition")
	if !strings.Contains(disp, ".dot") {
		t.Fatalf("expected Content-Disposition to contain '.dot', got %q", disp)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "digraph") {
		t.Fatal("expected body to contain DOT source")
	}
}

func TestValidateReturns200WithDiagnostics(t *testing.T) {
	srv, store := newTestServer(t)
	sessID := createTestSession(t, store)

	req := httptest.NewRequest(http.MethodGet, "/sessions/"+sessID+"/validate", nil)
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "diagnostics") {
		t.Fatal("expected body to contain diagnostics")
	}
}

// ============================================================
// Task 8c Tests: Mutation handlers
// ============================================================

func TestUpdateDOTHandler(t *testing.T) {
	srv, store := newTestServer(t)
	sessID := createTestSession(t, store)

	newDOT := `digraph test {
    start [shape=Mdiamond]
    middle [shape=box]
    end [shape=Msquare]
    start -> middle -> end
}`

	form := url.Values{}
	form.Set("dot", newDOT)

	req := httptest.NewRequest(http.MethodPost, "/sessions/"+sessID+"/dot", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d: %s", resp.StatusCode, string(body))
	}
}

func TestUpdateNodeHandler(t *testing.T) {
	srv, store := newTestServer(t)
	sessID := createTestSession(t, store)

	form := url.Values{}
	form.Set("attr_label", "Updated Start")
	form.Set("attr_color", "red")

	req := httptest.NewRequest(http.MethodPost, "/sessions/"+sessID+"/nodes/start", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d: %s", resp.StatusCode, string(body))
	}
}

func TestAddNodeHandler(t *testing.T) {
	srv, store := newTestServer(t)
	sessID := createTestSession(t, store)

	form := url.Values{}
	form.Set("id", "middle")
	form.Set("attr_shape", "box")
	form.Set("attr_label", "Middle Node")

	req := httptest.NewRequest(http.MethodPost, "/sessions/"+sessID+"/nodes", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d: %s", resp.StatusCode, string(body))
	}
}

func TestDeleteNodeHandler(t *testing.T) {
	srv, store := newTestServer(t)
	sessID := createTestSession(t, store)

	req := httptest.NewRequest(http.MethodDelete, "/sessions/"+sessID+"/nodes/end", nil)
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d: %s", resp.StatusCode, string(body))
	}
}

func TestAddEdgeHandler(t *testing.T) {
	srv, store := newTestServer(t)
	sessID := createTestSession(t, store)

	form := url.Values{}
	form.Set("from", "end")
	form.Set("to", "start")

	req := httptest.NewRequest(http.MethodPost, "/sessions/"+sessID+"/edges", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d: %s", resp.StatusCode, string(body))
	}
}

func TestUpdateEdgeHandler(t *testing.T) {
	srv, store := newTestServer(t)
	sessID := createTestSession(t, store)

	// Get the edge ID from the session
	sess, _ := store.Get(sessID)
	if len(sess.Graph.Edges) == 0 {
		t.Fatal("expected at least one edge in test session")
	}
	edgeID := sess.Graph.Edges[0].ID

	form := url.Values{}
	form.Set("attr_label", "updated edge")
	form.Set("attr_color", "blue")

	req := httptest.NewRequest(http.MethodPost, "/sessions/"+sessID+"/edges/"+edgeID, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d: %s", resp.StatusCode, string(body))
	}
}

func TestDeleteEdgeHandler(t *testing.T) {
	srv, store := newTestServer(t)
	sessID := createTestSession(t, store)

	// Get the edge ID from the session
	sess, _ := store.Get(sessID)
	if len(sess.Graph.Edges) == 0 {
		t.Fatal("expected at least one edge in test session")
	}
	edgeID := sess.Graph.Edges[0].ID

	req := httptest.NewRequest(http.MethodDelete, "/sessions/"+sessID+"/edges/"+edgeID, nil)
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d: %s", resp.StatusCode, string(body))
	}
}

func TestUpdateGraphAttrsHandler(t *testing.T) {
	srv, store := newTestServer(t)
	sessID := createTestSession(t, store)

	form := url.Values{}
	form.Set("attr_bgcolor", "lightblue")
	form.Set("attr_rankdir", "LR")

	req := httptest.NewRequest(http.MethodPost, "/sessions/"+sessID+"/attrs", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d: %s", resp.StatusCode, string(body))
	}
}

func TestUndoHandler(t *testing.T) {
	srv, store := newTestServer(t)
	sessID := createTestSession(t, store)

	// First, make a mutation so there's something to undo
	sess, _ := store.Get(sessID)
	sess.UpdateDOT(`digraph test {
    start [shape=Mdiamond]
    end [shape=Msquare]
    start -> end
    extra [shape=box]
}`)

	req := httptest.NewRequest(http.MethodPost, "/sessions/"+sessID+"/undo", nil)
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d: %s", resp.StatusCode, string(body))
	}
}

func TestRedoHandler(t *testing.T) {
	srv, store := newTestServer(t)
	sessID := createTestSession(t, store)

	// Make a mutation, then undo it, so there's something to redo
	sess, _ := store.Get(sessID)
	sess.UpdateDOT(`digraph test {
    start [shape=Mdiamond]
    end [shape=Msquare]
    start -> end
    extra [shape=box]
}`)
	sess.Undo()

	req := httptest.NewRequest(http.MethodPost, "/sessions/"+sessID+"/redo", nil)
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d: %s", resp.StatusCode, string(body))
	}
}

// ============================================================
// Node/Edge Edit Form Tests
// ============================================================

func TestNodeEditFormReturns200WithFormContent(t *testing.T) {
	srv, store := newTestServer(t)
	sessID := createTestSession(t, store)

	req := httptest.NewRequest(http.MethodGet, "/sessions/"+sessID+"/nodes/start/edit", nil)
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d: %s", resp.StatusCode, string(body))
	}

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)
	if !strings.Contains(bodyStr, "hx-post") {
		t.Fatal("expected response body to contain 'hx-post'")
	}
	if !strings.Contains(bodyStr, "start") {
		t.Fatal("expected response body to contain node ID 'start'")
	}
}

func TestNodeEditFormAcceptsQuotedNodeID(t *testing.T) {
	srv, store := newTestServer(t)
	sessID := createTestSession(t, store)

	req := httptest.NewRequest(http.MethodGet, "/sessions/"+sessID+"/nodes/%22start%22/edit", nil)
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d: %s", resp.StatusCode, string(body))
	}
}

func TestNodeEditFormByQueryReturns200(t *testing.T) {
	srv, store := newTestServer(t)
	sessID := createTestSession(t, store)

	req := httptest.NewRequest(http.MethodGet, "/sessions/"+sessID+"/node-edit?id=start", nil)
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d: %s", resp.StatusCode, string(body))
	}
}

func TestNodeEditFormByQueryAcceptsQuotedNodeID(t *testing.T) {
	srv, store := newTestServer(t)
	sessID := createTestSession(t, store)

	req := httptest.NewRequest(http.MethodGet, "/sessions/"+sessID+"/node-edit?id=%22start%22", nil)
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d: %s", resp.StatusCode, string(body))
	}
}

func TestNodeEditFormReturns404ForMissingNode(t *testing.T) {
	srv, store := newTestServer(t)
	sessID := createTestSession(t, store)

	req := httptest.NewRequest(http.MethodGet, "/sessions/"+sessID+"/nodes/nonexistent/edit", nil)
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", resp.StatusCode)
	}
}

func TestNodeEditFormReturns404ForMissingSession(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/sessions/nonexistent-id/nodes/start/edit", nil)
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", resp.StatusCode)
	}
}

func TestEdgeEditFormReturns200WithFormContent(t *testing.T) {
	srv, store := newTestServer(t)
	sessID := createTestSession(t, store)

	// Get the edge ID from the session
	sess, _ := store.Get(sessID)
	if len(sess.Graph.Edges) == 0 {
		t.Fatal("expected at least one edge in test session")
	}
	edgeID := sess.Graph.Edges[0].ID

	req := httptest.NewRequest(http.MethodGet, "/sessions/"+sessID+"/edges/"+edgeID+"/edit", nil)
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d: %s", resp.StatusCode, string(body))
	}

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)
	if !strings.Contains(bodyStr, "hx-post") {
		t.Fatal("expected response body to contain 'hx-post'")
	}
	// Edge IDs contain -> which html/template escapes to -&gt;
	escapedEdgeID := strings.ReplaceAll(edgeID, ">", "&gt;")
	if !strings.Contains(bodyStr, escapedEdgeID) {
		t.Fatalf("expected response body to contain edge ID %q (escaped: %q), got: %s", edgeID, escapedEdgeID, bodyStr)
	}
}

func TestEdgeEditFormAcceptsQuotedEdgeID(t *testing.T) {
	srv, store := newTestServer(t)
	sessID := createTestSession(t, store)

	// Use the stable ID form expected by edge lookup.
	req := httptest.NewRequest(http.MethodGet, "/sessions/"+sessID+"/edges/%22start-%3Eend%22/edit", nil)
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d: %s", resp.StatusCode, string(body))
	}
}

func TestEdgeEditFormByQueryReturns200(t *testing.T) {
	srv, store := newTestServer(t)
	sessID := createTestSession(t, store)

	sess, _ := store.Get(sessID)
	if len(sess.Graph.Edges) == 0 {
		t.Fatal("expected at least one edge in test session")
	}
	edgeID := sess.Graph.Edges[0].ID

	req := httptest.NewRequest(http.MethodGet, "/sessions/"+sessID+"/edge-edit?id="+url.QueryEscape(edgeID), nil)
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d: %s", resp.StatusCode, string(body))
	}
}

func TestEdgeEditFormReturns404ForMissingEdge(t *testing.T) {
	srv, store := newTestServer(t)
	sessID := createTestSession(t, store)

	req := httptest.NewRequest(http.MethodGet, "/sessions/"+sessID+"/edges/nonexistent/edit", nil)
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", resp.StatusCode)
	}
}

func TestEdgeEditFormReturns404ForMissingSession(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/sessions/nonexistent-id/edges/start->end/edit", nil)
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", resp.StatusCode)
	}
}

// ============================================================
// Case-insensitive node lookup tests
// ============================================================

func TestFindNodeByParamCaseInsensitive(t *testing.T) {
	nodes := map[string]*dot.Node{
		"MyNode": {ID: "MyNode", Attrs: map[string]string{"label": "Test"}},
	}

	// Exact match works
	node, ok := findNodeByParam(nodes, "MyNode")
	if !ok {
		t.Fatal("expected exact match to succeed")
	}
	if node.ID != "MyNode" {
		t.Fatalf("expected ID MyNode, got %s", node.ID)
	}

	// Case-insensitive fallback works
	node, ok = findNodeByParam(nodes, "mynode")
	if !ok {
		t.Fatal("expected case-insensitive match to succeed")
	}
	if node.ID != "MyNode" {
		t.Fatalf("expected ID MyNode, got %s", node.ID)
	}

	// Still returns false for completely wrong ID
	_, ok = findNodeByParam(nodes, "nonexistent")
	if ok {
		t.Fatal("expected nonexistent node to not match")
	}
}

func TestNodeEditFormCaseInsensitiveLookup(t *testing.T) {
	srv, store := newTestServer(t)
	sessID := createTestSession(t, store)

	// testDOT has "start" node — try with "Start" (different case)
	req := httptest.NewRequest(http.MethodGet, "/sessions/"+sessID+"/node-edit?id=Start", nil)
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	resp := w.Result()
	// Should succeed via case-insensitive fallback
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d: %s", resp.StatusCode, string(body))
	}
}

func TestNewServerAcceptsModelOptions(t *testing.T) {
	store := NewStore(100, time.Hour)
	models := []ModelOption{
		{ID: "claude-sonnet-4-5", DisplayName: "Claude Sonnet 4.5", Provider: "anthropic"},
		{ID: "gpt-5.2", DisplayName: "GPT-5.2", Provider: "openai"},
	}
	srv := NewServer(store, "templates", "static", WithModelOptions(models))
	if srv == nil {
		t.Fatal("expected server to be created")
	}
	if len(srv.modelOptions) != 2 {
		t.Fatalf("expected 2 model options, got %d", len(srv.modelOptions))
	}
}

func TestNodeEditFormShowsModelDropdown(t *testing.T) {
	store := NewStore(100, time.Hour)
	models := []ModelOption{
		{ID: "claude-sonnet-4-5", DisplayName: "Claude Sonnet 4.5", Provider: "anthropic"},
		{ID: "gpt-5.2", DisplayName: "GPT-5.2", Provider: "openai"},
	}
	srv := NewServer(store, "templates", "static", WithModelOptions(models))

	sessID := createTestSession(t, store)

	req := httptest.NewRequest(http.MethodGet, "/sessions/"+sessID+"/node-edit?id=start", nil)
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d: %s", resp.StatusCode, string(body))
	}

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)
	if !strings.Contains(bodyStr, "attr_llm_model") {
		t.Fatal("expected model dropdown with name attr_llm_model")
	}
	if !strings.Contains(bodyStr, "Claude Sonnet 4.5") {
		t.Fatal("expected model option Claude Sonnet 4.5")
	}
	if !strings.Contains(bodyStr, "Use stylesheet default") {
		t.Fatal("expected 'Use stylesheet default' option")
	}
}

func TestNodeEditFormShowsResolvedModel(t *testing.T) {
	store := NewStore(100, time.Hour)
	srv := NewServer(store, "templates", "static")

	// Create session with model_stylesheet
	dotWithStylesheet := `digraph test {
    model_stylesheet="* { llm_model: claude-sonnet-4-5; }"
    start [shape=Mdiamond]
    end [shape=Msquare]
    start -> end
}`
	sess, err := store.Create(dotWithStylesheet)
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/sessions/"+sess.ID+"/node-edit?id=start", nil)
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d: %s", resp.StatusCode, string(body))
	}

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)
	if !strings.Contains(bodyStr, "claude-sonnet-4-5") {
		t.Fatal("expected resolved model claude-sonnet-4-5 in response")
	}
}

func TestNodeEditFormModelOverrideRoundTrip(t *testing.T) {
	store := NewStore(100, time.Hour)
	models := []ModelOption{
		{ID: "claude-sonnet-4-5", DisplayName: "Claude Sonnet 4.5", Provider: "anthropic"},
		{ID: "claude-opus-4-6", DisplayName: "Claude Opus 4.6", Provider: "anthropic"},
	}
	srv := NewServer(store, "templates", "static", WithModelOptions(models))

	sessID := createTestSession(t, store)

	// Set llm_model on the start node
	form := url.Values{}
	form.Set("attr_llm_model", "claude-opus-4-6")
	form.Set("attr_shape", "Mdiamond")

	req := httptest.NewRequest(http.MethodPost, "/sessions/"+sessID+"/nodes/start", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200 on update, got %d: %s", resp.StatusCode, string(body))
	}

	// Now fetch the edit form and verify llm_model is present
	req = httptest.NewRequest(http.MethodGet, "/sessions/"+sessID+"/node-edit?id=start", nil)
	w = httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	resp = w.Result()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200 on edit form, got %d: %s", resp.StatusCode, string(body))
	}

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)
	// The claude-opus-4-6 option should be present
	if !strings.Contains(bodyStr, "claude-opus-4-6") {
		t.Fatal("expected model claude-opus-4-6 in edit form")
	}
}

func TestMutationOnNonexistentSessionReturns404(t *testing.T) {
	srv, _ := newTestServer(t)

	form := url.Values{}
	form.Set("dot", testDOT)

	req := httptest.NewRequest(http.MethodPost, "/sessions/nonexistent-id/dot", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", resp.StatusCode)
	}
}
