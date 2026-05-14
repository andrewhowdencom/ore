package http

import (
	"github.com/andrewhowdencom/ore/thread"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/state"
)

// Session represents an active HTTP session backed by a Thread.
type Session struct {
	id     string
	thread *thread.Thread
	step   *loop.Step
}

// ID returns the session's unique identifier (same as the thread ID).
func (s *Session) ID() string { return s.id }

// Step returns the loop.Step associated with this session.
func (s *Session) Step() *loop.Step { return s.step }

// State returns the session's thread state.
func (s *Session) State() state.State { return s.thread.State }

// Lock attempts to acquire the thread lock.
func (s *Session) Lock() bool {
	return s.thread.Lock()
}

// Unlock releases the thread lock.
func (s *Session) Unlock() {
	s.thread.Unlock()
}
