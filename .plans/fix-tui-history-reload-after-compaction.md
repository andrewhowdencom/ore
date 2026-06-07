# Plan: Fix TUI History Reload After Compaction

## Objective

Add a `reloadHistoryMsg` Bubble Tea message to the TUI model so that downstream applications can instruct the TUI to discard its local `m.turns` slice and rebuild it from a fresh `[]state.Turn` slice. This fixes the issue where the TUI continues to display the full, pre-compaction conversation history after the backend stream's turns are replaced with a synthetic summary via `stream.LoadTurns()`.

## Context

The TUI model (`x/conduit/tui/model.go`) maintains `m.turns`, a slice of `renderedTurn` values that represent the conversation history displayed in the scrollable viewport. This slice is populated **exactly once** during startup via `loadHistory()` inside `initModel()` (`x/conduit/tui/tui.go`). After that, the model only receives new turns via `turnMsg` (for completed turns) and delta artifacts via `artifactMsg` (for streaming assistant content).

When compaction runs (whether via a `/compact` slash command or an auto-compaction trigger), the backend code calls `stream.LoadTurns(compactedTurns)` (`session/stream.go:339`). This replaces the thread's persistent state with a synthetic summary turn plus any preserved tail turns. However, the TUI model never learns about this mutation. The user sees the stale, full-length history in the terminal, while the compacted state exists only in the JSON store.

The fix follows the pattern already used by `PlayDone` and `PlayError` in `x/conduit/tui/tui.go`, which send `tea.Msg` values to the running `tea.Program` via `t.program.Send()`. The `tea.Program` is private to `*TUI`, so a public method must be added to expose the reload capability to application code.

## Architectural Blueprint

**Selected path: TUI-local message type with public `ReloadHistory` method.**

- **Alternative A (add a new `loop.OutputEvent` type and emit from `LoadTurns`)**: Would require changing `session/stream.go` to emit an event inside `LoadTurns`, which would require restructuring lock acquisition to avoid deadlocks (`LoadTurns` currently holds `s.mu` and `s.Emit` also acquires `s.mu`). Also, emitting an event from `LoadTurns` changes the core contract of a previously silent state-mutation method. Rejected because the issue is localized to the TUI view model and should not bleed into the core session/loop contracts.

- **Alternative B (add a new `session.Event` type and route through `eventsCh`)**: Would require defining a new ingress event type in `session/event.go` and modifying the `eventsCh` goroutine in `tui.go` to intercept it and convert to a `tea.Msg`. Rejected because `session.Event` is semantically for events entering the inference pipeline (user messages, interrupts, session switches), not for UI notifications.

- **Selected path**: Add `reloadHistoryMsg` as a `tea.Msg` inside `x/conduit/tui/model.go`, handle it in `model.Update()`, and add `ReloadHistory([]state.Turn)` on `*TUI` in `tui.go`. This is the minimal change, keeps all concerns inside the TUI package, and follows the existing `PlayDone`/`PlayError` pattern for sending messages to the Bubble Tea program from outside the model's `Update` loop.

## Requirements

1. Introduce a new Bubble Tea message type (`reloadHistoryMsg`) carrying `[]state.Turn`.
2. Handle `reloadHistoryMsg` in `model.Update()` by resetting `m.turns`, re-calling `loadHistory()`, and refreshing the viewport.
3. Expose a `ReloadHistory([]state.Turn)` method on `*TUI` so downstream applications can trigger the reload after `stream.LoadTurns()`.
4. Preserve the user's scroll position (scroll to bottom only if the user was already at the bottom before the reload).
5. Add unit tests covering the reload message handling in the model.
6. Ensure the repository compiles and all existing tests pass after each task.

## Task Breakdown

### Task 1: Add `reloadHistoryMsg` and handler in `model.go`
- **Goal**: Define the `reloadHistoryMsg` type and wire it into `model.Update()` so the model can replace its turn history.
- **Dependencies**: None.
- **Files Affected**: `x/conduit/tui/model.go`
- **New Files**: None.
- **Interfaces**:
  - New type: `reloadHistoryMsg struct { turns []state.Turn }`
  - New branch in `model.Update(msg tea.Msg)`:
    ```go
    case reloadHistoryMsg:
        m.turns = nil
        m.currentTurn = renderedTurn{} // defensive: clear any partial turn
        m.pending = false              // defensive: reset pending state
        m.loadHistory(msg.turns)
        m.contentDirty = true
        wasAtBottom := m.viewport.AtBottom()
        m.syncViewport()
        if wasAtBottom {
            m.viewport.GotoBottom()
        }
    ```
- **Validation**: `go test ./x/conduit/tui/...` passes. `go build ./x/conduit/tui` compiles.
- **Details**: The `reloadHistoryMsg` type should be defined alongside the other `tea.Msg` types (`artifactMsg`, `turnMsg`, `statusMsg`, etc.) near the top of `model.go`. The handler in `Update()` must clear `m.turns` (set to `nil`) before calling `loadHistory` so the old turns are fully discarded. It should also defensively clear `m.currentTurn` and `m.pending` to avoid leaving stale streaming state from an interrupted turn. The scroll-preservation logic (`wasAtBottom`) should match the existing pattern used in `turnMsg` and `errorMsg` handling.

### Task 2: Add `ReloadHistory` public method on `TUI` in `tui.go`
- **Goal**: Expose a method on `*TUI` that sends a `reloadHistoryMsg` to the running Bubble Tea program.
- **Dependencies**: Task 1.
- **Files Affected**: `x/conduit/tui/tui.go`
- **New Files**: None.
- **Interfaces**:
  - New method on `*TUI`:
    ```go
    func (t *TUI) ReloadHistory(turns []state.Turn) error {
        t.program.Send(reloadHistoryMsg{turns: turns})
        return nil
    }
    ```
- **Validation**: `go test ./x/conduit/tui/...` passes. `go build ./x/conduit/tui` compiles.
- **Details**: This method mirrors `PlayDone` and `PlayError` which already use `t.program.Send(...)`. It follows the same pattern: a public method on `*TUI` that forwards a `tea.Msg` into the Bubble Tea program's message loop. This is the only way application-level code (e.g., a slash command handler or compaction processor) can trigger a history reload, because `t.program` is private. The method is idempotent and safe to call from any goroutine because `tea.Program.Send` is goroutine-safe.

### Task 3: Add unit tests for `reloadHistoryMsg` in `model_test.go`
- **Goal**: Verify that `reloadHistoryMsg` correctly replaces the model's turn history and refreshes the viewport.
- **Dependencies**: Task 1.
- **Files Affected**: `x/conduit/tui/model_test.go`
- **New Files**: None.
- **Interfaces**: No new interfaces; tests exercise existing `model.Update` signature.
- **Validation**: `go test ./x/conduit/tui/...` passes (including new tests).
- **Details**: Add three test cases:
  1. **`TestModel_Update_ReloadHistory`** — Start with a model containing two rendered turns, send a `reloadHistoryMsg` with a single replacement turn, assert that `m.turns` now has exactly 1 turn with the correct role and blocks.
  2. **`TestModel_Update_ReloadHistory_Empty`** — Start with a model containing turns, send a `reloadHistoryMsg` with an empty slice, assert `m.turns` is empty and `contentDirty` is true.
  3. **`TestModel_Update_ReloadHistory_PreservesScroll`** — Set the viewport to a known size, add enough turns to exceed the viewport height, scroll to the bottom, send `reloadHistoryMsg`, and assert the viewport remains at the bottom. Then scroll up, send `reloadHistoryMsg` again, and assert the viewport does not jump to the bottom.

  Use `newTestModel()` for the helper (already defined in `model_test.go`) and initialize the viewport with `viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))` as done in existing tests. For scroll preservation tests, manually populate `m.turns` with enough `renderedTurn` entries that exceed the viewport height before sending the message.

## Dependency Graph

- Task 1 → Task 2 (Task 2 references `reloadHistoryMsg` which is defined in Task 1)
- Task 1 → Task 3 (Task 3 tests the behavior added in Task 1)
- Task 2 || Task 3 (Task 2 and Task 3 are independent once Task 1 is complete; Task 2 is a tui.go change, Task 3 is a test change)

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| `reloadHistoryMsg` sent while a turn is in-flight (streaming) causes visual corruption because `m.currentTurn` is cleared mid-stream | Medium | Low | Task 1 includes defensive clearing of `m.currentTurn` and `m.pending`. Compaction is typically triggered after a turn completes, so this race is unlikely in practice. If it becomes a problem, the application can call `stream.Cancel()` before reloading. |
| `ReloadHistory` called before `Start()` causes a nil-pointer panic because `t.program` is nil | High | Medium | The current `PlayDone`/`PlayError` methods have the same risk (no nil guard). Task 2 follows the same pattern. If desired, a nil guard can be added as a follow-up, but it should be consistent across all three methods. |
| `loadHistory` re-renders Markdown for all turns, which could be expensive for very large histories after compaction | Medium | Low | Compaction is designed to reduce history size, so the post-compaction turn count should be small. The existing `loadHistory` already does this rendering once at startup. If performance becomes a concern, a future optimization could cache rendered blocks. |
| Tests for `ReloadHistory` on `*TUI` are difficult to write because `t.program` requires a running `tea.Program` | Low | High | Mitigated by testing `model.Update(reloadHistoryMsg{})` directly in `model_test.go` instead of trying to integration-test the `TUI.ReloadHistory` method. The `TUI` method is a thin wrapper around `t.program.Send` with no branching logic. |

## Validation Criteria

- [ ] `go test -race ./x/conduit/tui/...` passes with zero failures.
- [ ] `go test -race ./...` passes across the entire repository.
- [ ] `go build ./x/conduit/tui` and `go build ./examples/tui-chat` compile without errors.
- [ ] The new `reloadHistoryMsg` type is handled in `model.Update` with the exact semantics described: `m.turns` is cleared, `loadHistory` is re-invoked, `contentDirty` is set, and the viewport is synced with scroll preservation.
- [ ] `TUI.ReloadHistory` is a public method on `*TUI` that sends the message to `t.program`.
- [ ] Unit tests cover (a) replacement of existing turns, (b) empty reload, and (c) scroll position preservation.
