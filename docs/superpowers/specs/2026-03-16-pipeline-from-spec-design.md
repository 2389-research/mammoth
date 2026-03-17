# Pipeline-Based Spec-to-DOT Generation

## Goal

Replace the deterministic spec-to-DOT export with an LLM-driven pipeline that generates higher-quality, pattern-rich pipeline DOT files from spec documents.

## Architecture

Mammoth embeds `pipeline_from_spec.dot` (a meta-pipeline) in the binary. When the user triggers generation from the spec UI, mammoth exports the spec to markdown, then runs the meta-pipeline through tracker. The result is a validated, quality-scored pipeline DOT file stored directly in the project.

## Trigger

`POST /spec/generate-pipeline` in the spec web UI. The Diagram tab shows the last generated pipeline or a placeholder prompting the user to generate.

## Data Flow

1. User clicks "Generate Pipeline" in spec UI
2. Handler exports `SpecState` → markdown via `export.ExportMarkdown(specState)` (returns `string`, no error)
3. Creates a working directory at `workspace.ArtifactDir(projectID, runID)` and writes `spec.md` there
4. Launches the embedded `pipeline_from_spec.dot` through tracker via `startGenerationBuild` (new function, not `startBuildExecution` — see below)
5. SSE events stream progress to the browser (analyze → generate → validate → score → refine)
6. On successful completion, reads `pipeline.dot` from working directory and stores in `p.DOT`
7. Diagram tab renders `p.DOT`

## Components

### Embedded meta-pipeline

`pipeline_from_spec.dot` is copied into the mammoth repo at `web/pipeline_from_spec.dot` and embedded via `//go:embed` in `web/generate.go`. The `find_spec` node is simplified to just check for `spec.md` (mammoth guarantees it exists).

Fixes required to the DOT file before embedding:
- **`score_quality` stdout routing bug**: the script outputs `score=6/7 impl=5 quality_high` as a single string, but edge conditions use exact match (`context.tool_stdout=quality_high`). Fix: send diagnostic info to stderr, only the quality token to stdout.
- **`find_spec` simplification**: replace the multi-file search with a simple `test -f spec.md` check.
- **`run_validate` / `fix_validation` use `tracker validate`**: replace with `mammoth -validate` since the tracker CLI may not be in PATH but mammoth will be.
- **`run_validate` dead path**: remove the `no_dot_file` output branch from the tool_command since `generate_dot` always produces the file. Simplify to output only `valid` or `invalid`.

### Generation handler (`web/generate.go`)

New file with:

- `startGenerationBuild(ctx, projectID, specMarkdown string)` — a dedicated build launcher for pipeline generation. Unlike `startBuildExecution` which reads `p.DOT`, this function accepts a DOT string parameter (the embedded meta-pipeline). Otherwise follows the same pattern: creates `BuildRun`, starts goroutine, emits SSE events.
- `POST /spec/generate-pipeline` handler — exports SpecState to markdown, calls `startGenerationBuild`, returns build ID.
- Completion callback: reads `pipeline.dot` from the working directory, stores it in `p.DOT` via the project store. Only overwrites on success.

### Concurrency guard

Only one generation build runs per project at a time. The handler checks `s.builds[projectID]` and returns HTTP 409 Conflict if a build (generation or regular) is already active.

### Phase transitions

Generation does not change the project phase. The project stays in `PhaseSpec` during generation. When generation completes and `p.DOT` is populated, the project can transition to `PhaseEdit` (existing transition logic already handles "spec has DOT → can edit"). The "Generate Pipeline" button is available in `PhaseSpec`.

### Spec markdown exporter

Already exists at `spec/export/markdown.go`. Function signature: `export.ExportMarkdown(state *core.SpecState) string`. No changes needed.

### Diagram tab update

The Diagram tab reads from `p.DOT`. Shows a placeholder with a "Generate Pipeline" button when no pipeline exists yet. After generation, renders the DOT with the existing graph visualization.

## Error Handling

- Pipeline failure (no LLM key, budget exhausted): build shows "failed" with error, same as any build
- Spec too thin: `analyze_spec` node fails at goal gate, visible in build log
- Previous `p.DOT` preserved on failure — only overwritten on success
- Working directory cleaned up after build completes (success or failure)
- Double-click / concurrent generation: returns HTTP 409

## Code Deleted

| File | Lines | Purpose |
|------|-------|---------|
| `spec/export/dot.go` | ~546 | Deterministic DOT export (`ExportDOT`, `ExportGraph`) |
| `spec/core/export/dot.go` | ~433 | Legacy template-based export |
| DotGenerator role in `spec/agents/roles.go` | partial | Advisory agent that narrated graphs |
| DotGenerator prompts in `spec/agents/prompts.go` | partial | System prompt for above |
| `spec/web/handlers_export.go` — `Diagram()`, `ExportDOT()` | partial | Handlers calling `ExportDOT()` |
| `web/transitions.go` — `TransitionSpecToEditor` | partial | Calls `exportAndValidate` → `export.ExportDOT` |
| `web/transitions.go` — `TransitionSpecToBuild` | partial | Also calls `exportAndValidate` → `export.ExportDOT` |
| `web/transitions.go` — `exportAndValidate` helper | partial | The actual function calling `export.ExportDOT`; remove entirely |
| `web/spec_adapter.go` — calls to `TransitionSpecToEditor` | partial | Adapter call sites that trigger `ExportDOT` indirectly |
| `web/transitions_test.go` — `TransitionSpecToEditor`, `TransitionSpecToBuild` tests | partial | Tests for deleted transition functions |
| Related tests for `spec/export/dot.go`, `spec/core/export/dot.go` | varies | Tests for deleted export code |

## The Meta-Pipeline

The embedded `pipeline_from_spec.dot` implements this flow:

```
start → find_spec → analyze_spec → check_analysis → generate_dot
  → run_validate → [fix loop with budget] → score_quality
  → [refine loop with budget] → done
```

Key features:
- **Structured analysis**: extracts components, dependencies, tech stack, topology recommendation
- **Rich prompts**: each generated node gets TDD-oriented prompts with tech stack and success criteria
- **Mandatory patterns**: resume support, scope gating, per-phase verification, budget tracking, graduated escalation, build validation, code-reading review
- **Self-healing**: validate → fix → regenerate loop with budget limits (3 fix attempts, then full regeneration)
- **Quality scoring**: 7-dimension objective scoring with iterative refinement until quality_high
- **Topology awareness**: LINEAR/FAN_OUT/DAG chosen based on component analysis

## Testing

- Unit test: verify SpecState → markdown → spec.md round-trip
- Unit test: verify embedded DOT parses and validates
- Integration test: real LLM test double (not mock), run the generation handler, verify p.DOT is populated on completion
- Integration test: verify failed generation preserves previous p.DOT
- Integration test: verify concurrent generation returns 409

## Success Criteria

- "Generate Pipeline" button in spec UI triggers a tracker build
- Build progress visible via SSE events in the browser
- On success, `p.DOT` contains a validated pipeline with mandatory patterns
- Old deterministic export code fully removed
- No regression in spec builder functionality (cards, lanes, constraints still work)
