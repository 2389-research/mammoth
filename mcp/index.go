// ABOUTME: RunIndex provides disk-backed persistence for pipeline run metadata.
// ABOUTME: Stores DOT source, config, and status per run to enable resume across server restarts.
package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// IndexEntry stores metadata for a single run on disk.
type IndexEntry struct {
	RunID         string    `json:"run_id"`
	Source        string    `json:"-"` // stored separately as source.dot
	Config        RunConfig `json:"config"`
	Status        string    `json:"status"`
	CheckpointDir string    `json:"checkpoint_dir,omitempty"`
	ArtifactDir   string    `json:"artifact_dir,omitempty"`
}

// RunIndex manages disk-backed run metadata.
type RunIndex struct {
	dir string
	mu  sync.RWMutex
}

// NewRunIndex creates a new index rooted at the given directory.
func NewRunIndex(dir string) *RunIndex {
	return &RunIndex{dir: dir}
}

// Save persists an index entry to disk.
func (idx *RunIndex) Save(entry *IndexEntry) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	runDir := filepath.Join(idx.dir, entry.RunID)
	if err := os.MkdirAll(runDir, 0755); err != nil {
		return fmt.Errorf("create run dir: %w", err)
	}
	if entry.Source != "" {
		sourcePath := filepath.Join(runDir, "source.dot")
		if err := os.WriteFile(sourcePath, []byte(entry.Source), 0644); err != nil {
			return fmt.Errorf("write source.dot: %w", err)
		}
	}
	metaPath := filepath.Join(runDir, "meta.json")
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal meta: %w", err)
	}
	if err := os.WriteFile(metaPath, data, 0644); err != nil {
		return fmt.Errorf("write meta.json: %w", err)
	}
	return nil
}

// Load reads an index entry from disk.
func (idx *RunIndex) Load(runID string) (*IndexEntry, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	runDir := filepath.Join(idx.dir, runID)
	metaPath := filepath.Join(runDir, "meta.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, fmt.Errorf("read meta.json for run %s: %w", runID, err)
	}
	var entry IndexEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, fmt.Errorf("unmarshal meta.json: %w", err)
	}
	entry.RunID = runID
	sourcePath := filepath.Join(runDir, "source.dot")
	sourceData, err := os.ReadFile(sourcePath)
	if err == nil {
		entry.Source = string(sourceData)
	}
	return &entry, nil
}

// List returns all index entries.
func (idx *RunIndex) List() ([]*IndexEntry, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	entries, err := os.ReadDir(idx.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read index dir: %w", err)
	}
	var result []*IndexEntry
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		metaPath := filepath.Join(idx.dir, e.Name(), "meta.json")
		data, err := os.ReadFile(metaPath)
		if err != nil {
			continue
		}
		var entry IndexEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			continue
		}
		entry.RunID = e.Name()
		result = append(result, &entry)
	}
	return result, nil
}

// RunDir returns the directory path for a given run ID.
func (idx *RunIndex) RunDir(runID string) string {
	return filepath.Join(idx.dir, runID)
}
