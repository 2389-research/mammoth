# MAKEATRON

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
- Go 1.22+, modules
- Interfaces over concrete types where extensibility matters
- Errors as values, not panics
- Table-driven tests
- `context.Context` everywhere for cancellation
- Package-level doc comments on every package
- ABOUTME comments at the top of every file
- No mocks - real implementations or test doubles with real behavior

## Project Structure
```
makeatron/
  _specs/           # NLSpec source documents
  llm/              # Layer 1: Unified LLM Client
    types.go        # Data model (Message, Request, Response, etc.)
    client.go       # Client, provider routing, middleware
    provider.go     # ProviderAdapter interface
    openai/         # OpenAI Responses API adapter
    anthropic/      # Anthropic Messages API adapter
    gemini/         # Gemini API adapter
    errors.go       # Error hierarchy
    retry.go        # Retry logic with backoff
    catalog.go      # Model catalog
    sse/            # SSE parser
    generate.go     # High-level generate/stream API
  agent/            # Layer 2: Coding Agent Loop
    session.go      # Session, config, lifecycle
    loop.go         # Core agentic loop
    profile.go      # ProviderProfile interface
    profiles/       # Provider-specific profiles
    tools/          # Tool registry, shared tools
    exec/           # ExecutionEnvironment interface + local impl
    truncate.go     # Tool output truncation
    events.go       # Event system
    steering.go     # Steering and follow-up
    detect.go       # Loop detection
    subagent.go     # Subagent spawning
    prompt.go       # System prompt construction
  attractor/        # Layer 3: Attractor Pipeline
    dot/            # DOT DSL parser
    validate.go     # Pipeline validation
    engine.go       # Execution engine (5-phase lifecycle)
    handlers/       # Node handlers (start, exit, codergen, etc.)
    state.go        # State, context, checkpoints
    human.go        # Human-in-the-loop (Interviewer)
    stylesheet.go   # Model stylesheet
    transform.go    # AST transforms
    condition.go    # Condition expression language
    server.go       # HTTP server mode
  cmd/
    makeatron/      # CLI entrypoint
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
