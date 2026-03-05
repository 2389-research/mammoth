// ABOUTME: Backend detection for the MCP server.
// ABOUTME: Selects the LLM backend (agent, claude-code) from env vars or explicit override.
package mcp

import (
	"fmt"
	"os"

	"github.com/2389-research/mammoth/attractor"
)

// DetectBackend selects the agent backend based on the backendType parameter
// or MAMMOTH_BACKEND env var.
func DetectBackend(backendType string) attractor.CodergenBackend {
	if backendType == "" {
		backendType = os.Getenv("MAMMOTH_BACKEND")
	}
	if backendType == "claude-code" {
		backend, err := attractor.NewClaudeCodeBackend()
		if err != nil {
			fmt.Fprintf(os.Stderr, "[mcp] claude-code backend: %v, falling back to agent\n", err)
		} else {
			return backend
		}
	}
	keys := []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GEMINI_API_KEY"}
	for _, k := range keys {
		if os.Getenv(k) != "" {
			return &attractor.AgentBackend{}
		}
	}
	return nil
}
