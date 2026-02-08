// ABOUTME: Subagent system for spawning child sessions that run their own agentic loops.
// ABOUTME: Provides SubAgentManager, lifecycle management, and tool constructors for spawn, send_input, wait, and close.

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/2389-research/makeatron/llm"
	"github.com/google/uuid"
)

// SubAgentStatus represents the lifecycle state of a subagent.
type SubAgentStatus string

const (
	SubAgentRunning   SubAgentStatus = "running"
	SubAgentCompleted SubAgentStatus = "completed"
	SubAgentFailed    SubAgentStatus = "failed"
)

// SubAgentHandle holds a reference to a spawned subagent.
type SubAgentHandle struct {
	ID      string
	Session *Session
	Status  SubAgentStatus
	Env     ExecutionEnvironment
	Profile ProviderProfile
	Client  *llm.Client
	cancel  context.CancelFunc
	result  *SubAgentResult
	done    chan struct{}
	mu      sync.Mutex
}

// SubAgentResult is the output from a completed subagent.
type SubAgentResult struct {
	Output    string `json:"output"`
	Success   bool   `json:"success"`
	TurnsUsed int    `json:"turns_used"`
}

// SubAgentManager manages the lifecycle of spawned subagents.
type SubAgentManager struct {
	agents   map[string]*SubAgentHandle
	mu       sync.Mutex
	depth    int
	maxDepth int
}

// NewSubAgentManager creates a new SubAgentManager with the given depth constraints.
func NewSubAgentManager(currentDepth, maxDepth int) *SubAgentManager {
	return &SubAgentManager{
		agents:   make(map[string]*SubAgentHandle),
		depth:    currentDepth,
		maxDepth: maxDepth,
	}
}

// Spawn creates a new subagent session, starts processing the task in a goroutine,
// and returns the agent's handle. Returns error if depth limit exceeded.
func (m *SubAgentManager) Spawn(ctx context.Context, task string, env ExecutionEnvironment, profile ProviderProfile, client *llm.Client, maxTurns int) (*SubAgentHandle, error) {
	m.mu.Lock()
	if m.depth >= m.maxDepth {
		m.mu.Unlock()
		return nil, fmt.Errorf("subagent depth limit exceeded: current depth %d, max depth %d", m.depth, m.maxDepth)
	}
	m.mu.Unlock()

	// Create a child session with reduced subagent depth allowance
	childConfig := DefaultSessionConfig()
	childConfig.MaxTurns = maxTurns
	childConfig.MaxSubagentDepth = 0 // Children cannot spawn further subagents by default

	childSession := NewSession(childConfig)

	childCtx, cancel := context.WithCancel(ctx)

	handle := &SubAgentHandle{
		ID:      uuid.New().String(),
		Session: childSession,
		Status:  SubAgentRunning,
		Env:     env,
		Profile: profile,
		Client:  client,
		cancel:  cancel,
		done:    make(chan struct{}),
	}

	m.mu.Lock()
	m.agents[handle.ID] = handle
	m.mu.Unlock()

	// Run the agentic loop in a goroutine
	go func() {
		defer close(handle.done)

		err := ProcessInput(childCtx, childSession, profile, env, client, task)

		handle.mu.Lock()
		defer handle.mu.Unlock()

		// Extract the output from the last AssistantTurn in the child session's history
		output := extractLastAssistantOutput(childSession)
		turnsUsed := childSession.TurnCount()

		if err != nil {
			handle.Status = SubAgentFailed
			handle.result = &SubAgentResult{
				Output:    output,
				Success:   false,
				TurnsUsed: turnsUsed,
			}
		} else {
			handle.Status = SubAgentCompleted
			handle.result = &SubAgentResult{
				Output:    output,
				Success:   true,
				TurnsUsed: turnsUsed,
			}
		}
	}()

	return handle, nil
}

// extractLastAssistantOutput walks the session history backwards to find the last
// AssistantTurn and returns its Content.
func extractLastAssistantOutput(session *Session) string {
	session.mu.Lock()
	defer session.mu.Unlock()

	for i := len(session.History) - 1; i >= 0; i-- {
		if at, ok := session.History[i].(AssistantTurn); ok {
			return at.Content
		}
	}
	return ""
}

// Get returns the handle for a given agent ID.
func (m *SubAgentManager) Get(agentID string) (*SubAgentHandle, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	handle, ok := m.agents[agentID]
	return handle, ok
}

// SendInput sends a message to a running subagent via the steering queue.
func (m *SubAgentManager) SendInput(agentID, message string) error {
	m.mu.Lock()
	handle, ok := m.agents[agentID]
	m.mu.Unlock()

	if !ok {
		return fmt.Errorf("agent %q not found", agentID)
	}

	handle.mu.Lock()
	status := handle.Status
	handle.mu.Unlock()

	if status != SubAgentRunning {
		return fmt.Errorf("agent %q is not running (status: %s)", agentID, status)
	}

	handle.Session.Steer(message)
	return nil
}

// Wait blocks until the subagent completes and returns its result.
func (m *SubAgentManager) Wait(agentID string) (*SubAgentResult, error) {
	m.mu.Lock()
	handle, ok := m.agents[agentID]
	m.mu.Unlock()

	if !ok {
		return nil, fmt.Errorf("agent %q not found", agentID)
	}

	// Block until the goroutine completes
	<-handle.done

	handle.mu.Lock()
	defer handle.mu.Unlock()
	return handle.result, nil
}

// Close terminates a subagent by cancelling its context.
func (m *SubAgentManager) Close(agentID string) error {
	m.mu.Lock()
	handle, ok := m.agents[agentID]
	m.mu.Unlock()

	if !ok {
		return fmt.Errorf("agent %q not found", agentID)
	}

	// Cancel the context to signal the goroutine to stop
	handle.cancel()

	// Wait for the goroutine to finish
	<-handle.done

	return nil
}

// CloseAll terminates all running subagents.
func (m *SubAgentManager) CloseAll() {
	m.mu.Lock()
	handles := make([]*SubAgentHandle, 0, len(m.agents))
	for _, h := range m.agents {
		handles = append(handles, h)
	}
	m.mu.Unlock()

	// Cancel all contexts first
	for _, h := range handles {
		h.cancel()
	}

	// Wait for all goroutines to finish
	for _, h := range handles {
		<-h.done
	}
}

// --- Tool Constructors ---

// NewSpawnAgentTool creates the spawn_agent tool. The manager, profile, and client
// are captured in the closure.
func NewSpawnAgentTool(manager *SubAgentManager, profile ProviderProfile, client *llm.Client) *RegisteredTool {
	return &RegisteredTool{
		Definition: llm.ToolDefinition{
			Name:        "spawn_agent",
			Description: "Spawn a subagent to handle a scoped task autonomously.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"task": {
						"type": "string",
						"description": "Natural language task description for the subagent"
					},
					"working_dir": {
						"type": "string",
						"description": "Subdirectory to scope the agent to (optional)"
					},
					"model": {
						"type": "string",
						"description": "Model override (optional, default: parent's model)"
					},
					"max_turns": {
						"type": "integer",
						"description": "Turn limit for the subagent (default: 50)"
					}
				},
				"required": ["task"]
			}`),
		},
		Description: "Spawn a subagent to handle a scoped task autonomously.",
		Execute: func(args map[string]any, env ExecutionEnvironment) (string, error) {
			task, err := getStringArg(args, "task", true)
			if err != nil {
				return "", err
			}

			maxTurns, err := getIntArg(args, "max_turns", 50)
			if err != nil {
				return "", err
			}

			// working_dir is optional; if provided, we note it but the env is shared
			_, _ = getStringArg(args, "working_dir", false)

			handle, err := manager.Spawn(context.Background(), task, env, profile, client, maxTurns)
			if err != nil {
				return "", err
			}

			result := map[string]any{
				"agent_id": handle.ID,
				"status":   string(SubAgentRunning),
			}
			resultJSON, err := json.Marshal(result)
			if err != nil {
				return "", fmt.Errorf("marshalling spawn result: %w", err)
			}
			return string(resultJSON), nil
		},
	}
}

// NewSendInputTool creates the send_input tool.
func NewSendInputTool(manager *SubAgentManager) *RegisteredTool {
	return &RegisteredTool{
		Definition: llm.ToolDefinition{
			Name:        "send_input",
			Description: "Send a message to a running subagent.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"agent_id": {
						"type": "string",
						"description": "ID of the subagent to send the message to"
					},
					"message": {
						"type": "string",
						"description": "Message to send to the subagent"
					}
				},
				"required": ["agent_id", "message"]
			}`),
		},
		Description: "Send a message to a running subagent.",
		Execute: func(args map[string]any, env ExecutionEnvironment) (string, error) {
			agentID, err := getStringArg(args, "agent_id", true)
			if err != nil {
				return "", err
			}

			message, err := getStringArg(args, "message", true)
			if err != nil {
				return "", err
			}

			err = manager.SendInput(agentID, message)
			if err != nil {
				return fmt.Sprintf("Error: %s", err.Error()), nil
			}

			return fmt.Sprintf("Message sent to agent %s. Steering message queued.", agentID), nil
		},
	}
}

// NewWaitTool creates the wait tool.
func NewWaitTool(manager *SubAgentManager) *RegisteredTool {
	return &RegisteredTool{
		Definition: llm.ToolDefinition{
			Name:        "wait",
			Description: "Wait for a subagent to complete and return its result.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"agent_id": {
						"type": "string",
						"description": "ID of the subagent to wait for"
					}
				},
				"required": ["agent_id"]
			}`),
		},
		Description: "Wait for a subagent to complete and return its result.",
		Execute: func(args map[string]any, env ExecutionEnvironment) (string, error) {
			agentID, err := getStringArg(args, "agent_id", true)
			if err != nil {
				return "", err
			}

			result, err := manager.Wait(agentID)
			if err != nil {
				return fmt.Sprintf("Error: %s", err.Error()), nil
			}

			resultJSON, err := json.Marshal(result)
			if err != nil {
				return "", fmt.Errorf("marshalling wait result: %w", err)
			}
			return string(resultJSON), nil
		},
	}
}

// NewCloseAgentTool creates the close_agent tool.
func NewCloseAgentTool(manager *SubAgentManager) *RegisteredTool {
	return &RegisteredTool{
		Definition: llm.ToolDefinition{
			Name:        "close_agent",
			Description: "Terminate a subagent.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"agent_id": {
						"type": "string",
						"description": "ID of the subagent to terminate"
					}
				},
				"required": ["agent_id"]
			}`),
		},
		Description: "Terminate a subagent.",
		Execute: func(args map[string]any, env ExecutionEnvironment) (string, error) {
			agentID, err := getStringArg(args, "agent_id", true)
			if err != nil {
				return "", err
			}

			err = manager.Close(agentID)
			if err != nil {
				return fmt.Sprintf("Error: %s", err.Error()), nil
			}

			// Get the final status
			handle, ok := manager.Get(agentID)
			status := "terminated"
			if ok {
				handle.mu.Lock()
				status = string(handle.Status)
				handle.mu.Unlock()
			}

			result := map[string]any{
				"agent_id": agentID,
				"status":   status,
			}
			resultJSON, err := json.Marshal(result)
			if err != nil {
				return "", fmt.Errorf("marshalling close result: %w", err)
			}
			return string(resultJSON), nil
		},
	}
}

// RegisterSubAgentTools registers all 4 subagent tools on the given registry.
func RegisterSubAgentTools(registry *ToolRegistry, manager *SubAgentManager, profile ProviderProfile, client *llm.Client) {
	registry.Register(NewSpawnAgentTool(manager, profile, client))
	registry.Register(NewSendInputTool(manager))
	registry.Register(NewWaitTool(manager))
	registry.Register(NewCloseAgentTool(manager))
}
