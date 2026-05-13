package http

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"

	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/state"
)

// Session represents an ephemeral conversation session held in memory.
// It owns a state.Memory and a loop.Step, and tracks whether a turn is
// currently in progress to prevent concurrent message processing.
type Session struct {
	id    string
	state *state.Memory
	step  *loop.Step
	mu    sync.Mutex
	busy  bool
}

// Lock attempts to mark the session as busy. It returns true if the session
// was not already busy and is now locked, or false if the session is already
// processing a turn. The caller must call Unlock when finished.
func (s *Session) Lock() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy {
		return false
	}
	s.busy = true
	return true
}

// Unlock marks the session as no longer busy.
func (s *Session) Unlock() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.busy = false
}

// SessionStore is a thread-safe, in-memory map of sessions keyed by ID.
type SessionStore struct {
	sessions map[string]*Session
	mu       sync.RWMutex
}

// NewSessionStore creates an empty session store.
func NewSessionStore() *SessionStore {
	return &SessionStore{
		sessions: make(map[string]*Session),
	}
}

// Create generates a new session ID, creates a Session with the given Step,
// and stores it. The Step is owned by the session and will be closed when the
// session is deleted.
func (s *SessionStore) Create(step *loop.Step) (*Session, error) {
	id, err := generateSessionID()
	if err != nil {
		return nil, fmt.Errorf("generate session id: %w", err)
	}

	session := &Session{
		id:    id,
		state: &state.Memory{},
		step:  step,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[id] = session
	return session, nil
}

// Get retrieves a session by ID. The second return value is false if the
// session does not exist.
func (s *SessionStore) Get(id string) (*Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.sessions[id]
	return session, ok
}

// Delete removes a session from the store, closes its Step, and returns true
// if the session existed.
func (s *SessionStore) Delete(id string) bool {
	s.mu.Lock()
	session, ok := s.sessions[id]
	if ok {
		delete(s.sessions, id)
	}
	s.mu.Unlock()

	if ok && session.step != nil {
		_ = session.step.Close()
	}
	return ok
}

// generateSessionID creates a random 32-character hex string (128 bits).
func generateSessionID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
