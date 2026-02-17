// ABOUTME: Core agentic loop that orchestrates LLM calls, tool execution, steering, and session management.
// ABOUTME: Provides ProcessInput (the main loop), drainSteering, and tool execution dispatch functions.

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/2389-research/mammoth/llm"
)

// ProcessInput runs the core agentic loop: it appends the user input to the session,
// calls the LLM, executes any tool calls, and loops until the model produces a text-only
// response, a limit is hit, or the context is cancelled.
func ProcessInput(ctx context.Context, session *Session, profile ProviderProfile, env ExecutionEnvironment, client *llm.Client, userInput string) error {
	session.SetState(StateProcessing)
	session.AppendTurn(UserTurn{Content: userInput, Timestamp: time.Now()})
	session.Emit(EventUserInput, map[string]any{"content": userInput})

	// Drain any pending steering messages before the first LLM call
	drainSteering(session)

	roundCount := 0

	for {
		// 1. Check round limit
		if roundCount >= session.Config.MaxToolRoundsPerInput {
			session.Emit(EventTurnLimit, map[string]any{"round": roundCount})
			break
		}

		// 2. Check turn limit
		if session.Config.MaxTurns > 0 && session.TurnCount() >= session.Config.MaxTurns {
			session.Emit(EventTurnLimit, map[string]any{"total_turns": session.TurnCount()})
			break
		}

		// 3. Check context cancellation
		if ctx.Err() != nil {
			break
		}

		// 4. Build LLM request
		projectDocs := DiscoverProjectDocs(env)
		systemPrompt := profile.BuildSystemPrompt(env, projectDocs)
		if session.Config.UserOverride != "" {
			systemPrompt += "\n\n## User Instructions\n\n" + session.Config.UserOverride
		}

		session.mu.Lock()
		historyForLLM := session.History
		if session.Config.FidelityMode != "" {
			historyForLLM = ApplyFidelity(session.History, session.Config.FidelityMode, profile.ContextWindowSize())
		}
		messages := ConvertHistoryToMessages(historyForLLM)
		session.mu.Unlock()

		// Prepend system prompt as the first message
		allMessages := make([]llm.Message, 0, len(messages)+1)
		allMessages = append(allMessages, llm.SystemMessage(systemPrompt))
		allMessages = append(allMessages, messages...)

		request := llm.Request{
			Model:           profile.Model(),
			Messages:        allMessages,
			Tools:           profile.Tools(),
			ToolChoice:      &llm.ToolChoice{Mode: llm.ToolChoiceAuto},
			ReasoningEffort: session.Config.ReasoningEffort,
			Provider:        profile.ID(),
			ProviderOptions: profile.ProviderOptions(),
		}

		// 5. Call LLM
		response, err := client.Complete(ctx, request)
		if err != nil {
			// If context was cancelled, break out gracefully
			if ctx.Err() != nil {
				break
			}
			// For other errors, emit and return
			session.Emit(EventError, map[string]any{"error": err.Error()})
			session.SetState(StateIdle)
			session.Emit(EventSessionEnd, nil)
			return fmt.Errorf("LLM call failed: %w", err)
		}

		// 6. Extract tool calls from the response
		toolCalls := response.ToolCalls()
		textContent := response.TextContent()
		reasoning := response.Reasoning()

		// 7. Record assistant turn
		assistantTurn := AssistantTurn{
			Content:    textContent,
			ToolCalls:  toolCalls,
			Reasoning:  reasoning,
			Usage:      response.Usage,
			ResponseID: response.ID,
			Timestamp:  time.Now(),
		}
		session.AppendTurn(assistantTurn)
		session.Emit(EventAssistantTextEnd, map[string]any{
			"text":               textContent,
			"reasoning":          reasoning,
			"input_tokens":       response.Usage.InputTokens,
			"output_tokens":      response.Usage.OutputTokens,
			"total_tokens":       response.Usage.TotalTokens,
			"reasoning_tokens":   response.Usage.ReasoningTokens,
			"cache_read_tokens":  response.Usage.CacheReadTokens,
			"cache_write_tokens": response.Usage.CacheWriteTokens,
		})

		// 8. If no tool calls, natural completion
		if len(toolCalls) == 0 {
			break
		}

		// 9. Execute tool calls
		roundCount++
		results := executeToolCalls(ctx, session, profile, env, toolCalls, profile.SupportsParallelToolCalls())
		session.AppendTurn(ToolResultsTurn{Results: results, Timestamp: time.Now()})

		// 10. Drain steering messages injected during tool execution
		drainSteering(session)

		// 11. Loop detection
		if session.Config.EnableLoopDetection {
			session.mu.Lock()
			loopDetected := DetectLoop(session.History, session.Config.LoopDetectionWindow)
			session.mu.Unlock()

			if loopDetected {
				warning := fmt.Sprintf("Loop detected: the last %d tool calls follow a repeating pattern. Try a different approach.",
					session.Config.LoopDetectionWindow)
				session.AppendTurn(SteeringTurn{Content: warning, Timestamp: time.Now()})
				session.Emit(EventLoopDetection, map[string]any{"message": warning})
			}
		}
	}

	// Process follow-up messages if any are queued
	followup := session.DrainFollowup()
	if followup != "" {
		return ProcessInput(ctx, session, profile, env, client, followup)
	}

	session.SetState(StateIdle)
	session.Emit(EventSessionEnd, nil)
	return nil
}

// drainSteering removes all pending steering messages from the session queue,
// appends them as SteeringTurns in the history, and emits events for each.
func drainSteering(session *Session) {
	messages := session.DrainSteering()
	for _, msg := range messages {
		session.AppendTurn(SteeringTurn{Content: msg, Timestamp: time.Now()})
		session.Emit(EventSteeringInjected, map[string]any{"content": msg})
	}
}

// executeToolCalls runs tool calls either sequentially or in parallel depending on the
// parallel flag and the number of calls. Results are returned in the same order as the
// input tool calls.
func executeToolCalls(ctx context.Context, session *Session, profile ProviderProfile, env ExecutionEnvironment, toolCalls []llm.ToolCallData, parallel bool) []llm.ToolResult {
	if parallel && len(toolCalls) > 1 {
		results := make([]llm.ToolResult, len(toolCalls))
		var wg sync.WaitGroup
		wg.Add(len(toolCalls))
		for i, tc := range toolCalls {
			go func(idx int, call llm.ToolCallData) {
				defer wg.Done()
				results[idx] = executeSingleTool(session, profile, env, call)
			}(i, tc)
		}
		wg.Wait()
		return results
	}

	// Sequential execution
	results := make([]llm.ToolResult, 0, len(toolCalls))
	for _, tc := range toolCalls {
		results = append(results, executeSingleTool(session, profile, env, tc))
	}
	return results
}

// executeSingleTool looks up and executes a single tool call, handling errors
// and output truncation. It emits TOOL_CALL_START and TOOL_CALL_END events.
func executeSingleTool(session *Session, profile ProviderProfile, env ExecutionEnvironment, tc llm.ToolCallData) llm.ToolResult {
	session.Emit(EventToolCallStart, map[string]any{
		"tool_name": tc.Name,
		"call_id":   tc.ID,
	})

	// Look up tool in registry
	registry := profile.ToolRegistry()
	registered := registry.Get(tc.Name)
	if registered == nil {
		errorMsg := fmt.Sprintf("Unknown tool: %s", tc.Name)
		session.Emit(EventToolCallEnd, map[string]any{
			"call_id": tc.ID,
			"error":   errorMsg,
		})
		return llm.ToolResult{
			ToolCallID: tc.ID,
			Content:    errorMsg,
			IsError:    true,
		}
	}

	// Parse arguments
	var args map[string]any
	if len(tc.Arguments) > 0 {
		if err := json.Unmarshal(tc.Arguments, &args); err != nil {
			errorMsg := fmt.Sprintf("Tool error (%s): failed to parse arguments: %s", tc.Name, err)
			session.Emit(EventToolCallEnd, map[string]any{
				"call_id": tc.ID,
				"error":   errorMsg,
			})
			return llm.ToolResult{
				ToolCallID: tc.ID,
				Content:    errorMsg,
				IsError:    true,
			}
		}
	} else {
		args = make(map[string]any)
	}

	// Execute the tool
	rawOutput, err := registered.Execute(args, env)
	if err != nil {
		errorMsg := fmt.Sprintf("Tool error (%s): %s", tc.Name, err)
		session.Emit(EventToolCallEnd, map[string]any{
			"call_id": tc.ID,
			"error":   errorMsg,
		})
		return llm.ToolResult{
			ToolCallID: tc.ID,
			Content:    errorMsg,
			IsError:    true,
		}
	}

	// Truncate output before sending to LLM
	truncatedOutput := TruncateToolOutput(rawOutput, tc.Name, session.Config.ToolOutputLimits)

	// Emit full (untruncated) output via event stream
	session.Emit(EventToolCallEnd, map[string]any{
		"call_id": tc.ID,
		"output":  rawOutput,
	})

	return llm.ToolResult{
		ToolCallID: tc.ID,
		Content:    truncatedOutput,
		IsError:    false,
	}
}
