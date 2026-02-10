// ABOUTME: In-memory render cache that wraps a DOT rendering function with sha256-keyed caching.
// ABOUTME: Supports TTL-based expiry, concurrent access, and manual cache clearing.
package render

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync"
	"time"
)

// RenderFunc is the signature for a DOT rendering function that the cache wraps.
type RenderFunc func(ctx context.Context, dotText string, format string) ([]byte, error)

// cacheEntry holds a single cached render result with its creation timestamp.
type cacheEntry struct {
	data      []byte
	createdAt time.Time
}

// RenderCache wraps a DOT rendering function with an in-memory cache.
// Cache keys are derived from the sha256 hash of the DOT content combined with the format.
// Entries expire after the configured TTL.
type RenderCache struct {
	renderFn RenderFunc
	ttl      time.Duration
	entries  map[string]*cacheEntry
	mu       sync.RWMutex
}

// NewRenderCache creates a RenderCache wrapping the given rendering function.
// Cached entries expire after the specified TTL duration.
func NewRenderCache(renderFn RenderFunc, ttl time.Duration) *RenderCache {
	return &RenderCache{
		renderFn: renderFn,
		ttl:      ttl,
		entries:  make(map[string]*cacheEntry),
	}
}

// RenderDOTSource renders DOT text to the specified format, returning cached results
// when available and not expired. Errors are never cached.
func (c *RenderCache) RenderDOTSource(ctx context.Context, dotText string, format string) ([]byte, error) {
	key := cacheKey(dotText, format)

	// Check cache under read lock
	c.mu.RLock()
	if entry, ok := c.entries[key]; ok {
		if time.Since(entry.createdAt) < c.ttl {
			data := entry.data
			c.mu.RUnlock()
			return data, nil
		}
	}
	c.mu.RUnlock()

	// Cache miss or expired: render
	data, err := c.renderFn(ctx, dotText, format)
	if err != nil {
		return nil, err
	}

	// Store result in cache
	c.mu.Lock()
	c.entries[key] = &cacheEntry{
		data:      data,
		createdAt: time.Now(),
	}
	c.mu.Unlock()

	return data, nil
}

// Len returns the number of entries currently in the cache (including expired ones).
func (c *RenderCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// Clear removes all entries from the cache.
func (c *RenderCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]*cacheEntry)
}

// cacheKey generates a deterministic cache key from DOT text content and output format.
// Uses sha256 of the DOT content combined with the format string.
func cacheKey(dotText string, format string) string {
	return fmt.Sprintf("%x:%s", sha256.Sum256([]byte(dotText)), format)
}
