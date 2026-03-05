// ABOUTME: validate_pipeline MCP tool handler for synchronous DOT pipeline validation.
// ABOUTME: Parses DOT source, applies transforms, runs validation rules, and returns errors/warnings.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/2389-research/mammoth/attractor"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ValidatePipelineInput is the input schema for the validate_pipeline tool.
type ValidatePipelineInput struct {
	Source string `json:"source,omitempty" jsonschema:"DOT source string to validate"`
	File   string `json:"file,omitempty"   jsonschema:"path to a DOT file to validate"`
}

// ValidatePipelineOutput is the structured output of the validate_pipeline tool.
type ValidatePipelineOutput struct {
	Valid    bool     `json:"valid"`
	Errors   []string `json:"errors,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// resolveSource returns DOT source from either a direct string or a file path.
// Returns an error if neither is provided, both are provided, or the file cannot be read.
func resolveSource(source, file string) (string, error) {
	if source != "" && file != "" {
		return "", fmt.Errorf("provide either 'source' or 'file', not both")
	}
	if source != "" {
		return source, nil
	}
	if file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("read DOT file: %w", err)
		}
		return string(data), nil
	}
	return "", fmt.Errorf("either 'source' or 'file' must be provided")
}

// registerValidatePipeline registers the validate_pipeline tool on the given MCP server.
func (s *Server) registerValidatePipeline(srv *mcpsdk.Server) {
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "validate_pipeline",
		Description: "Validate a DOT pipeline definition. Returns validation errors and warnings without running the pipeline.",
	}, s.handleValidatePipeline)
}

// handleValidatePipeline parses and validates a DOT pipeline, returning structured diagnostics.
func (s *Server) handleValidatePipeline(_ context.Context, _ *mcpsdk.CallToolRequest, input ValidatePipelineInput) (*mcpsdk.CallToolResult, ValidatePipelineOutput, error) {
	src, err := resolveSource(input.Source, input.File)
	if err != nil {
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: err.Error()}},
			IsError: true,
		}, ValidatePipelineOutput{}, nil
	}

	graph, err := attractor.Parse(src)
	if err != nil {
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: fmt.Sprintf("parse error: %v", err)}},
			IsError: true,
		}, ValidatePipelineOutput{}, nil
	}

	// Apply default transforms before validation.
	graph = attractor.ApplyTransforms(graph, attractor.DefaultTransforms()...)

	diags, valErr := attractor.ValidateOrError(graph)

	output := ValidatePipelineOutput{Valid: valErr == nil}
	for _, d := range diags {
		msg := fmt.Sprintf("[%s] %s: %s", d.Severity, d.Rule, d.Message)
		switch d.Severity {
		case attractor.SeverityError:
			output.Errors = append(output.Errors, msg)
		default:
			output.Warnings = append(output.Warnings, msg)
		}
	}

	data, err := json.Marshal(output)
	if err != nil {
		return nil, ValidatePipelineOutput{}, fmt.Errorf("marshal output: %w", err)
	}
	return &mcpsdk.CallToolResult{
		Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: string(data)}},
	}, output, nil
}
