// ABOUTME: Tests for fidelity filtering, focusing on tool call/result pairing repair.
// ABOUTME: Ensures ApplyFidelity never produces orphaned ToolResultsTurns or AssistantTurns with dangling ToolCalls.
package agent

import (
	"testing"
	"time"

	"github.com/2389-research/mammoth/llm"
)

func makeUserTurn(content string) UserTurn {
	return UserTurn{Content: content, Timestamp: time.Now()}
}

func makeAssistantTurn(content string) AssistantTurn {
	return AssistantTurn{Content: content, Timestamp: time.Now()}
}

func makeAssistantTurnWithTools(content string, toolCalls ...llm.ToolCallData) AssistantTurn {
	return AssistantTurn{Content: content, ToolCalls: toolCalls, Timestamp: time.Now()}
}

func makeToolResultsTurn(results ...llm.ToolResult) ToolResultsTurn {
	return ToolResultsTurn{Results: results, Timestamp: time.Now()}
}

func makeSystemTurn(content string) SystemTurn {
	return SystemTurn{Content: content, Timestamp: time.Now()}
}

func TestRepairToolPairing_OrphanedToolResults(t *testing.T) {
	// ToolResultsTurn at the start of the tail (no preceding AssistantTurn) should be dropped
	history := []Turn{
		makeSystemTurn("system"),
		makeUserTurn("hello"),
		// Gap: AssistantTurn with ToolCalls was dropped
		makeToolResultsTurn(llm.ToolResult{ToolCallID: "call_1", Content: "result"}),
		makeAssistantTurn("done"),
	}

	repaired := repairToolPairing(history)

	// Should have 3 turns: system, user, assistant (ToolResultsTurn dropped)
	if len(repaired) != 3 {
		t.Fatalf("expected 3 turns after repair, got %d", len(repaired))
	}
	for _, turn := range repaired {
		if _, ok := turn.(ToolResultsTurn); ok {
			t.Fatal("orphaned ToolResultsTurn should have been dropped")
		}
	}
}

func TestRepairToolPairing_OrphanedAssistantWithToolCalls(t *testing.T) {
	// AssistantTurn with ToolCalls but no following ToolResultsTurn should have ToolCalls stripped
	tc := llm.ToolCallData{ID: "call_1", Name: "read_file", Arguments: []byte(`{}`)}
	history := []Turn{
		makeSystemTurn("system"),
		makeUserTurn("hello"),
		makeAssistantTurnWithTools("thinking...", tc),
		// Gap: ToolResultsTurn was dropped
		makeUserTurn("next question"),
		makeAssistantTurn("answer"),
	}

	repaired := repairToolPairing(history)

	if len(repaired) != 5 {
		t.Fatalf("expected 5 turns after repair, got %d", len(repaired))
	}

	// The assistant turn should have had its ToolCalls stripped
	at, ok := repaired[2].(AssistantTurn)
	if !ok {
		t.Fatalf("turn 2 should be AssistantTurn, got %T", repaired[2])
	}
	if len(at.ToolCalls) != 0 {
		t.Fatalf("expected 0 ToolCalls after repair, got %d", len(at.ToolCalls))
	}
	if at.Content != "thinking..." {
		t.Fatalf("expected content preserved, got %q", at.Content)
	}
}

func TestRepairToolPairing_ValidPairsPreserved(t *testing.T) {
	// Valid AssistantTurn→ToolResultsTurn pairs should be preserved
	tc := llm.ToolCallData{ID: "call_1", Name: "read_file", Arguments: []byte(`{}`)}
	tr := llm.ToolResult{ToolCallID: "call_1", Content: "file contents"}

	history := []Turn{
		makeSystemTurn("system"),
		makeUserTurn("hello"),
		makeAssistantTurnWithTools("reading...", tc),
		makeToolResultsTurn(tr),
		makeAssistantTurn("done"),
	}

	repaired := repairToolPairing(history)

	if len(repaired) != 5 {
		t.Fatalf("expected all 5 turns preserved, got %d", len(repaired))
	}

	// Verify the pair is intact
	at, ok := repaired[2].(AssistantTurn)
	if !ok {
		t.Fatalf("turn 2 should be AssistantTurn, got %T", repaired[2])
	}
	if len(at.ToolCalls) != 1 {
		t.Fatalf("expected 1 ToolCall preserved, got %d", len(at.ToolCalls))
	}

	trt, ok := repaired[3].(ToolResultsTurn)
	if !ok {
		t.Fatalf("turn 3 should be ToolResultsTurn, got %T", repaired[3])
	}
	if len(trt.Results) != 1 {
		t.Fatalf("expected 1 result preserved, got %d", len(trt.Results))
	}
}

func TestRepairToolPairing_EmptyHistory(t *testing.T) {
	repaired := repairToolPairing(nil)
	if repaired != nil {
		t.Fatalf("expected nil for nil input, got %v", repaired)
	}

	repaired = repairToolPairing([]Turn{})
	if len(repaired) != 0 {
		t.Fatalf("expected empty for empty input, got %d turns", len(repaired))
	}
}

func TestApplyFidelity_TruncatePreservesToolPairing(t *testing.T) {
	// Build a history with 20+ turns that includes tool call pairs in the middle
	// that will be at the truncation boundary
	var history []Turn
	history = append(history, makeSystemTurn("system prompt"))
	history = append(history, makeUserTurn("initial question"))
	history = append(history, makeAssistantTurn("initial answer"))

	// Add enough turns to trigger truncation (need >= minTurnsForReduction = 10)
	for i := 0; i < 8; i++ {
		tc := llm.ToolCallData{ID: "call_" + string(rune('a'+i)), Name: "tool", Arguments: []byte(`{}`)}
		tr := llm.ToolResult{ToolCallID: tc.ID, Content: "result"}

		history = append(history, makeUserTurn("question"))
		history = append(history, makeAssistantTurnWithTools("calling tool...", tc))
		history = append(history, makeToolResultsTurn(tr))
		history = append(history, makeAssistantTurn("tool response"))
	}

	result := ApplyFidelity(history, "truncate", 128000)

	// Verify no orphaned ToolResultsTurns
	for i, turn := range result {
		if _, ok := turn.(ToolResultsTurn); ok {
			if i == 0 {
				t.Fatal("ToolResultsTurn at position 0 is orphaned")
			}
			prev, ok := result[i-1].(AssistantTurn)
			if !ok || len(prev.ToolCalls) == 0 {
				t.Fatalf("ToolResultsTurn at position %d has no preceding AssistantTurn with ToolCalls", i)
			}
		}
	}

	// Verify no AssistantTurns with dangling ToolCalls
	for i, turn := range result {
		if at, ok := turn.(AssistantTurn); ok && len(at.ToolCalls) > 0 {
			if i+1 >= len(result) {
				t.Fatalf("AssistantTurn with ToolCalls at position %d is last turn (no ToolResultsTurn follows)", i)
			}
			if _, ok := result[i+1].(ToolResultsTurn); !ok {
				t.Fatalf("AssistantTurn with ToolCalls at position %d not followed by ToolResultsTurn", i)
			}
		}
	}
}

func TestApplyFidelity_CompactPreservesToolPairing(t *testing.T) {
	var history []Turn
	history = append(history, makeSystemTurn("system prompt"))

	for i := 0; i < 12; i++ {
		tc := llm.ToolCallData{ID: "call_" + string(rune('a'+i)), Name: "tool", Arguments: []byte(`{}`)}
		tr := llm.ToolResult{ToolCallID: tc.ID, Content: "result"}

		history = append(history, makeUserTurn("question"))
		history = append(history, makeAssistantTurnWithTools("calling tool...", tc))
		history = append(history, makeToolResultsTurn(tr))
		history = append(history, makeAssistantTurn("response"))
	}

	result := ApplyFidelity(history, "compact", 128000)

	for i, turn := range result {
		if _, ok := turn.(ToolResultsTurn); ok {
			if i == 0 {
				t.Fatal("orphaned ToolResultsTurn at position 0")
			}
			prev, ok := result[i-1].(AssistantTurn)
			if !ok || len(prev.ToolCalls) == 0 {
				t.Fatalf("orphaned ToolResultsTurn at position %d", i)
			}
		}
		if at, ok := turn.(AssistantTurn); ok && len(at.ToolCalls) > 0 {
			if i+1 >= len(result) || func() bool { _, ok := result[i+1].(ToolResultsTurn); return !ok }() {
				t.Fatalf("dangling AssistantTurn with ToolCalls at position %d", i)
			}
		}
	}
}

func TestApplyFidelity_SummaryPreservesToolPairing(t *testing.T) {
	var history []Turn
	history = append(history, makeSystemTurn("system prompt"))

	for i := 0; i < 12; i++ {
		tc := llm.ToolCallData{ID: "call_" + string(rune('a'+i)), Name: "tool", Arguments: []byte(`{}`)}
		tr := llm.ToolResult{ToolCallID: tc.ID, Content: "result"}

		history = append(history, makeUserTurn("question"))
		history = append(history, makeAssistantTurnWithTools("calling tool...", tc))
		history = append(history, makeToolResultsTurn(tr))
		history = append(history, makeAssistantTurn("response"))
	}

	for _, mode := range []string{"summary:low", "summary:medium", "summary:high"} {
		result := ApplyFidelity(history, mode, 128000)

		for i, turn := range result {
			if _, ok := turn.(ToolResultsTurn); ok {
				if i == 0 {
					t.Fatalf("[%s] orphaned ToolResultsTurn at position 0", mode)
				}
				prev, ok := result[i-1].(AssistantTurn)
				if !ok || len(prev.ToolCalls) == 0 {
					t.Fatalf("[%s] orphaned ToolResultsTurn at position %d", mode, i)
				}
			}
			if at, ok := turn.(AssistantTurn); ok && len(at.ToolCalls) > 0 {
				if i+1 >= len(result) || func() bool { _, ok := result[i+1].(ToolResultsTurn); return !ok }() {
					t.Fatalf("[%s] dangling AssistantTurn with ToolCalls at position %d", mode, i)
				}
			}
		}
	}
}
