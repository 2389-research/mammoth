// ABOUTME: RunDirectory manages the per-run directory layout for pipeline executions.
// ABOUTME: Provides structured storage for node artifacts, prompts, responses, and checkpoints.
package attractor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// RunDirectory represents the directory structure for a single pipeline run.
type RunDirectory struct {
	BaseDir string
	RunID   string
}

// NewRunDirectory creates a new run directory structure at baseDir/runID.
func NewRunDirectory(baseDir, runID string) (*RunDirectory, error) {
	if baseDir == "" {
		return nil, fmt.Errorf("baseDir must not be empty")
	}
	if runID == "" {
		return nil, fmt.Errorf("runID must not be empty")
	}

	rd := &RunDirectory{
		BaseDir: filepath.Join(baseDir, runID),
		RunID:   runID,
	}

	// Create run directory and nodes subdirectory
	nodesDir := filepath.Join(rd.BaseDir, "nodes")
	if err := os.MkdirAll(nodesDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating run directory structure: %w", err)
	}

	return rd, nil
}

// NodeDir returns the path for a node's artifact directory.
func (rd *RunDirectory) NodeDir(nodeID string) string {
	return filepath.Join(rd.BaseDir, "nodes", nodeID)
}

// EnsureNodeDir creates the directory for a node if it doesn't exist.
func (rd *RunDirectory) EnsureNodeDir(nodeID string) error {
	if nodeID == "" {
		return fmt.Errorf("nodeID must not be empty")
	}
	return os.MkdirAll(rd.NodeDir(nodeID), 0o755)
}

// WriteNodeArtifact writes data to a file within a node's directory.
func (rd *RunDirectory) WriteNodeArtifact(nodeID, filename string, data []byte) error {
	if nodeID == "" {
		return fmt.Errorf("nodeID must not be empty")
	}
	if filename == "" {
		return fmt.Errorf("filename must not be empty")
	}
	if err := rd.EnsureNodeDir(nodeID); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(rd.NodeDir(nodeID), filename), data, 0o644)
}

// ReadNodeArtifact reads data from a file within a node's directory.
func (rd *RunDirectory) ReadNodeArtifact(nodeID, filename string) ([]byte, error) {
	return os.ReadFile(filepath.Join(rd.NodeDir(nodeID), filename))
}

// ListNodeArtifacts returns the filenames of all artifacts for a node.
func (rd *RunDirectory) ListNodeArtifacts(nodeID string) ([]string, error) {
	dir := rd.NodeDir(nodeID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

// SaveCheckpoint saves a checkpoint to checkpoint.json in the run directory.
func (rd *RunDirectory) SaveCheckpoint(cp *Checkpoint) error {
	data, err := json.Marshal(cp)
	if err != nil {
		return fmt.Errorf("marshaling checkpoint: %w", err)
	}
	cpPath := filepath.Join(rd.BaseDir, "checkpoint.json")
	return os.WriteFile(cpPath, data, 0o644)
}

// LoadCheckpoint loads a checkpoint from checkpoint.json in the run directory.
func (rd *RunDirectory) LoadCheckpoint() (*Checkpoint, error) {
	cpPath := filepath.Join(rd.BaseDir, "checkpoint.json")
	data, err := os.ReadFile(cpPath)
	if err != nil {
		return nil, fmt.Errorf("reading checkpoint: %w", err)
	}
	var cp Checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return nil, fmt.Errorf("unmarshaling checkpoint: %w", err)
	}
	return &cp, nil
}

// WritePrompt writes a prompt to prompt.md in a node's directory.
func (rd *RunDirectory) WritePrompt(nodeID, prompt string) error {
	if nodeID == "" {
		return fmt.Errorf("nodeID must not be empty")
	}
	return rd.WriteNodeArtifact(nodeID, "prompt.md", []byte(prompt))
}

// WriteResponse writes a response to response.md in a node's directory.
func (rd *RunDirectory) WriteResponse(nodeID, response string) error {
	if nodeID == "" {
		return fmt.Errorf("nodeID must not be empty")
	}
	return rd.WriteNodeArtifact(nodeID, "response.md", []byte(response))
}
