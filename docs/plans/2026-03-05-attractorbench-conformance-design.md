# AttractorBench Conformance CLI Design

Validate mammoth's attractor implementation against strongDM's AttractorBench Tier 3 conformance tests. Thin CLI binary that translates between mammoth's engine API and AttractorBench's expected JSON schemas.

Scope: Tier 3 only (Attractor Pipeline). Not Tiers 0-2.

## Architecture

```
cmd/mammoth-conformance/
  main.go         — CLI dispatch (parse|validate|run|list-handlers)
  parse.go        — dot.Parse() → JSON AST translation
  validate.go     — attractor.Validate() → JSON diagnostics translation
  run.go          — attractor.Engine.Run() → JSON result translation
  handlers.go     — registry.All() → JSON handler list
  types.go        — conformance-specific JSON output structs
```

No new packages. All translation logic stays in the binary. Core mammoth types are consumed read-only. No conformance-specific code leaks into `dot/`, `attractor/`, or `agent/`.

## CLI Commands

Four subcommands matching AttractorBench's conformance interface:

### `parse <dotfile>`

Parses DOT source into JSON AST.

**Flow:** `os.ReadFile → dot.Parse() → translate Graph → JSON stdout`

**Output:**
```json
{
  "nodes": [
    {"id": "start", "shape": "Mdiamond", "label": "...", "attributes": {}}
  ],
  "edges": [
    {"from": "start", "to": "step_a", "label": "...", "condition": "...", "weight": 0}
  ],
  "attributes": {"goal": "...", "label": "..."}
}
```

**Exit code:** 0 on success, 1 on parse failure.

### `validate <dotfile>`

Lints DOT source for structural issues.

**Flow:** `dot.Parse() → DefaultTransforms() → Validate() → translate Diagnostics → JSON stdout`

**Output:**
```json
{
  "diagnostics": [
    {"severity": "error", "message": "Missing start node"},
    {"severity": "warning", "message": "Orphan node 'orphan' is unreachable"}
  ]
}
```

**Exit code:** 0 if no errors (warnings OK), 1 if any error-severity diagnostics.

### `run <dotfile>`

Executes pipeline against LLM backend (real or mock).

**Flow:** `dot.Parse() → build EngineConfig → engine.RunGraph() → translate RunResult → JSON stdout`

**Engine wiring:**
- Backend: `attractor.AgentBackend{}` — respects `OPENAI_BASE_URL`, `ANTHROPIC_BASE_URL` env vars naturally
- Handlers: `attractor.DefaultHandlerRegistry()` — all 10 handler types
- Transforms: `attractor.DefaultTransforms()` — variable expansion, stylesheet, subpipeline
- Human gates: `attractor.NewAutoApproveInterviewer()` — no blocking in conformance mode
- Artifacts: temp directory, discarded after execution
- Checkpoints: disabled (one-shot conformance runs)
- Timeout: 60-second context deadline

**Output:**
```json
{
  "status": "success",
  "context": {
    "executed_nodes": ["start", "step_a"],
    "final_status": "success"
  },
  "nodes": [
    {"id": "start", "status": "success", "output": "...", "retry_count": 0}
  ]
}
```

**Exit code:** 0 on success/completion, 1 on failure.

**Mock LLM handling:** None. AttractorBench sets `OPENAI_BASE_URL=http://localhost:9999/v1` and `OPENAI_API_KEY=test-key`. Mammoth's provider adapters pick these up with zero conformance-specific code.

### `list-handlers`

Returns registered handler type names.

**Flow:** `DefaultHandlerRegistry().All() → extract keys → JSON stdout`

**Output:**
```json
["start", "exit", "codergen", "conditional", "parallel", "parallel.fan_in", "wait.human", "tool", "stack.manager_loop", "verify"]
```

**Exit code:** always 0.

## JSON Translation Layer

All conformance-specific JSON structs live in `cmd/mammoth-conformance/types.go`. Mappings:

| Mammoth Type | Conformance JSON | Translation |
|-------------|-----------------|-------------|
| `dot.Node` | `ConformanceNode` | Flatten attributes map, extract id/shape/label |
| `dot.Edge` | `ConformanceEdge` | Map From/To, extract label/condition/weight |
| `dot.Graph.Attrs` | `attributes` object | Direct pass-through |
| `attractor.Diagnostic` | `ConformanceDiagnostic` | Severity enum → string, message as-is |
| `attractor.RunResult` | `ConformanceRunResult` | Status from FinalOutcome, nodes from NodeOutcomes |

## Error Handling

- Parse errors → exit 1, JSON `{"error": "parse error: ..."}` to stdout
- Validation errors → exit 1 if any error-severity, diagnostics JSON still emitted
- Engine execution failure → exit 1, partial result if available
- Unknown subcommand → exit 1, usage to stderr
- Missing dotfile argument → exit 1, usage to stderr
- File not found → exit 1, JSON error to stdout

## Configuration

All via environment variables (set by AttractorBench test harness):

- `OPENAI_API_KEY` — API key for OpenAI-compatible endpoints
- `OPENAI_BASE_URL` — Override for OpenAI API base (mock server)
- `ANTHROPIC_API_KEY` — API key for Anthropic endpoints
- `ANTHROPIC_BASE_URL` — Override for Anthropic API base
- `GEMINI_API_KEY` — API key for Gemini endpoints

No CLI flags needed. The conformance binary is meant to be invoked by the test harness with the environment pre-configured.

## Testing

**Unit tests** in `cmd/mammoth-conformance/`:
- Translation functions: feed mammoth types, verify JSON schema compliance
- Each command's output format tested independently

**Integration tests:**
- Parse/validate against real DOT files from `examples/` directory
- No mock LLM needed for parse/validate/list-handlers

**End-to-end:**
- Build binary, invoke as subprocess, verify JSON output
- Build-tagged tests requiring compiled binary

**The real validation:** Run AttractorBench Tier 3 suite against the built binary.
