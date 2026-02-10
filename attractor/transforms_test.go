// ABOUTME: Tests for AST transforms applied between parsing and validation.
// ABOUTME: Covers variable expansion, stylesheet application, transform chaining, and default transform ordering.
package attractor

import (
	"testing"
)

func TestVariableExpansion(t *testing.T) {
	g := &Graph{
		Attrs: map[string]string{"goal": "Build a web app"},
		Nodes: map[string]*Node{
			"worker": {
				ID: "worker",
				Attrs: map[string]string{
					"prompt": "Your goal is: $goal. Please complete it.",
				},
			},
			"reviewer": {
				ID: "reviewer",
				Attrs: map[string]string{
					"prompt": "Review the work for $goal compliance.",
				},
			},
		},
	}

	transform := &VariableExpansionTransform{}
	result := transform.Apply(g)

	worker := result.Nodes["worker"]
	if worker.Attrs["prompt"] != "Your goal is: Build a web app. Please complete it." {
		t.Errorf("worker prompt = %q, want goal expanded", worker.Attrs["prompt"])
	}

	reviewer := result.Nodes["reviewer"]
	if reviewer.Attrs["prompt"] != "Review the work for Build a web app compliance." {
		t.Errorf("reviewer prompt = %q, want goal expanded", reviewer.Attrs["prompt"])
	}
}

func TestVariableExpansion_NoGoal(t *testing.T) {
	g := &Graph{
		Attrs: map[string]string{},
		Nodes: map[string]*Node{
			"worker": {
				ID: "worker",
				Attrs: map[string]string{
					"prompt": "Your goal is: $goal. Please complete it.",
				},
			},
		},
	}

	transform := &VariableExpansionTransform{}
	result := transform.Apply(g)

	worker := result.Nodes["worker"]
	// When goal is empty, $goal should remain unexpanded or be replaced with empty string.
	// The spec says expand $goal, so with no goal set the literal $goal stays.
	if worker.Attrs["prompt"] != "Your goal is: $goal. Please complete it." {
		t.Errorf("worker prompt = %q, want $goal to remain when goal is not set", worker.Attrs["prompt"])
	}
}

func TestStylesheetApplication(t *testing.T) {
	g := &Graph{
		Attrs: map[string]string{
			"model_stylesheet": `* { llm_model: claude-sonnet-4-5; llm_provider: anthropic; }
.code { llm_model: claude-opus-4-6; }`,
		},
		Nodes: map[string]*Node{
			"plain":  {ID: "plain", Attrs: map[string]string{"prompt": "do stuff"}},
			"coder":  {ID: "coder", Attrs: map[string]string{"class": "code", "prompt": "write code"}},
		},
	}

	transform := &StylesheetApplicationTransform{}
	result := transform.Apply(g)

	if result.Nodes["plain"].Attrs["llm_model"] != "claude-sonnet-4-5" {
		t.Errorf("plain llm_model = %q, want %q",
			result.Nodes["plain"].Attrs["llm_model"], "claude-sonnet-4-5")
	}
	if result.Nodes["coder"].Attrs["llm_model"] != "claude-opus-4-6" {
		t.Errorf("coder llm_model = %q, want %q",
			result.Nodes["coder"].Attrs["llm_model"], "claude-opus-4-6")
	}
	if result.Nodes["coder"].Attrs["llm_provider"] != "anthropic" {
		t.Errorf("coder llm_provider = %q, want %q",
			result.Nodes["coder"].Attrs["llm_provider"], "anthropic")
	}
}

func TestStylesheetApplication_NoStylesheet(t *testing.T) {
	g := &Graph{
		Attrs: map[string]string{},
		Nodes: map[string]*Node{
			"worker": {ID: "worker", Attrs: map[string]string{"prompt": "do stuff"}},
		},
	}

	transform := &StylesheetApplicationTransform{}
	result := transform.Apply(g)

	// Should be a no-op -- node attrs unchanged
	if _, exists := result.Nodes["worker"].Attrs["llm_model"]; exists {
		t.Error("llm_model should not be set when no stylesheet is present")
	}
}

func TestApplyTransforms(t *testing.T) {
	g := &Graph{
		Attrs: map[string]string{
			"goal":             "Build a web app",
			"model_stylesheet": `* { llm_model: claude-sonnet-4-5; }`,
		},
		Nodes: map[string]*Node{
			"worker": {
				ID: "worker",
				Attrs: map[string]string{
					"prompt": "Complete $goal now.",
				},
			},
		},
	}

	transforms := []Transform{
		&VariableExpansionTransform{},
		&StylesheetApplicationTransform{},
	}

	result := ApplyTransforms(g, transforms...)

	// Variable expansion should have happened
	if result.Nodes["worker"].Attrs["prompt"] != "Complete Build a web app now." {
		t.Errorf("prompt = %q, want variable expanded", result.Nodes["worker"].Attrs["prompt"])
	}

	// Stylesheet should have been applied
	if result.Nodes["worker"].Attrs["llm_model"] != "claude-sonnet-4-5" {
		t.Errorf("llm_model = %q, want %q",
			result.Nodes["worker"].Attrs["llm_model"], "claude-sonnet-4-5")
	}
}

func TestDefaultTransforms(t *testing.T) {
	transforms := DefaultTransforms()

	if len(transforms) != 3 {
		t.Fatalf("got %d transforms, want 3", len(transforms))
	}

	// First should be sub-pipeline composition (must run before variable expansion)
	if _, ok := transforms[0].(*SubPipelineTransform); !ok {
		t.Errorf("transforms[0] is %T, want *SubPipelineTransform", transforms[0])
	}

	// Second should be variable expansion
	if _, ok := transforms[1].(*VariableExpansionTransform); !ok {
		t.Errorf("transforms[1] is %T, want *VariableExpansionTransform", transforms[1])
	}

	// Third should be stylesheet application
	if _, ok := transforms[2].(*StylesheetApplicationTransform); !ok {
		t.Errorf("transforms[2] is %T, want *StylesheetApplicationTransform", transforms[2])
	}
}
