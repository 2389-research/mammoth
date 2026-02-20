# Mammoth Audit Command Design

## Problem

When a mammoth pipeline fails, the user sees a terse error message. The event
journal in `.mammoth/runs/<id>/events.jsonl` contains everything needed to
understand what happened, but reading raw NDJSON is painful. We need a
`mammoth audit` command that reconstructs a human-readable narrative from the
run data, with LLM-powered root cause analysis.

## Decisions

| Question | Answer |
|----------|--------|
| Input source | No args = most recent run; optional `<runID>` for specific run |
| Output format | Plain text to terminal (structured sections) |
| Detail level | Summary by default; `--verbose` for full tool traces |
| Graph context | Include brief flow summary from the DOT source |
| Narrative engine | LLM-powered (uses same provider auto-detection as pipelines) |
| LLM config | Same auto-detection as pipeline runs (ANTHROPIC > OPENAI > GEMINI) |

## Architecture

```
cmd/mammoth/main.go
  parseAuditArgs()        — parse "audit [runID] [--verbose]"
  runAudit()              — load data, call audit, print result

attractor/audit.go
  AuditRequest            — RunState + events + graph + verbose flag
  AuditReport             — narrative text + structured metadata
  GenerateAudit(ctx, req, adapter) (*AuditReport, error)
    buildAuditContext()   — marshal run data into structured text for LLM
    LLM call              — system prompt + context → narrative

attractor/audit_test.go
  TestBuildAuditContext   — verify context construction (pure, no LLM)
```

## Data Flow

1. CLI loads run data from `.mammoth/runs/<id>/`:
   - `manifest.json` → RunState (status, timestamps, error)
   - `events.jsonl` → []EngineEvent (full timeline)
   - `source.dot` → original pipeline source
   - `context.json` → execution context bindings

2. `buildAuditContext()` transforms this into a structured text blob:
   - Pipeline flow summary (linearized node path from DOT)
   - Run metadata (status, duration, workdir)
   - Event timeline with timestamps and durations
   - In verbose mode: raw tool call arguments
   - In default mode: tool calls aggregated as counts per node

3. LLM receives system prompt + context, produces narrative with sections:
   - **Summary** — one paragraph: what ran, how it ended
   - **Timeline** — chronological key events with relative timestamps
   - **Diagnosis** — root cause analysis of failures
   - **Suggestions** — actionable next steps (retry policies, API limits, etc.)

4. CLI prints the narrative to stdout.

## LLM Prompt Design

System prompt instructs the LLM to:
- Act as a pipeline execution analyst
- Produce a concise, structured report (not verbose prose)
- Identify patterns: rate limits, retry loops, agent errors, validation failures
- Suggest specific mammoth flags or config changes when applicable
- Use relative timestamps ("+1.7s") not absolute wall clock

## CLI Interface

```
mammoth audit                      # audit most recent run
mammoth audit <runID>              # audit specific run
mammoth audit --verbose            # include full tool call details
mammoth audit --verbose <runID>    # both
```

Subcommand uses its own FlagSet (like `serve` and `setup`).

## Files to Create/Modify

| File | Action |
|------|--------|
| `attractor/audit.go` | NEW — AuditRequest, AuditReport, GenerateAudit, buildAuditContext |
| `attractor/audit_test.go` | NEW — TestBuildAuditContext (unit), integration test (build-tagged) |
| `cmd/mammoth/main.go` | MODIFY — add parseAuditArgs, runAudit, wire subcommand dispatch |
| `cmd/mammoth/help.go` | MODIFY — add audit to usage/examples |

## Error Handling

- No API key set → print error suggesting `ANTHROPIC_API_KEY` etc.
- No runs found → print "no runs found in .mammoth/runs/"
- Run ID not found → print "run <id> not found" with list of available IDs
- LLM call fails → print error, suggest checking API key/connectivity

## Example Output (from real failed run)

```
Audit: ebbe59cd241c09df
Pipeline  kayabot4.dot
Status    FAILED (5.3s)

Flow: Start → setup → implement → verify → check → Done

The pipeline failed because node "setup" hit the maximum visit limit (3).
All three attempts failed with the same error: Anthropic API returned
429 Too Many Requests due to concurrent connection rate limits.

Timeline:
  +0.0s  Start         passed
  +0.0s  setup         FAIL — 429 rate_limit_error (1.7s)
  +1.7s  setup (retry) FAIL — 429 rate_limit_error (1.9s)
  +3.6s  setup (retry) FAIL — 429 rate_limit_error (1.7s)
  +5.3s  Pipeline aborted: max_node_visits exceeded

This is a transient API rate limit, not a pipeline logic error. The setup
node never got to execute its actual work. Consider:
  - Waiting a few minutes and retrying
  - Using `mammoth -retry patient kayabot4.dot` for exponential backoff
  - Checking your Anthropic API tier for concurrent connection limits
```
