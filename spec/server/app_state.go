// ABOUTME: Shared application state for the mammoth spec builder HTTP server.
// ABOUTME: Contains actor handles, swarm handles, event persisters, and provider status.
package server

import (
	"context"
	"log"
	"sync"

	"github.com/2389-research/mux/llm"
	"github.com/oklog/ulid/v2"

	"github.com/2389-research/mammoth/spec/agents"
	"github.com/2389-research/mammoth/spec/core"
)

// SwarmHandle bundles a swarm orchestrator reference with a cancel function
// so the agent loop can be stopped on cleanup.
type SwarmHandle struct {
	Orchestrator *agents.SwarmOrchestrator
	Cancel       context.CancelFunc
}

// AppState holds the shared state accessible by all HTTP handlers.
type AppState struct {
	mu              sync.RWMutex
	Actors          map[ulid.ULID]*core.SpecActorHandle
	Swarms          map[ulid.ULID]*SwarmHandle
	EventPersisters map[ulid.ULID]chan struct{} // stop channels for event persister goroutines
	MammothHome     string
	ProviderStatus  ProviderStatus
	LLMClient       llm.Client // may be nil if no provider is configured
	LLMModel        string
}

// NewAppState creates a new AppState with empty maps and the given home directory.
func NewAppState(mammothHome string, providerStatus ProviderStatus) *AppState {
	return &AppState{
		Actors:          make(map[ulid.ULID]*core.SpecActorHandle),
		Swarms:          make(map[ulid.ULID]*SwarmHandle),
		EventPersisters: make(map[ulid.ULID]chan struct{}),
		MammothHome:     mammothHome,
		ProviderStatus:  providerStatus,
	}
}

// GetActor returns the actor handle for a spec, or nil if not found.
func (s *AppState) GetActor(specID ulid.ULID) *core.SpecActorHandle {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Actors[specID]
}

// SetActor stores an actor handle for a spec.
func (s *AppState) SetActor(specID ulid.ULID, handle *core.SpecActorHandle) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Actors[specID] = handle
}

// ListActorIDs returns all spec IDs with active actors.
func (s *AppState) ListActorIDs() []ulid.ULID {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]ulid.ULID, 0, len(s.Actors))
	for id := range s.Actors {
		ids = append(ids, id)
	}
	return ids
}

// GetSwarm returns the swarm handle for a spec, or nil if not found.
func (s *AppState) GetSwarm(specID ulid.ULID) *SwarmHandle {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Swarms[specID]
}

// SetSwarm stores a swarm handle for a spec.
func (s *AppState) SetSwarm(specID ulid.ULID, handle *SwarmHandle) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Swarms[specID] = handle
}

// StopSwarm cancels and removes the swarm for a specific spec. It returns true
// if a swarm was found and stopped.
func (s *AppState) StopSwarm(specID ulid.ULID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	handle, exists := s.Swarms[specID]
	if !exists {
		return false
	}
	log.Printf("component=spec.server action=swarm_stop spec_id=%s", specID)
	handle.Cancel()
	delete(s.Swarms, specID)
	return true
}

// SetEventPersister stores a stop channel for a spec's event persister goroutine.
func (s *AppState) SetEventPersister(specID ulid.ULID, stopCh chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.EventPersisters[specID] = stopCh
}

// StopAllEventPersisters closes all event persister stop channels.
func (s *AppState) StopAllEventPersisters() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for specID, stopCh := range s.EventPersisters {
		close(stopCh)
		delete(s.EventPersisters, specID)
	}
}

// TryStartAgents starts the agent swarm for a spec if an LLM provider is configured
// and no swarm is already running. Holds the lock across check-and-set to prevent
// double-start races. Returns true if a swarm was started.
func (s *AppState) TryStartAgents(specID ulid.ULID) bool {
	if s.LLMClient == nil {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.Swarms[specID]; exists {
		return false
	}

	actor := s.Actors[specID]
	if actor == nil {
		return false
	}

	orchestrator := agents.NewSwarmOrchestrator(specID, actor, s.LLMClient, s.LLMModel)
	ctx, cancel := context.WithCancel(context.Background())
	go orchestrator.RunLoop(ctx)

	s.Swarms[specID] = &SwarmHandle{
		Orchestrator: orchestrator,
		Cancel:       cancel,
	}

	log.Printf("component=spec.server action=swarm_started spec_id=%s agent_count=%d", specID, orchestrator.AgentCount())
	return true
}

// StopAllSwarms cancels all running swarm orchestrators.
func (s *AppState) StopAllSwarms() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for specID, handle := range s.Swarms {
		log.Printf("component=spec.server action=swarm_stop spec_id=%s", specID)
		handle.Cancel()
		delete(s.Swarms, specID)
	}
}
