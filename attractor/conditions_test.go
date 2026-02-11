// ABOUTME: Tests for the condition expression language used in edge guards.
// ABOUTME: Covers outcome matching, context lookups, AND conjunctions, and key resolution.
package attractor

import (
	"testing"
)

func TestEvaluateCondition_OutcomeEquals(t *testing.T) {
	outcome := &Outcome{Status: StatusSuccess}
	ctx := NewContext()

	if !EvaluateCondition("outcome = success", outcome, ctx) {
		t.Error("expected outcome = success to be true")
	}
	if EvaluateCondition("outcome = fail", outcome, ctx) {
		t.Error("expected outcome = fail to be false")
	}
}

func TestEvaluateCondition_OutcomeNotEquals(t *testing.T) {
	outcome := &Outcome{Status: StatusFail}
	ctx := NewContext()

	if !EvaluateCondition("outcome != success", outcome, ctx) {
		t.Error("expected outcome != success to be true")
	}
	if EvaluateCondition("outcome != fail", outcome, ctx) {
		t.Error("expected outcome != fail to be false")
	}
}

func TestEvaluateCondition_ContextValue(t *testing.T) {
	outcome := &Outcome{Status: StatusSuccess}
	ctx := NewContext()
	ctx.Set("context.language", "go")

	if !EvaluateCondition("context.language = go", outcome, ctx) {
		t.Error("expected context.language = go to be true")
	}
	if EvaluateCondition("context.language = python", outcome, ctx) {
		t.Error("expected context.language = python to be false")
	}
}

func TestEvaluateCondition_AndConjunction(t *testing.T) {
	outcome := &Outcome{Status: StatusSuccess, PreferredLabel: "fast"}
	ctx := NewContext()

	if !EvaluateCondition("outcome = success && preferred_label = fast", outcome, ctx) {
		t.Error("expected AND conjunction to be true")
	}
	if EvaluateCondition("outcome = success && preferred_label = slow", outcome, ctx) {
		t.Error("expected AND conjunction to be false when second clause fails")
	}
}

func TestEvaluateCondition_EmptyCondition(t *testing.T) {
	outcome := &Outcome{Status: StatusFail}
	ctx := NewContext()

	if !EvaluateCondition("", outcome, ctx) {
		t.Error("empty condition should return true")
	}
	if !EvaluateCondition("   ", outcome, ctx) {
		t.Error("whitespace-only condition should return true")
	}
}

func TestEvaluateCondition_PreferredLabel(t *testing.T) {
	outcome := &Outcome{Status: StatusSuccess, PreferredLabel: "detailed"}
	ctx := NewContext()

	if !EvaluateCondition("preferred_label = detailed", outcome, ctx) {
		t.Error("expected preferred_label = detailed to be true")
	}
	if EvaluateCondition("preferred_label = brief", outcome, ctx) {
		t.Error("expected preferred_label = brief to be false")
	}
}

func TestEvaluateCondition_MissingContextKey(t *testing.T) {
	outcome := &Outcome{Status: StatusSuccess}
	ctx := NewContext()

	// Missing context key resolves to empty string
	if EvaluateCondition("context.missing = something", outcome, ctx) {
		t.Error("missing context key should compare as empty string, not match 'something'")
	}
	if !EvaluateCondition("context.missing = ", outcome, ctx) {
		t.Error("missing context key should match empty string")
	}
}

func TestEvaluateCondition_CaseInsensitive(t *testing.T) {
	tests := []struct {
		name      string
		condition string
		outcome   *Outcome
		want      bool
	}{
		{
			name:      "uppercase SUCCESS matches lowercase status",
			condition: "outcome = SUCCESS",
			outcome:   &Outcome{Status: StatusSuccess},
			want:      true,
		},
		{
			name:      "uppercase FAIL matches lowercase status",
			condition: "outcome = FAIL",
			outcome:   &Outcome{Status: StatusFail},
			want:      true,
		},
		{
			name:      "mixed case Success matches lowercase status",
			condition: "outcome = Success",
			outcome:   &Outcome{Status: StatusSuccess},
			want:      true,
		},
		{
			name:      "lowercase still works",
			condition: "outcome = success",
			outcome:   &Outcome{Status: StatusSuccess},
			want:      true,
		},
		{
			name:      "uppercase SUCCESS does not match fail status",
			condition: "outcome = SUCCESS",
			outcome:   &Outcome{Status: StatusFail},
			want:      false,
		},
		{
			name:      "not-equals case insensitive",
			condition: "outcome != SUCCESS",
			outcome:   &Outcome{Status: StatusSuccess},
			want:      false,
		},
		{
			name:      "not-equals case insensitive true case",
			condition: "outcome != FAIL",
			outcome:   &Outcome{Status: StatusSuccess},
			want:      true,
		},
	}

	ctx := NewContext()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := EvaluateCondition(tc.condition, tc.outcome, ctx)
			if got != tc.want {
				t.Errorf("EvaluateCondition(%q) = %v, want %v", tc.condition, got, tc.want)
			}
		})
	}
}

func TestResolveKey_Outcome(t *testing.T) {
	outcome := &Outcome{Status: StatusPartialSuccess}
	ctx := NewContext()

	got := resolveKey("outcome", outcome, ctx)
	if got != string(StatusPartialSuccess) {
		t.Errorf("expected %q, got %q", StatusPartialSuccess, got)
	}
}

func TestResolveKey_PreferredLabel(t *testing.T) {
	outcome := &Outcome{PreferredLabel: "review"}
	ctx := NewContext()

	got := resolveKey("preferred_label", outcome, ctx)
	if got != "review" {
		t.Errorf("expected 'review', got %q", got)
	}
}

func TestResolveKey_ContextDotPrefix(t *testing.T) {
	outcome := &Outcome{}
	ctx := NewContext()
	ctx.Set("context.mode", "production")

	got := resolveKey("context.mode", outcome, ctx)
	if got != "production" {
		t.Errorf("expected 'production', got %q", got)
	}

	// Also try with value stored without prefix
	ctx2 := NewContext()
	ctx2.Set("mode", "staging")

	got2 := resolveKey("context.mode", outcome, ctx2)
	if got2 != "staging" {
		t.Errorf("expected 'staging' via fallback, got %q", got2)
	}
}

func TestResolveKey_BareKey(t *testing.T) {
	outcome := &Outcome{}
	ctx := NewContext()
	ctx.Set("environment", "test")

	got := resolveKey("environment", outcome, ctx)
	if got != "test" {
		t.Errorf("expected 'test', got %q", got)
	}
}
