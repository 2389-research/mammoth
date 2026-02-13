// ABOUTME: Tests for the high-level StorageManager filesystem operations.
// ABOUTME: Covers directory creation, spec discovery, spec dir creation, and export writing.
package store_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/2389-research/mammoth/spec/core"
	"github.com/2389-research/mammoth/spec/store"
	"github.com/oklog/ulid/v2"
)

func TestStorageManagerCreatesDirectories(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "mammoth_home")

	mgr, err := store.NewStorageManager(home)
	if err != nil {
		t.Fatalf("NewStorageManager: %v", err)
	}

	if _, err := os.Stat(filepath.Join(home, "specs")); os.IsNotExist(err) {
		t.Error("expected specs directory to exist")
	}
	if mgr.Home() != home {
		t.Errorf("Home() = %q, want %q", mgr.Home(), home)
	}
}

func TestStorageManagerCreatesSpecDir(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "mammoth_home")
	mgr, err := store.NewStorageManager(home)
	if err != nil {
		t.Fatalf("NewStorageManager: %v", err)
	}

	specID := core.NewULID()
	specDir, err := mgr.CreateSpecDir(specID)
	if err != nil {
		t.Fatalf("CreateSpecDir: %v", err)
	}

	if _, err := os.Stat(specDir); os.IsNotExist(err) {
		t.Error("expected spec directory to exist")
	}
	if _, err := os.Stat(filepath.Join(specDir, "snapshots")); os.IsNotExist(err) {
		t.Error("expected snapshots subdirectory to exist")
	}
	if _, err := os.Stat(filepath.Join(specDir, "exports")); os.IsNotExist(err) {
		t.Error("expected exports subdirectory to exist")
	}

	// GetSpecDir should return the same path
	if got := mgr.GetSpecDir(specID); got != specDir {
		t.Errorf("GetSpecDir() = %q, want %q", got, specDir)
	}
}

func TestStorageManagerListSpecDirs(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "mammoth_home")
	mgr, err := store.NewStorageManager(home)
	if err != nil {
		t.Fatalf("NewStorageManager: %v", err)
	}

	// Create 3 spec dirs
	id1 := core.NewULID()
	id2 := core.NewULID()
	id3 := core.NewULID()
	ids := []ulid.ULID{id1, id2, id3}
	for _, id := range ids {
		if _, err := mgr.CreateSpecDir(id); err != nil {
			t.Fatalf("CreateSpecDir: %v", err)
		}
	}

	// Create a non-ULID directory that should be skipped
	_ = os.MkdirAll(filepath.Join(home, "specs", "not-a-ulid"), 0o755)

	// Create a file that should be skipped
	_ = os.WriteFile(filepath.Join(home, "specs", "some-file.txt"), []byte("hi"), 0o644)

	dirs, err := mgr.ListSpecDirs()
	if err != nil {
		t.Fatalf("ListSpecDirs: %v", err)
	}
	if len(dirs) != 3 {
		t.Fatalf("expected 3 spec dirs, got %d", len(dirs))
	}

	// Verify all 3 IDs are present
	foundIDs := map[string]bool{}
	for _, sd := range dirs {
		foundIDs[sd.SpecID.String()] = true
	}
	for _, id := range ids {
		if !foundIDs[id.String()] {
			t.Errorf("expected to find spec dir for %s", id.String())
		}
	}
}

func TestStorageManagerListSpecDirsEmpty(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "mammoth_home")
	mgr, err := store.NewStorageManager(home)
	if err != nil {
		t.Fatalf("NewStorageManager: %v", err)
	}

	dirs, err := mgr.ListSpecDirs()
	if err != nil {
		t.Fatalf("ListSpecDirs: %v", err)
	}
	if len(dirs) != 0 {
		t.Errorf("expected 0 dirs, got %d", len(dirs))
	}
}

func TestWriteExports(t *testing.T) {
	dir := t.TempDir()
	specDir := filepath.Join(dir, "spec_dir")
	_ = os.MkdirAll(specDir, 0o755)

	state := core.NewSpecState()
	state.Core = &core.SpecCore{
		SpecID:   core.NewULID(),
		Title:    "Export Spec",
		OneLiner: "For export testing",
		Goal:     "Verify exports",
	}

	card := core.NewCard("idea", "Export Card", "human")
	card.Lane = "Plan"
	state.Cards.Set(card.CardID, card)

	if err := store.WriteExports(specDir, state); err != nil {
		t.Fatalf("WriteExports: %v", err)
	}

	exportsDir := filepath.Join(specDir, "exports")
	mdPath := filepath.Join(exportsDir, "spec.md")
	if _, err := os.Stat(mdPath); os.IsNotExist(err) {
		t.Error("expected spec.md to exist")
	}

	md, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("ReadFile spec.md: %v", err)
	}

	mdStr := string(md)
	if !contains(mdStr, "# Export Spec") {
		t.Error("markdown should contain spec title")
	}
	if !contains(mdStr, "Export Card") {
		t.Error("markdown should contain card title")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
