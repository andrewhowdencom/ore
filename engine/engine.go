// Package engine is the process-wide execution boundary for ore
// sessions. It accepts session events, serializes them per session via
// a per-session mailbox, and drives the agent for each dequeued event.
//
// # Why engine exists
//
// Multiple conduits (TUI, HTTP, Slack, …) may target the same session.
// Without a shared execution boundary, each conduit would have to
// serialize its own events, and two conduits could submit events
// concurrently against the same session, racing on the underlying
// ledger. Engine owns the mailbox so that any number of conduits can
// submit to a session and events are processed in arrival order by a
// single worker per session.
//
// # Public surface
//
// Engine is constructed with a session.Registry (so it can resolve
// session IDs to *session.Session) and an agent.Factory (so it can
// construct an agent from current session metadata at execution time).
// The two public operations are Submit (enqueue an event for a
// session) and Close (drain active mailboxes and refuse further
// submissions). The engine's public API is intentionally minimal: no
// session listing, no queue depth inspection, no per-session options.
// Anything beyond this lives elsewhere (session.Registry for lookup,
// session itself for thread access).
//
// # Per-session mailbox
//
// Engine maintains one mailbox per active session. Each mailbox has a
// bounded FIFO (default 64) and one worker goroutine that drains it
// serially. Concurrent submissions to the same session are
// serialized by the mailbox. Different sessions execute concurrently
// because their mailboxes and workers are independent.
//
// # Event lifecycle
//
// For each dequeued event, the worker:
//
//  1. (UserMessageEvent only) records the user turn via
//     session.Session.Submit, which emits a TurnCompleteEvent and
//     auto-appends to the session's thread. (InterruptEvent is not
//     submitted; the engine treats it as a "skip inference" signal
//     so that queued events are not discarded but no extra turn is
//     added.)
//
//  2. Builds an agent via the factory using the session's current
//     metadata, binding the session's step via agent.WithStep so that
//     every artifact the pattern emits reaches session.Subscribe.
//
//  3. Runs the agent against the session's thread. The agent's
//     pattern drives Step.Turn, which emits the assistant turn.
//
//  4. Publishes a LifecycleEvent{Phase: "done"} on completion,
//     regardless of outcome (success, error, or interrupt).
//
// # Failure isolation
//
// A failing event emits an ErrorEvent and a LifecycleEvent{Phase:
// "done"}, then the worker continues with the next queued event. The
// queue is not poisoned; one bad turn does not halt the session.
//
// # Cancellation
//
// session.InterruptEvent causes the engine to skip inference for that
// event. Pending events remain queued. The engine's Close drains
// active operations and refuses further submissions.
package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/andrewhowdencom/ore/agent"
	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/ledger"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/session"
)

// defaultQueueSize is the per-session mailbox capacity. It is generous
// enough for typical interactive chat (the user can type a few messages
// ahead while the model is still producing) and small enough to
// bound memory under sustained load.
const defaultQueueSize = 64

// ErrQueueFull is returned by Submit when the per-session mailbox is at
// capacity. Callers detect this with errors.Is.
var ErrQueueFull = errors.New("engine: session queue is full")

// ErrClosed is returned by Submit when the engine has been Closed and
// is no longer accepting new work.
var ErrClosed = errors.New("engine: closed")

// ErrSessionNotFound is returned by Submit when the session ID does not
// resolve via the configured session.Registry.
var ErrSessionNotFound = errors.New("engine: session not found")

// Option configures an Engine.
type Option func(*Engine)

// WithQueueSize overrides the default per-session mailbox capacity.
// The size is per-session; a value of 0 (the default) uses
// defaultQueueSize. Negative values are clamped to 0.
func WithQueueSize(n int) Option {
	return func(e *Engine) {
		if n < 0 {
			n = 0
		}
		e.queueSize = n
	}
}

// Engine owns per-session event serialization. See the package doc.
type Engine struct {
	registry session.Registry
	factory  agent.Factory

	queueSize int

	mu        sync.Mutex
	mailboxes map[string]*mailbox
	closed    bool
}

// New constructs an Engine with the given registry and factory. The
// factory is invoked per dequeued event; the registry is consulted per
// submission. The engine is safe to use from multiple goroutines.
func New(registry session.Registry, factory agent.Factory, opts ...Option) (*Engine, error) {
	if registry == nil {
		return nil, fmt.Errorf("engine.New: nil registry")
	}
	if factory == nil {
		return nil, fmt.Errorf("engine.New: nil factory")
	}
	e := &Engine{
		registry:  registry,
		factory:   factory,
		queueSize: defaultQueueSize,
		mailboxes: make(map[string]*mailbox),
	}
	for _, opt := range opts {
		opt(e)
	}
	if e.queueSize <= 0 {
		e.queueSize = defaultQueueSize
	}
	return e, nil
}

// mailbox is the per-session bounded queue + worker goroutine.
type mailbox struct {
	// inflightMu serialises the cancel func to avoid a race
	// between handleEvent (which writes) and Close (which reads).
	inflightMu sync.Mutex
	inflight   context.CancelFunc

	ch   chan session.Event
	stop chan struct{}
	done chan struct{}
}

// setInflight records the cancel func for the active execution. The
// caller must invoke clearInflight on the same goroutine when the
// execution completes. Close reads the cancel func; the mutex makes
// that read race-free.
func (m *mailbox) setInflight(cancel context.CancelFunc) {
	m.inflightMu.Lock()
	m.inflight = cancel
	m.inflightMu.Unlock()
}

// clearInflight drops the recorded cancel func.
func (m *mailbox) clearInflight() {
	m.inflightMu.Lock()
	m.inflight = nil
	m.inflightMu.Unlock()
}

// inflight returns the recorded cancel func, or nil if none.
func (m *mailbox) getInflight() context.CancelFunc {
	m.inflightMu.Lock()
	defer m.inflightMu.Unlock()
	return m.inflight
}

// Submit accepts an event for the given session. The event is
// enqueued in that session's mailbox. Submission returns once the
// event has been admitted or rejected. It does NOT block on inference
// completion.
//
// Submission can fail for three reasons:
//
//   - ErrSessionNotFound when the session ID does not resolve.
//   - ErrQueueFull when the per-session mailbox is at capacity.
//   - ErrClosed when the engine has been Closed.
//
// To cancel the active operation in a session, submit a
// session.InterruptEvent; that does not discard pending events.
func (e *Engine) Submit(ctx context.Context, sessionID string, event session.Event) error {
	if event == nil {
		return fmt.Errorf("engine.Submit: nil event")
	}

	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return ErrClosed
	}

	sess, err := e.registry.Get(sessionID)
	if err != nil {
		e.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrSessionNotFound, sessionID)
	}

	mb, ok := e.mailboxes[sessionID]
	if !ok {
		mb = e.newMailbox(sess)
		e.mailboxes[sessionID] = mb
	}
	e.mu.Unlock()

	select {
	case mb.ch <- event:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return fmt.Errorf("%w (session %q, cap=%d)", ErrQueueFull, sessionID, cap(mb.ch))
	}
}

// newMailbox constructs the per-session mailbox and starts its worker.
// Caller must hold e.mu.
func (e *Engine) newMailbox(sess *session.Session) *mailbox {
	mb := &mailbox{
		ch:   make(chan session.Event, e.queueSize),
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
	go e.runMailbox(sess, mb)
	return mb
}

// runMailbox is the per-session worker loop. It exits when mb.stop is
// closed (during Close) or when the session's step closes (the
// upstream signal that the session is no longer usable).
func (e *Engine) runMailbox(sess *session.Session, mb *mailbox) {
	defer close(mb.done)

	for {
		select {
		case <-mb.stop:
			return
		case event, ok := <-mb.ch:
			if !ok {
				return
			}
			e.handleEvent(sess, mb, event)
		}
	}
}

// handleEvent is invoked serially by the worker. It runs a single
// event against the session and publishes lifecycle events. It does
// not return until the agent has completed (or failed).
//
// Lifecycle contract: a single LifecycleEvent{Phase: "done"} is
// emitted on every successful or failed path. Interrupt events skip
// inference but still emit the lifecycle marker so subscribers
// observe a clean end-of-handling.
func (e *Engine) handleEvent(sess *session.Session, mb *mailbox, event session.Event) {
	ctx, cancel := context.WithCancel(context.Background())
	mb.setInflight(cancel)
	defer func() {
		cancel()
		mb.clearInflight()
	}()

	// Snapshot the session's metadata once per event and bind it to
	// the per-event context. The same ctx flows into sess.Submit (the
	// user turn) and ag.Run (the assistant turn, which calls
	// Step.Turn), so both Step.Submit and Step.Turn spans carry the
	// same session.<key> attributes. Mid-event SetMetadata calls
	// (e.g. a slash command handler) are intentionally NOT reflected
	// on subsequent turns within this event — snapshot semantics.
	ctx = loop.WithSpanAttributes(ctx, sess.Attributes()...)

	eventCtx := event.Context()

	// 1. Record the user turn, if applicable. The Submit method
	//    emits a TurnCompleteEvent and auto-appends to the session's
	//    thread (because the session's step is bound to it via
	//    WithState).
	switch e := event.(type) {
	case session.UserMessageEvent:
		if _, err := sess.Submit(ctx, ledger.RoleUser, artifact.Text{Content: e.Content}); err != nil {
			sess.Emitter().Emit(ctx, loop.ErrorEvent{Err: fmt.Errorf("submit user turn: %w", err), Ctx: eventCtx})
			sess.Emitter().Emit(ctx, loop.LifecycleEvent{Phase: "done", Ctx: eventCtx})
			return
		}
	case session.InterruptEvent:
		// Interrupt: skip inference. Pending events remain queued.
		// The active operation (the previous event in this mailbox)
		// has already completed; the cancel() on the active event
		// context is a no-op for that case. We emit just "done"
		// so subscribers observe a clean end-of-handling.
		sess.Emitter().Emit(ctx, loop.LifecycleEvent{Phase: "done", Ctx: eventCtx})
		return
	}

	// 2. Build the agent for this turn, binding the session's step
	//    so artifacts the pattern emits reach session.Subscribe.
	ag, err := e.factory.Build(sess)
	if err != nil {
		sess.Emitter().Emit(ctx, loop.ErrorEvent{Err: fmt.Errorf("agent factory: %w", err), Ctx: eventCtx})
		sess.Emitter().Emit(ctx, loop.LifecycleEvent{Phase: "done", Ctx: eventCtx})
		return
	}

	// 3. Run the agent. Errors are surfaced via ErrorEvent; success
	//    is silent (the pattern's emissions are already on the
	//    session's event stream).
	if _, err := ag.Run(ctx, sess.Thread()); err != nil {
		sess.Emitter().Emit(ctx, loop.ErrorEvent{Err: err, Ctx: eventCtx})
	}

	// 4. Always publish the terminal lifecycle event.
	sess.Emitter().Emit(ctx, loop.LifecycleEvent{Phase: "done", Ctx: eventCtx})
}

// Close stops accepting submissions, cancels any in-flight execution
// in every active mailbox, and waits for mailbox workers to drain.
// After Close returns, the engine is unusable; subsequent Submit
// calls return ErrClosed.
func (e *Engine) Close(ctx context.Context) error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil
	}
	e.closed = true
	mailboxes := make([]*mailbox, 0, len(e.mailboxes))
	for _, mb := range e.mailboxes {
		mailboxes = append(mailboxes, mb)
	}
	e.mailboxes = nil
	e.mu.Unlock()

	// Cancel any in-flight execution. Each mailbox's worker will
	// observe the cancel via the active event's context.
	for _, mb := range mailboxes {
		if c := mb.getInflight(); c != nil {
			c()
		}
	}

	// Signal workers to stop. The worker exits after the current
	// event completes (which the cancel above short-circuits).
	for _, mb := range mailboxes {
		close(mb.stop)
	}

	// Wait for workers to drain, or for ctx to fire.
	for _, mb := range mailboxes {
		select {
		case <-mb.done:
		case <-ctx.Done():
			slog.Warn("engine.Close timed out waiting for mailbox worker", "err", ctx.Err())
			return ctx.Err()
		}
	}
	return nil
}
