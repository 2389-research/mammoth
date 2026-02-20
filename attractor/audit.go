// ABOUTME: Pipeline run audit narrative generator using LLM analysis.
// ABOUTME: Builds structured context from run events and generates human-readable diagnostic reports.
package attractor

import (
	"fmt"
	"strings"
	"time"
)

// AuditRequest holds all the data needed to generate an audit narrative.
type AuditRequest struct {
	State   *RunState
	Events  []EngineEvent
	Graph   *Graph
	Verbose bool
}

// AuditReport holds the generated audit narrative.
type AuditReport struct {
	Narrative string
}

// buildAuditContext transforms run data into a structured text blob for the LLM.
func buildAuditContext(req AuditRequest) string {
	var b strings.Builder

	// Run metadata
	b.WriteString("## Run Metadata\n")
	b.WriteString(fmt.Sprintf("Run ID: %s\n", req.State.ID))
	b.WriteString(fmt.Sprintf("Pipeline: %s\n", req.State.PipelineFile))
	b.WriteString(fmt.Sprintf("Status: %s\n", req.State.Status))

	duration := "unknown"
	if req.State.CompletedAt != nil {
		d := req.State.CompletedAt.Sub(req.State.StartedAt)
		duration = d.Round(100 * time.Millisecond).String()
	}
	b.WriteString(fmt.Sprintf("Duration: %s\n", duration))

	if req.State.Error != "" {
		b.WriteString(fmt.Sprintf("Error: %s\n", req.State.Error))
	}

	// Pipeline flow
	if req.Graph != nil {
		b.WriteString("\n## Pipeline Flow\n")
		flow := linearizeGraph(req.Graph)
		b.WriteString(flow + "\n")
	}

	// Event timeline
	b.WriteString("\n## Event Timeline\n")
	var baseTime time.Time
	for _, evt := range req.Events {
		if baseTime.IsZero() {
			baseTime = evt.Timestamp
		}
		offset := evt.Timestamp.Sub(baseTime).Round(100 * time.Millisecond)
		line := fmt.Sprintf("+%s  [%s]", offset, evt.Type)
		if evt.NodeID != "" {
			line += fmt.Sprintf(" node=%s", evt.NodeID)
		}

		// Include event data based on verbosity
		if evt.Data != nil {
			switch evt.Type {
			case EventStageFailed, EventPipelineFailed:
				// Always include failure reasons
				if reason, ok := evt.Data["reason"]; ok {
					line += fmt.Sprintf(" reason=%v", reason)
				}
				if errMsg, ok := evt.Data["error"]; ok {
					line += fmt.Sprintf(" error=%v", errMsg)
				}
			case EventAgentToolCallStart:
				if req.Verbose {
					if name, ok := evt.Data["tool_name"]; ok {
						line += fmt.Sprintf(" tool=%v", name)
					}
					if args, ok := evt.Data["arguments"]; ok {
						line += fmt.Sprintf(" args=%v", args)
					}
				}
			case EventAgentToolCallEnd:
				if req.Verbose {
					if name, ok := evt.Data["tool_name"]; ok {
						line += fmt.Sprintf(" tool=%v", name)
					}
					if dur, ok := evt.Data["duration_ms"]; ok {
						line += fmt.Sprintf(" duration=%vms", dur)
					}
				}
			case EventAgentLLMTurn:
				if req.Verbose {
					if tokens, ok := evt.Data["total_tokens"]; ok {
						line += fmt.Sprintf(" tokens=%v", tokens)
					}
				}
			}
		}

		b.WriteString(line + "\n")
	}

	// Summarize tool usage when not verbose
	if !req.Verbose {
		toolCounts := map[string]int{}
		llmTurns := 0
		for _, evt := range req.Events {
			switch evt.Type {
			case EventAgentToolCallStart:
				if name, ok := evt.Data["tool_name"].(string); ok {
					toolCounts[name]++
				}
			case EventAgentLLMTurn:
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

// linearizeGraph walks the graph from start to exit via BFS and returns
// a human-readable flow string like "start -> build -> verify -> exit".
func linearizeGraph(g *Graph) string {
	start := g.FindStartNode()
	if start == nil {
		return "(no start node found)"
	}

	visited := map[string]bool{}
	var path []string
	queue := []string{start.ID}
	visited[start.ID] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		path = append(path, current)

		for _, e := range g.OutgoingEdges(current) {
			if !visited[e.To] {
				visited[e.To] = true
				queue = append(queue, e.To)
			}
		}
	}

	return strings.Join(path, " -> ")
}
