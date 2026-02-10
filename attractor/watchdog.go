// ABOUTME: Background watchdog that detects stalled pipeline stages via progress timestamps.
// ABOUTME: Emits warning events when a node exceeds its configured stall timeout without progress.
package attractor

import (
	"context"
	"sync"
	"time"
)

// WatchdogConfig holds configuration for the stall-detection watchdog.
type WatchdogConfig struct {
	StallTimeout  time.Duration // how long before a node is considered stalled
	CheckInterval time.Duration // how often to check for stalls
}

// DefaultWatchdogConfig returns a WatchdogConfig with sensible defaults:
// 5 minute stall timeout and 10 second check interval.
func DefaultWatchdogConfig() WatchdogConfig {
	return WatchdogConfig{
		StallTimeout:  5 * time.Minute,
		CheckInterval: 10 * time.Second,
	}
}

// Watchdog monitors active pipeline nodes and emits EventStageStalled warnings
// when a node has not made progress within the configured StallTimeout. It never
// cancels execution -- it is purely an observability tool.
type Watchdog struct {
	config       WatchdogConfig
	eventHandler func(EngineEvent)
	mu           sync.Mutex
	activeNodes  map[string]time.Time // nodeID -> last activity time
	warned       map[string]bool      // nodeID -> already warned
}

// NewWatchdog creates a Watchdog with the given config and event handler.
// The event handler is called (from the watchdog goroutine) whenever a stall
// warning is emitted.
func NewWatchdog(cfg WatchdogConfig, eventHandler func(EngineEvent)) *Watchdog {
	return &Watchdog{
		config:       cfg,
		eventHandler: eventHandler,
		activeNodes:  make(map[string]time.Time),
		warned:       make(map[string]bool),
	}
}

// Start launches the background monitoring goroutine. It stops when ctx is cancelled.
func (w *Watchdog) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(w.config.CheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.check()
			}
		}
	}()
}

// NodeStarted records that a node has become active. If the node was previously
// tracked and warned, the warning state is reset so a new stall can be detected
// if the node stalls again.
func (w *Watchdog) NodeStarted(nodeID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.activeNodes[nodeID] = time.Now()
	delete(w.warned, nodeID)
}

// NodeFinished removes a node from active tracking. After this call the watchdog
// will no longer consider the node for stall detection.
func (w *Watchdog) NodeFinished(nodeID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.activeNodes, nodeID)
	delete(w.warned, nodeID)
}

// HandleEvent is a convenience method that routes engine events to NodeStarted
// or NodeFinished based on event type. This lets the watchdog be composed with
// another event handler in a pipeline's EventHandler chain.
func (w *Watchdog) HandleEvent(evt EngineEvent) {
	switch evt.Type {
	case EventStageStarted:
		w.NodeStarted(evt.NodeID)
	case EventStageCompleted, EventStageFailed:
		w.NodeFinished(evt.NodeID)
	}
}

// ActiveNodes returns a slice of node IDs currently being tracked. The order is
// non-deterministic.
func (w *Watchdog) ActiveNodes() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	nodes := make([]string, 0, len(w.activeNodes))
	for id := range w.activeNodes {
		nodes = append(nodes, id)
	}
	return nodes
}

// check inspects all active nodes and emits a stall warning for any node that
// has exceeded the StallTimeout without finishing. Each node is warned at most
// once until it finishes and starts again. Events are emitted outside the lock
// to prevent deadlocks when the event handler acquires its own locks.
func (w *Watchdog) check() {
	w.mu.Lock()
	var toEmit []EngineEvent
	now := time.Now()
	for nodeID, lastActivity := range w.activeNodes {
		if w.warned[nodeID] {
			continue
		}
		elapsed := now.Sub(lastActivity)
		if elapsed > w.config.StallTimeout {
			w.warned[nodeID] = true
			toEmit = append(toEmit, EngineEvent{
				Type:      EventStageStalled,
				NodeID:    nodeID,
				Timestamp: now,
				Data: map[string]any{
					"elapsed":       elapsed.String(),
					"stall_timeout": w.config.StallTimeout.String(),
				},
			})
		}
	}
	w.mu.Unlock()

	for _, evt := range toEmit {
		if w.eventHandler != nil {
			w.eventHandler(evt)
		}
	}
}
