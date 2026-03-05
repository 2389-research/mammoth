// ABOUTME: answer_question MCP tool handler for responding to human gate questions.
// ABOUTME: Delivers answers to paused pipeline runs via the run's answer channel.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// AnswerQuestionInput is the input schema for the answer_question tool.
type AnswerQuestionInput struct {
	RunID  string `json:"run_id" jsonschema:"the run ID with a pending question"`
	Answer string `json:"answer" jsonschema:"the answer to the pending question"`
}

// AnswerQuestionOutput is the output of the answer_question tool.
type AnswerQuestionOutput struct {
	Acknowledged bool `json:"acknowledged"`
}

// registerAnswerQuestion registers the answer_question tool on the given MCP server.
func (s *Server) registerAnswerQuestion(srv *mcpsdk.Server) {
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "answer_question",
		Description: "Answer a pending human gate question in a paused pipeline run. The run must be in 'paused' status with a pending question.",
	}, s.handleAnswerQuestion)
}

// handleAnswerQuestion delivers an answer to a paused run's answer channel.
func (s *Server) handleAnswerQuestion(_ context.Context, _ *mcpsdk.CallToolRequest, input AnswerQuestionInput) (*mcpsdk.CallToolResult, AnswerQuestionOutput, error) {
	run, ok := s.registry.Get(input.RunID)
	if !ok {
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: fmt.Sprintf("run %q not found", input.RunID)}},
			IsError: true,
		}, AnswerQuestionOutput{}, nil
	}

	run.mu.RLock()
	status := run.Status
	hasPending := run.PendingQuestion != nil
	run.mu.RUnlock()

	if status != StatusPaused || !hasPending {
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: fmt.Sprintf("run %q is not waiting for input (status: %s)", input.RunID, status)}},
			IsError: true,
		}, AnswerQuestionOutput{}, nil
	}

	// Non-blocking send — if channel is full (stale answer), replace it.
	select {
	case run.answerCh <- input.Answer:
	default:
		// Drain stale answer and send the replacement.
		select {
		case <-run.answerCh:
		default:
		}
		run.answerCh <- input.Answer
	}

	output := AnswerQuestionOutput{Acknowledged: true}
	data, err := json.Marshal(output)
	if err != nil {
		return nil, AnswerQuestionOutput{}, fmt.Errorf("marshal output: %w", err)
	}
	return &mcpsdk.CallToolResult{
		Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: string(data)}},
	}, output, nil
}
