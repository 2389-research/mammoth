# SPRINT-002: Fix OpenAI Token Tracking in Streaming Mode

## Goal
Fix the bug where all OpenAI (Chat Completions API) nodes report 0 tokens and 0 tool calls in pipeline output, despite executing successfully.

## Background
When running pipelines through Cloudflare AI Gateway, OpenAI nodes use `OpenAICompatClient` (Chat Completions API at `/v1/chat/completions`) instead of the Responses API. The Chat Completions streaming API does NOT include usage data in stream chunks by default. However, OpenAI supports `stream_options.include_usage = true` which sends a final chunk containing usage data.

## Root Cause
File: `llm/openai_compat.go`

1. `convertCompatRequest()` does not set `StreamOptions.IncludeUsage = true` on the request params
2. Therefore `acc.ChatCompletion.Usage` is always zero after streaming
3. `convertCompatResponse()` reads the zero usage and propagates it through the entire chain

## Requirements
1. In `convertCompatRequest()`, set `StreamOptions` with `IncludeUsage: true` so the final stream chunk includes token counts
2. Verify that `convertCompatResponse()` correctly reads `resp.Usage.PromptTokens` and `resp.Usage.CompletionTokens` (it already does, the data just isn't there)
3. Also check `resp.Usage.CompletionTokensDetails.ReasoningTokens` for reasoning token tracking

## Key Files
- `llm/openai_compat.go` — The fix goes here, in `convertCompatRequest()`
- `llm/openai_compat_test.go` — Add/update tests to verify usage is populated
- `llm/mux_adapter.go` — Verify the mux adapter correctly passes usage through (likely no changes needed)

## Definition of Done
- [x] `convertCompatRequest()` sets `StreamOptions.IncludeUsage = true`
- [x] Streaming responses from OpenAI Chat Completions API include usage data
- [x] `convertCompatResponse()` maps reasoning tokens if available (checked; muxllm.Response has no ReasoningTokens field — documented in code comment)
- [x] Tests verify usage fields are non-zero after streaming
- [x] `go test ./llm/...` passes
- [x] `go vet ./...` passes

## Expected Artifacts
- Modified `llm/openai_compat.go`
- Modified or new `llm/openai_compat_test.go`
