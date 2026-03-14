// ABOUTME: get_run_events MCP tool handler for reading pipeline engine events.
// ABOUTME: Supports filtering by event type and RFC3339 timestamp for incremental polling.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// GetRunEventsInput is the input schema for the get_run_events tool.
type GetRunEventsInput struct {
	RunID string   `json:"run_id" jsonschema:"the run ID to query"`
	Since string   `json:"since,omitempty" jsonschema:"RFC3339 timestamp to filter events after"`
	Types []string `json:"types,omitempty" jsonschema:"event type strings to include"`
}

// EventEntry is a serializable representation of a run event.
type EventEntry struct {
	Type      string         `json:"type"`
	NodeID    string         `json:"node_id,omitempty"`
	Timestamp string         `json:"timestamp"`
	Data      map[string]any `json:"data,omitempty"`
}

// GetRunEventsOutput is the output of the get_run_events tool.
type GetRunEventsOutput struct {
	Events []EventEntry `json:"events"`
	Total  int          `json:"total"`
}

// registerGetRunEvents registers the get_run_events tool on the given MCP server.
func (s *Server) registerGetRunEvents(srv *mcpsdk.Server) {
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "get_run_events",
		Description: "Get engine events from a pipeline run. Supports filtering by event type and timestamp for incremental polling.",
	}, s.handleGetRunEvents)
}

// handleGetRunEvents reads the event buffer and applies filters.
func (s *Server) handleGetRunEvents(_ context.Context, _ *mcpsdk.CallToolRequest, input GetRunEventsInput) (*mcpsdk.CallToolResult, GetRunEventsOutput, error) {
	run, ok := s.registry.Get(input.RunID)
	if !ok {
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: fmt.Sprintf("run %q not found", input.RunID)}},
			IsError: true,
		}, GetRunEventsOutput{}, nil
	}

	// Parse since filter.
	var sinceTime time.Time
	if input.Since != "" {
		var err error
		sinceTime, err = time.Parse(time.RFC3339Nano, input.Since)
		if err != nil {
			sinceTime, err = time.Parse(time.RFC3339, input.Since)
			if err != nil {
				return &mcpsdk.CallToolResult{
					Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: fmt.Sprintf("invalid since timestamp: %v", err)}},
					IsError: true,
				}, GetRunEventsOutput{}, nil
			}
		}
	}

	// Build type filter set.
	typeFilter := make(map[string]bool, len(input.Types))
	for _, t := range input.Types {
		typeFilter[t] = true
	}

	run.mu.RLock()
	events := make([]EventEntry, 0, len(run.EventBuffer))
	for _, evt := range run.EventBuffer {
		if !sinceTime.IsZero() && !evt.Timestamp.After(sinceTime) {
			continue
		}
		if len(typeFilter) > 0 && !typeFilter[evt.Type] {
			continue
		}
		events = append(events, EventEntry{
			Type:      evt.Type,
			NodeID:    evt.NodeID,
			Timestamp: evt.Timestamp.Format(time.RFC3339Nano),
			Data:      evt.Data,
		})
	}
	run.mu.RUnlock()

	output := GetRunEventsOutput{
		Events: events,
		Total:  len(events),
	}
	data, err := json.Marshal(output)
	if err != nil {
		return nil, GetRunEventsOutput{}, fmt.Errorf("marshal output: %w", err)
	}
	return &mcpsdk.CallToolResult{
		Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: string(data)}},
	}, output, nil
}
