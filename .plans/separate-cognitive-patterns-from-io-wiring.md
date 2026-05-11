# Plan: Separate Cognitive Patterns from IO Wiring

## Objective

Refactor ore's architecture to eliminate the conflation of cognitive patterns (ReAct feedback loop) with IO orchestration (surface event handling, rendering, status). `loop.Step` currently couples itself to `surface.Surface` for streaming delta rendering, while `orchestrate.ReAct` simultaneously owns both the ReAct feedback loop and the surface event loop. This plan separates three distinct concerns: `loop.Step` becomes a surface-agnostic transform node that emits output events; ReAct is extracted as a pure, stateless cognitive pattern under a new `cognitive/` package; and surface/IO wiring becomes the sole responsibility of application-level code.

## Context

### Current Architecture (discovered from codebase)

- **`loop/loop.go`** — `loop.Step` struct embeds a `surface.Surface` field and calls `surface.RenderDelta()` during streaming provider invocation. `WithSurface()` option configures this coupling. Step also runs `loop.Handler` artifact handlers (including tool execution) after each turn.
- **`loop/handler.go`** — `Handler` interface processes artifacts from assistant turns. `tool.Handler` (`tool/handler.go`) implements this to execute tool calls and append `RoleTool` turns to state.
- **`orchestrate/orchestrator.go`** — `Orchestrator` interface (`Run(ctx) error`) bakes in surface ownership by requiring the orchestrator to already own the surface, state, provider, and step.
- **`orchestrate/react.go`** — `ReAct` struct embeds `surface.Surface`, `state.State`, `loop.Step`, and `provider.Provider`. It runs a surface event loop in `Run()`, renders turns and status via the surface, and delegates to `Step.Turn()` for inference. The actual ReAct feedback loop ("check state, loop if tool results exist") is thin; most of the code is IO wiring.
- **`examples/single-turn-cli/main.go`** — Uses `loop.New()` with no options. Output is entirely manual: the caller reads `mem.Turns()` after `Step.Turn()` returns. No surface involved.
- **`examples/tui-chat/main.go`** — Passes the same TUI surface `s` to both `loop.New(loop.WithSurface(s))` and `orchestrate.ReAct{Surface: s}`. Surface is split across two components.
- **`examples/calculator/main.go`** — Manually implements the ReAct feedback loop in `main()`: calls `Step.Turn()`, checks if last turn is `RoleAssistant`, loops if tool calls were handled. This is exactly the pattern `orchestrate.ReAct` should encapsulate — but without any surface.

### Design Decisions from Ideation

- **Tree-of-Thought deliberation**: We evaluated whether tool execution should move into the cognitive pattern (ReAct) or stay in Step. The ReAct paper defines "Act" as tool execution and "Observe → Reason → loop" as the cognitive evaluation. Tool execution via `loop.Handler` is a natural artifact-processing primitive; the cognitive layer's job is deciding *when* to stop looping. Tool execution stays in `Step`.
- **Step as transform node**: Step should define input/output formats and emit events to a subscriber-provided channel rather than calling `surface.RenderDelta()` directly. This makes Step IO-aware (it knows output is happening) without being surface-aware (it doesn't know the `Surface` abstraction).
- **ReAct as pure cognitive pattern**: Stripped of surface, state ownership, and event loops, ReAct becomes a stateless function: `Run(ctx, state.State) (state.State, error)`. It repeatedly calls `Step.Turn()` and inspects the resulting state.
- **IO wiring is application-level**: The surface event loop, status management, interrupt handling, and rendering belong in `main()` (or a thin application-specific driver), not in framework packages.
- **Cognitive pattern chaining is out of scope**: Interesting future direction, but not part of this refactor.

## Architectural Blueprint

**Before:**
```
loop/          → depends on surface/ (Step calls RenderDelta)
orchestrate/   → ReAct owns surface event loop + cognitive loop + rendering + status
examples/      → tui-chat passes surface to both loop and orchestrate
```

**After:**
```
loop/          → no surface dependency; emits OutputEvent to provided channel
cognitive/     → ReAct as pure feedback loop; no surface, no state ownership
examples/      → application main() wires surface events → state → cognitive.Run() → Step output → surface rendering
```

**Three layers with clean boundaries:**

1. **Transform layer (`loop.Step`)**: Receives `state.State` + `provider.Provider`, invokes inference, optionally collects streaming deltas into output events, runs artifact handlers (tool execution), appends to state, emits `TurnCompleteEvent`.
2. **Cognitive layer (`cognitive.ReAct`)**: Stateless. Receives `state.State`, repeatedly invokes `Step.Turn()`, inspects resulting state to detect pending tool calls, loops until assistant turn is final. No surface, no rendering, no event loop.
3. **IO wiring layer (application `main()`)**: Owns the `surface.Surface`. Reads `surface.Events()`, appends user messages to state, calls `cognitive.ReAct.Run()`, subscribes to `Step.Output()` channel to route delta/turn events back to the surface, manages interrupts and status.

## Requirements

1. `loop.Step` must not import `surface/` or reference `surface.Surface`. Streaming deltas are emitted as `DeltaEvent` to a caller-provided `chan<- OutputEvent` instead of being routed to `surface.RenderDelta()`.
2. `cognitive.ReAct` must be surface-agnostic and stateless. It accepts `state.State` as a parameter and returns it, without embedding it. It must not reference `surface.Surface` or `surface.Event`.
3. `orchestrate/` package is deleted entirely. The `Orchestrator` interface is removed; there is no framework-level abstraction that conflates IO wiring with cognitive strategy.
4. `examples/tui-chat/main.go` demonstrates the new architecture: surface events are handled in `main()`, `cognitive.ReAct.Run()` is called for the cognitive loop, and `loop.Step` output events are routed to the TUI surface via a goroutine.
5. `examples/calculator/main.go` uses `cognitive.ReAct` instead of manually implementing the feedback loop in `main()`.
6. `examples/single-turn-cli/main.go` requires no substantive changes (it already uses `loop.Step` without surface).
7. All existing tests are migrated and `go test -race ./...` passes after each task.
8. `README.md` architecture description is updated to remove references to `orchestrate/`, `Orchestrator`, and `WithSurface`.

## Task Breakdown

### Task 1: Add Output Events to loop.Step (Backward-Compatible)
- **Goal**: Introduce an `OutputEvent` event system into `loop.Step` so callers can subscribe to streaming deltas and turn completion without Step knowing about `surface.Surface`.
- **Dependencies**: None.
- **Files Affected**:
  - `loop/loop.go`
  - `loop/loop_test.go`
- **New Files**: None.
- **Interfaces**:
  ```go
  // In loop/loop.go
  type OutputEvent interface {
      Kind() string
  }

  type DeltaEvent struct {
      Delta artifact.Artifact
  }
  func (e DeltaEvent) Kind() string { return "delta" }

  type TurnCompleteEvent struct {
      Turn state.Turn
  }
  func (e TurnCompleteEvent) Kind() string { return "turn_complete" }

  func WithOutput(ch chan<- OutputEvent) Option
  ```
  - `Step` struct gains `output chan<- OutputEvent` field.
  - `Turn()` modified: if `s.output != nil`, create `deltasCh` and spawn a goroutine that reads from `deltasCh` and emits `DeltaEvent{Delta: delta}` to `s.output` (with `select` on `ctx.Done()` for cancellation safety). After appending artifacts and running handlers, if `s.output != nil`, emit `TurnCompleteEvent{Turn: last}` where `last` is the final `RoleAssistant` turn.
  - `WithSurface()` and `surface` field are **preserved unchanged** in this task to maintain backward compatibility.
- **Validation**:
  - `go build ./...` passes with no errors.
  - `go test -race ./loop/...` passes.
  - New tests verify that `DeltaEvent` and `TurnCompleteEvent` are emitted to the output channel during `Turn()` with a streaming provider.
- **Details**: This is an additive change. Do not remove `WithSurface()` or the `surface` field yet — `orchestrate/` still depends on them. Add new test functions (`TestStep_Turn_OutputEvents`, `TestStep_Turn_OutputEventsWithStreaming`) alongside existing surface tests. The streaming goroutine logic is structurally identical to the current surface goroutine; only the destination changes from `s.surface.RenderDelta()` to `s.output <- DeltaEvent{}`.

### Task 2: Create cognitive/ Package with Pure ReAct Pattern
- **Goal**: Extract ReAct's cognitive feedback loop into a new `cognitive/` package, stripped of all surface awareness and state ownership.
- **Dependencies**: None (parallelizable with Task 1; `cognitive.ReAct` only needs `loop.Step.Turn()` which already exists).
- **Files Affected**: None (new package).
- **New Files**:
  - `cognitive/doc.go` — package documentation
  - `cognitive/react.go` — `ReAct` type and `Run()` method
  - `cognitive/react_test.go` — tests for pure ReAct loop
- **Interfaces**:
  ```go
  // In cognitive/react.go
  type ReAct struct {
      Step     *loop.Step
      Provider provider.Provider
  }

  // Run repeatedly invokes Step.Turn() while the last turn in state
  // is not RoleAssistant (indicating pending tool results). It returns
  // when an assistant turn completes or when the context is cancelled.
  func (r *ReAct) Run(ctx context.Context, st state.State) (state.State, error)
  ```
  - `Run()` implements: `for { result, err := r.Step.Turn(ctx, st, r.Provider); ...; if last.Role == state.RoleAssistant { return result, nil }; st = result }`.
  - Error wrapping: `fmt.Errorf("react turn failed: %w", err)`.
  - No surface references. No `state.State` field embedding.
- **Validation**:
  - `go build ./...` passes.
  - `go test -race ./cognitive/...` passes.
  - Tests verify: (a) single assistant turn returns immediately, (b) tool-call sequence loops correctly, (c) provider errors propagate.
- **Details**: Use the same mock providers from the old `orchestrate/react_test.go` (`simpleProvider`, `countingProvider`), but remove all `mockSurface` code. `cognitive.ReAct` tests only need mock providers and mock handlers (for appending tool results). The `loop.Handler` test double from `orchestrate/react_test.go` can be copied or reused.

### Task 3: Update examples/tui-chat to Use Output Events and cognitive.ReAct
- **Goal**: Restructure `examples/tui-chat/main.go` so all surface IO wiring lives in `main()`, using `loop.WithOutput()` and `cognitive.ReAct`.
- **Dependencies**: Task 1 (for `WithOutput`), Task 2 (for `cognitive.ReAct`).
- **Files Affected**:
  - `examples/tui-chat/main.go`
- **New Files**: None.
- **Interfaces**: None (application-level wiring, no new framework types).
- **Validation**:
  - `go build ./examples/tui-chat/...` passes.
  - `go test -race ./...` passes.
- **Details**: The new structure in `main()` should be:
  1. Create TUI surface `s := tui.New()`.
  2. Create output channel: `outputCh := make(chan loop.OutputEvent, 100)`.
  3. Create Step: `st := loop.New(loop.WithOutput(outputCh))`.
  4. Start goroutine that reads `outputCh` and routes to surface:
     - `loop.DeltaEvent` → `s.RenderDelta(ctx, e.Delta)`
     - `loop.TurnCompleteEvent` → `s.RenderTurn(ctx, e.Turn)`
  5. Create `cognitive.ReAct{Step: st, Provider: prov}`.
  6. Create `state.Memory` for conversation state.
  7. Event loop over `s.Events()`:
     - `surface.UserMessageEvent`: append to state, call `s.SetStatus(ctx, "thinking...")`, call `react.Run(ctx, mem)`, call `s.SetStatus(ctx, "")`. Use a per-operation `context.WithCancel(ctx)` so `surface.InterruptEvent` can cancel only the in-progress `react.Run`, not the entire application.
     - `surface.InterruptEvent`: cancel the current operation's context.
  8. Remove all imports of `orchestrate/`.

### Task 4: Update examples/calculator to Use cognitive.ReAct
- **Goal**: Replace the manual ReAct feedback loop in `examples/calculator/main.go` with `cognitive.ReAct`.
- **Dependencies**: Task 2 (for `cognitive.ReAct`).
- **Files Affected**:
  - `examples/calculator/main.go`
- **New Files**: None.
- **Interfaces**: None.
- **Validation**:
  - `go build ./examples/calculator/...` passes.
  - `go test -race ./...` passes.
- **Details**: Replace the `for { ... }` loop that manually calls `step.Turn()` and checks `last.Role` with:
  ```go
  react := &cognitive.ReAct{Step: step, Provider: prov}
  mem.Append(state.RoleUser, artifact.Text{Content: message})
  result, err := react.Run(ctx, mem)
  ```
  After `react.Run()` returns, read `result.Turns()` to find the final assistant turn and print its artifacts (the existing artifact printing logic can be reused). Remove the manual loop-detection code.

### Task 5: Delete orchestrate/ Package
- **Goal**: Remove the conflated `orchestrate/` package entirely.
- **Dependencies**: Task 3 (tui-chat no longer imports `orchestrate/`).
- **Files Affected**:
  - `orchestrate/orchestrator.go` (delete)
  - `orchestrate/react.go` (delete)
  - `orchestrate/react_test.go` (delete)
- **New Files**: None.
- **Interfaces**: None.
- **Validation**:
  - `go test -race ./...` passes (no package at `orchestrate/` should be tested).
  - `find /home/andrewhowdencom/Development/ore/orchestrate -type f | wc -l` should return `0`.
  - `grep -r "orchestrate" /home/andrewhowdencom/Development/ore --include="*.go"` should find no imports (except possibly in `README.md`, which is handled in Task 7).
- **Details**: Delete all three files. Delete the `orchestrate/` directory if it becomes empty. Verify no other Go files in the repository import `github.com/andrewhowdencom/ore/orchestrate`.

### Task 6: Remove surface Dependency from loop/
- **Goal**: Strip the remaining surface awareness from `loop.Step` by removing `WithSurface()`, the `surface` field, and the `surface/` import.
- **Dependencies**: Task 5 (orchestrate/ deleted, so nothing uses `WithSurface()` anymore).
- **Files Affected**:
  - `loop/loop.go`
  - `loop/loop_test.go`
  - `loop/doc.go`
- **New Files**: None.
- **Interfaces**:
  - Remove `WithSurface(surf surface.Surface) Option`.
  - Remove `surface` field from `Step` struct.
  - Remove `surface` import from `loop/loop.go`.
  - Update `Turn()`: remove the code path that calls `s.surface.RenderDelta()`. The output channel path (added in Task 1) is the sole mechanism for streaming output.
- **Validation**:
  - `go build ./...` passes with no errors.
  - `go test -race ./...` passes.
  - `grep -n "surface" /home/andrewhowdencom/Development/ore/loop/loop.go` should find no matches.
  - `grep -n "surface" /home/andrewhowdencom/Development/ore/loop/loop_test.go` should find no matches.
- **Details**: Update `loop/loop_test.go` to remove all `mockSurface` code and tests that depend on `WithSurface()`. The output-event tests added in Task 1 remain and now cover the sole streaming mechanism. Update `loop/doc.go` to remove references to "optionally routes streaming deltas to a surface" — replace with "optionally emits streaming deltas as OutputEvent to a provided channel".

### Task 7: Update README.md Architecture Documentation
- **Goal**: Update the README to reflect the new three-layer architecture.
- **Dependencies**: Tasks 1–6 (all code changes complete).
- **Files Affected**:
  - `README.md`
- **New Files**: None.
- **Interfaces**: None.
- **Validation**:
  - `grep -c "orchestrate" /home/andrewhowdencom/Development/ore/README.md` should return `0`.
  - `grep -c "Orchestrator" /home/andrewhowdencom/Development/ore/README.md` should return `0`.
  - `grep -c "WithSurface" /home/andrewhowdencom/Development/ore/README.md` should return `0`.
- **Details**: Update the "System Architecture" section to describe the three layers (Transform, Cognitive, IO Wiring) instead of the old core/step/orchestrate split. Replace code examples that reference `orchestrate.ReAct` and `WithSurface` with examples using `cognitive.ReAct` and `WithOutput`. Update the package listing to include `cognitive/` and remove `orchestrate/`. Update the description of `loop.Step` to describe output events instead of surface rendering.

## Dependency Graph

- Task 1 || Task 2 (parallelizable; both are additive and independent)
- Task 1 → Task 3 (tui-chat needs `WithOutput`)
- Task 2 → Task 3 (tui-chat needs `cognitive.ReAct`)
- Task 2 → Task 4 (calculator needs `cognitive.ReAct`)
- Task 3 || Task 4 (parallelizable after Tasks 1 and 2)
- Task 3 → Task 5 (can delete orchestrate/ only after tui-chat stops importing it)
- Task 5 → Task 6 (can remove `WithSurface()` only after orchestrate/ is gone)
- Tasks 1–6 → Task 7 (documentation reflects final code state)

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| `examples/tui-chat` interrupt handling becomes buggy when moved to `main()` | High | Medium | Preserve the per-operation `context.WithCancel` pattern from `orchestrate/react.go` exactly in `main()`. Test interrupt behavior manually after implementation. |
| Output channel goroutine leaks or races during streaming | Medium | Medium | Ensure `close(deltasCh)` and `wg.Wait()` are preserved from the old surface goroutine pattern. Use buffered channels (size 100) and `select` with `ctx.Done()`. Race tests in `go test -race` will catch issues. |
| `README.md` code examples drift from actual API | Low | Medium | Task 7 is scoped explicitly to README updates. Search README for old terms ("orchestrate", "WithSurface", "Orchestrator") and replace all occurrences. |
| `loop_test.go` surface tests deleted prematurely, losing coverage | Medium | Low | Task 1 adds output-event tests *before* Task 6 removes surface tests. There is always test coverage for streaming behavior. Verify `go test -race ./loop/...` passes after each task. |
| Builder confused about `cognitive/` package naming | Low | Low | The plan specifies `cognitive/` as the package path and `ReAct` as the type. The builder should follow this exactly. If a better name emerges during implementation, a follow-up rename is trivial. |

## Validation Criteria

- [ ] `go build ./...` succeeds with no compilation errors.
- [ ] `go test -race ./...` passes with no failures or race conditions.
- [ ] `loop/` package contains no imports of `surface/` and no references to `surface.Surface`.
- [ ] `orchestrate/` directory does not exist in the repository.
- [ ] `cognitive/` package exists with `cognitive.ReAct` type and `Run(ctx, state.State) (state.State, error)` method.
- [ ] `cognitive.ReAct` struct does not contain `surface.Surface` or `state.State` fields.
- [ ] `examples/tui-chat/main.go` compiles and demonstrates the three-layer architecture (surface in `main()`, `cognitive.ReAct` for loop, `loop.WithOutput` for streaming).
- [ ] `examples/calculator/main.go` uses `cognitive.ReAct` instead of a manual loop.
- [ ] `README.md` contains no references to `orchestrate/`, `Orchestrator`, or `WithSurface`.
- [ ] `README.md` describes the three-layer architecture (Transform, Cognitive, IO Wiring).
