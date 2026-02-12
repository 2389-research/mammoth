# Documentation Audit Report

Generated: 2026-02-11 | Commit: 604b53e

## Executive Summary

| Metric | Count |
|--------|-------|
| Documents scanned | 11 |
| Claims verified | ~310 |
| Verified TRUE | ~293 (94%) |
| **Verified FALSE** | **14 (5%)** |
| Needs human review | 3 (1%) |

## False Claims Requiring Fixes

### README.md

| Line | Claim | Reality | Fix |
|------|-------|---------|-----|
| 44 | `mammoth serve --addr :8080` | No `serve` subcommand; flag is `-server` + `-port` | Change to `mammoth -server -port 8080` |
| 53 | "1,578 tests" | Actual: 1,433 tests | Update count |
| 78 | "191/193 requirements" | Actual: 192 DONE / 196 total (3 PARTIAL, 1 MISSING) | Update to "192/196" |
| 82 | "See LICENSE for details" | No LICENSE file exists | Add LICENSE file or remove reference |

### docs/cli-spec.md

| Line | Claim | Reality | Fix |
|------|-------|---------|-----|
| 148-149 | Server prints `listening on :2389` | Actual: `listening on 127.0.0.1:2389` (includes IP) | Update message format |
| -- | Missing flags: `--tui`, `--fresh`, `--data-dir`, `--base-url` | These flags exist in main.go:66-70 | Add to flags table |

### docs/handlers.md

| Line | Claim | Reality | Fix |
|------|-------|---------|-----|
| 71 | CodergenHandler "operates in stub mode" when no backend | Returns StatusFail with error message (code comment: "this is a hard error, not a stub") | Update to describe failure behavior |

### docs/api-reference.md

| Line | Claim | Reality | Fix |
|------|-------|---------|-----|
| ~1328 | Graph struct: `ID string`, `Nodes []*Node` | Actual: `Name string`, `Nodes map[string]*Node`; also missing `NodeDefaults`, `EdgeDefaults`, `Subgraphs` fields | Update Graph struct |
| ~1160 | 8 event types listed | Missing `EventStageStalled` (engine.go:28) | Add to event types table |
| ~890 | EngineConfig fields | Missing: `AutoCheckpointPath`, `ArtifactsBaseDir`, `RunID`, `BaseURL` | Add missing fields |
| ~1005 | AgentRunConfig fields | Missing: `EventHandler func(EngineEvent)` | Add field |
| ~1015 | AgentRunResult fields | Missing: `ToolCallLog`, `TurnCount`, `Usage` | Add missing fields |

### docs/coverage.md

| Line | Claim | Reality | Fix |
|------|-------|---------|-----|
| various | Test counts from earlier build | Counts outdated; actual per-package: llm 198, sse 33, agent 173, attractor 737, cmd/mammoth 64, render 46, tui 182 | Update counts |

## Documents With No False Claims

| Document | Claims Checked | Notes |
|----------|---------------|-------|
| docs/parity-matrix.md | ~195 | All status claims verified correct |
| docs/backend-config.md | ~40 | All env vars, models, retry policies correct |
| docs/walkthrough.md | ~30 | All examples, file refs, code snippets correct |
| docs/dsl-reference.md | ~45 | All shape mappings, condition syntax, fidelity modes correct |
| docs/quickstart.md | ~45 | 1 minor table formatting issue (cosmetic) |

## Pattern Summary

| Pattern | Count | Root Cause |
|---------|-------|------------|
| Outdated test counts | 2 | Tests grew, docs not updated |
| Missing CLI flags | 4 | New flags added after docs written |
| Missing API struct fields | 4 | Fields added during implementation, docs not refreshed |
| Behavioral description drift | 2 | Implementation changed from stub to hard-fail; server addr format changed |
| Stale README | 2 | Serve subcommand never existed; LICENSE never added |

## Human Review Queue

- [ ] README line 82: Decide whether to add a LICENSE file or remove the reference
- [ ] cli-spec.md: Confirm whether TTY vs non-TTY output differences should be documented
- [ ] api-reference.md: Decide whether agent-level events deserve their own subsection
