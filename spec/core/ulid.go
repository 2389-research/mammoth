// ABOUTME: ULID generation helper using crypto/rand for monotonic IDs.
// ABOUTME: Centralizes ULID creation so all code uses the same entropy source.
package core

import (
	"crypto/rand"

	"github.com/oklog/ulid/v2"
)

// NewULID generates a new ULID using crypto/rand entropy.
func NewULID() ulid.ULID {
	return ulid.MustNew(ulid.Now(), rand.Reader)
}
