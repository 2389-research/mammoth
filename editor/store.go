// ABOUTME: In-memory session store with TTL cleanup and capacity limits
// ABOUTME: Thread-safe storage for managing active editor sessions

package editor

import (
	"fmt"
	"sync"
	"time"

	"github.com/2389-research/mammoth/dot"
	"github.com/2389-research/mammoth/dot/validator"
	"github.com/google/uuid"
)

type Store struct {
	mu          sync.RWMutex
	sessions    map[string]*Session
	maxSessions int
	ttl         time.Duration
}

// NewStore creates a new session store
func NewStore(maxSessions int, ttl time.Duration) *Store {
	return &Store{
		sessions:    make(map[string]*Session),
		maxSessions: maxSessions,
		ttl:         ttl,
	}
}

// Create creates a new session from DOT source
func (s *Store) Create(rawDOT string) (*Session, error) {
	graph, err := dot.Parse(rawDOT)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check capacity
	if len(s.sessions) >= s.maxSessions {
		// Evict oldest session
		var oldestID string
		var oldestTime time.Time
		for id, sess := range s.sessions {
			if oldestTime.IsZero() || sess.LastAccess.Before(oldestTime) {
				oldestID = id
				oldestTime = sess.LastAccess
			}
		}
		delete(s.sessions, oldestID)
	}

	now := time.Now()
	sess := &Session{
		ID:          uuid.New().String(),
		Graph:       graph,
		RawDOT:      rawDOT,
		Diagnostics: validator.Lint(graph),
		UndoStack:   make([]string, 0, 50),
		RedoStack:   make([]string, 0, 50),
		CreatedAt:   now,
		LastAccess:  now,
	}

	s.sessions[sess.ID] = sess
	return sess, nil
}

// Get retrieves a session by ID and updates its LastAccess time
func (s *Store) Get(id string) (*Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, ok := s.sessions[id]
	if !ok {
		return nil, false
	}

	sess.LastAccess = time.Now()
	return sess, true
}

// Cleanup removes sessions older than TTL
func (s *Store) Cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-s.ttl)
	for id, sess := range s.sessions {
		if sess.LastAccess.Before(cutoff) {
			delete(s.sessions, id)
		}
	}
}

// StartCleanup starts a background cleanup goroutine and returns a stop function
func (s *Store) StartCleanup(interval time.Duration) func() {
	ticker := time.NewTicker(interval)
	done := make(chan struct{})

	go func() {
		for {
			select {
			case <-ticker.C:
				s.Cleanup()
			case <-done:
				ticker.Stop()
				return
			}
		}
	}()

	return func() {
		close(done)
	}
}
