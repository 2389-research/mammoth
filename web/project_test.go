// ABOUTME: Tests for the Project data model and ProjectStore.
// ABOUTME: Covers creation, retrieval, listing, updates, persistence, and phase transitions.
package web

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestProjectCreate(t *testing.T) {
	store := NewProjectStore(t.TempDir())

	p, err := store.Create("my-project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if p.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if p.Name != "my-project" {
		t.Errorf("expected name %q, got %q", "my-project", p.Name)
	}
	if p.Phase != PhaseSpec {
		t.Errorf("expected phase %q, got %q", PhaseSpec, p.Phase)
	}
	if p.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt")
	}
	if time.Since(p.CreatedAt) > time.Second {
		t.Error("expected CreatedAt to be recent")
	}
}

func TestProjectCreateEmptyName(t *testing.T) {
	store := NewProjectStore(t.TempDir())

	_, err := store.Create("")
	if err == nil {
		t.Fatal("expected error for empty name, got nil")
	}
}

func TestProjectGet(t *testing.T) {
	store := NewProjectStore(t.TempDir())

	created, err := store.Create("test-project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, ok := store.Get(created.ID)
	if !ok {
		t.Fatal("expected to find project")
	}
	if got.ID != created.ID {
		t.Errorf("expected ID %q, got %q", created.ID, got.ID)
	}
	if got.Name != created.Name {
		t.Errorf("expected name %q, got %q", created.Name, got.Name)
	}
}

func TestProjectGetNotFound(t *testing.T) {
	store := NewProjectStore(t.TempDir())

	_, ok := store.Get("nonexistent-id")
	if ok {
		t.Fatal("expected not found for nonexistent ID")
	}
}

func TestProjectList(t *testing.T) {
	store := NewProjectStore(t.TempDir())

	// Create projects with slightly staggered times so ordering is deterministic.
	p1, err := store.Create("first")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Nudge time forward so the second project is definitively newer.
	p1.CreatedAt = p1.CreatedAt.Add(-time.Second)
	if err := store.Update(p1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	p2, err := store.Create("second")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	list := store.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(list))
	}

	// Newest first.
	if list[0].ID != p2.ID {
		t.Errorf("expected newest project first, got %q", list[0].Name)
	}
	if list[1].ID != p1.ID {
		t.Errorf("expected oldest project second, got %q", list[1].Name)
	}
}

func TestProjectUpdate(t *testing.T) {
	store := NewProjectStore(t.TempDir())

	p, err := store.Create("update-test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	p.Phase = PhaseEdit
	p.DOT = "digraph { a -> b }"
	if err := store.Update(p); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, ok := store.Get(p.ID)
	if !ok {
		t.Fatal("expected to find project after update")
	}
	if got.Phase != PhaseEdit {
		t.Errorf("expected phase %q, got %q", PhaseEdit, got.Phase)
	}
	if got.DOT != "digraph { a -> b }" {
		t.Errorf("expected DOT %q, got %q", "digraph { a -> b }", got.DOT)
	}
}

func TestProjectPersistence(t *testing.T) {
	dir := t.TempDir()

	// Create and save a project.
	store1 := NewProjectStore(dir)
	p, err := store1.Create("persist-test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	p.Phase = PhaseEdit
	p.DOT = "digraph { start -> end }"
	if err := store1.Update(p); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := store1.Save(p); err != nil {
		t.Fatalf("unexpected error saving: %v", err)
	}

	// Verify the file exists on disk.
	jsonPath := filepath.Join(dir, p.ID, "project.json")
	if _, err := os.Stat(jsonPath); os.IsNotExist(err) {
		t.Fatalf("expected project.json at %s", jsonPath)
	}

	// Load into a fresh store.
	store2 := NewProjectStore(dir)
	if err := store2.LoadAll(); err != nil {
		t.Fatalf("unexpected error loading: %v", err)
	}

	got, ok := store2.Get(p.ID)
	if !ok {
		t.Fatal("expected to find project after LoadAll")
	}
	if got.Name != "persist-test" {
		t.Errorf("expected name %q, got %q", "persist-test", got.Name)
	}
	if got.Phase != PhaseEdit {
		t.Errorf("expected phase %q, got %q", PhaseEdit, got.Phase)
	}
	if got.DOT != "digraph { start -> end }" {
		t.Errorf("expected DOT round-trip, got %q", got.DOT)
	}
	if got.DataDir != filepath.Join(dir, p.ID) {
		t.Errorf("expected DataDir %q, got %q", filepath.Join(dir, p.ID), got.DataDir)
	}
}

func TestProjectPhaseTransitions(t *testing.T) {
	phases := []ProjectPhase{PhaseSpec, PhaseEdit, PhaseBuild, PhaseDone}

	store := NewProjectStore(t.TempDir())
	p, err := store.Create("phase-test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for i, expected := range phases {
		if p.Phase != expected {
			t.Errorf("step %d: expected phase %q, got %q", i, expected, p.Phase)
		}
		if i < len(phases)-1 {
			p.Phase = phases[i+1]
			if err := store.Update(p); err != nil {
				t.Fatalf("unexpected error updating phase: %v", err)
			}
		}
	}
}

func TestProjectWithDOT(t *testing.T) {
	store := NewProjectStore(t.TempDir())

	p, err := store.Create("dot-project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Simulate starting from uploaded DOT (skip spec phase).
	p.Phase = PhaseEdit
	p.DOT = "digraph pipeline { start -> process -> end }"
	if err := store.Update(p); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, ok := store.Get(p.ID)
	if !ok {
		t.Fatal("expected to find project")
	}
	if got.Phase != PhaseEdit {
		t.Errorf("expected phase %q, got %q", PhaseEdit, got.Phase)
	}
	if got.DOT == "" {
		t.Error("expected non-empty DOT")
	}
}

func TestProjectJSON(t *testing.T) {
	p := &Project{
		ID:          "test-id-123",
		Name:        "json-test",
		CreatedAt:   time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC),
		Phase:       PhaseBuild,
		SpecID:      "spec-abc",
		DOT:         "digraph { a -> b }",
		Diagnostics: []string{"warning: unused node"},
		RunID:       "run-xyz",
		DataDir:     "/tmp/should-not-serialize",
	}

	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("unexpected error marshaling: %v", err)
	}

	// DataDir should not appear in JSON (json:"-" tag).
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unexpected error unmarshaling to map: %v", err)
	}
	if _, exists := raw["DataDir"]; exists {
		t.Error("DataDir should not be serialized in JSON")
	}

	// Round-trip deserialization.
	var restored Project
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unexpected error unmarshaling: %v", err)
	}
	if restored.ID != p.ID {
		t.Errorf("expected ID %q, got %q", p.ID, restored.ID)
	}
	if restored.Name != p.Name {
		t.Errorf("expected Name %q, got %q", p.Name, restored.Name)
	}
	if restored.Phase != p.Phase {
		t.Errorf("expected Phase %q, got %q", p.Phase, restored.Phase)
	}
	if restored.SpecID != p.SpecID {
		t.Errorf("expected SpecID %q, got %q", p.SpecID, restored.SpecID)
	}
	if restored.DOT != p.DOT {
		t.Errorf("expected DOT %q, got %q", p.DOT, restored.DOT)
	}
	if len(restored.Diagnostics) != 1 || restored.Diagnostics[0] != "warning: unused node" {
		t.Errorf("expected Diagnostics %v, got %v", p.Diagnostics, restored.Diagnostics)
	}
	if restored.RunID != p.RunID {
		t.Errorf("expected RunID %q, got %q", p.RunID, restored.RunID)
	}
	if restored.DataDir != "" {
		t.Errorf("expected empty DataDir after deserialization, got %q", restored.DataDir)
	}
}
