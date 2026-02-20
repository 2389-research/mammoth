// ABOUTME: Adapter that wraps a mux/llm.Client as a mammoth ProviderAdapter.
// ABOUTME: Translates between mammoth's LLM types and mux's types for seamless agent integration.

package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	muxllm "github.com/2389-research/mux/llm"
)

// rateLimitRetryPolicy returns a RetryPolicy tuned for rate limit backoff.
// Uses exponential backoff (2s base, 3x multiplier) with up to 5 retries,
// giving the API up to ~3 minutes to recover.
func rateLimitRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxRetries:        5,
		BaseDelay:         2 * time.Second,
		MaxDelay:          90 * time.Second,
		BackoffMultiplier: 3.0,
		Jitter:            true,
		OnRetry: func(err error, attempt int, delay time.Duration) {
			log.Printf("component=llm.mux action=rate_limit_retry attempt=%d delay=%s err=%v", attempt+1, delay, err)
		},
	}
}

// isRateLimitError detects 429 rate limit errors from mux provider SDKs.
// The underlying SDKs (anthropic-sdk-go, openai-go, genai) surface 429 status
// codes in their error messages.
func isRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "429") || strings.Contains(msg, "rate limit") || strings.Contains(msg, "rate_limit")
}

// MuxAdapter wraps a mux/llm.Client as a mammoth ProviderAdapter. This allows
// mammoth's agent loop to use mux as an LLM provider without any changes to
// the agent code.
type MuxAdapter struct {
	client muxllm.Client
	name   string
}

// NewMuxAdapter creates a MuxAdapter with the given provider name and mux client.
func NewMuxAdapter(name string, client muxllm.Client) *MuxAdapter {
	return &MuxAdapter{name: name, client: client}
}

// Name returns the provider name for this adapter.
func (a *MuxAdapter) Name() string {
	return a.name
}

// Complete sends a completion request through the mux client. Rate limit
// errors (429) are automatically retried with exponential backoff.
func (a *MuxAdapter) Complete(ctx context.Context, req Request) (*Response, error) {
	muxReq := convertRequest(req)

	var muxResp *muxllm.Response
	policy := rateLimitRetryPolicy()

	err := retryOnRateLimit(ctx, policy, func() error {
		var callErr error
		muxResp, callErr = a.client.CreateMessage(ctx, muxReq)
		return callErr
	})
	if err != nil {
		return nil, fmt.Errorf("mux adapter complete: %w", err)
	}
	return convertResponse(muxResp, a.name), nil
}

// Stream sends a streaming request through the mux client, converting each mux
// stream event into a mammoth StreamEvent. Rate limit errors (429) on the
// initial connection are automatically retried with exponential backoff.
func (a *MuxAdapter) Stream(ctx context.Context, req Request) (<-chan StreamEvent, error) {
	muxReq := convertRequest(req)

	var muxCh <-chan muxllm.StreamEvent
	policy := rateLimitRetryPolicy()

	err := retryOnRateLimit(ctx, policy, func() error {
		var callErr error
		muxCh, callErr = a.client.CreateMessageStream(ctx, muxReq)
		return callErr
	})
	if err != nil {
		return nil, fmt.Errorf("mux adapter stream: %w", err)
	}

	ch := make(chan StreamEvent)
	go func() {
		defer close(ch)
		// Track the current content block type so we can emit the correct
		// mammoth event type for delta/stop events (text vs tool_call).
		var currentBlockType muxllm.ContentType
		for muxEvt := range muxCh {
			// Update block type tracking on content_block_start.
			if muxEvt.Type == muxllm.EventContentStart && muxEvt.Block != nil {
				currentBlockType = muxEvt.Block.Type
			}

			evt := convertStreamEvent(muxEvt, &currentBlockType)
			select {
			case ch <- evt:
			case <-ctx.Done():
				return
			}

			// Reset block type on content_block_stop.
			if muxEvt.Type == muxllm.EventContentStop {
				currentBlockType = ""
			}
		}
	}()

	return ch, nil
}

// Close releases any resources held by the adapter. The underlying mux client
// does not expose a Close method, so this is a no-op.
func (a *MuxAdapter) Close() error {
	return nil
}

// retryOnRateLimit retries fn when it returns a rate limit error (429).
// Uses the provided RetryPolicy for backoff timing. Non-rate-limit errors
// are returned immediately without retry.
func retryOnRateLimit(ctx context.Context, policy RetryPolicy, fn func() error) error {
	var lastErr error
	for attempt := 0; ; attempt++ {
		lastErr = fn()
		if lastErr == nil {
			return nil
		}
		if !isRateLimitError(lastErr) || attempt >= policy.MaxRetries {
			return lastErr
		}

		delay := policy.CalculateDelay(attempt)
		if policy.OnRetry != nil {
			policy.OnRetry(lastErr, attempt, delay)
		}

		select {
		case <-ctx.Done():
			return lastErr
		case <-time.After(delay):
		}
	}
}

// convertRequest translates a mammoth Request into a mux Request. System and
// developer role messages are extracted into the mux Request.System field.
func convertRequest(req Request) *muxllm.Request {
	systemText, remaining := ExtractSystemMessages(req.Messages)

	muxMsgs := convertMessages(remaining)
	muxTools := convertTools(req.Tools)

	muxReq := &muxllm.Request{
		Model:       req.Model,
		Messages:    muxMsgs,
		Tools:       muxTools,
		System:      systemText,
		Temperature: req.Temperature,
	}

	if req.MaxTokens != nil {
		muxReq.MaxTokens = *req.MaxTokens
	}

	return muxReq
}

// convertMessages translates mammoth Messages into mux Messages. Tool role
// messages are converted to user role messages with tool_result content blocks,
// which is the format mux (and Anthropic) expects.
func convertMessages(msgs []Message) []muxllm.Message {
	var result []muxllm.Message
	for _, msg := range msgs {
		result = append(result, convertMessage(msg))
	}
	return result
}

// convertMessage translates a single mammoth Message into a mux Message.
func convertMessage(msg Message) muxllm.Message {
	muxMsg := muxllm.Message{}

	switch msg.Role {
	case RoleUser:
		muxMsg.Role = muxllm.RoleUser
	case RoleAssistant:
		muxMsg.Role = muxllm.RoleAssistant
	case RoleTool:
		// Tool results are sent as user messages in mux.
		muxMsg.Role = muxllm.RoleUser
	default:
		// System and developer are handled by ExtractSystemMessages before
		// we reach here; treat any other role as user.
		muxMsg.Role = muxllm.RoleUser
	}

	// Optimization: if the message has a single text part (the common case),
	// use the Content string field instead of Blocks for a simpler payload.
	if len(msg.Content) == 1 && msg.Content[0].Kind == ContentText {
		muxMsg.Content = msg.Content[0].Text
		return muxMsg
	}

	// For messages with multiple parts or non-text content, use Blocks.
	blocks := convertContentPartsToBlocks(msg.Content)
	if len(blocks) > 0 {
		muxMsg.Blocks = blocks
	}

	return muxMsg
}

// convertContentPartsToBlocks translates mammoth ContentParts into mux ContentBlocks.
// Unsupported content types (thinking, redacted_thinking, image, audio, document)
// are silently dropped.
func convertContentPartsToBlocks(parts []ContentPart) []muxllm.ContentBlock {
	var blocks []muxllm.ContentBlock
	for _, part := range parts {
		switch part.Kind {
		case ContentText:
			blocks = append(blocks, muxllm.ContentBlock{
				Type: muxllm.ContentTypeText,
				Text: part.Text,
			})

		case ContentToolCall:
			if part.ToolCall == nil {
				continue
			}
			// Convert json.RawMessage arguments to map[string]any.
			var input map[string]any
			if len(part.ToolCall.Arguments) > 0 {
				if err := json.Unmarshal(part.ToolCall.Arguments, &input); err != nil {
					log.Printf("mux adapter: failed to unmarshal tool call arguments for %q: %v", part.ToolCall.Name, err)
				}
			}
			blocks = append(blocks, muxllm.ContentBlock{
				Type:  muxllm.ContentTypeToolUse,
				ID:    part.ToolCall.ID,
				Name:  part.ToolCall.Name,
				Input: input,
			})

		case ContentToolResult:
			if part.ToolResult == nil {
				continue
			}
			blocks = append(blocks, muxllm.ContentBlock{
				Type:      muxllm.ContentTypeToolResult,
				ToolUseID: part.ToolResult.ToolCallID,
				Text:      part.ToolResult.Content,
				IsError:   part.ToolResult.IsError,
			})

		// Thinking, redacted thinking, image, audio, document have no mux
		// equivalent and are silently dropped.
		case ContentThinking, ContentRedactedThinking, ContentImage, ContentAudio, ContentDocument:
			continue
		}
	}
	return blocks
}

// convertBlocksToContentParts translates mux ContentBlocks back into mammoth
// ContentParts.
func convertBlocksToContentParts(blocks []muxllm.ContentBlock) []ContentPart {
	var parts []ContentPart
	for _, block := range blocks {
		switch block.Type {
		case muxllm.ContentTypeText:
			parts = append(parts, TextPart(block.Text))

		case muxllm.ContentTypeToolUse:
			// Convert map[string]any arguments to json.RawMessage.
			args, err := json.Marshal(block.Input)
			if err != nil {
				args = []byte("{}")
			}
			parts = append(parts, ToolCallPart(block.ID, block.Name, json.RawMessage(args)))

		case muxllm.ContentTypeToolResult:
			parts = append(parts, ToolResultPart(block.ToolUseID, block.Text, block.IsError))
		}
	}
	return parts
}

// convertTools translates mammoth ToolDefinitions into mux ToolDefinitions.
// Parameters (json.RawMessage) are deserialized into map[string]any for mux.
func convertTools(tools []ToolDefinition) []muxllm.ToolDefinition {
	if len(tools) == 0 {
		return nil
	}
	result := make([]muxllm.ToolDefinition, 0, len(tools))
	for _, tool := range tools {
		var schema map[string]any
		if len(tool.Parameters) > 0 {
			if err := json.Unmarshal(tool.Parameters, &schema); err != nil {
				log.Printf("mux adapter: failed to unmarshal tool parameters for %q: %v", tool.Name, err)
			}
		}
		result = append(result, muxllm.ToolDefinition{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: schema,
		})
	}
	return result
}

// convertResponse translates a mux Response into a mammoth Response.
func convertResponse(resp *muxllm.Response, providerName string) *Response {
	parts := convertBlocksToContentParts(resp.Content)

	return &Response{
		ID:       resp.ID,
		Model:    resp.Model,
		Provider: providerName,
		Message: Message{
			Role:    RoleAssistant,
			Content: parts,
		},
		FinishReason: mapStopReason(resp.StopReason),
		Usage: Usage{
			InputTokens:  resp.Usage.InputTokens,
			OutputTokens: resp.Usage.OutputTokens,
			TotalTokens:  resp.Usage.InputTokens + resp.Usage.OutputTokens,
		},
	}
}

// mapStopReason translates a mux StopReason into a mammoth FinishReason.
func mapStopReason(reason muxllm.StopReason) FinishReason {
	raw := string(reason)
	switch reason {
	case muxllm.StopReasonEndTurn:
		return FinishReason{Reason: FinishStop, Raw: raw}
	case muxllm.StopReasonToolUse:
		return FinishReason{Reason: FinishToolCalls, Raw: raw}
	case muxllm.StopReasonMaxTokens:
		return FinishReason{Reason: FinishLength, Raw: raw}
	default:
		return FinishReason{Reason: FinishOther, Raw: raw}
	}
}

// convertStreamEvent translates a mux StreamEvent into a mammoth StreamEvent.
// The currentBlockType parameter tracks the type of the current content block
// so that delta and stop events can be mapped to the correct mammoth event type
// (text vs tool_call).
func convertStreamEvent(evt muxllm.StreamEvent, currentBlockType *muxllm.ContentType) StreamEvent {
	switch evt.Type {
	case muxllm.EventMessageStart:
		se := StreamEvent{Type: StreamStart}
		// Anthropic sends usage (input_tokens) in message_start; capture it.
		if evt.Response != nil && (evt.Response.Usage.InputTokens > 0 || evt.Response.Usage.OutputTokens > 0) {
			se.Usage = &Usage{
				InputTokens:  evt.Response.Usage.InputTokens,
				OutputTokens: evt.Response.Usage.OutputTokens,
				TotalTokens:  evt.Response.Usage.InputTokens + evt.Response.Usage.OutputTokens,
			}
		}
		return se

	case muxllm.EventContentStart:
		if evt.Block != nil {
			switch evt.Block.Type {
			case muxllm.ContentTypeToolUse:
				return StreamEvent{
					Type: StreamToolStart,
					ToolCall: &ToolCall{
						ID:   evt.Block.ID,
						Name: evt.Block.Name,
					},
				}
			default:
				return StreamEvent{Type: StreamTextStart}
			}
		}
		return StreamEvent{Type: StreamTextStart}

	case muxllm.EventContentDelta:
		if currentBlockType != nil && *currentBlockType == muxllm.ContentTypeToolUse {
			return StreamEvent{
				Type:  StreamToolDelta,
				Delta: evt.Text,
			}
		}
		return StreamEvent{
			Type:  StreamTextDelta,
			Delta: evt.Text,
		}

	case muxllm.EventContentStop:
		if currentBlockType != nil && *currentBlockType == muxllm.ContentTypeToolUse {
			return StreamEvent{Type: StreamToolEnd}
		}
		return StreamEvent{Type: StreamTextEnd}

	case muxllm.EventMessageDelta:
		// message_delta carries stop reason updates; map to finish if present.
		if evt.Response != nil {
			fr := mapStopReason(evt.Response.StopReason)
			return StreamEvent{
				Type:         StreamFinish,
				FinishReason: &fr,
				Usage: &Usage{
					InputTokens:  evt.Response.Usage.InputTokens,
					OutputTokens: evt.Response.Usage.OutputTokens,
					TotalTokens:  evt.Response.Usage.InputTokens + evt.Response.Usage.OutputTokens,
				},
			}
		}
		return StreamEvent{Type: StreamFinish}

	case muxllm.EventMessageStop:
		result := StreamEvent{Type: StreamFinish}
		if evt.Response != nil {
			fr := mapStopReason(evt.Response.StopReason)
			result.FinishReason = &fr
			result.Usage = &Usage{
				InputTokens:  evt.Response.Usage.InputTokens,
				OutputTokens: evt.Response.Usage.OutputTokens,
				TotalTokens:  evt.Response.Usage.InputTokens + evt.Response.Usage.OutputTokens,
			}
		}
		return result

	case muxllm.EventError:
		return StreamEvent{
			Type:  StreamErrorEvt,
			Error: evt.Error,
		}

	default:
		return StreamEvent{Type: StreamProviderEvt}
	}
}
