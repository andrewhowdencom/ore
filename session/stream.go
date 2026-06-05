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
)

// Stream is a per-session primitive that owns the loop.Step, Thread,
// TurnProcessor, and provider for a single active conversation. It provides
// ingress (Process, Submit) and egress (Subscribe) for the session, plus
// lifecycle controls (Cancel, Close).
//
// Events submitted via Submit() are enqueued in an unbounded FIFO queue
// and processed serially by a single internal worker goroutine. Process()
// also enqueues but blocks until the event has been fully processed.
type Stream struct {
	id          string
	thread      *Thread
	step        *loop.Step
	provider    provider.Provider
	processor   TurnProcessor
	store        Store
	interceptor  Interceptor
	mu           sync.Mutex
	cancel       context.CancelFunc
	closed       bool
	forwardOnce  sync.Once

	// queue is an unbounded FIFO of events waiting to be processed.
	queue      []queuedEvent
	queueCond  *sync.Cond
	workerOnce sync.Once
	workerWG   sync.WaitGroup
}

// queuedEvent wraps an Event with the caller's context and an optional
// completion channel. If done is non-nil, the worker signals the final
// error on it after processing.
type queuedEvent struct {
	event Event
	ctx   context.Context
	done  chan error
}

// Submit enqueues the event in the stream's unbounded FIFO queue and
// returns immediately. A single internal worker goroutine drains the
// queue and processes events serially, one at a time.
//
// InterruptEvent clears all pending events from the queue before being
// enqueued itself, and cancels any in-flight turn via Cancel().
//
// Errors:
//   - "session %s is closed" if the stream has been closed
func (s *Stream) Submit(event Event) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("session %s is closed", s.id)
	}
	if s.queueCond == nil {
		s.queueCond = sync.NewCond(&s.mu)
	}
	s.mu.Unlock()

	s.workerOnce.Do(func() {
		go s.worker()
	})

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("session %s is closed", s.id)
	}
	if _, ok := event.(InterruptEvent); ok {
		s.queue = s.queue[:0]
	}
	s.queue = append(s.queue, queuedEvent{event: event, ctx: context.Background()})
	s.mu.Unlock()
	s.queueCond.Signal()

	if _, ok := event.(InterruptEvent); ok {
		_ = s.Cancel()
	}

	return nil
}

// Process enqueues the event and blocks until the worker has finished
// processing it. Context cancellation aborts the waiting, not the
// in-flight turn; use Cancel() to abort a running turn.
//
// After the TurnProcessor returns (including all tool-call loops),
// Process emits a LifecycleEvent{Phase: "done"} to signal pipeline
// completion before performing save cleanup. Subscribers can use
// this event for lifecycle signalling (audio notifications, UI state
// finalization).
//
// Errors:
//   - "session %s is closed" if the stream has been closed
//   - "unsupported event kind" for unknown event types
//   - "process event: ..." wrapping any TurnProcessor or save error
func (s *Stream) Process(ctx context.Context, event Event) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("session %s is closed", s.id)
	}
	if s.queueCond == nil {
		s.queueCond = sync.NewCond(&s.mu)
	}
	s.mu.Unlock()

	s.workerOnce.Do(func() {
		go s.worker()
	})

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("session %s is closed", s.id)
	}
	if _, ok := event.(InterruptEvent); ok {
		s.queue = s.queue[:0]
	}
	done := make(chan error, 1)
	s.queue = append(s.queue, queuedEvent{event: event, ctx: ctx, done: done})
	s.mu.Unlock()
	s.queueCond.Signal()

	if _, ok := event.(InterruptEvent); ok {
		_ = s.Cancel()
	}

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// processOne runs the full inference pipeline for a single event.
// It is called by the worker goroutine and must not be called
// concurrently for the same Stream.
func (s *Stream) processOne(ctx context.Context, event Event) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("session %s is closed", s.id)
	}
	turnCtx, cancel := context.WithCancel(ctx)
	turnCtx = loop.WithThreadID(turnCtx, s.id)
	s.cancel = cancel
	s.mu.Unlock()

	// Run interceptor if configured.
	if s.interceptor != nil {
		if _, ok := event.(UserMessageEvent); ok {
			newEvent, consumed, err := s.interceptor.Intercept(ctx, event)
			if err != nil {
				return fmt.Errorf("interceptor: %w", err)
			}
			if consumed {
				return nil
			}
			event = newEvent
		}
	}

	var runErr error
	var eventCtx context.Context
	switch e := event.(type) {
	case UserMessageEvent:
		eventCtx = e.Context()
		s.step.SetEventContext(e.Context())
		defer s.step.SetEventContext(context.Background())
		_, runErr = s.step.Submit(turnCtx, s.thread.State, state.RoleUser, artifact.Text{Content: e.Content})
		if runErr == nil {
			_, runErr = s.processor(turnCtx, s.step, s.thread.State, s.provider)
		}
	case InterruptEvent:
		// Interrupt is handled by cancelling the ongoing turn context.
		// No inference is started for an interrupt event itself.
		eventCtx = e.Context()
		s.step.SetEventContext(e.Context())
		defer s.step.SetEventContext(context.Background())
		cancel()
	case SessionSwitchEvent:
		// SessionSwitchEvent is a meta-event emitted by slash handlers to
		// signal cross-session navigation. It does not trigger inference.
		eventCtx = e.Context()
		s.step.Emit(context.Background(), e)
	default:
		runErr = fmt.Errorf("unsupported event kind: %s", event.Kind())
	}

	// Save thread state regardless of run outcome.
	if saveErr := s.store.Save(s.thread); saveErr != nil && runErr == nil {
		runErr = fmt.Errorf("save thread: %w", saveErr)
	}

	if runErr != nil {
		if errors.Is(runErr, context.Canceled) {
			s.step.Emit(context.Background(), loop.LifecycleEvent{Phase: "cancelled", Ctx: eventCtx})
		} else {
			s.step.Emit(context.Background(), loop.ErrorEvent{Err: runErr, Ctx: eventCtx})
		}
	}

	// Emit LifecycleEvent to signal pipeline completion.
	s.step.Emit(context.Background(), loop.LifecycleEvent{Phase: "done", Ctx: eventCtx})

	// Cleanup.
	s.mu.Lock()
	s.cancel = nil
	s.mu.Unlock()
	cancel()

	if runErr != nil {
		return fmt.Errorf("process event: %w", runErr)
	}
	return nil
}

// worker is the single goroutine that drains the event queue and
// processes each event serially via processOne.
func (s *Stream) worker() {
	s.workerWG.Add(1)
	defer s.workerWG.Done()
	for {
		s.mu.Lock()
		for len(s.queue) == 0 && !s.closed {
			s.queueCond.Wait()
		}
		if s.closed {
			// Drain remaining queue items and signal errors.
			for _, qe := range s.queue {
				if qe.done != nil {
					select {
					case qe.done <- fmt.Errorf("session %s is closed", s.id):
					default:
					}
				}
			}
			s.mu.Unlock()
			return
		}
		qe := s.queue[0]
		s.queue = s.queue[1:]
		s.mu.Unlock()

		err := s.processOne(qe.ctx, qe.event)
		if qe.done != nil {
			select {
			case qe.done <- err:
			default:
			}
		}
	}
}

// Cancel aborts an ongoing turn by cancelling its context.
func (s *Stream) Cancel() error {
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

// Subscribe returns a filtered output event channel for the stream's
// loop.Step FanOut. If no kinds are provided, the channel receives all
// events regardless of kind. If the stream is closed, the returned channel
// is immediately closed.
//
// The returned channel is closed when the session is closed.
// Callers should range over the channel and handle closure:
//
//	ch := stream.Subscribe("text_delta", "turn_complete")
//	for event := range ch {
//	    // process event
//	}
func (s *Stream) Subscribe(kinds ...string) <-chan loop.OutputEvent {
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

// Emit injects a custom output event into the stream's FanOut, allowing
// handlers, interceptors, and application logic to emit meta-events that
// are delivered to all subscribers alongside standard artifact and
// turn-complete events.
//
// The stream must not be closed.
//
// Errors:
//   - "session %s is closed" if the stream has been closed
func (s *Stream) Emit(ctx context.Context, event loop.OutputEvent) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("session %s is closed", s.id)
	}
	s.mu.Unlock()
	s.step.Emit(ctx, event)
	return nil
}

// ID returns the stream's unique identifier (same as the thread ID).
func (s *Stream) ID() string { return s.id }

// Turns returns a defensive (shallow) copy of the thread's turn history.
// The slice of Turns is copied, but each Turn's Artifacts slice is shared.
// Callers should treat the returned artifacts as immutable.
func (s *Stream) Turns() []state.Turn {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.thread.State.Turns()
}

// GetMetadata retrieves a metadata value from the underlying thread.
func (s *Stream) GetMetadata(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.thread.Metadata[key]
	return v, ok
}

// SetMetadata sets a metadata value on the underlying thread.
func (s *Stream) SetMetadata(key, value string) {
	s.mu.Lock()
	s.thread.Metadata[key] = value
	s.mu.Unlock()
	_ = s.Emit(context.Background(), loop.PropertiesEvent{
		Properties: map[string]string{key: value},
		Ctx:        loop.WithProvenance(context.Background(), "app"),
	})
}

// Save persists the underlying thread to the store.
func (s *Stream) Save() error {
	return s.store.Save(s.thread)
}

// Close closes the stream's Step and marks it as closed.
// The underlying thread is NOT deleted from the store.
//
// Close cancels any in-flight turn, waits for the worker goroutine to
// exit, and then closes the step. This ensures all pending Process
// calls receive errors and no goroutine leak occurs.
func (s *Stream) Close() error {
	s.mu.Lock()
	s.closed = true
	if s.cancel != nil {
		s.cancel()
	}
	cond := s.queueCond
	s.mu.Unlock()
	if cond != nil {
		cond.Broadcast()
	}
	s.workerWG.Wait()
	if s.step != nil {
		_ = s.step.Close()
	}
	return nil
}
