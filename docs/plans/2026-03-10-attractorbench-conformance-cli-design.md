# AttractorBench Conformance CLI Design

## Summary

Build a `cmd/conformance/` binary that adapts mammoth's existing packages to the
CLI interface expected by [attractorbench](https://github.com/strongdm/attractorbench).
This lets us measure mammoth's spec conformance against the same benchmark used to
evaluate coding agents.

## Architecture

Single Go binary at `./bin/conformance` with subcommand dispatch (`os.Args[1]`).
Each subcommand is a thin adapter: read env/stdin/args, call mammoth APIs, marshal
JSON to stdout.

## Subcommands by Tier

### Tier 1: Unified LLM SDK

| Subcommand | Input | Mammoth API | Output |
|------------|-------|-------------|--------|
| `client-from-env` | env vars only | `llm.FromEnv()` | exit 0/1 |
| `list-models` | none | model catalog | JSON array of model objects |
| `complete` | JSON request on stdin | `client.Complete()` | JSON response |
| `stream` | JSON request on stdin | `client.Stream()` | NDJSON events |
| `tool-call` | JSON request on stdin | `client.Complete()` with tools | JSON response |
| `generate-object` | JSON request on stdin | `llm.GenerateObject()` | JSON object |

### Tier 2: Coding Agent Loop

| Subcommand | Input | Mammoth API | Output |
|------------|-------|-------------|--------|
| `session-create` | none | `agent.NewSession()` | JSON with id/status |
| `process-input` | JSON prompt on stdin | `agent.ProcessInput()` | JSON session result |
| `tool-dispatch` | JSON tool call on stdin | `exec_local` tool dispatch | JSON tool result |
| `events` | none | session event subscription | NDJSON events |
| `steering` | JSON message on stdin | `session.Steer()` | JSON ack or exit 0 |

### Tier 3: Attractor Pipeline

| Subcommand | Input | Mammoth API | Output |
|------------|-------|-------------|--------|
| `parse <file>` | DOT file path arg | `dot.Parse()` | JSON AST |
| `validate <file>` | DOT file path arg | `attractor.Validate()` | JSON diagnostics |
| `run <file>` | DOT file path arg | `engine.Run()` | JSON execution result |
| `list-handlers` | none | `DefaultHandlerRegistry()` | JSON array of types |

## JSON Translation Layer

The benchmark tests check specific field names. Key mappings:

**Request (stdin) -> mammoth types:**
- `provider` field -> mammoth provider routing
- `messages[].content` as string or array -> `llm.Message` with `ContentParts`
- `tools` array -> `llm.ToolDefinition` slice
- `response_schema` -> JSON schema for `GenerateObject`

**Response (stdout) <- mammoth types:**
- `llm.Response` -> `{id, output, usage: {input_tokens, output_tokens, total_tokens}}`
- Stream events -> NDJSON with `type` and `delta` fields
- `dot.Graph` -> `{nodes: [{id, shape, attributes}], edges: [{from, to, attributes}]}`
- `attractor.RunResult` -> `{status, context, trace}`
- Diagnostics -> `{diagnostics: [{severity, message, node}]}`

## Environment Variables

The mock server at `http://localhost:9999` sets:
- `OPENAI_API_KEY=test-key`, `OPENAI_BASE_URL=http://localhost:9999/v1`
- `ANTHROPIC_API_KEY=test-key`, `ANTHROPIC_BASE_URL=http://localhost:9999`
- `GEMINI_API_KEY=test-key`, `GEMINI_BASE_URL=http://localhost:9999`

The conformance binary must respect `*_BASE_URL` env vars to route to the mock.

## File Structure

```
cmd/conformance/
  main.go           # Subcommand dispatch
  tier1.go          # LLM SDK subcommands
  tier2.go          # Agent loop subcommands
  tier3.go          # Pipeline subcommands
  types.go          # JSON request/response types for the conformance interface
```

## Build Integration

Add to `Makefile`:
```makefile
conformance:
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/conformance ./cmd/conformance
```

## Testing Strategy

- Unit tests for each JSON translation function (request parsing, response marshaling)
- Integration test: start mock server, run each subcommand, verify JSON output
- Run the actual attractorbench conformance suite as the end-to-end test

## Risk: JSON Schema Mismatches

The benchmark tests check specific field names and structures. We must be precise about:
- Supporting both `from`/`to` and `source`/`target` for edge serialization
- Stream event format with proper `type` discriminators
- Response `id` field always present and non-empty
- Usage fields using the expected names (`input_tokens` not `prompt_tokens`)
