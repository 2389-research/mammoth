// ABOUTME: Tests for the Context thread-safe key-value store and Outcome/StageStatus types.
// ABOUTME: Covers get/set, snapshots, cloning, log appending, and concurrent access.
package attractor

import (
	"fmt"
	"sync"
	"testing"
)

func TestNewContext(t *testing.T) {
	ctx := NewContext()
	if ctx == nil {
		t.Fatal("NewContext returned nil")
	}
	snap := ctx.Snapshot()
	if len(snap) != 0 {
		t.Errorf("expected empty snapshot, got %v", snap)
	}
	logs := ctx.Logs()
	if len(logs) != 0 {
		t.Errorf("expected empty logs, got %v", logs)
	}
}

func TestContextSetGet(t *testing.T) {
	ctx := NewContext()
	ctx.Set("key1", "value1")
	ctx.Set("key2", 42)

	got1 := ctx.Get("key1")
	if got1 != "value1" {
		t.Errorf("expected 'value1', got %v", got1)
	}

	got2 := ctx.Get("key2")
	if got2 != 42 {
		t.Errorf("expected 42, got %v", got2)
	}

	gotNil := ctx.Get("nonexistent")
	if gotNil != nil {
		t.Errorf("expected nil for missing key, got %v", gotNil)
	}
}

func TestContextGetString(t *testing.T) {
	ctx := NewContext()
	ctx.Set("name", "makeatron")

	got := ctx.GetString("name", "default")
	if got != "makeatron" {
		t.Errorf("expected 'makeatron', got %q", got)
	}
}

func TestContextGetStringDefault(t *testing.T) {
	ctx := NewContext()

	got := ctx.GetString("missing", "fallback")
	if got != "fallback" {
		t.Errorf("expected 'fallback', got %q", got)
	}

	// Non-string value should also return default
	ctx.Set("number", 123)
	got = ctx.GetString("number", "fallback")
	if got != "fallback" {
		t.Errorf("expected 'fallback' for non-string value, got %q", got)
	}
}

func TestContextAppendLog(t *testing.T) {
	ctx := NewContext()
	ctx.AppendLog("step 1 done")
	ctx.AppendLog("step 2 done")

	logs := ctx.Logs()
	if len(logs) != 2 {
		t.Fatalf("expected 2 logs, got %d", len(logs))
	}
	if logs[0] != "step 1 done" {
		t.Errorf("expected 'step 1 done', got %q", logs[0])
	}
	if logs[1] != "step 2 done" {
		t.Errorf("expected 'step 2 done', got %q", logs[1])
	}
}

func TestContextSnapshot(t *testing.T) {
	ctx := NewContext()
	ctx.Set("a", "1")
	ctx.Set("b", "2")

	snap := ctx.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("expected 2 keys in snapshot, got %d", len(snap))
	}
	if snap["a"] != "1" {
		t.Errorf("expected snapshot['a']='1', got %v", snap["a"])
	}

	// Mutating the snapshot should not affect the context
	snap["a"] = "mutated"
	if ctx.Get("a") != "1" {
		t.Error("mutating snapshot affected the original context")
	}
}

func TestContextClone(t *testing.T) {
	ctx := NewContext()
	ctx.Set("x", "original")
	ctx.AppendLog("log entry")

	cloned := ctx.Clone()

	// Cloned context has same values
	if cloned.Get("x") != "original" {
		t.Errorf("cloned context missing key 'x'")
	}
	logs := cloned.Logs()
	if len(logs) != 1 || logs[0] != "log entry" {
		t.Errorf("cloned context has wrong logs: %v", logs)
	}

	// Modifying clone does not affect original
	cloned.Set("x", "modified")
	cloned.AppendLog("new log")
	if ctx.Get("x") != "original" {
		t.Error("modifying clone affected original context")
	}
	if len(ctx.Logs()) != 1 {
		t.Error("modifying clone logs affected original context")
	}
}

func TestContextApplyUpdates(t *testing.T) {
	ctx := NewContext()
	ctx.Set("existing", "old")

	updates := map[string]any{
		"existing": "new",
		"added":    "fresh",
	}
	ctx.ApplyUpdates(updates)

	if ctx.Get("existing") != "new" {
		t.Errorf("expected 'new', got %v", ctx.Get("existing"))
	}
	if ctx.Get("added") != "fresh" {
		t.Errorf("expected 'fresh', got %v", ctx.Get("added"))
	}
}

func TestContextConcurrency(t *testing.T) {
	ctx := NewContext()
	var wg sync.WaitGroup
	iterations := 100

	// Concurrent writers
	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			ctx.Set(fmt.Sprintf("key-%d", n), n)
			ctx.AppendLog(fmt.Sprintf("log-%d", n))
		}(i)
	}

	// Concurrent readers
	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = ctx.Get(fmt.Sprintf("key-%d", n))
			_ = ctx.GetString(fmt.Sprintf("key-%d", n), "")
			_ = ctx.Snapshot()
			_ = ctx.Logs()
		}(i)
	}

	wg.Wait()

	// Verify all writes landed
	snap := ctx.Snapshot()
	if len(snap) != iterations {
		t.Errorf("expected %d keys, got %d", iterations, len(snap))
	}
	logs := ctx.Logs()
	if len(logs) != iterations {
		t.Errorf("expected %d logs, got %d", iterations, len(logs))
	}
}
