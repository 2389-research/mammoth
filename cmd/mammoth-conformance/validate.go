// ABOUTME: Validate command for the conformance CLI, running attractor validation on DOT pipelines.
// ABOUTME: Translates attractor.Diagnostic results to conformance JSON output with severity mapping.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/2389-research/mammoth/attractor"
	"github.com/2389-research/mammoth/dot"
)

// severityString converts an attractor.Severity enum to its conformance string.
func severityString(s attractor.Severity) string {
	switch s {
	case attractor.SeverityError:
		return "error"
	case attractor.SeverityWarning:
		return "warning"
	case attractor.SeverityInfo:
		return "info"
	default:
		return "info"
	}
}

// translateDiagnostics converts attractor diagnostics to conformance output.
// Empty input produces an empty (non-nil) diagnostics slice.
func translateDiagnostics(diags []attractor.Diagnostic) ConformanceValidateOutput {
	result := make([]ConformanceDiagnostic, 0, len(diags))
	for _, d := range diags {
		result = append(result, ConformanceDiagnostic{
			Severity: severityString(d.Severity),
			Message:  d.Message,
		})
	}
	return ConformanceValidateOutput{Diagnostics: result}
}

// hasErrors returns true if any diagnostic has error severity.
func hasErrors(diags []attractor.Diagnostic) bool {
	for _, d := range diags {
		if d.Severity == attractor.SeverityError {
			return true
		}
	}
	return false
}

// cmdValidate reads a DOT file, parses, transforms, validates, and outputs conformance JSON.
// Returns 0 if no errors, 1 if validation errors exist.
func cmdValidate(dotfile string) int {
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

	transforms := attractor.DefaultTransforms()
	graph = attractor.ApplyTransforms(graph, transforms...)

	diags := attractor.Validate(graph)
	output := translateDiagnostics(diags)

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(output); err != nil {
		writeError(fmt.Sprintf("encoding JSON: %v", err))
		return 1
	}

	if hasErrors(diags) {
		return 1
	}
	return 0
}
