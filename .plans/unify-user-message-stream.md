# Plan: Unify User Message Stream

## Objective

Eliminate the dual-path architecture where user messages bypass the artifact stream entirely. Currently, assistant turns flow through `loop.Step.Turn()` → `FanOut` → subscribers, while user turns take an ad-hoc direct route via `mem.Append()` and TUI model mutation. This plan introduces `Step.Submit()` as a canonical mechanism for non-inference turns, unifying both user and assistant content through the same event stream so all subscribers (TUI, loggers, middleware) see the complete conversation in chronological order.

## Context

The ore framework (`github.com/andrewhowdencom/ore`) is a minimal provider-agnostic framework for agentic applications. Key packages:

- `artifact/` — defines the `Artifact` interface (`Kind() string`) and common types (`Text`, `ToolCall`, `TextDelta`, `ReasoningDelta`, etc.). No role context on artifacts by design.
- `state/` — defines `State` interface with `Turns() []Turn` and `Append(role Role, artifacts ...Artifact)`. `state.Memory` is the in-memory implementation.
- `provider/` — minimal `Invoke(ctx, State, chan<- Artifact, ...InvokeOption) error` contract.
- `loop/` — `Step` orchestrates inference turns: `Turn(ctx, State, Provider) (State, error)`. Contains an embedded `FanOut` that distributes `OutputEvent` values to subscribers by kind.
- `surface/tui/` — Bubble Tea TUI surface that subscribes to the loop's `FanOut` and renders deltas/turns.
- `examples/tui-chat/` — reference application demonstrating the dual-path problem.

### Current dual-path architecture

**Assistant path (canonical, via artifact stream):**
`loop.Step.Turn()` → provider emits `Artifact` deltas to `provCh` → accumulate into blocks → `events` channel → `FanOut` → TUI renders via `deltaMsg`/`turnMsg` → `st.Append(RoleAssistant, ...)` → handlers run → `TurnCompleteEvent` emitted.

**User path (ad-hoc, direct):**
TUI `KeyEnter` → **directly mutates** `m.turns` with `renderedTurn{role: RoleUser}` AND emits `surface.UserMessageEvent` → app goroutine → **directly calls** `mem.Append(RoleUser, ...)` → then invokes `react.Run()` which triggers assistant `Turn()`.

### Observed during discovery

- `loop/loop.go` — `Step.Turn()` has the post-append pipeline (append → handlers → `TurnCompleteEvent` emission) embedded directly after provider accumulation. No shared method.
- `surface/tui/model.go` — `KeyEnter` contains the direct `m.turns` mutation for user turns. The `turnMsg` handler already uses `msg.turn.Role` and correctly renders user turns (confirmed by `TestModel_Update_Turn_User_LeavesRenderedEmpty`).
- `examples/tui-chat/main.go` — `mem.Append(state.RoleUser, ...)` is called directly in the event goroutine, bypassing the loop entirely.
- `examples/single-turn-cli/main.go` and `examples/calculator/main.go` — also use `mem.Append(RoleUser, ...)` directly, but these are non-interactive CLI examples with no artifact stream subscribers. They are out of scope for this plan; `Submit()` will be available for future use but no changes are required.
- `cognitive/react.go` — `ReAct.Run()` only calls `Step.Turn()`. No changes needed; the example app will call `Step.Submit()` directly via `react.Step.Submit()`.

### Design convergence from ideation

| Decision | Resolution |
|---|---|
| New artifact type for user content? | No — reuse `artifact.Text`. Role conveyed via `TurnCompleteEvent.Turn.Role`. |
| Role through artifact channel? | `TurnCompleteEvent` already carries `state.Turn` with `Role`. No channel contract changes. |
| TUI direct mutation? | Remove it. Render user turns exclusively from `FanOut` `turnMsg` events, accepting one channel-hop latency. |
| Loop API naming? | `Turn()` stays for inference turns. New `Submit()` for non-inference turns. |
| beforeTurns / handlers on Submit? | Yes — run both, same as `Turn()`. Handlers can inspect `st.Turns()` to determine role if needed. |

## Architectural Blueprint

The unified architecture restructures the loop so that **all** observable state mutation flows through `Step`, not ad-hoc application code.

```
User types message
       │
       ▼
TUI KeyEnter ──► resets input, emits surface.UserMessageEvent
       │                          (NO direct m.turns mutation)
       ▼
App goroutine receives UserMessageEvent
       │
       ▼
react.Step.Submit(ctx, mem, RoleUser, Text{...})
       │
       ├──► beforeTurns hooks
       ├──► st.Append(RoleUser, ...)
       ├──► handlers
       └──► events channel ──► FanOut ──► turnMsg ──► TUI renders "You: ..."
       │
       ▼
react.Run(ctx, mem)
       │
       ├──► Step.Turn(ctx, mem, Provider)
       │       ├──► beforeTurns hooks
       │       ├──► provider.Invoke()
       │       ├──► deltas ──► FanOut ──► deltaMsg ──► TUI streams
       │       ├──► accumulate into blocks
       │       ├──► st.Append(RoleAssistant, ...)
       │       ├──► handlers
       │       └──► events channel ──► FanOut ──► turnMsg ──► TUI renders "Assistant: ..."
       │
       └──► loops while last turn != assistant (tool results)
```

### Key implementation strategy

1. **Extract shared pipeline**: `Turn()` currently has the post-append logic (handlers + `TurnCompleteEvent`) inline. Extract it to a private `finalizeTurn()` so both `Turn()` and `Submit()` share identical emission and handler execution.
2. **Add `Submit()`**: Runs `beforeTurns` hooks, then calls `finalizeTurn()` with caller-provided role and artifacts. Emits `TurnCompleteEvent` to the same `FanOut` as `Turn()`.
3. **Remove TUI direct mutation**: The TUI's `turnMsg` handler already supports any role. Removing the direct `m.turns` append in `KeyEnter` makes the TUI render user turns exclusively from the loop's `FanOut`.
4. **Wire example app**: Replace `mem.Append()` with `react.Step.Submit()`.

## Requirements

1. `Step.Submit(ctx, State, Role, ...Artifact)` appends a turn to state, runs handlers, and emits `TurnCompleteEvent` to all `FanOut` subscribers.
2. `Step.Turn()` continues to work identically for inference turns; no behavioral regression.
3. `Step.Submit()` runs the same `beforeTurns` hooks and `handlers` as `Step.Turn()`.
4. TUI `KeyEnter` no longer directly mutates `m.turns`. User turns render through the `turnMsg` path from `FanOut`.
5. `examples/tui-chat/main.go` uses `react.Step.Submit()` instead of `mem.Append()` for user messages.
6. All existing tests pass; new tests cover `Submit()` behavior and TUI `KeyEnter` change.
7. `go test -race ./...` passes after all changes.

## Task Breakdown

### Task 1: Extract shared turn-finalization pipeline from Step.Turn()
- **Goal**: Refactor `Step.Turn()` to extract the "append to state → run handlers → emit `TurnCompleteEvent`" logic into a private `finalizeTurn()` method, with zero behavioral change.
- **Dependencies**: None
- **Files Affected**: `loop/loop.go`
- **New Files**: None
- **Interfaces**: New private method: `func (s *Step) finalizeTurn(ctx context.Context, st state.State, role state.Role, artifacts []artifact.Artifact) (state.State, error)`
- **Validation**: `go test ./loop/...` passes with no test changes. All existing `Turn()` tests confirm identical behavior.
- **Details**: Extract the block starting from `st.Append(state.RoleAssistant, accumulatedArtifacts...)` through the `TurnCompleteEvent` emission into `finalizeTurn()`. `Turn()` calls this method after provider delta accumulation. The defensive `if last.Role != role` check should be preserved using the passed role parameter instead of hardcoded `RoleAssistant`.

### Task 2: Add Step.Submit() for non-inference turns
- **Goal**: Add a public `Submit()` method that runs `beforeTurns` hooks, appends a turn with caller-provided role, runs handlers, and emits `TurnCompleteEvent` via the shared `finalizeTurn()` pipeline.
- **Dependencies**: Task 1
- **Files Affected**: `loop/loop.go`, `loop/loop_test.go`
- **New Files**: None
- **Interfaces**: `func (s *Step) Submit(ctx context.Context, st state.State, role state.Role, artifacts ...artifact.Artifact) (state.State, error)`
- **Validation**: `go test ./loop/...` passes, including new tests covering:
  - `Submit` appends turn with correct role
  - `Submit` emits `TurnCompleteEvent` to `FanOut` subscribers
  - `Submit` runs `beforeTurns` hooks and propagates their errors
  - `Submit` runs handlers and propagates their errors
  - Multiple sequential `Submit` calls produce chronologically ordered events
  - `Submit` followed by `Turn` produces ordered events (user turn before assistant turn)
- **Details**: Implement `Submit()` by iterating `s.beforeTurns` (same pattern as `Turn()`), then calling `s.finalizeTurn()`. Add tests mirroring the existing `Turn()` test patterns. Use `mockBeforeTurn`, `mockHandler`, and `collectEvents` already defined in `loop_test.go`.

### Task 3: Remove direct TUI m.turns mutation on KeyEnter
- **Goal**: Stop the TUI from directly appending a user turn to `m.turns` when `KeyEnter` is pressed. User turns must render exclusively through the `turnMsg` path from the loop's `FanOut`.
- **Dependencies**: Task 2
- **Files Affected**: `surface/tui/model.go`, `surface/tui/model_test.go`
- **New Files**: None
- **Interfaces**: None new; behavior change in `model.Update()` `KeyEnter` handling
- **Validation**: `go test ./surface/tui/...` passes. `TestModel_Update_KeyEnter_WithInput` updated to assert `mm.turns` is empty after `KeyEnter` (since no `turnMsg` has arrived yet), while still asserting `eventsCh` receives the `UserMessageEvent` and `m.input` is reset.
- **Details**: In `model.go`, remove the `m.turns = append(m.turns, renderedTurn{role: state.RoleUser, ...})` block from `KeyEnter`. The `m.eventsCh <- surface.UserMessageEvent{Content: content}` emission stays. In `model_test.go`, update `TestModel_Update_KeyEnter_WithInput` to remove assertions on `mm.turns` length and content after `KeyEnter`; instead assert `mm.turns` is empty and the event was emitted. The existing `TestModel_Update_Turn_User_LeavesRenderedEmpty` already validates that `turnMsg` with `RoleUser` renders correctly.

### Task 4: Update tui-chat example to use Step.Submit()
- **Goal**: Replace the ad-hoc `mem.Append(state.RoleUser, ...)` call in `examples/tui-chat/main.go` with `react.Step.Submit()` to route user turns through the loop infrastructure.
- **Dependencies**: Task 2
- **Files Affected**: `examples/tui-chat/main.go`
- **New Files**: None
- **Interfaces**: None new; usage of `react.Step.Submit()`
- **Validation**: `go build ./examples/tui-chat/...` compiles.
- **Details**: Replace:
  ```go
  mem.Append(state.RoleUser, artifact.Text{Content: e.Content})
  ```
  with:
  ```go
  var err error
  mem, err = react.Step.Submit(ctx, mem, state.RoleUser, artifact.Text{Content: e.Content})
  if err != nil {
      slog.Error("submit failed", "err", err)
      continue
  }
  ```
  Keep the existing `s.SetStatus(ctx, "thinking...")` call after the submit. Add proper error handling — on `Submit` error, log and `continue` (skip the assistant turn).

### Task 5: Full integration verification
- **Goal**: Verify the entire codebase compiles and passes all tests with race detection after all prior tasks.
- **Dependencies**: Task 3, Task 4
- **Files Affected**: None (verification only)
- **New Files**: None
- **Validation**:
  - `go test -race ./...` passes
  - `go build ./...` passes
  - `go vet ./...` clean
- **Details**: Run the full test suite with race detection. Verify all packages compile. Confirm no other examples or consumers are broken by the `loop` changes. Check that the `single-turn-cli` and `calculator` examples still compile (they use `mem.Append()` directly, which is still valid; no changes needed).

## Dependency Graph

```
Task 1 ──► Task 2 ──► Task 3 ──► Task 5
            │
            └──► Task 4 ──► Task 5
```

- Task 1 → Task 2: `Submit()` depends on the extracted `finalizeTurn()` shared pipeline.
- Task 2 → Task 3: TUI removes direct mutation only after `Submit()` can provide user turns through `FanOut`.
- Task 2 → Task 4: Example app calls `Submit()` which must exist.
- Task 3 || Task 4: Parallelizable after Task 2 completes.
- (Task 3, Task 4) → Task 5: Integration verification needs both the TUI and example changes in place.

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| `beforeTurns` hook that assumes provider invocation runs on user `Submit()` and fails unexpectedly | Medium | Medium | Mitigated by requirement #3 (run same hooks on both). Hooks are already expected to be idempotent/state-transforming. Document that `beforeTurns` runs before *any* turn. If a hook is provider-specific, it should inspect state to determine context. |
| Handler that mutates state (e.g., appends tool results) runs on user turns and produces unexpected state | Medium | Low | Existing handlers already receive `st state.State` and can inspect `st.Turns()` to determine the last turn's role. This is not new behavior — `Turn()` already runs handlers, and handlers can already append to state. |
| TUI latency for user message display feels sluggish | Low | Low | One in-memory channel hop (microseconds). Acceptable per ideation consensus. Mitigation: if latency is problematic in practice, a future optimization could add optimistic rendering, but that reintroduces dual-path and should be avoided. |
| Backward compatibility: existing consumers of `Step.Turn()` broken by refactor | Medium | Low | `Turn()` signature and behavior unchanged. Pure internal refactor to `finalizeTurn()`. Mitigation: extensive existing test coverage in `loop_test.go` validates no regression. |
| Test coverage gap for `Submit()` edge cases (empty artifacts, multiple roles, error paths) | Medium | Low | Mitigation: Task 2 validation requires comprehensive tests mirroring `Turn()` test patterns. Table-driven tests for error propagation, event ordering, and role variations. |

## Validation Criteria

- [ ] `go test -race ./loop/...` passes after Task 1 (pure refactor, no regressions)
- [ ] `go test -race ./loop/...` passes after Task 2, including new `Submit()` tests
- [ ] `go test -race ./surface/tui/...` passes after Task 3, with updated `KeyEnter` test
- [ ] `go build ./examples/tui-chat/...` succeeds after Task 4
- [ ] `go test -race ./...` passes after Task 5
- [ ] `go build ./...` passes after Task 5
- [ ] `go vet ./...` clean after Task 5
- [ ] User turns appear in the TUI through `turnMsg` from `FanOut`, not via direct `model.turns` mutation
- [ ] The artifact stream (`events` channel / `FanOut`) contains both user and assistant `TurnCompleteEvent`s in chronological order
- [ ] State persistence of user messages in the example app happens through `react.Step.Submit()`, not `mem.Append()`
