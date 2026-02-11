// ABOUTME: Bridge connecting the attractor engine to the Bubble Tea message loop.
// ABOUTME: Provides EventBridge for event injection, and tea.Cmd factories for pipeline execution, human gates, and ticks.
package tui

import (
	"context"
	"time"

	"github.com/2389-research/mammoth/attractor"
	tea "github.com/charmbracelet/bubbletea"
)

// gateRequest carries a human gate question and its options from the engine
// goroutine into the TUI message loop. Used by both bridge.go and human_gate.go.
type gateRequest struct {
	question string
	options  []string
}

// EventBridge wraps a tea.Program's Send method for injecting engine events
// into the Bubble Tea message loop.
type EventBridge struct {
	send func(msg tea.Msg)
}

// NewEventBridge creates an EventBridge that sends messages via the given function.
// Typically called with program.Send as the argument.
func NewEventBridge(send func(msg tea.Msg)) *EventBridge {
	return &EventBridge{send: send}
}

// HandleEvent implements the attractor.EngineConfig.EventHandler signature.
// It wraps the event in an EngineEventMsg and sends it to the TUI.
func (b *EventBridge) HandleEvent(evt attractor.EngineEvent) {
	b.send(EngineEventMsg{Event: evt})
}

// RunPipelineCmd returns a tea.Cmd that runs the engine with the given source DOT.
// When the pipeline completes (or fails), it sends a PipelineResultMsg.
// The context allows cancellation when the user quits the TUI.
func RunPipelineCmd(ctx context.Context, engine *attractor.Engine, source string) tea.Cmd {
	return func() tea.Msg {
		result, err := engine.Run(ctx, source)
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
			Question: req.question,
			Options:  req.options,
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

// WireHumanGate attaches the given HumanGateModel as the Interviewer on
// the engine's WaitForHumanHandler, enabling human-in-the-loop nodes to
// route through the TUI text input instead of the console.
func WireHumanGate(engine *attractor.Engine, gate *HumanGateModel) {
	handler := engine.GetHandler("wait.human")
	if handler == nil {
		return
	}
	if hh, ok := handler.(*attractor.WaitForHumanHandler); ok {
		hh.Interviewer = gate
	}
}
