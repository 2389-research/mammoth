// ABOUTME: Tests for XDG-based data and config directory resolution used by the mammoth CLI.
// ABOUTME: Covers XDG_DATA_HOME/XDG_CONFIG_HOME overrides, default fallbacks, and error handling.
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

func TestDefaultConfigDirUsesXDGConfigHome(t *testing.T) {
	customDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", customDir)

	got, err := defaultConfigDir()
	if err != nil {
		t.Fatalf("defaultConfigDir failed: %v", err)
	}

	want := filepath.Join(customDir, "mammoth")
	if got != want {
		t.Errorf("defaultConfigDir() = %q, want %q", got, want)
	}
}

func TestDefaultConfigDirFallsBackToHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")

	got, err := defaultConfigDir()
	if err != nil {
		t.Fatalf("defaultConfigDir failed: %v", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir failed: %v", err)
	}

	want := filepath.Join(home, ".config", "mammoth")
	if got != want {
		t.Errorf("defaultConfigDir() = %q, want %q", got, want)
	}
}

func TestDefaultConfigDirReturnsAbsolutePath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")

	got, err := defaultConfigDir()
	if err != nil {
		t.Fatalf("defaultConfigDir failed: %v", err)
	}

	if !filepath.IsAbs(got) {
		t.Errorf("defaultConfigDir() returned relative path: %q", got)
	}
}
