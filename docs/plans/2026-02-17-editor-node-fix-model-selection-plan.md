# Editor Node Click Fix + Model Selection Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix broken node-click-to-edit flow and add per-node model selection to the DOT editor.

**Architecture:** Three independent fixes (graphviz stale DOM, resilient node lookup, debug logging) followed by model selection UI that adds model resolution to the node edit form and a dropdown populated from the LLM catalog.

**Tech Stack:** Go (net/http, html/template), JavaScript (d3-graphviz, htmx), CSS

---

### Task 1: Fix Graphviz Stale DOM Reference After OOB Swap

When htmx OOB swaps replace `#graph-viewer`, the `#graph-container` div is destroyed and recreated. The `graphviz` closure variable in `graph.js` still points at the detached element. This breaks graph rendering after any mutation.

**Files:**
- Modify: `editor/static/js/graph.js:106-154`

**Step 1: Write the fix in graph.js**

In `editor/static/js/graph.js`, add a `resetGraphviz()` function and call it from `renderGraph()` when the container has changed. The key insight: we need to detect when the DOM element the `graphviz` instance is bound to has been detached.

Replace the `renderGraph` function (lines 135-176) with a version that checks for a stale graphviz reference. Also add a `resetGraphviz` helper:

```javascript
// Reset graphviz when the DOM container has been replaced (e.g. by OOB swap)
function resetGraphviz() {
    graphviz = null;
}

// Render the graph from the DOT source
window.renderGraph = function() {
    const dotSource = document.getElementById('dot-source');
    const container = document.getElementById('graph-container');

    if (!dotSource || !container) {
        console.warn('DOT source or container not found');
        return;
    }

    const dot = dotSource.value || dotSource.textContent;
    if (!dot || dot.trim() === '') {
        console.warn('Empty DOT source');
        container.innerHTML = '<p class="no-graph">No graph to display</p>';
        return;
    }

    // Detect stale graphviz: if the container element has been replaced by OOB swap,
    // the graphviz instance references a detached DOM node. Reset and re-init.
    if (graphviz && !document.contains(container)) {
        console.log('Graph container was replaced, resetting graphviz');
        graphviz = null;
    }

    if (!graphviz) {
        initGraphviz();
        return;
    }
    graphviz
        .width(Math.max(320, container.clientWidth || 0))
        .height(Math.max(220, container.clientHeight || 0));

    try {
        graphviz
            .renderDot(dot)
            .on('end', () => {
                console.log('Graph rendered');
                applyNodeTypeClasses(dot);
                attachClickHandlers();
            });
    } catch (err) {
        console.error('Error rendering graph:', err);
        var errorP = document.createElement('p');
        errorP.className = 'error';
        errorP.textContent = 'Error rendering graph: ' + err.message;
        container.innerHTML = '';
        container.appendChild(errorP);
    }
};
```

Also update the `htmx:afterSwap` handler in `editor/static/js/editor.js` (lines 85-96) to explicitly reset graphviz when `graph-viewer` is swapped:

```javascript
// After htmx swaps content, re-render the graph
document.body.addEventListener('htmx:afterSwap', (e) => {
    if (e.detail.target.id === 'graph-viewer') {
        // OOB swap replaced graph-viewer — force graphviz re-init
        if (window.resetGraphviz) {
            window.resetGraphviz();
        }
    }
    // Re-render graph if it was updated
    if (e.detail.target.id === 'editor-panels' ||
        e.detail.target.id === 'graph-viewer' ||
        e.detail.target.closest('#editor-panels')) {
        if (window.renderGraph) {
            window.renderGraph();
        }
    }
});
```

And expose `resetGraphviz` on `window` in graph.js alongside `renderGraph`:

```javascript
window.resetGraphviz = resetGraphviz;
```

**Step 2: Verify manually**

Run: `go test ./editor/... -v -count=1`
Expected: All existing tests PASS (this is a JS-only change; Go tests verify server behavior).

**Step 3: Commit**

```bash
agentjj commit -m "fix(editor): reset graphviz instance when graph container replaced by OOB swap"
```

---

### Task 2: Add Case-Insensitive Node Lookup Fallback

The `findNodeByParam()` function in `editor/handlers.go` only tries exact matches and basic quote stripping. If d3-graphviz produces an SVG title that differs in case from the stored node ID, the lookup fails with a 404.

**Files:**
- Modify: `editor/handlers.go:512-562`
- Modify: `editor/handlers_test.go`

**Step 1: Write failing tests**

Add these tests to `editor/handlers_test.go`:

```go
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
```

**Step 2: Run tests to verify they fail**

Run: `go test ./editor/... -v -run "TestFindNodeByParamCaseInsensitive|TestNodeEditFormCaseInsensitiveLookup" -count=1`
Expected: FAIL — `findNodeByParam` doesn't do case-insensitive matching.

**Step 3: Implement case-insensitive fallback**

In `editor/handlers.go`, update `findNodeByParam` (line 512-519):

```go
func findNodeByParam(nodes map[string]*dot.Node, id string) (*dot.Node, bool) {
	// Exact match with candidate variations (quotes, URL-encoding)
	for _, cand := range elementIDCands(id) {
		if node, ok := nodes[cand]; ok {
			return node, true
		}
	}
	// Case-insensitive fallback: d3-graphviz SVG titles may differ in case
	lower := strings.ToLower(strings.TrimSpace(id))
	if lower != "" {
		for key, node := range nodes {
			if strings.ToLower(key) == lower {
				return node, true
			}
		}
	}
	return nil, false
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./editor/... -v -run "TestFindNodeByParamCaseInsensitive|TestNodeEditFormCaseInsensitiveLookup" -count=1`
Expected: PASS

**Step 5: Run all editor tests**

Run: `go test ./editor/... -v -count=1`
Expected: All PASS

**Step 6: Commit**

```bash
agentjj commit -m "fix(editor): add case-insensitive fallback to node lookup"
```

---

### Task 3: Add Debug Logging to Node Click

Add `console.debug` calls so the node click flow can be diagnosed in the browser console.

**Files:**
- Modify: `editor/static/js/graph.js:220-229`

**Step 1: Add debug logging to handleNodeClick**

In `editor/static/js/graph.js`, update `handleNodeClick` (line 220-229):

```javascript
function handleNodeClick(nodeId, element) {
    clearSelection();
    selectedElement = element;
    element.classList.add('selected');
    const sessionID = document.querySelector('.editor').dataset.sessionId;
    const textEl = element.querySelector('text');
    const nodeLabel = textEl ? String(textEl.textContent || '').trim() : '';
    const query = `id=${encodeURIComponent(nodeId)}${nodeLabel ? `&label=${encodeURIComponent(nodeLabel)}` : ''}`;
    const url = `${editorBasePath()}/sessions/${sessionID}/node-edit?${query}`;
    console.debug('[graph] node click:', { nodeId, nodeLabel, url });
    htmx.ajax('GET', url, {target: '#selection-props', swap: 'innerHTML'});
}
```

Also add logging to `handleEdgeClick` (line 232-238):

```javascript
function handleEdgeClick(edgeId, element) {
    clearSelection();
    selectedElement = element;
    element.classList.add('selected');
    const sessionID = document.querySelector('.editor').dataset.sessionId;
    const url = `${editorBasePath()}/sessions/${sessionID}/edge-edit?id=${encodeURIComponent(edgeId)}`;
    console.debug('[graph] edge click:', { edgeId, url });
    htmx.ajax('GET', url, {target: '#selection-props', swap: 'innerHTML'});
}
```

**Step 2: Run editor tests**

Run: `go test ./editor/... -v -count=1`
Expected: All PASS (JS-only change)

**Step 3: Commit**

```bash
agentjj commit -m "fix(editor): add debug logging to node and edge click handlers"
```

---

### Task 4: Add Model Options to Editor Server

The editor needs a list of known models to populate the model dropdown. Pass model options at construction time.

**Files:**
- Modify: `editor/server.go:15-53`
- Modify: `editor/handlers_test.go:26-31`
- Modify: `web/editor_adapter.go` (where `NewServer` is called)

**Step 1: Write failing test**

Add to `editor/handlers_test.go`:

```go
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
```

**Step 2: Run test to verify it fails**

Run: `go test ./editor/... -v -run "TestNewServerAcceptsModelOptions" -count=1`
Expected: FAIL — `ModelOption` type and `WithModelOptions` function don't exist.

**Step 3: Implement ModelOption and server option**

In `editor/server.go`, add the `ModelOption` type and a functional options pattern:

```go
// ModelOption describes an LLM model available for selection in the editor.
type ModelOption struct {
	ID          string
	DisplayName string
	Provider    string
}

// ServerOption configures optional Server behavior.
type ServerOption func(*Server)

// WithModelOptions sets the list of models available for selection in node edit forms.
func WithModelOptions(models []ModelOption) ServerOption {
	return func(s *Server) {
		s.modelOptions = models
	}
}
```

Add `modelOptions` field to the `Server` struct:

```go
type Server struct {
	router       chi.Router
	store        *Store
	templates    *template.Template
	landingTmpl  *template.Template
	editorTmpl   *template.Template
	modelOptions []ModelOption
}
```

Update `NewServer` signature to accept variadic options:

```go
func NewServer(store *Store, templateDir string, staticDir string, opts ...ServerOption) *Server {
	s := &Server{
		store: store,
	}
	for _, opt := range opts {
		opt(s)
	}
	// ... rest of existing code unchanged ...
```

**Step 4: Update newTestServer**

In `editor/handlers_test.go`, update `newTestServer` (line 26-31) — no change needed since `opts` is variadic and existing callers pass zero options.

**Step 5: Update web/editor_adapter.go**

Find where `editor.NewServer` is called in `web/editor_adapter.go` and pass model options built from `llm.DefaultCatalog()`. Read the file to find the exact location:

```go
// In web/editor_adapter.go or web/server.go, where the editor server is created:
catalog := llm.DefaultCatalog()
var modelOpts []editor.ModelOption
for _, m := range catalog.ListModels("") {
	modelOpts = append(modelOpts, editor.ModelOption{
		ID:          m.ID,
		DisplayName: m.DisplayName,
		Provider:    m.Provider,
	})
}
editorSrv := editor.NewServer(store, templateDir, staticDir, editor.WithModelOptions(modelOpts))
```

**Step 6: Run tests**

Run: `go test ./editor/... -v -count=1 && go test ./web/... -v -count=1`
Expected: All PASS

**Step 7: Commit**

```bash
agentjj commit -m "feat(editor): add model options to editor server via functional option"
```

---

### Task 5: Add Model Resolution to Node Edit Form Handler

When rendering the node edit form, resolve which model applies to the node (from the `model_stylesheet` graph attribute or direct `llm_model` node attribute) and pass it to the template.

**Files:**
- Modify: `editor/server.go:27-32` (NodeEditData struct)
- Modify: `editor/handlers.go:427-453` (renderNodeEditFormByIDOrLabel)
- Create: `editor/model_resolve.go`
- Create: `editor/model_resolve_test.go`

**Step 1: Write failing tests for model resolution**

Create `editor/model_resolve_test.go`:

```go
package editor

import (
	"testing"

	"github.com/2389-research/mammoth/dot"
)

func TestResolveNodeModel_DirectAttribute(t *testing.T) {
	node := &dot.Node{
		ID:    "task_0",
		Attrs: map[string]string{"llm_model": "gpt-5.2"},
	}
	model, source := resolveNodeModel(node, "")
	if model != "gpt-5.2" {
		t.Fatalf("expected model gpt-5.2, got %s", model)
	}
	if source != "node attribute" {
		t.Fatalf("expected source 'node attribute', got %s", source)
	}
}

func TestResolveNodeModel_UniversalStylesheet(t *testing.T) {
	node := &dot.Node{
		ID:    "task_0",
		Attrs: map[string]string{"label": "Do stuff"},
	}
	stylesheet := `* { llm_model: claude-sonnet-4-5; }`
	model, source := resolveNodeModel(node, stylesheet)
	if model != "claude-sonnet-4-5" {
		t.Fatalf("expected model claude-sonnet-4-5, got %s", model)
	}
	if source != "stylesheet (* rule)" {
		t.Fatalf("expected source 'stylesheet (* rule)', got %s", source)
	}
}

func TestResolveNodeModel_IDSelectorOverridesUniversal(t *testing.T) {
	node := &dot.Node{
		ID:    "critical_review",
		Attrs: map[string]string{"label": "Review"},
	}
	stylesheet := `* { llm_model: claude-sonnet-4-5; } #critical_review { llm_model: claude-opus-4-6; }`
	model, source := resolveNodeModel(node, stylesheet)
	if model != "claude-opus-4-6" {
		t.Fatalf("expected model claude-opus-4-6, got %s", model)
	}
	if source != "stylesheet (#critical_review rule)" {
		t.Fatalf("expected source 'stylesheet (#critical_review rule)', got %s", source)
	}
}

func TestResolveNodeModel_ClassSelector(t *testing.T) {
	node := &dot.Node{
		ID:    "task_0",
		Attrs: map[string]string{"class": "code"},
	}
	stylesheet := `* { llm_model: claude-sonnet-4-5; } .code { llm_model: claude-opus-4-6; }`
	model, source := resolveNodeModel(node, stylesheet)
	if model != "claude-opus-4-6" {
		t.Fatalf("expected model claude-opus-4-6, got %s", model)
	}
	if source != "stylesheet (.code rule)" {
		t.Fatalf("expected source 'stylesheet (.code rule)', got %s", source)
	}
}

func TestResolveNodeModel_NoStylesheet(t *testing.T) {
	node := &dot.Node{
		ID:    "task_0",
		Attrs: map[string]string{"label": "Do stuff"},
	}
	model, source := resolveNodeModel(node, "")
	if model != "" {
		t.Fatalf("expected empty model, got %s", model)
	}
	if source != "" {
		t.Fatalf("expected empty source, got %s", source)
	}
}

func TestResolveNodeModel_DirectOverridesStylesheet(t *testing.T) {
	node := &dot.Node{
		ID:    "task_0",
		Attrs: map[string]string{"llm_model": "gpt-5.2"},
	}
	stylesheet := `* { llm_model: claude-sonnet-4-5; }`
	model, source := resolveNodeModel(node, stylesheet)
	if model != "gpt-5.2" {
		t.Fatalf("expected model gpt-5.2, got %s", model)
	}
	if source != "node attribute" {
		t.Fatalf("expected source 'node attribute', got %s", source)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./editor/... -v -run "TestResolveNodeModel" -count=1`
Expected: FAIL — `resolveNodeModel` function doesn't exist.

**Step 3: Implement resolveNodeModel**

Create `editor/model_resolve.go`:

```go
// ABOUTME: Resolves which LLM model applies to a graph node from direct attributes or stylesheet rules.
// ABOUTME: Provides lightweight stylesheet parsing for the editor without importing the attractor package.

package editor

import (
	"fmt"
	"strings"

	"github.com/2389-research/mammoth/dot"
)

// resolveNodeModel determines which LLM model applies to a node.
// It checks (in priority order):
//  1. Direct llm_model attribute on the node (highest priority)
//  2. Stylesheet rules from the graph's model_stylesheet attribute
//
// Returns the model ID and a human-readable source description.
// Returns ("", "") if no model is configured.
func resolveNodeModel(node *dot.Node, stylesheet string) (model string, source string) {
	// Direct attribute takes highest priority
	if m, ok := node.Attrs["llm_model"]; ok && strings.TrimSpace(m) != "" {
		return strings.TrimSpace(m), "node attribute"
	}

	// Parse stylesheet
	if strings.TrimSpace(stylesheet) == "" {
		return "", ""
	}

	rules, err := parseModelStylesheet(stylesheet)
	if err != nil {
		return "", ""
	}

	// Apply rules in specificity order
	var bestModel, bestSelector string
	bestSpec := -1

	for _, rule := range rules {
		if !stylesheetSelectorMatches(rule.selector, node) {
			continue
		}
		if rule.specificity > bestSpec {
			if m, ok := rule.properties["llm_model"]; ok {
				bestModel = m
				bestSelector = rule.selector
				bestSpec = rule.specificity
			}
		}
	}

	if bestModel != "" {
		return bestModel, fmt.Sprintf("stylesheet (%s rule)", bestSelector)
	}
	return "", ""
}

type stylesheetRule struct {
	selector    string
	properties  map[string]string
	specificity int
}

// parseModelStylesheet parses a CSS-like model_stylesheet string.
func parseModelStylesheet(input string) ([]stylesheetRule, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return nil, fmt.Errorf("empty stylesheet")
	}

	var rules []stylesheetRule
	rest := trimmed

	for rest != "" {
		rest = strings.TrimSpace(rest)
		if rest == "" {
			break
		}

		braceIdx := strings.Index(rest, "{")
		if braceIdx < 0 {
			break
		}
		selector := strings.TrimSpace(rest[:braceIdx])
		rest = rest[braceIdx+1:]

		closeBraceIdx := strings.Index(rest, "}")
		if closeBraceIdx < 0 {
			break
		}
		propsStr := rest[:closeBraceIdx]
		rest = rest[closeBraceIdx+1:]

		props := make(map[string]string)
		for _, part := range strings.Split(propsStr, ";") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			colonIdx := strings.Index(part, ":")
			if colonIdx < 0 {
				continue
			}
			key := strings.TrimSpace(part[:colonIdx])
			val := strings.TrimSpace(part[colonIdx+1:])
			if key != "" {
				props[key] = val
			}
		}

		spec := 0
		if strings.HasPrefix(selector, "#") {
			spec = 2
		} else if strings.HasPrefix(selector, ".") {
			spec = 1
		}

		rules = append(rules, stylesheetRule{
			selector:    selector,
			properties:  props,
			specificity: spec,
		})
	}
	return rules, nil
}

// stylesheetSelectorMatches checks if a CSS-like selector matches a node.
func stylesheetSelectorMatches(selector string, node *dot.Node) bool {
	if selector == "*" {
		return true
	}
	if strings.HasPrefix(selector, "#") {
		return node.ID == selector[1:]
	}
	if strings.HasPrefix(selector, ".") {
		className := selector[1:]
		nodeClass := node.Attrs["class"]
		if nodeClass == "" {
			return false
		}
		for _, c := range strings.Split(nodeClass, ",") {
			if strings.TrimSpace(c) == className {
				return true
			}
		}
	}
	return false
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./editor/... -v -run "TestResolveNodeModel" -count=1`
Expected: All PASS

**Step 5: Update NodeEditData struct**

In `editor/server.go`, update `NodeEditData` (line 27-32):

```go
type NodeEditData struct {
	SessionID     string
	BasePath      string
	NodeID        string
	Node          *dot.Node
	ResolvedModel string
	ModelSource   string
	ModelOptions  []ModelOption
}
```

**Step 6: Update renderNodeEditFormByIDOrLabel**

In `editor/handlers.go`, update `renderNodeEditFormByIDOrLabel` (line 446-452) to compute model info:

```go
	stylesheet := ""
	if sess.Graph.Attrs != nil {
		stylesheet = sess.Graph.Attrs["model_stylesheet"]
	}
	resolvedModel, modelSource := resolveNodeModel(node, stylesheet)

	data := NodeEditData{
		SessionID:     sessionID,
		BasePath:      basePathFromRequest(r),
		NodeID:        node.ID,
		Node:          node,
		ResolvedModel: resolvedModel,
		ModelSource:   modelSource,
		ModelOptions:  s.modelOptions,
	}
	s.renderPartial(w, "node_edit_form", data, http.StatusOK)
```

**Step 7: Run all tests**

Run: `go test ./editor/... -v -count=1`
Expected: All PASS

**Step 8: Commit**

```bash
agentjj commit -m "feat(editor): add model resolution for node edit form"
```

---

### Task 6: Update Node Edit Form Template With Model Dropdown

Add a "Model" section to the node edit form that shows the resolved model and provides a dropdown to override it.

**Files:**
- Modify: `editor/templates/partials/node_edit_form.html`

**Step 1: Update the template**

Replace the contents of `editor/templates/partials/node_edit_form.html`:

```html
{{define "node_edit_form"}}
<div class="edit-form">
    <h4>Node: {{.NodeID}}</h4>
    <form hx-post="{{.BasePath}}/sessions/{{.SessionID}}/nodes/{{urlquery .NodeID}}" hx-target="#editor-panels" hx-swap="none">
        {{/* Model selection section */}}
        <div class="form-group model-section">
            <label>Model</label>
            {{if and .ResolvedModel (not (index .Node.Attrs "llm_model"))}}
            <p class="hint model-hint">Resolves to: {{.ResolvedModel}} ({{.ModelSource}})</p>
            {{end}}
            <select name="attr_llm_model" class="form-input">
                <option value=""{{if not (index .Node.Attrs "llm_model")}} selected{{end}}>Use stylesheet default</option>
                {{range .ModelOptions}}
                <option value="{{.ID}}"{{if eq .ID (index $.Node.Attrs "llm_model")}} selected{{end}}>{{.DisplayName}} ({{.Provider}})</option>
                {{end}}
            </select>
        </div>
        {{range $k, $v := .Node.Attrs}}
        {{if ne $k "llm_model"}}
        <div class="form-group">
            <label>{{$k}}</label>
            <input name="attr_{{$k}}" value="{{$v}}" class="form-input">
        </div>
        {{end}}
        {{end}}
        <!-- new attr key/value pair -->
        <div class="form-group new-attr-group">
            <label>Add attribute</label>
            <div class="new-attr-row">
                <input name="new_attr_key" placeholder="key" class="form-input">
                <input name="new_attr_value" placeholder="value" class="form-input">
            </div>
        </div>
        <div class="form-actions">
            <button type="submit" class="btn-primary">Save</button>
            <button type="button" class="btn-danger"
                    hx-delete="{{.BasePath}}/sessions/{{.SessionID}}/nodes/{{urlquery .NodeID}}"
                    hx-target="#editor-panels" hx-swap="none"
                    hx-confirm="Delete node {{.NodeID}}?">Delete</button>
        </div>
    </form>
</div>
{{end}}
```

Key changes:
- `llm_model` is excluded from the generic attribute loop (shown in dropdown instead)
- Dropdown shows "Use stylesheet default" + all catalog models
- When no direct `llm_model` attr, shows hint with resolved model from stylesheet
- Selected state matches current `llm_model` attr value

**Step 2: Add CSS for model section**

In `editor/static/css/style.css`, add at the end:

```css
/* Model selection */
.model-section {
    border-bottom: 1px solid var(--border);
    padding-bottom: 0.75rem;
    margin-bottom: 0.5rem;
}
.model-hint {
    font-size: 0.8rem;
    margin: 0.25rem 0;
    opacity: 0.7;
}
```

**Step 3: Write a test for template rendering with model data**

Add to `editor/handlers_test.go`:

```go
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
```

**Step 4: Run tests**

Run: `go test ./editor/... -v -count=1`
Expected: All PASS

**Step 5: Commit**

```bash
agentjj commit -m "feat(editor): add model selection dropdown to node edit form"
```

---

### Task 7: Wire Model Catalog Into Web Server

Connect the web layer's editor initialization to pass the LLM catalog models.

**Files:**
- Modify: `web/server.go` or `web/editor_adapter.go` (wherever `editor.NewServer` is called)

**Step 1: Find and read the editor creation code**

Read `web/server.go` and `web/editor_adapter.go` to find where `editor.NewServer` is called.

**Step 2: Pass model options**

Where the editor server is created, add:

```go
import "github.com/2389-research/mammoth/llm"

// Build model options from catalog
catalog := llm.DefaultCatalog()
var modelOpts []editor.ModelOption
for _, m := range catalog.ListModels("") {
    modelOpts = append(modelOpts, editor.ModelOption{
        ID:          m.ID,
        DisplayName: m.DisplayName,
        Provider:    m.Provider,
    })
}
```

Pass `editor.WithModelOptions(modelOpts)` to `editor.NewServer(...)`.

**Step 3: Run all tests**

Run: `go test ./editor/... ./web/... -v -count=1`
Expected: All PASS

**Step 4: Commit**

```bash
agentjj commit -m "feat(web): wire LLM catalog models into editor server"
```

---

### Task 8: Integration Test — Full Node Edit Flow

Verify the entire flow works: create session with model_stylesheet, fetch node edit form, verify model dropdown and resolved model are present.

**Files:**
- Modify: `editor/handlers_test.go` (or `web/server_test.go`)

**Step 1: Write integration test**

Add to `editor/handlers_test.go`:

```go
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

	// Now fetch the edit form and verify llm_model is selected
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
	// The claude-opus-4-6 option should have "selected" attribute
	if !strings.Contains(bodyStr, "claude-opus-4-6") {
		t.Fatal("expected model claude-opus-4-6 in edit form")
	}
}
```

**Step 2: Run the test**

Run: `go test ./editor/... -v -run "TestNodeEditFormModelOverrideRoundTrip" -count=1`
Expected: PASS

**Step 3: Run full test suite**

Run: `go test ./editor/... ./web/... ./dot/... -v -count=1`
Expected: All PASS

**Step 4: Commit**

```bash
agentjj commit -m "test(editor): add integration test for model override round-trip"
```
