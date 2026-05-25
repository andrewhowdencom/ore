# Plan: Add `ProcessCompleteEvent` for Pipeline-Finished Audio Notifications

## Objective

Add a new `ProcessCompleteEvent` type to the `loop` core package, emitted by `session.Stream.Process()` when the entire inference pipeline (including all tool-call loops) has finished. All conduits will migrate their audio notification triggers from `TurnCompleteEvent` to this new event, eliminating the false-positive bell sounds that fire during intermediate assistant turns.

## Context

The `session.Stream.Process()` method in `session/stream.go` is fully synchronous: it blocks until `s.processor()` (a `session.TurnProcessor`, e.g. `cognitive.ReAct.Run`) returns. Inside that processor, `loop.Step.Turn()` may execute multiple times — each completion emits `TurnCompleteEvent` with `RoleAssistant`. Today every conduit reacts to that event, ringing the bell on every intermediate turn even though the system is still processing.

The TUI (`x/conduit/tui/tui.go`) is the most affected because terminal bell `\a` is synchronous and jarring. The same bug exists in the HTTP front-end (`chat.js` plays a tone on every `turn_complete`), Slack, and Telegram sinks — they all deliver or react to intermediate assistant turns.

Current signal path:
- `Process()` → `processor(ctx, step, state, provider)` → (ReAct loop: `Turn()` → tool execution → `Turn()` → ...) → returns
- Each `Turn()` → `finalizeTurn()` emits `TurnCompleteEvent`
- Conduits subscribe to `turn_complete` and ring on `RoleAssistant`

Desired signal path:
- `Process()` → processor runs → returns → `Process()` emits `ProcessCompleteEvent`
- Conduits subscribe to `process_complete` and ring exactly once per user interaction

## Architectural Blueprint

Extend the `loop` package with a new `OutputEvent` type `ProcessCompleteEvent` carrying the final error state and `EventContext`. The event propagates through the existing `FanOut`/`Subscribe` infrastructure with no new distribution mechanism. `session.Stream.Process()` emits this event immediately after its `processor()` call returns, **before** save cleanup so that the event hits subscribers before any error handling.

All conduits then:
1. Subscribe to the new `process_complete` kind (in addition to or instead of `turn_complete` for audio)
2. Move audio/notification triggers from `TurnCompleteEvent` to `ProcessCompleteEvent`
3. Keep rendering (conversation history, message display) bound to `TurnCompleteEvent` so UI updates remain streaming and incremental

This cleanly separates **incremental data** (TurnCompleteEvent → render text/tool blocks) from **lifecycle signal** (ProcessCompleteEvent → sound, typing indicator dismissal, button re-enabling).

### Alternative considered and rejected

**Local-only fix (TUI skip on tool-call artifacts):** We could check `Turn.Artifacts` for `ToolCall` in the TUI and skip the bell. Rejected per user request: "I'd like a structural fix" and "all other conduits" need the same change.

## Requirements

1. `loop` package defines `ProcessCompleteEvent` satisfying `OutputEvent`
2. `session.Stream.Process()` emits `ProcessCompleteEvent` after `processor()` returns, before cleanup
3. TUI subscribes to `process_complete` and rings on that event instead of `TurnCompleteEvent`
4. HTTP conduit subscribes to `process_complete`, streams it over NDJSON, and plays the front-end tone on it
5. Slack sink subscribes to `process_complete` (or keeps using `turn_complete` for delivery + adds `process_complete` for notifications)
6. Telegram sink subscribes to `process_complete` (same pattern)
7. HTTP JSON serialization round-trips `process_complete` correctly
8. All existing tests pass; new tests cover the new event emission

## Task Breakdown

### Task 1: Add `ProcessCompleteEvent` to `loop` core
- **Goal**: Define the new event type with `Kind() == "process_complete"`, an `error` field (or similar), and `EventContext`.
- **Dependencies**: None
- **Files Affected**: `loop/loop.go`
- **New Files**: None
- **Interfaces**:
  - New type: `type ProcessCompleteEvent struct { Err error; Ctx EventContext }`
  - Methods: `Kind() string` returning `"process_complete"`, `Context() EventContext`
- **Validation**: `go test ./loop/...` passes; verify `Kind()` returns expected string in a unit test
- **Details**: Add the struct and methods in `loop/loop.go` near `ErrorEvent`. No behavior changes yet.

### Task 2: Emit `ProcessCompleteEvent` from `session.Stream.Process()`
- **Goal**: Wire the emission so that every `Process()` call broadcasts exactly one `ProcessCompleteEvent` after `processor()` returns.
- **Dependencies**: Task 1
- **Files Affected**: `session/stream.go`
- **New Files**: None
- **Interfaces**: `Stream.Process()` gains an internal `s.step.Emit(...)` call
- **Validation**: `go test ./session/...` passes; add `TestStream_Process_EmitsProcessCompleteEvent` asserting the event is received by a subscriber
- **Details**: In `Process()`, after `runErr` is populated (post-processor) and before save cleanup, call `s.Emit(ctx, loop.ProcessCompleteEvent{Err: runErr, Ctx: s.step.eventContext})`. Note: `s.step` is already accessible; if `Emit` is available on `*Stream`, use `s.Emit()`. If not, use `s.step.Emit()`. Ensure `eventContext` is captured before the processor returns.

### Task 3: Update TUI to ring on `ProcessCompleteEvent`
- **Goal**: Move the terminal bell trigger from `turn_complete` to `process_complete`.
- **Dependencies**: Task 2
- **Files Affected**: `x/conduit/tui/tui.go`, `x/conduit/tui/model.go`, `x/conduit/tui/model_test.go`
- **New Files**: None
- **Interfaces**:
  - `tui.go`: subscribe to `"turn_complete", "error", "process_complete"` instead of `"turn_complete", "error"`
  - Handle `loop.ProcessCompleteEvent` in the event goroutine: call `PlayDone()` or `PlayError()` based on `Err != nil`
  - Remove `if e.Turn.Role == state.RoleAssistant { _ = t.PlayDone(ctx) }` from `TurnCompleteEvent` handler
  - Remove `if e.Turn.Role == state.RoleAssistant` guard entirely
- **Validation**: `go test ./x/conduit/tui/...` passes; `TestAudioMsg` still passes (it tests the model's handling of `audioMsg` itself, which doesn't change); verify new test covers that `PlayDone` is called on `ProcessCompleteEvent` and NOT on intermediate `TurnCompleteEvent`
- **Details**: In the `outputCh` goroutine in `Start()`, add a case for `loop.ProcessCompleteEvent` that calls `PlayDone` (or `PlayError` on non-nil `Err`). Remove the `PlayDone` call from the `TurnCompleteEvent` branch. The `turnMsg` and rendering logic stays in `TurnCompleteEvent` — that still drives the conversation view.

### Task 4: Add HTTP NDJSON/SSE serialization for `ProcessCompleteEvent`
- **Goal**: Add JSON DTO, marshal/unmarshal, and event subscription wiring so the web UI receives `process_complete`.
- **Dependencies**: Task 1
- **Files Affected**: `x/conduit/http/types.go`, `x/conduit/http/handler.go`, `x/conduit/http/handler_test.go`
- **New Files**: None
- **Interfaces**:
  - `processCompleteEventJSON` struct in `types.go`
  - `MarshalOutputEvent` handles `"process_complete"` kind
  - `UnmarshalOutputEvent` handles `"process_complete"` kind
  - `handler.go`: `sendMessage` subscribes to `"process_complete"`; streams it to the NDJSON response; `done` branch writes `process_complete` or `error` as the terminal event
- **Validation**: `go test ./x/conduit/http/...` passes; new test verifies `MarshalOutputEvent` round-trips `ProcessCompleteEvent`; existing `TestHandler_SendMessage` family passes
- **Details**: In `sendMessage`, add `"process_complete"` to the default kinds list. In the event streaming loop, when `ProcessCompleteEvent` arrives, marshal and write it. The `done` channel case should send a `ProcessCompleteEvent` (or `ErrorEvent`) as the final line before returning. `sessionEvents` default kinds should also include `"process_complete"`.

### Task 5: Update HTTP web UI (`chat.js`) to play tone on `process_complete`
- **Goal**: Move audio from `turn_complete` to `process_complete` in the front-end.
- **Dependencies**: Task 4
- **Files Affected**: `x/conduit/http/static/chat.js`
- **New Files**: None
- **Interfaces**: `handleEvent()` gets a new branch for `event.kind === 'process_complete'`
- **Validation**: Manual verification: run the HTTP server and confirm tone plays only after the full assistant response finishes (including tool loops)
- **Details**: In `chat.js`, add `if (event.kind === 'process_complete')` — call `playDone()` if no error, `playError()` if `event.error` present. Remove `playDone()` from the `turn_complete` branch. Keep `finalizeTurn()` in `turn_complete` for UI state (typing indicator, button enable). Optionally, move `finalizeTurn()` to `process_complete` if the typing indicator should remain active during tool loops.

### Task 6: Update Slack sink to react on `process_complete` for delivery
- **Goal**: Ensure Slack message delivery happens at the correct granularity.
- **Dependencies**: Task 2
- **Files Affected**: `x/conduit/slack/slack.go`
- **New Files**: None
- **Interfaces**: `RegisterSink` kinds change
- **Validation**: `go test ./x/conduit/slack/...` passes
- **Details**: Slack currently posts each assistant `turn_complete` as a message. For ReAct loops, this would post multiple messages mid-interaction. The sink should continue using `turn_complete` for message rendering (each turn is a distinct message) OR switch to `process_complete` with accumulated text. Evaluate: Slack's UX is async, so posting intermediate tool calls is actually useful. The simplest structural fix: keep `turn_complete` for delivery, and only add `process_complete` for any future notification hooks. If there are no audio/notification hooks in Slack today, this task may be a no-op — but verify. Given no audio hook exists, we may just update the sink to subscribe to `process_complete` so it's available.

### Task 7: Update Telegram conduit to react on `process_complete` for delivery
- **Goal**: Ensure Telegram message delivery happens at the correct granularity.
- **Dependencies**: Task 2
- **Files Affected**: `x/conduit/telegram/telegram.go`
- **New Files**: None
- **Interfaces**: `RegisterSink` kinds change
- **Validation**: `go test ./x/conduit/telegram/...` passes
- **Details**: Same pattern as Slack. Telegram sends a message on each assistant `turn_complete`. For ReAct, this means multiple messages per user interaction. Unlike Slack, Telegram is more synchronous, so mid-interaction messages are confusing. Consider: switch Telegram to use `process_complete` for message delivery, accumulating all assistant text across the ReAct loop into one reply. Alternatively, keep `turn_complete` but use `process_complete` for a final status update. The simplest structural fix that eliminates the bug: change the sink to `process_complete`, accumulate text across turns, and send one message at the end. This requires storing text in the Telegram conduit between events.

### Task 8: Add comprehensive tests and documentation
- **Goal**: Verify all conduits work correctly with ReAct-style multi-turn processors.
- **Dependencies**: Tasks 1–7
- **Files Affected**: `loop/loop_test.go`, `session/stream_test.go`, `x/conduit/tui/model_test.go`, `x/conduit/http/handler_test.go`, `x/conduit/http/types_test.go`
- **New Files**: None
- **Validation**: `go test -race ./...` passes
- **Details**: Add tests for:
  - `ProcessCompleteEvent` emission with `nil` and non-nil error
  - TUI receives exactly one audio message after a multi-turn processor
  - HTTP round-trips `process_complete` correctly
  - Existing tests that assert on `turn_complete` count are adjusted if they were incorrectly counting intermediate turns for notification purposes

## Dependency Graph

- Task 1 → Task 2 → Task 3
- Task 1 → Task 4 → Task 5
- Task 2 → Task 6
- Task 2 → Task 7
- Task 1 → Task 8
- Task 3 || Task 4 || Task 6 || Task 7 (parallel after Task 2)
- Task 5 → Task 8
- Task 8 depends on all previous tasks

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Slack/Telegram users rely on seeing intermediate turns | Medium | Low | Conduits can keep rendering on `turn_complete` and only move audio/notification to `process_complete`. For Slack, this is the intended behavior. For Telegram, evaluate whether to accumulate text or send multiple messages. |
| `ProcessCompleteEvent` creates confusion about when to render vs. when to notify | Medium | Medium | Add clear documentation in `loop/loop.go` and `session/stream.go` distinguishing `TurnCompleteEvent` (incremental render) from `ProcessCompleteEvent` (pipeline lifecycle). |
| Adding a new event type breaks existing `UnmarshalOutputEvent` callers that don't know the kind | Low | Medium | `UnmarshalOutputEvent` returns an error for unknown kinds. The HTTP `handleEvent()` in `chat.js` already has a `default: console.warn`. This is acceptable — old clients will log a warning and skip the new event. |
| `Stream.Process()` error context not captured in `ProcessCompleteEvent` | Low | Low | Include the `error` in `ProcessCompleteEvent`. Ensure `runErr` is captured before save-cleanup error masking. |

## Validation Criteria

- [ ] `go test ./loop/...` passes with new `ProcessCompleteEvent` tests
- [ ] `go test ./session/...` passes with new `ProcessCompleteEvent` emission test
- [ ] `go test ./x/conduit/tui/...` passes; TUI bell fires exactly once per user message
- [ ] `go test ./x/conduit/http/...` passes; HTTP NDJSON includes `process_complete`
- [ ] `go test ./x/conduit/slack/...` passes
- [ ] `go test ./x/conduit/telegram/...` passes
- [ ] `go test -race ./...` passes cleanly
- [ ] `chat.js` front-end plays tone on `process_complete`, not `turn_complete`
