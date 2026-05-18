# Plan: Add Event Provenance Metadata for Echo Suppression

## Objective

Add provenance metadata to session ingress events and loop output events so that bidirectional conduits operating in a broadcast sink model can identify events originating from their own submissions and skip re-rendering them. The framework provides the metadata; each conduit decides whether to use it for suppression.

## Context

The ore session manager uses a broadcast `RegisterSink` model where all registered sinks receive all output events from all active streams. When a bidirectional conduit (e.g., Slack, Telegram, Discord bot) submits a `UserMessageEvent` to a stream, the resulting `TurnCompleteEvent` (for the user turn) and downstream assistant artifacts are broadcast back to the same conduit, creating an echo loop. The framework must attach opaque provenance metadata to events so conduits can skip their own submissions.

Key observations from the codebase:

- `session.Event` is a minimal interface with only `Kind() string` (`session/event.go`).
- `UserMessageEvent` carries `Content string`; `InterruptEvent` is empty (`session/event.go`).
- `Stream.Process` handles events via a type switch, submits via `step.Submit`, then runs the `TurnProcessor` (`session/stream.go`).
- `loop.Step.Submit` and `loop.Step.Turn` both emit `TurnCompleteEvent` via `finalizeTurn` (`loop/loop.go`).
- `loop.TurnCompleteEvent` carries `Turn state.Turn`; `loop.ErrorEvent` carries `Err error` — neither has provenance metadata today.
- `OutputEvent` is `Kind() string`. `Artifact` also implements `OutputEvent` via `Kind() string`. Adding a second method to `OutputEvent` would break this relationship and require every artifact type to carry routing metadata, which is architecturally wrong.
- `Manager.RegisterSink` forwards ALL events to ALL sinks with no filtering (`session/manager.go`).
- The HTTP conduit marshals events to JSON via `x/conduit/http/types.go` and would need provenance fields included for completeness.
- The TUI conduit subscribes directly to its own stream and does not use `RegisterSink`, so it does not face the echo problem — but it will still receive provenance metadata on `TurnCompleteEvent` values it subscribes to.

## Architectural Blueprint

Provenance is an opaque string set by the caller when constructing an event (e.g., a conduit sets `"slack-webhook-42"`), threaded through the turn pipeline, and attached to the resulting `TurnCompleteEvent` and `ErrorEvent` emitted by `loop.Step`. Artifact deltas streamed during `Step.Turn` do NOT carry individual provenance — the final `TurnCompleteEvent` carries it, which is sufficient for conduits that act on complete turns. Conduits type-assert to `loop.ProvenancedEvent` to access provenance.

The architecture avoids breaking `Artifact` ↔ `OutputEvent` compatibility by NOT adding a method to `OutputEvent`. Only concrete loop event types (`TurnCompleteEvent`, `ErrorEvent`) are modified.

## Requirements

1. `UserMessageEvent` and `InterruptEvent` carry an optional `Provenance string` field.
2. `TurnCompleteEvent` and `ErrorEvent` carry an optional `Provenance string` field.
3. `loop.Step` stores the current turn's provenance internally and exposes `SetProvenance(string)`.
4. `Stream.Process` extracts provenance from the concrete event, sets it on the `Step`, runs the pipeline, and clears it afterward.
5. `finalizeTurn` emits `TurnCompleteEvent` with the stored provenance.
6. `Step.Turn` error path emits `ErrorEvent` with the stored provenance.
7. A `loop.ProvenancedEvent` interface is provided for type-assertion by conduits.
8. HTTP conduit JSON serialization includes provenance in `turn_complete` and `error` events.
9. All existing tests continue to pass; new tests verify provenance threading end-to-end.

## Task Breakdown

### Task 1: Add Provenance Field to Session Event Types
- **Goal**: Add `Provenance string` to `UserMessageEvent` and `InterruptEvent` and verify with tests.
- **Dependencies**: None.
- **Files Affected**: `session/event.go`, `session/event_test.go`
- **New Files**: None.
- **Interfaces**: None changed (the `Event` interface retains only `Kind() string`).
- **Validation**: `go test ./session/...` passes.
- **Details**: Add `Provenance string` field to both struct types. Update `session/event_test.go` to construct events with `Provenance` set and assert it is preserved. Leave `Event` interface unchanged to avoid breaking custom event types in other packages.

### Task 2: Add Provenance Support to Loop Events and Step
- **Goal**: Add `Provenance string` to `TurnCompleteEvent` and `ErrorEvent`, add `SetProvenance` to `Step`, and thread provenance through `finalizeTurn` and error emission.
- **Dependencies**: Task 1.
- **Files Affected**: `loop/loop.go`, `loop/loop_test.go`
- **New Files**: None.
- **Interfaces**:
  - `loop.ProvenancedEvent` (new interface):
    ```go
    type ProvenancedEvent interface {
        OutputEvent
        Provenance() string
    }
    ```
  - `TurnCompleteEvent.Provenance() string` and `ErrorEvent.Provenance() string` (new methods)
  - `Step.SetProvenance(provenance string)` (new method)
- **Validation**: `go test ./loop/...` passes.
- **Details**:
  1. Add `provenance string` field to `Step` struct.
  2. Add `SetProvenance(provenance string)` method to `Step`.
  3. Add `Provenance string` field to `TurnCompleteEvent` and `ErrorEvent`.
  4. Add `Provenance() string` methods to both so they satisfy `ProvenancedEvent`.
  5. In `finalizeTurn`, emit `TurnCompleteEvent{Turn: last, Provenance: s.provenance}`.
  6. In `Step.Turn` error path, emit `ErrorEvent{Err: err, Provenance: s.provenance}`.
  7. Add tests in `loop/loop_test.go`:
     - `TestStep_Submit_Provenance`: Submit with `SetProvenance("test-id")` and assert the emitted `TurnCompleteEvent` carries `"test-id"`.
     - `TestStep_Turn_Provenance`: Set provenance on Step, run `Turn`, and assert both `TurnCompleteEvent` and any `ErrorEvent` carry the provenance.
     - `TestStep_ProvenanceCleared`: After a turn, assert that a subsequent `Submit` without provenance emits an event with empty provenance.

### Task 3: Thread Provenance Through Stream.Process
- **Goal**: Extract provenance from input events in `Stream.Process`, set it on the Step, run the pipeline, and clear it afterward.
- **Dependencies**: Task 2.
- **Files Affected**: `session/stream.go`, `session/stream_test.go`, `session/manager_test.go`
- **New Files**: None.
- **Interfaces**: None changed.
- **Validation**: `go test ./session/...` passes.
- **Details**:
  1. In `Stream.Process`, in the `UserMessageEvent` branch, read `e.Provenance` and call `s.step.SetProvenance(e.Provenance)` before `s.step.Submit`. Use `defer s.step.SetProvenance("")` to clear after.
  2. In the `InterruptEvent` branch, if interrupt does not produce a `TurnCompleteEvent`, provenance handling is a no-op (no need to set/clear since no events are emitted). But for consistency and future-proofing, still set/clear around the `cancel()` call.
  3. Add `TestStream_Process_Provenance` in `session/stream_test.go`: create a stream, process a `UserMessageEvent{Content: "hi", Provenance: "test-provenance"}`, subscribe to `"turn_complete"`, and assert the emitted `TurnCompleteEvent` carries `"test-provenance"`.
  4. Add `TestManager_RegisterSink_ProvenanceEchoSuppression` in `session/manager_test.go`: register a sink, process a `UserMessageEvent` with provenance, and assert the sink callback receives a `TurnCompleteEvent` whose `Provenance` matches the input. This validates the end-to-end path through `RegisterSink`.

### Task 4: Update HTTP Conduit JSON Serialization
- **Goal**: Include `provenance` in the JSON DTOs for `turn_complete` and `error` events, and round-trip it correctly.
- **Dependencies**: Task 2.
- **Files Affected**: `x/conduit/http/types.go`, `x/conduit/http/types_test.go`
- **New Files**: None.
- **Interfaces**: None changed.
- **Validation**: `go test ./x/conduit/http/...` passes.
- **Details**:
  1. Add `Provenance string` field to `turnCompleteEventJSON` and `errorEventJSON` with `json:"provenance,omitempty"`.
  2. In `MarshalOutputEvent`, when handling `loop.TurnCompleteEvent`, include `Provenance` in the DTO.
  3. In `MarshalOutputEvent`, when handling `loop.ErrorEvent`, include `Provenance` in the DTO.
  4. In `UnmarshalOutputEvent`, when handling `"turn_complete"`, populate `loop.TurnCompleteEvent.Provenance` from the DTO.
  5. In `UnmarshalOutputEvent`, when handling `"error"`, populate `loop.ErrorEvent.Provenance` from the DTO.
  6. Update `x/conduit/http/types_test.go`:
     - Update `TestMarshalOutputEvent` `turn_complete` and `error` test cases to include `Provenance` and verify it appears in JSON output.
     - Update `TestUnmarshalOutputEvent` `turn_complete` and `error` test cases to include `Provenance` and verify it round-trips.
     - Update `TestRoundTrip_OutputEvent` to include `Provenance` on `TurnCompleteEvent` and `ErrorEvent`.
  7. Ensure `omitempty` keeps JSON clean when `Provenance` is empty.

### Task 5: Full Test Suite Verification
- **Goal**: Ensure the entire repository builds and all tests pass with race detection.
- **Dependencies**: Task 1, Task 2, Task 3, Task 4.
- **Files Affected**: None (verification only).
- **New Files**: None.
- **Interfaces**: None changed.
- **Validation**:
  - `go build ./...` succeeds with no errors.
  - `go test -race ./...` passes.
- **Details**: Run `go test -race ./...` and fix any compile or test failures. The most likely regressions are:
  - `x/conduit/http/handler_test.go` may construct `loop.TurnCompleteEvent` or `loop.ErrorEvent` directly in table tests; these will still compile because the new `Provenance` field is optional, but verify no test assertions break.
  - `cognitive/react_test.go` and `examples/*` may use `step.Submit` or `step.Turn` directly; these signatures are unchanged, so they should compile without modification.

## Dependency Graph

- Task 1 → Task 2 (Task 2 reads provenance from events)
- Task 2 → Task 3 (Task 3 uses `Step.SetProvenance` and `TurnCompleteEvent.Provenance`)
- Task 2 → Task 4 (Task 4 serializes `TurnCompleteEvent.Provenance` and `ErrorEvent.Provenance`)
- Task 3, Task 4 || (parallel after Task 2)
- Task 5 → Task 3, Task 4 (final verification)

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Adding `Provenance` to `TurnCompleteEvent`/`ErrorEvent` breaks type assertions in unexamined test files | Low | Low | The new field is optional (`string` zero-value). All existing struct literal constructions remain valid. Full `go test -race ./...` in Task 5 catches any regressions. |
| `Stream.Process` `busy` mutex does not prevent concurrent `SetProvenance` calls if `Step` is shared across streams | Medium | Low | Each `Stream` owns its own `Step` (created via `newStep()` in `Manager.Create`/`Attach`). Steps are never shared. The `busy` mutex in `Stream.Process` already serializes access per stream. |
| Conduits (e.g., TUI) that rely on receiving their own `TurnCompleteEvent` to render user messages will break if they naively suppress all events with matching provenance | Medium | Medium | The plan does NOT modify any conduit to perform suppression. It only makes metadata available. The TUI and HTTP handler remain unchanged. Echo suppression is an application-layer concern for future bidirectional conduits. This is documented in the plan. |
| `omitempty` on `Provenance` in JSON DTOs may cause `UnmarshalOutputEvent` to fail if a client sends `"provenance": null` | Low | Low | Use `string` type with `omitempty`. `null` unmarshals to `""` for string fields in Go, which is the correct zero value. Add a test case in Task 4 for `"provenance": null`. |

## Validation Criteria

- [ ] `session.UserMessageEvent` and `session.InterruptEvent` have a `Provenance string` field.
- [ ] `loop.TurnCompleteEvent` and `loop.ErrorEvent` have a `Provenance string` field and implement `loop.ProvenancedEvent`.
- [ ] `loop.Step.SetProvenance` exists and threads provenance through `Submit`, `Turn`, and `finalizeTurn`.
- [ ] `session.Stream.Process` extracts provenance from input events, sets it on the Step, and the resulting `TurnCompleteEvent` carries the same provenance.
- [ ] `session.Manager.RegisterSink` callbacks receive `TurnCompleteEvent` values with correct provenance.
- [ ] HTTP JSON serialization round-trips provenance on `turn_complete` and `error` events.
- [ ] `go test -race ./...` passes with zero failures.
- [ ] No changes are made to `Artifact` types, `OutputEvent` interface, or `TurnProcessor` signature.
