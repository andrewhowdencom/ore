# Plan: Decompose `loop.Step` into `EventBus`, `Pipeline`, and Thin Orchestrator

## Objective

Decompose the `loop.Step` monolith into three focused, single-responsibility components — `EventBus` (broadcast infrastructure), `Pipeline` (single-turn execution engine), and a thin `Step` orchestrator — while defining public `TurnRunner` and `TurnSubmitter` interfaces that enable `cognitive` patterns to be modeled as composable middleware. This addresses the `software-development` skill signal that `loop/loop.go` `Turn()` is over-complex (~115 lines handling transforms, provider invocation, delta accumulation, event emission, and handlers in a single central method).

## Context

Key findings from repository exploration:

- `loop/loop.go` `Turn()` is explicitly flagged by the `software-development` skill as a signal: *"loop/loop.go: Turn() spans ~115 lines handling transforms, provider invocation, delta accumulation, event emission, and handlers. Too many responsibilities in one method."* The Boolean Guard says: *"IF a core package method handles both orchestration AND transformation AND emission in the same function → STOP and decompose."*
- `loop/` is central (core chain: `artifact/` → `state/` → `provider/` → `loop/`). It must be radically simple per the centrality-weighted reasonability principle.
- `cognitive/` is peripheral (imported only by `examples/` and `cmd/`). It can carry more complexity but currently depends on the concrete `*loop.Step` type, preventing clean middleware composition.
- `session/` defines `TurnProcessor` which passes `*loop.Step` to cognitive patterns. The `session` package and `session` tests have multiple function literals that match the `TurnProcessor` type.
- `cognitive/react.go` has `Step *loop.Step` field; `cognitive/verify.go` has `step *loop.Step` field and parameter.
- `loop/handler.go` already defines the `Emitter` interface (one method: `Emit(ctx, event)`). `Step` already satisfies it.
- `loop/fanout.go` is already a standalone component. It will be owned by `EventBus`.
- The `loop` public API (`New`, `Turn`, `Submit`, `Emit`, `Subscribe`, `Close`, `Option` functions) must remain unchanged so that `examples/` and `session/` continue to work.
- Examples `calculator`, `filesystem`, and `verifier-chat` construct `cognitive.ReAct` directly with struct literals. Because `*loop.Step` will satisfy `loop.TurnRunner`, and the field name `Step` can be kept with an interface type, these examples do not require changes.
- Examples `tui-chat` and `http-chat` use `cognitive.NewTurnProcessor(cognitive.ReActFactory, tracer)` with function references. Because `ReActFactory` and `NewTurnProcessor` signatures will be updated to match, these call sites work without modification.

## Architectural Blueprint

The decomposition splits `Step` into three internal components within `loop/`:

1. **`EventBus`** — the broadcast infrastructure. Owns the `events` channel, `*FanOut`, `OnEmit` callbacks, and the bound `state.State` for auto-append. Exposes `Emit()`, `Subscribe()`, and `Close()`.
2. **`Pipeline`** — the single-turn execution engine. Owns `transforms`, `handlers`, `invokeOpts`, and `tracer`. Runs transforms, calls `provider.Invoke`, accumulates streaming artifacts (with delta merging), and runs handlers. `Pipeline.Turn()` takes an `onArtifact func(artifact.Artifact)` callback so the orchestrator can emit streaming events into the `EventBus`.
3. **`Step`** — the thin orchestrator. Composes `EventBus` + `Pipeline`. Sequences events: `LifecycleEvent{Phase: "submitted"}` → `Pipeline.Turn()` with artifact callback → `LifecycleEvent{Phase: "streaming"}` / `ArtifactEvent`s → `finalizeTurn()` → `TurnCompleteEvent` → handler execution. `Step` keeps `eventContext`, `tracer` for `loop.turn` span, and `New/Option` public API.

Public interfaces for `cognitive` middleware:

```go
type TurnRunner interface {
    Turn(ctx context.Context, st state.State, p provider.Provider) (state.State, error)
}

type TurnSubmitter interface {
    Submit(ctx context.Context, st state.State, role state.Role, artifacts ...artifact.Artifact) (state.State, error)
}

type TurnExecutor interface {
    TurnRunner
    TurnSubmitter
}
```

`Step` (via delegation) satisfies all three. `ReAct` wraps `TurnRunner`. `WithVerification` wraps `Pattern` and uses `TurnSubmitter` to inject system turns. The middleware model is `WithVerification(ReAct(inner), submitter)`.

## Requirements

1. `EventBus` is extracted from `Step` as a standalone `loop` internal type. `Step` delegates `Emit/Subscribe/Close` to it.
2. `Pipeline` is extracted from `Step` as a standalone `loop` internal type. `Step` delegates `Turn()` execution (transforms, provider, accumulation, enrichToolCalls, handlers) to it.
3. `Step.Turn()` is reduced to under 50 lines of pure event sequencing.
4. `loop` public API (`New`, `Turn`, `Submit`, `Emit`, `Subscribe`, `Close`, `Option` functions) is preserved — no `examples/` or `session/` changes required for the `loop/` decomposition alone.
5. `TurnRunner`, `TurnSubmitter`, and `TurnExecutor` interfaces are defined in `loop/` and exported.
6. `cognitive.ReAct` depends on `loop.TurnRunner` (not `*loop.Step`).
7. `cognitive.WithVerification` depends on `loop.TurnSubmitter` (not `*loop.Step`).
8. `session.TurnProcessor` receives `loop.TurnExecutor` instead of `*loop.Step`.
9. All tests pass with `go test -race ./...` after full decomposition.
10. All examples compile with `go build ./examples/...` after full decomposition.

## Task Breakdown

### Task 1: Define `TurnRunner`, `TurnSubmitter`, and `TurnExecutor` interfaces in `loop/`
- **Goal:** Add public capability interfaces that `Step` already satisfies, enabling `cognitive` patterns to depend on abstractions rather than concrete `*loop.Step`.
- **Dependencies:** None
- **Files Affected:** `loop/loop.go` (add interfaces), `loop/doc.go` (update package docs to mention the new interfaces)
- **New Files:** `loop/interfaces.go` (or append to `loop.go` if the project prefers fewer files)
- **Interfaces:**
  - `TurnRunner` — `Turn(ctx context.Context, st state.State, p provider.Provider) (state.State, error)`
  - `TurnSubmitter` — `Submit(ctx context.Context, st state.State, role state.Role, artifacts ...artifact.Artifact) (state.State, error)`
  - `TurnExecutor` — embeds `TurnRunner` and `TurnSubmitter`
- **Validation:** `go test ./loop/...` passes. `go build ./...` passes (interfaces only, no consumer changes yet).
- **Details:** This is a zero-risk type-only change. `Step` already satisfies these interfaces by definition. No `Option` functions, no method bodies, no `examples/` changes. The interfaces are the foundation for the cognitive middleware model.

### Task 2: Extract `EventBus` from `Step`
- **Goal:** Separate the event broadcast infrastructure (channel, FanOut, OnEmit callbacks, state binding, Emit/Subscribe/Close) from `Step` into a standalone `EventBus` component. `Step` holds `*EventBus` and delegates.
- **Dependencies:** None (can be done in parallel with Task 1)
- **Files Affected:** `loop/loop.go` (remove event-related fields and methods)
- **New Files:** `loop/eventbus.go`
- **Interfaces:**
  - `EventBus` struct with `events chan outputEventEnvelope`, `fanOut *FanOut`, `onEmit []OnEmit`, `state state.State`
  - `EventBus.Emit(ctx, event)` — runs OnEmit callbacks, auto-appends `TurnCompleteEvent` to bound state, sends to FanOut
  - `EventBus.Subscribe(kinds...)` — delegates to `FanOut.Subscribe()`
  - `EventBus.Close()` — delegates to `FanOut.Close()`
- **Validation:** `go test ./loop/...` passes. All loop tests pass without modification. `go build ./examples/...` passes (public API unchanged).
- **Details:** `New()` creates `EventBus` before applying `Option` functions so that `WithOnEmit`/`WithState` can access `s.eventBus` safely. `Step.Emit()`, `Step.Subscribe()`, `Step.Close()` become one-line delegations to `s.eventBus`. The `outputEventEnvelope` type stays in `loop/` (shared between `EventBus` and `Step`).

### Task 3: Extract `Pipeline` from `Step`
- **Goal:** Move single-turn execution logic (transforms, provider invocation, artifact accumulation, `enrichToolCalls`, handler execution) from `Step` into a standalone `Pipeline` component. `Step` becomes a thin orchestrator that sequences event emission around `Pipeline.Turn()`.
- **Dependencies:** Task 2
- **Files Affected:** `loop/loop.go` (remove pipeline execution logic, shrink `Step.Turn()` to event sequencing)
- **New Files:** `loop/pipeline.go`
- **Interfaces:**
  - `Pipeline` struct with `transforms []Transform`, `handlers []Handler`, `invokeOpts []provider.InvokeOption`
  - `Pipeline.Turn(ctx, st, provider, onArtifact func(artifact.Artifact), opts...) (state.State, []artifact.Artifact, error)` — runs transforms, calls `provider.Invoke`, accumulates deltas, calls `onArtifact` for each artifact, calls `enrichToolCalls`, returns final accumulated artifacts
  - `Pipeline.RunHandlers(ctx, artifacts []artifact.Artifact, emitter Emitter) error` — runs all registered handlers on each artifact
- **Validation:** `go test ./loop/...` passes. All loop tests pass without modification. `go build ./examples/...` passes.
- **Details:** `Step.Turn()` becomes ~25–40 lines: emit `submitted` → call `Pipeline.Turn()` with an `onArtifact` callback that emits `streaming`/`ArtifactEvent`s → call `finalizeTurn()` which emits `TurnCompleteEvent` and delegates handler execution to `Pipeline.RunHandlers()`. `Step.Submit()` stays the same (calls `finalizeTurn()` with span). `Option` functions (`WithTransforms`, `WithHandlers`, `WithInvokeOptions`) now modify `s.pipeline` instead of `s` directly. `WithOnEmit`/`WithState`/`WithTracer` stay on `Step` or `EventBus` as appropriate.

### Task 4: Refactor `cognitive/` and `session/` to use `loop` interfaces
- **Goal:** Replace all `*loop.Step` concrete dependencies in `cognitive/` and `session/` with `loop.TurnRunner`, `loop.TurnSubmitter`, and `loop.TurnExecutor` interfaces. Update `TurnProcessor` type and all test function literals.
- **Dependencies:** Task 1 (interfaces must exist before consumers can depend on them)
- **Files Affected:**
  - `cognitive/react.go` — change `Step` field type from `*loop.Step` to `loop.TurnRunner`; keep field name `Step` so struct-literal examples compile unchanged
  - `cognitive/verify.go` — change `step` field type from `*loop.Step` to `loop.TurnSubmitter`; change `WithVerification` parameter type; keep parameter name `step` so call sites compile unchanged
  - `cognitive/react.go` — update `NewTurnProcessor` factory signature to `func(loop.TurnExecutor, provider.Provider, trace.Tracer) Pattern` and `ReActFactory` to match
  - `session/manager.go` — update `TurnProcessor` type signature: `func(ctx context.Context, executor loop.TurnExecutor, st state.State, prov provider.Provider) (state.State, error)`
  - `session/manager_test.go` — update all `TurnProcessor` function literals from `*loop.Step` to `loop.TurnExecutor`
  - `session/stream_test.go` — update all `TurnProcessor` function literals from `*loop.Step` to `loop.TurnExecutor`
- **New Files:** None
- **Interfaces:** No new interfaces; this task applies the interfaces from Task 1.
- **Validation:** `go test -race ./cognitive/... ./session/...` passes. `go build ./examples/...` passes.
- **Details:** Because `*loop.Step` satisfies `loop.TurnExecutor` (which embeds `loop.TurnRunner` and `loop.TurnSubmitter`), and the field/parameter names are preserved, **examples do not need changes** in this task. The `session.Stream` struct still holds `*loop.Step` — no change needed there. Only the `TurnProcessor` type and the `cognitive` pattern constructors change. The `verifyingPattern` calls `p.step.Submit()` — after the change it calls `p.submitter.Submit()` which is the same method on the same interface.

### Task 5: Full validation and documentation update
- **Goal:** Run the complete test suite, verify all examples compile, and update package documentation to reflect the new component boundaries.
- **Dependencies:** Task 1, Task 2, Task 3, Task 4
- **Files Affected:** `loop/doc.go` (describe EventBus/Pipeline/Step decomposition), `cognitive/doc.go` (describe middleware composition model)
- **New Files:** None
- **Interfaces:** None
- **Validation:** `go test -race ./...` passes. `go build ./examples/...` passes. `go build ./cmd/...` passes. `cognitive/react.go` contains no `*loop.Step` type references. `cognitive/verify.go` contains no `*loop.Step` type references. `loop/loop.go` `Turn()` method is under 50 lines.
- **Details:** This is a final pass. No new code. Verify that the decomposition is complete and that the cognitive middleware model is functional. If any example fails to compile, it means an interface compatibility assumption was wrong — fix it here.

## Dependency Graph

- Task 1 → Task 4 (Task 4 needs the interfaces defined in Task 1)
- Task 2 → Task 3 (Task 3 needs EventBus extracted before it can safely factor out Pipeline)
- Task 1 || Task 2 (independent; both modify `loop/` but touch different parts)
- Tasks 3, 4 → Task 5 (Task 5 verifies everything after all decomposition and consumer updates are done)

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Streaming artifact emission order changes during `Pipeline` extraction | High | Medium | `loop_test.go` has extensive tests for artifact accumulation and event emission. Run `go test -race ./loop/...` after Task 3. If streaming order breaks, add an explicit test that asserts the exact sequence of `ArtifactEvent` + `LifecycleEvent` emissions for a mixed delta/complete artifact stream. |
| Handler execution `Emitter` reference changes, causing handlers to emit to the wrong bus | Medium | Low | `Step.finalizeTurn()` must pass `Step` (or `Step.eventBus`) as the `Emitter` to `Pipeline.RunHandlers()`. The `loop_test.go` `mockHandler` captures the `Emitter` passed to `Handle()` — verify it is the same object the test subscribed to. |
| `Option` functions applied before `EventBus` is initialized in `New()` | High | Low | `New()` must create `EventBus` and `Pipeline` before the `for _, opt := range opts { opt(s) }` loop. The `stepWithState` helper in `loop_test.go` exercises `WithOnEmit` + `WithState` — if initialization order is wrong, this test will fail immediately. |
| Session test function literal updates missed in `manager_test.go` or `stream_test.go` | Low | High | The compiler will catch this. `go test ./session/...` will fail with a type mismatch if any function literal still references `*loop.Step`. The plan explicitly lists both files in Task 4. |
| Example compilation failures due to interface incompatibility assumptions | Low | Low | `go build ./examples/...` is part of Task 5 validation. If any example fails, the specific example is added to Task 4's file list and fixed. |
| `enrichToolCalls` post-processing misses tool options after Pipeline extraction | Medium | Low | `enrichToolCalls` is called in `Pipeline.Turn()` with the combined `invokeOpts` slice, same as today. Verify with a test that `ToolCall.Value` is still populated when `DisplayHint` is present. |

## Validation Criteria

- [ ] `go test -race ./loop/...` passes after Task 1.
- [ ] `go test -race ./loop/...` passes after Task 2.
- [ ] `go test -race ./loop/...` passes after Task 3.
- [ ] `go test -race ./cognitive/... ./session/...` passes after Task 4.
- [ ] `go test -race ./...` passes after Task 5.
- [ ] `go build ./examples/...` passes after Task 5.
- [ ] `go build ./cmd/...` passes after Task 5.
- [ ] `cognitive/react.go` contains no `*loop.Step` type references (only `loop.TurnRunner`).
- [ ] `cognitive/verify.go` contains no `*loop.Step` type references (only `loop.TurnSubmitter`).
- [ ] `loop/loop.go` `Turn()` method is under 50 lines after Task 3.
- [ ] `loop/loop.go` `Step` struct has no `events`, `fanOut`, `transforms`, `handlers`, `invokeOpts`, `onEmit`, or `state` fields (all delegated to `EventBus` or `Pipeline`).
