// ABOUTME: Tests for failure signature normalization and tracking.
// ABOUTME: Verifies UUID, hex, number, path, and timestamp replacement plus deterministic failure detection.
package attractor

import (
	"errors"
	"testing"
)

func TestNormalizeFailureUUIDs(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "lowercase UUID",
			input: "request 550e8400-e29b-41d4-a716-446655440000 failed",
			want:  "request <UUID> failed",
		},
		{
			name:  "uppercase UUID",
			input: "id=550E8400-E29B-41D4-A716-446655440000 not found",
			want:  "id=<UUID> not found",
		},
		{
			name:  "multiple UUIDs",
			input: "copy 550e8400-e29b-41d4-a716-446655440000 to 6ba7b810-9dad-11d1-80b4-00c04fd430c8",
			want:  "copy <UUID> to <UUID>",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeFailure(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeFailure(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeFailureHexStrings(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "0x prefixed hex",
			input: "OOM at address 0x7fff5fbff8c0",
			want:  "OOM at address <HEX>",
		},
		{
			name:  "0x short hex",
			input: "pointer 0xDEAD",
			want:  "pointer <HEX>",
		},
		{
			name:  "standalone long hex without prefix",
			input: "commit abcdef1234567890 failed",
			want:  "commit <HEX> failed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeFailure(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeFailure(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeFailureNumbers(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "standalone number",
			input: "failed with code 500",
			want:  "failed with code <N>",
		},
		{
			name:  "multiple numbers",
			input: "line 42 column 7",
			want:  "line <N> column <N>",
		},
		{
			name:  "number at start",
			input: "3 errors found",
			want:  "<N> errors found",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeFailure(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeFailure(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeFailurePaths(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "quoted file path",
			input: `error in "/tmp/abc123/main.go" at line`,
			want:  `error in <PATH> at line`,
		},
		{
			name:  "single quoted path",
			input: `cannot open '/var/log/app.log' for reading`,
			want:  `cannot open <PATH> for reading`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeFailure(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeFailure(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeFailureTimestamps(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "ISO 8601 with time",
			input: "failed at 2024-01-15T12:00:00 during sync",
			want:  "failed at <TIMESTAMP> during sync",
		},
		{
			name:  "ISO 8601 with timezone",
			input: "timeout at 2024-01-15T12:00:00Z retrying",
			want:  "timeout at <TIMESTAMP> retrying",
		},
		{
			name:  "ISO 8601 with offset",
			input: "error at 2024-01-15T12:00:00+05:00 in handler",
			want:  "error at <TIMESTAMP> in handler",
		},
		{
			name:  "ISO 8601 with milliseconds",
			input: "crash at 2024-01-15T12:00:00.123Z logged",
			want:  "crash at <TIMESTAMP> logged",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeFailure(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeFailure(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeFailureCombined(t *testing.T) {
	input := `error at "/tmp/abc123/main.go":42: request 550e8400-e29b-41d4-a716-446655440000 failed with code 500 at 2024-01-15T12:00:00Z`
	got := NormalizeFailure(input)

	// Should contain all placeholder types
	for _, placeholder := range []string{"<PATH>", "<UUID>", "<N>", "<TIMESTAMP>"} {
		if !containsSubstring(got, placeholder) {
			t.Errorf("NormalizeFailure combined: expected %s in result %q", placeholder, got)
		}
	}
	// Should NOT contain the original variable content
	for _, variable := range []string{"abc123", "550e8400", "500", "2024-01-15"} {
		if containsSubstring(got, variable) {
			t.Errorf("NormalizeFailure combined: should not contain %q in result %q", variable, got)
		}
	}
}

func TestNormalizeFailurePreservesStructure(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain text unchanged",
			input: "connection refused",
			want:  "connection refused",
		},
		{
			name:  "preserves colons and punctuation",
			input: "error: connection refused: no route to host",
			want:  "error: connection refused: no route to host",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeFailure(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeFailure(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFailureSignatureDeterministic(t *testing.T) {
	// Two messages that differ only in variable content should produce the same signature.
	msg1 := "request 550e8400-e29b-41d4-a716-446655440000 failed with code 500"
	msg2 := "request 6ba7b810-9dad-11d1-80b4-00c04fd430c8 failed with code 503"

	sig1 := FailureSignature(msg1)
	sig2 := FailureSignature(msg2)

	if sig1 != sig2 {
		t.Errorf("FailureSignature should be deterministic for same-structure errors:\n  sig1=%q\n  sig2=%q", sig1, sig2)
	}
}

func TestFailureSignatureDifferent(t *testing.T) {
	msg1 := "connection refused"
	msg2 := "permission denied"

	sig1 := FailureSignature(msg1)
	sig2 := FailureSignature(msg2)

	if sig1 == sig2 {
		t.Errorf("FailureSignature should differ for different errors: both got %q", sig1)
	}
}

func TestFailureTrackerRecord(t *testing.T) {
	tracker := NewFailureTracker()

	sig1 := tracker.Record(errors.New("request 550e8400-e29b-41d4-a716-446655440000 failed with code 500"))
	sig2 := tracker.Record(errors.New("request 6ba7b810-9dad-11d1-80b4-00c04fd430c8 failed with code 503"))

	// Both should produce the same signature
	if sig1 != sig2 {
		t.Errorf("Record should normalize: sig1=%q, sig2=%q", sig1, sig2)
	}

	// Count should be 2
	if count := tracker.Count(sig1); count != 2 {
		t.Errorf("Count(%q) = %d, want 2", sig1, count)
	}
}

func TestFailureTrackerIsDeterministic(t *testing.T) {
	tracker := NewFailureTracker()

	err := errors.New("timeout after 30 seconds")

	// After first occurrence, not deterministic yet
	sig := tracker.Record(err)
	if tracker.IsDeterministic(sig) {
		t.Error("IsDeterministic should be false after 1 occurrence")
	}

	// After second occurrence, it's deterministic
	tracker.Record(err)
	if !tracker.IsDeterministic(sig) {
		t.Error("IsDeterministic should be true after 2 occurrences")
	}
}

func TestFailureTrackerDifferentErrors(t *testing.T) {
	tracker := NewFailureTracker()

	sig1 := tracker.Record(errors.New("connection refused"))
	sig2 := tracker.Record(errors.New("permission denied"))

	if sig1 == sig2 {
		t.Errorf("different errors should have different signatures")
	}

	if tracker.Count(sig1) != 1 {
		t.Errorf("Count(sig1) = %d, want 1", tracker.Count(sig1))
	}
	if tracker.Count(sig2) != 1 {
		t.Errorf("Count(sig2) = %d, want 1", tracker.Count(sig2))
	}

	if tracker.IsDeterministic(sig1) {
		t.Error("sig1 should not be deterministic with count 1")
	}
	if tracker.IsDeterministic(sig2) {
		t.Error("sig2 should not be deterministic with count 1")
	}
}

// containsSubstring is a simple helper to check substring presence.
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
