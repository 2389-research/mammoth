// ABOUTME: Sentinel errors for the spec actor command validation.
// ABOUTME: Maps to barnstormer's ActorError enum variants.
package core

import (
	"errors"
	"fmt"

	"github.com/oklog/ulid/v2"
)

var (
	// ErrSpecNotCreated indicates a command requires a spec that hasn't been created yet.
	ErrSpecNotCreated = errors.New("spec not yet created")

	// ErrQuestionAlreadyPending indicates a question is already waiting for an answer.
	ErrQuestionAlreadyPending = errors.New("a question is already pending")

	// ErrNoPendingQuestion indicates there's no question to answer.
	ErrNoPendingQuestion = errors.New("no pending question to answer")

	// ErrNothingToUndo indicates the undo stack is empty.
	ErrNothingToUndo = errors.New("nothing to undo")

	// ErrChannelClosed indicates the actor's command channel was closed.
	ErrChannelClosed = errors.New("actor channel closed")

	// ErrActorBusy indicates the actor's command buffer is full.
	ErrActorBusy = errors.New("actor command buffer full")

	// ErrUnknownCommand indicates the command type is not recognized by the actor.
	ErrUnknownCommand = errors.New("unknown command type")
)

// CardNotFoundError indicates the referenced card doesn't exist.
type CardNotFoundError struct {
	CardID ulid.ULID
}

func (e *CardNotFoundError) Error() string {
	return fmt.Sprintf("card not found: %s", e.CardID)
}

// QuestionIDMismatchError indicates the answered question ID doesn't match the pending one.
type QuestionIDMismatchError struct {
	Expected ulid.ULID
	Got      ulid.ULID
}

func (e *QuestionIDMismatchError) Error() string {
	return fmt.Sprintf("question id mismatch: expected %s, got %s", e.Expected, e.Got)
}
