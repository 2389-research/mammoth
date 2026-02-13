// ABOUTME: Tests for StatusBarModel which renders a single-line pipeline status bar.
// ABOUTME: Covers construction, state mutations, elapsed time, and View() rendering.
package tui

import (
	"strings"
	"testing"
	"time"
)

func TestStatusBarNewStatusBarModel(t *testing.T) {
	tests := []struct {
		name       string
		pipeline   string
		totalNodes int
	}{
		{name: "basic", pipeline: "my_pipeline", totalNodes: 7},
		{name: "empty name", pipeline: "", totalNodes: 0},
		{name: "large pipeline", pipeline: "big_one", totalNodes: 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewStatusBarModel(tt.pipeline, tt.totalNodes)
			if m.pipelineName != tt.pipeline {
				t.Errorf("pipelineName = %q, want %q", m.pipelineName, tt.pipeline)
			}
			if m.totalNodes != tt.totalNodes {
				t.Errorf("totalNodes = %d, want %d", m.totalNodes, tt.totalNodes)
			}
			if m.completedNodes != 0 {
				t.Errorf("completedNodes = %d, want 0", m.completedNodes)
			}
			if m.activeNode != "" {
				t.Errorf("activeNode = %q, want empty", m.activeNode)
			}
		})
	}
}

func TestStatusBarStart(t *testing.T) {
	m := NewStatusBarModel("test", 5)
	if !m.startTime.IsZero() {
		t.Fatal("startTime should be zero before Start()")
	}
	before := time.Now()
	m.Start()
	after := time.Now()

	if m.startTime.Before(before) || m.startTime.After(after) {
		t.Errorf("startTime %v not between %v and %v", m.startTime, before, after)
	}
}

func TestStatusBarSetCompleted(t *testing.T) {
	m := NewStatusBarModel("test", 10)
	m.SetCompleted(3)
	if m.completedNodes != 3 {
		t.Errorf("completedNodes = %d, want 3", m.completedNodes)
	}
	m.SetCompleted(7)
	if m.completedNodes != 7 {
		t.Errorf("completedNodes = %d, want 7", m.completedNodes)
	}
}

func TestStatusBarSetActiveNode(t *testing.T) {
	m := NewStatusBarModel("test", 5)
	m.SetActiveNode("build")
	if m.activeNode != "build" {
		t.Errorf("activeNode = %q, want %q", m.activeNode, "build")
	}
	m.SetActiveNode("deploy")
	if m.activeNode != "deploy" {
		t.Errorf("activeNode = %q, want %q", m.activeNode, "deploy")
	}
}

func TestStatusBarElapsed(t *testing.T) {
	t.Run("returns zero when not started", func(t *testing.T) {
		m := NewStatusBarModel("test", 5)
		elapsed := m.Elapsed()
		if elapsed != 0 {
			t.Errorf("Elapsed() = %v, want 0", elapsed)
		}
	})

	t.Run("returns positive duration after start", func(t *testing.T) {
		m := NewStatusBarModel("test", 5)
		m.Start()
		// Sleep briefly so elapsed is measurable
		time.Sleep(5 * time.Millisecond)
		elapsed := m.Elapsed()
		if elapsed <= 0 {
			t.Errorf("Elapsed() = %v, want > 0", elapsed)
		}
	})
}

func TestStatusBarViewContainsPipelineName(t *testing.T) {
	m := NewStatusBarModel("my_cool_pipeline", 5)
	m.SetWidth(120)
	view := m.View()
	if !strings.Contains(view, "my_cool_pipeline") {
		t.Errorf("View() does not contain pipeline name, got: %q", view)
	}
}

func TestStatusBarViewContainsNodeCount(t *testing.T) {
	tests := []struct {
		name      string
		total     int
		completed int
		want      string
	}{
		{name: "zero of seven", total: 7, completed: 0, want: "0/7"},
		{name: "three of seven", total: 7, completed: 3, want: "3/7"},
		{name: "all done", total: 5, completed: 5, want: "5/5"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewStatusBarModel("test", tt.total)
			m.SetCompleted(tt.completed)
			m.SetWidth(120)
			view := m.View()
			if !strings.Contains(view, tt.want) {
				t.Errorf("View() does not contain %q, got: %q", tt.want, view)
			}
		})
	}
}

func TestStatusBarViewShowsIdleWhenNoActiveNode(t *testing.T) {
	m := NewStatusBarModel("test", 5)
	m.SetWidth(120)
	view := m.View()
	if !strings.Contains(view, "idle") {
		t.Errorf("View() should contain 'idle' when no active node, got: %q", view)
	}
}

func TestStatusBarViewShowsActiveNode(t *testing.T) {
	m := NewStatusBarModel("test", 5)
	m.SetActiveNode("build")
	m.SetWidth(120)
	view := m.View()
	if !strings.Contains(view, "build") {
		t.Errorf("View() should contain active node 'build', got: %q", view)
	}
	if strings.Contains(view, "idle") {
		t.Errorf("View() should not contain 'idle' when active node is set, got: %q", view)
	}
}

func TestStatusBarViewShowsZeroSecondsWhenNotStarted(t *testing.T) {
	m := NewStatusBarModel("test", 5)
	m.SetWidth(120)
	view := m.View()
	if !strings.Contains(view, "0s") {
		t.Errorf("View() should contain '0s' when not started, got: %q", view)
	}
}

func TestStatusBarViewShowsElapsedAfterStart(t *testing.T) {
	m := NewStatusBarModel("test", 5)
	// Manually set startTime in the past to get a predictable elapsed
	m.startTime = time.Now().Add(-15 * time.Second)
	m.SetWidth(120)
	view := m.View()
	// Should contain "15s" (or close to it, but since we check View at a point in time
	// we need to be flexible)
	if strings.Contains(view, "0s") {
		t.Errorf("View() should not contain '0s' after start, got: %q", view)
	}
	// Should contain "Elapsed:" label
	if !strings.Contains(view, "Elapsed:") {
		t.Errorf("View() should contain 'Elapsed:' label, got: %q", view)
	}
}

func TestStatusBarViewMinutesFormat(t *testing.T) {
	m := NewStatusBarModel("test", 5)
	m.startTime = time.Now().Add(-150 * time.Second) // 2m30s
	m.SetWidth(120)
	view := m.View()
	if !strings.Contains(view, "2m30s") {
		t.Errorf("View() should format as '2m30s' for 150 seconds, got: %q", view)
	}
}

func TestStatusBarSetWidthAffectsRendering(t *testing.T) {
	m := NewStatusBarModel("test", 5)

	m.SetWidth(40)
	narrow := m.View()

	m.SetWidth(120)
	wide := m.View()

	// The wide version should be wider (more padding/fill)
	if len(wide) <= len(narrow) {
		t.Errorf("wider SetWidth should produce longer output: narrow=%d, wide=%d", len(narrow), len(wide))
	}
}
