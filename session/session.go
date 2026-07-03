package session

import (
	"context"
	"fmt"
	"sync"

	"github.com/andrewhowdencom/ore/ledger"
	"github.com/andrewhowdencom/ore/loop"
)

// Session is the per-conversation primitive. It owns the identity, the
// ledger thread, the conduit-mapping metadata, the long-lived
// loop.Step used for subscriber fanout, and the work queue that
// serializes ingress events.
//
// Session is not safe for concurrent closure. Submit, Subscribe,
// metadata access, and close are all serialized via Session.mu. The
// internal worker goroutine is the only reader of the work queue.
type Session struct {
	id       string
	thread   *ledger.Thread
	metadata map[string]string
	step     *loop.Step
	queue    *workQueue

	mu     sync.Mutex
	closed bool
	cancel context.CancelFunc
}

// Option configures a Session at construction.
type Option func(*Session)

// New constructs a Session with the given identity and thread. The
// thread must not be nil. The session is fully initialized: an internal
// loop.Step is created for fanout, and a worker goroutine is started
// to drain the work queue.
//
// Future tasks will wire agent invocation into the worker. For now,
// the worker is a stub that emits a LifecycleEvent{Phase: "done"}
// after processing each event.
func New(id string, thread *ledger.Thread, opts ...Option) *Session {
	if thread == nil {
		panic("session.New: thread must not be nil")
	}
	s := &Session{
		id:       id,
		thread:   thread,
		metadata: make(map[string]string),
		step:     loop.New(),
		queue:    newWorkQueue(),
	}
	for _, opt := range opts {
		opt(s)
	}
	go s.queue.runWorker(s.processEvent)
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
		Properties: map[string]string{key: value},
		Ctx:        loop.WithProvenance(context.Background(), "app"),
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

// Run enqueues an event for processing by the session's worker. It
// returns immediately; processing happens asynchronously. If the
// session is closed, Run returns errSessionClosed.
func (s *Session) Run(ctx context.Context, evt Event) error {
	if evt == nil {
		return fmt.Errorf("session %s: nil event", s.id)
	}
	return s.queue.submit(ctx, evt)
}

// Subscribe returns a filtered output event channel from the session's
// loop.Step FanOut. If no kinds are provided, the channel receives all
// events regardless of kind. If the session is closed, the returned
// channel is immediately closed.
//
// Subscribe is live-only: it delivers events from the point of
// subscription onward and does not replay historical events.
func (s *Session) Subscribe(kinds ...string) <-chan loop.OutputEvent {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		ch := make(chan loop.OutputEvent)
		close(ch)
		return ch
	}
	s.mu.Unlock()
	return s.step.Subscribe(kinds...)
}

// processEvent is the worker's per-event handler. In Task 2 this is a
// stub that emits LifecycleEvent{Phase: "done"} after the event has
// been acknowledged. Future tasks will wire agent invocation here.
func (s *Session) processEvent(ctx context.Context, evt Event) error {
	s.step.Emit(ctx, loop.LifecycleEvent{Phase: "done", Ctx: evt.Context()})
	return nil
}

// Close closes the session's step, drains the worker, and marks the
// session as closed. Subsequent calls to Run return errSessionClosed;
// Subscribe returns an immediately-closed channel. Close is safe to
// call multiple times.
func (s *Session) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()
	s.queue.close()
	if s.step != nil {
		_ = s.step.Close()
	}
	return nil
}