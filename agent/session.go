// ABOUTME: Session management for the coding agent loop, including turn types, config, and loop detection.
// ABOUTME: Provides Session struct with history, steering/followup queues, and ConvertHistoryToMessages.

package agent

import (
	"crypto/sha256"
	"fmt"
	"sync"
	"time"

	"github.com/2389-research/makeatron/llm"
	"github.com/google/uuid"
)

// SessionState represents the lifecycle state of a session.
type SessionState string

const (
	StateIdle          SessionState = "idle"
	StateProcessing    SessionState = "processing"
	StateAwaitingInput SessionState = "awaiting_input"
	StateClosed        SessionState = "closed"
)

// SessionConfig holds configuration for a session.
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
}

// DefaultSessionConfig returns a SessionConfig with spec-defined defaults.
func DefaultSessionConfig() SessionConfig {
	return SessionConfig{
		MaxTurns:                0,
		MaxToolRoundsPerInput:   200,
		DefaultCommandTimeoutMs: 10000,
		MaxCommandTimeoutMs:     600000,
		EnableLoopDetection:     true,
		LoopDetectionWindow:     10,
		MaxSubagentDepth:        1,
		ToolOutputLimits:        make(map[string]int),
	}
}

// Turn is the interface implemented by all conversation turn types.
type Turn interface {
	// TurnType returns a string discriminator: "user", "assistant", "tool_results", "system", or "steering".
	TurnType() string

	// TurnTimestamp returns the time when the turn was created.
	TurnTimestamp() time.Time
}

// UserTurn represents a user-submitted message.
type UserTurn struct {
	Content   string
	Timestamp time.Time
}

func (t UserTurn) TurnType() string        { return "user" }
func (t UserTurn) TurnTimestamp() time.Time { return t.Timestamp }

// AssistantTurn represents the model's response, optionally including tool calls.
type AssistantTurn struct {
	Content    string
	ToolCalls  []llm.ToolCallData
	Reasoning  string
	Usage      llm.Usage
	ResponseID string
	Timestamp  time.Time
}

func (t AssistantTurn) TurnType() string        { return "assistant" }
func (t AssistantTurn) TurnTimestamp() time.Time { return t.Timestamp }

// ToolResultsTurn holds results from executing one or more tool calls.
type ToolResultsTurn struct {
	Results   []llm.ToolResult
	Timestamp time.Time
}

func (t ToolResultsTurn) TurnType() string        { return "tool_results" }
func (t ToolResultsTurn) TurnTimestamp() time.Time { return t.Timestamp }

// SystemTurn represents a system-level message in the conversation.
type SystemTurn struct {
	Content   string
	Timestamp time.Time
}

func (t SystemTurn) TurnType() string        { return "system" }
func (t SystemTurn) TurnTimestamp() time.Time { return t.Timestamp }

// SteeringTurn represents an injected steering message from the host application.
type SteeringTurn struct {
	Content   string
	Timestamp time.Time
}

func (t SteeringTurn) TurnType() string        { return "steering" }
func (t SteeringTurn) TurnTimestamp() time.Time { return t.Timestamp }

// Session is the central orchestrator for the coding agent loop.
// It holds conversation state, manages queues, and dispatches events.
type Session struct {
	ID            string
	Config        SessionConfig
	History       []Turn
	State         SessionState
	EventEmitter  *EventEmitter
	steeringQueue []string
	followupQueue []string
	mu            sync.Mutex
}

// NewSession creates a new Session with a generated UUID and the given configuration.
func NewSession(config SessionConfig) *Session {
	return &Session{
		ID:            uuid.New().String(),
		Config:        config,
		History:       make([]Turn, 0),
		State:         StateIdle,
		EventEmitter:  NewEventEmitter(),
		steeringQueue: make([]string, 0),
		followupQueue: make([]string, 0),
	}
}

// Emit emits a session event with the given kind and data, auto-populating
// the session ID and timestamp.
func (s *Session) Emit(kind EventKind, data map[string]any) {
	s.EventEmitter.Emit(SessionEvent{
		Kind:      kind,
		Timestamp: time.Now(),
		SessionID: s.ID,
		Data:      data,
	})
}

// SetState transitions the session to the given state.
func (s *Session) SetState(state SessionState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.State = state
}

// Steer queues a steering message to be injected after the current tool round.
func (s *Session) Steer(message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.steeringQueue = append(s.steeringQueue, message)
}

// FollowUp queues a follow-up message to be processed after the current input completes.
func (s *Session) FollowUp(message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.followupQueue = append(s.followupQueue, message)
}

// DrainSteering removes and returns all pending steering messages.
func (s *Session) DrainSteering() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.steeringQueue) == 0 {
		return nil
	}
	messages := s.steeringQueue
	s.steeringQueue = make([]string, 0)
	return messages
}

// DrainFollowup removes and returns the first pending follow-up message.
// Returns an empty string if the queue is empty.
func (s *Session) DrainFollowup() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.followupQueue) == 0 {
		return ""
	}
	msg := s.followupQueue[0]
	s.followupQueue = s.followupQueue[1:]
	return msg
}

// AppendTurn adds a turn to the session history.
func (s *Session) AppendTurn(turn Turn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.History = append(s.History, turn)
}

// TurnCount returns the number of turns in the session history.
func (s *Session) TurnCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.History)
}

// Close transitions the session to StateClosed and closes the event emitter.
func (s *Session) Close() {
	s.mu.Lock()
	s.State = StateClosed
	s.mu.Unlock()
	s.EventEmitter.Close()
}

// ConvertHistoryToMessages converts a slice of Turn values into LLM messages
// suitable for sending to a language model.
func ConvertHistoryToMessages(history []Turn) []llm.Message {
	messages := make([]llm.Message, 0, len(history))

	for _, turn := range history {
		switch t := turn.(type) {
		case SystemTurn:
			messages = append(messages, llm.SystemMessage(t.Content))

		case UserTurn:
			messages = append(messages, llm.UserMessage(t.Content))

		case AssistantTurn:
			parts := make([]llm.ContentPart, 0)
			if t.Content != "" {
				parts = append(parts, llm.TextPart(t.Content))
			}
			for _, tc := range t.ToolCalls {
				parts = append(parts, llm.ToolCallPart(tc.ID, tc.Name, tc.Arguments))
			}
			messages = append(messages, llm.Message{
				Role:    llm.RoleAssistant,
				Content: parts,
			})

		case ToolResultsTurn:
			for _, result := range t.Results {
				messages = append(messages, llm.ToolResultMessage(
					result.ToolCallID,
					result.Content,
					result.IsError,
				))
			}

		case SteeringTurn:
			// Steering messages are presented to the LLM as user-role messages
			messages = append(messages, llm.UserMessage(t.Content))
		}
	}

	return messages
}

// DetectLoop checks whether the recent tool call history contains a repeating
// pattern of length 1, 2, or 3. It extracts tool call signatures (name + args hash)
// from the last windowSize assistant turns that contain tool calls.
func DetectLoop(history []Turn, windowSize int) bool {
	signatures := ExtractToolCallSignatures(history, windowSize)
	if len(signatures) < windowSize {
		return false
	}

	// Check for repeating patterns of length 1, 2, or 3
	for patternLen := 1; patternLen <= 3; patternLen++ {
		if windowSize%patternLen != 0 {
			continue
		}
		pattern := signatures[:patternLen]
		allMatch := true
		for i := patternLen; i < windowSize; i += patternLen {
			for j := 0; j < patternLen; j++ {
				if signatures[i+j] != pattern[j] {
					allMatch = false
					break
				}
			}
			if !allMatch {
				break
			}
		}
		if allMatch {
			return true
		}
	}

	return false
}

// ExtractToolCallSignatures extracts the last `count` tool call signatures
// from the history. A signature is "name:sha256(arguments)" for each tool call
// found in AssistantTurn entries.
func ExtractToolCallSignatures(history []Turn, count int) []string {
	var signatures []string

	// Walk history backwards to collect the most recent tool call signatures
	for i := len(history) - 1; i >= 0 && len(signatures) < count; i-- {
		if at, ok := history[i].(AssistantTurn); ok {
			for _, tc := range at.ToolCalls {
				hash := sha256.Sum256(tc.Arguments)
				sig := fmt.Sprintf("%s:%x", tc.Name, hash[:8])
				signatures = append(signatures, sig)
			}
		}
	}

	// Reverse so signatures are in chronological order
	for i, j := 0, len(signatures)-1; i < j; i, j = i+1, j-1 {
		signatures[i], signatures[j] = signatures[j], signatures[i]
	}

	// If we collected more than count, take only the last count
	if len(signatures) > count {
		signatures = signatures[len(signatures)-count:]
	}

	return signatures
}
