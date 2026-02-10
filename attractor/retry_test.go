// ABOUTME: Tests for retry policy configuration and exponential backoff delay calculations.
// ABOUTME: Covers all preset policies, delay computation, max delay capping, and jitter bounds.
package attractor

import (
	"testing"
	"time"
)

// --- RetryPolicy preset tests ---

func TestRetryPolicyNoneHasMaxAttempts1(t *testing.T) {
	p := RetryPolicyNone()
	if p.MaxAttempts != 1 {
		t.Errorf("expected MaxAttempts=1, got %d", p.MaxAttempts)
	}
}

func TestRetryPolicyStandard(t *testing.T) {
	p := RetryPolicyStandard()
	if p.MaxAttempts != 5 {
		t.Errorf("expected MaxAttempts=5, got %d", p.MaxAttempts)
	}
	if p.Backoff.InitialDelay != 200*time.Millisecond {
		t.Errorf("expected InitialDelay=200ms, got %v", p.Backoff.InitialDelay)
	}
	if p.Backoff.Factor != 2.0 {
		t.Errorf("expected Factor=2.0, got %v", p.Backoff.Factor)
	}
}

func TestRetryPolicyAggressive(t *testing.T) {
	p := RetryPolicyAggressive()
	if p.MaxAttempts != 5 {
		t.Errorf("expected MaxAttempts=5, got %d", p.MaxAttempts)
	}
	if p.Backoff.InitialDelay != 500*time.Millisecond {
		t.Errorf("expected InitialDelay=500ms, got %v", p.Backoff.InitialDelay)
	}
	if p.Backoff.Factor != 2.0 {
		t.Errorf("expected Factor=2.0, got %v", p.Backoff.Factor)
	}
}

func TestRetryPolicyLinear(t *testing.T) {
	p := RetryPolicyLinear()
	if p.MaxAttempts != 3 {
		t.Errorf("expected MaxAttempts=3, got %d", p.MaxAttempts)
	}
	if p.Backoff.InitialDelay != 500*time.Millisecond {
		t.Errorf("expected InitialDelay=500ms, got %v", p.Backoff.InitialDelay)
	}
	if p.Backoff.Factor != 1.0 {
		t.Errorf("expected Factor=1.0 (linear), got %v", p.Backoff.Factor)
	}
}

func TestRetryPolicyPatient(t *testing.T) {
	p := RetryPolicyPatient()
	if p.MaxAttempts != 3 {
		t.Errorf("expected MaxAttempts=3, got %d", p.MaxAttempts)
	}
	if p.Backoff.InitialDelay != 2000*time.Millisecond {
		t.Errorf("expected InitialDelay=2000ms, got %v", p.Backoff.InitialDelay)
	}
	if p.Backoff.Factor != 3.0 {
		t.Errorf("expected Factor=3.0, got %v", p.Backoff.Factor)
	}
}

// --- BackoffConfig.DelayForAttempt tests ---

func TestDelayForAttemptExponentialBackoff(t *testing.T) {
	bc := BackoffConfig{
		InitialDelay: 100 * time.Millisecond,
		Factor:       2.0,
		MaxDelay:     60 * time.Second,
		Jitter:       false,
	}

	// Attempt 0: 100ms * 2^0 = 100ms
	d0 := bc.DelayForAttempt(0)
	if d0 != 100*time.Millisecond {
		t.Errorf("attempt 0: expected 100ms, got %v", d0)
	}

	// Attempt 1: 100ms * 2^1 = 200ms
	d1 := bc.DelayForAttempt(1)
	if d1 != 200*time.Millisecond {
		t.Errorf("attempt 1: expected 200ms, got %v", d1)
	}

	// Attempt 2: 100ms * 2^2 = 400ms
	d2 := bc.DelayForAttempt(2)
	if d2 != 400*time.Millisecond {
		t.Errorf("attempt 2: expected 400ms, got %v", d2)
	}

	// Attempt 3: 100ms * 2^3 = 800ms
	d3 := bc.DelayForAttempt(3)
	if d3 != 800*time.Millisecond {
		t.Errorf("attempt 3: expected 800ms, got %v", d3)
	}
}

func TestDelayForAttemptLinearBackoff(t *testing.T) {
	bc := BackoffConfig{
		InitialDelay: 500 * time.Millisecond,
		Factor:       1.0,
		MaxDelay:     60 * time.Second,
		Jitter:       false,
	}

	// All attempts should be 500ms with factor 1.0
	for attempt := 0; attempt < 5; attempt++ {
		d := bc.DelayForAttempt(attempt)
		if d != 500*time.Millisecond {
			t.Errorf("attempt %d: expected 500ms, got %v", attempt, d)
		}
	}
}

func TestDelayForAttemptMaxDelayCap(t *testing.T) {
	bc := BackoffConfig{
		InitialDelay: 1 * time.Second,
		Factor:       10.0,
		MaxDelay:     5 * time.Second,
		Jitter:       false,
	}

	// Attempt 0: 1s * 10^0 = 1s
	d0 := bc.DelayForAttempt(0)
	if d0 != 1*time.Second {
		t.Errorf("attempt 0: expected 1s, got %v", d0)
	}

	// Attempt 1: 1s * 10^1 = 10s -> capped at 5s
	d1 := bc.DelayForAttempt(1)
	if d1 != 5*time.Second {
		t.Errorf("attempt 1: expected 5s (capped), got %v", d1)
	}

	// Attempt 2: 1s * 10^2 = 100s -> capped at 5s
	d2 := bc.DelayForAttempt(2)
	if d2 != 5*time.Second {
		t.Errorf("attempt 2: expected 5s (capped), got %v", d2)
	}
}

func TestDelayForAttemptJitterProducesValuesInRange(t *testing.T) {
	bc := BackoffConfig{
		InitialDelay: 1 * time.Second,
		Factor:       1.0,
		MaxDelay:     60 * time.Second,
		Jitter:       true,
	}

	// With jitter, result should be in [0, baseDelay]
	// Run multiple times to check bounds
	baseDelay := 1 * time.Second
	for i := 0; i < 100; i++ {
		d := bc.DelayForAttempt(0)
		if d < 0 {
			t.Errorf("jittered delay should be >= 0, got %v", d)
		}
		if d > baseDelay {
			t.Errorf("jittered delay should be <= %v, got %v", baseDelay, d)
		}
	}
}

// --- DefaultShouldRetry tests ---

func TestDefaultShouldRetryReturnsTrueForErrors(t *testing.T) {
	err := &testError{msg: "something failed"}
	if !DefaultShouldRetry(err) {
		t.Error("expected DefaultShouldRetry to return true for generic error")
	}
}

func TestDefaultShouldRetryReturnsFalseForNil(t *testing.T) {
	if DefaultShouldRetry(nil) {
		t.Error("expected DefaultShouldRetry to return false for nil error")
	}
}

// --- buildRetryPolicy tests ---

func TestBuildRetryPolicyFromNodeAttr(t *testing.T) {
	node := &Node{ID: "n1", Attrs: map[string]string{"max_retries": "3"}}
	graph := &Graph{Attrs: map[string]string{}}
	defaultPolicy := RetryPolicyNone()

	policy := buildRetryPolicy(node, graph, defaultPolicy)
	// max_retries=3 means MaxAttempts=4 (initial + 3 retries)
	if policy.MaxAttempts != 4 {
		t.Errorf("expected MaxAttempts=4, got %d", policy.MaxAttempts)
	}
}

func TestBuildRetryPolicyFromGraphDefault(t *testing.T) {
	node := &Node{ID: "n1", Attrs: map[string]string{}}
	graph := &Graph{Attrs: map[string]string{"default_max_retry": "2"}}
	defaultPolicy := RetryPolicyNone()

	policy := buildRetryPolicy(node, graph, defaultPolicy)
	// default_max_retry=2 means MaxAttempts=3
	if policy.MaxAttempts != 3 {
		t.Errorf("expected MaxAttempts=3, got %d", policy.MaxAttempts)
	}
}

func TestBuildRetryPolicyFallsBackToDefault(t *testing.T) {
	node := &Node{ID: "n1", Attrs: map[string]string{}}
	graph := &Graph{Attrs: map[string]string{}}
	defaultPolicy := RetryPolicyStandard()

	policy := buildRetryPolicy(node, graph, defaultPolicy)
	if policy.MaxAttempts != defaultPolicy.MaxAttempts {
		t.Errorf("expected MaxAttempts=%d, got %d", defaultPolicy.MaxAttempts, policy.MaxAttempts)
	}
}

func TestBuildRetryPolicyNodeOverridesGraph(t *testing.T) {
	node := &Node{ID: "n1", Attrs: map[string]string{"max_retries": "5"}}
	graph := &Graph{Attrs: map[string]string{"default_max_retry": "2"}}
	defaultPolicy := RetryPolicyNone()

	policy := buildRetryPolicy(node, graph, defaultPolicy)
	// Node says 5 retries, graph says 2 -- node wins
	if policy.MaxAttempts != 6 {
		t.Errorf("expected MaxAttempts=6 (node attr wins), got %d", policy.MaxAttempts)
	}
}

// --- isTerminal tests ---

func TestIsTerminalMsquare(t *testing.T) {
	node := &Node{ID: "exit", Attrs: map[string]string{"shape": "Msquare"}}
	if !isTerminal(node) {
		t.Error("expected Msquare node to be terminal")
	}
}

func TestIsTerminalNonTerminal(t *testing.T) {
	node := &Node{ID: "normal", Attrs: map[string]string{"shape": "box"}}
	if isTerminal(node) {
		t.Error("expected box node to NOT be terminal")
	}
}

func TestIsTerminalNoShape(t *testing.T) {
	node := &Node{ID: "bare", Attrs: map[string]string{}}
	if isTerminal(node) {
		t.Error("expected node without shape to NOT be terminal")
	}
}

func TestIsTerminalNodeTypeExit(t *testing.T) {
	node := &Node{ID: "exit", Attrs: map[string]string{"node_type": "exit", "shape": "doublecircle"}}
	if !isTerminal(node) {
		t.Error("expected node with node_type=exit to be terminal")
	}
}

func TestIsTerminalTypeExit(t *testing.T) {
	node := &Node{ID: "exit", Attrs: map[string]string{"type": "exit", "shape": "doublecircle"}}
	if !isTerminal(node) {
		t.Error("expected node with type=exit to be terminal")
	}
}

// --- checkGoalGates tests ---

func TestCheckGoalGatesAllPassed(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"gate1": {ID: "gate1", Attrs: map[string]string{"goal_gate": "true"}},
			"gate2": {ID: "gate2", Attrs: map[string]string{"goal_gate": "true"}},
		},
	}
	outcomes := map[string]*Outcome{
		"gate1": {Status: StatusSuccess},
		"gate2": {Status: StatusPartialSuccess},
	}

	ok, failed := checkGoalGates(g, outcomes)
	if !ok {
		t.Error("expected all gates passed")
	}
	if failed != nil {
		t.Errorf("expected no failed node, got %v", failed)
	}
}

func TestCheckGoalGatesFailed(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"gate1": {ID: "gate1", Attrs: map[string]string{"goal_gate": "true"}},
		},
	}
	outcomes := map[string]*Outcome{
		"gate1": {Status: StatusFail},
	}

	ok, failed := checkGoalGates(g, outcomes)
	if ok {
		t.Error("expected gate to fail")
	}
	if failed == nil || failed.ID != "gate1" {
		t.Errorf("expected failed node gate1, got %v", failed)
	}
}

func TestCheckGoalGatesSkipsUnvisitedNodes(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"gate1": {ID: "gate1", Attrs: map[string]string{"goal_gate": "true"}},
		},
	}
	outcomes := map[string]*Outcome{}

	ok, failed := checkGoalGates(g, outcomes)
	if !ok {
		t.Error("expected unvisited goal gates to pass (not checked)")
	}
	if failed != nil {
		t.Errorf("expected no failed node for unvisited gates, got %v", failed)
	}
}

// --- getRetryTarget tests ---

func TestGetRetryTargetFromNode(t *testing.T) {
	node := &Node{ID: "n1", Attrs: map[string]string{"retry_target": "retry_node"}}
	graph := &Graph{Attrs: map[string]string{}}

	target := getRetryTarget(node, graph)
	if target != "retry_node" {
		t.Errorf("expected 'retry_node', got %q", target)
	}
}

func TestGetRetryTargetFallbackFromNode(t *testing.T) {
	node := &Node{ID: "n1", Attrs: map[string]string{"fallback_retry_target": "fallback_node"}}
	graph := &Graph{Attrs: map[string]string{}}

	target := getRetryTarget(node, graph)
	if target != "fallback_node" {
		t.Errorf("expected 'fallback_node', got %q", target)
	}
}

func TestGetRetryTargetFromGraph(t *testing.T) {
	node := &Node{ID: "n1", Attrs: map[string]string{}}
	graph := &Graph{Attrs: map[string]string{"retry_target": "graph_retry"}}

	target := getRetryTarget(node, graph)
	if target != "graph_retry" {
		t.Errorf("expected 'graph_retry', got %q", target)
	}
}

func TestGetRetryTargetFallbackFromGraph(t *testing.T) {
	node := &Node{ID: "n1", Attrs: map[string]string{}}
	graph := &Graph{Attrs: map[string]string{"fallback_retry_target": "graph_fallback"}}

	target := getRetryTarget(node, graph)
	if target != "graph_fallback" {
		t.Errorf("expected 'graph_fallback', got %q", target)
	}
}

func TestGetRetryTargetNone(t *testing.T) {
	node := &Node{ID: "n1", Attrs: map[string]string{}}
	graph := &Graph{Attrs: map[string]string{}}

	target := getRetryTarget(node, graph)
	if target != "" {
		t.Errorf("expected empty string, got %q", target)
	}
}

// --- test helper types ---

type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}
