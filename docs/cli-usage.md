# CLI Usage Guide

Mammoth is a command-line tool for running DOT-based LLM agent pipelines. It supports three modes: pipeline execution, validation, and HTTP server mode.

## Installation

Build from source:

```bash
go build -o mammoth ./cmd/mammoth/
```

## Synopsis

```
mammoth [options] <pipeline.dot>
```

## Commands by Mode

### Run a Pipeline

```bash
mammoth pipeline.dot
```

This is the default mode. Mammoth parses the DOT file, validates the graph, and executes the pipeline from the start node to an exit node. The pipeline runs synchronously -- the process blocks until completion or failure.

### Validate a Pipeline

```bash
mammoth -validate pipeline.dot
```

Parses and validates the DOT file without executing it. Reports errors and warnings to stderr and exits with code 0 (valid) or 1 (errors found). Useful for CI/CD and pre-commit checks.

### Start HTTP Server

```bash
mammoth -server
```

Starts an HTTP server for managing pipeline execution via REST API. Pipelines are submitted and monitored via HTTP endpoints. See [Server Mode](#server-mode) below.

### Print Version

```bash
mammoth -version
```

Prints the version string and exits.

## Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-server` | bool | `false` | Start HTTP server mode instead of running a pipeline. |
| `-port` | int | `2389` | Server listen port. Only used with `-server`. |
| `-validate` | bool | `false` | Validate pipeline without executing. |
| `-checkpoint-dir` | string | `""` | Directory for saving checkpoint files. Empty disables checkpointing. |
| `-artifact-dir` | string | `""` | Directory for storing artifacts (large outputs). Empty uses temp directory. |
| `-retry` | string | `none` | Default retry policy preset. See [Retry Policies](#retry-policies). |
| `-verbose` | bool | `false` | Enable verbose output. Prints engine lifecycle events to stderr. |
| `-version` | bool | `false` | Print version and exit. |

## Examples

### Basic Pipeline Execution

```bash
# Run a simple pipeline
mammoth examples/simple.dot

# Run with verbose output to see stage transitions
mammoth -verbose examples/branching.dot

# Run with checkpointing for crash recovery
mammoth -checkpoint-dir ./checkpoints examples/full_pipeline.dot

# Run with artifact storage
mammoth -artifact-dir ./artifacts examples/build_pong.dot

# Run with retry policy
mammoth -retry standard examples/plan_implement_review.dot
```

### Validation

```bash
# Validate a pipeline
mammoth -validate examples/simple.dot
# Output: "Pipeline is valid."

# Validate with verbose output
mammoth -verbose -validate examples/branching.dot

# Use in CI (exit code 0 = valid, 1 = errors)
mammoth -validate my_pipeline.dot && echo "Valid" || echo "Invalid"
```

### Server Mode

```bash
# Start server on default port (2389)
mammoth -server

# Start server on custom port with verbose logging
mammoth -server -port 8080 -verbose

# Start server with retry policy and checkpointing
mammoth -server -retry standard -checkpoint-dir ./checkpoints
```

## Output

### Pipeline Execution Output

On success:

```
Pipeline completed successfully.
Completed nodes: [start plan implement validate done]
Final status: success
```

On failure:

```
error: node "implement" execution error: execution error after 3 attempt(s): ...
```

### Verbose Output

With `-verbose`, engine lifecycle events are printed to stderr:

```
[pipeline] started
[stage] start started
[stage] start completed
[stage] plan started
[stage] plan completed
[stage] implement started
[stage] implement completed
[checkpoint] saved at implement
[stage] validate started
[stage] validate completed
[pipeline] completed
```

### Validation Output

Successful validation:

```
Pipeline is valid.
```

Failed validation:

```
[ERROR] graph has no start node (shape=Mdiamond) -- fix: add a node with shape=Mdiamond
[WARNING] codergen node "task" has no prompt or label attribute (node: task) -- fix: add a prompt or label attribute
Validation failed.
```

## Retry Policies

The `-retry` flag sets the default retry policy for all nodes. Individual nodes can override this with the `max_retries` attribute.

| Policy | Max Attempts | Initial Delay | Backoff Factor | Max Delay | Jitter |
|--------|-------------|---------------|----------------|-----------|--------|
| `none` | 1 (no retries) | 200ms | 2.0x | 60s | No |
| `standard` | 5 | 200ms | 2.0x | 60s | Yes |
| `aggressive` | 5 | 500ms | 2.0x | 60s | Yes |
| `linear` | 3 | 500ms | 1.0x (constant) | 60s | No |
| `patient` | 3 | 2000ms | 3.0x | 60s | Yes |

Per-node overrides via the `max_retries` attribute adjust only the attempt count, keeping the backoff timing from the active policy.

## Signal Handling

Mammoth handles `SIGINT` (Ctrl+C) and `SIGTERM` gracefully. When interrupted:

1. The cancellation propagates to all running handlers via Go's `context.Context`.
2. Tool handlers kill their entire process group on cancellation.
3. The pipeline reports the cancellation and exits with code 1.

In server mode, the HTTP server performs a graceful shutdown on signal.

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success (pipeline completed or validation passed) |
| 1 | Failure (pipeline error, validation failed, or runtime error) |
| 2 | Usage error (invalid flags or arguments) |

## Server Mode

When started with `-server`, mammoth exposes a REST API for managing pipelines.

### Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `POST /pipelines` | Submit a pipeline for execution | Returns pipeline ID |
| `GET /pipelines/{id}` | Get pipeline status | Returns status, completed nodes, errors |
| `GET /pipelines/{id}/events` | SSE event stream | Real-time engine events via Server-Sent Events |
| `GET /pipelines/{id}/events/query` | Query events | Filtered event retrieval with pagination |
| `GET /pipelines/{id}/events/tail` | Tail events | Last N events |
| `GET /pipelines/{id}/events/summary` | Event summary | Aggregate statistics |
| `POST /pipelines/{id}/cancel` | Cancel a running pipeline | Sends cancellation signal |
| `GET /pipelines/{id}/questions` | List pending human questions | For human-in-the-loop |
| `POST /pipelines/{id}/questions/{qid}/answer` | Answer a question | Submit human response |
| `GET /pipelines/{id}/context` | Get pipeline context | Current key-value state |

### Submit a Pipeline

```bash
# Submit DOT source as plain text
curl -X POST http://localhost:2389/pipelines \
  -d @examples/simple.dot

# Submit as JSON
curl -X POST http://localhost:2389/pipelines \
  -H "Content-Type: application/json" \
  -d '{"source": "digraph { ... }"}'
```

Response:

```json
{"id": "a1b2c3d4-...", "status": "running"}
```

### Check Pipeline Status

```bash
curl http://localhost:2389/pipelines/a1b2c3d4-...
```

Response:

```json
{
  "id": "a1b2c3d4-...",
  "status": "completed",
  "completed_nodes": ["start", "run_tests", "report", "exit"],
  "created_at": "2026-02-07T12:00:00Z"
}
```

### Stream Events (SSE)

```bash
curl -N http://localhost:2389/pipelines/a1b2c3d4-.../events
```

Each event is delivered as a Server-Sent Event:

```
data: {"type":"stage.started","node_id":"plan","data":null}

data: {"type":"stage.completed","node_id":"plan","data":null}

data: {"status":"completed"}
```

### Human-in-the-Loop via HTTP

When a pipeline reaches a human gate node, a question appears at the questions endpoint:

```bash
# List pending questions
curl http://localhost:2389/pipelines/a1b2c3d4-.../questions
```

```json
[
  {
    "id": "q-abc123",
    "question": "Review the changes",
    "options": ["[A] Approve", "[R] Revise"],
    "answered": false
  }
]
```

```bash
# Submit an answer
curl -X POST http://localhost:2389/pipelines/a1b2c3d4-.../questions/q-abc123/answer \
  -H "Content-Type: application/json" \
  -d '{"answer": "[A] Approve"}'
```

### Query Events

```bash
# Filter by event type
curl "http://localhost:2389/pipelines/{id}/events/query?type=stage.completed"

# Filter by node and time range
curl "http://localhost:2389/pipelines/{id}/events/query?node=implement&since=2026-02-07T12:00:00Z"

# Paginate
curl "http://localhost:2389/pipelines/{id}/events/query?limit=10&offset=20"
```

### Tail Events

```bash
# Get last 5 events
curl "http://localhost:2389/pipelines/{id}/events/tail?n=5"
```

## Environment Variables

Mammoth reads LLM API keys from the environment. See [Backend Configuration](backend-config.md) for details.

```bash
export ANTHROPIC_API_KEY="sk-ant-..."
export OPENAI_API_KEY="sk-..."
export GEMINI_API_KEY="..."

mammoth examples/full_pipeline.dot
```

## Checkpointing

When `-checkpoint-dir` is set, the engine saves a checkpoint file after each node completes. Checkpoint files are JSON and contain:

- Current node ID
- All completed node IDs
- Retry counters
- Pipeline context snapshot
- Log entries

Checkpoint filenames follow the pattern: `checkpoint_{nodeID}_{timestamp}.json`.

Checkpoints enable crash recovery by resuming from the last completed node rather than restarting the entire pipeline.

See also: [DSL Reference](dsl-reference.md) for pipeline syntax, [Handlers Reference](handlers.md) for handler details, [Backend Configuration](backend-config.md) for LLM setup, [Walkthrough](walkthrough.md) for end-to-end examples.
