// ABOUTME: OpenAI Chat Completions API client with base URL support for compatible providers.
// ABOUTME: Enables Cerebras, OpenRouter, Cloudflare AI Gateway, and other OpenAI-compatible services.

package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	muxllm "github.com/2389-research/mux/llm"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

// OpenAICompatClient implements muxllm.Client using the OpenAI Chat Completions
// API. Unlike mux's built-in OpenAIClient, this supports custom base URLs for
// OpenAI-compatible providers (Cerebras, OpenRouter, Cloudflare AI Gateway, etc.).
type OpenAICompatClient struct {
	client openai.Client
	model  string
}

// NewOpenAICompatClient creates a Chat Completions client with a custom base URL.
// This uses /v1/chat/completions (not /v1/responses), which is the standard
// endpoint supported by all OpenAI-compatible providers.
func NewOpenAICompatClient(apiKey, model, baseURL string) *OpenAICompatClient {
	if model == "" {
		model = "gpt-5.2"
	}
	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	return &OpenAICompatClient{
		client: openai.NewClient(opts...),
		model:  model,
	}
}

// CreateMessage sends a message and returns the complete response.
func (c *OpenAICompatClient) CreateMessage(ctx context.Context, req *muxllm.Request) (*muxllm.Response, error) {
	if req.Model == "" {
		req.Model = c.model
	}
	if req.MaxTokens == 0 {
		req.MaxTokens = 4096
	}

	params := convertCompatRequest(req)
	resp, err := c.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, err
	}

	return convertCompatResponse(resp), nil
}

// CreateMessageStream sends a message and returns a channel of streaming events.
func (c *OpenAICompatClient) CreateMessageStream(ctx context.Context, req *muxllm.Request) (<-chan muxllm.StreamEvent, error) {
	if req.Model == "" {
		req.Model = c.model
	}
	if req.MaxTokens == 0 {
		req.MaxTokens = 4096
	}

	params := convertCompatRequest(req)
	stream := c.client.Chat.Completions.NewStreaming(ctx, params)

	eventChan := make(chan muxllm.StreamEvent, 100)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Fprintf(os.Stderr, "Error: panic recovered in OpenAICompatClient stream: %v\n", r)
				eventChan <- muxllm.StreamEvent{
					Type:  muxllm.EventError,
					Error: fmt.Errorf("panic in stream processing: %v", r),
				}
			}
			close(eventChan)
		}()

		var acc openai.ChatCompletionAccumulator

		eventChan <- muxllm.StreamEvent{Type: muxllm.EventMessageStart}

		for stream.Next() {
			chunk := stream.Current()
			acc.AddChunk(chunk)

			if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
				eventChan <- muxllm.StreamEvent{
					Type: muxllm.EventContentDelta,
					Text: chunk.Choices[0].Delta.Content,
				}
			}

			if toolCall, ok := acc.JustFinishedToolCall(); ok {
				var input map[string]any
				if err := json.Unmarshal([]byte(toolCall.Arguments), &input); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to parse tool call arguments for %s: %v\n", toolCall.Name, err)
					input = make(map[string]any)
				}

				eventChan <- muxllm.StreamEvent{
					Type: muxllm.EventContentStop,
					Block: &muxllm.ContentBlock{
						Type:  muxllm.ContentTypeToolUse,
						ID:    toolCall.ID,
						Name:  toolCall.Name,
						Input: input,
					},
				}
			}
		}

		if err := stream.Err(); err != nil {
			eventChan <- muxllm.StreamEvent{
				Type:  muxllm.EventError,
				Error: err,
			}
			return
		}

		eventChan <- muxllm.StreamEvent{
			Type:     muxllm.EventMessageStop,
			Response: convertCompatResponse(&acc.ChatCompletion),
		}
	}()

	return eventChan, nil
}

// convertCompatRequest converts a mux Request to OpenAI ChatCompletionNewParams.
func convertCompatRequest(req *muxllm.Request) openai.ChatCompletionNewParams {
	params := openai.ChatCompletionNewParams{
		Model: req.Model,
	}

	if req.MaxTokens > 0 {
		params.MaxCompletionTokens = openai.Int(int64(req.MaxTokens))
	}

	if req.Temperature != nil {
		params.Temperature = openai.Float(*req.Temperature)
	}

	var messages []openai.ChatCompletionMessageParamUnion

	if req.System != "" {
		messages = append(messages, openai.SystemMessage(req.System))
	}

	for _, msg := range req.Messages {
		switch msg.Role {
		case muxllm.RoleUser:
			messages = append(messages, convertCompatUserMessage(msg))
		case muxllm.RoleAssistant:
			messages = append(messages, convertCompatAssistantMessage(msg))
		}
	}
	params.Messages = messages

	if len(req.Tools) > 0 {
		tools := make([]openai.ChatCompletionToolParam, 0, len(req.Tools))
		for _, tool := range req.Tools {
			toolParam := openai.ChatCompletionToolParam{
				Type: "function",
				Function: openai.FunctionDefinitionParam{
					Name:        tool.Name,
					Description: openai.String(tool.Description),
					Parameters:  openai.FunctionParameters(tool.InputSchema),
				},
			}
			tools = append(tools, toolParam)
		}
		params.Tools = tools
	}

	return params
}

// convertCompatUserMessage converts a mux user message to OpenAI format.
func convertCompatUserMessage(msg muxllm.Message) openai.ChatCompletionMessageParamUnion {
	for _, block := range msg.Blocks {
		if block.Type == muxllm.ContentTypeToolResult {
			return openai.ToolMessage(block.Text, block.ToolUseID)
		}
	}

	if msg.Content != "" {
		return openai.UserMessage(msg.Content)
	}

	for _, block := range msg.Blocks {
		if block.Type == muxllm.ContentTypeText {
			return openai.UserMessage(block.Text)
		}
	}

	return openai.UserMessage("")
}

// convertCompatAssistantMessage converts a mux assistant message to OpenAI format.
func convertCompatAssistantMessage(msg muxllm.Message) openai.ChatCompletionMessageParamUnion {
	var toolCalls []openai.ChatCompletionMessageToolCallParam
	var textContent string

	if msg.Content != "" {
		textContent = msg.Content
	}

	for _, block := range msg.Blocks {
		switch block.Type {
		case muxllm.ContentTypeText:
			textContent = block.Text
		case muxllm.ContentTypeToolUse:
			argsJSON, _ := json.Marshal(block.Input)
			toolCalls = append(toolCalls, openai.ChatCompletionMessageToolCallParam{
				ID:   block.ID,
				Type: "function",
				Function: openai.ChatCompletionMessageToolCallFunctionParam{
					Name:      block.Name,
					Arguments: string(argsJSON),
				},
			})
		}
	}

	if len(toolCalls) > 0 {
		asstMsg := openai.ChatCompletionAssistantMessageParam{
			Role:      "assistant",
			ToolCalls: toolCalls,
		}
		if textContent != "" {
			asstMsg.Content = openai.ChatCompletionAssistantMessageParamContentUnion{
				OfString: openai.String(textContent),
			}
		}
		return openai.ChatCompletionMessageParamUnion{OfAssistant: &asstMsg}
	}

	return openai.AssistantMessage(textContent)
}

// convertCompatResponse converts OpenAI ChatCompletion to a mux Response.
func convertCompatResponse(resp *openai.ChatCompletion) *muxllm.Response {
	result := &muxllm.Response{
		ID:    resp.ID,
		Model: resp.Model,
		Usage: muxllm.Usage{
			InputTokens:  int(resp.Usage.PromptTokens),
			OutputTokens: int(resp.Usage.CompletionTokens),
		},
	}

	if len(resp.Choices) == 0 {
		return result
	}

	choice := resp.Choices[0]

	switch choice.FinishReason {
	case "stop":
		result.StopReason = muxllm.StopReasonEndTurn
	case "tool_calls":
		result.StopReason = muxllm.StopReasonToolUse
	case "length":
		result.StopReason = muxllm.StopReasonMaxTokens
	default:
		result.StopReason = muxllm.StopReasonEndTurn
	}

	if choice.Message.Content != "" {
		result.Content = append(result.Content, muxllm.ContentBlock{
			Type: muxllm.ContentTypeText,
			Text: choice.Message.Content,
		})
	}

	for _, tc := range choice.Message.ToolCalls {
		var input map[string]any
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &input); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to parse tool call arguments for %s: %v\n", tc.Function.Name, err)
			input = make(map[string]any)
		}

		result.Content = append(result.Content, muxllm.ContentBlock{
			Type:  muxllm.ContentTypeToolUse,
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: input,
		})
	}

	return result
}

// Compile-time interface assertion.
var _ muxllm.Client = (*OpenAICompatClient)(nil)
