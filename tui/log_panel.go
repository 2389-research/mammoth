// ABOUTME: Implements a scrollable event log panel using the bubbles viewport component.
// ABOUTME: Displays engine events with color-coded formatting based on event type.
package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"

	"github.com/2389-research/tracker/agent"
	"github.com/2389-research/tracker/pipeline"
)

// logEntry is a unified internal representation of an event for the log panel.
type logEntry struct {
	Timestamp time.Time
	Type      string // display string for the event type
	NodeID    string
	Message   string
	Style     lipgloss.Style
}

// LogPanelModel is a scrollable event log that displays engine events.
type LogPanelModel struct {
	entries  []logEntry
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
		entries:  make([]logEntry, 0, maxEntries),
		max:      maxEntries,
		viewport: vp,
	}
}

// AppendPipelineEvent adds a pipeline event to the log, evicting the oldest entry if at capacity.
func (m *LogPanelModel) AppendPipelineEvent(evt pipeline.PipelineEvent) {
	entry := logEntry{
		Timestamp: evt.Timestamp,
		Type:      string(evt.Type),
		NodeID:    evt.NodeID,
		Message:   evt.Message,
		Style:     pipelineEventStyle(evt.Type),
	}
	m.appendEntry(entry)
}

// AppendAgentEvent adds an agent event to the log, evicting the oldest entry if at capacity.
func (m *LogPanelModel) AppendAgentEvent(evt agent.Event) {
	entry := logEntry{
		Timestamp: evt.Timestamp,
		Type:      string(evt.Type),
		NodeID:    "", // agent events don't carry a node ID directly
		Message:   agentEventMessage(evt),
		Style:     agentEventStyle(evt.Type),
	}
	m.appendEntry(entry)
}

// appendEntry adds a log entry, evicting the oldest if at capacity.
func (m *LogPanelModel) appendEntry(entry logEntry) {
	if len(m.entries) >= m.max {
		m.entries = m.entries[1:]
	}
	m.entries = append(m.entries, entry)
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
	for _, entry := range m.entries {
		lines = append(lines, formatLogEntry(entry))
	}
	m.viewport.SetContent(strings.Join(lines, "\n"))
	m.viewport.GotoBottom()
}

// formatLogEntry formats a single log entry as a log line.
func formatLogEntry(entry logEntry) string {
	ts := LogTimestampStyle.Render(entry.Timestamp.Format("15:04:05"))
	evtType := entry.Style.Render(entry.Type)

	var parts []string
	parts = append(parts, ts, evtType)

	if entry.NodeID != "" {
		parts = append(parts, fmt.Sprintf("[%s]", entry.NodeID))
	}

	if entry.Message != "" {
		parts = append(parts, entry.Message)
	}

	return strings.Join(parts, " ")
}

// pipelineEventStyle returns the appropriate lipgloss style for a given pipeline event type.
func pipelineEventStyle(evtType pipeline.PipelineEventType) lipgloss.Style {
	switch evtType {
	case pipeline.EventPipelineStarted, pipeline.EventStageStarted:
		return LogEventStyle
	case pipeline.EventPipelineCompleted, pipeline.EventStageCompleted, pipeline.EventCheckpointSaved:
		return LogSuccessStyle
	case pipeline.EventPipelineFailed, pipeline.EventStageFailed:
		return LogErrorStyle
	case pipeline.EventStageRetrying:
		return LogRetryStyle
	default:
		return LogEventStyle
	}
}

// agentEventStyle returns the appropriate lipgloss style for a given agent event type.
func agentEventStyle(evtType agent.EventType) lipgloss.Style {
	switch evtType {
	case agent.EventToolCallStart, agent.EventToolCallEnd:
		return LogAgentToolStyle
	case agent.EventTurnStart, agent.EventTurnEnd:
		return LogAgentTurnStyle
	case agent.EventSteeringInjected:
		return LogAgentSteeringStyle
	case agent.EventError:
		return LogErrorStyle
	default:
		return LogEventStyle
	}
}

// agentEventMessage builds a human-readable summary from an agent event.
func agentEventMessage(evt agent.Event) string {
	switch evt.Type {
	case agent.EventToolCallStart:
		return fmt.Sprintf("tool: %s", evt.ToolName)
	case agent.EventToolCallEnd:
		if evt.ToolError != "" {
			return fmt.Sprintf("tool done: %s (error: %s)", evt.ToolName, evt.ToolError)
		}
		return fmt.Sprintf("tool done: %s", evt.ToolName)
	case agent.EventTextDelta:
		return truncateOneLine(evt.Text, 80)
	case agent.EventSteeringInjected:
		return "steering injected"
	case agent.EventError:
		if evt.Err != nil {
			return evt.Err.Error()
		}
		return "error"
	default:
		return string(evt.Type)
	}
}
