// ABOUTME: Loop restart handling for edges with loop_restart=true attribute.
// ABOUTME: Provides ErrLoopRestart sentinel, EdgeHasLoopRestart check, RestartConfig, and restart wrapper logic.
package attractor

import "fmt"

// ErrLoopRestart is a sentinel error returned by executeGraph when a selected edge
// has the loop_restart=true attribute. It carries the target node ID so the engine
// can restart traversal from that node with a fresh context.
type ErrLoopRestart struct {
	TargetNode string
}

// Error implements the error interface.
func (e *ErrLoopRestart) Error() string {
	return fmt.Sprintf("loop_restart triggered, restarting from node %q", e.TargetNode)
}

// EdgeHasLoopRestart returns true if the edge has the loop_restart attribute set to "true".
func EdgeHasLoopRestart(edge *Edge) bool {
	if edge.Attrs == nil {
		return false
	}
	return edge.Attrs["loop_restart"] == "true"
}

// RestartConfig controls loop restart behavior.
type RestartConfig struct {
	MaxRestarts int // maximum number of restarts before giving up (default 5)
}

// DefaultRestartConfig returns a RestartConfig with sensible defaults.
func DefaultRestartConfig() *RestartConfig {
	return &RestartConfig{
		MaxRestarts: 5,
	}
}
