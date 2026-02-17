// ABOUTME: AgentBackend wires CodergenBackend to the real agent loop and LLM SDK.
// ABOUTME: Creates agent sessions, configures provider profiles, and runs ProcessInput for each codergen node.
package attractor

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/2389-research/mammoth/agent"
	"github.com/2389-research/mammoth/llm"
	muxllm "github.com/2389-research/mux/llm"
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
		envClient, err := createClientFromEnv(ctx, config.Provider, config.BaseURL)
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
		workDir, err = os.MkdirTemp("", "mammoth-codergen-*")
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
	sessionConfig.UserOverride = config.SystemPrompt

	session := agent.NewSession(sessionConfig)
	defer session.Close()

	// Subscribe to agent events and bridge to engine events if a handler is provided
	var toolLog []ToolCallEntry
	var toolLogMu sync.Mutex
	var turnCount int32
	if config.EventHandler != nil {
		eventCh := session.EventEmitter.Subscribe()
		toolStarts := &sync.Map{}
		done := make(chan struct{})
		go func() {
			defer close(done)
			for evt := range eventCh {
				bridgeSessionEvent(evt, config.NodeID, config.EventHandler, toolStarts, &toolLog, &toolLogMu)
				if evt.Kind == agent.EventAssistantTextEnd {
					atomicAddInt32(&turnCount, 1)
				}
			}
		}()
		defer func() {
			session.EventEmitter.Unsubscribe(eventCh)
			<-done // wait for goroutine to drain
		}()
	}

	// Build the user input from the prompt and goal
	userInput := buildAgentInput(config.Prompt, config.Goal, config.NodeID)

	// Run the agent loop
	err := agent.ProcessInput(ctx, session, profile, env, client, userInput)
	if err != nil {
		return nil, fmt.Errorf("agent processing failed: %w", err)
	}

	// Extract results from the session
	result := extractResult(session)
	toolLogMu.Lock()
	result.ToolCallLog = toolLog
	toolLogMu.Unlock()
	result.TurnCount = int(atomic.LoadInt32(&turnCount))
	return result, nil
}

// createClientFromEnv creates an LLM client configured from environment variables.
// It checks for API keys and returns a clear error if none are found. When
// baseURL is non-empty, it is applied to the preferred provider's adapter.
// Provider-specific base URL env vars (ANTHROPIC_BASE_URL, OPENAI_BASE_URL,
// GEMINI_BASE_URL) are also checked as fallbacks. The ctx parameter is needed
// for providers like Gemini whose mux client constructor requires a context.
func createClientFromEnv(ctx context.Context, preferredProvider, baseURL string) (*llm.Client, error) {
	// Check for API keys based on preferred provider
	providers := []struct {
		envVar     string
		name       string
		baseEnvVar string
		priority   int
	}{
		{"ANTHROPIC_API_KEY", "anthropic", "ANTHROPIC_BASE_URL", 0},
		{"OPENAI_API_KEY", "openai", "OPENAI_BASE_URL", 0},
		{"GEMINI_API_KEY", "gemini", "GEMINI_BASE_URL", 0},
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
			// Resolve base URL: explicit config > provider-specific env var > empty (default)
			providerBaseURL := ""
			if p.name == preferredProvider && baseURL != "" {
				providerBaseURL = baseURL
			}
			if providerBaseURL == "" {
				providerBaseURL = os.Getenv(p.baseEnvVar)
			}
			adapter := createProviderAdapter(ctx, p.name, key, providerBaseURL)
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

// createProviderAdapter creates a provider adapter for the given provider.
// By default it creates mux/llm clients wrapped with llm.NewMuxAdapter for
// a unified client implementation. When baseURL is non-empty, it falls back
// to mammoth's built-in adapters which support custom API endpoints.
// Unknown providers default to Anthropic.
func createProviderAdapter(ctx context.Context, name, apiKey, baseURL string) llm.ProviderAdapter {
	// When a custom base URL is specified, fall back to mammoth's built-in
	// adapters which support base URL overrides. The mux clients do not
	// expose base URL configuration.
	if baseURL != "" {
		return createLegacyProviderAdapter(name, apiKey, baseURL)
	}

	switch name {
	case "anthropic":
		// Empty model string uses the mux client's built-in default;
		// the actual model is set per-request by the agent profile.
		client := muxllm.NewAnthropicClient(apiKey, "")
		return llm.NewMuxAdapter(name, client)
	case "openai":
		client := muxllm.NewOpenAIClient(apiKey, "")
		return llm.NewMuxAdapter(name, client)
	case "gemini":
		client, err := muxllm.NewGeminiClient(ctx, apiKey, "")
		if err != nil {
			log.Printf("failed to create Gemini mux client, falling back to built-in adapter: %v", err)
			//nolint:staticcheck // SA1019: Using deprecated adapter as fallback for mux client creation failure
			return llm.NewGeminiAdapter(apiKey)
		}
		return llm.NewMuxAdapter(name, client)
	default:
		client := muxllm.NewAnthropicClient(apiKey, "")
		return llm.NewMuxAdapter("anthropic", client)
	}
}

// createLegacyProviderAdapter creates a mammoth built-in provider adapter.
// Used when a custom base URL is specified, since the mux clients do not
// support base URL overrides.
//
//nolint:staticcheck // SA1019: Using deprecated adapters intentionally for custom base URL fallback
func createLegacyProviderAdapter(name, apiKey, baseURL string) llm.ProviderAdapter {
	switch name {
	case "anthropic":
		return llm.NewAnthropicAdapter(apiKey, llm.WithAnthropicBaseURL(baseURL))
	case "openai":
		return llm.NewOpenAIAdapter(apiKey, llm.WithOpenAIBaseURL(baseURL))
	case "gemini":
		return llm.NewGeminiAdapter(apiKey, llm.WithGeminiBaseURL(baseURL))
	default:
		return llm.NewAnthropicAdapter(apiKey, llm.WithAnthropicBaseURL(baseURL))
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
			result.Usage = result.Usage.Add(tokenUsageFromLLM(t.Usage))
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

// bridgeSessionEvent translates a single agent SessionEvent into an EngineEvent
// and calls the handler. It also tracks tool call start times and builds the
// tool call log for enriched results.
func bridgeSessionEvent(
	evt agent.SessionEvent,
	nodeID string,
	handler func(EngineEvent),
	toolStarts *sync.Map,
	toolLog *[]ToolCallEntry,
	toolLogMu *sync.Mutex,
) {
	switch evt.Kind {
	case agent.EventToolCallStart:
		toolName, _ := evt.Data["tool_name"].(string)
		callID, _ := evt.Data["call_id"].(string)

		if toolStarts != nil && callID != "" {
			toolStarts.Store(callID, time.Now())
			toolStarts.Store(callID+"_name", toolName)
		}

		handler(EngineEvent{
			Type:      EventAgentToolCallStart,
			NodeID:    nodeID,
			Timestamp: evt.Timestamp,
			Data: map[string]any{
				"tool_name": toolName,
				"call_id":   callID,
			},
		})

	case agent.EventToolCallEnd:
		callID, _ := evt.Data["call_id"].(string)
		outputRaw, _ := evt.Data["output"].(string)
		errorMsg, _ := evt.Data["error"].(string)

		// Determine output snippet (prefer output, fall back to error)
		outputSnippet := outputRaw
		if outputSnippet == "" {
			outputSnippet = errorMsg
		}
		outputSnippet = truncateString(outputSnippet, 500)

		// Look up start time and tool name
		var duration time.Duration
		var toolName string
		if toolStarts != nil && callID != "" {
			if startVal, ok := toolStarts.Load(callID); ok {
				if startTime, ok := startVal.(time.Time); ok {
					duration = time.Since(startTime)
				}
				toolStarts.Delete(callID)
			}
			if nameVal, ok := toolStarts.Load(callID + "_name"); ok {
				toolName, _ = nameVal.(string)
				toolStarts.Delete(callID + "_name")
			}
		}

		handler(EngineEvent{
			Type:      EventAgentToolCallEnd,
			NodeID:    nodeID,
			Timestamp: evt.Timestamp,
			Data: map[string]any{
				"call_id":        callID,
				"tool_name":      toolName,
				"output_snippet": outputSnippet,
				"duration_ms":    duration.Milliseconds(),
			},
		})

		// Append to tool call log
		if toolLog != nil && toolLogMu != nil {
			entry := ToolCallEntry{
				ToolName: toolName,
				CallID:   callID,
				Duration: duration,
				Output:   truncateString(outputRaw, 500),
			}
			toolLogMu.Lock()
			*toolLog = append(*toolLog, entry)
			toolLogMu.Unlock()
		}

	case agent.EventAssistantTextEnd:
		text, _ := evt.Data["text"].(string)
		reasoning, _ := evt.Data["reasoning"].(string)

		data := map[string]any{
			"text_length":   len(text),
			"has_reasoning": reasoning != "",
		}
		// Forward granular token data when present
		for _, key := range []string{
			"input_tokens", "output_tokens", "total_tokens",
			"reasoning_tokens", "cache_read_tokens", "cache_write_tokens",
		} {
			if v, ok := evt.Data[key]; ok {
				data[key] = v
			}
		}

		handler(EngineEvent{
			Type:      EventAgentLLMTurn,
			NodeID:    nodeID,
			Timestamp: evt.Timestamp,
			Data:      data,
		})

	case agent.EventSteeringInjected:
		content, _ := evt.Data["content"].(string)
		handler(EngineEvent{
			Type:      EventAgentSteering,
			NodeID:    nodeID,
			Timestamp: evt.Timestamp,
			Data: map[string]any{
				"message": content,
			},
		})

	case agent.EventLoopDetection:
		message, _ := evt.Data["message"].(string)
		handler(EngineEvent{
			Type:      EventAgentLoopDetected,
			NodeID:    nodeID,
			Timestamp: evt.Timestamp,
			Data: map[string]any{
				"message": message,
			},
		})
	}
}

// tokenUsageFromLLM converts an llm.Usage value into our TokenUsage struct,
// dereferencing optional pointer fields (reasoning, cache) safely.
func tokenUsageFromLLM(u llm.Usage) TokenUsage {
	tu := TokenUsage{
		InputTokens:  u.InputTokens,
		OutputTokens: u.OutputTokens,
		TotalTokens:  u.TotalTokens,
	}
	if u.ReasoningTokens != nil {
		tu.ReasoningTokens = *u.ReasoningTokens
	}
	if u.CacheReadTokens != nil {
		tu.CacheReadTokens = *u.CacheReadTokens
	}
	if u.CacheWriteTokens != nil {
		tu.CacheWriteTokens = *u.CacheWriteTokens
	}
	return tu
}

// truncateString truncates a string to maxLen runes, preserving valid UTF-8.
func truncateString(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen])
}

// atomicAddInt32 is a thin wrapper around atomic.AddInt32 to keep it testable
// from the bridge_test.go (which runs in the same package).
func atomicAddInt32(addr *int32, delta int32) {
	atomic.AddInt32(addr, delta)
}

// Compile-time interface check
var _ CodergenBackend = (*AgentBackend)(nil)
