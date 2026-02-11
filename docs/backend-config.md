# Backend Configuration

Mammoth's LLM layer supports three providers: Anthropic, OpenAI, and Google Gemini. This document covers API key setup, provider selection, the model catalog, and retry/fallback configuration.

## Environment Variables

Set API keys as environment variables. The LLM client auto-detects configured providers.

| Variable | Provider | Example |
|----------|----------|---------|
| `ANTHROPIC_API_KEY` | Anthropic (Claude) | `sk-ant-api03-...` |
| `OPENAI_API_KEY` | OpenAI (GPT) | `sk-...` |
| `GEMINI_API_KEY` | Google Gemini | `AIza...` |

```bash
# Configure one or more providers
export ANTHROPIC_API_KEY="sk-ant-api03-..."
export OPENAI_API_KEY="sk-..."
export GEMINI_API_KEY="AIza..."
```

At least one API key must be set. The first detected provider (in order: OpenAI, Anthropic, Gemini) becomes the default provider.

## Provider Selection

Models are assigned to pipeline nodes through three mechanisms, in order of precedence:

### 1. Node Attributes (Highest Priority)

Set `llm_model` and `llm_provider` directly on a node:

```dot
implement [
    shape=box,
    prompt="Write the code",
    llm_model="claude-opus-4-6",
    llm_provider="anthropic"
]
```

### 2. Model Stylesheet

Use the CSS-like `model_stylesheet` graph attribute to assign models based on selectors. This is the recommended approach for most pipelines.

```dot
graph [
    model_stylesheet="
        * { llm_model: claude-sonnet-4-5; llm_provider: anthropic; }
        .code { llm_model: claude-opus-4-6; llm_provider: anthropic; }
        #critical_review { llm_model: gpt-5.2; llm_provider: openai; }
    "
]
```

Selectors and specificity:

| Selector | Specificity | Description |
|----------|------------|-------------|
| `*` | 0 | Matches all nodes |
| `.classname` | 1 | Matches nodes with `class="classname"` |
| `#nodeid` | 2 | Matches a single node by ID |

Higher specificity overrides lower. Explicit node attributes override all stylesheet properties. See [DSL Reference](dsl-reference.md#stylesheet-syntax) for full syntax.

### 3. Default Provider (Lowest Priority)

If no model or provider is specified on a node or via stylesheet, the default provider is used. The default provider is the first one detected from environment variables.

## Model Catalog

The built-in model catalog provides metadata about known models. Models can be referenced by their canonical ID or any alias.

### Anthropic Models

| ID | Display Name | Aliases | Context Window | Capabilities |
|----|-------------|---------|---------------|--------------|
| `claude-opus-4-6` | Claude Opus 4.6 | `opus`, `claude-opus` | 200K | Tools, Vision, Reasoning |
| `claude-sonnet-4-5` | Claude Sonnet 4.5 | `sonnet`, `claude-sonnet` | 200K | Tools, Vision, Reasoning |

### OpenAI Models

| ID | Display Name | Aliases | Context Window | Capabilities |
|----|-------------|---------|---------------|--------------|
| `gpt-5.2` | GPT-5.2 | `gpt5` | ~1M | Tools, Vision, Reasoning |
| `gpt-5.2-mini` | GPT-5.2 Mini | `gpt5-mini` | ~1M | Tools, Vision, Reasoning |
| `gpt-5.2-codex` | GPT-5.2 Codex | `codex` | ~1M | Tools, Vision, Reasoning |

### Gemini Models

| ID | Display Name | Aliases | Context Window | Capabilities |
|----|-------------|---------|---------------|--------------|
| `gemini-3-pro-preview` | Gemini 3 Pro (Preview) | `gemini-pro`, `gemini-3-pro` | ~1M | Tools, Vision, Reasoning |
| `gemini-3-flash-preview` | Gemini 3 Flash (Preview) | `gemini-flash`, `gemini-3-flash` | ~1M | Tools, Vision, Reasoning |

### Using Aliases

Aliases work anywhere a model ID is accepted:

```dot
implement [llm_model="opus", prompt="..."]
// Resolves to claude-opus-4-6

review [llm_model="codex", prompt="..."]
// Resolves to gpt-5.2-codex
```

```dot
graph [
    model_stylesheet="
        * { llm_model: sonnet; llm_provider: anthropic; }
    "
]
```

## Provider-Specific Notes

### Anthropic

- Uses the Messages API with `x-api-key` authentication (not Bearer token).
- Requires strict user/assistant message alternation. Consecutive messages with the same role are automatically merged.
- System messages are extracted and sent via the dedicated `system` parameter.

### OpenAI

- Uses the Responses API (not the legacy Chat Completions API).
- Supports Bearer token authentication.
- System and developer messages are handled natively.

### Gemini

- Uses query parameter authentication (`?key=API_KEY`).
- Does not assign tool call IDs natively; mammoth generates synthetic UUIDs.
- System messages are sent via the `systemInstruction` parameter.

## Retry Configuration

### CLI-Level Retry Policy

Set the default retry policy via the `-retry` flag:

```bash
mammoth -retry standard pipeline.dot
```

| Policy | Attempts | Initial Delay | Backoff | Jitter |
|--------|----------|---------------|---------|--------|
| `none` | 1 | 200ms | 2.0x exponential | No |
| `standard` | 5 | 200ms | 2.0x exponential | Yes |
| `aggressive` | 5 | 500ms | 2.0x exponential | Yes |
| `linear` | 3 | 500ms | 1.0x constant | No |
| `patient` | 3 | 2000ms | 3.0x exponential | Yes |

All policies cap the maximum delay at 60 seconds.

### Graph-Level Retry Default

Set a default retry count for all nodes in the graph:

```dot
graph [default_max_retry=2]
```

### Node-Level Retry Override

Override retries for a specific node:

```dot
implement [max_retries=3, prompt="..."]
```

### Retry Precedence

1. Node `max_retries` attribute
2. Graph `default_max_retry` attribute
3. CLI `-retry` policy

### Partial Success

Nodes with `allow_partial=true` return `partial_success` instead of `fail` when retries are exhausted:

```dot
implement [allow_partial=true, max_retries=2, prompt="..."]
```

## LLM Client SDK (Programmatic Usage)

For programmatic use, the LLM client SDK supports:

### Creating a Client from Environment

```go
client, err := llm.FromEnv()
if err != nil {
    log.Fatal(err) // No API keys found
}
defer client.Close()
```

### Creating a Client with Explicit Providers

```go
client := llm.NewClient(
    llm.WithProvider("anthropic", anthropicAdapter),
    llm.WithProvider("openai", openaiAdapter),
    llm.WithDefaultProvider("anthropic"),
)
```

### Middleware

The client supports middleware for logging, caching, and other cross-cutting concerns:

```go
client := llm.NewClient(
    llm.WithProvider("anthropic", adapter),
    llm.WithMiddleware(loggingMiddleware, cachingMiddleware),
)
```

Middleware executes in registration order for requests and reverse order for responses (onion pattern).

### Module-Level Default Client

For convenience, a module-level default client can be set:

```go
llm.SetDefaultClient(client)

// Later, anywhere in the codebase:
defaultClient := llm.GetDefaultClient()
```

If no default client is set, `GetDefaultClient()` attempts lazy initialization via `FromEnv()`.

## Fidelity and Token Management

Context fidelity modes control how much information is carried between pipeline stages, directly affecting token consumption:

| Mode | Token Impact | Use Case |
|------|-------------|----------|
| `full` | Highest | Short pipelines, critical context |
| `truncate` | Moderate | When only recent context matters |
| `compact` | Low (default) | Most pipelines |
| `summary:high` | Moderate | When a detailed summary suffices |
| `summary:medium` | Low-moderate | Balanced summary |
| `summary:low` | Minimal | Long pipelines, minimal context needed |

Set fidelity at the graph, node, or edge level. See [DSL Reference](dsl-reference.md#fidelity-modes) for precedence rules.

## Checkpoint Resume and Fidelity

When resuming from a checkpoint, if the previous node used `full` fidelity, the engine automatically degrades the first transition to `summary:high`. This is because in-memory LLM sessions cannot be serialized across checkpoint boundaries.

See also: [DSL Reference](dsl-reference.md) for pipeline syntax, [Handlers Reference](handlers.md) for handler details, [CLI Usage](cli-usage.md) for command-line options, [Walkthrough](walkthrough.md) for end-to-end examples.
