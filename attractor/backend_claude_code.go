// ABOUTME: ClaudeCodeBackend shells out to the `claude` CLI for codergen pipeline nodes.
// ABOUTME: Parses streaming JSONL output to extract results, token usage, and OUTCOME markers.
package attractor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// ClaudeCodeBackend implements CodergenBackend by shelling out to the `claude` CLI.
// It uses --print --output-format stream-json to get streaming JSONL output with
// real token breakdowns, then parses the result events.
//
// The claude CLI does not support a --max-turns flag. AgentRunConfig.MaxTurns is
// not honored by this backend. Use --max-budget-usd for cost control instead.
type ClaudeCodeBackend struct {
	BinaryPath         string   // resolved via exec.LookPath("claude") if empty
	DefaultModel       string   // falls back to "" (let claude pick)
	AllowedTools       []string // e.g. ["Bash","Read","Edit","Write","Glob","Grep"]
	SkipPermissions    bool     // default: true (required for autonomous pipelines)
	AppendSystemPrompt string   // appended to claude's default system prompt
	MaxBudgetUSD       float64  // maximum dollar spend per run (0 = no limit)
}

// ClaudeCodeOption configures a ClaudeCodeBackend via functional options.
type ClaudeCodeOption func(*ClaudeCodeBackend)

// WithClaudeBinaryPath sets the path to the claude binary.
func WithClaudeBinaryPath(path string) ClaudeCodeOption {
	return func(b *ClaudeCodeBackend) {
		b.BinaryPath = path
	}
}

// WithClaudeModel sets the default model for claude CLI invocations.
func WithClaudeModel(model string) ClaudeCodeOption {
	return func(b *ClaudeCodeBackend) {
		b.DefaultModel = model
	}
}

// WithClaudeAllowedTools sets the allowed tools list for claude CLI invocations.
func WithClaudeAllowedTools(tools []string) ClaudeCodeOption {
	return func(b *ClaudeCodeBackend) {
		b.AllowedTools = tools
	}
}

// WithClaudeSkipPermissions controls whether --dangerously-skip-permissions is passed.
func WithClaudeSkipPermissions(skip bool) ClaudeCodeOption {
	return func(b *ClaudeCodeBackend) {
		b.SkipPermissions = skip
	}
}

// WithClaudeAppendSystemPrompt sets additional system prompt text.
func WithClaudeAppendSystemPrompt(prompt string) ClaudeCodeOption {
	return func(b *ClaudeCodeBackend) {
		b.AppendSystemPrompt = prompt
	}
}

// WithClaudeMaxBudgetUSD sets the maximum dollar spend per invocation.
func WithClaudeMaxBudgetUSD(budget float64) ClaudeCodeOption {
	return func(b *ClaudeCodeBackend) {
		b.MaxBudgetUSD = budget
	}
}

// NewClaudeCodeBackend creates a ClaudeCodeBackend with the given options.
// By default it resolves the "claude" binary from PATH and enables
// SkipPermissions (required for non-interactive pipeline execution).
func NewClaudeCodeBackend(opts ...ClaudeCodeOption) (*ClaudeCodeBackend, error) {
	b := &ClaudeCodeBackend{
		SkipPermissions: true,
	}

	for _, opt := range opts {
		opt(b)
	}

	// Resolve binary path
	if b.BinaryPath == "" {
		path, err := exec.LookPath("claude")
		if err != nil {
			return nil, fmt.Errorf("claude binary not found in PATH: %w", err)
		}
		b.BinaryPath = path
	} else {
		// Verify the provided path exists
		if _, err := os.Stat(b.BinaryPath); err != nil {
			return nil, fmt.Errorf("claude binary not found at %q: %w", b.BinaryPath, err)
		}
	}

	return b, nil
}

// RunAgent executes the claude CLI with the given configuration and parses
// the streaming JSONL output to build an AgentRunResult.
func (b *ClaudeCodeBackend) RunAgent(ctx context.Context, config AgentRunConfig) (*AgentRunResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Build the user input from the prompt and goal (reuses the same
	// formatting logic as AgentBackend for consistency)
	userInput := buildAgentInput(config.Prompt, config.Goal, config.NodeID)

	args := b.buildArgs(userInput, config)

	cmd := exec.CommandContext(ctx, b.BinaryPath, args...)

	// Set process group so we can kill the entire tree on cancellation
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// When context expires, kill the entire process group.
	// cmd.Cancel is only called by Go after Start() succeeds, so
	// cmd.Process is guaranteed non-nil here.
	cmd.Cancel = func() error {
		killProcessGroup(cmd)
		return cmd.Process.Kill()
	}
	cmd.WaitDelay = 3 * time.Second

	// Set working directory
	if config.WorkDir != "" {
		cmd.Dir = config.WorkDir
	}

	// Inherit environment for ANTHROPIC_API_KEY, PATH, etc.
	cmd.Env = os.Environ()

	// Capture stdout for JSONL parsing, stderr for diagnostics
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start claude process: %w", err)
	}

	// Parse streaming JSONL output line by line.
	// The --verbose flag is required when using --output-format stream-json with
	// --print. It also causes additional system events (hook_started, etc.) to
	// appear in the stream, which are harmlessly ignored by the switch below.
	var resultEvent *claudeStreamEvent
	var lastAssistantText string
	var toolCallCount int
	scanner := bufio.NewScanner(stdout)
	// Allow large lines (some assistant content can be very long)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		evt, parseErr := parseClaudeStreamEvent(line)
		if parseErr != nil {
			// Skip unparseable lines (e.g. progress indicators)
			continue
		}

		switch evt.Type {
		case "system":
			// Log session ID for traceability. Additional system events
			// (hook_started, hook_response) from --verbose are harmless no-ops.
			if config.EventHandler != nil && evt.SessionID != "" {
				config.EventHandler(EngineEvent{
					Type:      EventStageStarted,
					NodeID:    config.NodeID,
					Timestamp: time.Now(),
					Data: map[string]any{
						"claude_session_id": evt.SessionID,
					},
				})
			}

		case "assistant":
			// Track the latest assistant text and emit tool call events.
			// The claude CLI stream-json format only emits assistant events
			// with the full message, not individual tool_call_end events, so
			// we emit EventAgentToolCallStart for each tool_use block. There
			// is no separate EventAgentToolCallEnd in this stream format.
			if evt.Message != nil {
				for _, block := range evt.Message.Content {
					if block.Type == "text" && block.Text != "" {
						lastAssistantText = block.Text
					}
					if block.Type == "tool_use" {
						toolCallCount++
						if config.EventHandler != nil {
							config.EventHandler(EngineEvent{
								Type:      EventAgentToolCallStart,
								NodeID:    config.NodeID,
								Timestamp: time.Now(),
								Data: map[string]any{
									"tool_name": block.Name,
									"call_id":   block.ID,
								},
							})
						}
					}
				}

				// Emit an LLM turn event when we see an assistant message
				if config.EventHandler != nil {
					config.EventHandler(EngineEvent{
						Type:      EventAgentLLMTurn,
						NodeID:    config.NodeID,
						Timestamp: time.Now(),
						Data: map[string]any{
							"text_length":   len(lastAssistantText),
							"has_reasoning": false,
						},
					})
				}
			}

		case "result":
			resultEvent = evt
			// Emit a final LLM turn event with token usage from the result.
			// The claude CLI only reports total token counts in the result event,
			// not per-turn, so we emit them here as a single aggregate.
			if config.EventHandler != nil && evt.Usage != nil {
				config.EventHandler(EngineEvent{
					Type:      EventAgentLLMTurn,
					NodeID:    config.NodeID,
					Timestamp: time.Now(),
					Data: map[string]any{
						"input_tokens":  evt.Usage.InputTokens,
						"output_tokens": evt.Usage.OutputTokens,
					},
				})
			}
		}
	}

	// Surface scanner errors (e.g. ErrTooLong from oversized JSONL lines)
	if scanErr := scanner.Err(); scanErr != nil && resultEvent == nil {
		return nil, fmt.Errorf("reading claude output: %w", scanErr)
	}

	// Wait for the process to finish
	waitErr := cmd.Wait()

	// Build result from the parsed events
	if resultEvent != nil {
		usage := claudeUsageToTokenUsage(resultEvent.Usage)
		success := claudeResultToSuccess(resultEvent.Result, resultEvent.IsError)

		return &AgentRunResult{
			Output:     resultEvent.Result,
			Success:    success,
			TurnCount:  resultEvent.NumTurns,
			ToolCalls:  toolCallCount,
			TokensUsed: usage.TotalTokens,
			Usage:      usage,
		}, nil
	}

	// No result event parsed — fall back to error handling
	if waitErr != nil {
		exitCode := extractExitCode(waitErr)
		return nil, fmt.Errorf("claude process exited with code %d: %s", exitCode, stderrBuf.String())
	}

	// Process completed but produced no result event
	return &AgentRunResult{
		Output:  lastAssistantText,
		Success: lastAssistantText != "",
	}, nil
}

// buildArgs constructs the argument list for the claude CLI invocation.
// The userInput parameter is the formatted prompt from buildAgentInput.
func (b *ClaudeCodeBackend) buildArgs(userInput string, config AgentRunConfig) []string {
	// --verbose is required when using --output-format stream-json with --print.
	// Without it, the CLI returns an error.
	args := []string{
		"--print",
		"--verbose",
		"--output-format", "stream-json",
		"--no-session-persistence",
	}

	if b.SkipPermissions {
		args = append(args, "--dangerously-skip-permissions")
	}

	// Model: config overrides default
	model := config.Model
	if model == "" {
		model = b.DefaultModel
	}
	if model != "" {
		args = append(args, "--model", model)
	}

	// The claude CLI does not have a --max-turns flag. We use --max-budget-usd
	// instead when configured for cost control.
	if b.MaxBudgetUSD > 0 {
		args = append(args, "--max-budget-usd", fmt.Sprintf("%.2f", b.MaxBudgetUSD))
	}

	if len(b.AllowedTools) > 0 {
		args = append(args, "--allowedTools", strings.Join(b.AllowedTools, ","))
	}

	if b.AppendSystemPrompt != "" {
		args = append(args, "--append-system-prompt", b.AppendSystemPrompt)
	}

	// Prompt is the final positional argument
	args = append(args, userInput)

	return args
}

// --- JSONL event types ---

// claudeStreamEvent represents a single line of JSONL output from the claude CLI
// when using --output-format stream-json. The exact fields populated depend on
// the event type.
type claudeStreamEvent struct {
	Type      string              `json:"type"`
	Subtype   string              `json:"subtype,omitempty"`
	SessionID string              `json:"session_id,omitempty"`
	Result    string              `json:"result,omitempty"`
	IsError   bool                `json:"is_error,omitempty"`
	NumTurns  int                 `json:"num_turns,omitempty"`
	Usage     *claudeUsage        `json:"usage,omitempty"`
	Message   *claudeMessageBlock `json:"message,omitempty"`
	CostUSD   float64             `json:"total_cost_usd,omitempty"`
}

// claudeUsage tracks token consumption from the claude CLI result event.
type claudeUsage struct {
	InputTokens         int `json:"input_tokens"`
	OutputTokens        int `json:"output_tokens"`
	CacheReadTokens     int `json:"cache_read_input_tokens"`
	CacheCreationTokens int `json:"cache_creation_input_tokens"`
	ThinkingTokens      int `json:"thinking_tokens"`
}

// claudeMessageBlock represents the message field in assistant events.
type claudeMessageBlock struct {
	Role    string              `json:"role"`
	Content []claudeContentPart `json:"content"`
}

// claudeContentPart represents a single content block in an assistant message.
type claudeContentPart struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

// parseClaudeStreamEvent parses a single line of JSONL output from the claude CLI.
func parseClaudeStreamEvent(line []byte) (*claudeStreamEvent, error) {
	var evt claudeStreamEvent
	if err := json.Unmarshal(line, &evt); err != nil {
		return nil, fmt.Errorf("parse claude stream event: %w", err)
	}
	return &evt, nil
}

// claudeUsageToTokenUsage converts claude CLI usage stats to our TokenUsage struct.
func claudeUsageToTokenUsage(usage *claudeUsage) TokenUsage {
	if usage == nil {
		return TokenUsage{}
	}
	return TokenUsage{
		InputTokens:      usage.InputTokens,
		OutputTokens:     usage.OutputTokens,
		TotalTokens:      usage.InputTokens + usage.OutputTokens + usage.ThinkingTokens,
		ReasoningTokens:  usage.ThinkingTokens,
		CacheReadTokens:  usage.CacheReadTokens,
		CacheWriteTokens: usage.CacheCreationTokens,
	}
}

// claudeResultToSuccess determines the Success flag from the claude CLI result.
// OUTCOME:FAIL in the result text always means failure. If is_error is true,
// success is false. Otherwise, success defaults to true (matching the behavior
// of extractResult in backend_agent.go).
func claudeResultToSuccess(resultText string, isError bool) bool {
	if isError {
		return false
	}
	if strings.Contains(resultText, "OUTCOME:FAIL") {
		return false
	}
	return true
}

// Compile-time interface check
var _ CodergenBackend = (*ClaudeCodeBackend)(nil)
