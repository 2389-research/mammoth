// ABOUTME: get_run_logs MCP tool handler for human-readable pipeline log output.
// ABOUTME: Converts engine events to formatted log lines with optional tail and node filtering.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/2389-research/mammoth/attractor"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// GetRunLogsInput is the input schema for the get_run_logs tool.
type GetRunLogsInput struct {
	RunID  string `json:"run_id" jsonschema:"the run ID to query"`
	Tail   int    `json:"tail,omitempty" jsonschema:"return only the last N log lines"`
	NodeID string `json:"node_id,omitempty" jsonschema:"filter logs to a specific node ID"`
}

// GetRunLogsOutput is the output of the get_run_logs tool.
type GetRunLogsOutput struct {
	Lines []string `json:"lines"`
	Total int      `json:"total"`
}

// registerGetRunLogs registers the get_run_logs tool on the given MCP server.
func (s *Server) registerGetRunLogs(srv *mcpsdk.Server) {
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "get_run_logs",
		Description: "Get human-readable log lines from a pipeline run. Supports tail (last N lines) and node_id filtering.",
	}, s.handleGetRunLogs)
}

// handleGetRunLogs converts events to log lines and applies filters.
func (s *Server) handleGetRunLogs(_ context.Context, _ *mcpsdk.CallToolRequest, input GetRunLogsInput) (*mcpsdk.CallToolResult, GetRunLogsOutput, error) {
	if input.Tail < 0 {
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "tail must be non-negative"}},
			IsError: true,
		}, GetRunLogsOutput{}, nil
	}

	run, ok := s.registry.Get(input.RunID)
	if !ok {
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: fmt.Sprintf("run %q not found", input.RunID)}},
			IsError: true,
		}, GetRunLogsOutput{}, nil
	}

	run.mu.RLock()
	var lines []string
	for _, evt := range run.EventBuffer {
		if input.NodeID != "" && evt.NodeID != input.NodeID {
			continue
		}
		lines = append(lines, formatEventAsLog(evt))
	}
	run.mu.RUnlock()

	// Apply tail filter.
	if input.Tail > 0 && input.Tail < len(lines) {
		lines = lines[len(lines)-input.Tail:]
	}

	output := GetRunLogsOutput{
		Lines: lines,
		Total: len(lines),
	}
	data, err := json.Marshal(output)
	if err != nil {
		return nil, GetRunLogsOutput{}, fmt.Errorf("marshal output: %w", err)
	}
	return &mcpsdk.CallToolResult{
		Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: string(data)}},
	}, output, nil
}

// formatEventAsLog renders an engine event as a human-readable log line.
func formatEventAsLog(evt attractor.EngineEvent) string {
	ts := evt.Timestamp.Format("15:04:05.000")
	if evt.NodeID != "" {
		return fmt.Sprintf("[%s] %s %s", ts, evt.Type, evt.NodeID)
	}
	return fmt.Sprintf("[%s] %s", ts, evt.Type)
}
