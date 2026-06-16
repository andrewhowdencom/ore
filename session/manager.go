package session

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/models"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/state"
)

// TurnProcessor runs the full inference pipeline for a single turn after
// the user event has been submitted to state. It is called with the
// stream's loop.Step, state, provider, and a [models.Spec] derived
// from the session's metadata (or the empty zero value when no
// metadata is set, in which case the step's configured default
// spec applies).
type TurnProcessor func(ctx context.Context, step *loop.Step, st state.State, prov provider.Provider, spec models.Spec) (state.State, error)

// ManagerOption configures a Manager.
type ManagerOption func(*Manager)

// WithDefaultMetadata sets a function that provides default metadata for
// every stream at creation/attachment time. The function receives the
// stream so it can include the thread ID or other stream-specific values.
func WithDefaultMetadata(fn func(*Stream) map[string]string) ManagerOption {
	return func(m *Manager) {
		m.defaultMeta = fn
	}
}

// WithInterceptor sets an interceptor that processes UserMessageEvent events
// before they enter the LLM inference pipeline for every stream managed by
// this Manager.
func WithInterceptor(interceptor Interceptor) ManagerOption {
	return func(m *Manager) {
		m.interceptor = interceptor
	}
}

// SinkFunc receives OutputEvents from a specific stream, together with the
// originating stream ID. It may be invoked concurrently for multiple streams.
type SinkFunc func(streamID string, event loop.OutputEvent)

type sink struct {
	id    int64
	kinds map[string]struct{}
	fn    SinkFunc
}

func makeKindsSet(kinds []string) map[string]struct{} {
	if len(kinds) == 0 {
		return nil
	}
	kindSet := make(map[string]struct{}, len(kinds))
	for _, k := range kinds {
		kindSet[k] = struct{}{}
	}
	return kindSet
}

// Manager owns the Thread↔Step binding and acts as a factory/registry for
// Stream handles.
type Manager struct {
	store       Store
	provider    provider.Provider
	newStep     func(*Stream) ([]loop.Option, error)
	processor   TurnProcessor
	interceptor Interceptor
	sessions    map[string]*Stream
	mu          sync.RWMutex
	sinks       []sink
	sinksMu     sync.RWMutex
	sinkID      int64
	defaultMeta func(*Stream) map[string]string
}

// NewManager creates a new Manager with the given dependencies.
func NewManager(store Store, prov provider.Provider, newStep func(*Stream) ([]loop.Option, error), processor TurnProcessor, opts ...ManagerOption) *Manager {
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
// Returns an error if the underlying thread store cannot create a thread
// or if the step factory fails.
func (m *Manager) Create() (*Stream, error) {
	thr, err := m.store.Create()
	if err != nil {
		return nil, fmt.Errorf("create thread: %w", err)
	}
	stream := &Stream{
		id:          thr.ID,
		thread:      thr,
		provider:    m.provider,
		processor:   m.processor,
		store:       m.store,
		interceptor: m.interceptor,
	}
	factoryOpts, err := m.newStep(stream)
	if err != nil {
		return nil, fmt.Errorf("create step: %w", err)
	}
	defaultOnEmit := loop.WithOnEmit(func(ctx context.Context, event loop.OutputEvent) {
		if tc, ok := event.(loop.TurnCompleteEvent); ok {
			stream.mu.Lock()
			stream.thread.State.Append(tc.Turn.Role, tc.Turn.Artifacts...)
			stream.mu.Unlock()
		}
	})
	stream.step = loop.New(append([]loop.Option{defaultOnEmit}, factoryOpts...)...)

	m.mu.Lock()
	m.sessions[thr.ID] = stream
	m.mu.Unlock()

	m.startSinkForwarding(stream)
	m.applyDefaultMetadata(stream)

	return stream, nil
}

// Attach gets or creates an active stream for an existing thread.
// If the thread does not exist in the store, an error is returned.
// May also return an error if the step factory fails.
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

	stream = &Stream{
		id:          threadID,
		thread:      thr,
		provider:    m.provider,
		processor:   m.processor,
		store:       m.store,
		interceptor: m.interceptor,
	}
	factoryOpts, err := m.newStep(stream)
	if err != nil {
		return nil, fmt.Errorf("create step: %w", err)
	}
	defaultOnEmit := loop.WithOnEmit(func(ctx context.Context, event loop.OutputEvent) {
		if tc, ok := event.(loop.TurnCompleteEvent); ok {
			stream.mu.Lock()
			stream.thread.State.Append(tc.Turn.Role, tc.Turn.Artifacts...)
			stream.mu.Unlock()
		}
	})
	stream.step = loop.New(append([]loop.Option{defaultOnEmit}, factoryOpts...)...)

	m.mu.Lock()
	if existing, ok := m.sessions[threadID]; ok {
		m.mu.Unlock()
		return existing, nil
	}
	m.sessions[threadID] = stream
	m.mu.Unlock()

	m.startSinkForwarding(stream)
	m.applyDefaultMetadata(stream)

	return stream, nil
}

func (m *Manager) applyDefaultMetadata(stream *Stream) {
	if m.defaultMeta == nil {
		return
	}
	for k, v := range m.defaultMeta(stream) {
		stream.SetMetadata(k, v)
	}
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

// GetBy retrieves a thread by a metadata key-value pair.
func (m *Manager) GetBy(key, value string) (*Thread, bool) {
	return m.store.GetBy(key, value)
}

// GetThread retrieves a thread by ID.
func (m *Manager) GetThread(id string) (*Thread, bool) {
	return m.store.Get(id)
}

// ListThreads returns all stored threads.
func (m *Manager) ListThreads() ([]*Thread, error) {
	return m.store.List()
}

// CreateWithID creates a new thread with the given ID and an active stream backed by it.
func (m *Manager) CreateWithID(id string) (*Stream, error) {
	thr := &Thread{
		ID:        id,
		State:     &state.Buffer{},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Metadata:  make(map[string]string),
	}
	if err := m.store.Save(thr); err != nil {
		return nil, fmt.Errorf("save thread: %w", err)
	}
	stream := &Stream{
		id:          thr.ID,
		thread:      thr,
		provider:    m.provider,
		processor:   m.processor,
		store:       m.store,
		interceptor: m.interceptor,
	}
	factoryOpts, err := m.newStep(stream)
	if err != nil {
		return nil, fmt.Errorf("create step: %w", err)
	}
	defaultOnEmit := loop.WithOnEmit(func(ctx context.Context, event loop.OutputEvent) {
		if tc, ok := event.(loop.TurnCompleteEvent); ok {
			stream.mu.Lock()
			stream.thread.State.Append(tc.Turn.Role, tc.Turn.Artifacts...)
			stream.mu.Unlock()
		}
	})
	stream.step = loop.New(append([]loop.Option{defaultOnEmit}, factoryOpts...)...)

	m.mu.Lock()
	m.sessions[thr.ID] = stream
	m.mu.Unlock()

	m.startSinkForwarding(stream)
	m.applyDefaultMetadata(stream)

	return stream, nil
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

// RegisterSink registers a callback that receives OutputEvents from all
// active and future streams matching the given kinds. An empty kinds slice
// means all event kinds. Registration also triggers forwarding for any
// streams that already exist at the time of registration.
//
// It returns a function that removes the sink from the manager. Calling
// the returned function more than once is safe and idempotent.
func (m *Manager) RegisterSink(kinds []string, fn SinkFunc) func() {
	m.sinksMu.Lock()
	id := m.sinkID
	m.sinkID++
	s := sink{
		id:    id,
		kinds: makeKindsSet(kinds),
		fn:    fn,
	}
	m.sinks = append(m.sinks, s)
	m.sinksMu.Unlock()

	// Start forwarding for all existing streams.
	m.mu.RLock()
	streams := make([]*Stream, 0, len(m.sessions))
	for _, stream := range m.sessions {
		streams = append(streams, stream)
	}
	m.mu.RUnlock()

	for _, stream := range streams {
		m.startSinkForwarding(stream)
	}

	return func() {
		m.sinksMu.Lock()
		defer m.sinksMu.Unlock()
		for i, existing := range m.sinks {
			if existing.id == id {
				m.sinks = append(m.sinks[:i], m.sinks[i+1:]...)
				return
			}
		}
	}
}

func callSink(fn SinkFunc, streamID string, event loop.OutputEvent) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("sink callback panicked", "recover", r)
		}
	}()
	fn(streamID, event)
}

func (m *Manager) startSinkForwarding(stream *Stream) {
	stream.forwardOnce.Do(func() {
		ch := stream.Subscribe()
		go func() {
			for event := range ch {
				m.sinksMu.RLock()
				sinks := make([]sink, len(m.sinks))
				copy(sinks, m.sinks)
				m.sinksMu.RUnlock()

				for _, s := range sinks {
					if s.kinds == nil {
						callSink(s.fn, stream.ID(), event)
						continue
					}
					if _, ok := s.kinds[event.Kind()]; ok {
						callSink(s.fn, stream.ID(), event)
					}
				}
			}
		}()
	})
}
