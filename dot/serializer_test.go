// ABOUTME: Tests for the DOT serializer that converts Graph ASTs back to DOT source strings.
// ABOUTME: Covers Serialize, ApplyColorCoding, quoteValue, sortedKeys, and round-trip scenarios.
package dot

import (
	"strings"
	"testing"
)

// --- quoteValue tests ---

func TestQuoteValue(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty string", "", `""`},
		{"simple identifier", "box", "box"},
		{"lowercase with underscore", "my_shape", "my_shape"},
		{"value with spaces", "My Node", `"My Node"`},
		{"value with uppercase", "Mdiamond", `"Mdiamond"`},
		{"value with special char hash", "#ADD8E6", `"#ADD8E6"`},
		{"value with special char slash", "path/to", `"path/to"`},
		{"numeric value", "42", "42"},
		{"float value", "3.14", "3.14"},
		{"negative number", "-1", "-1"},
		{"value with comma", "a,b", `"a,b"`},
		{"value with equals", "a=b", `"a=b"`},
		{"value with dot only", "0.75", "0.75"},
		{"single lowercase word", "filled", "filled"},
		{"already safe identifier with digits", "node1", "node1"},
		{"value with embedded quote", `say "hi"`, `"say \"hi\""`},
		{"value with backslash", `path\to`, `"path\\to"`},
		{"value with semicolon", "a;b", `"a;b"`},
		{"boolean true", "true", "true"},
		{"boolean false", "false", "false"},
		{"value with newline", "line1\nline2", `"line1\nline2"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := quoteValue(tt.in)
			if got != tt.want {
				t.Errorf("quoteValue(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// --- sortedKeys tests ---

func TestSortedKeys(t *testing.T) {
	tests := []struct {
		name string
		m    map[string]string
		want []string
	}{
		{"nil map", nil, []string{}},
		{"empty map", map[string]string{}, []string{}},
		{"single key", map[string]string{"a": "1"}, []string{"a"}},
		{
			"multiple keys sorted",
			map[string]string{"zebra": "z", "alpha": "a", "mid": "m"},
			[]string{"alpha", "mid", "zebra"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sortedKeys(tt.m)
			if len(got) != len(tt.want) {
				t.Fatalf("sortedKeys returned %d keys, want %d", len(got), len(tt.want))
			}
			for i, k := range got {
				if k != tt.want[i] {
					t.Errorf("sortedKeys[%d] = %q, want %q", i, k, tt.want[i])
				}
			}
		})
	}
}

// --- Serialize tests ---

func TestSerializeEmptyGraph(t *testing.T) {
	g := &Graph{Name: "empty"}
	got := Serialize(g)

	if !strings.Contains(got, "digraph empty {") {
		t.Errorf("expected digraph header, got:\n%s", got)
	}
	if !strings.Contains(got, "}") {
		t.Errorf("expected closing brace, got:\n%s", got)
	}
}

func TestSerializeGraphWithAttributes(t *testing.T) {
	g := &Graph{
		Name:  "pipeline",
		Attrs: map[string]string{"goal": "Run tests", "rankdir": "LR"},
	}
	got := Serialize(g)

	if !strings.Contains(got, "digraph pipeline {") {
		t.Errorf("expected digraph header, got:\n%s", got)
	}
	if !strings.Contains(got, "graph [") {
		t.Errorf("expected graph attributes block, got:\n%s", got)
	}
	if !strings.Contains(got, `goal="Run tests"`) {
		t.Errorf("expected goal attribute with quotes, got:\n%s", got)
	}
	if !strings.Contains(got, "rankdir=") {
		t.Errorf("expected rankdir attribute, got:\n%s", got)
	}
}

func TestSerializeNodeDefaults(t *testing.T) {
	g := &Graph{
		Name:         "test",
		NodeDefaults: map[string]string{"shape": "box", "style": "filled"},
	}
	got := Serialize(g)

	if !strings.Contains(got, "node [") {
		t.Errorf("expected node defaults block, got:\n%s", got)
	}
	if !strings.Contains(got, "shape=box") {
		t.Errorf("expected shape=box in node defaults, got:\n%s", got)
	}
	if !strings.Contains(got, "style=filled") {
		t.Errorf("expected style=filled in node defaults, got:\n%s", got)
	}
}

func TestSerializeEdgeDefaults(t *testing.T) {
	g := &Graph{
		Name:         "test",
		EdgeDefaults: map[string]string{"color": "gray"},
	}
	got := Serialize(g)

	if !strings.Contains(got, "edge [") {
		t.Errorf("expected edge defaults block, got:\n%s", got)
	}
	if !strings.Contains(got, "color=gray") {
		t.Errorf("expected color=gray in edge defaults, got:\n%s", got)
	}
}

func TestSerializeNodesWithAttributes(t *testing.T) {
	g := &Graph{
		Name: "test",
		Nodes: map[string]*Node{
			"beta":  {ID: "beta", Attrs: map[string]string{"shape": "box", "label": "Beta Node"}},
			"alpha": {ID: "alpha", Attrs: map[string]string{"shape": "Mdiamond"}},
		},
	}
	got := Serialize(g)

	// alpha should appear before beta (sorted by ID)
	alphaIdx := strings.Index(got, "alpha")
	betaIdx := strings.Index(got, "beta")
	if alphaIdx < 0 || betaIdx < 0 {
		t.Fatalf("expected both alpha and beta in output, got:\n%s", got)
	}
	if alphaIdx > betaIdx {
		t.Errorf("expected alpha before beta (sorted), got alpha at %d, beta at %d", alphaIdx, betaIdx)
	}

	// Check node attribute formatting
	if !strings.Contains(got, `shape="Mdiamond"`) {
		t.Errorf("expected Mdiamond to be quoted (uppercase), got:\n%s", got)
	}
	if !strings.Contains(got, `label="Beta Node"`) {
		t.Errorf("expected label to be quoted (spaces), got:\n%s", got)
	}
}

func TestSerializeNodeWithNoAttributes(t *testing.T) {
	g := &Graph{
		Name: "test",
		Nodes: map[string]*Node{
			"solo": {ID: "solo", Attrs: map[string]string{}},
		},
	}
	got := Serialize(g)

	// Node with no attributes should still appear, just without brackets
	if !strings.Contains(got, "solo") {
		t.Errorf("expected node 'solo' in output, got:\n%s", got)
	}
	// It should NOT have empty brackets
	if strings.Contains(got, "solo []") {
		t.Errorf("node with no attrs should not have empty brackets, got:\n%s", got)
	}
}

func TestSerializeEdges(t *testing.T) {
	g := &Graph{
		Name: "test",
		Nodes: map[string]*Node{
			"a": {ID: "a"},
			"b": {ID: "b"},
			"c": {ID: "c"},
		},
		Edges: []*Edge{
			{From: "a", To: "b"},
			{From: "b", To: "c", Attrs: map[string]string{"label": "next"}},
		},
	}
	got := Serialize(g)

	if !strings.Contains(got, "a -> b") {
		t.Errorf("expected edge 'a -> b', got:\n%s", got)
	}
	if !strings.Contains(got, "b -> c") {
		t.Errorf("expected edge 'b -> c', got:\n%s", got)
	}
	if !strings.Contains(got, "label=next") {
		t.Errorf("expected edge attribute label=next, got:\n%s", got)
	}
}

func TestSerializeEdgeWithNoAttributes(t *testing.T) {
	g := &Graph{
		Name: "test",
		Edges: []*Edge{
			{From: "a", To: "b"},
		},
	}
	got := Serialize(g)

	if !strings.Contains(got, "a -> b") {
		t.Errorf("expected 'a -> b', got:\n%s", got)
	}
	// Should not have empty brackets on bare edge
	if strings.Contains(got, "a -> b []") {
		t.Errorf("edge with no attrs should not have empty brackets, got:\n%s", got)
	}
}

func TestSerializeSubgraphs(t *testing.T) {
	g := &Graph{
		Name: "test",
		Nodes: map[string]*Node{
			"a": {ID: "a", Attrs: map[string]string{"shape": "box"}},
			"b": {ID: "b", Attrs: map[string]string{"shape": "box"}},
		},
		Subgraphs: []*Subgraph{
			{
				ID:           "cluster_0",
				Name:         "setup",
				Attrs:        map[string]string{"label": "Setup Phase", "style": "dashed"},
				NodeIDs:      []string{"a", "b"},
				NodeDefaults: map[string]string{"color": "blue"},
			},
		},
	}
	got := Serialize(g)

	if !strings.Contains(got, "subgraph cluster_0") {
		t.Errorf("expected subgraph cluster_0, got:\n%s", got)
	}
	if !strings.Contains(got, `label="Setup Phase"`) {
		t.Errorf("expected subgraph label attribute, got:\n%s", got)
	}
	if !strings.Contains(got, "style=dashed") {
		t.Errorf("expected subgraph style attribute, got:\n%s", got)
	}
	if !strings.Contains(got, "color=blue") {
		t.Errorf("expected subgraph node defaults, got:\n%s", got)
	}
}

func TestSerializeRoundTrip(t *testing.T) {
	// Build a known graph and verify the output matches expected DOT format
	g := &Graph{
		Name:         "pipeline",
		Attrs:        map[string]string{"goal": "Run tests"},
		NodeDefaults: map[string]string{"style": "filled"},
		Nodes: map[string]*Node{
			"start":   {ID: "start", Attrs: map[string]string{"shape": "Mdiamond", "label": "Start"}},
			"process": {ID: "process", Attrs: map[string]string{"shape": "box"}},
			"end":     {ID: "end", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*Edge{
			{From: "start", To: "process"},
			{From: "process", To: "end", Attrs: map[string]string{"label": "done"}},
		},
	}

	got := Serialize(g)

	// Verify structural elements
	checks := []struct {
		desc string
		want string
	}{
		{"digraph header", "digraph pipeline {"},
		{"graph attrs", `goal="Run tests"`},
		{"node defaults", "node ["},
		{"start node", "start ["},
		{"end node", "end ["},
		{"process node", "process ["},
		{"first edge", "start -> process"},
		{"second edge", "process -> end"},
		{"edge attr", "label=done"},
		{"closing brace", "}"},
	}
	for _, c := range checks {
		if !strings.Contains(got, c.want) {
			t.Errorf("round-trip missing %s (%q) in:\n%s", c.desc, c.want, got)
		}
	}
}

func TestSerializeAttributesSorted(t *testing.T) {
	// Verify that attributes within a node's brackets are sorted by key
	g := &Graph{
		Name: "test",
		Nodes: map[string]*Node{
			"n": {ID: "n", Attrs: map[string]string{
				"zebra": "z",
				"alpha": "a",
				"mid":   "m",
			}},
		},
	}
	got := Serialize(g)

	// Find the attribute line for node n
	lines := strings.Split(got, "\n")
	var attrLine string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "n [") {
			attrLine = trimmed
			break
		}
	}
	if attrLine == "" {
		t.Fatalf("could not find node 'n' attribute line in:\n%s", got)
	}

	// alpha should appear before mid, mid before zebra
	alphaIdx := strings.Index(attrLine, "alpha=")
	midIdx := strings.Index(attrLine, "mid=")
	zebraIdx := strings.Index(attrLine, "zebra=")
	if alphaIdx < 0 || midIdx < 0 || zebraIdx < 0 {
		t.Fatalf("expected all three attrs in line %q", attrLine)
	}
	if !(alphaIdx < midIdx && midIdx < zebraIdx) {
		t.Errorf("expected attrs sorted (alpha < mid < zebra), got positions %d, %d, %d in %q",
			alphaIdx, midIdx, zebraIdx, attrLine)
	}
}

func TestSerializeGraphNameNeedingQuotes(t *testing.T) {
	g := &Graph{Name: "My Pipeline"}
	got := Serialize(g)

	if !strings.Contains(got, `digraph "My Pipeline" {`) {
		t.Errorf("expected graph name to be quoted, got:\n%s", got)
	}
}

// --- ApplyColorCoding tests ---

func TestApplyColorCodingStartNode(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
		},
	}
	ApplyColorCoding(g)

	if g.Nodes["start"].Attrs["fillcolor"] != "#90EE90" {
		t.Errorf("start node fillcolor = %q, want #90EE90", g.Nodes["start"].Attrs["fillcolor"])
	}
	if g.Nodes["start"].Attrs["style"] != "filled" {
		t.Errorf("start node style = %q, want filled", g.Nodes["start"].Attrs["style"])
	}
}

func TestApplyColorCodingExitNode(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"end": {ID: "end", Attrs: map[string]string{"shape": "Msquare"}},
		},
	}
	ApplyColorCoding(g)

	if g.Nodes["end"].Attrs["fillcolor"] != "#FFB6C1" {
		t.Errorf("exit node fillcolor = %q, want #FFB6C1", g.Nodes["end"].Attrs["fillcolor"])
	}
	if g.Nodes["end"].Attrs["style"] != "filled" {
		t.Errorf("exit node style = %q, want filled", g.Nodes["end"].Attrs["style"])
	}
}

func TestApplyColorCodingBoxNode(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"code": {ID: "code", Attrs: map[string]string{"shape": "box"}},
		},
	}
	ApplyColorCoding(g)

	if g.Nodes["code"].Attrs["fillcolor"] != "#ADD8E6" {
		t.Errorf("box node fillcolor = %q, want #ADD8E6", g.Nodes["code"].Attrs["fillcolor"])
	}
}

func TestApplyColorCodingDiamondNode(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"cond": {ID: "cond", Attrs: map[string]string{"shape": "diamond"}},
		},
	}
	ApplyColorCoding(g)

	if g.Nodes["cond"].Attrs["fillcolor"] != "#FFFFE0" {
		t.Errorf("diamond node fillcolor = %q, want #FFFFE0", g.Nodes["cond"].Attrs["fillcolor"])
	}
}

func TestApplyColorCodingHexagonNode(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"human": {ID: "human", Attrs: map[string]string{"shape": "hexagon"}},
		},
	}
	ApplyColorCoding(g)

	if g.Nodes["human"].Attrs["fillcolor"] != "#DDA0DD" {
		t.Errorf("hexagon node fillcolor = %q, want #DDA0DD", g.Nodes["human"].Attrs["fillcolor"])
	}
}

func TestApplyColorCodingParallelogramNode(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"tool": {ID: "tool", Attrs: map[string]string{"shape": "parallelogram"}},
		},
	}
	ApplyColorCoding(g)

	if g.Nodes["tool"].Attrs["fillcolor"] != "#FFA500" {
		t.Errorf("parallelogram node fillcolor = %q, want #FFA500", g.Nodes["tool"].Attrs["fillcolor"])
	}
}

func TestApplyColorCodingAllShapes(t *testing.T) {
	tests := []struct {
		shape     string
		wantColor string
	}{
		{"Mdiamond", "#90EE90"},
		{"Msquare", "#FFB6C1"},
		{"box", "#ADD8E6"},
		{"diamond", "#FFFFE0"},
		{"hexagon", "#DDA0DD"},
		{"parallelogram", "#FFA500"},
	}

	for _, tt := range tests {
		t.Run(tt.shape, func(t *testing.T) {
			g := &Graph{
				Nodes: map[string]*Node{
					"n": {ID: "n", Attrs: map[string]string{"shape": tt.shape}},
				},
			}
			ApplyColorCoding(g)

			if g.Nodes["n"].Attrs["fillcolor"] != tt.wantColor {
				t.Errorf("shape %s: fillcolor = %q, want %q", tt.shape, g.Nodes["n"].Attrs["fillcolor"], tt.wantColor)
			}
			if g.Nodes["n"].Attrs["style"] != "filled" {
				t.Errorf("shape %s: style = %q, want filled", tt.shape, g.Nodes["n"].Attrs["style"])
			}
		})
	}
}

func TestApplyColorCodingUnknownShape(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"unknown": {ID: "unknown", Attrs: map[string]string{"shape": "ellipse"}},
		},
	}
	ApplyColorCoding(g)

	// Unknown shapes should not get a fillcolor
	if _, ok := g.Nodes["unknown"].Attrs["fillcolor"]; ok {
		t.Errorf("unknown shape should not get fillcolor, got %q", g.Nodes["unknown"].Attrs["fillcolor"])
	}
}

func TestApplyColorCodingSuccessEdge(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"a": {ID: "a"},
			"b": {ID: "b"},
		},
		Edges: []*Edge{
			{From: "a", To: "b", Attrs: map[string]string{"label": "success"}},
		},
	}
	ApplyColorCoding(g)

	if g.Edges[0].Attrs["color"] != "green" {
		t.Errorf("success edge color = %q, want green", g.Edges[0].Attrs["color"])
	}
}

func TestApplyColorCodingFailEdge(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"a": {ID: "a"},
			"b": {ID: "b"},
		},
		Edges: []*Edge{
			{From: "a", To: "b", Attrs: map[string]string{"label": "fail"}},
		},
	}
	ApplyColorCoding(g)

	if g.Edges[0].Attrs["color"] != "red" {
		t.Errorf("fail edge color = %q, want red", g.Edges[0].Attrs["color"])
	}
	if g.Edges[0].Attrs["style"] != "dashed" {
		t.Errorf("fail edge style = %q, want dashed", g.Edges[0].Attrs["style"])
	}
}

func TestApplyColorCodingEdgeConditionVariants(t *testing.T) {
	tests := []struct {
		name      string
		label     string
		wantColor string
		wantStyle string
	}{
		{"success", "success", "green", ""},
		{"Success uppercase", "Success", "green", ""},
		{"fail", "fail", "red", "dashed"},
		{"failure", "failure", "red", "dashed"},
		{"Failure uppercase", "Failure", "red", "dashed"},
		{"neutral label", "next", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &Graph{
				Nodes: map[string]*Node{
					"a": {ID: "a"},
					"b": {ID: "b"},
				},
				Edges: []*Edge{
					{From: "a", To: "b", Attrs: map[string]string{"label": tt.label}},
				},
			}
			ApplyColorCoding(g)

			gotColor := g.Edges[0].Attrs["color"]
			if gotColor != tt.wantColor {
				t.Errorf("edge label=%q: color = %q, want %q", tt.label, gotColor, tt.wantColor)
			}
			gotStyle := g.Edges[0].Attrs["style"]
			if gotStyle != tt.wantStyle {
				t.Errorf("edge label=%q: style = %q, want %q", tt.label, gotStyle, tt.wantStyle)
			}
		})
	}
}

func TestApplyColorCodingNilAttrs(t *testing.T) {
	// Nodes with nil Attrs should not panic
	g := &Graph{
		Nodes: map[string]*Node{
			"bare": {ID: "bare"},
		},
		Edges: []*Edge{
			{From: "bare", To: "bare"},
		},
	}
	ApplyColorCoding(g) // should not panic
}

func TestSerializeCompleteGraph(t *testing.T) {
	// A complete graph with all features to verify nothing is lost
	g := &Graph{
		Name:         "full_pipeline",
		Attrs:        map[string]string{"goal": "Full test", "rankdir": "LR"},
		NodeDefaults: map[string]string{"style": "filled"},
		EdgeDefaults: map[string]string{"fontsize": "10"},
		Nodes: map[string]*Node{
			"start":   {ID: "start", Attrs: map[string]string{"shape": "Mdiamond", "label": "Begin"}},
			"process": {ID: "process", Attrs: map[string]string{"shape": "box"}},
			"check":   {ID: "check", Attrs: map[string]string{"shape": "diamond"}},
			"end":     {ID: "end", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*Edge{
			{From: "start", To: "process"},
			{From: "process", To: "check"},
			{From: "check", To: "end", Attrs: map[string]string{"label": "success"}},
			{From: "check", To: "process", Attrs: map[string]string{"label": "fail"}},
		},
		Subgraphs: []*Subgraph{
			{
				ID:      "cluster_main",
				Name:    "main",
				Attrs:   map[string]string{"label": "Main Flow"},
				NodeIDs: []string{"process", "check"},
			},
		},
	}

	got := Serialize(g)

	checks := []string{
		"digraph full_pipeline {",
		"graph [",
		"node [",
		"edge [",
		"start [",
		"process [",
		"check [",
		"end [",
		"start -> process",
		"process -> check",
		"check -> end",
		"check -> process",
		"subgraph cluster_main {",
		`label="Main Flow"`,
		"}",
	}
	for _, want := range checks {
		if !strings.Contains(got, want) {
			t.Errorf("complete graph missing %q in:\n%s", want, got)
		}
	}
}
