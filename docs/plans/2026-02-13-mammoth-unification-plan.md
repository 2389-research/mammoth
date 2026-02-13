# Mammoth Unification Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Merge mammoth-specd and mammoth-dot-editor into mammoth-dev as a single binary with wizard-flow web UI.

**Architecture:** Five-phase migration: (1) consolidate DOT parser, (2) swap LLM to mux/llm, (3) import spec builder, (4) import editor, (5) build unified web layer. Each phase leaves the existing CLI/TUI working.

**Tech Stack:** Go 1.25+, mux/llm (LLM client), Chi v5 (HTTP router), HTMX (web UI), d3-graphviz (visualization), Bubble Tea (TUI), SQLite (spec persistence)

**Source repos:**
- mammoth-dev: `/Users/harper/Public/src/2389/mammoth-dev` (module: `github.com/2389-research/mammoth`)
- mammoth-dot-editor: `/Users/harper/Public/src/2389/mammoth-dot-editor` (module: `github.com/2389-research/mammoth-dot-editor`)
- mammoth-specd: `/Users/harper/Public/src/2389/mammoth-specd` (module: `github.com/2389-research/mammoth-specd`)

---

## Phase 1: Consolidate DOT Parser

The editor's DOT parser (`mammoth-dot-editor/internal/dot/`) and mammoth's parser (`mammoth-dev/attractor/lexer.go`, `parser.go`, `ast.go`) get merged into a new top-level `dot/` package. The editor's parser has richer validation (22+ lint rules); mammoth's has tighter runtime integration. We take the union.

### Task 1.1: Create `dot/` Package with AST Types

**Files:**
- Create: `dot/ast.go`
- Create: `dot/ast_test.go`
- Reference: `attractor/ast.go` (mammoth's AST)
- Reference: `/Users/harper/Public/src/2389/mammoth-dot-editor/internal/dot/ast.go` (editor's AST)

**Step 1: Write the failing test**

Write `dot/ast_test.go` with tests for Graph, Node, Edge, Subgraph construction. Use the union of fields from both ASTs. Key differences to merge:
- Editor has `Diagnostic` type and `AssignEdgeIDs()` on Graph
- Mammoth has `Defaults` (NodeDefaults, EdgeDefaults as `map[string]string`)
- Both have `Graph.Name`, `Graph.Attrs`, `Node.ID`, `Node.Attrs`, `Edge.From`, `Edge.To`, `Edge.Attrs`

```go
// dot/ast_test.go
package dot

import "testing"

func TestGraphConstruction(t *testing.T) {
    g := &Graph{Name: "test"}
    g.AddNode(&Node{ID: "a", Attrs: map[string]string{"label": "A"}})
    g.AddNode(&Node{ID: "b", Attrs: map[string]string{"label": "B"}})
    g.AddEdge(&Edge{From: "a", To: "b"})

    if len(g.Nodes) != 2 { t.Fatalf("expected 2 nodes, got %d", len(g.Nodes)) }
    if len(g.Edges) != 1 { t.Fatalf("expected 1 edge, got %d", len(g.Edges)) }
}

func TestAssignEdgeIDs(t *testing.T) {
    g := &Graph{Name: "test"}
    g.AddEdge(&Edge{From: "a", To: "b"})
    g.AddEdge(&Edge{From: "a", To: "b"})
    g.AssignEdgeIDs()

    if g.Edges[0].ID != "a->b" { t.Fatalf("expected a->b, got %s", g.Edges[0].ID) }
    if g.Edges[1].ID != "a->b#1" { t.Fatalf("expected a->b#1, got %s", g.Edges[1].ID) }
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./dot/ -v -run TestGraph`
Expected: FAIL (package does not exist)

**Step 3: Write minimal implementation**

Create `dot/ast.go` merging both ASTs. Start from the editor's AST (it has more features like `AssignEdgeIDs`, `Diagnostic`) and add any fields from mammoth's AST that are missing. Include ABOUTME comment.

Key types to define: `Graph`, `Node`, `Edge`, `Subgraph`, `Diagnostic`, `Severity`

**Step 4: Run test to verify it passes**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./dot/ -v`
Expected: PASS

**Step 5: Commit**

```bash
agentjj commit -m "feat(dot): add consolidated AST types for unified DOT parser"
```

---

### Task 1.2: Create `dot/` Lexer

**Files:**
- Create: `dot/lexer.go`
- Create: `dot/lexer_test.go`
- Reference: `attractor/lexer.go`
- Reference: `/Users/harper/Public/src/2389/mammoth-dot-editor/internal/dot/lexer.go`

**Step 1: Write the failing test**

Port tests from both lexers. The editor's lexer tracks line/column positions (better error reporting). Include tests for: keywords (digraph, graph, subgraph, node, edge), strings (quoted, with escapes), punctuation ({, }, [, ], ;, =, ->), comments (// and /* */), identifiers, numbers.

**Step 2: Run test to verify it fails**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./dot/ -v -run TestLex`
Expected: FAIL

**Step 3: Write minimal implementation**

Use the editor's lexer as the base (has line/col tracking). Add any token types from mammoth's lexer that are missing. Key: the lexer must produce the same token stream both parsers expect.

**Step 4: Run test to verify it passes**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./dot/ -v -run TestLex`
Expected: PASS

**Step 5: Commit**

```bash
agentjj commit -m "feat(dot): add lexer with line/column tracking"
```

---

### Task 1.3: Create `dot/` Parser

**Files:**
- Create: `dot/parser.go`
- Create: `dot/parser_test.go`
- Reference: `attractor/parser.go`
- Reference: `/Users/harper/Public/src/2389/mammoth-dot-editor/internal/dot/parser.go`

**Step 1: Write the failing test**

Port parser tests from both codebases. Test: simple digraph, nodes with attrs, edges with attrs, chained edges (a -> b -> c), subgraphs, defaults (node [...], edge [...]), graph-level attrs, error cases (invalid syntax, missing braces).

**Step 2: Run test to verify it fails**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./dot/ -v -run TestParse`
Expected: FAIL

**Step 3: Write minimal implementation**

Use mammoth's parser as the base (tighter integration with execution engine). Add the editor's improvements: better error messages with line/col, support for `AssignEdgeIDs()` after parse.

Public API: `Parse(input string) (*Graph, error)`

**Step 4: Run test to verify it passes**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./dot/ -v -run TestParse`
Expected: PASS

**Step 5: Commit**

```bash
agentjj commit -m "feat(dot): add recursive descent parser"
```

---

### Task 1.4: Create `dot/` Serializer

**Files:**
- Create: `dot/serializer.go`
- Create: `dot/serializer_test.go`
- Reference: `/Users/harper/Public/src/2389/mammoth-dot-editor/internal/dot/serializer.go`

**Step 1: Write the failing test**

Test round-trip: `Parse(input) → Serialize(graph) → Parse(result)` should produce equivalent AST. Test color-coding logic: start=green, exit=red, codergen=blue, conditional=yellow, human=purple.

**Step 2: Run test to verify it fails**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./dot/ -v -run TestSerialize`
Expected: FAIL

**Step 3: Write minimal implementation**

Port the editor's serializer (has color-coding). Public API: `Serialize(g *Graph) string`

**Step 4: Run test to verify it passes**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./dot/ -v -run TestSerialize`
Expected: PASS

**Step 5: Commit**

```bash
agentjj commit -m "feat(dot): add serializer with color-coding"
```

---

### Task 1.5: Create `dot/validator/` Package

**Files:**
- Create: `dot/validator/lint.go`
- Create: `dot/validator/lint_test.go`
- Reference: `/Users/harper/Public/src/2389/mammoth-dot-editor/internal/validator/lint.go` (22+ rules)
- Reference: `attractor/validate.go` (runtime validation rules)

**Step 1: Write the failing test**

Port all validation tests from both codebases. The editor has 22+ lint rules. Mammoth's `validate.go` has runtime-specific checks (e.g., handler compatibility, condition expression validity). Union them all.

Key rules to test:
- Exactly one start node (Mdiamond), at least one exit (Msquare)
- All nodes reachable from start
- No incoming edges to start, no outgoing from exit
- No self-loops
- Valid shapes
- Codergen nodes should have `prompt` attribute
- Valid conditions: `outcome=SUCCESS`, `outcome=FAIL`
- `max_retries` >= 0
- `goal_gate=true` only on codergen nodes
- `goal` attribute recommended on graph

**Step 2: Run test to verify it fails**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./dot/validator/ -v`
Expected: FAIL

**Step 3: Write minimal implementation**

Port the editor's `Lint()` function, then add mammoth's runtime rules. Public API: `Lint(g *dot.Graph) []dot.Diagnostic`

**Step 4: Run test to verify it passes**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./dot/validator/ -v`
Expected: PASS

**Step 5: Commit**

```bash
agentjj commit -m "feat(dot/validator): add unified lint rules from editor and runtime"
```

---

### Task 1.6: Wire `attractor/` to Use New `dot/` Package

**Files:**
- Modify: `attractor/engine.go` (change import from local parser to `dot/`)
- Modify: `attractor/validate.go` (delegate to `dot/validator/`)
- Modify: all `attractor/*.go` files that reference `attractor.Graph`, `attractor.Node`, etc.
- Delete (eventually): `attractor/lexer.go`, `attractor/parser.go`, `attractor/ast.go`

**Step 1: Run existing tests to establish baseline**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./attractor/ -v -count=1`
Expected: All PASS (baseline)

**Step 2: Update imports and type references**

Change `attractor` package to import `github.com/2389-research/mammoth/dot` and use `dot.Graph`, `dot.Node`, `dot.Edge`, `dot.Subgraph` instead of local types. This is a mechanical replacement.

Where `attractor.Parse()` was called, call `dot.Parse()` instead.
Where `attractor.Validate()` was called, delegate to `dot.Validate()` (or keep a thin wrapper).

**Step 3: Run tests to verify nothing broke**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./attractor/ -v -count=1`
Expected: All PASS

**Step 4: Remove old parser files**

Delete `attractor/lexer.go`, `attractor/parser.go`, `attractor/ast.go` and their test files. Keep `attractor/validate.go` as a thin wrapper if it has runtime-specific logic beyond what `dot/validator/` provides.

**Step 5: Run full test suite**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./... -count=1`
Expected: All PASS

**Step 6: Commit**

```bash
agentjj commit -m "refactor(attractor): use consolidated dot/ parser package"
```

---

## Phase 2: Swap LLM to mux/llm

Replace mammoth's `llm/` package with the `mux/llm` external dependency. The `agent/` package and `attractor/` backends need to adapt.

### Task 2.1: Add mux/llm Dependency

**Files:**
- Modify: `go.mod`

**Step 1: Add the dependency**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go get github.com/2389-research/mux@latest`

**Step 2: Verify it resolves**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go mod tidy`
Expected: No errors, `go.sum` updated

**Step 3: Commit**

```bash
agentjj commit -m "chore: add mux/llm dependency"
```

---

### Task 2.2: Create Adapter Layer from mux/llm to mammoth's Agent

**Files:**
- Create: `llm/mux_adapter.go`
- Create: `llm/mux_adapter_test.go`
- Reference: `llm/client.go` (current Client interface)
- Reference: `llm/provider.go` (current ProviderAdapter interface)
- Reference: mammoth-specd's usage of mux in `internal/agent/swarm.go`

The agent loop (`agent/loop.go`) calls `llm.Client` methods. We need an adapter that wraps `mux/llm.Client` with the same interface mammoth's agent expects, OR we change the agent to use mux directly.

**Step 1: Analyze the interface boundary**

Read `agent/loop.go:ProcessInput()` and catalog every `llm.Client` method it calls. Read `agent/session.go` for the message types used. Document the mapping between mammoth `llm.Message` and mux `llm.Message`.

**Step 2: Write adapter tests**

Test that the adapter correctly translates: requests, responses, messages (with content parts), tool calls, tool results, streaming events.

**Step 3: Implement adapter**

Create `llm/mux_adapter.go` that wraps `mux/llm.Client` and exposes the interface the agent expects. This lets us migrate incrementally without changing every callsite at once.

**Step 4: Run tests**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./llm/ -v -run TestMuxAdapter`
Expected: PASS

**Step 5: Commit**

```bash
agentjj commit -m "feat(llm): add mux/llm adapter for agent compatibility"
```

---

### Task 2.3: Wire Agent to Use mux/llm via Adapter

**Files:**
- Modify: `agent/loop.go`
- Modify: `agent/session.go`
- Modify: `cmd/mammoth/main.go` (client construction)

**Step 1: Run existing agent tests**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./agent/ -v -count=1`
Expected: All PASS (baseline)

**Step 2: Replace client construction in main.go**

Change `cmd/mammoth/main.go` to construct a `mux/llm.Client` and wrap it with the adapter from Task 2.2.

**Step 3: Run agent tests**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./agent/ -v -count=1`
Expected: All PASS

**Step 4: Run full test suite**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./... -count=1`
Expected: All PASS

**Step 5: Commit**

```bash
agentjj commit -m "refactor(agent): wire agent loop to mux/llm via adapter"
```

---

### Task 2.4: Deprecate Old llm/ Package Internals

**Files:**
- Modify: Various files in `llm/`

After the adapter is working, mark the old provider implementations (openai.go, anthropic.go, gemini.go) as deprecated. Don't delete yet - they may still be used by tests or other code paths. The full removal happens after all consumers are migrated.

**Step 1: Add deprecation comments to old providers**

**Step 2: Run full test suite to ensure nothing uses deleted code paths**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./... -count=1`
Expected: All PASS

**Step 3: Commit**

```bash
agentjj commit -m "chore(llm): deprecate old provider implementations in favor of mux/llm"
```

---

## Phase 3: Import Spec Builder

Bring mammoth-specd's internals into a new `spec/` package.

### Task 3.1: Create `spec/` Package Structure

**Files:**
- Create: `spec/` directory structure mirroring specd's `internal/`
- Source: `/Users/harper/Public/src/2389/mammoth-specd/internal/core/` → `spec/core/`
- Source: `/Users/harper/Public/src/2389/mammoth-specd/internal/agent/` → `spec/agents/`
- Source: `/Users/harper/Public/src/2389/mammoth-specd/internal/store/` → `spec/store/`

**Step 1: Copy core data model**

Copy from mammoth-specd:
- `internal/core/model.go` → `spec/core/model.go`
- `internal/core/events.go` → `spec/core/events.go`
- `internal/core/commands.go` → `spec/core/commands.go`
- `internal/core/state.go` → `spec/core/state.go`
- `internal/core/actor.go` → `spec/core/actor.go`

Update package declarations from `core` to `core` (under `spec/core`).
Update module import paths from `github.com/2389-research/mammoth-specd/internal/core` to `github.com/2389-research/mammoth/spec/core`.

**Step 2: Copy tests**

Copy all `*_test.go` files alongside.

**Step 3: Fix imports and compile**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go build ./spec/core/`
Expected: Compiles

**Step 4: Run tests**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./spec/core/ -v`
Expected: All PASS

**Step 5: Commit**

```bash
agentjj commit -m "feat(spec/core): import spec builder core model from mammoth-specd"
```

---

### Task 3.2: Import Agent Swarm

**Files:**
- Create: `spec/agents/` directory
- Source: `/Users/harper/Public/src/2389/mammoth-specd/internal/agent/` → `spec/agents/`

**Step 1: Copy agent files**

Copy from mammoth-specd:
- `internal/agent/roles.go` → `spec/agents/roles.go`
- `internal/agent/prompts.go` → `spec/agents/prompts.go`
- `internal/agent/swarm.go` → `spec/agents/swarm.go`
- `internal/agent/context.go` → `spec/agents/context.go`
- `internal/agent/import.go` → `spec/agents/import.go`
- `internal/agent/tools/` → `spec/agents/tools/`

**Step 2: Fix imports**

Update all imports from `mammoth-specd` module to `mammoth` module. The agents already use `mux/llm` so those imports stay as-is.

**Step 3: Compile and test**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./spec/agents/ -v`
Expected: All PASS

**Step 4: Commit**

```bash
agentjj commit -m "feat(spec/agents): import agent swarm from mammoth-specd"
```

---

### Task 3.3: Import Spec Persistence (Store)

**Files:**
- Create: `spec/store/` directory
- Source: `/Users/harper/Public/src/2389/mammoth-specd/internal/store/` → `spec/store/`

**Step 1: Copy store files**

Copy JSONL event log writer, SQLite snapshot store, recovery logic.

**Step 2: Add SQLite dependency**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go get github.com/mattn/go-sqlite3`

**Step 3: Fix imports, compile, test**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./spec/store/ -v`
Expected: All PASS

**Step 4: Commit**

```bash
agentjj commit -m "feat(spec/store): import spec persistence from mammoth-specd"
```

---

### Task 3.4: Replace Spec's DOT Export with Dynamic Generation

**Files:**
- Create: `spec/export/dot.go`
- Create: `spec/export/dot_test.go`
- Reference: `/Users/harper/Public/src/2389/mammoth-specd/internal/core/export/dot.go` (old fixed template)
- Reference: `dot/ast.go` (new consolidated AST)

This is where the fixed 10-node template becomes dynamic. The new `ExportDOT` builds pipeline topology based on spec structure.

**Step 1: Write failing tests**

Test cases:
- Spec with 3 sequential tasks → linear pipeline (start → a → b → c → exit)
- Spec with 2 independent tasks → parallel branch (start → fork → {a, b} → join → exit)
- Spec with risk card → adds verification gate
- Spec with open question → adds human gate
- Empty spec → minimal pipeline (start → implement → exit)

**Step 2: Run tests to verify they fail**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./spec/export/ -v`
Expected: FAIL

**Step 3: Implement dynamic DOT generation**

The export function:
1. Reads cards from SpecState, grouped by type and lane
2. Builds a `dot.Graph` using the new AST
3. Maps spec patterns to pipeline patterns (per design doc table)
4. Validates the generated graph using `dot/validator/`
5. Serializes to DOT string

Public API: `ExportDOT(state *core.SpecState) (string, error)`

**Step 4: Run tests**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./spec/export/ -v`
Expected: All PASS

**Step 5: Commit**

```bash
agentjj commit -m "feat(spec/export): dynamic DOT generation from spec structure"
```

---

### Task 3.5: Import Spec Web Handlers (HTMX Templates)

**Files:**
- Create: `spec/web/` directory
- Source: `/Users/harper/Public/src/2389/mammoth-specd/internal/server/web/` → `spec/web/`
- Source: `/Users/harper/Public/src/2389/mammoth-specd/web/templates/` → `spec/web/templates/`

**Step 1: Copy handler files and templates**

Copy the HTMX handlers for: spec creation, kanban board, card CRUD, agent control, SSE streaming, chat.

**Step 2: Fix imports**

Update module paths. Templates may need path adjustments for embed directives.

**Step 3: Compile**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go build ./spec/web/`
Expected: Compiles

**Step 4: Commit**

```bash
agentjj commit -m "feat(spec/web): import spec builder web handlers and templates"
```

---

## Phase 4: Import Editor

Bring mammoth-dot-editor's internals into a new `editor/` package.

### Task 4.1: Import Editor Session and Handlers

**Files:**
- Create: `editor/session.go`
- Create: `editor/session_test.go`
- Create: `editor/handlers.go`
- Create: `editor/handlers_test.go`
- Source: `/Users/harper/Public/src/2389/mammoth-dot-editor/internal/session/session.go`
- Source: `/Users/harper/Public/src/2389/mammoth-dot-editor/internal/server/handlers.go`

**Step 1: Copy session management**

The editor's `Session` struct holds: Graph, RawDOT, Diagnostics, UndoStack, RedoStack. Copy it, but change it to use `dot.Graph` from the new consolidated package instead of `internal/dot.Graph`.

**Step 2: Copy HTTP handlers**

The editor's handlers implement: DOT update, node CRUD, edge CRUD, graph attrs, undo/redo, export. Copy them, updating imports to use the new `dot/` package.

**Step 3: Fix imports, compile, test**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./editor/ -v`
Expected: All PASS

**Step 4: Commit**

```bash
agentjj commit -m "feat(editor): import DOT editor session and handlers"
```

---

### Task 4.2: Import Editor Templates and Static Assets

**Files:**
- Create: `editor/templates/` directory
- Create: `editor/static/` directory
- Source: `/Users/harper/Public/src/2389/mammoth-dot-editor/web/templates/` → `editor/templates/`
- Source: `/Users/harper/Public/src/2389/mammoth-dot-editor/web/static/` → `editor/static/`

**Step 1: Copy templates**

The editor has HTMX templates for: landing page, three-panel editor, property panel, diagnostics bar, node/edge edit forms.

**Step 2: Copy static assets**

JS files: `editor.js` (keyboard shortcuts), `graph.js` (d3-graphviz integration, SVG interaction).

**Step 3: Update embed directives**

Ensure Go `embed` directives point to correct relative paths within the `editor/` package.

**Step 4: Compile**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go build ./editor/`
Expected: Compiles

**Step 5: Commit**

```bash
agentjj commit -m "feat(editor): import templates and static assets"
```

---

## Phase 5: Unified Web Layer

Build the `web/` package that orchestrates the wizard flow and adds the `mammoth serve` command.

### Task 5.1: Create Project Data Model

**Files:**
- Create: `web/project.go`
- Create: `web/project_test.go`

**Step 1: Write failing tests**

Test: project creation, project with spec phase, project with DOT only, project with runs, project persistence to disk.

**Step 2: Run tests to verify they fail**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./web/ -v -run TestProject`
Expected: FAIL

**Step 3: Implement Project type**

```go
// web/project.go
type Project struct {
    ID          string
    Name        string
    CreatedAt   time.Time
    Spec        *core.SpecState   // optional, nil if started from DOT upload
    DOT         string
    Graph       *dot.Graph
    Diagnostics []dot.Diagnostic
    EditHistory []string          // undo stack
    Runs        []attractor.RunState
    ActiveRun   *attractor.RunState
}
```

Persistence: JSON metadata + delegates to spec/store for spec state, existing runstate for runs.

**Step 4: Run tests**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./web/ -v -run TestProject`
Expected: PASS

**Step 5: Commit**

```bash
agentjj commit -m "feat(web): add Project data model"
```

---

### Task 5.2: Create Web Server and Router

**Files:**
- Create: `web/server.go`
- Create: `web/server_test.go`

**Step 1: Write failing tests**

Test: server starts, health endpoint responds, project list endpoint works, project create returns redirect.

**Step 2: Run tests to verify they fail**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./web/ -v -run TestServer`
Expected: FAIL

**Step 3: Implement server**

Chi router with route groups:

```go
r := chi.NewRouter()
r.Get("/", s.handleHome)
r.Get("/health", s.handleHealth)

r.Route("/projects", func(r chi.Router) {
    r.Get("/", s.handleProjectList)
    r.Get("/new", s.handleProjectNew)
    r.Post("/", s.handleProjectCreate)

    r.Route("/{projectID}", func(r chi.Router) {
        r.Get("/", s.handleProjectOverview)
        r.Get("/validate", s.handleValidate)

        // Spec builder (delegates to spec/web handlers)
        r.Mount("/spec", s.specRouter())

        // DOT editor (delegates to editor handlers)
        r.Mount("/editor", s.editorRouter())

        // Build runner
        r.Post("/build/start", s.handleBuildStart)
        r.Get("/build", s.handleBuildView)
        r.Get("/build/events", s.handleBuildEvents)
        r.Post("/build/stop", s.handleBuildStop)
    })
})
```

**Step 4: Run tests**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./web/ -v -run TestServer`
Expected: PASS

**Step 5: Commit**

```bash
agentjj commit -m "feat(web): add unified HTTP server with wizard flow routing"
```

---

### Task 5.3: Create Wizard Flow Templates

**Files:**
- Create: `web/templates/layout.html` (shared layout with nav)
- Create: `web/templates/home.html` (landing page)
- Create: `web/templates/project_new.html` (new project form)
- Create: `web/templates/project_overview.html` (wizard progress)

**Step 1: Design shared layout**

The layout includes:
- Top nav bar with mammoth logo, project name, wizard progress indicator
- Wizard steps: Spec → Edit → Build (with clickable navigation)
- Content area that loads phase-specific views

**Step 2: Create home page**

Three entry paths:
- "New from Idea" → project creation form (title, one-liner, goal)
- "Upload DOT" → file upload + textarea paste
- Recent projects list

**Step 3: Create project overview**

Shows current phase, links to each phase, project metadata.

**Step 4: Test templates render**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./web/ -v -run TestTemplate`
Expected: PASS (templates parse without error)

**Step 5: Commit**

```bash
agentjj commit -m "feat(web): add wizard flow templates (layout, home, project)"
```

---

### Task 5.4: Wire Spec → Editor Transition

**Files:**
- Modify: `web/server.go`
- Create: `web/transitions.go`
- Create: `web/transitions_test.go`

**Step 1: Write failing test**

Test: when spec's DotGenerator produces DOT, the transition handler creates an editor session from that DOT and redirects to the editor view.

**Step 2: Implement transition logic**

When the spec builder's agent swarm finishes (or user clicks "Proceed to Editor"):
1. Call `spec/export.ExportDOT(specState)` to generate DOT
2. Parse with `dot.Parse()` to get Graph
3. Validate with `dot.Lint()`
4. Store DOT + Graph + Diagnostics on the Project
5. Redirect to `/projects/{id}/editor`

Also handle "Build Now" shortcut:
1. Same as above, but if Diagnostics has no errors, redirect to `/projects/{id}/build/start`
2. If errors, redirect to editor with diagnostics

**Step 3: Run tests**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./web/ -v -run TestTransition`
Expected: PASS

**Step 4: Commit**

```bash
agentjj commit -m "feat(web): wire spec-to-editor and build-now transitions"
```

---

### Task 5.5: Wire Editor → Build Transition

**Files:**
- Modify: `web/server.go`
- Modify: `web/transitions.go`

**Step 1: Write failing test**

Test: when user clicks "Build" in editor, the handler validates current DOT, creates an attractor engine run, and redirects to build view with SSE event stream.

**Step 2: Implement build trigger**

When user POSTs to `/projects/{id}/build/start`:
1. Get current DOT from project
2. Validate (if errors and `skip_editor` not set, redirect to editor)
3. Create attractor `Engine` with the DOT
4. Start engine in background goroutine
5. Store `RunState` on project
6. Redirect to `/projects/{id}/build`

The build view shows live progress via SSE (reuse existing `attractor.EngineEvent` types).

**Step 3: Run tests**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./web/ -v -run TestBuild`
Expected: PASS

**Step 4: Commit**

```bash
agentjj commit -m "feat(web): wire editor-to-build transition with SSE progress"
```

---

### Task 5.6: Build Progress View (Web TUI)

**Files:**
- Create: `web/templates/build.html`
- Create: `web/static/build.js`

**Step 1: Design build view**

Port the inline streaming TUI (`tui/stream.go`) concepts to HTML:
- Node list with status indicators (pending/running/completed/failed)
- Elapsed time per node
- Token count per node
- Tool call log (collapsible per node)
- Overall progress bar

Use SSE + HTMX to update in real-time (same pattern as specd's agent activity feed).

**Step 2: Implement SSE endpoint**

The `/projects/{id}/build/events` endpoint streams `attractor.EngineEvent` as SSE. The HTML template uses HTMX SSE extension to update node status elements.

**Step 3: Test**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./web/ -v -run TestBuildView`
Expected: PASS

**Step 4: Commit**

```bash
agentjj commit -m "feat(web): add build progress view with SSE streaming"
```

---

### Task 5.7: Add `mammoth serve` CLI Command

**Files:**
- Modify: `cmd/mammoth/main.go`

**Step 1: Run existing CLI tests**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./cmd/mammoth/ -v`
Expected: All PASS (baseline)

**Step 2: Add serve subcommand**

Add to the CLI flag parsing in `main.go`:
- `mammoth serve` - starts the unified web server
- `mammoth serve --port 2389` - custom port (default: 2389)
- `mammoth serve --data-dir /path` - custom data directory

The serve command:
1. Loads project state from data directory
2. Creates web.Server
3. Starts HTTP listener
4. Opens browser (if TTY)
5. Blocks until SIGINT/SIGTERM

**Step 3: Run CLI tests**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./cmd/mammoth/ -v`
Expected: All PASS

**Step 4: Commit**

```bash
agentjj commit -m "feat(cli): add 'mammoth serve' command for unified web UI"
```

---

### Task 5.8: Integration Tests

**Files:**
- Create: `web/integration_test.go`

**Step 1: Write end-to-end tests**

Test the three user flows:

**Flow A: Idea → Spec → Edit → Build**
1. POST `/projects` with title/goal → creates project
2. POST `/projects/{id}/spec/agents/start` → starts swarm
3. Wait for DOT generation event
4. GET `/projects/{id}/editor` → editor loads with generated DOT
5. POST `/projects/{id}/build/start` → build starts
6. GET `/projects/{id}/build/events` → receives SSE events

**Flow B: Upload DOT → Edit → Build**
1. POST `/projects` with DOT file → creates project
2. GET `/projects/{id}/editor` → editor loads with uploaded DOT
3. POST `/projects/{id}/build/start` → build starts

**Flow C: Upload DOT → Build (skip editor)**
1. POST `/projects` with DOT file → creates project
2. POST `/projects/{id}/build/start?skip_editor=true` → validates and runs

**Step 2: Run integration tests**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./web/ -v -run TestIntegration -tags=integration`
Expected: All PASS

**Step 3: Commit**

```bash
agentjj commit -m "test(web): add integration tests for all three user flows"
```

---

### Task 5.9: Full Test Suite Verification

**Files:** None (verification only)

**Step 1: Run complete test suite**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./... -count=1`
Expected: All PASS

**Step 2: Run with race detector**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./... -race -count=1`
Expected: All PASS, no race conditions

**Step 3: Build binary**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go build -o mammoth ./cmd/mammoth/`
Expected: Compiles successfully

**Step 4: Smoke test CLI modes**

```bash
./mammoth --help                    # shows serve subcommand
./mammoth validate examples/simple.dot  # existing validation works
```

**Step 5: Commit if any fixups were needed**

```bash
agentjj commit -m "fix: address issues found in full test suite verification"
```

---

## Summary

| Phase | Tasks | What Changes |
|-------|-------|-------------|
| 1: DOT Parser | 1.1-1.6 | New `dot/` package, `attractor/` rewired |
| 2: mux/llm | 2.1-2.4 | `llm/` wraps mux, agent adapted |
| 3: Spec Builder | 3.1-3.5 | New `spec/` package from mammoth-specd |
| 4: Editor | 4.1-4.2 | New `editor/` package from mammoth-dot-editor |
| 5: Unified Web | 5.1-5.9 | New `web/` package, wizard flow, `mammoth serve` |

Total: 21 tasks across 5 phases. Each phase leaves the binary working.
