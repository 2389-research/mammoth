# Mammoth MCP Server Design

Expose the attractor DOT pipeline runner as a general-purpose MCP server over stdio. Any MCP client (Claude Code, Cursor, custom agents) can discover and run mammoth pipelines.

Scope: DOT runner only. Not the spec generation stack.

## Architecture

```
┌─────────────────┐     stdio      ┌──────────────────┐
│   MCP Client    │◄──────────────►│  cmd/mammoth-mcp │
│ (Claude, Cursor) │                │     main.go      │
└─────────────────┘                └────────┬─────────┘
                                            │
                                   ┌────────▼─────────┐
                                   │    mcp/ package   │
                                   │  - tool handlers  │
                                   │  - run registry   │
                                   │  - run index (fs) │
                                   └────────┬─────────┘
                                            │
                                   ┌────────▼─────────┐
                                   │ attractor.Engine  │
                                   │  (existing API)   │
                                   └──────────────────┘
```

- **`cmd/mammoth-mcp/main.go`** — Entrypoint. Creates MCP server via official SDK, registers tools, serves over stdio.
- **`mcp/` package** — Tool handler implementations, `RunRegistry` (in-memory active runs), `RunIndex` (disk-backed run metadata for resume).
- **`attractor.Engine`** — Unchanged. One engine instance per pipeline run.

Pipelines run in goroutines. The MCP server stays responsive while pipelines execute in the background.

## MCP Library

`github.com/modelcontextprotocol/go-sdk` v1.4.0 — official SDK, Tier 1 certified (100% spec conformance), stable v1.x API, backed by Anthropic + Google.

## Tools

Seven tools exposed:

| Tool | Description | Key Params | Returns |
|------|-------------|------------|---------|
| `run_pipeline` | Start a pipeline execution | `source` (DOT string) OR `file` (path), optional `config` (retry policy, backend) | Run ID, initial status |
| `validate_pipeline` | Lint/check DOT without executing | `source` OR `file` | Errors, warnings, auto-fix suggestions |
| `get_run_status` | Query a run's current state | `run_id` | Status, current node, completed nodes, current activity, pending question |
| `get_run_events` | Fetch events for a run | `run_id`, optional `since` (timestamp), optional `types` (filter) | Array of engine events |
| `get_run_logs` | Get console output | `run_id`, optional `tail` (last N lines), optional `node_id` (filter to node) | Log lines |
| `answer_question` | Unblock a human gate | `run_id`, `answer` (string) | Acknowledgment, pipeline resumes |
| `resume_pipeline` | Resume a checkpointed run | `run_id` | New run ID, initial status |

### Behavioral notes

- `run_pipeline` validates DOT synchronously, then spawns execution in a goroutine. Returns immediately with run ID.
- `get_run_status` includes `current_activity` — a snapshot of what the agent is doing right now (e.g. "calling tool: write_file", "LLM generating response", "waiting for human input").
- `get_run_logs` pulls from the `events.jsonl` the engine already writes, with tail/node filtering.
- `validate_pipeline` is fully synchronous.

## State Management

### In-memory: RunRegistry

Tracks active runs:

```
RunRegistry
  runs map[string]*ActiveRun

ActiveRun
  ID              string
  Status          "running" | "paused" | "completed" | "failed"
  Engine          *attractor.Engine
  Graph           *dot.Graph
  Source          string
  Config          RunConfig
  CurrentNode     string
  CurrentActivity string
  CompletedNodes  []string
  PendingQuestion *Question
  EventBuffer     []EngineEvent    // rolling buffer, last ~500
  Cancel          context.CancelFunc
  Result          *attractor.RunResult
```

The `EventHandler` callback wired into each engine updates `CurrentNode`, `CurrentActivity`, `CompletedNodes`, and appends to `EventBuffer` in real time.

Protected by `sync.RWMutex` — reads don't block each other, writes are serialized. Multiple pipelines can run concurrently.

### On-disk: RunIndex

Survives restarts, enables resume:

```
~/.mammoth/mcp-runs/
  index.json              // run_id → metadata mapping
  {run_id}/
    source.dot            // original DOT source
    config.json           // engine config used
    checkpoint/           // attractor checkpoint files
    events.jsonl          // full event log
```

When a run completes or checkpoints, the index is updated. On `resume_pipeline`, the server reads `source.dot` + `config.json` + latest checkpoint and hands them to `engine.ResumeFromCheckpoint()`.

## Human Gate Flow

1. Engine hits hexagon node → handler calls interviewer strategy
2. Interviewer blocks on a channel
3. Registry sets `Status = "paused"`, populates `PendingQuestion`
4. Client calls `get_run_status`, sees the question
5. Client calls `answer_question` with response
6. Registry sends answer on channel, interviewer unblocks, pipeline continues

## Configuration

All via environment variables (standard MCP server pattern, no flags):

- `MAMMOTH_DATA_DIR` — override `~/.mammoth/mcp-runs/` location
- `MAMMOTH_BACKEND` — force backend (agent, claude-code)
- `MAMMOTH_BASE_URL` — custom LLM provider base URL
- Standard API keys: `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `GEMINI_API_KEY`

### MCP client config example

```json
{
  "mcpServers": {
    "mammoth": {
      "command": "mammoth-mcp",
      "env": {
        "ANTHROPIC_API_KEY": "..."
      }
    }
  }
}
```

## Error Handling

**Pipeline errors:**
- DOT parse failures → `run_pipeline` returns error immediately (synchronous validation before spawning goroutine)
- Engine validation errors → same, caught before execution starts
- Node execution failures → reflected in `get_run_status` as `"failed"` with `failure_reason`
- Context cancellation → marks run as `"failed"` with reason `"cancelled"`

**MCP tool errors:**
- Unknown `run_id` → `"run not found"`
- `answer_question` on non-paused run → `"run is not waiting for input"`
- `resume_pipeline` with no checkpoint → `"no checkpoint available for this run"`
- `run_pipeline` with invalid DOT → error with parse/validation details

## Testing

**Unit tests:** Real `RunRegistry` with temp directories (no mocks). Tool handlers tested by creating a real registry, feeding it state, verifying responses. Human gate flow tested end-to-end within a single test.

**Integration tests:** Full MCP server in-process, tools called through the SDK's client interface. Run a simple pipeline, poll status until completion, verify result. Checkpoint/resume cycle: run, kill mid-execution, resume, verify continuation.

**End-to-end tests:** Spawn `mammoth-mcp` as a subprocess, communicate over stdio using MCP client SDK. Run a real pipeline against a real LLM backend (build-tagged, requires API keys).
