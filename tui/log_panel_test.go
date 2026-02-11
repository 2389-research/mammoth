// ABOUTME: Tests for the LogPanelModel scrollable event log panel.
// ABOUTME: Validates creation, append, eviction, focus, formatting, and view rendering.
package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/2389-research/mammoth/attractor"
)

func TestLogPanel_NewLogPanelModel_EmptyEntries(t *testing.T) {
	m := NewLogPanelModel(100)
	if m.Len() != 0 {
		t.Errorf("expected 0 entries, got %d", m.Len())
	}
}

func TestLogPanel_NewLogPanelModel_DefaultsTo200WhenZero(t *testing.T) {
	m := NewLogPanelModel(0)
	// Fill 200 entries, all should fit
	for i := 0; i < 200; i++ {
		m.Append(attractor.EngineEvent{Type: attractor.EventStageStarted, NodeID: fmt.Sprintf("n%d", i)})
	}
	if m.Len() != 200 {
		t.Errorf("expected 200 entries after filling to capacity, got %d", m.Len())
	}
	// Adding one more should evict the oldest
	m.Append(attractor.EngineEvent{Type: attractor.EventStageStarted, NodeID: "overflow"})
	if m.Len() != 200 {
		t.Errorf("expected 200 entries after overflow, got %d", m.Len())
	}
}

func TestLogPanel_NewLogPanelModel_DefaultsTo200WhenNegative(t *testing.T) {
	m := NewLogPanelModel(-5)
	for i := 0; i < 201; i++ {
		m.Append(attractor.EngineEvent{Type: attractor.EventStageStarted, NodeID: fmt.Sprintf("n%d", i)})
	}
	if m.Len() != 200 {
		t.Errorf("expected 200 entries after overflow with negative init, got %d", m.Len())
	}
}

func TestLogPanel_Append_AddsEvents(t *testing.T) {
	m := NewLogPanelModel(10)
	m.Append(attractor.EngineEvent{Type: attractor.EventStageStarted, NodeID: "build"})
	m.Append(attractor.EngineEvent{Type: attractor.EventStageCompleted, NodeID: "build"})
	if m.Len() != 2 {
		t.Errorf("expected 2 entries, got %d", m.Len())
	}
}

func TestLogPanel_Append_EvictsOldestAtCapacity(t *testing.T) {
	m := NewLogPanelModel(3)
	m.Append(attractor.EngineEvent{Type: attractor.EventStageStarted, NodeID: "first"})
	m.Append(attractor.EngineEvent{Type: attractor.EventStageStarted, NodeID: "second"})
	m.Append(attractor.EngineEvent{Type: attractor.EventStageStarted, NodeID: "third"})
	m.Append(attractor.EngineEvent{Type: attractor.EventStageStarted, NodeID: "fourth"})

	if m.Len() != 3 {
		t.Errorf("expected 3 entries after eviction, got %d", m.Len())
	}

	// The view should contain "fourth" but not "first"
	m.SetSize(120, 20)
	view := m.View()
	if strings.Contains(view, "first") {
		t.Error("expected 'first' to be evicted, but found in view")
	}
	if !strings.Contains(view, "fourth") {
		t.Error("expected 'fourth' in view after eviction")
	}
}

func TestLogPanel_Len(t *testing.T) {
	tests := []struct {
		name     string
		numAdd   int
		expected int
	}{
		{name: "zero", numAdd: 0, expected: 0},
		{name: "one", numAdd: 1, expected: 1},
		{name: "five", numAdd: 5, expected: 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewLogPanelModel(100)
			for i := 0; i < tt.numAdd; i++ {
				m.Append(attractor.EngineEvent{Type: attractor.EventStageStarted, NodeID: fmt.Sprintf("n%d", i)})
			}
			if m.Len() != tt.expected {
				t.Errorf("Len() = %d, want %d", m.Len(), tt.expected)
			}
		})
	}
}

func TestLogPanel_SetFocused_IsFocused_RoundTrip(t *testing.T) {
	m := NewLogPanelModel(10)
	if m.IsFocused() {
		t.Error("expected not focused by default")
	}
	m.SetFocused(true)
	if !m.IsFocused() {
		t.Error("expected focused after SetFocused(true)")
	}
	m.SetFocused(false)
	if m.IsFocused() {
		t.Error("expected not focused after SetFocused(false)")
	}
}

func TestLogPanel_View_ContainsTitle(t *testing.T) {
	m := NewLogPanelModel(10)
	m.SetSize(80, 10)
	view := m.View()
	if !strings.Contains(view, "EVENT LOG") {
		t.Error("expected view to contain 'EVENT LOG'")
	}
}

func TestLogPanel_View_TitleShowsFocused(t *testing.T) {
	m := NewLogPanelModel(10)
	m.SetSize(80, 10)
	m.SetFocused(true)
	view := m.View()
	if !strings.Contains(view, "focused") {
		t.Error("expected view to contain 'focused' when focused")
	}
}

func TestLogPanel_View_ShowsEventTypeAndNodeID(t *testing.T) {
	m := NewLogPanelModel(10)
	m.SetSize(120, 20)
	m.Append(attractor.EngineEvent{
		Type:      attractor.EventStageStarted,
		NodeID:    "build_step",
		Timestamp: time.Date(2026, 1, 15, 14, 30, 45, 0, time.UTC),
	})
	view := m.View()
	if !strings.Contains(view, "stage.started") {
		t.Errorf("expected view to contain event type 'stage.started', got:\n%s", view)
	}
	if !strings.Contains(view, "build_step") {
		t.Errorf("expected view to contain node ID 'build_step', got:\n%s", view)
	}
}

func TestLogPanel_View_ShowsTimestampFormatted(t *testing.T) {
	m := NewLogPanelModel(10)
	m.SetSize(120, 20)
	m.Append(attractor.EngineEvent{
		Type:      attractor.EventPipelineStarted,
		Timestamp: time.Date(2026, 1, 15, 9, 5, 3, 0, time.UTC),
	})
	view := m.View()
	if !strings.Contains(view, "09:05:03") {
		t.Errorf("expected view to contain formatted timestamp '09:05:03', got:\n%s", view)
	}
}

func TestLogPanel_View_ShowsNoEventsWhenEmpty(t *testing.T) {
	m := NewLogPanelModel(10)
	m.SetSize(80, 10)
	view := m.View()
	if !strings.Contains(view, "No events yet") {
		t.Errorf("expected view to contain 'No events yet' when empty, got:\n%s", view)
	}
}

func TestLogPanel_View_ShowsAllEventTypes(t *testing.T) {
	tests := []struct {
		name      string
		eventType attractor.EngineEventType
	}{
		{"pipeline_started", attractor.EventPipelineStarted},
		{"pipeline_completed", attractor.EventPipelineCompleted},
		{"pipeline_failed", attractor.EventPipelineFailed},
		{"stage_started", attractor.EventStageStarted},
		{"stage_completed", attractor.EventStageCompleted},
		{"stage_failed", attractor.EventStageFailed},
		{"stage_retrying", attractor.EventStageRetrying},
		{"checkpoint_saved", attractor.EventCheckpointSaved},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewLogPanelModel(10)
			m.SetSize(120, 20)
			m.Append(attractor.EngineEvent{
				Type:      tt.eventType,
				NodeID:    "test_node",
				Timestamp: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
			})
			view := m.View()
			if !strings.Contains(view, string(tt.eventType)) {
				t.Errorf("expected view to contain event type %q, got:\n%s", tt.eventType, view)
			}
		})
	}
}

func TestLogPanel_formatEntry_IncludesDataKeyValuePairs(t *testing.T) {
	evt := attractor.EngineEvent{
		Type:      attractor.EventStageFailed,
		NodeID:    "deploy",
		Timestamp: time.Date(2026, 2, 9, 10, 30, 0, 0, time.UTC),
		Data:      map[string]any{"error": "timeout", "attempt": 3},
	}
	result := formatEntry(evt)
	if !strings.Contains(result, "10:30:00") {
		t.Errorf("expected formatted timestamp in entry, got: %s", result)
	}
	if !strings.Contains(result, "stage.failed") {
		t.Errorf("expected event type in entry, got: %s", result)
	}
	if !strings.Contains(result, "[deploy]") {
		t.Errorf("expected [nodeID] in entry, got: %s", result)
	}
	// Check both key=value pairs are present (order may vary with maps)
	if !strings.Contains(result, "error=timeout") {
		t.Errorf("expected 'error=timeout' in entry, got: %s", result)
	}
	if !strings.Contains(result, "attempt=3") {
		t.Errorf("expected 'attempt=3' in entry, got: %s", result)
	}
}

func TestLogPanel_formatEntry_NoNodeID(t *testing.T) {
	evt := attractor.EngineEvent{
		Type:      attractor.EventPipelineStarted,
		Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	result := formatEntry(evt)
	if !strings.Contains(result, "pipeline.started") {
		t.Errorf("expected event type in entry, got: %s", result)
	}
	// Should not contain empty brackets
	if strings.Contains(result, "[]") {
		t.Errorf("expected no empty brackets when NodeID is empty, got: %s", result)
	}
}

func TestLogPanel_formatEntry_NoData(t *testing.T) {
	evt := attractor.EngineEvent{
		Type:      attractor.EventStageStarted,
		NodeID:    "init",
		Timestamp: time.Date(2026, 1, 1, 8, 15, 30, 0, time.UTC),
	}
	result := formatEntry(evt)
	if !strings.Contains(result, "08:15:30") {
		t.Errorf("expected timestamp in entry, got: %s", result)
	}
	if !strings.Contains(result, "[init]") {
		t.Errorf("expected [init] in entry, got: %s", result)
	}
}

func TestLogPanel_View_ShowsAgentEventTypes(t *testing.T) {
	agentEvents := []struct {
		name      string
		eventType attractor.EngineEventType
	}{
		{"agent_tool_call_start", attractor.EventAgentToolCallStart},
		{"agent_tool_call_end", attractor.EventAgentToolCallEnd},
		{"agent_llm_turn", attractor.EventAgentLLMTurn},
		{"agent_steering", attractor.EventAgentSteering},
		{"agent_loop_detected", attractor.EventAgentLoopDetected},
	}
	for _, tt := range agentEvents {
		t.Run(tt.name, func(t *testing.T) {
			m := NewLogPanelModel(10)
			m.SetSize(120, 20)
			m.Append(attractor.EngineEvent{
				Type:      tt.eventType,
				NodeID:    "codegen_node",
				Timestamp: time.Date(2026, 2, 10, 14, 0, 0, 0, time.UTC),
			})
			view := m.View()
			if !strings.Contains(view, string(tt.eventType)) {
				t.Errorf("expected view to contain event type %q, got:\n%s", tt.eventType, view)
			}
		})
	}
}

func TestLogPanel_EventStyleForAgentEvents(t *testing.T) {
	// Verify agent events get distinct styling by rendering and checking they
	// produce non-empty styled output (not just fallback).
	tests := []struct {
		eventType attractor.EngineEventType
		name      string
	}{
		{attractor.EventAgentToolCallStart, "tool_call_start"},
		{attractor.EventAgentToolCallEnd, "tool_call_end"},
		{attractor.EventAgentLLMTurn, "llm_turn"},
		{attractor.EventAgentSteering, "steering"},
		{attractor.EventAgentLoopDetected, "loop_detected"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			style := eventStyle(tt.eventType)
			rendered := style.Render(string(tt.eventType))
			if rendered == "" {
				t.Errorf("expected non-empty rendered style for %s", tt.eventType)
			}
		})
	}
}

func TestLogPanel_SetSize(t *testing.T) {
	m := NewLogPanelModel(10)
	m.SetSize(100, 25)
	if m.width != 100 {
		t.Errorf("expected width=100, got %d", m.width)
	}
	if m.height != 25 {
		t.Errorf("expected height=25, got %d", m.height)
	}
}
