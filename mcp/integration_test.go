// ABOUTME: Integration tests for the mammoth MCP server using the SDK's in-memory transport.
// ABOUTME: Tests validate_pipeline and run_pipeline+get_run_status as full round-trip tool calls.
package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// setupTestServer creates a fully-wired MCP server with all tools registered,
// connects it over in-memory transports, and returns the client session and
// the underlying mammoth Server for state inspection.
func setupTestServer(t *testing.T) (*mcpsdk.ClientSession, *Server) {
	t.Helper()
	ctx := context.Background()

	srv := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    "mammoth-integration-test",
		Version: "v0.0.1-test",
	}, nil)

	ms := NewServer(t.TempDir())
	ms.RegisterTools(srv)

	t1, t2 := mcpsdk.NewInMemoryTransports()
	if _, err := srv.Connect(ctx, t1, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}

	client := mcpsdk.NewClient(&mcpsdk.Implementation{
		Name:    "integration-test-client",
		Version: "v0.0.1-test",
	}, nil)
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs, ms
}

// mustJSON marshals v to json.RawMessage or panics.
func mustJSON(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic("mustJSON: " + err.Error())
	}
	return data
}

// extractTextContent extracts the text string from the first Content element
// of a CallToolResult, assuming it is a TextContent.
func extractTextContent(t *testing.T, result *mcpsdk.CallToolResult) string {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatal("extractTextContent: empty Content slice")
	}
	tc, ok := result.Content[0].(*mcpsdk.TextContent)
	if !ok {
		t.Fatalf("extractTextContent: expected *TextContent, got %T", result.Content[0])
	}
	return tc.Text
}

// TestIntegrationValidatePipeline verifies that validate_pipeline works
// end-to-end through the full MCP server with all tools registered.
func TestIntegrationValidatePipeline(t *testing.T) {
	cs, _ := setupTestServer(t)
	ctx := context.Background()

	result, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "validate_pipeline",
		Arguments: map[string]any{"source": validDOT},
	})
	if err != nil {
		t.Fatalf("CallTool error: %v", err)
	}
	if result.IsError {
		text := extractTextContent(t, result)
		t.Fatalf("unexpected tool error: %s", text)
	}

	text := extractTextContent(t, result)
	var output ValidatePipelineOutput
	if err := json.Unmarshal([]byte(text), &output); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if !output.Valid {
		t.Errorf("expected valid=true, got false; errors=%v", output.Errors)
	}
	if len(output.Errors) != 0 {
		t.Errorf("expected no errors, got %v", output.Errors)
	}
}

// TestIntegrationRunAndPollStatus launches a simple pipeline, extracts the
// run ID, and polls get_run_status until the pipeline reaches a terminal state.
func TestIntegrationRunAndPollStatus(t *testing.T) {
	cs, _ := setupTestServer(t)
	ctx := context.Background()

	// Start a simple pipeline.
	runResult, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "run_pipeline",
		Arguments: map[string]any{"source": simplePipeline},
	})
	if err != nil {
		t.Fatalf("run_pipeline CallTool error: %v", err)
	}
	if runResult.IsError {
		text := extractTextContent(t, runResult)
		t.Fatalf("run_pipeline tool error: %s", text)
	}

	// Extract run_id from the response.
	runText := extractTextContent(t, runResult)
	var runOutput RunPipelineOutput
	if err := json.Unmarshal([]byte(runText), &runOutput); err != nil {
		t.Fatalf("unmarshal run output: %v", err)
	}
	if runOutput.RunID == "" {
		t.Fatal("expected non-empty run_id")
	}
	if runOutput.Status != string(StatusRunning) {
		t.Errorf("expected initial status=%q, got %q", StatusRunning, runOutput.Status)
	}

	// Poll get_run_status until the pipeline reaches a terminal state.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		statusResult, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
			Name:      "get_run_status",
			Arguments: map[string]any{"run_id": runOutput.RunID},
		})
		if err != nil {
			t.Fatalf("get_run_status CallTool error: %v", err)
		}
		if statusResult.IsError {
			text := extractTextContent(t, statusResult)
			t.Fatalf("get_run_status tool error: %s", text)
		}

		statusText := extractTextContent(t, statusResult)
		var statusOutput GetRunStatusOutput
		if err := json.Unmarshal([]byte(statusText), &statusOutput); err != nil {
			t.Fatalf("unmarshal status output: %v", err)
		}

		switch statusOutput.Status {
		case string(StatusCompleted):
			t.Logf("pipeline completed successfully")
			return
		case string(StatusFailed):
			// A simple start->end pipeline may fail if no real backend is
			// configured, but the key assertion is that async execution ran
			// and reached a terminal state via the MCP tool round-trip.
			t.Logf("pipeline reached terminal state: failed (error=%s)", statusOutput.Error)
			return
		}

		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("pipeline did not reach a terminal state within 15 seconds")
}
