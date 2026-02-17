// ABOUTME: Streaming response accumulator that consumes LLM stream events into an llm.Response.
// ABOUTME: Provides consumeStream (with event emission and delta batching) and buildResponseFromStream.

package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/2389-research/mammoth/llm"
)

// deltaFlushThreshold is the character count at which buffered text deltas are flushed
// as an EventAssistantTextDelta event. This reduces event frequency for many small deltas.
const deltaFlushThreshold = 200

// streamAccumulator gathers incremental stream data so it can be assembled into
// a complete llm.Response once the stream finishes.
type streamAccumulator struct {
	textBuf      string
	reasoningBuf string

	// Tool call state: the current tool call being built from deltas
	toolCalls       []llm.ToolCallData
	currentToolID   string
	currentToolName string
	currentToolArgs string

	finishReason *llm.FinishReason
	usage        *llm.Usage

	// Metadata that may arrive in a finish event's embedded Response
	responseID string
	model      string
	provider   string
}

// consumeStream reads all events from the stream channel, emits agent session events
// for observability, and accumulates stream data into an *llm.Response. It batches
// text deltas to reduce event frequency: flushes occur when the buffer exceeds
// deltaFlushThreshold characters or when a non-text-delta event arrives.
//
// Returns an error if the context is cancelled or the stream sends an error event.
func consumeStream(ctx context.Context, session *Session, stream <-chan llm.StreamEvent) (*llm.Response, error) {
	acc := &streamAccumulator{}

	// deltaBuf holds text deltas that haven't been flushed as events yet
	deltaBuf := ""

	// flushDelta emits the buffered delta text as an EventAssistantTextDelta
	flushDelta := func() {
		if deltaBuf == "" {
			return
		}
		session.Emit(EventAssistantTextDelta, map[string]any{
			"text": deltaBuf,
		})
		deltaBuf = ""
	}

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()

		case ev, ok := <-stream:
			if !ok {
				// Channel closed: build response from what we have
				flushDelta()
				return buildResponseFromStream(acc), nil
			}

			switch ev.Type {
			case llm.StreamStart:
				// Nothing to accumulate for stream start

			case llm.StreamTextStart:
				session.Emit(EventAssistantTextStart, nil)

			case llm.StreamTextDelta:
				acc.textBuf += ev.Delta
				deltaBuf += ev.Delta

				// Flush if buffer exceeds threshold
				if len(deltaBuf) >= deltaFlushThreshold {
					flushDelta()
				}

			case llm.StreamTextEnd:
				// Flush any remaining buffered deltas
				flushDelta()

			case llm.StreamReasonStart:
				// Flush any pending text deltas before switching to reasoning
				flushDelta()

			case llm.StreamReasonDelta:
				acc.reasoningBuf += ev.ReasoningDelta

			case llm.StreamReasonEnd:
				// Reasoning block complete

			case llm.StreamToolStart:
				// Flush any pending text deltas before tool call
				flushDelta()

				if ev.ToolCall != nil {
					acc.currentToolID = ev.ToolCall.ID
					acc.currentToolName = ev.ToolCall.Name
					acc.currentToolArgs = ""
				}

			case llm.StreamToolDelta:
				acc.currentToolArgs += ev.Delta

			case llm.StreamToolEnd:
				// Finalize the current tool call
				tc := llm.ToolCallData{
					ID:        acc.currentToolID,
					Name:      acc.currentToolName,
					Arguments: json.RawMessage(acc.currentToolArgs),
				}
				acc.toolCalls = append(acc.toolCalls, tc)
				acc.currentToolID = ""
				acc.currentToolName = ""
				acc.currentToolArgs = ""

			case llm.StreamFinish:
				flushDelta()

				if ev.FinishReason != nil {
					acc.finishReason = ev.FinishReason
				}
				if ev.Usage != nil {
					acc.usage = ev.Usage
				}
				// If the finish event carries a full embedded Response, extract metadata
				if ev.Response != nil {
					acc.responseID = ev.Response.ID
					acc.model = ev.Response.Model
					acc.provider = ev.Response.Provider
				}

			case llm.StreamErrorEvt:
				flushDelta()
				if ev.Error != nil {
					return nil, fmt.Errorf("stream error: %w", ev.Error)
				}
				return nil, fmt.Errorf("stream error: unknown")

			case llm.StreamProviderEvt:
				// Provider-specific events are passed through without accumulation
			}
		}
	}
}

// buildResponseFromStream constructs an *llm.Response from the accumulated stream data.
// The response message contains text, reasoning (as thinking content), and tool call parts.
func buildResponseFromStream(acc *streamAccumulator) *llm.Response {
	var parts []llm.ContentPart

	// Add reasoning as thinking content if present
	if acc.reasoningBuf != "" {
		parts = append(parts, llm.ContentPart{
			Kind:     llm.ContentThinking,
			Thinking: &llm.ThinkingData{Text: acc.reasoningBuf},
		})
	}

	// Add text content if present
	if acc.textBuf != "" {
		parts = append(parts, llm.TextPart(acc.textBuf))
	}

	// Add tool calls
	for _, tc := range acc.toolCalls {
		parts = append(parts, llm.ToolCallPart(tc.ID, tc.Name, tc.Arguments))
	}

	// Build finish reason
	finishReason := llm.FinishReason{}
	if acc.finishReason != nil {
		finishReason = *acc.finishReason
	}

	// Build usage
	usage := llm.Usage{}
	if acc.usage != nil {
		usage = *acc.usage
	}

	return &llm.Response{
		ID:       acc.responseID,
		Model:    acc.model,
		Provider: acc.provider,
		Message: llm.Message{
			Role:    llm.RoleAssistant,
			Content: parts,
		},
		FinishReason: finishReason,
		Usage:        usage,
	}
}
