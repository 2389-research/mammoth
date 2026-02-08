// ABOUTME: Thread-safe key-value context store shared across pipeline stages.
// ABOUTME: Also defines StageStatus and Outcome types for node execution results.
package attractor

import (
	"sync"
)

// StageStatus represents the outcome of executing a node.
type StageStatus string

const (
	StatusSuccess        StageStatus = "success"
	StatusFail           StageStatus = "fail"
	StatusPartialSuccess StageStatus = "partial_success"
	StatusRetry          StageStatus = "retry"
	StatusSkipped        StageStatus = "skipped"
)

// Outcome is the result of executing a node handler.
type Outcome struct {
	Status           StageStatus
	PreferredLabel   string
	SuggestedNextIDs []string
	ContextUpdates   map[string]any
	Notes            string
	FailureReason    string
}

// Context is a thread-safe key-value store shared across pipeline stages.
type Context struct {
	values map[string]any
	logs   []string
	mu     sync.RWMutex
}

// NewContext creates a new empty Context.
func NewContext() *Context {
	return &Context{
		values: make(map[string]any),
		logs:   make([]string, 0),
	}
}

// Set stores a value under the given key.
func (c *Context) Set(key string, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values[key] = value
}

// Get retrieves the value for the given key, or nil if not found.
func (c *Context) Get(key string) any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.values[key]
}

// GetString retrieves the string value for the given key.
// If the key is missing or the value is not a string, defaultVal is returned.
func (c *Context) GetString(key string, defaultVal string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.values[key]
	if !ok {
		return defaultVal
	}
	s, ok := v.(string)
	if !ok {
		return defaultVal
	}
	return s
}

// AppendLog adds an entry to the context's log.
func (c *Context) AppendLog(entry string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.logs = append(c.logs, entry)
}

// Snapshot returns a shallow copy of all key-value pairs.
func (c *Context) Snapshot() map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	snap := make(map[string]any, len(c.values))
	for k, v := range c.values {
		snap[k] = v
	}
	return snap
}

// Clone creates a deep copy of the Context with independent values and logs.
func (c *Context) Clone() *Context {
	c.mu.RLock()
	defer c.mu.RUnlock()
	cloned := &Context{
		values: make(map[string]any, len(c.values)),
		logs:   make([]string, len(c.logs)),
	}
	for k, v := range c.values {
		cloned.values[k] = v
	}
	copy(cloned.logs, c.logs)
	return cloned
}

// ApplyUpdates merges the given key-value pairs into the context.
func (c *Context) ApplyUpdates(updates map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, v := range updates {
		c.values[k] = v
	}
}

// Logs returns a copy of the context's log entries.
func (c *Context) Logs() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]string, len(c.logs))
	copy(result, c.logs)
	return result
}
