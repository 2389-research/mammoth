// ABOUTME: HumanGateModel bridges tracker's handlers.Interviewer with Bubble Tea's message loop via channels.
// ABOUTME: Renders a styled dialog with text input when a human gate node requires user interaction.
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// gateRequest carries a human gate question and its options from the engine
// goroutine into the TUI message loop. Used by both bridge.go and human_gate.go.
type gateRequest struct {
	question      string
	options       []string
	defaultChoice string
	nodeID        string // originating pipeline node ID (may be empty)
}

// gateResponse carries the user's answer from the TUI message loop back to the engine goroutine.
type gateResponse struct {
	answer string
	err    error
}

// HumanGateModel implements handlers.Interviewer and handlers.FreeformInterviewer,
// rendering a text input dialog inside the Bubble Tea TUI. The engine calls Ask()
// from its own goroutine, which sends a request on requestCh and blocks on responseCh.
// A persistent tea.Cmd polls requestCh and injects HumanGateRequestMsg into the
// message loop. When the user presses Enter, Submit() sends the answer on responseCh,
// unblocking the engine.
type HumanGateModel struct {
	textInput  textinput.Model
	question   string
	options    []string
	active     bool
	requestCh  chan gateRequest
	responseCh chan gateResponse

	// Node context fields for displaying which pipeline node triggered the gate
	nodeID    string // originating node ID
	nodeLabel string // human-readable label (set by StreamModel)
	position  string // e.g. "step 4/6" (set by StreamModel)
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

// Ask implements handlers.Interviewer. It sends the question on requestCh for the
// TUI to pick up, then blocks until a response arrives on responseCh.
// This method is goroutine-safe and is called from the engine goroutine.
func (m *HumanGateModel) Ask(prompt string, choices []string, defaultChoice string) (string, error) {
	req := gateRequest{
		question:      prompt,
		options:       choices,
		defaultChoice: defaultChoice,
	}

	// Send request
	m.requestCh <- req

	// Wait for response
	resp := <-m.responseCh
	return resp.answer, resp.err
}

// AskFreeform implements handlers.FreeformInterviewer. It sends the prompt as a
// question with no options, then blocks until a response arrives.
func (m *HumanGateModel) AskFreeform(prompt string) (string, error) {
	return m.Ask(prompt, nil, "")
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

// SetNodeContext attaches pipeline node context for display in the gate dialog.
// Call this after SetActive to add the context header showing which node
// triggered the gate and where it is in the pipeline.
func (m *HumanGateModel) SetNodeContext(nodeID, nodeLabel, position string) {
	m.nodeID = nodeID
	m.nodeLabel = nodeLabel
	m.position = position
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
	m.nodeID = ""
	m.nodeLabel = ""
	m.position = ""
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

	// Node context header (when available)
	if m.nodeLabel != "" {
		if m.position != "" {
			b.WriteString(fmt.Sprintf("⬡ %s — waiting for input (%s)\n\n", m.nodeLabel, m.position))
		} else {
			b.WriteString(fmt.Sprintf("⬡ %s — waiting for input\n\n", m.nodeLabel))
		}
	}

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
