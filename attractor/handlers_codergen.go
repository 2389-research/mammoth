// ABOUTME: Codergen (LLM coding agent) handler for the attractor pipeline runner.
// ABOUTME: Delegates to a CodergenBackend for actual LLM execution; falls back to stub when backend is nil.
package attractor

import (
	"context"
	"fmt"
	"strconv"
)

// CodergenHandler handles LLM-powered coding task nodes (shape=box).
// This is the default handler for nodes without an explicit type.
// When Backend is set, it delegates to the agent loop for real LLM execution.
// When Backend is nil, it falls back to stub behavior for testing.
type CodergenHandler struct {
	// Backend is the agent execution backend. When nil, the handler uses
	// stub behavior that records prompt/config without calling an LLM.
	Backend CodergenBackend
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

	// If no backend is configured, fall back to stub behavior
	if h.Backend == nil {
		return h.executeStub(node.ID, prompt, label, llmModel, llmProvider)
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

	config := AgentRunConfig{
		Prompt:       prompt,
		Model:        llmModel,
		Provider:     llmProvider,
		WorkDir:      workDir,
		Goal:         goal,
		NodeID:       node.ID,
		MaxTurns:     maxTurns,
		FidelityMode: fidelityMode,
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
		Notes:          fmt.Sprintf("Stage completed: %s (tools: %d, tokens: %d)", label, result.ToolCalls, result.TokensUsed),
		ContextUpdates: updates,
	}, nil
}

// executeStub is the fallback behavior when no backend is configured.
// It records the prompt and configuration in the outcome without calling an LLM.
func (h *CodergenHandler) executeStub(nodeID, prompt, label, llmModel, llmProvider string) (*Outcome, error) {
	updates := map[string]any{
		"last_stage": nodeID,
	}

	if llmModel != "" {
		updates["codergen.model"] = llmModel
	}
	if llmProvider != "" {
		updates["codergen.provider"] = llmProvider
	}

	updates["codergen.prompt"] = prompt

	return &Outcome{
		Status:         StatusSuccess,
		Notes:          "Stage completed (stub): " + label,
		ContextUpdates: updates,
	}, nil
}
