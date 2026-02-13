// ABOUTME: AgentContext feeds state and history to LLM-backed agents within the swarm.
// ABOUTME: Tracks rolling summary, key decisions, and event cursor for incremental updates.
package agents

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"unicode/utf8"

	"github.com/2389-research/mammoth/spec/core"
	"github.com/oklog/ulid/v2"
)

// RollingSummaryCap is the maximum character length for a rolling summary before compaction.
const RollingSummaryCap = 2000

// MaxKeyDecisions is the maximum number of key decisions to retain per agent.
const MaxKeyDecisions = 50

// AgentContext is the contextual information provided to an agent for each reasoning step.
// Contains the current state summary, recent events, transcript history,
// and the agent's accumulated memory (rolling summary and key decisions).
type AgentContext struct {
	SpecID           ulid.ULID                `json:"spec_id"`
	AgentID          string                   `json:"agent_id"`
	AgentRole        AgentRole                `json:"agent_role"`
	StateSummary     string                   `json:"state_summary"`
	RecentEvents     []core.Event             `json:"recent_events"`
	RecentTranscript []core.TranscriptMessage `json:"recent_transcript"`
	RollingSummary   string                   `json:"rolling_summary"`
	KeyDecisions     []string                 `json:"key_decisions"`
	LastEventSeen    uint64                   `json:"last_event_seen"`
}

// NewAgentContext creates a fresh context for a given agent with no accumulated memory.
func NewAgentContext(specID ulid.ULID, agentID string, role AgentRole) *AgentContext {
	return &AgentContext{
		SpecID:           specID,
		AgentID:          agentID,
		AgentRole:        role,
		RecentEvents:     []core.Event{},
		RecentTranscript: []core.TranscriptMessage{},
		KeyDecisions:     []string{},
	}
}

// UpdateFromEvents processes new events to update the rolling summary and LastEventSeen cursor.
// Events with IDs at or below LastEventSeen are skipped.
func (ctx *AgentContext) UpdateFromEvents(events []core.Event) {
	for i := range events {
		event := &events[i]
		if event.EventID <= ctx.LastEventSeen {
			continue
		}
		ctx.LastEventSeen = event.EventID

		description := fmt.Sprintf("Event #%d: %s", event.EventID, describeEventPayload(event.Payload))

		if ctx.RollingSummary == "" {
			ctx.RollingSummary = description
		} else {
			ctx.RollingSummary += "; " + description
		}
	}

	ctx.CompactSummary()
}

// AddDecision appends a key decision to the bounded decision list.
func (ctx *AgentContext) AddDecision(decision string) {
	ctx.KeyDecisions = append(ctx.KeyDecisions, decision)
	if len(ctx.KeyDecisions) > MaxKeyDecisions {
		excess := len(ctx.KeyDecisions) - MaxKeyDecisions
		ctx.KeyDecisions = ctx.KeyDecisions[excess:]
	}
}

// CompactSummary truncates the rolling summary if it exceeds the character cap,
// keeping the tail and prepending a compaction marker.
func (ctx *AgentContext) CompactSummary() {
	charCount := utf8.RuneCountInString(ctx.RollingSummary)
	if charCount <= RollingSummaryCap {
		return
	}

	prefix := "[earlier context compacted] "
	prefixChars := utf8.RuneCountInString(prefix)
	budget := RollingSummaryCap - prefixChars
	if budget < 0 {
		budget = 0
	}

	// Take the last `budget` characters using rune-safe indexing.
	skip := charCount - budget
	if skip < 0 {
		skip = 0
	}
	runes := []rune(ctx.RollingSummary)
	tail := string(runes[skip:])

	// Find a clean break point (semicolon boundary) within the tail.
	cleanStart := strings.Index(tail, "; ")
	if cleanStart >= 0 {
		tail = tail[cleanStart+2:]
	}

	ctx.RollingSummary = prefix + tail
}

// ToSnapshotValue serializes this context to a JSON value for inclusion in snapshot data.
func (ctx *AgentContext) ToSnapshotValue() json.RawMessage {
	data, err := json.Marshal(ctx)
	if err != nil {
		return json.RawMessage("null")
	}
	return data
}

// FromSnapshotValue restores an AgentContext from a previously-serialized snapshot value.
func FromSnapshotValue(data json.RawMessage) (*AgentContext, error) {
	var ctx AgentContext
	if err := json.Unmarshal(data, &ctx); err != nil {
		return nil, err
	}
	return &ctx, nil
}

// ContextsToSnapshotMap serializes a collection of agent contexts into a map
// suitable for inclusion in snapshot data.
func ContextsToSnapshotMap(contexts []*AgentContext) map[string]json.RawMessage {
	result := make(map[string]json.RawMessage, len(contexts))
	for _, ctx := range contexts {
		result[ctx.AgentID] = ctx.ToSnapshotValue()
	}
	return result
}

// ContextsFromSnapshotMap restores agent contexts from a snapshot map.
// Contexts that fail to deserialize are skipped with a warning.
func ContextsFromSnapshotMap(m map[string]json.RawMessage) []*AgentContext {
	var result []*AgentContext
	for _, data := range m {
		ctx, err := FromSnapshotValue(data)
		if err != nil {
			log.Printf("warning: failed to restore agent context from snapshot: %v", err)
			continue
		}
		result = append(result, ctx)
	}
	return result
}

// truncateChars truncates a string to at most maxChars characters, appending "..." if truncated.
func truncateChars(s string, maxChars int) string {
	runes := []rune(s)
	if len(runes) <= maxChars {
		return s
	}
	return string(runes[:maxChars]) + "..."
}

// describeEventPayload produces a human-readable description of an event payload for rolling summaries.
func describeEventPayload(payload core.EventPayload) string {
	switch p := payload.(type) {
	case core.SpecCreatedPayload:
		return fmt.Sprintf("spec created: '%s'", p.Title)

	case core.SpecCoreUpdatedPayload:
		if p.Title != nil {
			return fmt.Sprintf("spec updated (title -> '%s')", *p.Title)
		}
		return "spec metadata updated"

	case core.CardCreatedPayload:
		return fmt.Sprintf("card created: '%s' (%s)", p.Card.Title, p.Card.CardType)

	case core.CardUpdatedPayload:
		if p.Title != nil {
			return fmt.Sprintf("card %s updated (title -> '%s')", p.CardID, *p.Title)
		}
		return fmt.Sprintf("card %s updated", p.CardID)

	case core.CardMovedPayload:
		return fmt.Sprintf("card %s moved to '%s'", p.CardID, p.Lane)

	case core.CardDeletedPayload:
		return fmt.Sprintf("card %s deleted", p.CardID)

	case core.TranscriptAppendedPayload:
		preview := truncateChars(p.Message.Content, 50)
		return fmt.Sprintf("%s said: %s", p.Message.Sender, preview)

	case core.QuestionAskedPayload:
		return "question asked to user"

	case core.QuestionAnsweredPayload:
		preview := truncateChars(p.Answer, 50)
		return fmt.Sprintf("user answered: %s", preview)

	case core.AgentStepStartedPayload:
		return fmt.Sprintf("agent %s started: %s", p.AgentID, p.Description)

	case core.AgentStepFinishedPayload:
		return fmt.Sprintf("agent %s finished: %s", p.AgentID, p.DiffSummary)

	case core.UndoAppliedPayload:
		return fmt.Sprintf("undo applied to event #%d", p.TargetEventID)

	case core.SnapshotWrittenPayload:
		return fmt.Sprintf("snapshot #%d written", p.SnapshotID)

	default:
		return fmt.Sprintf("unknown event: %T", payload)
	}
}
