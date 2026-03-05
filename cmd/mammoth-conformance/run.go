// ABOUTME: Run command for the conformance CLI, executing a pipeline and outputting results as JSON.
// ABOUTME: Bridges attractor.RunResult to conformance JSON, handling backend detection and retry tracking.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/2389-research/mammoth/attractor"
	"github.com/2389-research/mammoth/dot"
	"github.com/2389-research/mammoth/mcp"
)

// translateRunResult maps an attractor.RunResult to a ConformanceRunResult.
// Status comes from FinalOutcome.Status (or "unknown" if nil). Context includes
// executed_nodes and final_status. Nodes are built from CompletedNodes order.
// Internal context keys (starting with "_") are skipped.
func translateRunResult(result *attractor.RunResult, retries map[string]int) ConformanceRunResult {
	status := "unknown"
	if result.FinalOutcome != nil {
		status = string(result.FinalOutcome.Status)
	}

	// Build context map, skipping internal keys
	ctxMap := make(map[string]any)
	if result.Context != nil {
		snap := result.Context.Snapshot()
		for k, v := range snap {
			if strings.HasPrefix(k, "_") {
				continue
			}
			ctxMap[k] = v
		}
	}

	// Add executed_nodes and final_status
	executedNodes := make([]string, len(result.CompletedNodes))
	copy(executedNodes, result.CompletedNodes)
	ctxMap["executed_nodes"] = executedNodes
	ctxMap["final_status"] = status

	// Build node results from CompletedNodes order
	seen := make(map[string]bool)
	nodes := make([]ConformanceNodeResult, 0, len(result.CompletedNodes))
	for _, nodeID := range result.CompletedNodes {
		if seen[nodeID] {
			continue
		}
		seen[nodeID] = true

		nodeStatus := "unknown"
		var output string
		if outcome, ok := result.NodeOutcomes[nodeID]; ok {
			nodeStatus = string(outcome.Status)
			output = outcome.Notes
		}

		nodes = append(nodes, ConformanceNodeResult{
			ID:         nodeID,
			Status:     nodeStatus,
			Output:     output,
			RetryCount: retries[nodeID],
		})
	}

	return ConformanceRunResult{
		Status:  status,
		Context: ctxMap,
		Nodes:   nodes,
	}
}

// cmdRun reads a DOT file, runs the pipeline, and outputs conformance JSON.
// Returns 0 on success, 1 on error.
func cmdRun(dotfile string) int {
	data, err := os.ReadFile(dotfile)
	if err != nil {
		writeError(fmt.Sprintf("reading file: %v", err))
		return 1
	}

	graph, err := dot.Parse(string(data))
	if err != nil {
		writeError(fmt.Sprintf("parsing DOT: %v", err))
		return 1
	}

	// Apply transforms and validate
	transforms := attractor.DefaultTransforms()
	graph = attractor.ApplyTransforms(graph, transforms...)

	diags := attractor.Validate(graph)
	if hasErrors(diags) {
		output := translateDiagnostics(diags)
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(output)
		return 1
	}

	// Create temp artifact directory
	artifactDir, err := os.MkdirTemp("", "mammoth-conformance-*")
	if err != nil {
		writeError(fmt.Sprintf("creating artifact dir: %v", err))
		return 1
	}
	defer os.RemoveAll(artifactDir)

	// Detect backend
	backend := mcp.DetectBackend("")

	// Track retries via event handler
	retries := make(map[string]int)
	eventHandler := func(evt attractor.EngineEvent) {
		if evt.Type == attractor.EventStageRetrying {
			retries[evt.NodeID]++
		}
	}

	// Build engine config
	config := attractor.EngineConfig{
		ArtifactDir:  artifactDir,
		Handlers:     attractor.DefaultHandlerRegistry(),
		Backend:      backend,
		EventHandler: eventHandler,
	}

	engine := attractor.NewEngine(config)

	// Set auto-approve interviewer on wait.human handler
	handler := engine.GetHandler("wait.human")
	if wh, ok := handler.(*attractor.WaitForHumanHandler); ok {
		wh.Interviewer = attractor.NewAutoApproveInterviewer("yes")
	}

	// Run with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result, err := engine.RunGraph(ctx, graph)
	if err != nil {
		if result != nil {
			output := translateRunResult(result, retries)
			output.Context["error"] = err.Error()
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if encErr := enc.Encode(output); encErr != nil {
				fmt.Fprintf(os.Stderr, "encoding partial result: %v\n", encErr)
			}
			return 1
		}
		writeError(fmt.Sprintf("pipeline execution: %v", err))
		return 1
	}

	output := translateRunResult(result, retries)
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(output); err != nil {
		writeError(fmt.Sprintf("encoding JSON: %v", err))
		return 1
	}

	return 0
}
