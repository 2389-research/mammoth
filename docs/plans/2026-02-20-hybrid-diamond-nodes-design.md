# Hybrid Diamond Nodes

## Problem

Diamond nodes (`shape=diamond`) in the attractor engine are pure pass-throughs.
The `ConditionalHandler` never reads the `prompt` attribute, never dispatches to
an LLM backend, and echoes the previous node's outcome from the pipeline context.

This breaks TDD verification pipelines. Nodes like `verify_entity_graph_red`
have prompts instructing an agent to run tests and report pass/fail, but the
engine skips execution entirely. The conditional edges
(`condition="outcome=FAIL"`) route based on the *previous* node's outcome
instead of an actual verification result.

## Solution

Make `ConditionalHandler` prompt-aware (hybrid). When a diamond node has a
`prompt` attribute and a backend is wired, run the agent first, then use the
agent's reported outcome for conditional edge routing. When no prompt is present,
keep the current pass-through behavior.

## Design

### ConditionalHandler Changes

Add a `Backend` field (type `CodergenBackend`) and supporting fields (`BaseURL`,
`EventHandler`) mirroring `CodergenHandler`.

Execute logic:

1. If `prompt` attribute is empty/missing → current pass-through behavior
2. If `prompt` is present and `Backend` is nil → return `StatusFail` with
   config error (same as CodergenHandler)
3. If `prompt` is present and `Backend` is set:
   a. Build `AgentRunConfig` from node attributes (prompt, model, provider, etc.)
   b. Call `Backend.RunAgent(ctx, config)`
   c. Parse agent output with `DetectOutcomeMarker(result.Output)`
   d. Determine status:
      - Explicit outcome marker found → use it (`"fail"` → StatusFail, else StatusSuccess)
      - No marker, agent `Success: false` → StatusFail
      - No marker, agent `Success: true` → StatusSuccess
   e. Set `outcome` in context updates so downstream nodes see the correct value

### Engine Wiring

The backend wiring block in `engine.go` (Run and executeGraph) needs to also
wire into the `ConditionalHandler`, same pattern as `CodergenHandler`:

```go
if condHandler := registry.Get("conditional"); condHandler != nil {
    if ch, ok := unwrapHandler(condHandler).(*ConditionalHandler); ok {
        ch.Backend = e.config.Backend
        ch.BaseURL = e.config.BaseURL
        ch.EventHandler = e.emitEvent
    }
}
```

### What Stays the Same

- Edge selection algorithm (already evaluates conditions against node outcome)
- Shape-to-handler mapping (`diamond` → `conditional`)
- Box nodes and all other handlers
- Diamond nodes without prompts
- `DetectOutcomeMarker` function (already exists in backend.go)

### Outcome Detection Precedence

1. Explicit `OUTCOME:FAIL` / `OUTCOME:PASS` / `OUTCOME:SUCCESS` in agent output
2. Agent result `Success` field (false → fail, true → success)

This matches how pipeline authors already write verification prompts: "Run the
tests. If they pass, output OUTCOME:PASS. If they fail, output OUTCOME:FAIL."
