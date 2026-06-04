# Plan: Fix TUI Viewport Empty on Thread Resume

## Objective

Fix the bug where the TUI viewport displays no conversation history when resuming an existing thread via `--thread <uuid>`. The underlying thread state is correctly loaded into the `session.Stream`'s `state.Buffer`, but the Bubble Tea `model.turns` slice initializes empty and historical turns are never replayed into the viewport.

## Context

### Repository Topology

The relevant code spans three packages:

- **`session/`** — `Manager`, `Stream`, and `Thread` types. `Stream` wraps a `*Thread` whose `State *state.Buffer` holds the conversation history. `Manager.Attach()` loads a persisted thread from the `Store`, deserializing turns via `Thread.UnmarshalJSON` → `Buffer.LoadTurns()`.
- **`state/`** — `Buffer` is the in-memory `State` implementation with `Turns() []Turn` (defensive copy) and `Append()` for live mutation. `Turn` holds `Role`, `Artifacts []artifact.Artifact`, and `Timestamp`.
- **`x/conduit/tui/`** — The Bubble Tea TUI model (`model.go`) maintains `turns []renderedTurn` for viewport display. The `Start()` method in `tui.go` creates/attaches a `Stream`, initializes the `model`, subscribes to the stream's `FanOut`, and starts the Bubble Tea program. Live turns arrive via `artifactMsg`/`turnMsg` messages sent through `program.Send()`.

### Key Observations

1. **`Stream` has no `Turns()` accessor.** The `thread` field on `Stream` is unexported, so external packages (including `x/conduit/tui`) cannot read historical turns without a new public method.

2. **The `turnMsg` handler for assistant turns is incremental.** It finalizes `m.currentTurn.blocks` that were accumulated from prior `artifactMsg` delta events. Simply sending `turnMsg` for historical assistant turns would append an empty turn (no prior artifact deltas), so a direct pre-population approach is required.

3. **The `renderArtifact()` method already converts `artifact.Artifact` → `renderedBlock` for all known artifact kinds** (`Text`, `Reasoning`, `ToolCall`, `ToolResult`). It handles Markdown rendering, compact formatting, and style assignment. This logic can be reused for historical replay.

4. **The `WindowSizeMsg` handler re-renders assistant turns at the correct viewport width.** Pre-populating at width 0 is acceptable because `renderArtifact` falls back to raw source on glamour failure, and the first `WindowSizeMsg` re-renders assistant/tool blocks properly.

5. **Project conventions from `AGENTS.md`:** Prefer aggressive refactoring over backwards compatibility. The codebase has no production users. Keep package dependencies acyclic. Use table-driven tests. Run `go test -race ./...`.

## Architectural Blueprint

The fix is a two-part change with a clear dependency:

1. **Expose historical turns from `Stream`.** Add a read-only `Turns() []state.Turn` method to `session.Stream` that delegates to `s.thread.State.Turns()`. This is the minimal, cleanest API — the TUI cannot (and should not) reach into the unexported `thread` field.

2. **Pre-populate the TUI model from stream history.** In `x/conduit/tui/tui.go`'s `Start()`, after stream creation/attachment and before `tea.NewProgram(&m).Run()`, iterate over `stream.Turns()`. For each historical `state.Turn`, convert its artifacts into `renderedBlock`s using `model.renderArtifact()` with the same `shouldRenderMarkdown` logic used by the live `turnMsg` handler (assistant and tool roles get Markdown; user does not). Build a `renderedTurn` and append it to `m.turns`. Mark `m.contentDirty = true` so the viewport rebuilds on the first `syncViewport()` call.

### Why not send `turnMsg` via `program.Send()`?

The live `turnMsg` handler for `RoleAssistant` assumes `m.currentTurn` was built incrementally from `artifactMsg` delta events. For historical replay, there are no deltas — the full artifacts are already in `Turn.Artifacts`. Sending `turnMsg` would append an empty `renderedTurn` because `m.currentTurn.blocks` is empty. Direct pre-population avoids this impedance mismatch.

### Why not add a new message type (`historyMsg`)?

Over-engineering. Direct pre-population at init time is simpler, synchronous, and requires no changes to the `Update()` message dispatch logic. The model and TUI are in the same package, so unexported field access is allowed.

## Requirements

1. Resuming a thread via `--thread <uuid>` must display all prior conversation turns in the TUI viewport.
2. Historical turns must render with the same formatting (Markdown, block headers, compact/expanded states, timestamps) as live turns.
3. New sessions (no `--thread`) must continue to start with an empty viewport.
4. The fix must not break the incremental streaming model for live assistant turns.
5. The `Stream.Turns()` accessor must return a defensive copy (consistent with `state.Buffer.Turns()`).

## Task Breakdown

### Task 1: Add `Turns()` accessor to `session.Stream`

- **Goal**: Expose the stream's historical conversation turns as a read-only slice.
- **Dependencies**: None.
- **Files Affected**: `session/stream.go`
- **New Files**: None.
- **Interfaces**:
  ```go
  // Turns returns a defensive copy of the thread's turn history.
  func (s *Stream) Turns() []state.Turn
  ```
- **Validation**:
  - `go test -race ./session/...` passes.
  - New table-driven test in `session/stream_test.go` verifies:
    - `Turns()` returns empty slice for a newly created stream.
    - After `Process()` of a user message + assistant response, `Turns()` returns 2 turns with correct roles.
    - Modifying the returned slice does not affect the stream's internal state.
- **Details**: The method must acquire `s.mu` for read safety, then delegate to `s.thread.State.Turns()`. The `state.Buffer.Turns()` already returns a defensive copy of the slice, so no additional copying is needed beyond the mutex guard.

### Task 2: Pre-populate TUI model turns from stream history

- **Goal**: Populate `model.turns` with historical conversation state when the TUI attaches to an existing thread.
- **Dependencies**: Task 1 (needs `stream.Turns()`).
- **Files Affected**: `x/conduit/tui/tui.go`, `x/conduit/tui/model.go` (read-only, for reference)
- **New Files**: None.
- **Interfaces**: No new public interfaces. The change is internal to `TUI.Start()`.
- **Validation**:
  - `go test -race ./x/conduit/tui/...` passes.
  - New test in `x/conduit/tui/tui_test.go` verifies:
    - Create a stream, process a user + assistant exchange, close the stream.
    - Create a new TUI with `WithThreadID(stream.ID())`.
    - Start the TUI with a short timeout context.
    - Verify the TUI model's `turns` slice contains the historical turns after initialization (access via type assertion to `*TUI`, then inspect the model through the program before Run, or via a test-specific initialization helper).
  - The existing `TestStart_AttachNotFound` must still pass.
- **Details**: In `TUI.Start()`, after the `stream` is obtained (whether via `Attach` or `Create`), add the following block before `p.Run()`:

  ```go
  for _, turn := range stream.Turns() {
      var blocks []renderedBlock
      for _, art := range turn.Artifacts {
          block := m.renderArtifact(art, turn.Role, turn.Role == state.RoleAssistant || turn.Role == state.RoleTool)
          if block.kind != "" {
              blocks = append(blocks, block)
          }
      }
      m.turns = append(m.turns, renderedTurn{
          role:      turn.Role,
          blocks:    blocks,
          timestamp: turn.Timestamp,
      })
  }
  if len(m.turns) > 0 {
      m.contentDirty = true
  }
  ```

  Note: `m` and `TUI` are in the same package (`tui`), so `m.turns` (unexported) is accessible. The `shouldRenderMarkdown` parameter matches the live `turnMsg` handler logic exactly. The `contentDirty` flag ensures `buildContent()` rebuilds on the next `syncViewport()` call (triggered by the first `WindowSizeMsg`).

## Dependency Graph

- Task 1 → Task 2 (Task 2 calls `stream.Turns()`, introduced in Task 1)

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| `renderArtifact` with `viewport.Width() == 0` during init produces poor formatting or glamour render errors | Low | High | `renderArtifact` already handles render errors gracefully (leaves `rendered` empty, falls back to `block.source` in `buildContent`). The first `WindowSizeMsg` re-renders assistant and tool blocks at the correct width. This is an acceptable transient state. |
| Pre-populating `m.turns` interferes with the `currentTurn` / `pending` state machine for the first live turn | Medium | Low | Pre-populated turns go into `m.turns`, not `m.currentTurn`. `pending` starts false. The first user submission will set `pending = true` via `lifecycleMsg{"submitted"}`, and the subsequent assistant response will accumulate into `m.currentTurn` as normal. Add a test that verifies a live turn after historical pre-population works correctly. |
| Existing TUI tests break due to unanticipated state changes | Low | Low | All existing tests in `x/conduit/tui/` use fresh streams with no history (no `WithThreadID`), so the pre-population loop iterates over zero turns and has no effect. Run `go test -race ./x/conduit/tui/...` to confirm. |
| `Stream.Turns()` exposes mutable artifact slices because `state.Buffer.Turns()` does a shallow copy | Low | Medium | This is pre-existing behavior of `state.Buffer.Turns()` (documented in its comment: "shallow copy of the slice itself; the Artifacts slices within each Turn are not deep-copied"). The TUI only reads from the returned turns, so this is acceptable. Document the same contract on `Stream.Turns()`. |

## Validation Criteria

- [ ] `go test -race ./session/... ./x/conduit/tui/...` passes with zero failures.
- [ ] `go test ./...` passes with zero failures.
- [ ] New test `TestStream_Turns_ReturnsHistory` in `session/stream_test.go` passes.
- [ ] New test `TestStart_ResumesThreadWithHistory` in `x/conduit/tui/tui_test.go` passes.
- [ ] The reproduction steps from issue #328 produce visible conversation history:
  1. Create a thread, exchange some messages.
  2. Quit the TUI.
  3. Restart with `--thread <uuid>`.
  4. Observe: viewport displays the prior conversation turns.
