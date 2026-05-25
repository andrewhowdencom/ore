# Plan: Add CapAudioNotification capability with AudioNotifier interface

## Objective

Replace the unused `CapRenderAudio` capability with a new `CapAudioNotification` capability and `AudioNotifier` interface, then wire audio notifications into the TUI (terminal bell) and HTTP (Web Audio API) conduits. The TUI will play sounds on assistant turn completion and errors, while the HTTP frontend JavaScript will play oscillator-based tones on the same lifecycle events.

## Context

### Current State

- `CapRenderAudio` exists in `x/conduit/conduit.go:23` as a capability constant but is referenced nowhere outside `x/conduit/conduit_test.go:19`.
- The TUI conduit (`x/conduit/tui/tui.go`) subscribes only to `"turn_complete"` events via `stream.Subscribe("turn_complete")`. Its output event goroutine handles `loop.TurnCompleteEvent` by sending `turnMsg` into the Bubble Tea message loop, but `loop.ErrorEvent` is explicitly ignored with a comment stating errors are handled via status updates rather than the message loop.
- The HTTP conduit (`x/conduit/http/handler.go`) has a `Descriptor` listing four capabilities (`CapEventSource`, `CapShowStatus`, `CapRenderTurn`, `CapRenderMarkdown`).
- The HTTP frontend (`x/conduit/http/static/chat.js`) handles `turn_complete` and `error` event kinds in `handleEvent()` but does not play any sounds.
- The HTTP handler test (`x/conduit/http/handler_test.go:889`) has `TestDescriptor` that asserts the exact set of HTTP capabilities.

### Conventions from AGENTS.md

- Core packages live at the root level; concrete provider adapters branch off.
- Table-driven tests are standard; mock interfaces use local struct implementations.
- Functional options pattern for constructors with optional parameters.
- Use `fmt.Errorf("...: %w", err)` for error wrapping.
- `log/slog` with `TextHandler` for lifecycle events.

## Architectural Blueprint

The selected architecture is a **capability-constant + interface** pattern:

1. **Remove the unused `CapRenderAudio`** and replace it with `CapAudioNotification`.
2. **Define `AudioNotifier`** as a minimal interface in `x/conduit` with `PlayDone` and `PlayError` methods, making it a cross-package contract.
3. **TUI implementation**: The `*TUI` type implements `AudioNotifier` by sending a new `audioMsg` through the Bubble Tea `program.Send()` channel. The `model.Update` handler receives `audioMsg` and emits a terminal bell (`\a`). The TUI also subscribes to `"error"` events and introduces an `errorMsg` type so errors flow through the Bubble Tea message loop for consistent UI updates.
4. **HTTP implementation**: The HTTP `Descriptor` gains `CapAudioNotification`. The embedded `chat.js` gains lightweight Web Audio API oscillator tones initialized lazily on the first user interaction (the send button click), with silent fallback if the browser blocks audio.

### Alternative Considered: Event-kind-based notification

An alternative was to encode audio notification as a new event kind in the loop/stream layer (e.g., `AudioEvent`). This was rejected because audio is a conduit-specific rendering concern, not a core framework event. The capability + interface pattern keeps the framework layer agnostic.

## Requirements

1. `CapRenderAudio` is removed; no references remain in the codebase.
2. `CapAudioNotification` is defined in `x/conduit/conduit.go`.
3. `AudioNotifier` interface is defined in `x/conduit` with `PlayDone` and `PlayError` methods.
4. TUI implements `AudioNotifier` and plays a sound on assistant `TurnCompleteEvent` and on `ErrorEvent`.
5. TUI subscribes to `"error"` events alongside `"turn_complete"`.
6. TUI `Descriptor` lists `CapAudioNotification`.
7. HTTP `Descriptor` lists `CapAudioNotification`.
8. HTTP frontend JS plays sounds on `turn_complete` (assistant role) and `error` events using Web Audio API with silent fallback.

## Task Breakdown

### Task 1: Remove CapRenderAudio, add CapAudioNotification and AudioNotifier interface

- **Goal**: Update the shared conduit capability constants and introduce the `AudioNotifier` interface contract.
- **Dependencies**: None.
- **Files Affected**:
  - `x/conduit/conduit.go`
  - `x/conduit/conduit_test.go`
- **New Files**: None.
- **Interfaces**:
  - Remove `CapRenderAudio Capability = "render-audio"` constant.
  - Add `CapAudioNotification Capability = "audio-notification"` constant.
  - Add `AudioNotifier` interface:
    ```go
    type AudioNotifier interface {
        PlayDone(ctx context.Context) error
        PlayError(ctx context.Context) error
    }
    ```
- **Validation**: `go test ./x/conduit/...` passes. No compile errors across the codebase.
- **Details**:
  1. In `x/conduit/conduit.go`, remove `CapRenderAudio` from the capability constants block. Add `CapAudioNotification` below `CapRenderImage` (or in a logical position near rendering capabilities).
  2. Add the `AudioNotifier` interface definition immediately after the `Capability` type definition.
  3. In `x/conduit/conduit_test.go`, remove `CapRenderAudio` from the `caps` slice in `TestCapabilityConstants_NonEmpty`. Add `CapAudioNotification`.

### Task 2: TUI - implement AudioNotifier and wire error/audio events

- **Goal**: Add audio notification support to the TUI conduit using the terminal bell (`\a`), and wire `ErrorEvent` through the Bubble Tea message loop.
- **Dependencies**: Task 1.
- **Files Affected**:
  - `x/conduit/tui/tui.go`
  - `x/conduit/tui/model.go`
  - `x/conduit/tui/tui_test.go`
- **New Files**: None.
- **Interfaces**:
  - `*TUI` implements `conduit.AudioNotifier`.
  - New message types:
    ```go
    type audioMsg struct{}
    type errorMsg struct{ err error }
    ```
- **Validation**: `go test ./x/conduit/tui/...` passes.
- **Details**:
  1. In `x/conduit/tui/tui.go`:
     - Add `conduit.CapAudioNotification` to `Descriptor.Capabilities`.
     - Change `stream.Subscribe("turn_complete")` to `stream.Subscribe("turn_complete", "error")`.
     - Add compile-time interface assertion: `var _ conduit.AudioNotifier = (*TUI)(nil)`.
     - Implement `func (t *TUI) PlayDone(ctx context.Context) error` that sends `audioMsg{}` to `t.program`.
     - Implement `func (t *TUI) PlayError(ctx context.Context) error` that sends `audioMsg{}` to `t.program`.
     - In the output event goroutine, update the `switch`:
       - On `loop.TurnCompleteEvent`: keep the existing `turnMsg` send. After sending, if `e.Turn.Role == state.RoleAssistant`, call `t.PlayDone(ctx)` (ignore error).
       - On `loop.ErrorEvent`: send `errorMsg{err: e.Err}` to `t.program`, then call `t.PlayError(ctx)` (ignore error).
  2. In `x/conduit/tui/model.go`:
     - Add `audioMsg` and `errorMsg` type definitions near the existing message types (`turnMsg`, `statusMsg`, `clearPendingMsg`).
     - In `model.Update`, add a handler for `audioMsg` that executes `fmt.Print("\a")` and returns `m, nil`.
     - In `model.Update`, add a handler for `errorMsg` that sets `m.pending = false`, sets `m.status = "Error: " + msg.err.Error()`, and returns `m, nil`.
  3. In `x/conduit/tui/tui_test.go`:
     - Add a test (e.g., `TestTUI_ImplementsAudioNotifier`) that asserts `var _ conduit.AudioNotifier = (*TUI)(nil)` compiles. This can be a zero-line test or an explicit assertion.

### Task 3: HTTP - add CapAudioNotification and frontend sounds

- **Goal**: Add audio notification capability to the HTTP conduit Descriptor and wire Web Audio API sounds into the embedded chat frontend.
- **Dependencies**: Task 1.
- **Files Affected**:
  - `x/conduit/http/handler.go`
  - `x/conduit/http/handler_test.go`
  - `x/conduit/http/static/chat.js`
- **New Files**: None.
- **Interfaces**: None.
- **Validation**: `go test ./x/conduit/http/...` passes.
- **Details**:
  1. In `x/conduit/http/handler.go`:
     - Add `conduit.CapAudioNotification` to `Descriptor.Capabilities`.
  2. In `x/conduit/http/handler_test.go`:
     - Update `TestDescriptor` to include `conduit.CapAudioNotification` in the expected capabilities slice.
  3. In `x/conduit/http/static/chat.js`:
     - Add a module-level variable `let audioCtx = null;`.
     - Add `function ensureAudio()` that lazily creates `window.AudioContext` (or `window.webkitAudioContext`) if null, wrapped in try/catch. Returns the context or null.
     - Add `function playTone(freq, duration, type = 'sine')` that:
       - Calls `ensureAudio()`.
       - If no context, returns silently.
       - Creates an oscillator and gain node, connects them, starts the oscillator, ramps gain to 0.001 over `duration`, and stops after `duration`.
       - Wraps all audio graph operations in try/catch for silent fallback.
     - Add `function playDone()` that calls `playTone(880, 0.15)`.
     - Add `function playError()` that calls `playTone(220, 0.3, 'sawtooth')`.
     - In `handleSend()`, call `ensureAudio()` at the top before proceeding.
     - In `handleEvent()`, update the `turn_complete` branch:
       - If `event.turn && event.turn.role === 'assistant'`, call `playDone()`.
     - In `handleEvent()`, update the `error` branch:
       - Call `playError()` before setting status.

### Task 4: Full validation

- **Goal**: Ensure the entire repository builds and tests pass after all changes.
- **Dependencies**: Task 2, Task 3.
- **Files Affected**: None (validation only).
- **New Files**: None.
- **Validation**: `go test -race ./...` passes. `go build ./...` passes.
- **Details**: Run the full test suite with race detection to catch any goroutine synchronization issues introduced by the new TUI event subscription or message types.

## Dependency Graph

- Task 1 → Task 2 (TUI imports `conduit.AudioNotifier` and `CapAudioNotification`)
- Task 1 → Task 3 (HTTP imports `conduit.CapAudioNotification`)
- Task 2 || Task 3 (parallelizable after Task 1)
- Task 4 depends on Task 2, Task 3

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Terminal bell `\a` is disabled in user's terminal emulator | Low | Medium | Accepted — terminal bell is the only universally available TUI sound without new dependencies, per issue constraints. |
| Web Audio API blocked by browser (no user interaction yet) | Low | Medium | Lazy-initialize AudioContext on first `handleSend()` call, which requires a click/Enter press. |
| Browser does not support Web Audio API at all | Low | Low | Silent fallback via try/catch around all audio graph operations. |
| Subscribing TUI to `"error"` events causes unexpected message loop traffic | Low | Low | `errorMsg` handler is minimal (sets status, clears pending), matching existing error handling semantics. |

## Validation Criteria

- [ ] `CapRenderAudio` is removed; `grep -r "CapRenderAudio" --include="*.go" .` returns no results.
- [ ] `CapAudioNotification` is defined in `x/conduit/conduit.go`.
- [ ] `AudioNotifier` interface is defined in `x/conduit/conduit.go`.
- [ ] TUI `Descriptor.Capabilities` includes `conduit.CapAudioNotification`.
- [ ] HTTP `Descriptor.Capabilities` includes `conduit.CapAudioNotification`.
- [ ] TUI implements `conduit.AudioNotifier` (compile-time check passes).
- [ ] TUI subscribes to `"turn_complete"` and `"error"` events.
- [ ] TUI `model.Update` handles `audioMsg` (prints `\a`) and `errorMsg` (sets status, clears pending).
- [ ] HTTP frontend `chat.js` calls `playDone()` on assistant `turn_complete` and `playError()` on `error`.
- [ ] All tests pass: `go test -race ./...`.