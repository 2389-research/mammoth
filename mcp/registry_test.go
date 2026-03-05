// ABOUTME: Tests for RunRegistry in-memory run tracking.
// ABOUTME: Validates create, get, list, and concurrent access.
package mcp

import (
	"sync"
	"testing"
)

func TestRegistryCreateAndGet(t *testing.T) {
	reg := NewRunRegistry()
	run := reg.Create("digraph { start -> end }", RunConfig{})
	if run.ID == "" {
		t.Fatal("expected non-empty run ID")
	}
	if run.Status != StatusRunning {
		t.Errorf("expected status %q, got %q", StatusRunning, run.Status)
	}
	got, ok := reg.Get(run.ID)
	if !ok {
		t.Fatal("expected to find run by ID")
	}
	if got.ID != run.ID {
		t.Errorf("ID mismatch: got %q, want %q", got.ID, run.ID)
	}
}

func TestRegistryGetNotFound(t *testing.T) {
	reg := NewRunRegistry()
	_, ok := reg.Get("nonexistent")
	if ok {
		t.Fatal("expected not found")
	}
}

func TestRegistryList(t *testing.T) {
	reg := NewRunRegistry()
	reg.Create("digraph { a -> b }", RunConfig{})
	reg.Create("digraph { c -> d }", RunConfig{})
	runs := reg.List()
	if len(runs) != 2 {
		t.Errorf("expected 2 runs, got %d", len(runs))
	}
}

func TestRegistryConcurrentAccess(t *testing.T) {
	reg := NewRunRegistry()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			run := reg.Create("digraph { x -> y }", RunConfig{})
			reg.Get(run.ID)
			reg.List()
		}()
	}
	wg.Wait()
	runs := reg.List()
	if len(runs) != 50 {
		t.Errorf("expected 50 runs, got %d", len(runs))
	}
}
