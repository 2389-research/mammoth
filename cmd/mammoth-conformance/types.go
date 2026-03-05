// ABOUTME: Conformance JSON output types for AttractorBench integration.
// ABOUTME: Defines structs matching the AttractorBench expected schemas for parse, validate, and run output.
package main

// ConformanceNode is a single node in the conformance parse output.
type ConformanceNode struct {
	ID         string            `json:"id"`
	Shape      string            `json:"shape,omitempty"`
	Label      string            `json:"label,omitempty"`
	Attributes map[string]string `json:"attributes"`
}

// ConformanceEdge is a single edge in the conformance parse output.
type ConformanceEdge struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Label     string `json:"label,omitempty"`
	Condition string `json:"condition,omitempty"`
	Weight    int    `json:"weight"`
}

// ConformanceParseOutput is the top-level parse command output.
type ConformanceParseOutput struct {
	Nodes      []ConformanceNode `json:"nodes"`
	Edges      []ConformanceEdge `json:"edges"`
	Attributes map[string]string `json:"attributes"`
}

// ConformanceDiagnostic is a single diagnostic from validation.
type ConformanceDiagnostic struct {
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// ConformanceValidateOutput is the validate command output.
type ConformanceValidateOutput struct {
	Diagnostics []ConformanceDiagnostic `json:"diagnostics"`
}

// ConformanceNodeResult is a single node execution result in the run output.
type ConformanceNodeResult struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	Output     string `json:"output,omitempty"`
	RetryCount int    `json:"retry_count"`
}

// ConformanceRunResult is the run command output.
type ConformanceRunResult struct {
	Status  string                  `json:"status"`
	Context map[string]any          `json:"context"`
	Nodes   []ConformanceNodeResult `json:"nodes"`
}

// ConformanceError is the error output format.
type ConformanceError struct {
	Error string `json:"error"`
}
