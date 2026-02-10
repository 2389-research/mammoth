// ABOUTME: Implements a single-line status bar for the bottom of the TUI showing pipeline progress.
// ABOUTME: Displays pipeline name, elapsed time, node completion count, and currently active node.
package tui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// StatusBarModel displays pipeline status in a single line.
type StatusBarModel struct {
	pipelineName   string
	startTime      time.Time
	totalNodes     int
	completedNodes int
	activeNode     string
	width          int
}

// NewStatusBarModel creates a new StatusBarModel with the given pipeline name and total node count.
func NewStatusBarModel(pipelineName string, totalNodes int) StatusBarModel {
	return StatusBarModel{
		pipelineName: pipelineName,
		totalNodes:   totalNodes,
	}
}

// Start records the pipeline start time.
func (m *StatusBarModel) Start() {
	m.startTime = time.Now()
}

// SetCompleted updates the completed node count.
func (m *StatusBarModel) SetCompleted(n int) {
	m.completedNodes = n
}

// SetActiveNode sets the currently running node name.
func (m *StatusBarModel) SetActiveNode(name string) {
	m.activeNode = name
}

// SetWidth sets the bar width for rendering.
func (m *StatusBarModel) SetWidth(w int) {
	m.width = w
}

// Elapsed returns the time since Start() was called, or zero if not started.
func (m StatusBarModel) Elapsed() time.Duration {
	if m.startTime.IsZero() {
		return 0
	}
	return time.Since(m.startTime)
}

// formatElapsed formats a duration as a human-readable string.
// Durations under a minute show as seconds (e.g. "12s").
// Durations of a minute or more show as minutes and seconds (e.g. "2m30s").
func formatElapsed(d time.Duration) string {
	d = d.Truncate(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	minutes := int(d.Minutes())
	seconds := int(d.Seconds()) - minutes*60
	return fmt.Sprintf("%dm%ds", minutes, seconds)
}

// View renders the status bar as a single styled line.
func (m StatusBarModel) View() string {
	active := m.activeNode
	if active == "" {
		active = "idle"
	}

	elapsed := formatElapsed(m.Elapsed())

	content := fmt.Sprintf("Pipeline: %s | Elapsed: %s | %d/%d nodes | Active: %s",
		m.pipelineName, elapsed, m.completedNodes, m.totalNodes, active)

	style := StatusBarStyle.Width(m.width)

	return lipgloss.PlaceHorizontal(m.width, lipgloss.Left, style.Render(content))
}
