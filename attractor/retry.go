// ABOUTME: Retry policy configuration and exponential backoff delay calculation for pipeline node execution.
// ABOUTME: Provides preset policies (none, standard, aggressive, linear, patient) and helper functions.
package attractor

import (
	"math"
	"math/rand"
	"strconv"
	"time"
)

// RetryPolicy controls how many times a node execution is retried on failure.
type RetryPolicy struct {
	MaxAttempts int // minimum 1 (1 = no retries)
	Backoff     BackoffConfig
	ShouldRetry func(error) bool
}

// BackoffConfig controls delay timing between retry attempts.
type BackoffConfig struct {
	InitialDelay time.Duration // default 200ms
	Factor       float64       // default 2.0
	MaxDelay     time.Duration // default 60s
	Jitter       bool          // default true
}

// DelayForAttempt calculates the delay for a given attempt number (0-indexed).
// The formula is: InitialDelay * Factor^attempt, capped at MaxDelay.
// If Jitter is enabled, the delay is randomized in [0, calculated_delay].
func (b BackoffConfig) DelayForAttempt(attempt int) time.Duration {
	baseNanos := float64(b.InitialDelay.Nanoseconds()) * math.Pow(b.Factor, float64(attempt))
	maxNanos := float64(b.MaxDelay.Nanoseconds())
	delayNanos := math.Min(baseNanos, maxNanos)

	if b.Jitter {
		delayNanos = rand.Float64() * delayNanos
	}

	return time.Duration(int64(delayNanos))
}

// RetryPolicyNone returns a policy with no retries (single attempt).
func RetryPolicyNone() RetryPolicy {
	return RetryPolicy{
		MaxAttempts: 1,
		Backoff: BackoffConfig{
			InitialDelay: 200 * time.Millisecond,
			Factor:       2.0,
			MaxDelay:     60 * time.Second,
			Jitter:       false,
		},
		ShouldRetry: DefaultShouldRetry,
	}
}

// RetryPolicyStandard returns a standard retry policy with 5 attempts and exponential backoff.
func RetryPolicyStandard() RetryPolicy {
	return RetryPolicy{
		MaxAttempts: 5,
		Backoff: BackoffConfig{
			InitialDelay: 200 * time.Millisecond,
			Factor:       2.0,
			MaxDelay:     60 * time.Second,
			Jitter:       true,
		},
		ShouldRetry: DefaultShouldRetry,
	}
}

// RetryPolicyAggressive returns a policy with 5 attempts and a higher initial delay.
func RetryPolicyAggressive() RetryPolicy {
	return RetryPolicy{
		MaxAttempts: 5,
		Backoff: BackoffConfig{
			InitialDelay: 500 * time.Millisecond,
			Factor:       2.0,
			MaxDelay:     60 * time.Second,
			Jitter:       true,
		},
		ShouldRetry: DefaultShouldRetry,
	}
}

// RetryPolicyLinear returns a policy with 3 attempts and constant delay (factor=1.0).
func RetryPolicyLinear() RetryPolicy {
	return RetryPolicy{
		MaxAttempts: 3,
		Backoff: BackoffConfig{
			InitialDelay: 500 * time.Millisecond,
			Factor:       1.0,
			MaxDelay:     60 * time.Second,
			Jitter:       false,
		},
		ShouldRetry: DefaultShouldRetry,
	}
}

// RetryPolicyPatient returns a policy with 3 attempts, high initial delay, and steep backoff.
func RetryPolicyPatient() RetryPolicy {
	return RetryPolicy{
		MaxAttempts: 3,
		Backoff: BackoffConfig{
			InitialDelay: 2000 * time.Millisecond,
			Factor:       3.0,
			MaxDelay:     60 * time.Second,
			Jitter:       true,
		},
		ShouldRetry: DefaultShouldRetry,
	}
}

// DefaultShouldRetry returns true for most errors as a simple default retry predicate.
// Returns false for nil errors.
func DefaultShouldRetry(err error) bool {
	return err != nil
}

// buildRetryPolicy constructs a RetryPolicy by checking node attributes, then graph attributes,
// then falling back to the provided default policy.
func buildRetryPolicy(node *Node, graph *Graph, defaultPolicy RetryPolicy) RetryPolicy {
	// Check node attribute first
	if node.Attrs != nil {
		if maxRetriesStr, ok := node.Attrs["max_retries"]; ok && maxRetriesStr != "" {
			if maxRetries, err := strconv.Atoi(maxRetriesStr); err == nil {
				policy := defaultPolicy
				policy.MaxAttempts = maxRetries + 1
				return policy
			}
		}
	}

	// Check graph attribute
	if graph.Attrs != nil {
		if maxRetryStr, ok := graph.Attrs["default_max_retry"]; ok && maxRetryStr != "" {
			if maxRetry, err := strconv.Atoi(maxRetryStr); err == nil {
				policy := defaultPolicy
				policy.MaxAttempts = maxRetry + 1
				return policy
			}
		}
	}

	// Fall back to default
	return defaultPolicy
}

// resolveNodeTimeout determines the execution timeout for a node by checking,
// in order: node "timeout" attribute, graph "default_node_timeout" attribute,
// and finally the config default. Returns 0 (no timeout) if nothing is set.
func resolveNodeTimeout(node *Node, graph *Graph, configDefault time.Duration) time.Duration {
	// Check node attribute first
	if node.Attrs != nil {
		if timeoutStr, ok := node.Attrs["timeout"]; ok && timeoutStr != "" {
			if d, err := time.ParseDuration(timeoutStr); err == nil {
				return d
			}
		}
	}

	// Check graph attribute
	if graph.Attrs != nil {
		if timeoutStr, ok := graph.Attrs["default_node_timeout"]; ok && timeoutStr != "" {
			if d, err := time.ParseDuration(timeoutStr); err == nil {
				return d
			}
		}
	}

	// Fall back to config default
	return configDefault
}

// isTerminal returns true if the node is a terminal/exit node.
// Recognized via shape=Msquare, node_type=exit, or type=exit.
func isTerminal(node *Node) bool {
	if node.Attrs == nil {
		return false
	}
	if node.Attrs["shape"] == "Msquare" {
		return true
	}
	if node.Attrs["node_type"] == "exit" || node.Attrs["type"] == "exit" {
		return true
	}
	return false
}

// checkGoalGates checks all visited nodes with goal_gate=true and verifies they have
// a success or partial success outcome. Returns (true, nil) if all gates pass,
// or (false, failedNode) if any gate has a non-success outcome.
func checkGoalGates(graph *Graph, outcomes map[string]*Outcome) (bool, *Node) {
	for _, node := range graph.Nodes {
		if node.Attrs["goal_gate"] != "true" {
			continue
		}
		outcome, visited := outcomes[node.ID]
		if !visited {
			continue
		}
		if outcome.Status != StatusSuccess && outcome.Status != StatusPartialSuccess {
			return false, node
		}
	}
	return true, nil
}

// getRetryTarget resolves the retry target node ID from the node and graph attributes.
// Checks in order: node.retry_target, node.fallback_retry_target,
// graph.retry_target, graph.fallback_retry_target.
func getRetryTarget(node *Node, graph *Graph) string {
	if node.Attrs != nil {
		if target := node.Attrs["retry_target"]; target != "" {
			return target
		}
		if target := node.Attrs["fallback_retry_target"]; target != "" {
			return target
		}
	}
	if graph.Attrs != nil {
		if target := graph.Attrs["retry_target"]; target != "" {
			return target
		}
		if target := graph.Attrs["fallback_retry_target"]; target != "" {
			return target
		}
	}
	return ""
}
