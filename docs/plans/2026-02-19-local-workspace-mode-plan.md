# Local Workspace Mode Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make `mammoth serve` default to local mode where state goes in `.mammoth/` and artifacts output to CWD, with `--global` preserving today's behavior.

**Architecture:** A `Workspace` type resolves all paths based on mode (local vs global). It replaces the `DataDir string` in `ServerConfig`. The CLI `parseServeArgs` gains a `--global` flag, defaulting to local mode. Build artifact output changes from state dir to workspace root dir in local mode.

**Tech Stack:** Go, chi router, filesystem I/O, existing ProjectStore/EngineConfig

---

### Task 1: Add Workspace Type

**Files:**
- Create: `web/workspace.go`
- Create: `web/workspace_test.go`

**Step 1: Write the failing test**

```go
// web/workspace_test.go
package web

import (
    "path/filepath"
    "testing"
)

func TestLocalWorkspacePaths(t *testing.T) {
    ws := NewLocalWorkspace("/home/user/projects/app")

    if ws.Mode != ModeLocal {
        t.Fatalf("expected local mode, got %s", ws.Mode)
    }
    if ws.RootDir != "/home/user/projects/app" {
        t.Fatalf("expected root /home/user/projects/app, got %s", ws.RootDir)
    }
    if ws.StateDir != filepath.Join("/home/user/projects/app", ".mammoth") {
        t.Fatalf("expected state dir .mammoth, got %s", ws.StateDir)
    }
    if ws.ProjectStoreDir() != ws.StateDir {
        t.Fatal("project store dir should equal state dir")
    }
    if ws.RunStateDir() != filepath.Join(ws.StateDir, "runs") {
        t.Fatalf("unexpected run state dir: %s", ws.RunStateDir())
    }
}

func TestGlobalWorkspacePaths(t *testing.T) {
    ws := NewGlobalWorkspace("/home/user/.local/share/mammoth")

    if ws.Mode != ModeGlobal {
        t.Fatalf("expected global mode, got %s", ws.Mode)
    }
    if ws.RootDir != "/home/user/.local/share/mammoth" {
        t.Fatalf("expected root to be data dir, got %s", ws.RootDir)
    }
    if ws.StateDir != ws.RootDir {
        t.Fatal("global mode: state dir should equal root dir")
    }
}

func TestLocalArtifactDir(t *testing.T) {
    ws := NewLocalWorkspace("/home/user/projects/app")
    got := ws.ArtifactDir("proj-123", "run-456")
    if got != "/home/user/projects/app" {
        t.Fatalf("local artifact dir should be root dir, got %s", got)
    }
}

func TestGlobalArtifactDir(t *testing.T) {
    ws := NewGlobalWorkspace("/data/mammoth")
    got := ws.ArtifactDir("proj-123", "run-456")
    expected := filepath.Join("/data/mammoth", "proj-123", "artifacts", "run-456")
    if got != expected {
        t.Fatalf("expected %s, got %s", expected, got)
    }
}

func TestCheckpointDir(t *testing.T) {
    ws := NewLocalWorkspace("/home/user/projects/app")
    got := ws.CheckpointDir("proj-123", "run-456")
    expected := filepath.Join("/home/user/projects/app", ".mammoth", "proj-123", "artifacts", "run-456")
    if got != expected {
        t.Fatalf("expected %s, got %s", expected, got)
    }
}

func TestProgressLogDir(t *testing.T) {
    ws := NewLocalWorkspace("/home/user/projects/app")
    got := ws.ProgressLogDir("proj-123", "run-456")
    expected := filepath.Join("/home/user/projects/app", ".mammoth", "proj-123", "artifacts", "run-456")
    if got != expected {
        t.Fatalf("expected %s, got %s", expected, got)
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./web/... -run TestLocalWorkspacePaths -v`
Expected: FAIL — `NewLocalWorkspace` not defined

**Step 3: Write minimal implementation**

```go
// web/workspace.go
// ABOUTME: Workspace abstraction that resolves all paths based on local vs global mode.
// ABOUTME: Local mode stores state in .mammoth/ and outputs artifacts to CWD.

package web

import "path/filepath"

// WorkspaceMode determines how mammoth resolves paths for state and artifacts.
type WorkspaceMode string

const (
    // ModeLocal stores state in .mammoth/ under CWD and outputs artifacts to CWD.
    ModeLocal WorkspaceMode = "local"
    // ModeGlobal stores everything under a centralized data directory (XDG).
    ModeGlobal WorkspaceMode = "global"
)

// Workspace resolves all filesystem paths for mammoth based on the active mode.
type Workspace struct {
    Mode     WorkspaceMode
    RootDir  string // Where artifacts/code output goes
    StateDir string // Where .mammoth state lives (projects, checkpoints, runs)
}

// NewLocalWorkspace creates a workspace rooted at the given directory.
// State goes in {rootDir}/.mammoth/, artifacts output to {rootDir}/.
func NewLocalWorkspace(rootDir string) Workspace {
    return Workspace{
        Mode:     ModeLocal,
        RootDir:  rootDir,
        StateDir: filepath.Join(rootDir, ".mammoth"),
    }
}

// NewGlobalWorkspace creates a workspace where root and state are the same
// centralized directory (the XDG data dir).
func NewGlobalWorkspace(dataDir string) Workspace {
    return Workspace{
        Mode:     ModeGlobal,
        RootDir:  dataDir,
        StateDir: dataDir,
    }
}

// ProjectStoreDir returns the directory where project.json files are stored.
func (w Workspace) ProjectStoreDir() string {
    return w.StateDir
}

// RunStateDir returns the directory for persistent run state (manifests, events).
func (w Workspace) RunStateDir() string {
    return filepath.Join(w.StateDir, "runs")
}

// ArtifactDir returns where build artifacts (generated code) should be written.
// In local mode this is the project root (CWD). In global mode it is nested
// under the state directory.
func (w Workspace) ArtifactDir(projectID, runID string) string {
    if w.Mode == ModeLocal {
        return w.RootDir
    }
    return filepath.Join(w.StateDir, projectID, "artifacts", runID)
}

// CheckpointDir returns where checkpoints and progress logs are stored.
// Always under the state directory regardless of mode.
func (w Workspace) CheckpointDir(projectID, runID string) string {
    return filepath.Join(w.StateDir, projectID, "artifacts", runID)
}

// ProgressLogDir returns where progress.ndjson is stored.
// Always under the state directory regardless of mode.
func (w Workspace) ProgressLogDir(projectID, runID string) string {
    return filepath.Join(w.StateDir, projectID, "artifacts", runID)
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./web/... -run TestLocalWorkspace -v && go test ./web/... -run TestGlobalWorkspace -v && go test ./web/... -run TestCheckpointDir -v && go test ./web/... -run TestProgressLogDir -v`
Expected: All PASS

**Step 5: Commit**

```
feat(web): add Workspace type for local vs global path resolution
```

---

### Task 2: Wire Workspace into ServerConfig and NewServer

**Files:**
- Modify: `web/server.go:68-141` (ServerConfig, NewServer, Server struct)
- Modify: `web/server_test.go:759-775` (newTestServer helper)

**Step 1: Write the failing test**

```go
// Add to web/server_test.go
func TestNewServerWithLocalWorkspace(t *testing.T) {
    t.Setenv("MAMMOTH_BACKEND", "")
    t.Setenv("MAMMOTH_DISABLE_PROGRESS_LOG", "1")
    t.Setenv("ANTHROPIC_API_KEY", "")
    t.Setenv("OPENAI_API_KEY", "")
    t.Setenv("GEMINI_API_KEY", "")

    tmpDir := t.TempDir()
    ws := NewLocalWorkspace(tmpDir)
    cfg := ServerConfig{
        Addr:      "127.0.0.1:0",
        Workspace: ws,
    }
    srv, err := NewServer(cfg)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if srv.workspace.Mode != ModeLocal {
        t.Fatalf("expected local mode, got %s", srv.workspace.Mode)
    }
}

func TestNewServerWithGlobalWorkspace(t *testing.T) {
    t.Setenv("MAMMOTH_BACKEND", "")
    t.Setenv("MAMMOTH_DISABLE_PROGRESS_LOG", "1")
    t.Setenv("ANTHROPIC_API_KEY", "")
    t.Setenv("OPENAI_API_KEY", "")
    t.Setenv("GEMINI_API_KEY", "")

    tmpDir := t.TempDir()
    ws := NewGlobalWorkspace(tmpDir)
    cfg := ServerConfig{
        Addr:      "127.0.0.1:0",
        Workspace: ws,
    }
    srv, err := NewServer(cfg)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if srv.workspace.Mode != ModeGlobal {
        t.Fatalf("expected global mode, got %s", srv.workspace.Mode)
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./web/... -run TestNewServerWithLocalWorkspace -v`
Expected: FAIL — `ServerConfig` has no `Workspace` field

**Step 3: Implement changes**

In `web/server.go`:

1. Replace `ServerConfig.DataDir` with `Workspace`:
```go
type ServerConfig struct {
    Addr      string
    Workspace Workspace
}
```

2. Add `workspace` field to `Server` struct, replace `dataDir`:
```go
type Server struct {
    store     *ProjectStore
    templates *TemplateEngine
    router    chi.Router
    addr      string
    workspace Workspace
    // ... rest unchanged
}
```

3. Update `NewServer` to use `cfg.Workspace`:
   - `NewProjectStore(cfg.Workspace.ProjectStoreDir())`
   - `os.MkdirAll(cfg.Workspace.StateDir, ...)`
   - `s.workspace = cfg.Workspace` instead of `s.dataDir = cfg.DataDir`
   - `server.NewAppState(cfg.Workspace.StateDir, ...)`

4. Update `newTestServer` helper:
```go
func newTestServer(t *testing.T) *Server {
    t.Helper()
    // ... env setup unchanged
    cfg := ServerConfig{
        Addr:      "127.0.0.1:0",
        Workspace: NewGlobalWorkspace(t.TempDir()),
    }
    srv, err := NewServer(cfg)
    // ... rest unchanged
}
```

5. Update all `s.dataDir` references to use workspace methods:
   - Line 665: `artifactDir := s.workspace.ArtifactDir(projectID, runID)`
   - Line 666: `checkpointPath := filepath.Join(s.workspace.CheckpointDir(projectID, runID), "checkpoint.json")`
   - Line 673: `progressLogger, progressErr = attractor.NewProgressLogger(s.workspace.ProgressLogDir(projectID, runID))`
   - Line 833: `progressPath := filepath.Join(s.workspace.ProgressLogDir(projectID, p.RunID), "progress.ndjson")`
   - Line 1201: `baseDir := filepath.Join(s.workspace.CheckpointDir(projectID, p.RunID))`
   - Line 1315: `baseDir := filepath.Join(s.workspace.CheckpointDir(projectID, p.RunID))`

**Step 4: Run full web test suite**

Run: `go test ./web/... -v -count=1`
Expected: All PASS

**Step 5: Commit**

```
refactor(web): replace DataDir with Workspace in ServerConfig and Server
```

---

### Task 3: Wire Workspace into CLI serve command

**Files:**
- Modify: `cmd/mammoth/main.go:46-49` (serveConfig)
- Modify: `cmd/mammoth/main.go:732-758` (parseServeArgs)
- Modify: `cmd/mammoth/main.go:763-782` (buildWebServer)

**Step 1: Write the failing test**

Add to `cmd/mammoth/main_test.go` (create if needed):

```go
func TestParseServeArgsGlobal(t *testing.T) {
    scfg, ok := parseServeArgs([]string{"serve", "--global"})
    if !ok {
        t.Fatal("expected serve subcommand to be detected")
    }
    if !scfg.global {
        t.Fatal("expected global flag to be true")
    }
}

func TestParseServeArgsDefaultLocal(t *testing.T) {
    scfg, ok := parseServeArgs([]string{"serve"})
    if !ok {
        t.Fatal("expected serve subcommand to be detected")
    }
    if scfg.global {
        t.Fatal("expected global flag to be false by default")
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/mammoth/... -run TestParseServeArgs -v`
Expected: FAIL — `serveConfig` has no `global` field

**Step 3: Implement changes**

1. Add `global` field to `serveConfig`:
```go
type serveConfig struct {
    port    int
    dataDir string
    global  bool
}
```

2. Add `--global` flag to `parseServeArgs`:
```go
fs.BoolVar(&scfg.global, "global", false, "Use global data directory (~/.local/share/mammoth) instead of local .mammoth/")
```

3. Update `buildWebServer` to construct workspace:
```go
func buildWebServer(scfg serveConfig) (*web.Server, error) {
    var ws web.Workspace

    if scfg.dataDir != "" {
        // Explicit --data-dir: use it for both (backward compat)
        ws = web.NewGlobalWorkspace(scfg.dataDir)
    } else if scfg.global {
        // --global: use XDG data dir
        resolved, err := resolveDataDir("")
        if err != nil {
            return nil, fmt.Errorf("resolve data dir: %w", err)
        }
        ws = web.NewGlobalWorkspace(resolved)
    } else {
        // Default: local mode, CWD is root
        cwd, err := os.Getwd()
        if err != nil {
            return nil, fmt.Errorf("get working directory: %w", err)
        }
        ws = web.NewLocalWorkspace(cwd)
    }

    addr := fmt.Sprintf("127.0.0.1:%d", scfg.port)
    srv, err := web.NewServer(web.ServerConfig{
        Addr:      addr,
        Workspace: ws,
    })
    if err != nil {
        return nil, fmt.Errorf("create web server: %w", err)
    }
    return srv, nil
}
```

4. Update `runServe` startup message to show mode:
```go
if ws.Mode == web.ModeLocal {
    fmt.Fprintf(os.Stderr, "mammoth web UI: http://%s (local: %s)\n", addr, ws.RootDir)
} else {
    fmt.Fprintf(os.Stderr, "mammoth web UI: http://%s (global: %s)\n", addr, ws.StateDir)
}
```

5. Update help text in `parseServeArgs` usage:
```go
fs.Usage = func() {
    fmt.Fprintln(os.Stderr, "Usage: mammoth serve [flags]")
    fmt.Fprintln(os.Stderr)
    fmt.Fprintln(os.Stderr, "Start the unified web server for the mammoth wizard flow.")
    fmt.Fprintln(os.Stderr, "By default, uses current directory as project root (.mammoth/ for state).")
    fmt.Fprintln(os.Stderr)
    fmt.Fprintln(os.Stderr, "Flags:")
    fs.PrintDefaults()
}
```

**Step 4: Run tests**

Run: `go test ./cmd/mammoth/... -v -count=1`
Expected: All PASS

**Step 5: Commit**

```
feat(cli): add --global flag to mammoth serve, default to local workspace mode
```

---

### Task 4: Update artifact listing endpoints for local mode

**Files:**
- Modify: `web/server.go` (handleArtifactList, handleArtifactFile)
- Modify: `web/server_test.go` (add test)

**Step 1: Write the failing test**

```go
func TestArtifactListLocalMode(t *testing.T) {
    t.Setenv("MAMMOTH_BACKEND", "")
    t.Setenv("MAMMOTH_DISABLE_PROGRESS_LOG", "1")
    t.Setenv("ANTHROPIC_API_KEY", "")
    t.Setenv("OPENAI_API_KEY", "")
    t.Setenv("GEMINI_API_KEY", "")

    tmpDir := t.TempDir()

    // Write a fake artifact to the root dir (local mode behavior)
    os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main"), 0o644)

    ws := NewLocalWorkspace(tmpDir)
    cfg := ServerConfig{
        Addr:      "127.0.0.1:0",
        Workspace: ws,
    }
    srv, err := NewServer(cfg)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }

    // Create a project and advance to build phase
    p, err := srv.store.Create("test-proj")
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    p.Phase = PhaseDone
    p.RunID = "run-123"
    srv.store.Save(p)

    req := httptest.NewRequest(http.MethodGet, "/projects/"+p.ID+"/artifacts/list", nil)
    req.Header.Set("Accept", "text/html")
    rec := httptest.NewRecorder()
    srv.ServeHTTP(rec, req)

    if rec.Code != http.StatusOK {
        body, _ := io.ReadAll(rec.Result().Body)
        t.Fatalf("expected 200, got %d: %s", rec.Code, string(body))
    }

    body := rec.Body.String()
    if !strings.Contains(body, "main.go") {
        t.Fatal("expected main.go in artifact listing")
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./web/... -run TestArtifactListLocalMode -v`
Expected: FAIL — artifact listing looks in wrong dir for local mode

**Step 3: Update `handleArtifactList` and `handleArtifactFile`**

Replace hardcoded `filepath.Join(s.dataDir, projectID, "artifacts", p.RunID)` with
`s.workspace.ArtifactDir(projectID, p.RunID)` in both handlers. This was partially
done in Task 2 but the artifact listing endpoints may need the root dir for local
mode. Verify and adjust as needed.

**Step 4: Run tests**

Run: `go test ./web/... -v -count=1`
Expected: All PASS

**Step 5: Commit**

```
fix(web): update artifact listing endpoints for local workspace mode
```

---

### Task 5: Update build view progress log path

**Files:**
- Modify: `web/server.go` (~line 833, handleBuildEvents progress path)

**Step 1: Write a test verifying progress log path resolution**

```go
func TestProgressLogPathLocalMode(t *testing.T) {
    ws := NewLocalWorkspace("/home/user/app")
    got := ws.ProgressLogDir("proj-1", "run-1")
    expected := "/home/user/app/.mammoth/proj-1/artifacts/run-1"
    if got != expected {
        t.Fatalf("expected %s, got %s", expected, got)
    }
}
```

**Step 2: Run test — should already pass from Task 1**

Run: `go test ./web/... -run TestProgressLogPathLocalMode -v`
Expected: PASS (Workspace already implements this)

**Step 3: Verify build wiring uses workspace**

Ensure the build start function (around line 665) uses:
- `s.workspace.ArtifactDir(projectID, runID)` for EngineConfig.ArtifactDir
- `s.workspace.CheckpointDir(projectID, runID)` for checkpoint path
- `s.workspace.ProgressLogDir(projectID, runID)` for progress logger

And the progress event endpoint (around line 833) uses:
- `s.workspace.ProgressLogDir(projectID, p.RunID)` for the ndjson path

**Step 4: Run full test suite**

Run: `go test ./web/... -v -count=1`
Expected: All PASS

**Step 5: Commit**

```
fix(web): wire progress log and checkpoint paths through workspace
```

---

### Task 6: Update help text to document local/global modes

**Files:**
- Modify: `cmd/mammoth/help.go` (or wherever `printHelp` is defined)

**Step 1: Find and read the help text**

Look for `printHelp` in `cmd/mammoth/`.

**Step 2: Update the serve section in help output**

Add documentation about the default local mode behavior and `--global` flag. The
`mammoth serve` section should explain:

```
  mammoth serve              Start web UI (local mode: CWD is project root)
  mammoth serve --global     Start web UI (global mode: ~/.local/share/mammoth)
```

**Step 3: Run `go build ./cmd/mammoth && ./mammoth --help`**

Verify help text renders correctly.

**Step 4: Commit**

```
docs(cli): update help text with local/global workspace modes
```

---

### Task 7: Integration test — full local mode round-trip

**Files:**
- Modify: `web/server_test.go`

**Step 1: Write integration test**

```go
func TestLocalModeProjectRoundTrip(t *testing.T) {
    t.Setenv("MAMMOTH_BACKEND", "")
    t.Setenv("MAMMOTH_DISABLE_PROGRESS_LOG", "1")
    t.Setenv("ANTHROPIC_API_KEY", "")
    t.Setenv("OPENAI_API_KEY", "")
    t.Setenv("GEMINI_API_KEY", "")

    tmpDir := t.TempDir()
    ws := NewLocalWorkspace(tmpDir)
    cfg := ServerConfig{
        Addr:      "127.0.0.1:0",
        Workspace: ws,
    }
    srv, err := NewServer(cfg)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }

    // Verify .mammoth was created
    mammothDir := filepath.Join(tmpDir, ".mammoth")
    if _, err := os.Stat(mammothDir); os.IsNotExist(err) {
        t.Fatal("expected .mammoth directory to be created")
    }

    // Create a project
    form := url.Values{"prompt": {"Build a test app"}}
    req := httptest.NewRequest(http.MethodPost, "/projects", strings.NewReader(form.Encode()))
    req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
    rec := httptest.NewRecorder()
    srv.ServeHTTP(rec, req)

    if rec.Code != http.StatusSeeOther && rec.Code != http.StatusOK {
        t.Fatalf("expected redirect or 200, got %d", rec.Code)
    }

    // Verify project.json was created under .mammoth/
    projects := srv.store.List()
    if len(projects) != 1 {
        t.Fatalf("expected 1 project, got %d", len(projects))
    }

    p := projects[0]
    projectJSON := filepath.Join(mammothDir, p.ID, "project.json")
    if _, err := os.Stat(projectJSON); os.IsNotExist(err) {
        t.Fatalf("expected project.json at %s", projectJSON)
    }
}
```

**Step 2: Run test**

Run: `go test ./web/... -run TestLocalModeProjectRoundTrip -v`
Expected: PASS

**Step 3: Commit**

```
test(web): add integration test for local workspace mode round-trip
```

---

### Task 8: Full test suite verification

**Step 1: Run full test suite**

Run: `go test ./... -count=1`
Expected: All packages PASS

**Step 2: Manual smoke test**

```bash
cd /tmp && mkdir test-app && cd test-app
mammoth serve
# Visit http://127.0.0.1:2389
# Verify: project list at /
# Create a project, verify .mammoth/ created
# Ctrl-C
ls -la .mammoth/
```

```bash
mammoth serve --global
# Verify: uses ~/.local/share/mammoth/
```

**Step 3: Commit any final fixes**

```
chore: final cleanup for local workspace mode
```
