// ABOUTME: Bubble Tea message types used in the TUI message loop.
// ABOUTME: Each type wraps domain events for the tea.Msg interface (which is interface{}).
package tui

import (
	"time"

	"github.com/2389-research/mammoth/attractor"
)

// EngineEventMsg wraps an attractor.EngineEvent for the Bubble Tea message loop.
type EngineEventMsg struct {
	Event attractor.EngineEvent
}

// PipelineResultMsg signals that the pipeline has finished executing.
type PipelineResultMsg struct {
	Result *attractor.RunResult
	Err    error
}

// TickMsg is sent periodically to update timers and spinners.
type TickMsg struct {
	Time time.Time
}

// HumanGateRequestMsg signals that a human gate node needs user input.
type HumanGateRequestMsg struct {
	Question string
	Options  []string
}

// HumanGateResponseMsg carries the user's response back to the engine.
type HumanGateResponseMsg struct {
	Answer string
	Err    error
}

// WindowSizeMsg is forwarded from tea.WindowSizeMsg for layout updates.
type WindowSizeMsg struct {
	Width  int
	Height int
}
