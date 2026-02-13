// ABOUTME: SpecCore holds the top-level metadata for a specification.
// ABOUTME: Three required fields (title, one_liner, goal) plus five optional detail fields.
package core

import (
	"time"

	"github.com/oklog/ulid/v2"
)

// SpecCore represents the core metadata of a specification.
// Mirrors barnstormer's SpecCore with identical JSON field names.
type SpecCore struct {
	SpecID          ulid.ULID `json:"spec_id"`
	Title           string    `json:"title"`
	OneLiner        string    `json:"one_liner"`
	Goal            string    `json:"goal"`
	Description     *string   `json:"description,omitempty"`
	Constraints     *string   `json:"constraints,omitempty"`
	SuccessCriteria *string   `json:"success_criteria,omitempty"`
	Risks           *string   `json:"risks,omitempty"`
	Notes           *string   `json:"notes,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// NewSpecCore creates a SpecCore with the three required fields and a fresh ULID.
func NewSpecCore(title, oneLiner, goal string) SpecCore {
	now := time.Now().UTC()
	return SpecCore{
		SpecID:    NewULID(),
		Title:     title,
		OneLiner:  oneLiner,
		Goal:      goal,
		CreatedAt: now,
		UpdatedAt: now,
	}
}
