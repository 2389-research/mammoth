// ABOUTME: Tests for HumanGateModel, the TUI bridge between attractor.Interviewer and Bubble Tea.
// ABOUTME: Covers model construction, activation, submission, view rendering, Ask channel bridge, and key forwarding.
package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestNewHumanGateModel(t *testing.T) {
	m := NewHumanGateModel()

	if m.requestCh == nil {
		t.Error("expected requestCh to be non-nil")
	}
	if m.responseCh == nil {
		t.Error("expected responseCh to be non-nil")
	}
	if m.active {
		t.Error("expected model to start inactive")
	}
	if m.question != "" {
		t.Errorf("expected empty question, got %q", m.question)
	}
	if len(m.options) != 0 {
		t.Errorf("expected no options, got %v", m.options)
	}
}

func TestHumanGateModelIsActiveInitiallyFalse(t *testing.T) {
	m := NewHumanGateModel()
	if m.IsActive() {
		t.Error("expected IsActive() to return false initially")
	}
}

func TestHumanGateModelSetActive(t *testing.T) {
	m := NewHumanGateModel()

	question := "Approve deployment?"
	options := []string{"yes", "no"}
	m.SetActive(question, options)

	if !m.active {
		t.Error("expected model to be active after SetActive")
	}
	if m.question != question {
		t.Errorf("expected question %q, got %q", question, m.question)
	}
	if len(m.options) != len(options) {
		t.Errorf("expected %d options, got %d", len(options), len(m.options))
	}
	for i, opt := range options {
		if m.options[i] != opt {
			t.Errorf("option[%d]: expected %q, got %q", i, opt, m.options[i])
		}
	}
}

func TestHumanGateModelSetActiveEnablesFocus(t *testing.T) {
	m := NewHumanGateModel()

	m.SetActive("Do you approve?", nil)

	if !m.textInput.Focused() {
		t.Error("expected textInput to be focused after SetActive")
	}
}

func TestHumanGateModelSubmit(t *testing.T) {
	m := NewHumanGateModel()
	m.SetActive("Approve?", []string{"yes", "no"})

	// Set a value in the text input
	m.textInput.SetValue("yes")

	// Read from responseCh in a goroutine
	done := make(chan gateResponse, 1)
	go func() {
		resp := <-m.responseCh
		done <- resp
	}()

	m.Submit()

	select {
	case resp := <-done:
		if resp.answer != "yes" {
			t.Errorf("expected answer %q, got %q", "yes", resp.answer)
		}
		if resp.err != nil {
			t.Errorf("expected nil error, got %v", resp.err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for response on responseCh")
	}

	if m.active {
		t.Error("expected model to be inactive after Submit")
	}
}

func TestHumanGateModelSubmitClearsInput(t *testing.T) {
	m := NewHumanGateModel()
	m.SetActive("Question?", nil)
	m.textInput.SetValue("my answer")

	// Consume the response so Submit doesn't block
	go func() {
		<-m.responseCh
	}()

	m.Submit()

	if m.textInput.Value() != "" {
		t.Errorf("expected text input to be cleared after Submit, got %q", m.textInput.Value())
	}
}

func TestHumanGateModelViewInactive(t *testing.T) {
	m := NewHumanGateModel()
	view := m.View()

	if view != "" {
		t.Errorf("expected empty view when inactive, got %q", view)
	}
}

func TestHumanGateModelViewActive(t *testing.T) {
	m := NewHumanGateModel()
	m.SetActive("Approve the deployment?", nil)

	view := m.View()
	if !strings.Contains(view, "Approve the deployment?") {
		t.Errorf("expected view to contain question text, got:\n%s", view)
	}
}

func TestHumanGateModelViewWithOptions(t *testing.T) {
	m := NewHumanGateModel()
	m.SetActive("Choose environment:", []string{"staging", "production"})

	view := m.View()
	if !strings.Contains(view, "staging") {
		t.Errorf("expected view to contain option 'staging', got:\n%s", view)
	}
	if !strings.Contains(view, "production") {
		t.Errorf("expected view to contain option 'production', got:\n%s", view)
	}
}

func TestHumanGateModelAskSendsRequest(t *testing.T) {
	m := NewHumanGateModel()

	// Start Ask in a goroutine (it blocks)
	go func() {
		_, _ = m.Ask(context.Background(), "Continue?", []string{"yes", "no"})
	}()

	// Read the request from the channel
	select {
	case req := <-m.requestCh:
		if req.question != "Continue?" {
			t.Errorf("expected question %q, got %q", "Continue?", req.question)
		}
		if len(req.options) != 2 || req.options[0] != "yes" || req.options[1] != "no" {
			t.Errorf("expected options [yes no], got %v", req.options)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for request on requestCh")
	}

	// Unblock Ask by sending a response
	m.responseCh <- gateResponse{answer: "yes"}
}

func TestHumanGateModelAskBlocksUntilResponse(t *testing.T) {
	m := NewHumanGateModel()

	resultCh := make(chan struct {
		answer string
		err    error
	}, 1)

	go func() {
		answer, err := m.Ask(context.Background(), "Proceed?", nil)
		resultCh <- struct {
			answer string
			err    error
		}{answer, err}
	}()

	// Drain the request
	select {
	case <-m.requestCh:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for request")
	}

	// Verify Ask hasn't returned yet
	select {
	case <-resultCh:
		t.Fatal("Ask returned before response was sent")
	case <-time.After(50 * time.Millisecond):
		// expected - Ask is still blocking
	}

	// Send response
	m.responseCh <- gateResponse{answer: "go ahead"}

	// Verify Ask returns the response
	select {
	case result := <-resultCh:
		if result.answer != "go ahead" {
			t.Errorf("expected answer %q, got %q", "go ahead", result.answer)
		}
		if result.err != nil {
			t.Errorf("expected nil error, got %v", result.err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for Ask to return")
	}
}

func TestHumanGateModelAskContextCancelled(t *testing.T) {
	m := NewHumanGateModel()

	ctx, cancel := context.WithCancel(context.Background())

	resultCh := make(chan struct {
		answer string
		err    error
	}, 1)

	go func() {
		answer, err := m.Ask(ctx, "Waiting forever?", nil)
		resultCh <- struct {
			answer string
			err    error
		}{answer, err}
	}()

	// Drain the request so Ask moves to waiting for response
	select {
	case <-m.requestCh:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for request")
	}

	// Cancel the context
	cancel()

	// Verify Ask returns with context error
	select {
	case result := <-resultCh:
		if result.err == nil {
			t.Fatal("expected error from cancelled context, got nil")
		}
		if result.err != context.Canceled {
			t.Errorf("expected context.Canceled, got %v", result.err)
		}
		if result.answer != "" {
			t.Errorf("expected empty answer, got %q", result.answer)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for Ask to return after cancel")
	}
}

func TestHumanGateModelRequestChan(t *testing.T) {
	m := NewHumanGateModel()

	ch := m.RequestChan()
	if ch == nil {
		t.Fatal("expected non-nil request channel")
	}

	// Verify it's the same channel
	if ch != m.requestCh {
		t.Error("expected RequestChan() to return the model's requestCh")
	}
}

func TestHumanGateModelUpdateKeyMsg(t *testing.T) {
	m := NewHumanGateModel()
	m.SetActive("Type something:", nil)

	// Send a key press for the letter 'a'
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}
	updated := m.Update(msg)

	// The textinput should have received the key
	if updated.textInput.Value() != "a" {
		t.Errorf("expected textInput value 'a' after key event, got %q", updated.textInput.Value())
	}
}

func TestHumanGateModelAskContextAlreadyCancelled(t *testing.T) {
	m := NewHumanGateModel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before calling Ask

	answer, err := m.Ask(ctx, "Already cancelled?", nil)
	if err == nil {
		t.Fatal("expected error from pre-cancelled context, got nil")
	}
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if answer != "" {
		t.Errorf("expected empty answer, got %q", answer)
	}
}

func TestHumanGateModelSetActiveWithNilOptions(t *testing.T) {
	m := NewHumanGateModel()
	m.SetActive("Free text question:", nil)

	if !m.active {
		t.Error("expected model to be active")
	}
	if m.options != nil {
		t.Errorf("expected nil options, got %v", m.options)
	}
}

func TestHumanGateModelSetActiveWithEmptyOptions(t *testing.T) {
	m := NewHumanGateModel()
	m.SetActive("Free text:", []string{})

	if !m.active {
		t.Error("expected model to be active")
	}
	if len(m.options) != 0 {
		t.Errorf("expected empty options, got %v", m.options)
	}
}
