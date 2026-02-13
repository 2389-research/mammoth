// ABOUTME: Tests for the render package covering DOT serialization, status overlay, and graphviz rendering.
// ABOUTME: Validates ToDOT, ToDOTWithStatus, and Render functions with real graph structures.
package render

import (
	"context"
	"strings"
	"testing"

	"github.com/2389-research/mammoth/attractor"
)

// buildTestGraph constructs a simple graph for testing DOT serialization.
func buildTestGraph() *attractor.Graph {
	return &attractor.Graph{
		Name: "test_pipeline",
		Nodes: map[string]*attractor.Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":  {ID: "work", Attrs: map[string]string{"shape": "box", "label": "Do Work"}},
			"done":  {ID: "done", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*attractor.Edge{
			{From: "start", To: "work", Attrs: map[string]string{}},
			{From: "work", To: "done", Attrs: map[string]string{"label": "complete"}},
		},
		Attrs:        map[string]string{"rankdir": "LR"},
		NodeDefaults: map[string]string{"fontname": "Helvetica"},
		EdgeDefaults: map[string]string{"color": "gray"},
	}
}

// buildMinimalGraph constructs a graph with no optional attributes.
func buildMinimalGraph() *attractor.Graph {
	return &attractor.Graph{
		Name: "minimal",
		Nodes: map[string]*attractor.Node{
			"a": {ID: "a", Attrs: map[string]string{}},
			"b": {ID: "b", Attrs: map[string]string{}},
		},
		Edges: []*attractor.Edge{
			{From: "a", To: "b", Attrs: map[string]string{}},
		},
		Attrs:        map[string]string{},
		NodeDefaults: map[string]string{},
		EdgeDefaults: map[string]string{},
	}
}

func TestToDOT_ProducesValidDigraph(t *testing.T) {
	g := buildTestGraph()
	dot := ToDOT(g)

	if !strings.Contains(dot, "digraph test_pipeline {") {
		t.Errorf("expected digraph declaration, got:\n%s", dot)
	}
	if !strings.Contains(dot, "}") {
		t.Error("expected closing brace")
	}
}

func TestToDOT_IncludesGraphAttributes(t *testing.T) {
	g := buildTestGraph()
	dot := ToDOT(g)

	if !strings.Contains(dot, `rankdir="LR"`) {
		t.Errorf("expected graph attribute rankdir=LR in output:\n%s", dot)
	}
}

func TestToDOT_IncludesNodeDefaults(t *testing.T) {
	g := buildTestGraph()
	dot := ToDOT(g)

	if !strings.Contains(dot, `node [fontname="Helvetica"]`) {
		t.Errorf("expected node defaults in output:\n%s", dot)
	}
}

func TestToDOT_IncludesEdgeDefaults(t *testing.T) {
	g := buildTestGraph()
	dot := ToDOT(g)

	if !strings.Contains(dot, `edge [color="gray"]`) {
		t.Errorf("expected edge defaults in output:\n%s", dot)
	}
}

func TestToDOT_IncludesNodes(t *testing.T) {
	g := buildTestGraph()
	dot := ToDOT(g)

	if !strings.Contains(dot, `done [shape="Msquare"]`) {
		t.Errorf("expected done node with Msquare shape in output:\n%s", dot)
	}
	if !strings.Contains(dot, `start [shape="Mdiamond"]`) {
		t.Errorf("expected start node with Mdiamond shape in output:\n%s", dot)
	}
	if !strings.Contains(dot, `label="Do Work"`) {
		t.Errorf("expected work node with label in output:\n%s", dot)
	}
}

func TestToDOT_IncludesEdges(t *testing.T) {
	g := buildTestGraph()
	dot := ToDOT(g)

	if !strings.Contains(dot, "start -> work") {
		t.Errorf("expected edge start -> work in output:\n%s", dot)
	}
	if !strings.Contains(dot, `work -> done [label="complete"]`) {
		t.Errorf("expected edge work -> done with label in output:\n%s", dot)
	}
}

func TestToDOT_MinimalGraph(t *testing.T) {
	g := buildMinimalGraph()
	dot := ToDOT(g)

	if !strings.Contains(dot, "digraph minimal {") {
		t.Errorf("expected digraph declaration, got:\n%s", dot)
	}
	if !strings.Contains(dot, "a -> b") {
		t.Errorf("expected edge a -> b in output:\n%s", dot)
	}
	// Minimal graph should not include defaults blocks
	if strings.Contains(dot, "node [") {
		t.Errorf("expected no node defaults in minimal graph, got:\n%s", dot)
	}
	if strings.Contains(dot, "edge [") {
		t.Errorf("expected no edge defaults in minimal graph, got:\n%s", dot)
	}
}

func TestToDOT_NilGraph(t *testing.T) {
	dot := ToDOT(nil)
	if dot != "" {
		t.Errorf("expected empty string for nil graph, got:\n%s", dot)
	}
}

func TestToDOT_EmptyNodesGraph(t *testing.T) {
	g := &attractor.Graph{
		Name:  "empty",
		Nodes: map[string]*attractor.Node{},
	}
	dot := ToDOT(g)

	if !strings.Contains(dot, "digraph empty {") {
		t.Errorf("expected digraph declaration, got:\n%s", dot)
	}
}

func TestToDOT_DeterministicNodeOrder(t *testing.T) {
	g := buildTestGraph()
	// Run multiple times and verify output is consistent
	first := ToDOT(g)
	for i := 0; i < 10; i++ {
		result := ToDOT(g)
		if result != first {
			t.Fatalf("non-deterministic output on iteration %d:\nfirst:\n%s\ngot:\n%s", i, first, result)
		}
	}
}

func TestToDOT_RoundTrip(t *testing.T) {
	g := buildTestGraph()
	dot := ToDOT(g)

	// The output should be parseable by the attractor parser
	parsed, err := attractor.Parse(dot)
	if err != nil {
		t.Fatalf("failed to re-parse generated DOT: %v\nDOT:\n%s", err, dot)
	}

	if parsed.Name != g.Name {
		t.Errorf("expected graph name %q, got %q", g.Name, parsed.Name)
	}
	if len(parsed.Nodes) != len(g.Nodes) {
		t.Errorf("expected %d nodes, got %d", len(g.Nodes), len(parsed.Nodes))
	}
	if len(parsed.Edges) != len(g.Edges) {
		t.Errorf("expected %d edges, got %d", len(g.Edges), len(parsed.Edges))
	}
}

func TestToDOTWithStatus_ColorsSuccessGreen(t *testing.T) {
	g := buildTestGraph()
	outcomes := map[string]*attractor.Outcome{
		"work": {Status: attractor.StatusSuccess},
	}
	dot := ToDOTWithStatus(g, outcomes)

	if !strings.Contains(dot, `fillcolor="`) {
		t.Errorf("expected fillcolor attribute in status output:\n%s", dot)
	}
	if !strings.Contains(dot, `style="filled"`) {
		t.Errorf("expected style=filled in status output:\n%s", dot)
	}
	// Check for green color on the work node line
	lines := strings.Split(dot, "\n")
	found := false
	for _, line := range lines {
		if strings.Contains(line, `"work"`) || strings.Contains(line, "work [") {
			if strings.Contains(line, StatusColorSuccess) {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected green fill for successful work node in output:\n%s", dot)
	}
}

func TestToDOTWithStatus_ColorsFailedRed(t *testing.T) {
	g := buildTestGraph()
	outcomes := map[string]*attractor.Outcome{
		"work": {Status: attractor.StatusFail},
	}
	dot := ToDOTWithStatus(g, outcomes)

	lines := strings.Split(dot, "\n")
	found := false
	for _, line := range lines {
		if strings.Contains(line, `"work"`) || strings.Contains(line, "work [") {
			if strings.Contains(line, StatusColorFailed) {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected red fill for failed work node in output:\n%s", dot)
	}
}

func TestToDOTWithStatus_ColorsPendingGray(t *testing.T) {
	g := buildTestGraph()
	// Empty outcomes map means all nodes are pending
	outcomes := map[string]*attractor.Outcome{}
	dot := ToDOTWithStatus(g, outcomes)

	lines := strings.Split(dot, "\n")
	found := false
	for _, line := range lines {
		if strings.Contains(line, "work [") || strings.Contains(line, `"work"`) {
			if strings.Contains(line, StatusColorPending) {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected gray fill for pending work node in output:\n%s", dot)
	}
}

func TestToDOTWithStatus_ColorsRetryYellow(t *testing.T) {
	g := buildTestGraph()
	outcomes := map[string]*attractor.Outcome{
		"work": {Status: attractor.StatusRetry},
	}
	dot := ToDOTWithStatus(g, outcomes)

	lines := strings.Split(dot, "\n")
	found := false
	for _, line := range lines {
		if strings.Contains(line, "work [") || strings.Contains(line, `"work"`) {
			if strings.Contains(line, StatusColorRunning) {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected yellow fill for retrying work node in output:\n%s", dot)
	}
}

func TestToDOTWithStatus_NilOutcomes(t *testing.T) {
	g := buildTestGraph()
	dot := ToDOTWithStatus(g, nil)

	// Should still produce valid DOT, all nodes pending/gray
	if !strings.Contains(dot, "digraph test_pipeline {") {
		t.Errorf("expected valid digraph output:\n%s", dot)
	}
}

func TestToDOTWithStatus_PreservesOriginalAttributes(t *testing.T) {
	g := buildTestGraph()
	outcomes := map[string]*attractor.Outcome{
		"work": {Status: attractor.StatusSuccess},
	}
	dot := ToDOTWithStatus(g, outcomes)

	// Should still have the original shape attributes
	if !strings.Contains(dot, `shape="Mdiamond"`) {
		t.Errorf("expected original shape attributes preserved:\n%s", dot)
	}
}

func TestRender_DOTFormat(t *testing.T) {
	g := buildTestGraph()
	data, err := Render(context.Background(), g, "dot")
	if err != nil {
		t.Fatalf("Render(dot) failed: %v", err)
	}

	dot := string(data)
	if !strings.Contains(dot, "digraph test_pipeline {") {
		t.Errorf("expected digraph output from dot format, got:\n%s", dot)
	}
}

func TestRender_InvalidFormat(t *testing.T) {
	g := buildTestGraph()
	_, err := Render(context.Background(), g, "gif")
	if err == nil {
		t.Error("expected error for invalid format")
	}
	if !strings.Contains(err.Error(), "unsupported format") {
		t.Errorf("expected 'unsupported format' error, got: %v", err)
	}
}

func TestRender_NilGraph(t *testing.T) {
	_, err := Render(context.Background(), nil, "dot")
	if err == nil {
		t.Error("expected error for nil graph")
	}
}

func TestRender_SVGFormat(t *testing.T) {
	if !graphvizAvailable() {
		t.Skip("graphviz not installed, skipping SVG render test")
	}

	g := buildTestGraph()
	data, err := Render(context.Background(), g, "svg")
	if err != nil {
		t.Fatalf("Render(svg) failed: %v", err)
	}

	svg := string(data)
	if !strings.Contains(svg, "<svg") {
		t.Errorf("expected SVG output, got:\n%s", svg[:min(len(svg), 200)])
	}
}

func TestRender_PNGFormat(t *testing.T) {
	if !graphvizAvailable() {
		t.Skip("graphviz not installed, skipping PNG render test")
	}

	g := buildTestGraph()
	data, err := Render(context.Background(), g, "png")
	if err != nil {
		t.Fatalf("Render(png) failed: %v", err)
	}

	// PNG files start with the PNG signature bytes
	if len(data) < 8 {
		t.Fatal("PNG output too short")
	}
	pngSig := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	for i, b := range pngSig {
		if data[i] != b {
			t.Fatalf("expected PNG signature at byte %d: got %x, want %x", i, data[i], b)
		}
	}
}

func TestRender_SVGContextCancellation(t *testing.T) {
	if !graphvizAvailable() {
		t.Skip("graphviz not installed, skipping context cancellation test")
	}

	g := buildTestGraph()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := Render(ctx, g, "svg")
	if err == nil {
		t.Error("expected error when context is cancelled")
	}
}

func TestGraphvizAvailable_ReturnsBoolean(t *testing.T) {
	// This function should return a boolean without panicking
	result := graphvizAvailable()
	// We can't assert the value because it depends on the environment,
	// but it should be a valid bool
	_ = result
}

func TestRender_EmptyGraphDOT(t *testing.T) {
	g := &attractor.Graph{
		Name:  "empty",
		Nodes: map[string]*attractor.Node{},
		Edges: []*attractor.Edge{},
		Attrs: map[string]string{},
	}
	data, err := Render(context.Background(), g, "dot")
	if err != nil {
		t.Fatalf("Render(dot) for empty graph failed: %v", err)
	}
	if !strings.Contains(string(data), "digraph empty {") {
		t.Errorf("expected digraph output for empty graph, got:\n%s", string(data))
	}
}

func TestToDOT_SubgraphHandling(t *testing.T) {
	g := &attractor.Graph{
		Name: "with_subgraph",
		Nodes: map[string]*attractor.Node{
			"a": {ID: "a", Attrs: map[string]string{}},
			"b": {ID: "b", Attrs: map[string]string{}},
		},
		Edges: []*attractor.Edge{
			{From: "a", To: "b", Attrs: map[string]string{}},
		},
		Subgraphs: []*attractor.Subgraph{
			{
				Name:         "cluster_0",
				Nodes:        []string{"a"},
				NodeDefaults: map[string]string{"style": "filled"},
				Attrs:        map[string]string{"label": "Group A"},
			},
		},
		Attrs: map[string]string{},
	}
	dot := ToDOT(g)

	if !strings.Contains(dot, "subgraph cluster_0") {
		t.Errorf("expected subgraph declaration in output:\n%s", dot)
	}
	if !strings.Contains(dot, `label="Group A"`) {
		t.Errorf("expected subgraph label in output:\n%s", dot)
	}
}

func TestToDOT_EdgeWithNoAttrs(t *testing.T) {
	g := buildMinimalGraph()
	dot := ToDOT(g)

	// Edge without attributes should not have brackets
	if strings.Contains(dot, "a -> b [") {
		t.Errorf("expected edge without attr brackets for no-attr edge:\n%s", dot)
	}
}

func TestToDOT_NodeWithNoAttrs(t *testing.T) {
	g := buildMinimalGraph()
	dot := ToDOT(g)

	// Nodes with no attributes - just node ID
	lines := strings.Split(dot, "\n")
	foundBareNode := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// A bare node is just its ID followed by a semicolon, no brackets
		if trimmed == "a;" || trimmed == `"a";` {
			foundBareNode = true
		}
	}
	if !foundBareNode {
		t.Errorf("expected bare node declaration (no brackets) for node without attrs:\n%s", dot)
	}
}

func TestToDOTWithStatus_PartialSuccess(t *testing.T) {
	g := buildTestGraph()
	outcomes := map[string]*attractor.Outcome{
		"work": {Status: attractor.StatusPartialSuccess},
	}
	dot := ToDOTWithStatus(g, outcomes)

	// Partial success should use the success color
	lines := strings.Split(dot, "\n")
	found := false
	for _, line := range lines {
		if strings.Contains(line, "work") && strings.Contains(line, StatusColorSuccess) {
			found = true
		}
	}
	if !found {
		t.Errorf("expected green fill for partial_success node in output:\n%s", dot)
	}
}

func TestToDOTWithStatus_Skipped(t *testing.T) {
	g := buildTestGraph()
	outcomes := map[string]*attractor.Outcome{
		"work": {Status: attractor.StatusSkipped},
	}
	dot := ToDOTWithStatus(g, outcomes)

	// Skipped should use the pending/gray color
	lines := strings.Split(dot, "\n")
	found := false
	for _, line := range lines {
		if strings.Contains(line, "work") && strings.Contains(line, StatusColorPending) {
			found = true
		}
	}
	if !found {
		t.Errorf("expected gray fill for skipped node in output:\n%s", dot)
	}
}

// --- RenderDOTSource tests ---

func TestRenderDOTSource_DOTFormat(t *testing.T) {
	dotText := "digraph test { a -> b }"
	data, err := RenderDOTSource(context.Background(), dotText, "dot")
	if err != nil {
		t.Fatalf("RenderDOTSource(dot) failed: %v", err)
	}
	if string(data) != dotText {
		t.Errorf("expected DOT text back as-is, got: %s", string(data))
	}
}

func TestRenderDOTSource_EmptyText(t *testing.T) {
	_, err := RenderDOTSource(context.Background(), "", "dot")
	if err == nil {
		t.Error("expected error for empty DOT text")
	}
}

func TestRenderDOTSource_InvalidFormat(t *testing.T) {
	_, err := RenderDOTSource(context.Background(), "digraph t { a -> b }", "gif")
	if err == nil {
		t.Error("expected error for invalid format")
	}
}

func TestRenderDOTSource_SVGFormat(t *testing.T) {
	if !graphvizAvailable() {
		t.Skip("graphviz not installed")
	}

	dotText := "digraph test { a -> b }"
	data, err := RenderDOTSource(context.Background(), dotText, "svg")
	if err != nil {
		t.Fatalf("RenderDOTSource(svg) failed: %v", err)
	}
	if !strings.Contains(string(data), "<svg") {
		t.Errorf("expected SVG output, got: %s", string(data)[:min(len(string(data)), 200)])
	}
}

func TestRenderDOTSource_PNGFormat(t *testing.T) {
	if !graphvizAvailable() {
		t.Skip("graphviz not installed")
	}

	dotText := "digraph test { a -> b }"
	data, err := RenderDOTSource(context.Background(), dotText, "png")
	if err != nil {
		t.Fatalf("RenderDOTSource(png) failed: %v", err)
	}
	if len(data) < 8 {
		t.Fatal("PNG output too short")
	}
	// PNG magic bytes
	if data[0] != 0x89 || data[1] != 0x50 {
		t.Fatalf("expected PNG signature, got %x %x", data[0], data[1])
	}
}

func TestGraphvizAvailable_Public(t *testing.T) {
	// Verify the public function works
	result := GraphvizAvailable()
	_ = result
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
