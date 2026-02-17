# Mammoth

A DOT-based pipeline runner for LLM agent workflows. Define a graph, run it, watch it think.

Go implementation of the [StrongDM Attractor](https://github.com/strongdm/attractor) specification.

## Architecture

Mammoth is three layers, bottom-up:

| Layer | Package | Description |
|-------|---------|-------------|
| **LLM SDK** | `llm/` | Unified client for OpenAI, Anthropic, and Gemini APIs |
| **Agent Loop** | `agent/` | Agentic loop with provider profiles, tools, steering, and subagents |
| **Pipeline Runner** | `attractor/` | DOT-based DAG execution, node handlers, human-in-the-loop gates |

Supporting packages: `dot/` (DOT parser/serializer), `spec/` (event-sourced spec builder), `web/` (unified web layer), `editor/` (DOT graph editor), `render/` (DOT→SVG/PNG), `tui/` (Bubble Tea terminal UI), `cmd/mammoth/` (CLI).

## Install

### Homebrew (macOS/Linux)

```bash
brew install 2389-research/tap/mammoth
```

### Go

```bash
go install github.com/2389-research/mammoth/cmd/mammoth@latest
```

### Download binary

Pre-built binaries for macOS and Linux (amd64/arm64) are available on the [releases page](https://github.com/2389-research/mammoth/releases/latest).

```bash
# Example: Linux amd64 — replace VERSION with the desired release (e.g. 0.2.0)
VERSION="0.2.0"
curl -sL "https://github.com/2389-research/mammoth/releases/download/v${VERSION}/mammoth_${VERSION}_Linux_x86_64.tar.gz" | tar xz
sudo mv mammoth /usr/local/bin/
```

### Build from source

```bash
git clone https://github.com/2389-research/mammoth.git
cd mammoth
make build
# Binary is at ./bin/mammoth
```

## Quick Start

```bash
# Set an API key (at least one required)
export ANTHROPIC_API_KEY="sk-ant-..."
# or: export OPENAI_API_KEY="sk-..."
# or: export GEMINI_API_KEY="..."

# Run a pipeline
mammoth run pipeline.dot
```

## Usage

```bash
# Validate a pipeline without running it
mammoth -validate pipeline.dot

# Run with the terminal UI
mammoth run -tui pipeline.dot

# Start the HTTP server
mammoth -server -port 8080
```

## Testing

```bash
# Run all tests (5,200+ tests)
go test ./...

# Run with race detector
make test-race

# Run with coverage
go test ./... -coverprofile=coverage.out
go tool cover -func=coverage.out
```

## Documentation

- [Quickstart](docs/quickstart.md) — Get a pipeline running in under five minutes
- [CLI Usage](docs/cli-usage.md) — Pipeline execution, validation, and server mode
- [CLI Specification](docs/cli-spec.md) — Commands, flags, output formats, and exit codes
- [DSL Reference](docs/dsl-reference.md) — DOT digraph syntax for LLM agent pipelines
- [Handlers](docs/handlers.md) — 9 built-in execution units
- [Backend Configuration](docs/backend-config.md) — Provider keys, model catalog, retry
- [API Reference](docs/api-reference.md) — Go packages, HTTP endpoints, types, and events
- [Walkthrough](docs/walkthrough.md) — Write, validate, and run pipelines from scratch
- [Parity Matrix](docs/parity-matrix.md) — Spec coverage tracking
- [Test Coverage](docs/coverage.md) — Per-package thresholds

Browse the full docs hub at [`docs/index.html`](docs/index.html).

## Spec Parity

192/195 Attractor spec requirements implemented (3 partial, 0 missing). See the [parity matrix](docs/parity-matrix.md) for details.

## License

MIT License. See [LICENSE](LICENSE) for details.

---

Built by [2389 Research, Inc.](https://2389.ai) | [hello@2389.ai](mailto:hello@2389.ai)
