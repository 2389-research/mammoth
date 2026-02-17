// ABOUTME: Tests for the streaming response accumulator that consumes LLM stream events.
// ABOUTME: Covers text accumulation, tool call assembly, event emission, context cancellation, and error handling.

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/2389-research/mammoth/llm"
)

// sendEvents is a test helper that sends a sequence of StreamEvents into a channel
// and closes it when done.
func sendEvents(events []llm.StreamEvent) <-chan llm.StreamEvent {
	ch := make(chan llm.StreamEvent, len(events))
	for _, ev := range events {
		ch <- ev
	}
	close(ch)
	return ch
}

func TestConsumeStream_TextOnly(t *testing.T) {
	session := NewSession(DefaultSessionConfig())
	defer session.Close()

	events := []llm.StreamEvent{
		{Type: llm.StreamStart},
		{Type: llm.StreamTextStart},
		{Type: llm.StreamTextDelta, Delta: "Hello"},
		{Type: llm.StreamTextDelta, Delta: ", world!"},
		{Type: llm.StreamTextEnd},
		{Type: llm.StreamFinish, FinishReason: &llm.FinishReason{Reason: llm.FinishStop}, Usage: &llm.Usage{
			InputTokens:  10,
			OutputTokens: 5,
			TotalTokens:  15,
		}},
	}
	ch := sendEvents(events)

	resp, err := consumeStream(context.Background(), session, ch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp == nil {
		t.Fatal("expected non-nil response")
	}

	text := resp.TextContent()
	if text != "Hello, world!" {
		t.Errorf("expected text 'Hello, world!', got %q", text)
	}

	if resp.FinishReason.Reason != llm.FinishStop {
		t.Errorf("expected finish reason %q, got %q", llm.FinishStop, resp.FinishReason.Reason)
	}

	if resp.Usage.InputTokens != 10 {
		t.Errorf("expected input tokens 10, got %d", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 5 {
		t.Errorf("expected output tokens 5, got %d", resp.Usage.OutputTokens)
	}
	if resp.Usage.TotalTokens != 15 {
		t.Errorf("expected total tokens 15, got %d", resp.Usage.TotalTokens)
	}

	// Should have no tool calls
	if len(resp.ToolCalls()) != 0 {
		t.Errorf("expected 0 tool calls, got %d", len(resp.ToolCalls()))
	}
}

func TestConsumeStream_WithToolCalls(t *testing.T) {
	session := NewSession(DefaultSessionConfig())
	defer session.Close()

	events := []llm.StreamEvent{
		{Type: llm.StreamStart},
		{Type: llm.StreamTextStart},
		{Type: llm.StreamTextDelta, Delta: "I'll read the file."},
		{Type: llm.StreamTextEnd},
		{Type: llm.StreamToolStart, ToolCall: &llm.ToolCall{
			ID:   "call-1",
			Name: "read_file",
		}},
		{Type: llm.StreamToolDelta, Delta: `{"file_pa`},
		{Type: llm.StreamToolDelta, Delta: `th": "/tmp/test.go"}`},
		{Type: llm.StreamToolEnd},
		{Type: llm.StreamFinish, FinishReason: &llm.FinishReason{Reason: llm.FinishToolCalls}, Usage: &llm.Usage{
			InputTokens:  50,
			OutputTokens: 30,
			TotalTokens:  80,
		}},
	}
	ch := sendEvents(events)

	resp, err := consumeStream(context.Background(), session, ch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify text content
	text := resp.TextContent()
	if text != "I'll read the file." {
		t.Errorf("expected text \"I'll read the file.\", got %q", text)
	}

	// Verify tool calls
	toolCalls := resp.ToolCalls()
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(toolCalls))
	}

	if toolCalls[0].ID != "call-1" {
		t.Errorf("expected tool call ID 'call-1', got %q", toolCalls[0].ID)
	}
	if toolCalls[0].Name != "read_file" {
		t.Errorf("expected tool call name 'read_file', got %q", toolCalls[0].Name)
	}

	var args map[string]any
	if err := json.Unmarshal(toolCalls[0].Arguments, &args); err != nil {
		t.Fatalf("failed to parse tool call arguments: %v", err)
	}
	if args["file_path"] != "/tmp/test.go" {
		t.Errorf("expected file_path '/tmp/test.go', got %v", args["file_path"])
	}

	if resp.FinishReason.Reason != llm.FinishToolCalls {
		t.Errorf("expected finish reason %q, got %q", llm.FinishToolCalls, resp.FinishReason.Reason)
	}
}

func TestConsumeStream_MultipleToolCalls(t *testing.T) {
	session := NewSession(DefaultSessionConfig())
	defer session.Close()

	events := []llm.StreamEvent{
		{Type: llm.StreamStart},
		{Type: llm.StreamToolStart, ToolCall: &llm.ToolCall{
			ID:   "call-1",
			Name: "read_file",
		}},
		{Type: llm.StreamToolDelta, Delta: `{"path": "a.go"}`},
		{Type: llm.StreamToolEnd},
		{Type: llm.StreamToolStart, ToolCall: &llm.ToolCall{
			ID:   "call-2",
			Name: "write_file",
		}},
		{Type: llm.StreamToolDelta, Delta: `{"path": "b.go", "content": "hi"}`},
		{Type: llm.StreamToolEnd},
		{Type: llm.StreamFinish, FinishReason: &llm.FinishReason{Reason: llm.FinishToolCalls}, Usage: &llm.Usage{
			InputTokens:  100,
			OutputTokens: 60,
			TotalTokens:  160,
		}},
	}
	ch := sendEvents(events)

	resp, err := consumeStream(context.Background(), session, ch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	toolCalls := resp.ToolCalls()
	if len(toolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(toolCalls))
	}

	if toolCalls[0].Name != "read_file" {
		t.Errorf("expected first tool call 'read_file', got %q", toolCalls[0].Name)
	}
	if toolCalls[1].Name != "write_file" {
		t.Errorf("expected second tool call 'write_file', got %q", toolCalls[1].Name)
	}
}

func TestConsumeStream_EventEmission(t *testing.T) {
	session := NewSession(DefaultSessionConfig())
	defer session.Close()

	sub := session.EventEmitter.Subscribe()

	events := []llm.StreamEvent{
		{Type: llm.StreamStart},
		{Type: llm.StreamTextStart},
		{Type: llm.StreamTextDelta, Delta: "Hello"},
		{Type: llm.StreamTextDelta, Delta: " there"},
		{Type: llm.StreamTextEnd},
		{Type: llm.StreamFinish, FinishReason: &llm.FinishReason{Reason: llm.FinishStop}, Usage: &llm.Usage{}},
	}
	ch := sendEvents(events)

	_, err := consumeStream(context.Background(), session, ch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Collect all emitted events with a short timeout
	var emitted []SessionEvent
	timeout := time.After(time.Second)
	for done := false; !done; {
		select {
		case ev, ok := <-sub:
			if !ok {
				done = true
			} else {
				emitted = append(emitted, ev)
			}
		case <-timeout:
			done = true
		}
	}

	// Verify we got at least an EventAssistantTextStart
	hasTextStart := false
	hasDelta := false
	for _, ev := range emitted {
		if ev.Kind == EventAssistantTextStart {
			hasTextStart = true
		}
		if ev.Kind == EventAssistantTextDelta {
			hasDelta = true
		}
	}

	if !hasTextStart {
		t.Error("expected EventAssistantTextStart to be emitted")
	}
	if !hasDelta {
		t.Error("expected EventAssistantTextDelta to be emitted")
	}
}

func TestConsumeStream_DeltaBatching(t *testing.T) {
	session := NewSession(DefaultSessionConfig())
	defer session.Close()

	sub := session.EventEmitter.Subscribe()

	// Create many small deltas that should be batched together
	events := []llm.StreamEvent{
		{Type: llm.StreamStart},
		{Type: llm.StreamTextStart},
	}
	// 10 small deltas of "ab" (20 chars total, under 200 threshold)
	for i := 0; i < 10; i++ {
		events = append(events, llm.StreamEvent{Type: llm.StreamTextDelta, Delta: "ab"})
	}
	events = append(events,
		llm.StreamEvent{Type: llm.StreamTextEnd},
		llm.StreamEvent{Type: llm.StreamFinish, FinishReason: &llm.FinishReason{Reason: llm.FinishStop}, Usage: &llm.Usage{}},
	)
	ch := sendEvents(events)

	resp, err := consumeStream(context.Background(), session, ch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify full text accumulated
	text := resp.TextContent()
	expected := ""
	for i := 0; i < 10; i++ {
		expected += "ab"
	}
	if text != expected {
		t.Errorf("expected text %q, got %q", expected, text)
	}

	// Collect emitted events
	var deltaEvents []SessionEvent
	timeout := time.After(time.Second)
	for done := false; !done; {
		select {
		case ev, ok := <-sub:
			if !ok {
				done = true
			} else if ev.Kind == EventAssistantTextDelta {
				deltaEvents = append(deltaEvents, ev)
			}
		case <-timeout:
			done = true
		}
	}

	// With batching, we should have fewer delta events than raw deltas (10).
	// The final flush at text end should ensure we get at least one.
	if len(deltaEvents) == 0 {
		t.Error("expected at least one batched delta event")
	}
	if len(deltaEvents) >= 10 {
		t.Errorf("expected fewer than 10 delta events due to batching, got %d", len(deltaEvents))
	}
}

func TestConsumeStream_LargeDeltaFlush(t *testing.T) {
	session := NewSession(DefaultSessionConfig())
	defer session.Close()

	sub := session.EventEmitter.Subscribe()

	// Create deltas that exceed 200 chars to trigger flush
	events := []llm.StreamEvent{
		{Type: llm.StreamStart},
		{Type: llm.StreamTextStart},
	}
	// Each delta is 100 chars, so after 3 we've accumulated 300 chars, exceeding 200 threshold
	bigChunk := ""
	for i := 0; i < 100; i++ {
		bigChunk += "x"
	}
	for i := 0; i < 5; i++ {
		events = append(events, llm.StreamEvent{Type: llm.StreamTextDelta, Delta: bigChunk})
	}
	events = append(events,
		llm.StreamEvent{Type: llm.StreamTextEnd},
		llm.StreamEvent{Type: llm.StreamFinish, FinishReason: &llm.FinishReason{Reason: llm.FinishStop}, Usage: &llm.Usage{}},
	)
	ch := sendEvents(events)

	_, err := consumeStream(context.Background(), session, ch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Collect delta events
	var deltaEvents []SessionEvent
	timeout := time.After(time.Second)
	for done := false; !done; {
		select {
		case ev, ok := <-sub:
			if !ok {
				done = true
			} else if ev.Kind == EventAssistantTextDelta {
				deltaEvents = append(deltaEvents, ev)
			}
		case <-timeout:
			done = true
		}
	}

	// With 500 chars total and 200 char threshold, we should get multiple flushes
	if len(deltaEvents) < 2 {
		t.Errorf("expected at least 2 delta events for 500 chars with 200 char threshold, got %d", len(deltaEvents))
	}
}

func TestConsumeStream_ContextCancellation(t *testing.T) {
	session := NewSession(DefaultSessionConfig())
	defer session.Close()

	ctx, cancel := context.WithCancel(context.Background())

	// Create a channel that sends some events then blocks (simulating a slow stream)
	ch := make(chan llm.StreamEvent, 3)
	ch <- llm.StreamEvent{Type: llm.StreamStart}
	ch <- llm.StreamEvent{Type: llm.StreamTextStart}
	ch <- llm.StreamEvent{Type: llm.StreamTextDelta, Delta: "partial"}

	// Cancel the context before the stream finishes
	cancel()

	resp, err := consumeStream(ctx, session, ch)
	if err == nil {
		t.Fatal("expected error from context cancellation")
	}
	if resp != nil {
		t.Error("expected nil response on cancellation")
	}
}

func TestConsumeStream_StreamError(t *testing.T) {
	session := NewSession(DefaultSessionConfig())
	defer session.Close()

	streamErr := fmt.Errorf("provider connection lost")
	events := []llm.StreamEvent{
		{Type: llm.StreamStart},
		{Type: llm.StreamTextStart},
		{Type: llm.StreamTextDelta, Delta: "partial"},
		{Type: llm.StreamErrorEvt, Error: streamErr},
	}
	ch := sendEvents(events)

	resp, err := consumeStream(context.Background(), session, ch)
	if err == nil {
		t.Fatal("expected error from stream error event")
	}
	if resp != nil {
		t.Error("expected nil response on stream error")
	}
	if err.Error() != "stream error: provider connection lost" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestConsumeStream_WithReasoning(t *testing.T) {
	session := NewSession(DefaultSessionConfig())
	defer session.Close()

	events := []llm.StreamEvent{
		{Type: llm.StreamStart},
		{Type: llm.StreamReasonStart},
		{Type: llm.StreamReasonDelta, ReasoningDelta: "Let me think"},
		{Type: llm.StreamReasonDelta, ReasoningDelta: " about this..."},
		{Type: llm.StreamReasonEnd},
		{Type: llm.StreamTextStart},
		{Type: llm.StreamTextDelta, Delta: "Here is my answer."},
		{Type: llm.StreamTextEnd},
		{Type: llm.StreamFinish, FinishReason: &llm.FinishReason{Reason: llm.FinishStop}, Usage: &llm.Usage{
			InputTokens:     20,
			OutputTokens:    10,
			TotalTokens:     30,
			ReasoningTokens: llm.IntPtr(5),
		}},
	}
	ch := sendEvents(events)

	resp, err := consumeStream(context.Background(), session, ch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := resp.TextContent()
	if text != "Here is my answer." {
		t.Errorf("expected text 'Here is my answer.', got %q", text)
	}

	reasoning := resp.Reasoning()
	if reasoning != "Let me think about this..." {
		t.Errorf("expected reasoning 'Let me think about this...', got %q", reasoning)
	}

	if resp.Usage.ReasoningTokens == nil || *resp.Usage.ReasoningTokens != 5 {
		t.Errorf("expected 5 reasoning tokens, got %v", resp.Usage.ReasoningTokens)
	}
}

func TestConsumeStream_EmptyStream(t *testing.T) {
	session := NewSession(DefaultSessionConfig())
	defer session.Close()

	// Channel closed immediately with no events
	ch := make(chan llm.StreamEvent)
	close(ch)

	resp, err := consumeStream(context.Background(), session, ch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should get a valid response with empty content
	if resp == nil {
		t.Fatal("expected non-nil response for empty stream")
	}
	if resp.TextContent() != "" {
		t.Errorf("expected empty text, got %q", resp.TextContent())
	}
}

func TestConsumeStream_FinishWithEmbeddedResponse(t *testing.T) {
	session := NewSession(DefaultSessionConfig())
	defer session.Close()

	// Some providers send a full Response object in the finish event
	embeddedResp := &llm.Response{
		ID:       "resp-abc",
		Model:    "test-model",
		Provider: "test-provider",
	}

	events := []llm.StreamEvent{
		{Type: llm.StreamStart},
		{Type: llm.StreamTextStart},
		{Type: llm.StreamTextDelta, Delta: "Done."},
		{Type: llm.StreamTextEnd},
		{Type: llm.StreamFinish, FinishReason: &llm.FinishReason{Reason: llm.FinishStop}, Usage: &llm.Usage{
			InputTokens:  5,
			OutputTokens: 2,
			TotalTokens:  7,
		}, Response: embeddedResp},
	}
	ch := sendEvents(events)

	resp, err := consumeStream(context.Background(), session, ch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.ID != "resp-abc" {
		t.Errorf("expected response ID 'resp-abc', got %q", resp.ID)
	}
	if resp.Model != "test-model" {
		t.Errorf("expected model 'test-model', got %q", resp.Model)
	}
	if resp.Provider != "test-provider" {
		t.Errorf("expected provider 'test-provider', got %q", resp.Provider)
	}

	// Text should still be accumulated from deltas
	if resp.TextContent() != "Done." {
		t.Errorf("expected text 'Done.', got %q", resp.TextContent())
	}
}

func TestBuildResponseFromStream(t *testing.T) {
	acc := &streamAccumulator{
		textBuf:      "Hello, world!",
		reasoningBuf: "thinking...",
		toolCalls: []llm.ToolCallData{
			{ID: "tc-1", Name: "read_file", Arguments: json.RawMessage(`{"path":"a.go"}`)},
		},
		finishReason: &llm.FinishReason{Reason: llm.FinishToolCalls},
		usage:        &llm.Usage{InputTokens: 100, OutputTokens: 50, TotalTokens: 150},
		responseID:   "resp-123",
		model:        "claude-4",
		provider:     "anthropic",
	}

	resp := buildResponseFromStream(acc)

	if resp.ID != "resp-123" {
		t.Errorf("expected ID 'resp-123', got %q", resp.ID)
	}
	if resp.Model != "claude-4" {
		t.Errorf("expected model 'claude-4', got %q", resp.Model)
	}
	if resp.Provider != "anthropic" {
		t.Errorf("expected provider 'anthropic', got %q", resp.Provider)
	}
	if resp.TextContent() != "Hello, world!" {
		t.Errorf("expected text 'Hello, world!', got %q", resp.TextContent())
	}
	if resp.Reasoning() != "thinking..." {
		t.Errorf("expected reasoning 'thinking...', got %q", resp.Reasoning())
	}
	if len(resp.ToolCalls()) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls()))
	}
	if resp.ToolCalls()[0].Name != "read_file" {
		t.Errorf("expected tool call name 'read_file', got %q", resp.ToolCalls()[0].Name)
	}
	if resp.FinishReason.Reason != llm.FinishToolCalls {
		t.Errorf("expected finish reason %q, got %q", llm.FinishToolCalls, resp.FinishReason.Reason)
	}
	if resp.Usage.InputTokens != 100 {
		t.Errorf("expected input tokens 100, got %d", resp.Usage.InputTokens)
	}
}

func TestBuildResponseFromStream_EmptyAccumulator(t *testing.T) {
	acc := &streamAccumulator{}
	resp := buildResponseFromStream(acc)

	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.TextContent() != "" {
		t.Errorf("expected empty text, got %q", resp.TextContent())
	}
	if len(resp.ToolCalls()) != 0 {
		t.Errorf("expected 0 tool calls, got %d", len(resp.ToolCalls()))
	}
	if resp.FinishReason.Reason != "" {
		t.Errorf("expected empty finish reason, got %q", resp.FinishReason.Reason)
	}
}

func TestConsumeStream_SplitUsageMerge(t *testing.T) {
	// Anthropic sends input_tokens in StreamStart and output_tokens in StreamFinish.
	// The accumulator must merge both into the final response.
	session := NewSession(DefaultSessionConfig())
	defer session.Close()

	events := []llm.StreamEvent{
		{Type: llm.StreamStart, Usage: &llm.Usage{InputTokens: 1500}},
		{Type: llm.StreamTextStart},
		{Type: llm.StreamTextDelta, Delta: "hello"},
		{Type: llm.StreamTextEnd},
		{Type: llm.StreamFinish, Usage: &llm.Usage{OutputTokens: 200}},
	}

	resp, err := consumeStream(context.Background(), session, sendEvents(events))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Usage.InputTokens != 1500 {
		t.Errorf("expected InputTokens=1500, got %d", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 200 {
		t.Errorf("expected OutputTokens=200, got %d", resp.Usage.OutputTokens)
	}
	if resp.Usage.TotalTokens != 1700 {
		t.Errorf("expected TotalTokens=1700, got %d", resp.Usage.TotalTokens)
	}
}

func TestMergeUsage_TakesMaxValues(t *testing.T) {
	acc := &streamAccumulator{}

	// First merge: input tokens from message_start
	acc.mergeUsage(&llm.Usage{InputTokens: 1000})
	if acc.usage.InputTokens != 1000 {
		t.Errorf("expected 1000, got %d", acc.usage.InputTokens)
	}

	// Second merge: output tokens from message_delta, should not clobber input
	acc.mergeUsage(&llm.Usage{OutputTokens: 500})
	if acc.usage.InputTokens != 1000 {
		t.Errorf("expected InputTokens preserved at 1000, got %d", acc.usage.InputTokens)
	}
	if acc.usage.OutputTokens != 500 {
		t.Errorf("expected OutputTokens=500, got %d", acc.usage.OutputTokens)
	}
	if acc.usage.TotalTokens != 1500 {
		t.Errorf("expected TotalTokens=1500, got %d", acc.usage.TotalTokens)
	}

	// Third merge: nil should be safe
	acc.mergeUsage(nil)
	if acc.usage.TotalTokens != 1500 {
		t.Errorf("nil merge should be no-op, got %d", acc.usage.TotalTokens)
	}
}
