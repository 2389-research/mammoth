// ABOUTME: Interviewer interface and built-in implementations for human-in-the-loop interaction.
// ABOUTME: Provides AutoApprove, Callback, Queue, Recording, and Console interviewers.
package attractor

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

// Interviewer is the abstraction for human-in-the-loop interaction.
// Any frontend (CLI, web, Slack, programmatic) implements this interface.
type Interviewer interface {
	Ask(ctx context.Context, question string, options []string) (string, error)
}

// nodeContextKey is the context key for attaching the originating node ID
// to an Ask context. This lets Interviewer implementations display which
// pipeline node triggered a human gate without changing the Interviewer
// interface signature.
type nodeContextKey struct{}

// WithNodeID attaches a pipeline node ID to the context.
// The node ID can later be extracted with NodeIDFromContext.
func WithNodeID(ctx context.Context, nodeID string) context.Context {
	return context.WithValue(ctx, nodeContextKey{}, nodeID)
}

// NodeIDFromContext extracts the pipeline node ID from the context.
// Returns an empty string when no node ID is present.
func NodeIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(nodeContextKey{}).(string); ok {
		return v
	}
	return ""
}

// Question represents a structured question for human review.
type Question struct {
	ID       string
	Text     string
	Options  []string          // empty means free-text
	Default  string            // default answer if timeout
	Metadata map[string]string // arbitrary key-value pairs
}

// QAPair records a question-answer interaction for auditing and replay.
type QAPair struct {
	Question string
	Options  []string
	Answer   string
}

// --- AutoApproveInterviewer ---

// AutoApproveInterviewer always returns a configured answer (or the first option).
// Intended for testing and automated pipelines where no human is available.
type AutoApproveInterviewer struct {
	defaultAnswer string
}

// NewAutoApproveInterviewer creates an AutoApproveInterviewer with the given default answer.
func NewAutoApproveInterviewer(defaultAnswer string) *AutoApproveInterviewer {
	return &AutoApproveInterviewer{defaultAnswer: defaultAnswer}
}

// Ask returns the configured default answer, or the first option if no default is set.
func (a *AutoApproveInterviewer) Ask(ctx context.Context, question string, options []string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if a.defaultAnswer != "" {
		return a.defaultAnswer, nil
	}
	if len(options) > 0 {
		return options[0], nil
	}
	return "", nil
}

// --- CallbackInterviewer ---

// CallbackInterviewer delegates question answering to a provided callback function.
// Useful for integrating with external systems (Slack, web UI, API).
type CallbackInterviewer struct {
	fn func(ctx context.Context, question string, options []string) (string, error)
}

// NewCallbackInterviewer creates a CallbackInterviewer that delegates to the given function.
func NewCallbackInterviewer(fn func(ctx context.Context, question string, options []string) (string, error)) *CallbackInterviewer {
	return &CallbackInterviewer{fn: fn}
}

// Ask delegates to the callback function.
func (c *CallbackInterviewer) Ask(ctx context.Context, question string, options []string) (string, error) {
	return c.fn(ctx, question, options)
}

// --- QueueInterviewer ---

// QueueInterviewer reads answers from a pre-filled queue (FIFO order).
// Intended for deterministic testing and replay scenarios.
type QueueInterviewer struct {
	answers []string
	mu      sync.Mutex
}

// NewQueueInterviewer creates a QueueInterviewer pre-loaded with the given answers.
func NewQueueInterviewer(answers ...string) *QueueInterviewer {
	return &QueueInterviewer{answers: append([]string{}, answers...)}
}

// Ask dequeues the next answer. Returns an error when the queue is exhausted.
func (q *QueueInterviewer) Ask(ctx context.Context, question string, options []string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.answers) == 0 {
		return "", fmt.Errorf("answer queue exhausted: no answer for question %q", question)
	}
	answer := q.answers[0]
	q.answers = q.answers[1:]
	return answer, nil
}

// --- RecordingInterviewer ---

// RecordingInterviewer wraps another Interviewer and records all Q&A pairs.
// Useful for replay, debugging, and audit trails.
type RecordingInterviewer struct {
	inner      Interviewer
	recordings []QAPair
	mu         sync.Mutex
}

// NewRecordingInterviewer wraps the given inner Interviewer with recording capability.
func NewRecordingInterviewer(inner Interviewer) *RecordingInterviewer {
	return &RecordingInterviewer{
		inner:      inner,
		recordings: make([]QAPair, 0),
	}
}

// Ask delegates to the inner Interviewer and records the Q&A pair.
func (r *RecordingInterviewer) Ask(ctx context.Context, question string, options []string) (string, error) {
	answer, err := r.inner.Ask(ctx, question, options)
	if err != nil {
		return "", err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	optionsCopy := make([]string, len(options))
	copy(optionsCopy, options)
	r.recordings = append(r.recordings, QAPair{
		Question: question,
		Options:  optionsCopy,
		Answer:   answer,
	})
	return answer, nil
}

// Recordings returns a copy of all recorded Q&A pairs.
func (r *RecordingInterviewer) Recordings() []QAPair {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]QAPair, len(r.recordings))
	copy(result, r.recordings)
	return result
}

// --- ConsoleInterviewer ---

// ConsoleInterviewer reads answers from an io.Reader and writes prompts to an io.Writer.
// By default, uses os.Stdin and os.Stdout.
type ConsoleInterviewer struct {
	reader io.Reader
	writer io.Writer
}

// NewConsoleInterviewer creates a ConsoleInterviewer using os.Stdin and os.Stdout.
func NewConsoleInterviewer() *ConsoleInterviewer {
	return &ConsoleInterviewer{reader: os.Stdin, writer: os.Stdout}
}

// NewConsoleInterviewerWithIO creates a ConsoleInterviewer with configurable reader and writer.
func NewConsoleInterviewerWithIO(r io.Reader, w io.Writer) *ConsoleInterviewer {
	return &ConsoleInterviewer{reader: r, writer: w}
}

// Ask prints the question and options, then reads a line from the reader.
// If options are provided, validates the answer is one of the options.
func (c *ConsoleInterviewer) Ask(ctx context.Context, question string, options []string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	// Print node context header when a node ID is attached to the context
	if nodeID := NodeIDFromContext(ctx); nodeID != "" {
		fmt.Fprintf(c.writer, "[Node: %s]\n", nodeID)
	}

	// Print the question
	fmt.Fprintf(c.writer, "[?] %s\n", question)
	if len(options) > 0 {
		for _, opt := range options {
			fmt.Fprintf(c.writer, "  - %s\n", opt)
		}
		fmt.Fprint(c.writer, "Select: ")
	} else {
		fmt.Fprint(c.writer, "> ")
	}

	// Read input in a goroutine to support context cancellation
	type readResult struct {
		line string
		err  error
	}
	ch := make(chan readResult, 1)
	go func() {
		scanner := bufio.NewScanner(c.reader)
		if scanner.Scan() {
			ch <- readResult{line: strings.TrimSpace(scanner.Text()), err: nil}
		} else {
			err := scanner.Err()
			if err == nil {
				err = io.EOF
			}
			ch <- readResult{err: err}
		}
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case result := <-ch:
		if result.err != nil {
			return "", fmt.Errorf("reading input: %w", result.err)
		}
		// Validate against options if provided
		if len(options) > 0 {
			for _, opt := range options {
				if strings.EqualFold(result.line, opt) {
					return opt, nil
				}
			}
			return "", fmt.Errorf("invalid option %q: must be one of %v", result.line, options)
		}
		return result.line, nil
	}
}
