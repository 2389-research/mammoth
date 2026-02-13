# Mammoth Unification Design

**Date:** 2026-02-13
**Author:** SKULLCRUSHER McBYTES + THE NOTORIOUS H.A.R.P.
**Status:** Approved

## Summary

Merge three repositories (mammoth-dev, mammoth-specd, mammoth-dot-editor) into a single Go binary that provides a holistic system for building software pipelines: from idea to spec to validated DOT graph to executed pipeline.

## Context

Three separate tools currently handle different stages of the pipeline lifecycle:

- **mammoth-dev** (the builder) - Parses DOT files and executes them as DAG pipelines with LLM-powered agent nodes
- **mammoth-specd** (the spec builder) - Takes ideas, builds them into structured specs via a 4-agent swarm, exports DOT
- **mammoth-dot-editor** (the editor) - Web-based DOT editor with validation, visualization, and three-panel editing

Users must manually move artifacts between tools. This design unifies them into one system.

## Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Integration model | Single binary ("Layer Cake") | Simplest deployment, shared packages, one process |
| LLM abstraction | `mux/llm` | Already used by specd, external shared library |
| DOT parser | Consolidate into one | Avoids divergence, union of validation rules |
| Web UI model | Wizard flow (Spec → Edit → Build) | Step-based with free navigation between stages |
| DOT generation | Dynamic (spec-driven topology) | Pipeline shape reflects actual spec structure |

## Package Structure

```
mammoth/
  cmd/mammoth/        # Single binary entry point
    main.go           # CLI: run, serve, validate, tui subcommands

  dot/                # Consolidated DOT parser (merged from both repos)
    lexer.go          # Tokenizer
    parser.go         # Recursive descent -> AST
    ast.go            # Graph, Node, Edge, Subgraph
    serializer.go     # AST -> DOT string (with color-coding)
    validator/        # 22+ lint rules (from editor) + runtime rules (from attractor)

  spec/               # Spec builder (from mammoth-specd)
    core.go           # SpecCore, Card, Lane data model
    actor.go          # Event-sourced actor per spec
    events.go         # 13 event types
    commands.go       # 12 command types
    store.go          # JSONL + SQLite persistence
    export.go         # Spec -> DOT (dynamic generation)
    agents/           # 4-agent swarm (Manager, Brainstormer, Planner, DotGenerator)

  editor/             # DOT editor (from mammoth-dot-editor)
    session.go        # Editor sessions with undo/redo
    handlers.go       # Node/edge/attr CRUD
    templates/        # HTMX templates for three-panel editor

  agent/              # Existing agent loop (preserved)
  attractor/          # Existing pipeline runner (preserved)

  web/                # Unified wizard UI (new)
    server.go         # HTTP server, router, middleware
    wizard.go         # Wizard flow orchestration
    templates/        # Shared layout, wizard chrome, nav
    static/           # JS, CSS, d3-graphviz assets

  tui/                # Existing Bubble Tea TUI (preserved)
```

## User Flows

### Path A: Idea -> Spec -> Edit -> Build

1. User opens `mammoth serve`, lands on home page
2. Clicks "New from Idea", enters title, one-liner, goal
3. Agent swarm activates (Manager, Brainstormer, Planner, DotGenerator)
4. Kanban board fills up across Ideas, Plan, Spec lanes
5. DotGenerator agent produces DOT dynamically based on spec structure
6. User chooses "Edit First" (editor view) or "Build Now" (skip to execution)
7. If editing: three-panel editor with continuous validation
8. Clicks "Build" to execute pipeline with live progress in browser

### Path B: Upload DOT -> Edit -> Build

1. User uploads `.dot` file or pastes DOT source
2. Goes to Editor view (or "Build Now" to skip)
3. Validation + editing
4. Clicks "Build" to execute

### Path C: Upload DOT -> Build (skip editor)

1. User uploads `.dot` and clicks "Build Now"
2. Validation runs silently
3. If clean: executes immediately
4. If errors: bounces to Editor with diagnostics highlighted

### CLI (preserved)

```
mammoth run pipeline.dot          # direct execution
mammoth run --tui pipeline.dot    # full Bubble Tea TUI
mammoth validate pipeline.dot     # validation only
mammoth serve                     # unified web UI
mammoth serve --port 2389         # custom port
```

## Dynamic DOT Generation

The DotGenerator agent builds pipeline topology that reflects the actual spec, replacing the fixed 10-node template.

The agent uses the consolidated `dot/` package AST builder and validator as tools.

| Spec Pattern | Pipeline Pattern |
|---|---|
| Sequential tasks in Plan lane | Linear chain: `a -> b -> c` |
| Independent tasks (no refs) | Parallel branch: `fork -> {a, b, c} -> join` |
| Task with "if/when" language | Conditional diamond node |
| Risk/assumption cards | Verification gate after implementation |
| Open questions remaining | Human gate (wait.human) node |
| Multiple implementation phases | Nested sub-pipelines or retry loops |

Guardrails:
- Always has exactly one start (Mdiamond) and one exit (Msquare)
- Every generated pipeline passes validation before presenting to user
- User always has the option to edit before building

## Data Model

```go
Project {
    ID          string
    Name        string
    CreatedAt   time.Time

    // Phase 1: Spec (optional - only if started from idea)
    Spec        *SpecState       // event-sourced kanban + agents

    // Phase 2: DOT (always present)
    DOT         string           // current DOT source
    Graph       *dot.Graph       // parsed AST
    Diagnostics []Diagnostic     // validation results
    EditHistory []string         // undo stack (from editor)

    // Phase 3: Build (when executed)
    Runs        []RunState       // pipeline execution history
    ActiveRun   *RunState        // currently executing (if any)
}
```

Persistence at `$XDG_DATA_HOME/mammoth/projects/{id}/`:

```
projects/{id}/
    project.json          # metadata
    spec/                 # event log, snapshots (if spec phase used)
        events.jsonl
        snapshots/
    dot/                  # current + history
        current.dot
        history/
    runs/                 # execution history + checkpoints
        {run_id}/
            state.json
            checkpoint.json
        artifacts/
```

Each phase keeps its own proven state management pattern:
- Spec: event-sourced (from specd)
- DOT: snapshot-based with undo stack (from editor)
- Build: checkpoint-based (from attractor)

## Server Architecture

Single HTTP server on one port:

```
/                              # Home page
/health                        # Health check

/projects                      # List projects
/projects/new                  # Create from idea or upload
/projects/{id}                 # Project overview

/projects/{id}/spec            # Spec builder (kanban, agents, chat)
/projects/{id}/spec/commands   # Spec mutations
/projects/{id}/spec/agents/*   # Agent swarm control
/projects/{id}/spec/events     # SSE stream

/projects/{id}/editor          # DOT editor (three-panel)
/projects/{id}/editor/dot      # Update DOT source
/projects/{id}/editor/nodes/*  # Node CRUD
/projects/{id}/editor/edges/*  # Edge CRUD
/projects/{id}/editor/undo     # Undo/redo
/projects/{id}/editor/export   # Download DOT file

/projects/{id}/build           # Build runner (live progress)
/projects/{id}/build/start     # Kick off execution
/projects/{id}/build/events    # SSE stream (engine events)
/projects/{id}/build/stop      # Cancel execution

/projects/{id}/validate        # Run validation, return diagnostics

/api/...                       # JSON API mirror (programmatic use)
```

Tech: Chi router, HTMX + server-side templates, SSE for real-time, d3-graphviz for visualization. No frontend build step.

## Migration Strategy

### Phase 1: Foundation
- Consolidate DOT parser into `dot/` package (merge both parsers, union of validation rules)
- Replace mammoth's `llm/` with `mux/llm` dependency
- Ensure existing `mammoth run`, `mammoth validate`, and TUI still work

### Phase 2: Import Spec Builder
- Bring specd's internals into `spec/` package
- Adapt agent swarm imports (already uses mux/llm)
- Swap specd's DOT parser for consolidated `dot/` package
- Replace fixed 10-node export with dynamic DotGenerator

### Phase 3: Import Editor
- Bring editor's internals into `editor/` package
- Swap editor's DOT parser for consolidated `dot/` package
- Editor sessions operate on Project state

### Phase 4: Unified Web
- Build `web/` package with wizard flow, project management, home page
- Wire spec -> editor -> build transitions
- SSE streams for all three phases
- "Build Now" shortcut (skip editor)

### Phase 5: Polish
- Unified templates and styling across all views
- Project persistence and history
- `mammoth serve` command in CLI

### What stays untouched
- `agent/` (agent loop)
- `attractor/` (engine, handlers, backends)
- `tui/` (Bubble Tea dashboard)

### What gets archived
- `mammoth-specd` repo
- `mammoth-dot-editor` repo
