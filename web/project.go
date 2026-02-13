// ABOUTME: Project data model representing a mammoth project through its lifecycle.
// ABOUTME: Includes ProjectStore for in-memory and filesystem-based persistence.
package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ProjectPhase represents the current stage of a project in the wizard flow.
type ProjectPhase string

const (
	PhaseSpec  ProjectPhase = "spec"
	PhaseEdit  ProjectPhase = "edit"
	PhaseBuild ProjectPhase = "build"
	PhaseDone  ProjectPhase = "done"
)

// Project represents a mammoth project spanning the full wizard flow (Spec -> Edit -> Build).
type Project struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	CreatedAt   time.Time    `json:"created_at"`
	Phase       ProjectPhase `json:"phase"`
	SpecID      string       `json:"spec_id,omitempty"`
	DOT         string       `json:"dot,omitempty"`
	Diagnostics []string     `json:"diagnostics,omitempty"`
	RunID       string       `json:"run_id,omitempty"`
	DataDir     string       `json:"-"`
}

// ProjectStore provides in-memory storage with filesystem persistence for projects.
type ProjectStore struct {
	mu       sync.RWMutex
	projects map[string]*Project
	baseDir  string
}

// NewProjectStore creates a new ProjectStore rooted at the given base directory.
func NewProjectStore(baseDir string) *ProjectStore {
	return &ProjectStore{
		projects: make(map[string]*Project),
		baseDir:  baseDir,
	}
}

// Create makes a new project with the given name. It starts in the spec phase.
func (s *ProjectStore) Create(name string) (*Project, error) {
	if name == "" {
		return nil, errors.New("project name must not be empty")
	}

	id := uuid.New().String()
	dataDir := filepath.Join(s.baseDir, id)

	p := &Project{
		ID:        id,
		Name:      name,
		CreatedAt: time.Now(),
		Phase:     PhaseSpec,
		DataDir:   dataDir,
	}

	s.mu.Lock()
	s.projects[id] = p
	s.mu.Unlock()

	return p, nil
}

// Get retrieves a project by ID. Returns the project and true if found,
// or nil and false if not found.
func (s *ProjectStore) Get(id string) (*Project, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	p, ok := s.projects[id]
	return p, ok
}

// List returns all projects sorted by creation time, newest first.
func (s *ProjectStore) List() []*Project {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*Project, 0, len(s.projects))
	for _, p := range s.projects {
		result = append(result, p)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})

	return result
}

// Update replaces the stored project with the provided one. The project must
// already exist in the store (matched by ID).
func (s *ProjectStore) Update(p *Project) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.projects[p.ID]; !ok {
		return fmt.Errorf("project %q not found", p.ID)
	}

	s.projects[p.ID] = p
	return nil
}

// Save persists a project to disk as JSON in its data directory.
func (s *ProjectStore) Save(p *Project) error {
	dir := filepath.Join(s.baseDir, p.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating project directory: %w", err)
	}

	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling project: %w", err)
	}

	path := filepath.Join(dir, "project.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing project.json: %w", err)
	}

	return nil
}

// LoadAll reads all project.json files from subdirectories of baseDir and
// populates the in-memory store.
func (s *ProjectStore) LoadAll() error {
	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		return fmt.Errorf("reading base directory: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		path := filepath.Join(s.baseDir, entry.Name(), "project.json")
		data, err := os.ReadFile(path)
		if err != nil {
			// Skip directories without project.json.
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("reading %s: %w", path, err)
		}

		var p Project
		if err := json.Unmarshal(data, &p); err != nil {
			return fmt.Errorf("parsing %s: %w", path, err)
		}

		// Restore the DataDir field which is not serialized.
		p.DataDir = filepath.Join(s.baseDir, entry.Name())
		s.projects[p.ID] = &p
	}

	return nil
}
