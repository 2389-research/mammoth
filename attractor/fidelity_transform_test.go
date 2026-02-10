// ABOUTME: Tests for fidelity-based context compaction and preamble generation.
// ABOUTME: Covers all six fidelity modes, FidelityOptions, and GeneratePreamble output.
package attractor

import (
	"fmt"
	"strings"
	"testing"
)

func TestApplyFidelity_FullMode(t *testing.T) {
	pctx := NewContext()
	pctx.Set("key1", "value1")
	pctx.Set("key2", "value2")
	pctx.Set("_internal", "secret")
	pctx.AppendLog("log entry 1")

	result, preamble := ApplyFidelity(pctx, FidelityFull, FidelityOptions{})

	// Full mode preserves everything, no preamble
	if preamble != "" {
		t.Errorf("expected empty preamble for full mode, got %q", preamble)
	}

	snap := result.Snapshot()
	if len(snap) != 3 {
		t.Errorf("expected 3 keys preserved, got %d", len(snap))
	}
	if snap["key1"] != "value1" {
		t.Errorf("expected key1=value1, got %v", snap["key1"])
	}
	if snap["_internal"] != "secret" {
		t.Errorf("expected _internal preserved in full mode, got %v", snap["_internal"])
	}

	logs := result.Logs()
	if len(logs) != 1 {
		t.Errorf("expected 1 log entry preserved, got %d", len(logs))
	}
}

func TestApplyFidelity_FullMode_ReturnsOriginalContext(t *testing.T) {
	pctx := NewContext()
	pctx.Set("x", "y")

	result, _ := ApplyFidelity(pctx, FidelityFull, FidelityOptions{})

	// Full mode should return the same context object (not a clone)
	result.Set("x", "modified")
	if pctx.Get("x") != "modified" {
		t.Error("full mode should return the original context, not a copy")
	}
}

func TestApplyFidelity_TruncateMode_DefaultLimit(t *testing.T) {
	pctx := NewContext()
	// Add 60 keys
	for i := 0; i < 60; i++ {
		pctx.Set(fmt.Sprintf("key_%03d", i), fmt.Sprintf("val_%d", i))
	}

	result, preamble := ApplyFidelity(pctx, FidelityTruncate, FidelityOptions{})

	snap := result.Snapshot()
	if len(snap) != 50 {
		t.Errorf("expected 50 keys after truncation (default), got %d", len(snap))
	}

	if !strings.Contains(preamble, "truncated") {
		t.Errorf("expected preamble to mention truncation, got %q", preamble)
	}
	if !strings.Contains(preamble, "50") {
		t.Errorf("expected preamble to mention limit 50, got %q", preamble)
	}
}

func TestApplyFidelity_TruncateMode_CustomLimit(t *testing.T) {
	pctx := NewContext()
	for i := 0; i < 20; i++ {
		pctx.Set(fmt.Sprintf("key_%03d", i), fmt.Sprintf("val_%d", i))
	}

	result, preamble := ApplyFidelity(pctx, FidelityTruncate, FidelityOptions{MaxKeys: 10})

	snap := result.Snapshot()
	if len(snap) != 10 {
		t.Errorf("expected 10 keys after truncation, got %d", len(snap))
	}

	if !strings.Contains(preamble, "10") {
		t.Errorf("expected preamble to mention limit 10, got %q", preamble)
	}
}

func TestApplyFidelity_TruncateMode_UnderLimit(t *testing.T) {
	pctx := NewContext()
	pctx.Set("a", "1")
	pctx.Set("b", "2")

	result, preamble := ApplyFidelity(pctx, FidelityTruncate, FidelityOptions{MaxKeys: 50})

	snap := result.Snapshot()
	if len(snap) != 2 {
		t.Errorf("expected 2 keys (under limit), got %d", len(snap))
	}

	// Should still produce a preamble indicating truncation mode was applied
	if !strings.Contains(preamble, "truncated") {
		t.Errorf("expected preamble to mention truncation, got %q", preamble)
	}
}

func TestApplyFidelity_TruncateMode_DoesNotModifyOriginal(t *testing.T) {
	pctx := NewContext()
	for i := 0; i < 60; i++ {
		pctx.Set(fmt.Sprintf("key_%03d", i), fmt.Sprintf("val_%d", i))
	}

	_, _ = ApplyFidelity(pctx, FidelityTruncate, FidelityOptions{})

	// Original context should be untouched
	snap := pctx.Snapshot()
	if len(snap) != 60 {
		t.Errorf("original context was modified: expected 60 keys, got %d", len(snap))
	}
}

func TestApplyFidelity_CompactMode(t *testing.T) {
	pctx := NewContext()
	pctx.Set("visible_key", "short value")
	pctx.Set("_internal_key", "should be removed")
	pctx.Set("_another_internal", 42)
	pctx.Set("big_value", strings.Repeat("x", 1500))
	pctx.Set("normal_value", "keep me")

	// Add 25 log entries
	for i := 0; i < 25; i++ {
		pctx.AppendLog(fmt.Sprintf("log %d", i))
	}

	result, preamble := ApplyFidelity(pctx, FidelityCompact, FidelityOptions{})

	snap := result.Snapshot()

	// Internal keys (prefixed with _) should be removed
	if _, ok := snap["_internal_key"]; ok {
		t.Error("expected _internal_key to be removed in compact mode")
	}
	if _, ok := snap["_another_internal"]; ok {
		t.Error("expected _another_internal to be removed in compact mode")
	}

	// Normal key should be preserved
	if snap["visible_key"] != "short value" {
		t.Errorf("expected visible_key to be preserved, got %v", snap["visible_key"])
	}
	if snap["normal_value"] != "keep me" {
		t.Errorf("expected normal_value to be preserved, got %v", snap["normal_value"])
	}

	// Large string value should be truncated
	bigVal, ok := snap["big_value"].(string)
	if !ok {
		t.Fatal("expected big_value to remain a string")
	}
	if bigVal != "[truncated]" {
		t.Errorf("expected big_value to be '[truncated]', got %q", bigVal)
	}

	// Logs should be limited to last 20
	logs := result.Logs()
	if len(logs) != 20 {
		t.Errorf("expected 20 log entries, got %d", len(logs))
	}
	// Should keep the most recent logs
	if logs[0] != "log 5" {
		t.Errorf("expected first kept log to be 'log 5', got %q", logs[0])
	}
	if logs[19] != "log 24" {
		t.Errorf("expected last kept log to be 'log 24', got %q", logs[19])
	}

	if !strings.Contains(preamble, "compacted") {
		t.Errorf("expected preamble to mention compaction, got %q", preamble)
	}
}

func TestApplyFidelity_CompactMode_CustomLimits(t *testing.T) {
	pctx := NewContext()
	pctx.Set("short", "ok")
	pctx.Set("medium", strings.Repeat("m", 600))

	for i := 0; i < 15; i++ {
		pctx.AppendLog(fmt.Sprintf("entry %d", i))
	}

	result, _ := ApplyFidelity(pctx, FidelityCompact, FidelityOptions{
		MaxValueLength: 500,
		MaxLogs:        5,
	})

	snap := result.Snapshot()
	if snap["short"] != "ok" {
		t.Errorf("expected short to be preserved, got %v", snap["short"])
	}
	if snap["medium"] != "[truncated]" {
		t.Errorf("expected medium to be truncated with custom limit, got %v", snap["medium"])
	}

	logs := result.Logs()
	if len(logs) != 5 {
		t.Errorf("expected 5 logs with custom limit, got %d", len(logs))
	}
}

func TestApplyFidelity_CompactMode_NonStringValues(t *testing.T) {
	pctx := NewContext()
	pctx.Set("number", 42)
	pctx.Set("bool", true)
	pctx.Set("slice", []string{"a", "b"})

	result, _ := ApplyFidelity(pctx, FidelityCompact, FidelityOptions{})

	snap := result.Snapshot()
	// Non-string values should be preserved (only string values > limit are truncated)
	if snap["number"] != 42 {
		t.Errorf("expected number preserved, got %v", snap["number"])
	}
	if snap["bool"] != true {
		t.Errorf("expected bool preserved, got %v", snap["bool"])
	}
}

func TestApplyFidelity_SummaryLow(t *testing.T) {
	pctx := NewContext()
	pctx.Set("last_stage", "build")
	pctx.Set("outcome", "success")
	pctx.Set("goal", "compile the code")
	pctx.Set("error", "none")
	pctx.Set("random_key", "should be removed")
	pctx.Set("debug_info", "should be removed")
	pctx.Set("_internal", "should be removed")

	result, preamble := ApplyFidelity(pctx, FidelitySummaryLow, FidelityOptions{})

	snap := result.Snapshot()

	// Only whitelist keys should remain
	if len(snap) != 4 {
		t.Errorf("expected 4 keys in summary:low, got %d: %v", len(snap), snap)
	}
	if snap["last_stage"] != "build" {
		t.Errorf("expected last_stage preserved, got %v", snap["last_stage"])
	}
	if snap["outcome"] != "success" {
		t.Errorf("expected outcome preserved, got %v", snap["outcome"])
	}
	if snap["goal"] != "compile the code" {
		t.Errorf("expected goal preserved, got %v", snap["goal"])
	}
	if snap["error"] != "none" {
		t.Errorf("expected error preserved, got %v", snap["error"])
	}

	if !strings.Contains(preamble, "summarized") {
		t.Errorf("expected preamble to mention summarization, got %q", preamble)
	}
	if !strings.Contains(preamble, "low") {
		t.Errorf("expected preamble to mention 'low' detail, got %q", preamble)
	}
}

func TestApplyFidelity_SummaryLow_MissingWhitelistKeys(t *testing.T) {
	pctx := NewContext()
	pctx.Set("outcome", "success")
	pctx.Set("unrelated", "gone")

	result, _ := ApplyFidelity(pctx, FidelitySummaryLow, FidelityOptions{})

	snap := result.Snapshot()
	if len(snap) != 1 {
		t.Errorf("expected 1 key (only outcome present from whitelist), got %d", len(snap))
	}
	if snap["outcome"] != "success" {
		t.Errorf("expected outcome preserved, got %v", snap["outcome"])
	}
}

func TestApplyFidelity_SummaryMedium(t *testing.T) {
	pctx := NewContext()
	pctx.Set("last_stage", "test")
	pctx.Set("outcome", "success")
	pctx.Set("goal", "run tests")
	pctx.Set("error", "")
	pctx.Set("test_result", "all passed")
	pctx.Set("build_output", "binary created")
	pctx.Set("deploy_status", "pending")
	pctx.Set("random_data", "should be removed")
	pctx.Set("_debug", "should be removed")

	result, preamble := ApplyFidelity(pctx, FidelitySummaryMedium, FidelityOptions{})

	snap := result.Snapshot()

	// Whitelist keys + keys containing result/output/status
	expectedKeys := map[string]bool{
		"last_stage":    true,
		"outcome":       true,
		"goal":          true,
		"error":         true,
		"test_result":   true,
		"build_output":  true,
		"deploy_status": true,
	}

	if len(snap) != len(expectedKeys) {
		t.Errorf("expected %d keys in summary:medium, got %d: %v", len(expectedKeys), len(snap), snap)
	}

	for k := range expectedKeys {
		if _, ok := snap[k]; !ok {
			t.Errorf("expected key %q to be preserved in summary:medium", k)
		}
	}

	if _, ok := snap["random_data"]; ok {
		t.Error("expected random_data to be removed in summary:medium")
	}
	if _, ok := snap["_debug"]; ok {
		t.Error("expected _debug to be removed in summary:medium")
	}

	if !strings.Contains(preamble, "summarized") {
		t.Errorf("expected preamble to mention summarization, got %q", preamble)
	}
	if !strings.Contains(preamble, "medium") {
		t.Errorf("expected preamble to mention 'medium' detail, got %q", preamble)
	}
}

func TestApplyFidelity_SummaryHigh(t *testing.T) {
	pctx := NewContext()
	pctx.Set("key1", "short")
	pctx.Set("key2", strings.Repeat("a", 800))
	pctx.Set("_internal", "preserved in high")
	pctx.Set("number", 42)

	result, preamble := ApplyFidelity(pctx, FidelitySummaryHigh, FidelityOptions{})

	snap := result.Snapshot()

	// All keys should be preserved in summary:high
	if len(snap) != 4 {
		t.Errorf("expected 4 keys in summary:high, got %d", len(snap))
	}

	// Short values should be untouched
	if snap["key1"] != "short" {
		t.Errorf("expected key1=short, got %v", snap["key1"])
	}

	// Long string values should be truncated to 500 chars
	val2, ok := snap["key2"].(string)
	if !ok {
		t.Fatal("expected key2 to be a string")
	}
	if len(val2) != 500 {
		t.Errorf("expected key2 to be truncated to 500 chars, got %d", len(val2))
	}

	// Internal keys preserved
	if snap["_internal"] != "preserved in high" {
		t.Errorf("expected _internal preserved in summary:high, got %v", snap["_internal"])
	}

	// Non-string values preserved
	if snap["number"] != 42 {
		t.Errorf("expected number preserved, got %v", snap["number"])
	}

	if !strings.Contains(preamble, "summarized") {
		t.Errorf("expected preamble to mention summarization, got %q", preamble)
	}
	if !strings.Contains(preamble, "high") {
		t.Errorf("expected preamble to mention 'high' detail, got %q", preamble)
	}
}

func TestApplyFidelity_SummaryHigh_CustomMaxValueLength(t *testing.T) {
	pctx := NewContext()
	pctx.Set("data", strings.Repeat("z", 300))

	result, _ := ApplyFidelity(pctx, FidelitySummaryHigh, FidelityOptions{MaxValueLength: 200})

	snap := result.Snapshot()
	val, ok := snap["data"].(string)
	if !ok {
		t.Fatal("expected data to be a string")
	}
	if len(val) != 200 {
		t.Errorf("expected data to be truncated to 200 chars, got %d", len(val))
	}
}

func TestApplyFidelity_SummaryLow_CustomWhitelist(t *testing.T) {
	pctx := NewContext()
	pctx.Set("custom_key", "keep me")
	pctx.Set("outcome", "success")
	pctx.Set("other", "remove me")

	result, _ := ApplyFidelity(pctx, FidelitySummaryLow, FidelityOptions{
		Whitelist: []string{"custom_key"},
	})

	snap := result.Snapshot()
	if len(snap) != 1 {
		t.Errorf("expected 1 key with custom whitelist, got %d: %v", len(snap), snap)
	}
	if snap["custom_key"] != "keep me" {
		t.Errorf("expected custom_key preserved, got %v", snap["custom_key"])
	}
}

func TestGeneratePreamble(t *testing.T) {
	tests := []struct {
		name        string
		prevNode    string
		mode        FidelityMode
		removedKeys int
		wantContain []string
	}{
		{
			name:        "full mode",
			prevNode:    "build",
			mode:        FidelityFull,
			removedKeys: 0,
			wantContain: []string{"build", "full"},
		},
		{
			name:        "truncate mode",
			prevNode:    "analyze",
			mode:        FidelityTruncate,
			removedKeys: 15,
			wantContain: []string{"analyze", "truncat", "15"},
		},
		{
			name:        "compact mode",
			prevNode:    "deploy",
			mode:        FidelityCompact,
			removedKeys: 8,
			wantContain: []string{"deploy", "compact", "8"},
		},
		{
			name:        "summary low",
			prevNode:    "test",
			mode:        FidelitySummaryLow,
			removedKeys: 20,
			wantContain: []string{"test", "summar", "low", "20"},
		},
		{
			name:        "summary medium",
			prevNode:    "review",
			mode:        FidelitySummaryMedium,
			removedKeys: 10,
			wantContain: []string{"review", "summar", "medium", "10"},
		},
		{
			name:        "summary high",
			prevNode:    "compile",
			mode:        FidelitySummaryHigh,
			removedKeys: 0,
			wantContain: []string{"compile", "summar", "high"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GeneratePreamble(tt.prevNode, tt.mode, tt.removedKeys)
			lower := strings.ToLower(got)
			for _, want := range tt.wantContain {
				if !strings.Contains(lower, strings.ToLower(want)) {
					t.Errorf("GeneratePreamble(%q, %q, %d) = %q, expected to contain %q",
						tt.prevNode, tt.mode, tt.removedKeys, got, want)
				}
			}
		})
	}
}

func TestGeneratePreamble_EmptyPrevNode(t *testing.T) {
	got := GeneratePreamble("", FidelityCompact, 5)
	if got == "" {
		t.Error("expected non-empty preamble even with empty prevNode")
	}
}

func TestApplyFidelity_CompactMode_DoesNotModifyOriginal(t *testing.T) {
	pctx := NewContext()
	pctx.Set("_internal", "secret")
	pctx.Set("visible", "public")
	pctx.Set("big", strings.Repeat("x", 2000))

	_, _ = ApplyFidelity(pctx, FidelityCompact, FidelityOptions{})

	// Original context should be untouched
	snap := pctx.Snapshot()
	if len(snap) != 3 {
		t.Errorf("original context modified: expected 3 keys, got %d", len(snap))
	}
	if snap["_internal"] != "secret" {
		t.Error("original _internal key was modified")
	}
	bigVal := snap["big"].(string)
	if len(bigVal) != 2000 {
		t.Error("original big value was modified")
	}
}

func TestApplyFidelity_SummaryLow_DoesNotModifyOriginal(t *testing.T) {
	pctx := NewContext()
	pctx.Set("outcome", "success")
	pctx.Set("noise", "data")

	_, _ = ApplyFidelity(pctx, FidelitySummaryLow, FidelityOptions{})

	snap := pctx.Snapshot()
	if len(snap) != 2 {
		t.Errorf("original context modified: expected 2 keys, got %d", len(snap))
	}
}

func TestFidelityOptions_Defaults(t *testing.T) {
	// Zero-value FidelityOptions should result in sensible defaults
	opts := FidelityOptions{}

	pctx := NewContext()
	for i := 0; i < 60; i++ {
		pctx.Set(fmt.Sprintf("k%03d", i), "v")
	}

	result, _ := ApplyFidelity(pctx, FidelityTruncate, opts)
	snap := result.Snapshot()

	// Default MaxKeys should be 50
	if len(snap) != 50 {
		t.Errorf("expected default MaxKeys=50, got %d keys", len(snap))
	}
}
