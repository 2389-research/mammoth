// ABOUTME: Named, typed artifact storage for large pipeline outputs.
// ABOUTME: Transparently file-backs artifacts exceeding a size threshold (default 100KB).
package attractor

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ArtifactInfo describes a stored artifact.
type ArtifactInfo struct {
	ID           string
	Name         string
	SizeBytes    int
	StoredAt     time.Time
	IsFileBacked bool
}

// artifactEntry holds the artifact metadata and data (in-memory or file path).
type artifactEntry struct {
	info     ArtifactInfo
	data     []byte // non-nil for in-memory artifacts
	filePath string // non-empty for file-backed artifacts
}

// ArtifactStore provides named, typed storage for large outputs.
type ArtifactStore struct {
	artifacts map[string]artifactEntry
	mu        sync.RWMutex
	baseDir   string
	threshold int // file-backing threshold in bytes (default 100KB)
}

const defaultFileBackingThreshold = 100 * 1024 // 100KB

// NewArtifactStore creates a new artifact store rooted at the given directory.
func NewArtifactStore(baseDir string) *ArtifactStore {
	return &ArtifactStore{
		artifacts: make(map[string]artifactEntry),
		baseDir:   baseDir,
		threshold: defaultFileBackingThreshold,
	}
}

// Store saves an artifact. Large artifacts (exceeding the threshold) are written to disk.
func (s *ArtifactStore) Store(id, name string, data []byte) (*ArtifactInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	info := ArtifactInfo{
		ID:        id,
		Name:      name,
		SizeBytes: len(data),
		StoredAt:  time.Now(),
	}

	entry := artifactEntry{info: info}

	if len(data) >= s.threshold {
		// File-back the artifact
		filePath := filepath.Join(s.baseDir, id)
		if err := os.WriteFile(filePath, data, 0644); err != nil {
			return nil, fmt.Errorf("writing artifact %q to disk: %w", id, err)
		}
		entry.filePath = filePath
		entry.info.IsFileBacked = true
	} else {
		// Store in memory
		stored := make([]byte, len(data))
		copy(stored, data)
		entry.data = stored
	}

	s.artifacts[id] = entry
	return &entry.info, nil
}

// Retrieve returns the data for the given artifact ID.
func (s *ArtifactStore) Retrieve(id string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.artifacts[id]
	if !ok {
		return nil, fmt.Errorf("artifact %q not found", id)
	}

	if entry.info.IsFileBacked {
		data, err := os.ReadFile(entry.filePath)
		if err != nil {
			return nil, fmt.Errorf("reading artifact %q from disk: %w", id, err)
		}
		return data, nil
	}

	result := make([]byte, len(entry.data))
	copy(result, entry.data)
	return result, nil
}

// Has checks whether an artifact with the given ID exists.
func (s *ArtifactStore) Has(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.artifacts[id]
	return ok
}

// List returns metadata for all stored artifacts.
func (s *ArtifactStore) List() []ArtifactInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]ArtifactInfo, 0, len(s.artifacts))
	for _, entry := range s.artifacts {
		result = append(result, entry.info)
	}
	return result
}

// Remove deletes an artifact by ID. File-backed artifacts have their disk file removed.
func (s *ArtifactStore) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.artifacts[id]
	if !ok {
		return fmt.Errorf("artifact %q not found", id)
	}

	if entry.info.IsFileBacked && entry.filePath != "" {
		if err := os.Remove(entry.filePath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing artifact file %q: %w", entry.filePath, err)
		}
	}

	delete(s.artifacts, id)
	return nil
}

// Clear removes all artifacts, including any file-backed data on disk.
func (s *ArtifactStore) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, entry := range s.artifacts {
		if entry.info.IsFileBacked && entry.filePath != "" {
			if err := os.Remove(entry.filePath); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("clearing artifact file %q: %w", entry.filePath, err)
			}
		}
		delete(s.artifacts, id)
	}

	return nil
}
