package http

import (
	"github.com/andrewhowdencom/ore/conversation"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/state"
)

// Session represents an active HTTP session backed by a Conversation.
type Session struct {
	id   string
	conv *conversation.Conversation
	step *loop.Step
}

// ID returns the session's unique identifier (same as the conversation ID).
func (s *Session) ID() string { return s.id }

// Step returns the loop.Step associated with this session.
func (s *Session) Step() *loop.Step { return s.step }

// State returns the session's conversation state.
func (s *Session) State() state.State { return s.conv.State }

// Lock attempts to acquire the conversation lock.
func (s *Session) Lock() bool {
	return s.conv.Lock()
}

// Unlock releases the conversation lock.
func (s *Session) Unlock() {
	s.conv.Unlock()
}
