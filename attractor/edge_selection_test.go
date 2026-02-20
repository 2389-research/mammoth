// ABOUTME: Tests for the edge selection algorithm used during pipeline graph traversal.
// ABOUTME: Covers priority order: condition > preferred label > suggested IDs > weight > lexical tiebreak.
package attractor

import (
	"testing"
)

// --- NormalizeLabel tests ---

func TestNormalizeLabelLowercase(t *testing.T) {
	got := NormalizeLabel("YES")
	if got != "yes" {
		t.Errorf("expected 'yes', got %q", got)
	}
}

func TestNormalizeLabelTrimsWhitespace(t *testing.T) {
	got := NormalizeLabel("  hello  ")
	if got != "hello" {
		t.Errorf("expected 'hello', got %q", got)
	}
}

func TestNormalizeLabelStripsAcceleratorBracket(t *testing.T) {
	got := NormalizeLabel("[Y] Yes please")
	if got != "yes please" {
		t.Errorf("expected 'yes please', got %q", got)
	}
}

func TestNormalizeLabelStripsAcceleratorParen(t *testing.T) {
	got := NormalizeLabel("Y) Continue")
	if got != "continue" {
		t.Errorf("expected 'continue', got %q", got)
	}
}

func TestNormalizeLabelStripsAcceleratorDash(t *testing.T) {
	got := NormalizeLabel("Y - Proceed")
	if got != "proceed" {
		t.Errorf("expected 'proceed', got %q", got)
	}
}

func TestNormalizeLabelEmpty(t *testing.T) {
	got := NormalizeLabel("")
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestNormalizeLabelNoAccelerator(t *testing.T) {
	got := NormalizeLabel("just a label")
	if got != "just a label" {
		t.Errorf("expected 'just a label', got %q", got)
	}
}

// --- bestByWeightThenLexical tests ---

func TestBestByWeightThenLexicalSingleEdge(t *testing.T) {
	edges := []*Edge{
		{From: "a", To: "b", Attrs: map[string]string{}},
	}
	got := bestByWeightThenLexical(edges)
	if got == nil {
		t.Fatal("expected non-nil edge")
	}
	if got.To != "b" {
		t.Errorf("expected To='b', got %q", got.To)
	}
}

func TestBestByWeightThenLexicalHigherWeightWins(t *testing.T) {
	edges := []*Edge{
		{From: "a", To: "low", Attrs: map[string]string{"weight": "1"}},
		{From: "a", To: "high", Attrs: map[string]string{"weight": "10"}},
	}
	got := bestByWeightThenLexical(edges)
	if got == nil {
		t.Fatal("expected non-nil edge")
	}
	if got.To != "high" {
		t.Errorf("expected To='high', got %q", got.To)
	}
}

func TestBestByWeightThenLexicalTiebreakByTo(t *testing.T) {
	edges := []*Edge{
		{From: "a", To: "zebra", Attrs: map[string]string{"weight": "5"}},
		{From: "a", To: "alpha", Attrs: map[string]string{"weight": "5"}},
	}
	got := bestByWeightThenLexical(edges)
	if got == nil {
		t.Fatal("expected non-nil edge")
	}
	if got.To != "alpha" {
		t.Errorf("expected To='alpha' (lexical first), got %q", got.To)
	}
}

func TestBestByWeightThenLexicalNoEdges(t *testing.T) {
	got := bestByWeightThenLexical(nil)
	if got != nil {
		t.Errorf("expected nil for empty edges, got %v", got)
	}
}

func TestBestByWeightThenLexicalDefaultWeightIsZero(t *testing.T) {
	edges := []*Edge{
		{From: "a", To: "no_weight", Attrs: map[string]string{}},
		{From: "a", To: "has_weight", Attrs: map[string]string{"weight": "1"}},
	}
	got := bestByWeightThenLexical(edges)
	if got == nil {
		t.Fatal("expected non-nil edge")
	}
	if got.To != "has_weight" {
		t.Errorf("expected To='has_weight' (weight 1 > default 0), got %q", got.To)
	}
}

// --- SelectEdge tests ---

func TestSelectEdgeSingleUnconditionalEdge(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"a": {ID: "a", Attrs: map[string]string{}},
			"b": {ID: "b", Attrs: map[string]string{}},
		},
		Edges: []*Edge{
			{From: "a", To: "b", Attrs: map[string]string{}},
		},
	}
	node := g.Nodes["a"]
	outcome := &Outcome{Status: StatusSuccess}
	ctx := NewContext()

	got := SelectEdge(node, outcome, ctx, g)
	if got == nil {
		t.Fatal("expected non-nil edge")
	}
	if got.To != "b" {
		t.Errorf("expected To='b', got %q", got.To)
	}
}

func TestSelectEdgeConditionMatchingTakesPriority(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"a":      {ID: "a", Attrs: map[string]string{}},
			"cond":   {ID: "cond", Attrs: map[string]string{}},
			"uncond": {ID: "uncond", Attrs: map[string]string{}},
		},
		Edges: []*Edge{
			{From: "a", To: "uncond", Attrs: map[string]string{"weight": "100"}},
			{From: "a", To: "cond", Attrs: map[string]string{"condition": "outcome = success"}},
		},
	}
	node := g.Nodes["a"]
	outcome := &Outcome{Status: StatusSuccess}
	ctx := NewContext()

	got := SelectEdge(node, outcome, ctx, g)
	if got == nil {
		t.Fatal("expected non-nil edge")
	}
	if got.To != "cond" {
		t.Errorf("expected condition-matching edge To='cond', got %q", got.To)
	}
}

func TestSelectEdgeConditionNotMatchingFallsThrough(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"a":      {ID: "a", Attrs: map[string]string{}},
			"cond":   {ID: "cond", Attrs: map[string]string{}},
			"uncond": {ID: "uncond", Attrs: map[string]string{}},
		},
		Edges: []*Edge{
			{From: "a", To: "uncond", Attrs: map[string]string{}},
			{From: "a", To: "cond", Attrs: map[string]string{"condition": "outcome = fail"}},
		},
	}
	node := g.Nodes["a"]
	outcome := &Outcome{Status: StatusSuccess}
	ctx := NewContext()

	got := SelectEdge(node, outcome, ctx, g)
	if got == nil {
		t.Fatal("expected non-nil edge")
	}
	if got.To != "uncond" {
		t.Errorf("expected unconditional edge To='uncond', got %q", got.To)
	}
}

func TestSelectEdgePreferredLabelMatching(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"a": {ID: "a", Attrs: map[string]string{}},
			"b": {ID: "b", Attrs: map[string]string{}},
			"c": {ID: "c", Attrs: map[string]string{}},
		},
		Edges: []*Edge{
			{From: "a", To: "b", Attrs: map[string]string{"label": "[Y] Yes"}},
			{From: "a", To: "c", Attrs: map[string]string{"label": "[N] No"}},
		},
	}
	node := g.Nodes["a"]
	outcome := &Outcome{Status: StatusSuccess, PreferredLabel: "yes"}
	ctx := NewContext()

	got := SelectEdge(node, outcome, ctx, g)
	if got == nil {
		t.Fatal("expected non-nil edge")
	}
	if got.To != "b" {
		t.Errorf("expected preferred label match To='b', got %q", got.To)
	}
}

func TestSelectEdgeSuggestedNextIDsMatching(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"a": {ID: "a", Attrs: map[string]string{}},
			"b": {ID: "b", Attrs: map[string]string{}},
			"c": {ID: "c", Attrs: map[string]string{}},
		},
		Edges: []*Edge{
			{From: "a", To: "b", Attrs: map[string]string{}},
			{From: "a", To: "c", Attrs: map[string]string{}},
		},
	}
	node := g.Nodes["a"]
	outcome := &Outcome{Status: StatusSuccess, SuggestedNextIDs: []string{"c"}}
	ctx := NewContext()

	got := SelectEdge(node, outcome, ctx, g)
	if got == nil {
		t.Fatal("expected non-nil edge")
	}
	if got.To != "c" {
		t.Errorf("expected suggested next ID match To='c', got %q", got.To)
	}
}

func TestSelectEdgeWeightBasedSelection(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"a":    {ID: "a", Attrs: map[string]string{}},
			"low":  {ID: "low", Attrs: map[string]string{}},
			"high": {ID: "high", Attrs: map[string]string{}},
		},
		Edges: []*Edge{
			{From: "a", To: "low", Attrs: map[string]string{"weight": "1"}},
			{From: "a", To: "high", Attrs: map[string]string{"weight": "10"}},
		},
	}
	node := g.Nodes["a"]
	outcome := &Outcome{Status: StatusSuccess}
	ctx := NewContext()

	got := SelectEdge(node, outcome, ctx, g)
	if got == nil {
		t.Fatal("expected non-nil edge")
	}
	if got.To != "high" {
		t.Errorf("expected highest weight To='high', got %q", got.To)
	}
}

func TestSelectEdgeLexicalTiebreakWhenWeightsEqual(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"a":     {ID: "a", Attrs: map[string]string{}},
			"zebra": {ID: "zebra", Attrs: map[string]string{}},
			"alpha": {ID: "alpha", Attrs: map[string]string{}},
		},
		Edges: []*Edge{
			{From: "a", To: "zebra", Attrs: map[string]string{}},
			{From: "a", To: "alpha", Attrs: map[string]string{}},
		},
	}
	node := g.Nodes["a"]
	outcome := &Outcome{Status: StatusSuccess}
	ctx := NewContext()

	got := SelectEdge(node, outcome, ctx, g)
	if got == nil {
		t.Fatal("expected non-nil edge")
	}
	if got.To != "alpha" {
		t.Errorf("expected lexical first To='alpha', got %q", got.To)
	}
}

func TestSelectEdgeNoEdgesReturnsNil(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"a": {ID: "a", Attrs: map[string]string{}},
		},
		Edges: []*Edge{},
	}
	node := g.Nodes["a"]
	outcome := &Outcome{Status: StatusSuccess}
	ctx := NewContext()

	got := SelectEdge(node, outcome, ctx, g)
	if got != nil {
		t.Errorf("expected nil for no edges, got %v", got)
	}
}

func TestSelectEdgeEmptyConditionTreatedAsUnconditional(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"a": {ID: "a", Attrs: map[string]string{}},
			"b": {ID: "b", Attrs: map[string]string{}},
		},
		Edges: []*Edge{
			{From: "a", To: "b", Attrs: map[string]string{"condition": ""}},
		},
	}
	node := g.Nodes["a"]
	outcome := &Outcome{Status: StatusSuccess}
	ctx := NewContext()

	got := SelectEdge(node, outcome, ctx, g)
	if got == nil {
		t.Fatal("expected non-nil edge for empty condition")
	}
	if got.To != "b" {
		t.Errorf("expected To='b', got %q", got.To)
	}
}

func TestSelectEdgeMultipleConditionsBestByWeight(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"a":   {ID: "a", Attrs: map[string]string{}},
			"low": {ID: "low", Attrs: map[string]string{}},
			"hi":  {ID: "hi", Attrs: map[string]string{}},
		},
		Edges: []*Edge{
			{From: "a", To: "low", Attrs: map[string]string{"condition": "outcome = success", "weight": "1"}},
			{From: "a", To: "hi", Attrs: map[string]string{"condition": "outcome = success", "weight": "10"}},
		},
	}
	node := g.Nodes["a"]
	outcome := &Outcome{Status: StatusSuccess}
	ctx := NewContext()

	got := SelectEdge(node, outcome, ctx, g)
	if got == nil {
		t.Fatal("expected non-nil edge")
	}
	if got.To != "hi" {
		t.Errorf("expected highest weight condition match To='hi', got %q", got.To)
	}
}

func TestSelectEdgePreferredLabelWithAccelerator(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"gate": {ID: "gate", Attrs: map[string]string{}},
			"yes":  {ID: "yes", Attrs: map[string]string{}},
			"no":   {ID: "no", Attrs: map[string]string{}},
		},
		Edges: []*Edge{
			{From: "gate", To: "yes", Attrs: map[string]string{"label": "[Y] Yes"}},
			{From: "gate", To: "no", Attrs: map[string]string{"label": "[N] No"}},
		},
	}
	node := g.Nodes["gate"]
	outcome := &Outcome{Status: StatusSuccess, PreferredLabel: "No"}
	ctx := NewContext()

	got := SelectEdge(node, outcome, ctx, g)
	if got == nil {
		t.Fatal("expected non-nil edge")
	}
	if got.To != "no" {
		t.Errorf("expected preferred label 'No' to match '[N] No' -> To='no', got %q", got.To)
	}
}

func TestSelectEdgeFailedOutcomeDoesNotFollowUnconditionalEdge(t *testing.T) {
	// When a node fails but only has unconditional edges (no condition="outcome = fail"),
	// SelectEdge should return nil so the engine can halt with an error.
	g := &Graph{
		Nodes: map[string]*Node{
			"a": {ID: "a", Attrs: map[string]string{}},
			"b": {ID: "b", Attrs: map[string]string{}},
		},
		Edges: []*Edge{
			{From: "a", To: "b", Attrs: map[string]string{}},
		},
	}
	node := g.Nodes["a"]
	outcome := &Outcome{Status: StatusFail, FailureReason: "some error"}
	ctx := NewContext()

	got := SelectEdge(node, outcome, ctx, g)
	if got != nil {
		t.Errorf("expected nil for failed outcome with only unconditional edges, got edge to %q", got.To)
	}
}

func TestSelectEdgeFailedOutcomeFollowsConditionMatchedEdge(t *testing.T) {
	// When a node fails and there IS a condition="outcome = fail" edge, it should be followed.
	g := &Graph{
		Nodes: map[string]*Node{
			"a":        {ID: "a", Attrs: map[string]string{}},
			"recovery": {ID: "recovery", Attrs: map[string]string{}},
			"normal":   {ID: "normal", Attrs: map[string]string{}},
		},
		Edges: []*Edge{
			{From: "a", To: "normal", Attrs: map[string]string{}},
			{From: "a", To: "recovery", Attrs: map[string]string{"condition": "outcome = fail"}},
		},
	}
	node := g.Nodes["a"]
	outcome := &Outcome{Status: StatusFail, FailureReason: "some error"}
	ctx := NewContext()

	got := SelectEdge(node, outcome, ctx, g)
	if got == nil {
		t.Fatal("expected condition-matched fail edge, got nil")
	}
	if got.To != "recovery" {
		t.Errorf("expected fail edge to 'recovery', got %q", got.To)
	}
}
