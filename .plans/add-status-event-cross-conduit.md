# Plan: Add Structured StatusEvent for Cross-Conduit Status Updates

## Objective

Introduce a new `StatusEvent` implementing `loop.OutputEvent` that carries a `map[string]string` of structured key-value status pairs. This event kind (`"status"`) is distributed through the existing per-session `FanOut`, allowing any component holding a `*session.Stream` to emit ambient, persistent status information (e.g. thread ID, token counts, model name) that all subscribing conduits receive simultaneously. Update the TUI and HTTP conduits — the two conduits that advertise `CapShowStatus` — to consume `"status"` events, and migrate their existing transient `"thinking..."` local status onto the event bus so all status display is unified.

## Context

The codebase is the **ore** framework for building agentic applications. Relevant findings from discovery:

- **`loop/loop.go`** defines the `OutputEvent` interface and all concrete event types: `TurnCompleteEvent`, `ErrorEvent`, `ProcessCompleteEvent`, `ArtifactEvent`. These all live in the `loop/` package and implement `Kind() string` and `Context() EventContext`.
- **`session/stream.go`** provides `Emit(ctx, event loop.OutputEvent) error`, which injects custom output events into the stream's `FanOut`. This is the canonical producer API.
- **`x/conduit/conduit.go`** defines `CapShowStatus` as a well-known capability, but currently neither TUI nor HTTP has a cross-conduit mechanism for sharing structured status.
- **TUI (`x/conduit/tui/`)** currently has a local `SetStatus(ctx, string)` method (`tui.go:177-180`) that sends `statusMsg{status: string}` into the Bubble Tea model. The model stores `status string` and renders it as `[status]` in `buildContent()` (`view.go:168-170`). The TUI advertises `CapShowStatus` in its `Descriptor` (`tui.go`).
- **HTTP (`x/conduit/http/`)** serves an embedded web UI (`static/chat.js`, `static/index.html`). The web UI has a `#status` div updated by `setStatus(text)`. The HTTP handler's `sendMessage` and `sessionEvents` endpoints stream NDJSON and SSE respectively. `MarshalOutputEvent` / `UnmarshalOutputEvent` in `types.go` handle all event serialization. Default event kinds in `handler.go` include artifact kinds, `turn_complete`, `error`, and `process_complete`, but not `"status"`.
- **Other conduits** (`slack`, `stdio`, `telegram`) do **not** advertise `CapShowStatus`, so they are out of scope.

The issue explicitly requests that the **current conversation thread ID** be included as a specific status field to aid debugging when multiple sessions or conduits are active.

## Architectural Blueprint

### Where StatusEvent Lives: `loop/` (not `session/`)

All existing `OutputEvent` implementations (`TurnCompleteEvent`, `ErrorEvent`, `ProcessCompleteEvent`, `ArtifactEvent`) are defined in `loop/loop.go`. The `OutputEvent` interface itself lives in `loop/`. Moving `StatusEvent` to `session/` would split the event taxonomy across packages and create an asymmetry where `loop.OutputEvent` implementations live in two places. `session/` already imports `loop/`, so placing `StatusEvent` in `loop/` maintains a single source of truth for the output event taxonomy and follows the existing pattern.

**Alternative considered**: `session/` — rejected because it would break the existing convention that all `OutputEvent` implementations live in `loop/`, and would not reduce imports (session already imports loop).

### Transient "thinking..." Status: Migrate to Event Bus

The TUI's `SetStatus("thinking...")` and the HTTP web UI's `setStatus('thinking...')` are local side-effects today. Migrating them to the event bus via `stream.Emit(ctx, StatusEvent{Status: map[string]string{"state": "thinking..."}})` provides two benefits:
1. All conduits subscribed to `"status"` see the same state simultaneously.
2. The mechanism is unified — there is no separate "local status" and "event status".

The TUI's `SetStatus(string)` method can be removed or repurposed, and the HTTP handler's local status calls replaced with stream emission. This aligns with the project's preference for aggressive refactoring and structural cleanliness.

### Event Kind Inclusion in Defaults

Both `sendMessage` and `sessionEvents` in the HTTP handler default their `kinds` list. `"status"` must be added to both defaults so clients receive status events without explicitly requesting them. The TUI subscription in `tui.go` must also include `"status"`.

### Thread ID as Initial Status

After creating or attaching to a stream, both the TUI (`Start()`) and HTTP (`createSession` and `sendMessage`) should emit a `StatusEvent` with `{"thread_id": stream.ID()}`. For the HTTP web UI (which uses NDJSON from `/messages` rather than SSE `/events`), the thread_id will be re-emitted at the start of each `sendMessage` processing so it is visible in the NDJSON stream.

## Requirements

1. Define `StatusEvent` in `loop/loop.go` with `Kind() == "status"`, carrying `map[string]string` and `EventContext`.
2. Update `x/conduit/http/types.go` to marshal/unmarshal `"status"` events via `MarshalOutputEvent` / `UnmarshalOutputEvent`.
3. Add `"status"` to default event kind lists in HTTP handler `sendMessage` and `sessionEvents`.
4. Update TUI to subscribe to `"status"` events, maintain a `map[string]string` on the model, and render selected keys compactly in the status line.
5. Migrate TUI's transient `"thinking..."` from `SetStatus(string)` to `stream.Emit(ctx, StatusEvent{...})`.
6. Update HTTP handler `sendMessage` to emit `StatusEvent` with `thread_id` and `state` at the start of processing, and to clear/update `state` at completion.
7. Update `static/chat.js` to handle `event.kind === 'status'`, updating the `#status` div from the received map.
8. Update TUI and HTTP tests for the new behavior.
9. Document that `CapShowStatus` means a conduit consumes structured `"status"` events.
10. [inferred] Well-known status keys (e.g. `thread_id`, `state`) are agent-defined; conduits render all keys they receive in a compact format.

## Task Breakdown

### Task 1: Define StatusEvent in loop/ Package
- **Goal**: Add `StatusEvent` struct implementing `loop.OutputEvent` with `Kind() == "status"`.
- **Dependencies**: None.
- **Files Affected**: `loop/loop.go`
- **New Files**: None.
- **Interfaces**:
  ```go
  type StatusEvent struct {
      Status map[string]string
      Ctx    EventContext
  }
  func (e StatusEvent) Kind() string       { return "status" }
  func (e StatusEvent) Context() EventContext { return e.Ctx }
  ```
- **Validation**: `go test ./loop/...` passes. New unit tests in `loop/loop_test.go` verify `Kind()`, `Context()`, and that `StatusEvent` satisfies `loop.OutputEvent`.
- **Details**: Add `StatusEvent` after the existing `ArtifactEvent` in `loop/loop.go`. Add table-driven tests asserting the kind string and context propagation. No other file changes in this task.

### Task 2: Update HTTP Types for StatusEvent Marshaling
- **Goal**: Extend `MarshalOutputEvent` and `UnmarshalOutputEvent` in the HTTP conduit to handle `"status"` kind serialization.
- **Dependencies**: Task 1 (requires `loop.StatusEvent` type).
- **Files Affected**: `x/conduit/http/types.go`, `x/conduit/http/types_test.go`
- **New Files**: None.
- **Interfaces**:
  - New DTO: `statusEventJSON` with `Kind string`, `Status map[string]string`, `Context *eventContextJSON`.
  - New case in `MarshalOutputEvent` switch for `loop.StatusEvent`.
  - New case in `UnmarshalOutputEvent` switch for `"status"` kind.
- **Validation**: `go test ./x/conduit/http/...` passes. New tests verify round-trip marshal/unmarshal of `StatusEvent` with populated and empty maps, with and without `EventContext`.
- **Details**: Follow the exact pattern used for `errorEventJSON`, `processCompleteEventJSON`, etc. The `default` branch in `MarshalOutputEvent` that checks `json.Marshaler` should remain, but an explicit `loop.StatusEvent` case is preferred for clarity and consistency.

### Task 3: Update HTTP Handler to Emit and Stream Status Events
- **Goal**: Add `"status"` to default event kind lists and emit `StatusEvent` from the HTTP handler during request lifecycle.
- **Dependencies**: Task 2 (requires marshal support).
- **Files Affected**: `x/conduit/http/handler.go`, `x/conduit/http/handler_test.go`
- **New Files**: None.
- **Interfaces**: No new interfaces; existing `sendMessage` and `sessionEvents` signatures unchanged.
- **Validation**: `go test ./x/conduit/http/...` passes.
- **Details**:
  1. In `sendMessage`, add `"status"` to the `req.Kinds` default slice.
  2. In `sessionEvents`, add `"status"` to the `kinds` default slice.
  3. In `sendMessage`, before calling `stream.Process`, emit:
     ```go
     _ = stream.Emit(r.Context(), loop.StatusEvent{
         Status: map[string]string{"thread_id": stream.ID(), "state": "thinking..."},
         Ctx:    loop.EventContext{Provenance: "http"},
     })
     ```
  4. After `stream.Process` returns (success or error), emit a clearing/update `StatusEvent` with `{"state": ""}` or `{"state": "ready"}`.
  5. In `createSession`, after creating/attaching the stream, emit a `StatusEvent` with `{"thread_id": stream.ID()}` for SSE subscribers.
  6. Update handler tests to expect `"status"` in default kind lists.

### Task 4: Update TUI to Consume and Emit StatusEvent
- **Goal**: Replace the local `status string` field with a `map[string]string`, subscribe to `"status"` events, migrate transient status to the event bus, and emit initial thread_id status.
- **Dependencies**: Task 1.
- **Files Affected**:
  - `x/conduit/tui/model.go`
  - `x/conduit/tui/tui.go`
  - `x/conduit/tui/view.go`
  - `x/conduit/tui/model_test.go`
  - `x/conduit/tui/tui_test.go`
- **New Files**: None.
- **Interfaces**:
  - `statusMsg` struct changes to carry `map[string]string` instead of `string`.
  - `SetStatus(ctx, string) error` is repurposed or replaced. Since status is now event-driven, `SetStatus` can be removed; the goroutine emits `StatusEvent` directly via `stream.Emit()`.
- **Validation**: `go test ./x/conduit/tui/...` passes.
- **Details**:
  1. In `model.go`, change `status string` to `status map[string]string`. Change `statusMsg` accordingly.
  2. In `model.go` `Update()`, the `statusMsg` handler merges the received map into `m.status` (overwriting keys present, leaving absent keys untouched), then marks dirty and syncs viewport.
  3. In `tui.go`, add `"status"` to the `stream.Subscribe(...)` call.
  4. In `tui.go` `Start()`, after creating/attaching to the stream, emit:
     ```go
     _ = stream.Emit(ctx, loop.StatusEvent{
         Status: map[string]string{"thread_id": stream.ID()},
         Ctx:    loop.EventContext{Provenance: "tui"},
     })
     ```
  5. In the user event processing goroutine (`tui.go`), replace the `t.SetStatus(ctx, "thinking...")` and `t.SetStatus(ctx, "")` calls with `stream.Emit(ctx, loop.StatusEvent{Status: map[string]string{"state": "thinking..."}})` and `stream.Emit(ctx, loop.StatusEvent{Status: map[string]string{"state": ""}})` respectively.
  6. Remove the old `SetStatus` method entirely, or change it to a no-op/deprecated comment if external callers might exist (none found in the repo).
  7. In `view.go` `buildContent()`, replace the simple `[status]` rendering with compact key-value rendering of the status map: iterate keys in sorted order, format as `key=val` separated by spaces, wrapped in `[...]`. If the map is empty, render nothing.
  8. Update all `model_test.go` tests that assert on `m.status` or send `statusMsg`.

### Task 5: Update HTTP Web UI (chat.js) for Status Events
- **Goal**: Handle `"status"` events in the web UI's event parser and update the `#status` div from structured status data.
- **Dependencies**: Task 3 (HTTP handler must actually send status events).
- **Files Affected**: `x/conduit/http/static/chat.js`
- **New Files**: None.
- **Interfaces**: No new JS interfaces; `handleEvent` function gains a new branch.
- **Validation**: Manual: open the web UI, send a message, and verify the status line shows `thread_id=<id>` and `state=thinking...` during processing, then `state=` or `state=ready` after completion.
- **Details**:
  1. In `handleEvent(event)`, add a new branch before the `console.warn` fallback:
     ```js
     if (event.kind === 'status') {
         const parts = [];
         for (const [key, val] of Object.entries(event.status || {})) {
             if (val) parts.push(`${key}=${val}`);
         }
         setStatus(parts.join(' | ') || '');
         return;
     }
     ```
  2. Ensure `setStatus('')` still clears the status div correctly when the status map has no non-empty values.

### Task 6: Add Unit Tests and Documentation
- **Goal**: Add comprehensive tests for the new status event flow and update capability documentation.
- **Dependencies**: Tasks 1–5.
- **Files Affected**:
  - `loop/loop_test.go`
  - `x/conduit/http/types_test.go`
  - `x/conduit/http/handler_test.go`
  - `x/conduit/tui/model_test.go`
  - `x/conduit/conduit.go`
- **New Files**: None.
- **Interfaces**: No new interfaces.
- **Validation**: `go test -race ./...` passes.
- **Details**:
  1. In `loop/loop_test.go`: add tests for `StatusEvent` construction, kind, and context.
  2. In `x/conduit/http/types_test.go`: add round-trip marshal/unmarshal tests for `StatusEvent` with various maps (empty, single key, multiple keys, with context).
  3. In `x/conduit/http/handler_test.go`: add a test that POSTs to `/messages` and verifies an NDJSON line with `kind: "status"` is received containing `thread_id`.
  4. In `x/conduit/tui/model_test.go`: update existing status-related tests to use `map[string]string`, add a test verifying that a `statusMsg` merges into the model's status map and that `buildContent()` renders the map correctly.
  5. In `x/conduit/conduit.go`, update the comment on `CapShowStatus` to document that it means the conduit subscribes to `"status"` `OutputEvent`s and renders the structured key-value map.

## Dependency Graph

- Task 1 → Task 2 → Task 3 → Task 5
- Task 1 → Task 4
- Task 2 || Task 4 (parallelizable after Task 1)
- Task 3 || Task 4 (parallelizable; Task 3 depends on Task 2, Task 4 depends only on Task 1)
- Task 6 depends on Tasks 1–5

Visual:
```
Task 1
├──→ Task 2 → Task 3 → Task 5
│
└──→ Task 4

Task 6 depends on all above
```

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| TUI tests heavily assert on `m.status` string value; changing to `map[string]string` breaks many tests | Medium | High | Task 4 includes a thorough test update. The builder should run `go test ./x/conduit/tui/...` after each sub-change in Task 4 to catch breakage early. |
| HTTP web UI (chat.js) does not use SSE `/events`; NDJSON status events from `sendMessage` only appear during active turns, so ambient status (like thread_id at session creation) is invisible until first message | Medium | High | Accept as known limitation per issue scope. Thread_id is re-emitted at start of `sendMessage`. A future enhancement could have the web UI also subscribe to SSE `/events` for ambient status. Document this in the plan. |
| Removing `SetStatus(string)` breaks external consumers of the TUI package | Low | Low | No external callers exist in the repo. The method was only used internally. If breakage occurs, the method can be re-added as a thin wrapper that emits a `StatusEvent`. |
| Bubble Tea message loop panics or drops messages if `statusMsg` struct changes shape while in-flight | Low | Low | Bubble Tea messages are plain structs sent by value. Changing the field type from `string` to `map[string]string` is safe as long as all senders are updated atomically in the same task. |
| Status map rendering in TUI overflows narrow terminal widths | Low | Medium | Render keys in sorted order and truncate with `…` if the combined width exceeds viewport width. Use the existing `truncateString` utility in `view.go`. |

## Validation Criteria

- [ ] `go test -race ./...` passes after all tasks are complete.
- [ ] `StatusEvent` satisfies `loop.OutputEvent` with `Kind() == "status"`.
- [ ] `MarshalOutputEvent` and `UnmarshalOutputEvent` round-trip a `StatusEvent` with arbitrary `map[string]string` correctly.
- [ ] HTTP handler `sendMessage` default kinds include `"status"`.
- [ ] HTTP handler `sessionEvents` default kinds include `"status"`.
- [ ] TUI model renders a status map as compact `[key=val key2=val2]` in the status line.
- [ ] TUI's transient "thinking..." is no longer emitted via `SetStatus(string)`; it flows through `stream.Emit()` as a `StatusEvent`.
- [ ] HTTP web UI `chat.js` updates `#status` div when receiving `kind: "status"` NDJSON events.
- [ ] Both TUI and HTTP emit an initial `StatusEvent` containing `thread_id` after session creation/attachment.
- [ ] `CapShowStatus` documentation in `x/conduit/conduit.go` is updated to reference structured `"status"` events.
