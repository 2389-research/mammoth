// ABOUTME: Bridge connecting the tracker pipeline engine to the Bubble Tea message loop.
// ABOUTME: Provides EventBridge for event injection, and tea.Cmd factories for pipeline execution, human gates, and ticks.
package tui

import (
	"context"
	"time"

	"github.com/2389-research/tracker/agent"
	"github.com/2389-research/tracker/pipeline"
	tea "github.com/charmbracelet/bubbletea"
)

// EventBridge wraps a tea.Program's Send method for injecting engine events
// into the Bubble Tea message loop. It provides separate handler factories
// for pipeline events and agent events.
type EventBridge struct {
	send func(msg tea.Msg)
}

// NewEventBridge creates an EventBridge that sends messages via the given function.
// Typically called with program.Send as the argument.
func NewEventBridge(send func(msg tea.Msg)) *EventBridge {
	return &EventBridge{send: send}
}

// PipelineHandler returns a pipeline.PipelineEventHandlerFunc that forwards
// pipeline events to the TUI as EngineEventMsg with PipelineEvent set.
func (b *EventBridge) PipelineHandler() pipeline.PipelineEventHandlerFunc {
	return func(evt pipeline.PipelineEvent) {
		b.send(EngineEventMsg{PipelineEvent: &evt})
	}
}

// AgentHandler returns an agent.EventHandlerFunc that forwards agent events
// to the TUI as EngineEventMsg with AgentEvent set.
func (b *EventBridge) AgentHandler() agent.EventHandlerFunc {
	return func(evt agent.Event) {
		b.send(EngineEventMsg{AgentEvent: &evt})
	}
}

// RunPipelineCmd returns a tea.Cmd that runs the engine.
// When the pipeline completes (or fails), it sends a PipelineResultMsg.
// The context allows cancellation when the user quits the TUI.
func RunPipelineCmd(ctx context.Context, engine *pipeline.Engine) tea.Cmd {
	return func() tea.Msg {
		result, err := engine.Run(ctx)
		return PipelineResultMsg{Result: result, Err: err}
	}
}

// WaitForHumanGateCmd returns a tea.Cmd that blocks on the given request channel
// and sends a HumanGateRequestMsg when a request arrives.
func WaitForHumanGateCmd(requestCh <-chan gateRequest) tea.Cmd {
	return func() tea.Msg {
		req, ok := <-requestCh
		if !ok {
			return nil // channel closed, no more gates
		}
		return HumanGateRequestMsg{
			Question:      req.question,
			Options:       req.options,
			DefaultChoice: req.defaultChoice,
			NodeID:        req.nodeID,
		}
	}
}

// TickCmd returns a tea.Cmd that sends a TickMsg after the given interval.
// Used for spinner animation and periodic UI refreshes.
func TickCmd(interval time.Duration) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(interval)
		return TickMsg{Time: time.Now()}
	}
}
