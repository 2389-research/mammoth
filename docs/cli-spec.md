# Makeatron CLI Specification

This document describes the command-line interface for `makeatron`, the pipeline runner for Attractor DOT-based workflows. It covers all commands, flags, output formats, exit codes, and error behaviors as implemented in `cmd/makeatron/main.go`.

---

## Table of Contents

1. [Synopsis](#1-synopsis)
2. [Modes of Operation](#2-modes-of-operation)
3. [Flags Reference](#3-flags-reference)
4. [Output Specification](#4-output-specification)
5. [Exit Codes](#5-exit-codes)
6. [Error Message Format](#6-error-message-format)
7. [Signal Handling](#7-signal-handling)
8. [Environment Variables](#8-environment-variables)
9. [Retry Policies](#9-retry-policies)
10. [HTTP Server API](#10-http-server-api)
11. [Verbose Event Types](#11-verbose-event-types)
12. [Examples](#12-examples)
13. [Future Enhancements](#13-future-enhancements)

---

## 1. Synopsis

```
makeatron [options] <pipeline.dot>
```

`makeatron` reads an Attractor pipeline definition written in DOT syntax and either executes it, validates it, or serves it over HTTP. The pipeline file is a positional argument (the first non-flag argument).

---

## 2. Modes of Operation

Makeatron has four mutually exclusive modes, selected by flags:

| Mode       | Trigger                            | Description                                      |
|------------|------------------------------------|--------------------------------------------------|
| **Run**    | `makeatron <pipeline.dot>`         | Parse, validate, and execute the pipeline         |
| **Validate** | `makeatron --validate <pipeline.dot>` | Parse and validate without executing          |
| **Server** | `makeatron --server`               | Start an HTTP server for pipeline management      |
| **Version** | `makeatron --version`             | Print version string and exit                     |

### 2.1 Run Mode (default)

Reads the DOT file, constructs an `Engine` with the configured retry policy, checkpoint directory, and artifact directory, then executes the pipeline. On success, prints completion summary to stdout. On failure, prints error to stderr and exits with code 1.

Pipeline execution respects `context.Context` cancellation via signal handling (see [Signal Handling](#7-signal-handling)).

### 2.2 Validate Mode

Reads the DOT file, parses it, applies default transforms, and runs all built-in validation/lint rules. Diagnostics are printed to stderr with severity, message, optional node ID, and optional fix suggestion. If any diagnostic has `ERROR` severity, validation fails.

### 2.3 Server Mode

Starts an HTTP server on the configured port (default `2389`). The server exposes a REST API for submitting, querying, streaming events from, and cancelling pipelines. Does not require a positional pipeline file argument. See [HTTP Server API](#10-http-server-api) for endpoint details.

### 2.4 Version Mode

Prints `makeatron <version>` to stdout and exits with code 0. The version defaults to `"dev"` at compile time and can be overridden via `-ldflags` at build time.

---

## 3. Flags Reference

All flags use the `--flag` (double-dash) convention. Single-character shorthand flags are not supported.

| Flag               | Type     | Default  | Description                                       |
|--------------------|----------|----------|---------------------------------------------------|
| `--server`         | `bool`   | `false`  | Start HTTP server mode                            |
| `--port`           | `int`    | `2389`   | Server listen port (only meaningful with `--server`) |
| `--validate`       | `bool`   | `false`  | Validate pipeline without executing               |
| `--checkpoint-dir` | `string` | `""`     | Directory for checkpoint files (empty = no checkpoints) |
| `--artifact-dir`   | `string` | `""`     | Directory for artifact storage (empty = temp dir)  |
| `--retry`          | `string` | `"none"` | Default retry policy: `none`, `standard`, `aggressive`, `linear`, `patient` |
| `--verbose`        | `bool`   | `false`  | Print engine lifecycle events to stderr            |
| `--version`        | `bool`   | `false`  | Print version and exit                            |

### 3.1 Positional Arguments

The first positional argument after flags is interpreted as the pipeline file path. In run and validate modes, the pipeline file is required. In server mode, it is ignored.

### 3.2 Unknown Flags

Unknown flags cause an error message to be printed to stderr and the process exits with code 2 (Go's `flag.ContinueOnError` behavior).

---

## 4. Output Specification

### 4.1 Stdout

Stdout is reserved for primary output and results.

**Run mode (success):**
```
Pipeline completed successfully.
Completed nodes: [start step1 step2 finish]
Final status: success
```

The completed nodes are printed as a Go slice string representation. The final status line is only printed if the pipeline produces a `FinalOutcome`.

**Validate mode (success):**
```
Pipeline is valid.
```

**Version mode:**
```
makeatron dev
```

### 4.2 Stderr

Stderr is used for errors, diagnostics, verbose events, and server status messages.

**Error messages:**
```
error: <description>
```

**Validation diagnostics** (validate mode):
```
[ERROR] <message> (node: <node_id>) -- fix: <suggestion>
[WARNING] <message> (node: <node_id>) -- fix: <suggestion>
[INFO] <message>
```

The `(node: ...)` suffix is omitted when the diagnostic is not node-specific. The `-- fix: ...` suffix is omitted when no fix suggestion is available.

**Verbose events** (when `--verbose` is set):
```
[pipeline] started
[stage] node_id started
[stage] node_id completed
[stage] node_id failed
[stage] node_id retrying
[pipeline] completed
[pipeline] failed
[checkpoint] saved at node_id
```

**Server mode startup:**
```
listening on :2389
```

**Signal interruption:**
```

Interrupted, shutting down...
```

(Note the leading blank line from the `\n` prefix.)

### 4.3 JSON Output

The CLI itself does not currently have a `--json` flag or structured JSON output mode for pipeline execution results. JSON output is available exclusively through the HTTP server API endpoints (see [HTTP Server API](#10-http-server-api)).

---

## 5. Exit Codes

| Code | Meaning                                                        |
|------|----------------------------------------------------------------|
| `0`  | Success (pipeline executed, validation passed, version printed) |
| `1`  | Failure (pipeline error, validation failed, missing file, no pipeline file specified) |
| `2`  | Usage error (unknown flag, malformed flag value)               |

---

## 6. Error Message Format

All error messages follow the pattern:

```
error: <description>
```

Written to stderr. Examples of error descriptions:

- `pipeline file required (use makeatron <pipeline.dot>)` -- no pipeline file provided in non-server mode
- `open /path/to/file.dot: no such file or directory` -- file not found
- `parse error: <details>` -- DOT syntax error
- `validation failed: pipeline validation failed with N error(s)` -- validation errors during run mode
- `node "X" execution error: <details>` -- handler failure
- `stage "X" failed with no outgoing fail edge` -- dead-end failure

---

## 7. Signal Handling

In both run mode and server mode, `makeatron` listens for `SIGINT` (Ctrl-C) and `SIGTERM`. On receipt:

1. Prints `\nInterrupted, shutting down...` to stderr
2. Cancels the `context.Context` passed to the engine or HTTP server
3. In run mode: the engine stops executing at the next cancellation check point
4. In server mode: the HTTP server performs a graceful close

A second signal is not handled specially; the first signal initiates an orderly shutdown.

---

## 8. Environment Variables

The CLI binary itself does not read any environment variables. However, when running pipelines that use `codergen` nodes with an LLM backend, the following environment variables are read by the backend adapter layer (in `attractor/backend_agent.go`):

| Variable            | Purpose                              |
|---------------------|--------------------------------------|
| `ANTHROPIC_API_KEY` | API key for Anthropic Claude models  |
| `OPENAI_API_KEY`    | API key for OpenAI models            |
| `GEMINI_API_KEY`    | API key for Google Gemini models     |

These are only relevant when a pipeline includes `codergen` nodes backed by LLM calls.

---

## 9. Retry Policies

The `--retry` flag selects a preset retry policy applied as the default for all pipeline nodes. The value is case-insensitive. Unrecognized values silently fall back to `none`.

| Policy         | Max Attempts | Initial Delay | Backoff Factor | Max Delay | Jitter |
|----------------|-------------|---------------|----------------|-----------|--------|
| `none`         | 1           | 200ms         | 2.0            | 60s       | No     |
| `standard`     | 5           | 200ms         | 2.0            | 60s       | Yes    |
| `aggressive`   | 5           | 500ms         | 2.0            | 60s       | Yes    |
| `linear`       | 3           | 500ms         | 1.0            | 60s       | No     |
| `patient`      | 3           | 2000ms        | 3.0            | 60s       | Yes    |

Individual nodes can override the default via `max_retries` attribute in the DOT file. Graph-level `default_max_retry` attribute also takes precedence over the CLI default. The resolution order is: node attribute > graph attribute > CLI `--retry` flag.

---

## 10. HTTP Server API

When started with `--server`, makeatron exposes the following REST endpoints. All responses use `Content-Type: application/json`.

### 10.1 Submit Pipeline

```
POST /pipelines
```

**Request body:** DOT source as plain text (`text/plain`) or JSON (`application/json` with `{"source": "..."}`)

**Response (202 Accepted):**
```json
{
  "id": "<hex-uuid>",
  "status": "running"
}
```

### 10.2 Get Pipeline Status

```
GET /pipelines/{id}
```

**Response (200 OK):**
```json
{
  "id": "<hex-uuid>",
  "status": "running|completed|failed|cancelled",
  "completed_nodes": ["start", "step1"],
  "error": "",
  "created_at": "2026-02-07T12:00:00Z"
}
```

**Response (404 Not Found):**
```json
{"error": "pipeline not found"}
```

### 10.3 Stream Events (SSE)

```
GET /pipelines/{id}/events
```

Server-Sent Events stream. Each event:
```
data: {"type":"stage.started","node_id":"step1","data":null}
```

Final event when pipeline completes:
```
data: {"status":"completed"}
```

### 10.4 Query Events

```
GET /pipelines/{id}/events/query?type=stage.started&node=step1&since=2026-01-01T00:00:00Z&until=2026-12-31T23:59:59Z&limit=10&offset=0
```

All query parameters are optional. Requires `EventQuery` backend to be configured (returns 503 otherwise).

**Response (200 OK):**
```json
{
  "events": [...],
  "total": 42
}
```

### 10.5 Tail Events

```
GET /pipelines/{id}/events/tail?n=10
```

Returns the last `n` events (default 10). Requires `EventQuery` backend.

**Response (200 OK):**
```json
{
  "events": [...]
}
```

### 10.6 Event Summary

```
GET /pipelines/{id}/events/summary
```

Requires `EventQuery` backend.

**Response (200 OK):**
```json
{
  "total_events": 15,
  "by_type": {"stage.started": 5, "stage.completed": 5, ...},
  "by_node": {"step1": 3, "step2": 2, ...},
  "first_event": "2026-02-07T12:00:00.000000000Z",
  "last_event": "2026-02-07T12:01:30.000000000Z"
}
```

### 10.7 Cancel Pipeline

```
POST /pipelines/{id}/cancel
```

**Response (200 OK):**
```json
{"status": "cancelled"}
```

### 10.8 Get Pending Questions (Human-in-the-Loop)

```
GET /pipelines/{id}/questions
```

Returns unanswered questions waiting for human input.

**Response (200 OK):**
```json
[
  {
    "id": "<question-uuid>",
    "question": "Should we proceed with deployment?",
    "options": ["yes", "no"],
    "answered": false
  }
]
```

### 10.9 Answer Question

```
POST /pipelines/{id}/questions/{qid}/answer
```

**Request body:**
```json
{"answer": "yes"}
```

**Response (200 OK):**
```json
{"status": "answered"}
```

### 10.10 Get Pipeline Context

```
GET /pipelines/{id}/context
```

Returns the current pipeline context snapshot (key-value state accumulated during execution).

**Response (200 OK):**
```json
{
  "key1": "value1",
  "key2": "value2"
}
```

Returns `{}` if context is not yet available or pipeline has not completed.

---

## 11. Verbose Event Types

When `--verbose` is enabled, the following engine event types are printed to stderr:

| Event Type              | Output Format                        |
|-------------------------|--------------------------------------|
| `pipeline.started`      | `[pipeline] started`                 |
| `pipeline.completed`    | `[pipeline] completed`               |
| `pipeline.failed`       | `[pipeline] failed`                  |
| `stage.started`         | `[stage] <node_id> started`          |
| `stage.completed`       | `[stage] <node_id> completed`        |
| `stage.failed`          | `[stage] <node_id> failed`           |
| `stage.retrying`        | `[stage] <node_id> retrying`         |
| `checkpoint.saved`      | `[checkpoint] saved at <node_id>`    |

---

## 12. Examples

### 12.1 Run a pipeline

```bash
makeatron pipeline.dot
```

Output (stdout):
```
Pipeline completed successfully.
Completed nodes: [start generate_code review finish]
Final status: success
```

### 12.2 Run with verbose output and aggressive retry

```bash
makeatron --verbose --retry aggressive pipeline.dot
```

Output (stderr, verbose events):
```
[pipeline] started
[stage] start started
[stage] start completed
[stage] generate_code started
[stage] generate_code retrying
[stage] generate_code completed
[stage] finish started
[stage] finish completed
[pipeline] completed
```

Output (stdout):
```
Pipeline completed successfully.
Completed nodes: [start generate_code finish]
Final status: success
```

### 12.3 Validate a pipeline

```bash
makeatron --validate pipeline.dot
```

Output (stdout, valid):
```
Pipeline is valid.
```

Output (stderr, invalid):
```
[ERROR] graph has no start node (shape=Mdiamond) -- fix: add a node with shape=Mdiamond
[ERROR] node "orphan" is not reachable from start node "start" (node: orphan) -- fix: add an edge path from start to "orphan"
Validation failed.
```

### 12.4 Run with checkpointing

```bash
makeatron --checkpoint-dir /tmp/checkpoints --artifact-dir /tmp/artifacts pipeline.dot
```

Checkpoint files are written to `/tmp/checkpoints` as `checkpoint_<node_id>_<timestamp>.json` after each node completes.

### 12.5 Start the HTTP server

```bash
makeatron --server --port 2389 --verbose
```

Output (stderr):
```
listening on :2389
```

### 12.6 Submit a pipeline via the server

```bash
curl -X POST http://localhost:2389/pipelines \
  -H "Content-Type: application/json" \
  -d '{"source": "digraph test { start [shape=Mdiamond]; finish [shape=Msquare]; start -> finish }"}'
```

Response:
```json
{"id":"a1b2c3d4-e5f6-7890-abcd-ef1234567890","status":"running"}
```

### 12.7 Check pipeline status

```bash
curl http://localhost:2389/pipelines/a1b2c3d4-e5f6-7890-abcd-ef1234567890
```

Response:
```json
{
  "id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "status": "completed",
  "completed_nodes": ["start", "finish"],
  "error": "",
  "created_at": "2026-02-07T12:00:00Z"
}
```

### 12.8 Print version

```bash
makeatron --version
```

Output (stdout):
```
makeatron dev
```

### 12.9 Stream pipeline events

```bash
curl -N http://localhost:2389/pipelines/a1b2c3d4-e5f6-7890-abcd-ef1234567890/events
```

Output (SSE stream):
```
data: {"type":"pipeline.started","node_id":"","data":null}

data: {"type":"stage.started","node_id":"start","data":null}

data: {"type":"stage.completed","node_id":"start","data":null}

data: {"type":"stage.started","node_id":"finish","data":null}

data: {"type":"stage.completed","node_id":"finish","data":null}

data: {"type":"pipeline.completed","node_id":"","data":null}

data: {"status":"completed"}
```

### 12.10 Missing pipeline file

```bash
makeatron
```

Output (stderr):
```
error: pipeline file required (use makeatron <pipeline.dot>)
```

Exit code: `1`

---

## 13. Future Enhancements

The following capabilities are not currently implemented but would be natural extensions based on user expectations and the existing architecture:

1. **`--json` flag for structured output** -- A `--json` flag on run and validate modes would emit results as JSON to stdout instead of plain text, enabling programmatic consumption without requiring the HTTP server.

2. **`--output` / `-o` flag** -- Allow specifying an output file for results instead of stdout.

3. **`--resume` flag** -- The engine supports `ResumeFromCheckpoint()` but there is no CLI flag to trigger it. A `--resume <checkpoint-file>` flag would enable crash recovery from the command line.

4. **`--dry-run` flag** -- Similar to validate but would also resolve handlers and report which would be invoked, without executing them.

5. **`--list-handlers` flag** -- Enumerate registered handler types and their descriptions.

6. **`--model` / `--backend` flag** -- Specify the preferred LLM backend for `codergen` nodes directly from the CLI.

7. **`--timeout` flag** -- Set a global timeout for pipeline execution (currently relies on signal handling).

8. **Short flags** -- Single-character aliases like `-v` for `--verbose`, `-p` for `--port`, etc.

9. **Subcommand style** -- Restructure as `makeatron run`, `makeatron validate`, `makeatron server` subcommands instead of flag-based mode selection.

10. **`--quiet` flag** -- Suppress all non-essential output, complement to `--verbose`.

11. **Progress reporting** -- Real-time progress bar or percentage for pipeline execution in TTY mode.

12. **`--config` flag** -- Load configuration from a YAML/TOML file instead of requiring all options as flags.
