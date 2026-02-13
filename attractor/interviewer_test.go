// ABOUTME: Tests for the Interviewer interface and all built-in implementations.
// ABOUTME: Covers AutoApprove, Callback, Queue, Recording, and Console interviewers.
package attractor

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// --- AutoApproveInterviewer Tests ---

func TestAutoApproveReturnsDefaultAnswer(t *testing.T) {
	iv := NewAutoApproveInterviewer("yes")
	answer, err := iv.Ask(context.Background(), "Approve deployment?", []string{"yes", "no"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if answer != "yes" {
		t.Errorf("expected 'yes', got %q", answer)
	}
}

func TestAutoApproveReturnsFirstOptionWhenNoDefault(t *testing.T) {
	iv := NewAutoApproveInterviewer("")
	answer, err := iv.Ask(context.Background(), "Pick one", []string{"alpha", "beta", "gamma"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if answer != "alpha" {
		t.Errorf("expected 'alpha', got %q", answer)
	}
}

func TestAutoApproveWithEmptyOptionsReturnsDefault(t *testing.T) {
	iv := NewAutoApproveInterviewer("fallback")
	answer, err := iv.Ask(context.Background(), "Free text question?", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if answer != "fallback" {
		t.Errorf("expected 'fallback', got %q", answer)
	}
}

func TestAutoApproveWithEmptyOptionsAndNoDefault(t *testing.T) {
	iv := NewAutoApproveInterviewer("")
	answer, err := iv.Ask(context.Background(), "Free text question?", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if answer != "" {
		t.Errorf("expected empty string, got %q", answer)
	}
}

// --- CallbackInterviewer Tests ---

func TestCallbackDelegatesCorrectly(t *testing.T) {
	called := false
	fn := func(ctx context.Context, question string, options []string) (string, error) {
		called = true
		if question != "What color?" {
			t.Errorf("expected question 'What color?', got %q", question)
		}
		if len(options) != 2 || options[0] != "red" || options[1] != "blue" {
			t.Errorf("unexpected options: %v", options)
		}
		return "red", nil
	}
	iv := NewCallbackInterviewer(fn)
	answer, err := iv.Ask(context.Background(), "What color?", []string{"red", "blue"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("callback was not called")
	}
	if answer != "red" {
		t.Errorf("expected 'red', got %q", answer)
	}
}

func TestCallbackPropagatesErrors(t *testing.T) {
	expectedErr := errors.New("callback failed")
	fn := func(ctx context.Context, question string, options []string) (string, error) {
		return "", expectedErr
	}
	iv := NewCallbackInterviewer(fn)
	_, err := iv.Ask(context.Background(), "Will this fail?", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected %v, got %v", expectedErr, err)
	}
}

// --- QueueInterviewer Tests ---

func TestQueueReturnsFIFOOrder(t *testing.T) {
	iv := NewQueueInterviewer("first", "second", "third")

	a1, err := iv.Ask(context.Background(), "Q1?", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a1 != "first" {
		t.Errorf("expected 'first', got %q", a1)
	}

	a2, err := iv.Ask(context.Background(), "Q2?", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a2 != "second" {
		t.Errorf("expected 'second', got %q", a2)
	}

	a3, err := iv.Ask(context.Background(), "Q3?", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a3 != "third" {
		t.Errorf("expected 'third', got %q", a3)
	}
}

func TestQueueReturnsErrorWhenExhausted(t *testing.T) {
	iv := NewQueueInterviewer("only-one")

	_, err := iv.Ask(context.Background(), "Q1?", nil)
	if err != nil {
		t.Fatalf("unexpected error on first ask: %v", err)
	}

	_, err = iv.Ask(context.Background(), "Q2?", nil)
	if err == nil {
		t.Fatal("expected error when queue exhausted, got nil")
	}
	if !strings.Contains(err.Error(), "exhausted") {
		t.Errorf("expected error mentioning 'exhausted', got: %v", err)
	}
}

// --- RecordingInterviewer Tests ---

func TestRecordingWrapsAndRecords(t *testing.T) {
	inner := NewAutoApproveInterviewer("approved")
	iv := NewRecordingInterviewer(inner)

	answer, err := iv.Ask(context.Background(), "Approve?", []string{"approved", "rejected"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if answer != "approved" {
		t.Errorf("expected 'approved', got %q", answer)
	}

	recs := iv.Recordings()
	if len(recs) != 1 {
		t.Fatalf("expected 1 recording, got %d", len(recs))
	}
	if recs[0].Question != "Approve?" {
		t.Errorf("expected question 'Approve?', got %q", recs[0].Question)
	}
	if recs[0].Answer != "approved" {
		t.Errorf("expected answer 'approved', got %q", recs[0].Answer)
	}
	if len(recs[0].Options) != 2 {
		t.Errorf("expected 2 options, got %d", len(recs[0].Options))
	}
}

func TestRecordingDelegatesToInner(t *testing.T) {
	inner := NewQueueInterviewer("a1", "a2")
	iv := NewRecordingInterviewer(inner)

	ans1, _ := iv.Ask(context.Background(), "Q1?", nil)
	ans2, _ := iv.Ask(context.Background(), "Q2?", nil)

	if ans1 != "a1" {
		t.Errorf("expected 'a1', got %q", ans1)
	}
	if ans2 != "a2" {
		t.Errorf("expected 'a2', got %q", ans2)
	}

	recs := iv.Recordings()
	if len(recs) != 2 {
		t.Fatalf("expected 2 recordings, got %d", len(recs))
	}
}

// --- ConsoleInterviewer Tests ---

func TestConsoleReadsFromReader(t *testing.T) {
	input := strings.NewReader("my answer\n")
	output := &bytes.Buffer{}
	iv := NewConsoleInterviewerWithIO(input, output)

	answer, err := iv.Ask(context.Background(), "What is your name?", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if answer != "my answer" {
		t.Errorf("expected 'my answer', got %q", answer)
	}
	if !strings.Contains(output.String(), "What is your name?") {
		t.Errorf("expected question in output, got %q", output.String())
	}
}

func TestConsoleValidatesOptions(t *testing.T) {
	input := strings.NewReader("beta\n")
	output := &bytes.Buffer{}
	iv := NewConsoleInterviewerWithIO(input, output)

	answer, err := iv.Ask(context.Background(), "Pick one", []string{"alpha", "beta", "gamma"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if answer != "beta" {
		t.Errorf("expected 'beta', got %q", answer)
	}
}

func TestConsoleRejectsInvalidOption(t *testing.T) {
	// Provide an invalid answer followed by EOF so it doesn't hang
	input := strings.NewReader("invalid\n")
	output := &bytes.Buffer{}
	iv := NewConsoleInterviewerWithIO(input, output)

	_, err := iv.Ask(context.Background(), "Pick one", []string{"alpha", "beta"})
	if err == nil {
		t.Fatal("expected error for invalid option, got nil")
	}
}

// --- Context Cancellation Tests ---

func TestAutoApproveRespectsContextCancellation(t *testing.T) {
	iv := NewAutoApproveInterviewer("yes")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := iv.Ask(ctx, "Should proceed?", nil)
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
}

func TestCallbackRespectsContextCancellation(t *testing.T) {
	fn := func(ctx context.Context, question string, options []string) (string, error) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
			return "answer", nil
		}
	}
	iv := NewCallbackInterviewer(fn)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := iv.Ask(ctx, "Will this be cancelled?", nil)
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
}

func TestQueueRespectsContextCancellation(t *testing.T) {
	iv := NewQueueInterviewer("answer")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := iv.Ask(ctx, "Q?", nil)
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
}

func TestConsoleRespectsContextCancellation(t *testing.T) {
	// Use a reader that will block, combined with a cancelled context
	pr := &slowReader{}
	output := &bytes.Buffer{}
	iv := NewConsoleInterviewerWithIO(pr, output)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := iv.Ask(ctx, "This should timeout", nil)
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
}

// --- Question Struct Tests ---

func TestQuestionStructFields(t *testing.T) {
	q := Question{
		ID:       "q1",
		Text:     "Approve?",
		Options:  []string{"yes", "no"},
		Default:  "yes",
		Metadata: map[string]string{"stage": "review"},
	}
	if q.ID != "q1" {
		t.Errorf("expected ID 'q1', got %q", q.ID)
	}
	if q.Text != "Approve?" {
		t.Errorf("expected Text 'Approve?', got %q", q.Text)
	}
	if len(q.Options) != 2 {
		t.Errorf("expected 2 options, got %d", len(q.Options))
	}
	if q.Default != "yes" {
		t.Errorf("expected default 'yes', got %q", q.Default)
	}
	if q.Metadata["stage"] != "review" {
		t.Errorf("expected metadata stage='review', got %q", q.Metadata["stage"])
	}
}

// --- WithNodeID / NodeIDFromContext Tests ---

func TestWithNodeID(t *testing.T) {
	ctx := context.Background()
	ctx = WithNodeID(ctx, "deploy")

	got := NodeIDFromContext(ctx)
	if got != "deploy" {
		t.Errorf("expected 'deploy', got %q", got)
	}
}

func TestNodeIDFromContext_Missing(t *testing.T) {
	ctx := context.Background()
	got := NodeIDFromContext(ctx)
	if got != "" {
		t.Errorf("expected empty string for bare context, got %q", got)
	}
}

func TestNodeIDFromContext_WrongType(t *testing.T) {
	// Stuff a non-string value under the same key type — should return ""
	ctx := context.WithValue(context.Background(), nodeContextKey{}, 42)
	got := NodeIDFromContext(ctx)
	if got != "" {
		t.Errorf("expected empty string for non-string value, got %q", got)
	}
}

func TestConsoleInterviewerShowsNodeContext(t *testing.T) {
	input := strings.NewReader("yes\n")
	output := &bytes.Buffer{}
	iv := NewConsoleInterviewerWithIO(input, output)

	ctx := WithNodeID(context.Background(), "deploy")
	answer, err := iv.Ask(ctx, "Approve deployment?", []string{"yes", "no"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if answer != "yes" {
		t.Errorf("expected 'yes', got %q", answer)
	}
	if !strings.Contains(output.String(), "[Node: deploy]") {
		t.Errorf("expected output to contain '[Node: deploy]', got %q", output.String())
	}
}

func TestConsoleInterviewerNoNodeContext(t *testing.T) {
	input := strings.NewReader("yes\n")
	output := &bytes.Buffer{}
	iv := NewConsoleInterviewerWithIO(input, output)

	answer, err := iv.Ask(context.Background(), "Approve?", []string{"yes", "no"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if answer != "yes" {
		t.Errorf("expected 'yes', got %q", answer)
	}
	if strings.Contains(output.String(), "[Node:") {
		t.Errorf("expected no '[Node:' header when node ID is empty, got %q", output.String())
	}
}

// slowReader is a reader that blocks forever (for testing context cancellation)
type slowReader struct{}

func (r *slowReader) Read(p []byte) (n int, err error) {
	// Block forever by waiting on a channel that never sends
	<-make(chan struct{})
	return 0, nil
}
