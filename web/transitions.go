// ABOUTME: Transition logic connecting the spec builder phase to the DOT editor and build phases.
// ABOUTME: Exports DOT from spec state, validates it, and routes to the correct project phase.
package web

import (
	"fmt"
	"strings"

	"github.com/2389-research/mammoth/dot"
	"github.com/2389-research/mammoth/dot/validator"
	"github.com/2389-research/mammoth/spec/core"
	coreexport "github.com/2389-research/mammoth/spec/core/export"
)

// TransitionSpecToEditor generates DOT from a spec state and updates the project
// for the editor phase. The DOT is exported, parsed for validation, and linted
// for diagnostics. The project is always set to the edit phase on success.
func TransitionSpecToEditor(project *Project, specState *core.SpecState) error {
	dotStr, diags, err := exportAndValidate(specState)
	if err != nil {
		return fmt.Errorf("spec to editor: %w", err)
	}

	project.DOT = dotStr
	project.Diagnostics = formatDiagnostics(diags)
	project.Phase = PhaseEdit
	return nil
}

// TransitionSpecToBuild is the "Build Now" shortcut that skips the editor.
// If the DOT has no lint errors, it transitions straight to build phase.
// If there are lint errors, it transitions to edit phase with diagnostics.
func TransitionSpecToBuild(project *Project, specState *core.SpecState) error {
	dotStr, diags, err := exportAndValidate(specState)
	if err != nil {
		return fmt.Errorf("spec to build: %w", err)
	}

	project.DOT = dotStr
	project.Diagnostics = formatDiagnostics(diags)

	if hasErrors(diags) {
		project.Phase = PhaseEdit
		return nil
	}

	project.Phase = PhaseBuild
	return nil
}

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

// exportAndValidate generates DOT from the spec state, parses it back to validate
// round-trip correctness, and runs the linter for diagnostics.
func exportAndValidate(specState *core.SpecState) (string, []dot.Diagnostic, error) {
	if specState == nil {
		return "", nil, fmt.Errorf("export DOT: spec state is nil")
	}

	dotStr := coreexport.ExportDOT(specState)

	g, err := dot.Parse(dotStr)
	if err != nil {
		return "", nil, fmt.Errorf("parse exported DOT: %w", err)
	}

	diags := validator.Lint(g)
	return dotStr, diags, nil
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
