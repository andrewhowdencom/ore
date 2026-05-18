# Plan: Add Broadcast Sink Forwarding to session.Manager

## Objective

Extend `session.Manager` with a registry of sink callbacks so that conduits can observe output artifacts from streams they do not directly own. When a `Stream` is created or attached, `Manager` auto-subscribes to the stream's `FanOut` and forwards `OutputEvents` to all registered sinks. This enables multi-conduit agents where the same conversation thread is mirrored across multiple frontends (e.g., a user interacting via both Slack and TUI simultaneously, or an agent responding to GitHub issues while also posting to Slack).

## Context

From repository topology mapping and file inspection:

- **`session/manager.go`**: `Manager` owns a `map[string]*Stream` registry, a `thread.Store`, a `provider.Provider`, and a `TurnProcessor`. `Create()` and `Attach()` instantiate `Stream` values and add them to the map. `Close()` removes a stream and calls `stream.Close()`. There is currently no mechanism for cross-stream event observation.
- **`session/stream.go`**: `Stream` owns a `loop.Step`, which has an embedded `loop.FanOut`. `Stream.Subscribe(kinds...)` delegates to `step.Subscribe()`, returning a receive-only channel filtered by event kind. `Stream.Process()` runs the inference pipeline and emits events to the FanOut. Each Stream's FanOut is isolated — subscribers to one Stream cannot see another Stream's events.
- **`loop/fanout.go`**: `FanOut` distributes `OutputEvent` values from a source channel to multiple subscriber channels, filtered by `Kind()`. `Subscribe(kinds ...string)` requires at least one kind argument; an empty slice creates an empty `kindSet`, meaning no events ever match. There is no "subscribe to all" option.
- **`loop/loop.go`**: `Step.Subscribe(kinds...)` delegates directly to `FanOut.Subscribe()`. Events emitted by `Step.Turn()` include `artifact.TextDelta`, `artifact.ReasoningDelta`, `loop.TurnCompleteEvent`, `loop.ErrorEvent`, and accumulated complete artifacts (`artifact.Text`, `artifact.Reasoning`, etc.).
- **`x/conduit/tui/tui.go`**: The TUI conduit creates or attaches to a stream in `Start()`, then calls `stream.Subscribe("turn_complete")` to receive assistant turns for its own session. It has no visibility into other streams.
- **`x/conduit/http/handler.go`**: The HTTP handler's `sendMessage` and `sessionEvents` endpoints call `stream.Subscribe(kinds...)` for a specific session ID. Each HTTP client only sees events from its own stream.

**Project conventions from `AGENTS.md`:**
- Core packages (`session/`, `loop/`) live at the root level for external import.
- Table-driven tests are the standard. Race detection (`go test -race ./...`) is mandatory.
- Functional options pattern is used for constructors.
- Errors are wrapped with `fmt.Errorf("...: %w", err)`.
- Backwards compatibility is not a concern at this stage — aggressive refactoring is preferred.

## Architectural Blueprint

**Selected approach: callback-based sink registry on Manager.**

The `Manager` gains a `sinks` slice protected by a `sync.RWMutex`. A sink is a callback function (`SinkFunc`) registered with a set of event kinds. When a `Stream` is created or attached, `Manager` auto-subscribes to the stream's `FanOut` (with all event kinds) and starts a forwarding goroutine. This goroutine reads from the subscription channel and invokes all registered sinks whose kind filters match the event. The sink callback receives both the `streamID` and the `event`, allowing sinks to route events to the appropriate destination.

**Key design decisions:**

1. **FanOut empty-kinds semantics**: `loop.FanOut.Subscribe()` is modified so that calling it with no arguments (empty `kinds` slice) means "match all event kinds." This is a minimal, backward-compatible change required because the Manager's forwarding goroutine needs to receive all event types from a stream without enumerating every possible kind.

2. **Sink registration API**: `RegisterSink(kinds []string, fn SinkFunc) func()` returns an unregistration function. This is idiomatic Go and avoids the need for explicit identifiers. Sinks are called synchronously in the forwarding goroutine; slow sinks will block other sinks for that event, matching the existing FanOut behavior where slow subscribers drop events.

3. **Idempotent forwarding per stream**: A `sync.Once` field is added to `Stream` to ensure `startSinkForwarding` is invoked at most once per stream, preventing duplicate event delivery when `RegisterSink` and `Create`/`Attach` race.

4. **No conduit modifications required**: Existing conduits (TUI, HTTP) continue to use direct `stream.Subscribe()` for their own sessions. The sink mechanism is additive — new or updated conduits can optionally register as global sinks to observe all streams.

**Alternative considered and rejected:**
- **Channel-based Manager-level subscription**: Exposing a `Manager.SubscribeSinks(kinds ...string) <-chan loop.OutputEvent` that returns events from all streams. Rejected because it would essentially re-implement `FanOut` at the Manager level (the unexported `outputEventEnvelope` in `loop` prevents direct reuse), and callback-based sinks more closely match the issue's stated requirement of "sink callbacks."

## Requirements

1. `loop.FanOut.Subscribe()` with an empty `kinds` slice must match all event kinds.
2. `session.Manager` must expose `RegisterSink(kinds []string, fn SinkFunc) func()` for registering callback sinks.
3. When a `Stream` is created via `Manager.Create()` or attached via `Manager.Attach()`, `Manager` must auto-subscribe to the stream's FanOut and forward matching `OutputEvents` to all registered sinks.
4. Sink callbacks must receive the stream ID and the `OutputEvent`.
5. Unregistering a sink via the returned function must stop event delivery to that sink.
6. A sink must receive events from both newly created streams (registered before stream creation) and existing streams (registered after stream creation).
7. Closing a stream must cleanly terminate its sink forwarding goroutine.
8. Existing `session.Manager` and `loop.FanOut` behavior must remain unchanged for all non-sink code paths.
9. `go test -race ./...` must pass after all changes.

## Task Breakdown

### Task 1: Allow FanOut Subscribe Without Kind Filtering
- **Goal**: Modify `loop.FanOut.Subscribe` and `loop.FanOut.send` so that an empty `kinds` slice matches all event kinds, enabling the Manager's forwarding goroutine to receive all events from a stream without enumerating every possible kind.
- **Dependencies**: None.
- **Files Affected**: `loop/fanout.go`, `loop/fanout_test.go`
- **New Files**: None.
- **Interfaces**: No new exported interfaces. The existing `Subscribe(kinds ...string) <-chan OutputEvent` signature is unchanged; only its semantics for the empty slice case are extended.
- **Validation**: `go test ./loop/...` passes. A new test `TestFanOut_SubscribeAllKinds` verifies that a subscriber created with no kind arguments receives events of all kinds (`text_delta`, `turn_complete`, `error`). Existing FanOut tests continue to pass unchanged.
- **Details**: In `Subscribe`, change `kindSet` construction so that `len(kinds) == 0` produces `nil` instead of an empty map. In `send`, change the filter condition from `_, ok := sub.kinds[event.Kind()]` to `sub.kinds == nil || _, ok := sub.kinds[event.Kind()]`. This is backward-compatible: all existing code that passes explicit kinds behaves identically.

### Task 2: Add Sink Callback Registry to session.Manager
- **Goal**: Extend `session.Manager` with a sink callback registry, idempotent per-stream forwarding, and auto-subscription on stream creation/attachment.
- **Dependencies**: Task 1 (the Manager's forwarding goroutine relies on `stream.Subscribe()` with no arguments matching all events).
- **Files Affected**: `session/manager.go`, `session/stream.go`
- **New Files**: None.
- **Interfaces**:
  ```go
  // SinkFunc receives OutputEvents from a specific stream.
  type SinkFunc func(streamID string, event loop.OutputEvent)

  // RegisterSink registers a callback that receives OutputEvents from all
  // active and future streams matching the given kinds. An empty kinds slice
  // means all event kinds. It returns a function that unregisters the sink.
  func (m *Manager) RegisterSink(kinds []string, fn SinkFunc) func()
  ```
- **Validation**: `go build ./...` passes. Existing `session` package tests continue to pass. (Full sink validation is deferred to Task 3.)
- **Details**:
  1. Add a `sync.Once` field named `forwardOnce` to `session.Stream` to ensure sink forwarding is started at most once per stream.
  2. Add `sinks []sink` and `sinksMu sync.RWMutex` fields to `session.Manager`.
  3. Define an unexported `sink` struct with `kinds map[string]struct{}` and `fn SinkFunc`.
  4. Implement `RegisterSink`: add the sink to the `sinks` slice under lock; iterate over all existing streams (read lock on `m.mu`) and call `m.startSinkForwarding(stream)` for each; return an unregistration closure that removes the sink from the slice.
  5. Implement `startSinkForwarding(stream *Stream)`: use `stream.forwardOnce.Do(...)` to start at most once; inside the `Do`, call `stream.Subscribe()` with no arguments (matches all kinds thanks to Task 1); if subscription succeeds, start a goroutine that ranges over the channel and calls all currently registered sinks whose `kinds` filter matches the event kind. The goroutine exits naturally when the stream is closed (channel closes).
  6. Modify `Manager.Create()`: after adding the new stream to `m.sessions`, call `m.startSinkForwarding(stream)`.
  7. Modify `Manager.Attach()`: after adding the new stream to `m.sessions` (the non-duplicate path), call `m.startSinkForwarding(stream)`. Do NOT call it on the duplicate-return path.

### Task 3: Add Tests for Manager Sink Forwarding
- **Goal**: Add comprehensive tests to `session/manager_test.go` verifying that the sink registry correctly forwards events, filters by kind, supports registration after stream creation, supports unregistration, and handles stream closure.
- **Dependencies**: Task 2 (implementation required before tests can validate behavior).
- **Files Affected**: `session/manager_test.go`
- **New Files**: None.
- **Interfaces**: None (tests only).
- **Validation**: `go test ./session/...` passes, `go test -race ./...` passes across the entire repository.
- **Details**:
  1. `TestManager_RegisterSink_ReceivesEventsFromNewStream`: Register a sink with `[]string{"text_delta", "turn_complete"}` before creating a stream. Process a user message through the stream with a mock provider that emits `TextDelta` and completes a turn. Assert the sink receives both events with the correct stream ID.
  2. `TestManager_RegisterSink_ReceivesEventsFromExistingStream`: Create a stream first, then register a sink. Process a user message. Assert the sink receives events (validates that `RegisterSink` iterates over existing streams).
  3. `TestManager_RegisterSink_UnregisterStopsDelivery`: Register a sink, unregister it via the returned function, then process a user message. Assert the sink callback is NOT invoked.
  4. `TestManager_RegisterSink_MultipleSinks`: Register two sinks with the same kinds. Process a user message. Assert both sinks receive the same events.
  5. `TestManager_RegisterSink_KindFiltering`: Register one sink with `[]string{"text_delta"}` and another with `[]string{"turn_complete"}`. Process a user message. Assert each sink receives only its subscribed kind.
  6. `TestManager_RegisterSink_ClosedStreamNoEvents`: Create a stream, close it, register a sink, and attempt to process a user message (which should fail because the stream is closed). Assert the sink receives no events.
  7. Use the existing test helpers (`mockProvider`, `simpleProcessor`, `drainWithClose`) and table-driven patterns already present in `session/manager_test.go`.

## Dependency Graph

- Task 1 → Task 2 (Task 2's `startSinkForwarding` relies on `stream.Subscribe()` with no arguments matching all events)
- Task 2 → Task 3 (Task 3 tests the sink behavior introduced in Task 2)

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| FanOut empty-kinds change accidentally alters existing subscription behavior | High | Low | The change only affects the `len(kinds) == 0` case. All existing code passes at least one kind. Covered by existing `loop/fanout_test.go` tests. |
| Duplicate event delivery if `startSinkForwarding` is called concurrently for the same stream | High | Medium | Mitigated by adding `sync.Once` to `Stream`. Documented in Task 2 implementation details. Verified in Task 3 race tests. |
| Slow sink callback blocks other sinks and the forwarding goroutine | Medium | Medium | Acceptable trade-off matching existing FanOut behavior. Document that sinks should be fast; if slow work is needed, sinks should start their own goroutines. |
| Goroutine leak if streams are created but never closed | Medium | Low | Same lifecycle issue exists for FanOut's own `run()` goroutine. Not a new problem. Streams are expected to be closed by conduits. |
| Sink callback panics crash the forwarding goroutine | High | Low | Not addressed by this plan. Follow-up: consider `recover()` in the forwarding loop or document that sinks must not panic. |

## Validation Criteria

- [ ] `loop.FanOut.Subscribe()` with no arguments receives all event kinds (verified by new test in Task 1).
- [ ] `go test ./loop/...` passes.
- [ ] `session.Manager.RegisterSink` exists with the correct signature.
- [ ] A sink registered before stream creation receives events from that stream.
- [ ] A sink registered after stream creation receives events from existing streams.
- [ ] Unregistering a sink stops event delivery.
- [ ] Multiple sinks receive the same events independently.
- [ ] Sink kind filtering correctly limits events to subscribed kinds.
- [ ] Closing a stream terminates its forwarding goroutine without panic or leak.
- [ ] `go test ./session/...` passes.
- [ ] `go test -race ./...` passes across the entire repository.
- [ ] `go build ./...` passes.
