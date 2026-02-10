// ABOUTME: Tests for NodeStatus enum type, its String/Icon methods, and SpinnerFrames.
// ABOUTME: Uses table-driven tests covering all status values and edge cases.
package tui

import "testing"

func TestNodeStatusString(t *testing.T) {
	tests := []struct {
		name   string
		status NodeStatus
		want   string
	}{
		{"pending", NodePending, "pending"},
		{"running", NodeRunning, "running"},
		{"completed", NodeCompleted, "completed"},
		{"failed", NodeFailed, "failed"},
		{"skipped", NodeSkipped, "skipped"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.status.String()
			if got != tt.want {
				t.Errorf("NodeStatus(%d).String() = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func TestNodeStatusStringUnknown(t *testing.T) {
	tests := []struct {
		name   string
		status NodeStatus
	}{
		{"negative", NodeStatus(-1)},
		{"out of range high", NodeStatus(99)},
		{"one past last", NodeSkipped + 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.status.String()
			if got != "unknown" {
				t.Errorf("NodeStatus(%d).String() = %q, want %q", tt.status, got, "unknown")
			}
		})
	}
}

func TestNodeStatusIcon(t *testing.T) {
	tests := []struct {
		name   string
		status NodeStatus
		want   string
	}{
		{"pending", NodePending, "[ ]"},
		{"running", NodeRunning, "[~]"},
		{"completed", NodeCompleted, "[*]"},
		{"failed", NodeFailed, "[!]"},
		{"skipped", NodeSkipped, "[-]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.status.Icon()
			if got != tt.want {
				t.Errorf("NodeStatus(%d).Icon() = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func TestSpinnerFramesLength(t *testing.T) {
	if len(SpinnerFrames) != 10 {
		t.Errorf("len(SpinnerFrames) = %d, want 10", len(SpinnerFrames))
	}
}
