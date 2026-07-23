package junk

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/models"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/ledger"
)

// Compaction boundary metadata keys. The session package defines these
// directly to avoid an import cycle: x/compaction imports agent, and
// agent's runtime path imports session, so depending on x/compaction
// here would be a cycle. The values must match the constants in
// x/compaction:
//
//	MetaKeyBoundaryIndex = "ore.compaction.boundary.index"
//	MetaKeyBoundaryInfo  = "ore.compaction.boundary.info"
//
// Any drift between these and x/compaction's constants is caught
// by TestStream_MarkBoundary_RoundTrip which exercises the
// end-to-end Save/Load via the in-memory store.
const (
	boundaryKeyIndex = "ore.compaction.boundary.index"
	boundaryKeyInfo  = "ore.compaction.boundary.info"
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
	store       Store
	mu          sync.Mutex
	cancel      context.CancelFunc
	closed      bool
	forwardOnce sync.Once

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

	var runErr error
	var eventCtx context.Context
	switch e := event.(type) {
	case UserMessageEvent:
		eventCtx = e.Context()
		s.step.SetEventContext(e.Context())
		defer s.step.SetEventContext(context.Background())
		_, runErr = s.step.Submit(turnCtx, s.thread.State, ledger.RoleUser, artifact.Text{Content: e.Content})
		if runErr == nil {
			// Derive the per-turn Spec from session metadata. The
			// step's configured default is used when no
			// metadata-driven spec is present; an empty
			// models.Spec tells the step to use that default.
			spec, _ := s.Spec()
			_, runErr = s.processor(turnCtx, s.step, s.thread.State, s.provider, spec)
		}
	case InterruptEvent:
		// Interrupt is handled by cancelling the ongoing turn context.
		// No inference is started for an interrupt event itself.
		eventCtx = e.Context()
		s.step.SetEventContext(e.Context())
		defer s.step.SetEventContext(context.Background())
		cancel()
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
// Subscribe is live-only: it delivers events from the point of subscription
// onward and does not replay historical events. Conduits that need historical
// state should fetch it via Turns() before subscribing, or load it via
// LoadTurns() after external mutations (e.g. compaction).
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
// handlers and application logic to emit meta-events that are delivered
// to all subscribers alongside standard artifact and turn-complete
// events.
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
func (s *Stream) Turns() []ledger.Turn {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.thread.State.Turns()
}

// State returns the thread's mutable conversation ledger. The handle
// is the same State the loop pipeline uses (via loop.WithState); reads
// observe the current turn history, and writes through the returned
// Meta propagate to subsequent reads.
//
// As with the rest of the State interface, the returned handle is not
// safe for concurrent use; the stream serializes access to its own
// turns and metadata, but the State object itself shares the
// Buffer's "serial pipeline only" contract.
func (s *Stream) State() ledger.State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.thread.State
}

// LoadTurns replaces the thread's turn state with the provided slice.
// It acquires the stream's mutex to ensure thread-safe state mutation.
//
// This is the legacy replacement path; the new Thread is tree-backed,
// so callers migrating from a linear Buffer-backed state should
// construct a fresh ledger.Thread with each turn's ParentID set
// explicitly.
func (s *Stream) LoadTurns(turns []ledger.Turn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Build a fresh Thread from the linear slice. Each turn's
	// ParentID is set to the previous turn's ID (or "" for the
	// first), preserving the linear chain.
	newThread := ledger.NewThread()
	var prevID string
	for i := range turns {
		turn := turns[i]
		turn.ParentID = prevID
		newThread.SaveTurn(&turn)
		prevID = turn.ID
	}
	if len(turns) > 0 {
		newThread.SetCurrentTip(prevID)
	}
	s.thread.State = newThread
}

// AppendTurn records a turn into the thread state and broadcasts a
// TurnCompleteEvent to all subscribers of the stream. It is the
// canonical entry point for external producers (compaction, future
// tool-side producers) that need to inject turns without going through
// step.Turn / step.Submit.
//
// Behavior:
//
//   - The TurnCompleteEvent is emitted via s.step.Emit. The stream's
//     default OnEmit (registered by the Manager) appends the turn to
//     thread.State synchronously, before the event reaches the async
//     FanOut. Subscribers see the event with the canonical state
//     already updated, so live consumers (TUI, telemetry) do not
//     need to call ReloadHistory to observe the new turn.
//   - The thread is NOT auto-saved. Callers that want the change to
//     persist to the store should call Save() explicitly, or rely on
//     the stream's normal save flow after the next Process() call.
//     This matches the rest of the Stream's contract: state mutations
//     are persisted by the inference pipeline, not by ad-hoc producers.
//
// Errors:
//
//   - "session %s is closed" if the stream has been closed.
func (s *Stream) AppendTurn(ctx context.Context, role ledger.Role, artifacts ...artifact.Artifact) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("session %s is closed", s.id)
	}
	s.mu.Unlock()

	turn := ledger.Turn{
		Role:      role,
		Artifacts: artifacts,
		Timestamp: time.Now(),
	}
	s.step.Emit(ctx, loop.TurnCompleteEvent{Turn: turn, Ctx: ctx})
	return nil
}

// GetMetadata retrieves a metadata value from the underlying thread.
func (s *Stream) GetMetadata(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.thread.Metadata[key]
	return v, ok
}

// AllMetadata returns a defensive copy of the thread's metadata map.
// Mutating the returned map does not affect the underlying thread.
// The stream mutex is held for the duration of the copy so a concurrent
// SetMetadata cannot race with the read. Conduits that need to seed
// their view at Start time (e.g. the TUI status bar) should call
// AllMetadata() before stream.Subscribe, since Subscribe is live-only
// and does not replay historical PropertiesEvents.
func (s *Stream) AllMetadata() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]string, len(s.thread.Metadata))
	for k, v := range s.thread.Metadata {
		out[k] = v
	}
	return out
}

// SetMetadata sets a metadata value on the underlying thread.
func (s *Stream) SetMetadata(key, value string) {
	s.mu.Lock()
	s.thread.Metadata[key] = value
	s.mu.Unlock()
	_ = s.Emit(context.Background(), loop.PropertiesEvent{
		Operations: []loop.PropertyOperation{
			{Op: loop.PropertyOpSet, Key: key, Value: value},
		},
		Ctx: loop.WithProvenance(context.Background(), "app"),
	})
}

// DeleteMetadata removes a metadata key from the underlying thread and
// emits a PropertiesEvent carrying a single PropertyOpDelete operation.
// Deleting a non-present key is a no-op for both the thread state
// (Go's delete() on a missing key returns the zero value) and the
// receiving state in conduits (they apply deletes via the same path
// the TUI uses in Task 4).
//
// DeleteMetadata exists to fulfill issue #531: prior to this method,
// callers could not represent key removal through the property event
// protocol without collapsing "absent" onto "present-with-empty-value".
func (s *Stream) DeleteMetadata(key string) {
	s.mu.Lock()
	delete(s.thread.Metadata, key)
	s.mu.Unlock()
	_ = s.Emit(context.Background(), loop.PropertiesEvent{
		Operations: []loop.PropertyOperation{
			{Op: loop.PropertyOpDelete, Key: key},
		},
		Ctx: loop.WithProvenance(context.Background(), "app"),
	})
}

// MarkBoundary records a compaction boundary by stamping the summary
// turn with [ledger.ControlStop]. The walk
// ([ledger.Thread.ResolveActivePath]) then terminates at the summary,
// hiding everything that came before it from the LLM-facing view.
//
// summaryTurnID is the ID of the summary turn (already appended to
// the thread's state via AppendTurn or equivalent). info is the
// JSON-encoded BoundaryInfo produced by [Summarize]; it is
// persisted to [Thread.Metadata] under
// "ore.compaction.boundary.info" so TUI/audit consumers can read it
// without re-deriving from the LLM-facing summary text.
//
// MarkBoundary is the canonical way to record a compaction. The
// caller is responsible for appending the summary turn (via
// AppendTurn or [ledger.Thread.Append]) before calling MarkBoundary;
// the function does not mutate the turn list.
//
// MarkBoundary holds the stream mutex for the duration of the
// metadata and control writes. The serial-pipeline-only contract
// applies.
func (s *Stream) MarkBoundary(summaryTurnID, info string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.thread.State.SetControl(summaryTurnID, ledger.ControlStop)

	if info != "" {
		s.thread.Metadata[boundaryKeyInfo] = info
	}
	return nil
}

// Model metadata keys. These are the framework contract keys for
// session-level model configuration written via SetMetadata. The
// session package owns the canonical form (session.MetadataKeyModelName
// et al.); downstream tools import those constants when they need to
// set a value. The "ore.model." prefix reserves the namespace for
// framework-level model configuration.
const (
	MetadataKeyModelName            = "ore.model.name"
	MetadataKeyModelThinkingLevel   = "ore.model.thinking_level"
	MetadataKeyModelTemperature     = "ore.model.temperature"
	MetadataKeyModelMaxOutputTokens = "ore.model.max_output_tokens"
)

// Spec derives a [models.Spec] from the thread's metadata. The
// bool result is false when no recognized metadata keys are set;
// the caller should use the loop's default in that case. The
// framework does not merge metadata into a base Spec; the session
// is responsible for producing the effective Spec for each turn.
//
// Recognized keys (all private to this package; documented here as
// the framework contract):
//
//	"ore.model.name"              → Spec.Name
//	"ore.model.thinking_level"    → Spec.ThinkingLevel
//	"ore.model.temperature"       → Spec.Temperature (parsed float)
//	"ore.model.max_output_tokens" → Spec.MaxOutputTokens (parsed int)
//
// Unknown keys are ignored.
func (s *Stream) Spec() (models.Spec, bool) {
	name, hasName := s.GetMetadata(MetadataKeyModelName)
	if !hasName || name == "" {
		return models.Spec{}, false
	}
	spec := models.Spec{Name: name}

	if level, ok := s.GetMetadata(MetadataKeyModelThinkingLevel); ok && level != "" {
		spec.ThinkingLevel = models.ThinkingLevel(level)
	}

	if tempStr, ok := s.GetMetadata(MetadataKeyModelTemperature); ok && tempStr != "" {
		if t, err := strconv.ParseFloat(tempStr, 64); err == nil {
			spec.Temperature = &t
		}
	}

	if maxStr, ok := s.GetMetadata(MetadataKeyModelMaxOutputTokens); ok && maxStr != "" {
		if n, err := strconv.ParseInt(maxStr, 10, 64); err == nil {
			spec.MaxOutputTokens = n
		}
	}

	return spec, true
}

func (s *Stream) Save() error {
	// Sync the compaction-boundary metadata from ledger.Meta to
	// thread.Metadata under the ore.compaction.boundary.* namespace.
	// The boundary is the only state-level fact currently carried
	// in ledger.Meta; persisting it under the existing Metadata
	// channel avoids introducing a new JSON field on Thread. Other
	// ledger.Meta entries (none today) would require extending this
	// sync path before they could be persisted.
	s.mu.Lock()
	s.syncBoundaryToMetadataLocked()
	s.mu.Unlock()

	return s.store.Save(s.thread)
}

// syncBoundaryToMetadataLocked copies the compaction boundary from
// ledger.Meta into thread.Metadata under the ore.compaction.boundary.*
// keys. Must be called with s.mu held.
func (s *Stream) syncBoundaryToMetadataLocked() {
	if s.thread == nil || s.thread.State == nil {
		return
	}
	meta := s.thread.State.Meta()
	for _, key := range []string{boundaryKeyIndex, boundaryKeyInfo} {
		if v, ok := meta.Get(key); ok {
			s.thread.Metadata[key] = v
		}
	}
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
