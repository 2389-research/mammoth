# Pipeline Reliability Fixes

## Problem

Three pipeline runs (vkbvault, kaya-bot, Clint) exposed systemic bugs where
mammoth reports success despite incomplete or broken output. Root causes:

1. `read_file` returns line-numbered content; `write_file` doesn't strip it,
   so agents re-writing files embed line numbers into source code.
2. `extractResult()` defaults `Success: true`. Agents that hit the turn limit
   never emit an OUTCOME marker, so exhausted stages report success.
3. No mechanism for trustless, independent verification of agent claims.
   Agents self-report outcomes and can lie about test results or file creation.
4. Fan-in nodes accept parallel results without consistency checks.
5. Exit nodes declare pipeline success without verifying the stated goal.

## Fix 1: Strip line numbers on write (P0)

**File:** `agent/tools_core.go`

Add a `stripLineNumbers` function that removes leading line-number prefixes
matching the pattern `^\s*\d+\s*[|\t]` from each line. Call it in the
`write_file` handler before `env.WriteFile()`.

The regex pattern matches the exact format produced by `formatLineNumbers`
(`  1 | code`) and also covers tab-delimited variants (`1\tcode`). It will
not false-positive on code like `x = 123 | mask` because the line-number
pattern requires the number to be at the start of the line with only
leading whitespace before it.

## Fix 2: Turn exhaustion = failure (P0)

**Files:** `agent/session.go`, `agent/loop.go`, `attractor/backend_agent.go`

1. Add `HitTurnLimit bool` to `Session` struct.
2. In `loop.go` line 37-40, set `session.HitTurnLimit = true` when the turn
   limit breaks the loop.
3. In `backend_agent.go` `extractResult()`, after the current OUTCOME marker
   check: if `session.HitTurnLimit` is true and no explicit success marker
   (`OUTCOME:PASS` or `OUTCOME:SUCCESS`) was found, set `result.Success = false`.

Agents that finish early and declare `OUTCOME:SUCCESS` are still respected.
Only silent exhaustion without a success marker triggers failure.

## Fix 3: `verify_command` on codergen nodes (P1)

**File:** `attractor/handlers_codergen.go`, `attractor/handlers_conditional.go`

Extract a shared `runVerifyCommand` helper (modeled on `ToolHandler`'s exec
pattern) that:
1. Takes a command string, working directory, timeout, and context
2. Runs it via `sh -c`
3. Returns the exit code, stdout, and stderr

In `CodergenHandler.Execute()` and `ConditionalHandler.executeWithAgent()`,
after the agent returns, check for a `verify_command` attribute. If present:
- Run the verify command
- Exit 0 → keep agent's outcome
- Non-zero → override to `StatusFail` with verify output as failure reason
- Store verify output as artifact (`nodeID.verify_output`)

## Fix 4: Deterministic verify nodes — `shape=octagon` (P1)

**Files:** `attractor/handlers.go`, new `attractor/handlers_verify.go`

New `VerifyHandler` mapped to `shape=octagon`, handler type `"verify"`.
Reads `command` attribute, runs it via the shared `runVerifyCommand` helper,
returns `StatusSuccess` on exit 0 or `StatusFail` on non-zero. No LLM.

Also supports `working_dir`, `timeout`, and `env_*` attributes (same as
ToolHandler). The key difference from ToolHandler: VerifyHandler sets
`"outcome"` in ContextUpdates so conditional edges work, and its failure
notes emphasize verification semantics.

Register in `DefaultHandlerRegistry()` and add to `shapeToType`.

## Fix 5: Fan-in `verify_command` (P1)

**File:** `attractor/handlers_fanin.go`

After recording the merge, check for a `verify_command` attribute. If present,
run it via `runVerifyCommand`. Non-zero exit → `StatusFail` with the command
output as failure reason.

## Fix 6: Exit node `verify_command` (P2)

**File:** `attractor/handlers_exit.go`

Same pattern. If the exit node has a `verify_command`, run it before declaring
pipeline success. Non-zero → pipeline fails.

## Shared Infrastructure

The `runVerifyCommand` helper goes in a new file
`attractor/verify_command.go`. It mirrors the ToolHandler's exec pattern
(timeout, working dir, process group management, output capture) but returns
a result struct instead of an Outcome. All handlers that support
`verify_command` call the same function.

## What Stays The Same

- All existing node shapes and handler types
- Edge selection algorithm
- Condition evaluation
- Pipeline context and artifact store
- Agent loop and tool execution
