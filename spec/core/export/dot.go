// ABOUTME: Exports a SpecState as a DOT graph for the DOT Runner constrained runtime DSL.
// ABOUTME: Synthesizes cards into a fixed 10-phase pipeline with TDD and scenario testing gates.
package export

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/2389-research/mammoth/spec/core"
	"github.com/oklog/ulid/v2"
)

// maxPromptLen is the maximum character length for synthesized prompts before truncation.
const maxPromptLen = 500

// ExportDOT exports the spec state as a DOT graph conforming to the DOT Runner
// constrained runtime DSL.
//
// Produces a fixed pipeline of 10 phases with TDD enforcement and
// scenario-driven validation. Card data is aggregated into each phase's
// prompt rather than mapped 1:1 to nodes.
//
// Pipeline:
//
//	start -> plan -> setup -> tdd -> implement -> verify -> verify_ok
//	verify_ok -> scenario_test [Pass] | implement [Fail]
//	scenario_test -> scenario_ok
//	scenario_ok -> review_gate [Pass] | tdd [Fail]
//	review_gate -> release [Approve] | polish [Fix]
//	polish -> tdd
//	release -> done
//
// Card-type-to-phase mapping:
//   - plan: ideas, constraints, spec_constraints
//   - tdd: tasks, plans (write failing tests first)
//   - implement: tasks, plans (make the tests pass)
//   - verify: decisions, success_criteria (run unit/integration tests)
//   - scenario_test: assumptions, success_criteria (real deps, no mocks)
//   - review_gate: open_questions (human must decide)
//   - polish: risks
func ExportDOT(state *core.SpecState) string {
	var out strings.Builder

	graphName := "unnamed_spec"
	if state.Core != nil {
		graphName = toSnakeCase(state.Core.Title)
	}

	goal := ""
	if state.Core != nil {
		if state.Core.Goal == "" {
			goal = fmt.Sprintf("%s: %s", state.Core.Title, state.Core.OneLiner)
		} else {
			goal = state.Core.Goal
		}
	}

	specConstraints := ""
	if state.Core != nil && state.Core.Constraints != nil {
		specConstraints = *state.Core.Constraints
	}
	pipelineOpts, cleanedSpecConstraints := parsePipelineOptions(specConstraints)

	successCriteria := ""
	if state.Core != nil && state.Core.SuccessCriteria != nil {
		successCriteria = *state.Core.SuccessCriteria
	}

	// Collect cards by type, excluding the Ideas lane (unrefined cards
	// should not feed into the pipeline — only Plan/Spec/other lanes).
	var cards []core.Card
	state.Cards.Range(func(_ ulid.ULID, card core.Card) bool {
		if card.Lane != "Ideas" {
			cards = append(cards, card)
		}
		return true
	})

	ideas := filterCardTitles(cards, func(c core.Card) bool {
		return c.CardType == "idea" || c.CardType == "inspiration" || c.CardType == "vibes"
	})
	tasks := filterCardTitles(cards, func(c core.Card) bool {
		return c.CardType == "task"
	})
	plans := filterCardTitles(cards, func(c core.Card) bool {
		return c.CardType == "plan"
	})
	decisions := filterCardTitles(cards, func(c core.Card) bool {
		return c.CardType == "decision"
	})
	constraints := filterCardTitles(cards, func(c core.Card) bool {
		return c.CardType == "constraint"
	})
	risks := filterCardTitles(cards, func(c core.Card) bool {
		return c.CardType == "risk"
	})
	assumptions := filterCardTitles(cards, func(c core.Card) bool {
		return c.CardType == "assumption"
	})
	openQuestions := filterCardTitles(cards, func(c core.Card) bool {
		return c.CardType == "open_question"
	})

	// Build synthesized prompts for each pipeline phase
	planPrompt := buildPlanPrompt(goal, ideas, constraints, cleanedSpecConstraints)
	setupPrompt := buildSetupPrompt(goal)
	tddPrompt := buildTDDPrompt(goal, tasks, plans)
	implementPrompt := buildImplementPrompt(goal, tasks, plans)
	verifyPrompt := buildVerifyPrompt(goal, decisions, successCriteria)
	scenarioTestPrompt := buildScenarioTestPrompt(goal, assumptions, successCriteria)
	reviewPrompt := buildReviewPrompt(goal, openQuestions)
	polishPrompt := buildPolishPrompt(risks)
	releasePrompt := buildReleasePrompt(goal)

	// Graph declaration
	fmt.Fprintf(&out, "digraph %s {\n", graphName)
	fmt.Fprintln(&out, "graph [")
	fmt.Fprintf(&out, "goal=\"%s\",\n", escapeDOTString(goal))
	fmt.Fprintln(&out, "retry_target=\"implement\",")
	fmt.Fprintln(&out, "default_max_retry=2,")
	fmt.Fprintln(&out, "rankdir=LR")
	fmt.Fprintln(&out, "]")
	fmt.Fprintln(&out)

	// Sentinel nodes
	fmt.Fprintln(&out)
	fmt.Fprintln(&out, "start [shape=Mdiamond, label=\"Start\"]")
	fmt.Fprintln(&out, "done  [shape=Msquare, label=\"Done\"]")
	fmt.Fprintln(&out)

	// Pipeline phase nodes
	fmt.Fprintf(&out, "plan [shape=box, label=\"Plan\", prompt=\"%s\"]\n", escapeDOTString(planPrompt))
	fmt.Fprintf(&out, "setup [shape=box, label=\"Setup\", prompt=\"%s\"]\n", escapeDOTString(setupPrompt))
	if pipelineOpts.TDD {
		fmt.Fprintf(&out, "tdd [shape=box, label=\"TDD\", prompt=\"%s\"]\n", escapeDOTString(tddPrompt))
	}
	fmt.Fprintf(&out, "implement [shape=box, label=\"Implement\", prompt=\"%s\", goal_gate=true, max_retries=3]\n", escapeDOTString(implementPrompt))
	fmt.Fprintf(&out, "verify [shape=box, label=\"Verify\", prompt=\"%s\"]\n", escapeDOTString(verifyPrompt))
	fmt.Fprintln(&out, "verify_ok [shape=diamond, label=\"Tests passed?\"]")
	fmt.Fprintln(&out)
	if pipelineOpts.ScenarioTesting {
		fmt.Fprintf(&out, "scenario_test [shape=box, label=\"Scenario Test\", prompt=\"%s\"]\n", escapeDOTString(scenarioTestPrompt))
		fmt.Fprintln(&out, "scenario_ok [shape=diamond, label=\"Scenarios passed?\"]")
		fmt.Fprintln(&out)
	}
	if pipelineOpts.HumanReview {
		fmt.Fprintf(&out, "review_gate [shape=hexagon, type=\"wait.human\", label=\"Review\", prompt=\"%s\"]\n", escapeDOTString(reviewPrompt))
		fmt.Fprintf(&out, "polish [shape=box, label=\"Polish\", prompt=\"%s\"]\n", escapeDOTString(polishPrompt))
	}
	fmt.Fprintf(&out, "release [shape=box, label=\"Release\", prompt=\"%s\"]\n", escapeDOTString(releasePrompt))
	fmt.Fprintln(&out)

	// Edges: main chain (TDD before implement)
	if pipelineOpts.TDD {
		fmt.Fprintln(&out, "start -> plan -> setup -> tdd -> implement -> verify -> verify_ok")
	} else {
		fmt.Fprintln(&out, "start -> plan -> setup -> implement -> verify -> verify_ok")
	}
	fmt.Fprintln(&out)

	// Conditional gate: verify_ok (unit tests)
	if pipelineOpts.ScenarioTesting {
		fmt.Fprintln(&out, "verify_ok -> scenario_test [label=\"Pass\", condition=\"outcome=SUCCESS\"]")
	} else if pipelineOpts.HumanReview {
		fmt.Fprintln(&out, "verify_ok -> review_gate [label=\"Pass\", condition=\"outcome=SUCCESS\"]")
	} else {
		fmt.Fprintln(&out, "verify_ok -> release [label=\"Pass\", condition=\"outcome=SUCCESS\"]")
	}
	fmt.Fprintln(&out, "verify_ok -> implement [label=\"Fail\", condition=\"outcome=FAIL\"]")
	fmt.Fprintln(&out)

	if pipelineOpts.ScenarioTesting {
		// Scenario test flows into its own diamond gate
		fmt.Fprintln(&out, "scenario_test -> scenario_ok")
		fmt.Fprintln(&out)

		// Conditional gate: scenario_ok (real-dependency validation)
		if pipelineOpts.HumanReview {
			fmt.Fprintln(&out, "scenario_ok -> review_gate [label=\"Pass\", condition=\"outcome=SUCCESS\"]")
		} else {
			fmt.Fprintln(&out, "scenario_ok -> release [label=\"Pass\", condition=\"outcome=SUCCESS\"]")
		}
		if pipelineOpts.TDD {
			fmt.Fprintln(&out, "scenario_ok -> tdd [label=\"Fail\", condition=\"outcome=FAIL\"]")
		} else {
			fmt.Fprintln(&out, "scenario_ok -> implement [label=\"Fail\", condition=\"outcome=FAIL\"]")
		}
		fmt.Fprintln(&out)
	}

	if pipelineOpts.HumanReview {
		// Human gate: review_gate
		fmt.Fprintln(&out, "review_gate -> release [label=\"[A] Approve\", weight=3]")
		fmt.Fprintln(&out, "review_gate -> polish  [label=\"[F] Fix\", weight=1]")
		fmt.Fprintln(&out)

		// Retry loop
		if pipelineOpts.TDD {
			fmt.Fprintln(&out, "polish -> tdd")
		} else {
			fmt.Fprintln(&out, "polish -> implement")
		}
	}
	fmt.Fprintln(&out, "release -> done")
	fmt.Fprintln(&out)

	fmt.Fprintln(&out)
	fmt.Fprintln(&out, "}")

	return out.String()
}

// filterCardTitles extracts titles from cards matching the predicate.
func filterCardTitles(cards []core.Card, pred func(core.Card) bool) []string {
	var titles []string
	for _, c := range cards {
		if pred(c) {
			titles = append(titles, c.Title)
		}
	}
	return titles
}

// buildPlanPrompt aggregates ideas and constraints into a planning directive.
func buildPlanPrompt(goal string, ideas, constraints []string, specConstraints string) string {
	parts := []string{fmt.Sprintf("Plan the approach for: %s", goal)}
	if len(ideas) > 0 {
		parts = append(parts, fmt.Sprintf("Key ideas: %s", strings.Join(ideas, "; ")))
	}
	allConstraints := make([]string, len(constraints))
	copy(allConstraints, constraints)
	if specConstraints != "" {
		allConstraints = append(allConstraints, specConstraints)
	}
	if len(allConstraints) > 0 {
		parts = append(parts, fmt.Sprintf("Constraints: %s", strings.Join(allConstraints, "; ")))
	}
	return truncatePrompt(strings.Join(parts, ". "))
}

// buildSetupPrompt creates the setup phase prompt.
func buildSetupPrompt(goal string) string {
	return truncatePrompt(fmt.Sprintf("Set up the project infrastructure for: %s", goal))
}

// buildTDDPrompt aggregates tasks and plans into test-first specifications.
func buildTDDPrompt(goal string, tasks, plans []string) string {
	parts := []string{fmt.Sprintf("Write failing tests for: %s", goal)}
	if len(tasks) > 0 {
		parts = append(parts, fmt.Sprintf("Cover: %s", strings.Join(tasks, "; ")))
	}
	if len(plans) > 0 {
		parts = append(parts, fmt.Sprintf("Following: %s", strings.Join(plans, "; ")))
	}
	parts = append(parts, "Tests must fail before implementation begins.")
	return truncatePrompt(strings.Join(parts, ". "))
}

// buildImplementPrompt aggregates tasks and plans into implementation directives.
func buildImplementPrompt(goal string, tasks, plans []string) string {
	parts := []string{fmt.Sprintf("Implement: %s", goal)}
	if len(tasks) > 0 {
		parts = append(parts, fmt.Sprintf("Deliver: %s", strings.Join(tasks, "; ")))
	}
	if len(plans) > 0 {
		parts = append(parts, fmt.Sprintf("Following: %s", strings.Join(plans, "; ")))
	}
	parts = append(parts, "Write only enough code to make the failing tests pass.")
	return truncatePrompt(strings.Join(parts, ". "))
}

// buildVerifyPrompt aggregates decisions and success criteria into test directives.
func buildVerifyPrompt(goal string, decisions []string, successCriteria string) string {
	parts := []string{fmt.Sprintf("Verify: %s", goal)}
	parts = append(parts, "Run typecheck, lint, unit tests, and integration tests.")
	if len(decisions) > 0 {
		parts = append(parts, fmt.Sprintf("Validate: %s", strings.Join(decisions, "; ")))
	}
	if successCriteria != "" {
		parts = append(parts, fmt.Sprintf("Success criteria: %s", successCriteria))
	}
	parts = append(parts, "Report outcome=SUCCESS if all pass, else outcome=FAIL.")
	return truncatePrompt(strings.Join(parts, ". "))
}

// buildScenarioTestPrompt aggregates assumptions and success criteria into
// real-dependency validation. Enforces: no mocks, real dependencies only.
func buildScenarioTestPrompt(goal string, assumptions []string, successCriteria string) string {
	parts := []string{fmt.Sprintf("Run scenario tests against real dependencies for: %s", goal)}
	parts = append(parts, "No mocks allowed. Exercise real systems end-to-end.")
	if len(assumptions) > 0 {
		parts = append(parts, fmt.Sprintf("Validate assumptions: %s", strings.Join(assumptions, "; ")))
	}
	if successCriteria != "" {
		parts = append(parts, fmt.Sprintf("Success criteria: %s", successCriteria))
	}
	parts = append(parts, "Report outcome=SUCCESS if all scenarios pass, else outcome=FAIL.")
	return truncatePrompt(strings.Join(parts, ". "))
}

// buildReviewPrompt aggregates open questions for the human reviewer.
func buildReviewPrompt(goal string, openQuestions []string) string {
	parts := []string{fmt.Sprintf("Human review: %s", goal)}
	if len(openQuestions) > 0 {
		parts = append(parts, fmt.Sprintf("Open questions: %s", strings.Join(openQuestions, "; ")))
	}
	parts = append(parts, "Approve?")
	return truncatePrompt(strings.Join(parts, ". "))
}

// buildPolishPrompt aggregates risks into fix directives.
func buildPolishPrompt(risks []string) string {
	parts := []string{"Apply fixes based on review feedback."}
	if len(risks) > 0 {
		parts = append(parts, fmt.Sprintf("Risks: %s", strings.Join(risks, "; ")))
	}
	return truncatePrompt(strings.Join(parts, ". "))
}

// buildReleasePrompt creates the release phase prompt.
func buildReleasePrompt(goal string) string {
	return truncatePrompt(fmt.Sprintf("Prepare release: %s", goal))
}

// truncatePrompt truncates a string to at most maxPromptLen characters,
// using rune-safe indexing.
func truncatePrompt(s string) string {
	runes := []rune(s)
	if len(runes) <= maxPromptLen {
		return s
	}
	return string(runes[:maxPromptLen])
}

// toSnakeCase converts a string to snake_case for use as a DOT node identifier.
// Strips non-alphanumeric characters (except underscores), lowercases,
// and replaces spaces with underscores.
func toSnakeCase(s string) string {
	var result strings.Builder
	prevWasSeparator := false

	for _, ch := range s {
		if unicode.IsLetter(ch) || unicode.IsDigit(ch) {
			if unicode.IsUpper(ch) {
				// Insert underscore before uppercase if not at start and previous wasn't separator
				if result.Len() > 0 && !prevWasSeparator {
					// Check if the last rune was lowercase
					str := result.String()
					lastRune, _ := utf8.DecodeLastRuneInString(str)
					if unicode.IsLower(lastRune) {
						result.WriteRune('_')
					}
				}
				result.WriteRune(unicode.ToLower(ch))
			} else {
				result.WriteRune(ch)
			}
			prevWasSeparator = false
		} else if (ch == ' ' || ch == '-' || ch == '_') && result.Len() > 0 && !prevWasSeparator {
			result.WriteRune('_')
			prevWasSeparator = true
		}
		// Skip other characters
	}

	str := result.String()
	// Trim trailing underscore
	str = strings.TrimRight(str, "_")

	if str == "" {
		return "node"
	}
	return str
}

// escapeDOTString escapes a string for use within DOT quoted attributes.
func escapeDOTString(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	return s
}

type pipelineOptions struct {
	HumanReview     bool
	ScenarioTesting bool
	TDD             bool
}

// parsePipelineOptions reads optional mammoth.option.* markers from constraints.
// When absent, defaults preserve historical behavior (all enabled).
// Returns parsed options plus constraints text with markers removed.
func parsePipelineOptions(raw string) (pipelineOptions, string) {
	opts := pipelineOptions{
		HumanReview:     false,
		ScenarioTesting: true,
		TDD:             true,
	}
	if strings.TrimSpace(raw) == "" {
		return opts, ""
	}

	lines := strings.Split(raw, "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[mammoth.option.") && strings.HasSuffix(trimmed, "]") {
			body := strings.TrimSuffix(strings.TrimPrefix(trimmed, "["), "]")
			parts := strings.SplitN(body, "=", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				val := strings.EqualFold(strings.TrimSpace(parts[1]), "true")
				switch key {
				case "mammoth.option.human_review":
					opts.HumanReview = val
				case "mammoth.option.scenario_testing":
					opts.ScenarioTesting = val
				case "mammoth.option.tdd":
					opts.TDD = val
				}
			}
			continue
		}
		if trimmed != "" {
			filtered = append(filtered, trimmed)
		}
	}
	return opts, strings.Join(filtered, "\n")
}
