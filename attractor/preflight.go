// ABOUTME: Pre-execution validation that checks provider accessibility and pipeline requirements.
// ABOUTME: Runs before the engine starts to provide fast, clear failure messages.
package attractor

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// PreflightCheck represents a single validation check to run before pipeline execution.
type PreflightCheck struct {
	Name  string                         // human-readable check name
	Check func(ctx context.Context) error // the actual check; nil error means pass
}

// PreflightResult holds the aggregated results of all preflight checks.
type PreflightResult struct {
	Passed []string           // names of checks that passed
	Failed []PreflightFailure // checks that failed with reasons
}

// PreflightFailure records a single check failure with its name and reason.
type PreflightFailure struct {
	Name   string
	Reason string
}

// OK returns true if no checks failed.
func (r PreflightResult) OK() bool {
	return len(r.Failed) == 0
}

// Error formats all failures as a multi-line string. Returns empty string if no failures.
func (r PreflightResult) Error() string {
	if len(r.Failed) == 0 {
		return ""
	}
	lines := make([]string, 0, len(r.Failed)+1)
	lines = append(lines, fmt.Sprintf("preflight: %d check(s) failed:", len(r.Failed)))
	for _, f := range r.Failed {
		lines = append(lines, fmt.Sprintf("  - %s: %s", f.Name, f.Reason))
	}
	return strings.Join(lines, "\n")
}

// RunPreflight executes all checks and collects results. Every check is run
// regardless of whether earlier checks fail, so the caller gets a complete
// picture of what needs to be fixed.
func RunPreflight(ctx context.Context, checks []PreflightCheck) PreflightResult {
	result := PreflightResult{
		Passed: make([]string, 0, len(checks)),
		Failed: make([]PreflightFailure, 0),
	}

	for _, c := range checks {
		if err := c.Check(ctx); err != nil {
			result.Failed = append(result.Failed, PreflightFailure{
				Name:   c.Name,
				Reason: err.Error(),
			})
		} else {
			result.Passed = append(result.Passed, c.Name)
		}
	}

	return result
}

// BuildPreflightChecks analyzes the graph and engine configuration to produce
// the set of preflight checks appropriate for this pipeline. Checks include:
//   - Backend availability when codergen nodes are present
//   - Required environment variables declared via env_required node attributes
func BuildPreflightChecks(graph *Graph, cfg EngineConfig) []PreflightCheck {
	var checks []PreflightCheck

	// Check 1: Backend availability for codergen nodes
	if HasCodergenNodes(graph) {
		if cfg.Backend == nil {
			checks = append(checks, PreflightCheck{
				Name: "codergen-backend",
				Check: func(ctx context.Context) error {
					return fmt.Errorf("codergen nodes found but no backend configured (set an API key)")
				},
			})
		} else {
			checks = append(checks, PreflightCheck{
				Name: "backend-configured",
				Check: func(ctx context.Context) error {
					return nil
				},
			})
		}
	}

	// Check 2: Required environment variables
	seen := make(map[string]bool)
	for _, node := range graph.Nodes {
		if node.Attrs == nil {
			continue
		}
		envRequired := node.Attrs["env_required"]
		if envRequired == "" {
			continue
		}
		// Support comma-separated list of env vars
		for _, envVar := range strings.Split(envRequired, ",") {
			envVar = strings.TrimSpace(envVar)
			if envVar == "" || seen[envVar] {
				continue
			}
			seen[envVar] = true
			varName := envVar // capture for closure
			checks = append(checks, PreflightCheck{
				Name: fmt.Sprintf("env:%s", varName),
				Check: func(ctx context.Context) error {
					if os.Getenv(varName) == "" {
						return fmt.Errorf("required environment variable %s is not set", varName)
					}
					return nil
				},
			})
		}
	}

	return checks
}

// HasCodergenNodes returns true if the graph contains any nodes that would be
// resolved to the codergen handler. This mirrors the resolution logic in
// HandlerRegistry.Resolve: explicit type attribute first, then shape-based
// mapping, then default to codergen.
func HasCodergenNodes(graph *Graph) bool {
	if graph == nil || graph.Nodes == nil {
		return false
	}

	// Build the set of known handler types from the default registry so we
	// can check whether an explicit "type" attribute maps to a real handler.
	knownTypes := map[string]bool{
		"start":              true,
		"exit":               true,
		"codergen":           true,
		"conditional":        true,
		"parallel":           true,
		"parallel.fan_in":    true,
		"tool":               true,
		"stack.manager_loop": true,
		"wait.human":         true,
	}

	for _, node := range graph.Nodes {
		// Step 1: If node has an explicit type attribute that maps to a known
		// handler, use that. If it maps to "codergen", it counts.
		if node.Attrs != nil {
			if typeName, ok := node.Attrs["type"]; ok && typeName != "" {
				if knownTypes[typeName] {
					if typeName == "codergen" {
						return true
					}
					// Explicit non-codergen type: skip this node
					continue
				}
				// Unknown explicit type: falls through to shape-based resolution
			}
		}

		// Step 2: Shape-based resolution
		shape := ""
		if node.Attrs != nil {
			shape = node.Attrs["shape"]
		}
		handlerType := ShapeToHandlerType(shape)
		if handlerType == "codergen" {
			return true
		}
	}

	return false
}
