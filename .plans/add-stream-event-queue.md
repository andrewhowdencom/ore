# Plan: Add per-stream event queue to replace ErrSessionBusy polling

## Objective
Replace the synchronous, lock-based `session.Stream.Process()` with an internal unbounded FIFO event queue and a single worker goroutine per stream. Conduits submit events via a new non-blocking `Submit(event)` method. This eliminates message drops caused by `ErrSessionBusy` and removes the need for conduits to poll or pace themselves against `turn_complete` for flow control.

## Context

### Current architecture
`session.Stream.Process()` in `session/stream.go` is fully synchronous: it acquires a `sync.Mutex`, checks a `busy` boolean, and either runs the entire inference pipeline inline or returns `session.ErrSessionBusy`. Conduits handle `ErrSessionBusy` in different, often lossy ways:

- **TUI** (`x/conduit/tui/tui.go`): calls `stream.Process()` in a goroutine; logs the error and clears the pending UI state.
- **HTTP** (`x/conduit/http/handler.go`): calls `h.mgr.Check(id)` before `stream.Process()` to pre-check busy state; returns HTTP 409 Conflict if busy.
- **Slack** (`x/conduit/slack/events.go`): catches `ErrSessionBusy`, logs it as non-fatal, and drops the message.
- **Telegram** (`x/conduit/telegram/telegram.go`): calls `stream.Process()` directly; logs any error (including busy).
- **stdio** (`x/conduit/stdio/stdio.go`): single-shot usage; not affected by queue design.

### Key files
- `session/stream.go` — core Stream with `Process()`, `Cancel()`, `Subscribe()`, `Emit()`, `Close()`
- `session/manager.go` — `Manager` with `Check()` that exposes `ErrSessionBusy`
- `session/event.go` — `Event`, `UserMessageEvent`, `InterruptEvent` interfaces
- `session/stream_test.go` — tests for Process, Emit, Interrupt, context propagation
- `session/manager_test.go` — tests for Check, concurrent Process, ErrSessionBusy
- `x/conduit/tui/tui.go` — Bubble Tea conduit
- `x/conduit/http/handler.go` — HTTP/NDJSON/SSE conduit
- `x/conduit/slack/events.go` — Slack message handler
- `x/conduit/telegram/telegram.go` — Telegram long-polling conduit
- `x/conduit/stdio/stdio.go` — single-shot stdio conduit

### Project conventions
Per `AGENTS.md`: aggressive refactoring is preferred over backwards compatibility at this stage. No production users, no migrations. All conduits must remain "dumb pipes" — they must not invoke the provider directly or manage turn loops. The queue lives inside `session.Stream`.

## Architectural Blueprint

### Selected architecture
A **single worker goroutine per Stream** with a **mutex-protected slice queue** and `sync.Cond` signaling.

**Why this over alternatives?**
- **Alternative A: Channel-based queue.** Go channels are bounded; an unbounded queue requires a custom ring buffer or linked list plus a `chan struct{}` wake signal. A mutex + slice is simpler and natively unbounded.
- **Alternative B: Central manager-level queue.** Would serialize across all streams, violating per-stream isolation. Rejected.
- **Alternative C: Per-conduit queue.** Would duplicate queue logic in every conduit and break the "dumb pipe" boundary. Rejected.

The worker goroutine is started **lazily** via `sync.Once` on the first `Submit()` or `Process()` call, avoiding goroutine overhead for idle streams.

### Unified processing path
Both `Submit()` and `Process()` enqueue into the same queue. The only difference is whether the caller waits for completion:
- `Submit(event Event) error` — enqueues and returns immediately (non-blocking)
- `Process(ctx context.Context, event Event) error` — enqueues and blocks on a per-event `done` channel until the worker signals completion (or the caller's context is cancelled)

This gives conduits the non-blocking API they need while preserving the synchronous contract for tests and single-shot usage (stdio).

### InterruptEvent semantics
When an `InterruptEvent` is submitted:
1. The queue is cleared (all pending non-interrupt events are dropped).
2. The `InterruptEvent` itself is enqueued as the sole remaining item.
3. `Cancel()` is called to abort the in-flight turn (if any).
4. The worker, when it finishes the cancelled turn, picks up the `InterruptEvent`, sets context, saves, and emits `ProcessCompleteEvent`.

### Removal of ErrSessionBusy
`ErrSessionBusy` and `Manager.Check()` are removed entirely. The `busy` field is deleted from `Stream`. The queue naturally serializes turns, so the concept of "busy" is no longer meaningful at the API surface.

## Requirements

1. Add `Stream.Submit(event Event) error` as a non-blocking enqueue API.
2. Change `Stream.Process()` to enqueue and wait on a per-event completion channel.
3. Add a single internal worker goroutine per Stream that drains the queue serially.
4. Implement `InterruptEvent` semantics: clear queue, cancel in-flight turn, enqueue the interrupt itself.
5. Remove `session.ErrSessionBusy` variable and `Manager.Check()` method.
6. Update TUI, HTTP, Slack, and Telegram conduits to call `Submit()` instead of `Process()` and remove all `ErrSessionBusy` handling.
7. Keep stdio conduit using `Process()` (single-shot, no concurrency).
8. Update all session tests to reflect queue behavior instead of busy errors.
9. Add new tests for queue FIFO ordering, concurrent submission, and interrupt queue clearing.
10. All changes must pass `go test -race ./...`.

## Task Breakdown

### Task 1: Implement queue and worker in session.Stream
- **Goal**: Add the unbounded FIFO queue, worker goroutine, `Submit()` method, and rewire `Process()` to enqueue-and-wait.
- **Dependencies**: None.
- **Files Affected**: `session/stream.go`
- **New Files**: None.
- **Interfaces**:
  - New method: `func (s *Stream) Submit(event Event) error`
  - Updated method: `func (s *Stream) Process(ctx context.Context, event Event) error` — now enqueues and blocks on completion
  - New private type: `type queuedEvent struct { event Event; ctx context.Context; done chan error }`
  - New private method: `func (s *Stream) processOne(ctx context.Context, event Event) error` — extracted synchronous inference pipeline
  - New private method: `func (s *Stream) startWorker()` and `func (s *Stream) worker()`
- **Validation**: `go test ./session/...` compiles (tests will fail until Task 2).
- **Details**:
  - Remove `busy` field and `sync.Mutex` sections that guard it.
  - Add `queue []queuedEvent`, `queueCond *sync.Cond`, `workerOnce sync.Once` fields to `Stream`.
  - `startWorker()` initializes `queueCond = sync.NewCond(&s.mu)` and starts `worker()` goroutine.
  - `worker()` loop: lock `s.mu`, wait on `queueCond` while queue empty and not closed, check closed and drain/exit, dequeue head, unlock, call `processOne()`, signal `done` if present.
  - `Submit()` checks `s.closed`, lazily starts worker, locks `s.mu`, clears queue on `InterruptEvent`, appends event, unlocks, signals `queueCond`, calls `Cancel()` on `InterruptEvent`.
  - `Process()` checks `s.closed`, lazily starts worker, creates buffered `done` channel, enqueues with `ctx` and `done`, signals `queueCond`, calls `Cancel()` on `InterruptEvent`, then selects on `done` or `ctx.Done()`.
  - Extract the current `Process()` body (after the busy check) into `processOne(ctx, event)`.
  - `Close()` sets `s.closed = true` and broadcasts `queueCond` (if non-nil) to wake the worker for graceful exit.
  - `Cancel()` remains unchanged.

### Task 2: Remove ErrSessionBusy and Manager.Check
- **Goal**: Delete the `ErrSessionBusy` error variable and the `Manager.Check()` method.
- **Dependencies**: Task 1.
- **Files Affected**: `session/manager.go`
- **New Files**: None.
- **Interfaces**:
  - Remove: `var ErrSessionBusy = errors.New(...)`
  - Remove: `func (m *Manager) Check(sessionID string) error`
- **Validation**: `go build ./...` compiles (conduit code will fail until Task 3).
- **Details**:
  - Delete `ErrSessionBusy` from `session/manager.go`.
  - Delete `Check()` method and its doc comment.
  - Update any doc comments in `session/stream.go` that reference `ErrSessionBusy`.

### Task 3: Update session tests for queue semantics
- **Goal**: Rewrite tests that depend on `ErrSessionBusy` or concurrent-process-failure behavior to instead verify queue ordering and completion.
- **Dependencies**: Task 1, Task 2.
- **Files Affected**: `session/stream_test.go`, `session/manager_test.go`
- **New Files**: None.
- **Validation**: `go test -race ./session/...` passes.
- **Details**:
  - In `manager_test.go`: remove tests for `Check()` and `ErrSessionBusy`. Replace concurrent-Process-failure tests with tests that verify multiple concurrent `Process()` calls both succeed and events are processed serially in FIFO order.
  - In `stream_test.go`: verify existing sequential `Process()` tests still pass. Add `TestStream_Submit_NonBlocking`, `TestStream_Submit_FIFOOrder`, `TestStream_Submit_InterruptClearsQueue`, `TestStream_ProcessAndSubmit_Mixed`.
  - Ensure `TestStream_Emit_AllowedWhileBusy` still passes (the queue worker replaces the `busy` lock, but `Emit()` is still allowed while a turn is in flight).
  - For tests that previously expected `ErrSessionBusy`, use `Submit()` to enqueue multiple events, then subscribe and verify they are processed one at a time in order.

### Task 4: Update TUI conduit to use Submit
- **Goal**: Replace `stream.Process()` with `stream.Submit()` in the TUI event processing goroutine.
- **Dependencies**: Task 1.
- **Files Affected**: `x/conduit/tui/tui.go`
- **New Files**: None.
- **Validation**: `go build ./x/conduit/tui/...` compiles.
- **Details**:
  - In the event processing goroutine, change `stream.Process(context.Background(), e)` to `stream.Submit(e)` for `UserMessageEvent`.
  - Remove error handling that was specific to `ErrSessionBusy`; `Submit()` errors (only possible for closed stream) should still be logged and send `clearPendingMsg{}`.
  - `InterruptEvent` handling remains `stream.Cancel()` — the TUI does not need to change this because the queue's `InterruptEvent` semantics are already handled correctly if the TUI were to submit an `InterruptEvent`. However, the TUI currently calls `Cancel()` directly. This is acceptable because `Cancel()` still aborts the in-flight turn. To be fully aligned with the new model, the TUI could submit an `InterruptEvent` instead, but calling `Cancel()` directly is functionally equivalent for UI interruption. Keep it simple: leave `Cancel()` as-is.

### Task 5: Update HTTP conduit to use Submit and remove Check
- **Goal**: Remove the pre-flight `h.mgr.Check()` call from `sendMessage`, replace `stream.Process()` with `stream.Submit()`, and adapt the NDJSON streaming loop.
- **Dependencies**: Task 1, Task 2.
- **Files Affected**: `x/conduit/http/handler.go`
- **New Files**: None.
- **Validation**: `go build ./x/conduit/http/...` compiles.
- **Details**:
  - In `sendMessage`: remove the `h.mgr.Check(id)` block entirely. Remove the `session.ErrSessionBusy` → HTTP 409 logic.
  - Replace `stream.Process(r.Context(), ...)` in the goroutine with `stream.Submit(session.UserMessageEvent{Content: req.Content})`.
  - Because `Submit()` is non-blocking, the goroutine that called `Process()` is no longer needed to wait for the turn. The NDJSON streaming loop already reads from the subscription channel until `process_complete`. The `done` channel and the goroutine can be removed entirely. The handler simply subscribes, submits, and streams events from the subscription until `process_complete` or client disconnect.
  - Update `x/conduit/http/handler_test.go` accordingly: remove tests for 409 Conflict on busy session.

### Task 6: Update Slack conduit to use Submit
- **Goal**: Replace `stream.Process()` with `stream.Submit()` and remove the `ErrSessionBusy` error swallowing.
- **Dependencies**: Task 1, Task 2.
- **Files Affected**: `x/conduit/slack/events.go`, `x/conduit/slack/events_test.go`
- **New Files**: None.
- **Validation**: `go build ./x/conduit/slack/...` compiles; `go test -race ./x/conduit/slack/...` passes.
- **Details**:
  - In `handleMessageEvent`: change `stream.Process(ctx, userEvent)` to `stream.Submit(userEvent)`.
  - Remove the `errors.Is(err, session.ErrSessionBusy)` branch entirely; all errors from `Submit()` (only closed stream) should be logged and returned.
  - Update `events_test.go` to remove tests or assertions that expect `ErrSessionBusy` handling.

### Task 7: Update Telegram conduit to use Submit
- **Goal**: Replace `stream.Process()` with `stream.Submit()` in the polling loop.
- **Dependencies**: Task 1.
- **Files Affected**: `x/conduit/telegram/telegram.go`, `x/conduit/telegram/telegram_test.go`
- **New Files**: None.
- **Validation**: `go build ./x/conduit/telegram/...` compiles; `go test -race ./x/conduit/telegram/...` passes.
- **Details**:
  - In `poll()`: change `stream.Process(ctx, event)` to `stream.Submit(event)`.
  - Remove any error-handling branches that were specific to busy states.
  - Update `telegram_test.go` where `stream.Process()` is called directly in tests.

### Task 8: Update session documentation and examples
- **Goal**: Update `session/doc.go` to document `Submit()` and remove references to `ErrSessionBusy`.
- **Dependencies**: Task 1, Task 2.
- **Files Affected**: `session/doc.go`
- **New Files**: None.
- **Validation**: `go build ./session/...` compiles.
- **Details**:
  - Update the package doc comment example to show `stream.Submit(...)` for conduits.
  - Remove or update any mention of `ErrSessionBusy` and synchronous polling.
  - Keep `stream.Process(...)` documented for single-shot and test usage.

### Task 9: Run full race-test validation
- **Goal**: Verify the entire repository compiles and passes race detection after all changes.
- **Dependencies**: Task 3, Task 4, Task 5, Task 6, Task 7, Task 8.
- **Files Affected**: None (validation step).
- **New Files**: None.
- **Validation**: `go test -race ./...` passes cleanly.
- **Details**:
  - Run `go test -race ./...`.
  - Fix any race conditions detected (likely around `queueCond` initialization or `s.closed` reads in the worker).
  - Verify no `ErrSessionBusy` references remain via `grep -rn "ErrSessionBusy"`.
  - Verify no `Check(` calls remain on Manager via `grep -rn "\.Check("`.

## Dependency Graph
- Task 1 → Task 2 (Task 2 deletes APIs that Task 1's new code no longer references)
- Task 1 → Task 4, Task 5, Task 6, Task 7 (conduit updates depend on `Submit()` existing)
- Task 2 → Task 5 (HTTP handler removes `Check()`)
- Task 1 + Task 2 → Task 3 (tests need both the queue and the removed APIs)
- Task 1 + Task 2 → Task 8 (docs need both new API and removed concepts)
- Task 3 + Task 4 + Task 5 + Task 6 + Task 7 + Task 8 → Task 9 (full validation)
- Task 4 || Task 5 || Task 6 || Task 7 || Task 8 (parallelizable after Task 1+2)

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Worker goroutine leak if `Close()` races with first `Submit()` | Medium | Low | Use `sync.Once` for worker start; ensure `Close()` broadcasts `queueCond` only after `sync.Once` guarantees initialization. Buffer `done` channels to prevent sender blocking on abandoned callers. |
| `sync.Cond` misuse causing spurious wakeups or missed signals | Medium | Medium | Follow the standard `sync.Cond` pattern: check condition in `for` loop, hold lock during `Signal`/`Broadcast`, always pair with `Unlock`. Add race-test coverage for concurrent Submit/Close. |
| Existing tests relying on `Process()` returning before subscription events fire | Low | Low | `Process()` still blocks until the worker finishes, preserving the synchronous contract for callers. Test event ordering remains deterministic. |
| Memory growth from unbounded queue if a conduit spams events | Medium | Low | Accepted per issue scope ("Risk: Memory growth... acceptable for UI use case"). Document this in `session/doc.go`. Future work can add bounded backpressure. |
| HTTP handler removing `Check()` changes client-visible behavior (no more 409) | Low | High | Intentional per issue requirements. Clients that previously retried on 409 will now receive NDJSON as normal. No breaking change for correct clients. |

## Validation Criteria
- [ ] `go test -race ./session/...` passes with updated tests.
- [ ] `go test -race ./x/conduit/tui/...` passes.
- [ ] `go test -race ./x/conduit/http/...` passes.
- [ ] `go test -race ./x/conduit/slack/...` passes.
- [ ] `go test -race ./x/conduit/telegram/...` passes.
- [ ] `go test -race ./x/conduit/stdio/...` passes.
- [ ] `grep -rn "ErrSessionBusy" --include="*.go" .` returns zero matches.
- [ ] `grep -rn "\.Check(" --include="*.go" session/manager.go` returns zero matches (or only unrelated Check calls).
- [ ] `go build ./...` compiles with no errors.
- [ ] All conduits (TUI, HTTP, Slack, Telegram) no longer reference `ErrSessionBusy`.
