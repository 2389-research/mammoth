// ABOUTME: Tests for editor-to-build transition logic.
// ABOUTME: Covers valid DOT routing to build phase and invalid DOT rejection staying in edit phase.
package web

import (
	"strings"
	"testing"
)

// makeTestProject creates a Project in the spec phase for testing.
func makeTestProject() *Project {
	return &Project{
		ID:    "test-project-1",
		Name:  "Test Project",
		Phase: PhaseSpec,
	}
}

// TestTransitionEditorToBuild verifies that valid DOT in the editor transitions
// to the build phase.
func TestTransitionEditorToBuild(t *testing.T) {
	project := makeTestProject()
	project.Phase = PhaseEdit
	project.DOT = `digraph pipeline {
	graph [goal="Test pipeline"]
	start [shape=Mdiamond]
	task1 [label="Do work", prompt="Execute task"]
	done [shape=Msquare]
	start -> task1 -> done
}`

	err := TransitionEditorToBuild(project)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if project.Phase != PhaseBuild {
		t.Errorf("expected phase %q, got %q", PhaseBuild, project.Phase)
	}
}

// TestTransitionEditorToBuildInvalid verifies that invalid DOT in the editor
// keeps the project in the edit phase and returns an error.
func TestTransitionEditorToBuildInvalid(t *testing.T) {
	project := makeTestProject()
	project.Phase = PhaseEdit
	project.DOT = "this is not valid DOT at all"

	err := TransitionEditorToBuild(project)
	if err == nil {
		t.Fatal("expected error for invalid DOT, got nil")
	}

	// Should stay in edit phase
	if project.Phase != PhaseEdit {
		t.Errorf("expected phase %q after invalid DOT, got %q", PhaseEdit, project.Phase)
	}

	// Diagnostics should contain error info
	if len(project.Diagnostics) == 0 {
		t.Error("expected diagnostics to be populated for invalid DOT")
	}

	// Verify the build_blocked summary is present
	found := false
	for _, d := range project.Diagnostics {
		if strings.Contains(d, "build_blocked") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected diagnostics to contain build_blocked summary")
	}
}
