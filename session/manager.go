package session

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/state"
	"github.com/andrewhowdencom/ore/thread"
)

// TurnProcessor runs the full inference pipeline for a single turn after
// the user event has been submitted to state. It is called with the
// stream's loop.Step, state, and provider.
type TurnProcessor func(ctx context.Context, step *loop.Step, st state.State, prov provider.Provider) (state.State, error)

// ManagerOption configures a Manager.
type ManagerOption func(*Manager)

// ErrSessionBusy is returned when a stream is already processing a turn.
var ErrSessionBusy = errors.New("session is busy processing another turn")

// Manager owns the Thread↔Step binding and acts as a factory/registry for
// Stream handles.
type Manager struct {
	store     thread.Store
	provider  provider.Provider
	newStep   func() *loop.Step
	processor TurnProcessor
	sessions  map[string]*Stream
	mu        sync.RWMutex
}

// NewManager creates a new Manager with the given dependencies.
func NewManager(store thread.Store, prov provider.Provider, newStep func() *loop.Step, processor TurnProcessor, opts ...ManagerOption) *Manager {
	m := &Manager{
		store:     store,
		provider:  prov,
		newStep:   newStep,
		processor: processor,
		sessions:  make(map[string]*Stream),
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Create creates a new thread and an active stream backed by it.
func (m *Manager) Create() (*Stream, error) {
	thr, err := m.store.Create()
	if err != nil {
		return nil, fmt.Errorf("create thread: %w", err)
	}
	step := m.newStep()
	stream := &Stream{
		id:        thr.ID,
		thread:    thr,
		step:      step,
		provider:  m.provider,
		processor: m.processor,
		store:     m.store,
	}

	m.mu.Lock()
	m.sessions[thr.ID] = stream
	m.mu.Unlock()

	return stream, nil
}

// Attach gets or creates an active stream for an existing thread.
// If the thread does not exist in the store, an error is returned.
func (m *Manager) Attach(threadID string) (*Stream, error) {
	m.mu.RLock()
	stream, ok := m.sessions[threadID]
	m.mu.RUnlock()
	if ok {
		return stream, nil
	}

	thr, ok := m.store.Get(threadID)
	if !ok {
		return nil, fmt.Errorf("thread %s not found", threadID)
	}

	step := m.newStep()
	stream = &Stream{
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
	m.sessions[threadID] = stream
	m.mu.Unlock()

	return stream, nil
}

// Close closes a stream and removes it from the active map.
// The underlying thread is NOT deleted from the store.
func (m *Manager) Close(sessionID string) error {
	m.mu.Lock()
	stream, ok := m.sessions[sessionID]
	delete(m.sessions, sessionID)
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}
	return stream.Close()
}

// Store returns the underlying thread.Store.
func (m *Manager) Store() thread.Store {
	return m.store
}

// List returns handles for all active streams.
func (m *Manager) List() []*Stream {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*Stream, 0, len(m.sessions))
	for _, stream := range m.sessions {
		result = append(result, stream)
	}
	return result
}

// Get returns the active Stream for a given session ID.
// An error is returned if the stream does not exist.
func (m *Manager) Get(sessionID string) (*Stream, error) {
	m.mu.RLock()
	stream, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	return stream, nil
}

// Check verifies that the stream exists and is not busy. It returns nil if
// the stream is ready to process.
//
// Errors:
//   - "session not found" if the stream does not exist
//   - ErrSessionBusy if the stream is already processing a turn
func (m *Manager) Check(sessionID string) error {
	m.mu.RLock()
	stream, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}
	stream.mu.Lock()
	busy := stream.busy
	stream.mu.Unlock()
	if busy {
		return ErrSessionBusy
	}
	return nil
}
