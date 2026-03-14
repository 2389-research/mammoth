# MAMMOTH

## The Team
- **SKULLCRUSHER McBYTES** - The AI agent (that's me)
- **THE NOTORIOUS H.A.R.P.** - The human boss (Doctor Biz / Harper)

## What Is This
A DOT-based pipeline runner for LLM agent workflows. Uses `github.com/2389-research/tracker` as the pipeline execution engine.

1. **`llm/`** - Unified LLM Client SDK (OpenAI Responses API, Anthropic Messages API, Gemini API)
2. **`agent/`** - Coding Agent Loop (mammoth's agent; retained for audit/spec agents, not used by pipeline execution)
3. **`dot/`** - DOT DSL parser, serializer, AST, and validator (mammoth-owned; used by editor, validation, rendering)
4. **Pipeline execution** - Powered by `tracker` library (DAG execution, node handlers, human-in-the-loop)

Specs live in `_specs/` (cloned from upstream).

## Code Style
- Go 1.25+, modules
- Interfaces over concrete types where extensibility matters
- Errors as values, not panics
- Table-driven tests
- `context.Context` everywhere for cancellation
- Package-level doc comments on every package
- ABOUTME comments at the top of every file
- No mocks - real implementations or test doubles with real behavior

## Project Structure
```
mammoth/
  _specs/             # NLSpec source documents
  llm/                # Unified LLM Client SDK
    types.go          # Data model (Message, Request, Response, etc.)
    client.go         # Client, provider routing, middleware
    provider.go       # ProviderAdapter interface
    openai.go         # OpenAI Responses API adapter
    anthropic.go      # Anthropic Messages API adapter
    gemini.go         # Gemini API adapter
    mux_adapter.go    # Mux/LLM adapter bridging to ProviderAdapter
    errors.go         # Error hierarchy
    retry.go          # Retry logic with backoff
    catalog.go        # Model catalog
    generate.go       # High-level generate/stream API
    sse/              # SSE parser
  agent/              # Mammoth's Coding Agent Loop (used by spec agents, audit)
    session.go        # Session, config, lifecycle, loop detection
    loop.go           # Core agentic loop
    profiles.go       # ProviderProfile interface + provider profiles
    tools.go          # Tool registry
    tools_core.go     # Core tool implementations
    exec_env.go       # ExecutionEnvironment interface
    exec_local.go     # Local execution implementation
    events.go         # Event system
    steering.go       # Steering, follow-up, system prompt construction
    subagents.go      # Subagent spawning
    fidelity.go       # Fidelity modes
    patch.go          # Patch application
  dot/                # DOT DSL (parser, lexer, AST, serializer)
    ast.go            # Graph, Node, Edge types
    lexer.go          # Tokenizer
    parser.go         # DOT parser
    serializer.go     # Graph→DOT string
    validator/        # Lint rules (21 rules)
  runstate/           # Pipeline run state persistence
    store.go          # FSRunStateStore, RunState, RunEvent types
  spec/               # Spec builder (event-sourced)
    core/             # Domain model, commands, events, state
    agents/           # LLM swarm agents for spec generation
    store/            # JSONL event log, snapshots, SQLite
    export/           # DOT, YAML, Markdown export
    server/           # Standalone spec server config
    web/              # HTTP handlers and templates
  web/                # Unified web layer
    server.go         # Main HTTP server
    project.go        # Project store and lifecycle
    build_event.go    # BuildEvent type and tracker event mappers
    channel_interviewer.go  # Human gate interviewer for web/MCP
    dot_fixer.go      # LLM-powered DOT repair
    build.go          # Pipeline build orchestration (SSE)
    transitions.go    # Phase state machine
  editor/             # DOT graph editor
    session.go        # Editor sessions with undo/redo
    store.go          # Session store with TTL
    handlers.go       # HTTP handlers
  render/             # Graph rendering
    render.go         # DOT→SVG/PNG via Graphviz
    cache.go          # Render cache with TTL
  tui/                # Terminal UI (Bubble Tea)
    app.go            # Main TUI application
    bridge.go         # Tracker event bridge to Bubble Tea
    graph_panel.go    # Graph visualization
    log_panel.go      # Log output
    human_gate.go     # Interactive gate prompts
  mcp/                # MCP server for pipeline execution
    tool_run.go       # Pipeline execution via MCP
    interviewer.go    # Human gate interviewer for MCP
  cmd/
    mammoth/          # CLI entrypoint
    mammoth-mcp/      # MCP server binary
```

## Testing
- TDD: write tests first, then implementation
- Unit tests alongside source files (`*_test.go`)
- Integration tests in `*_integration_test.go` (build-tagged)
- No mocks, no mock modes

## Dependencies
- Keep dependencies minimal
- Standard library preferred
- For DOT parsing: consider a lightweight parser, or hand-roll
- For SSE: hand-roll (it's simple)
- For HTTP: `net/http` standard library
