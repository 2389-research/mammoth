// ABOUTME: Defines the CodergenBackend interface that decouples CodergenHandler from the agent loop.
// ABOUTME: Provides AgentRunConfig and AgentRunResult types for configuring and receiving agent outcomes.
package attractor

import (
	"context"
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
	Prompt       string // the prompt/instructions for the LLM
	Model        string // LLM model name (e.g., "claude-sonnet-4-5")
	Provider     string // LLM provider name (e.g., "anthropic", "openai", "gemini")
	WorkDir      string // working directory for file operations and commands
	Goal         string // pipeline-level goal for additional context
	NodeID       string // pipeline node identifier for logging/tracking
	MaxTurns     int    // maximum agent loop turns (0 = use default of 20)
	FidelityMode string // fidelity mode controlling conversation history management ("full", "compact", "truncate", "summary:*")
}

// AgentRunResult holds the outcome of an agent run.
type AgentRunResult struct {
	Output     string // final text output from the agent
	ToolCalls  int    // total number of tool calls made during the run
	TokensUsed int    // total tokens consumed across all LLM calls
	Success    bool   // whether the agent completed without errors
}
