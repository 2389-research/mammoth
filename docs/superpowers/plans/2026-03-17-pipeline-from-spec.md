# Pipeline-Based Spec-to-DOT Generation Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the deterministic spec-to-DOT export with an LLM-driven meta-pipeline that generates higher-quality pipeline DOT files.

**Architecture:** Embed `pipeline_from_spec.dot` in the binary. When the user triggers "Generate Pipeline" from the spec UI, export SpecState to markdown, write it to a working directory, and run the meta-pipeline through tracker. On success, store the generated `pipeline.dot` in the project's DOT field.

**Tech Stack:** Go 1.25+, tracker v0.5.0, `//go:embed`, chi router, SSE

**Spec:** `docs/superpowers/specs/2026-03-16-pipeline-from-spec-design.md`

---

## File Structure

| File | Action | Responsibility |
|------|--------|---------------|
| `web/pipeline_from_spec.dot` | Create | Embedded meta-pipeline DOT (fixed copy from dot-files/) |
| `web/generate.go` | Create | Generation handler, `startGenerationBuild`, embedded DOT |
| `web/generate_test.go` | Create | Tests for generation handler |
| `web/server.go` | Modify | Add route for `POST /spec/generate-pipeline` |
| `web/transitions.go` | Modify | Remove `TransitionSpecToEditor`, `TransitionSpecToBuild`, `exportAndValidate`; keep `TransitionEditorToBuild` |
| `web/transitions_test.go` | Modify | Remove tests for deleted functions; keep `TestTransitionEditorToBuild*` |
| `web/spec_adapter.go` | Modify | Replace `TransitionSpecToEditor` call in `syncProjectFromSpec` |
| `spec/export/dot.go` | Delete | Old deterministic DOT export |
| `spec/export/dot_test.go` | Delete | Tests for above |
| `spec/core/export/dot.go` | Delete | Legacy template-based DOT export |
| `spec/core/export/dot_test.go` | Delete | Tests for above |
| `spec/agents/roles.go` | Modify | Remove `RoleDotGenerator` |
| `spec/agents/prompts.go` | Modify | Remove `dotGeneratorSystemPrompt` and its case in `SystemPromptForRole` |
| `spec/agents/swarm.go` | Modify | Remove `RoleDotGenerator` from roles slices |
| `spec/agents/context_test.go` | Modify | Remove `RoleDotGenerator` test case |
| `spec/web/handlers_export.go` | Modify | Remove `Diagram()`, `ExportDOT()`, `exportDOTSafe()`; update `Artifacts()` and `Regenerate()` |
| `web/integration_test.go` | Modify (if exists) | Replace `TransitionSpecToEditor` call with hardcoded DOT |

---

## Chunk 1: Embed and fix the meta-pipeline

### Task 1: Copy and fix pipeline_from_spec.dot

**Files:**
- Create: `web/pipeline_from_spec.dot`

- [ ] **Step 1: Copy the DOT file into the web package**

Copy `../dot-files/pipeline_from_spec.dot` to `web/pipeline_from_spec.dot`.

- [ ] **Step 2: Fix `score_quality` stdout routing bug**

In `web/pipeline_from_spec.dot`, change the `score_quality` tool_command so diagnostic info goes to stderr and only the quality token goes to stdout:

```sh
#!/bin/sh
set -eu
score=0
total=7
grep -qi 'check_existing\|Check Existing' pipeline.dot && score=$((score + 1))
grep -qi 'scope_analysis\|scope' pipeline.dot && score=$((score + 1))
verify_count=$(grep -c 'shape=diamond' pipeline.dot 2>/dev/null || echo 0)
[ "$verify_count" -gt 0 ] && score=$((score + 1))
grep -qi 'debug_investigate\|debug\|replan' pipeline.dot && score=$((score + 1))
grep -qi 'check_budget\|budget' pipeline.dot && score=$((score + 1))
grep -qi 'validate_build' pipeline.dot && score=$((score + 1))
grep -qi 'IRON LAW' pipeline.dot && score=$((score + 1))
impl_count=$(grep -c 'implement_' pipeline.dot 2>/dev/null || echo 0)
printf 'score=%s/%s impl=%s ' "$score" "$total" "$impl_count" >&2
if [ "$score" -ge 6 ]; then
  printf 'quality_high'
elif [ "$score" -ge 4 ]; then
  printf 'quality_medium'
else
  printf 'quality_low'
fi
```

- [ ] **Step 3: Simplify `find_spec` node**

Replace the multi-file search with a simple existence check since mammoth guarantees `spec.md`:

```sh
#!/bin/sh
set -eu
if [ -f spec.md ]; then
  printf 'spec.md'
else
  printf 'no_spec_found'
fi
```

- [ ] **Step 4: Fix `run_validate` to remove dead `no_dot_file` path**

Replace the tool_command to only output `valid` or `invalid`:

```sh
#!/bin/sh
set -eu
if mammoth -validate pipeline.dot >/dev/null 2>&1; then
  printf 'valid'
else
  printf 'invalid'
fi
```

- [ ] **Step 5: Replace `tracker validate` with `mammoth -validate` in `fix_validation` prompt**

In the `fix_validation` node prompt, change all references from `tracker validate pipeline.dot` to `mammoth -validate pipeline.dot`.

- [ ] **Step 6: Same replacement in `regenerate_dot` and `refine_pipeline` prompts**

Change `tracker validate` to `mammoth -validate` in both node prompts.

- [ ] **Step 7: Commit**

```bash
git add -f web/pipeline_from_spec.dot
git commit -m "feat(web): add embedded pipeline_from_spec.dot meta-pipeline"
```

---

### Task 2: Embed DOT and add parse test

**Files:**
- Create: `web/generate.go`
- Create: `web/generate_test.go`

- [ ] **Step 1: Write failing test — embedded DOT parses and validates**

```go
// ABOUTME: Tests for the embedded meta-pipeline DOT and the pipeline generation handler.
// ABOUTME: Verifies parsing, validation, build creation, HTTP endpoint, and failure preservation.
// web/generate_test.go
package web

import (
    "testing"

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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./web/ -run TestEmbeddedMetaPipeline -v`
Expected: FAIL — `metaPipelineDOT` undefined

- [ ] **Step 3: Write minimal implementation — embed the DOT**

```go
// web/generate.go
// ABOUTME: Pipeline generation handler that runs the embedded meta-pipeline to produce DOT from specs.
// ABOUTME: Exports SpecState to markdown, launches tracker build, stores result in project DOT field.
package web

import _ "embed"

//go:embed pipeline_from_spec.dot
var metaPipelineDOT string
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./web/ -run TestEmbeddedMetaPipeline -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add -f web/generate.go web/generate_test.go
git commit -m "feat(web): embed meta-pipeline DOT with parse/validate tests"
```

---

## Chunk 2: Generation build launcher

### Task 3: startGenerationBuild function

**Files:**
- Modify: `web/generate.go`
- Modify: `web/generate_test.go`

- [ ] **Step 1: Write failing test — generation build creates BuildRun and populates p.DOT on completion**

```go
// web/generate_test.go

func TestStartGenerationBuild_CreatesBuildRun(t *testing.T) {
    srv := newTestServer(t) // use existing test helper pattern from web package
    p, err := srv.store.Create("test-gen")
    if err != nil {
        t.Fatal(err)
    }

    // Start generation with a trivial spec markdown
    runID := srv.startGenerationBuild(p.ID, "# Test Spec\n\nBuild a hello world app.")
    if runID == "" {
        t.Fatal("expected non-empty run ID")
    }

    // Verify a build run exists
    srv.buildsMu.RLock()
    run, ok := srv.builds[p.ID]
    srv.buildsMu.RUnlock()
    if !ok {
        t.Fatal("expected build run to exist")
    }
    if run.State.Status != "running" {
        t.Errorf("expected status running, got %s", run.State.Status)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./web/ -run TestStartGenerationBuild -v`
Expected: FAIL — `startGenerationBuild` undefined

- [ ] **Step 3: Implement startGenerationBuild**

Add to `web/generate.go`:

```go
import (
    "context"
    "fmt"
    "log"
    "os"
    "path/filepath"
    "time"

    "github.com/2389-research/mammoth/spec/export"
    "github.com/2389-research/tracker/agent"
    "github.com/2389-research/tracker/agent/exec"
    "github.com/2389-research/tracker/pipeline"
    "github.com/2389-research/tracker/pipeline/handlers"
    "github.com/google/uuid"
)

// startGenerationBuild launches the embedded meta-pipeline to generate a
// pipeline DOT file from a spec markdown string. Returns the run ID.
// The generated pipeline.dot is stored in p.DOT on successful completion.
func (s *Server) startGenerationBuild(projectID, specMarkdown string) string {
    runID := uuid.New().String()
    ctx, cancel := context.WithCancel(context.Background())

    state := &RunState{
        ID:        runID,
        Status:    "running",
        StartedAt: time.Now(),
    }

    events := make(chan SSEEvent, 100)
    run := &BuildRun{
        State:  state,
        Events: events,
        Cancel: cancel,
        Ctx:    ctx,
    }
    run.EnsureFanoutStarted()

    s.buildsMu.Lock()
    s.builds[projectID] = run
    s.buildsMu.Unlock()

    // Set up working directory with spec.md
    workDir := s.workspace.ArtifactDir(projectID, runID)
    if err := os.MkdirAll(workDir, 0755); err != nil {
        log.Printf("component=web.generate action=mkdir_fail project_id=%s err=%v", projectID, err)
        cancel()
        return runID
    }
    specPath := filepath.Join(workDir, "spec.md")
    if err := os.WriteFile(specPath, []byte(specMarkdown), 0644); err != nil {
        log.Printf("component=web.generate action=write_spec_fail project_id=%s err=%v", projectID, err)
        cancel()
        return runID
    }

    broadcastEvent := func(be BuildEvent) {
        sseEvt := buildEventToSSE(be)
        select {
        case events <- sseEvt:
        default:
        }
    }

    interviewer := newBuildInterviewer(ctx, broadcastEvent)

    pipelineHandler := pipeline.PipelineEventHandlerFunc(func(evt pipeline.PipelineEvent) {
        be := buildEventFromPipeline(evt)
        s.buildsMu.Lock()
        if evt.NodeID != "" {
            state.CurrentNode = evt.NodeID
        }
        if evt.Type == pipeline.EventStageCompleted {
            state.CompletedNodes = append(state.CompletedNodes, evt.NodeID)
        }
        s.buildsMu.Unlock()
        broadcastEvent(be)
    })

    agentHandler := agent.EventHandlerFunc(func(evt agent.Event) {
        be := buildEventFromAgent(evt)
        if be.Type != "" {
            broadcastEvent(be)
        }
    })

    go func() {
        defer close(events)
        defer cancel()
        defer func() {
            if rec := recover(); rec != nil {
                s.buildsMu.Lock()
                completedAt := time.Now()
                state.CompletedAt = &completedAt
                state.Status = "failed"
                state.Error = fmt.Sprintf("panic: %v", rec)
                s.buildsMu.Unlock()
                s.persistBuildOutcome(projectID, state)
                log.Printf("component=web.generate action=panic_recovered project_id=%s run_id=%s recovered=%v", projectID, runID, rec)
            }
        }()

        graph, parseErr := pipeline.ParseDOT(metaPipelineDOT)
        if parseErr != nil {
            s.buildsMu.Lock()
            now := time.Now()
            state.CompletedAt = &now
            state.Status = "failed"
            state.Error = fmt.Sprintf("parse meta-pipeline: %v", parseErr)
            s.buildsMu.Unlock()
            return
        }

        artifactDir := workDir
        registryOpts := []handlers.RegistryOption{
            handlers.WithInterviewer(interviewer, graph),
        }
        if s.llmClient != nil {
            registryOpts = append(registryOpts, handlers.WithLLMClient(s.llmClient, artifactDir))
            registryOpts = append(registryOpts, handlers.WithExecEnvironment(exec.NewLocalEnvironment(artifactDir)))
            registryOpts = append(registryOpts, handlers.WithAgentEventHandler(agentHandler))
        }
        registry := handlers.NewDefaultRegistry(graph, registryOpts...)

        opts := []pipeline.EngineOption{
            pipeline.WithPipelineEventHandler(pipelineHandler),
            pipeline.WithArtifactDir(artifactDir),
        }
        engine := pipeline.NewEngine(graph, registry, opts...)
        _, runErr := engine.Run(ctx)

        s.buildsMu.Lock()
        now := time.Now()
        state.CompletedAt = &now
        if runErr != nil {
            if ctx.Err() != nil {
                state.Status = "cancelled"
            } else {
                state.Status = "failed"
                state.Error = runErr.Error()
            }
        } else {
            state.Status = "completed"
        }
        s.buildsMu.Unlock()

        s.persistBuildOutcome(projectID, state)

        // On success, read pipeline.dot and store in project
        if runErr == nil {
            dotPath := filepath.Join(workDir, "pipeline.dot")
            dotBytes, readErr := os.ReadFile(dotPath)
            if readErr == nil && len(dotBytes) > 0 {
                p, ok := s.store.Get(projectID)
                if ok {
                    p.DOT = string(dotBytes)
                    _ = s.store.Update(p)
                }
            }
        }
    }()

    return runID
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./web/ -run TestStartGenerationBuild -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add -f web/generate.go web/generate_test.go
git commit -m "feat(web): add startGenerationBuild for meta-pipeline execution"
```

---

### Task 4: HTTP handler and route

**Files:**
- Modify: `web/generate.go`
- Modify: `web/generate_test.go`
- Modify: `web/server.go`

- [ ] **Step 1: Write failing test — POST /spec/generate-pipeline returns 200 with run ID**

```go
// web/generate_test.go

import (
    "net/http"
    "net/http/httptest"
    "encoding/json"
)

func TestHandleGeneratePipeline(t *testing.T) {
    srv := newTestServer(t)
    p, _ := srv.store.Create("test-gen")
    // Put project in spec phase with some spec state
    p.Phase = PhaseSpec
    srv.store.Update(p)

    req := httptest.NewRequest("POST", "/projects/"+p.ID+"/spec/generate-pipeline", nil)
    w := httptest.NewRecorder()
    srv.router.ServeHTTP(w, req)

    if w.Code != http.StatusOK {
        t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
    }
    var resp map[string]string
    if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
        t.Fatalf("decode response: %v", err)
    }
    if resp["run_id"] == "" {
        t.Error("expected non-empty run_id in response")
    }
}

func TestHandleGeneratePipeline_Conflict(t *testing.T) {
    srv := newTestServer(t)
    p, _ := srv.store.Create("test-gen")
    p.Phase = PhaseSpec
    srv.store.Update(p)

    // Simulate an active build
    srv.buildsMu.Lock()
    srv.builds[p.ID] = &BuildRun{
        State: &RunState{Status: "running"},
    }
    srv.buildsMu.Unlock()

    req := httptest.NewRequest("POST", "/projects/"+p.ID+"/spec/generate-pipeline", nil)
    w := httptest.NewRecorder()
    srv.router.ServeHTTP(w, req)

    if w.Code != http.StatusConflict {
        t.Fatalf("expected 409, got %d", w.Code)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./web/ -run TestHandleGeneratePipeline -v`
Expected: FAIL — route not registered / handler not found

- [ ] **Step 3: Implement handler**

Add to `web/generate.go`:

```go
import (
    "encoding/json"
    "net/http"

    "github.com/go-chi/chi/v5"
    "github.com/oklog/ulid/v2"
    "github.com/2389-research/mammoth/spec/core"
    "github.com/2389-research/mammoth/spec/export"
)

// handleGeneratePipeline triggers pipeline generation from the current spec.
func (s *Server) handleGeneratePipeline(w http.ResponseWriter, r *http.Request) {
    projectID := chi.URLParam(r, "projectID")
    p, ok := s.store.Get(projectID)
    if !ok {
        http.Error(w, "project not found", http.StatusNotFound)
        return
    }

    // Concurrency guard — only block if there's an actively running build
    s.buildsMu.RLock()
    existing, hasEntry := s.builds[projectID]
    active := hasEntry && existing.State.Status == "running"
    s.buildsMu.RUnlock()
    if active {
        http.Error(w, "a build is already running for this project", http.StatusConflict)
        return
    }

    // Resolve spec actor and export to markdown.
    // ReadState is callback-based: handle.ReadState(func(st *core.SpecState) { ... })
    if p.SpecID == "" {
        http.Error(w, "project has no spec", http.StatusBadRequest)
        return
    }
    specID, parseErr := ulid.Parse(p.SpecID)
    if parseErr != nil {
        http.Error(w, "invalid spec ID", http.StatusBadRequest)
        return
    }
    handle := s.specState.GetActor(specID)
    if handle == nil {
        http.Error(w, "spec actor not found", http.StatusNotFound)
        return
    }

    var specMarkdown string
    handle.ReadState(func(st *core.SpecState) {
        specMarkdown = export.ExportMarkdown(st)
    })

    if specMarkdown == "" {
        http.Error(w, "spec is empty — add cards before generating", http.StatusBadRequest)
        return
    }

    runID := s.startGenerationBuild(p.ID, specMarkdown)

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]string{
        "run_id":     runID,
        "project_id": projectID,
    })
}
```

- [ ] **Step 4: Register route in server.go**

In `web/server.go`, find the spec route group (inside `specRouter` or the project routes) and add:

```go
r.Post("/projects/{projectID}/spec/generate-pipeline", s.handleGeneratePipeline)
```

Add this near the existing `/projects/{projectID}/spec/continue` route (~line 550).

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./web/ -run TestHandleGeneratePipeline -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add -f web/generate.go web/generate_test.go web/server.go
git commit -m "feat(web): add POST /spec/generate-pipeline handler"
```

---

## Chunk 3: Delete old export code

### Task 5: Remove spec/export/dot.go and spec/core/export/dot.go

**Files:**
- Delete: `spec/export/dot.go`
- Delete: `spec/export/dot_test.go` (if exists)
- Delete: `spec/core/export/dot.go`
- Delete: `spec/core/export/dot_test.go` (if exists)

- [ ] **Step 1: Check what imports these files**

Run: `grep -r "spec/export" web/ spec/ --include="*.go" -l` and `grep -r "spec/core/export" web/ spec/ --include="*.go" -l` to identify all dependents before deleting.

- [ ] **Step 2: Delete the files**

```bash
rm -f spec/export/dot.go spec/export/dot_test.go
rm -f spec/core/export/dot.go spec/core/export/dot_test.go
```

- [ ] **Step 3: Verify build**

Run: `go build ./...`
Expected: compilation errors in files that import the deleted functions — these are fixed in Tasks 6-8.

- [ ] **Step 4: Do NOT commit yet** — wait until dependents are updated.

---

### Task 6: Update web/transitions.go and web/transitions_test.go

**Files:**
- Modify: `web/transitions.go` (lines 18-97)
- Modify: `web/transitions_test.go`

- [ ] **Step 1: Remove `TransitionSpecToEditor`, `TransitionSpecToBuild`, and `exportAndValidate`**

In `web/transitions.go`, delete these three functions (lines 18-49 and lines 78-97). Keep `TransitionEditorToBuild` (lines 54-76) and all helper functions below it (`hasErrors`, `formatDiagnostics`, `countSeverity`, `prependBuildBlockedSummary`).

Remove the `"github.com/2389-research/mammoth/spec/export"` and `"github.com/2389-research/mammoth/spec/core"` imports since they're only used by the deleted functions. Keep `"github.com/2389-research/mammoth/dot"` and `"github.com/2389-research/mammoth/dot/validator"` (used by `TransitionEditorToBuild`).

- [ ] **Step 2: Update transitions_test.go**

Remove:
- `TestTransitionSpecToEditor` (lines 53-83)
- `TestTransitionSpecToEditorNilCore` (lines 85-105)
- `TestTransitionSpecToBuildClean` (lines 107-130)
- `TestTransitionSpecToBuildWithErrors` (lines 132-168)
- `makeTestState` helper (lines 14-19)
- `makeTestCard` helper (lines 22-40)

Remove `"time"` and `"github.com/2389-research/mammoth/spec/core"` imports.

**Fix `TestTransitionEditorToBuild` (line 172-197)**: This test uses `TransitionSpecToEditor` as setup to get valid DOT. Replace with a hardcoded valid DOT string:

```go
func TestTransitionEditorToBuild(t *testing.T) {
    project := makeTestProject()
    project.Phase = PhaseEdit
    project.DOT = `digraph pipeline {
        start [node_type="start"];
        task1 [node_type="tool" tool_command="echo done"];
        done [node_type="done"];
        start -> task1;
        task1 -> done;
    }`

    err := TransitionEditorToBuild(project)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if project.Phase != PhaseBuild {
        t.Errorf("expected phase %q, got %q", PhaseBuild, project.Phase)
    }
}
```

Keep `TestTransitionEditorToBuildInvalid` (lines 199-220) and `makeTestProject` helper (lines 43-49).

- [ ] **Step 3: Verify build compiles**

Run: `go build ./...`
Expected: may still fail due to `spec_adapter.go` — fixed in next task.

---

### Task 7: Update web/spec_adapter.go

**Files:**
- Modify: `web/spec_adapter.go` (lines ~54, ~69, ~296-328)

- [ ] **Step 1: Remove `TransitionSpecToEditor` call in `syncProjectFromSpec`**

In `syncProjectFromSpec` (line 296), the function currently calls `TransitionSpecToEditor(p, st)` via `handle.ReadState` callback at line 314-316. Replace the callback body with a no-op or simple field update — the DOT is now populated by the generation pipeline, not by sync. Keep the `s.store.Update(p)` call and `p.SpecID` assignment below it.

Replace lines 313-316:
```go
var transitionErr error
handle.ReadState(func(st *core.SpecState) {
    transitionErr = TransitionSpecToEditor(p, st)
})
```

With:
```go
// DOT is populated by the generation pipeline, not sync.
// Just ensure the spec ID is linked to the project.
```

Remove the `transitionErr` check at lines 317-319 as well.

- [ ] **Step 2: Remove deleted handler route registrations**

In `web/spec_adapter.go`, remove these route registrations:
- Line 54: `r.Get("/diagram", specweb.Diagram(state, renderer))` — `Diagram` handler will be deleted in Task 8
- Line 69: `r.Get("/export/dot", specweb.ExportDOT(state))` — `ExportDOT` handler will be deleted in Task 8

- [ ] **Step 3: Check for `web/integration_test.go`**

If `web/integration_test.go` exists and calls `TransitionSpecToEditor`, update it to set `project.DOT` directly with a hardcoded test DOT string instead of calling the deleted function.

- [ ] **Step 4: Verify build compiles**

Run: `go build ./...`

---

### Task 8: Remove DotGenerator agent role and export handlers

**Files:**
- Modify: `spec/agents/roles.go` (remove `RoleDotGenerator`)
- Modify: `spec/agents/prompts.go` (remove `dotGeneratorSystemPrompt` and its case)
- Modify: `spec/agents/swarm.go` (remove `RoleDotGenerator` from roles slices at lines ~74 and ~139)
- Modify: `spec/agents/context_test.go` (remove `RoleDotGenerator` test case at line ~220)
- Modify: `spec/web/handlers_export.go` (remove `Diagram()`, `ExportDOT()`, `exportDOTSafe()`)

- [ ] **Step 1: Remove `RoleDotGenerator` from roles.go**

Remove the `RoleDotGenerator` constant from the iota block (line ~8). Update the `Label()` and `String()` switch cases.

- [ ] **Step 2: Remove `RoleDotGenerator` from swarm.go**

In `spec/agents/swarm.go`, remove `RoleDotGenerator` from the roles slices at lines ~74 and ~139. These are the lists of roles that the swarm spawns agents for.

- [ ] **Step 3: Remove `RoleDotGenerator` from context_test.go**

In `spec/agents/context_test.go`, remove the `{RoleDotGenerator, "dot_generator"}` test table entry at line ~220.

- [ ] **Step 4: Remove `dotGeneratorSystemPrompt` from prompts.go**

Delete the constant (lines 43-60) and its case in `SystemPromptForRole` (line ~75).

- [ ] **Step 5: Update handlers_export.go — remove DOT export functions**

Delete `exportDOTSafe()` (lines 51-58), `Diagram()` (lines 92-116), and `ExportDOT()` (lines 267-290).

**Important cascading fix:** `Artifacts()` (line 84) and the `Regenerate()` handler (line ~316) also call `exportDOTSafe()`. For `Artifacts()`, replace the `exportDOTSafe(s)` call with an empty string (or remove the DOTContent field assignment — artifacts can show markdown and YAML without DOT). For `Regenerate()`, if it calls `exportDOTSafe()` to populate DOT content for file export, replace with an empty string or remove the DOT file write.

- [ ] **Step 6: Verify full build compiles**

Run: `go build ./...`
Expected: PASS — all deleted code references resolved.

- [ ] **Step 7: Run all tests**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 8: Commit all deletions together**

```bash
git add -A
git commit -m "refactor: remove deterministic DOT export, DotGenerator agent, and old export handlers"
```

---

## Chunk 4: Integration test

### Task 9: Integration test for generation flow

**Files:**
- Modify: `web/generate_test.go`

- [ ] **Step 1: Write integration test — generation preserves previous DOT on failure**

```go
func TestGenerationPreservesDOTOnFailure(t *testing.T) {
    srv := newTestServer(t)
    p, _ := srv.store.Create("test-preserve")
    p.DOT = "digraph old { a -> b; }"
    srv.store.Update(p)

    // Start generation with empty spec (will fail at analyze_spec).
    // Without a real LLM client, the build will fail quickly,
    // which is exactly what we want to test.
    runID := srv.startGenerationBuild(p.ID, "")
    if runID == "" {
        t.Fatal("expected run ID")
    }

    // Wait for build to complete with timeout
    srv.buildsMu.RLock()
    run := srv.builds[p.ID]
    srv.buildsMu.RUnlock()
    if run != nil {
        ch, unsub := run.Subscribe()
        defer unsub()
        timeout := time.After(10 * time.Second)
        for {
            select {
            case _, ok := <-ch:
                if !ok {
                    goto done
                }
            case <-timeout:
                t.Fatal("timed out waiting for build to complete")
            }
        }
    }
done:

    // Verify old DOT is preserved
    updated, _ := srv.store.Get(p.ID)
    if updated.DOT != "digraph old { a -> b; }" {
        t.Errorf("expected old DOT preserved, got %q", updated.DOT)
    }
}
```

- [ ] **Step 2: Run test**

Run: `go test ./web/ -run TestGenerationPreserves -v`
Expected: PASS

- [ ] **Step 3: Run full test suite**

Run: `go test ./...`
Expected: ALL PASS

- [ ] **Step 4: Commit**

```bash
git add -f web/generate_test.go
git commit -m "test(web): add integration tests for pipeline generation"
```

---

**Note on success-path integration test:** The spec calls for "real LLM test double (not mock), run the generation handler, verify p.DOT is populated on completion." This requires a running LLM provider with a valid API key, which makes it an environment-dependent integration test. This test should be added as a build-tagged `_integration_test.go` that runs in CI with LLM credentials but is skipped locally. The failure-path and concurrency tests above are sufficient for the initial implementation — the success-path test should be added when the CI environment supports it.

---

## Chunk 5: Final verification

### Task 10: Full build and test verification

- [ ] **Step 1: Clean build**

```bash
go build ./...
```

- [ ] **Step 2: Full test suite**

```bash
go test ./...
```

- [ ] **Step 3: Verify no references to deleted code remain**

```bash
grep -r "ExportDOT\|ExportGraph\|exportAndValidate\|TransitionSpecToEditor\|TransitionSpecToBuild\|RoleDotGenerator\|dotGeneratorSystemPrompt" --include="*.go" . | grep -v "docs/"
```

Expected: no matches. This includes test files — all references should have been cleaned up in Chunks 3-4.

- [ ] **Step 4: Verify embedded DOT is in binary**

```bash
go build -o /tmp/mammoth-test ./cmd/mammoth && strings /tmp/mammoth-test | grep -c "pipeline_from_spec"
```

Expected: at least 1 match.

- [ ] **Step 5: Push**

```bash
git push
```
