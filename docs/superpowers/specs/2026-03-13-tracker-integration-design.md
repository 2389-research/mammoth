# Replace attractor/ with tracker library

**Date:** 2026-03-13
**Status:** Draft
**Author:** SKULLCRUSHER McBYTES

## Summary

Replace mammoth's `attractor/` engine package with `github.com/2389-research/tracker` as the DOT pipeline runner. Tracker now exposes a top-level library API (`tracker.Run()`, `tracker.NewEngine()`) that auto-wires LLM clients, handler registries, and execution environments. This eliminates ~50 files in `attractor/` and simplifies every consumer (web, CLI, TUI, MCP).

## Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Replacement strategy | Big bang — delete attractor/, rewrite consumers | No external consumers; adapter layer is throwaway work |
| DOT parser | Keep mammoth's `dot/` for editor; tracker parses internally | Two parsers, different purposes; tracker accepts DOT strings |
| Event model | New `web.BuildEvent` unifying tracker's two event streams | Single SSE wire format; isolates frontend from upstream changes |
| Human gates | Blocking `ChannelInterviewer` surfaced as SSE events | Not auto-approved; user responds via HTTP POST or MCP tool |
| ClaudeCodeBackend | Dropped | Not actively used; tracker's built-in agent loop covers the use case |
| Conformance CLI | Deferred | Can be rebuilt later against tracker's types |

## Tracker Dependency

Tracker's top-level API (`tracker.Config`, `tracker.Result`, `tracker.Engine`) provides:

- `Run(ctx, dotSource, Config) (*Result, error)` — one-call convenience
- `NewEngine(dotSource, Config) (*Engine, error)` — manual lifecycle control
- `Config.LLMClient` — bring your own `agent.Completer` or auto-detect from env
- `Config.EventHandler` — `pipeline.PipelineEventHandler` for pipeline lifecycle events
- `Config.AgentEvents` — `agent.EventHandler` for agent-level events (tool calls, LLM turns)
- `Config.RetryPolicy` — string name passed through to tracker's retry policy registry. Documented values: "none", "standard", "aggressive". Additional values "patient" and "linear" are supported by the internal registry.
- `Config.Context` — initial pipeline context as `map[string]string`
- `Config.CheckpointDir` — enables checkpoint save/resume
- `Config.ArtifactDir` — output artifact directory
- `Config.Model` / `Config.Provider` — override defaults (DOT graph attrs take precedence)

### Tracker-side request (blocking)

Add `Interviewer handlers.Interviewer` field to `tracker.Config`, passed through to `handlers.WithInterviewer()` during registry construction. When nil and a human gate is reached, the handler must return an error (not auto-approve). BBS thread posted with details.

**Fallback if not merged:** Use the lower-level `pipeline.NewEngine()` API directly, constructing the handler registry manually via `handlers.NewDefaultRegistry(graph, handlers.WithInterviewer(iv, graph), handlers.WithLLMClient(...), ...)`. This loses the auto-wiring convenience but preserves full control. The `Config.Interviewer` approach is preferred because it avoids duplicating tracker's internal wiring logic.

## Architecture

### Unified Event Type

A new `web.BuildEvent` type serves as mammoth's SSE wire format, replacing `attractor.EngineEvent`:

```go
type BuildEventType string

const (
    // Pipeline lifecycle (mapped from pipeline.PipelineEvent)
    // Source constants: pipeline.EventPipelineStarted, EventStageStarted, etc.
    // Note: tracker uses "stage_*" naming; we map to "node_*" for the SSE wire format.
    BuildEventPipelineStarted   BuildEventType = "pipeline_started"
    BuildEventPipelineCompleted BuildEventType = "pipeline_completed"
    BuildEventPipelineFailed    BuildEventType = "pipeline_failed"
    BuildEventNodeStarted       BuildEventType = "node_started"   // from stage_started
    BuildEventNodeCompleted     BuildEventType = "node_completed" // from stage_completed
    BuildEventNodeFailed        BuildEventType = "node_failed"    // from stage_failed
    BuildEventNodeRetrying      BuildEventType = "node_retrying"  // from stage_retrying
    BuildEventCheckpointSaved   BuildEventType = "checkpoint_saved"
    BuildEventParallelStarted   BuildEventType = "parallel_started"
    BuildEventParallelCompleted BuildEventType = "parallel_completed"
    BuildEventLoopRestart       BuildEventType = "loop_restart"
    // interview_started/interview_completed are not surfaced here because
    // ChannelInterviewer emits human_gate_* events directly.

    // Agent activity (mapped from agent.Event)
    // Only a subset of tracker's 19 agent event types are surfaced to the browser.
    // Mapped: tool_call_start, tool_call_end, text_delta, turn_metrics, session_start,
    //         session_end, error
    // Dropped (internal detail): turn_start, turn_end, llm_request_start, llm_reasoning,
    //         llm_text, llm_tool_prepare, llm_finish, llm_provider_raw, tool_cache_hit,
    //         context_compaction, steering_injected, context_window_warning
    BuildEventToolCallStart  BuildEventType = "tool_call_start"
    BuildEventToolCallEnd    BuildEventType = "tool_call_end"
    BuildEventTextDelta      BuildEventType = "text_delta"
    BuildEventTurnMetrics    BuildEventType = "turn_metrics"
    BuildEventSessionStart   BuildEventType = "session_start"
    BuildEventSessionEnd     BuildEventType = "session_end"
    BuildEventAgentError     BuildEventType = "agent_error"

    // Human gates
    BuildEventHumanGateChoice   BuildEventType = "human_gate_choice"
    BuildEventHumanGateFreeform BuildEventType = "human_gate_freeform"
    BuildEventHumanGateAnswered BuildEventType = "human_gate_answered"
)

type BuildEvent struct {
    Type      BuildEventType `json:"type"`
    Timestamp time.Time      `json:"timestamp"`
    NodeID    string         `json:"node_id,omitempty"`
    Message   string         `json:"message,omitempty"`
    Data      map[string]any `json:"data,omitempty"`
}
```

Two mapper functions convert tracker events:

- `buildEventFromPipeline(pipeline.PipelineEvent) BuildEvent`
- `buildEventFromAgent(agent.Event) BuildEvent`

### Human Gate: ChannelInterviewer

Implements both `handlers.Interviewer` (multiple-choice) and `handlers.FreeformInterviewer` (open-ended text). Blocks the pipeline handler and surfaces the gate as a `BuildEvent` to consumers.

```go
type ChannelInterviewer struct {
    broadcast func(BuildEvent)
    pending   map[string]chan string  // gateID -> response channel
    mu        sync.Mutex
}

func (iv *ChannelInterviewer) Ask(prompt string, choices []string, defaultChoice string) (string, error)
func (iv *ChannelInterviewer) AskFreeform(prompt string) (string, error)
func (iv *ChannelInterviewer) Respond(gateID, answer string) error
```

Web layer: new `POST /projects/{id}/build/gate/{gateID}` endpoint calls `Respond()`.
MCP layer: existing `answer_gate` tool routes to the MCP interviewer's `Respond()`.

### Web Layer Integration

`startBuildExecution()` constructs a `tracker.Config` with:

- `ChannelInterviewer` for human gates
- `PipelineEventHandlerFunc` that broadcasts `BuildEvent` to SSE subscribers
- `agent.EventHandlerFunc` that broadcasts `BuildEvent` to SSE subscribers

No more `detectBackendFromEnv()`, `DefaultHandlerRegistry()`, or `CodergenBackend`.

### MCP Package

`executePipeline()` uses `tracker.Run()` directly. The `mcpInterviewer` implements both `handlers.Interviewer` (for choice-mode gates) and `handlers.FreeformInterviewer` (for open-ended text gates) with the same blocking channel pattern. `mcp/backend.go` is deleted.

### TUI

- `tui/messages.go` — message types reference tracker's event types instead of attractor's
- `tui/bridge.go` — simplified; `RunPipelineCmd` takes `*tracker.Engine`; `WireHumanGate` removed (interviewer passed via Config)
- `tui/human_gate.go` — implements `handlers.Interviewer` + `handlers.FreeformInterviewer`
- Event panels switch on tracker event types

### CLI

Massive simplification. `runPipeline` collapses `runPipelineFresh`/`runPipelineResume` into a single path using `tracker.Run()`. Removed: `detectBackend()`, `wireInterviewer()`, `retryPolicyFromName()`, `buildEventHandler()`, `--backend` flag, `--base-url` flag.

### Run State Store

`attractor.FSRunStateStore` is extracted to a new `runstate/` package. Stores `tracker.Result` instead of `attractor.RunResult`. Supports auto-resume (by source hash) and audit command. On-disk format: one JSON file per run in `<dataDir>/runs/<runID>/state.json`, with an `events.jsonl` file alongside for event history. Old attractor-format runs will not be loadable (acceptable — they can be deleted).

## Scope

### Deleted

- `attractor/` — all ~50 files
- `cmd/mammoth-conformance/` — deferred
- `web/backend.go`
- `mcp/backend.go`

### New files

- `web/build_event.go` — BuildEvent type and mappers
- `web/human_gate.go` — rewritten with ChannelInterviewer
- `runstate/store.go` — extracted run state persistence

### Rewritten

- `web/server.go` — build orchestration uses tracker
- `web/build.go` — SSE formatting uses BuildEvent
- `mcp/tool_run.go` — uses tracker.Run()
- `mcp/events.go` — tracker event types
- `mcp/interviewer.go` — implements handlers.FreeformInterviewer
- `mcp/types.go` — references tracker.Result
- `mcp/registry.go` — ActiveRun.Result type change
- `tui/bridge.go`, `tui/messages.go`, `tui/stream.go`, `tui/app.go` — tracker types
- `tui/human_gate.go`, `tui/graph_panel.go`, `tui/log_panel.go` — tracker events
- `cmd/mammoth/main.go` — uses tracker.Run()/NewEngine()

### Unchanged

- `dot/` — mammoth's parser, used by editor/validator/render
- `editor/` — no attractor dependency
- `spec/` — no attractor dependency
- `llm/` — mammoth's LLM layer (audit, dot_fixer, spec agents)
- `agent/` — mammoth's agent (only imported by `attractor/`, so becomes fully unused after deletion; keep for now, remove in follow-up)
- `render/` — graph rendering

## Testing

- Unit tests for `BuildEvent` mapper functions
- Unit tests for `ChannelInterviewer` (Ask, AskFreeform, Respond, timeout, concurrent gates)
- Integration test: construct `tracker.Engine` with `ChannelInterviewer`, run a DOT with a human gate, verify blocking + response
- Web handler tests for gate POST endpoint
- MCP tool tests for pipeline execution with tracker
- TUI model tests with tracker event types
- CLI end-to-end: validate, run fresh, run with checkpoint resume

## Risks

| Risk | Mitigation |
|------|------------|
| Tracker `Config.Interviewer` not merged yet | BBS posted; small change (~5 lines); we can use lower-level `pipeline.NewEngine()` as fallback |
| Event type mapping gaps | Tracker's event types are well-documented; map conservatively, add types as needed |
| Run state store migration | Keep the same on-disk format where possible; old runs won't be loadable (acceptable) |
| mammoth's `agent/` package becomes unused | Keep for now; remove in a follow-up if confirmed unused |
