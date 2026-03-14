// ABOUTME: Bubble Tea message types used in the TUI message loop.
// ABOUTME: Each type wraps domain events for the tea.Msg interface (which is interface{}).
package tui

import (
	"time"

	"github.com/2389-research/tracker/agent"
	"github.com/2389-research/tracker/pipeline"
)

// EngineEventMsg wraps pipeline and agent events for the Bubble Tea message loop.
// Exactly one of PipelineEvent or AgentEvent is non-nil per message.
type EngineEventMsg struct {
	PipelineEvent *pipeline.PipelineEvent
	AgentEvent    *agent.Event
}

// PipelineResultMsg signals that the pipeline has finished executing.
type PipelineResultMsg struct {
	Result *pipeline.EngineResult
	Err    error
}

// TickMsg is sent periodically to update timers and spinners.
type TickMsg struct {
	Time time.Time
}

// HumanGateRequestMsg signals that a human gate node needs user input.
type HumanGateRequestMsg struct {
	Question      string
	Options       []string
	DefaultChoice string
	NodeID        string // originating pipeline node ID (may be empty)
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
