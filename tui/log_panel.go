// ABOUTME: Implements a scrollable event log panel using the bubbles viewport component.
// ABOUTME: Displays engine events with color-coded formatting based on event type.
package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"

	"github.com/2389-research/mammoth/attractor"
)

// LogPanelModel is a scrollable event log that displays engine events.
type LogPanelModel struct {
	entries  []attractor.EngineEvent
	max      int
	viewport viewport.Model
	focused  bool
	width    int
	height   int
}

// NewLogPanelModel creates a new log panel with a maximum number of entries.
// If maxEntries is <= 0, it defaults to 200.
func NewLogPanelModel(maxEntries int) LogPanelModel {
	if maxEntries <= 0 {
		maxEntries = 200
	}
	vp := viewport.New(80, 10)
	return LogPanelModel{
		entries:  make([]attractor.EngineEvent, 0, maxEntries),
		max:      maxEntries,
		viewport: vp,
	}
}

// Append adds an event to the log, evicting the oldest entry if at capacity.
func (m *LogPanelModel) Append(evt attractor.EngineEvent) {
	if len(m.entries) >= m.max {
		m.entries = m.entries[1:]
	}
	m.entries = append(m.entries, evt)
	m.syncViewport()
}

// Len returns the number of entries in the log.
func (m LogPanelModel) Len() int {
	return len(m.entries)
}

// SetFocused sets whether this panel accepts keyboard input.
func (m *LogPanelModel) SetFocused(focused bool) {
	m.focused = focused
}

// IsFocused returns whether the panel is focused.
func (m LogPanelModel) IsFocused() bool {
	return m.focused
}

// SetSize sets the available dimensions and updates the viewport.
func (m *LogPanelModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	// Reserve space for the border (2 lines top/bottom) and title (1 line)
	vpWidth := w - 2
	vpHeight := h - 3
	if vpWidth < 1 {
		vpWidth = 1
	}
	if vpHeight < 1 {
		vpHeight = 1
	}
	m.viewport.Width = vpWidth
	m.viewport.Height = vpHeight
	m.syncViewport()
}

// View renders the log panel.
func (m LogPanelModel) View() string {
	title := "EVENT LOG"
	if m.focused {
		title = "EVENT LOG (focused)"
	}

	var content string
	if len(m.entries) == 0 {
		content = "No events yet"
	} else {
		content = m.viewport.View()
	}

	rendered := TitleStyle.Render(title) + "\n" + content

	return BorderStyle.
		Width(m.width - 2).
		Height(m.height - 2).
		Render(rendered)
}

// syncViewport rebuilds the viewport content from entries and scrolls to the bottom.
func (m *LogPanelModel) syncViewport() {
	if len(m.entries) == 0 {
		m.viewport.SetContent("")
		return
	}
	var lines []string
	for _, evt := range m.entries {
		lines = append(lines, formatEntry(evt))
	}
	m.viewport.SetContent(strings.Join(lines, "\n"))
	m.viewport.GotoBottom()
}

// formatEntry formats a single engine event as a log line.
func formatEntry(evt attractor.EngineEvent) string {
	ts := LogTimestampStyle.Render(evt.Timestamp.Format("15:04:05"))
	evtType := eventStyle(evt.Type).Render(string(evt.Type))

	var parts []string
	parts = append(parts, ts, evtType)

	if evt.NodeID != "" {
		parts = append(parts, fmt.Sprintf("[%s]", evt.NodeID))
	}

	if len(evt.Data) > 0 {
		parts = append(parts, formatData(evt.Data))
	}

	return strings.Join(parts, " ")
}

// formatData formats event data as compact sorted key=value pairs.
func formatData(data map[string]any) string {
	// Sort keys for deterministic output
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	pairs := make([]string, 0, len(data))
	for _, k := range keys {
		pairs = append(pairs, fmt.Sprintf("%s=%v", k, data[k]))
	}
	return strings.Join(pairs, " ")
}

// eventStyle returns the appropriate lipgloss style for a given event type.
func eventStyle(evtType attractor.EngineEventType) lipgloss.Style {
	switch evtType {
	case attractor.EventPipelineStarted, attractor.EventStageStarted:
		return LogEventStyle
	case attractor.EventPipelineCompleted, attractor.EventStageCompleted, attractor.EventCheckpointSaved:
		return LogSuccessStyle
	case attractor.EventPipelineFailed, attractor.EventStageFailed:
		return LogErrorStyle
	case attractor.EventStageRetrying:
		return LogRetryStyle
	case attractor.EventAgentToolCallStart, attractor.EventAgentToolCallEnd:
		return LogAgentToolStyle
	case attractor.EventAgentLLMTurn:
		return LogAgentTurnStyle
	case attractor.EventAgentSteering:
		return LogAgentSteeringStyle
	case attractor.EventAgentLoopDetected:
		return LogRetryStyle
	default:
		return LogEventStyle
	}
}
