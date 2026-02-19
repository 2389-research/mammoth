// ABOUTME: Tests for the Workspace type that resolves paths for local vs global mode.
// ABOUTME: Verifies path construction for project store, artifacts, checkpoints, and progress logs.

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
	// Local mode: artifacts go directly in the project root so generated
	// files appear in the user's working directory.
	expected := "/home/user/projects/app"
	if got != expected {
		t.Fatalf("expected %s, got %s", expected, got)
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
