# Editor Node Click Fix + Model Selection Design

**Goal:** Fix the broken node-click-to-edit flow in the DOT editor and add per-node model selection.

**Context:** The editor is the middle step in the mammoth wizard (spec → editor → build). Clicking a node in the graph viewer shows "Could not load properties" (a 404 from the server). Additionally, there is no UI for selecting which LLM model a node should use — models are only configurable via the `model_stylesheet` graph attribute.

---

## Problem Analysis

### Bug: Node Click Returns 404

**Flow:** SVG `<title>` → `normalizeElementId()` → `handleNodeClick()` → htmx GET `/node-edit?id={nodeId}` → proxy strips prefix → editor `handleNodeEditFormByQuery()` → `findNodeByParam()` → 404.

Server-side tests pass with programmatic IDs. The bug manifests immediately on page open in the browser, suggesting a mismatch between what d3-graphviz puts in SVG `<title>` elements and what the server expects.

### Bug: Graphviz Stale Reference After OOB Swap

When htmx OOB swaps replace `#graph-viewer`, the `#graph-container` div is destroyed and recreated. The `graphviz` closure variable in `graph.js` still references the old (detached) DOM element. After any mutation (save, undo, redo, edit DOT), the graph fails to re-render.

### Missing Feature: Model Selection

Models are only configurable via `model_stylesheet` (CSS-like syntax at the graph level). The editor has no per-node model UI. Users should be able to select a model for individual nodes.

---

## Design

### 1. Graph.js — Stale Reference Fix

Listen for htmx `afterSwap` events. When `#graph-viewer` is swapped, re-initialize the d3-graphviz instance.

```javascript
document.addEventListener('htmx:afterSwap', (evt) => {
    if (evt.detail.target.id === 'graph-viewer' ||
        evt.detail.target.querySelector('#graph-container')) {
        graphviz = null;
        initGraphviz();
    }
});
```

Add a guard in `renderGraph()`: if `graphviz` is null or the container is detached, re-init before rendering.

### 2. Node Click — Debug Logging + Resilient Lookup

**Client-side (graph.js):**
- Add `console.debug` in `handleNodeClick` showing: raw title text, normalized ID, request URL.

**Server-side (handlers.go):**
- Expand `elementIDCands()` to also try lowercase variants.
- Add case-insensitive fallback in `findNodeByParam()`: if exact match fails, iterate all nodes comparing lowercased keys.
- Return a more helpful 404 response body including the searched ID and available node IDs (for debugging).

```go
func findNodeByParam(nodes map[string]*dot.Node, id string) (*dot.Node, bool) {
    for _, cand := range elementIDCands(id) {
        if node, ok := nodes[cand]; ok {
            return node, true
        }
    }
    lower := strings.ToLower(id)
    for key, node := range nodes {
        if strings.ToLower(key) == lower {
            return node, true
        }
    }
    return nil, false
}
```

### 3. Model Selection UI

**Template (`node_edit_form.html`):**
Add a "Model" section to the node edit form showing:
- Resolved model (read-only hint): e.g. "Resolves to: claude-sonnet-4-5 (from * rule)"
- Override dropdown with common models from catalog + text input for custom
- Clear override button that removes the per-node `llm_model` attribute

**Data flow:**
```
User picks model in dropdown
  → htmx POST to /sessions/{id}/nodes/{nodeId}/attrs
  → Sets llm_model="selected-model" on the node
  → Node re-serialized to DOT
  → OOB swap updates graph + code editor + properties panel
```

**Model resolution:** Parse the `model_stylesheet` graph attribute using existing `attractor/stylesheet.go` logic to show what model currently resolves for the node. Per-node `llm_model` attribute takes highest priority (equivalent to `#nodeId` selector).

**Catalog:** Use `llm/catalog.go` known models list to populate the dropdown options.

### 4. Out of Scope

- No stylesheet editor UI (separate feature)
- No DOT parsing/serialization changes (round-trip is solid)
- No provider selection (provider resolved from model)
- No changes to the stylesheet cascade system
