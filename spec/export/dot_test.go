// ABOUTME: Tests for dynamic DOT pipeline generation from SpecState.
// ABOUTME: Covers empty specs, sequential/parallel tasks, risk/question gates, validation, and goal attributes.
package export_test

import (
	"strings"
	"testing"
	"time"

	"github.com/2389-research/mammoth/dot"
	"github.com/2389-research/mammoth/dot/validator"
	"github.com/2389-research/mammoth/spec/core"
	"github.com/2389-research/mammoth/spec/export"
)

// makeState creates an empty SpecState with a SpecCore for testing.
func makeState(title, oneLiner, goal string) *core.SpecState {
	sc := core.NewSpecCore(title, oneLiner, goal)
	state := core.NewSpecState()
	state.Core = &sc
	return state
}

// makeCard creates a Card with the given attributes. Body is optional (pass "" for no body).
func makeCard(cardType, title, lane string, order float64, body string, refs []string) core.Card {
	now := time.Now().UTC()
	card := core.Card{
		CardID:    core.NewULID(),
		CardType:  cardType,
		Title:     title,
		Lane:      lane,
		Order:     order,
		Refs:      refs,
		CreatedAt: now,
		UpdatedAt: now,
		CreatedBy: "test",
		UpdatedBy: "test",
	}
	if body != "" {
		card.Body = &body
	}
	return card
}

// 1. Empty spec produces minimal pipeline (start -> implement -> exit)
func TestExportDOTEmptySpec(t *testing.T) {
	state := makeState("Empty Project", "Nothing here", "Build something")
	result, err := export.ExportDOT(state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should contain start and exit nodes
	if !strings.Contains(result, "Mdiamond") {
		t.Error("missing start node (Mdiamond)")
	}
	if !strings.Contains(result, "Msquare") {
		t.Error("missing exit node (Msquare)")
	}

	// Should have at least one codergen node (implement)
	if !strings.Contains(result, "codergen") {
		t.Error("empty spec should still have at least one codergen node")
	}

	// Should be valid DOT
	g := export.ExportGraph(state)
	if g == nil {
		t.Fatal("ExportGraph returned nil")
	}
}

// 2. Spec with 3 sequential tasks (refs form chain) produces linear pipeline
func TestExportDOTSequentialTasks(t *testing.T) {
	state := makeState("Sequential", "Three tasks in a row", "Build sequentially")

	// Create 3 tasks where each refs the previous (forming a chain)
	task1 := makeCard("task", "Step 1: Foundation", "Plan", 1.0, "Lay the groundwork", nil)
	task2 := makeCard("task", "Step 2: Walls", "Plan", 2.0, "Build the walls", []string{task1.CardID.String()})
	task3 := makeCard("task", "Step 3: Roof", "Plan", 3.0, "Add the roof", []string{task2.CardID.String()})

	state.Cards.Set(task1.CardID, task1)
	state.Cards.Set(task2.CardID, task2)
	state.Cards.Set(task3.CardID, task3)

	g := export.ExportGraph(state)
	if g == nil {
		t.Fatal("ExportGraph returned nil")
	}

	// Verify there are nodes for each task (they become codergen nodes)
	// The graph should have at least: start, 3 task nodes, exit = 5 nodes
	if len(g.Nodes) < 5 {
		t.Errorf("expected at least 5 nodes for sequential 3-task spec, got %d", len(g.Nodes))
	}

	// Verify the pipeline is linear (no fork/join nodes)
	// In a sequential pipeline, we should NOT see fork or join nodes
	for id := range g.Nodes {
		if strings.Contains(id, "fork") || strings.Contains(id, "join") {
			t.Errorf("sequential tasks should not produce fork/join nodes, found: %s", id)
		}
	}
}

// 3. Spec with 2 independent tasks (no refs) produces parallel branch
func TestExportDOTParallelTasks(t *testing.T) {
	state := makeState("Parallel", "Two independent tasks", "Build in parallel")

	task1 := makeCard("task", "Build API", "Plan", 1.0, "REST API endpoints", nil)
	task2 := makeCard("task", "Build UI", "Plan", 2.0, "React frontend", nil)

	state.Cards.Set(task1.CardID, task1)
	state.Cards.Set(task2.CardID, task2)

	g := export.ExportGraph(state)
	if g == nil {
		t.Fatal("ExportGraph returned nil")
	}

	// Parallel tasks should produce fork and join nodes
	hasFork := false
	hasJoin := false
	for id, node := range g.Nodes {
		if strings.Contains(id, "fork") || node.Attrs["type"] == "parallel" {
			hasFork = true
		}
		if strings.Contains(id, "join") || node.Attrs["type"] == "parallel.fan_in" {
			hasJoin = true
		}
	}
	if !hasFork {
		t.Error("parallel tasks should produce a fork node")
	}
	if !hasJoin {
		t.Error("parallel tasks should produce a join node")
	}
}

// 4. Spec with risk card adds verification gate
func TestExportDOTWithRiskCard(t *testing.T) {
	state := makeState("Risky", "Has risks", "Build carefully")

	task := makeCard("task", "Deploy service", "Plan", 1.0, "Deploy the service", nil)
	risk := makeCard("risk", "Data loss possible", "Plan", 2.0, "Could lose customer data", nil)

	state.Cards.Set(task.CardID, task)
	state.Cards.Set(risk.CardID, risk)

	g := export.ExportGraph(state)
	if g == nil {
		t.Fatal("ExportGraph returned nil")
	}

	// Should have a verification gate (diamond shape)
	hasVerifyGate := false
	for _, node := range g.Nodes {
		if node.Attrs["shape"] == "diamond" {
			hasVerifyGate = true
			break
		}
	}
	if !hasVerifyGate {
		t.Error("spec with risk card should have a verification gate (diamond node)")
	}
}

// 5. Spec with open_question card does NOT auto-insert human gate
func TestExportDOTWithOpenQuestionNoHumanGate(t *testing.T) {
	state := makeState("Questions", "Has open questions", "Build with clarity")

	task := makeCard("task", "Implement feature", "Plan", 1.0, "Build the feature", nil)
	question := makeCard("open_question", "Which database?", "Plan", 2.0, "PostgreSQL vs MySQL", nil)

	state.Cards.Set(task.CardID, task)
	state.Cards.Set(question.CardID, question)

	g := export.ExportGraph(state)
	if g == nil {
		t.Fatal("ExportGraph returned nil")
	}

	// Human gates should NOT be auto-inserted from open_question cards
	for _, node := range g.Nodes {
		if node.Attrs["shape"] == "hexagon" && node.Attrs["type"] == "wait.human" {
			t.Error("open_question cards should not auto-insert human gates")
		}
	}
}

// 6. Always has start (Mdiamond) and exit (Msquare)
func TestExportDOTAlwaysHasStartAndExit(t *testing.T) {
	tests := []struct {
		name  string
		state *core.SpecState
	}{
		{"empty spec", makeState("Empty", "empty", "test")},
		{"spec with tasks", func() *core.SpecState {
			s := makeState("Tasks", "tasks", "test")
			t := makeCard("task", "Do thing", "Plan", 1.0, "", nil)
			s.Cards.Set(t.CardID, t)
			return s
		}()},
		{"spec with nil core", func() *core.SpecState {
			s := core.NewSpecState()
			return s
		}()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := export.ExportGraph(tt.state)
			if g == nil {
				t.Fatal("ExportGraph returned nil")
			}

			startNode := g.FindStartNode()
			if startNode == nil {
				t.Error("graph must have a start node (Mdiamond)")
			}

			exitNode := g.FindExitNode()
			if exitNode == nil {
				t.Error("graph must have an exit node (Msquare)")
			}
		})
	}
}

// 7. Graph passes validation
func TestExportDOTPassesValidation(t *testing.T) {
	tests := []struct {
		name  string
		state *core.SpecState
	}{
		{"empty spec", makeState("Empty", "empty", "validate me")},
		{"sequential tasks", func() *core.SpecState {
			s := makeState("Seq", "seq", "validate seq")
			t1 := makeCard("task", "Task A", "Plan", 1.0, "first", nil)
			t2 := makeCard("task", "Task B", "Plan", 2.0, "second", []string{t1.CardID.String()})
			s.Cards.Set(t1.CardID, t1)
			s.Cards.Set(t2.CardID, t2)
			return s
		}()},
		{"parallel tasks", func() *core.SpecState {
			s := makeState("Par", "par", "validate par")
			t1 := makeCard("task", "Task A", "Plan", 1.0, "first", nil)
			t2 := makeCard("task", "Task B", "Plan", 2.0, "second", nil)
			s.Cards.Set(t1.CardID, t1)
			s.Cards.Set(t2.CardID, t2)
			return s
		}()},
		{"with risk", func() *core.SpecState {
			s := makeState("Risk", "risky", "validate risk")
			t1 := makeCard("task", "Deploy", "Plan", 1.0, "deploy", nil)
			r := makeCard("risk", "Data loss", "Plan", 2.0, "bad", nil)
			s.Cards.Set(t1.CardID, t1)
			s.Cards.Set(r.CardID, r)
			return s
		}()},
		{"with open question", func() *core.SpecState {
			s := makeState("Q", "questions", "validate questions")
			t1 := makeCard("task", "Build", "Plan", 1.0, "build", nil)
			q := makeCard("open_question", "Which DB?", "Plan", 2.0, "pg vs mysql", nil)
			s.Cards.Set(t1.CardID, t1)
			s.Cards.Set(q.CardID, q)
			return s
		}()},
		{"full spec", func() *core.SpecState {
			s := makeState("Full", "everything", "validate all")
			t1 := makeCard("task", "API", "Plan", 1.0, "build api", nil)
			t2 := makeCard("task", "UI", "Plan", 2.0, "build ui", nil)
			r := makeCard("risk", "Security", "Plan", 3.0, "vulnerabilities", nil)
			q := makeCard("open_question", "Architecture?", "Plan", 4.0, "monolith vs micro", nil)
			s.Cards.Set(t1.CardID, t1)
			s.Cards.Set(t2.CardID, t2)
			s.Cards.Set(r.CardID, r)
			s.Cards.Set(q.CardID, q)
			return s
		}()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := export.ExportGraph(tt.state)
			if g == nil {
				t.Fatal("ExportGraph returned nil")
			}

			diags := validator.Lint(g)
			var errors []dot.Diagnostic
			for _, d := range diags {
				if d.Severity == "error" {
					errors = append(errors, d)
				}
			}
			if len(errors) > 0 {
				for _, d := range errors {
					t.Errorf("validation error: [%s] %s (node=%s, edge=%s)", d.Rule, d.Message, d.NodeID, d.EdgeID)
				}
			}
		})
	}
}

// 8. Goal attribute is set from SpecCore
func TestExportDOTSetsGoal(t *testing.T) {
	state := makeState("My Project", "A great project", "Build the best thing ever")
	g := export.ExportGraph(state)
	if g == nil {
		t.Fatal("ExportGraph returned nil")
	}

	if g.Attrs == nil {
		t.Fatal("graph has no attributes")
	}

	goal := g.Attrs["goal"]
	if goal != "Build the best thing ever" {
		t.Errorf("expected goal='Build the best thing ever', got %q", goal)
	}
}

// Additional: goal fallback when Goal is empty
func TestExportDOTGoalFallback(t *testing.T) {
	sc := core.NewSpecCore("Fallback", "Uses title and one_liner", "")
	state := core.NewSpecState()
	state.Core = &sc

	g := export.ExportGraph(state)
	if g == nil {
		t.Fatal("ExportGraph returned nil")
	}

	goal := g.Attrs["goal"]
	if goal != "Fallback: Uses title and one_liner" {
		t.Errorf("expected fallback goal, got %q", goal)
	}
}

// Additional: nil core still works
func TestExportDOTNilCore(t *testing.T) {
	state := core.NewSpecState()
	state.Core = nil

	result, err := export.ExportDOT(state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Error("ExportDOT should not return empty string even with nil core")
	}
}

// Additional: codergen nodes have prompts
func TestExportDOTCodergenNodesHavePrompts(t *testing.T) {
	state := makeState("Prompts", "test prompts", "test goal")
	task := makeCard("task", "Build the API", "Plan", 1.0, "REST endpoints for users", nil)
	state.Cards.Set(task.CardID, task)

	g := export.ExportGraph(state)
	if g == nil {
		t.Fatal("ExportGraph returned nil")
	}

	hasPrompt := false
	for _, node := range g.Nodes {
		if node.Attrs["type"] == "codergen" && node.Attrs["prompt"] != "" {
			hasPrompt = true
			break
		}
	}
	if !hasPrompt {
		t.Error("at least one codergen node should have a prompt attribute")
	}
}

// Additional: prompt truncation
func TestExportDOTPromptTruncation(t *testing.T) {
	state := makeState("Long", "long prompts", "test truncation")

	// Create a task with a very long body
	longBody := strings.Repeat("This is a very long body text for testing prompt truncation. ", 20)
	task := makeCard("task", "Long task", "Plan", 1.0, longBody, nil)
	state.Cards.Set(task.CardID, task)

	g := export.ExportGraph(state)
	if g == nil {
		t.Fatal("ExportGraph returned nil")
	}

	for _, node := range g.Nodes {
		prompt := node.Attrs["prompt"]
		if len([]rune(prompt)) > 500 {
			t.Errorf("prompt exceeds 500 chars: %d runes on node %s", len([]rune(prompt)), node.ID)
		}
	}
}

// Additional: rankdir is set to TB
func TestExportDOTRankdirTB(t *testing.T) {
	state := makeState("Rankdir", "test rankdir", "test")
	g := export.ExportGraph(state)
	if g == nil {
		t.Fatal("ExportGraph returned nil")
	}

	if g.Attrs["rankdir"] != "TB" {
		t.Errorf("expected rankdir=TB, got %q", g.Attrs["rankdir"])
	}
}

// Additional: ExportDOT produces parseable DOT output
func TestExportDOTProducesSerializedOutput(t *testing.T) {
	state := makeState("Serialize", "test serialization", "test goal")
	task := makeCard("task", "Build it", "Plan", 1.0, "Just build it", nil)
	state.Cards.Set(task.CardID, task)

	result, err := export.ExportDOT(state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.HasPrefix(result, "digraph") {
		t.Errorf("expected DOT output to start with 'digraph', got: %s", result[:min(50, len(result))])
	}
	if !strings.HasSuffix(strings.TrimSpace(result), "}") {
		t.Error("expected DOT output to end with '}'")
	}
}

// Additional: Ideas lane cards are excluded
func TestExportDOTExcludesIdeasLane(t *testing.T) {
	state := makeState("Ideas", "test ideas exclusion", "test")

	// Card in Ideas lane should not influence the pipeline
	ideaCard := makeCard("task", "Raw Idea", "Ideas", 1.0, "unrefined", nil)
	planCard := makeCard("task", "Refined Task", "Plan", 1.0, "refined", nil)

	state.Cards.Set(ideaCard.CardID, ideaCard)
	state.Cards.Set(planCard.CardID, planCard)

	result, err := export.ExportDOT(state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(result, "Raw Idea") {
		t.Error("Ideas lane card should be excluded from pipeline")
	}
	if !strings.Contains(result, "Refined Task") {
		t.Error("Plan lane card should be included in pipeline")
	}
}

// Additional: conditional language detection
func TestExportDOTConditionalLanguage(t *testing.T) {
	state := makeState("Conditional", "test conditions", "test")

	// Task with "if" language should trigger a conditional diamond node
	task := makeCard("task", "If tests pass, deploy to prod", "Plan", 1.0, "Deploy when all tests pass", nil)
	state.Cards.Set(task.CardID, task)

	g := export.ExportGraph(state)
	if g == nil {
		t.Fatal("ExportGraph returned nil")
	}

	hasDiamond := false
	for _, node := range g.Nodes {
		if node.Attrs["shape"] == "diamond" && node.Attrs["type"] == "conditional" {
			hasDiamond = true
			break
		}
	}
	if !hasDiamond {
		t.Error("task with 'if/when' language should produce a conditional diamond node")
	}
}
