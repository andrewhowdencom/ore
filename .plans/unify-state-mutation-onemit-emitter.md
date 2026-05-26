# Plan: Unify state mutation and event emission via Step.OnEmit and Emitter

## Objective

Refactor the ore framework so that all observable state mutations flow through a single gateway: `Step.Emit`. Introduce a synchronous `OnEmit` callback tier that runs before the async FanOut, and replace `state.State` in `loop.Handler` with a new `loop.Emitter` interface. This fixes the architectural split where tool handlers mutate `state.Buffer` directly without emitting events, causing tool results to be invisible to TUI and HTTP conduits.

## Context

The codebase has two uncoordinated mutation paths:

1. **Event stream**: `Step.Emit` → `FanOut` → conduits. Async, droppable.
2. **State mutation**: `state.Append` → `state.Buffer`. Synchronous, blocking, used by providers.

In `loop/loop.go`, `finalizeTurn` appends the assistant turn to `state.State`, runs handlers, then emits `TurnCompleteEvent`. Handlers (specifically `x/tool/handler.go`) append `RoleTool` turns to `state.State` via `s.Append()`, but nothing broadcasts those additions. Since conduits are pure event consumers and never read `state.State` directly, tool results are invisible.

Key files and their roles:

- `loop/loop.go` — Defines `Step`, `finalizeTurn`, `Emit`, `TurnCompleteEvent`, `ArtifactEvent`, `ErrorEvent`, `ProcessCompleteEvent`. The core of the bug is here: `finalizeTurn` both appends to state directly and emits events after handlers.
- `loop/handler.go` — Defines `Handler` interface with `Handle(ctx, art, state.State)`.
- `loop/loop_test.go` — `mockHandler` implements `Handler`. Tests assert on `mem.Turns()` after `s.Turn()` and `s.Submit()`.
- `x/tool/handler.go` — Concrete `Handler` that looks up tools in a registry and appends `RoleTool` turns with `artifact.ToolResult`.
- `x/tool/handler_test.go` — Tests assert on `mem.Turns()` after `h.Handle()`.
- `cognitive/react_test.go` — `testHandler` implements `Handler`. `ReAct.Run` uses `Step.Turn` in a loop.
- `session/manager_test.go` — `stepFactory` returns `loop.New()`. Tests assert on `thr.State.Turns()` after `stream.Process()`.
- `examples/tui-chat/main.go` and `examples/http-chat/main.go` — `stepFactory` returns `loop.New()` for `session.Manager`.
- `examples/calculator/main.go`, `examples/filesystem/main.go`, `examples/single-turn-cli/main.go` — Create `loop.Step` directly with local `state.Buffer`.

Per `AGENTS.md`, this is an aggressive internal refactor with no backwards-compatibility commitment.

## Architectural Blueprint

### Selected Architecture

The refactor makes `Step.Emit` the single gateway for all observable mutations by adding a synchronous, ordered, zero-drop `OnEmit` callback tier:

1. **`OnEmit` callback** — `type OnEmit func(ctx context.Context, event OutputEvent)`. Registered via `WithOnEmit(...OnEmit) Option`. `Step.Emit` iterates all `OnEmit` callbacks synchronously before forwarding to the async `FanOut`.

2. **`Emitter` interface** — `type Emitter interface { Emit(ctx context.Context, event OutputEvent) }`. `*Step` satisfies this natively. Handlers receive an `Emitter` instead of `state.State` and emit `TurnCompleteEvent{Role: RoleTool, ...}` to produce tool results.

3. **State appender as OnEmit callback** — Wherever a `Step` is constructed alongside a `state.State` or `state.Buffer`, an `OnEmit` callback is wired that intercepts `TurnCompleteEvent` and calls `state.Append`. This is the sole code path that mutates persistent state.

4. **`finalizeTurn` refactor** — No longer calls `st.Append()` directly. Instead:
   - Build `turn := state.Turn{Role: role, Artifacts: artifacts}`
   - Emit `TurnCompleteEvent` for the turn **before** running handlers (preserves ordering: assistant turn first, then tool results)
   - Run handlers with `s` (the Step, acting as `Emitter`) instead of `st`
   - Any handler-emitted `TurnCompleteEvent` flows through the same `OnEmit` callbacks and async `FanOut`

### Why not smaller fixes

- Re-reading `st.Turns()` after handlers is redundant double-bookkeeping.
- Giving handlers access to `state.State` perpetuates the architectural split.
- The `FanOut` is intentionally lossy; state must be lossless. A separate synchronous tier (`OnEmit`) is the minimal clean separation.

### Why not an alternative architecture

- **Alternative: Handler returns artifacts instead of emitting** — Would require changing `Handler.Handle` signature to return `([]artifact.Artifact, error)`, then `finalizeTurn` appends returned artifacts. This is simpler but loses the event-stream visibility for intermediate handler actions (e.g., a handler wanting to emit a custom meta-event). The `Emitter` approach preserves extensibility.
- **Alternative: Give handlers access to both state and emitter** — Would not solve the architectural split; handlers would still bypass the event stream.

## Requirements

1. Add `OnEmit` callback type and `WithOnEmit` Option to `loop.Step`.
2. Add `Emitter` interface to `loop/` package.
3. Change `loop.Handler` to receive `Emitter` instead of `state.State`.
4. Modify `Step.Emit` to run `OnEmit` callbacks synchronously before async `FanOut`.
5. Modify `Step.finalizeTurn` to emit `TurnCompleteEvent` instead of directly appending to state; run handlers with `s` as `Emitter`.
6. Update `x/tool/handler.go` to emit `TurnCompleteEvent{Role: RoleTool, Artifacts: ...}` instead of `s.Append()`.
7. Wire state-appending `OnEmit` callback in all `loop.New()` calls that are used with a `state.State`.
8. Update all `mockHandler` / `testHandler` implementations across tests.
9. Update all tests that asserted on `mem.Turns()` after `h.Handle()` to assert on emitted `TurnCompleteEvent` instead.
10. `go test -race ./...` passes.
11. [inferred] TUI and HTTP conduits receive `TurnCompleteEvent` for tool results and display them.

## Task Breakdown

### Task 1: Add OnEmit and Emitter to loop.Step and update tool handler
- **Goal**: Introduce the synchronous OnEmit callback tier and Emitter interface in `loop/`, change the `Handler` contract, and update the primary handler implementation (`x/tool/`) and its tests.
- **Dependencies**: None
- **Files Affected**:
  - `loop/loop.go`
  - `loop/handler.go`
  - `loop/loop_test.go`
  - `x/tool/handler.go`
  - `x/tool/handler_test.go`
- **New Files**: None
- **Interfaces**:
  ```go
  // In loop/loop.go
  type OnEmit func(ctx context.Context, event OutputEvent)

  // In loop/handler.go
  type Emitter interface {
      Emit(ctx context.Context, event OutputEvent)
  }

  type Handler interface {
      Handle(ctx context.Context, art artifact.Artifact, e Emitter) error
  }
  ```
  New Option:
  ```go
  func WithOnEmit(fns ...OnEmit) Option
  ```
- **Details**:
  - In `loop/loop.go`:
    - Add `onEmit []OnEmit` field to `Step` struct.
    - Implement `WithOnEmit` Option.
    - Modify `Emit`: iterate `s.onEmit` synchronously (in registration order) before creating and sending the `outputEventEnvelope` to `s.events`.
    - Modify `finalizeTurn`:
      - Remove `st.Append(role, artifacts...)`.
      - Build `turn := state.Turn{Role: role, Artifacts: artifacts}`.
      - Call `s.Emit(ctx, TurnCompleteEvent{Turn: turn, Ctx: s.eventContext})` **before** running handlers.
      - In the handler loop, call `h.Handle(ctx, art, s)` instead of `h.Handle(ctx, art, st)`. `s` satisfies `Emitter`.
      - Return `st` (the same state object, now mutated by the OnEmit callback).
  - In `loop/handler.go`:
    - Define `Emitter` interface.
    - Change `Handler.Handle` signature to receive `Emitter` instead of `state.State`.
  - In `loop/loop_test.go`:
    - Update `mockHandler` struct and `Handle` method to implement the new `Handler` interface.
    - Add a test helper `stepWithState(st state.State, opts ...Option) *Step` that appends a `WithOnEmit` Option: the callback intercepts `TurnCompleteEvent` and calls `st.Append(tc.Turn.Role, tc.Turn.Artifacts...)`.
    - Replace every `New(...)` call that uses a `mem *state.Buffer` with `stepWithState(mem, ...)`.
    - Update `TestStep_Turn_HandlerAppendsToolResult`: the mock handler `fn` must call `e.Emit(ctx, TurnCompleteEvent{Turn: state.Turn{Role: state.RoleTool, Artifacts: []artifact.Artifact{artifact.Text{Content: "tool result"}}}})` instead of `s.Append(...)`. The test still asserts `result.Turns()` has 3 turns (the OnEmit callback appended both the assistant turn and the tool turn).
    - Update `TestStep_Turn_OutputEventsWithHandler`: the `turn_complete` subscriber must now receive **2** events (assistant + tool), not 1. This is the acceptance test for the bug fix.
    - Update all other handler tests (`TestStep_Turn_Handler`, `TestStep_Turn_HandlerError`, `TestStep_Submit_Handler`, etc.) to use the new mock handler and `stepWithState`.
  - In `x/tool/handler.go`:
    - Change `Handle` signature: `func (h *Handler) Handle(ctx context.Context, art artifact.Artifact, e loop.Emitter) error`.
    - Replace every `s.Append(state.RoleTool, artifact.ToolResult{...})` with `e.Emit(ctx, loop.TurnCompleteEvent{Turn: state.Turn{Role: state.RoleTool, Artifacts: []artifact.Artifact{artifact.ToolResult{...}}}})`.
    - Update `var _ loop.Handler = (*Handler)(nil)`.
  - In `x/tool/handler_test.go`:
    - Create a `mockEmitter` that records all emitted `OutputEvent`s in a slice.
    - In every test, pass a `mockEmitter` to `h.Handle` instead of a `state.Buffer`.
    - Assert on `emitter.events` instead of `mem.Turns()`. Each test that previously expected a `RoleTool` turn should now verify exactly one `TurnCompleteEvent` with `Role == state.RoleTool` and one `ToolResult` artifact inside.
- **Validation**: `go test ./loop/... && go test ./x/tool/... && go build ./...`

### Task 2: Update all remaining consumers to wire OnEmit callbacks
- **Goal**: Wire state-appending `OnEmit` callbacks in all remaining code that constructs `loop.Step` alongside a `state.State` or `state.Buffer`, and update remaining `Handler` mock implementations.
- **Dependencies**: Task 1
- **Files Affected**:
  - `cognitive/react_test.go`
  - `session/manager_test.go`
  - `examples/tui-chat/main.go`
  - `examples/http-chat/main.go`
  - `examples/calculator/main.go`
  - `examples/filesystem/main.go`
  - `examples/single-turn-cli/main.go`
- **New Files**: None
- **Details**:
  - `cognitive/react_test.go`:
    - Update `testHandler` to implement new `loop.Handler` (receive `Emitter`).
    - In every test that creates `loop.New()`, add `loop.WithOnEmit` that appends `TurnCompleteEvent` to the local `mem *state.Buffer`.
    - Update `toolHandler` `fn` in `TestReAct_ToolLoop` to call `e.Emit(ctx, TurnCompleteEvent{...})` instead of `s.Append(...)`.
  - `session/manager_test.go`:
    - Update every `stepFactory` closure (`func(*thread.Thread) (*loop.Step, error) { return loop.New(), nil }`) to:
      ```go
      func(thr *thread.Thread) (*loop.Step, error) {
          return loop.New(loop.WithOnEmit(func(ctx context.Context, event loop.OutputEvent) {
              if tc, ok := event.(loop.TurnCompleteEvent); ok {
                  thr.State.Append(tc.Turn.Role, tc.Turn.Artifacts...)
              }
          })), nil
      }
      ```
    - No other test logic changes; the same assertions on `thr.State.Turns()` remain valid because the OnEmit callback appends in the same order.
  - `examples/tui-chat/main.go`:
    - Update `stepFactory` to wire `WithOnEmit` appending to `thr.State`.
  - `examples/http-chat/main.go`:
    - Update `stepFactory` to wire `WithOnEmit` appending to `thr.State`, alongside existing `WithHandlers` and `WithInvokeOptions`.
  - `examples/calculator/main.go`:
    - Update `loop.New(...)` to include `loop.WithOnEmit` appending to local `mem`.
  - `examples/filesystem/main.go`:
    - Update `loop.New(...)` to include `loop.WithOnEmit` appending to local `mem`.
  - `examples/single-turn-cli/main.go`:
    - Update `loop.New(stepOpts...)` to include `loop.WithOnEmit` appending to local `mem`.
- **Validation**: `go test ./... && go build ./...`

### Task 3: Run full validation suite
- **Goal**: Ensure no regressions with race detector and verify the bug is fixed end-to-end.
- **Dependencies**: Task 2
- **Files Affected**: None (validation only)
- **New Files**: None
- **Details**:
  - Run `go test -race ./...` and confirm it passes.
  - Confirm `loop/loop_test.go` `TestStep_Turn_OutputEventsWithHandler` receives exactly 2 `turn_complete` events when a handler emits a tool result.
  - Confirm `x/tool/handler_test.go` no longer contains any assertions on `mem.Turns()`; all assertions are on emitted events.
  - Confirm no `s.Append()` calls remain in `x/tool/handler.go`.
  - Confirm no direct `st.Append()` calls remain in `loop.finalizeTurn`.
- **Validation**: `go test -race ./...` passes cleanly.

## Dependency Graph

- Task 1 → Task 2 (Task 2 depends on Task 1)
- Task 2 → Task 3 (Task 3 depends on Task 2)

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Missed `loop.New()` call that needs `WithOnEmit` wired | High (tests fail or panic at runtime) | Medium | Systematic `grep -r 'loop.New(' --include='*.go' .` to find and update all occurrences. |
| TUI/HTTP conduit rendering breaks with additional `TurnCompleteEvent`s | Medium | Low | TUI and HTTP already render `TurnCompleteEvent` by inspecting `Turn.Role` and `Turn.Artifacts`; tool turns are structurally identical to user/assistant turns. Verify with manual integration test if needed. |
| Existing tests silently pass with empty state because OnEmit is not wired | High | Medium | `grep -r 'mem.Turns()' --include='*_test.go'` to find all state assertions; ensure every corresponding `loop.New()` has `WithOnEmit`. |
| OnEmit callback ordering: handlers emit after assistant turn, which is correct | Low | Low | The design explicitly emits the original turn BEFORE running handlers, matching current append-then-handlers semantics. |

## Validation Criteria

- [ ] `go test -race ./...` passes.
- [ ] `loop/loop_test.go` `TestStep_Turn_OutputEventsWithHandler` asserts exactly 2 `turn_complete` events (assistant + tool) on the subscription channel.
- [ ] `x/tool/handler_test.go` contains zero assertions on `state.Buffer.Turns()`; all tests assert on `mockEmitter.events`.
- [ ] `x/tool/handler.go` contains zero calls to `state.State.Append` (or `state.Buffer.Append`).
- [ ] `loop/loop.go` `finalizeTurn` contains zero calls to `st.Append`.
- [ ] All `examples/` build successfully (`go build ./...`).
- [ ] All `loop.New()` calls across the entire repository that are used with a `state.State` or `state.Buffer` include a `loop.WithOnEmit` callback that appends `TurnCompleteEvent` to state.
