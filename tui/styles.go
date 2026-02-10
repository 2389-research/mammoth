// ABOUTME: Defines lipgloss style constants for the TUI layout panels, status colors, and log formatting.
// ABOUTME: Provides StyleForStatus to map NodeStatus values to their corresponding display styles.
package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Panel borders
	BorderStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62"))

	// Title styling
	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("170"))

	// Status colors
	PendingStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	RunningStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
	CompletedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	FailedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	SkippedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

	// Log event colors
	LogTimestampStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	LogEventStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("75"))
	LogErrorStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	LogSuccessStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	LogRetryStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))

	// Status bar
	StatusBarStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("236")).
			Foreground(lipgloss.Color("252")).
			Padding(0, 1)

	// Detail panel labels
	LabelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Width(10)
	ValueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))

	// Human gate
	HumanGateStyle = lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(lipgloss.Color("214")).
			Padding(1, 2)
)

// StyleForStatus returns the appropriate lipgloss style for a NodeStatus.
func StyleForStatus(status NodeStatus) lipgloss.Style {
	switch status {
	case NodePending:
		return PendingStyle
	case NodeRunning:
		return RunningStyle
	case NodeCompleted:
		return CompletedStyle
	case NodeFailed:
		return FailedStyle
	case NodeSkipped:
		return SkippedStyle
	default:
		return PendingStyle
	}
}
