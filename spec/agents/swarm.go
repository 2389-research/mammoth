// ABOUTME: SwarmOrchestrator manages multiple agents per spec using mux Agent for LLM execution.
// ABOUTME: Each agent runs in round-robin, coordinated by pause/resume flags and event subscriptions.
package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	muxagent "github.com/2389-research/mux/agent"
	"github.com/2389-research/mux/llm"

	"github.com/2389-research/mammoth/spec/agents/tools"
	"github.com/2389-research/mammoth/spec/core"
	"github.com/oklog/ulid/v2"
)

// AgentRunner wraps a single agent's role and mutable context.
// The mu field protects concurrent access to Context fields.
type AgentRunner struct {
	Role    AgentRole
	Context *AgentContext
	AgentID string
	mu      sync.RWMutex
}

// NewAgentRunner creates a new runner for the given role with a unique agent ID.
func NewAgentRunner(specID ulid.ULID, role AgentRole) *AgentRunner {
	agentID := fmt.Sprintf("%s-%s", role.Label(), core.NewULID().String())
	ctx := NewAgentContext(specID, agentID, role)
	return &AgentRunner{
		Role:    role,
		Context: ctx,
		AgentID: agentID,
	}
}

// SwarmOrchestrator orchestrates a swarm of agents working on a single spec.
// Manages the agent loop, action routing, pause/resume, and question queue.
type SwarmOrchestrator struct {
	SpecID ulid.ULID
	Actor  *core.SpecActorHandle
	Agents []*AgentRunner

	// Per-agent event channels for independent event draining.
	eventChannels []chan core.Event

	Paused          atomic.Bool
	QuestionPending atomic.Bool

	Client llm.Client
	Model  string

	// HumanMessageNotify signals that a human message has arrived;
	// wakes the RunLoop from its idle sleep so the manager agent responds promptly.
	HumanMessageNotify chan struct{}

	mu sync.Mutex
}

// NewSwarmOrchestrator creates a new orchestrator with 4 default agents
// (Manager, Brainstormer, Planner, DotGenerator).
func NewSwarmOrchestrator(
	specID ulid.ULID,
	actor *core.SpecActorHandle,
	client llm.Client,
	model string,
) *SwarmOrchestrator {
	roles := []AgentRole{RoleManager, RoleBrainstormer, RolePlanner, RoleDotGenerator}

	agents := make([]*AgentRunner, len(roles))
	eventChannels := make([]chan core.Event, len(roles))
	for i, role := range roles {
		agents[i] = NewAgentRunner(specID, role)
		eventChannels[i] = actor.Subscribe()
	}

	return &SwarmOrchestrator{
		SpecID:             specID,
		Actor:              actor,
		Agents:             agents,
		eventChannels:      eventChannels,
		Client:             client,
		Model:              model,
		HumanMessageNotify: make(chan struct{}, 1),
	}
}

// AgentCount returns the number of agent slots in this swarm.
func (s *SwarmOrchestrator) AgentCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.Agents)
}

// Pause all agent loops. Agents will complete their current step
// but won't start new ones.
func (s *SwarmOrchestrator) Pause() {
	s.Paused.Store(true)
	log.Printf("component=spec.swarm action=paused spec_id=%s", s.SpecID)
}

// Resume agent loops.
func (s *SwarmOrchestrator) Resume() {
	s.Paused.Store(false)
	log.Printf("component=spec.swarm action=resumed spec_id=%s", s.SpecID)
}

// IsPaused returns true if the swarm is currently paused.
func (s *SwarmOrchestrator) IsPaused() bool {
	return s.Paused.Load()
}

// HasPendingQuestion returns true if a question is currently pending.
func (s *SwarmOrchestrator) HasPendingQuestion() bool {
	return s.QuestionPending.Load()
}

// NotifyHumanMessage signals that a human message has arrived so the RunLoop
// wakes from its idle sleep and prioritises the manager agent.
func (s *SwarmOrchestrator) NotifyHumanMessage() {
	select {
	case s.HumanMessageNotify <- struct{}{}:
	default:
		// Already signalled, don't block
	}
}

// RecoverEmptySlots re-creates any nil agent runner slots with fresh runners.
func (s *SwarmOrchestrator) RecoverEmptySlots() {
	s.mu.Lock()
	defer s.mu.Unlock()

	defaultRoles := []AgentRole{RoleManager, RoleBrainstormer, RolePlanner, RoleDotGenerator}
	for i := range s.Agents {
		if s.Agents[i] == nil && i < len(defaultRoles) {
			log.Printf("component=spec.swarm action=recover_slot slot=%d role=%s spec_id=%s", i, defaultRoles[i].Label(), s.SpecID)
			s.Agents[i] = NewAgentRunner(s.SpecID, defaultRoles[i])
			s.eventChannels[i] = s.Actor.Subscribe()
		}
	}
}

// CollectAgentContexts returns a snapshot map of all agent contexts for persistence.
// Acquires read locks on each runner to safely read Context fields.
func (s *SwarmOrchestrator) CollectAgentContexts() map[string]json.RawMessage {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := make(map[string]json.RawMessage, len(s.Agents))
	for _, runner := range s.Agents {
		if runner != nil {
			runner.mu.RLock()
			result[runner.Context.AgentID] = runner.Context.ToSnapshotValue()
			runner.mu.RUnlock()
		}
	}
	return result
}

// RestoreAgentContexts restores agent contexts from a snapshot map.
// Matches by AgentRole, since agent_ids may differ between sessions.
// Acquires write locks on each runner to safely mutate Context fields.
func (s *SwarmOrchestrator) RestoreAgentContexts(m map[string]json.RawMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()

	restored := ContextsFromSnapshotMap(m)
	for _, ctx := range restored {
		for _, runner := range s.Agents {
			if runner != nil && runner.Role == ctx.AgentRole {
				runner.mu.Lock()
				runner.Context.RollingSummary = ctx.RollingSummary
				runner.Context.KeyDecisions = ctx.KeyDecisions
				runner.Context.LastEventSeen = ctx.LastEventSeen
				runner.mu.Unlock()
			}
		}
	}
}

// RefreshContext drains buffered events for the agent at index, reads the current state,
// and updates the agent's context accordingly. Acquires a write lock on the runner
// to prevent races with concurrent snapshot reads.
func (s *SwarmOrchestrator) RefreshContext(runner *AgentRunner, eventCh chan core.Event) {
	// Drain buffered events before acquiring the lock
	var events []core.Event
	for {
		select {
		case event := <-eventCh:
			events = append(events, event)
		default:
			goto drained
		}
	}
drained:

	runner.mu.Lock()
	runner.Context.UpdateFromEvents(events)
	runner.Context.RecentEvents = events
	runner.mu.Unlock()

	// Read current state for the summary
	s.Actor.ReadState(func(state *core.SpecState) {
		runner.mu.Lock()
		defer runner.mu.Unlock()

		if state.Core != nil {
			runner.Context.StateSummary = fmt.Sprintf(
				"Title: %s. Goal: %s. Cards: %d. Pending question: %t",
				state.Core.Title,
				state.Core.Goal,
				state.Cards.Len(),
				state.PendingQuestion != nil,
			)
		}

		// Sync question pending flag from actor state
		s.QuestionPending.Store(state.PendingQuestion != nil)

		// Copy recent transcript (last 10 messages)
		transcriptLen := len(state.Transcript)
		start := transcriptLen - 10
		if start < 0 {
			start = 0
		}
		runner.Context.RecentTranscript = make([]core.TranscriptMessage, transcriptLen-start)
		copy(runner.Context.RecentTranscript, state.Transcript[start:])
	})
}

// RunAgentStep runs a single agent step using a mux Agent.
// Creates a fresh Agent with the domain tool registry, sends it the
// agent's context as a task prompt, and lets mux handle the think-act loop.
// Returns true if the agent produced useful work, false if idle/error.
func (s *SwarmOrchestrator) RunAgentStep(ctx context.Context, runner *AgentRunner) bool {
	// Start agent step
	startCmd := core.StartAgentStepCommand{
		AgentID:     runner.AgentID,
		Description: fmt.Sprintf("%s reasoning step", runner.Role.Label()),
	}
	if _, err := s.Actor.SendCommand(startCmd); err != nil {
		log.Printf("component=spec.agent action=start_step_failed agent_id=%s role=%s err=%v", runner.AgentID, runner.Role.Label(), err)
	}

	// Build tool registry for this agent. The stepFinished flag is set by
	// emit_diff_summary if the agent calls it, so we skip the fallback below.
	var stepFinished atomic.Bool
	registry := tools.BuildRegistry(s.Actor, &s.QuestionPending, runner.AgentID, &stepFinished)

	// Create agent with role-specific system prompt + tool guide
	agentCfg := muxagent.Config{
		Name:          runner.Role.Label(),
		Registry:      registry,
		LLMClient:     s.Client,
		SystemPrompt:  FullSystemPrompt(runner.Role, runner.AgentID),
		MaxIterations: 10,
	}
	muxAgent := muxagent.New(agentCfg)

	// Build task prompt from context
	taskPrompt := BuildTaskPrompt(runner.Context)

	// Run the agent
	if err := muxAgent.Run(ctx, taskPrompt); err != nil {
		log.Printf("component=spec.agent action=step_failed agent_id=%s role=%s err=%v", runner.AgentID, runner.Role.Label(), err)
		// Post a sanitized error message to the transcript
		userMsg := fmt.Sprintf("[%s] encountered an issue and will retry on the next cycle.", runner.Role.Label())
		_, _ = s.Actor.SendCommand(core.AppendTranscriptCommand{
			Sender:  runner.AgentID,
			Content: userMsg,
		})
		// Mark the step as finished even on failure so UI updates
		_, _ = s.Actor.SendCommand(core.FinishAgentStepCommand{
			AgentID:     runner.AgentID,
			DiffSummary: "step failed",
		})
		return false
	}

	// Only emit the fallback FinishAgentStep if the agent didn't already
	// finish via emit_diff_summary (which sets stepFinished).
	if !stepFinished.Load() {
		_, _ = s.Actor.SendCommand(core.FinishAgentStepCommand{
			AgentID:     runner.AgentID,
			DiffSummary: fmt.Sprintf("%s step completed", runner.Role.Label()),
		})
	}

	log.Printf("component=spec.agent action=step_completed agent_id=%s role=%s", runner.AgentID, runner.Role.Label())
	return true
}

// findManagerIndex returns the index of the first manager agent, or -1 if not found.
func (s *SwarmOrchestrator) findManagerIndex() int {
	for i, runner := range s.Agents {
		if runner != nil && runner.Role == RoleManager {
			return i
		}
	}
	return -1
}

// Cleanup unsubscribes all agent event channels from the actor broadcaster.
func (s *SwarmOrchestrator) Cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, ch := range s.eventChannels {
		if ch != nil {
			s.Actor.Unsubscribe(ch)
			s.eventChannels[i] = nil
		}
	}
}

// RunLoop drives all agents in the swarm through their think-act cycles.
// Runs until the context is cancelled. Each agent has its own event channel,
// so events are never stolen by whichever agent drains the channel first.
// When a human sends a chat message, HumanMessageNotify wakes the loop
// from its idle sleep so the manager agent responds promptly.
func (s *SwarmOrchestrator) RunLoop(ctx context.Context) {
	defer s.Cleanup()
	for {
		// Check for cancellation
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Recover empty slots and check pause
		s.RecoverEmptySlots()
		if s.IsPaused() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(500 * time.Millisecond):
			}
			continue
		}

		anyWork := false
		s.mu.Lock()
		agentCount := len(s.Agents)
		s.mu.Unlock()

		for i := 0; i < agentCount; i++ {
			// Check pause before each agent
			if s.IsPaused() {
				break
			}
			select {
			case <-ctx.Done():
				return
			default:
			}

			s.mu.Lock()
			runner := s.Agents[i]
			eventCh := s.eventChannels[i]
			s.mu.Unlock()

			if runner == nil {
				continue
			}

			s.RefreshContext(runner, eventCh)
			didWork := s.RunAgentStep(ctx, runner)
			if didWork {
				anyWork = true
				time.Sleep(100 * time.Millisecond)
			}
		}

		// Wait between cycles. Use select so a human message notification
		// can interrupt the idle sleep and wake us early.
		sleepDuration := 5 * time.Second
		if anyWork {
			sleepDuration = 1 * time.Second
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(sleepDuration):
		case <-s.HumanMessageNotify:
			// Human message arrived -- run the manager agent immediately
			if !s.IsPaused() {
				s.mu.Lock()
				mgrIdx := s.findManagerIndex()
				s.mu.Unlock()
				if mgrIdx >= 0 {
					log.Printf("component=spec.swarm action=prioritize_manager reason=human_message spec_id=%s", s.SpecID)
					s.mu.Lock()
					runner := s.Agents[mgrIdx]
					eventCh := s.eventChannels[mgrIdx]
					s.mu.Unlock()
					if runner != nil {
						s.RefreshContext(runner, eventCh)
						s.RunAgentStep(ctx, runner)
					}
				}
			}
		}
	}
}
