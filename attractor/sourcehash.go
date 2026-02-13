// ABOUTME: Computes a content-addressable hash of pipeline DOT source for matching runs.
// ABOUTME: Uses SHA-256 with no normalization — any byte change produces a different hash.
package attractor

import (
	"crypto/sha256"
	"encoding/hex"
)

// SourceHash returns the lowercase hex-encoded SHA-256 hash of the raw source bytes.
// No normalization is applied: if the file changed at all, the hash changes.
func SourceHash(source string) string {
	sum := sha256.Sum256([]byte(source))
	return hex.EncodeToString(sum[:])
}
