# Enhanced Setup Command Design

## Goal

Enhance `mammoth setup` to collect API keys + base URLs for all LLM providers and store
configuration in XDG config directory instead of local `.env` files.

## Architecture

- Config stored as `config.env` in `XDG_CONFIG_HOME/mammoth/` (defaults to `~/.config/mammoth/`)
- env-format file, compatible with existing `loadDotEnv()` loader
- Loading priority: environment vars > local `.env` > XDG config (no-clobber semantics)
- Base URLs use existing env var names: `ANTHROPIC_BASE_URL`, `OPENAI_BASE_URL`, `GEMINI_BASE_URL`

## Components

### 1. `defaultConfigDir()` in `datadir.go`
- Mirrors `defaultDataDir()` but uses `XDG_CONFIG_HOME`
- Falls back to `~/.config/mammoth`

### 2. Base URL collection in setup wizard
- After each API key prompt, ask for base URL (leave blank for default)
- Store as `PROVIDER_BASE_URL=value` in config file
- No format validation needed (just a URL)

### 3. XDG config loading in `loadDotEnvAuto()`
- Add XDG config dir to the auto-load chain
- Loaded after local `.env` files (lower priority)

### 4. Setup writes to XDG by default
- Default `--env-file` changes from `.env` to XDG config path
- `--env-file` flag still works as override
- Creates parent directories automatically

## Env Vars Stored

| Variable | Provider | Type |
|---|---|---|
| `ANTHROPIC_API_KEY` | Anthropic | API key |
| `ANTHROPIC_BASE_URL` | Anthropic | Base URL |
| `OPENAI_API_KEY` | OpenAI | API key |
| `OPENAI_BASE_URL` | OpenAI | Base URL |
| `GEMINI_API_KEY` | Gemini | API key |
| `GEMINI_BASE_URL` | Gemini | Base URL |
