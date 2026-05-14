package session

import "github.com/andrewhowdencom/ore/thread"

// Session is a public handle to an active session managed by a Manager.
// It provides identity and thread access without exposing the internal
// loop.Step or inference machinery.
type Session struct {
	id     string
	thread *thread.Thread
}

// ID returns the session's unique identifier (same as the thread ID).
func (s *Session) ID() string { return s.id }

// Thread returns the persistent thread backing this session.
func (s *Session) Thread() *thread.Thread { return s.thread }
