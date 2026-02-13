// ABOUTME: Tests for the GraphPanelModel which renders a DAG visualization in the TUI.
// ABOUTME: Covers model construction, status tracking, spinner animation, view rendering, and topological sorting.
package tui

import (
	"strings"
	"testing"

	"github.com/2389-research/mammoth/attractor"
)

// testGraph creates a simple linear DAG: start -> build -> test -> deploy -> done.
func testGraph() *attractor.Graph {
	return &attractor.Graph{
		Name: "test_pipeline",
		Nodes: map[string]*attractor.Node{
			"start":  {ID: "start", Attrs: map[string]string{"shape": "Mdiamond", "label": "Start"}},
			"build":  {ID: "build", Attrs: map[string]string{"shape": "box", "label": "Build"}},
			"test":   {ID: "test", Attrs: map[string]string{"shape": "box", "label": "Test"}},
			"deploy": {ID: "deploy", Attrs: map[string]string{"shape": "box", "label": "Deploy"}},
			"done":   {ID: "done", Attrs: map[string]string{"shape": "Msquare", "label": "Done"}},
		},
		Edges: []*attractor.Edge{
			{From: "start", To: "build"},
			{From: "build", To: "test"},
			{From: "test", To: "deploy"},
			{From: "deploy", To: "done"},
		},
	}
}

func TestGraphPanelNewGraphPanelModel(t *testing.T) {
	g := testGraph()
	m := NewGraphPanelModel(g)

	if m.graph != g {
		t.Error("expected graph to be set")
	}
	if m.statuses == nil {
		t.Error("expected statuses map to be initialized")
	}
	if len(m.statuses) != 0 {
		t.Errorf("expected statuses to be empty, got %d", len(m.statuses))
	}
	if m.spinnerIndex != 0 {
		t.Errorf("expected spinnerIndex 0, got %d", m.spinnerIndex)
	}
}

func TestGraphPanelSetGetNodeStatus(t *testing.T) {
	tests := []struct {
		name     string
		nodeID   string
		status   NodeStatus
		expected NodeStatus
	}{
		{"set pending", "build", NodePending, NodePending},
		{"set running", "test", NodeRunning, NodeRunning},
		{"set completed", "deploy", NodeCompleted, NodeCompleted},
		{"set failed", "start", NodeFailed, NodeFailed},
		{"set skipped", "done", NodeSkipped, NodeSkipped},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := testGraph()
			m := NewGraphPanelModel(g)
			m.SetNodeStatus(tt.nodeID, tt.status)
			got := m.GetNodeStatus(tt.nodeID)
			if got != tt.expected {
				t.Errorf("GetNodeStatus(%q) = %v, want %v", tt.nodeID, got, tt.expected)
			}
		})
	}
}

func TestGraphPanelGetNodeStatusDefaultsPending(t *testing.T) {
	g := testGraph()
	m := NewGraphPanelModel(g)

	got := m.GetNodeStatus("nonexistent_node")
	if got != NodePending {
		t.Errorf("expected NodePending for unknown node, got %v", got)
	}

	// Also check a graph node that was never explicitly set
	got = m.GetNodeStatus("build")
	if got != NodePending {
		t.Errorf("expected NodePending for unset node, got %v", got)
	}
}

func TestGraphPanelAdvanceSpinner(t *testing.T) {
	g := testGraph()
	m := NewGraphPanelModel(g)

	if m.spinnerIndex != 0 {
		t.Fatalf("expected initial spinnerIndex 0, got %d", m.spinnerIndex)
	}

	m.AdvanceSpinner()
	if m.spinnerIndex != 1 {
		t.Errorf("expected spinnerIndex 1 after first advance, got %d", m.spinnerIndex)
	}

	m.AdvanceSpinner()
	m.AdvanceSpinner()
	if m.spinnerIndex != 3 {
		t.Errorf("expected spinnerIndex 3, got %d", m.spinnerIndex)
	}
}

func TestGraphPanelAdvanceSpinnerWrapsAround(t *testing.T) {
	g := testGraph()
	m := NewGraphPanelModel(g)

	m.SetNodeStatus("build", NodeRunning)
	m.SetWidth(80)

	// Set spinner past the end of SpinnerFrames
	for i := 0; i < len(SpinnerFrames)+2; i++ {
		m.AdvanceSpinner()
	}
	view := m.View()
	expectedFrame := SpinnerFrames[(len(SpinnerFrames)+2)%len(SpinnerFrames)]
	if !strings.Contains(view, expectedFrame) {
		t.Errorf("expected view to contain spinner frame %q after wrap-around", expectedFrame)
	}
}

func TestGraphPanelViewContainsPipelineName(t *testing.T) {
	g := testGraph()
	m := NewGraphPanelModel(g)
	m.SetWidth(80)

	view := m.View()
	if !strings.Contains(view, "test_pipeline") {
		t.Errorf("expected view to contain pipeline name 'test_pipeline', got:\n%s", view)
	}
	if !strings.Contains(view, "PIPELINE") {
		t.Errorf("expected view to contain 'PIPELINE' header, got:\n%s", view)
	}
}

func TestGraphPanelViewShowsAllNodeLabels(t *testing.T) {
	g := testGraph()
	m := NewGraphPanelModel(g)
	m.SetWidth(80)

	view := m.View()
	expectedLabels := []string{"Start", "Build", "Test", "Deploy", "Done"}
	for _, label := range expectedLabels {
		if !strings.Contains(view, label) {
			t.Errorf("expected view to contain label %q, got:\n%s", label, view)
		}
	}
}

func TestGraphPanelViewShowsNodeIDWhenNoLabel(t *testing.T) {
	g := &attractor.Graph{
		Name: "no_labels",
		Nodes: map[string]*attractor.Node{
			"alpha": {ID: "alpha", Attrs: map[string]string{"shape": "box"}},
			"beta":  {ID: "beta", Attrs: map[string]string{"shape": "box"}},
		},
		Edges: []*attractor.Edge{
			{From: "alpha", To: "beta"},
		},
	}
	m := NewGraphPanelModel(g)
	m.SetWidth(80)

	view := m.View()
	if !strings.Contains(view, "alpha") {
		t.Errorf("expected view to contain node ID 'alpha', got:\n%s", view)
	}
	if !strings.Contains(view, "beta") {
		t.Errorf("expected view to contain node ID 'beta', got:\n%s", view)
	}
}

func TestGraphPanelViewShowsEdgeIndicators(t *testing.T) {
	g := testGraph()
	m := NewGraphPanelModel(g)
	m.SetWidth(80)

	view := m.View()
	if !strings.Contains(view, "-->") {
		t.Errorf("expected view to contain edge indicator '-->', got:\n%s", view)
	}
}

func TestGraphPanelViewShowsStatusIcons(t *testing.T) {
	tests := []struct {
		name     string
		status   NodeStatus
		wantIcon string
	}{
		{"pending icon", NodePending, "[ ]"},
		{"completed icon", NodeCompleted, "[*]"},
		{"failed icon", NodeFailed, "[!]"},
		{"skipped icon", NodeSkipped, "[-]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := testGraph()
			m := NewGraphPanelModel(g)
			m.SetNodeStatus("build", tt.status)
			m.SetWidth(80)

			view := m.View()
			if !strings.Contains(view, tt.wantIcon) {
				t.Errorf("expected view to contain icon %q for status %v, got:\n%s", tt.wantIcon, tt.status, view)
			}
		})
	}
}

func TestGraphPanelViewShowsRunningIcon(t *testing.T) {
	g := testGraph()
	m := NewGraphPanelModel(g)
	m.SetNodeStatus("build", NodeRunning)
	m.SetWidth(80)

	view := m.View()
	// Running nodes show [~] icon
	if !strings.Contains(view, "[~]") {
		t.Errorf("expected view to contain running icon '[~]', got:\n%s", view)
	}
}

func TestGraphPanelViewShowsSpinnerForRunningNodes(t *testing.T) {
	g := testGraph()
	m := NewGraphPanelModel(g)
	m.SetNodeStatus("build", NodeRunning)
	m.SetWidth(80)

	// Default spinner index is 0
	view := m.View()
	expectedFrame := SpinnerFrames[0]
	if !strings.Contains(view, expectedFrame) {
		t.Errorf("expected view to contain spinner frame %q for running node, got:\n%s", expectedFrame, view)
	}

	// Advance spinner and check next frame
	m.AdvanceSpinner()
	view = m.View()
	expectedFrame = SpinnerFrames[1]
	if !strings.Contains(view, expectedFrame) {
		t.Errorf("expected view to contain spinner frame %q after advance, got:\n%s", expectedFrame, view)
	}
}

func TestGraphPanelViewShowsHandlerType(t *testing.T) {
	g := testGraph()
	m := NewGraphPanelModel(g)
	m.SetWidth(80)

	view := m.View()
	// start node has shape=Mdiamond which maps to "start" handler
	if !strings.Contains(view, "(start)") {
		t.Errorf("expected view to contain handler type '(start)', got:\n%s", view)
	}
	// build node has shape=box which maps to "codergen" handler
	if !strings.Contains(view, "(codergen)") {
		t.Errorf("expected view to contain handler type '(codergen)', got:\n%s", view)
	}
	// done node has shape=Msquare which maps to "exit" handler
	if !strings.Contains(view, "(exit)") {
		t.Errorf("expected view to contain handler type '(exit)', got:\n%s", view)
	}
}

func TestGraphPanelTopologicalLevelsLinear(t *testing.T) {
	g := testGraph()
	m := NewGraphPanelModel(g)

	levels := m.topologicalLevels()

	// Linear chain: each node should be in its own level
	if len(levels) != 5 {
		t.Fatalf("expected 5 levels for linear chain, got %d: %v", len(levels), levels)
	}

	expectedOrder := []string{"start", "build", "test", "deploy", "done"}
	for i, level := range levels {
		if len(level) != 1 {
			t.Errorf("level %d: expected 1 node, got %d: %v", i, len(level), level)
			continue
		}
		if level[0] != expectedOrder[i] {
			t.Errorf("level %d: expected %q, got %q", i, expectedOrder[i], level[0])
		}
	}
}

func TestGraphPanelTopologicalLevelsDiamond(t *testing.T) {
	// Diamond DAG: start -> {A, B} -> end
	g := &attractor.Graph{
		Name: "diamond",
		Nodes: map[string]*attractor.Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"a":     {ID: "a", Attrs: map[string]string{"shape": "box"}},
			"b":     {ID: "b", Attrs: map[string]string{"shape": "box"}},
			"end":   {ID: "end", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*attractor.Edge{
			{From: "start", To: "a"},
			{From: "start", To: "b"},
			{From: "a", To: "end"},
			{From: "b", To: "end"},
		},
	}
	m := NewGraphPanelModel(g)

	levels := m.topologicalLevels()

	if len(levels) != 3 {
		t.Fatalf("expected 3 levels for diamond DAG, got %d: %v", len(levels), levels)
	}

	// Level 0: start
	if len(levels[0]) != 1 || levels[0][0] != "start" {
		t.Errorf("level 0: expected [start], got %v", levels[0])
	}

	// Level 1: a, b (sorted)
	if len(levels[1]) != 2 {
		t.Errorf("level 1: expected 2 nodes, got %d: %v", len(levels[1]), levels[1])
	} else {
		if levels[1][0] != "a" || levels[1][1] != "b" {
			t.Errorf("level 1: expected [a b] (sorted), got %v", levels[1])
		}
	}

	// Level 2: end
	if len(levels[2]) != 1 || levels[2][0] != "end" {
		t.Errorf("level 2: expected [end], got %v", levels[2])
	}
}

func TestGraphPanelTopologicalLevelsEmptyGraph(t *testing.T) {
	g := &attractor.Graph{
		Name:  "empty",
		Nodes: map[string]*attractor.Node{},
		Edges: []*attractor.Edge{},
	}
	m := NewGraphPanelModel(g)

	levels := m.topologicalLevels()
	if len(levels) != 0 {
		t.Errorf("expected 0 levels for empty graph, got %d", len(levels))
	}
}

func TestGraphPanelTopologicalLevelsSingleNode(t *testing.T) {
	g := &attractor.Graph{
		Name: "single",
		Nodes: map[string]*attractor.Node{
			"only": {ID: "only", Attrs: map[string]string{"shape": "box"}},
		},
		Edges: []*attractor.Edge{},
	}
	m := NewGraphPanelModel(g)

	levels := m.topologicalLevels()
	if len(levels) != 1 {
		t.Fatalf("expected 1 level, got %d", len(levels))
	}
	if len(levels[0]) != 1 || levels[0][0] != "only" {
		t.Errorf("expected level 0 = [only], got %v", levels[0])
	}
}

func TestGraphPanelSetWidth(t *testing.T) {
	g := testGraph()
	m := NewGraphPanelModel(g)

	m.SetWidth(120)
	if m.width != 120 {
		t.Errorf("expected width 120, got %d", m.width)
	}
}

func TestGraphPanelNodeLabelFallsBackToID(t *testing.T) {
	nodeWithLabel := &attractor.Node{ID: "my_node", Attrs: map[string]string{"label": "My Node"}}
	nodeWithoutLabel := &attractor.Node{ID: "raw_id", Attrs: map[string]string{}}
	nodeNilAttrs := &attractor.Node{ID: "nil_attrs", Attrs: nil}

	if got := nodeLabel(nodeWithLabel); got != "My Node" {
		t.Errorf("expected 'My Node', got %q", got)
	}
	if got := nodeLabel(nodeWithoutLabel); got != "raw_id" {
		t.Errorf("expected 'raw_id', got %q", got)
	}
	if got := nodeLabel(nodeNilAttrs); got != "nil_attrs" {
		t.Errorf("expected 'nil_attrs', got %q", got)
	}
}

func TestGraphPanelViewNilGraph(t *testing.T) {
	m := NewGraphPanelModel(nil)
	m.SetWidth(80)

	// Should not panic, should render something sensible
	view := m.View()
	if view == "" {
		t.Error("expected non-empty view even with nil graph")
	}
}
