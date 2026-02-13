// ABOUTME: Tests for the SourceHash utility that computes SHA-256 hashes of DOT source.
// ABOUTME: Covers determinism, empty input, whitespace sensitivity, and hex format.
package attractor

import (
	"strings"
	"testing"
)

func TestSourceHashDeterministic(t *testing.T) {
	source := `digraph test { start -> finish }`
	hash1 := SourceHash(source)
	hash2 := SourceHash(source)

	if hash1 != hash2 {
		t.Errorf("expected deterministic hash, got %q and %q", hash1, hash2)
	}
}

func TestSourceHashIsHex(t *testing.T) {
	source := `digraph test { start -> finish }`
	hash := SourceHash(source)

	// SHA-256 produces 32 bytes = 64 hex characters
	if len(hash) != 64 {
		t.Errorf("expected 64-character hex string, got %d characters: %q", len(hash), hash)
	}

	// Should only contain lowercase hex characters
	for _, c := range hash {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			t.Errorf("hash contains non-hex character %q: %q", string(c), hash)
			break
		}
	}
}

func TestSourceHashEmptyInput(t *testing.T) {
	hash := SourceHash("")

	// SHA-256 of empty string is a known value
	expected := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if hash != expected {
		t.Errorf("expected SHA-256 of empty string %q, got %q", expected, hash)
	}
}

func TestSourceHashDifferentInputs(t *testing.T) {
	hash1 := SourceHash(`digraph a { start -> finish }`)
	hash2 := SourceHash(`digraph b { start -> finish }`)

	if hash1 == hash2 {
		t.Error("expected different hashes for different inputs")
	}
}

func TestSourceHashWhitespaceSensitive(t *testing.T) {
	hash1 := SourceHash(`digraph test { start -> finish }`)
	hash2 := SourceHash(`digraph test {  start -> finish }`)

	if hash1 == hash2 {
		t.Error("expected different hashes for inputs differing by whitespace (no normalization)")
	}
}

func TestSourceHashLowercase(t *testing.T) {
	hash := SourceHash("anything")
	if hash != strings.ToLower(hash) {
		t.Errorf("expected lowercase hex, got %q", hash)
	}
}
