// ABOUTME: HumanGateModel bridges attractor.Interviewer with Bubble Tea's message loop via channels.
// ABOUTME: Renders a styled dialog with text input when a human gate node requires user interaction.
package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/2389-research/mammoth/attractor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// Compile-time interface assertion: *HumanGateModel implements attractor.Interviewer.
var _ attractor.Interviewer = (*HumanGateModel)(nil)

// gateResponse carries the user's answer from the TUI message loop back to the engine goroutine.
type gateResponse struct {
	answer string
	err    error
}

// HumanGateModel implements attractor.Interviewer and renders a text input dialog
// inside the Bubble Tea TUI. The engine calls Ask() from its own goroutine, which
// sends a request on requestCh and blocks on responseCh. A persistent tea.Cmd polls
// requestCh and injects HumanGateRequestMsg into the message loop. When the user
// presses Enter, Submit() sends the answer on responseCh, unblocking the engine.
type HumanGateModel struct {
	textInput  textinput.Model
	question   string
	options    []string
	active     bool
	requestCh  chan gateRequest
	responseCh chan gateResponse
}

// NewHumanGateModel creates a HumanGateModel with initialized channels and text input.
func NewHumanGateModel() HumanGateModel {
	ti := textinput.New()
	ti.Prompt = "> "
	ti.Placeholder = "Type your answer..."

	return HumanGateModel{
		textInput:  ti,
		requestCh:  make(chan gateRequest),
		responseCh: make(chan gateResponse, 1),
	}
}

// Ask implements attractor.Interviewer. It sends the question on requestCh for the
// TUI to pick up, then blocks until either a response arrives on responseCh or the
// context is cancelled. This method is goroutine-safe and is called from the engine
// goroutine.
func (m *HumanGateModel) Ask(ctx context.Context, question string, options []string) (string, error) {
	// Check context before sending
	if err := ctx.Err(); err != nil {
		return "", err
	}

	req := gateRequest{
		question: question,
		options:  options,
	}

	// Send request, respecting context cancellation
	select {
	case m.requestCh <- req:
	case <-ctx.Done():
		return "", ctx.Err()
	}

	// Wait for response, respecting context cancellation
	select {
	case resp := <-m.responseCh:
		return resp.answer, resp.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// RequestChan returns the request channel for the bridge command to poll.
// A persistent tea.Cmd should read from this channel and inject
// HumanGateRequestMsg into the Bubble Tea message loop.
func (m *HumanGateModel) RequestChan() <-chan gateRequest {
	return m.requestCh
}

// SetActive activates the human gate dialog with the given question and options.
// This is called from the TUI message loop when a HumanGateRequestMsg arrives.
func (m *HumanGateModel) SetActive(question string, options []string) {
	m.question = question
	m.options = options
	m.active = true
	m.textInput.Focus()
}

// Submit sends the current text input value as the response on responseCh,
// then deactivates the dialog and clears the input. Called from the TUI
// message loop when the user presses Enter.
func (m *HumanGateModel) Submit() {
	answer := m.textInput.Value()
	m.responseCh <- gateResponse{answer: answer}
	m.active = false
	m.question = ""
	m.options = nil
	m.textInput.Reset()
	m.textInput.Blur()
}

// IsActive returns whether the human gate dialog is currently visible.
func (m *HumanGateModel) IsActive() bool {
	return m.active
}

// Update handles incoming tea.Msg events. Key events are forwarded to the
// embedded textinput. Returns the updated model.
func (m HumanGateModel) Update(msg tea.Msg) HumanGateModel {
	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	_ = cmd // textinput cmds (cursor blink) are ignored in sub-model updates
	return m
}

// View renders the human gate dialog. Returns an empty string when inactive.
// When active, displays the question, available options (if any), and the
// text input field inside a styled border.
func (m HumanGateModel) View() string {
	if !m.active {
		return ""
	}

	var b strings.Builder

	// Question header
	b.WriteString(fmt.Sprintf("[?] %s\n", m.question))

	// Options list (if provided)
	if len(m.options) > 0 {
		for _, opt := range m.options {
			b.WriteString(fmt.Sprintf("  - %s\n", opt))
		}
	}

	// Text input
	b.WriteString("\n")
	b.WriteString(m.textInput.View())

	return HumanGateStyle.Render(b.String())
}
