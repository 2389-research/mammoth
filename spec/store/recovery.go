// ABOUTME: Crash recovery and self-healing for spec state reconstruction.
// ABOUTME: Combines snapshots, JSONL repair, event replay, and SQLite integrity checks.
package store

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/2389-research/mammoth/spec/core"
	"github.com/oklog/ulid/v2"
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

	// Derive the expected SpecID from the directory basename. Spec directories
	// are named by their ULID (e.g. home/specs/{ulid}). If the basename is not
	// a valid ULID (e.g. during tests with arbitrary names), we skip SpecID
	// filtering and accept all events.
	var expectedSpecID ulid.ULID
	var filterBySpecID bool
	if parsed, err := ulid.Parse(filepath.Base(specDir)); err == nil {
		expectedSpecID = parsed
		filterBySpecID = true
	}

	// Step 1: Try to load latest snapshot
	snapshot, err := LoadLatestSnapshot(snapshotsDir)
	if err != nil {
		return nil, 0, fmt.Errorf("load snapshot: %w", err)
	}

	var state *core.SpecState
	var snapshotEventID uint64

	if snapshot != nil {
		log.Printf("component=spec.store action=recover_loaded_snapshot event_id=%d", snapshot.LastEventID)
		state = snapshot.State
		snapshotEventID = snapshot.LastEventID
	} else {
		log.Printf("component=spec.store action=recover_no_snapshot")
		state = core.NewSpecState()
		snapshotEventID = 0
	}

	// Step 2: Repair JSONL if it exists
	if _, err := os.Stat(eventsPath); err == nil {
		repairedCount, err := RepairJsonl(eventsPath)
		if err != nil {
			return nil, 0, fmt.Errorf("repair jsonl: %w", err)
		}
		log.Printf("component=spec.store action=recover_jsonl_repaired valid_events=%d", repairedCount)
	}

	// Step 3: Replay events from the JSONL log, filtering by SpecID when the
	// directory name is a valid ULID. Events belonging to a different spec are
	// rejected to prevent cross-contamination from a mixed or corrupt log.
	var allEvents []core.Event
	if _, err := os.Stat(eventsPath); err == nil {
		rawEvents, replayErr := ReplayJsonl(eventsPath)
		if replayErr != nil {
			return nil, 0, fmt.Errorf("replay jsonl: %w", replayErr)
		}
		for i := range rawEvents {
			if filterBySpecID && rawEvents[i].SpecID != expectedSpecID {
				log.Printf("component=spec.store action=recover_skip_mismatched_spec event_id=%d found_spec_id=%s expected_spec_id=%s",
					rawEvents[i].EventID, rawEvents[i].SpecID, expectedSpecID)
				continue
			}
			allEvents = append(allEvents, rawEvents[i])
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

	log.Printf("component=spec.store action=recover_replayed_events replayed=%d total_on_disk=%d",
		tailCount, len(allEvents))

	lastEventID := state.LastEventID

	// Step 5 & 6: Check SQLite integrity and rebuild if needed.
	// When a snapshot exists but the event log is empty or missing, trust the
	// snapshot state and set last_event_id from the snapshot rather than
	// rebuilding an empty index (which would reset last_event_id to 0).
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
		log.Printf("component=spec.store action=recover_sqlite_uptodate event_id=%d", sqliteLastID)
	} else if len(allEvents) == 0 && snapshot != nil {
		// No events on disk but a snapshot exists: trust the snapshot and
		// set the SQLite last_event_id to match, without rebuilding empty.
		log.Printf("component=spec.store action=recover_trust_snapshot_no_events event_id=%d", lastEventID)
		if err := index.SetLastEventID(lastEventID); err != nil {
			return nil, 0, fmt.Errorf("set sqlite last_event_id from snapshot: %w", err)
		}
	} else if found {
		log.Printf("component=spec.store action=recover_sqlite_stale_rebuilding sqlite_event_id=%d expected_event_id=%d",
			sqliteLastID, lastEventID)
		if err := index.RebuildFromEvents(allEvents); err != nil {
			return nil, 0, fmt.Errorf("rebuild sqlite: %w", err)
		}
	} else {
		log.Printf("component=spec.store action=recover_sqlite_empty_rebuilding")
		if err := index.RebuildFromEvents(allEvents); err != nil {
			return nil, 0, fmt.Errorf("build sqlite: %w", err)
		}
	}

	return state, lastEventID, nil
}
