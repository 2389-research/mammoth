# MAMMOTH

## The Team
- **SKULLCRUSHER McBYTES** - The AI agent (that's me)
- **THE NOTORIOUS H.A.R.P.** - The human boss (Doctor Biz / Harper)

## What Is This
A Go implementation of the [StrongDM Attractor](https://github.com/strongdm/attractor) specification.
Three layers, bottom-up:

1. **`llm/`** - Unified LLM Client SDK (OpenAI Responses API, Anthropic Messages API, Gemini API)
2. **`agent/`** - Coding Agent Loop (agentic loop, provider-aligned toolsets, subagents, steering)
3. **`attractor/`** - DOT-based pipeline runner (DAG execution, node handlers, human-in-the-loop)

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
  llm/                # Layer 1: Unified LLM Client
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
  agent/              # Layer 2: Coding Agent Loop
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
  attractor/          # Layer 3: Attractor Pipeline
    engine.go         # Execution engine (5-phase lifecycle)
    context.go        # Pipeline context and state
    checkpoint.go     # Checkpoint save/resume
    validate.go       # Pipeline validation
    handlers.go       # Handler registry + base types
    handlers_*.go     # Node handlers (codergen, start, exit, human, parallel, etc.)
    interviewer.go    # Human-in-the-loop (Interviewer strategies)
    stylesheet.go     # Model stylesheet
    transforms.go     # AST transforms
    conditions.go     # Condition expression language
    edge_selection.go # Edge selection algorithm
    fidelity.go       # Fidelity types and resolution
    server.go         # HTTP server mode
    subpipeline.go    # Sub-pipeline graph composition
    watchdog.go       # Stall detection
    backend.go        # CodergenBackend interface
    backend_*.go      # Backend implementations (agent, claude_code)
    compat.go         # Type aliases bridging to dot/ package
  dot/                # DOT DSL (parser, lexer, AST, serializer)
    ast.go            # Graph, Node, Edge types
    lexer.go          # Tokenizer
    parser.go         # DOT parser
    serializer.go     # Graph→DOT string
    validator/        # Lint rules (21 rules)
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
    spec_adapter.go   # Spec builder integration
    editor_adapter.go # Editor integration
    dot_fixer.go      # LLM-powered DOT repair
    build.go          # Pipeline build orchestration
    transitions.go    # Phase state machine
  editor/             # DOT graph editor
    session.go        # Editor sessions with undo/redo
    store.go          # Session store with TTL
    handlers.go       # HTTP handlers
    server.go         # Editor HTTP server
  render/             # Graph rendering
    render.go         # DOT→SVG/PNG via Graphviz
    cache.go          # Render cache with TTL
  tui/                # Terminal UI (Bubble Tea)
    app.go            # Main TUI application
    graph_panel.go    # Graph visualization
    log_panel.go      # Log output
    human_gate.go     # Interactive gate prompts
  cmd/
    mammoth/          # CLI entrypoint
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
