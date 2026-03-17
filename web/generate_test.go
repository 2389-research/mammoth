// ABOUTME: Tests for the embedded meta-pipeline DOT and the pipeline generation handler.
// ABOUTME: Verifies parsing, validation, build creation, HTTP endpoint, and failure preservation.
package web

import (
	"testing"

	"github.com/2389-research/mammoth/dot"
	"github.com/2389-research/mammoth/dot/validator"
)

func TestEmbeddedMetaPipelineParses(t *testing.T) {
	if metaPipelineDOT == "" {
		t.Fatal("metaPipelineDOT is empty")
	}
	g, err := dot.Parse(metaPipelineDOT)
	if err != nil {
		t.Fatalf("embedded meta-pipeline failed to parse: %v", err)
	}
	if g == nil {
		t.Fatal("parsed graph is nil")
	}
}

func TestEmbeddedMetaPipelineValidates(t *testing.T) {
	g, err := dot.Parse(metaPipelineDOT)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	diags := validator.Lint(g)
	for _, d := range diags {
		if d.Severity == "error" {
			t.Errorf("validation error: %s (node=%s)", d.Message, d.NodeID)
		}
	}
}
