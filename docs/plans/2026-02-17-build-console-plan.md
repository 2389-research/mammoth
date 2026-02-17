# Build Console View Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a real-time streaming console to the build page showing LLM token streaming, tool calls with arguments, and outputs as they happen.

**Architecture:** Switch the agent loop from blocking `client.Complete()` to `client.Stream()`, emit text deltas as agent events, bridge them through the engine event system to SSE, and render them in a new console panel on the build page. Deltas are ephemeral (SSE-only, not persisted to progress.ndjson).

**Tech Stack:** Go (agent loop, bridge, engine events), HTML/CSS/JS (console panel, SSE handlers)

---

### Task 1: Stream accumulator helper

**Files:**
- Create: `agent/stream.go`
- Test: `agent/stream_test.go`

**Context:** The agent loop currently calls `client.Complete(ctx, request)` at `agent/loop.go:78` which blocks until the full response arrives. The LLM client already has `client.Stream(ctx, request)` (see `llm/client.go:200-206`) that returns `<-chan llm.StreamEvent`. The `llm.StreamEvent` type (defined at `llm/types.go:441-453`) carries text deltas, reasoning deltas, tool call starts/deltas, usage, and finish events. The `MuxAdapter.Stream()` at `llm/mux_adapter.go:45-79` converts mux stream events to mammoth StreamEvents.

We need a helper that consumes the stream channel, emits agent session events for text deltas, and accumulates all stream data into an `llm.Response` equivalent that the rest of the loop can use unchanged.

**Step 1: Write the failing test**

Create `agent/stream_test.go` with tests for the stream accumulator:

```go
// ABOUTME: Tests for the streaming LLM response accumulator used by the agent loop.
// ABOUTME: Verifies text/reasoning/tool accumulation, delta batching, and event emission.

package agent

import (
	"context"
	"testing"
	"time"

	"github.com/2389-research/mammoth/llm"
)

func TestConsumeStream_TextOnly(t *testing.T) {
	ctx := context.Background()
	session := NewSession(DefaultSessionConfig())
	defer session.Close()

	ch := make(chan llm.StreamEvent, 10)
	ch <- llm.StreamEvent{Type: llm.StreamStart}
	ch <- llm.StreamEvent{Type: llm.StreamTextStart}
	ch <- llm.StreamEvent{Type: llm.StreamTextDelta, Delta: "Hello "}
	ch <- llm.StreamEvent{Type: llm.StreamTextDelta, Delta: "world"}
	ch <- llm.StreamEvent{Type: llm.StreamTextEnd}
	ch <- llm.StreamEvent{Type: llm.StreamFinish, Usage: &llm.Usage{
		InputTokens: 10, OutputTokens: 5, TotalTokens: 15,
	}}
	close(ch)

	resp, err := consumeStream(ctx, ch, session)
	if err != nil {
		t.Fatalf("consumeStream error: %v", err)
	}
	if resp.TextContent() != "Hello world" {
		t.Errorf("text = %q, want %q", resp.TextContent(), "Hello world")
	}
	if resp.Usage.TotalTokens != 15 {
		t.Errorf("total tokens = %d, want 15", resp.Usage.TotalTokens)
	}
}

func TestConsumeStream_WithToolCalls(t *testing.T) {
	ctx := context.Background()
	session := NewSession(DefaultSessionConfig())
	defer session.Close()

	ch := make(chan llm.StreamEvent, 20)
	ch <- llm.StreamEvent{Type: llm.StreamStart}
	ch <- llm.StreamEvent{Type: llm.StreamTextStart}
	ch <- llm.StreamEvent{Type: llm.StreamTextDelta, Delta: "I'll run bash"}
	ch <- llm.StreamEvent{Type: llm.StreamTextEnd}
	ch <- llm.StreamEvent{Type: llm.StreamToolStart, ToolCall: &llm.ToolCall{
		ID: "call_123", Name: "bash",
	}}
	ch <- llm.StreamEvent{Type: llm.StreamToolDelta, Delta: `{"command"`}
	ch <- llm.StreamEvent{Type: llm.StreamToolDelta, Delta: `: "ls -la"}`}
	ch <- llm.StreamEvent{Type: llm.StreamToolEnd}
	ch <- llm.StreamEvent{Type: llm.StreamFinish, Usage: &llm.Usage{
		InputTokens: 20, OutputTokens: 10, TotalTokens: 30,
	}}
	close(ch)

	resp, err := consumeStream(ctx, ch, session)
	if err != nil {
		t.Fatalf("consumeStream error: %v", err)
	}
	if resp.TextContent() != "I'll run bash" {
		t.Errorf("text = %q, want %q", resp.TextContent(), "I'll run bash")
	}
	toolCalls := resp.ToolCalls()
	if len(toolCalls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(toolCalls))
	}
	if toolCalls[0].Name != "bash" {
		t.Errorf("tool name = %q, want %q", toolCalls[0].Name, "bash")
	}
	if toolCalls[0].ID != "call_123" {
		t.Errorf("tool ID = %q, want %q", toolCalls[0].ID, "call_123")
	}
}

func TestConsumeStream_EmitsEvents(t *testing.T) {
	ctx := context.Background()
	session := NewSession(DefaultSessionConfig())
	defer session.Close()

	eventCh := session.EventEmitter.Subscribe()

	ch := make(chan llm.StreamEvent, 10)
	ch <- llm.StreamEvent{Type: llm.StreamStart}
	ch <- llm.StreamEvent{Type: llm.StreamTextStart}
	ch <- llm.StreamEvent{Type: llm.StreamTextDelta, Delta: "Hi"}
	ch <- llm.StreamEvent{Type: llm.StreamTextEnd}
	ch <- llm.StreamEvent{Type: llm.StreamFinish}
	close(ch)

	_, err := consumeStream(ctx, ch, session)
	if err != nil {
		t.Fatalf("consumeStream error: %v", err)
	}

	// Drain events and check we got start + at least one delta
	var kinds []EventKind
	timeout := time.After(500 * time.Millisecond)
	for {
		select {
		case evt, ok := <-eventCh:
			if !ok {
				goto done
			}
			kinds = append(kinds, evt.Kind)
		case <-timeout:
			goto done
		}
	}
done:
	hasStart := false
	hasDelta := false
	for _, k := range kinds {
		if k == EventAssistantTextStart {
			hasStart = true
		}
		if k == EventAssistantTextDelta {
			hasDelta = true
		}
	}
	if !hasStart {
		t.Error("missing EventAssistantTextStart")
	}
	if !hasDelta {
		t.Error("missing EventAssistantTextDelta")
	}
}

func TestConsumeStream_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	session := NewSession(DefaultSessionConfig())
	defer session.Close()

	ch := make(chan llm.StreamEvent, 5)
	ch <- llm.StreamEvent{Type: llm.StreamStart}
	ch <- llm.StreamEvent{Type: llm.StreamTextStart}
	// Cancel before sending more events
	cancel()

	_, err := consumeStream(ctx, ch, session)
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

func TestConsumeStream_StreamError(t *testing.T) {
	ctx := context.Background()
	session := NewSession(DefaultSessionConfig())
	defer session.Close()

	ch := make(chan llm.StreamEvent, 5)
	ch <- llm.StreamEvent{Type: llm.StreamStart}
	ch <- llm.StreamEvent{Type: llm.StreamErrorEvt, Error: fmt.Errorf("provider error")}
	close(ch)

	_, err := consumeStream(ctx, ch, session)
	if err == nil {
		t.Error("expected error from stream error event")
	}
}
```

Note: you'll need to add `"fmt"` to imports for the last test.

**Step 2: Run test to verify it fails**

Run: `go test ./agent/ -run TestConsumeStream -v`
Expected: FAIL with "consumeStream not defined"

**Step 3: Write the implementation**

Create `agent/stream.go`:

```go
// ABOUTME: Streaming LLM response consumer for the agent loop.
// ABOUTME: Consumes llm.StreamEvent channel, emits session events, and accumulates into a Response.

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/2389-research/mammoth/llm"
)

// consumeStream reads from a StreamEvent channel, emits agent session events
// for observability (text start, text deltas, text end), and accumulates all
// stream data into an llm.Response. Text deltas are batched with a ~50ms
// accumulation window to reduce event frequency.
func consumeStream(ctx context.Context, ch <-chan llm.StreamEvent, session *Session) (*llm.Response, error) {
	var textParts []string
	var reasoningParts []string
	var toolCalls []llm.ToolCallData
	var currentToolID string
	var currentToolName string
	var currentToolArgs strings.Builder
	var usage llm.Usage
	var finishReason *llm.FinishReason
	var responseID string

	// Delta batching state
	var pendingDelta strings.Builder
	var batchTimer *time.Timer
	textStarted := false

	flushDelta := func() {
		if pendingDelta.Len() > 0 {
			session.Emit(EventAssistantTextDelta, map[string]any{
				"text": pendingDelta.String(),
			})
			pendingDelta.Reset()
		}
		if batchTimer != nil {
			batchTimer.Stop()
			batchTimer = nil
		}
	}

	for {
		select {
		case <-ctx.Done():
			flushDelta()
			return nil, ctx.Err()

		case evt, ok := <-ch:
			if !ok {
				// Channel closed, build the response
				flushDelta()
				return buildResponseFromStream(textParts, reasoningParts, toolCalls, usage, finishReason, responseID), nil
			}

			switch evt.Type {
			case llm.StreamStart:
				if evt.Response != nil {
					responseID = evt.Response.ID
				}

			case llm.StreamTextStart:
				if !textStarted {
					textStarted = true
					session.Emit(EventAssistantTextStart, map[string]any{})
				}

			case llm.StreamTextDelta:
				textParts = append(textParts, evt.Delta)
				pendingDelta.WriteString(evt.Delta)

				// Batch deltas: flush every ~50ms
				if batchTimer == nil {
					batchTimer = time.AfterFunc(50*time.Millisecond, func() {
						// The timer fires asynchronously; we handle it by
						// checking pendingDelta at the next event or at stream end.
					})
				}
				// If buffer is getting large, flush immediately
				if pendingDelta.Len() > 200 {
					flushDelta()
				}

			case llm.StreamTextEnd:
				flushDelta()

			case llm.StreamReasonStart:
				// No special event for reasoning start

			case llm.StreamReasonDelta:
				reasoningParts = append(reasoningParts, evt.ReasoningDelta)

			case llm.StreamReasonEnd:
				// No special event for reasoning end

			case llm.StreamToolStart:
				// Flush any pending text delta before tool events
				flushDelta()
				if evt.ToolCall != nil {
					currentToolID = evt.ToolCall.ID
					currentToolName = evt.ToolCall.Name
					currentToolArgs.Reset()
				}

			case llm.StreamToolDelta:
				currentToolArgs.WriteString(evt.Delta)

			case llm.StreamToolEnd:
				if currentToolID != "" {
					toolCalls = append(toolCalls, llm.ToolCallData{
						ID:        currentToolID,
						Name:      currentToolName,
						Arguments: json.RawMessage(currentToolArgs.String()),
					})
					currentToolID = ""
					currentToolName = ""
					currentToolArgs.Reset()
				}

			case llm.StreamFinish:
				flushDelta()
				if evt.Usage != nil {
					usage = *evt.Usage
				}
				if evt.FinishReason != nil {
					finishReason = evt.FinishReason
				}
				if evt.Response != nil {
					responseID = evt.Response.ID
				}

			case llm.StreamErrorEvt:
				flushDelta()
				if evt.Error != nil {
					return nil, fmt.Errorf("stream error: %w", evt.Error)
				}
				return nil, fmt.Errorf("stream error: unknown")
			}
		}
	}
}

// buildResponseFromStream constructs an llm.Response from accumulated stream data.
func buildResponseFromStream(
	textParts []string,
	reasoningParts []string,
	toolCalls []llm.ToolCallData,
	usage llm.Usage,
	finishReason *llm.FinishReason,
	responseID string,
) *llm.Response {
	var parts []llm.ContentPart

	// Add reasoning if present
	if len(reasoningParts) > 0 {
		parts = append(parts, llm.ContentPart{
			Kind: llm.ContentThinking,
			Text: strings.Join(reasoningParts, ""),
		})
	}

	// Add text if present
	fullText := strings.Join(textParts, "")
	if fullText != "" {
		parts = append(parts, llm.TextPart(fullText))
	}

	// Add tool calls
	for _, tc := range toolCalls {
		parts = append(parts, llm.ToolCallPart(tc.ID, tc.Name, tc.Arguments))
	}

	resp := &llm.Response{
		ID:    responseID,
		Usage: usage,
		Message: llm.Message{
			Role:    llm.RoleAssistant,
			Content: parts,
		},
	}

	if finishReason != nil {
		resp.FinishReason = *finishReason
	}

	return resp
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./agent/ -run TestConsumeStream -v`
Expected: PASS

**Step 5: Commit**

```bash
git add agent/stream.go agent/stream_test.go
git commit -m "feat(agent): add streaming response accumulator for real-time token emission"
```

---

### Task 2: Wire streaming into ProcessInput

**Files:**
- Modify: `agent/loop.go:77-78` (replace `Complete` with streaming)
- Test: `agent/loop_test.go` (existing tests should still pass)

**Context:** The `ProcessInput` function at `agent/loop.go:78` calls `client.Complete(ctx, request)`. We need to replace this with a call that uses `client.Stream()` via the `consumeStream` helper from Task 1. The key constraint: the rest of the loop (extracting tool calls, recording assistant turn, emitting `EventAssistantTextEnd`) must work identically.

**Step 1: Write a test that verifies streaming events are emitted during ProcessInput**

Add to the existing agent loop test file (or create one if needed). The test should set up a session, subscribe to events, run ProcessInput with a mock provider that returns a simple response, and verify that `EventAssistantTextStart` and `EventAssistantTextDelta` events are emitted.

Note: The existing tests use `Complete()` via mock providers. For streaming to work, the mock provider must implement `Stream()`. Check what existing test infrastructure exists. If mock providers return `nil, fmt.Errorf("not implemented")` for `Stream()`, we need a test-specific one that returns a channel.

**Step 2: Modify ProcessInput**

In `agent/loop.go`, replace line 78:

```go
// OLD:
response, err := client.Complete(ctx, request)

// NEW:
streamCh, streamErr := client.Stream(ctx, request)
var response *llm.Response
if streamErr != nil {
    // Fall back to non-streaming if streaming is not supported
    response, err = client.Complete(ctx, request)
} else {
    response, err = consumeStream(ctx, streamCh, session)
}
```

This fallback approach means:
- If `Stream()` works, we get real-time deltas
- If `Stream()` returns an error (e.g., adapter doesn't support it), we fall back to `Complete()`
- The rest of the loop is unchanged since both paths produce `*llm.Response`

**Step 3: Run all agent tests**

Run: `go test ./agent/ -v`
Expected: PASS (existing behavior preserved, streaming adds new events)

**Step 4: Commit**

```bash
git add agent/loop.go
git commit -m "feat(agent): switch LLM calls to streaming with Complete fallback"
```

---

### Task 3: Enrich EventToolCallStart with arguments

**Files:**
- Modify: `agent/loop.go:195-198` (add arguments to event data)
- Test: `agent/loop_test.go` or `agent/stream_test.go`

**Context:** Currently `executeSingleTool` at `agent/loop.go:194-198` emits `EventToolCallStart` with only `tool_name` and `call_id`. The console needs the tool arguments to display what the tool was called with.

**Step 1: Write the test**

Test that `EventToolCallStart` event data includes an `arguments` field containing the raw JSON arguments.

**Step 2: Modify executeSingleTool**

At `agent/loop.go:195-198`, change:

```go
// OLD:
session.Emit(EventToolCallStart, map[string]any{
    "tool_name": tc.Name,
    "call_id":   tc.ID,
})

// NEW:
session.Emit(EventToolCallStart, map[string]any{
    "tool_name": tc.Name,
    "call_id":   tc.ID,
    "arguments": string(tc.Arguments),
})
```

**Step 3: Run tests**

Run: `go test ./agent/ -v`
Expected: PASS

**Step 4: Commit**

```bash
git add agent/loop.go
git commit -m "feat(agent): include tool arguments in EventToolCallStart"
```

---

### Task 4: Add engine event constants

**Files:**
- Modify: `attractor/engine.go:31-36` (add new constants)
- Test: verify with `go build ./attractor/...`

**Context:** The engine event types at `attractor/engine.go:31-36` need two new constants for the streaming text events.

**Step 1: Add the constants**

At `attractor/engine.go`, after line 36 (after `EventAgentLoopDetected`), add:

```go
EventAgentTextStart EngineEventType = "agent.text.start"
EventAgentTextDelta EngineEventType = "agent.text.delta"
```

**Step 2: Verify it compiles**

Run: `go build ./attractor/...`
Expected: Success

**Step 3: Commit**

```bash
git add attractor/engine.go
git commit -m "feat(attractor): add agent.text.start and agent.text.delta engine event types"
```

---

### Task 5: Bridge new agent events to engine events

**Files:**
- Modify: `attractor/backend_agent.go:337-457` (add cases in bridgeSessionEvent)
- Test: `attractor/backend_agent_test.go` (add bridge tests)

**Context:** The `bridgeSessionEvent` function at `attractor/backend_agent.go:329-457` currently handles `EventToolCallStart`, `EventToolCallEnd`, `EventAssistantTextEnd`, `EventSteeringInjected`, and `EventLoopDetection`. We need to add cases for `EventAssistantTextStart` and `EventAssistantTextDelta`, and enrich `EventToolCallStart` with arguments.

**Step 1: Write the test**

Add tests to verify:
- `EventAssistantTextStart` → `EventAgentTextStart`
- `EventAssistantTextDelta` with text → `EventAgentTextDelta` with text in data
- `EventToolCallStart` with arguments → forwarded with arguments in data

**Step 2: Add bridge cases**

In `bridgeSessionEvent`, add before the closing `}` of the switch (or in logical order):

```go
case agent.EventAssistantTextStart:
    handler(EngineEvent{
        Type:      EventAgentTextStart,
        NodeID:    nodeID,
        Timestamp: evt.Timestamp,
        Data:      map[string]any{},
    })

case agent.EventAssistantTextDelta:
    text, _ := evt.Data["text"].(string)
    handler(EngineEvent{
        Type:      EventAgentTextDelta,
        NodeID:    nodeID,
        Timestamp: evt.Timestamp,
        Data: map[string]any{
            "text": text,
        },
    })
```

Also modify the existing `EventToolCallStart` case to forward arguments:

```go
case agent.EventToolCallStart:
    toolName, _ := evt.Data["tool_name"].(string)
    callID, _ := evt.Data["call_id"].(string)
    arguments, _ := evt.Data["arguments"].(string)

    // ... existing toolStarts tracking ...

    data := map[string]any{
        "tool_name": toolName,
        "call_id":   callID,
    }
    if arguments != "" {
        data["arguments"] = arguments
    }

    handler(EngineEvent{
        Type:      EventAgentToolCallStart,
        NodeID:    nodeID,
        Timestamp: evt.Timestamp,
        Data:      data,
    })
```

**Step 3: Run tests**

Run: `go test ./attractor/ -run TestBridge -v`
Expected: PASS

**Step 4: Commit**

```bash
git add attractor/backend_agent.go attractor/backend_agent_test.go
git commit -m "feat(attractor): bridge text streaming and tool argument events"
```

---

### Task 6: Filter delta events from progress.ndjson

**Files:**
- Modify: `attractor/progress.go:76-133` (skip delta events)
- Test: `attractor/progress_test.go`

**Context:** The `ProgressLogger.HandleEvent` at `attractor/progress.go:76` writes every event to the NDJSON file. Text deltas are high-frequency ephemeral events that should only be streamed via SSE, not persisted.

**Step 1: Write the test**

Add a test that sends an `EventAgentTextDelta` event and verifies it does NOT appear in the NDJSON file but the event count still increments.

**Step 2: Add the filter**

At the top of `HandleEvent`, after the `p.closed` check, add:

```go
// Skip high-frequency ephemeral events from persistence.
// These are streamed via SSE but not worth persisting to disk.
if evt.Type == EventAgentTextDelta {
    return
}
```

**Step 3: Run tests**

Run: `go test ./attractor/ -run TestProgress -v`
Expected: PASS

**Step 4: Commit**

```bash
git add attractor/progress.go attractor/progress_test.go
git commit -m "feat(attractor): skip agent.text.delta from progress.ndjson persistence"
```

---

### Task 7: Console panel CSS

**Files:**
- Modify: `web/static/css/build.css` (append console styles)

**Context:** The console panel needs a dark background (#0B1821 from the Cold Launch palette), monospace IBM Plex Mono font, amber headers, and indented streaming text. These styles must coexist with the existing build.css styles. The build view template will load IBM Plex Mono from Google Fonts.

**Step 1: Add the console styles**

Append to `web/static/css/build.css`:

```css
/* Console panel */
.build-tab-bar {
    display: flex;
    gap: 0;
    border-bottom: 2px solid var(--border);
    margin-bottom: 14px;
}
.build-tab {
    padding: 8px 16px;
    font-size: 12px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    color: var(--text-muted);
    background: none;
    border: none;
    border-bottom: 2px solid transparent;
    margin-bottom: -2px;
    cursor: pointer;
    transition: color 0.15s, border-color 0.15s;
}
.build-tab:hover {
    color: var(--text-primary);
}
.build-tab.active {
    color: #D4651A;
    border-bottom-color: #D4651A;
}
.build-tab-panel {
    display: none;
}
.build-tab-panel.active {
    display: block;
}

.build-console {
    background: #0B1821;
    border-radius: var(--radius-xl);
    border: 1px solid #1B3A4B;
    font-family: 'IBM Plex Mono', monospace;
    font-size: 13px;
    line-height: 1.5;
    max-height: 600px;
    overflow-y: auto;
    padding: 12px 0;
    scroll-behavior: smooth;
}
.build-console-empty {
    padding: 20px;
    color: #8B9DAF;
    text-align: center;
    font-style: italic;
}

.console-entry {
    padding: 0 14px;
    margin-bottom: 2px;
}

.console-header {
    color: #D4651A;
    font-weight: 700;
    padding: 6px 14px 2px 14px;
    font-size: 12px;
}
.console-header .console-node {
    color: #D4651A;
}
.console-header .console-type {
    color: #8B9DAF;
}

.console-text {
    color: #8B9DAF;
    padding: 0 14px 0 14px;
    border-left: 2px solid #1B3A4B;
    margin-left: 14px;
    white-space: pre-wrap;
    word-break: break-word;
}
.console-text .console-cursor {
    display: inline-block;
    width: 7px;
    height: 14px;
    background: #D4651A;
    animation: consoleBlink 1s step-end infinite;
    vertical-align: text-bottom;
    margin-left: 1px;
}

.console-tool-input {
    color: #F4F1EC;
    padding: 2px 14px;
    border-left: 2px solid #1B3A4B;
    margin-left: 14px;
    white-space: pre-wrap;
    word-break: break-word;
}
.console-tool-input .console-prefix {
    color: #D4651A;
    user-select: none;
}

.console-tool-output {
    color: #5a7080;
    padding: 2px 14px;
    border-left: 2px solid #1B3A4B;
    margin-left: 14px;
    white-space: pre-wrap;
    word-break: break-word;
    max-height: 200px;
    overflow: hidden;
    position: relative;
}
.console-tool-output.expanded {
    max-height: none;
}
.console-tool-output .console-prefix {
    color: #5a7080;
    user-select: none;
}
.console-expand-toggle {
    background: none;
    border: none;
    color: #D4651A;
    font-family: 'IBM Plex Mono', monospace;
    font-size: 11px;
    cursor: pointer;
    padding: 2px 14px;
    margin-left: 14px;
    border-left: 2px solid #1B3A4B;
}
.console-expand-toggle:hover {
    text-decoration: underline;
}

.console-summary {
    color: #5a7080;
    font-style: italic;
    font-size: 11px;
    padding: 2px 14px;
    border-left: 2px solid #1B3A4B;
    margin-left: 14px;
}

.console-resume-bar {
    position: sticky;
    bottom: 0;
    background: linear-gradient(transparent, #0B1821 40%);
    text-align: center;
    padding: 12px;
}
.console-resume-btn {
    background: #1B3A4B;
    color: #D4651A;
    border: 1px solid #D4651A;
    border-radius: 4px;
    padding: 4px 14px;
    font-family: 'IBM Plex Mono', monospace;
    font-size: 11px;
    cursor: pointer;
}
.console-resume-btn:hover {
    background: #D4651A;
    color: #0B1821;
}

@keyframes consoleBlink {
    0%, 100% { opacity: 1; }
    50% { opacity: 0; }
}
```

**Step 2: Verify CSS loads**

Run: `go test ./web/ -run TestServer -v` (existing tests should still pass)
Expected: PASS

**Step 3: Commit**

```bash
git add web/static/css/build.css
git commit -m "feat(web): add console panel CSS with Cold Launch styling"
```

---

### Task 8: Console panel HTML and JavaScript

**Files:**
- Modify: `web/templates/build_view.html` (add tab switcher, console panel, JS handlers)

**Context:** The build view at `web/templates/build_view.html` currently has a Live Timeline section (lines 66-78) and a side panel. We need to add a tab switcher above the grid that toggles between `[Console]` (default) and `[Metrics]` (the existing timeline + side panels). The console panel handles new SSE events: `agent.text.start`, `agent.text.delta`, `agent.tool_call.start` (enriched with arguments), `agent.tool_call.end`, and `agent.llm_turn`.

The existing event handlers for metrics (tool counts, token tracking, stage events) must continue working regardless of which tab is active.

**Step 1: Add IBM Plex Mono font import**

At the top of the template (after the build.css link), add:

```html
<link rel="preconnect" href="https://fonts.googleapis.com">
<link href="https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:wght@400;700&display=swap" rel="stylesheet">
```

**Step 2: Add tab bar and console panel HTML**

Replace the `<section class="build-grid">` block (lines 66-96) with:

```html
<div class="build-tab-bar">
    <button class="build-tab active" data-tab="console">Console</button>
    <button class="build-tab" data-tab="metrics">Metrics</button>
</div>

<div id="tab-console" class="build-tab-panel active">
    <div id="build-console" class="build-console">
        <div class="build-console-empty">Waiting for agent activity...</div>
    </div>
</div>

<div id="tab-metrics" class="build-tab-panel">
    <section class="build-grid">
        <!-- existing Live Timeline and side panels go here unchanged -->
        <section class="build-card">
            ...existing timeline card...
        </section>
        <div class="build-side">
            ...existing side cards...
        </div>
    </section>
</div>
```

**Step 3: Add tab switching JS**

In the `<script>` block, add tab switching logic:

```javascript
// Tab switching
var tabs = document.querySelectorAll('.build-tab');
tabs.forEach(function(tab) {
    tab.addEventListener('click', function() {
        tabs.forEach(function(t) { t.classList.remove('active'); });
        tab.classList.add('active');
        document.querySelectorAll('.build-tab-panel').forEach(function(p) {
            p.classList.remove('active');
        });
        document.getElementById('tab-' + tab.dataset.tab).classList.add('active');
    });
});
```

**Step 4: Add console handler functions**

```javascript
var consoleDiv = document.getElementById('build-console');
var consoleAutoScroll = true;
var currentConsoleTextEl = null;

// Detect user scroll to pause auto-scroll
consoleDiv.addEventListener('scroll', function() {
    var atBottom = consoleDiv.scrollHeight - consoleDiv.scrollTop - consoleDiv.clientHeight < 30;
    consoleAutoScroll = atBottom;
    var resumeBar = consoleDiv.querySelector('.console-resume-bar');
    if (resumeBar) {
        resumeBar.style.display = consoleAutoScroll ? 'none' : 'block';
    }
});

function consoleScrollToBottom() {
    if (consoleAutoScroll) {
        consoleDiv.scrollTop = consoleDiv.scrollHeight;
    }
}

function consoleClearEmpty() {
    var empty = consoleDiv.querySelector('.build-console-empty');
    if (empty) { empty.remove(); }
}

function appendConsoleHeader(nodeId, type) {
    consoleClearEmpty();
    var el = document.createElement('div');
    el.className = 'console-header';
    el.innerHTML = '<span class="console-node">node:' + escapeHtml(nodeId || '?') + '</span> ▸ <span class="console-type">' + escapeHtml(type) + '</span>';
    consoleDiv.appendChild(el);
    currentConsoleTextEl = null;
    consoleScrollToBottom();
}

function appendConsoleText(text) {
    consoleClearEmpty();
    if (!currentConsoleTextEl) {
        currentConsoleTextEl = document.createElement('div');
        currentConsoleTextEl.className = 'console-text';
        currentConsoleTextEl.textContent = '';
        // Add blinking cursor
        var cursor = document.createElement('span');
        cursor.className = 'console-cursor';
        currentConsoleTextEl.appendChild(cursor);
        consoleDiv.appendChild(currentConsoleTextEl);
    }
    // Insert text before the cursor
    var cursor = currentConsoleTextEl.querySelector('.console-cursor');
    if (cursor) {
        currentConsoleTextEl.insertBefore(document.createTextNode(text), cursor);
    } else {
        currentConsoleTextEl.appendChild(document.createTextNode(text));
    }
    consoleScrollToBottom();
}

function finishConsoleText() {
    if (currentConsoleTextEl) {
        var cursor = currentConsoleTextEl.querySelector('.console-cursor');
        if (cursor) { cursor.remove(); }
        currentConsoleTextEl = null;
    }
}

function appendConsoleToolInput(name, args) {
    consoleClearEmpty();
    finishConsoleText();
    var el = document.createElement('div');
    el.className = 'console-tool-input';
    var display = name || 'unknown';
    if (args) {
        try {
            var parsed = JSON.parse(args);
            if (parsed.command) {
                display = '$ ' + parsed.command;
            } else {
                display = name + '(' + truncateStr(args, 200) + ')';
            }
        } catch(_e) {
            display = name + '(' + truncateStr(args, 200) + ')';
        }
    }
    el.innerHTML = '<span class="console-prefix">▸ </span>' + escapeHtml(display);
    consoleDiv.appendChild(el);
    consoleScrollToBottom();
}

function appendConsoleToolOutput(output, durationMs) {
    consoleClearEmpty();
    if (!output) { return; }
    var el = document.createElement('div');
    el.className = 'console-tool-output';
    var truncated = truncateStr(output, 500);
    var suffix = '';
    if (durationMs) {
        suffix = durationMs < 1000 ? ' (' + durationMs + 'ms)' : ' (' + (durationMs / 1000).toFixed(1) + 's)';
    }
    el.innerHTML = '<span class="console-prefix">> </span>' + escapeHtml(truncated) + escapeHtml(suffix);
    consoleDiv.appendChild(el);

    if (output.length > 500) {
        var toggle = document.createElement('button');
        toggle.className = 'console-expand-toggle';
        toggle.textContent = '... show more';
        toggle.addEventListener('click', function() {
            if (el.classList.contains('expanded')) {
                el.classList.remove('expanded');
                el.innerHTML = '<span class="console-prefix">> </span>' + escapeHtml(truncated) + escapeHtml(suffix);
                toggle.textContent = '... show more';
            } else {
                el.classList.add('expanded');
                el.innerHTML = '<span class="console-prefix">> </span>' + escapeHtml(output) + escapeHtml(suffix);
                toggle.textContent = '... show less';
            }
        });
        consoleDiv.appendChild(toggle);
    }
    consoleScrollToBottom();
}

function appendConsoleSummary(tokens, durationMs) {
    var el = document.createElement('div');
    el.className = 'console-summary';
    var parts = [];
    if (tokens) { parts.push(formatNumber(tokens) + ' tokens'); }
    if (durationMs) {
        parts.push(durationMs < 1000 ? durationMs + 'ms' : (durationMs / 1000).toFixed(1) + 's');
    }
    el.textContent = '(' + parts.join(', ') + ')';
    consoleDiv.appendChild(el);
    consoleScrollToBottom();
}

function escapeHtml(str) {
    var div = document.createElement('div');
    div.appendChild(document.createTextNode(str || ''));
    return div.innerHTML;
}

function truncateStr(s, max) {
    if (!s || s.length <= max) { return s || ''; }
    return s.substring(0, max);
}
```

**Step 5: Add SSE event listeners for console**

In the `connectStream` function, add listeners for the new event types:

```javascript
source.addEventListener('agent.text.start', function(e) {
    var data = safeJSON(e.data);
    appendConsoleHeader(data.node_id || metricCurrentNode.textContent, 'agent thinking...');
});

source.addEventListener('agent.text.delta', function(e) {
    var data = safeJSON(e.data);
    if (data.text) {
        appendConsoleText(data.text);
    }
});

source.addEventListener('agent.llm_turn', function(e) {
    var data = safeJSON(e.data);
    finishConsoleText();
    var totalTk = data.total_tokens || 0;
    appendConsoleSummary(totalTk, null);
    // Also update metrics (existing logic)
    registerLLMTurn(data);
    var inTokens = data.input_tokens || 0;
    var outTokens = data.output_tokens || 0;
    var totalTokens = data.total_tokens || (Number(inTokens) + Number(outTokens));
    addEvent('LLM turn: ' + totalTokens + ' tokens (in ' + inTokens + ', out ' + outTokens + ')', 'muted');
});
```

Also modify the existing `agent.tool_call.start` and `agent.tool_call.end` listeners to also update the console:

```javascript
source.addEventListener('agent.tool_call.start', function(e) {
    var data = safeJSON(e.data);
    registerToolCallStart(data);
    addEvent('Tool start: ' + toolSummary(data), 'normal');
    // Console
    appendConsoleHeader(data.node_id || metricCurrentNode.textContent, 'tool_call: ' + (data.tool_name || 'unknown'));
    appendConsoleToolInput(data.tool_name, data.arguments);
});

source.addEventListener('agent.tool_call.end', function(e) {
    var data = safeJSON(e.data);
    addEvent('Tool done: ' + toolSummary(data) + toolDurationSuffix(data), 'success');
    // Console
    appendConsoleToolOutput(data.output_snippet, data.duration_ms);
});
```

And for stage events, add console headers:

```javascript
source.addEventListener('stage.started', function(e) {
    var data = safeJSON(e.data);
    var node = data.node_id || 'unknown';
    addEvent('Stage started: ' + node, 'normal');
    metricCurrentNode.textContent = node;
    setActiveNodeHighlight(node);
    // Console
    appendConsoleHeader(node, 'stage started');
});
```

**Step 6: Add resume bar**

At the bottom of the console div initialization, add the resume bar:

```javascript
// Add resume bar (hidden by default)
var resumeBar = document.createElement('div');
resumeBar.className = 'console-resume-bar';
resumeBar.style.display = 'none';
var resumeBtn = document.createElement('button');
resumeBtn.className = 'console-resume-btn';
resumeBtn.textContent = 'Resume auto-scroll';
resumeBtn.addEventListener('click', function() {
    consoleAutoScroll = true;
    consoleDiv.scrollTop = consoleDiv.scrollHeight;
    resumeBar.style.display = 'none';
});
resumeBar.appendChild(resumeBtn);
consoleDiv.appendChild(resumeBar);
```

**Step 7: Run tests**

Run: `go test ./web/ -v`
Expected: PASS

**Step 8: Commit**

```bash
git add web/templates/build_view.html
git commit -m "feat(web): add real-time console panel to build view with tab switcher"
```

---

### Task 9: Integration test

**Files:**
- Modify: `web/server_test.go` (add console-related test)

**Context:** Add a test that verifies the build view HTML includes the console panel elements and tab switcher.

**Step 1: Write the test**

```go
func TestBuildViewHasConsolePanel(t *testing.T) {
    // Set up server with a test project that has a build
    // Render the build view template
    // Assert it contains:
    // - class="build-tab-bar"
    // - class="build-console"
    // - data-tab="console"
    // - IBM Plex Mono font import
}
```

**Step 2: Run test**

Run: `go test ./web/ -run TestBuildViewHasConsolePanel -v`
Expected: PASS

**Step 3: Commit**

```bash
git add web/server_test.go
git commit -m "test(web): add build console panel integration test"
```

---

## Summary

| Task | Component | Change |
|------|-----------|--------|
| 1 | `agent/stream.go` | Stream accumulator: consumes `<-chan StreamEvent`, emits deltas, returns `*Response` |
| 2 | `agent/loop.go` | Switch `Complete()` → `Stream()` with `Complete()` fallback |
| 3 | `agent/loop.go` | Add `arguments` to `EventToolCallStart` data |
| 4 | `attractor/engine.go` | Add `EventAgentTextStart`, `EventAgentTextDelta` constants |
| 5 | `attractor/backend_agent.go` | Bridge `EventAssistantTextStart/Delta` + forward tool arguments |
| 6 | `attractor/progress.go` | Skip `agent.text.delta` from NDJSON persistence |
| 7 | `web/static/css/build.css` | Console panel styles (Cold Launch dark theme) |
| 8 | `web/templates/build_view.html` | Tab switcher, console HTML, JS handlers for SSE |
| 9 | `web/server_test.go` | Integration test for console panel presence |

**No changes needed:**
- `web/server.go` — SSE conversion (`engineEventToSSE`) is generic
- `web/build.go` — Already handles any `EngineEvent` type
- `llm/` — Streaming infrastructure already exists
