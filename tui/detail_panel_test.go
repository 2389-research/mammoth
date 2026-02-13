// ABOUTME: Tests for the DetailPanelModel Bubble Tea sub-model.
// ABOUTME: Verifies rendering, node detail updates, clearing, truncation, and sizing.
package tui

import (
	"strings"
	"testing"
	"time"
)

func TestDetailPanel_NewDetailPanelModel(t *testing.T) {
	m := NewDetailPanelModel()
	if m.active != nil {
		t.Errorf("expected active to be nil, got %v", m.active)
	}
}

func TestDetailPanel_SetActiveNode_View(t *testing.T) {
	tests := []struct {
		name     string
		detail   NodeDetail
		wantSubs []string // substrings that must appear in rendered output
	}{
		{
			name: "completed codergen node",
			detail: NodeDetail{
				Name:        "build_feature",
				HandlerType: "codergen",
				Status:      NodeCompleted,
				Duration:    5 * time.Second,
				Model:       "claude-opus-4",
				ToolCalls:   12,
				TokensUsed:  4500,
				LastOutput:  "all tests passing",
			},
			wantSubs: []string{
				"build_feature",
				"codergen",
				"completed",
				"5s",
				"claude-opus-4",
				"12 calls",
				"4500",
				"all tests passing",
			},
		},
		{
			name: "running node with no model",
			detail: NodeDetail{
				Name:        "run_tests",
				HandlerType: "tool",
				Status:      NodeRunning,
				Duration:    12 * time.Second,
				Model:       "",
				ToolCalls:   3,
				TokensUsed:  0,
				LastOutput:  "executing...",
			},
			wantSubs: []string{
				"run_tests",
				"tool",
				"running",
				"12s",
				"3 calls",
				"0",
				"executing...",
			},
		},
		{
			name: "failed node",
			detail: NodeDetail{
				Name:        "deploy",
				HandlerType: "manager",
				Status:      NodeFailed,
				Duration:    45 * time.Second,
				Model:       "gpt-4o",
				ToolCalls:   0,
				TokensUsed:  1200,
				LastOutput:  "exit code 1",
			},
			wantSubs: []string{
				"deploy",
				"manager",
				"failed",
				"45s",
				"gpt-4o",
				"0 calls",
				"1200",
				"exit code 1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewDetailPanelModel()
			m.SetActiveNode(tt.detail)
			view := m.View()
			for _, sub := range tt.wantSubs {
				if !strings.Contains(view, sub) {
					t.Errorf("View() missing %q\ngot:\n%s", sub, view)
				}
			}
		})
	}
}

func TestDetailPanel_SetActiveNode_Updates(t *testing.T) {
	m := NewDetailPanelModel()

	first := NodeDetail{
		Name:        "first_node",
		HandlerType: "codergen",
		Status:      NodeRunning,
		Duration:    2 * time.Second,
	}
	m.SetActiveNode(first)
	view1 := m.View()
	if !strings.Contains(view1, "first_node") {
		t.Errorf("expected first_node in view, got:\n%s", view1)
	}

	second := NodeDetail{
		Name:        "second_node",
		HandlerType: "tool",
		Status:      NodeCompleted,
		Duration:    10 * time.Second,
	}
	m.SetActiveNode(second)
	view2 := m.View()
	if !strings.Contains(view2, "second_node") {
		t.Errorf("expected second_node in view, got:\n%s", view2)
	}
	if strings.Contains(view2, "first_node") {
		t.Errorf("expected first_node to be replaced, but still found in:\n%s", view2)
	}
}

func TestDetailPanel_Clear(t *testing.T) {
	m := NewDetailPanelModel()
	m.SetActiveNode(NodeDetail{
		Name:        "some_node",
		HandlerType: "codergen",
		Status:      NodeRunning,
	})
	m.Clear()
	if m.active != nil {
		t.Error("expected active to be nil after Clear()")
	}
	view := m.View()
	if !strings.Contains(view, "No active node") {
		t.Errorf("expected 'No active node' in view after Clear(), got:\n%s", view)
	}
}

func TestDetailPanel_View_NoActiveNode(t *testing.T) {
	m := NewDetailPanelModel()
	view := m.View()
	if !strings.Contains(view, "No active node") {
		t.Errorf("expected 'No active node' in empty panel view, got:\n%s", view)
	}
	// Should still have the title
	if !strings.Contains(view, "NODE DETAIL") {
		t.Errorf("expected 'NODE DETAIL' title in empty panel view, got:\n%s", view)
	}
}

func TestDetailPanel_View_TruncatesLongOutput(t *testing.T) {
	longOutput := strings.Repeat("x", 200)
	m := NewDetailPanelModel()
	m.SetActiveNode(NodeDetail{
		Name:        "chatty_node",
		HandlerType: "codergen",
		Status:      NodeCompleted,
		Duration:    1 * time.Second,
		LastOutput:  longOutput,
	})
	view := m.View()

	// The full 200-char string should NOT appear
	if strings.Contains(view, longOutput) {
		t.Error("expected long output to be truncated, but full string found")
	}
	// Should have the truncation indicator
	if !strings.Contains(view, "...") {
		t.Error("expected '...' truncation suffix in view")
	}
	// The first 80 chars should appear
	truncated := longOutput[:80]
	if !strings.Contains(view, truncated) {
		t.Errorf("expected first 80 chars of output in view, got:\n%s", view)
	}
}

func TestDetailPanel_View_DurationFormatting(t *testing.T) {
	tests := []struct {
		name     string
		status   NodeStatus
		duration time.Duration
		wantSub  string
	}{
		{
			name:     "running shows duration",
			status:   NodeRunning,
			duration: 12 * time.Second,
			wantSub:  "running 12s",
		},
		{
			name:     "completed shows duration",
			status:   NodeCompleted,
			duration: 3 * time.Minute,
			wantSub:  "completed 3m0s",
		},
		{
			name:     "pending shows no duration",
			status:   NodePending,
			duration: 0,
			wantSub:  "pending",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewDetailPanelModel()
			m.SetActiveNode(NodeDetail{
				Name:        "test_node",
				HandlerType: "codergen",
				Status:      tt.status,
				Duration:    tt.duration,
			})
			view := m.View()
			if !strings.Contains(view, tt.wantSub) {
				t.Errorf("expected %q in view, got:\n%s", tt.wantSub, view)
			}
		})
	}
}

func TestDetailPanel_SetSize(t *testing.T) {
	m := NewDetailPanelModel()
	m.SetSize(60, 20)

	if m.width != 60 {
		t.Errorf("expected width 60, got %d", m.width)
	}
	if m.height != 20 {
		t.Errorf("expected height 20, got %d", m.height)
	}

	// View should render (not panic) with set size
	m.SetActiveNode(NodeDetail{
		Name:        "sized_node",
		HandlerType: "tool",
		Status:      NodeCompleted,
	})
	view := m.View()
	if view == "" {
		t.Error("expected non-empty view with set size")
	}
}

func TestDetailPanel_View_ShowsTitle(t *testing.T) {
	m := NewDetailPanelModel()
	m.SetActiveNode(NodeDetail{
		Name:        "any_node",
		HandlerType: "codergen",
		Status:      NodeRunning,
	})
	view := m.View()
	if !strings.Contains(view, "NODE DETAIL") {
		t.Errorf("expected 'NODE DETAIL' title, got:\n%s", view)
	}
}

func TestDetailPanel_View_OutputNotTruncatedWhenShort(t *testing.T) {
	shortOutput := "short output"
	m := NewDetailPanelModel()
	m.SetActiveNode(NodeDetail{
		Name:        "brief_node",
		HandlerType: "tool",
		Status:      NodeCompleted,
		LastOutput:  shortOutput,
	})
	view := m.View()
	if !strings.Contains(view, shortOutput) {
		t.Errorf("expected short output %q in view, got:\n%s", shortOutput, view)
	}
}

func TestDetailPanel_View_ExactlyEightyCharsNotTruncated(t *testing.T) {
	exactOutput := strings.Repeat("a", 80)
	m := NewDetailPanelModel()
	m.SetActiveNode(NodeDetail{
		Name:       "exact_node",
		Status:     NodeCompleted,
		LastOutput: exactOutput,
	})
	view := m.View()
	if !strings.Contains(view, exactOutput) {
		t.Errorf("expected exactly 80-char output without truncation, got:\n%s", view)
	}
}
