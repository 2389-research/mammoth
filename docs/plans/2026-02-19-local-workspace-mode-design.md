# Local Workspace Mode Design

## Problem

`mammoth serve` stores all state under `~/.local/share/mammoth/` and writes
artifacts into nested subdirectories. This makes mammoth feel like a
standalone app rather than a codegen tool you run inside your project. When
you run it from `~/projects/app-test/`, you expect output files to appear
there — not buried in XDG data dirs.

## Solution: Workspace Abstraction

Introduce a `Workspace` type that resolves all paths based on mode. Two
modes:

- **Local (default):** `mammoth serve` uses CWD as the project root.
  State goes in `.mammoth/`, artifacts go directly into CWD.
- **Global (`--global`):** Today's behavior. Everything under
  `~/.local/share/mammoth/`.

## Workspace Type

```go
type WorkspaceMode string

const (
    ModeLocal  WorkspaceMode = "local"
    ModeGlobal WorkspaceMode = "global"
)

type Workspace struct {
    Mode    WorkspaceMode
    RootDir  string // Where artifacts/code output goes
    StateDir string // Where .mammoth state lives
}
```

### Local mode (`mammoth serve` from `~/projects/app-test/`):
- `RootDir`  = `~/projects/app-test/`
- `StateDir` = `~/projects/app-test/.mammoth/`

### Global mode (`mammoth serve --global`):
- `RootDir`  = `~/.local/share/mammoth/`
- `StateDir` = `~/.local/share/mammoth/`

### Path helpers:
- `ProjectStoreDir()` — where project.json files live (`StateDir`)
- `ArtifactDir(projectID, runID)` — local: `RootDir`; global: `StateDir/{projectID}/artifacts/{runID}/`
- `RunStateDir()` — `StateDir/runs/`
- `CheckpointDir(projectID, runID)` — always under `StateDir`

## CLI Changes

`serveConfig` gains a `global` flag:

```go
type serveConfig struct {
    port    int
    dataDir string
    global  bool
}
```

Resolution in `buildWebServer`:
1. `--global` → XDG data dir for everything (today's behavior)
2. `--data-dir` explicit → use that path for both root and state (backward compat)
3. Otherwise (default) → local mode: CWD is root, `.mammoth/` is state

`web.ServerConfig` changes from `DataDir string` to `Workspace Workspace`.

## Artifact Output

In local mode, build artifacts (generated code) write directly to CWD.
Pipeline state (checkpoints, progress logs, run manifests) stays under
`.mammoth/`.

The `EngineConfig` wiring changes:
- `ArtifactDir` → `workspace.RootDir` (local) or `workspace.ArtifactDir(projectID, runID)` (global)
- `AutoCheckpointPath` → always under `workspace.StateDir`

Artifact listing endpoints (`/artifacts/list`, `/artifacts/file`) look at
`RootDir` in local mode.

## Directory Layout

### Local mode (`~/projects/app-test/`)
```
~/projects/app-test/
  main.go                    # generated code (artifacts)
  go.mod                     # generated code
  ...
  .mammoth/
    {projectID}/
      project.json
      artifacts/{runID}/
        checkpoint.json
        progress.ndjson
        nodes/{nodeID}/
    runs/
      {runID}/
        manifest.json
        context.json
        events.jsonl
```

### Global mode (`~/.local/share/mammoth/`)
```
~/.local/share/mammoth/
  {projectID}/
    project.json
    artifacts/{runID}/
      checkpoint.json
      progress.ndjson
      nodes/{nodeID}/
        (generated files here too)
  runs/
    {runID}/
      manifest.json
      ...
```

## Out of Scope

- CLI pipeline mode (`mammoth pipeline.dot`) — unchanged, has its own flags
- Old `-server` mode — deprecated, untouched
- `.mammoth/config.json` — no persistent config; mode determined by flag
- Auto-detection of `.mammoth/` — local is always default, no sniffing
- TUI mode — only `mammoth serve` affected
- Migration of existing global state — `--global` still finds it
