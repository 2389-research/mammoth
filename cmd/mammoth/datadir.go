// ABOUTME: XDG-based data directory resolution for persistent pipeline state.
// ABOUTME: Checks XDG_DATA_HOME, falls back to ~/.local/share/mammoth.
package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// defaultDataDir returns the default data directory for mammoth persistent state.
// It checks XDG_DATA_HOME first, then falls back to ~/.local/share/mammoth.
func defaultDataDir() (string, error) {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "mammoth"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}

	return filepath.Join(home, ".local", "share", "mammoth"), nil
}
