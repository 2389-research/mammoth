# Build Console View Design

## Goal

Add a real-time streaming console to the build page that shows LLM calls as they stream back and forth, similar to watching Claude Code work. Lets the user know things are happening.

## Data Flow

The agent event → engine event → SSE → browser pipeline already exists for tool calls and turn completions. Three paths need to be lit up:

1. **LLM text streaming**: Agent loop emits `EventAssistantTextDelta` → bridge forwards as `EventAgentTextDelta` → SSE → browser appends tokens
2. **LLM turn bookends**: `EventAssistantTextStart`/`EventAssistantTextEnd` → bridge → SSE → browser shows "thinking..." / summary
3. **Tool call inputs**: Enrich existing `EventToolCallStart` with tool arguments

## New Engine Event Types

```
agent.text.start  — LLM begins generating (node, model info)
agent.text.delta  — streaming tokens (batched ~50ms)
agent.text.end    — LLM turn done (token count summary)
```

Existing `agent.tool_call.start` and `agent.tool_call.end` stay, enriched with input args on start.

## Agent Loop Changes

- Emit `EventAssistantTextStart` when LLM response begins
- Emit `EventAssistantTextDelta` with batched token chunks (~50ms accumulation)
- `EventAssistantTextEnd` already emitted, keep as-is
- `EventToolCallStart` already emitted, add `arguments` field to Data

## Backend Bridge Changes

Add cases in `bridgeSessionEvent()` for:
- `EventAssistantTextStart` → `EngineEvent{Type: "agent.text.start"}`
- `EventAssistantTextDelta` → `EngineEvent{Type: "agent.text.delta", Data: {text: "..."}}`
- Already handles `EventAssistantTextEnd` → `EventAgentLLMTurn` (keep)

## SSE Changes

None needed — `engineEventToSSE()` is generic, handles any event type.

## Bandwidth Mitigation

- **Batch deltas**: 50ms accumulation window before emitting, reduces SSE event count ~20x
- **Don't persist deltas**: progress.ndjson only logs turn-level events, deltas are ephemeral SSE-only

## UI: Console Panel

### Layout

Tab-based switcher on the build page: `[Metrics] [Console]`. Console is the new default.

Full-width, dark background (#0B1821), monospace IBM Plex Mono.

### Visual Hierarchy

```
node:lint ▸ agent thinking...                       (amber header)
│ Looking at the golangci-lint configuration.       (muted gray, indented)
│ I'll run the linter first to see what issues      (streams in live)
│ exist in the codebase, then fix them one by one.
│
node:lint ▸ tool_call: bash                         (amber header)
│ $ golangci-lint run ./...                         (white, monospace)
│ > main.go:42: unused variable 'foo'               (dim output)
│ > (exit 1, 0.8s)
│
node:lint ▸ agent thinking...                       (amber header)
│ Found one lint issue. Let me fix the unused var   (streams live)
│ iable on line 42█                                 (blinking cursor)
```

### Formatting Rules

- **Node headers** — amber bold: `node:{id} ▸ agent thinking...` or `node:{id} ▸ tool_call: {name}`
- **LLM text** — muted gray, indented with `│ ` prefix, streams token by token
- **Tool call input** — white monospace, `$ ` prefix for commands
- **Tool output** — dim, `> ` prefix, truncated to 500 chars with expand toggle
- **Turn summary** — dim italic: `(1,204 tokens, 0.8s)`
- **Auto-scroll** — scrolls to bottom by default
- **Scroll lock** — if user scrolls up, auto-scroll pauses; click "Resume" to re-enable

### CSS

Add `.build-console` styles to `web/static/css/build.css`:
- Dark bg matching Cold Launch palette
- Monospace font (IBM Plex Mono)
- Indented line styling with left border
- Amber accents for headers
- Smooth scroll behavior

### JavaScript

Add console handler functions to build_view.html:
- `appendConsoleHeader(nodeId, type)` — adds amber header line
- `appendConsoleText(text)` — appends streamed tokens to current block
- `appendConsoleToolInput(name, args)` — shows tool call with arguments
- `appendConsoleToolOutput(output, duration)` — shows tool result
- `appendConsoleSummary(tokens, duration)` — turn summary line
- Auto-scroll management with scroll position detection

## Files to Change

### Backend (Go)
- `agent/loop.go` — emit `EventAssistantTextStart`, `EventAssistantTextDelta`
- `agent/events.go` — no changes needed (types already defined)
- `attractor/backend_agent.go` — bridge new events in `bridgeSessionEvent()`
- `attractor/engine.go` — add new `EventAgentTextStart`, `EventAgentTextDelta` constants

### Frontend (HTML/CSS/JS)
- `web/templates/build_view.html` — add console panel HTML + tab switcher + JS handlers
- `web/static/css/build.css` — add console panel styles

### No changes needed
- `web/server.go` — SSE conversion is generic
- `web/build.go` — `engineEventToSSE()` handles any event type
- `attractor/progress.go` — skip persisting delta events (filter by type)
