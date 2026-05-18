# Plan: Add Event Provenance Metadata for Echo Suppression

## Objective

Add uniform event context metadata to the ore event model so that bidirectional conduits operating in a broadcast sink model can identify events originating from their own submissions and skip re-rendering them. This is accomplished by separating `Artifact` from `OutputEvent` and giving every event a first-class `Context() EventContext` method, analogous to Go's `context.Context` traveling alongside a value.

## Context

The ore session manager uses a broadcast `RegisterSink` model where all registered sinks receive all output events from all active streams. When a bidirectional conduit (e.g., Slack, Telegram, Discord bot) submits a `UserMessageEvent` to a stream, the resulting `TurnCompleteEvent` (for the user turn) and downstream assistant artifacts are broadcast back to the same conduit, creating an echo loop.

The current event model is accidentally conflated: `Artifact` implements `OutputEvent` because both expose `Kind() string`. This means any event-level metadata (provenance, trace IDs, timestamps) must also be added to every LLM artifact type — semantically wrong, since artifacts are data objects, not routing signals.

Key observations:

- `session.Event` is `Kind() string`; `UserMessageEvent` carries `Content`, `InterruptEvent` is empty. No common metadata.
- `loop.OutputEvent` is `Kind() string`; `TurnCompleteEvent` carries `Turn`, `ErrorEvent` carries `Err`. No common metadata.
- `artifact.Artifact` is `Kind() string`; ten-plus concrete types (Text, TextDelta, ToolCall, etc.) implement it. None should carry routing metadata.
- `Step.Turn` emits `artifact.Artifact` values directly as `OutputEvent` via `s.emit(ctx, art)` — the conflation point.
- `Manager.RegisterSink` forwards ALL events to ALL sinks with no filtering (`session/manager.go`).
- The TUI and HTTP conduits subscribe to specific event kinds on their own streams and do not currently face the echo problem, but they will receive context metadata on all events.

## Architectural Blueprint

**The separation:** `Artifact` stops implementing `OutputEvent`. When `Step.Turn` emits a streaming artifact, it wraps it in `ArtifactEvent{Artifact, Context}`. `TurnCompleteEvent` and `ErrorEvent` carry `Context` directly. Every `OutputEvent` value in the system now exposes `Context() EventContext` uniformly.

**Input/output symmetry:** `session.Event` also gains `Context() EventContext`. `UserMessageEvent` and `InterruptEvent` implement it. `Stream.Process` extracts the context from the input event, stores it on the `Step` via `SetEventContext`, and the resulting output events carry the same context.

**Why this reduces total complexity:**
- One `EventContext` struct replaces ad-hoc `Provenance string` fields scattered across structs.
- Future metadata (trace IDs, tenant IDs, request IDs) extends `EventContext` without touching any event type.
- Artifacts remain pure data objects. Routing metadata lives only on events.
- Subscribers access context uniformly: `event.Context()` works for every event in the stream.

## Requirements

1. `Artifact` no longer implements `OutputEvent`.
2. `loop.EventContext` is a struct carrying `Provenance string` (extensible for future metadata).
3. `loop.OutputEvent` requires `Context() EventContext`.
4. `loop.ArtifactEvent` wraps an `artifact.Artifact` with `EventContext` and implements `OutputEvent`.
5. `loop.TurnCompleteEvent` and `loop.ErrorEvent` carry `Context EventContext` and implement `Context()`.
6. `session.Event` requires `Context() loop.EventContext`.
7. `session.UserMessageEvent` and `session.InterruptEvent` carry `Context loop.EventContext` and implement `Context()`.
8. `loop.Step` stores `eventContext EventContext` and exposes `SetEventContext(ctx EventContext)`.
9. `Step.Turn` wraps every emitted artifact in `ArtifactEvent` with the stored context.
10. `finalizeTurn` emits `TurnCompleteEvent` with the stored context.
11. `Step.Turn` error path emits `ErrorEvent` with the stored context.
12. `Stream.Process` extracts `Context` from the concrete input event and calls `step.SetEventContext` before running the pipeline.
13. HTTP conduit JSON serialization handles `ArtifactEvent` and includes context in `turn_complete`, `error`, and artifact JSON.
14. All existing tests pass; no regressions in `go test -race ./...`.

## Task Breakdown

### Task 1: Separate Artifact from OutputEvent in loop Package
- **Goal**: Redefine `OutputEvent` with `Context()`, introduce `EventContext` and `ArtifactEvent`, update all artifact emissions in `Step.Turn`, and fix all loop tests.
- **Dependencies**: None.
- **Files Affected**: `loop/loop.go`, `loop/loop_test.go`, `loop/fanout_test.go`, `loop/doc.go`
- **New Files**: None.
- **Interfaces**:
  - `loop.OutputEvent` updated to: `Kind() string; Context() EventContext`
  - `loop.EventContext` (new struct): `Provenance string`
  - `loop.ArtifactEvent` (new struct): `Artifact artifact.Artifact; Context EventContext` — implements `OutputEvent`
  - `loop.TurnCompleteEvent.Context() EventContext` (new method)
  - `loop.ErrorEvent.Context() EventContext` (new method)
  - `loop.Step.SetEventContext(ctx EventContext)` (new method)
- **Validation**: `go test ./loop/...` passes.
- **Details**:
  1. Add `EventContext struct { Provenance string }` in `loop/loop.go`.
  2. Change `OutputEvent` interface to require `Context() EventContext`.
  3. Add `Context EventContext` field and `Context() EventContext` method to `TurnCompleteEvent` and `ErrorEvent`.
  4. Define `ArtifactEvent` struct with `Artifact artifact.Artifact` and `Context EventContext`, implementing `Kind() string` and `Context() EventContext`.
  5. Add `eventContext EventContext` field and `SetEventContext(ctx EventContext)` method to `Step`.
  6. In `Step.Turn`, wrap every `s.emit(ctx, art)` and `s.emit(ctx, currentBlock)` call in `ArtifactEvent{Artifact: ..., Context: s.eventContext}`.
  7. In `finalizeTurn`, emit `TurnCompleteEvent{Turn: last, Context: s.eventContext}`.
  8. In `Step.Turn` error path, emit `ErrorEvent{Err: err, Context: s.eventContext}`.
  9. Update `loop/fanout_test.go`: `unknownOutputEvent` must implement `Context() EventContext`.
  10. Update `loop/loop_test.go`: Every type assertion `events[N].(artifact.TextDelta)` becomes `events[N].(loop.ArtifactEvent).Artifact.(artifact.TextDelta)`. Every `events[N].(artifact.Text)` becomes `events[N].(loop.ArtifactEvent).Artifact.(artifact.Text)`. Accumulated artifact slices in tests (e.g., `deltas []artifact.Artifact`) unwrap via `ArtifactEvent.Artifact`.
  11. Update `loop/doc.go` to document that artifact values are delivered as `ArtifactEvent` wrappers.

### Task 2: Add Context to Session Events and Thread Through Stream.Process
- **Goal**: Update `session.Event` to require `Context()`, add context to `UserMessageEvent` and `InterruptEvent`, and thread context from input events through `Stream.Process` to the `Step`.
- **Dependencies**: Task 1.
- **Files Affected**: `session/event.go`, `session/stream.go`, `session/event_test.go`, `session/stream_test.go`, `session/manager_test.go`, `session/doc.go`
- **New Files**: None.
- **Interfaces**:
  - `session.Event` updated to: `Kind() string; Context() loop.EventContext`
  - `session.UserMessageEvent.Context() loop.EventContext` (new method)
  - `session.InterruptEvent.Context() loop.EventContext` (new method)
- **Validation**: `go test ./session/...` passes.
- **Details**:
  1. Change `session.Event` interface to require `Context() loop.EventContext`.
  2. Add `Context loop.EventContext` field to `UserMessageEvent` and `InterruptEvent`; implement `Context()` method on both.
  3. In `Stream.Process`, in the `UserMessageEvent` branch, read `e.Context` and call `s.step.SetEventContext(e.Context)` before `s.step.Submit`. Use `defer s.step.SetEventContext(loop.EventContext{})` to clear after.
  4. In the `InterruptEvent` branch, also set/clear context for consistency (though no events are emitted).
  5. Update `session/event_test.go` to construct events with `Context` and assert it is preserved.
  6. Update `session/manager_test.go`: `unsupportedEvent` must implement `Context() loop.EventContext`. Update any sink callback that type-asserts artifact types from `loop.OutputEvent` to unwrap via `loop.ArtifactEvent`.
  7. Add `TestStream_Process_ContextPropagation` in `session/stream_test.go`: process a `UserMessageEvent{Content: "hi", Context: loop.EventContext{Provenance: "test-id"}}`, subscribe to `"turn_complete"`, and assert the emitted `TurnCompleteEvent` carries `Context.Provenance == "test-id"`.
  8. Add `TestManager_RegisterSink_ContextEchoSuppression` in `session/manager_test.go`: register a sink, process a user message with provenance context, and assert the sink callback receives a `TurnCompleteEvent` whose `Context.Provenance` matches the input.
  9. Update `session/doc.go` to document the context field on events.

### Task 3: Update HTTP Conduit Serialization for ArtifactEvent and Context
- **Goal**: Update `MarshalOutputEvent` to handle `loop.ArtifactEvent`, include `context` in JSON DTOs, and update all HTTP serialization tests.
- **Dependencies**: Task 1.
- **Files Affected**: `x/conduit/http/types.go`, `x/conduit/http/types_test.go`, `x/conduit/http/handler.go`, `x/conduit/http/handler_test.go`
- **New Files**: None.
- **Interfaces**: None changed.
- **Validation**: `go test ./x/conduit/http/...` passes.
- **Details**:
  1. Add `eventContextJSON struct { Provenance string \`json:"provenance,omitempty"\` }` in `x/conduit/http/types.go`.
  2. Add `Context eventContextJSON` field to `turnCompleteEventJSON` and `errorEventJSON` with `json:"context,omitempty"`.
  3. Update `MarshalOutputEvent`: replace `case artifact.Artifact:` with `case loop.ArtifactEvent:` and unwrap via `e.Artifact` before calling `artifactToJSON`. Include `Context` in the JSON DTO.
  4. Update `MarshalOutputEvent` for `loop.TurnCompleteEvent` and `loop.ErrorEvent` to include `Context` in JSON.
  5. Update `UnmarshalOutputEvent`: when handling `"turn_complete"` and `"error"`, populate `loop.TurnCompleteEvent.Context` and `loop.ErrorEvent.Context` from JSON. When handling artifact kinds, construct `loop.ArtifactEvent{Artifact: art, Context: ctx}`.
  6. Update `x/conduit/http/types_test.go`:
     - `TestMarshalOutputEvent`: Update `turn_complete` and `error` test cases to include `Context` in expected JSON. Update `text_artifact` and `text_delta_artifact` cases to expect `ArtifactEvent` input (kind assertions via `event.Kind()` still work).
     - `TestUnmarshalOutputEvent`: Update `turn_complete` and `error` cases to include `Context` and verify round-trip.
     - `TestRoundTrip_OutputEvent`: Include `Context` on `TurnCompleteEvent`, `ErrorEvent`, and wrap artifacts in `ArtifactEvent`.
  7. Update `x/conduit/http/handler_test.go`: Any test that directly constructs `loop.ErrorEvent` or `loop.TurnCompleteEvent` and passes to `MarshalOutputEvent` should still compile (zero-value `Context` is fine). Any test that type-asserts received artifacts from the handler should unwrap via `loop.ArtifactEvent`.
  8. Update `x/conduit/http/handler.go` `sendMessage` and `sessionEvents` handlers: No signature changes needed (`stream.Subscribe` still returns `<-chan loop.OutputEvent`). The `MarshalOutputEvent` function handles the new event types internally.

### Task 4: Verify TUI and Cognitive Conduit Compatibility
- **Goal**: Ensure the TUI and cognitive packages compile and pass tests with the new `OutputEvent.Context()` requirement.
- **Dependencies**: Task 1, Task 2.
- **Files Affected**: `x/conduit/tui/tui.go`, `x/conduit/tui/model.go`, `x/conduit/tui/tui_test.go`, `x/conduit/tui/model_test.go`, `x/conduit/tui/view_test.go`, `cognitive/react.go`, `cognitive/react_test.go`
- **New Files**: None.
- **Interfaces**: None changed.
- **Validation**: `go test ./x/conduit/tui/... ./cognitive/...` passes.
- **Details**:
  1. The TUI subscribes only to `"turn_complete"` and type-asserts to `loop.TurnCompleteEvent`. The TUI code should compile without changes because `TurnCompleteEvent` now has a `Context` field (zero value by default). Verify by running tests.
  2. The cognitive package (`cognitive/react.go`) calls `step.Turn` and `step.Submit` but does not subscribe to events. No changes needed.
  3. If any TUI or cognitive tests define custom `OutputEvent` implementations, add `Context() loop.EventContext` methods returning `loop.EventContext{}`.
  4. If any TUI tests type-assert artifacts from `OutputEvent` channels, update to unwrap via `loop.ArtifactEvent`.

### Task 5: Update Examples and Remaining Tests
- **Goal**: Check all examples and remaining test files for custom `OutputEvent` or `session.Event` implementations and update them.
- **Dependencies**: Task 1, Task 2.
- **Files Affected**: `examples/http-chat/main.go`, `examples/tui-chat/main.go`, `examples/single-turn-cli/main.go`, `examples/calculator/main.go`, plus any test files not covered in prior tasks.
- **New Files**: None.
- **Interfaces**: None changed.
- **Validation**: `go build ./examples/...` succeeds.
- **Details**:
  1. Search the codebase for any remaining custom types that implement `loop.OutputEvent` or `session.Event` and add the required `Context()` method.
  2. Search for any remaining direct `artifact.Artifact` type assertions from `loop.OutputEvent` values (e.g., in example code or tests) and update to unwrap via `loop.ArtifactEvent`.
  3. Build all examples to ensure compilation.

### Task 6: Full Repository Verification
- **Goal**: Build and test the entire repository with race detection to confirm zero regressions.
- **Dependencies**: Task 1, Task 2, Task 3, Task 4, Task 5.
- **Files Affected**: None (verification only).
- **New Files**: None.
- **Interfaces**: None changed.
- **Validation**:
  - `go build ./...` succeeds with zero errors.
  - `go test -race ./...` passes with zero failures.
- **Details**: Run `go test -race ./...`. The most likely failure mode is a missed type assertion `event.(artifact.TextDelta)` that should now be `event.(loop.ArtifactEvent).Artifact.(artifact.TextDelta)`. Fix any compilation errors and test assertion mismatches. Run `go vet ./...` as a sanity check.

## Dependency Graph

- Task 1 → Task 2 (Task 2 depends on loop.EventContext and loop.OutputEvent.Context)
- Task 1 → Task 3 (Task 3 depends on loop.ArtifactEvent and loop.OutputEvent.Context)
- Task 1 → Task 4 (Task 4 depends on updated loop.OutputEvent)
- Task 2 → Task 5 (Task 5 checks session.Event implementations)
- Task 3 || Task 4 || Task 5 (parallel after their respective dependencies)
- Task 6 → Task 1, Task 2, Task 3, Task 4, Task 5

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Missed type assertion `event.(artifact.TextDelta)` in tests or handler code | High | Medium | The compiler catches these immediately. `go build ./...` in Task 6 is the safety net. Search for `\.\([a-zA-Z_]*artifact\.\)` before Task 6. |
| HTTP JSON clients break because artifact JSON shape changes (now wrapped in event envelope) | Medium | Low | `ArtifactEvent` does NOT change the JSON shape of the artifact payload. The JSON still emits the artifact fields directly (e.g., `{"kind":"text_delta","content":"he"}`), with an optional `"context"` envelope. The HTTP serialization unwraps `ArtifactEvent.Artifact` before JSON conversion. Existing clients see no change unless they opt into context. Verify in Task 3 tests. |
| `omitempty` on `context` in JSON DTOs may cause `UnmarshalOutputEvent` to fail on `"context": null` | Low | Low | `null` unmarshals to the zero value of `eventContextJSON` (`Provenance: ""`), which is correct. Add a test case in Task 3 for `"context": null`. |
| TUI or example code uses custom `OutputEvent` implementations not caught in Tasks 1–4 | Low | Low | `go build ./...` in Task 6 will fail with a clear error: missing `Context() EventContext` method. Fix is mechanical. |
| `Step.SetEventContext` introduces mutable state that could be misused across concurrent calls | Medium | Low | `Stream.Process` already serializes access per stream via the `busy` mutex. Only one turn runs at a time per stream. `SetEventContext` is set at the start of `Process` and cleared via `defer`. Document this invariant in code comments during Task 2. |

## Validation Criteria

- [ ] `go test ./loop/...` passes after Task 1.
- [ ] `go test ./session/...` passes after Task 2.
- [ ] `go test ./x/conduit/http/...` passes after Task 3.
- [ ] `go test ./x/conduit/tui/... ./cognitive/...` passes after Task 4.
- [ ] `go build ./examples/...` succeeds after Task 5.
- [ ] `go build ./...` succeeds after Task 6.
- [ ] `go test -race ./...` passes after Task 6.
- [ ] `artifact.Artifact` does not implement `loop.OutputEvent` — verified by attempting to assign an `artifact.Text` to a `loop.OutputEvent` variable (should fail to compile).
- [ ] `loop.OutputEvent.Context()` is accessible on every value emitted from `Step.Subscribe`.
- [ ] `session.Event.Context()` is accessible on `UserMessageEvent` and `InterruptEvent`.
- [ ] `Stream.Process` propagates input `EventContext` to output `TurnCompleteEvent.Context`.
- [ ] `MarshalOutputEvent` correctly serializes `ArtifactEvent`, `TurnCompleteEvent`, and `ErrorEvent` with context, and `UnmarshalOutputEvent` round-trips them.
