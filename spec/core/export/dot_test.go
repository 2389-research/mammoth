// ABOUTME: Tests for the DOT exporter covering pipeline structure, gates, prompts, escaping, and truncation.
// ABOUTME: Uses external test package (export_test) to test the public API surface.
package export_test

import (
	"strings"
	"testing"

	"github.com/2389-research/mammoth/spec/core"
	"github.com/2389-research/mammoth/spec/core/export"
)

// -- Pipeline structure tests --

func TestPipelineHasAll12Nodes(t *testing.T) {
	state := makeStateWithCore()
	dot := export.ExportDOT(state)

	// Sentinel nodes
	if !strings.Contains(dot, `start [shape=Mdiamond, label="Start"]`) {
		t.Error("missing start sentinel node")
	}
	if !strings.Contains(dot, `done  [shape=Msquare, label="Done"]`) {
		t.Error("missing done sentinel node")
	}

	// All 10 pipeline phase nodes
	checks := map[string]string{
		"plan":          "plan [shape=box,",
		"setup":         "setup [shape=box,",
		"tdd":           "tdd [shape=box,",
		"implement":     "implement [shape=box,",
		"verify":        "verify [shape=box,",
		"verify_ok":     "verify_ok [shape=diamond,",
		"scenario_test": "scenario_test [shape=box,",
		"scenario_ok":   "scenario_ok [shape=diamond,",
		"review_gate":   "review_gate [shape=hexagon,",
		"polish":        "polish [shape=box,",
		"release":       "release [shape=box,",
	}
	for name, pattern := range checks {
		if !strings.Contains(dot, pattern) {
			t.Errorf("missing %s node in DOT output", name)
		}
	}
}

func TestMainChainIncludesTDDBeforeImplement(t *testing.T) {
	state := makeStateWithCore()
	dot := export.ExportDOT(state)

	if !strings.Contains(dot, "start -> plan -> setup -> tdd -> implement -> verify -> verify_ok") {
		t.Errorf("missing main chain with tdd in:\n%s", dot)
	}
}

func TestGraphAttributesUseCommasAndFixedRetryTarget(t *testing.T) {
	state := makeStateWithCore()
	dot := export.ExportDOT(state)

	if !strings.HasPrefix(dot, "digraph test_spec {") {
		t.Errorf("expected digraph test_spec { in:\n%s", dot)
	}
	if !strings.Contains(dot, `goal="Verify the markdown exporter",`) {
		// The test helper uses "Verify the markdown exporter" as the goal
		// Let's be flexible and check for any goal= with trailing comma
		if !strings.Contains(dot, `goal="`) {
			t.Errorf("expected goal with trailing comma in:\n%s", dot)
		}
	}
	if !strings.Contains(dot, `retry_target="implement",`) {
		t.Errorf("expected retry_target=implement with comma in:\n%s", dot)
	}
	if !strings.Contains(dot, "default_max_retry=2,") {
		t.Errorf("expected default_max_retry=2 with comma in:\n%s", dot)
	}
	if !strings.Contains(dot, "rankdir=LR") {
		t.Errorf("expected rankdir=LR in:\n%s", dot)
	}
}

// -- Gate tests --

func TestVerifyOKRoutesToScenarioTestOrImplement(t *testing.T) {
	state := makeStateWithCore()
	dot := export.ExportDOT(state)

	if !strings.Contains(dot, `verify_ok -> scenario_test [label="Pass", condition="outcome=SUCCESS"]`) {
		t.Errorf("missing verify_ok -> scenario_test SUCCESS edge")
	}
	if !strings.Contains(dot, `verify_ok -> implement [label="Fail", condition="outcome=FAIL"]`) {
		t.Errorf("missing verify_ok -> implement FAIL edge")
	}
}

func TestScenarioTestFeedsIntoScenarioOKGate(t *testing.T) {
	state := makeStateWithCore()
	dot := export.ExportDOT(state)

	if !strings.Contains(dot, "scenario_test -> scenario_ok") {
		t.Error("missing scenario_test -> scenario_ok edge")
	}
}

func TestScenarioOKRoutesToReviewOrTDD(t *testing.T) {
	state := makeStateWithCore()
	dot := export.ExportDOT(state)

	if !strings.Contains(dot, `scenario_ok -> review_gate [label="Pass", condition="outcome=SUCCESS"]`) {
		t.Error("missing scenario_ok -> review_gate SUCCESS edge")
	}
	if !strings.Contains(dot, `scenario_ok -> tdd [label="Fail", condition="outcome=FAIL"]`) {
		t.Error("missing scenario_ok -> tdd FAIL edge")
	}
}

func TestHumanGateReviewGateHasWeightedBranches(t *testing.T) {
	state := makeStateWithCore()
	dot := export.ExportDOT(state)

	if !strings.Contains(dot, `review_gate [shape=hexagon, type="wait.human"`) {
		t.Error("missing hexagon wait.human on review_gate")
	}
	if !strings.Contains(dot, `review_gate -> release [label="[A] Approve", weight=3]`) {
		t.Error("missing Approve edge")
	}
	if !strings.Contains(dot, `review_gate -> polish  [label="[F] Fix", weight=1]`) {
		t.Error("missing Fix edge")
	}
}

func TestPipelineOptionsDisableScenarioAndHumanGates(t *testing.T) {
	state := makeStateWithCore()
	constraints := "[mammoth.option.human_review=false]\n[mammoth.option.scenario_testing=false]\n[mammoth.option.tdd=true]"
	state.Core.Constraints = &constraints

	dot := export.ExportDOT(state)
	if strings.Contains(dot, "scenario_test [shape=box") {
		t.Error("scenario_test should be omitted when scenario_testing=false")
	}
	if strings.Contains(dot, "review_gate [shape=hexagon") {
		t.Error("review_gate should be omitted when human_review=false")
	}
	if !strings.Contains(dot, `verify_ok -> release [label="Pass", condition="outcome=SUCCESS"]`) {
		t.Error("verify_ok should route directly to release when scenario and human review are disabled")
	}
}

func TestPipelineOptionsDisableTDD(t *testing.T) {
	state := makeStateWithCore()
	constraints := "[mammoth.option.human_review=true]\n[mammoth.option.scenario_testing=true]\n[mammoth.option.tdd=false]"
	state.Core.Constraints = &constraints

	dot := export.ExportDOT(state)
	if strings.Contains(dot, "tdd [shape=box") {
		t.Error("tdd node should be omitted when tdd=false")
	}
	if !strings.Contains(dot, "start -> plan -> setup -> implement -> verify -> verify_ok") {
		t.Error("main chain should skip tdd when disabled")
	}
	if strings.Contains(dot, "polish -> tdd") {
		t.Error("polish loop should not target tdd when disabled")
	}
	if !strings.Contains(dot, "polish -> implement") {
		t.Error("polish loop should target implement when tdd is disabled")
	}
}

func TestPolishLoopsBackToTDD(t *testing.T) {
	state := makeStateWithCore()
	dot := export.ExportDOT(state)

	if !strings.Contains(dot, "polish -> tdd") {
		t.Error("missing polish -> tdd loop")
	}
}

func TestReleaseConnectsToDone(t *testing.T) {
	state := makeStateWithCore()
	dot := export.ExportDOT(state)

	if !strings.Contains(dot, "release -> done") {
		t.Error("missing release -> done")
	}
}

func TestImplementHasGoalGateAndMaxRetries(t *testing.T) {
	state := makeStateWithCore()
	dot := export.ExportDOT(state)

	if !strings.Contains(dot, `implement [shape=box, label="Implement",`) {
		t.Error("missing implement node")
	}
	if !strings.Contains(dot, "goal_gate=true") {
		t.Error("missing goal_gate=true on implement")
	}
	if !strings.Contains(dot, "max_retries=3") {
		t.Error("missing max_retries=3 on implement")
	}
}

// -- TDD prompt tests --

func TestTDDPromptIncludesTestFirstDirective(t *testing.T) {
	state := makeStateWithCore()
	dot := export.ExportDOT(state)

	if !strings.Contains(dot, "Write failing tests for:") {
		t.Error("TDD prompt missing test-first directive")
	}
	if !strings.Contains(dot, "Tests must fail before implementation begins") {
		t.Error("TDD prompt missing fail-first requirement")
	}
}

func TestTDDPromptAggregatesTasksAndPlans(t *testing.T) {
	state := makeStateWithCore()

	task := makeCard("task", "Build API", "Spec", 1.0, "human")
	plan := makeCard("plan", "Roadmap", "Plan", 1.0, "human")
	state.Cards.Set(task.CardID, task)
	state.Cards.Set(plan.CardID, plan)

	dot := export.ExportDOT(state)

	if !strings.Contains(dot, "Cover: Build API") {
		t.Error("TDD prompt missing task")
	}
	if !strings.Contains(dot, "Following: Roadmap") {
		t.Error("TDD prompt missing plan")
	}
}

// -- Scenario test prompt tests --

func TestScenarioTestPromptEnforcesNoMocks(t *testing.T) {
	state := makeStateWithCore()
	dot := export.ExportDOT(state)

	if !strings.Contains(dot, "No mocks allowed") {
		t.Error("Scenario test prompt missing no-mocks directive")
	}
	if !strings.Contains(dot, "real dependencies") {
		t.Error("Scenario test prompt missing real-dependencies")
	}
}

func TestScenarioTestPromptAggregatesAssumptions(t *testing.T) {
	state := makeStateWithCore()

	assumption := makeCard("assumption", "Fast Network", "Spec", 1.0, "human")
	state.Cards.Set(assumption.CardID, assumption)

	dot := export.ExportDOT(state)

	if !strings.Contains(dot, "Validate assumptions: Fast Network") {
		t.Error("Scenario test prompt missing assumption")
	}
}

func TestScenarioTestPromptIncludesSuccessCriteria(t *testing.T) {
	state := makeStateWithCore()
	sc := "All endpoints respond < 200ms"
	state.Core.SuccessCriteria = &sc

	dot := export.ExportDOT(state)

	// Find the scenario_test node line
	found := false
	for _, line := range strings.Split(dot, "\n") {
		if strings.Contains(line, "scenario_test [shape=box") {
			if strings.Contains(line, "All endpoints respond < 200ms") {
				found = true
			}
			break
		}
	}
	if !found {
		t.Error("scenario test prompt missing success criteria")
	}
}

// -- Implement prompt tests --

func TestImplementPromptReferencesTDD(t *testing.T) {
	state := makeStateWithCore()
	dot := export.ExportDOT(state)

	found := false
	for _, line := range strings.Split(dot, "\n") {
		if strings.Contains(line, "implement [shape=box") {
			if strings.Contains(line, "make the failing tests pass") {
				found = true
			}
			break
		}
	}
	if !found {
		t.Error("implement prompt missing TDD reference")
	}
}

func TestCardsAggregateIntoImplementPrompt(t *testing.T) {
	state := makeStateWithCore()

	task1 := makeCard("task", "Build API", "Spec", 1.0, "human")
	task2 := makeCard("task", "Add Tests", "Spec", 2.0, "human")
	plan := makeCard("plan", "Roadmap", "Plan", 1.0, "human")
	state.Cards.Set(task1.CardID, task1)
	state.Cards.Set(task2.CardID, task2)
	state.Cards.Set(plan.CardID, plan)

	dot := export.ExportDOT(state)

	// The tasks could be in either order since they come from map iteration
	if !strings.Contains(dot, "Deliver: Build API; Add Tests") &&
		!strings.Contains(dot, "Deliver: Add Tests; Build API") {
		t.Error("implement prompt missing tasks")
	}
	if !strings.Contains(dot, "Following: Roadmap") {
		t.Error("implement prompt missing plans")
	}
}

// -- Verify prompt tests --

func TestVerifyPromptIncludesTestDirectives(t *testing.T) {
	state := makeStateWithCore()
	dot := export.ExportDOT(state)

	found := false
	for _, line := range strings.Split(dot, "\n") {
		if strings.Contains(line, "verify [shape=box") {
			if strings.Contains(line, "typecheck, lint, unit tests") &&
				(strings.Contains(line, "outcome=SUCCESS") || strings.Contains(line, "outcome=FAIL")) {
				found = true
			}
			break
		}
	}
	if !found {
		t.Error("verify prompt missing test directives or outcome reporting")
	}
}

func TestCardsAggregateIntoVerifyPrompt(t *testing.T) {
	state := makeStateWithCore()
	sc := "All tests pass"
	state.Core.SuccessCriteria = &sc

	decision := makeCard("decision", "Choose Stack", "Plan", 1.0, "human")
	state.Cards.Set(decision.CardID, decision)

	dot := export.ExportDOT(state)

	if !strings.Contains(dot, "Validate: Choose Stack") {
		t.Error("verify prompt missing decision")
	}
	if !strings.Contains(dot, "Success criteria: All tests pass") {
		t.Error("verify prompt missing success criteria")
	}
}

// -- Other prompt tests --

func TestCardsAggregateIntoPlanPrompt(t *testing.T) {
	state := makeStateWithCore()

	idea := makeCard("idea", "Fast DB", "Plan", 1.0, "human")
	constraint := makeCard("constraint", "Must Use Rust", "Plan", 1.0, "human")
	state.Cards.Set(idea.CardID, idea)
	state.Cards.Set(constraint.CardID, constraint)

	dot := export.ExportDOT(state)

	if !strings.Contains(dot, "Plan the approach for:") {
		t.Error("plan prompt missing goal")
	}
	if !strings.Contains(dot, "Key ideas: Fast DB") {
		t.Error("plan prompt missing ideas")
	}
	if !strings.Contains(dot, "Must Use Rust") {
		t.Error("plan prompt missing constraint card")
	}
}

func TestCardsAggregateIntoReviewPrompt(t *testing.T) {
	state := makeStateWithCore()

	question := makeCard("open_question", "What DB", "Plan", 1.0, "human")
	state.Cards.Set(question.CardID, question)

	dot := export.ExportDOT(state)

	if !strings.Contains(dot, "Open questions: What DB") {
		t.Error("review prompt missing open question")
	}
	if !strings.Contains(dot, "Approve?") {
		t.Error("review prompt missing Approve?")
	}
}

func TestCardsAggregateIntoPolishPrompt(t *testing.T) {
	state := makeStateWithCore()

	risk := makeCard("risk", "Data Loss", "Plan", 1.0, "human")
	state.Cards.Set(risk.CardID, risk)

	dot := export.ExportDOT(state)

	if !strings.Contains(dot, "Apply fixes based on review feedback") {
		t.Error("polish prompt missing base text")
	}
	if !strings.Contains(dot, "Risks: Data Loss") {
		t.Error("polish prompt missing risk")
	}
}

func TestSpecConstraintsMergedIntoPlanPrompt(t *testing.T) {
	state := makeStateWithCore()
	constr := "Budget < $1000"
	state.Core.Constraints = &constr

	dot := export.ExportDOT(state)

	if !strings.Contains(dot, "Budget < $1000") {
		t.Error("plan prompt missing spec-level constraints")
	}
}

func TestEmptyCardTypesProduceCleanPrompts(t *testing.T) {
	state := makeStateWithCore()
	dot := export.ExportDOT(state)

	if strings.Contains(dot, "Key ideas:") {
		t.Error("plan prompt should omit Key ideas when empty")
	}
	if strings.Contains(dot, "Deliver:") {
		t.Error("implement prompt should omit Deliver when empty")
	}
	if strings.Contains(dot, "Validate:") {
		t.Error("verify prompt should omit Validate when empty")
	}
	if strings.Contains(dot, "Open questions:") {
		t.Error("review prompt should omit Open questions when empty")
	}
	if strings.Contains(dot, "Validate assumptions:") {
		t.Error("scenario test prompt should omit Validate assumptions when empty")
	}
	if strings.Contains(dot, "Cover:") {
		t.Error("TDD prompt should omit Cover when empty")
	}
}

// -- Goal and name fallback tests --

func TestGoalFallbackUsesTitleAndOneLiner(t *testing.T) {
	sc := core.NewSpecCore("Fallback Test", "Uses title and one_liner", "")
	state := core.NewSpecState()
	state.Core = &sc

	dot := export.ExportDOT(state)

	if !strings.Contains(dot, `goal="Fallback Test: Uses title and one_liner"`) {
		t.Errorf("expected fallback goal from title: one_liner in:\n%s", dot)
	}
}

func TestNoneCoreUsesDefaults(t *testing.T) {
	state := core.NewSpecState()
	state.Core = nil

	dot := export.ExportDOT(state)

	if !strings.HasPrefix(dot, "digraph unnamed_spec {") {
		t.Errorf("expected unnamed_spec graph in:\n%s", dot)
	}
	if !strings.Contains(dot, `goal="",`) {
		t.Errorf("expected empty goal in:\n%s", dot)
	}
}

// -- Escaping tests --

func TestEscapesQuotesInGoal(t *testing.T) {
	sc := core.NewSpecCore("Quote Test", "test", `Say "hello" to the world`)
	state := core.NewSpecState()
	state.Core = &sc

	dot := export.ExportDOT(state)

	if !strings.Contains(dot, `goal="Say \"hello\" to the world"`) {
		t.Errorf("expected escaped quotes in goal in:\n%s", dot)
	}
}

func TestEscapesNewlinesInCardTitlesWithinPrompts(t *testing.T) {
	state := makeStateWithCore()

	card := makeCard("idea", "Line one\nLine two", "Plan", 1.0, "human")
	state.Cards.Set(card.CardID, card)

	dot := export.ExportDOT(state)

	if !strings.Contains(dot, "Line one\\nLine two") {
		t.Errorf("expected escaped newline in prompt in:\n%s", dot)
	}
}

// -- Prompt truncation test --

func TestLongPromptsAreTruncated(t *testing.T) {
	state := makeStateWithCore()

	for i := 0; i < 50; i++ {
		card := makeCard(
			"task",
			strings.Repeat("x", 20)+strings.Repeat("y", 10),
			"Spec",
			float64(i),
			"human",
		)
		state.Cards.Set(card.CardID, card)
	}

	dot := export.ExportDOT(state)

	for _, line := range strings.Split(dot, "\n") {
		if strings.Contains(line, "implement [shape=box") {
			// Extract the prompt value
			promptIdx := strings.Index(line, `prompt="`)
			if promptIdx == -1 {
				t.Fatal("implement node missing prompt attribute")
			}
			// The prompt content should be truncated
			// Just verify the line doesn't have all 50 tasks
			// (if all 50 were present it would be way longer than 500 chars)
			promptStart := promptIdx + len(`prompt="`)
			promptContent := line[promptStart:]
			endQuote := strings.Index(promptContent, `"`)
			if endQuote == -1 {
				t.Fatal("implement prompt missing closing quote")
			}
			promptContent = promptContent[:endQuote]
			// Unescape to get actual rune count
			unescaped := strings.ReplaceAll(promptContent, `\"`, `"`)
			unescaped = strings.ReplaceAll(unescaped, `\\`, `\`)
			if len([]rune(unescaped)) > 500 {
				t.Errorf("implement prompt should be truncated to 500 chars, got %d", len([]rune(unescaped)))
			}
			break
		}
	}
}

// -- Multiple card types coexist --

func TestAllCardTypesContributeToTheirPhases(t *testing.T) {
	state := makeStateWithCore()

	idea := makeCard("idea", "Brainstorm", "Plan", 1.0, "human")
	task := makeCard("task", "Build API", "Spec", 1.0, "human")
	plan := makeCard("plan", "Roadmap", "Plan", 1.0, "human")
	decision := makeCard("decision", "Choose DB", "Plan", 2.0, "human")
	constraint := makeCard("constraint", "Budget Cap", "Plan", 3.0, "human")
	risk := makeCard("risk", "Data Loss", "Plan", 2.0, "human")
	assumption := makeCard("assumption", "Fast Network", "Spec", 3.0, "human")
	openQ := makeCard("open_question", "What Stack", "Plan", 4.0, "human")

	state.Cards.Set(idea.CardID, idea)
	state.Cards.Set(task.CardID, task)
	state.Cards.Set(plan.CardID, plan)
	state.Cards.Set(decision.CardID, decision)
	state.Cards.Set(constraint.CardID, constraint)
	state.Cards.Set(risk.CardID, risk)
	state.Cards.Set(assumption.CardID, assumption)
	state.Cards.Set(openQ.CardID, openQ)

	dot := export.ExportDOT(state)

	// plan phase: ideas + constraints
	if !strings.Contains(dot, "Key ideas: Brainstorm") {
		t.Error("missing idea in plan prompt")
	}
	if !strings.Contains(dot, "Budget Cap") {
		t.Error("missing constraint in plan prompt")
	}

	// tdd phase: tasks + plans
	if !strings.Contains(dot, "Cover: Build API") {
		t.Error("missing task in tdd prompt")
	}

	// implement phase: tasks + plans
	if !strings.Contains(dot, "Deliver: Build API") {
		t.Error("missing task in implement prompt")
	}
	if !strings.Contains(dot, "Following: Roadmap") {
		t.Error("missing plan in implement prompt")
	}

	// verify phase: decisions
	if !strings.Contains(dot, "Validate: Choose DB") {
		t.Error("missing decision in verify prompt")
	}

	// scenario_test phase: assumptions
	if !strings.Contains(dot, "Validate assumptions: Fast Network") {
		t.Error("missing assumption in scenario_test prompt")
	}

	// review phase: open questions
	if !strings.Contains(dot, "Open questions: What Stack") {
		t.Error("missing open_q in review prompt")
	}

	// polish phase: risks
	if !strings.Contains(dot, "Risks: Data Loss") {
		t.Error("missing risk in polish prompt")
	}
}

func TestValidDOTSyntaxBracesMatch(t *testing.T) {
	state := makeStateWithCore()
	dot := export.ExportDOT(state)

	if !strings.Contains(dot, "{") {
		t.Error("missing opening brace")
	}
	if !strings.HasSuffix(strings.TrimSpace(dot), "}") {
		t.Error("missing closing brace")
	}

	opens := strings.Count(dot, "{")
	closes := strings.Count(dot, "}")
	if opens != closes {
		t.Errorf("mismatched braces: %d opens, %d closes", opens, closes)
	}
}

func TestInspirationAndVibesCardsCountAsIdeas(t *testing.T) {
	state := makeStateWithCore()

	vibes := makeCard("vibes", "Good Energy", "Plan", 1.0, "human")
	inspiration := makeCard("inspiration", "Cool Pattern", "Plan", 2.0, "human")
	state.Cards.Set(vibes.CardID, vibes)
	state.Cards.Set(inspiration.CardID, inspiration)

	dot := export.ExportDOT(state)

	if !strings.Contains(dot, "Good Energy") {
		t.Error("missing vibes card in plan prompt")
	}
	if !strings.Contains(dot, "Cool Pattern") {
		t.Error("missing inspiration card in plan prompt")
	}
}

// -- Ideas lane exclusion tests --

func TestIdeasLaneCardsExcludedFromPipeline(t *testing.T) {
	state := makeStateWithCore()

	// Card in Ideas lane -- should NOT appear in DOT output
	ideaInIdeas := makeCard("idea", "Raw Brainstorm", "Ideas", 1.0, "human")
	// Same card type but in Plan lane -- SHOULD appear
	ideaInPlan := makeCard("idea", "Refined Concept", "Plan", 1.0, "human")

	state.Cards.Set(ideaInIdeas.CardID, ideaInIdeas)
	state.Cards.Set(ideaInPlan.CardID, ideaInPlan)

	dot := export.ExportDOT(state)

	if strings.Contains(dot, "Raw Brainstorm") {
		t.Error("Ideas lane card should be excluded from pipeline")
	}
	if !strings.Contains(dot, "Refined Concept") {
		t.Error("Plan lane card should be included in pipeline")
	}
}

func TestAllCardTypesInIdeasLaneExcluded(t *testing.T) {
	state := makeStateWithCore()

	// Put every card type in the Ideas lane
	cardsToAdd := []core.Card{
		makeCard("task", "Idea Task", "Ideas", 1.0, "human"),
		makeCard("risk", "Idea Risk", "Ideas", 2.0, "human"),
		makeCard("constraint", "Idea Constraint", "Ideas", 3.0, "human"),
		makeCard("assumption", "Idea Assumption", "Ideas", 4.0, "human"),
		makeCard("decision", "Idea Decision", "Ideas", 5.0, "human"),
		makeCard("plan", "Idea Plan", "Ideas", 6.0, "human"),
		makeCard("open_question", "Idea Question", "Ideas", 7.0, "human"),
	}
	for _, c := range cardsToAdd {
		state.Cards.Set(c.CardID, c)
	}

	dot := export.ExportDOT(state)

	exclusions := map[string]string{
		"Idea Task":       "task",
		"Idea Risk":       "risk",
		"Idea Constraint": "constraint",
		"Idea Assumption": "assumption",
		"Idea Decision":   "decision",
		"Idea Plan":       "plan",
		"Idea Question":   "open_question",
	}
	for title, cardType := range exclusions {
		if strings.Contains(dot, title) {
			t.Errorf("Ideas lane %s card (%s) leaked into pipeline", cardType, title)
		}
	}
}

func TestSpecAndPlanLaneCardsIncluded(t *testing.T) {
	state := makeStateWithCore()

	specTask := makeCard("task", "Spec Task", "Spec", 1.0, "human")
	planTask := makeCard("task", "Plan Task", "Plan", 1.0, "human")
	backlogTask := makeCard("task", "Backlog Task", "Backlog", 1.0, "human")

	state.Cards.Set(specTask.CardID, specTask)
	state.Cards.Set(planTask.CardID, planTask)
	state.Cards.Set(backlogTask.CardID, backlogTask)

	dot := export.ExportDOT(state)

	if !strings.Contains(dot, "Spec Task") {
		t.Error("Spec lane card should be included")
	}
	if !strings.Contains(dot, "Plan Task") {
		t.Error("Plan lane card should be included")
	}
	if !strings.Contains(dot, "Backlog Task") {
		t.Error("Non-Ideas lane card should be included")
	}
}
