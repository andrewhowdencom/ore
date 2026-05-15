package session

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/state"
	"github.com/andrewhowdencom/ore/thread"
)

// TurnProcessor runs the full inference pipeline for a single turn after
// the user event has been submitted to state. The Manager calls this with
// the session's loop.Step, state, and provider.
type TurnProcessor func(ctx context.Context, step *loop.Step, st state.State, prov provider.Provider) (state.State, error)

// ManagerOption configures a Manager.
type ManagerOption func(*Manager)

// ErrSessionBusy is returned when a session is already processing a turn.
var ErrSessionBusy = errors.New("session is busy processing another turn")

// Manager owns the Thread↔Step binding and acts as a factory/registry for
// Session handles.
type Manager struct {
	store     thread.Store
	provider  provider.Provider
	newStep   func() *loop.Step
	processor TurnProcessor
	sessions  map[string]*managedSession
	mu        sync.RWMutex
}

type managedSession struct {
	id        string
	thread    *thread.Thread
	step      *loop.Step
	provider  provider.Provider
	processor TurnProcessor
	store     thread.Store
	mu        sync.Mutex
	busy      bool
	cancel    context.CancelFunc
	closed    bool
}

// NewManager creates a new Manager with the given dependencies.
func NewManager(store thread.Store, prov provider.Provider, newStep func() *loop.Step, processor TurnProcessor, opts ...ManagerOption) *Manager {
	m := &Manager{
		store:     store,
		provider:  prov,
		newStep:   newStep,
		processor: processor,
		sessions:  make(map[string]*managedSession),
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Create creates a new thread and an active session backed by it.
func (m *Manager) Create() (Session, error) {
	thr, err := m.store.Create()
	if err != nil {
		return nil, fmt.Errorf("create thread: %w", err)
	}
	step := m.newStep()
	sess := &managedSession{
		id:        thr.ID,
		thread:    thr,
		step:      step,
		provider:  m.provider,
		processor: m.processor,
		store:     m.store,
	}

	m.mu.Lock()
	m.sessions[thr.ID] = sess
	m.mu.Unlock()

	return sess, nil
}

// Attach gets or creates an active session for an existing thread.
// If the thread does not exist in the store, an error is returned.
func (m *Manager) Attach(threadID string) (Session, error) {
	m.mu.RLock()
	sess, ok := m.sessions[threadID]
	m.mu.RUnlock()
	if ok {
		return sess, nil
	}

	thr, ok := m.store.Get(threadID)
	if !ok {
		return nil, fmt.Errorf("thread %s not found", threadID)
	}

	step := m.newStep()
	sess = &managedSession{
		id:        threadID,
		thread:    thr,
		step:      step,
		provider:  m.provider,
		processor: m.processor,
		store:     m.store,
	}

	m.mu.Lock()
	if existing, ok := m.sessions[threadID]; ok {
		m.mu.Unlock()
		return existing, nil
	}
	m.sessions[threadID] = sess
	m.mu.Unlock()

	return sess, nil
}

// Process submits the event to the session's state and runs the inference
// pipeline. The session must not be busy. Context cancellation aborts the
// running TurnProcessor.
//
// Errors:
//   - ErrSessionBusy if the session is already processing a turn
//   - "unsupported event kind" for unknown event types
//   - "process event: ..." wrapping any TurnProcessor or save error
func (s *managedSession) Process(ctx context.Context, event Event) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("session %s is closed", s.id)
	}
	if s.busy {
		s.mu.Unlock()
		return ErrSessionBusy
	}
	s.busy = true
	turnCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.mu.Unlock()

	var runErr error
	switch e := event.(type) {
	case UserMessageEvent:
		_, runErr = s.step.Submit(turnCtx, s.thread.State, state.RoleUser, artifact.Text{Content: e.Content})
		if runErr == nil {
			_, runErr = s.processor(turnCtx, s.step, s.thread.State, s.provider)
		}
	case InterruptEvent:
		// Interrupt is handled by cancelling the ongoing turn context.
		// No inference is started for an interrupt event itself.
		cancel()
	default:
		runErr = fmt.Errorf("unsupported event kind: %s", event.Kind())
	}

	// Cleanup.
	s.mu.Lock()
	s.busy = false
	s.cancel = nil
	s.mu.Unlock()
	cancel()

	// Save thread state regardless of run outcome.
	if saveErr := s.store.Save(s.thread); saveErr != nil && runErr == nil {
		runErr = fmt.Errorf("save thread: %w", saveErr)
	}

	if runErr != nil {
		return fmt.Errorf("process event: %w", runErr)
	}
	return nil
}

// Cancel aborts an ongoing turn by cancelling its context.
func (s *managedSession) Cancel() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("session %s is closed", s.id)
	}
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()
	return nil
}

// Subscribe returns a filtered output event channel for the session's
// loop.Step FanOut. An error is returned if the session is closed.
//
// The returned channel is closed when the session is closed.
// Callers should range over the channel and handle closure:
//
//	ch, _ := sess.Subscribe("text_delta", "turn_complete")
//	for event := range ch {
//	    // process event
//	}
func (s *managedSession) Subscribe(kinds ...string) (<-chan loop.OutputEvent, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, fmt.Errorf("session %s is closed", s.id)
	}
	s.mu.Unlock()
	return s.step.Subscribe(kinds...), nil
}

// ID returns the session's unique identifier (same as the thread ID).
func (s *managedSession) ID() string { return s.id }

// Close closes the session's Step and marks it as closed.
// The underlying thread is NOT deleted from the store.
func (s *managedSession) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	if s.step != nil {
		_ = s.step.Close()
	}
	return nil
}

// Close closes a session and removes it from the active map.
// The underlying thread is NOT deleted from the store.
func (m *Manager) Close(sessionID string) error {
	m.mu.Lock()
	sess, ok := m.sessions[sessionID]
	delete(m.sessions, sessionID)
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}
	return sess.Close()
}

// Store returns the underlying thread.Store.
func (m *Manager) Store() thread.Store {
	return m.store
}

// List returns handles for all active sessions.
func (m *Manager) List() []Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]Session, 0, len(m.sessions))
	for _, sess := range m.sessions {
		result = append(result, sess)
	}
	return result
}

// Get returns the public Session handle for an active session.
// An error is returned if the session does not exist.
func (m *Manager) Get(sessionID string) (Session, error) {
	m.mu.RLock()
	sess, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	return sess, nil
}

// Check verifies that the session exists and is not busy. It returns nil if
// the session is ready to process.
//
// Errors:
//   - "session not found" if the session does not exist
//   - ErrSessionBusy if the session is already processing a turn
func (m *Manager) Check(sessionID string) error {
	m.mu.RLock()
	sess, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}
	sess.mu.Lock()
	busy := sess.busy
	sess.mu.Unlock()
	if busy {
		return ErrSessionBusy
	}
	return nil
}
