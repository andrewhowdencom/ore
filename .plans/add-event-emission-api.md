# Plan: Add Event Emission API for Custom OutputEvents

## Objective

Add a public `Emit()` API to `session.Stream` and `loop.Step` that allows external code — including artifact handlers, interceptors, and application logic — to inject custom `loop.OutputEvent` implementations into the stream's output FanOut. This unblocks the slash command extension (#110), which requires handlers to emit meta-events (e.g., `SessionSwitchEvent`) that cross layer boundaries without coupling directly to conduit implementations.

## Context

The codebase has the following relevant structure:

- **`loop/loop.go`** defines `loop.Step`, which owns a private `emit()` method sending `outputEventEnvelope` values to an internal `events` channel consumed by `FanOut`. `Step` already exports `Subscribe()`, `SetEventContext()`, `Close()`, `Turn()`, and `Submit()`.
- **`loop/fanout.go`** implements `FanOut`, which distributes `OutputEvent` values from a source channel to multiple filtered subscribers.
- **`session/stream.go`** defines `session.Stream`, the public session API. It wraps a `*loop.Step` and currently provides `Process()`, `Subscribe()`, `Cancel()`, `Close()`, and `ID()`. There is no ingress path for custom events.
- **`session/event.go`** defines ingress events (`UserMessageEvent`, `InterruptEvent`) implementing the `session.Event` interface.
- **`session/manager.go`** owns `Stream` lifecycle and sink forwarding.
- **`x/conduit/http/types.go`** implements `MarshalOutputEvent`, which handles `TurnCompleteEvent`, `ErrorEvent`, and `ArtifactEvent`. For unknown event kinds it returns `fmt.Errorf("unsupported event kind: %s", event.Kind())`.
- **`examples/tui-chat/main.go`** composes `session.Manager`, `loop.Step`, and `cognitive.ReAct` — demonstrating that application-level code (and handlers constructed with closures) can hold `*session.Stream` references and would benefit from `Emit()`.

Project conventions from `AGENTS.md`:
- Prefer aggressive refactoring over backwards compatibility.
- Core packages live at root level; never place framework contracts under `internal/`.
- Use `log/slog`, table-driven tests, `go test -race ./...`.

## Architectural Blueprint

### Selected Architecture

Add `Emit()` to **both** `loop.Step` and `session.Stream`:

1. **`loop.Step.Emit(ctx, event)`** — exports the existing private `emit()` method. This is the low-level primitive. It is symmetric with the already-public `Step.Subscribe()`. All existing internal call sites (`Turn()`, `Submit()`, `finalizeTurn()`) are updated to use the exported name.

2. **`session.Stream.Emit(ctx, event)`** — the application-level API. It checks `s.closed` (but **not** `s.busy`, since handlers running during an active turn need to emit) and delegates to `s.step.Emit(ctx, event)`. This completes the ingress/egress symmetry on `Stream`: `Process()` and `Emit()` for input, `Subscribe()` for output.

3. **HTTP Conduit Extensibility** — `MarshalOutputEvent` gains a `json.Marshaler` interface check as a fallback for unknown event kinds. Custom events can control their own JSON representation by implementing `json.Marshaler`.

### Evaluated Alternatives

| Approach | Rationale | Why Rejected |
|---|---|---|
| Add `Emit()` only to `Stream` | Keeps `Step` internals private | `Step` already exports `Subscribe()`; `Emit()` is its natural counterpart. Hiding it forces applications that hold `*Step` directly (e.g., examples, tests) to reach through `Stream` unnecessarily. |
| Add `Emit()` only to `Step` | Direct access to event source | Applications typically interact with `Stream`, not `Step` directly. `Stream` holds the lifecycle state (`closed`) and should gate emission. |
| Skip HTTP marshaling extensibility | "Separate concern" per issue | Without it, the HTTP conduit silently drops custom events with an error. This leaves the feature unusable for HTTP-based applications, which are a primary use case. |

## Requirements

1. `loop.Step` must expose a public `Emit(ctx context.Context, event OutputEvent)` method. [inferred from issue]
2. All existing internal `emit()` calls in `loop/loop.go` must be updated to call the exported method. [inferred]
3. `session.Stream` must expose a public `Emit(ctx context.Context, event loop.OutputEvent) error` method. [explicit]
4. `Stream.Emit()` must return an error if the stream is closed. [inferred]
5. `Stream.Emit()` must **not** reject calls while the stream is busy (handlers need to emit during active turns). [inferred from use case]
6. Emitted custom events must be deliverable to existing subscribers through their `Subscribe()` channels. [explicit]
7. Custom events must implement `loop.OutputEvent` (already required by interface). [explicit]
8. The HTTP conduit's `MarshalOutputEvent` must support custom event types via a `json.Marshaler` fallback. [inferred from design considerations]
9. All changes must be covered by unit tests and pass `go test -race ./...`. [convention]

## Task Breakdown

### Task 1: Export `Step.emit()` as `Step.Emit()`
- **Goal**: Make the Step's event emission primitive public so external code can inject custom OutputEvents.
- **Dependencies**: None.
- **Files Affected**: `loop/loop.go`
- **New Files**: None.
- **Interfaces**: 
  - Rename `func (s *Step) emit(ctx context.Context, event OutputEvent)` to `func (s *Step) Emit(ctx context.Context, event OutputEvent)`
  - Update all call sites inside `loop/loop.go` (within `Turn()` and `finalizeTurn()`) to use `s.Emit(...)`
- **Validation**: 
  - `go test ./loop/...` passes
  - `go test -race ./loop/...` passes
  - `go build ./...` passes
- **Details**: This is a pure rename with no behavioral change. The method body remains identical. Verify every `s.emit(...)` call in `loop.go` is updated. Ensure no other packages reference the private `emit` name (there shouldn't be any — grep to confirm).

### Task 2: Add `Stream.Emit()` to session package
- **Goal**: Provide the application-level API for injecting custom events into a session stream.
- **Dependencies**: Task 1.
- **Files Affected**: `session/stream.go`, `session/stream_test.go`
- **New Files**: None.
- **Interfaces**: 
  - Add `func (s *Stream) Emit(ctx context.Context, event loop.OutputEvent) error` to `session.Stream`
  - The method checks `s.closed` under `s.mu` and returns `fmt.Errorf("session %s is closed", s.id)` if closed
  - It does **not** check `s.busy`
  - On success, it calls `s.step.Emit(ctx, event)` and returns `nil`
- **Validation**: 
  - `go test ./session/...` passes
  - `go test -race ./session/...` passes
  - `go build ./...` passes
- **Details**: 
  - Add the `Emit()` method to `Stream` in `session/stream.go`.
  - Add tests in `session/stream_test.go`:
    1. `TestStream_Emit_DeliversToSubscribers` — create a stream, subscribe to a custom event kind, emit a custom event, assert it is received.
    2. `TestStream_Emit_ClosedReturnsError` — close a stream, call `Emit()`, assert it returns an error.
    3. `TestStream_Emit_AllowedWhileBusy` — start a turn (mock provider with slow/delayed response), emit a custom event from a handler or concurrently, assert the event is delivered despite `busy == true`.
  - Define a test-only custom `OutputEvent` type in the test file (e.g., `testCustomEvent`) implementing `loop.OutputEvent`.

### Task 3: Add `json.Marshaler` fallback to HTTP `MarshalOutputEvent`
- **Goal**: Allow the HTTP conduit to serialize custom event types that are not among the built-in `loop.*Event` variants.
- **Dependencies**: None (parallelizable with Task 1; must complete before Task 4 integration).
- **Files Affected**: `x/conduit/http/types.go`, `x/conduit/http/types_test.go`
- **New Files**: None.
- **Interfaces**: 
  - In `MarshalOutputEvent`, in the `default` branch of the type switch, check if `event` implements `json.Marshaler`. If so, call `m.MarshalJSON()` and return the result.
  - If neither the type switch nor `json.Marshaler` matches, return the existing error.
- **Validation**: 
  - `go test ./x/conduit/http/...` passes
  - `go test -race ./x/conduit/http/...` passes
  - `go build ./...` passes
- **Details**: 
  - In `x/conduit/http/types.go`, modify the `default` case of `MarshalOutputEvent`:
    ```go
    default:
        if m, ok := event.(json.Marshaler); ok {
            return m.MarshalJSON()
        }
        return nil, fmt.Errorf("unsupported event kind: %s", event.Kind())
    ```
  - In `x/conduit/http/types_test.go`, add a test with a custom event type that implements `json.Marshaler` and `loop.OutputEvent`, verifying `MarshalOutputEvent` returns the custom JSON.
  - Ensure the custom JSON includes the `kind` field so that `UnmarshalOutputEvent` can potentially round-trip it if needed (though round-tripping custom events is not required for this task).

### Task 4: Integration verification
- **Goal**: Verify the full stack works together after all changes.
- **Dependencies**: Task 1, Task 2, Task 3.
- **Files Affected**: None (verification only).
- **New Files**: None.
- **Interfaces**: None.
- **Validation**: 
  - `go test -race ./...` passes
  - `go vet ./...` passes
  - `go build ./...` passes
- **Details**: Run the full test suite. If any package outside the direct changes relies on the old private `emit` name (unlikely), fix it. Confirm that custom events emitted via `Stream.Emit()` flow through to HTTP conduit subscribers and are marshaled correctly.

## Dependency Graph

- Task 1 → Task 2 (Task 2 calls `Step.Emit()` which is created in Task 1)
- Task 3 || Task 1 (Task 3 is independent of the `loop`/`session` rename)
- Task 1 → Task 4
- Task 2 → Task 4
- Task 3 → Task 4

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| `Step.Emit()` called after `Step.Close()` causes goroutine leak (no reader on `s.events` channel) | Medium | Low | `Stream.Emit()` checks `closed` before delegating. For direct `Step.Emit()` callers, document that behavior after `Close()` is undefined. If needed, add a `closed` field to `Step` in a follow-up. |
| Existing tests or external code reference the old private `emit` name | Low | Low | `grep -r '\.emit(' --include='*.go'` before Task 1. The private name is only used inside `loop/loop.go`. |
| `json.Marshaler` fallback accidentally matches types that shouldn't be standalone events | Low | Low | The fallback only triggers for unknown `loop.OutputEvent` kinds. Well-formed custom events should implement `json.Marshaler` intentionally. |
| Race between `Stream.Emit()` and `Stream.Close()` | Medium | Low | `Stream.Emit()` acquires `mu`, checks `closed`, releases `mu`, then calls `Step.Emit()`. `Close()` sets `closed = true` before calling `Step.Close()`. This ordering prevents new emits after close begins. |

## Validation Criteria

- [ ] `loop.Step.Emit()` is exported and all internal call sites are updated.
- [ ] `session.Stream.Emit()` is exported, checks `closed`, and delegates to `Step.Emit()`.
- [ ] Unit tests verify custom events are delivered to subscribers via both `Step.Emit()` and `Stream.Emit()`.
- [ ] Unit test verifies `Stream.Emit()` returns an error when the stream is closed.
- [ ] Unit test verifies `Stream.Emit()` succeeds while the stream is busy (during a turn).
- [ ] HTTP conduit's `MarshalOutputEvent` falls back to `json.Marshaler` for unknown event kinds.
- [ ] Unit test verifies custom events implementing `json.Marshaler` are serialized correctly by `MarshalOutputEvent`.
- [ ] `go test -race ./...` passes with no failures or data races.
- [ ] `go build ./...` passes.
