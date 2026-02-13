// ABOUTME: SQLite-backed index for fast spec and card queries without replaying events.
// ABOUTME: Provides upsert, delete, list, and rebuild operations synchronized with the event log.
package store

import (
	"database/sql"
	"fmt"

	"github.com/2389-research/mammoth/spec/core"
	_ "github.com/mattn/go-sqlite3"
	"github.com/oklog/ulid/v2"
)

// SpecSummary is a summary of a spec for list queries, matching the API's shape.
type SpecSummary struct {
	SpecID    string
	Title     string
	OneLiner  string
	Goal      string
	UpdatedAt string
}

// CardRow is a row from the cards table for list query results.
type CardRow struct {
	CardID    string
	SpecID    string
	CardType  string
	Title     string
	Body      *string
	Lane      string
	SortOrder float64
	CreatedBy string
	UpdatedAt string
}

// SqliteIndex is a SQLite-backed index that mirrors spec and card data for
// fast reads. This index is always rebuildable from the event log and serves
// as a queryable cache, not the source of truth.
type SqliteIndex struct {
	db *sql.DB
}

// OpenSqlite opens or creates a SQLite index database at the given path.
// Runs migrations to ensure the schema is up to date.
func OpenSqlite(path string) (*SqliteIndex, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	schema := `
		CREATE TABLE IF NOT EXISTS specs (
			spec_id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			one_liner TEXT NOT NULL,
			goal TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);

		CREATE TABLE IF NOT EXISTS cards (
			card_id TEXT PRIMARY KEY,
			spec_id TEXT NOT NULL,
			card_type TEXT NOT NULL,
			title TEXT NOT NULL,
			body TEXT,
			lane TEXT NOT NULL,
			sort_order REAL NOT NULL,
			created_by TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY (spec_id) REFERENCES specs(spec_id)
		);

		CREATE TABLE IF NOT EXISTS meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);`

	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}

	return &SqliteIndex{db: db}, nil
}

// Close closes the SQLite database connection.
func (idx *SqliteIndex) Close() error {
	return idx.db.Close()
}

// UpdateSpec upserts a spec row from a SpecCore.
func (idx *SqliteIndex) UpdateSpec(spec *core.SpecCore) error {
	_, err := idx.db.Exec(
		`INSERT INTO specs (spec_id, title, one_liner, goal, updated_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(spec_id) DO UPDATE SET
			title = excluded.title,
			one_liner = excluded.one_liner,
			goal = excluded.goal,
			updated_at = excluded.updated_at`,
		spec.SpecID.String(),
		spec.Title,
		spec.OneLiner,
		spec.Goal,
		spec.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	)
	if err != nil {
		return fmt.Errorf("upsert spec: %w", err)
	}
	return nil
}

// UpdateCard upserts a card row.
func (idx *SqliteIndex) UpdateCard(specID ulid.ULID, card *core.Card) error {
	var body *string
	if card.Body != nil {
		body = card.Body
	}
	_, err := idx.db.Exec(
		`INSERT INTO cards (card_id, spec_id, card_type, title, body, lane, sort_order, created_by, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(card_id) DO UPDATE SET
			card_type = excluded.card_type,
			title = excluded.title,
			body = excluded.body,
			lane = excluded.lane,
			sort_order = excluded.sort_order,
			updated_at = excluded.updated_at`,
		card.CardID.String(),
		specID.String(),
		card.CardType,
		card.Title,
		body,
		card.Lane,
		card.Order,
		card.CreatedBy,
		card.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	)
	if err != nil {
		return fmt.Errorf("upsert card: %w", err)
	}
	return nil
}

// DeleteCard removes a card row by card_id.
func (idx *SqliteIndex) DeleteCard(cardID ulid.ULID) error {
	_, err := idx.db.Exec("DELETE FROM cards WHERE card_id = ?", cardID.String())
	if err != nil {
		return fmt.Errorf("delete card: %w", err)
	}
	return nil
}

// ListSpecs returns all specs as summaries, ordered by updated_at descending.
func (idx *SqliteIndex) ListSpecs() ([]SpecSummary, error) {
	rows, err := idx.db.Query(
		"SELECT spec_id, title, one_liner, goal, updated_at FROM specs ORDER BY updated_at DESC")
	if err != nil {
		return nil, fmt.Errorf("query specs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var specs []SpecSummary
	for rows.Next() {
		var s SpecSummary
		if err := rows.Scan(&s.SpecID, &s.Title, &s.OneLiner, &s.Goal, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan spec row: %w", err)
		}
		specs = append(specs, s)
	}
	return specs, rows.Err()
}

// ListCards returns all cards for a given spec, ordered by sort_order ascending.
func (idx *SqliteIndex) ListCards(specID ulid.ULID) ([]CardRow, error) {
	rows, err := idx.db.Query(
		`SELECT card_id, spec_id, card_type, title, body, lane, sort_order, created_by, updated_at
		 FROM cards WHERE spec_id = ? ORDER BY sort_order ASC`,
		specID.String())
	if err != nil {
		return nil, fmt.Errorf("query cards: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var cards []CardRow
	for rows.Next() {
		var c CardRow
		if err := rows.Scan(&c.CardID, &c.SpecID, &c.CardType, &c.Title, &c.Body,
			&c.Lane, &c.SortOrder, &c.CreatedBy, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan card row: %w", err)
		}
		cards = append(cards, c)
	}
	return cards, rows.Err()
}

// GetLastEventID returns the last event ID that was indexed, from the meta table.
// Returns 0, false if no last event ID has been set.
func (idx *SqliteIndex) GetLastEventID() (uint64, bool, error) {
	var val string
	err := idx.db.QueryRow("SELECT value FROM meta WHERE key = 'last_event_id'").Scan(&val)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("query last_event_id: %w", err)
	}
	var id uint64
	if _, err := fmt.Sscanf(val, "%d", &id); err != nil {
		return 0, false, fmt.Errorf("parse last_event_id: %w", err)
	}
	return id, true, nil
}

// SetLastEventID stores the last event ID in the meta table.
func (idx *SqliteIndex) SetLastEventID(eventID uint64) error {
	_, err := idx.db.Exec(
		`INSERT INTO meta (key, value) VALUES ('last_event_id', ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		fmt.Sprintf("%d", eventID))
	if err != nil {
		return fmt.Errorf("set last_event_id: %w", err)
	}
	return nil
}

// RebuildFromEvents clears all data and rebuilds from a list of events.
func (idx *SqliteIndex) RebuildFromEvents(events []core.Event) error {
	if _, err := idx.db.Exec("DELETE FROM cards"); err != nil {
		return fmt.Errorf("clear cards: %w", err)
	}
	if _, err := idx.db.Exec("DELETE FROM specs"); err != nil {
		return fmt.Errorf("clear specs: %w", err)
	}
	if _, err := idx.db.Exec("DELETE FROM meta"); err != nil {
		return fmt.Errorf("clear meta: %w", err)
	}

	for i := range events {
		if err := idx.ApplyEvent(&events[i]); err != nil {
			return fmt.Errorf("apply event %d during rebuild: %w", events[i].EventID, err)
		}
	}

	return nil
}

// ApplyEvent incrementally applies a single event to update the index.
func (idx *SqliteIndex) ApplyEvent(event *core.Event) error {
	specID := event.SpecID
	ts := event.Timestamp.Format("2006-01-02T15:04:05Z07:00")

	switch p := event.Payload.(type) {
	case core.SpecCreatedPayload:
		_, err := idx.db.Exec(
			`INSERT INTO specs (spec_id, title, one_liner, goal, updated_at)
			 VALUES (?, ?, ?, ?, ?)
			 ON CONFLICT(spec_id) DO UPDATE SET
				title = excluded.title,
				one_liner = excluded.one_liner,
				goal = excluded.goal,
				updated_at = excluded.updated_at`,
			specID.String(), p.Title, p.OneLiner, p.Goal, ts)
		if err != nil {
			return fmt.Errorf("apply SpecCreated: %w", err)
		}

	case core.SpecCoreUpdatedPayload:
		if p.Title != nil {
			if _, err := idx.db.Exec("UPDATE specs SET title = ?, updated_at = ? WHERE spec_id = ?",
				*p.Title, ts, specID.String()); err != nil {
				return fmt.Errorf("apply SpecCoreUpdated title: %w", err)
			}
		}
		if p.OneLiner != nil {
			if _, err := idx.db.Exec("UPDATE specs SET one_liner = ?, updated_at = ? WHERE spec_id = ?",
				*p.OneLiner, ts, specID.String()); err != nil {
				return fmt.Errorf("apply SpecCoreUpdated one_liner: %w", err)
			}
		}
		if p.Goal != nil {
			if _, err := idx.db.Exec("UPDATE specs SET goal = ?, updated_at = ? WHERE spec_id = ?",
				*p.Goal, ts, specID.String()); err != nil {
				return fmt.Errorf("apply SpecCoreUpdated goal: %w", err)
			}
		}
		// Always update the updated_at timestamp
		if _, err := idx.db.Exec("UPDATE specs SET updated_at = ? WHERE spec_id = ?",
			ts, specID.String()); err != nil {
			return fmt.Errorf("apply SpecCoreUpdated updated_at: %w", err)
		}

	case core.CardCreatedPayload:
		card := p.Card
		if err := idx.UpdateCard(specID, &card); err != nil {
			return fmt.Errorf("apply CardCreated: %w", err)
		}

	case core.CardUpdatedPayload:
		if p.Title != nil {
			if _, err := idx.db.Exec("UPDATE cards SET title = ?, updated_at = ? WHERE card_id = ?",
				*p.Title, ts, p.CardID.String()); err != nil {
				return fmt.Errorf("apply CardUpdated title: %w", err)
			}
		}
		if p.Body.Set {
			var body *string
			if p.Body.Valid {
				body = &p.Body.Value
			}
			if _, err := idx.db.Exec("UPDATE cards SET body = ?, updated_at = ? WHERE card_id = ?",
				body, ts, p.CardID.String()); err != nil {
				return fmt.Errorf("apply CardUpdated body: %w", err)
			}
		}
		if p.CardType != nil {
			if _, err := idx.db.Exec("UPDATE cards SET card_type = ?, updated_at = ? WHERE card_id = ?",
				*p.CardType, ts, p.CardID.String()); err != nil {
				return fmt.Errorf("apply CardUpdated card_type: %w", err)
			}
		}
		if _, err := idx.db.Exec("UPDATE cards SET updated_at = ? WHERE card_id = ?",
			ts, p.CardID.String()); err != nil {
			return fmt.Errorf("apply CardUpdated updated_at: %w", err)
		}

	case core.CardMovedPayload:
		if _, err := idx.db.Exec(
			"UPDATE cards SET lane = ?, sort_order = ?, updated_at = ? WHERE card_id = ?",
			p.Lane, p.Order, ts, p.CardID.String()); err != nil {
			return fmt.Errorf("apply CardMoved: %w", err)
		}

	case core.CardDeletedPayload:
		if err := idx.DeleteCard(p.CardID); err != nil {
			return fmt.Errorf("apply CardDeleted: %w", err)
		}

	case core.UndoAppliedPayload:
		// Apply inverse events to the index
		for _, inversePayload := range p.InverseEvents {
			synthetic := &core.Event{
				EventID:   event.EventID,
				SpecID:    event.SpecID,
				Timestamp: event.Timestamp,
				Payload:   inversePayload,
			}
			if err := idx.ApplyEvent(synthetic); err != nil {
				return fmt.Errorf("apply UndoApplied inverse: %w", err)
			}
		}

	default:
		// Other event types don't affect the index
	}

	if err := idx.SetLastEventID(event.EventID); err != nil {
		return fmt.Errorf("set last_event_id after apply: %w", err)
	}

	return nil
}
