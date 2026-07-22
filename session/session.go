package session

import (
	"context"
	"sync"

	"github.com/andrewhowdencom/ore/ledger"
	"github.com/andrewhowdencom/ore/loop"
)

// Session is the per-conversation primitive. It owns the identity, the
// ledger thread, the conduit-mapping metadata, and the long-lived
// loop.Step used for subscriber fanout.
//
// Session.mu serializes metadata access. Subscribe and Close delegate
// to loop.Step, which manages its own concurrency. Event submission and
// inference execution are the caller's responsibility; see
// session.Runner for the canonical inference-driving path.
type Session struct {
	id       string
	thread   *ledger.Thread
	metadata map[string]string
	step     *loop.Step

	mu sync.Mutex
}

// Option configures a Session at construction.
type Option func(*Session)

// New constructs a Session with the given identity and thread. The
// thread must not be nil. The session is fully initialized with an
// internal loop.Step for subscriber fanout.
//
// Event submission and inference execution are the caller's
// responsibility; see session.Runner for the canonical inference
// path. The Option type is reserved for follow-up plans that wire
// inference-governing metadata onto the session.
func New(id string, thread *ledger.Thread, opts ...Option) *Session {
	if thread == nil {
		panic("session.New: thread must not be nil")
	}
	s := &Session{
		id:       id,
		thread:   thread,
		metadata: make(map[string]string),
		step:     loop.New(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// ID returns the session's unique identifier.
func (s *Session) ID() string { return s.id }

// Thread returns the session's ledger.Thread. The returned pointer is
// the same one passed to New; the session does not copy.
func (s *Session) Thread() *ledger.Thread { return s.thread }

// Turns returns the active-path projection of the session's tree, as
// a defensive copy.
func (s *Session) Turns() []ledger.Turn {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.thread.Turns()
}

// GetMetadata retrieves a metadata value.
func (s *Session) GetMetadata(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.metadata[key]
	return v, ok
}

// SetMetadata sets a metadata value and emits a PropertiesEvent so
// subscribers (TUI status bar, HTTP web UI, etc.) react to the change.
// The event is emitted via the session's loop.Step FanOut.
func (s *Session) SetMetadata(key, value string) {
	s.mu.Lock()
	if s.metadata == nil {
		s.metadata = make(map[string]string)
	}
	s.metadata[key] = value
	s.mu.Unlock()
	s.step.Emit(context.Background(), loop.PropertiesEvent{
		Operations: []loop.PropertyOperation{
			{Op: loop.PropertyOpSet, Key: key, Value: value},
		},
		Ctx: loop.WithProvenance(context.Background(), "app"),
	})
}

// AllMetadata returns a defensive copy of the metadata map. Mutating
// the returned map does not affect the session's state.
func (s *Session) AllMetadata() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]string, len(s.metadata))
	for k, v := range s.metadata {
		out[k] = v
	}
	return out
}

// Subscribe returns a filtered output event channel from the session's
// loop.Step FanOut. If no kinds are provided, the channel receives all
// events regardless of kind. If the session is closed, the returned
// channel is immediately closed.
//
// Subscribe is live-only: it delivers events from the point of
// subscription onward and does not replay historical events.
func (s *Session) Subscribe(kinds ...string) <-chan loop.OutputEvent {
	return s.step.Subscribe(kinds...)
}

// Emitter returns a loop.Emitter that publishes events through the
// session's step. Interceptors (slash commands, etc.) receive this
// in their Intercept call to publish events (e.g. PropertiesEvent)
// during pre-LLM processing.
func (s *Session) Emitter() loop.Emitter { return s.step }

// Close closes the session's loop.Step. After Close returns, Subscribe
// returns an immediately-closed channel (loop.Step's FanOut propagates
// closure to subscribers). Close is safe to call multiple times: step
// closure is sync.Once-guarded inside loop.FanOut.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.step != nil {
		_ = s.step.Close()
	}
	return nil
}