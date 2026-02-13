// ABOUTME: Tests for the render cache covering TTL-based expiry, cache hits, and concurrent access.
// ABOUTME: Validates RenderCache wraps RenderDOTSource with sha256-keyed in-memory caching.
package render

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeDOTRenderer is a test double that counts invocations and returns fixed output.
type fakeDOTRenderer struct {
	callCount atomic.Int64
	output    []byte
	err       error
}

func (f *fakeDOTRenderer) render(ctx context.Context, dotText string, format string) ([]byte, error) {
	f.callCount.Add(1)
	if f.err != nil {
		return nil, f.err
	}
	return f.output, nil
}

func TestRenderCacheReturnsCachedResult(t *testing.T) {
	renderer := &fakeDOTRenderer{output: []byte("<svg>test</svg>")}
	cache := NewRenderCache(renderer.render, 5*time.Minute)

	dotText := "digraph test { a -> b }"
	ctx := context.Background()

	// First call should invoke the renderer
	data1, err := cache.RenderDOTSource(ctx, dotText, "svg")
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if string(data1) != "<svg>test</svg>" {
		t.Errorf("expected <svg>test</svg>, got %s", string(data1))
	}
	if renderer.callCount.Load() != 1 {
		t.Errorf("expected 1 renderer call, got %d", renderer.callCount.Load())
	}

	// Second call with same input should use cache
	data2, err := cache.RenderDOTSource(ctx, dotText, "svg")
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}
	if string(data2) != "<svg>test</svg>" {
		t.Errorf("expected cached result, got %s", string(data2))
	}
	if renderer.callCount.Load() != 1 {
		t.Errorf("expected still 1 renderer call (cached), got %d", renderer.callCount.Load())
	}
}

func TestRenderCacheDifferentFormatsDifferentEntries(t *testing.T) {
	renderer := &fakeDOTRenderer{output: []byte("output")}
	cache := NewRenderCache(renderer.render, 5*time.Minute)

	dotText := "digraph test { a -> b }"
	ctx := context.Background()

	cache.RenderDOTSource(ctx, dotText, "svg")
	cache.RenderDOTSource(ctx, dotText, "png")

	// Different formats should result in separate cache entries and separate renderer calls
	if renderer.callCount.Load() != 2 {
		t.Errorf("expected 2 renderer calls for different formats, got %d", renderer.callCount.Load())
	}
}

func TestRenderCacheDifferentInputsDifferentEntries(t *testing.T) {
	renderer := &fakeDOTRenderer{output: []byte("output")}
	cache := NewRenderCache(renderer.render, 5*time.Minute)

	ctx := context.Background()

	cache.RenderDOTSource(ctx, "digraph a { x -> y }", "svg")
	cache.RenderDOTSource(ctx, "digraph b { p -> q }", "svg")

	if renderer.callCount.Load() != 2 {
		t.Errorf("expected 2 renderer calls for different inputs, got %d", renderer.callCount.Load())
	}
}

func TestRenderCacheTTLExpiry(t *testing.T) {
	renderer := &fakeDOTRenderer{output: []byte("output")}
	// Use a very short TTL for testing
	cache := NewRenderCache(renderer.render, 50*time.Millisecond)

	dotText := "digraph test { a -> b }"
	ctx := context.Background()

	cache.RenderDOTSource(ctx, dotText, "svg")
	if renderer.callCount.Load() != 1 {
		t.Fatalf("expected 1 call, got %d", renderer.callCount.Load())
	}

	// Wait for TTL to expire
	time.Sleep(100 * time.Millisecond)

	// Should re-render after expiry
	cache.RenderDOTSource(ctx, dotText, "svg")
	if renderer.callCount.Load() != 2 {
		t.Errorf("expected 2 calls after TTL expiry, got %d", renderer.callCount.Load())
	}
}

func TestRenderCacheDoesNotCacheErrors(t *testing.T) {
	renderer := &fakeDOTRenderer{err: fmt.Errorf("render failed")}
	cache := NewRenderCache(renderer.render, 5*time.Minute)

	dotText := "digraph test { a -> b }"
	ctx := context.Background()

	_, err := cache.RenderDOTSource(ctx, dotText, "svg")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Fix the renderer
	renderer.err = nil
	renderer.output = []byte("fixed output")

	// Should re-render (not serve cached error)
	data, err := cache.RenderDOTSource(ctx, dotText, "svg")
	if err != nil {
		t.Fatalf("expected success after fix, got: %v", err)
	}
	if string(data) != "fixed output" {
		t.Errorf("expected 'fixed output', got %s", string(data))
	}
}

func TestRenderCacheConcurrentAccess(t *testing.T) {
	renderer := &fakeDOTRenderer{output: []byte("concurrent output")}
	cache := NewRenderCache(renderer.render, 5*time.Minute)

	dotText := "digraph test { a -> b }"
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			data, err := cache.RenderDOTSource(ctx, dotText, "svg")
			if err != nil {
				t.Errorf("concurrent call failed: %v", err)
				return
			}
			if string(data) != "concurrent output" {
				t.Errorf("expected 'concurrent output', got %s", string(data))
			}
		}()
	}
	wg.Wait()

	// Due to concurrency, there might be a few calls, but not 20
	// The exact number depends on timing, but it should be significantly less than 20
	// At minimum, only the first should trigger a real render (subsequent should hit cache)
	if renderer.callCount.Load() > 5 {
		t.Errorf("expected much fewer than 20 renderer calls with caching, got %d", renderer.callCount.Load())
	}
}

func TestRenderCacheKeyIncludesFormatAndContent(t *testing.T) {
	// Verify the cache key generation logic is based on sha256 of content + format
	dotText := "digraph test { a -> b }"
	format := "svg"

	expected := fmt.Sprintf("%x:%s", sha256.Sum256([]byte(dotText)), format)

	key := cacheKey(dotText, format)
	if key != expected {
		t.Errorf("expected cache key %q, got %q", expected, key)
	}
}

func TestRenderCacheDOTFormatPassthrough(t *testing.T) {
	// DOT format should still be cached (it goes through the renderer func)
	renderer := &fakeDOTRenderer{output: []byte("digraph test { a -> b }")}
	cache := NewRenderCache(renderer.render, 5*time.Minute)

	ctx := context.Background()

	data, err := cache.RenderDOTSource(ctx, "digraph test { a -> b }", "dot")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != "digraph test { a -> b }" {
		t.Errorf("unexpected output: %s", string(data))
	}

	// Second call should be cached
	cache.RenderDOTSource(ctx, "digraph test { a -> b }", "dot")
	if renderer.callCount.Load() != 1 {
		t.Errorf("expected 1 renderer call for dot format, got %d", renderer.callCount.Load())
	}
}

func TestRenderCacheLen(t *testing.T) {
	renderer := &fakeDOTRenderer{output: []byte("out")}
	cache := NewRenderCache(renderer.render, 5*time.Minute)

	ctx := context.Background()

	if cache.Len() != 0 {
		t.Errorf("expected 0 entries initially, got %d", cache.Len())
	}

	cache.RenderDOTSource(ctx, "digraph a { x -> y }", "svg")
	cache.RenderDOTSource(ctx, "digraph b { p -> q }", "svg")

	if cache.Len() != 2 {
		t.Errorf("expected 2 entries, got %d", cache.Len())
	}
}

func TestRenderCacheClear(t *testing.T) {
	renderer := &fakeDOTRenderer{output: []byte("out")}
	cache := NewRenderCache(renderer.render, 5*time.Minute)

	ctx := context.Background()

	cache.RenderDOTSource(ctx, "digraph a { x -> y }", "svg")
	cache.Clear()

	if cache.Len() != 0 {
		t.Errorf("expected 0 entries after clear, got %d", cache.Len())
	}

	// After clearing, a re-render should happen
	cache.RenderDOTSource(ctx, "digraph a { x -> y }", "svg")
	if renderer.callCount.Load() != 2 {
		t.Errorf("expected 2 renderer calls after clear, got %d", renderer.callCount.Load())
	}
}
