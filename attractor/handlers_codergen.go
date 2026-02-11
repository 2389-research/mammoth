// ABOUTME: Codergen (LLM coding agent) handler for the attractor pipeline runner.
// ABOUTME: Delegates to a CodergenBackend for actual LLM execution; returns error when backend is nil.
package attractor

import (
	"context"
	"fmt"
	"strconv"
)

// CodergenHandler handles LLM-powered coding task nodes (shape=box).
// This is the default handler for nodes without an explicit type.
// When Backend is set, it delegates to the agent loop for real LLM execution.
// When Backend is nil, it returns StatusFail with a configuration error.
type CodergenHandler struct {
	// Backend is the agent execution backend. When nil, the handler
	// returns StatusFail indicating no LLM backend is configured.
	Backend CodergenBackend

	// BaseURL is the default API base URL for the LLM provider. Set by the
	// engine during backend wiring. Can be overridden per-node via base_url attr.
	BaseURL string

	// EventHandler receives agent-level observability events bridged from the
	// agent session into the engine event system. Set by the engine during
	// backend wiring.
	EventHandler func(EngineEvent)
}

// Type returns the handler type string "codergen".
func (h *CodergenHandler) Type() string {
	return "codergen"
}

// Execute processes a codergen node by reading its prompt, label, model, and provider.
// If a Backend is configured, it runs the agent loop for real LLM execution.
// Otherwise, it falls back to stub behavior.
func (h *CodergenHandler) Execute(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	attrs := node.Attrs
	if attrs == nil {
		attrs = make(map[string]string)
	}

	// Read prompt, falling back to label if prompt is empty
	prompt := attrs["prompt"]
	if prompt == "" {
		prompt = attrs["label"]
	}
	if prompt == "" {
		prompt = node.ID
	}

	label := attrs["label"]
	if label == "" {
		label = node.ID
	}

	llmModel := attrs["llm_model"]
	llmProvider := attrs["llm_provider"]

	// No backend means no LLM — this is a hard error, not a stub.
	if h.Backend == nil {
		return &Outcome{
			Status:        StatusFail,
			FailureReason: "no LLM backend configured: set ANTHROPIC_API_KEY, OPENAI_API_KEY, or GEMINI_API_KEY",
		}, nil
	}

	// Build agent run configuration
	maxTurns := 20
	if maxTurnsStr := attrs["max_turns"]; maxTurnsStr != "" {
		if parsed, err := strconv.Atoi(maxTurnsStr); err == nil && parsed > 0 {
			maxTurns = parsed
		}
	}

	// Extract the pipeline goal from the context
	goal := ""
	if goalVal := pctx.Get("goal"); goalVal != nil {
		if goalStr, ok := goalVal.(string); ok {
			goal = goalStr
		}
	}

	// Resolve fidelity mode: node attribute takes precedence over pipeline context
	fidelityMode := ""
	if f := attrs["fidelity"]; f != "" && IsValidFidelity(f) {
		fidelityMode = f
	} else if fVal := pctx.Get("_fidelity_mode"); fVal != nil {
		if fStr, ok := fVal.(string); ok && IsValidFidelity(fStr) {
			fidelityMode = fStr
		}
	}

	// Resolve working directory: explicit attr > artifact store base > temp dir (in backend)
	workDir := attrs["workdir"]
	if workDir == "" && store != nil && store.BaseDir() != "" {
		workDir = store.BaseDir()
	}

	baseURL := attrs["base_url"]
	if baseURL == "" {
		baseURL = h.BaseURL
	}

	config := AgentRunConfig{
		Prompt:       prompt,
		Model:        llmModel,
		Provider:     llmProvider,
		BaseURL:      baseURL,
		WorkDir:      workDir,
		Goal:         goal,
		NodeID:       node.ID,
		MaxTurns:     maxTurns,
		FidelityMode: fidelityMode,
		EventHandler: h.EventHandler,
	}

	// Run the agent
	result, err := h.Backend.RunAgent(ctx, config)
	if err != nil {
		return &Outcome{
			Status:        StatusFail,
			FailureReason: fmt.Sprintf("agent backend error: %v", err),
			ContextUpdates: map[string]any{
				"last_stage":      node.ID,
				"codergen.prompt": prompt,
			},
		}, nil
	}

	// Build context updates
	updates := map[string]any{
		"last_stage":      node.ID,
		"codergen.prompt": prompt,
	}
	if llmModel != "" {
		updates["codergen.model"] = llmModel
	}
	if llmProvider != "" {
		updates["codergen.provider"] = llmProvider
	}
	updates["codergen.tool_calls"] = result.ToolCalls
	updates["codergen.tokens_used"] = result.TokensUsed
	updates["codergen.turn_count"] = result.TurnCount
	updates["codergen.input_tokens"] = result.Usage.InputTokens
	updates["codergen.output_tokens"] = result.Usage.OutputTokens
	updates["codergen.reasoning_tokens"] = result.Usage.ReasoningTokens
	updates["codergen.cache_read_tokens"] = result.Usage.CacheReadTokens
	updates["codergen.cache_write_tokens"] = result.Usage.CacheWriteTokens

	// Store agent output as an artifact
	if result.Output != "" {
		artifactID := node.ID + ".output"
		if _, storeErr := store.Store(artifactID, "agent_output", []byte(result.Output)); storeErr != nil {
			// Log but do not fail the node
			pctx.AppendLog(fmt.Sprintf("warning: failed to store agent output artifact: %v", storeErr))
		}
	}

	if !result.Success {
		return &Outcome{
			Status:         StatusFail,
			FailureReason:  fmt.Sprintf("agent did not complete successfully: %s", result.Output),
			ContextUpdates: updates,
		}, nil
	}

	return &Outcome{
		Status:         StatusSuccess,
		Notes:          fmt.Sprintf("Stage completed: %s (tools: %d, tokens: %d [in:%d out:%d])", label, result.ToolCalls, result.TokensUsed, result.Usage.InputTokens, result.Usage.OutputTokens),
		ContextUpdates: updates,
	}, nil
}
