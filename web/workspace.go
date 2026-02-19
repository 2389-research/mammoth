// ABOUTME: Workspace abstraction that resolves all paths based on local vs global mode.
// ABOUTME: All state and artifacts are namespaced under the state directory.

package web

import "path/filepath"

// WorkspaceMode determines how mammoth resolves paths for state and artifacts.
type WorkspaceMode string

const (
	// ModeLocal stores state and artifacts in .mammoth/ under CWD.
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
// State and artifacts go in {rootDir}/.mammoth/.
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
// In local mode, artifacts go directly into the project root so the user sees
// generated files in their working directory. In global mode, artifacts are
// namespaced under the state directory by project and run.
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
