// ABOUTME: Template-friendly diagnostics view that splits DOT validation results by severity.
// ABOUTME: Provides error/warning counts and messages for the build UI.
package web

import "strings"

// DiagnosticsView provides a template-friendly summary split by severity.
type DiagnosticsView struct {
	BuildBlocked bool
	ErrorCount   int
	WarningCount int
	Errors       []string
	Warnings     []string
	Other        []string
}

func classifyDiagnostics(diags []string) DiagnosticsView {
	view := DiagnosticsView{}
	for _, d := range diags {
		line := strings.TrimSpace(d)
		switch {
		case strings.HasPrefix(line, "error:"):
			view.Errors = append(view.Errors, line)
			view.ErrorCount++
		case strings.HasPrefix(line, "warning:"):
			view.Warnings = append(view.Warnings, line)
			view.WarningCount++
		default:
			view.Other = append(view.Other, line)
		}
	}
	view.BuildBlocked = view.ErrorCount > 0
	return view
}
