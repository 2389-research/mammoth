// ABOUTME: Tests for the parse command, verifying DOT-to-conformance JSON translation.
// ABOUTME: Covers basic graphs, chained edges, empty attributes, weight parsing, and error handling.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/2389-research/mammoth/dot"
)

func TestTranslateGraphToParseOutput_BasicGraph(t *testing.T) {
	source := `digraph pipeline {
		start [shape=Mdiamond]
		build [shape=box, label="Build Code", prompt="write code"]
		done [shape=Msquare]
		start -> build
		build -> done
	}`
	g, err := dot.Parse(source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	output := translateGraphToParseOutput(g)

	// Nodes should be sorted by ID
	if len(output.Nodes) != 3 {
		t.Fatalf("got %d nodes, want 3", len(output.Nodes))
	}
	// build, done, start (alphabetical)
	if output.Nodes[0].ID != "build" {
		t.Errorf("first node = %q, want build", output.Nodes[0].ID)
	}
	if output.Nodes[1].ID != "done" {
		t.Errorf("second node = %q, want done", output.Nodes[1].ID)
	}
	if output.Nodes[2].ID != "start" {
		t.Errorf("third node = %q, want start", output.Nodes[2].ID)
	}

	// Check shape and label extraction
	buildNode := output.Nodes[0]
	if buildNode.Shape != "box" {
		t.Errorf("build.shape = %q, want box", buildNode.Shape)
	}
	if buildNode.Label != "Build Code" {
		t.Errorf("build.label = %q, want 'Build Code'", buildNode.Label)
	}
	if buildNode.Attributes["prompt"] != "write code" {
		t.Errorf("build.attributes.prompt = %q, want 'write code'", buildNode.Attributes["prompt"])
	}
	// Shape and label should NOT be in attributes map
	if _, ok := buildNode.Attributes["shape"]; ok {
		t.Error("shape should not be in attributes map")
	}
	if _, ok := buildNode.Attributes["label"]; ok {
		t.Error("label should not be in attributes map")
	}

	// Check edges
	if len(output.Edges) != 2 {
		t.Fatalf("got %d edges, want 2", len(output.Edges))
	}
	if output.Edges[0].From != "start" || output.Edges[0].To != "build" {
		t.Errorf("edge[0] = %s->%s, want start->build", output.Edges[0].From, output.Edges[0].To)
	}
	if output.Edges[1].From != "build" || output.Edges[1].To != "done" {
		t.Errorf("edge[1] = %s->%s, want build->done", output.Edges[1].From, output.Edges[1].To)
	}
}

func TestTranslateGraphToParseOutput_ChainedEdges(t *testing.T) {
	source := `digraph pipeline {
		a [shape=Mdiamond]
		b [shape=box]
		c [shape=box]
		d [shape=Msquare]
		a -> b -> c -> d
	}`
	g, err := dot.Parse(source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	output := translateGraphToParseOutput(g)

	if len(output.Edges) != 3 {
		t.Fatalf("got %d edges, want 3", len(output.Edges))
	}
	// Chained edges should expand to a->b, b->c, c->d
	wantEdges := [][2]string{{"a", "b"}, {"b", "c"}, {"c", "d"}}
	for i, want := range wantEdges {
		if output.Edges[i].From != want[0] || output.Edges[i].To != want[1] {
			t.Errorf("edge[%d] = %s->%s, want %s->%s",
				i, output.Edges[i].From, output.Edges[i].To, want[0], want[1])
		}
	}
}

func TestTranslateGraphToParseOutput_EmptyAttributes(t *testing.T) {
	source := `digraph pipeline {
		solo [shape=Mdiamond]
	}`
	g, err := dot.Parse(source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	output := translateGraphToParseOutput(g)

	if len(output.Nodes) != 1 {
		t.Fatalf("got %d nodes, want 1", len(output.Nodes))
	}
	if output.Nodes[0].Attributes == nil {
		t.Error("attributes should not be nil, should be empty map")
	}
	if len(output.Edges) != 0 {
		t.Errorf("got %d edges, want 0", len(output.Edges))
	}
}

func TestTranslateGraphToParseOutput_WeightParsing(t *testing.T) {
	source := `digraph pipeline {
		a [shape=Mdiamond]
		b [shape=Msquare]
		a -> b [weight="5", label="main"]
	}`
	g, err := dot.Parse(source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	output := translateGraphToParseOutput(g)

	if len(output.Edges) != 1 {
		t.Fatalf("got %d edges, want 1", len(output.Edges))
	}
	if output.Edges[0].Weight != 5 {
		t.Errorf("weight = %d, want 5", output.Edges[0].Weight)
	}
	if output.Edges[0].Label != "main" {
		t.Errorf("label = %q, want 'main'", output.Edges[0].Label)
	}
}

func TestTranslateGraphToParseOutput_InvalidWeight(t *testing.T) {
	source := `digraph pipeline {
		a [shape=Mdiamond]
		b [shape=Msquare]
		a -> b [weight="not_a_number"]
	}`
	g, err := dot.Parse(source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	output := translateGraphToParseOutput(g)

	// Invalid weight should default to 0
	if output.Edges[0].Weight != 0 {
		t.Errorf("weight = %d, want 0 for invalid weight string", output.Edges[0].Weight)
	}
}

func TestTranslateGraphToParseOutput_GraphAttributes(t *testing.T) {
	source := `digraph pipeline {
		goal="build a thing"
		model="claude"
		start [shape=Mdiamond]
		done [shape=Msquare]
		start -> done
	}`
	g, err := dot.Parse(source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	output := translateGraphToParseOutput(g)

	if output.Attributes["goal"] != "build a thing" {
		t.Errorf("attributes.goal = %q, want 'build a thing'", output.Attributes["goal"])
	}
	if output.Attributes["model"] != "claude" {
		t.Errorf("attributes.model = %q, want 'claude'", output.Attributes["model"])
	}
}

func TestTranslateGraphToParseOutput_EdgeCondition(t *testing.T) {
	source := `digraph pipeline {
		a [shape=Mdiamond]
		b [shape=box]
		c [shape=Msquare]
		a -> b
		b -> c [condition="outcome=SUCCESS"]
	}`
	g, err := dot.Parse(source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	output := translateGraphToParseOutput(g)

	if len(output.Edges) != 2 {
		t.Fatalf("got %d edges, want 2", len(output.Edges))
	}
	if output.Edges[1].Condition != "outcome=SUCCESS" {
		t.Errorf("condition = %q, want 'outcome=SUCCESS'", output.Edges[1].Condition)
	}
}

func TestCmdParse_Success(t *testing.T) {
	source := `digraph pipeline {
		start [shape=Mdiamond]
		end [shape=Msquare]
		start -> end
	}`
	dir := t.TempDir()
	dotfile := filepath.Join(dir, "test.dot")
	if err := os.WriteFile(dotfile, []byte(source), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	exitCode := cmdParse(dotfile)

	w.Close()
	os.Stdout = oldStdout

	if exitCode != 0 {
		t.Errorf("exit code = %d, want 0", exitCode)
	}

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	output := string(buf[:n])
	if len(output) == 0 {
		t.Error("expected JSON output, got empty")
	}
}

func TestCmdParse_FileNotFound(t *testing.T) {
	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	exitCode := cmdParse("/nonexistent/file.dot")

	w.Close()
	os.Stdout = oldStdout

	if exitCode != 1 {
		t.Errorf("exit code = %d, want 1", exitCode)
	}

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	output := string(buf[:n])
	if len(output) == 0 {
		t.Error("expected error JSON output, got empty")
	}
}

func TestCmdParse_InvalidDOT(t *testing.T) {
	dir := t.TempDir()
	dotfile := filepath.Join(dir, "bad.dot")
	if err := os.WriteFile(dotfile, []byte("not a valid dot file {{{"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	exitCode := cmdParse(dotfile)

	w.Close()
	os.Stdout = oldStdout

	if exitCode != 1 {
		t.Errorf("exit code = %d, want 1", exitCode)
	}

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	output := string(buf[:n])
	if len(output) == 0 {
		t.Error("expected error JSON output, got empty")
	}
}

func TestWriteError(t *testing.T) {
	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	writeError("something went wrong")

	w.Close()
	os.Stdout = oldStdout

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	output := string(buf[:n])
	if len(output) == 0 {
		t.Error("expected error JSON output, got empty")
	}
	// Should contain the error message
	if !strings.Contains(output, "something went wrong") {
		t.Errorf("output %q does not contain error message", output)
	}
}
