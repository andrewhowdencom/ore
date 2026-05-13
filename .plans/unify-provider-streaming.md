# Plan: Unify Provider Streaming/Non-Streaming Paths

## Objective

Collapse the `Provider`/`StreamingProvider` bifurcation into a single channel-based interface where all providers emit typed artifacts (both delta and complete) through one `chan<- artifact.Artifact`. The `loop.Step` embeds `FanOut` as its native event distribution mechanism, conduits subscribe by artifact `Kind()`, and mid-stream errors emit an `ErrorEvent` without mutating state.

## Context

From discovery of the ore codebase:

- **Provider contract** (`provider/provider.go`) currently defines two interfaces: `Provider` with `Invoke(...)` returning `[]artifact.Artifact`, and `StreamingProvider` extending it with `InvokeStreaming(..., deltasCh chan<- artifact.Artifact, ...)`. The loop's `Turn()` method type-asserts the provider to `StreamingProvider` when an output channel is configured.
- **OpenAI adapter** (`provider/openai/openai.go`) implements both interfaces, duplicating parameter serialization and maintaining separate buffering logic for streaming vs. non-streaming paths.
- **Loop** (`loop/loop.go`) emits `DeltaEvent` wrappers around artifacts for the output channel. Conduits must type-assert through `DeltaEvent.Delta` to reach the actual artifact.
- **FanOut** (`loop/fanout.go`) distributes `OutputEvent` values by `Kind()` string, already supporting filtered subscriptions. The recent conduit refactor (`f2a4751`) moved TUI conduits from explicit `RenderDelta`/`RenderTurn` methods to subscribing to a `FanOut` by event kind.
- **Artifact types** (`artifact/artifact.go`) define `TextDelta`, `ReasoningDelta`, `ToolCallDelta` (ephemeral) alongside `Text`, `ToolCall`, `Usage`, `Reasoning` (complete). There is currently no way to distinguish ephemeral from persisted artifacts at the type level.
- **Cognitive patterns** (`cognitive/react.go`) rely on `Step.Turn()` being an atomic operation that produces a complete `RoleAssistant` turn. The turn-completion boundary is critical for `ReAct` to decide whether to loop again.
- **State** (`state/state.go`, `state/memory.go`) appends complete turns; delta artifacts must never reach state.

The converged design from ideation (#42) uses a single provider method that emits both delta and complete artifacts to a channel, with the loop buffering complete artifacts for state while forwarding deltas to an embedded `FanOut`.

## Architectural Blueprint

1. **Single Provider Interface**
   - `Provider` becomes: `Invoke(ctx context.Context, s state.State, ch chan<- artifact.Artifact, opts ...InvokeOption) error`
   - `StreamingProvider` is removed entirely.
   - The provider adapter owns delta-to-complete assembly internally (it knows the wire format) and emits both to the channel.

2. **Delta Marker Interface**
   - `type Delta interface { Artifact; IsDelta() }` added in `artifact/`.
   - `TextDelta`, `ReasoningDelta`, `ToolCallDelta` implement it.
   - The loop uses this to route ephemeral content to conduits immediately while buffering complete artifacts for state.

3. **Embedded FanOut in Step**
   - `Step` owns a `FanOut` internally, fed by a persistent event channel.
   - `Step.Subscribe(kinds ...string) <-chan OutputEvent` exposes the FanOut's subscription mechanism.
   - `WithOutput` is removed; callers subscribe directly.

4. **Event Model**
   - `DeltaEvent` wrapper is removed. `artifact.Artifact` values are emitted directly as `OutputEvent` (they already satisfy `Kind() string`).
   - `TurnCompleteEvent` stays.
   - `ErrorEvent{Err: error}` (`Kind() = "error"`) is added for mid-stream failures.

5. **Error Contract**
   - On `Invoke` error, partial artifacts already forwarded to conduits remain visible, but state is NOT mutated. An `ErrorEvent` is emitted.

## Requirements

1. Remove `StreamingProvider` and collapse to a single `Provider.Invoke()` with artifact channel.
2. Add `Delta` marker interface to `artifact` package and implement on all delta types.
3. Refactor `loop.Step` to buffer complete artifacts, forward deltas to embedded `FanOut`, and emit `ErrorEvent` on failure.
4. Refactor OpenAI adapter to single method emitting both deltas and complete artifacts.
5. Remove `DeltaEvent` wrapper; emit artifacts directly as events.
6. Update all tests, examples, and conduits to the new model.
7. `go test -race ./...` must pass at every commit.

## Task Breakdown

### Task 1: Add `Delta` Marker Interface to Artifact Package
- **Goal**: Introduce a type-level way to distinguish ephemeral delta artifacts from state-persisted complete artifacts.
- **Dependencies**: None.
- **Files Affected**: `artifact/artifact.go`, `artifact/artifact_test.go`
- **New Files**: None.
- **Interfaces**:
  ```go
  type Delta interface {
      Artifact
      IsDelta()
  }
  ```
  Implemented by `TextDelta`, `ReasoningDelta`, `ToolCallDelta` (add `IsDelta()` method to each).
- **Validation**: `go test ./artifact/...` passes. The repo still compiles (`go build ./...`) because no consumers reference `Delta` yet.
- **Details**: Add the interface and method implementations. Add unit tests verifying that delta artifacts satisfy `Delta` and non-delta artifacts do not.

### Task 2: Refactor Provider Interface and Core Framework (Atomic Cross-Cutting Change)
- **Goal**: Collapse `Provider`/`StreamingProvider` into one channel-based interface, refactor `loop.Step` to use it, update all implementations and consumers atomically.
- **Dependencies**: Task 1.
- **Files Affected**:
  - `provider/provider.go` — remove `StreamingProvider`, change `Provider.Invoke` signature
  - `provider/openai/openai.go` — refactor to single method emitting deltas + completes
  - `provider/openai/openai_test.go` — rewrite tests for channel emission
  - `provider/doc.go`, `provider/openai/doc.go` — update documentation
  - `loop/loop.go` — embed `FanOut`, add `Subscribe`, remove `WithOutput`, refactor `Turn()` for channel-based provider, add `ErrorEvent`, remove `DeltaEvent`
  - `loop/loop_test.go` — rewrite mock providers and all tests
  - `loop/fanout_test.go` — update tests to emit artifacts directly instead of `DeltaEvent`
  - `loop/doc.go` — update package documentation
  - `cognitive/react_test.go` — update mock providers
  - `conduit/tui/tui.go` — update event reading goroutine to handle artifacts directly
  - `conduit/tui/tui_test.go` — update tests
  - `conduit/conduit_test.go` — update if needed
  - `examples/single-turn-cli/main.go` — adapt provider construction and calling
  - `examples/calculator/main.go` — adapt provider construction and calling
  - `examples/tui-chat/main.go` — adapt to `step.Subscribe(...)`
- **New Files**: None.
- **Interfaces**:
  ```go
  // provider/provider.go
  type Provider interface {
      Invoke(ctx context.Context, s state.State, ch chan<- artifact.Artifact, opts ...InvokeOption) error
  }

  // loop/loop.go
  type Step struct {
      events      chan OutputEvent   // feeds internal FanOut
      fanOut      *FanOut
      beforeTurns []BeforeTurn
      handlers    []Handler
      invokeOpts  []provider.InvokeOption
  }

  func (s *Step) Subscribe(kinds ...string) <-chan OutputEvent

  type ErrorEvent struct { Err error }
  func (e ErrorEvent) Kind() string { return "error" }
  ```
- **Validation**: `go test ./...` passes, `go build ./...` passes, `go test -race ./...` passes. This is the critical path commit; every file in the list must be updated in this single commit to keep the repo buildable.
- **Details**:
  1. Update `provider.Provider` interface and remove `StreamingProvider`.
  2. Refactor OpenAI adapter: remove `InvokeStreaming`, refactor `Invoke` to use streaming internally, emit `TextDelta`/`ReasoningDelta`/`ToolCallDelta` chunks to the channel during streaming, buffer complete artifacts internally, then emit `Text`/`Reasoning`/`ToolCall`/`Usage` to the channel at the end. Return `nil` on success or `error` on failure.
  3. Refactor `loop.Step`:
     - Replace `output chan<- OutputEvent` with `events chan OutputEvent` and `fanOut *FanOut`.
     - `New()` initializes the event channel and `FanOut`.
     - Add `Subscribe(kinds ...string) <-chan OutputEvent` delegating to `fanOut.Subscribe`.
     - Remove `WithOutput` option.
     - `Turn()` creates a provider channel, starts a goroutine that reads artifacts: if `Delta`, non-blocking send to `events`; else buffer in `completeArtifacts []artifact.Artifact`. After provider returns, close channel, wait for goroutine. On error: emit `ErrorEvent` to `events`, return error without appending to state. On success: append `completeArtifacts` to state as `RoleAssistant`, run handlers, emit `TurnCompleteEvent`.
     - Remove `DeltaEvent` type.
     - Add `ErrorEvent` type.
  4. Update all mock providers in tests to implement the new single interface.
  5. Update `conduit/tui/tui.go` goroutine to type-switch on concrete artifact types (or `artifact.Artifact` interface) instead of `loop.DeltaEvent`.
  6. Update all examples to use `step.Subscribe(...)` instead of `loop.WithOutput` + `loop.NewFanOut`.

### Task 3: Final Validation and Cleanup
- **Goal**: Verify race safety, clean up any remaining references to old patterns, ensure examples run correctly.
- **Dependencies**: Task 2.
- **Files Affected**: Any remaining files with stale references (discovered via `grep` for `StreamingProvider`, `DeltaEvent`, `WithOutput`, `InvokeStreaming`).
- **New Files**: None.
- **Interfaces**: None.
- **Validation**:
  - `go test -race ./...` passes
  - `go build ./...` passes
  - `grep -r "StreamingProvider\|InvokeStreaming\|DeltaEvent\|WithOutput" --include="*.go" .` returns no matches outside `vendor/` or `.git/`
- **Details**: Run the grep, fix any stragglers, run final race tests.

## Dependency Graph

- Task 1 → Task 2 (Task 2 depends on `Delta` interface existing)
- Task 2 → Task 3 (Task 3 is cleanup/verification after the core refactor)

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Go interface change breaks compilation across packages | High | Certain (by design) | All affected files updated atomically in Task 2. No intermediate commits. |
| OpenAI streaming does not expose `Usage` metadata, losing token counts | Medium | High | Document as known limitation. Usage can be added later if SDK supports it. |
| Goroutine leak in `Turn()` if provider hangs or panics | High | Medium | Use `ctx.Done()` in all channel sends/receives. Add deferred cleanup. Test with `go test -race`. |
| Conduits drop events if they subscribe late or buffer fills | Low | Medium | Documented FanOut behavior; conduits must subscribe before `Turn()` and read promptly. |
| Partial deltas rendered by conduit before error, but no state record | Low | Low | Acceptable per design: conduits show "live" content, state remains consistent. |

## Validation Criteria

- [ ] `Provider` interface has single `Invoke` method with `chan<- artifact.Artifact` parameter.
- [ ] `StreamingProvider` interface does not exist.
- [ ] `Delta` interface exists and is implemented by `TextDelta`, `ReasoningDelta`, `ToolCallDelta`.
- [ ] `Step` exposes `Subscribe(kinds ...string) <-chan OutputEvent`.
- [ ] `WithOutput` option does not exist.
- [ ] `DeltaEvent` type does not exist.
- [ ] `ErrorEvent` type exists with `Kind() = "error"`.
- [ ] OpenAI adapter emits both deltas and complete artifacts to channel.
- [ ] `go test ./...` passes.
- [ ] `go test -race ./...` passes.
- [ ] `go build ./...` passes.
- [ ] No grep matches for `StreamingProvider`, `InvokeStreaming`, `DeltaEvent`, or `WithOutput` in `.go` files.
