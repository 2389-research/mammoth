// ABOUTME: Tests for the stall watchdog that monitors pipeline stages for lack of progress.
// ABOUTME: Covers detection, false positives, single-warning semantics, concurrency, and context cancellation.
package attractor

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"
)

func TestDefaultWatchdogConfig(t *testing.T) {
	cfg := DefaultWatchdogConfig()
	if cfg.StallTimeout != 5*time.Minute {
		t.Errorf("StallTimeout: got %v, want %v", cfg.StallTimeout, 5*time.Minute)
	}
	if cfg.CheckInterval != 10*time.Second {
		t.Errorf("CheckInterval: got %v, want %v", cfg.CheckInterval, 10*time.Second)
	}
}

func TestWatchdogNodeStarted(t *testing.T) {
	cfg := WatchdogConfig{StallTimeout: time.Minute, CheckInterval: time.Second}
	w := NewWatchdog(cfg, func(EngineEvent) {})

	w.NodeStarted("node_a")
	w.NodeStarted("node_b")

	active := w.ActiveNodes()
	sort.Strings(active)
	if len(active) != 2 {
		t.Fatalf("ActiveNodes: got %d, want 2", len(active))
	}
	if active[0] != "node_a" || active[1] != "node_b" {
		t.Errorf("ActiveNodes: got %v, want [node_a node_b]", active)
	}
}

func TestWatchdogNodeFinished(t *testing.T) {
	cfg := WatchdogConfig{StallTimeout: time.Minute, CheckInterval: time.Second}
	w := NewWatchdog(cfg, func(EngineEvent) {})

	w.NodeStarted("node_a")
	w.NodeStarted("node_b")
	w.NodeFinished("node_a")

	active := w.ActiveNodes()
	if len(active) != 1 {
		t.Fatalf("ActiveNodes after finish: got %d, want 1", len(active))
	}
	if active[0] != "node_b" {
		t.Errorf("ActiveNodes: got %v, want [node_b]", active)
	}
}

func TestWatchdogDetectsStall(t *testing.T) {
	var mu sync.Mutex
	var events []EngineEvent

	cfg := WatchdogConfig{StallTimeout: 10 * time.Millisecond, CheckInterval: 5 * time.Millisecond}
	w := NewWatchdog(cfg, func(evt EngineEvent) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, evt)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w.NodeStarted("slow_node")
	w.Start(ctx)

	// Wait long enough for the stall to be detected
	time.Sleep(100 * time.Millisecond)
	cancel()

	mu.Lock()
	defer mu.Unlock()

	if len(events) == 0 {
		t.Fatal("expected at least one stall event, got none")
	}

	found := false
	for _, evt := range events {
		if evt.Type == EventStageStalled && evt.NodeID == "slow_node" {
			found = true
			if evt.Data == nil {
				t.Error("stall event Data should not be nil")
			}
			if _, ok := evt.Data["elapsed"]; !ok {
				t.Error("stall event Data should contain 'elapsed' key")
			}
			break
		}
	}
	if !found {
		t.Errorf("expected EventStageStalled for slow_node, got events: %+v", events)
	}
}

func TestWatchdogNoFalsePositive(t *testing.T) {
	var mu sync.Mutex
	var events []EngineEvent

	cfg := WatchdogConfig{StallTimeout: 50 * time.Millisecond, CheckInterval: 5 * time.Millisecond}
	w := NewWatchdog(cfg, func(evt EngineEvent) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, evt)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start and finish quickly -- well within the stall timeout
	w.NodeStarted("fast_node")
	w.NodeFinished("fast_node")
	w.Start(ctx)

	// Wait a bit but not forever
	time.Sleep(80 * time.Millisecond)
	cancel()

	mu.Lock()
	defer mu.Unlock()

	for _, evt := range events {
		if evt.Type == EventStageStalled && evt.NodeID == "fast_node" {
			t.Error("should not emit stall event for a node that finished quickly")
		}
	}
}

func TestWatchdogOnlyWarnsOnce(t *testing.T) {
	var mu sync.Mutex
	var events []EngineEvent

	cfg := WatchdogConfig{StallTimeout: 10 * time.Millisecond, CheckInterval: 5 * time.Millisecond}
	w := NewWatchdog(cfg, func(evt EngineEvent) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, evt)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w.NodeStarted("stuck_node")
	w.Start(ctx)

	// Wait long enough for multiple check cycles to fire
	time.Sleep(100 * time.Millisecond)
	cancel()

	mu.Lock()
	defer mu.Unlock()

	stallCount := 0
	for _, evt := range events {
		if evt.Type == EventStageStalled && evt.NodeID == "stuck_node" {
			stallCount++
		}
	}
	if stallCount != 1 {
		t.Errorf("expected exactly 1 stall warning for stuck_node, got %d", stallCount)
	}
}

func TestWatchdogMultipleNodes(t *testing.T) {
	var mu sync.Mutex
	var events []EngineEvent

	cfg := WatchdogConfig{StallTimeout: 10 * time.Millisecond, CheckInterval: 5 * time.Millisecond}
	w := NewWatchdog(cfg, func(evt EngineEvent) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, evt)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w.NodeStarted("node_a")
	w.NodeStarted("node_b")
	// Finish node_b quickly so it should NOT stall
	w.NodeFinished("node_b")
	w.Start(ctx)

	time.Sleep(100 * time.Millisecond)
	cancel()

	mu.Lock()
	defer mu.Unlock()

	stalledNodes := make(map[string]bool)
	for _, evt := range events {
		if evt.Type == EventStageStalled {
			stalledNodes[evt.NodeID] = true
		}
	}

	if !stalledNodes["node_a"] {
		t.Error("expected stall warning for node_a (still active)")
	}
	if stalledNodes["node_b"] {
		t.Error("should not emit stall warning for node_b (already finished)")
	}
}

func TestWatchdogStopsOnContextCancel(t *testing.T) {
	var mu sync.Mutex
	var events []EngineEvent

	cfg := WatchdogConfig{StallTimeout: 5 * time.Millisecond, CheckInterval: 2 * time.Millisecond}
	w := NewWatchdog(cfg, func(evt EngineEvent) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, evt)
	})

	ctx, cancel := context.WithCancel(context.Background())

	w.NodeStarted("node_x")
	w.Start(ctx)

	// Let it detect the stall
	time.Sleep(30 * time.Millisecond)
	cancel()

	// Record the event count after cancellation
	time.Sleep(30 * time.Millisecond)
	mu.Lock()
	countAfterCancel := len(events)
	mu.Unlock()

	// Wait more -- no new events should arrive
	time.Sleep(30 * time.Millisecond)
	mu.Lock()
	countLater := len(events)
	mu.Unlock()

	if countLater != countAfterCancel {
		t.Errorf("events grew after context cancel: %d -> %d", countAfterCancel, countLater)
	}
}

func TestWatchdogHandleEvent(t *testing.T) {
	var mu sync.Mutex
	var events []EngineEvent

	cfg := WatchdogConfig{StallTimeout: 10 * time.Millisecond, CheckInterval: 5 * time.Millisecond}
	w := NewWatchdog(cfg, func(evt EngineEvent) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, evt)
	})

	// HandleEvent with stage.started should register the node
	w.HandleEvent(EngineEvent{Type: EventStageStarted, NodeID: "node_h"})
	active := w.ActiveNodes()
	if len(active) != 1 || active[0] != "node_h" {
		t.Errorf("after HandleEvent(stage.started): ActiveNodes = %v, want [node_h]", active)
	}

	// HandleEvent with stage.completed should remove the node
	w.HandleEvent(EngineEvent{Type: EventStageCompleted, NodeID: "node_h"})
	active = w.ActiveNodes()
	if len(active) != 0 {
		t.Errorf("after HandleEvent(stage.completed): ActiveNodes = %v, want []", active)
	}

	// HandleEvent with stage.failed should also remove the node
	w.HandleEvent(EngineEvent{Type: EventStageStarted, NodeID: "node_f"})
	w.HandleEvent(EngineEvent{Type: EventStageFailed, NodeID: "node_f"})
	active = w.ActiveNodes()
	if len(active) != 0 {
		t.Errorf("after HandleEvent(stage.failed): ActiveNodes = %v, want []", active)
	}
}

func TestWatchdogConcurrentAccess(t *testing.T) {
	cfg := WatchdogConfig{StallTimeout: time.Minute, CheckInterval: time.Second}
	w := NewWatchdog(cfg, func(EngineEvent) {})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			nodeID := "node_" + string(rune('a'+id%26))
			w.NodeStarted(nodeID)
			w.ActiveNodes()
			w.NodeFinished(nodeID)
		}(i)
	}
	wg.Wait()

	// Just verify no panics and ActiveNodes returns without error
	_ = w.ActiveNodes()
}
