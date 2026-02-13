// ABOUTME: Atomic snapshot save and load for SpecState persistence.
// ABOUTME: Writes snapshots with atomic rename for crash safety and loads the latest by event ID.
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/2389-research/mammoth/spec/core"
	"github.com/oklog/ulid/v2"
)

// SnapshotData is a full snapshot of spec state at a given event, including
// optional agent context for restoring agent-specific working memory.
type SnapshotData struct {
	State         *core.SpecState            `json:"-"` // Custom marshal/unmarshal
	LastEventID   uint64                     `json:"last_event_id"`
	AgentContexts map[string]json.RawMessage `json:"agent_contexts"`
	SavedAt       time.Time                  `json:"saved_at"`
}

// snapshotStateJSON is the wire format for SpecState within a snapshot,
// handling PendingQuestion (interface) and OrderedMap (no generic unmarshal).
type snapshotStateJSON struct {
	Core            *core.SpecCore           `json:"core"`
	Cards           map[string]core.Card     `json:"cards"`
	Transcript      []core.TranscriptMessage `json:"transcript"`
	PendingQuestion json.RawMessage          `json:"pending_question,omitempty"`
	UndoStack       []core.UndoEntry         `json:"undo_stack"`
	LastEventID     uint64                   `json:"last_event_id"`
	Lanes           []string                 `json:"lanes"`
}

// snapshotJSON is the wire format for SnapshotData.
type snapshotJSON struct {
	State         snapshotStateJSON          `json:"state"`
	LastEventID   uint64                     `json:"last_event_id"`
	AgentContexts map[string]json.RawMessage `json:"agent_contexts"`
	SavedAt       time.Time                  `json:"saved_at"`
}

// MarshalJSON serializes the SnapshotData with proper handling of SpecState internals.
func (sd SnapshotData) MarshalJSON() ([]byte, error) {
	stateJSON := snapshotStateJSON{
		Core:        sd.State.Core,
		Cards:       make(map[string]core.Card),
		Transcript:  sd.State.Transcript,
		UndoStack:   sd.State.UndoStack,
		LastEventID: sd.State.LastEventID,
		Lanes:       sd.State.Lanes,
	}

	// Convert OrderedMap to regular map for serialization
	sd.State.Cards.Range(func(k ulid.ULID, v core.Card) bool {
		stateJSON.Cards[k.String()] = v
		return true
	})

	// Serialize PendingQuestion if present
	if sd.State.PendingQuestion != nil {
		pqData, err := core.MarshalUserQuestion(sd.State.PendingQuestion)
		if err != nil {
			return nil, fmt.Errorf("marshal pending question: %w", err)
		}
		stateJSON.PendingQuestion = pqData
	}

	j := snapshotJSON{
		State:         stateJSON,
		LastEventID:   sd.LastEventID,
		AgentContexts: sd.AgentContexts,
		SavedAt:       sd.SavedAt,
	}

	return json.Marshal(j)
}

// UnmarshalJSON deserializes the SnapshotData with proper handling of SpecState internals.
func (sd *SnapshotData) UnmarshalJSON(data []byte) error {
	var j snapshotJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return err
	}

	sd.LastEventID = j.LastEventID
	sd.AgentContexts = j.AgentContexts
	sd.SavedAt = j.SavedAt

	state := core.NewSpecState()
	state.Core = j.State.Core
	state.Transcript = j.State.Transcript
	state.UndoStack = j.State.UndoStack
	state.LastEventID = j.State.LastEventID
	state.Lanes = j.State.Lanes

	// Rebuild OrderedMap from the deserialized map
	for keyStr, card := range j.State.Cards {
		id, err := ulid.Parse(keyStr)
		if err != nil {
			return fmt.Errorf("parse card ULID %q: %w", keyStr, err)
		}
		state.Cards.Set(id, card)
	}

	// Deserialize PendingQuestion if present
	if len(j.State.PendingQuestion) > 0 && string(j.State.PendingQuestion) != "null" {
		q, err := core.UnmarshalUserQuestion(j.State.PendingQuestion)
		if err != nil {
			return fmt.Errorf("unmarshal pending question: %w", err)
		}
		state.PendingQuestion = q
	}

	// Ensure slices are non-nil for consistent behavior
	if state.Transcript == nil {
		state.Transcript = []core.TranscriptMessage{}
	}
	if state.UndoStack == nil {
		state.UndoStack = []core.UndoEntry{}
	}

	sd.State = state
	return nil
}

// SaveSnapshot saves a snapshot to disk using atomic write (write to .tmp,
// fsync, rename). Creates the target directory if it does not exist.
func SaveSnapshot(dir string, data *SnapshotData) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create snapshot dir: %w", err)
	}

	tmpPath := filepath.Join(dir, fmt.Sprintf("state_%d.tmp", data.LastEventID))
	finalPath := filepath.Join(dir, fmt.Sprintf("state_%d.json", data.LastEventID))

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal snapshot: %w", err)
	}

	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("create temp snapshot: %w", err)
	}

	if _, err := tmpFile.Write(jsonData); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write snapshot data: %w", err)
	}

	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("fsync snapshot: %w", err)
	}
	_ = tmpFile.Close()

	if err := os.Rename(tmpPath, finalPath); err != nil {
		return fmt.Errorf("rename snapshot: %w", err)
	}

	return nil
}

// LoadLatestSnapshot loads the snapshot with the highest event ID from the
// given directory. Returns nil if the directory is empty or does not exist.
func LoadLatestSnapshot(dir string) (*SnapshotData, error) {
	info, err := os.Stat(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("stat snapshot dir: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("snapshot path is not a directory: %s", dir)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read snapshot dir: %w", err)
	}

	var bestEventID uint64
	var bestPath string
	found := false

	for _, entry := range entries {
		name := entry.Name()

		// Match pattern: state_<event_id>.json
		if !strings.HasPrefix(name, "state_") || !strings.HasSuffix(name, ".json") {
			continue
		}
		idStr := strings.TrimPrefix(name, "state_")
		idStr = strings.TrimSuffix(idStr, ".json")

		eventID, err := strconv.ParseUint(idStr, 10, 64)
		if err != nil {
			continue
		}

		if !found || eventID > bestEventID {
			bestEventID = eventID
			bestPath = filepath.Join(dir, name)
			found = true
		}
	}

	if !found {
		return nil, nil
	}

	contents, err := os.ReadFile(bestPath)
	if err != nil {
		return nil, fmt.Errorf("read snapshot file: %w", err)
	}

	var data SnapshotData
	if err := json.Unmarshal(contents, &data); err != nil {
		return nil, fmt.Errorf("parse snapshot: %w", err)
	}

	return &data, nil
}
