# Plan: Fix Session Manager Double OnEmit Append Corrupting Thread State

## Objective

Eliminate the redundant `defaultOnEmit` callback in `session.Manager` that causes conversation turns to be appended to `*state.Buffer` multiple times. Move state persistence into `loop.Step.Emit` via a declarative `WithState` option so all `TurnCompleteEvent` emissions (from `finalizeTurn`, tool handlers, and any future callers) are persisted exactly once without relying on a callback registered by `session.Manager`.

## Context

### Root Cause

In `session/manager.go`, `Create()`, `Attach()`, and `CreateWithID()` each register a `defaultOnEmit` callback:

```go
defaultOnEmit := loop.WithOnEmit(func(ctx context.Context, event loop.OutputEvent) {
    if tc, ok := event.(loop.TurnCompleteEvent); ok {
        stream.thread.State.Append(tc.Turn.Role, tc.Turn.Artifacts...)
    }
})
```

This callback was introduced in commit `680f569` ("Make Thread passive and consolidate Stream API") to replace direct `st.Append` calls that were removed from `loop.Step.finalizeTurn` during the earlier `OnEmit` refactor (commit `2367d96`).

The problem: this callback is the **sole** mechanism for persisting turns to thread state, yet it is registered as an `OnEmit` callback inside `session.Manager` rather than being a core property of `loop.Step`. When `Step.Turn` or `Step.Submit` calls `finalizeTurn`, the `TurnCompleteEvent` is emitted, triggering `defaultOnEmit` — which appends to `stream.thread.State`. Tool handlers (e.g., `x/tool/handler.go`) also emit `TurnCompleteEvent` via `Emitter.Emit`, which triggers the same callback. Because the callback appends to the shared `*state.Buffer` every time `Emit` runs, any scenario where an event flows through `Emit` more than expected produces duplicate turns, garbled conversation history, and corrupted JSON store writes.

### Relevant Files

- `session/manager.go` — contains the three `defaultOnEmit` definitions (lines ~102–108, ~146–152, ~227–233)
- `loop/loop.go` — contains `Step.Emit`, `Step.finalizeTurn`, `Step.Turn`, `Step.Submit`, and `WithOnEmit`
- `x/tool/handler.go` — emits `TurnCompleteEvent` via `Emitter.Emit` for tool results
- `session/manager_test.go` — existing tests that assert turn counts after processing
- `loop/loop_test.go` — tests for `Step` behavior
- `cognitive/react_test.go` — tests for ReAct loop that exercise tool-call paths

### Architectural Decision

Instead of reverting `finalizeTurn` to call `st.Append` directly (which would leave tool-handler emissions unpersisted, breaking ReAct tool loops), the fix introduces a `WithState(state.State)` option on `loop.Step`. When configured, `Step.Emit` automatically appends `TurnCompleteEvent` to the configured state. This is a declarative, callback-free mechanism that handles **all** `TurnCompleteEvent` emissions uniformly — whether they originate from `finalizeTurn`, tool handlers, or any future artifact handler.

This keeps the `OnEmit` callback tier available for secondary concerns (logging, metrics, side effects) while making state persistence a first-class property of `Step`.

## Requirements

1. Remove `defaultOnEmit` from `session.Manager.Create()`, `Attach()`, and `CreateWithID()`.
2. Ensure `TurnCompleteEvent` emissions are persisted to state exactly once, covering both `finalizeTurn` and tool-handler paths.
3. Do not break existing `loop.Step` tests that create `Step` without a state binding.
4. Add a regression test that asserts no duplicate turns after a full `Process` cycle.
5. All existing tests (`go test -race ./...`) must pass after the change.

## Task Breakdown

### Task 1: Add `WithState` option and auto-append to `loop.Step.Emit`

- **Goal**: Introduce a `WithState` functional option on `loop.Step` and wire automatic `TurnCompleteEvent` appending inside `Emit`.
- **Dependencies**: None.
- **Files Affected**: `loop/loop.go`
- **New Files**: None.
- **Interfaces**:
  - New exported option: `func WithState(st state.State) Option`
  - `Step` gains unexported field: `state state.State`
  - `Emit` behavior change: when `event` is a `TurnCompleteEvent` and `s.state != nil`, call `s.state.Append(tc.Turn.Role, tc.Turn.Artifacts...)` before running `OnEmit` callbacks.
- **Validation**:
  - `go test ./loop/...` passes.
  - No compiler errors in packages depending on `loop`.
- **Details**:
  - Add `state state.State` field to `Step` struct.
  - Implement `WithState(st state.State) Option` that assigns the field.
  - In `Emit`, insert the auto-append logic before the `OnEmit` loop:
    ```go
    if tc, ok := event.(TurnCompleteEvent); ok && s.state != nil {
        s.state.Append(tc.Turn.Role, tc.Turn.Artifacts...)
    }
    ```
  - Ensure the append happens **before** `OnEmit` callbacks so that state is already mutated when callbacks run (preserving current ordering semantics).

### Task 2: Replace `defaultOnEmit` with `WithState` in `session.Manager`

- **Goal**: Remove the redundant `defaultOnEmit` callback from `Create()`, `Attach()`, and `CreateWithID()`, replacing it with `loop.WithState(stream.thread.State)`.
- **Dependencies**: Task 1 (must have `WithState` available).
- **Files Affected**: `session/manager.go`
- **New Files**: None.
- **Interfaces**: None new; removes the `defaultOnEmit` local variable and `loop.WithOnEmit` usage in the three methods.
- **Validation**:
  - `go build ./session/...` passes.
  - `go test ./session/...` passes.
- **Details**:
  - In `Create()`, remove the `defaultOnEmit` variable and change:
    ```go
    stream.step = loop.New(append([]loop.Option{defaultOnEmit}, factoryOpts...)...)
    ```
    to:
    ```go
    stream.step = loop.New(append([]loop.Option{loop.WithState(stream.thread.State)}, factoryOpts...)...)
    ```
  - Apply the identical change in `Attach()` and `CreateWithID()`.
  - Verify no other references to `defaultOnEmit` remain in `session/manager.go`.

### Task 3: Update tests and add regression test

- **Goal**: Ensure tests reflect the new behavior and add a regression test that asserts no duplicate turns.
- **Dependencies**: Task 2.
- **Files Affected**: `session/manager_test.go`
- **New Files**: None.
- **Interfaces**: None.
- **Validation**:
  - `go test -race ./session/...` passes.
  - Regression test explicitly fails if run against pre-fix code (by duplicating the `defaultOnEmit` callback manually).
- **Details**:
  - Review existing tests (e.g., `TestManager_Process`, `TestStream_Process_Queued`) — they should continue to pass because turn counts remain correct (user + assistant turns are appended once each).
  - Add `TestManager_Process_NoDuplicateTurns`:
    ```go
    func TestManager_Process_NoDuplicateTurns(t *testing.T) {
        prov := &mockProvider{
            artifacts: []artifact.Artifact{
                artifact.TextDelta{Content: "Hello"},
                artifact.TextDelta{Content: " world"},
            },
        }
        store := NewMemoryStore()
        mgr := NewManager(store, prov, func(stream *Stream) ([]loop.Option, error) {
            return nil, nil
        }, simpleProcessor())

        stream, err := mgr.Create()
        require.NoError(t, err)

        err = stream.Process(context.Background(), UserMessageEvent{Content: "hi"})
        require.NoError(t, err)
        _ = stream.Close()

        thr, ok := store.Get(stream.ID())
        require.True(t, ok)
        turns := thr.State.Turns()

        // Exactly 2 turns: user message + assistant response
        require.Len(t, turns, 2)

        // Verify no role appears more times than expected
        roleCounts := make(map[state.Role]int)
        for _, turn := range turns {
            roleCounts[turn.Role]++
        }
        assert.Equal(t, 1, roleCounts[state.RoleUser], "user turn should appear exactly once")
        assert.Equal(t, 1, roleCounts[state.RoleAssistant], "assistant turn should appear exactly once")
    }
    ```
  - If any existing test manually counts turns and the count changes, adjust expectations to match the corrected single-append behavior.

### Task 4: Full test suite and integration validation

- **Goal**: Run the full test suite to catch any cross-package regressions.
- **Dependencies**: Task 3.
- **Files Affected**: None (validation only).
- **New Files**: None.
- **Interfaces**: None.
- **Validation**:
  - `go test -race ./...` passes.
  - `go vet ./...` is clean.
- **Details**:
  - Execute `go test -race ./...` from the repository root.
  - Pay special attention to:
    - `loop/` — core `Step` behavior
    - `session/` — manager and stream tests
    - `cognitive/` — ReAct loop tests that exercise tool-call paths
    - `x/tool/` — tool handler tests
    - `examples/` — example apps compile successfully (`go build ./examples/...`)
  - If any test fails, debug whether the failure is due to:
    - Tests that assumed `OnEmit` was the only persistence mechanism and now need adjustment
    - Tool handler tests that relied on `defaultOnEmit` being present

## Dependency Graph

- Task 1 → Task 2 (Task 2 depends on `WithState` being available)
- Task 2 → Task 3 (tests depend on the manager change)
- Task 3 → Task 4 (full suite depends on all tests being updated)

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Tool-handler `TurnCompleteEvent` emissions lose persistence because they go through `Emit` but not `finalizeTurn` | High | High (certain without mitigation) | Mitigated by Task 1: `Emit` auto-appends when `WithState` is configured, covering tool-handler paths |
| Existing tests outside `session/` create `Step` with manual `OnEmit` callbacks that append to state; adding `WithState` + auto-append causes double append in those tests | Medium | Low | Those tests don't set `WithState`, so auto-append is a no-op. Manual `OnEmit` callbacks continue to work. Verify in Task 4. |
| Examples or `cmd/` packages construct `loop.Step` directly (not through `session.Manager`) and rely on manual `OnEmit` for persistence | Medium | Low | Examples using `session.Manager` are fixed by Task 2. Examples creating `Step` directly (e.g., `examples/single-turn-cli/`) already manage their own `OnEmit` and don't use `WithState`, so behavior is unchanged. Verify in Task 4. |
| `Stream.Submit` or `Stream.Process` is called concurrently, causing race on `state.Buffer` | Low | Low | `state.Buffer` is documented as not goroutine-safe; concurrency is a future middleware concern (per `AGENTS.md`). This change does not alter existing concurrency semantics. |

## Validation Criteria

- [ ] `go test -race ./...` passes with zero failures.
- [ ] `go vet ./...` reports no issues.
- [ ] `go build ./examples/...` succeeds.
- [ ] `session/manager.go` contains no references to `defaultOnEmit`.
- [ ] `loop/loop.go` contains the `WithState` option and auto-append logic in `Emit`.
- [ ] Regression test `TestManager_Process_NoDuplicateTurns` passes and would fail if `defaultOnEmit` were reintroduced alongside `WithState`.
- [ ] Thread state after a `Process` cycle contains exactly one user turn and one assistant turn per message (no duplicates).
