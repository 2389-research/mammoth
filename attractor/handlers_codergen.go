// ABOUTME: Codergen (LLM coding agent) handler for the attractor pipeline runner.
// ABOUTME: Stub implementation that records prompt/config; actual LLM calls are wired in by the engine.
package attractor

import (
	"context"
)

// CodergenHandler handles LLM-powered coding task nodes (shape=box).
// This is the default handler for nodes without an explicit type.
// The actual LLM call will be wired in by the engine via a backend;
// this implementation records the prompt and configuration in the outcome.
type CodergenHandler struct{}

// Type returns the handler type string "codergen".
func (h *CodergenHandler) Type() string {
	return "codergen"
}

// Execute processes a codergen node by reading its prompt, label, model, and provider,
// then returning a success outcome with the configuration recorded.
// The actual LLM invocation is deferred to the engine's backend integration.
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

	// Build context updates with the configuration
	updates := map[string]any{
		"last_stage": node.ID,
	}

	// Record LLM config if present
	if llmModel != "" {
		updates["codergen.model"] = llmModel
	}
	if llmProvider != "" {
		updates["codergen.provider"] = llmProvider
	}

	// Record the prompt for inspection
	updates["codergen.prompt"] = prompt

	return &Outcome{
		Status:         StatusSuccess,
		Notes:          "Stage completed (stub): " + label,
		ContextUpdates: updates,
	}, nil
}
