// ABOUTME: Bubble Tea sub-model for rendering a pipeline DAG with status markers and spinner animation.
// ABOUTME: Uses Kahn's algorithm for topological level computation and lipgloss for styled output.
package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/2389-research/mammoth/attractor"
	"github.com/charmbracelet/lipgloss"
)

// GraphPanelModel displays the pipeline DAG with status markers.
type GraphPanelModel struct {
	graph        *attractor.Graph
	statuses     map[string]NodeStatus
	spinnerIndex int
	width        int
}

// NewGraphPanelModel creates a new graph panel for the given pipeline graph.
func NewGraphPanelModel(g *attractor.Graph) GraphPanelModel {
	return GraphPanelModel{
		graph:    g,
		statuses: make(map[string]NodeStatus),
	}
}

// SetNodeStatus updates a node's visual status.
func (m *GraphPanelModel) SetNodeStatus(nodeID string, status NodeStatus) {
	m.statuses[nodeID] = status
}

// GetNodeStatus returns the current status (defaults to NodePending).
func (m *GraphPanelModel) GetNodeStatus(nodeID string) NodeStatus {
	if s, ok := m.statuses[nodeID]; ok {
		return s
	}
	return NodePending
}

// AdvanceSpinner increments the spinner frame index.
func (m *GraphPanelModel) AdvanceSpinner() {
	m.spinnerIndex++
}

// SetWidth sets the available width for rendering.
func (m *GraphPanelModel) SetWidth(w int) {
	m.width = w
}

// View renders the graph panel as a string.
func (m GraphPanelModel) View() string {
	if m.graph == nil {
		content := TitleStyle.Render("=== PIPELINE: (none) ===")
		if m.width > 0 {
			return BorderStyle.Width(m.width - 2).Render(content)
		}
		return BorderStyle.Render(content)
	}

	var b strings.Builder

	// Pipeline title
	title := fmt.Sprintf("=== PIPELINE: %s ===", m.graph.Name)
	b.WriteString(TitleStyle.Render(title))
	b.WriteString("\n")

	levels := m.topologicalLevels()

	for levelIdx, level := range levels {
		for _, nodeID := range level {
			node := m.graph.FindNode(nodeID)
			if node == nil {
				continue
			}

			status := m.GetNodeStatus(nodeID)
			style := StyleForStatus(status)
			icon := status.Icon()
			label := nodeLabel(node)
			handlerType := attractor.ShapeToHandlerType(node.Attrs["shape"])

			var line string
			if status == NodeRunning {
				frame := SpinnerFrames[m.spinnerIndex%len(SpinnerFrames)]
				line = fmt.Sprintf("  %s %s (%s) %s", icon, label, handlerType, frame)
			} else {
				line = fmt.Sprintf("  %s %s (%s)", icon, label, handlerType)
			}

			b.WriteString(style.Render(line))
			b.WriteString("\n")

			// Render outgoing edges (only to nodes in subsequent levels)
			if levelIdx < len(levels)-1 {
				outgoing := m.graph.OutgoingEdges(nodeID)
				for _, edge := range outgoing {
					targetNode := m.graph.FindNode(edge.To)
					targetLabel := edge.To
					if targetNode != nil {
						targetLabel = nodeLabel(targetNode)
					}
					edgeLine := fmt.Sprintf("    --> %s", targetLabel)
					b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render(edgeLine))
					b.WriteString("\n")
				}
			}
		}
	}

	content := b.String()
	if m.width > 0 {
		return BorderStyle.Width(m.width - 2).Render(content)
	}
	return BorderStyle.Render(content)
}

// topologicalLevels computes topological levels using Kahn's algorithm (BFS).
// Each level contains nodes that can run concurrently. Nodes within a level are sorted
// alphabetically for deterministic output.
func (m GraphPanelModel) topologicalLevels() [][]string {
	if m.graph == nil || len(m.graph.Nodes) == 0 {
		return nil
	}

	// Compute in-degree for each node
	inDegree := make(map[string]int)
	for _, id := range m.graph.NodeIDs() {
		inDegree[id] = 0
	}
	for _, edge := range m.graph.Edges {
		inDegree[edge.To]++
	}

	// Collect zero in-degree nodes as the first frontier
	var queue []string
	for _, id := range m.graph.NodeIDs() {
		if inDegree[id] == 0 {
			queue = append(queue, id)
		}
	}
	sort.Strings(queue)

	var levels [][]string

	for len(queue) > 0 {
		// Current level is all nodes in the queue
		level := make([]string, len(queue))
		copy(level, queue)
		levels = append(levels, level)

		// Build next frontier
		var next []string
		for _, nodeID := range queue {
			for _, edge := range m.graph.OutgoingEdges(nodeID) {
				inDegree[edge.To]--
				if inDegree[edge.To] == 0 {
					next = append(next, edge.To)
				}
			}
		}
		sort.Strings(next)
		queue = next
	}

	return levels
}

// nodeLabel returns the display label for a node, falling back to the node ID.
func nodeLabel(node *attractor.Node) string {
	if node.Attrs != nil {
		if label, ok := node.Attrs["label"]; ok && label != "" {
			return label
		}
	}
	return node.ID
}
