# Setup Command Design

## Overview

`mammoth setup` — an interactive wizard that helps new users configure API keys and get started. Fast for power users (flags to skip steps), friendly for beginners (guided prompts).

## Audience

All levels: internal developers, external Go developers, non-technical users.

## Command

```
mammoth setup [flags]
```

### Flags

| Flag | Default | Purpose |
|------|---------|---------|
| `--skip-keys` | `false` | Skip API key collection |
| `--env-file` | `./.env` | Path to write .env file |

## Flow

### Step 1: Welcome + State Detection

Print a welcome banner. Detect existing state:
- Check for existing `.env` file
- Check which API keys are already set in environment

### Step 2: API Key Collection

Show provider status:
```
LLM Providers:
  [✓] Anthropic (ANTHROPIC_API_KEY is set)
  [ ] OpenAI    (OPENAI_API_KEY not set)
  [ ] Gemini    (GEMINI_API_KEY not set)

Enter API keys (leave blank to skip):
```

Format validation (warn but accept if mismatch):
- Anthropic: `sk-ant-*`
- OpenAI: `sk-*`
- Gemini: `AIza*`

Write to `.env` file. Never clobber existing values without confirmation.

At least one key should end up configured, but don't block if none are set (user may use `claude-code` backend).

### Step 3: Summary + Getting Started

```
Setup complete! You configured: Anthropic

Quick start:
  mammoth serve               Launch the web UI (recommended)
  mammoth pipeline.dot        Run a pipeline from the CLI
  mammoth -help               See all options
```

## Implementation

- New file: `cmd/mammoth/setup.go` — subcommand handler and step functions
- New file: `cmd/mammoth/setup_test.go` — tests for each step
- Edit: `cmd/mammoth/main.go` — register `setup` subcommand
- Interactive prompts via `bufio.Scanner` on stdin
- Each step is a self-contained function, testable in isolation
- Embed nothing extra — the command is self-contained

## Non-Goals

- No example scaffolding (users can find examples in repo)
- No first-run pipeline execution
- No system dependency checking
- No TUI wizard (overkill for a one-time command)
- No live API key validation (format check only)
