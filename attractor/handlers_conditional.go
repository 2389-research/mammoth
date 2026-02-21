// ABOUTME: Hybrid conditional branching handler for the attractor pipeline runner.
// ABOUTME: Runs an LLM agent when a prompt attribute is present; otherwise passes through the prior node's outcome.
package attractor

import (
	"context"
	"fmt"
	"strconv"
)

// ConditionalHandler handles conditional routing nodes (shape=diamond).
// When the node has a "prompt" attribute, it runs an LLM agent via the Backend
// and uses DetectOutcomeMarker to determine pass/fail for edge routing.
// When no prompt is present, it passes through the outcome status from the
// preceding node so that edge conditions like "outcome=FAIL" evaluate correctly.
type ConditionalHandler struct {
	// Backend is the agent execution backend. When nil and a prompt is present,
	// the handler returns StatusFail indicating no LLM backend is configured.
	Backend CodergenBackend

	// BaseURL is the default API base URL for the LLM provider. Set by the
	// engine during backend wiring. Can be overridden per-node via base_url attr.
	BaseURL string

	// EventHandler receives agent-level observability events bridged from the
	// agent session into the engine event system. Set by the engine during
	// backend wiring.
	EventHandler func(EngineEvent)
}

// Type returns the handler type string "conditional".
func (h *ConditionalHandler) Type() string {
	return "conditional"
}

// Execute processes a conditional node. If the node has a "prompt" attribute,
// it runs an LLM agent via the Backend and detects the outcome from the agent
// output. If no prompt is present, it reads the upstream outcome from the
// pipeline context and passes it through for edge selection.
func (h *ConditionalHandler) Execute(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	attrs := node.Attrs
	if attrs == nil {
		attrs = make(map[string]string)
	}

	prompt := attrs["prompt"]

	// No prompt: pass-through mode (backward-compatible behavior).
	// Read the outcome status set by the preceding node so that edge
	// conditions evaluate against the real upstream result.
	if prompt == "" {
		return h.executePassThrough(node, pctx)
	}

	// Prompt present: run the LLM agent to evaluate the condition.
	return h.executeWithAgent(ctx, node, attrs, prompt, pctx, store)
}

// executePassThrough implements the original pass-through behavior for diamond
// nodes without a prompt attribute. It reads the upstream "outcome" from the
// pipeline context and returns it as this node's status. The "outcome" key is
// intentionally NOT set in ContextUpdates here — the upstream value already
// lives in the context and edge selection evaluates against the Outcome.Status
// returned by this handler, not the context key.
func (h *ConditionalHandler) executePassThrough(node *Node, pctx *Context) (*Outcome, error) {
	status := StatusSuccess
	if prev, ok := pctx.Get("outcome").(string); ok && prev != "" {
		status = StageStatus(prev)
	}

	return &Outcome{
		Status: status,
		Notes:  "Conditional node evaluated: " + node.ID,
		ContextUpdates: map[string]any{
			"last_stage": node.ID,
		},
	}, nil
}

// executeWithAgent runs an LLM agent for a diamond node that has a prompt,
// detects the outcome from agent output, and returns the appropriate status.
func (h *ConditionalHandler) executeWithAgent(ctx context.Context, node *Node, attrs map[string]string, prompt string, pctx *Context, store *ArtifactStore) (*Outcome, error) {
	// No backend means no LLM — return fail with guidance.
	if h.Backend == nil {
		return &Outcome{
			Status:        StatusFail,
			FailureReason: "no LLM backend configured: set ANTHROPIC_API_KEY, OPENAI_API_KEY, or GEMINI_API_KEY",
			ContextUpdates: map[string]any{
				"outcome": "fail",
			},
		}, nil
	}

	// Build agent run configuration (mirrors CodergenHandler config building)
	maxTurns := 20
	if maxTurnsStr := attrs["max_turns"]; maxTurnsStr != "" {
		if parsed, err := strconv.Atoi(maxTurnsStr); err == nil && parsed > 0 {
			maxTurns = parsed
		}
	}

	goal := ""
	if goalVal := pctx.Get("goal"); goalVal != nil {
		if goalStr, ok := goalVal.(string); ok {
			goal = goalStr
		}
	}

	fidelityMode := ""
	if f := attrs["fidelity"]; f != "" && IsValidFidelity(f) {
		fidelityMode = f
	} else if fVal := pctx.Get("_fidelity_mode"); fVal != nil {
		if fStr, ok := fVal.(string); ok && IsValidFidelity(fStr) {
			fidelityMode = fStr
		}
	}

	workDir := attrs["workdir"]
	if workDir == "" && store != nil && store.BaseDir() != "" {
		workDir = store.BaseDir()
	}

	baseURL := attrs["base_url"]
	if baseURL == "" {
		if val := pctx.Get("base_url"); val != nil {
			if s, ok := val.(string); ok {
				baseURL = s
			}
		}
	}
	if baseURL == "" {
		baseURL = h.BaseURL
	}

	systemPrompt := attrs["system_prompt"]
	if systemPrompt == "" {
		if spVal := pctx.Get("system_prompt"); spVal != nil {
			if spStr, ok := spVal.(string); ok {
				systemPrompt = spStr
			}
		}
	}

	config := AgentRunConfig{
		Prompt:       prompt,
		Model:        attrs["llm_model"],
		Provider:     attrs["llm_provider"],
		BaseURL:      baseURL,
		WorkDir:      workDir,
		Goal:         goal,
		NodeID:       node.ID,
		MaxTurns:     maxTurns,
		FidelityMode: fidelityMode,
		SystemPrompt: systemPrompt,
		EventHandler: h.EventHandler,
	}

	result, err := h.Backend.RunAgent(ctx, config)
	if err != nil {
		return &Outcome{
			Status:        StatusFail,
			FailureReason: fmt.Sprintf("agent backend error: %v", err),
			ContextUpdates: map[string]any{
				"outcome":    "fail",
				"last_stage": node.ID,
			},
		}, nil
	}

	// Store agent output as an artifact
	if result.Output != "" && store != nil {
		artifactID := node.ID + ".output"
		if _, storeErr := store.Store(artifactID, "agent_output", []byte(result.Output)); storeErr != nil {
			pctx.AppendLog(fmt.Sprintf("warning: failed to store conditional agent output artifact: %v", storeErr))
		}
	}

	// Determine outcome: marker detection takes priority, then Success field
	status := h.resolveOutcome(result)

	// Post-execution verification: if verify_command is set, run it and
	// override the outcome on failure regardless of what the agent claimed.
	if verifyCmd := attrs["verify_command"]; verifyCmd != "" && status == StatusSuccess {
		workDir := attrs["workdir"]
		if workDir == "" && store != nil && store.BaseDir() != "" {
			workDir = store.BaseDir()
		}
		vResult := runVerifyCommand(ctx, verifyCmd, workDir, defaultVerifyTimeout)

		// Store verify output as artifact
		if store != nil {
			artifactID := node.ID + ".verify_output"
			verifyOutput := fmt.Sprintf("exit_code=%d\nstdout:\n%s\nstderr:\n%s", vResult.ExitCode, vResult.Stdout, vResult.Stderr)
			_, _ = store.Store(artifactID, "verify_output", []byte(verifyOutput))
		}

		if !vResult.Success {
			return &Outcome{
				Status:        StatusFail,
				FailureReason: fmt.Sprintf("verify_command failed (exit %d): %s", vResult.ExitCode, vResult.Stderr),
				ContextUpdates: map[string]any{
					"outcome":    "fail",
					"last_stage": node.ID,
				},
			}, nil
		}
	}

	return &Outcome{
		Status: status,
		Notes:  fmt.Sprintf("Conditional agent evaluated: %s", node.ID),
		ContextUpdates: map[string]any{
			"outcome":    string(status),
			"last_stage": node.ID,
		},
	}, nil
}

// resolveOutcome determines the StageStatus from an agent result.
// Precedence: DetectOutcomeMarker in output > result.Success field.
func (h *ConditionalHandler) resolveOutcome(result *AgentRunResult) StageStatus {
	// Check for explicit outcome markers in agent output
	if marker, found := DetectOutcomeMarker(result.Output); found {
		if marker == "fail" {
			return StatusFail
		}
		return StatusSuccess
	}

	// Fall back to the Success field
	if !result.Success {
		return StatusFail
	}
	return StatusSuccess
}
