// ABOUTME: Selects the codergen backend (Claude Code, agent, mux) from environment variables.
// ABOUTME: Used by the web server to configure which LLM backend drives pipeline execution.
package web

import (
	"log"
	"os"

	"github.com/2389-research/mammoth/attractor"
)

// detectBackendFromEnv selects a codergen backend for web/serve mode.
// Precedence:
// 1) MAMMOTH_BACKEND=claude-code (if available)
// 2) Any API key present -> AgentBackend
// 3) nil (preflight will fail for codergen nodes)
func detectBackendFromEnv(verbose bool) attractor.CodergenBackend {
	backendType := os.Getenv("MAMMOTH_BACKEND")
	if backendType == "claude-code" {
		backend, err := attractor.NewClaudeCodeBackend()
		if err == nil {
			return backend
		}
		log.Printf("component=web.backend action=claude_code_unavailable fallback=agent_backend err=%v", err)
	}

	keys := []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GEMINI_API_KEY"}
	for _, key := range keys {
		if os.Getenv(key) != "" {
			return &attractor.AgentBackend{}
		}
	}

	return nil
}
