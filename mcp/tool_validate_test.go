// ABOUTME: Tests for the validate_pipeline MCP tool handler.
// ABOUTME: Covers valid DOT, invalid DOT, file-based input, and missing input error cases.
package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const validDOT = `digraph pipeline {
	start [shape=Mdiamond]
	end [shape=Msquare]
	start -> end
}`

const invalidDOT = `digraph pipeline {
	end [shape=Msquare]
	start -> end
}`

// connectTestServer creates an MCP server with validate_pipeline registered
// and returns a connected client session.
func connectTestServer(t *testing.T) *mcpsdk.ClientSession {
	t.Helper()
	ctx := context.Background()
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    "mammoth-test",
		Version: "v0.0.1-test",
	}, nil)

	ms := &Server{
		registry: NewRunRegistry(),
		index:    NewRunIndex(t.TempDir()),
		dataDir:  t.TempDir(),
	}
	ms.registerValidatePipeline(srv)

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
	return cs
}

func TestValidatePipeline_ValidDOT(t *testing.T) {
	cs := connectTestServer(t)
	ctx := context.Background()

	result, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "validate_pipeline",
		Arguments: map[string]any{"source": validDOT},
	})
	if err != nil {
		t.Fatalf("CallTool error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %v", result.Content)
	}

	var output ValidatePipelineOutput
	text := result.Content[0].(*mcpsdk.TextContent).Text
	if err := json.Unmarshal([]byte(text), &output); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if !output.Valid {
		t.Errorf("expected valid=true, got false; errors=%v", output.Errors)
	}
}

func TestValidatePipeline_InvalidDOT(t *testing.T) {
	cs := connectTestServer(t)
	ctx := context.Background()

	result, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "validate_pipeline",
		Arguments: map[string]any{"source": invalidDOT},
	})
	if err != nil {
		t.Fatalf("CallTool error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %v", result.Content)
	}

	var output ValidatePipelineOutput
	text := result.Content[0].(*mcpsdk.TextContent).Text
	if err := json.Unmarshal([]byte(text), &output); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if output.Valid {
		t.Errorf("expected valid=false for missing start node")
	}
	if len(output.Errors) == 0 {
		t.Errorf("expected at least one error")
	}
}

func TestValidatePipeline_FromFile(t *testing.T) {
	cs := connectTestServer(t)
	ctx := context.Background()

	dir := t.TempDir()
	dotFile := filepath.Join(dir, "test.dot")
	if err := os.WriteFile(dotFile, []byte(validDOT), 0644); err != nil {
		t.Fatalf("write dot file: %v", err)
	}

	result, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "validate_pipeline",
		Arguments: map[string]any{"file": dotFile},
	})
	if err != nil {
		t.Fatalf("CallTool error: %v", err)
	}

	var output ValidatePipelineOutput
	text := result.Content[0].(*mcpsdk.TextContent).Text
	if err := json.Unmarshal([]byte(text), &output); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if !output.Valid {
		t.Errorf("expected valid=true from file; errors=%v", output.Errors)
	}
}

func TestValidatePipeline_NoInput(t *testing.T) {
	cs := connectTestServer(t)
	ctx := context.Background()

	result, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "validate_pipeline",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool error: %v", err)
	}
	if !result.IsError {
		t.Errorf("expected tool error for missing input")
	}
}
