// ABOUTME: Implements the write_commands tool for submitting spec-mutating commands via mux Tool interface.
// ABOUTME: Parses JSON command arrays and sends each to the actor, reporting successes and failures.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/2389-research/mammoth/spec/core"
	"github.com/2389-research/mux/tool"
)

// WriteCommandsTool accepts an array of Command objects and sends each to the spec actor.
type WriteCommandsTool struct {
	Actor   *core.SpecActorHandle
	AgentID string
}

func (t *WriteCommandsTool) Name() string {
	return "write_commands"
}

func (t *WriteCommandsTool) Description() string {
	return "Submit one or more commands to modify the spec. Commands can create/update/move/delete cards, update spec metadata, or append to the transcript."
}

func (t *WriteCommandsTool) RequiresApproval(_ map[string]any) bool {
	return false
}

func (t *WriteCommandsTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"commands": map[string]any{
				"type":        "array",
				"description": "List of commands to execute against the spec. Each command is an object with a 'type' field.",
				"items": map[string]any{
					"type":        "object",
					"description": "A tagged command object. The 'type' field selects the variant.",
					"properties": map[string]any{
						"type": map[string]any{
							"type": "string",
							"enum": []any{"CreateCard", "UpdateCard", "MoveCard", "DeleteCard", "UpdateSpecCore", "AppendTranscript"},
						},
					},
					"required": []any{"type"},
				},
			},
		},
		"required": []any{"commands"},
	}
}

func (t *WriteCommandsTool) Execute(_ context.Context, params map[string]any) (*tool.Result, error) {
	commandsRaw, ok := params["commands"]
	if !ok {
		return nil, fmt.Errorf("missing 'commands' parameter")
	}

	// Marshal the raw params back to JSON for proper deserialization through UnmarshalCommand
	commandsJSON, err := json.Marshal(commandsRaw)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal commands: %w", err)
	}

	var rawCommands []json.RawMessage
	if err := json.Unmarshal(commandsJSON, &rawCommands); err != nil {
		return nil, fmt.Errorf("failed to parse commands array: %w", err)
	}

	if len(rawCommands) == 0 {
		return tool.NewResult("write_commands", true, "No commands to execute.", ""), nil
	}

	total := len(rawCommands)
	successes := 0
	var failures []string

	for i, raw := range rawCommands {
		cmd, err := core.UnmarshalCommand(raw)
		if err != nil {
			log.Printf("agent %s: command %d parse error: %v", t.AgentID, i, err)
			failures = append(failures, fmt.Sprintf("command %d: parse error: %v", i, err))
			continue
		}

		cmd = enforceCommandIdentity(cmd, t.AgentID)

		events, err := t.Actor.SendCommand(cmd)
		if err != nil {
			log.Printf("agent %s: command %d execution failed: %v", t.AgentID, i, err)
			failures = append(failures, fmt.Sprintf("command %d: %v", i, err))
		} else {
			successes++
			log.Printf("agent %s: command %d executed, %d events produced", t.AgentID, i, len(events))
		}
	}

	var summary string
	if len(failures) == 0 {
		summary = fmt.Sprintf("All %d commands executed successfully.", total)
	} else {
		summary = fmt.Sprintf("%d/%d commands succeeded. Failures:\n%s",
			successes, total, joinStrings(failures, "\n"))
	}

	return tool.NewResult("write_commands", true, summary, ""), nil
}

// enforceCommandIdentity overrides identity fields (CreatedBy, UpdatedBy, Sender)
// on commands to the given agent ID. This prevents the LLM from spoofing
// identity or omitting audit fields.
func enforceCommandIdentity(cmd core.Command, agentID string) core.Command {
	switch c := cmd.(type) {
	case core.CreateCardCommand:
		c.CreatedBy = agentID
		return c
	case core.UpdateCardCommand:
		c.UpdatedBy = agentID
		return c
	case core.MoveCardCommand:
		c.UpdatedBy = agentID
		return c
	case core.DeleteCardCommand:
		c.UpdatedBy = agentID
		return c
	case core.AppendTranscriptCommand:
		c.Sender = agentID
		return c
	default:
		return cmd
	}
}

// joinStrings joins strings with a separator.
func joinStrings(parts []string, sep string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += sep
		}
		result += p
	}
	return result
}
