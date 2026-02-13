// ABOUTME: Tests for the CSS-like model stylesheet parser and applicator.
// ABOUTME: Covers parsing selectors, specificity resolution, and property application to graph nodes.
package attractor

import (
	"testing"
)

func TestParseStylesheet_Universal(t *testing.T) {
	input := `* { llm_model: claude-sonnet-4-5; }`
	ss, err := ParseStylesheet(input)
	if err != nil {
		t.Fatalf("ParseStylesheet() error = %v", err)
	}
	if len(ss.Rules) != 1 {
		t.Fatalf("got %d rules, want 1", len(ss.Rules))
	}
	rule := ss.Rules[0]
	if rule.Selector != "*" {
		t.Errorf("Selector = %q, want %q", rule.Selector, "*")
	}
	if rule.Specificity != 0 {
		t.Errorf("Specificity = %d, want 0", rule.Specificity)
	}
	if rule.Properties["llm_model"] != "claude-sonnet-4-5" {
		t.Errorf("llm_model = %q, want %q", rule.Properties["llm_model"], "claude-sonnet-4-5")
	}
}

func TestParseStylesheet_IDSelector(t *testing.T) {
	input := `#node_id { llm_model: gpt-5.2; }`
	ss, err := ParseStylesheet(input)
	if err != nil {
		t.Fatalf("ParseStylesheet() error = %v", err)
	}
	if len(ss.Rules) != 1 {
		t.Fatalf("got %d rules, want 1", len(ss.Rules))
	}
	rule := ss.Rules[0]
	if rule.Selector != "#node_id" {
		t.Errorf("Selector = %q, want %q", rule.Selector, "#node_id")
	}
	if rule.Specificity != 2 {
		t.Errorf("Specificity = %d, want 2", rule.Specificity)
	}
	if rule.Properties["llm_model"] != "gpt-5.2" {
		t.Errorf("llm_model = %q, want %q", rule.Properties["llm_model"], "gpt-5.2")
	}
}

func TestParseStylesheet_ClassSelector(t *testing.T) {
	input := `.code { llm_model: claude-opus-4-6; }`
	ss, err := ParseStylesheet(input)
	if err != nil {
		t.Fatalf("ParseStylesheet() error = %v", err)
	}
	if len(ss.Rules) != 1 {
		t.Fatalf("got %d rules, want 1", len(ss.Rules))
	}
	rule := ss.Rules[0]
	if rule.Selector != ".code" {
		t.Errorf("Selector = %q, want %q", rule.Selector, ".code")
	}
	if rule.Specificity != 1 {
		t.Errorf("Specificity = %d, want 1", rule.Specificity)
	}
	if rule.Properties["llm_model"] != "claude-opus-4-6" {
		t.Errorf("llm_model = %q, want %q", rule.Properties["llm_model"], "claude-opus-4-6")
	}
}

func TestParseStylesheet_MultipleRules(t *testing.T) {
	input := `
		* { llm_model: claude-sonnet-4-5; }
		.code { llm_model: claude-opus-4-6; }
		#review { llm_model: gpt-5.2; }
	`
	ss, err := ParseStylesheet(input)
	if err != nil {
		t.Fatalf("ParseStylesheet() error = %v", err)
	}
	if len(ss.Rules) != 3 {
		t.Fatalf("got %d rules, want 3", len(ss.Rules))
	}
	if ss.Rules[0].Selector != "*" {
		t.Errorf("Rules[0].Selector = %q, want %q", ss.Rules[0].Selector, "*")
	}
	if ss.Rules[1].Selector != ".code" {
		t.Errorf("Rules[1].Selector = %q, want %q", ss.Rules[1].Selector, ".code")
	}
	if ss.Rules[2].Selector != "#review" {
		t.Errorf("Rules[2].Selector = %q, want %q", ss.Rules[2].Selector, "#review")
	}
}

func TestParseStylesheet_MultipleProperties(t *testing.T) {
	input := `* { llm_model: claude-sonnet-4-5; llm_provider: anthropic; reasoning_effort: medium; }`
	ss, err := ParseStylesheet(input)
	if err != nil {
		t.Fatalf("ParseStylesheet() error = %v", err)
	}
	if len(ss.Rules) != 1 {
		t.Fatalf("got %d rules, want 1", len(ss.Rules))
	}
	props := ss.Rules[0].Properties
	if props["llm_model"] != "claude-sonnet-4-5" {
		t.Errorf("llm_model = %q, want %q", props["llm_model"], "claude-sonnet-4-5")
	}
	if props["llm_provider"] != "anthropic" {
		t.Errorf("llm_provider = %q, want %q", props["llm_provider"], "anthropic")
	}
	if props["reasoning_effort"] != "medium" {
		t.Errorf("reasoning_effort = %q, want %q", props["reasoning_effort"], "medium")
	}
}

func TestParseStylesheet_InvalidSyntax(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"missing brace", `* llm_model: claude-sonnet-4-5; }`},
		{"missing colon", `* { llm_model claude-sonnet-4-5; }`},
		{"missing closing brace", `* { llm_model: claude-sonnet-4-5;`},
		{"empty input", ``},
		{"bad selector", `@ { llm_model: foo; }`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseStylesheet(tt.input)
			if err == nil {
				t.Error("ParseStylesheet() expected error, got nil")
			}
		})
	}
}

func TestStylesheetApply_UniversalRule(t *testing.T) {
	ss := &Stylesheet{
		Rules: []StyleRule{
			{
				Selector:    "*",
				Properties:  map[string]string{"llm_model": "claude-sonnet-4-5", "llm_provider": "anthropic"},
				Specificity: 0,
			},
		},
	}
	g := &Graph{
		Nodes: map[string]*Node{
			"a": {ID: "a", Attrs: map[string]string{"prompt": "do stuff"}},
			"b": {ID: "b", Attrs: map[string]string{"prompt": "do more"}},
		},
	}

	ss.Apply(g)

	for _, id := range []string{"a", "b"} {
		node := g.Nodes[id]
		if node.Attrs["llm_model"] != "claude-sonnet-4-5" {
			t.Errorf("node %q llm_model = %q, want %q", id, node.Attrs["llm_model"], "claude-sonnet-4-5")
		}
		if node.Attrs["llm_provider"] != "anthropic" {
			t.Errorf("node %q llm_provider = %q, want %q", id, node.Attrs["llm_provider"], "anthropic")
		}
	}
}

func TestStylesheetApply_IDOverridesClass(t *testing.T) {
	ss := &Stylesheet{
		Rules: []StyleRule{
			{
				Selector:    ".code",
				Properties:  map[string]string{"llm_model": "claude-opus-4-6"},
				Specificity: 1,
			},
			{
				Selector:    "#special",
				Properties:  map[string]string{"llm_model": "gpt-5.2"},
				Specificity: 2,
			},
		},
	}
	g := &Graph{
		Nodes: map[string]*Node{
			"special": {ID: "special", Attrs: map[string]string{"class": "code"}},
		},
	}

	ss.Apply(g)

	node := g.Nodes["special"]
	if node.Attrs["llm_model"] != "gpt-5.2" {
		t.Errorf("llm_model = %q, want %q (ID should override class)", node.Attrs["llm_model"], "gpt-5.2")
	}
}

func TestStylesheetApply_ClassOverridesUniversal(t *testing.T) {
	ss := &Stylesheet{
		Rules: []StyleRule{
			{
				Selector:    "*",
				Properties:  map[string]string{"llm_model": "claude-sonnet-4-5"},
				Specificity: 0,
			},
			{
				Selector:    ".code",
				Properties:  map[string]string{"llm_model": "claude-opus-4-6"},
				Specificity: 1,
			},
		},
	}
	g := &Graph{
		Nodes: map[string]*Node{
			"worker": {ID: "worker", Attrs: map[string]string{"class": "code"}},
			"other":  {ID: "other", Attrs: map[string]string{}},
		},
	}

	ss.Apply(g)

	if g.Nodes["worker"].Attrs["llm_model"] != "claude-opus-4-6" {
		t.Errorf("worker llm_model = %q, want %q (class should override universal)",
			g.Nodes["worker"].Attrs["llm_model"], "claude-opus-4-6")
	}
	if g.Nodes["other"].Attrs["llm_model"] != "claude-sonnet-4-5" {
		t.Errorf("other llm_model = %q, want %q (universal should apply to non-class node)",
			g.Nodes["other"].Attrs["llm_model"], "claude-sonnet-4-5")
	}
}

func TestStylesheetApply_ExplicitNodeOverridesAll(t *testing.T) {
	ss := &Stylesheet{
		Rules: []StyleRule{
			{
				Selector:    "*",
				Properties:  map[string]string{"llm_model": "claude-sonnet-4-5"},
				Specificity: 0,
			},
			{
				Selector:    "#mynode",
				Properties:  map[string]string{"llm_model": "gpt-5.2"},
				Specificity: 2,
			},
		},
	}
	g := &Graph{
		Nodes: map[string]*Node{
			"mynode": {ID: "mynode", Attrs: map[string]string{"llm_model": "custom-model"}},
		},
	}

	ss.Apply(g)

	if g.Nodes["mynode"].Attrs["llm_model"] != "custom-model" {
		t.Errorf("llm_model = %q, want %q (explicit node attr should override all stylesheet rules)",
			g.Nodes["mynode"].Attrs["llm_model"], "custom-model")
	}
}

func TestStylesheetApply_CommaSeparatedClasses(t *testing.T) {
	ss := &Stylesheet{
		Rules: []StyleRule{
			{
				Selector:    ".code",
				Properties:  map[string]string{"llm_model": "claude-opus-4-6"},
				Specificity: 1,
			},
			{
				Selector:    ".critical",
				Properties:  map[string]string{"reasoning_effort": "high"},
				Specificity: 1,
			},
		},
	}
	g := &Graph{
		Nodes: map[string]*Node{
			"worker": {ID: "worker", Attrs: map[string]string{"class": "code,critical"}},
		},
	}

	ss.Apply(g)

	node := g.Nodes["worker"]
	if node.Attrs["llm_model"] != "claude-opus-4-6" {
		t.Errorf("llm_model = %q, want %q", node.Attrs["llm_model"], "claude-opus-4-6")
	}
	if node.Attrs["reasoning_effort"] != "high" {
		t.Errorf("reasoning_effort = %q, want %q", node.Attrs["reasoning_effort"], "high")
	}
}

func TestStylesheetMatchNode(t *testing.T) {
	ss := &Stylesheet{
		Rules: []StyleRule{
			{
				Selector:    "*",
				Properties:  map[string]string{"llm_model": "claude-sonnet-4-5", "llm_provider": "anthropic"},
				Specificity: 0,
			},
			{
				Selector:    ".code",
				Properties:  map[string]string{"llm_model": "claude-opus-4-6"},
				Specificity: 1,
			},
		},
	}

	node := &Node{
		ID:    "worker",
		Attrs: map[string]string{"class": "code"},
	}

	props := ss.MatchNode(node)
	if props["llm_model"] != "claude-opus-4-6" {
		t.Errorf("llm_model = %q, want %q", props["llm_model"], "claude-opus-4-6")
	}
	if props["llm_provider"] != "anthropic" {
		t.Errorf("llm_provider = %q, want %q (universal should still provide unoverridden props)",
			props["llm_provider"], "anthropic")
	}
}

func TestParseStylesheet_FullExample(t *testing.T) {
	input := `
		* { llm_model: claude-sonnet-4-5; llm_provider: anthropic; }
		.code { llm_model: claude-opus-4-6; llm_provider: anthropic; }
		#critical_review { llm_model: gpt-5.2; llm_provider: openai; reasoning_effort: high; }
	`
	ss, err := ParseStylesheet(input)
	if err != nil {
		t.Fatalf("ParseStylesheet() error = %v", err)
	}
	if len(ss.Rules) != 3 {
		t.Fatalf("got %d rules, want 3", len(ss.Rules))
	}

	// Rule 0: universal
	r0 := ss.Rules[0]
	if r0.Selector != "*" {
		t.Errorf("Rules[0].Selector = %q, want %q", r0.Selector, "*")
	}
	if r0.Specificity != 0 {
		t.Errorf("Rules[0].Specificity = %d, want 0", r0.Specificity)
	}
	if r0.Properties["llm_model"] != "claude-sonnet-4-5" {
		t.Errorf("Rules[0].llm_model = %q, want %q", r0.Properties["llm_model"], "claude-sonnet-4-5")
	}
	if r0.Properties["llm_provider"] != "anthropic" {
		t.Errorf("Rules[0].llm_provider = %q, want %q", r0.Properties["llm_provider"], "anthropic")
	}

	// Rule 1: class selector
	r1 := ss.Rules[1]
	if r1.Selector != ".code" {
		t.Errorf("Rules[1].Selector = %q, want %q", r1.Selector, ".code")
	}
	if r1.Specificity != 1 {
		t.Errorf("Rules[1].Specificity = %d, want 1", r1.Specificity)
	}
	if r1.Properties["llm_model"] != "claude-opus-4-6" {
		t.Errorf("Rules[1].llm_model = %q, want %q", r1.Properties["llm_model"], "claude-opus-4-6")
	}

	// Rule 2: ID selector
	r2 := ss.Rules[2]
	if r2.Selector != "#critical_review" {
		t.Errorf("Rules[2].Selector = %q, want %q", r2.Selector, "#critical_review")
	}
	if r2.Specificity != 2 {
		t.Errorf("Rules[2].Specificity = %d, want 2", r2.Specificity)
	}
	if r2.Properties["llm_model"] != "gpt-5.2" {
		t.Errorf("Rules[2].llm_model = %q, want %q", r2.Properties["llm_model"], "gpt-5.2")
	}
	if r2.Properties["llm_provider"] != "openai" {
		t.Errorf("Rules[2].llm_provider = %q, want %q", r2.Properties["llm_provider"], "openai")
	}
	if r2.Properties["reasoning_effort"] != "high" {
		t.Errorf("Rules[2].reasoning_effort = %q, want %q", r2.Properties["reasoning_effort"], "high")
	}

	// Apply to a graph and verify full resolution
	g := &Graph{
		Nodes: map[string]*Node{
			"plain":           {ID: "plain", Attrs: map[string]string{"prompt": "do stuff"}},
			"coder":           {ID: "coder", Attrs: map[string]string{"class": "code", "prompt": "write code"}},
			"critical_review": {ID: "critical_review", Attrs: map[string]string{"prompt": "review carefully"}},
		},
	}

	ss.Apply(g)

	// plain node gets universal defaults
	if g.Nodes["plain"].Attrs["llm_model"] != "claude-sonnet-4-5" {
		t.Errorf("plain llm_model = %q, want %q", g.Nodes["plain"].Attrs["llm_model"], "claude-sonnet-4-5")
	}

	// coder node gets class override for llm_model
	if g.Nodes["coder"].Attrs["llm_model"] != "claude-opus-4-6" {
		t.Errorf("coder llm_model = %q, want %q", g.Nodes["coder"].Attrs["llm_model"], "claude-opus-4-6")
	}
	if g.Nodes["coder"].Attrs["llm_provider"] != "anthropic" {
		t.Errorf("coder llm_provider = %q, want %q", g.Nodes["coder"].Attrs["llm_provider"], "anthropic")
	}

	// critical_review node gets ID override
	if g.Nodes["critical_review"].Attrs["llm_model"] != "gpt-5.2" {
		t.Errorf("critical_review llm_model = %q, want %q", g.Nodes["critical_review"].Attrs["llm_model"], "gpt-5.2")
	}
	if g.Nodes["critical_review"].Attrs["llm_provider"] != "openai" {
		t.Errorf("critical_review llm_provider = %q, want %q", g.Nodes["critical_review"].Attrs["llm_provider"], "openai")
	}
	if g.Nodes["critical_review"].Attrs["reasoning_effort"] != "high" {
		t.Errorf("critical_review reasoning_effort = %q, want %q", g.Nodes["critical_review"].Attrs["reasoning_effort"], "high")
	}
}
