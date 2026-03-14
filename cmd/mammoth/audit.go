// ABOUTME: Pipeline run audit narrative generator using LLM analysis.
// ABOUTME: Builds structured context from run events and generates human-readable diagnostic reports.
package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/2389-research/mammoth/dot"
	"github.com/2389-research/mammoth/llm"
	"github.com/2389-research/mammoth/runstate"
)

// eventTypeNormalize maps old dotted event names to canonical underscore form
// so audit analysis works for runs stored by either attractor or tracker.
var eventTypeNormalize = map[string]string{
	"stage.started":         "stage_started",
	"stage.completed":       "stage_completed",
	"stage.failed":          "stage_failed",
	"stage.retrying":        "stage_retrying",
	"pipeline.started":      "pipeline_started",
	"pipeline.completed":    "pipeline_completed",
	"pipeline.failed":       "pipeline_failed",
	"agent.tool_call.start": "tool_call_start",
	"agent.tool_call.end":   "tool_call_end",
	"agent.llm_turn":        "turn_metrics",
}

// normalizeEventType returns the canonical event type name.
func normalizeEventType(t string) string {
	if n, ok := eventTypeNormalize[t]; ok {
		return n
	}
	return t
}

// auditReport holds the generated audit narrative.
type auditReport struct {
	Narrative string
}

// buildAuditContext transforms run data into a structured text blob for the LLM.
func buildAuditContext(state *runstate.RunState, events []runstate.RunEvent, graph *dot.Graph, verbose bool) string {
	var b strings.Builder

	// Run metadata
	b.WriteString("## Run Metadata\n")
	b.WriteString(fmt.Sprintf("Run ID: %s\n", state.ID))
	b.WriteString(fmt.Sprintf("Pipeline: %s\n", state.PipelineFile))
	b.WriteString(fmt.Sprintf("Status: %s\n", state.Status))

	duration := "unknown"
	if state.CompletedAt != nil {
		d := state.CompletedAt.Sub(state.StartedAt)
		duration = d.Round(100 * time.Millisecond).String()
	}
	b.WriteString(fmt.Sprintf("Duration: %s\n", duration))

	if state.Error != "" {
		b.WriteString(fmt.Sprintf("Error: %s\n", state.Error))
	}

	// Pipeline flow
	if graph != nil {
		b.WriteString("\n## Pipeline Flow\n")
		flow := linearizeGraph(graph)
		b.WriteString(flow + "\n")
	}

	// Event timeline
	b.WriteString("\n## Event Timeline\n")
	var baseTime time.Time
	for _, evt := range events {
		if baseTime.IsZero() {
			baseTime = evt.Timestamp
		}
		offset := evt.Timestamp.Sub(baseTime).Round(100 * time.Millisecond)
		line := fmt.Sprintf("+%s  [%s]", offset, evt.Type)
		if evt.NodeID != "" {
			line += fmt.Sprintf(" node=%s", evt.NodeID)
		}

		// Include event data based on verbosity.
		// Normalize event types to handle both old dotted and new underscore names.
		evtType := normalizeEventType(evt.Type)
		if evt.Data != nil {
			switch evtType {
			case "stage_failed", "pipeline_failed":
				if reason, ok := evt.Data["reason"]; ok {
					line += fmt.Sprintf(" reason=%v", reason)
				}
				if errMsg, ok := evt.Data["error"]; ok {
					line += fmt.Sprintf(" error=%v", errMsg)
				}
			case "tool_call_start":
				if verbose {
					if name, ok := evt.Data["tool_name"]; ok {
						line += fmt.Sprintf(" tool=%v", name)
					}
					if args, ok := evt.Data["arguments"]; ok {
						line += fmt.Sprintf(" args=%v", args)
					}
				}
			case "tool_call_end":
				if verbose {
					if name, ok := evt.Data["tool_name"]; ok {
						line += fmt.Sprintf(" tool=%v", name)
					}
					if dur, ok := evt.Data["duration_ms"]; ok {
						line += fmt.Sprintf(" duration=%vms", dur)
					}
				}
			case "turn_metrics":
				if verbose {
					if tokens, ok := evt.Data["total_tokens"]; ok {
						line += fmt.Sprintf(" tokens=%v", tokens)
					}
				}
			}
		}

		b.WriteString(line + "\n")
	}

	// Summarize tool usage when not verbose
	if !verbose {
		toolCounts := map[string]int{}
		llmTurns := 0
		for _, evt := range events {
			switch normalizeEventType(evt.Type) {
			case "tool_call_start":
				if name, ok := evt.Data["tool_name"].(string); ok {
					toolCounts[name]++
				}
			case "turn_metrics":
				llmTurns++
			}
		}
		if len(toolCounts) > 0 || llmTurns > 0 {
			b.WriteString("\n## Agent Activity Summary\n")
			b.WriteString(fmt.Sprintf("LLM turns: %d\n", llmTurns))
			for tool, count := range toolCounts {
				b.WriteString(fmt.Sprintf("Tool %s: %d call(s)\n", tool, count))
			}
		}
	}

	return b.String()
}

// linearizeGraph walks the graph from start via BFS and returns a
// human-readable flow string like "start -> build -> verify -> exit".
func linearizeGraph(g *dot.Graph) string {
	// Find start node by shape=Mdiamond, node_type=start, or type=start.
	var startID string
	for id, n := range g.Nodes {
		if n.Attrs["shape"] == "Mdiamond" ||
			n.Attrs["node_type"] == "start" ||
			n.Attrs["type"] == "start" {
			startID = id
			break
		}
	}
	if startID == "" {
		return "(no start node found)"
	}

	// Build adjacency for BFS
	adj := map[string][]string{}
	for _, e := range g.Edges {
		adj[e.From] = append(adj[e.From], e.To)
	}

	visited := map[string]bool{}
	var path []string
	queue := []string{startID}
	visited[startID] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		path = append(path, current)

		for _, next := range adj[current] {
			if !visited[next] {
				visited[next] = true
				queue = append(queue, next)
			}
		}
	}

	return strings.Join(path, " -> ")
}

// auditSystemPrompt instructs the LLM to produce a structured audit narrative.
const auditSystemPrompt = `You are a pipeline execution analyst for "mammoth", a DOT-based AI pipeline runner.

Given the run metadata, pipeline graph, and event timeline, produce a concise audit report.

Report format (use plain text, not markdown):

SUMMARY
One paragraph: what pipeline ran, what happened, how it ended.

TIMELINE
Chronological list of key events with relative timestamps (+0.0s format).
Group repeated failures. Show each node's outcome (passed/failed/skipped).

DIAGNOSIS
Root cause analysis. Identify patterns:
- Rate limits (429 errors) — transient, suggest retry policy
- Retry loops — identify which node is looping and why
- Agent errors — tool failures, LLM errors
- Validation errors — graph structure issues
- Context cancellation — user interrupted

SUGGESTIONS
2-4 actionable next steps. Reference specific mammoth flags when applicable
(e.g. -retry patient, -fix, max_node_visits, goal_gate).

Keep the report concise. Use plain language. No markdown headers — use ALL CAPS section names.`

// generateAudit sends run data to an LLM and returns a narrative audit report.
func generateAudit(ctx context.Context, state *runstate.RunState, events []runstate.RunEvent, graph *dot.Graph, verbose bool, client *llm.Client) (*auditReport, error) {
	if client == nil {
		return nil, fmt.Errorf("audit requires an LLM client — set ANTHROPIC_API_KEY, OPENAI_API_KEY, or GEMINI_API_KEY")
	}

	auditCtx := buildAuditContext(state, events, graph, verbose)

	result, err := llm.Generate(ctx, llm.GenerateOptions{
		System: auditSystemPrompt,
		Prompt: auditCtx,
		Client: client,
	})
	if err != nil {
		return nil, fmt.Errorf("LLM audit generation failed: %w", err)
	}

	return &auditReport{
		Narrative: result.Text,
	}, nil
}
