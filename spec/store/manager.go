// ABOUTME: High-level storage manager for the mammoth-specd daemon's filesystem layout.
// ABOUTME: Handles directory creation, spec discovery, recovery orchestration, and export writing.
package store

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/2389-research/mammoth/spec/core"
	"github.com/oklog/ulid/v2"
)

// StorageManager manages the mammoth-specd home directory layout and provides
// high-level operations for spec storage, recovery, and export generation.
//
// Dir layout:
//
//	home/specs/{ulid}/events.jsonl
//	home/specs/{ulid}/index.db
//	home/specs/{ulid}/snapshots/
//	home/specs/{ulid}/exports/
type StorageManager struct {
	home string
}

// NewStorageManager creates a new StorageManager rooted at the given home directory.
// Creates the home and specs subdirectories if they do not exist.
func NewStorageManager(home string) (*StorageManager, error) {
	specsDir := filepath.Join(home, "specs")
	if err := os.MkdirAll(specsDir, 0o755); err != nil {
		return nil, fmt.Errorf("create specs dir: %w", err)
	}
	return &StorageManager{home: home}, nil
}

// Home returns the home directory path.
func (m *StorageManager) Home() string {
	return m.home
}

// ListSpecDirs scans the specs directory and returns all spec directories
// with their ULIDs.
func (m *StorageManager) ListSpecDirs() ([]SpecDir, error) {
	specsDir := filepath.Join(m.home, "specs")
	info, err := os.Stat(specsDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("stat specs dir: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("specs path is not a directory: %s", specsDir)
	}

	entries, err := os.ReadDir(specsDir)
	if err != nil {
		return nil, fmt.Errorf("read specs dir: %w", err)
	}

	var results []SpecDir
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		id, err := ulid.Parse(name)
		if err != nil {
			log.Printf("component=spec.store action=list_spec_dirs_skip_non_ulid dir=%s", name)
			continue
		}
		results = append(results, SpecDir{
			SpecID: id,
			Path:   filepath.Join(specsDir, name),
		})
	}

	return results, nil
}

// SpecDir pairs a spec's ULID with its filesystem path.
type SpecDir struct {
	SpecID ulid.ULID
	Path   string
}

// CreateSpecDir creates a spec directory with the required subdirectories.
func (m *StorageManager) CreateSpecDir(specID ulid.ULID) (string, error) {
	specDir := filepath.Join(m.home, "specs", specID.String())
	if err := os.MkdirAll(filepath.Join(specDir, "snapshots"), 0o755); err != nil {
		return "", fmt.Errorf("create snapshots dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(specDir, "exports"), 0o755); err != nil {
		return "", fmt.Errorf("create exports dir: %w", err)
	}
	return specDir, nil
}

// GetSpecDir returns the path to a spec's directory (does not create it).
func (m *StorageManager) GetSpecDir(specID ulid.ULID) string {
	return filepath.Join(m.home, "specs", specID.String())
}

// RecoverAllSpecs recovers all specs from their storage directories.
// Returns a list of (spec_id, recovered_state) pairs.
// Logs and skips specs that fail to recover.
func (m *StorageManager) RecoverAllSpecs() ([]RecoveredSpec, error) {
	specDirs, err := m.ListSpecDirs()
	if err != nil {
		return nil, err
	}

	var recovered []RecoveredSpec
	for _, sd := range specDirs {
		state, lastEventID, err := RecoverSpec(sd.Path)
		if err != nil {
			log.Printf("component=spec.store action=recover_spec_failed spec_id=%s err=%v", sd.SpecID, err)
			continue
		}
		log.Printf("component=spec.store action=recovered_spec spec_id=%s last_event_id=%d", sd.SpecID, lastEventID)
		recovered = append(recovered, RecoveredSpec{
			SpecID: sd.SpecID,
			State:  state,
		})
	}

	return recovered, nil
}

// RecoveredSpec pairs a recovered spec state with its ULID.
type RecoveredSpec struct {
	SpecID ulid.ULID
	State  *core.SpecState
}

// WriteExports writes export files to the exports/ subdirectory.
// This is a placeholder that creates the exports directory and writes
// a basic markdown export. Full export support (YAML, DOT) will be
// added when the export module is ported.
func WriteExports(specDir string, state *core.SpecState) error {
	exportsDir := filepath.Join(specDir, "exports")
	if err := os.MkdirAll(exportsDir, 0o755); err != nil {
		return fmt.Errorf("create exports dir: %w", err)
	}

	// Write a basic markdown export
	md := generateMarkdown(state)
	if err := os.WriteFile(filepath.Join(exportsDir, "spec.md"), []byte(md), 0o644); err != nil {
		return fmt.Errorf("write markdown export: %w", err)
	}

	return nil
}

// generateMarkdown produces a simple markdown representation of the spec state.
func generateMarkdown(state *core.SpecState) string {
	var out string

	if state.Core != nil {
		out += "# " + state.Core.Title + "\n\n"
		out += state.Core.OneLiner + "\n\n"
		out += "## Goal\n\n" + state.Core.Goal + "\n\n"

		if state.Core.Description != nil {
			out += "## Description\n\n" + *state.Core.Description + "\n\n"
		}
	} else {
		out += "# (untitled spec)\n\n"
	}

	if state.Cards.Len() > 0 {
		out += "## Cards\n\n"
		state.Cards.Range(func(id ulid.ULID, card core.Card) bool {
			out += fmt.Sprintf("- [%s] **%s** (%s)\n", card.Lane, card.Title, card.CardType)
			return true
		})
		out += "\n"
	}

	return out
}
