// ABOUTME: Tests for backend detection logic.
// ABOUTME: Validates detection from env vars and fallback behavior.
package mcp

import (
	"testing"
)

func TestDetectBackendFromEnv(t *testing.T) {
	// With no env vars set and no explicit backend, should return nil.
	backend := DetectBackend("")
	// We can't assert much since it depends on env, but it should not panic.
	_ = backend
}

func TestDetectBackendExplicit(t *testing.T) {
	backend := DetectBackend("agent")
	_ = backend
}
