// ABOUTME: Session struct with undo/redo and mutation operations
// ABOUTME: Handles DOT graph state management with validation and serialization

package editor

import (
	"fmt"
	"sync"
	"time"

	"github.com/2389-research/mammoth/dot"
	"github.com/2389-research/mammoth/dot/validator"
)

type Session struct {
	mu          sync.RWMutex
	ID          string
	Graph       *dot.Graph
	RawDOT      string
	Diagnostics []dot.Diagnostic
	UndoStack   []string
	RedoStack   []string
	CreatedAt   time.Time
	LastAccess  time.Time
}

// RLock acquires a read lock for safe concurrent reads of session data.
func (sess *Session) RLock() {
	sess.mu.RLock()
}

// RUnlock releases a read lock.
func (sess *Session) RUnlock() {
	sess.mu.RUnlock()
}

// Validate re-runs the linter and updates diagnostics under lock.
func (sess *Session) Validate() {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	sess.Diagnostics = validator.Lint(sess.Graph)
}

// UpdateDOT replaces the current DOT content with new DOT
func (sess *Session) UpdateDOT(rawDOT string) error {
	sess.mu.Lock()
	defer sess.mu.Unlock()

	graph, err := dot.Parse(rawDOT)
	if err != nil {
		return fmt.Errorf("parse error: %w", err)
	}

	sess.pushUndo()

	sess.Graph = graph
	sess.RawDOT = rawDOT
	sess.Graph.AssignEdgeIDs()
	sess.Diagnostics = validator.Lint(sess.Graph)

	return nil
}

// Undo restores the previous DOT state
func (sess *Session) Undo() error {
	sess.mu.Lock()
	defer sess.mu.Unlock()

	if len(sess.UndoStack) == 0 {
		return fmt.Errorf("nothing to undo")
	}

	// Pop from undo stack
	prevDOT := sess.UndoStack[len(sess.UndoStack)-1]
	sess.UndoStack = sess.UndoStack[:len(sess.UndoStack)-1]

	// Push current to redo stack
	sess.RedoStack = append(sess.RedoStack, sess.RawDOT)

	// Restore previous state
	graph, err := dot.Parse(prevDOT)
	if err != nil {
		return fmt.Errorf("failed to restore previous state: %w", err)
	}

	sess.Graph = graph
	sess.RawDOT = prevDOT
	sess.Graph.AssignEdgeIDs()
	sess.Diagnostics = validator.Lint(sess.Graph)

	return nil
}

// Redo restores a previously undone state
func (sess *Session) Redo() error {
	sess.mu.Lock()
	defer sess.mu.Unlock()

	if len(sess.RedoStack) == 0 {
		return fmt.Errorf("nothing to redo")
	}

	// Pop from redo stack
	nextDOT := sess.RedoStack[len(sess.RedoStack)-1]
	sess.RedoStack = sess.RedoStack[:len(sess.RedoStack)-1]

	// Push current to undo stack
	sess.UndoStack = append(sess.UndoStack, sess.RawDOT)
	if len(sess.UndoStack) > 50 {
		sess.UndoStack = sess.UndoStack[1:]
	}

	// Restore next state
	graph, err := dot.Parse(nextDOT)
	if err != nil {
		return fmt.Errorf("failed to restore next state: %w", err)
	}

	sess.Graph = graph
	sess.RawDOT = nextDOT
	sess.Graph.AssignEdgeIDs()
	sess.Diagnostics = validator.Lint(sess.Graph)

	return nil
}

// UpdateNode updates attributes of an existing node
func (sess *Session) UpdateNode(nodeID string, attrs map[string]string) error {
	sess.mu.Lock()
	defer sess.mu.Unlock()

	node, found := sess.Graph.Nodes[nodeID]
	if !found {
		return fmt.Errorf("node %s not found", nodeID)
	}

	sess.pushUndo()

	if node.Attrs == nil {
		node.Attrs = make(map[string]string)
	}
	for k, v := range attrs {
		node.Attrs[k] = v
	}

	sess.reserialize()
	return nil
}

// AddNode adds a new node to the graph
func (sess *Session) AddNode(id string, attrs map[string]string) error {
	sess.mu.Lock()
	defer sess.mu.Unlock()

	if _, exists := sess.Graph.Nodes[id]; exists {
		return fmt.Errorf("node %s already exists", id)
	}

	sess.pushUndo()

	sess.Graph.Nodes[id] = &dot.Node{
		ID:    id,
		Attrs: attrs,
	}

	sess.reserialize()
	return nil
}

// RemoveNode removes a node and its associated edges
func (sess *Session) RemoveNode(nodeID string) error {
	sess.mu.Lock()
	defer sess.mu.Unlock()

	if _, exists := sess.Graph.Nodes[nodeID]; !exists {
		return fmt.Errorf("node %s not found", nodeID)
	}

	sess.pushUndo()

	// Remove node from map
	delete(sess.Graph.Nodes, nodeID)

	// Remove edges connected to this node
	newEdges := make([]*dot.Edge, 0, len(sess.Graph.Edges))
	for _, edge := range sess.Graph.Edges {
		if edge.From != nodeID && edge.To != nodeID {
			newEdges = append(newEdges, edge)
		}
	}
	sess.Graph.Edges = newEdges

	sess.reserialize()
	return nil
}

// AddEdge adds a new edge to the graph
func (sess *Session) AddEdge(from, to string, attrs map[string]string) error {
	sess.mu.Lock()
	defer sess.mu.Unlock()

	if _, exists := sess.Graph.Nodes[from]; !exists {
		return fmt.Errorf("from node %s not found", from)
	}
	if _, exists := sess.Graph.Nodes[to]; !exists {
		return fmt.Errorf("to node %s not found", to)
	}

	sess.pushUndo()

	sess.Graph.Edges = append(sess.Graph.Edges, &dot.Edge{
		From:  from,
		To:    to,
		Attrs: attrs,
	})

	sess.reserialize()
	return nil
}

// UpdateEdge updates attributes of an existing edge
func (sess *Session) UpdateEdge(edgeID string, attrs map[string]string) error {
	sess.mu.Lock()
	defer sess.mu.Unlock()

	var edgeIndex = -1
	for i := range sess.Graph.Edges {
		if sess.Graph.Edges[i].ID == edgeID {
			edgeIndex = i
			break
		}
	}

	if edgeIndex == -1 {
		return fmt.Errorf("edge %s not found", edgeID)
	}

	sess.pushUndo()

	if sess.Graph.Edges[edgeIndex].Attrs == nil {
		sess.Graph.Edges[edgeIndex].Attrs = make(map[string]string)
	}
	for k, v := range attrs {
		sess.Graph.Edges[edgeIndex].Attrs[k] = v
	}

	sess.reserialize()
	return nil
}

// RemoveEdge removes an edge by its stable ID
func (sess *Session) RemoveEdge(edgeID string) error {
	sess.mu.Lock()
	defer sess.mu.Unlock()

	found := false
	for _, edge := range sess.Graph.Edges {
		if edge.ID == edgeID {
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("edge %s not found", edgeID)
	}

	sess.pushUndo()

	newEdges := make([]*dot.Edge, 0, len(sess.Graph.Edges))
	for _, edge := range sess.Graph.Edges {
		if edge.ID != edgeID {
			newEdges = append(newEdges, edge)
		}
	}
	sess.Graph.Edges = newEdges

	sess.reserialize()
	return nil
}

// UpdateGraphAttrs updates graph-level attributes
func (sess *Session) UpdateGraphAttrs(attrs map[string]string) error {
	sess.mu.Lock()
	defer sess.mu.Unlock()

	sess.pushUndo()

	if sess.Graph.Attrs == nil {
		sess.Graph.Attrs = make(map[string]string)
	}
	for k, v := range attrs {
		sess.Graph.Attrs[k] = v
	}

	sess.reserialize()
	return nil
}

// pushUndo saves current state to undo stack and clears redo stack
func (sess *Session) pushUndo() {
	sess.UndoStack = append(sess.UndoStack, sess.RawDOT)
	if len(sess.UndoStack) > 50 {
		sess.UndoStack = sess.UndoStack[1:]
	}
	sess.RedoStack = nil
}

// reserialize updates RawDOT, edge IDs, and diagnostics after mutation
func (sess *Session) reserialize() {
	sess.RawDOT = dot.Serialize(sess.Graph)
	sess.Graph.AssignEdgeIDs()
	sess.Diagnostics = validator.Lint(sess.Graph)
}
