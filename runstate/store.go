// ABOUTME: Defines RunState types and FSRunStateStore for persisting pipeline run state to the filesystem.
// ABOUTME: Standalone package with no attractor dependency; uses string-typed Context matching tracker's model.
package runstate

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// RunEvent is a generic event record stored in the JSONL event log.
type RunEvent struct {
	Type      string         `json:"type"`
	NodeID    string         `json:"node_id,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
}

// RunState represents the full state of a single pipeline run.
type RunState struct {
	ID             string            `json:"id"`
	PipelineFile   string            `json:"pipeline_file"`
	Status         string            `json:"status"` // "running", "completed", "failed", "cancelled"
	Source         string            `json:"source,omitempty"`
	SourceHash     string            `json:"source_hash,omitempty"`
	StartedAt      time.Time         `json:"started_at"`
	CompletedAt    *time.Time        `json:"completed_at,omitempty"`
	CurrentNode    string            `json:"current_node"`
	CompletedNodes []string          `json:"completed_nodes"`
	Context        map[string]string `json:"context"` // string values, matching tracker model
	Events         []RunEvent        `json:"events"`
	Error          string            `json:"error,omitempty"`
}

// RunStateStore is the interface for persisting and retrieving pipeline run state.
type RunStateStore interface {
	Create(state *RunState) error
	Get(id string) (*RunState, error)
	Update(state *RunState) error
	List() ([]*RunState, error)
	AddEvent(id string, event RunEvent) error
}

// GenerateRunID produces a random 16-character hex string (8 bytes of entropy).
func GenerateRunID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate run ID: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// SourceHash returns the lowercase hex-encoded SHA-256 hash of the raw source bytes.
// No normalization is applied: if the file changed at all, the hash changes.
func SourceHash(source string) string {
	sum := sha256.Sum256([]byte(source))
	return hex.EncodeToString(sum[:])
}

// timeFormat is the layout used for serializing timestamps to JSON strings.
const timeFormat = "2006-01-02T15:04:05.000Z07:00"

// runManifest is the on-disk representation of run metadata stored in manifest.json.
type runManifest struct {
	ID             string   `json:"id"`
	PipelineFile   string   `json:"pipeline_file"`
	Status         string   `json:"status"`
	SourceHash     string   `json:"source_hash,omitempty"`
	StartedAt      string   `json:"started_at"`
	CompletedAt    *string  `json:"completed_at,omitempty"`
	CurrentNode    string   `json:"current_node"`
	CompletedNodes []string `json:"completed_nodes"`
	Error          string   `json:"error,omitempty"`
}

// Compile-time check that FSRunStateStore implements RunStateStore.
var _ RunStateStore = (*FSRunStateStore)(nil)

// FSRunStateStore is a filesystem-backed RunStateStore.
// Each run is stored in a subdirectory of baseDir named by run ID.
type FSRunStateStore struct {
	baseDir string
	mu      sync.RWMutex
}

// NewFSRunStateStore creates a new filesystem-backed run state store rooted at baseDir.
// The base directory is created if it does not already exist.
func NewFSRunStateStore(baseDir string) (*FSRunStateStore, error) {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("create base dir: %w", err)
	}
	return &FSRunStateStore{baseDir: baseDir}, nil
}

// Create persists a new RunState to disk. Returns an error if a run with the same ID already exists.
func (s *FSRunStateStore) Create(state *RunState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	runDir := filepath.Join(s.baseDir, state.ID)

	// Check for duplicate
	if _, err := os.Stat(runDir); err == nil {
		return fmt.Errorf("run %q already exists", state.ID)
	}

	// Create directory structure
	if err := os.MkdirAll(filepath.Join(runDir, "nodes"), 0755); err != nil {
		return fmt.Errorf("create run directory: %w", err)
	}

	// Write manifest.json
	if err := s.writeManifest(runDir, state); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}

	// Write context.json
	if err := s.writeContext(runDir, state.Context); err != nil {
		return fmt.Errorf("write context: %w", err)
	}

	// Write source.dot if the pipeline source is available
	if state.Source != "" {
		sourcePath := filepath.Join(runDir, "source.dot")
		if err := os.WriteFile(sourcePath, []byte(state.Source), 0644); err != nil {
			return fmt.Errorf("write source.dot: %w", err)
		}
	}

	// Create empty events.jsonl
	eventsPath := filepath.Join(runDir, "events.jsonl")
	if err := os.WriteFile(eventsPath, []byte(""), 0644); err != nil {
		return fmt.Errorf("create events file: %w", err)
	}

	return nil
}

// Get loads a RunState from disk by its ID. Returns an error if the run does not exist
// or if any of the stored files are corrupt.
func (s *FSRunStateStore) Get(id string) (*RunState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.getUnlocked(id)
}

// getUnlocked performs the Get operation without acquiring locks. Callers must hold at least a read lock.
func (s *FSRunStateStore) getUnlocked(id string) (*RunState, error) {
	runDir := filepath.Join(s.baseDir, id)

	if _, err := os.Stat(runDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("run %q not found", id)
	}

	// Read manifest
	manifest, err := s.readManifest(runDir)
	if err != nil {
		return nil, fmt.Errorf("read manifest for %q: %w", id, err)
	}

	// Read context
	ctx, err := s.readContext(runDir)
	if err != nil {
		return nil, fmt.Errorf("read context for %q: %w", id, err)
	}

	// Read events
	events, err := s.readEvents(runDir)
	if err != nil {
		return nil, fmt.Errorf("read events for %q: %w", id, err)
	}

	// Read source.dot if it exists (optional file)
	var source string
	sourceData, sourceErr := os.ReadFile(filepath.Join(runDir, "source.dot"))
	if sourceErr == nil {
		source = string(sourceData)
	} else if !os.IsNotExist(sourceErr) {
		return nil, fmt.Errorf("read source.dot for %q: %w", id, sourceErr)
	}

	state := &RunState{
		ID:             manifest.ID,
		PipelineFile:   manifest.PipelineFile,
		Status:         manifest.Status,
		Source:         source,
		SourceHash:     manifest.SourceHash,
		CurrentNode:    manifest.CurrentNode,
		CompletedNodes: manifest.CompletedNodes,
		Context:        ctx,
		Events:         events,
		Error:          manifest.Error,
	}

	// Parse timestamps
	if manifest.StartedAt != "" {
		t, err := time.Parse(timeFormat, manifest.StartedAt)
		if err != nil {
			return nil, fmt.Errorf("parse started_at for %q: %w", id, err)
		}
		state.StartedAt = t
	}

	if manifest.CompletedAt != nil {
		t, err := time.Parse(timeFormat, *manifest.CompletedAt)
		if err != nil {
			return nil, fmt.Errorf("parse completed_at for %q: %w", id, err)
		}
		state.CompletedAt = &t
	}

	return state, nil
}

// Update overwrites the manifest and context for an existing run.
// Returns an error if the run does not exist.
func (s *FSRunStateStore) Update(state *RunState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	runDir := filepath.Join(s.baseDir, state.ID)

	if _, err := os.Stat(runDir); os.IsNotExist(err) {
		return fmt.Errorf("run %q not found", state.ID)
	}

	if err := s.writeManifest(runDir, state); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}

	if err := s.writeContext(runDir, state.Context); err != nil {
		return fmt.Errorf("write context: %w", err)
	}

	return nil
}

// List returns all RunStates stored on disk. Non-directory entries in the base
// directory are silently ignored.
func (s *FSRunStateStore) List() ([]*RunState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		return nil, fmt.Errorf("read base dir: %w", err)
	}

	var results []*RunState
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		state, err := s.getUnlocked(entry.Name())
		if err != nil {
			continue
		}
		results = append(results, state)
	}

	return results, nil
}

// FindResumable returns the most recent non-completed run whose SourceHash
// matches the given hash AND has a checkpoint.json file in its run directory.
// Returns nil if no matching run is found.
func (s *FSRunStateStore) FindResumable(sourceHash string) (*RunState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		return nil, fmt.Errorf("read base dir: %w", err)
	}

	type candidate struct {
		state     *RunState
		startedAt time.Time
	}
	var candidates []candidate

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		state, err := s.getUnlocked(entry.Name())
		if err != nil {
			continue
		}

		// Must match hash, be in a resumable status, and have a checkpoint.
		// A "running" run is only resumable if it appears stale (started > 5 min ago),
		// which indicates the process was killed rather than still active.
		if state.SourceHash != sourceHash {
			continue
		}
		if state.Status == "completed" {
			continue
		}
		if state.Status == "running" && time.Since(state.StartedAt) < 5*time.Minute {
			continue
		}

		cpPath := filepath.Join(s.baseDir, state.ID, "checkpoint.json")
		if _, err := os.Stat(cpPath); os.IsNotExist(err) {
			continue
		}

		candidates = append(candidates, candidate{state: state, startedAt: state.StartedAt})
	}

	if len(candidates) == 0 {
		return nil, nil
	}

	// Sort by StartedAt descending (most recent first)
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].startedAt.After(candidates[j].startedAt)
	})

	return candidates[0].state, nil
}

// CheckpointPath returns the path to the checkpoint.json file for a given run ID.
func (s *FSRunStateStore) CheckpointPath(runID string) string {
	return filepath.Join(s.baseDir, runID, "checkpoint.json")
}

// RunDir returns the base directory path for a given run ID.
func (s *FSRunStateStore) RunDir(runID string) string {
	return filepath.Join(s.baseDir, runID)
}

// AddEvent appends a RunEvent to the run's events.jsonl file.
// Returns an error if the run does not exist.
func (s *FSRunStateStore) AddEvent(id string, event RunEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	runDir := filepath.Join(s.baseDir, id)
	if _, err := os.Stat(runDir); os.IsNotExist(err) {
		return fmt.Errorf("run %q not found", id)
	}

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	eventsPath := filepath.Join(runDir, "events.jsonl")
	f, err := os.OpenFile(eventsPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return fmt.Errorf("open events file: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write event: %w", err)
	}

	return nil
}

// writeManifest serializes the run metadata to manifest.json using atomic write-via-temp-file.
func (s *FSRunStateStore) writeManifest(runDir string, state *RunState) error {
	m := runManifest{
		ID:             state.ID,
		PipelineFile:   state.PipelineFile,
		Status:         state.Status,
		SourceHash:     state.SourceHash,
		StartedAt:      state.StartedAt.Format(timeFormat),
		CurrentNode:    state.CurrentNode,
		CompletedNodes: state.CompletedNodes,
		Error:          state.Error,
	}

	if state.CompletedAt != nil {
		ct := state.CompletedAt.Format(timeFormat)
		m.CompletedAt = &ct
	}

	// Ensure CompletedNodes is never nil in JSON
	if m.CompletedNodes == nil {
		m.CompletedNodes = []string{}
	}

	return writeJSONAtomic(filepath.Join(runDir, "manifest.json"), m)
}

// readManifest loads and parses manifest.json from the given run directory.
func (s *FSRunStateStore) readManifest(runDir string) (*runManifest, error) {
	data, err := os.ReadFile(filepath.Join(runDir, "manifest.json"))
	if err != nil {
		return nil, err
	}
	var m runManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// writeContext serializes the pipeline context to context.json using atomic write.
func (s *FSRunStateStore) writeContext(runDir string, ctx map[string]string) error {
	if ctx == nil {
		ctx = map[string]string{}
	}
	return writeJSONAtomic(filepath.Join(runDir, "context.json"), ctx)
}

// readContext loads and parses context.json from the given run directory.
func (s *FSRunStateStore) readContext(runDir string) (map[string]string, error) {
	data, err := os.ReadFile(filepath.Join(runDir, "context.json"))
	if err != nil {
		return nil, err
	}
	var ctx map[string]string
	if err := json.Unmarshal(data, &ctx); err != nil {
		return nil, err
	}
	return ctx, nil
}

// readEvents parses the events.jsonl file, returning one RunEvent per line.
func (s *FSRunStateStore) readEvents(runDir string) ([]RunEvent, error) {
	data, err := os.ReadFile(filepath.Join(runDir, "events.jsonl"))
	if err != nil {
		return nil, err
	}

	content := strings.TrimSpace(string(data))
	if content == "" {
		return []RunEvent{}, nil
	}

	lines := strings.Split(content, "\n")
	events := make([]RunEvent, 0, len(lines))
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var evt RunEvent
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			return nil, fmt.Errorf("parse event line %d: %w", i, err)
		}
		events = append(events, evt)
	}

	return events, nil
}

// writeJSONAtomic writes a JSON-encoded value to a file using a temp file + rename for atomicity.
func writeJSONAtomic(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename temp file: %w", err)
	}

	return nil
}
