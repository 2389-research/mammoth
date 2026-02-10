// ABOUTME: Normalizes error messages into stable signatures for detecting repeat deterministic failures.
// ABOUTME: Replaces hex strings, UUIDs, numbers, timestamps, and file paths with placeholders.
package attractor

import (
	"regexp"
	"sync"
)

// Regex patterns for normalizing variable content in error messages.
// Order matters: more specific patterns (UUIDs, timestamps) must be applied
// before more general ones (hex strings, numbers) to avoid partial matches.
var (
	// UUIDs: 8-4-4-4-12 hex pattern (e.g., 550e8400-e29b-41d4-a716-446655440000)
	uuidPattern = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

	// ISO 8601 timestamps (e.g., 2024-01-15T12:00:00, 2024-01-15T12:00:00Z, 2024-01-15T12:00:00.123+05:00)
	timestampPattern = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})?`)

	// Quoted file paths containing "/" (separate patterns for double and single quotes
	// to avoid matching mismatched delimiters like "path/foo')
	doubleQuotedPathPattern = regexp.MustCompile(`"[^"]*\/[^"]*"`)
	singleQuotedPathPattern = regexp.MustCompile(`'[^']*\/[^']*'`)

	// Hex strings: 0x prefix followed by hex digits
	hexPrefixedPattern = regexp.MustCompile(`0x[0-9a-fA-F]+`)

	// Standalone hex strings: 8+ hex chars that contain at least one letter (to distinguish from pure numbers)
	hexStandalonePattern = regexp.MustCompile(`\b[0-9a-fA-F]{8,}\b`)

	// Standalone numbers: sequences of digits bounded by word boundaries
	numberPattern = regexp.MustCompile(`\b\d+\b`)
)

// NormalizeFailure takes an error message and replaces variable content
// (UUIDs, hex strings, timestamps, file paths, numbers) with stable placeholders.
// This allows structural comparison of errors that differ only in runtime-specific values.
func NormalizeFailure(msg string) string {
	if msg == "" {
		return ""
	}

	// Apply replacements in order from most specific to most general.
	// This prevents general patterns from consuming parts of specific ones.

	// 1. UUIDs first (most specific hex pattern)
	result := uuidPattern.ReplaceAllString(msg, "<UUID>")

	// 2. Timestamps (before numbers eat the digits)
	result = timestampPattern.ReplaceAllString(result, "<TIMESTAMP>")

	// 3. Quoted file paths (double-quoted, then single-quoted)
	result = doubleQuotedPathPattern.ReplaceAllString(result, "<PATH>")
	result = singleQuotedPathPattern.ReplaceAllString(result, "<PATH>")

	// 4. 0x-prefixed hex strings
	result = hexPrefixedPattern.ReplaceAllString(result, "<HEX>")

	// 5. Standalone long hex strings (8+ chars with at least one hex letter)
	result = hexStandalonePattern.ReplaceAllStringFunc(result, func(match string) string {
		// Only replace if it contains at least one hex letter (a-f/A-F)
		// to avoid replacing pure number sequences that will be caught by numberPattern
		for _, c := range match {
			if (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
				return "<HEX>"
			}
		}
		return match
	})

	// 6. Standalone numbers (most general)
	result = numberPattern.ReplaceAllString(result, "<N>")

	return result
}

// FailureSignature returns a deterministic signature for the given error message.
// Messages that differ only in variable content (UUIDs, timestamps, line numbers, etc.)
// will produce identical signatures.
func FailureSignature(msg string) string {
	return NormalizeFailure(msg)
}

// FailureTracker tracks failure signatures across retries to detect
// deterministic (repeating) failures. A failure is considered deterministic
// when the same normalized signature has been seen 2 or more times.
// FailureTracker is safe for concurrent use.
type FailureTracker struct {
	mu         sync.Mutex
	signatures map[string]int // signature -> count
}

// NewFailureTracker creates a FailureTracker ready to record errors.
func NewFailureTracker() *FailureTracker {
	return &FailureTracker{
		signatures: make(map[string]int),
	}
}

// Record normalizes the error message, increments the count for its signature,
// and returns the signature string.
func (t *FailureTracker) Record(err error) string {
	sig := FailureSignature(err.Error())
	t.mu.Lock()
	t.signatures[sig]++
	t.mu.Unlock()
	return sig
}

// Count returns how many times the given signature has been recorded.
func (t *FailureTracker) Count(signature string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.signatures[signature]
}

// IsDeterministic returns true if the given signature has been seen 2 or more times,
// indicating the failure is likely deterministic and not transient.
func (t *FailureTracker) IsDeterministic(signature string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.signatures[signature] >= 2
}
