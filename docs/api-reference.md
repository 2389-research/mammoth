# API Reference

This document provides a comprehensive reference for mammoth's Go package APIs, HTTP endpoints, configuration types, interfaces, event systems, and error types. It is intended for developers integrating with mammoth programmatically.

For higher-level guides, see: [CLI Usage](cli-usage.md), [DSL Reference](dsl-reference.md), [Handlers Reference](handlers.md), [Backend Configuration](backend-config.md), [Walkthrough](walkthrough.md).

---

## Table of Contents

1. [Pipeline Engine API (`attractor` package)](#1-pipeline-engine-api-attractor-package)
2. [HTTP Server API (`attractor` package)](#2-http-server-api-attractor-package)
3. [LLM Client API (`llm` package)](#3-llm-client-api-llm-package)
4. [Agent Session API (`agent` package)](#4-agent-session-api-agent-package)
5. [Configuration Reference](#5-configuration-reference)
6. [Interface Reference](#6-interface-reference)
7. [Event Types Reference](#7-event-types-reference)
8. [Error Types Reference](#8-error-types-reference)
9. [Data Types Reference](#9-data-types-reference)

---

## 1. Pipeline Engine API (`attractor` package)

The `Engine` is the central orchestrator for pipeline execution. It implements a 5-phase lifecycle: PARSE, VALIDATE, INITIALIZE, EXECUTE, FINALIZE.

### Engine

```go
type Engine struct { /* unexported fields */ }
```

#### `NewEngine`

```go
func NewEngine(config EngineConfig) *Engine
```

Creates a new pipeline execution engine with the given configuration. All configuration fields are optional and have sensible defaults.

#### `Engine.Run`

```go
func (e *Engine) Run(ctx context.Context, source string) (*RunResult, error)
```

Parses DOT source text, then runs the resulting graph through the full 5-phase lifecycle. This is the primary entry point for running a pipeline from raw DOT source.

**Parameters:**
- `ctx` -- Go context for cancellation and timeout propagation.
- `source` -- DOT digraph source text.

**Returns:**
- `*RunResult` -- Final execution state including outcomes, completed nodes, and context.
- `error` -- Parse errors, validation errors, or execution errors.

#### `Engine.RunGraph`

```go
func (e *Engine) RunGraph(ctx context.Context, graph *Graph) (*RunResult, error)
```

Runs an already-parsed graph through VALIDATE, INITIALIZE, EXECUTE, and FINALIZE phases. Use this when you have pre-parsed or programmatically constructed graphs.

**Parameters:**
- `ctx` -- Go context for cancellation.
- `graph` -- A parsed `*Graph` instance.

**Returns:**
- `*RunResult` -- Final execution state.
- `error` -- Validation or execution errors.

#### `Engine.ResumeFromCheckpoint`

```go
func (e *Engine) ResumeFromCheckpoint(ctx context.Context, graph *Graph, checkpointPath string) (*RunResult, error)
```

Loads a checkpoint from disk and resumes graph execution from the node after the checkpointed node. If the previous node used `full` fidelity, the first hop after resume is degraded to `summary:high` because in-memory LLM sessions cannot be serialized across checkpoint boundaries.

**Parameters:**
- `ctx` -- Go context for cancellation.
- `graph` -- The parsed graph (must contain the checkpoint's referenced nodes).
- `checkpointPath` -- Filesystem path to the checkpoint JSON file.

**Returns:**
- `*RunResult` -- Final execution state.
- `error` -- Checkpoint load errors, missing nodes, or execution errors.

### RunResult

```go
type RunResult struct {
    FinalOutcome   *Outcome
    CompletedNodes []string
    NodeOutcomes   map[string]*Outcome
    Context        *Context
}
```

Holds the final state of a completed pipeline execution.

| Field | Type | Description |
|-------|------|-------------|
| `FinalOutcome` | `*Outcome` | The outcome of the last executed node. |
| `CompletedNodes` | `[]string` | Ordered list of node IDs that completed execution. |
| `NodeOutcomes` | `map[string]*Outcome` | Map of node ID to its execution outcome. |
| `Context` | `*Context` | The pipeline context snapshot at completion. |

### Context

```go
type Context struct { /* unexported fields */ }
```

Thread-safe key-value store shared across pipeline stages.

| Method | Signature | Description |
|--------|-----------|-------------|
| `NewContext` | `func NewContext() *Context` | Creates a new empty context. |
| `Set` | `func (c *Context) Set(key string, value any)` | Stores a value. |
| `Get` | `func (c *Context) Get(key string) any` | Retrieves a value (nil if missing). |
| `GetString` | `func (c *Context) GetString(key, defaultVal string) string` | Retrieves a string value with default. |
| `AppendLog` | `func (c *Context) AppendLog(entry string)` | Adds a log entry. |
| `Snapshot` | `func (c *Context) Snapshot() map[string]any` | Returns a shallow copy of all key-value pairs. |
| `Clone` | `func (c *Context) Clone() *Context` | Creates a deep copy with independent values and logs. |
| `ApplyUpdates` | `func (c *Context) ApplyUpdates(updates map[string]any)` | Merges key-value pairs into the context. |
| `Logs` | `func (c *Context) Logs() []string` | Returns a copy of log entries. |

### Outcome

```go
type Outcome struct {
    Status           StageStatus
    PreferredLabel   string
    SuggestedNextIDs []string
    ContextUpdates   map[string]any
    Notes            string
    FailureReason    string
}
```

The result of executing a node handler.

| Field | Type | Description |
|-------|------|-------------|
| `Status` | `StageStatus` | Execution result: `success`, `fail`, `partial_success`, `retry`, `skipped`. |
| `PreferredLabel` | `string` | Preferred outgoing edge label for routing. |
| `SuggestedNextIDs` | `[]string` | Suggested target node IDs for routing. |
| `ContextUpdates` | `map[string]any` | Key-value pairs to merge into the pipeline context. |
| `Notes` | `string` | Human-readable notes about the execution (e.g., stdout from tool nodes). |
| `FailureReason` | `string` | Explanation of why the node failed (when Status is `fail`). |

### StageStatus Constants

```go
const (
    StatusSuccess        StageStatus = "success"
    StatusFail           StageStatus = "fail"
    StatusPartialSuccess StageStatus = "partial_success"
    StatusRetry          StageStatus = "retry"
    StatusSkipped        StageStatus = "skipped"
)
```

### Validation

```go
func Validate(g *Graph, extraRules ...LintRule) []Diagnostic
func ValidateOrError(g *Graph, extraRules ...LintRule) ([]Diagnostic, error)
```

`Validate` runs all built-in lint rules plus any extra rules and returns diagnostics. `ValidateOrError` additionally returns an error if any ERROR-severity diagnostics exist.

See [DSL Reference - Pipeline Validation](dsl-reference.md#pipeline-validation) for the list of built-in validation rules.

### Diagnostic

```go
type Diagnostic struct {
    Rule     string
    Severity Severity
    Message  string
    NodeID   string      // optional
    Edge     *[2]string  // optional (from, to)
    Fix      string      // optional suggested fix
}
```

### Severity Constants

```go
const (
    SeverityError   Severity = iota  // "ERROR"
    SeverityWarning                  // "WARNING"
    SeverityInfo                     // "INFO"
)
```

### Fidelity

```go
type FidelityMode string

const (
    FidelityFull          FidelityMode = "full"
    FidelityTruncate      FidelityMode = "truncate"
    FidelityCompact       FidelityMode = "compact"
    FidelitySummaryLow    FidelityMode = "summary:low"
    FidelitySummaryMedium FidelityMode = "summary:medium"
    FidelitySummaryHigh   FidelityMode = "summary:high"
)
```

```go
func ResolveFidelity(edge *Edge, targetNode *Node, graph *Graph) FidelityMode
func IsValidFidelity(mode string) bool
func ValidFidelityModes() []string
```

`ResolveFidelity` applies the fidelity precedence chain: edge attribute > node attribute > graph `default_fidelity` > hardcoded default (`compact`). See [DSL Reference - Fidelity Modes](dsl-reference.md#fidelity-modes) for details.

### Transforms

```go
type Transform interface {
    Apply(g *Graph) *Graph
}

func ApplyTransforms(g *Graph, transforms ...Transform) *Graph
func DefaultTransforms() []Transform
```

`DefaultTransforms` returns the standard ordered transform chain:

1. `SubPipelineTransform` -- Inlines sub-pipeline DOT files referenced by node `sub_pipeline` attributes.
2. `VariableExpansionTransform` -- Expands `$variable` references in node attributes using graph-level attributes.
3. `StylesheetApplicationTransform` -- Applies the `model_stylesheet` CSS-like rules to nodes.

### Handler Registry

```go
type HandlerRegistry struct { /* unexported fields */ }

func NewHandlerRegistry() *HandlerRegistry
func DefaultHandlerRegistry() *HandlerRegistry

func (r *HandlerRegistry) Register(handler NodeHandler)
func (r *HandlerRegistry) Get(typeName string) NodeHandler
func (r *HandlerRegistry) Resolve(node *Node) NodeHandler
```

`DefaultHandlerRegistry` creates a registry pre-loaded with all 9 built-in handlers. `Resolve` finds the handler for a node using: explicit `type` attribute > shape-based mapping > default to `codergen`. See [Handlers Reference](handlers.md) for handler details.

### Retry Policy

```go
type RetryPolicy struct {
    MaxAttempts int
    Backoff     BackoffConfig
    ShouldRetry func(error) bool
}

type BackoffConfig struct {
    InitialDelay time.Duration
    Factor       float64
    MaxDelay     time.Duration
    Jitter       bool
}
```

### Restart Configuration

```go
type RestartConfig struct {
    MaxRestarts int  // maximum loop_restart edges before giving up (default 5)
}

func DefaultRestartConfig() *RestartConfig
```

### ArtifactStore

```go
type ArtifactStore struct { /* unexported fields */ }

func NewArtifactStore(dir string) *ArtifactStore
func (s *ArtifactStore) Store(id, contentType string, data []byte) (string, error)
```

Stores large outputs (e.g., tool stdout exceeding 10KB) as files. Returns the artifact file path.

---

## 2. HTTP Server API (`attractor` package)

The `PipelineServer` provides a REST API for managing pipeline execution over HTTP. For endpoint usage examples, see [CLI Usage - Server Mode](cli-usage.md#server-mode).

### PipelineServer

```go
type PipelineServer struct {
    ToDOT           GraphDOTFunc
    ToDOTWithStatus GraphDOTWithStatusFunc
    RenderDOTSource DOTRenderFunc
    // unexported fields
}

func NewPipelineServer(engine *Engine) *PipelineServer
func (s *PipelineServer) ServeHTTP(w http.ResponseWriter, r *http.Request)
func (s *PipelineServer) Handler() http.Handler
func (s *PipelineServer) SetEventQuery(eq EventQuery)
```

**Usage:**

```go
engine := attractor.NewEngine(attractor.EngineConfig{})
server := attractor.NewPipelineServer(engine)

// Optional: configure event query backend for /events/query, /events/tail, /events/summary
server.SetEventQuery(eventQueryImpl)

// Optional: configure graph rendering
server.ToDOT = myGraphToDOTFunc
server.RenderDOTSource = myDOTRenderer

http.ListenAndServe(":2389", server.Handler())
```

### Endpoints

All responses use `Content-Type: application/json` unless otherwise noted.

#### `POST /pipelines`

Submit a pipeline for asynchronous execution.

**Request body:** DOT source as plain text or JSON `{"source": "digraph {...}"}`.

**Response (202 Accepted):**

```json
{"id": "<hex-uuid>", "status": "running"}
```

**Errors:**
- `400` -- Empty source or invalid JSON.

#### `GET /pipelines/{id}`

Get pipeline status.

**Response (200 OK):**

```json
{
  "id": "<hex-uuid>",
  "status": "running|completed|failed|cancelled",
  "completed_nodes": ["start", "step1"],
  "error": "",
  "created_at": "2026-02-07T12:00:00Z"
}
```

**Errors:**
- `404` -- Pipeline not found.

#### `GET /pipelines/{id}/events`

Server-Sent Events (SSE) stream of engine lifecycle events. The connection stays open until the pipeline completes, fails, or is cancelled.

**Content-Type:** `text/event-stream`

**Event format:**

```
data: {"type":"stage.started","node_id":"plan","data":null}
```

**Final event:**

```
data: {"status":"completed"}
```

#### `GET /pipelines/{id}/events/query`

Query events with filtering and pagination. Requires an `EventQuery` backend (returns `503` if not configured).

**Query parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `type` | string | Filter by event type (e.g., `stage.completed`). |
| `node` | string | Filter by node ID. |
| `since` | RFC3339 | Events at or after this time. |
| `until` | RFC3339 | Events at or before this time. |
| `limit` | int | Maximum results. |
| `offset` | int | Skip first N results. |

**Response (200 OK):**

```json
{
  "events": [{"type": "...", "node_id": "...", "data": {}, "Timestamp": "..."}],
  "total": 42
}
```

#### `GET /pipelines/{id}/events/tail`

Get the last N events. Requires `EventQuery` backend.

**Query parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `n` | int | 10 | Number of events to return. |

**Response (200 OK):**

```json
{"events": [...]}
```

#### `GET /pipelines/{id}/events/summary`

Aggregate event statistics. Requires `EventQuery` backend.

**Response (200 OK):**

```json
{
  "total_events": 15,
  "by_type": {"stage.started": 5, "stage.completed": 5},
  "by_node": {"plan": 3, "implement": 2},
  "first_event": "2026-02-07T12:00:00.000000000Z",
  "last_event": "2026-02-07T12:01:30.000000000Z"
}
```

#### `POST /pipelines/{id}/cancel`

Cancel a running pipeline.

**Response (200 OK):**

```json
{"status": "cancelled"}
```

#### `GET /pipelines/{id}/questions`

List pending human-in-the-loop questions (unanswered only).

**Response (200 OK):**

```json
[
  {
    "id": "<question-uuid>",
    "question": "Review the changes",
    "options": ["[A] Approve", "[R] Revise"],
    "answered": false
  }
]
```

#### `POST /pipelines/{id}/questions/{qid}/answer`

Submit an answer to a pending question.

**Request body:**

```json
{"answer": "[A] Approve"}
```

**Response (200 OK):**

```json
{"status": "answered"}
```

**Errors:**
- `404` -- Question not found.

#### `GET /pipelines/{id}/context`

Get the pipeline context snapshot (key-value state).

**Response (200 OK):**

```json
{"key1": "value1", "_started_at": "2026-02-07T12:00:00Z"}
```

Returns `{}` if context is not yet available.

#### `GET /pipelines/{id}/graph`

Get the pipeline graph rendered in the requested format.

**Query parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `format` | string | `svg` | Output format: `dot`, `svg`, or `png`. |

**Content types by format:**
- `dot` -- `text/vnd.graphviz`
- `svg` -- `image/svg+xml` (falls back to `text/vnd.graphviz` if no renderer)
- `png` -- `image/png` (returns `503` if no renderer)

If the pipeline has execution results and `ToDOTWithStatus` is configured, the graph includes status color overlays.

### Response Types

```go
type PipelineStatus struct {
    ID             string    `json:"id"`
    Status         string    `json:"status"`
    CompletedNodes []string  `json:"completed_nodes,omitempty"`
    Error          string    `json:"error,omitempty"`
    CreatedAt      time.Time `json:"created_at"`
}

type EventQueryResponse struct {
    Events []EngineEvent `json:"events"`
    Total  int           `json:"total"`
}

type EventTailResponse struct {
    Events []EngineEvent `json:"events"`
}

type EventSummaryResponse struct {
    TotalEvents int            `json:"total_events"`
    ByType      map[string]int `json:"by_type"`
    ByNode      map[string]int `json:"by_node"`
    FirstEvent  string         `json:"first_event,omitempty"`
    LastEvent   string         `json:"last_event,omitempty"`
}

type PendingQuestion struct {
    ID       string   `json:"id"`
    Question string   `json:"question"`
    Options  []string `json:"options"`
    Answered bool     `json:"answered"`
    Answer   string   `json:"answer,omitempty"`
}
```

---

## 3. LLM Client API (`llm` package)

The `llm` package provides a unified client SDK for calling LLM providers (OpenAI, Anthropic, Gemini). For configuration details, see [Backend Configuration](backend-config.md).

### Client

```go
type Client struct { /* unexported fields */ }
```

#### Constructors

```go
func NewClient(opts ...ClientOption) *Client
func FromEnv() (*Client, error)
```

`NewClient` creates a client with explicit configuration via functional options. `FromEnv` creates a client by detecting API keys in the environment (`OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `GEMINI_API_KEY`). The first detected provider becomes the default.

#### ClientOption Functions

```go
func WithProvider(name string, adapter ProviderAdapter) ClientOption
func WithDefaultProvider(name string) ClientOption
func WithMiddleware(mw ...Middleware) ClientOption
```

| Option | Description |
|--------|-------------|
| `WithProvider` | Registers a provider adapter. First registered becomes default if no default set. |
| `WithDefaultProvider` | Sets the default provider name. |
| `WithMiddleware` | Appends middleware (onion pattern: first registered = outermost). |

#### Methods

```go
func (c *Client) Complete(ctx context.Context, req Request) (*Response, error)
func (c *Client) Stream(ctx context.Context, req Request) (<-chan StreamEvent, error)
func (c *Client) Close() error
func (c *Client) RegisterProvider(name string, adapter ProviderAdapter)
```

| Method | Description |
|--------|-------------|
| `Complete` | Sends a completion request through middleware chain to the provider. |
| `Stream` | Sends a streaming request to the provider. Returns a channel of `StreamEvent`. |
| `Close` | Shuts down all registered provider adapters. |
| `RegisterProvider` | Adds or replaces a provider adapter at runtime. |

#### Module-Level Default Client

```go
func SetDefaultClient(c *Client)
func GetDefaultClient() *Client
```

`GetDefaultClient` returns the module-level default client. If none is set, it attempts lazy initialization via `FromEnv()`. Returns nil if `FromEnv` fails.

### Request

```go
type Request struct {
    Model           string            `json:"model"`
    Messages        []Message         `json:"messages"`
    Provider        string            `json:"provider,omitempty"`
    Tools           []ToolDefinition  `json:"tools,omitempty"`
    ToolChoice      *ToolChoice       `json:"tool_choice,omitempty"`
    ResponseFormat  *ResponseFormat   `json:"response_format,omitempty"`
    Temperature     *float64          `json:"temperature,omitempty"`
    TopP            *float64          `json:"top_p,omitempty"`
    MaxTokens       *int              `json:"max_tokens,omitempty"`
    StopSequences   []string          `json:"stop_sequences,omitempty"`
    ReasoningEffort string            `json:"reasoning_effort,omitempty"`
    Metadata        map[string]string `json:"metadata,omitempty"`
    ProviderOptions map[string]any    `json:"provider_options,omitempty"`
}
```

| Field | Description |
|-------|-------------|
| `Model` | Model identifier (e.g., `claude-opus-4-6`, `gpt-5.2`). |
| `Messages` | Conversation history as a slice of `Message`. |
| `Provider` | Target provider name. Empty uses the client's default provider. |
| `Tools` | Tool definitions available to the model. |
| `ToolChoice` | Controls tool usage: `auto`, `none`, `required`, `named`. |
| `ResponseFormat` | Desired output format: `text`, `json`, or `json_schema`. |
| `Temperature` | Sampling temperature. |
| `TopP` | Nucleus sampling parameter. |
| `MaxTokens` | Maximum tokens to generate. |
| `StopSequences` | Sequences that stop generation. |
| `ReasoningEffort` | Reasoning effort level: `none`, `low`, `medium`, `high`. |
| `Metadata` | Arbitrary key-value metadata. |
| `ProviderOptions` | Provider-specific options passed through to the adapter. |

### Response

```go
type Response struct {
    ID           string          `json:"id"`
    Model        string          `json:"model"`
    Provider     string          `json:"provider"`
    Message      Message         `json:"message"`
    FinishReason FinishReason    `json:"finish_reason"`
    Usage        Usage           `json:"usage"`
    Raw          json.RawMessage `json:"raw,omitempty"`
    Warnings     []Warning       `json:"warnings,omitempty"`
    RateLimit    *RateLimitInfo  `json:"rate_limit,omitempty"`
}
```

**Convenience methods:**

```go
func (r *Response) TextContent() string    // Concatenated text from response message.
func (r *Response) ToolCalls() []ToolCallData  // Tool calls from response message.
func (r *Response) Reasoning() string      // Concatenated reasoning text.
```

### Message

```go
type Message struct {
    Role       Role          `json:"role"`
    Content    []ContentPart `json:"content"`
    Name       string        `json:"name,omitempty"`
    ToolCallID string        `json:"tool_call_id,omitempty"`
}
```

**Roles:** `system`, `user`, `assistant`, `tool`, `developer`.

**Convenience constructors:**

```go
func SystemMessage(text string) Message
func UserMessage(text string) Message
func UserMessageWithParts(parts ...ContentPart) Message
func AssistantMessage(text string) Message
func ToolResultMessage(toolCallID, content string, isError bool) Message
func DeveloperMessage(text string) Message
```

### ContentPart

```go
type ContentPart struct {
    Kind       ContentKind    `json:"kind"`
    Text       string         `json:"text,omitempty"`
    Image      *ImageData     `json:"image,omitempty"`
    Audio      *AudioData     `json:"audio,omitempty"`
    Document   *DocumentData  `json:"document,omitempty"`
    ToolCall   *ToolCallData  `json:"tool_call,omitempty"`
    ToolResult *ToolResultData `json:"tool_result,omitempty"`
    Thinking   *ThinkingData  `json:"thinking,omitempty"`
}
```

**Content kinds:** `text`, `image`, `audio`, `document`, `tool_call`, `tool_result`, `thinking`, `redacted_thinking`.

**Convenience constructors:**

```go
func TextPart(text string) ContentPart
func ImageURLPart(url string) ContentPart
func ImageDataPart(data []byte, mediaType string) ContentPart
func ToolCallPart(id, name string, args json.RawMessage) ContentPart
func ToolResultPart(toolCallID, content string, isError bool) ContentPart
func ThinkingPart(text, signature string) ContentPart
func RedactedThinkingPart(text, signature string) ContentPart
```

### Usage

```go
type Usage struct {
    InputTokens      int              `json:"input_tokens"`
    OutputTokens     int              `json:"output_tokens"`
    TotalTokens      int              `json:"total_tokens"`
    ReasoningTokens  *int             `json:"reasoning_tokens,omitempty"`
    CacheReadTokens  *int             `json:"cache_read_tokens,omitempty"`
    CacheWriteTokens *int             `json:"cache_write_tokens,omitempty"`
    Raw              *json.RawMessage `json:"raw,omitempty"`
}

func (u Usage) Add(other Usage) Usage
```

### ToolDefinition and ToolChoice

```go
type ToolDefinition struct {
    Name        string          `json:"name"`
    Description string          `json:"description"`
    Parameters  json.RawMessage `json:"parameters"`  // JSON Schema
}

type ToolChoice struct {
    Mode     string `json:"mode"`                // "auto", "none", "required", "named"
    ToolName string `json:"tool_name,omitempty"`  // required when mode is "named"
}
```

### Middleware

```go
type Middleware func(ctx context.Context, req Request, next NextFunc) (*Response, error)
type NextFunc func(ctx context.Context, req Request) (*Response, error)
```

Middleware executes in registration order for requests and reverse order for responses (onion/chain-of-responsibility pattern).

### Timeout Configuration

```go
type AdapterTimeout struct {
    Connect    time.Duration  // default 10s
    Request    time.Duration  // default 120s
    StreamRead time.Duration  // default 30s
}

func DefaultAdapterTimeout() AdapterTimeout
```

---

## 4. Agent Session API (`agent` package)

The `agent` package provides a coding agent loop with session management, steering, and loop detection.

### Session

```go
type Session struct {
    ID           string
    Config       SessionConfig
    History      []Turn
    State        SessionState
    EventEmitter *EventEmitter
    // unexported fields
}

func NewSession(config SessionConfig) *Session
```

| Method | Signature | Description |
|--------|-----------|-------------|
| `Emit` | `func (s *Session) Emit(kind EventKind, data map[string]any)` | Emits a session event. |
| `SetState` | `func (s *Session) SetState(state SessionState)` | Transitions the session to a new state. |
| `Steer` | `func (s *Session) Steer(message string)` | Queues a steering message for injection. |
| `FollowUp` | `func (s *Session) FollowUp(message string)` | Queues a follow-up message. |
| `DrainSteering` | `func (s *Session) DrainSteering() []string` | Returns and clears all pending steering messages. |
| `DrainFollowup` | `func (s *Session) DrainFollowup() string` | Returns and clears the next follow-up message. |
| `AppendTurn` | `func (s *Session) AppendTurn(turn Turn)` | Adds a turn to session history. |
| `TurnCount` | `func (s *Session) TurnCount() int` | Returns the number of turns. |
| `Close` | `func (s *Session) Close()` | Transitions to `StateClosed` and closes the event emitter. |

### SessionState Constants

```go
const (
    StateIdle          SessionState = "idle"
    StateProcessing    SessionState = "processing"
    StateAwaitingInput SessionState = "awaiting_input"
    StateClosed        SessionState = "closed"
)
```

### Turn Types

The `Turn` interface is implemented by all conversation turn types:

```go
type Turn interface {
    TurnType() string
    TurnTimestamp() time.Time
}
```

| Type | TurnType() | Description |
|------|-----------|-------------|
| `UserTurn` | `"user"` | User-submitted message. Fields: `Content`, `Timestamp`. |
| `AssistantTurn` | `"assistant"` | Model response. Fields: `Content`, `ToolCalls`, `Reasoning`, `Usage`, `ResponseID`, `Timestamp`. |
| `ToolResultsTurn` | `"tool_results"` | Tool execution results. Fields: `Results` (`[]llm.ToolResult`), `Timestamp`. |
| `SystemTurn` | `"system"` | System-level message. Fields: `Content`, `Timestamp`. |
| `SteeringTurn` | `"steering"` | Injected steering message. Fields: `Content`, `Timestamp`. |

### History Conversion

```go
func ConvertHistoryToMessages(history []Turn) []llm.Message
```

Converts a slice of `Turn` values into LLM messages suitable for sending to a language model. Steering turns are converted to user-role messages.

### Loop Detection

```go
func DetectLoop(history []Turn, windowSize int) bool
func ExtractToolCallSignatures(history []Turn, count int) []string
```

`DetectLoop` checks whether the recent tool call history contains a repeating pattern of length 1, 2, or 3. Tool call signatures are `"name:sha256(arguments)"`.

### EventEmitter

```go
type EventEmitter struct { /* unexported fields */ }

func NewEventEmitter() *EventEmitter
func (e *EventEmitter) Subscribe() <-chan SessionEvent
func (e *EventEmitter) Unsubscribe(ch <-chan SessionEvent)
func (e *EventEmitter) Emit(event SessionEvent)
func (e *EventEmitter) Close()
```

Non-blocking event delivery via buffered channels (buffer size 64). Events are dropped for slow subscribers rather than blocking.

---

## 5. Configuration Reference

### EngineConfig

```go
type EngineConfig struct {
    CheckpointDir      string
    AutoCheckpointPath string
    ArtifactDir        string
    ArtifactsBaseDir   string
    RunID              string
    Transforms         []Transform
    ExtraLintRules     []LintRule
    DefaultRetry       RetryPolicy
    Handlers           *HandlerRegistry
    EventHandler       func(EngineEvent)
    Backend            CodergenBackend
    BaseURL            string
    RestartConfig      *RestartConfig
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `CheckpointDir` | `string` | `""` (disabled) | Directory for checkpoint files. Empty disables checkpointing. |
| `AutoCheckpointPath` | `string` | `""` (disabled) | Path to overwrite with latest checkpoint after each node. Empty disables. |
| `ArtifactDir` | `string` | `""` (temp dir) | Directory for artifact storage. Empty uses `ArtifactsBaseDir/<RunID>`. |
| `ArtifactsBaseDir` | `string` | `"./artifacts"` | Base directory for run artifact subdirectories. |
| `RunID` | `string` | auto-generated | Run identifier for the artifact subdirectory. Empty auto-generates a UUID. |
| `Transforms` | `[]Transform` | `DefaultTransforms()` | AST transforms applied between parsing and validation. |
| `ExtraLintRules` | `[]LintRule` | `nil` | Additional validation rules appended to built-in rules. |
| `DefaultRetry` | `RetryPolicy` | `{MaxAttempts: 1}` | Default retry policy for nodes without per-node overrides. |
| `Handlers` | `*HandlerRegistry` | `DefaultHandlerRegistry()` | Handler registry. Nil uses the default with all 9 built-in handlers. |
| `EventHandler` | `func(EngineEvent)` | `nil` | Callback for engine lifecycle events. |
| `Backend` | `CodergenBackend` | `nil` | Backend for codergen nodes. Nil means codergen nodes return `StatusFail`. |
| `BaseURL` | `string` | `""` | Default API base URL for codergen nodes (overridable per-node). |
| `RestartConfig` | `*RestartConfig` | `DefaultRestartConfig()` | Loop restart configuration. Default allows 5 restarts. |

### SessionConfig

```go
type SessionConfig struct {
    MaxTurns                int            `json:"max_turns"`
    MaxToolRoundsPerInput   int            `json:"max_tool_rounds_per_input"`
    DefaultCommandTimeoutMs int            `json:"default_command_timeout_ms"`
    MaxCommandTimeoutMs     int            `json:"max_command_timeout_ms"`
    ReasoningEffort         string         `json:"reasoning_effort,omitempty"`
    ToolOutputLimits        map[string]int `json:"tool_output_limits,omitempty"`
    EnableLoopDetection     bool           `json:"enable_loop_detection"`
    LoopDetectionWindow     int            `json:"loop_detection_window"`
    MaxSubagentDepth        int            `json:"max_subagent_depth"`
    FidelityMode            string         `json:"fidelity_mode,omitempty"`
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `MaxTurns` | `int` | `0` (unlimited) | Maximum conversation turns. 0 means unlimited. |
| `MaxToolRoundsPerInput` | `int` | `200` | Maximum tool call rounds per user input. |
| `DefaultCommandTimeoutMs` | `int` | `10000` | Default timeout for shell commands (ms). |
| `MaxCommandTimeoutMs` | `int` | `600000` | Maximum allowed command timeout (ms). |
| `ReasoningEffort` | `string` | `""` | Reasoning effort level for LLM calls. |
| `ToolOutputLimits` | `map[string]int` | `{}` | Per-tool output size limits. |
| `EnableLoopDetection` | `bool` | `true` | Whether to detect repeating tool call patterns. |
| `LoopDetectionWindow` | `int` | `10` | Number of recent tool calls to analyze for loops. |
| `MaxSubagentDepth` | `int` | `1` | Maximum nesting depth for subagent spawning. |
| `FidelityMode` | `string` | `""` (full) | Context fidelity mode for conversation history. |

`DefaultSessionConfig()` returns the above defaults.

### RetentionConfig

```go
type RetentionConfig struct {
    MaxAge  time.Duration  // prune runs older than this; 0 = no limit
    MaxRuns int            // keep at most this many runs; 0 = unlimited
}

func (rc RetentionConfig) PruneLoop(ctx context.Context, sink LogSink, interval time.Duration)
func (rc RetentionConfig) PruneByMaxRuns(sink LogSink) (int, error)
```

`PruneLoop` runs periodic retention cleanup in a blocking loop until context cancellation. `PruneByMaxRuns` removes the oldest runs exceeding `MaxRuns`.

---

## 6. Interface Reference

### NodeHandler

```go
type NodeHandler interface {
    Type() string
    Execute(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error)
}
```

All 9 built-in handlers implement this interface. See [Handlers Reference](handlers.md) for details on each handler. Register custom handlers via `HandlerRegistry.Register()`.

### ProviderAdapter

```go
type ProviderAdapter interface {
    Name() string
    Complete(ctx context.Context, req Request) (*Response, error)
    Stream(ctx context.Context, req Request) (<-chan StreamEvent, error)
    Close() error
}
```

All LLM provider adapters (OpenAI, Anthropic, Gemini) implement this interface. See [Backend Configuration](backend-config.md) for provider-specific notes.

**Optional interfaces adapters may implement:**

```go
type Initializer interface {
    Initialize() error
}

type ToolChoiceChecker interface {
    SupportsToolChoice(mode string) bool
}
```

### CodergenBackend

```go
type CodergenBackend interface {
    RunAgent(ctx context.Context, config AgentRunConfig) (*AgentRunResult, error)
}
```

Abstracts the LLM agent execution so that `CodergenHandler` does not depend directly on the `agent` or `llm` packages. Set on `EngineConfig.Backend`.

```go
type AgentRunConfig struct {
    Prompt       string            // instructions for the LLM
    Model        string            // model ID (e.g., "claude-sonnet-4-5")
    Provider     string            // provider name ("anthropic", "openai", "gemini")
    BaseURL      string            // custom API base URL (overrides provider default)
    WorkDir      string            // working directory
    Goal         string            // pipeline-level goal
    NodeID       string            // pipeline node identifier
    MaxTurns     int               // max agent loop turns (0 = default 20)
    FidelityMode string            // fidelity mode ("full", "compact", "truncate", "summary:*")
    EventHandler func(EngineEvent) // engine event callback for agent-level observability
}

type AgentRunResult struct {
    Output      string          // final text output
    ToolCalls   int             // total tool calls made
    TokensUsed  int             // total tokens consumed
    Success     bool            // whether the agent completed without errors
    ToolCallLog []ToolCallEntry // individual tool call details
    TurnCount   int             // LLM call rounds
    Usage       TokenUsage      // granular per-category token breakdown
}
```

### ManagerBackend

```go
type ManagerBackend interface {
    Observe(ctx context.Context, nodeID string, iteration int, pctx *Context) (string, error)
    Guard(ctx context.Context, nodeID string, iteration int, observation string, guardCondition string, pctx *Context) (bool, error)
    Steer(ctx context.Context, nodeID string, iteration int, steerPrompt string, pctx *Context) (string, error)
}
```

LLM-powered supervision for the `ManagerLoopHandler`. Implements the observe/guard/steer pattern. Set on `ManagerLoopHandler.Backend`.

### Interviewer

```go
type Interviewer interface {
    Ask(ctx context.Context, question string, options []string) (string, error)
}
```

Abstraction for human-in-the-loop interaction. Any frontend (CLI, web, Slack, programmatic) implements this interface.

**Built-in implementations:**

| Implementation | Constructor | Description |
|----------------|-------------|-------------|
| `AutoApproveInterviewer` | `NewAutoApproveInterviewer(defaultAnswer string)` | Always returns a configured answer or the first option. |
| `CallbackInterviewer` | `NewCallbackInterviewer(fn func(...) (string, error))` | Delegates to a Go callback function. |
| `QueueInterviewer` | `NewQueueInterviewer(answers ...string)` | Reads answers from a pre-filled FIFO queue. |
| `RecordingInterviewer` | `NewRecordingInterviewer(inner Interviewer)` | Wraps another interviewer and records all Q&A pairs. |
| `ConsoleInterviewer` | `NewConsoleInterviewer()` | Reads from stdin, writes prompts to stdout. |
| `ConsoleInterviewerWithIO` | `NewConsoleInterviewerWithIO(r io.Reader, w io.Writer)` | Configurable I/O for testing. |

See [Handlers Reference - WaitForHumanHandler](handlers.md#waitforhumanhandler-human-gate) for how interviewers integrate with the pipeline.

### LintRule

```go
type LintRule interface {
    Name() string
    Apply(g *Graph) []Diagnostic
}
```

Custom validation rules implement this interface and are passed via `EngineConfig.ExtraLintRules` or directly to `Validate()`/`ValidateOrError()`.

### Transform

```go
type Transform interface {
    Apply(g *Graph) *Graph
}
```

Custom AST transforms implement this interface and are passed via `EngineConfig.Transforms`.

### LogSink

```go
type LogSink interface {
    Append(runID string, event EngineEvent) error
    Query(runID string, filter EventFilter) ([]EngineEvent, int, error)
    Tail(runID string, n int) ([]EngineEvent, error)
    Summarize(runID string) (*EventSummary, error)
    Prune(olderThan time.Duration) (int, error)
    Close() error
}
```

Structured event log storage with query, retention, and lifecycle management. The built-in implementation is `FSLogSink` (filesystem-backed).

```go
func NewFSLogSink(baseDir string) (*FSLogSink, error)
```

`FSLogSink` also provides:

```go
func (s *FSLogSink) ListRuns() ([]RunIndexEntry, error)
```

### EventQuery

```go
type EventQuery interface {
    QueryEvents(runID string, filter EventFilter) ([]EngineEvent, error)
    CountEvents(runID string, filter EventFilter) (int, error)
    TailEvents(runID string, n int) ([]EngineEvent, error)
    SummarizeEvents(runID string) (*EventSummary, error)
}
```

Query API for the append-only event log. The built-in implementation is `FSEventQuery`.

```go
func NewFSEventQuery(store *FSRunStateStore) *FSEventQuery
```

### EventFilter

```go
type EventFilter struct {
    Types  []EngineEventType  // filter by type(s); empty = all
    NodeID string             // filter by node; empty = all
    Since  *time.Time         // lower time bound; nil = unbounded
    Until  *time.Time         // upper time bound; nil = unbounded
    Limit  int                // max results; 0 = unlimited
    Offset int                // skip first N results
}
```

### GraphDOTFunc, GraphDOTWithStatusFunc, DOTRenderFunc

```go
type GraphDOTFunc func(g *Graph) string
type GraphDOTWithStatusFunc func(g *Graph, outcomes map[string]*Outcome) string
type DOTRenderFunc func(ctx context.Context, dotText string, format string) ([]byte, error)
```

Optional callbacks for `PipelineServer` graph rendering. Set on `PipelineServer.ToDOT`, `PipelineServer.ToDOTWithStatus`, and `PipelineServer.RenderDOTSource` fields.

---

## 7. Event Types Reference

### Engine Events (`attractor` package)

```go
type EngineEventType string
```

| Constant | Value | Description |
|----------|-------|-------------|
| `EventPipelineStarted` | `"pipeline.started"` | Pipeline execution has begun. |
| `EventPipelineCompleted` | `"pipeline.completed"` | Pipeline execution completed successfully. |
| `EventPipelineFailed` | `"pipeline.failed"` | Pipeline execution failed. `Data["error"]` contains the error message. |
| `EventStageStarted` | `"stage.started"` | A node handler is about to execute. `NodeID` is set. |
| `EventStageCompleted` | `"stage.completed"` | A node handler completed successfully. `NodeID` is set. |
| `EventStageFailed` | `"stage.failed"` | A node handler failed. `NodeID` is set. |
| `EventStageRetrying` | `"stage.retrying"` | A node is being retried. `Data["attempt"]` contains the attempt number. |
| `EventStageStalled` | `"stage.stalled"` | A node has stalled (e.g., waiting for dependencies that cannot proceed). |
| `EventCheckpointSaved` | `"checkpoint.saved"` | A checkpoint was saved after node completion. `NodeID` is set. |

```go
type EngineEvent struct {
    Type      EngineEventType
    NodeID    string
    Data      map[string]any
    Timestamp time.Time
}
```

### Session Events (`agent` package)

```go
type EventKind string
```

| Constant | Value | Description |
|----------|-------|-------------|
| `EventSessionStart` | `"session_start"` | Session has been created and started. |
| `EventSessionEnd` | `"session_end"` | Session has ended. |
| `EventUserInput` | `"user_input"` | User submitted input. |
| `EventAssistantTextStart` | `"assistant_text_start"` | Model began generating text. |
| `EventAssistantTextDelta` | `"assistant_text_delta"` | Incremental text from the model. |
| `EventAssistantTextEnd` | `"assistant_text_end"` | Model finished generating text. |
| `EventToolCallStart` | `"tool_call_start"` | A tool call is starting. |
| `EventToolCallOutputDelta` | `"tool_call_output_delta"` | Incremental tool output. |
| `EventToolCallEnd` | `"tool_call_end"` | A tool call has finished. |
| `EventSteeringInjected` | `"steering_injected"` | A steering message was injected. |
| `EventTurnLimit` | `"turn_limit"` | Maximum turn limit reached. |
| `EventLoopDetection` | `"loop_detection"` | A repeating tool call pattern was detected. |
| `EventError` | `"error"` | An error occurred during processing. |

```go
type SessionEvent struct {
    Kind      EventKind      `json:"kind"`
    Timestamp time.Time      `json:"timestamp"`
    SessionID string         `json:"session_id"`
    Data      map[string]any `json:"data,omitempty"`
}
```

### Event Summary

```go
type EventSummary struct {
    TotalEvents int
    ByType      map[EngineEventType]int
    ByNode      map[string]int
    FirstEvent  *time.Time
    LastEvent   *time.Time
}
```

### Streaming Events (`llm` package)

| Constant | Value | Description |
|----------|-------|-------------|
| `StreamStart` | `"stream_start"` | Stream has started. |
| `StreamTextStart` | `"text_start"` | Text generation started. |
| `StreamTextDelta` | `"text_delta"` | Incremental text delta. |
| `StreamTextEnd` | `"text_end"` | Text generation ended. |
| `StreamReasonStart` | `"reasoning_start"` | Reasoning started. |
| `StreamReasonDelta` | `"reasoning_delta"` | Incremental reasoning delta. |
| `StreamReasonEnd` | `"reasoning_end"` | Reasoning ended. |
| `StreamToolStart` | `"tool_call_start"` | Tool call started. |
| `StreamToolDelta` | `"tool_call_delta"` | Incremental tool call arguments. |
| `StreamToolEnd` | `"tool_call_end"` | Tool call ended. |
| `StreamFinish` | `"finish"` | Stream finished. Contains `FinishReason` and `Usage`. |
| `StreamErrorEvt` | `"error"` | Stream error. Contains `Error` field. |
| `StreamProviderEvt` | `"provider_event"` | Raw provider-specific event. |

### FinishReason Constants

```go
const (
    FinishStop          = "stop"
    FinishLength        = "length"
    FinishToolCalls     = "tool_calls"
    FinishContentFilter = "content_filter"
    FinishError         = "error"
    FinishOther         = "other"
)
```

---

## 8. Error Types Reference

### LLM SDK Error Hierarchy (`llm` package)

All errors in the LLM SDK extend `SDKError`. The `IsRetryable()` method indicates whether the error is safe to retry.

```
SDKError (base)
  |
  +-- ProviderError (API responses with status code, provider, raw body)
  |     +-- AuthenticationError    (401) -- not retryable
  |     +-- AccessDeniedError      (403) -- not retryable
  |     +-- NotFoundError          (404) -- not retryable
  |     +-- InvalidRequestError    (400, 422) -- not retryable
  |     +-- RateLimitError         (429) -- retryable
  |     +-- ServerError            (5xx) -- retryable
  |     +-- ContentFilterError     -- not retryable
  |     +-- ContextLengthError     (413) -- not retryable
  |     +-- QuotaExceededError     -- not retryable
  |
  +-- RequestTimeoutError  (408, client-side) -- retryable
  +-- AbortError           -- not retryable
  +-- NetworkError         (DNS, connection) -- retryable
  +-- StreamError          (streaming failures) -- retryable
  +-- InvalidToolCallError -- not retryable
  +-- NoObjectGeneratedError -- not retryable
  +-- ConfigurationError   (missing API key, etc.) -- not retryable
```

#### SDKError

```go
type SDKError struct {
    Message string
    Cause   error
}

func (e *SDKError) Error() string
func (e *SDKError) Unwrap() error
func (e *SDKError) IsRetryable() bool  // always false
```

#### ProviderError

```go
type ProviderError struct {
    SDKError
    Provider   string
    StatusCode int
    ErrorCode  string
    Retryable  bool
    RetryAfter *float64
    Raw        json.RawMessage
}
```

#### ErrorFromStatusCode

```go
func ErrorFromStatusCode(statusCode int, message, provider, errorCode string, raw json.RawMessage, retryAfter *float64) error
```

Maps an HTTP status code to the appropriate error type. Unknown status codes return a `ProviderError` with `Retryable=true` (conservative default).

### Pipeline Errors (`attractor` package)

| Error | Description |
|-------|-------------|
| `ErrLoopRestart` | Signals that a `loop_restart` edge was taken. Contains `TargetNode` field. |
| Parse errors | Returned by `Parse()` for invalid DOT syntax. |
| Validation errors | Returned by `ValidateOrError()` when ERROR-severity diagnostics exist. |
| Execution errors | Formatted as `node "X" execution error: ...` or `stage "X" failed with no outgoing fail edge`. |

---

## 9. Data Types Reference

### Graph Types (`attractor` package)

```go
type Graph struct {
    Name         string
    Nodes        map[string]*Node
    Edges        []*Edge
    Attrs        map[string]string // graph-level attributes
    NodeDefaults map[string]string // node [...] defaults
    EdgeDefaults map[string]string // edge [...] defaults
    Subgraphs    []*Subgraph
}

type Node struct {
    ID    string
    Attrs map[string]string
}

type Edge struct {
    From  string
    To    string
    Attrs map[string]string
}
```

**Key Graph methods:**

```go
func Parse(source string) (*Graph, error)
func (g *Graph) FindNode(id string) *Node
func (g *Graph) FindStartNode() *Node
func (g *Graph) OutgoingEdges(nodeID string) []*Edge
func (g *Graph) IncomingEdges(nodeID string) []*Edge
func (g *Graph) NodeIDs() []string
```

### Shape-to-Handler Mapping

| Shape | Handler Type | Go Struct |
|-------|-------------|-----------|
| `Mdiamond` | `start` | `StartHandler` |
| `Msquare` | `exit` | `ExitHandler` |
| `box` | `codergen` | `CodergenHandler` |
| `diamond` | `conditional` | `ConditionalHandler` |
| `hexagon` | `wait.human` | `WaitForHumanHandler` |
| `component` | `parallel` | `ParallelHandler` |
| `tripleoctagon` | `parallel.fan_in` | `FanInHandler` |
| `parallelogram` | `tool` | `ToolHandler` |
| `house` | `stack.manager_loop` | `ManagerLoopHandler` |

Unknown shapes default to `codergen`.

### Question Types (`attractor` package)

```go
type Question struct {
    ID       string
    Text     string
    Options  []string           // empty means free-text
    Default  string             // default answer if timeout
    Metadata map[string]string  // arbitrary key-value pairs
}

type QAPair struct {
    Question string
    Options  []string
    Answer   string
}
```

### RunIndex Types

```go
type RunIndex struct {
    Runs    map[string]RunIndexEntry `json:"runs"`
    Updated time.Time               `json:"updated"`
}

type RunIndexEntry struct {
    ID         string    `json:"id"`
    Status     string    `json:"status"`
    StartTime  time.Time `json:"start_time"`
    EventCount int       `json:"event_count"`
}
```

---

See also: [CLI Specification](cli-spec.md) for detailed CLI behavior, [DSL Reference](dsl-reference.md) for DOT attribute syntax, [Handlers Reference](handlers.md) for handler implementation details, [Backend Configuration](backend-config.md) for LLM provider setup, [Walkthrough](walkthrough.md) for end-to-end examples.
