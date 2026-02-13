// ABOUTME: Card represents a kanban card with type, lane, ordering, and authorship.
// ABOUTME: Cards are the primary content units within a specification's board.
package core

import (
	"time"

	"github.com/oklog/ulid/v2"
)

// Card represents a kanban-style card within a spec.
// Mirrors barnstormer's Card struct with identical JSON field names.
type Card struct {
	CardID    ulid.ULID `json:"card_id"`
	CardType  string    `json:"card_type"`
	Title     string    `json:"title"`
	Body      *string   `json:"body,omitempty"`
	Lane      string    `json:"lane"`
	Order     float64   `json:"order"`
	Refs      []string  `json:"refs"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	CreatedBy string    `json:"created_by"`
	UpdatedBy string    `json:"updated_by"`
}

// NewCard creates a Card with the given type, title, and creator.
// Defaults: lane="Ideas", order=0.0, empty refs.
func NewCard(cardType, title, createdBy string) Card {
	now := time.Now().UTC()
	return Card{
		CardID:    NewULID(),
		CardType:  cardType,
		Title:     title,
		Lane:      "Ideas",
		Order:     0.0,
		Refs:      []string{},
		CreatedAt: now,
		UpdatedAt: now,
		CreatedBy: createdBy,
		UpdatedBy: createdBy,
	}
}
