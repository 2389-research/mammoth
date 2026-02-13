// ABOUTME: Crash recovery and self-healing for spec state reconstruction.
// ABOUTME: Combines snapshots, JSONL repair, event replay, and SQLite integrity checks.
package store

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/2389-research/mammoth/spec/core"
)

// RecoverSpec recovers a spec's state from its storage directory.
//
// Recovery sequence:
//  1. Try to load the latest snapshot
//  2. Repair the JSONL event log (truncate partial last line)
//  3. Replay all events from the JSONL
//  4. Apply events that are newer than the snapshot
//  5. Open SQLite index and check integrity (compare last_event_id)
//  6. If mismatch or empty: rebuild SQLite from all events
//  7. Return recovered state and last_event_id
func RecoverSpec(specDir string) (*core.SpecState, uint64, error) {
	eventsPath := filepath.Join(specDir, "events.jsonl")
	snapshotsDir := filepath.Join(specDir, "snapshots")
	indexPath := filepath.Join(specDir, "index.db")

	// Step 1: Try to load latest snapshot
	snapshot, err := LoadLatestSnapshot(snapshotsDir)
	if err != nil {
		return nil, 0, fmt.Errorf("load snapshot: %w", err)
	}

	var state *core.SpecState
	var snapshotEventID uint64

	if snapshot != nil {
		log.Printf("INFO: loaded snapshot at event %d", snapshot.LastEventID)
		state = snapshot.State
		snapshotEventID = snapshot.LastEventID
	} else {
		log.Printf("INFO: no snapshot found, starting from empty state")
		state = core.NewSpecState()
		snapshotEventID = 0
	}

	// Step 2: Repair JSONL if it exists
	if _, err := os.Stat(eventsPath); err == nil {
		repairedCount, err := RepairJsonl(eventsPath)
		if err != nil {
			return nil, 0, fmt.Errorf("repair jsonl: %w", err)
		}
		log.Printf("INFO: repaired JSONL: %d valid events", repairedCount)
	}

	// Step 3: Replay events from the JSONL log
	var allEvents []core.Event
	if _, err := os.Stat(eventsPath); err == nil {
		allEvents, err = ReplayJsonl(eventsPath)
		if err != nil {
			return nil, 0, fmt.Errorf("replay jsonl: %w", err)
		}
	}

	// Step 4: Apply events that are newer than the snapshot
	var tailCount int
	for i := range allEvents {
		if allEvents[i].EventID > snapshotEventID {
			state.Apply(&allEvents[i])
			tailCount++
		}
	}

	log.Printf("INFO: replayed %d events after snapshot (total %d events on disk)",
		tailCount, len(allEvents))

	lastEventID := state.LastEventID

	// Step 5 & 6: Check SQLite integrity and rebuild if needed
	index, err := OpenSqlite(indexPath)
	if err != nil {
		return nil, 0, fmt.Errorf("open sqlite index: %w", err)
	}
	defer func() { _ = index.Close() }()

	sqliteLastID, found, err := index.GetLastEventID()
	if err != nil {
		return nil, 0, fmt.Errorf("get sqlite last_event_id: %w", err)
	}

	if found && sqliteLastID == lastEventID {
		log.Printf("INFO: SQLite index is up to date at event %d", sqliteLastID)
	} else if found {
		log.Printf("WARNING: SQLite index stale (at event %d, expected %d), rebuilding",
			sqliteLastID, lastEventID)
		if err := index.RebuildFromEvents(allEvents); err != nil {
			return nil, 0, fmt.Errorf("rebuild sqlite: %w", err)
		}
	} else {
		log.Printf("INFO: SQLite index empty, building from events")
		if err := index.RebuildFromEvents(allEvents); err != nil {
			return nil, 0, fmt.Errorf("build sqlite: %w", err)
		}
	}

	return state, lastEventID, nil
}
