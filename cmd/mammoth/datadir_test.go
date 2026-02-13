// ABOUTME: Tests for XDG-based data directory resolution used by the mammoth CLI.
// ABOUTME: Covers XDG_DATA_HOME override, default fallback to ~/.local/share/mammoth, and error handling.
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultDataDirUsesXDGDataHome(t *testing.T) {
	customDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", customDir)

	got, err := defaultDataDir()
	if err != nil {
		t.Fatalf("defaultDataDir failed: %v", err)
	}

	want := filepath.Join(customDir, "mammoth")
	if got != want {
		t.Errorf("defaultDataDir() = %q, want %q", got, want)
	}
}

func TestDefaultDataDirFallsBackToHome(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")

	got, err := defaultDataDir()
	if err != nil {
		t.Fatalf("defaultDataDir failed: %v", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir failed: %v", err)
	}

	want := filepath.Join(home, ".local", "share", "mammoth")
	if got != want {
		t.Errorf("defaultDataDir() = %q, want %q", got, want)
	}
}

func TestDefaultDataDirReturnsAbsolutePath(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")

	got, err := defaultDataDir()
	if err != nil {
		t.Fatalf("defaultDataDir failed: %v", err)
	}

	if !filepath.IsAbs(got) {
		t.Errorf("defaultDataDir() returned relative path: %q", got)
	}
}
