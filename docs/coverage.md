# Coverage Analysis and Targets

Current coverage baseline captured from `go test ./... -coverprofile`.
This document defines per-package thresholds, critical paths that must stay covered,
and a maintenance strategy.

## Current Coverage by Package

| Package | Coverage | Threshold |
|---------|----------|-----------|
| `llm/sse` | 97.2% | 95% |
| `attractor` | 87.3% | 85% |
| `agent` | 82.3% | 80% |
| `llm` | 82.1% | 80% |
| `cmd/makeatron` | 68.6% | 65% |
| **Total** | **84.6%** | **80%** |

Thresholds are set 2-3 points below current coverage to prevent regressions while
leaving room for natural fluctuation as code evolves.

## Critical Paths (Must Maintain High Coverage)

These subsystems are the load-bearing walls of the project. Regressions here
are pipeline-breaking bugs waiting to happen.

### Engine Traversal (`attractor/engine.go`)
- `RunGraph` - 96.3%
- `executeGraph` - 86.7%
- `executeWithRetry` - 80.0%
- `ResumeFromCheckpoint` - 84.0%

### Edge Selection (`attractor/edge_selection.go`)
- `SelectEdge` - 94.3%
- `NormalizeLabel` - 100%
- `bestByWeightThenLexical` - 100%
- `edgeWeight` - 83.3%

### Fidelity Transforms (`attractor/fidelity_transform.go`)
- `ApplyFidelity` - 87.5%
- `GeneratePreamble` - 90.9%
- `applyTruncate` - 100%
- `applyCompact` - 100%
- All `applySummary*` functions - 100%

### Handler Dispatch (`attractor/handlers.go` and handler files)
- `Resolve` - 75.0%
- `DefaultHandlerRegistry` - 100%
- `CodergenHandler.Execute` - 95.3%
- `ManagerHandler.Execute` - 100%
- `ManagerHandler.executeSupervisionLoop` - 95.7%
- `ParallelHandler.Execute` - 91.3%
- `HumanHandler.Execute` - 92.6%
- `ToolHandler.Execute` - 100%
- `StartHandler.Execute` - 100%

### Parallel Execution (`attractor/parallel.go`)
- `ExecuteParallelBranches` - 86.7%
- `executeBranchChain` - 72.2%
- `MergeContexts` - 90.9%
- `mergeWaitAll` - 90.9%
- `mergeWaitAny` - 100%
- `mergeKOfN` - 100%
- `mergeQuorum` - 100%

### Checkpoint/Resume (`attractor/checkpoint.go`, `attractor/runstate_fs.go`)
- `Save` - 75.0%
- `LoadCheckpoint` - 85.7%
- `FSRunStateStore.Create` - 73.3%
- `FSRunStateStore.Update` - 80.0%
- `FSRunStateStore.AddEvent` - 81.2%

### Agent Core Loop (`agent/loop.go`)
- `ProcessInput` - 87.7%
- `executeToolCalls` - 100%
- `executeSingleTool` - 81.8%
- `drainSteering` - 100%

### LLM Provider Adapters
- `AnthropicAdapter.Complete` - 81.8%
- `AnthropicAdapter.Stream` - 84.6%
- `OpenAIAdapter.Complete` - 87.5%
- `OpenAIAdapter.Stream` - 90.9%
- `GeminiAdapter.Complete` - 83.3%
- `GeminiAdapter.Stream` - 85.7%

### Generate API (`llm/generate.go`)
- `Generate` - 97.7%
- `executeToolsConcurrently` - 90.0%
- `StreamGenerate` - 75.0%
- `GenerateObject` - 88.9%

## Functions at 0% Coverage (Prioritize These)

### agent package
| Function | File |
|----------|------|
| `listDirectoryRecursive` | `exec_local.go:181` |
| `grepWithRegex` | `exec_local.go:405` |
| `globRecursive` | `exec_local.go:487` |
| `Cleanup` | `exec_local.go:539` |
| `OSVersion` | `exec_local.go:549` |
| `findSequenceFuzzy` | `patch.go:489` |
| `ErrorTurn.TurnTimestamp` | `session.go:100` |
| `SystemTurn.TurnTimestamp` | `session.go:109` |

### attractor package
| Function | File |
|----------|------|
| `createClientFromEnv` | `backend_agent.go:95` |
| `ValidateConditionSyntax` | `conditions.go:80` |
| `ValidFidelityModes` | `fidelity.go:28` |
| `NewConsoleInterviewer` | `interviewer.go:166` |
| `ServeHTTP` | `server.go:142` |
| `interceptingHandler.Type` | `server.go:674` |
| `ValidationSeverity.String` | `validate.go:20` |

### llm package
| Function | File |
|----------|------|
| `WithAnthropicTimeout` | `anthropic.go:40` |
| `WithOpenAITimeout` | `openai.go:35` |
| `translateResponseFormat` | `openai.go:351` |
| `fallbackAdapter.Name` | `client.go:83` |
| `fallbackAdapter.Complete` | `client.go:85` |
| `fallbackAdapter.Stream` | `client.go:93` |
| `fallbackAdapter.Close` | `client.go:101` |
| `TextPromptResult.Response` | `generate.go:75` |
| Many error type `Error`/`Unwrap`/`As` methods | `errors.go` (various) |

### cmd/makeatron package
| Function | File |
|----------|------|
| `main` | `main.go:33` |
| `runServer` | `main.go:192` |

## Priority Recommendations

### High Priority (test these next)
1. `executeBranchChain` (72.2%) - parallel execution is complex and subtle
2. `createClientFromEnv` (0%) - runtime entry point for LLM usage in pipelines
3. `ResumeFromCheckpoint` (84.0%) - data loss risk if checkpoint resume is buggy
4. `grepWithRegex` / `globRecursive` (0%) - fallback paths that will execute when ripgrep is unavailable
5. `ValidateConditionSyntax` (0%) - validation gap means bad conditions silently pass

### Medium Priority
6. `findSequenceFuzzy` (0%) - fuzzy patch matching fallback
7. `translateResponseFormat` (0%) - OpenAI structured output support
8. `StreamGenerate` (75.0%) - streaming is a primary user-facing API
9. `runServer` (0%) - HTTP server mode entry point

### Low Priority
10. Error type boilerplate methods (Error/Unwrap/As at 0%) - these are trivial one-liners
11. `main` (0%) - CLI entry point, tested indirectly through `run()`
12. `NewConsoleInterviewer` (0%) - just sets os.Stdin/os.Stdout defaults
13. `lexer.String` (14.3%) - debug helper, not critical path

## Coverage Maintenance Strategy

### CI Integration
Run `scripts/check-coverage.sh` in CI. It fails the build if any critical package
drops below its threshold.

### When Adding New Code
- Write tests first (TDD). New code should ship with >= 80% coverage.
- Critical path code (engine, handlers, edge selection) should ship with >= 90%.

### When Modifying Existing Code
- Coverage for the modified file should not decrease.
- If touching a 0% function, add at least basic happy-path coverage.

### Quarterly Review
- Re-run full coverage analysis.
- Ratchet thresholds upward if coverage has improved.
- Update this document with new baselines.

### What NOT to Do
- Do not chase 100% coverage globally. Diminishing returns past ~90%.
- Do not write tests for trivial getters/setters just to hit a number.
- Do not test `main()` directly; test `run()` and the functions it calls.
- Do not add mock modes to reach coverage targets. Use real test doubles.
