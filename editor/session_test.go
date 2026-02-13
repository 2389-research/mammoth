// ABOUTME: Test suite for session management functionality
// ABOUTME: Covers session creation, mutations, undo/redo, and TTL cleanup

package editor

import (
	"testing"
	"time"
)

const validDOT = `digraph G {
  A [label="Node A"];
  B [label="Node B"];
  A -> B [label="edge"];
}`

const invalidDOT = `digraph G {
  ]
}`

// Task 7a Tests

func TestCreateSessionFromValidDOT(t *testing.T) {
	store := NewStore(100, time.Hour)
	sess, err := store.Create(validDOT)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if sess.Graph == nil {
		t.Fatal("expected Graph to be populated")
	}
	if sess.RawDOT == "" {
		t.Fatal("expected RawDOT to be populated")
	}
	if sess.ID == "" {
		t.Fatal("expected session ID to be set")
	}
	if sess.CreatedAt.IsZero() {
		t.Fatal("expected CreatedAt to be set")
	}
	if sess.LastAccess.IsZero() {
		t.Fatal("expected LastAccess to be set")
	}
}

func TestCreateSessionFromInvalidDOT(t *testing.T) {
	store := NewStore(100, time.Hour)
	_, err := store.Create(invalidDOT)
	if err == nil {
		t.Fatal("expected error for invalid DOT")
	}
}

func TestGetSessionByID(t *testing.T) {
	store := NewStore(100, time.Hour)
	sess, _ := store.Create(validDOT)

	originalAccess := sess.LastAccess
	time.Sleep(10 * time.Millisecond)

	retrieved, ok := store.Get(sess.ID)
	if !ok {
		t.Fatal("expected session to be found")
	}
	if retrieved.ID != sess.ID {
		t.Fatalf("expected ID %s, got %s", sess.ID, retrieved.ID)
	}
	if !retrieved.LastAccess.After(originalAccess) {
		t.Fatal("expected LastAccess to be updated")
	}
}

func TestGetNonexistentSession(t *testing.T) {
	store := NewStore(100, time.Hour)
	sess, ok := store.Get("nonexistent-id")
	if ok {
		t.Fatal("expected session not to be found")
	}
	if sess != nil {
		t.Fatal("expected nil session")
	}
}

func TestUpdateDOT(t *testing.T) {
	store := NewStore(100, time.Hour)
	sess, _ := store.Create(validDOT)

	if len(sess.UndoStack) != 0 {
		t.Fatalf("expected empty undo stack, got %d entries", len(sess.UndoStack))
	}

	newDOT := `digraph G {
  C [label="Node C"];
}`

	err := sess.UpdateDOT(newDOT)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(sess.UndoStack) != 1 {
		t.Fatalf("expected undo stack to have 1 entry, got %d", len(sess.UndoStack))
	}
	if sess.UndoStack[0] != validDOT {
		t.Fatal("expected previous DOT in undo stack")
	}
	if len(sess.RedoStack) != 0 {
		t.Fatal("expected redo stack to be cleared")
	}
}

func TestUndo(t *testing.T) {
	store := NewStore(100, time.Hour)
	sess, _ := store.Create(validDOT)

	newDOT := `digraph G {
  C [label="Node C"];
}`
	sess.UpdateDOT(newDOT)

	err := sess.Undo()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if sess.RawDOT != validDOT {
		t.Fatal("expected DOT to be restored to original")
	}
	if len(sess.RedoStack) != 1 {
		t.Fatalf("expected redo stack to have 1 entry, got %d", len(sess.RedoStack))
	}
	if len(sess.UndoStack) != 0 {
		t.Fatalf("expected undo stack to be empty, got %d", len(sess.UndoStack))
	}
}

func TestRedo(t *testing.T) {
	store := NewStore(100, time.Hour)
	sess, _ := store.Create(validDOT)

	newDOT := `digraph G {
  C [label="Node C"];
}`
	sess.UpdateDOT(newDOT)
	sess.Undo()

	err := sess.Redo()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if sess.RawDOT != newDOT {
		t.Fatal("expected DOT to be restored to updated version")
	}
	if len(sess.UndoStack) != 1 {
		t.Fatalf("expected undo stack to have 1 entry, got %d", len(sess.UndoStack))
	}
	if len(sess.RedoStack) != 0 {
		t.Fatal("expected redo stack to be empty")
	}
}

func TestUndoOnEmptyStack(t *testing.T) {
	store := NewStore(100, time.Hour)
	sess, _ := store.Create(validDOT)

	err := sess.Undo()
	if err == nil {
		t.Fatal("expected error when undoing on empty stack")
	}
}

func TestUndoStackCappedAt50(t *testing.T) {
	store := NewStore(100, time.Hour)
	sess, _ := store.Create(validDOT)

	// Push 51 updates
	for i := 0; i < 51; i++ {
		newDOT := `digraph G {
  X [label="Update"];
}`
		err := sess.UpdateDOT(newDOT)
		if err != nil {
			t.Fatalf("update %d failed: %v", i, err)
		}
	}

	if len(sess.UndoStack) != 50 {
		t.Fatalf("expected undo stack capped at 50, got %d", len(sess.UndoStack))
	}
}

func TestNewMutationClearsRedoStack(t *testing.T) {
	store := NewStore(100, time.Hour)
	sess, _ := store.Create(validDOT)

	sess.UpdateDOT(`digraph G { A; }`)
	sess.Undo()

	if len(sess.RedoStack) == 0 {
		t.Fatal("setup failed: expected redo stack to have entries")
	}

	sess.UpdateDOT(`digraph G { B; }`)

	if len(sess.RedoStack) != 0 {
		t.Fatalf("expected redo stack to be cleared, got %d entries", len(sess.RedoStack))
	}
}

// Task 7b Tests

func TestUpdateNode(t *testing.T) {
	store := NewStore(100, time.Hour)
	sess, _ := store.Create(validDOT)

	err := sess.UpdateNode("A", map[string]string{"label": "Updated A", "color": "red"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(sess.UndoStack) != 1 {
		t.Fatal("expected undo to be pushed")
	}

	// Verify node was updated in graph
	node, found := sess.Graph.Nodes["A"]
	if !found {
		t.Fatal("node A not found in graph")
	}
	if node.Attrs["label"] != "Updated A" {
		t.Errorf("expected label 'Updated A', got '%s'", node.Attrs["label"])
	}
	if node.Attrs["color"] != "red" {
		t.Errorf("expected color 'red', got '%s'", node.Attrs["color"])
	}
}

func TestUpdateNodeNonexistent(t *testing.T) {
	store := NewStore(100, time.Hour)
	sess, _ := store.Create(validDOT)

	err := sess.UpdateNode("Z", map[string]string{"label": "Ghost"})
	if err == nil {
		t.Fatal("expected error for nonexistent node")
	}
}

func TestUpdateEdge(t *testing.T) {
	store := NewStore(100, time.Hour)
	sess, _ := store.Create(validDOT)

	// Find the edge ID
	if len(sess.Graph.Edges) == 0 {
		t.Fatal("expected at least one edge")
	}
	edgeID := sess.Graph.Edges[0].ID

	err := sess.UpdateEdge(edgeID, map[string]string{"label": "Updated", "color": "blue"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(sess.UndoStack) != 1 {
		t.Fatal("expected undo to be pushed")
	}

	// Verify edge was updated
	var found bool
	for _, edge := range sess.Graph.Edges {
		if edge.ID == edgeID {
			found = true
			if edge.Attrs["label"] != "Updated" {
				t.Errorf("expected label 'Updated', got '%s'", edge.Attrs["label"])
			}
			if edge.Attrs["color"] != "blue" {
				t.Errorf("expected color 'blue', got '%s'", edge.Attrs["color"])
			}
		}
	}
	if !found {
		t.Fatal("edge not found in graph")
	}
}

func TestAddNode(t *testing.T) {
	store := NewStore(100, time.Hour)
	sess, _ := store.Create(validDOT)

	initialCount := len(sess.Graph.Nodes)

	err := sess.AddNode("C", map[string]string{"label": "Node C"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(sess.Graph.Nodes) != initialCount+1 {
		t.Fatal("expected node count to increase by 1")
	}

	node, found := sess.Graph.Nodes["C"]
	if !found {
		t.Fatal("new node not found in graph")
	}
	if node.Attrs["label"] != "Node C" {
		t.Errorf("expected label 'Node C', got '%s'", node.Attrs["label"])
	}
}

func TestAddNodeDuplicateID(t *testing.T) {
	store := NewStore(100, time.Hour)
	sess, _ := store.Create(validDOT)

	err := sess.AddNode("A", map[string]string{"label": "Duplicate"})
	if err == nil {
		t.Fatal("expected error for duplicate node ID")
	}
}

func TestRemoveNode(t *testing.T) {
	store := NewStore(100, time.Hour)
	sess, _ := store.Create(validDOT)

	initialNodeCount := len(sess.Graph.Nodes)
	initialEdgeCount := len(sess.Graph.Edges)

	err := sess.RemoveNode("A")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(sess.Graph.Nodes) != initialNodeCount-1 {
		t.Fatal("expected node count to decrease by 1")
	}

	// Verify edges connected to A are removed
	if len(sess.Graph.Edges) >= initialEdgeCount {
		t.Fatal("expected edges to be removed")
	}

	if _, exists := sess.Graph.Nodes["A"]; exists {
		t.Fatal("node A should have been removed")
	}
}

func TestAddEdge(t *testing.T) {
	store := NewStore(100, time.Hour)
	sess, _ := store.Create(validDOT)

	initialCount := len(sess.Graph.Edges)

	err := sess.AddEdge("B", "A", map[string]string{"label": "reverse"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(sess.Graph.Edges) != initialCount+1 {
		t.Fatal("expected edge count to increase by 1")
	}

	var found bool
	for _, edge := range sess.Graph.Edges {
		if edge.From == "B" && edge.To == "A" {
			found = true
			if edge.Attrs["label"] != "reverse" {
				t.Errorf("expected label 'reverse', got '%s'", edge.Attrs["label"])
			}
		}
	}
	if !found {
		t.Fatal("new edge not found in graph")
	}
}

func TestRemoveEdge(t *testing.T) {
	store := NewStore(100, time.Hour)
	sess, _ := store.Create(validDOT)

	if len(sess.Graph.Edges) == 0 {
		t.Fatal("expected at least one edge")
	}

	edgeID := sess.Graph.Edges[0].ID
	initialCount := len(sess.Graph.Edges)

	err := sess.RemoveEdge(edgeID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(sess.Graph.Edges) != initialCount-1 {
		t.Fatal("expected edge count to decrease by 1")
	}

	for _, edge := range sess.Graph.Edges {
		if edge.ID == edgeID {
			t.Fatal("edge should have been removed")
		}
	}
}

func TestAddEdgeNonexistentFromNode(t *testing.T) {
	store := NewStore(100, time.Hour)
	sess, _ := store.Create(validDOT)

	err := sess.AddEdge("Z", "A", nil)
	if err == nil {
		t.Fatal("expected error for nonexistent from node")
	}
	if len(sess.UndoStack) != 0 {
		t.Fatal("undo should not be pushed on failure")
	}
}

func TestAddEdgeNonexistentToNode(t *testing.T) {
	store := NewStore(100, time.Hour)
	sess, _ := store.Create(validDOT)

	err := sess.AddEdge("A", "Z", nil)
	if err == nil {
		t.Fatal("expected error for nonexistent to node")
	}
}

func TestRemoveNodeNonexistent(t *testing.T) {
	store := NewStore(100, time.Hour)
	sess, _ := store.Create(validDOT)

	err := sess.RemoveNode("Z")
	if err == nil {
		t.Fatal("expected error for nonexistent node")
	}
	if len(sess.UndoStack) != 0 {
		t.Fatal("undo should not be pushed on failure")
	}
}

func TestRemoveEdgeNonexistent(t *testing.T) {
	store := NewStore(100, time.Hour)
	sess, _ := store.Create(validDOT)

	err := sess.RemoveEdge("nonexistent->edge")
	if err == nil {
		t.Fatal("expected error for nonexistent edge")
	}
	if len(sess.UndoStack) != 0 {
		t.Fatal("undo should not be pushed on failure")
	}
}

func TestUpdateEdgeNonexistent(t *testing.T) {
	store := NewStore(100, time.Hour)
	sess, _ := store.Create(validDOT)

	err := sess.UpdateEdge("nonexistent->edge", map[string]string{"label": "test"})
	if err == nil {
		t.Fatal("expected error for nonexistent edge")
	}
}

func TestUpdateGraphAttrs(t *testing.T) {
	store := NewStore(100, time.Hour)
	sess, _ := store.Create(validDOT)

	err := sess.UpdateGraphAttrs(map[string]string{"bgcolor": "lightblue", "rankdir": "LR"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(sess.UndoStack) != 1 {
		t.Fatal("expected undo to be pushed")
	}

	if sess.Graph.Attrs["bgcolor"] != "lightblue" {
		t.Errorf("expected bgcolor 'lightblue', got '%s'", sess.Graph.Attrs["bgcolor"])
	}
	if sess.Graph.Attrs["rankdir"] != "LR" {
		t.Errorf("expected rankdir 'LR', got '%s'", sess.Graph.Attrs["rankdir"])
	}
}

func TestTTLExpiry(t *testing.T) {
	store := NewStore(100, time.Hour)
	sess, _ := store.Create(validDOT)

	// Simulate expiry by setting LastAccess to past
	sess.LastAccess = time.Now().Add(-2 * time.Hour)

	store.Cleanup()

	_, ok := store.Get(sess.ID)
	if ok {
		t.Fatal("expected expired session to be removed")
	}
}

func TestMaxSessions(t *testing.T) {
	store := NewStore(10, time.Hour)

	sessions := make([]*Session, 11)
	var err error

	for i := 0; i < 11; i++ {
		sessions[i], err = store.Create(validDOT)
		if i < 10 && err != nil {
			t.Fatalf("expected session %d to be created, got error: %v", i, err)
		}
	}

	// Either the 11th session failed to create, or the oldest was evicted
	store.mu.RLock()
	count := len(store.sessions)
	store.mu.RUnlock()

	if count > 10 {
		t.Fatalf("expected max 10 sessions, got %d", count)
	}
}
