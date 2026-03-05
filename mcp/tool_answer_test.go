// ABOUTME: Tests for the answer_question MCP tool handler.
// ABOUTME: Covers delivering answers to paused runs, errors on non-paused runs, and not-found.
package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// connectAnswerServer creates an MCP server with answer_question registered.
func connectAnswerServer(t *testing.T) (*mcpsdk.ClientSession, *Server) {
	t.Helper()
	ctx := context.Background()
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    "mammoth-test",
		Version: "v0.0.1-test",
	}, nil)

	ms := NewServer(t.TempDir())
	ms.registerAnswerQuestion(srv)

	t1, t2 := mcpsdk.NewInMemoryTransports()
	if _, err := srv.Connect(ctx, t1, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcpsdk.NewClient(&mcpsdk.Implementation{
		Name:    "test-client",
		Version: "v0.0.1-test",
	}, nil)
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs, ms
}

func TestAnswerQuestion_DeliversAnswer(t *testing.T) {
	cs, ms := connectAnswerServer(t)
	ctx := context.Background()

	run := ms.registry.Create(simplePipeline, RunConfig{})
	run.mu.Lock()
	run.Status = StatusPaused
	run.PendingQuestion = &PendingQuestion{
		ID:   "q1",
		Text: "Continue?",
	}
	run.mu.Unlock()

	// Start a goroutine to read the answer.
	received := make(chan string, 1)
	go func() {
		select {
		case answer := <-run.answerCh:
			received <- answer
		case <-time.After(5 * time.Second):
			received <- "TIMEOUT"
		}
	}()

	result, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "answer_question",
		Arguments: map[string]any{
			"run_id": run.ID,
			"answer": "yes",
		},
	})
	if err != nil {
		t.Fatalf("CallTool error: %v", err)
	}
	if result.IsError {
		text := result.Content[0].(*mcpsdk.TextContent).Text
		t.Fatalf("unexpected tool error: %s", text)
	}

	var output AnswerQuestionOutput
	text := result.Content[0].(*mcpsdk.TextContent).Text
	if err := json.Unmarshal([]byte(text), &output); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !output.Acknowledged {
		t.Error("expected acknowledged=true")
	}

	answer := <-received
	if answer != "yes" {
		t.Errorf("expected answer=yes, got %s", answer)
	}
}

func TestAnswerQuestion_ErrorOnNonPaused(t *testing.T) {
	cs, ms := connectAnswerServer(t)
	ctx := context.Background()

	run := ms.registry.Create(simplePipeline, RunConfig{})
	// Run is in StatusRunning, not paused.

	result, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "answer_question",
		Arguments: map[string]any{
			"run_id": run.ID,
			"answer": "yes",
		},
	})
	if err != nil {
		t.Fatalf("CallTool error: %v", err)
	}
	if !result.IsError {
		t.Error("expected tool error for non-paused run")
	}
}

func TestAnswerQuestion_NotFound(t *testing.T) {
	cs, _ := connectAnswerServer(t)
	ctx := context.Background()

	result, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "answer_question",
		Arguments: map[string]any{
			"run_id": "nonexistent",
			"answer": "yes",
		},
	})
	if err != nil {
		t.Fatalf("CallTool error: %v", err)
	}
	if !result.IsError {
		t.Error("expected tool error for not-found run")
	}
}
