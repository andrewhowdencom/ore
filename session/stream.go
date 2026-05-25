package session

import (
	"context"
	"fmt"
	"sync"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/state"
	"github.com/andrewhowdencom/ore/thread"
)

// Stream is a per-session primitive that owns the loop.Step, thread.Thread,
// TurnProcessor, and provider for a single active conversation. It provides
// ingress (Process) and egress (Subscribe) for the session, plus lifecycle
// controls (Cancel, Close).
type Stream struct {
	id          string
	thread      *thread.Thread
	step        *loop.Step
	provider    provider.Provider
	processor   TurnProcessor
	store       thread.Store
	mu          sync.Mutex
	busy        bool
	cancel      context.CancelFunc
	closed      bool
	forwardOnce sync.Once
}

// Process submits the event to the stream's state and runs the inference
// pipeline. The stream must not be busy. Context cancellation aborts the
// running TurnProcessor.
//
// After the TurnProcessor returns (including all tool-call loops),
// Process emits a ProcessCompleteEvent carrying the final error state
// before performing save cleanup. Subscribers can use this event for
// lifecycle signalling (audio notifications, UI state finalization).
//
// Errors:
//   - ErrSessionBusy if the stream is already processing a turn
//   - "unsupported event kind" for unknown event types
//   - "process event: ..." wrapping any TurnProcessor or save error
func (s *Stream) Process(ctx context.Context, event Event) error {
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
	var eventCtx loop.EventContext
	switch e := event.(type) {
	case UserMessageEvent:
		eventCtx = e.Context()
		s.step.SetEventContext(e.Context())
		defer s.step.SetEventContext(loop.EventContext{})
		_, runErr = s.step.Submit(turnCtx, s.thread.State, state.RoleUser, artifact.Text{Content: e.Content})
		if runErr == nil {
			_, runErr = s.processor(turnCtx, s.step, s.thread.State, s.provider)
		}
	case InterruptEvent:
		// Interrupt is handled by cancelling the ongoing turn context.
		// No inference is started for an interrupt event itself.
		eventCtx = e.Context()
		s.step.SetEventContext(e.Context())
		defer s.step.SetEventContext(loop.EventContext{})
		cancel()
	default:
		runErr = fmt.Errorf("unsupported event kind: %s", event.Kind())
	}

	// Save thread state regardless of run outcome.
	if saveErr := s.store.Save(s.thread); saveErr != nil && runErr == nil {
		runErr = fmt.Errorf("save thread: %w", saveErr)
	}

	// Emit ProcessCompleteEvent with the final error state (including save errors).
	s.step.Emit(ctx, loop.ProcessCompleteEvent{Err: runErr, Ctx: eventCtx})

	// Cleanup.
	s.mu.Lock()
	s.busy = false
	s.cancel = nil
	s.mu.Unlock()
	cancel()

	if runErr != nil {
		return fmt.Errorf("process event: %w", runErr)
	}
	return nil
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
// The stream must not be closed. Unlike Process(), Emit() does not check
// whether the stream is busy: handlers running during an active turn may
// need to emit events (e.g., session-switch signals from slash commands).
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

// Close closes the stream's Step and marks it as closed.
// The underlying thread is NOT deleted from the store.
func (s *Stream) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	if s.step != nil {
		_ = s.step.Close()
	}
	return nil
}
