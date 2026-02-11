# Quickstart

Get a mammoth pipeline running in under five minutes.

## Prerequisites

- **Go 1.22+** (`go version`)
- **At least one LLM API key** set in your environment:
  ```bash
  export ANTHROPIC_API_KEY="sk-ant-..."
  # and/or
  export OPENAI_API_KEY="sk-..."
  export GEMINI_API_KEY="..."
  ```
- **Graphviz** (optional) -- only needed if you want to visualize `.dot` files with `dot -Tpng`

## Install

From source:

```bash
go install github.com/2389-research/mammoth/cmd/mammoth@latest
```

Or clone and build locally:

```bash
git clone https://github.com/2389-research/mammoth.git
cd mammoth
go build -o mammoth ./cmd/mammoth/
```

Verify the binary works:

```bash
mammoth -version
```

## Run Your First Pipeline

Mammoth ships with example pipelines in `examples/`. Start with the simplest one:

```bash
mammoth -verbose examples/simple.dot
```

This runs a linear pipeline: **Start -> Run Tests -> Report -> Exit**.

### Understanding the Output

With `-verbose`, you see stage-by-stage progress on stderr:

```
[pipeline] started
[stage] start started
[stage] start completed
[stage] run_tests started
[stage] run_tests completed
[stage] report started
[stage] report completed
[stage] exit started
[stage] exit completed
[pipeline] completed
```

On stdout, the final result:

```
Pipeline completed successfully.
Completed nodes: [start run_tests report exit]
Final status: success
```

## Validate a Pipeline

Check a pipeline for errors without running it:

```bash
mammoth -validate examples/goal_gate.dot
```

Output on success:

```
Pipeline is valid.
```

On failure you get diagnostics with suggested fixes:

```
[ERROR] graph has no start node (shape=Mdiamond) -- fix: add a node with shape=Mdiamond
Validation failed.
```

Exit code 0 means valid, 1 means errors -- useful for CI gates.

## Supply Human Input

Some pipelines include human gate nodes (`shape=hexagon`) that pause and wait for your decision.

### CLI Mode (stdin)

Run the human gate example directly:

```bash
mammoth examples/human_gate.dot
```

When the pipeline reaches the gate, it prompts you:

```
[?] Review Changes
  - [A] Approve
  - [F] Fix
Select:
```

Type your choice (e.g. `A`) and press Enter. The pipeline continues along the selected edge.

### HTTP Server Mode (curl)

Start the server:

```bash
mammoth -server -port 2389
```

Submit a pipeline:

```bash
curl -X POST http://localhost:2389/pipelines -d @examples/human_gate.dot
# Returns: {"id": "<pipeline-id>", "status": "running"}
```

Poll for pending questions:

```bash
curl http://localhost:2389/pipelines/<pipeline-id>/questions
```

Answer a question:

```bash
curl -X POST http://localhost:2389/pipelines/<pipeline-id>/questions/<question-id>/answer \
  -H "Content-Type: application/json" \
  -d '{"answer": "[A] Approve"}'
```

Check pipeline status at any time:

```bash
curl http://localhost:2389/pipelines/<pipeline-id>
```

## Start the HTTP Server

The server exposes a REST API for submitting and managing pipelines:

```bash
mammoth -server -port 2389 -verbose -retry standard
```

Key endpoints:

| Method | Path | Purpose |
|--------|------|---------|
| `POST /pipelines` | Submit a pipeline |
| `GET /pipelines/{id}` | Check status |
| `GET /pipelines/{id}/events` | SSE event stream |
| `POST /pipelines/{id}/cancel` | Cancel a run |
| `GET /pipelines/{id}/questions` | List pending human questions |
| `POST /pipelines/{id}/questions/{qid}/answer` | Answer a question |

Full server reference: [CLI Usage](cli-usage.md#server-mode).

## Example Pipelines

| File | What It Does |
|------|-------------|
| `examples/simple.dot` | Minimal linear pipeline |
| `examples/branching.dot` | Conditional branching with retry loop |
| `examples/human_gate.dot` | Human-in-the-loop review cycle |
| `examples/goal_gate.dot` | Goal gate enforcement |
| `examples/full_pipeline.dot` | Stylesheets, goal gates, branching, human review |
| `examples/build_pong.dot` | Builds a Pong TUI game end-to-end |

## Next Steps

- [Walkthrough](walkthrough.md) -- Write pipelines from scratch, learn stylesheets and goal gates
- [CLI Usage](cli-usage.md) -- All flags, server endpoints, exit codes
- [DSL Reference](dsl-reference.md) -- Complete DOT attribute and syntax reference
- [Handlers Reference](handlers.md) -- Documentation for each handler type
- [Backend Configuration](backend-config.md) -- Provider setup, model catalog, retry policies
