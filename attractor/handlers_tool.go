// ABOUTME: External tool handler for the attractor pipeline runner.
// ABOUTME: Stub implementation that records tool command/name; actual execution is wired in by the engine.
package attractor

import (
	"context"
)

// ToolHandler handles external tool execution nodes (shape=parallelogram).
// It reads the command or tool_name from node attributes. This stub
// implementation records what would be executed; actual tool invocation
// is wired in by the engine.
type ToolHandler struct{}

// Type returns the handler type string "tool".
func (h *ToolHandler) Type() string {
	return "tool"
}

// Execute reads tool configuration from node attributes and records it.
// If neither tool_command nor tool_name is specified, it returns failure.
func (h *ToolHandler) Execute(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	attrs := node.Attrs
	if attrs == nil {
		attrs = make(map[string]string)
	}

	command := attrs["tool_command"]
	toolName := attrs["tool_name"]

	// Need at least one of command or tool_name
	if command == "" && toolName == "" {
		return &Outcome{
			Status:        StatusFail,
			FailureReason: "No tool_command or tool_name specified for tool node: " + node.ID,
		}, nil
	}

	updates := map[string]any{
		"last_stage": node.ID,
	}

	if command != "" {
		updates["tool.command"] = command
	}
	if toolName != "" {
		updates["tool.name"] = toolName
	}

	notes := "Tool recorded (stub): "
	if command != "" {
		notes += command
	} else {
		notes += toolName
	}

	return &Outcome{
		Status:         StatusSuccess,
		Notes:          notes,
		ContextUpdates: updates,
	}, nil
}
