// ABOUTME: Dynamic DOT pipeline generation from SpecState, replacing the old fixed 10-node template.
// ABOUTME: Analyzes spec cards to build topology-aware graphs: sequential chains, parallel branches, gates.
package export

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/2389-research/mammoth/dot"
	"github.com/2389-research/mammoth/dot/validator"
	"github.com/2389-research/mammoth/spec/core"
	"github.com/oklog/ulid/v2"
)

// maxPromptLen is the maximum character length for synthesized prompts before truncation.
const maxPromptLen = 500

// conditionalPattern matches task titles or bodies containing conditional language.
var conditionalPattern = regexp.MustCompile(`(?i)\b(if|when)\b`)

// ExportDOT generates a DOT pipeline graph from the spec state.
// The pipeline topology reflects the actual spec structure.
// Returns the DOT string and any validation warnings.
func ExportDOT(state *core.SpecState) (string, error) {
	g := ExportGraph(state)
	diags := validator.Lint(g)

	// Collect only errors (warnings are acceptable)
	var errs []string
	for _, d := range diags {
		if d.Severity == "error" {
			errs = append(errs, d.Message)
		}
	}
	if len(errs) > 0 {
		return "", fmt.Errorf("generated graph has validation errors: %s", strings.Join(errs, "; "))
	}

	return dot.Serialize(g), nil
}

// ExportGraph generates a dot.Graph from the spec state.
// This is the lower-level API; ExportDOT serializes the result.
func ExportGraph(state *core.SpecState) *dot.Graph {
	g := &dot.Graph{
		Name:         graphName(state),
		Nodes:        make(map[string]*dot.Node),
		Attrs:        make(map[string]string),
		NodeDefaults: make(map[string]string),
		EdgeDefaults: make(map[string]string),
	}

	// Graph-level attributes
	g.Attrs["goal"] = goalText(state)
	g.Attrs["rankdir"] = "TB"

	// Start and exit sentinel nodes
	g.AddNode(&dot.Node{
		ID: "start",
		Attrs: map[string]string{
			"shape": "Mdiamond",
			"label": "Start",
			"type":  "start",
		},
	})
	g.AddNode(&dot.Node{
		ID: "exit",
		Attrs: map[string]string{
			"shape": "Msquare",
			"label": "Done",
			"type":  "exit",
		},
	})

	// Extract actionable cards (exclude Ideas lane)
	cards := collectCards(state)
	riskCards := filterCards(cards, func(c core.Card) bool {
		return c.CardType == "risk"
	})
	// All non-risk cards are potential task nodes. The spec builder agents create
	// cards with varied types (idea, task, plan, decision, constraint, etc.) and
	// cards reaching this point have already been filtered to exclude the Ideas
	// lane, so they are promoted/actionable items.
	taskCards := filterCards(cards, func(c core.Card) bool {
		return c.CardType != "risk"
	})

	// Split task cards into ordered segments: groups of regular tasks separated by
	// conditional tasks. This preserves the original card ordering so conditionals
	// appear at their natural position in the pipeline rather than always at the end.
	type segment struct {
		isConditional bool
		cards         []core.Card
	}
	var segments []segment
	for _, tc := range taskCards {
		if hasConditionalLanguage(tc) {
			segments = append(segments, segment{isConditional: true, cards: []core.Card{tc}})
		} else {
			// Accumulate consecutive regular tasks into one segment
			if len(segments) > 0 && !segments[len(segments)-1].isConditional {
				segments[len(segments)-1].cards = append(segments[len(segments)-1].cards, tc)
			} else {
				segments = append(segments, segment{isConditional: false, cards: []core.Card{tc}})
			}
		}
	}

	// Build the pipeline topology based on task relationships
	// Track the "last" set of node IDs that need to connect to the next section
	var lastNodeIDs []string

	if len(segments) == 0 {
		// Empty spec: start -> implement -> exit
		implNode := &dot.Node{
			ID: "implement",
			Attrs: map[string]string{
				"shape":  "box",
				"type":   "codergen",
				"label":  "Implement",
				"prompt": truncatePrompt(fmt.Sprintf("Implement: %s", goalText(state))),
			},
		}
		g.AddNode(implNode)
		g.AddEdge(&dot.Edge{From: "start", To: "implement"})
		lastNodeIDs = []string{"implement"}
	} else {
		// Track counters for unique node ID prefixes across segments
		taskCounter := 0
		condCounter := 0

		lastNodeIDs = []string{"start"}

		for _, seg := range segments {
			if seg.isConditional {
				// Insert a conditional diamond node at this position in the pipeline
				ct := seg.cards[0]
				condID := fmt.Sprintf("condition_%d", condCounter)
				condNode := &dot.Node{
					ID: condID,
					Attrs: map[string]string{
						"shape": "diamond",
						"type":  "conditional",
						"label": truncatePrompt(ct.Title),
					},
				}
				g.AddNode(condNode)

				// Connect from last nodes
				for _, prevID := range lastNodeIDs {
					g.AddEdge(&dot.Edge{From: prevID, To: condID})
				}

				// Conditional has success and fail branches
				// Success goes forward, fail goes to exit
				successID := fmt.Sprintf("cond_impl_%d", condCounter)
				g.AddNode(&dot.Node{
					ID: successID,
					Attrs: map[string]string{
						"shape":  "box",
						"type":   "codergen",
						"label":  truncatePrompt(ct.Title),
						"prompt": synthesizePrompt(ct),
					},
				})
				g.AddEdge(&dot.Edge{
					From:  condID,
					To:    successID,
					Attrs: map[string]string{"label": "success", "condition": "outcome = SUCCESS"},
				})
				g.AddEdge(&dot.Edge{
					From:  condID,
					To:    "exit",
					Attrs: map[string]string{"label": "fail", "condition": "outcome = FAIL"},
				})

				lastNodeIDs = []string{successID}
				condCounter++
			} else {
				// Regular task group: classify into sequential and independent
				sequential, independent := classifyTasks(seg.cards)

				fromID := "start"
				if len(lastNodeIDs) == 1 {
					fromID = lastNodeIDs[0]
				}

				if len(sequential) > 0 && len(independent) == 0 {
					// All tasks in this segment form a sequential chain
					prefix := fmt.Sprintf("task_%d", taskCounter)
					lastNodeIDs = buildSequentialChain(g, sequential, fromID, prefix)
					taskCounter++
				} else if len(independent) > 0 && len(sequential) == 0 {
					// All tasks are independent (no cross-refs). The spec
					// builder's card order is more meaningful than an N-way
					// parallel fan-out, so use sequential chaining.
					prefix := fmt.Sprintf("task_%d", taskCounter)
					lastNodeIDs = buildSequentialChain(g, independent, fromID, prefix)
					taskCounter++
				} else {
					// Mix of sequential and independent within this segment.
					// Chain the sequential tasks first, then append the
					// independent tasks. All run in sequence since the
					// attractor engine doesn't support parallel execution.
					allTasks := make([]core.Card, 0, len(sequential)+len(independent))
					allTasks = append(allTasks, sequential...)
					allTasks = append(allTasks, independent...)
					prefix := fmt.Sprintf("task_%d", taskCounter)
					lastNodeIDs = buildSequentialChain(g, allTasks, fromID, prefix)
					taskCounter++
				}
			}
		}
	}

	// Add verification gate if there are risk cards
	if len(riskCards) > 0 {
		verifyID := "verify_risks"
		riskSummary := summarizeCards(riskCards)
		verifyNode := &dot.Node{
			ID: verifyID,
			Attrs: map[string]string{
				"shape": "diamond",
				"type":  "conditional",
				"label": "Verify Risks",
			},
		}
		g.AddNode(verifyNode)

		for _, prevID := range lastNodeIDs {
			g.AddEdge(&dot.Edge{From: prevID, To: verifyID})
		}

		// Success -> continue, Fail -> remediation codergen -> re-verify
		remediateID := "remediate"
		g.AddNode(&dot.Node{
			ID: remediateID,
			Attrs: map[string]string{
				"shape":  "box",
				"type":   "codergen",
				"label":  "Remediate Risks",
				"prompt": truncatePrompt(fmt.Sprintf("Address risks: %s", riskSummary)),
			},
		})

		g.AddEdge(&dot.Edge{
			From:  verifyID,
			To:    remediateID,
			Attrs: map[string]string{"label": "fail", "condition": "outcome = FAIL"},
		})
		g.AddEdge(&dot.Edge{
			From: remediateID,
			To:   verifyID,
		})

		// The success path from verify_risks continues forward
		// We need a waypoint to avoid verify_risks connecting directly to
		// both the next section and the remediate loop
		successContinueID := "risk_cleared"
		g.AddNode(&dot.Node{
			ID: successContinueID,
			Attrs: map[string]string{
				"shape":  "box",
				"type":   "codergen",
				"label":  "Risk Cleared",
				"prompt": truncatePrompt(fmt.Sprintf("All risks verified. Continue: %s", goalText(state))),
			},
		})
		g.AddEdge(&dot.Edge{
			From:  verifyID,
			To:    successContinueID,
			Attrs: map[string]string{"label": "success", "condition": "outcome = SUCCESS"},
		})
		lastNodeIDs = []string{successContinueID}
	}

	// Connect final nodes to exit
	for _, nodeID := range lastNodeIDs {
		g.AddEdge(&dot.Edge{From: nodeID, To: "exit"})
	}

	g.AssignEdgeIDs()
	return g
}

// graphName derives the graph name from the SpecCore title, falling back to a default.
func graphName(state *core.SpecState) string {
	if state.Core != nil && state.Core.Title != "" {
		return toSnakeCase(state.Core.Title)
	}
	return "pipeline"
}

// goalText extracts the goal from the SpecCore, falling back to title: one_liner.
func goalText(state *core.SpecState) string {
	if state.Core == nil {
		return ""
	}
	if state.Core.Goal != "" {
		return state.Core.Goal
	}
	return fmt.Sprintf("%s: %s", state.Core.Title, state.Core.OneLiner)
}

// collectCards gathers all cards excluding the Ideas lane, sorted by order.
func collectCards(state *core.SpecState) []core.Card {
	var cards []core.Card
	state.Cards.Range(func(_ ulid.ULID, card core.Card) bool {
		if card.Lane != "Ideas" {
			cards = append(cards, card)
		}
		return true
	})
	sort.SliceStable(cards, func(i, j int) bool {
		if cards[i].Order != cards[j].Order {
			return cards[i].Order < cards[j].Order
		}
		return cards[i].CardID.String() < cards[j].CardID.String()
	})
	return cards
}

// filterCards returns cards matching the predicate.
func filterCards(cards []core.Card, pred func(core.Card) bool) []core.Card {
	var result []core.Card
	for _, c := range cards {
		if pred(c) {
			result = append(result, c)
		}
	}
	return result
}

// classifyTasks separates tasks into sequential (have refs forming a chain) and
// independent (no refs to other tasks in the set).
func classifyTasks(tasks []core.Card) (sequential []core.Card, independent []core.Card) {
	if len(tasks) == 0 {
		return nil, nil
	}

	// Build a set of task card IDs for reference lookup
	taskIDs := make(map[string]bool)
	for _, t := range tasks {
		taskIDs[t.CardID.String()] = true
	}

	// A task is "dependent" if it refs another task in the set
	// A task is "depended-on" if another task refs it
	hasInternalRef := make(map[string]bool) // tasks that reference another task
	for _, t := range tasks {
		for _, ref := range t.Refs {
			if taskIDs[ref] {
				hasInternalRef[t.CardID.String()] = true
				break
			}
		}
	}

	// Tasks with internal refs form a chain; tasks without are independent
	for _, t := range tasks {
		id := t.CardID.String()
		isReferenced := false
		for _, other := range tasks {
			for _, ref := range other.Refs {
				if ref == id {
					isReferenced = true
					break
				}
			}
			if isReferenced {
				break
			}
		}

		if hasInternalRef[id] || isReferenced {
			sequential = append(sequential, t)
		} else {
			independent = append(independent, t)
		}
	}

	// Sort sequential tasks by dependency order (topological sort)
	if len(sequential) > 1 {
		sequential = topoSortTasks(sequential)
	}

	return sequential, independent
}

// topoSortTasks performs a topological sort on tasks based on their Refs.
func topoSortTasks(tasks []core.Card) []core.Card {
	taskMap := make(map[string]core.Card)
	for _, t := range tasks {
		taskMap[t.CardID.String()] = t
	}

	// Build adjacency: if task B refs task A, then A must come before B
	inDegree := make(map[string]int)
	dependents := make(map[string][]string) // A -> [B, C] means B,C depend on A
	taskIDs := make(map[string]bool)

	for _, t := range tasks {
		id := t.CardID.String()
		taskIDs[id] = true
		if _, ok := inDegree[id]; !ok {
			inDegree[id] = 0
		}
	}

	for _, t := range tasks {
		id := t.CardID.String()
		for _, ref := range t.Refs {
			if taskIDs[ref] {
				inDegree[id]++
				dependents[ref] = append(dependents[ref], id)
			}
		}
	}

	// Kahn's algorithm
	var queue []string
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}
	sort.Strings(queue) // deterministic

	var sorted []core.Card
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		sorted = append(sorted, taskMap[current])

		deps := dependents[current]
		sort.Strings(deps)
		for _, dep := range deps {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}

	// If topo sort didn't include all tasks (cycle), append remaining in original order
	if len(sorted) < len(tasks) {
		included := make(map[string]bool)
		for _, t := range sorted {
			included[t.CardID.String()] = true
		}
		for _, t := range tasks {
			if !included[t.CardID.String()] {
				sorted = append(sorted, t)
			}
		}
	}

	return sorted
}

// buildSequentialChain adds nodes in sequence, returning the last node ID.
// The prefix parameter controls node ID generation (e.g., "task" produces task_0, task_1).
func buildSequentialChain(g *dot.Graph, tasks []core.Card, fromID string, prefix string) []string {
	prevID := fromID
	for i, t := range tasks {
		nodeID := fmt.Sprintf("%s_%d", prefix, i)
		g.AddNode(&dot.Node{
			ID: nodeID,
			Attrs: map[string]string{
				"shape":  "box",
				"type":   "codergen",
				"label":  truncatePrompt(t.Title),
				"prompt": synthesizePrompt(t),
			},
		})
		g.AddEdge(&dot.Edge{From: prevID, To: nodeID})
		prevID = nodeID
	}
	return []string{prevID}
}

// synthesizePrompt builds a prompt string from a card's title and body.
func synthesizePrompt(card core.Card) string {
	parts := []string{card.Title}
	if card.Body != nil && *card.Body != "" {
		parts = append(parts, *card.Body)
	}
	return truncatePrompt(strings.Join(parts, ": "))
}

// summarizeCards returns a semicolon-separated summary of card titles.
func summarizeCards(cards []core.Card) string {
	titles := make([]string, len(cards))
	for i, c := range cards {
		titles[i] = c.Title
	}
	return strings.Join(titles, "; ")
}

// hasConditionalLanguage returns true if the card's title contains if/when language.
// Only titles are checked — card bodies routinely contain "if"/"when" in code examples
// and natural language descriptions, which would cause false positives.
func hasConditionalLanguage(card core.Card) bool {
	return conditionalPattern.MatchString(card.Title)
}

// truncatePrompt truncates a string to at most maxPromptLen runes.
func truncatePrompt(s string) string {
	runes := []rune(s)
	if len(runes) <= maxPromptLen {
		return s
	}
	return string(runes[:maxPromptLen])
}

// toSnakeCase converts a string to snake_case for use as a DOT identifier.
func toSnakeCase(s string) string {
	var result strings.Builder
	prevWasSeparator := false

	for _, ch := range s {
		if ch >= 'A' && ch <= 'Z' {
			if result.Len() > 0 && !prevWasSeparator {
				result.WriteByte('_')
			}
			result.WriteRune(ch + 32) // toLower
			prevWasSeparator = false
		} else if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') {
			result.WriteRune(ch)
			prevWasSeparator = false
		} else if (ch == ' ' || ch == '-' || ch == '_') && result.Len() > 0 && !prevWasSeparator {
			result.WriteByte('_')
			prevWasSeparator = true
		}
	}

	str := result.String()
	str = strings.TrimRight(str, "_")
	if str == "" {
		return "pipeline"
	}
	return str
}
