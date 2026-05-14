package session

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/conduit"
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

// Manager owns the Thread↔Step binding and the inference pipeline.
type Manager struct {
	store     thread.Store
	provider  provider.Provider
	newStep   func() *loop.Step
	processor TurnProcessor
	sessions  map[string]*managedSession
	mu        sync.RWMutex
}

type managedSession struct {
	thread *thread.Thread
	step   *loop.Step
	mu     sync.Mutex
	busy   bool
	cancel context.CancelFunc
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
func (m *Manager) Create() (*Session, error) {
	thr, err := m.store.Create()
	if err != nil {
		return nil, fmt.Errorf("create thread: %w", err)
	}
	step := m.newStep()
	sess := &managedSession{thread: thr, step: step}

	m.mu.Lock()
	m.sessions[thr.ID] = sess
	m.mu.Unlock()

	return &Session{id: thr.ID, thread: thr}, nil
}

// Attach gets or creates an active session for an existing thread.
// If the thread does not exist in the store, an error is returned.
func (m *Manager) Attach(threadID string) (*Session, error) {
	m.mu.RLock()
	sess, ok := m.sessions[threadID]
	m.mu.RUnlock()
	if ok {
		return &Session{id: threadID, thread: sess.thread}, nil
	}

	thr, ok := m.store.Get(threadID)
	if !ok {
		return nil, fmt.Errorf("thread %s not found", threadID)
	}

	step := m.newStep()
	sess = &managedSession{thread: thr, step: step}

	m.mu.Lock()
	if existing, ok := m.sessions[threadID]; ok {
		m.mu.Unlock()
		return &Session{id: threadID, thread: existing.thread}, nil
	}
	m.sessions[threadID] = sess
	m.mu.Unlock()

	return &Session{id: threadID, thread: thr}, nil
}

// Process submits the event to the session's state and runs the inference
// pipeline. The session must exist and not be busy; otherwise ErrSessionBusy
// or a not-found error is returned. Context cancellation aborts the running
// TurnProcessor.
func (m *Manager) Process(ctx context.Context, sessionID string, event conduit.Event) error {
	m.mu.RLock()
	sess, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}

	// Non-blocking lock.
	sess.mu.Lock()
	if sess.busy {
		sess.mu.Unlock()
		return ErrSessionBusy
	}
	sess.busy = true
	turnCtx, cancel := context.WithCancel(ctx)
	sess.cancel = cancel
	sess.mu.Unlock()

	var runErr error
	switch e := event.(type) {
	case conduit.UserMessageEvent:
		_, runErr = sess.step.Submit(turnCtx, sess.thread.State, state.RoleUser, artifact.Text{Content: e.Content})
		if runErr == nil {
			_, runErr = m.processor(turnCtx, sess.step, sess.thread.State, m.provider)
		}
	case conduit.InterruptEvent:
		// Interrupt is handled by cancelling the ongoing turn context.
		// No inference is started for an interrupt event itself.
		cancel()
	default:
		runErr = fmt.Errorf("unsupported event kind: %s", event.Kind())
	}

	// Cleanup.
	sess.mu.Lock()
	sess.busy = false
	sess.cancel = nil
	sess.mu.Unlock()
	cancel()

	// Save thread state regardless of run outcome.
	if saveErr := m.store.Save(sess.thread); saveErr != nil && runErr == nil {
		runErr = fmt.Errorf("save thread: %w", saveErr)
	}

	if runErr != nil {
		return fmt.Errorf("process event: %w", runErr)
	}
	return nil
}

// Cancel aborts an ongoing turn in the given session by cancelling its
// context. If the session is not found, an error is returned.
func (m *Manager) Cancel(sessionID string) error {
	m.mu.RLock()
	sess, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}

	sess.mu.Lock()
	if sess.cancel != nil {
		sess.cancel()
	}
	sess.mu.Unlock()
	return nil
}

// Subscribe returns a filtered output event channel for the session's
// loop.Step FanOut. The channel is closed when the session's Step is
// closed. An error is returned if the session does not exist.
func (m *Manager) Subscribe(sessionID string, kinds ...string) (<-chan loop.OutputEvent, error) {
	m.mu.RLock()
	sess, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	return sess.step.Subscribe(kinds...), nil
}

// Close closes a session's Step and removes it from the active map.
// The underlying thread is NOT deleted from the store.
func (m *Manager) Close(sessionID string) error {
	m.mu.Lock()
	sess, ok := m.sessions[sessionID]
	delete(m.sessions, sessionID)
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}
	if sess.step != nil {
		_ = sess.step.Close()
	}
	return nil
}

// Store returns the underlying thread.Store.
func (m *Manager) Store() thread.Store {
	return m.store
}

// List returns handles for all active sessions.
func (m *Manager) List() []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*Session, 0, len(m.sessions))
	for id, sess := range m.sessions {
		result = append(result, &Session{id: id, thread: sess.thread})
	}
	return result
}
