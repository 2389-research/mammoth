# Replace attractor/ with tracker library — Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace mammoth's `attractor/` engine with `github.com/2389-research/tracker` as the DOT pipeline runner, deleting ~50 attractor files and simplifying all consumers.

**Architecture:** Tracker's top-level `tracker.Run()` / `tracker.NewEngine()` API replaces all attractor engine construction. A new `web.BuildEvent` type unifies tracker's two event streams (pipeline + agent) into a single SSE wire format. Human gates use a blocking `ChannelInterviewer` that surfaces gates as events and waits for user responses.

**Tech Stack:** Go 1.25+, `github.com/2389-research/tracker`, `github.com/charmbracelet/bubbletea`

**Spec:** `docs/superpowers/specs/2026-03-13-tracker-integration-design.md`

---

## Chunk 1: Foundation — Add tracker dependency, BuildEvent type, ChannelInterviewer

### Task 1: Add tracker dependency

**Files:**
- Modify: `go.mod`

- [ ] **Step 1: Add tracker module**

```bash
cd /Users/harper/Public/src/2389/mammoth-dev
go get github.com/2389-research/tracker@latest
```

- [ ] **Step 2: Verify import works**

Create a throwaway test:
```bash
echo 'package scratch_test

import (
    "testing"
    _ "github.com/2389-research/tracker"
)

func TestImport(t *testing.T) {}
' > /tmp/tracker_import_test.go
cp /tmp/tracker_import_test.go scratch_test.go
go test -run TestImport -count=1 ./...
rm scratch_test.go
```
Expected: PASS

- [ ] **Step 3: Tidy**

```bash
go mod tidy
```

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add github.com/2389-research/tracker dependency"
```

---

### Task 2: Create web.BuildEvent type and mapper functions

**Files:**
- Create: `web/build_event.go`
- Create: `web/build_event_test.go`

- [ ] **Step 1: Write failing tests for BuildEvent mappers**

`web/build_event_test.go`:
```go
// ABOUTME: Tests for BuildEvent type and mapper functions from tracker event types.
// ABOUTME: Validates pipeline event mapping, agent event mapping, and field preservation.
package web

import (
    "testing"
    "time"

    "github.com/2389-research/tracker/agent"
    "github.com/2389-research/tracker/pipeline"
)

func TestBuildEventFromPipeline_StageStarted(t *testing.T) {
    evt := pipeline.PipelineEvent{
        Type:      pipeline.EventStageStarted,
        Timestamp: time.Now(),
        NodeID:    "build_ui",
        Message:   "starting node",
    }
    be := buildEventFromPipeline(evt)
    if be.Type != BuildEventNodeStarted {
        t.Errorf("expected %q, got %q", BuildEventNodeStarted, be.Type)
    }
    if be.NodeID != "build_ui" {
        t.Errorf("expected node_id build_ui, got %q", be.NodeID)
    }
}

func TestBuildEventFromPipeline_PipelineCompleted(t *testing.T) {
    evt := pipeline.PipelineEvent{
        Type: pipeline.EventPipelineCompleted,
    }
    be := buildEventFromPipeline(evt)
    if be.Type != BuildEventPipelineCompleted {
        t.Errorf("expected %q, got %q", BuildEventPipelineCompleted, be.Type)
    }
}

func TestBuildEventFromPipeline_UnmappedType(t *testing.T) {
    evt := pipeline.PipelineEvent{
        Type: pipeline.PipelineEventType("unknown_future_type"),
    }
    be := buildEventFromPipeline(evt)
    if be.Type != BuildEventType("unknown_future_type") {
        t.Errorf("unmapped types should pass through, got %q", be.Type)
    }
}

func TestBuildEventFromAgent_ToolCallStart(t *testing.T) {
    evt := agent.Event{
        Type:     agent.EventToolCallStart,
        ToolName: "bash",
    }
    be := buildEventFromAgent(evt)
    if be.Type != BuildEventToolCallStart {
        t.Errorf("expected %q, got %q", BuildEventToolCallStart, be.Type)
    }
    if be.Data["tool_name"] != "bash" {
        t.Errorf("expected tool_name=bash, got %v", be.Data["tool_name"])
    }
}

func TestBuildEventFromAgent_TextDelta(t *testing.T) {
    evt := agent.Event{
        Type: agent.EventTextDelta,
        Text: "hello world",
    }
    be := buildEventFromAgent(evt)
    if be.Type != BuildEventTextDelta {
        t.Errorf("expected %q, got %q", BuildEventTextDelta, be.Type)
    }
    if be.Data["text"] != "hello world" {
        t.Errorf("expected text in data")
    }
}

func TestBuildEventFromAgent_DroppedType(t *testing.T) {
    evt := agent.Event{
        Type: agent.EventLLMReasoning,
    }
    be := buildEventFromAgent(evt)
    // Dropped types should return zero-value BuildEvent
    if be.Type != "" {
        t.Errorf("expected empty type for dropped event, got %q", be.Type)
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./web/ -run TestBuildEvent -v -count=1
```
Expected: FAIL — `buildEventFromPipeline` undefined

- [ ] **Step 3: Implement BuildEvent type and mappers**

`web/build_event.go`:
```go
// ABOUTME: Unified SSE wire format for pipeline build events.
// ABOUTME: Maps tracker's pipeline.PipelineEvent and agent.Event into a single BuildEvent type.
package web

import (
    "time"

    "github.com/2389-research/tracker/agent"
    "github.com/2389-research/tracker/pipeline"
)

// BuildEventType identifies the kind of build event for SSE consumers.
type BuildEventType string

const (
    // Pipeline lifecycle (mapped from pipeline.PipelineEvent).
    // Tracker uses "stage_*" naming; we map to "node_*" for the SSE wire format.
    BuildEventPipelineStarted   BuildEventType = "pipeline_started"
    BuildEventPipelineCompleted BuildEventType = "pipeline_completed"
    BuildEventPipelineFailed    BuildEventType = "pipeline_failed"
    BuildEventNodeStarted       BuildEventType = "node_started"
    BuildEventNodeCompleted     BuildEventType = "node_completed"
    BuildEventNodeFailed        BuildEventType = "node_failed"
    BuildEventNodeRetrying      BuildEventType = "node_retrying"
    BuildEventCheckpointSaved   BuildEventType = "checkpoint_saved"
    BuildEventParallelStarted   BuildEventType = "parallel_started"
    BuildEventParallelCompleted BuildEventType = "parallel_completed"
    BuildEventLoopRestart       BuildEventType = "loop_restart"

    // Agent activity (mapped from agent.Event).
    // Only a subset of tracker's 19 agent event types are surfaced.
    BuildEventToolCallStart BuildEventType = "tool_call_start"
    BuildEventToolCallEnd   BuildEventType = "tool_call_end"
    BuildEventTextDelta     BuildEventType = "text_delta"
    BuildEventTurnMetrics   BuildEventType = "turn_metrics"
    BuildEventSessionStart  BuildEventType = "session_start"
    BuildEventSessionEnd    BuildEventType = "session_end"
    BuildEventAgentError    BuildEventType = "agent_error"

    // Human gates.
    BuildEventHumanGateChoice   BuildEventType = "human_gate_choice"
    BuildEventHumanGateFreeform BuildEventType = "human_gate_freeform"
    BuildEventHumanGateAnswered BuildEventType = "human_gate_answered"
)

// BuildEvent is the unified SSE wire format for build progress.
type BuildEvent struct {
    Type      BuildEventType `json:"type"`
    Timestamp time.Time      `json:"timestamp"`
    NodeID    string         `json:"node_id,omitempty"`
    Message   string         `json:"message,omitempty"`
    Data      map[string]any `json:"data,omitempty"`
}

// pipelineEventMap maps tracker stage_* events to mammoth node_* events.
var pipelineEventMap = map[pipeline.PipelineEventType]BuildEventType{
    pipeline.EventPipelineStarted:   BuildEventPipelineStarted,
    pipeline.EventPipelineCompleted: BuildEventPipelineCompleted,
    pipeline.EventPipelineFailed:    BuildEventPipelineFailed,
    pipeline.EventStageStarted:      BuildEventNodeStarted,
    pipeline.EventStageCompleted:    BuildEventNodeCompleted,
    pipeline.EventStageFailed:       BuildEventNodeFailed,
    pipeline.EventStageRetrying:     BuildEventNodeRetrying,
    pipeline.EventCheckpointSaved:   BuildEventCheckpointSaved,
    pipeline.EventParallelStarted:   BuildEventParallelStarted,
    pipeline.EventParallelCompleted: BuildEventParallelCompleted,
    pipeline.EventLoopRestart:       BuildEventLoopRestart,
}

// buildEventFromPipeline maps a tracker PipelineEvent to a BuildEvent.
func buildEventFromPipeline(evt pipeline.PipelineEvent) BuildEvent {
    typ, ok := pipelineEventMap[evt.Type]
    if !ok {
        typ = BuildEventType(evt.Type)
    }
    be := BuildEvent{
        Type:      typ,
        Timestamp: evt.Timestamp,
        NodeID:    evt.NodeID,
        Message:   evt.Message,
    }
    if evt.Err != nil {
        be.Data = map[string]any{"error": evt.Err.Error()}
    }
    return be
}

// agentEventMap maps tracker agent events to BuildEvent types.
// Events not in this map are dropped (internal detail not needed by UI).
var agentEventMap = map[agent.EventType]BuildEventType{
    agent.EventToolCallStart: BuildEventToolCallStart,
    agent.EventToolCallEnd:   BuildEventToolCallEnd,
    agent.EventTextDelta:     BuildEventTextDelta,
    agent.EventTurnMetrics:   BuildEventTurnMetrics,
    agent.EventSessionStart:  BuildEventSessionStart,
    agent.EventSessionEnd:    BuildEventSessionEnd,
    agent.EventError:         BuildEventAgentError,
}

// buildEventFromAgent maps a tracker agent.Event to a BuildEvent.
// Returns a zero-value BuildEvent for dropped event types.
func buildEventFromAgent(evt agent.Event) BuildEvent {
    typ, ok := agentEventMap[evt.Type]
    if !ok {
        return BuildEvent{}
    }
    be := BuildEvent{
        Type:      typ,
        Timestamp: evt.Timestamp,
        NodeID:    evt.SessionID,
    }
    data := make(map[string]any)
    switch evt.Type {
    case agent.EventToolCallStart:
        data["tool_name"] = evt.ToolName
        if evt.ToolInput != "" {
            data["input"] = evt.ToolInput
        }
    case agent.EventToolCallEnd:
        data["tool_name"] = evt.ToolName
        if evt.ToolDuration > 0 {
            data["duration_ms"] = evt.ToolDuration.Milliseconds()
        }
        if evt.ToolError != "" {
            data["error"] = evt.ToolError
        }
    case agent.EventTextDelta:
        data["text"] = evt.Text
    case agent.EventTurnMetrics:
        if evt.Metrics != nil {
            data["input_tokens"] = evt.Metrics.InputTokens
            data["output_tokens"] = evt.Metrics.OutputTokens
            data["estimated_cost"] = evt.Metrics.EstimatedCost
        }
    case agent.EventError:
        if evt.Err != nil {
            data["error"] = evt.Err.Error()
        }
    }
    if len(data) > 0 {
        be.Data = data
    }
    return be
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./web/ -run TestBuildEvent -v -count=1
```
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add web/build_event.go web/build_event_test.go
git commit -m "feat(web): add BuildEvent type and tracker event mappers"
```

---

### Task 3: Create ChannelInterviewer

**Files:**
- Create: `web/channel_interviewer.go`
- Create: `web/channel_interviewer_test.go`

- [ ] **Step 1: Write failing tests**

`web/channel_interviewer_test.go`:
```go
// ABOUTME: Tests for ChannelInterviewer that bridges human gates to SSE events.
// ABOUTME: Validates blocking Ask/AskFreeform, Respond, concurrent gates, and unknown gate errors.
package web

import (
    "sync"
    "testing"
    "time"
)

func TestChannelInterviewer_AskAndRespond(t *testing.T) {
    var received []BuildEvent
    var mu sync.Mutex
    broadcast := func(evt BuildEvent) {
        mu.Lock()
        received = append(received, evt)
        mu.Unlock()
    }

    iv := NewChannelInterviewer(broadcast)

    // Ask in a goroutine (it blocks)
    var answer string
    var askErr error
    done := make(chan struct{})
    go func() {
        answer, askErr = iv.Ask("approve?", []string{"yes", "no"}, "yes")
        close(done)
    }()

    // Wait for the gate event to be broadcast
    time.Sleep(50 * time.Millisecond)

    mu.Lock()
    if len(received) == 0 {
        t.Fatal("expected gate event to be broadcast")
    }
    evt := received[0]
    mu.Unlock()

    if evt.Type != BuildEventHumanGateChoice {
        t.Fatalf("expected %q, got %q", BuildEventHumanGateChoice, evt.Type)
    }
    gateID, ok := evt.Data["gate_id"].(string)
    if !ok || gateID == "" {
        t.Fatal("expected gate_id in event data")
    }

    // Respond
    if err := iv.Respond(gateID, "no"); err != nil {
        t.Fatalf("respond: %v", err)
    }

    <-done
    if askErr != nil {
        t.Fatalf("ask error: %v", askErr)
    }
    if answer != "no" {
        t.Errorf("expected answer=no, got %q", answer)
    }
}

func TestChannelInterviewer_AskFreeformAndRespond(t *testing.T) {
    var received []BuildEvent
    var mu sync.Mutex
    broadcast := func(evt BuildEvent) {
        mu.Lock()
        received = append(received, evt)
        mu.Unlock()
    }

    iv := NewChannelInterviewer(broadcast)

    var answer string
    var askErr error
    done := make(chan struct{})
    go func() {
        answer, askErr = iv.AskFreeform("describe the feature:")
        close(done)
    }()

    time.Sleep(50 * time.Millisecond)

    mu.Lock()
    if len(received) == 0 {
        t.Fatal("expected gate event")
    }
    evt := received[0]
    mu.Unlock()

    if evt.Type != BuildEventHumanGateFreeform {
        t.Fatalf("expected %q, got %q", BuildEventHumanGateFreeform, evt.Type)
    }
    gateID := evt.Data["gate_id"].(string)

    if err := iv.Respond(gateID, "a login page"); err != nil {
        t.Fatalf("respond: %v", err)
    }

    <-done
    if askErr != nil {
        t.Fatalf("ask error: %v", askErr)
    }
    if answer != "a login page" {
        t.Errorf("expected 'a login page', got %q", answer)
    }
}

func TestChannelInterviewer_RespondUnknownGate(t *testing.T) {
    iv := NewChannelInterviewer(func(BuildEvent) {})
    err := iv.Respond("nonexistent", "answer")
    if err == nil {
        t.Fatal("expected error for unknown gate")
    }
}

func TestChannelInterviewer_ConcurrentGates(t *testing.T) {
    iv := NewChannelInterviewer(func(BuildEvent) {})

    var wg sync.WaitGroup
    results := make([]string, 3)
    gateIDs := make([]string, 3)

    for i := 0; i < 3; i++ {
        wg.Add(1)
        idx := i
        go func() {
            defer wg.Done()
            // Use AskFreeform so we don't need choices
            answer, err := iv.AskFreeform("gate " + string(rune('A'+idx)))
            if err != nil {
                t.Errorf("gate %d: %v", idx, err)
                return
            }
            results[idx] = answer
        }()
    }

    // Wait for gates to register
    time.Sleep(100 * time.Millisecond)

    // Get the pending gate IDs
    iv.mu.Lock()
    i := 0
    for id := range iv.pending {
        gateIDs[i] = id
        i++
    }
    iv.mu.Unlock()

    // Respond to each
    for j, id := range gateIDs {
        if err := iv.Respond(id, "answer-"+string(rune('A'+j))); err != nil {
            t.Fatalf("respond gate %d: %v", j, err)
        }
    }

    wg.Wait()

    // All results should be non-empty
    for j, r := range results {
        if r == "" {
            t.Errorf("gate %d got empty result", j)
        }
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./web/ -run TestChannelInterviewer -v -count=1
```
Expected: FAIL — `NewChannelInterviewer` undefined

- [ ] **Step 3: Implement ChannelInterviewer**

`web/channel_interviewer.go`:
```go
// ABOUTME: ChannelInterviewer bridges pipeline human gates to SSE events.
// ABOUTME: Blocks the pipeline handler, broadcasts gate events, and waits for user responses.
package web

import (
    "crypto/rand"
    "fmt"
    "sync"
    "time"
)

// ChannelInterviewer implements handlers.Interviewer and handlers.FreeformInterviewer.
// When the pipeline hits a human gate, it broadcasts a BuildEvent and blocks
// until Respond() is called with the user's answer.
type ChannelInterviewer struct {
    broadcast func(BuildEvent)
    pending   map[string]chan string
    mu        sync.Mutex
}

// NewChannelInterviewer creates a ChannelInterviewer that broadcasts gate events
// via the given function.
func NewChannelInterviewer(broadcast func(BuildEvent)) *ChannelInterviewer {
    return &ChannelInterviewer{
        broadcast: broadcast,
        pending:   make(map[string]chan string),
    }
}

// Ask presents a multiple-choice gate. Blocks until Respond() is called.
func (iv *ChannelInterviewer) Ask(prompt string, choices []string, defaultChoice string) (string, error) {
    gateID := generateGateID()
    ch := make(chan string, 1)

    iv.mu.Lock()
    iv.pending[gateID] = ch
    iv.mu.Unlock()

    defer func() {
        iv.mu.Lock()
        delete(iv.pending, gateID)
        iv.mu.Unlock()
    }()

    iv.broadcast(BuildEvent{
        Type:      BuildEventHumanGateChoice,
        Timestamp: time.Now(),
        Message:   prompt,
        Data: map[string]any{
            "gate_id":  gateID,
            "choices":  choices,
            "default":  defaultChoice,
        },
    })

    answer := <-ch
    return answer, nil
}

// AskFreeform presents an open-ended text input gate. Blocks until Respond() is called.
func (iv *ChannelInterviewer) AskFreeform(prompt string) (string, error) {
    gateID := generateGateID()
    ch := make(chan string, 1)

    iv.mu.Lock()
    iv.pending[gateID] = ch
    iv.mu.Unlock()

    defer func() {
        iv.mu.Lock()
        delete(iv.pending, gateID)
        iv.mu.Unlock()
    }()

    iv.broadcast(BuildEvent{
        Type:      BuildEventHumanGateFreeform,
        Timestamp: time.Now(),
        Message:   prompt,
        Data:      map[string]any{"gate_id": gateID},
    })

    answer := <-ch
    return answer, nil
}

// Respond delivers the user's answer to a pending gate.
func (iv *ChannelInterviewer) Respond(gateID, answer string) error {
    iv.mu.Lock()
    ch, ok := iv.pending[gateID]
    iv.mu.Unlock()
    if !ok {
        return fmt.Errorf("no pending gate %q", gateID)
    }
    ch <- answer
    return nil
}

func generateGateID() string {
    b := make([]byte, 8)
    _, _ = rand.Read(b)
    return fmt.Sprintf("%x", b)
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./web/ -run TestChannelInterviewer -v -count=1
```
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add web/channel_interviewer.go web/channel_interviewer_test.go
git commit -m "feat(web): add ChannelInterviewer for human gate SSE bridging"
```

---

## Chunk 2: Extract runstate package and update render/

### Task 4: Extract runstate package from attractor.FSRunStateStore

**Files:**
- Create: `runstate/store.go`
- Create: `runstate/store_test.go`

This task extracts the run state persistence from `attractor/runstate.go` and `attractor/runstate_fs.go` into a standalone `runstate/` package that stores `tracker.Result` instead of `attractor.RunResult`.

- [ ] **Step 1: Read the existing attractor runstate implementation**

Read these files to understand the current implementation:
- `attractor/runstate.go` — RunState type and interface
- `attractor/runstate_fs.go` — FSRunStateStore implementation
- `attractor/runstate_test.go` — existing tests

- [ ] **Step 2: Write failing tests for runstate package**

Write `runstate/store_test.go` with tests for:
- `Create()` and `Get()` a run state
- `Update()` an existing run state
- `List()` all run states
- `FindResumable()` by source hash
- `AddEvent()` appending to events.jsonl
- `CheckpointPath()` returning the correct path

Model the `RunState` struct after the existing one but using `tracker.Result` for the result field and `string` context values (matching tracker's `map[string]string`).

- [ ] **Step 3: Run tests to verify they fail**

```bash
go test ./runstate/ -v -count=1
```
Expected: FAIL — package doesn't exist

- [ ] **Step 4: Implement runstate package**

Create `runstate/store.go` based on the existing `attractor/runstate_fs.go` implementation. Key changes:
- `RunState.Result` is `*tracker.Result` instead of `*attractor.RunResult`
- `RunState.Context` is `map[string]string` instead of `map[string]any`
- `RunState.Events` is removed (events are in events.jsonl, not in-memory)
- On-disk format: `<baseDir>/<runID>/state.json` + `<baseDir>/<runID>/events.jsonl`

- [ ] **Step 5: Run tests to verify they pass**

```bash
go test ./runstate/ -v -count=1
```
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add runstate/
git commit -m "feat: extract runstate package from attractor with tracker.Result"
```

---

### Task 5: Update render/ to use dot/ types directly

**Files:**
- Modify: `render/render.go`
- Modify: `render/render_test.go`

The render package currently imports `attractor` for `Graph`, `Node`, `Edge`, `Subgraph`, and `Outcome` — all of which are type aliases to `dot.*`. Switch to importing `dot` directly.

- [ ] **Step 1: Update render.go imports and type references**

In `render/render.go`:
- Replace `"github.com/2389-research/mammoth/attractor"` with `"github.com/2389-research/mammoth/dot"`
- Replace all `attractor.Graph` → `dot.Graph`
- Replace all `attractor.Node` → `dot.Node`
- Replace all `attractor.Edge` → `dot.Edge`
- Replace all `attractor.Subgraph` → `dot.Subgraph`
- Replace all `attractor.Outcome` → `dot.Outcome`
- Replace status constants: `attractor.StatusSuccess` → `dot.StatusSuccess`, etc.

- [ ] **Step 2: Update render_test.go imports**

Same import replacement in the test file.

- [ ] **Step 3: Run render tests**

```bash
go test ./render/ -v -count=1
```
Expected: PASS (types are identical, just imported from source package)

- [ ] **Step 4: Commit**

```bash
git add render/render.go render/render_test.go
git commit -m "refactor(render): import dot types directly instead of via attractor aliases"
```

---

## Chunk 3: Rewrite web/ layer

### Task 6: Rewrite web/dot_fixer.go to use llm/ directly

**Files:**
- Modify: `web/dot_fixer.go`

The DOT fixer currently uses `attractor.AgentRunConfig` and `backend.RunAgent()`. Since we're dropping the CodergenBackend, rewrite it to use mammoth's `llm/` package directly for the simple prompt→response call.

- [ ] **Step 1: Read the current dot_fixer.go implementation**

Read `web/dot_fixer.go` to understand the full flow. Key things to find:
- The `fixDOTWithAgent()` function signature
- How it constructs `attractor.AgentRunConfig`
- What prompt it sends to the LLM
- How it parses the response

- [ ] **Step 2: Rewrite fixDOTWithAgent to use llm.Client**

Replace the `backend.RunAgent()` call with a direct `llm.Client.Complete()` call. The DOT fixer is a simple single-turn prompt, not an agentic loop.

Key changes:
```go
// Before:
import "github.com/2389-research/mammoth/attractor"
// ... uses attractor.AgentRunConfig, backend.RunAgent()

// After:
import "github.com/2389-research/mammoth/llm"
// ... uses llm.FromEnv() to create client, client.Complete() for single-turn
```

The `llm.FromEnv()` function creates a client from env vars. Use `llm.Request{Messages: [...]}` with the existing system/user prompts. Parse the response text the same way.

Remove the `attractor` import. Keep `dot` and `dot/validator` imports.

- [ ] **Step 3: Run web tests**

```bash
go test ./web/ -v -count=1
```
Expected: PASS (dot_fixer tests should still pass with the new implementation)

- [ ] **Step 4: Verify build**

```bash
go build ./web/...
```
Expected: Clean build (no attractor references remaining in web/)

- [ ] **Step 5: Commit**

```bash
git add web/dot_fixer.go
git commit -m "refactor(web): rewrite dot_fixer to use llm/ directly instead of attractor backend"
```

---

### Task 7: Rewrite web/server.go build orchestration

**Files:**
- Modify: `web/server.go`
- Modify: `web/server_test.go` — update test helpers that construct attractor types
- Delete: `web/backend.go`
- Delete: `web/backend_test.go` — tests for removed `detectBackendFromEnv()`
- Modify: `web/human_gate.go`
- Modify: `web/build.go`
- Modify: `web/build_test.go` — update assertions for new event types
- Modify: `web/integration_test.go` — update integration tests that drive builds

This is the largest single task. The build execution flow in `web/server.go` needs to use `tracker.NewEngine()` / `tracker.Run()` instead of constructing an `attractor.Engine`.

- [ ] **Step 1: Read the current build orchestration code**

Read these sections of `web/server.go`:
- `startBuildExecution()` — where the engine is created and launched
- `maybeResumeBuild()` — auto-resume on server restart
- Any other functions that reference attractor types

Also read `web/backend.go`, `web/human_gate.go`, `web/build.go`, and all their test files.

- [ ] **Step 2: Delete web/backend.go and web/backend_test.go**

Both files are entirely about `detectBackendFromEnv()` which is no longer needed — tracker auto-detects LLM providers from env.

```bash
git rm web/backend.go web/backend_test.go
```

- [ ] **Step 3: Rewrite web/human_gate.go**

Replace the current `configureBuildInterviewer()` function (which monkey-patches the handler registry) with a function that creates a `ChannelInterviewer` and returns it for use in `tracker.Config`:

```go
// ABOUTME: Creates ChannelInterviewer instances for web build execution.
// ABOUTME: The interviewer bridges pipeline human gates to SSE events for the browser.
package web

// newBuildInterviewer creates a ChannelInterviewer wired to the given
// build run's SSE broadcast function.
func newBuildInterviewer(broadcast func(BuildEvent)) *ChannelInterviewer {
    return NewChannelInterviewer(broadcast)
}
```

- [ ] **Step 4: Rewrite build execution in web/server.go**

Replace all attractor imports and types:

```go
// Before:
cfg := attractor.EngineConfig{
    CheckpointDir: ...,
    Handlers:      attractor.DefaultHandlerRegistry(),
    Backend:       detectBackendFromEnv(verbose),
}
engine := attractor.NewEngine(cfg)
result, err := engine.Run(ctx, source)

// After:
iv := newBuildInterviewer(broadcastFn)
cfg := tracker.Config{
    CheckpointDir: ...,
    EventHandler:  pipeline.PipelineEventHandlerFunc(func(evt pipeline.PipelineEvent) {
        be := buildEventFromPipeline(evt)
        broadcastFn(be)
    }),
    AgentEvents: agent.EventHandlerFunc(func(evt agent.Event) {
        be := buildEventFromAgent(evt)
        if be.Type != "" {
            broadcastFn(be)
        }
    }),
    // Interviewer: iv, // once tracker merges Config.Interviewer
}
result, err := tracker.Run(ctx, source, cfg)
```

Key removals:
- `detectBackendFromEnv()` — tracker auto-wires from env
- `attractor.DefaultHandlerRegistry()` — tracker auto-wires
- `attractor.RunResult` → `tracker.Result`

Add new gate response endpoint:
```go
// POST /projects/{id}/build/gate/{gateID}
mux.HandleFunc("POST /projects/{id}/build/gate/{gateID}", s.handleGateResponse)

func (s *Server) handleGateResponse(w http.ResponseWriter, r *http.Request) {
    gateID := r.PathValue("gateID")
    // Read answer from request body, call iv.Respond(gateID, answer)
}
```

- [ ] **Step 5: Rewrite web/build.go SSE formatting**

Replace `attractor.RunResult` references with `tracker.Result`. Update SSE event serialization to use `BuildEvent` instead of `attractor.EngineEvent`.

- [ ] **Step 6: Update web/server_test.go, web/build_test.go, web/integration_test.go**

- Replace attractor type construction in test helpers with tracker types
- Update event type assertions from `attractor.EngineEvent` to `BuildEvent`
- Remove tests for `detectBackendFromEnv` (file deleted)
- Update integration tests that drive builds to expect new event wire format

- [ ] **Step 7: Verify build and tests**

```bash
go build ./web/...
go test ./web/ -v -count=1
```
Expected: Clean build, PASS

- [ ] **Step 8: Commit**

```bash
git add web/server.go web/human_gate.go web/build.go web/server_test.go web/build_test.go web/integration_test.go
git commit -m "feat(web): rewrite build orchestration to use tracker library"
```

---

## Chunk 4: Rewrite MCP package

### Task 8: Rewrite mcp/ to use tracker

**Files:**
- Modify: `mcp/tool_run.go` — replace `executePipeline()` with `tracker.Run()`
- Modify: `mcp/tool_run_test.go` — update pipeline execution tests
- Modify: `mcp/tool_resume.go` — use `tracker.Config.CheckpointDir`
- Modify: `mcp/tool_resume_test.go` — update resume tests
- Modify: `mcp/tool_validate.go` — use `dot.Parse()` + `dot/validator.Lint()`
- Modify: `mcp/tool_validate_test.go` — update validation tests
- Modify: `mcp/events.go` — wire tracker event handlers
- Modify: `mcp/events_test.go` — update event handler tests
- Modify: `mcp/types.go` — `ActiveRun.Result` becomes `*tracker.Result`
- Modify: `mcp/types_test.go` — update type tests
- Modify: `mcp/registry.go` — update event type references
- Modify: `mcp/registry_test.go` — update registry tests
- Modify: `mcp/interviewer.go` — implement both Interviewer interfaces
- Modify: `mcp/interviewer_test.go` — update interviewer tests
- Modify: `mcp/tool_answer.go` — update to use new interviewer channel pattern
- Modify: `mcp/tool_answer_test.go` — update answer tool tests
- Modify: `mcp/tool_logs.go` — update event type references
- Modify: `mcp/tool_logs_test.go` — update log tool tests
- Modify: `mcp/tool_events.go` — update event type references
- Modify: `mcp/tool_events_test.go` — update event tool tests
- Delete: `mcp/backend.go` — `DetectBackend()` no longer needed
- Delete: `mcp/backend_test.go` — tests for removed function
- Modify: `mcp/integration_test.go` — update integration tests

- [ ] **Step 1: Read all MCP files to understand current attractor usage**

Read each source file and its test file to understand what attractor types they use. Key things to find:
- Which attractor types are referenced (EngineEvent, RunResult, EngineConfig, etc.)
- How the event buffer is structured
- How `mcpInterviewer` currently works
- How `tool_answer.go` delivers answers to the interviewer

- [ ] **Step 2: Delete mcp/backend.go and mcp/backend_test.go**

```bash
git rm mcp/backend.go mcp/backend_test.go
```

- [ ] **Step 3: Rewrite mcp/types.go**

Define a local `RunEvent` type for the MCP event buffer (mcp/ should NOT import web/):

```go
// RunEvent captures a pipeline or agent event for the MCP event buffer.
type RunEvent struct {
    Type      string         `json:"type"`
    Timestamp time.Time      `json:"timestamp"`
    NodeID    string         `json:"node_id,omitempty"`
    Message   string         `json:"message,omitempty"`
    Data      map[string]any `json:"data,omitempty"`
}
```

Replace `ActiveRun.Result` from `*attractor.RunResult` to `*tracker.Result`.

- [ ] **Step 4: Rewrite mcp/events.go**

Create two event handler factory functions:

```go
// newPipelineEventHandler returns a handler that converts pipeline events
// to RunEvent and appends to the run's event buffer.
func newPipelineEventHandler(run *ActiveRun) pipeline.PipelineEventHandlerFunc {
    return func(evt pipeline.PipelineEvent) {
        re := RunEvent{
            Type:      string(evt.Type),
            Timestamp: evt.Timestamp,
            NodeID:    evt.NodeID,
            Message:   evt.Message,
        }
        if evt.Err != nil {
            re.Data = map[string]any{"error": evt.Err.Error()}
        }
        run.appendEvent(re)
    }
}

// newAgentEventHandler returns a handler that converts agent events
// to RunEvent and appends to the run's event buffer.
func newAgentEventHandler(run *ActiveRun) agent.EventHandlerFunc {
    return func(evt agent.Event) {
        re := RunEvent{
            Type:      string(evt.Type),
            Timestamp: evt.Timestamp,
            NodeID:    evt.SessionID,
        }
        // Populate Data fields based on evt.Type (tool_name, text, metrics, etc.)
        run.appendEvent(re)
    }
}
```

- [ ] **Step 5: Rewrite mcp/interviewer.go**

Implement both `handlers.Interviewer` and `handlers.FreeformInterviewer` on `mcpInterviewer`. Same blocking channel pattern as `ChannelInterviewer`:

```go
type mcpInterviewer struct {
    run *ActiveRun
}

func (iv *mcpInterviewer) Ask(prompt string, choices []string, defaultChoice string) (string, error) {
    // Set run.PendingQuestion with prompt + choices
    // Set run.Status = StatusPaused
    // Block on run.answerCh
    // Clear PendingQuestion, set Status = StatusRunning
    // Return answer
}

func (iv *mcpInterviewer) AskFreeform(prompt string) (string, error) {
    // Same pattern but with no choices
}
```

- [ ] **Step 6: Update mcp/tool_answer.go**

The existing `tool_answer.go` sends answers via `run.answerCh`. Verify it's compatible with the new interviewer. It should work as-is since the channel pattern is preserved, but update any `attractor` type references.

- [ ] **Step 7: Rewrite mcp/tool_run.go**

Replace the entire `executePipeline()` function:

```go
func (s *Server) executePipeline(ctx context.Context, run *ActiveRun, source string) {
    // Pre-validate DOT for immediate feedback
    _, errs := dot.Parse(source)
    if len(errs) > 0 { /* set run error, return */ }

    iv := &mcpInterviewer{run: run}
    cfg := tracker.Config{
        WorkingDir:    run.WorkDir,
        CheckpointDir: run.CheckpointDir,
        ArtifactDir:   run.ArtifactDir,
        RetryPolicy:   run.RetryPolicy,
        Context:       run.Context,
        EventHandler:  newPipelineEventHandler(run),
        AgentEvents:   newAgentEventHandler(run),
        // Interviewer: iv, // once tracker merges Config.Interviewer
    }

    result, err := tracker.Run(ctx, source, cfg)
    // Update run.Result, run.Status, run.Err
}
```

- [ ] **Step 8: Rewrite mcp/tool_resume.go**

Simplify to use `tracker.Config.CheckpointDir` instead of manual checkpoint loading:

```go
cfg := tracker.Config{
    CheckpointDir: existingCheckpointDir,
    // ... same config as tool_run
}
result, err := tracker.Run(ctx, source, cfg)
// tracker automatically resumes from checkpoint if CheckpointDir has state
```

- [ ] **Step 9: Rewrite mcp/tool_validate.go**

Replace `attractor.Parse()` etc. with mammoth's own packages:

```go
graph, errs := dot.Parse(source)
if len(errs) > 0 { return errs }
lintResults := validator.Lint(graph)
```

- [ ] **Step 10: Update mcp/tool_logs.go, mcp/tool_events.go, mcp/registry.go**

- `tool_logs.go`: Replace `attractor.EngineEvent` references with `RunEvent`
- `tool_events.go`: Same replacement
- `registry.go`: Replace `attractor.EngineEvent` references

- [ ] **Step 11: Update all test files**

Update test files to use new types:
- `tool_run_test.go`: Replace attractor config construction with tracker.Config
- `tool_resume_test.go`: Replace checkpoint loading assertions
- `tool_validate_test.go`: Replace attractor.Parse references
- `events_test.go`: Replace event type constants
- `types_test.go`: Replace RunResult references
- `registry_test.go`: Replace EngineEvent references
- `interviewer_test.go`: Update to test both Ask and AskFreeform
- `tool_answer_test.go`: Verify answer channel still works
- `tool_logs_test.go`, `tool_events_test.go`: Update event types
- `integration_test.go`: Update end-to-end tests

- [ ] **Step 12: Verify build and run MCP tests**

```bash
go build ./mcp/...
go test ./mcp/ -v -count=1
```
Expected: Clean build, PASS

- [ ] **Step 13: Commit**

```bash
git add mcp/
git commit -m "feat(mcp): rewrite pipeline tools to use tracker library"
```

---

## Chunk 5: Rewrite TUI package

### Task 9: Rewrite TUI to use tracker types

**Files:**
- Modify: `tui/messages.go` — new message types for tracker events
- Modify: `tui/bridge.go` — simplified; takes `*tracker.Engine`
- Modify: `tui/app.go` — replace `*attractor.Engine` with `*tracker.Engine`
- Modify: `tui/stream.go` — replace event type switches
- Modify: `tui/human_gate.go` — implement `handlers.Interviewer` + `handlers.FreeformInterviewer`
- Modify: `tui/graph_panel.go` — replace `*attractor.Graph` with `*dot.Graph`
- Modify: `tui/log_panel.go` — replace event type switches
- Modify all corresponding `*_test.go` files

- [ ] **Step 1: Read all TUI files to understand attractor usage**

Read each file to find every attractor type reference. Key things to catalog:
- `attractor.Engine` → `tracker.Engine`
- `attractor.Graph` → `dot.Graph`
- `attractor.EngineEvent` → split into pipeline/agent events
- `attractor.RunResult` → `tracker.Result`
- `attractor.Interviewer` → `handlers.Interviewer` + `handlers.FreeformInterviewer`

- [ ] **Step 2: Rewrite tui/messages.go**

Replace message types:
```go
import (
    "github.com/2389-research/tracker/agent"
    "github.com/2389-research/tracker/pipeline"
    "github.com/2389-research/tracker"
)

// EngineEventMsg wraps either a pipeline event or agent event from tracker.
type EngineEventMsg struct {
    PipelineEvent *pipeline.PipelineEvent
    AgentEvent    *agent.Event
}

// PipelineResultMsg carries the final pipeline result.
type PipelineResultMsg struct {
    Result *tracker.Result
    Err    error
}
```

- [ ] **Step 3: Rewrite tui/bridge.go**

```go
// RunPipelineCmd returns a tea.Cmd that runs the tracker engine.
func RunPipelineCmd(engine *tracker.Engine) tea.Cmd {
    return func() tea.Msg {
        result, err := engine.Run(context.Background())
        return PipelineResultMsg{Result: result, Err: err}
    }
}
```

Remove these functions (no longer needed):
- `RunPipelineGraphCmd` — tracker parses DOT internally
- `ResumeFromCheckpointCmd` — tracker handles via Config.CheckpointDir
- `WireHumanGate` — interviewer passed via Config, not monkey-patched

The `EventBridge` should be split into two handler functions:
```go
func (b *EventBridge) PipelineHandler() pipeline.PipelineEventHandlerFunc {
    return func(evt pipeline.PipelineEvent) {
        b.program.Send(EngineEventMsg{PipelineEvent: &evt})
    }
}

func (b *EventBridge) AgentHandler() agent.EventHandlerFunc {
    return func(evt agent.Event) {
        b.program.Send(EngineEventMsg{AgentEvent: &evt})
    }
}
```

- [ ] **Step 4: Rewrite tui/human_gate.go**

Implement `handlers.Interviewer` and `handlers.FreeformInterviewer`:

```go
import "github.com/2389-research/tracker/pipeline/handlers"

type tuiInterviewer struct {
    program *tea.Program
    // channel for receiving answers from the TUI
    answerCh chan string
}

func (iv *tuiInterviewer) Ask(prompt string, choices []string, defaultChoice string) (string, error) {
    iv.program.Send(HumanGateMsg{Prompt: prompt, Choices: choices, Default: defaultChoice})
    return <-iv.answerCh, nil
}

func (iv *tuiInterviewer) AskFreeform(prompt string) (string, error) {
    iv.program.Send(HumanGateMsg{Prompt: prompt, Freeform: true})
    return <-iv.answerCh, nil
}
```

- [ ] **Step 5: Rewrite tui/app.go**

- Replace `*attractor.Engine` with `*tracker.Engine`
- Replace `*attractor.Graph` — use `*dot.Graph` for TUI display, parsed from the DOT source string
- Remove `GetHandler("wait.human")` monkey-patching — interviewer is passed via Config
- Construct `tracker.Config` with `bridge.PipelineHandler()`, `bridge.AgentHandler()`, and `tuiInterviewer`

- [ ] **Step 6: Rewrite tui/stream.go**

Replace all `attractor.EngineEvent` type switches with `EngineEventMsg` pattern matching:

```go
case msg EngineEventMsg:
    if msg.PipelineEvent != nil {
        switch msg.PipelineEvent.Type {
        case pipeline.EventStageStarted:
            // update node status in graph panel
        case pipeline.EventStageCompleted:
            // mark node complete
        // ...
        }
    }
    if msg.AgentEvent != nil {
        switch msg.AgentEvent.Type {
        case agent.EventToolCallStart:
            // show tool call in log panel
        case agent.EventTextDelta:
            // append text to log panel
        // ...
        }
    }
```

- [ ] **Step 7: Rewrite tui/graph_panel.go and tui/log_panel.go**

- `graph_panel.go`: Replace `*attractor.Graph` with `*dot.Graph`. Node status lookups change from `attractor.NodeStatus` to `dot.NodeStatus` (same types via alias).
- `log_panel.go`: Replace `attractor.EngineEvent` switches with `EngineEventMsg` pipeline/agent event pattern matching.

- [ ] **Step 8: Update all test files**

Update test helpers, mock events, and assertions to use new types. Key changes:
- Construct `pipeline.PipelineEvent` and `agent.Event` instead of `attractor.EngineEvent`
- Replace `attractor.RunResult` with `tracker.Result` in assertions
- Update message type assertions for `EngineEventMsg` and `PipelineResultMsg`

- [ ] **Step 9: Verify build and run TUI tests**

```bash
go build ./tui/...
go test ./tui/ -v -count=1
```
Expected: Clean build, PASS

- [ ] **Step 10: Commit**

```bash
git add tui/
git commit -m "feat(tui): rewrite to use tracker library and dot/ types"
```

---

## Chunk 6: Rewrite CLI and delete attractor/

### Task 10: Rewrite cmd/mammoth/main.go

**Files:**
- Modify: `cmd/mammoth/main.go`
- Modify: `cmd/mammoth/main_test.go` — update CLI test assertions
- Modify: `cmd/mammoth/setup_test.go` — update setup tests if they reference attractor

**Decision: `--server` mode is DROPPED.** The CLI's `--server` flag uses `attractor.PipelineServer` which is being deleted. The web layer (`web.Server`) already serves the same purpose and is the correct way to run mammoth as a server. The `mammoth serve` command (which starts `web.Server`) replaces `mammoth run --server`.

- [ ] **Step 1: Read the full main.go**

Read `cmd/mammoth/main.go` to catalog all attractor usage. Key functions to map:

| Function | Uses | Replacement |
|----------|------|-------------|
| `runPipeline()` | dispatches fresh/resume | single path via `tracker.Run()` |
| `runPipelineFresh()` | `attractor.NewEngine()` | `tracker.Run()` or `tracker.NewEngine()` |
| `runPipelineResume()` | `engine.ResumeFromCheckpoint()` | `tracker.Run()` with `Config.CheckpointDir` |
| `runPipelineWithTUI()` | `attractor.NewEngine()` + TUI bridge | `tracker.NewEngine()` + TUI bridge |
| `detectBackend()` | selects codergen backend | DELETED — tracker auto-wires |
| `wireInterviewer()` | monkey-patches handler registry | DELETED — passed via Config |
| `retryPolicyFromName()` | maps string → attractor retry policy | DELETED — `tracker.Config.RetryPolicy` takes string directly |
| `buildEventHandler()` | creates attractor event handler | replaced by tracker event handler factories |
| `buildPipelineServer()` | creates `attractor.PipelineServer` | DELETED — use `mammoth serve` instead |
| `runServer()` | starts HTTP server from PipelineServer | DELETED — use `mammoth serve` instead |
| `validatePipeline()` | `attractor.Parse()` + validate | `dot.Parse()` + `dot/validator.Lint()` |
| `runAudit()` | `attractor.FSRunStateStore` | `runstate.FSRunStateStore` |

- [ ] **Step 2: Collapse pipeline execution**

```go
func runPipeline(ctx context.Context, cfg config, source string) int {
    // Console interviewer for CLI
    iv := &consoleInterviewer{} // or handlers.NewConsoleInterviewer()

    tcfg := tracker.Config{
        WorkingDir:    cfg.workDir,
        CheckpointDir: cfg.checkpointDir,
        ArtifactDir:   cfg.artifactDir,
        RetryPolicy:   cfg.retryPolicy, // string passed directly
        Context:       cfg.context,
        EventHandler:  newCLIEventHandler(cfg.verbose),
        AgentEvents:   newCLIAgentHandler(cfg.verbose),
        // Interviewer: iv, // once tracker merges Config.Interviewer
    }

    result, err := tracker.Run(ctx, source, tcfg)
    if err != nil {
        fmt.Fprintf(os.Stderr, "error: %v\n", err)
        return 1
    }
    // Store result in runstate if needed
    return 0
}
```

- [ ] **Step 3: Remove deleted functions and flags**

Delete these functions entirely:
- `detectBackend()`
- `wireInterviewer()`
- `retryPolicyFromName()`
- `buildEventHandler()`
- `buildPipelineServer()`
- `runServer()`

Remove these CLI flags:
- `--backend` — tracker auto-detects
- `--base-url` — was for ClaudeCodeBackend
- `--server` — use `mammoth serve` instead

- [ ] **Step 4: Update validatePipeline()**

```go
func validatePipeline(source string) int {
    graph, errs := dot.Parse(source)
    if len(errs) > 0 {
        for _, e := range errs { fmt.Fprintln(os.Stderr, e) }
        return 1
    }
    results := validator.Lint(graph)
    // Print results
    return 0
}
```

- [ ] **Step 5: Update runAudit()**

Replace `attractor.FSRunStateStore` with `runstate.FSRunStateStore`:
```go
store, err := runstate.NewFSRunStateStore(runsDir)
```

- [ ] **Step 6: Update test files**

- `main_test.go`: Update CLI invocation tests, remove `--server` mode tests, update event type assertions
- `setup_test.go`: Update if it references attractor types

- [ ] **Step 7: Verify build and tests**

```bash
go build ./cmd/mammoth/
go test ./cmd/mammoth/ -v -count=1
```
Expected: Clean build, PASS

- [ ] **Step 8: Commit**

```bash
git add cmd/mammoth/
git commit -m "feat(cmd): rewrite mammoth CLI to use tracker library

Drop --server flag (use 'mammoth serve' instead).
Drop --backend and --base-url flags (tracker auto-detects)."
```

---

### Task 11: Delete attractor/ and cmd/mammoth-conformance/

**Files:**
- Delete: `attractor/` (entire directory)
- Delete: `cmd/mammoth-conformance/` (entire directory)

- [ ] **Step 1: Verify no remaining attractor imports**

```bash
grep -r '"github.com/2389-research/mammoth/attractor"' --include='*.go' . | grep -v attractor/ | grep -v vendor/
```
Expected: No output (no files outside attractor/ import it)

- [ ] **Step 2: Delete attractor directory**

```bash
git rm -r attractor/
```

- [ ] **Step 3: Delete conformance CLI**

```bash
git rm -r cmd/mammoth-conformance/
```

- [ ] **Step 4: Run go mod tidy**

```bash
go mod tidy
```

- [ ] **Step 5: Run full test suite**

```bash
go test ./... -count=1
```
Expected: ALL PASS

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "chore: delete attractor/ and cmd/mammoth-conformance/

Replaced by github.com/2389-research/tracker as the DOT pipeline runner.
See docs/superpowers/specs/2026-03-13-tracker-integration-design.md"
```

---

### Task 12: Update documentation and CLAUDE.md

**Files:**
- Modify: `CLAUDE.md`
- Modify: `README.md`

- [ ] **Step 1: Update CLAUDE.md project structure**

Update the project structure section to remove `attractor/` and add `runstate/`. Update the description to mention tracker as the pipeline runner.

- [ ] **Step 2: Update README.md**

Update any references to attractor with tracker. Update architecture description.

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md README.md
git commit -m "docs: update project docs for tracker integration"
```

---

### Task 13: Final verification

- [ ] **Step 1: Full build**

```bash
go build ./...
```
Expected: Clean build — no compilation errors

- [ ] **Step 2: Full test suite**

```bash
go test ./... -count=1
```
Expected: ALL PASS — no test failures

- [ ] **Step 3: Verify no attractor imports remain**

```bash
grep -rn '"github.com/2389-research/mammoth/attractor"' --include='*.go' . | grep -v vendor/
```
Expected: No output — no Go files import attractor

- [ ] **Step 4: Verify no attractor directory exists**

```bash
ls attractor/ 2>&1
```
Expected: "No such file or directory"

- [ ] **Step 5: Verify no conformance CLI exists**

```bash
ls cmd/mammoth-conformance/ 2>&1
```
Expected: "No such file or directory"

- [ ] **Step 6: Run go vet**

```bash
go vet ./...
```
Expected: Clean — no vet warnings

- [ ] **Step 7: Run go mod tidy and verify no diff**

```bash
go mod tidy && git diff go.mod go.sum
```
Expected: No diff (already tidy)
