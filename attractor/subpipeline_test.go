// ABOUTME: Tests for sub-pipeline composition: loading DOT files, merging child graphs into parent graphs.
// ABOUTME: Covers LoadSubPipeline, ComposeGraphs, namespace prefixing, edge reconnection, and SubPipelineTransform.
package attractor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- LoadSubPipeline tests ---

func TestLoadSubPipeline_ValidFile(t *testing.T) {
	dir := t.TempDir()
	dotFile := filepath.Join(dir, "child.dot")

	content := `digraph child {
		start [shape=Mdiamond]
		work [shape=box, prompt="do work"]
		done [shape=Msquare]
		start -> work -> done
	}`
	if err := os.WriteFile(dotFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	g, err := LoadSubPipeline(dotFile)
	if err != nil {
		t.Fatalf("LoadSubPipeline returned error: %v", err)
	}

	if g.Name != "child" {
		t.Errorf("graph name = %q, want %q", g.Name, "child")
	}

	if len(g.Nodes) != 3 {
		t.Errorf("got %d nodes, want 3", len(g.Nodes))
	}

	startNode := g.FindStartNode()
	if startNode == nil {
		t.Error("expected start node, got nil")
	}

	exitNode := g.FindExitNode()
	if exitNode == nil {
		t.Error("expected exit node, got nil")
	}
}

func TestLoadSubPipeline_MissingFile(t *testing.T) {
	_, err := LoadSubPipeline("/nonexistent/path/to/file.dot")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoadSubPipeline_InvalidDOT(t *testing.T) {
	dir := t.TempDir()
	dotFile := filepath.Join(dir, "bad.dot")

	content := `this is not valid DOT syntax at all {{{`
	if err := os.WriteFile(dotFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	_, err := LoadSubPipeline(dotFile)
	if err == nil {
		t.Fatal("expected error for invalid DOT, got nil")
	}
}

// --- ComposeGraphs tests ---

func TestComposeGraphs_BasicMerge(t *testing.T) {
	// Parent: A -> manager -> C
	parent := &Graph{
		Name: "parent",
		Nodes: map[string]*Node{
			"A":       {ID: "A", Attrs: map[string]string{"shape": "Mdiamond"}},
			"manager": {ID: "manager", Attrs: map[string]string{"shape": "house", "sub_pipeline": "child.dot"}},
			"C":       {ID: "C", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*Edge{
			{From: "A", To: "manager", Attrs: map[string]string{}},
			{From: "manager", To: "C", Attrs: map[string]string{}},
		},
		Attrs:        map[string]string{},
		NodeDefaults: map[string]string{},
		EdgeDefaults: map[string]string{},
		Subgraphs:    make([]*Subgraph, 0),
	}

	// Child: start -> work -> done
	child := &Graph{
		Name: "child",
		Nodes: map[string]*Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":  {ID: "work", Attrs: map[string]string{"shape": "box", "prompt": "do work"}},
			"done":  {ID: "done", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*Edge{
			{From: "start", To: "work", Attrs: map[string]string{}},
			{From: "work", To: "done", Attrs: map[string]string{}},
		},
		Attrs:        map[string]string{},
		NodeDefaults: map[string]string{},
		EdgeDefaults: map[string]string{},
		Subgraphs:    make([]*Subgraph, 0),
	}

	result, err := ComposeGraphs(parent, child, "manager", "child")
	if err != nil {
		t.Fatalf("ComposeGraphs returned error: %v", err)
	}

	// The manager node should be removed
	if result.FindNode("manager") != nil {
		t.Error("manager node should have been removed from composed graph")
	}

	// Parent nodes A and C should still exist
	if result.FindNode("A") == nil {
		t.Error("parent node A should still exist")
	}
	if result.FindNode("C") == nil {
		t.Error("parent node C should still exist")
	}

	// Child nodes should exist with namespace prefix
	if result.FindNode("child.start") == nil {
		t.Error("expected namespaced child node child.start")
	}
	if result.FindNode("child.work") == nil {
		t.Error("expected namespaced child node child.work")
	}
	if result.FindNode("child.done") == nil {
		t.Error("expected namespaced child node child.done")
	}

	// Total nodes: A + C + child.start + child.work + child.done = 5
	if len(result.Nodes) != 5 {
		t.Errorf("got %d nodes, want 5; nodes: %v", len(result.Nodes), result.NodeIDs())
	}
}

func TestComposeGraphs_EdgeReconnection(t *testing.T) {
	parent := &Graph{
		Name: "parent",
		Nodes: map[string]*Node{
			"A":       {ID: "A", Attrs: map[string]string{"shape": "Mdiamond"}},
			"manager": {ID: "manager", Attrs: map[string]string{"shape": "house"}},
			"C":       {ID: "C", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*Edge{
			{From: "A", To: "manager", Attrs: map[string]string{"label": "go"}},
			{From: "manager", To: "C", Attrs: map[string]string{"label": "done"}},
		},
		Attrs:        map[string]string{},
		NodeDefaults: map[string]string{},
		EdgeDefaults: map[string]string{},
		Subgraphs:    make([]*Subgraph, 0),
	}

	child := &Graph{
		Name: "child",
		Nodes: map[string]*Node{
			"begin":  {ID: "begin", Attrs: map[string]string{"shape": "Mdiamond"}},
			"middle": {ID: "middle", Attrs: map[string]string{"shape": "box", "prompt": "process"}},
			"end":    {ID: "end", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*Edge{
			{From: "begin", To: "middle", Attrs: map[string]string{}},
			{From: "middle", To: "end", Attrs: map[string]string{}},
		},
		Attrs:        map[string]string{},
		NodeDefaults: map[string]string{},
		EdgeDefaults: map[string]string{},
		Subgraphs:    make([]*Subgraph, 0),
	}

	result, err := ComposeGraphs(parent, child, "manager", "sub")
	if err != nil {
		t.Fatalf("ComposeGraphs returned error: %v", err)
	}

	// Check that parent edge A->manager now points A->sub.begin
	foundIncoming := false
	for _, e := range result.Edges {
		if e.From == "A" && e.To == "sub.begin" {
			foundIncoming = true
			if e.Attrs["label"] != "go" {
				t.Errorf("reconnected incoming edge label = %q, want %q", e.Attrs["label"], "go")
			}
		}
	}
	if !foundIncoming {
		t.Error("expected edge A -> sub.begin (reconnected from A -> manager)")
	}

	// Check that parent edge manager->C now points sub.end->C
	foundOutgoing := false
	for _, e := range result.Edges {
		if e.From == "sub.end" && e.To == "C" {
			foundOutgoing = true
			if e.Attrs["label"] != "done" {
				t.Errorf("reconnected outgoing edge label = %q, want %q", e.Attrs["label"], "done")
			}
		}
	}
	if !foundOutgoing {
		t.Error("expected edge sub.end -> C (reconnected from manager -> C)")
	}

	// No edges should reference the old "manager" node
	for _, e := range result.Edges {
		if e.From == "manager" || e.To == "manager" {
			t.Errorf("found edge referencing removed manager node: %s -> %s", e.From, e.To)
		}
	}
}

func TestComposeGraphs_NamespacePreventsConflicts(t *testing.T) {
	// Parent has a node called "work", child also has a node called "work"
	parent := &Graph{
		Name: "parent",
		Nodes: map[string]*Node{
			"start":   {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":    {ID: "work", Attrs: map[string]string{"shape": "box", "prompt": "parent work"}},
			"manager": {ID: "manager", Attrs: map[string]string{"shape": "house"}},
			"end":     {ID: "end", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*Edge{
			{From: "start", To: "work", Attrs: map[string]string{}},
			{From: "work", To: "manager", Attrs: map[string]string{}},
			{From: "manager", To: "end", Attrs: map[string]string{}},
		},
		Attrs:        map[string]string{},
		NodeDefaults: map[string]string{},
		EdgeDefaults: map[string]string{},
		Subgraphs:    make([]*Subgraph, 0),
	}

	child := &Graph{
		Name: "child",
		Nodes: map[string]*Node{
			"begin": {ID: "begin", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":  {ID: "work", Attrs: map[string]string{"shape": "box", "prompt": "child work"}},
			"finish": {ID: "finish", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*Edge{
			{From: "begin", To: "work", Attrs: map[string]string{}},
			{From: "work", To: "finish", Attrs: map[string]string{}},
		},
		Attrs:        map[string]string{},
		NodeDefaults: map[string]string{},
		EdgeDefaults: map[string]string{},
		Subgraphs:    make([]*Subgraph, 0),
	}

	result, err := ComposeGraphs(parent, child, "manager", "ns")
	if err != nil {
		t.Fatalf("ComposeGraphs returned error: %v", err)
	}

	// Parent "work" should be untouched
	parentWork := result.FindNode("work")
	if parentWork == nil {
		t.Fatal("parent node 'work' should still exist")
	}
	if parentWork.Attrs["prompt"] != "parent work" {
		t.Errorf("parent work prompt = %q, want %q", parentWork.Attrs["prompt"], "parent work")
	}

	// Child "work" should be namespaced as "ns.work"
	childWork := result.FindNode("ns.work")
	if childWork == nil {
		t.Fatal("expected namespaced child node 'ns.work'")
	}
	if childWork.Attrs["prompt"] != "child work" {
		t.Errorf("child work prompt = %q, want %q", childWork.Attrs["prompt"], "child work")
	}
}

func TestComposeGraphs_ChildGraphAttributes(t *testing.T) {
	parent := &Graph{
		Name: "parent",
		Nodes: map[string]*Node{
			"A":       {ID: "A", Attrs: map[string]string{"shape": "Mdiamond"}},
			"manager": {ID: "manager", Attrs: map[string]string{"shape": "house"}},
			"B":       {ID: "B", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*Edge{
			{From: "A", To: "manager", Attrs: map[string]string{}},
			{From: "manager", To: "B", Attrs: map[string]string{}},
		},
		Attrs:        map[string]string{"parent_key": "parent_val", "shared_key": "parent_wins"},
		NodeDefaults: map[string]string{},
		EdgeDefaults: map[string]string{},
		Subgraphs:    make([]*Subgraph, 0),
	}

	child := &Graph{
		Name: "child",
		Nodes: map[string]*Node{
			"s": {ID: "s", Attrs: map[string]string{"shape": "Mdiamond"}},
			"e": {ID: "e", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*Edge{
			{From: "s", To: "e", Attrs: map[string]string{}},
		},
		Attrs:        map[string]string{"child_key": "child_val", "shared_key": "child_loses"},
		NodeDefaults: map[string]string{},
		EdgeDefaults: map[string]string{},
		Subgraphs:    make([]*Subgraph, 0),
	}

	result, err := ComposeGraphs(parent, child, "manager", "c")
	if err != nil {
		t.Fatalf("ComposeGraphs returned error: %v", err)
	}

	// Child attributes should be copied
	if result.Attrs["child_key"] != "child_val" {
		t.Errorf("child_key = %q, want %q", result.Attrs["child_key"], "child_val")
	}

	// Parent attributes take precedence on conflict
	if result.Attrs["shared_key"] != "parent_wins" {
		t.Errorf("shared_key = %q, want %q (parent takes precedence)", result.Attrs["shared_key"], "parent_wins")
	}

	// Parent's own attrs preserved
	if result.Attrs["parent_key"] != "parent_val" {
		t.Errorf("parent_key = %q, want %q", result.Attrs["parent_key"], "parent_val")
	}
}

func TestComposeGraphs_MissingStartInChild(t *testing.T) {
	parent := &Graph{
		Name: "parent",
		Nodes: map[string]*Node{
			"A":       {ID: "A", Attrs: map[string]string{"shape": "Mdiamond"}},
			"manager": {ID: "manager", Attrs: map[string]string{"shape": "house"}},
			"B":       {ID: "B", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*Edge{
			{From: "A", To: "manager", Attrs: map[string]string{}},
			{From: "manager", To: "B", Attrs: map[string]string{}},
		},
		Attrs:        map[string]string{},
		NodeDefaults: map[string]string{},
		EdgeDefaults: map[string]string{},
		Subgraphs:    make([]*Subgraph, 0),
	}

	// Child with no start node
	child := &Graph{
		Name: "child",
		Nodes: map[string]*Node{
			"work": {ID: "work", Attrs: map[string]string{"shape": "box"}},
			"end":  {ID: "end", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*Edge{
			{From: "work", To: "end", Attrs: map[string]string{}},
		},
		Attrs:        map[string]string{},
		NodeDefaults: map[string]string{},
		EdgeDefaults: map[string]string{},
		Subgraphs:    make([]*Subgraph, 0),
	}

	_, err := ComposeGraphs(parent, child, "manager", "c")
	if err == nil {
		t.Fatal("expected error for child graph without start node, got nil")
	}
	if !strings.Contains(err.Error(), "start") {
		t.Errorf("error message %q should mention 'start'", err.Error())
	}
}

func TestComposeGraphs_MissingTerminalInChild(t *testing.T) {
	parent := &Graph{
		Name: "parent",
		Nodes: map[string]*Node{
			"A":       {ID: "A", Attrs: map[string]string{"shape": "Mdiamond"}},
			"manager": {ID: "manager", Attrs: map[string]string{"shape": "house"}},
			"B":       {ID: "B", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*Edge{
			{From: "A", To: "manager", Attrs: map[string]string{}},
			{From: "manager", To: "B", Attrs: map[string]string{}},
		},
		Attrs:        map[string]string{},
		NodeDefaults: map[string]string{},
		EdgeDefaults: map[string]string{},
		Subgraphs:    make([]*Subgraph, 0),
	}

	// Child with no terminal node
	child := &Graph{
		Name: "child",
		Nodes: map[string]*Node{
			"begin": {ID: "begin", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":  {ID: "work", Attrs: map[string]string{"shape": "box"}},
		},
		Edges: []*Edge{
			{From: "begin", To: "work", Attrs: map[string]string{}},
		},
		Attrs:        map[string]string{},
		NodeDefaults: map[string]string{},
		EdgeDefaults: map[string]string{},
		Subgraphs:    make([]*Subgraph, 0),
	}

	_, err := ComposeGraphs(parent, child, "manager", "c")
	if err == nil {
		t.Fatal("expected error for child graph without terminal node, got nil")
	}
	if !strings.Contains(err.Error(), "terminal") {
		t.Errorf("error message %q should mention 'terminal'", err.Error())
	}
}

func TestComposeGraphs_InsertNodeNotFound(t *testing.T) {
	parent := &Graph{
		Name: "parent",
		Nodes: map[string]*Node{
			"A": {ID: "A", Attrs: map[string]string{"shape": "Mdiamond"}},
			"B": {ID: "B", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*Edge{
			{From: "A", To: "B", Attrs: map[string]string{}},
		},
		Attrs:        map[string]string{},
		NodeDefaults: map[string]string{},
		EdgeDefaults: map[string]string{},
		Subgraphs:    make([]*Subgraph, 0),
	}

	child := &Graph{
		Name: "child",
		Nodes: map[string]*Node{
			"s": {ID: "s", Attrs: map[string]string{"shape": "Mdiamond"}},
			"e": {ID: "e", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*Edge{
			{From: "s", To: "e", Attrs: map[string]string{}},
		},
		Attrs:        map[string]string{},
		NodeDefaults: map[string]string{},
		EdgeDefaults: map[string]string{},
		Subgraphs:    make([]*Subgraph, 0),
	}

	_, err := ComposeGraphs(parent, child, "nonexistent", "ns")
	if err == nil {
		t.Fatal("expected error for nonexistent insert node, got nil")
	}
}

func TestComposeGraphs_ChildInternalEdgesPreserved(t *testing.T) {
	parent := &Graph{
		Name: "parent",
		Nodes: map[string]*Node{
			"A":       {ID: "A", Attrs: map[string]string{"shape": "Mdiamond"}},
			"manager": {ID: "manager", Attrs: map[string]string{"shape": "house"}},
			"B":       {ID: "B", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*Edge{
			{From: "A", To: "manager", Attrs: map[string]string{}},
			{From: "manager", To: "B", Attrs: map[string]string{}},
		},
		Attrs:        map[string]string{},
		NodeDefaults: map[string]string{},
		EdgeDefaults: map[string]string{},
		Subgraphs:    make([]*Subgraph, 0),
	}

	// Child with 4 nodes and internal branching
	child := &Graph{
		Name: "child",
		Nodes: map[string]*Node{
			"s":  {ID: "s", Attrs: map[string]string{"shape": "Mdiamond"}},
			"w1": {ID: "w1", Attrs: map[string]string{"shape": "box", "prompt": "step 1"}},
			"w2": {ID: "w2", Attrs: map[string]string{"shape": "box", "prompt": "step 2"}},
			"e":  {ID: "e", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*Edge{
			{From: "s", To: "w1", Attrs: map[string]string{}},
			{From: "w1", To: "w2", Attrs: map[string]string{"label": "next"}},
			{From: "w2", To: "e", Attrs: map[string]string{}},
		},
		Attrs:        map[string]string{},
		NodeDefaults: map[string]string{},
		EdgeDefaults: map[string]string{},
		Subgraphs:    make([]*Subgraph, 0),
	}

	result, err := ComposeGraphs(parent, child, "manager", "sub")
	if err != nil {
		t.Fatalf("ComposeGraphs returned error: %v", err)
	}

	// Check child internal edges are preserved with namespace
	foundInternal := false
	for _, e := range result.Edges {
		if e.From == "sub.w1" && e.To == "sub.w2" {
			foundInternal = true
			if e.Attrs["label"] != "next" {
				t.Errorf("internal edge label = %q, want %q", e.Attrs["label"], "next")
			}
		}
	}
	if !foundInternal {
		t.Error("expected namespaced internal edge sub.w1 -> sub.w2")
	}
}

// --- SubPipelineTransform tests ---

func TestSubPipelineTransform_ImplementsTransform(t *testing.T) {
	var _ Transform = &SubPipelineTransform{}
}

func TestSubPipelineTransform_AppliesComposition(t *testing.T) {
	dir := t.TempDir()

	// Write a child DOT file
	childDOT := `digraph child {
		cstart [shape=Mdiamond]
		cwork [shape=box, prompt="child work"]
		cdone [shape=Msquare]
		cstart -> cwork -> cdone
	}`
	childPath := filepath.Join(dir, "child.dot")
	if err := os.WriteFile(childPath, []byte(childDOT), 0644); err != nil {
		t.Fatalf("failed to write child DOT file: %v", err)
	}

	// Create parent graph with a manager node referencing the child DOT file
	parent := &Graph{
		Name: "parent",
		Nodes: map[string]*Node{
			"start":   {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"manager": {ID: "manager", Attrs: map[string]string{"shape": "house", "sub_pipeline": childPath}},
			"end":     {ID: "end", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*Edge{
			{From: "start", To: "manager", Attrs: map[string]string{}},
			{From: "manager", To: "end", Attrs: map[string]string{}},
		},
		Attrs:        map[string]string{},
		NodeDefaults: map[string]string{},
		EdgeDefaults: map[string]string{},
		Subgraphs:    make([]*Subgraph, 0),
	}

	transform := &SubPipelineTransform{}
	result := transform.Apply(parent)

	// Manager node should be gone
	if result.FindNode("manager") != nil {
		t.Error("manager node should have been replaced by sub-pipeline")
	}

	// Child nodes should be namespaced
	if result.FindNode("manager.cstart") == nil {
		t.Error("expected namespaced child node manager.cstart")
	}
	if result.FindNode("manager.cwork") == nil {
		t.Error("expected namespaced child node manager.cwork")
	}
	if result.FindNode("manager.cdone") == nil {
		t.Error("expected namespaced child node manager.cdone")
	}

	// Parent nodes should still exist
	if result.FindNode("start") == nil {
		t.Error("parent start node should still exist")
	}
	if result.FindNode("end") == nil {
		t.Error("parent end node should still exist")
	}
}

func TestSubPipelineTransform_NoSubPipelinesIsNoop(t *testing.T) {
	g := &Graph{
		Name: "simple",
		Nodes: map[string]*Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"end":   {ID: "end", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*Edge{
			{From: "start", To: "end", Attrs: map[string]string{}},
		},
		Attrs:        map[string]string{},
		NodeDefaults: map[string]string{},
		EdgeDefaults: map[string]string{},
		Subgraphs:    make([]*Subgraph, 0),
	}

	transform := &SubPipelineTransform{}
	result := transform.Apply(g)

	if len(result.Nodes) != 2 {
		t.Errorf("got %d nodes, want 2 (no changes expected)", len(result.Nodes))
	}
}

func TestSubPipelineTransform_MissingFileLogsError(t *testing.T) {
	// When sub_pipeline points to a missing file, the transform should
	// leave the node intact rather than crashing
	g := &Graph{
		Name: "parent",
		Nodes: map[string]*Node{
			"start":   {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"manager": {ID: "manager", Attrs: map[string]string{"shape": "house", "sub_pipeline": "/nonexistent/child.dot"}},
			"end":     {ID: "end", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*Edge{
			{From: "start", To: "manager", Attrs: map[string]string{}},
			{From: "manager", To: "end", Attrs: map[string]string{}},
		},
		Attrs:        map[string]string{},
		NodeDefaults: map[string]string{},
		EdgeDefaults: map[string]string{},
		Subgraphs:    make([]*Subgraph, 0),
	}

	transform := &SubPipelineTransform{}
	result := transform.Apply(g)

	// The manager node should remain because the file could not be loaded
	if result.FindNode("manager") == nil {
		t.Error("manager node should remain when sub_pipeline file is missing")
	}
}

func TestComposeGraphs_MultipleIncomingEdges(t *testing.T) {
	// Parent: both A and B point to manager
	parent := &Graph{
		Name: "parent",
		Nodes: map[string]*Node{
			"A":       {ID: "A", Attrs: map[string]string{"shape": "Mdiamond"}},
			"B":       {ID: "B", Attrs: map[string]string{"shape": "box", "prompt": "alt path"}},
			"manager": {ID: "manager", Attrs: map[string]string{"shape": "house"}},
			"C":       {ID: "C", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*Edge{
			{From: "A", To: "B", Attrs: map[string]string{}},
			{From: "A", To: "manager", Attrs: map[string]string{"label": "direct"}},
			{From: "B", To: "manager", Attrs: map[string]string{"label": "via B"}},
			{From: "manager", To: "C", Attrs: map[string]string{}},
		},
		Attrs:        map[string]string{},
		NodeDefaults: map[string]string{},
		EdgeDefaults: map[string]string{},
		Subgraphs:    make([]*Subgraph, 0),
	}

	child := &Graph{
		Name: "child",
		Nodes: map[string]*Node{
			"s": {ID: "s", Attrs: map[string]string{"shape": "Mdiamond"}},
			"e": {ID: "e", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*Edge{
			{From: "s", To: "e", Attrs: map[string]string{}},
		},
		Attrs:        map[string]string{},
		NodeDefaults: map[string]string{},
		EdgeDefaults: map[string]string{},
		Subgraphs:    make([]*Subgraph, 0),
	}

	result, err := ComposeGraphs(parent, child, "manager", "m")
	if err != nil {
		t.Fatalf("ComposeGraphs returned error: %v", err)
	}

	// Both incoming edges should be reconnected to child start
	directFound := false
	viaBFound := false
	for _, e := range result.Edges {
		if e.From == "A" && e.To == "m.s" && e.Attrs["label"] == "direct" {
			directFound = true
		}
		if e.From == "B" && e.To == "m.s" && e.Attrs["label"] == "via B" {
			viaBFound = true
		}
	}
	if !directFound {
		t.Error("expected edge A -> m.s with label 'direct'")
	}
	if !viaBFound {
		t.Error("expected edge B -> m.s with label 'via B'")
	}
}
