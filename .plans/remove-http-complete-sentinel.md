# Plan: Remove HTTP Complete Sentinel and Standardize on TurnCompleteEvent

## Objective

Remove the synthetic `complete` event from the `POST /sessions/{id}/messages` NDJSON streaming response. The `complete` event was introduced as a workaround for not properly using `TurnCompleteEvent` as the stream boundary. This plan standardizes on the assistant `TurnCompleteEvent` as the definitive end-of-turn signal, eliminates the `beforeCount`/`newTurns` bookkeeping and `drainSubscription` race-prone drain, and cleans up the corresponding dead code in types, tests, and the frontend.

## Context

From the ideation session, we converged on the following:

- The `complete` event (`{"kind":"complete","turns":[...]}`) is redundant with `TurnCompleteEvent` for signaling turn completion.
- The frontend (`chat.js`) already ignores `complete` and uses `turn_complete` to call `finalizeTurn()`.
- The only reason `drainSubscription` existed was to flush remaining events before synthesizing the `complete` event after `done` fired. By reading the subscription until the assistant `turn_complete` arrives, the natural event ordering becomes the boundary.
- `beforeCount` was only used to compute `newTurns` for the `complete` payload. With `complete` removed, it becomes dead code.
- `MarshalCompleteEvent` and `completeEventJSON` are only called from `handler.go` and only referenced in tests.

## Architectural Blueprint

The selected architecture was converged during ideation; no deliberation was needed.

- **Streaming boundary**: The HTTP handler's `sendMessage` select loop reads from the subscription channel until it observes `loop.TurnCompleteEvent` whose `Turn.Role == state.RoleAssistant`. At that point the response stream ends naturally.
- **Error path unchanged**: When `done` fires with an error, the handler emits `loop.ErrorEvent` to the stream and returns. No `complete` sentinel is appended.
- **Frontend alignment**: `chat.js` already treats `turn_complete` as the canonical end-of-turn signal. The only cleanup needed is removing the explicit `complete` ignore and the redundant trailing `finalizeTurn()` safety net in `sendMessage`.
- **Dead code removal**: `MarshalCompleteEvent`, `completeEventJSON`, and `drainSubscription` are removed entirely.

## Requirements

1. `complete` event must no longer appear in the NDJSON stream from `POST /sessions/{id}/messages`.
2. The assistant `turn_complete` event must be the last event in the happy-path stream.
3. `beforeCount` and `newTurns` computation must be removed from `sendMessage`.
4. `drainSubscription` must be removed from `handler.go`.
5. `MarshalCompleteEvent` and `completeEventJSON` must be removed from `types.go`.
6. Frontend must stop ignoring `complete` (since it no longer exists) and stop calling `finalizeTurn()` redundantly after stream EOF.
7. Tests must assert on `turn_complete` (or `error`) as the stream boundary instead of `complete`.
8. `doc.go` default-kinds documentation must be corrected to match the actual handler defaults (`text`, `reasoning`, `tool_call`, `tool_result`, `turn_complete`, `error`).

## Task Breakdown

### Task 1: Update Handler Streaming Logic
- **Goal**: Re-wire `sendMessage` to use the assistant `TurnCompleteEvent` as the stream boundary and remove `complete` emission.
- **Dependencies**: None.
- **Files Affected**: `conduit/http/handler.go`
- **New Files**: None.
- **Interfaces**: No new interfaces. Removed: `drainSubscription` function; `beforeCount` local variable.
- **Validation**: `go build ./...` passes.
- **Details**:
  1. Remove `beforeCount := len(sess.Thread().State.Turns())` from `sendMessage`.
  2. In the `subCh` case of the select loop, after writing the event, check:
     ```go
     if tc, ok := event.(loop.TurnCompleteEvent); ok && tc.Turn.Role == state.RoleAssistant {
         return
     }
     ```
     This makes the assistant `turn_complete` the natural end of the response.
  3. In the `done` case of the select loop, remove the `drainSubscription(subCh, nw)` call, remove the `newTurns` computation, and remove the `MarshalCompleteEvent` call. Keep only the `ErrorEvent` emission when `err != nil`.
  4. Delete the `drainSubscription` function entirely.
  5. The SSE endpoint (`sessionEvents`) is unchanged; it does not emit `complete`.

### Task 2: Update HTTP Package Tests
- **Goal**: Adjust all test assertions that relied on `complete` as the last NDJSON line.
- **Dependencies**: Task 1.
- **Files Affected**: `conduit/http/handler_test.go`, `conduit/http/types_test.go`
- **New Files**: None.
- **Interfaces**: No changes.
- **Validation**: `go test ./conduit/http/...` passes.
- **Details**:
  1. In `handler_test.go`, replace every assertion that checks `lines[len(lines)-1]` for `kind == "complete"` with an assertion that it is `turn_complete` with `role == "assistant"` (for success tests) or `kind == "error"` (for error tests).
  2. Remove or replace `completeEventJSON` unmarshal usage in tests.
  3. In `types_test.go`, delete `TestMarshalCompleteEvent` entirely.
  4. For tests that previously inspected `complete.Turns` to verify user/assistant turn content (e.g., `TestHandler_SendMessage`), shift those assertions to inspect the earlier `turn_complete` lines or the streamed artifact blocks directly.

### Task 3: Remove Dead Types and Marshaling Code
- **Goal**: Delete `completeEventJSON`, `MarshalCompleteEvent`, and correct stale doc comments.
- **Dependencies**: Task 2.
- **Files Affected**: `conduit/http/types.go`, `conduit/http/doc.go`
- **New Files**: None.
- **Interfaces**: Removed: `MarshalCompleteEvent(turns []state.Turn) ([]byte, error)`; `completeEventJSON` struct.
- **Validation**: `go build ./...` and `go test ./conduit/http/...` pass.
- **Details**:
  1. In `types.go`, remove `completeEventJSON` struct definition and `MarshalCompleteEvent` function.
  2. In `doc.go`, update the default-kinds comment from the old delta list to the actual defaults used in `handler.go`: `text, reasoning, tool_call, tool_result, turn_complete, error`.

### Task 4: Clean Up Frontend chat.js
- **Goal**: Remove frontend code that was only there to accommodate or ignore the now-removed `complete` event.
- **Dependencies**: Task 1 (backend must stop emitting `complete` before frontend stops ignoring it, otherwise an unknown-event warning would fire).
- **Files Affected**: `conduit/http/static/chat.js`
- **New Files**: None.
- **Interfaces**: No changes.
- **Validation**: `go test ./...` passes (no JS test suite; backend tests verify the build is intact).
- **Details**:
  1. In `handleEvent`, remove `|| event.kind === 'complete'` from the early-return line that ignores deltas/complete.
  2. In `sendMessage`, remove the trailing `finalizeTurn()` call after `await readNDJSONStream(reader, decoder)`. The assistant `turn_complete` event already triggers `finalizeTurn()` via `handleEvent`, so this safety net is now redundant.

## Dependency Graph

- Task 1 → Task 2 (tests must match the new streaming behavior)
- Task 2 → Task 3 (dead types can only be deleted once nothing references them)
- Task 4 || Task 3 (frontend and dead-type removal are independent after Task 2)

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Race: `done` fires before assistant `turn_complete` reaches `subCh` | Medium: client would not see `turn_complete`, leaving UI in "typing" state | Low | Buffered channels (cap 100) and dedicated FanOut goroutine make this extremely unlikely. If it occurs, the frontend's SSE/ambient channel (`sessionEvents`) provides eventual consistency. |
| Tests assert on hardcoded `complete` in ways missed by grep | Medium: tests may fail unexpectedly | Low | After Task 2, run full suite `go test ./conduit/http/...` to catch any stragglers. |
| Frontend without trailing `finalizeTurn()` stalls if `turn_complete` is dropped | Low: UI typing indicator persists | Low | `turn_complete` is a core event with high priority; buffer drops are unlikely in single-turn request/response. If observed later, a frontend timeout can be added as a follow-up. |
| doc.go comment drift not caught | Low: documentation misleading | Low | Explicitly included in Task 3. |

## Validation Criteria

- [ ] `go build ./...` passes after Task 1.
- [ ] `go test ./conduit/http/...` passes after Task 2.
- [ ] No references to `MarshalCompleteEvent`, `completeEventJSON`, `drainSubscription`, or `beforeCount` remain in `conduit/http/*.go` after Task 3.
- [ ] `grep -r "kind.*complete" conduit/http/static/chat.js` shows no `complete` references (except in comments if any) after Task 4.
- [ ] Manual verification: send a message via the web UI and confirm the stream ends cleanly with `turn_complete` and no `complete` event in browser dev-tools Network tab.
