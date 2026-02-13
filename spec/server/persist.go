// ABOUTME: Event persistence helpers for writing events to JSONL files.
// ABOUTME: Provides PersistEvents for batch writes and SpawnEventPersister for background streaming.
package server

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"

	"github.com/oklog/ulid/v2"

	"github.com/2389-research/mammoth/spec/core"
)

// PersistEvents writes events to the JSONL file in the spec directory.
func PersistEvents(specDir string, events []core.Event) {
	logPath := filepath.Join(specDir, "events.jsonl")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		log.Printf("failed to open JSONL log: %v", err)
		return
	}
	defer func() { _ = f.Close() }()

	for _, event := range events {
		data, err := json.Marshal(event)
		if err != nil {
			log.Printf("failed to marshal event: %v", err)
			continue
		}
		data = append(data, '\n')
		if _, err := f.Write(data); err != nil {
			log.Printf("failed to write event: %v", err)
		}
	}
	_ = f.Sync()
}

// SpawnEventPersister starts a background goroutine that subscribes to an actor's
// broadcast channel and persists every event to JSONL.
func SpawnEventPersister(appState *AppState, specID ulid.ULID, handle *core.SpecActorHandle) {
	ch := handle.Subscribe()
	stopCh := make(chan struct{})
	appState.SetEventPersister(specID, stopCh)

	go func() {
		defer handle.Unsubscribe(ch)
		specDir := filepath.Join(appState.MammothHome, "specs", specID.String())
		logPath := filepath.Join(specDir, "events.jsonl")

		for {
			select {
			case event, ok := <-ch:
				if !ok {
					return
				}
				f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
				if err != nil {
					log.Printf("event persister: failed to open log: %v", err)
					continue
				}
				data, err := json.Marshal(event)
				if err != nil {
					log.Printf("event persister: failed to marshal event: %v", err)
					_ = f.Close()
					continue
				}
				data = append(data, '\n')
				_, _ = f.Write(data)
				_ = f.Sync()
				_ = f.Close()
			case <-stopCh:
				return
			}
		}
	}()
}
