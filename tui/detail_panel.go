// ABOUTME: Bubble Tea sub-model for displaying detailed information about the active pipeline node.
// ABOUTME: Renders node name, handler type, status, duration, model, tool calls, tokens, and output.
package tui

import (
	"fmt"
	"strings"
	"time"
)

// NodeDetail holds metadata for the currently active/selected node.
type NodeDetail struct {
	Name        string
	HandlerType string
	Status      NodeStatus
	Duration    time.Duration
	Model       string // LLM model name (from codergen outcome)
	ToolCalls   int    // number of tool calls
	TokensUsed  int    // tokens consumed
	LastOutput  string // last stdout/stderr snippet
}

// DetailPanelModel displays detailed information about the active pipeline node.
type DetailPanelModel struct {
	active *NodeDetail
	width  int
	height int
}

// NewDetailPanelModel creates a new DetailPanelModel with no active node.
func NewDetailPanelModel() DetailPanelModel {
	return DetailPanelModel{}
}

// SetActiveNode updates the panel with new node details.
func (m *DetailPanelModel) SetActiveNode(detail NodeDetail) {
	m.active = &detail
}

// Clear removes the active node.
func (m *DetailPanelModel) Clear() {
	m.active = nil
}

// SetSize sets the available dimensions.
func (m *DetailPanelModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// maxOutputLen is the maximum number of characters shown for LastOutput.
const maxOutputLen = 80

// truncateOutput truncates s to maxOutputLen characters, appending "..." if truncated.
func truncateOutput(s string) string {
	runes := []rune(s)
	if len(runes) <= maxOutputLen {
		return s
	}
	return string(runes[:maxOutputLen]) + "..."
}

// View renders the detail panel as a string.
func (m DetailPanelModel) View() string {
	title := TitleStyle.Render("NODE DETAIL")

	var content string
	if m.active == nil {
		content = title + "\n\n" + ValueStyle.Render("No active node")
	} else {
		d := m.active

		// Format status line with optional duration
		statusStr := StyleForStatus(d.Status).Render(d.Status.String())
		if d.Duration > 0 {
			statusStr += " " + d.Duration.String()
		}

		var lines []string
		lines = append(lines, title)
		lines = append(lines, row("Name:", d.Name))
		lines = append(lines, row("Handler:", d.HandlerType))
		lines = append(lines, LabelStyle.Render("Status:")+statusStr)
		lines = append(lines, row("Model:", d.Model))
		lines = append(lines, row("Tools:", fmt.Sprintf("%d calls", d.ToolCalls)))
		lines = append(lines, row("Tokens:", fmt.Sprintf("%d", d.TokensUsed)))
		lines = append(lines, row("Output:", truncateOutput(d.LastOutput)))

		content = strings.Join(lines, "\n")
	}

	style := BorderStyle
	if m.width > 0 {
		style = style.Width(m.width)
	}
	if m.height > 0 {
		style = style.Height(m.height)
	}

	return style.Render(content)
}

// row renders a label-value pair using the standard label and value styles.
func row(label, value string) string {
	return LabelStyle.Render(label) + ValueStyle.Render(value)
}
