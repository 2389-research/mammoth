// ABOUTME: Transition logic connecting the DOT editor phase to the build phase.
// ABOUTME: Validates DOT, runs lint diagnostics, and routes to the correct project phase.
package web

import (
	"fmt"
	"strings"

	"github.com/2389-research/mammoth/dot"
	"github.com/2389-research/mammoth/dot/validator"
)

// TransitionEditorToBuild validates the current DOT and transitions to build.
// If the DOT has parse or lint errors, it stays in the edit phase and returns an error.
// If the DOT is clean, it transitions to the build phase.
func TransitionEditorToBuild(project *Project) error {
	g, err := dot.Parse(project.DOT)
	if err != nil {
		project.Diagnostics = []string{
			"error: [build_blocked] build did not start because DOT parsing failed",
			fmt.Sprintf("error: [parse] %s", err),
		}
		project.Phase = PhaseEdit
		return fmt.Errorf("editor to build: DOT parse failed: %w", err)
	}

	diags := validator.Lint(g)
	project.Diagnostics = formatDiagnostics(diags)

	if hasErrors(diags) {
		project.Diagnostics = prependBuildBlockedSummary(project.Diagnostics, countSeverity(diags, "error"), countSeverity(diags, "warning"))
		project.Phase = PhaseEdit
		return fmt.Errorf("editor to build: DOT has validation errors")
	}

	project.Phase = PhaseBuild
	return nil
}

// hasErrors returns true if any diagnostic has severity "error".
func hasErrors(diags []dot.Diagnostic) bool {
	for _, d := range diags {
		if d.Severity == "error" {
			return true
		}
	}
	return false
}

// formatDiagnostics converts dot.Diagnostic values to human-readable strings
// for storage in the project's Diagnostics field.
func formatDiagnostics(diags []dot.Diagnostic) []string {
	result := make([]string, len(diags))
	for i, d := range diags {
		var locParts []string
		if d.NodeID != "" {
			locParts = append(locParts, fmt.Sprintf("node=%s", d.NodeID))
		}
		if d.EdgeID != "" {
			locParts = append(locParts, fmt.Sprintf("edge=%s", d.EdgeID))
		}
		loc := ""
		if len(locParts) > 0 {
			loc = " " + strings.Join(locParts, ",")
		}
		result[i] = fmt.Sprintf("%s: [%s]%s %s", d.Severity, d.Rule, loc, d.Message)
	}
	return result
}

func countSeverity(diags []dot.Diagnostic, severity string) int {
	n := 0
	for _, d := range diags {
		if d.Severity == severity {
			n++
		}
	}
	return n
}

func prependBuildBlockedSummary(diags []string, errors, warnings int) []string {
	summary := fmt.Sprintf("error: [build_blocked] build did not start; fix %d error(s)", errors)
	if warnings > 0 {
		summary += fmt.Sprintf(" (%d warning(s) also reported)", warnings)
	}
	out := make([]string, 0, len(diags)+1)
	out = append(out, summary)
	out = append(out, diags...)
	return out
}
