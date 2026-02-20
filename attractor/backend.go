// ABOUTME: Defines the CodergenBackend interface that decouples CodergenHandler from the agent loop.
// ABOUTME: Provides AgentRunConfig and AgentRunResult types for configuring and receiving agent outcomes.
package attractor

import (
	"context"
	"strings"
	"time"
)

// CodergenBackend abstracts the LLM agent execution so that CodergenHandler
// does not depend directly on the agent or llm packages.
type CodergenBackend interface {
	// RunAgent executes an agent loop with the given configuration and returns
	// the result. The context controls cancellation and timeouts.
	RunAgent(ctx context.Context, config AgentRunConfig) (*AgentRunResult, error)
}

// AgentRunConfig holds all configuration needed to execute an agent run
// for a single codergen pipeline node.
type AgentRunConfig struct {
	Prompt       string            // the prompt/instructions for the LLM
	Model        string            // LLM model name (e.g., "claude-sonnet-4-5")
	Provider     string            // LLM provider name (e.g., "anthropic", "openai", "gemini")
	BaseURL      string            // custom API base URL (overrides provider default)
	WorkDir      string            // working directory for file operations and commands
	Goal         string            // pipeline-level goal for additional context
	NodeID       string            // pipeline node identifier for logging/tracking
	MaxTurns     int               // maximum agent loop turns (0 = use default of 20)
	FidelityMode string            // fidelity mode controlling conversation history management ("full", "compact", "truncate", "summary:*")
	SystemPrompt string            // user override appended to the agent's system prompt (empty = no override)
	EventHandler func(EngineEvent) // engine event callback for agent-level observability
}

// TokenUsage tracks granular token consumption across an agent run, broken down
// by input, output, reasoning, and cache categories.
type TokenUsage struct {
	InputTokens      int `json:"input_tokens"`
	OutputTokens     int `json:"output_tokens"`
	TotalTokens      int `json:"total_tokens"`
	ReasoningTokens  int `json:"reasoning_tokens"`
	CacheReadTokens  int `json:"cache_read_tokens"`
	CacheWriteTokens int `json:"cache_write_tokens"`
}

// Add combines two TokenUsage values by summing all fields.
func (u TokenUsage) Add(other TokenUsage) TokenUsage {
	return TokenUsage{
		InputTokens:      u.InputTokens + other.InputTokens,
		OutputTokens:     u.OutputTokens + other.OutputTokens,
		TotalTokens:      u.TotalTokens + other.TotalTokens,
		ReasoningTokens:  u.ReasoningTokens + other.ReasoningTokens,
		CacheReadTokens:  u.CacheReadTokens + other.CacheReadTokens,
		CacheWriteTokens: u.CacheWriteTokens + other.CacheWriteTokens,
	}
}

// ToolCallEntry records details about a single tool invocation during an agent run.
type ToolCallEntry struct {
	ToolName string        `json:"tool_name"`
	CallID   string        `json:"call_id"`
	Duration time.Duration `json:"duration"`
	Output   string        `json:"output"` // truncated to 500 chars
}

// AgentRunResult holds the outcome of an agent run.
type AgentRunResult struct {
	Output      string          // final text output from the agent
	ToolCalls   int             // total number of tool calls made during the run
	TokensUsed  int             // total tokens consumed across all LLM calls
	Success     bool            // whether the agent completed without errors
	ToolCallLog []ToolCallEntry // individual tool call details
	TurnCount   int             // LLM call rounds
	Usage       TokenUsage      // granular per-category token breakdown
}

// DetectOutcomeMarker scans text for outcome markers in common formats:
//
//	OUTCOME:FAIL, outcome=FAIL, outcome:fail, OUTCOME=SUCCESS, etc.
//
// Returns ("fail", true) or ("success"/"pass", true) if a marker is found,
// or ("", false) if no marker is present. The check is case-insensitive and
// accepts both ":" and "=" as separators. When both PASS/SUCCESS and FAIL
// markers appear, FAIL wins (last-writer-wins would be fragile).
func DetectOutcomeMarker(text string) (string, bool) {
	upper := strings.ToUpper(text)
	hasFail := strings.Contains(upper, "OUTCOME:FAIL") ||
		strings.Contains(upper, "OUTCOME=FAIL")
	hasPass := strings.Contains(upper, "OUTCOME:PASS") ||
		strings.Contains(upper, "OUTCOME=PASS") ||
		strings.Contains(upper, "OUTCOME:SUCCESS") ||
		strings.Contains(upper, "OUTCOME=SUCCESS")

	if hasFail {
		return "fail", true
	}
	if hasPass {
		return "success", true
	}
	return "", false
}
