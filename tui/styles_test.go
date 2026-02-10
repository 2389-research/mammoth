// ABOUTME: Tests for lipgloss style definitions and StyleForStatus helper.
// ABOUTME: Validates all style variables are initialized and status-style mapping is correct.
package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestStyleForStatus(t *testing.T) {
	tests := []struct {
		name     string
		status   NodeStatus
		wantSame lipgloss.Style
	}{
		{"pending", NodePending, PendingStyle},
		{"running", NodeRunning, RunningStyle},
		{"completed", NodeCompleted, CompletedStyle},
		{"failed", NodeFailed, FailedStyle},
		{"skipped", NodeSkipped, SkippedStyle},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StyleForStatus(tt.status)
			// Render a test string with both styles and compare output
			testStr := "test"
			gotRendered := got.Render(testStr)
			wantRendered := tt.wantSame.Render(testStr)
			if gotRendered != wantRendered {
				t.Errorf("StyleForStatus(%v).Render(%q) = %q, want %q",
					tt.status, testStr, gotRendered, wantRendered)
			}
		})
	}
}

func TestStyleForStatusRendersNonEmpty(t *testing.T) {
	statuses := []NodeStatus{NodePending, NodeRunning, NodeCompleted, NodeFailed, NodeSkipped}
	for _, s := range statuses {
		t.Run(s.String(), func(t *testing.T) {
			rendered := StyleForStatus(s).Render("hello")
			if rendered == "" {
				t.Errorf("StyleForStatus(%v).Render(\"hello\") returned empty string", s)
			}
		})
	}
}

func TestStyleForStatusUnknownReturnsPending(t *testing.T) {
	// An unknown status should fall back to PendingStyle
	got := StyleForStatus(NodeStatus(99))
	testStr := "fallback"
	gotRendered := got.Render(testStr)
	wantRendered := PendingStyle.Render(testStr)
	if gotRendered != wantRendered {
		t.Errorf("StyleForStatus(99).Render(%q) = %q, want PendingStyle: %q",
			testStr, gotRendered, wantRendered)
	}
}

func TestAllStyleVariablesInitialized(t *testing.T) {
	// Verify each style has at least one non-default property set by
	// inspecting its getter methods. This avoids relying on ANSI output
	// which lipgloss suppresses in non-TTY environments.

	type styleCheck struct {
		name  string
		style lipgloss.Style
		check func(lipgloss.Style) bool
	}

	hasForeground := func(s lipgloss.Style) bool {
		return s.GetForeground() != nil
	}
	hasBold := func(s lipgloss.Style) bool {
		return s.GetBold()
	}
	hasBorder := func(s lipgloss.Style) bool {
		_, top, right, bottom, left := s.GetBorder()
		return top || right || bottom || left
	}
	hasBackground := func(s lipgloss.Style) bool {
		return s.GetBackground() != nil
	}
	hasWidth := func(s lipgloss.Style) bool {
		return s.GetWidth() > 0
	}
	hasPadding := func(s lipgloss.Style) bool {
		top, right, bottom, left := s.GetPadding()
		return top > 0 || right > 0 || bottom > 0 || left > 0
	}

	checks := []styleCheck{
		{"BorderStyle", BorderStyle, hasBorder},
		{"TitleStyle", TitleStyle, hasBold},
		{"TitleStyle_fg", TitleStyle, hasForeground},
		{"PendingStyle", PendingStyle, hasForeground},
		{"RunningStyle", RunningStyle, hasForeground},
		{"RunningStyle_bold", RunningStyle, hasBold},
		{"CompletedStyle", CompletedStyle, hasForeground},
		{"FailedStyle", FailedStyle, hasForeground},
		{"FailedStyle_bold", FailedStyle, hasBold},
		{"SkippedStyle", SkippedStyle, hasForeground},
		{"LogTimestampStyle", LogTimestampStyle, hasForeground},
		{"LogEventStyle", LogEventStyle, hasForeground},
		{"LogErrorStyle", LogErrorStyle, hasForeground},
		{"LogSuccessStyle", LogSuccessStyle, hasForeground},
		{"LogRetryStyle", LogRetryStyle, hasForeground},
		{"StatusBarStyle_bg", StatusBarStyle, hasBackground},
		{"StatusBarStyle_fg", StatusBarStyle, hasForeground},
		{"StatusBarStyle_pad", StatusBarStyle, hasPadding},
		{"LabelStyle_fg", LabelStyle, hasForeground},
		{"LabelStyle_width", LabelStyle, hasWidth},
		{"ValueStyle", ValueStyle, hasForeground},
		{"HumanGateStyle_border", HumanGateStyle, hasBorder},
		{"HumanGateStyle_pad", HumanGateStyle, hasPadding},
	}

	for _, tc := range checks {
		t.Run(tc.name, func(t *testing.T) {
			if !tc.check(tc.style) {
				t.Errorf("%s failed property check; style may not be properly initialized", tc.name)
			}
		})
	}
}
