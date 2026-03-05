// ABOUTME: get_run_status MCP tool handler for querying pipeline run state.
// ABOUTME: Returns current status, node, activity, completed nodes, and pending question.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// GetRunStatusInput is the input schema for the get_run_status tool.
type GetRunStatusInput struct {
	RunID string `json:"run_id" jsonschema:"the run ID to query"`
}

// GetRunStatusOutput is the output of the get_run_status tool.
type GetRunStatusOutput struct {
	RunID           string           `json:"run_id"`
	Status          string           `json:"status"`
	CurrentNode     string           `json:"current_node,omitempty"`
	CurrentActivity string           `json:"current_activity,omitempty"`
	CompletedNodes  []string         `json:"completed_nodes,omitempty"`
	PendingQuestion *PendingQuestion `json:"pending_question,omitempty"`
	Error           string           `json:"error,omitempty"`
}

// registerGetRunStatus registers the get_run_status tool on the given MCP server.
func (s *Server) registerGetRunStatus(srv *mcpsdk.Server) {
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "get_run_status",
		Description: "Get the current status of a pipeline run. Returns node progress, activity, and any pending human gate question.",
	}, s.handleGetRunStatus)
}

// handleGetRunStatus looks up a run and returns a snapshot of its state.
func (s *Server) handleGetRunStatus(_ context.Context, _ *mcpsdk.CallToolRequest, input GetRunStatusInput) (*mcpsdk.CallToolResult, GetRunStatusOutput, error) {
	run, ok := s.registry.Get(input.RunID)
	if !ok {
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: fmt.Sprintf("run %q not found", input.RunID)}},
			IsError: true,
		}, GetRunStatusOutput{}, nil
	}

	run.mu.RLock()
	completedNodes := make([]string, len(run.CompletedNodes))
	copy(completedNodes, run.CompletedNodes)
	var pq *PendingQuestion
	if run.PendingQuestion != nil {
		pqCopy := *run.PendingQuestion
		// Deep-copy the Options slice to avoid aliasing the original.
		if run.PendingQuestion.Options != nil {
			pqCopy.Options = make([]string, len(run.PendingQuestion.Options))
			copy(pqCopy.Options, run.PendingQuestion.Options)
		}
		pq = &pqCopy
	}
	output := GetRunStatusOutput{
		RunID:           run.ID,
		Status:          string(run.Status),
		CurrentNode:     run.CurrentNode,
		CurrentActivity: run.CurrentActivity,
		CompletedNodes:  completedNodes,
		PendingQuestion: pq,
		Error:           run.Error,
	}
	run.mu.RUnlock()

	data, err := json.Marshal(output)
	if err != nil {
		return nil, GetRunStatusOutput{}, fmt.Errorf("marshal output: %w", err)
	}
	return &mcpsdk.CallToolResult{
		Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: string(data)}},
	}, output, nil
}
