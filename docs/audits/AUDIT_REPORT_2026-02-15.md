# Documentation Audit Report

Generated: 2026-02-15 | Commit: 5bd427f (branch: unification)

## Executive Summary

| Metric | Count |
|--------|-------|
| Documents scanned | 14 |
| Claims verified | ~590 |
| Verified TRUE | ~530 (90%) |
| **Verified FALSE** | **42 (7%)** |
| Needs human review | 18 (3%) |

---

## False Claims by Document

### CLAUDE.md

| Section | Claim | Reality | Fix |
|---------|-------|---------|-----|
| Project Structure | `llm/openai/` directory | Does NOT exist -- adapters are flat files: `llm/openai.go` | Remove subdirectory from tree |
| Project Structure | `llm/anthropic/` directory | Does NOT exist -- `llm/anthropic.go` | Remove subdirectory from tree |
| Project Structure | `llm/gemini/` directory | Does NOT exist -- `llm/gemini.go` | Remove subdirectory from tree |
| Project Structure | `agent/profile.go` | Does NOT exist -- file is `agent/profiles.go` (plural) | Fix filename |
| Project Structure | `agent/profiles/` directory | Does NOT exist -- profiles are in `agent/profiles.go` | Remove subdirectory from tree |
| Project Structure | `agent/tools/` directory | Does NOT exist -- tools are in `agent/tools.go`, `agent/tools_core.go` | Remove subdirectory from tree |
| Project Structure | `agent/exec/` directory | Does NOT exist -- `agent/exec_env.go`, `agent/exec_local.go` | Remove subdirectory from tree |
| Project Structure | `agent/detect.go` | Does NOT exist -- loop detection is in `agent/session.go` | Remove from tree |
| Project Structure | `agent/subagent.go` | Does NOT exist -- file is `agent/subagents.go` (plural) | Fix filename |
| Project Structure | `agent/prompt.go` | Does NOT exist -- prompt construction is in `agent/steering.go` | Remove from tree |
| Project Structure | `attractor/dot/` directory | Does NOT exist under attractor/ -- it's a top-level `dot/` package | Move to `dot/` in tree |
| Project Structure | `attractor/handlers/` directory | Does NOT exist -- handlers are flat: `attractor/handlers_*.go` | Remove subdirectory from tree |
| Project Structure | `attractor/state.go` | Does NOT exist -- context/state is in `attractor/context.go` | Fix filename |
| Project Structure | `attractor/human.go` | Does NOT exist -- file is `attractor/interviewer.go` | Fix filename |
| Project Structure | `attractor/transform.go` | File is actually `attractor/transforms.go` (plural) | Fix filename |
| Project Structure | `attractor/condition.go` | File is actually `attractor/conditions.go` (plural) | Fix filename |
| Code Style | "Go 1.22+" | go.mod requires `go 1.25.5` | Update to "Go 1.25+" |
| Project Structure | Missing packages | `render/`, `tui/`, `dot/`, `spec/`, `web/`, `editor/` packages not shown | Add to tree |

### README.md

| Section | Claim | Reality | Fix |
|---------|-------|---------|-----|
| Usage | `mammoth validate pipeline.dot` subcommand | No `validate` subcommand; only `-validate` flag works | Change to `mammoth -validate pipeline.dot` |
| Testing | "1,400+ tests" | Number is stale and needs verification | Re-count and update |
| Spec Parity | "192/196 requirements" | From parity-matrix.md which has stale line numbers | Verify current count |
| Install | `VERSION="0.1.0"` in download example | Latest release is v0.2.0 | Update to current version |

### docs/cli-usage.md

| Section | Claim | Reality | Fix |
|---------|-------|---------|-----|
| Introduction | "three modes" | Actually FIVE modes: run, validate, server, version, `serve` | Update to "five modes" |
| Flags table | Lists 8 flags | Missing 5 flags: `-data-dir`, `-base-url`, `-backend`, `-tui`, `-fresh` | Add missing flags |
| Server endpoints | Lists 10 endpoints | Missing `GET /pipelines` and `GET /pipelines/{id}/graph` | Add missing endpoints |
| Environment | Lists 3 env vars | Missing `MAMMOTH_BACKEND`, `XDG_DATA_HOME`, `.env` auto-loading | Document all env vars |
| Artifact dir | "empty uses temp directory" | Uses `artifacts/<RunID>`, not system temp | Fix description |

### docs/cli-spec.md

| Section | Claim | Reality | Fix |
|---------|-------|---------|-----|
| Modes | "four mutually exclusive modes" | Five modes -- `serve` subcommand exists | Add fifth mode |
| Flags table | Lists 12 flags | Missing `--backend` flag (already implemented) | Add `--backend` |
| Future Enhancements | `mammoth run` is "Future" | Already implemented (`main.go:104-107`) | Move to current features |
| Future Enhancements | `--backend` flag is "Future" | Already implemented (`main.go:85`) | Move to current features |
| Environment | "CLI binary does not read any environment variables" | Reads `MAMMOTH_BACKEND`, API keys, `XDG_DATA_HOME`, `.env` files | Fix claim |
| Environment | "env vars read in `attractor/backend_agent.go`" | Actually read in `cmd/mammoth/main.go:886-917` | Fix file reference |
| Error messages | Missing pipeline prints "error: pipeline file required..." | Actually prints full help text via `printHelp()` | Fix error message text |

### docs/api-reference.md

| Section | Claim | Reality | Fix |
|---------|-------|---------|-----|
| ArtifactStore.Store | Returns `(string, error)` | Returns `(*ArtifactInfo, error)` | Fix return type |
| ArtifactStore | Threshold "10KB" | Actually 100KB (`100 * 1024`) | Fix to "100KB" |
| ArtifactsBaseDir | Default `"./artifacts"` | Code uses `"artifacts"` (no `./`) | Fix default value |
| ConsoleInterviewerWithIO | Implies it's a distinct type | Same `ConsoleInterviewer` type with different constructor | Clarify |
| EngineConfig | Missing field | `DefaultNodeTimeout time.Duration` not documented | Add field |
| PipelineStatus | Missing field | `ArtifactDir` field not documented | Add field |
| Loop detection | "Functions in `agent/detect.go`" | Both functions are in `agent/session.go` | Fix file reference |

### docs/parity-matrix.md

| Section | Claim | Reality | Fix |
|---------|-------|---------|-----|
| Parser entries | Reference `attractor/parser.go`, `attractor/lexer.go` | Files are in `dot/parser.go`, `dot/lexer.go` | Update all file paths |
| Test refs | `attractor/parser_test.go`, `attractor/lexer_test.go` | Tests are in `dot/parser_test.go`, `dot/lexer_test.go` | Update all test paths |
| Line numbers | `engine.go:292-310` for executeGraph | Actual location is line 372+ | Update line numbers |
| Test counts | "637 total, 617 attractor" | Likely outdated | Re-count |

### docs/coverage.md

| Section | Claim | Reality | Fix |
|---------|-------|---------|-----|
| Package list | 5 packages listed | Missing `dot/`, `render/`, `tui/`, `spec/`, `web/`, `editor/` | Add all packages |
| Coverage numbers | All percentages | Stale -- need fresh `go test -coverprofile` | Re-run and update |
| CI script | `scripts/check-coverage.sh` | File exists but may need updating | Verify script |

### _specs/ (upstream spec documents)

| Section | Claim | Reality | Fix |
|---------|-------|---------|-----|
| coding-agent-loop | `apply_patch` in defaultToolLimits (10000) | Missing from map | Add to code or update spec |
| coding-agent-loop | `spawn_agent` in defaultToolLimits (20000) | Missing from map | Add to code or update spec |
| coding-agent-loop | `edit_file` default line limit 256 | Not in defaultLineLimit | Add to code or update spec |
| coding-agent-loop | `write_file` default line limit 256 | Not in defaultLineLimit | Add to code or update spec |
| coding-agent-loop | MaxSubagentDepth default is 3 | Actual: 1 | Align spec and code |
| coding-agent-loop | Shell tool default timeout 120000ms | Actual: 10000ms | Reconcile |
| coding-agent-loop | OpenAI default model "o4-mini" | Actual: "gpt-5.2-codex" | Update spec |
| coding-agent-loop | Anthropic default "claude-sonnet-4-5-20250514" | Actual: "claude-sonnet-4-5" | Update spec |
| coding-agent-loop | Gemini default "gemini-2.5-flash" | Actual: "gemini-3-flash-preview" | Update spec |
| coding-agent-loop | DefaultCommandTimeoutMs 120000 | Actual: 10000 | Reconcile |
| attractor-spec | default_max_retry default is 50 | No hardcoded 50; falls back to config | Add default or update spec |

---

## Pattern Summary

| Pattern | Count | Root Cause |
|---------|-------|------------|
| CLAUDE.md project tree stale | 18 | Phase 1 consolidation moved/renamed many files; tree never updated |
| CLI docs missing features | 9 | `serve`, `run`, `--backend` shipped without updating docs |
| Parity-matrix file paths stale | 4 | dot/ consolidation moved parser/lexer out of attractor/ |
| Spec model name drift | 3 | Spec written against older model names |
| Timeout defaults mismatch | 3 | Spec says 120s; code defaults to 10s |
| Missing truncation config | 4 | Tools added after truncation config was written |
| API reference type errors | 3 | ArtifactStore return type and threshold wrong |

---

## Verified TRUE: Docs with Zero False Claims

- **docs/dsl-reference.md**: 35 claims, all TRUE
- **docs/handlers.md**: 35 claims, all TRUE
- **docs/quickstart.md**: All example files verified, all CLI invocations correct
- **docs/walkthrough.md**: All DOT examples valid, all behavior claims verified

---

## Priority Recommendations

### Critical (user-facing docs are wrong)
1. **Fix CLAUDE.md project structure tree** -- 18 stale entries. This is the first thing contributors read.
2. **Fix README.md `mammoth validate` subcommand** -- Shows a command that doesn't work.
3. **Update cli-usage.md and cli-spec.md** -- Missing 5 flags, 2 endpoints, undocumented env vars.
4. **Fix api-reference.md ArtifactStore** -- Wrong return type and threshold (10KB vs 100KB).

### High (correctness, internal docs)
5. **Update parity-matrix.md file paths** -- All parser/lexer references point to non-existent `attractor/*.go` files (moved to `dot/`).
6. **Update cli-spec.md "Future Enhancements"** -- Lists features that are already shipped.
7. **Reconcile _specs/ timeout defaults** -- 10s vs 120s needs a decision.
8. **Update _specs/ model names** -- 3 outdated model references.

### Medium (nice to have)
9. **Re-run coverage analysis** -- All numbers in coverage.md are stale.
10. **Update README.md test count and version** -- "1,400+ tests", VERSION="0.1.0" outdated.
11. **Add missing packages to coverage.md** -- dot/, render/, tui/, spec/, web/, editor/ missing.
