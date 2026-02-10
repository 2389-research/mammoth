// ABOUTME: AgentBackend wires CodergenBackend to the real agent loop and LLM SDK.
// ABOUTME: Creates agent sessions, configures provider profiles, and runs ProcessInput for each codergen node.
package attractor

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/2389-research/makeatron/agent"
	"github.com/2389-research/makeatron/llm"
)

// defaultAgentMaxTurns is the default maximum number of agent loop turns
// when no explicit MaxTurns is specified in the config.
const defaultAgentMaxTurns = 20

// AgentBackend implements CodergenBackend by wiring to the real agent loop.
// It creates an agent.Session, configures the appropriate provider profile,
// sets up the execution environment, and runs the agent loop to completion.
type AgentBackend struct {
	// Client is the LLM client to use for making API calls. If nil,
	// a client is created from environment variables at runtime.
	Client *llm.Client
}

// RunAgent executes an agent loop with the given configuration.
// It creates a session, selects the provider profile, sets up the execution
// environment, and runs ProcessInput until the agent completes or hits limits.
func (b *AgentBackend) RunAgent(ctx context.Context, config AgentRunConfig) (*AgentRunResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Resolve the LLM client
	client := b.Client
	if client == nil {
		envClient, err := createClientFromEnv(config.Provider)
		if err != nil {
			return nil, fmt.Errorf("create LLM client: %w", err)
		}
		client = envClient
		defer client.Close()
	}

	// Resolve the working directory
	workDir := config.WorkDir
	if workDir == "" {
		var err error
		workDir, err = os.MkdirTemp("", "makeatron-codergen-*")
		if err != nil {
			return nil, fmt.Errorf("create temp work dir: %w", err)
		}
	}

	// Set up execution environment with full env inheritance so the agent
	// can access API keys, PATH, and other variables needed for its work
	env := agent.NewLocalExecutionEnvironment(workDir, agent.WithEnvPolicy(agent.EnvPolicyInheritAll))
	if err := env.Initialize(); err != nil {
		return nil, fmt.Errorf("initialize execution environment: %w", err)
	}

	// Select the provider profile based on config
	profile := selectProfile(config.Provider, config.Model)

	// Configure the session
	maxTurns := config.MaxTurns
	if maxTurns <= 0 {
		maxTurns = defaultAgentMaxTurns
	}

	sessionConfig := agent.DefaultSessionConfig()
	sessionConfig.MaxTurns = maxTurns * 3 // turns include user, assistant, and tool results
	sessionConfig.MaxToolRoundsPerInput = maxTurns
	sessionConfig.FidelityMode = config.FidelityMode

	session := agent.NewSession(sessionConfig)
	defer session.Close()

	// Build the user input from the prompt and goal
	userInput := buildAgentInput(config.Prompt, config.Goal, config.NodeID)

	// Run the agent loop
	err := agent.ProcessInput(ctx, session, profile, env, client, userInput)
	if err != nil {
		return nil, fmt.Errorf("agent processing failed: %w", err)
	}

	// Extract results from the session
	return extractResult(session), nil
}

// createClientFromEnv creates an LLM client configured from environment variables.
// It checks for API keys and returns a clear error if none are found.
func createClientFromEnv(preferredProvider string) (*llm.Client, error) {
	// Check for API keys based on preferred provider
	providers := []struct {
		envVar   string
		name     string
		priority int
	}{
		{"ANTHROPIC_API_KEY", "anthropic", 0},
		{"OPENAI_API_KEY", "openai", 0},
		{"GEMINI_API_KEY", "gemini", 0},
	}

	// Boost priority of preferred provider
	for i := range providers {
		if providers[i].name == preferredProvider {
			providers[i].priority = 1
		}
	}

	var opts []llm.ClientOption
	found := false

	for _, p := range providers {
		key := os.Getenv(p.envVar)
		if key != "" {
			adapter := createProviderAdapter(p.name, key)
			opts = append(opts, llm.WithProvider(p.name, adapter))
			found = true
		}
	}

	if !found {
		return nil, fmt.Errorf("no API keys found in environment (checked ANTHROPIC_API_KEY, OPENAI_API_KEY, GEMINI_API_KEY)")
	}

	// Set the preferred provider as default if available
	if preferredProvider != "" {
		key := ""
		switch preferredProvider {
		case "anthropic":
			key = os.Getenv("ANTHROPIC_API_KEY")
		case "openai":
			key = os.Getenv("OPENAI_API_KEY")
		case "gemini":
			key = os.Getenv("GEMINI_API_KEY")
		}
		if key != "" {
			opts = append(opts, llm.WithDefaultProvider(preferredProvider))
		}
	}

	return llm.NewClient(opts...), nil
}

// createProviderAdapter creates a real provider adapter for the given provider.
// It delegates to the appropriate constructor in the llm package based on
// the provider name. Unknown providers default to Anthropic.
func createProviderAdapter(name, apiKey string) llm.ProviderAdapter {
	switch name {
	case "anthropic":
		return llm.NewAnthropicAdapter(apiKey)
	case "openai":
		return llm.NewOpenAIAdapter(apiKey)
	case "gemini":
		return llm.NewGeminiAdapter(apiKey)
	default:
		return llm.NewAnthropicAdapter(apiKey)
	}
}

// selectProfile creates the appropriate ProviderProfile for the given provider and model.
func selectProfile(provider, model string) agent.ProviderProfile {
	switch strings.ToLower(provider) {
	case "openai":
		return agent.NewOpenAIProfile(model)
	case "gemini":
		return agent.NewGeminiProfile(model)
	case "anthropic":
		return agent.NewAnthropicProfile(model)
	default:
		// Default to Anthropic profile
		return agent.NewAnthropicProfile(model)
	}
}

// buildAgentInput constructs the user message sent to the agent loop,
// incorporating the node prompt, pipeline goal, and node context.
func buildAgentInput(prompt, goal, nodeID string) string {
	var b strings.Builder

	if goal != "" {
		b.WriteString("## Pipeline Goal\n\n")
		b.WriteString(goal)
		b.WriteString("\n\n")
	}

	if nodeID != "" {
		b.WriteString("## Current Stage: ")
		b.WriteString(nodeID)
		b.WriteString("\n\n")
	}

	b.WriteString("## Task\n\n")
	b.WriteString(prompt)

	return b.String()
}

// extractResult builds an AgentRunResult from the completed session history.
// This is safe to call after ProcessInput returns because the session is idle
// and no concurrent goroutines are modifying the history.
//
// The last assistant message is checked for OUTCOME:PASS or OUTCOME:FAIL
// markers. If OUTCOME:FAIL is found, Success is set to false. If no marker
// is present, Success defaults to true (backward compatible with codergen nodes).
func extractResult(session *agent.Session) *AgentRunResult {
	result := &AgentRunResult{
		Success: true,
	}

	// Walk the history to extract the last assistant text and count tool calls/tokens
	for _, turn := range session.History {
		switch t := turn.(type) {
		case agent.AssistantTurn:
			if t.Content != "" {
				result.Output = t.Content
			}
			result.ToolCalls += len(t.ToolCalls)
			result.TokensUsed += t.Usage.TotalTokens
		}
	}

	// Check the last assistant output for OUTCOME markers
	if result.Output != "" {
		if strings.Contains(result.Output, "OUTCOME:FAIL") {
			result.Success = false
		} else if strings.Contains(result.Output, "OUTCOME:PASS") {
			result.Success = true
		}
	}

	return result
}

// Compile-time interface check
var _ CodergenBackend = (*AgentBackend)(nil)
