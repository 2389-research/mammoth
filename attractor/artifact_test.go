// ABOUTME: Tests for the ArtifactStore named storage system.
// ABOUTME: Covers store/retrieve round-trips, listing, removal, clearing, and file-backed large artifacts.
package attractor

import (
	"bytes"
	"testing"
)

func TestNewArtifactStore(t *testing.T) {
	dir := t.TempDir()
	store := NewArtifactStore(dir)
	if store == nil {
		t.Fatal("NewArtifactStore returned nil")
	}
	items := store.List()
	if len(items) != 0 {
		t.Errorf("expected empty list, got %d items", len(items))
	}
}

func TestArtifactStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := NewArtifactStore(dir)

	data := []byte("hello, artifact world")
	info, err := store.Store("art-1", "greeting.txt", data)
	if err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	if info.ID != "art-1" {
		t.Errorf("expected ID 'art-1', got %q", info.ID)
	}
	if info.Name != "greeting.txt" {
		t.Errorf("expected Name 'greeting.txt', got %q", info.Name)
	}
	if info.SizeBytes != len(data) {
		t.Errorf("expected SizeBytes %d, got %d", len(data), info.SizeBytes)
	}
	if info.StoredAt.IsZero() {
		t.Error("expected non-zero StoredAt")
	}

	retrieved, err := store.Retrieve("art-1")
	if err != nil {
		t.Fatalf("Retrieve failed: %v", err)
	}
	if !bytes.Equal(retrieved, data) {
		t.Errorf("retrieved data mismatch: got %q, want %q", retrieved, data)
	}
}

func TestArtifactStoreHas(t *testing.T) {
	dir := t.TempDir()
	store := NewArtifactStore(dir)

	if store.Has("nonexistent") {
		t.Error("expected Has to return false for missing artifact")
	}

	_, err := store.Store("exists", "file.bin", []byte("data"))
	if err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	if !store.Has("exists") {
		t.Error("expected Has to return true for stored artifact")
	}
}

func TestArtifactStoreList(t *testing.T) {
	dir := t.TempDir()
	store := NewArtifactStore(dir)

	_, _ = store.Store("a", "alpha.txt", []byte("aaa"))
	_, _ = store.Store("b", "beta.txt", []byte("bbb"))
	_, _ = store.Store("c", "gamma.txt", []byte("ccc"))

	items := store.List()
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}

	ids := map[string]bool{}
	for _, item := range items {
		ids[item.ID] = true
	}
	for _, expected := range []string{"a", "b", "c"} {
		if !ids[expected] {
			t.Errorf("expected artifact %q in list", expected)
		}
	}
}

func TestArtifactStoreRemove(t *testing.T) {
	dir := t.TempDir()
	store := NewArtifactStore(dir)

	_, _ = store.Store("removeme", "temp.txt", []byte("temporary"))

	if err := store.Remove("removeme"); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	if store.Has("removeme") {
		t.Error("artifact should be removed")
	}

	_, err := store.Retrieve("removeme")
	if err == nil {
		t.Error("expected error retrieving removed artifact")
	}
}

func TestArtifactStoreClear(t *testing.T) {
	dir := t.TempDir()
	store := NewArtifactStore(dir)

	_, _ = store.Store("x", "x.txt", []byte("xxx"))
	_, _ = store.Store("y", "y.txt", []byte("yyy"))

	if err := store.Clear(); err != nil {
		t.Fatalf("Clear failed: %v", err)
	}

	items := store.List()
	if len(items) != 0 {
		t.Errorf("expected 0 items after clear, got %d", len(items))
	}
}

func TestArtifactStoreFileBacked(t *testing.T) {
	dir := t.TempDir()
	store := NewArtifactStore(dir)

	// Create data larger than the default 100KB threshold
	largeData := make([]byte, 150*1024) // 150KB
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	info, err := store.Store("large", "bigfile.bin", largeData)
	if err != nil {
		t.Fatalf("Store failed for large artifact: %v", err)
	}

	if !info.IsFileBacked {
		t.Error("expected large artifact to be file-backed")
	}
	if info.SizeBytes != len(largeData) {
		t.Errorf("expected SizeBytes %d, got %d", len(largeData), info.SizeBytes)
	}

	// Small data should NOT be file-backed
	smallData := []byte("tiny")
	smallInfo, err := store.Store("small", "tiny.txt", smallData)
	if err != nil {
		t.Fatalf("Store failed for small artifact: %v", err)
	}
	if smallInfo.IsFileBacked {
		t.Error("expected small artifact to NOT be file-backed")
	}

	// Verify large data round-trips correctly
	retrieved, err := store.Retrieve("large")
	if err != nil {
		t.Fatalf("Retrieve failed for large artifact: %v", err)
	}
	if !bytes.Equal(retrieved, largeData) {
		t.Error("large artifact data mismatch after round-trip")
	}

	// Verify removal cleans up the file
	if err := store.Remove("large"); err != nil {
		t.Fatalf("Remove failed for file-backed artifact: %v", err)
	}
	if store.Has("large") {
		t.Error("file-backed artifact should be removed")
	}
}
