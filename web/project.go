// ABOUTME: Project data model representing a mammoth project through its lifecycle.
// ABOUTME: Includes ProjectStore for in-memory and filesystem-based persistence.
package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
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

	if err := s.Save(p); err != nil {
		s.mu.Lock()
		delete(s.projects, id)
		s.mu.Unlock()
		return nil, err
	}

	return p, nil
}

// Get retrieves a project by ID. Returns a copy of the project and true if
// found, or nil and false if not found. The copy prevents data races when
// callers modify the returned project concurrently.
func (s *ProjectStore) Get(id string) (*Project, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	p, ok := s.projects[id]
	if !ok {
		return nil, false
	}
	cp := *p
	cp.Diagnostics = copyStringSlice(p.Diagnostics)
	return &cp, true
}

// List returns all projects sorted by creation time, newest first. Each
// returned project is a copy to prevent data races from concurrent mutation.
func (s *ProjectStore) List() []*Project {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*Project, 0, len(s.projects))
	for _, p := range s.projects {
		cp := *p
		cp.Diagnostics = copyStringSlice(p.Diagnostics)
		result = append(result, &cp)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})

	return result
}

// copyStringSlice returns a shallow copy of a string slice, preserving nil
// vs empty semantics.
func copyStringSlice(s []string) []string {
	if s == nil {
		return nil
	}
	cp := make([]string, len(s))
	copy(cp, s)
	return cp
}

// Update replaces the stored project with the provided one. The project must
// already exist in the store (matched by ID).
func (s *ProjectStore) Update(p *Project) error {
	s.mu.Lock()
	if _, ok := s.projects[p.ID]; !ok {
		s.mu.Unlock()
		return fmt.Errorf("project %q not found", p.ID)
	}

	p.DataDir = filepath.Join(s.baseDir, p.ID)
	s.projects[p.ID] = p
	s.mu.Unlock()

	return s.Save(p)
}

// validateProjectID rejects IDs that could cause path traversal or filesystem issues.
func validateProjectID(id string) error {
	if id == "" {
		return errors.New("project ID must not be empty")
	}
	if strings.Contains(id, "..") {
		return errors.New("project ID must not contain '..'")
	}
	if strings.ContainsAny(id, "/\\") {
		return errors.New("project ID must not contain path separators")
	}
	return nil
}

// Save persists a project to disk as JSON in its data directory. It validates
// the project ID to prevent path traversal attacks before writing.
func (s *ProjectStore) Save(p *Project) error {
	if err := validateProjectID(p.ID); err != nil {
		return fmt.Errorf("invalid project ID: %w", err)
	}

	dir := filepath.Join(s.baseDir, p.ID)

	// Verify the resolved directory stays under baseDir.
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolving project directory: %w", err)
	}
	absBase, err := filepath.Abs(s.baseDir)
	if err != nil {
		return fmt.Errorf("resolving base directory: %w", err)
	}
	if !strings.HasPrefix(absDir, absBase+string(filepath.Separator)) {
		return fmt.Errorf("project directory escapes base directory")
	}

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
			log.Printf("project load: skipping %s: %v", path, err)
			continue
		}

		var p Project
		if err := json.Unmarshal(data, &p); err != nil {
			log.Printf("project load: skipping %s: %v", path, err)
			continue
		}

		// Restore the DataDir field which is not serialized.
		p.DataDir = filepath.Join(s.baseDir, entry.Name())
		s.projects[p.ID] = &p
	}

	return nil
}
