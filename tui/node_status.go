// ABOUTME: Defines the NodeStatus enum representing pipeline node execution states.
// ABOUTME: Provides String/Icon methods and spinner animation frames for TUI rendering.
package tui

// NodeStatus represents the execution state of a pipeline node.
type NodeStatus int

const (
	NodePending   NodeStatus = iota // Node has not started
	NodeRunning                     // Node is currently executing
	NodeCompleted                   // Node finished successfully
	NodeFailed                      // Node finished with an error
	NodeSkipped                     // Node was skipped
)

// String returns the lowercase name of the status.
func (s NodeStatus) String() string {
	switch s {
	case NodePending:
		return "pending"
	case NodeRunning:
		return "running"
	case NodeCompleted:
		return "completed"
	case NodeFailed:
		return "failed"
	case NodeSkipped:
		return "skipped"
	default:
		return "unknown"
	}
}

// Icon returns a bracket-style status marker for TUI display.
func (s NodeStatus) Icon() string {
	switch s {
	case NodePending:
		return "[ ]"
	case NodeRunning:
		return "[~]"
	case NodeCompleted:
		return "[*]"
	case NodeFailed:
		return "[!]"
	case NodeSkipped:
		return "[-]"
	default:
		return "[?]"
	}
}

// SpinnerFrames contains the Braille-dot animation frames for indicating
// active/running nodes in the TUI.
var SpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
