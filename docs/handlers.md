# Handler Reference

Handlers are the execution units of the makeatron pipeline engine. Each node in a pipeline graph is dispatched to a handler based on its shape or explicit `type` attribute. There are 9 built-in handlers.

## Handler Resolution

The engine resolves which handler to use for a node in this order:

1. **Explicit `type` attribute** on the node (e.g., `type="wait.human"`)
2. **Shape-based mapping** using the node's `shape` attribute
3. **Default**: `codergen` (the LLM handler)

## StartHandler

**Type string**: `start`
**Shape**: `Mdiamond`
**Purpose**: Pipeline entry point. Initializes execution.

The StartHandler performs no work beyond recording a start timestamp in the pipeline context. Every pipeline must have exactly one start node.

### Context Updates

| Key | Value |
|-----|-------|
| `_started_at` | RFC3339Nano timestamp of when the pipeline started |

### Configuration

No configurable attributes. The start node is identified solely by its shape.

### Example

```dot
start [shape=Mdiamond, label="Start"]
```

---

## ExitHandler

**Type string**: `exit`
**Shape**: `Msquare`
**Purpose**: Pipeline terminal node. Records completion.

The ExitHandler records the finish time and returns success. Goal gate enforcement happens in the engine after this handler returns -- if any goal-gated node has a non-success outcome, the engine retries from the configured `retry_target`.

### Context Updates

| Key | Value |
|-----|-------|
| `_finished_at` | RFC3339Nano timestamp of when the pipeline finished |

### Configuration

No configurable attributes. Exit nodes are identified by shape.

### Example

```dot
done [shape=Msquare, label="Done"]
```

---

## CodergenHandler

**Type string**: `codergen`
**Shape**: `box` (also the default for any unrecognized shape)
**Purpose**: LLM-powered coding agent node.

The CodergenHandler sends a prompt to an LLM backend and returns the result. When no backend is configured (e.g., during validation-only runs), it operates in stub mode, recording the configuration without making LLM calls.

### Node Attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `prompt` | string | Falls back to `label`, then node ID | Instructions sent to the LLM agent. Supports `$variable` expansion. |
| `label` | string | node ID | Display name; used as fallback prompt. |
| `llm_model` | string | From stylesheet or provider default | Model identifier (e.g., `claude-opus-4-6`). |
| `llm_provider` | string | From stylesheet or provider default | Provider name (`anthropic`, `openai`, `gemini`). |
| `max_turns` | int | 20 | Maximum number of agent loop turns. |
| `workdir` | string | "" | Working directory for the agent's file and command operations. |

### Context Updates

| Key | Value |
|-----|-------|
| `last_stage` | Node ID |
| `codergen.prompt` | The prompt that was sent |
| `codergen.model` | Model used (if specified) |
| `codergen.provider` | Provider used (if specified) |
| `codergen.tool_calls` | Number of tool calls made (backend mode only) |
| `codergen.tokens_used` | Total tokens consumed (backend mode only) |

### Example

```dot
implement [
    shape=box,
    label="Implement Feature",
    prompt="Write the code based on the plan for: $goal",
    llm_model="claude-opus-4-6",
    llm_provider="anthropic",
    max_turns=30,
    goal_gate=true,
    max_retries=3
]
```

---

## ConditionalHandler

**Type string**: `conditional`
**Shape**: `diamond`
**Purpose**: Routing node for conditional branching.

The ConditionalHandler itself is a no-op that returns success. The actual routing logic is handled by the engine's edge selection algorithm, which evaluates `condition` attributes on outgoing edges using the [condition expression language](dsl-reference.md#condition-expressions).

### Context Updates

| Key | Value |
|-----|-------|
| `last_stage` | Node ID |

### Example

```dot
tests_ok [shape=diamond, label="Tests passing?"]

tests_ok -> deploy    [label="Pass", condition="outcome = success"]
tests_ok -> implement [label="Fail", condition="outcome != success"]
```

The `outcome` key is automatically set by the engine to the status of the previously executed node (`success`, `fail`, `partial_success`, etc.).

---

## WaitForHumanHandler (Human Gate)

**Type string**: `wait.human`
**Shape**: `hexagon`
**Purpose**: Pauses the pipeline for human input.

The WaitForHumanHandler presents choices derived from the node's outgoing edges to a human via the `Interviewer` interface. The pipeline blocks until the human responds or a timeout expires.

### Node Attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `label` | string | "Select an option:" | Question text shown to the human. |
| `timeout` | duration | none (wait forever) | Time limit for human response (e.g., `5m`, `1h`). |
| `default_choice` | string | "" | Edge label to auto-select if timeout expires. |
| `reminder_interval` | duration | "" | Re-prompt interval (if the interviewer supports it). |

### Edge Labels as Choices

Outgoing edges become the human's options. Edge labels support accelerator key prefixes:

```dot
review [shape=hexagon, label="Review the changes", timeout="30m", default_choice="[A] Approve"]

review -> ship    [label="[A] Approve"]
review -> revise  [label="[R] Revise"]
review -> reject  [label="[X] Reject"]
```

### Context Updates

| Key | Value |
|-----|-------|
| `human.gate.selected` | Accelerator key of the selected option |
| `human.gate.label` | Full label of the selected edge |
| `human.timed_out` | Boolean: whether the timeout expired |
| `human.response_time_ms` | Response time in milliseconds |

### Timeout Behavior

When a `timeout` is configured and expires:
- If `default_choice` is set and matches an edge label, that edge is auto-selected.
- If `default_choice` is not set or doesn't match, the node fails.

### Interviewer Implementations

Makeatron provides several built-in `Interviewer` implementations:

| Implementation | Description |
|----------------|-------------|
| `ConsoleInterviewer` | Reads from stdin, writes prompts to stdout. Default for CLI mode. |
| `AutoApproveInterviewer` | Always returns a configured answer. For automated pipelines. |
| `CallbackInterviewer` | Delegates to a Go callback function. For custom integrations. |
| `QueueInterviewer` | Reads answers from a pre-filled FIFO queue. For testing/replay. |
| `RecordingInterviewer` | Wraps another interviewer and records all Q&A pairs. For auditing. |
| HTTP Interviewer | Used in server mode. Questions appear at the REST API; answers are submitted via HTTP POST. |

---

## ParallelHandler

**Type string**: `parallel`
**Shape**: `component`
**Purpose**: Fan-out node that spawns concurrent branches.

The ParallelHandler identifies all outgoing edges as parallel branches and signals the engine to execute them concurrently. Each branch gets a forked copy of the pipeline context and runs independently.

### Node Attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `join_policy` | string | `wait_all` | How to merge branch results. See Join Policies below. |
| `error_policy` | string | `continue` | How to handle branch failures: `continue` (run all) or `fail_fast` (cancel remaining on first failure). |
| `max_parallel` | int | 4 | Maximum number of branches running concurrently. |

### Join Policies

| Policy | Description |
|--------|-------------|
| `wait_all` | All branches must succeed. Fails if any branch fails. All contexts merged. |
| `wait_any` | At least one branch must succeed. Only successful branches merged. |
| `k_of_n` | At least K branches must succeed. K read from `parallel.k_required` in context. |
| `quorum` | Strict majority (>50%) of branches must succeed. |

### Context Merge

Branch contexts are merged into the parent using last-write-wins semantics. All merge operations are logged. Artifact references are consolidated into a `parallel.artifacts` manifest.

### Example

```dot
fork [shape=component, label="Fan Out", join_policy="wait_all", max_parallel=3]
branch_a [shape=box, prompt="Task A"]
branch_b [shape=box, prompt="Task B"]
merge [shape=tripleoctagon, label="Merge"]

fork -> branch_a
fork -> branch_b
branch_a -> merge
branch_b -> merge
```

---

## FanInHandler

**Type string**: `parallel.fan_in`
**Shape**: `tripleoctagon`
**Purpose**: Convergence point for parallel branches.

The FanInHandler reads parallel branch results from the pipeline context and confirms the merge. Branches stop execution when they reach a fan-in node without executing it -- the fan-in handler runs once after all branches have been collected.

### Context Updates

| Key | Value |
|-----|-------|
| `last_stage` | Node ID |
| `parallel.fan_in.completed` | `true` |

### Example

```dot
merge [shape=tripleoctagon, label="Merge Results"]
```

---

## ToolHandler

**Type string**: `tool`
**Shape**: `parallelogram`
**Purpose**: Execute an external shell command.

The ToolHandler runs a shell command via `sh -c` and captures stdout, stderr, and exit code. Large output (>10KB) is automatically stored as an artifact with a truncated version in the outcome notes.

### Node Attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `command` | string | Falls back to `prompt` | Shell command to execute. |
| `prompt` | string | "" | Fallback command source if `command` is not set. |
| `timeout` | duration | `30s` | Execution timeout. The entire process group is killed on timeout. |
| `working_dir` | string | "" | Working directory for command execution. |
| `env_*` | string | inherited | Environment variables. Prefix `env_` is stripped: `env_PATH="/usr/bin"` sets `PATH`. |

### Context Updates

| Key | Value |
|-----|-------|
| `last_stage` | Node ID |
| `tool.stdout` | Full stdout output |
| `tool.stderr` | Full stderr output |
| `tool.exit_code` | Process exit code (0 = success) |

### Example

```dot
run_tests [
    shape=parallelogram,
    label="Run Tests",
    command="go test ./... -v",
    timeout="5m",
    working_dir="/project",
    env_GOFLAGS="-count=1"
]
```

---

## ManagerLoopHandler

**Type string**: `stack.manager_loop`
**Shape**: `house`
**Purpose**: Supervision loop implementing observe/guard/steer pattern.

The ManagerLoopHandler runs a supervision cycle over a child pipeline or agent. Each iteration:
1. **Observe**: Inspect current state and produce an observation.
2. **Guard**: Evaluate whether the agent is on track.
3. **Steer**: If the guard fails, apply a correction.

When no `ManagerBackend` is configured, the handler operates in stub mode, logging the configuration.

### Node Attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `observe_prompt` | string | "" | Prompt for the observe step. |
| `guard_condition` | string | "" | Condition evaluated during the guard step. |
| `steer_prompt` | string | "" | Prompt for steering corrections. |
| `max_iterations` | int | 10 | Maximum observe/guard/steer cycles. |
| `sub_pipeline` | string | "" | Identifier for the sub-pipeline being supervised. |
| `manager.poll_interval` | duration | `45s` | Polling interval between observations. |
| `manager.max_cycles` | int | 1000 | Maximum total cycles (legacy). |
| `manager.stop_condition` | string | "" | Condition that terminates the loop (legacy). |
| `manager.actions` | string | `observe,wait` | Comma-separated action list (legacy). |

### Context Updates

| Key | Value |
|-----|-------|
| `last_stage` | Node ID |
| `manager.iterations_completed` | Number of completed iterations |
| `manager.steers_applied` | Number of steer corrections applied |
| `manager.last_observation` | Text of the last observation (backend mode) |
| `manager.sub_pipeline` | Sub-pipeline identifier (if set) |
| `manager.child_dotfile` | Child DOT file path from graph attributes |

### Example

```dot
supervisor [
    shape=house,
    label="Supervise Agent",
    observe_prompt="Check the agent's progress",
    guard_condition="agent is making forward progress",
    steer_prompt="Redirect the agent to focus on the core task",
    max_iterations=20
]
```

---

## Custom Handlers

You can register custom handlers by implementing the `NodeHandler` interface:

```go
type NodeHandler interface {
    Type() string
    Execute(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error)
}
```

Register custom handlers before creating the engine:

```go
registry := attractor.DefaultHandlerRegistry()
registry.Register(&MyCustomHandler{})

engine := attractor.NewEngine(attractor.EngineConfig{
    Handlers: registry,
})
```

Set the node's `type` attribute to your handler's `Type()` string to route to it.

See also: [DSL Reference](dsl-reference.md) for node attribute syntax, [CLI Usage](cli-usage.md) for running pipelines, [Backend Configuration](backend-config.md) for LLM backend setup.
