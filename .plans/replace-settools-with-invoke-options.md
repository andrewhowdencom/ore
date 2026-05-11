# Plan: Replace SetTools with Per-Invocation InvokeOption

## Objective

Eliminate all shared mutable state from the OpenAI provider adapter by redesigning the `Provider` and `StreamingProvider` interfaces to accept per-invocation `InvokeOption` parameters. `ToolProvider.SetTools`, the `sync.RWMutex`, and the `ToolProvider` capability interface are removed. Tools, temperature, and future provider-specific configuration become per-invocation options. `loop.Step` gains the ability to carry pre-bound options so that `cognitive.ReAct` remains completely blind to provider specifics while advanced applications can pass options dynamically per-turn.

## Context

The repository is an active Go module (`github.com/andrewhowdencom/ore`) with the following relevant structure:

- **`provider/provider.go`** defines `Provider`, `StreamingProvider`, and `ToolProvider` interfaces. `Provider.Invoke` currently takes only `ctx` and `state`. `ToolProvider` adds `SetTools(tools []Tool) error`.
- **`provider/openai/openai.go`** implements all three interfaces. It stores a mutable `tools []provider.Tool` slice protected by a `sync.RWMutex`. `SetTools` acquires a write lock; `Invoke` and `InvokeStreaming` acquire read locks around access.
- **`loop/loop.go`** defines `Step.Turn(ctx, state, provider.Provider)` which calls `Provider.Invoke` or `StreamingProvider.InvokeStreaming`. It never references `ToolProvider`.
- **`cognitive/react.go`** defines `ReAct.Run(ctx, state)` which loops calling `Step.Turn`. It never references `ToolProvider`.
- **`examples/calculator/main.go`** calls `prov.SetTools(tools)` before constructing `ReAct`.
- **`examples/single-turn-cli/main.go`** contains a commented block showing `SetTools` usage.
- **Conventions (from `AGENTS.md`)**: functional options pattern for constructors, `fmt.Errorf("...: %w", err)` wrapping, table-driven tests, `go test -race ./...`.

The `SetTools`/`mutex` pattern was introduced to allow dynamic per-turn tool configuration, but it creates a logical race in multi-session workloads: two goroutines sharing a provider instance can clobber each other's tool configuration. The selected design removes all mutable provider state by passing configuration per-invocation through a generic `InvokeOption` interface.

## Architectural Blueprint

### Selected Architecture: Per-Invocation Options via Variadic `InvokeOption`

The base `Provider` interface grows a variadic `InvokeOption` parameter:

```go
type InvokeOption interface {
    IsInvokeOption()
}

type Provider interface {
    Invoke(ctx context.Context, s state.State, opts ...InvokeOption) ([]artifact.Artifact, error)
}

type StreamingProvider interface {
    Provider
    InvokeStreaming(ctx context.Context, s state.State, deltasCh chan<- artifact.Artifact, opts ...InvokeOption) ([]artifact.Artifact, error)
}
```

`ToolProvider` and `SetTools` are deleted. The `Tool` struct remains in `provider/` because it is the provider-agnostic tool definition that option constructors reference.

Provider sub-packages (e.g. `provider/openai`) define concrete option types and exported constructors:

```go
// In provider/openai
func WithTools(tools []provider.Tool) provider.InvokeOption
func WithTemperature(t float64) provider.InvokeOption
```

The `openai.Provider` reads these options locally inside `Invoke`/`InvokeStreaming` — no field mutation, no mutex. The underlying `openai.Client` (HTTP connection pool) remains shared.

`loop.Step` gains a pre-bound options slice and a `WithInvokeOptions` functional option:

```go
func WithInvokeOptions(opts ...provider.InvokeOption) Option
```

`Step.Turn` accepts per-call options, prepends the pre-bound slice, and passes the merged slice to the provider:

```go
func (s *Step) Turn(ctx context.Context, st state.State, p provider.Provider, opts ...provider.InvokeOption) (state.State, error)
```

This preserves `ReAct`'s blindness: `ReAct.Run` calls `Step.Turn(ctx, st, r.Provider)` with no extra arguments, and any pre-bound options configured by the application (e.g. tools) are carried by the `Step` itself. Applications that need dynamic per-turn options bypass `ReAct` and call `Step.Turn` directly.

### Evaluated Alternatives

| Alternative | Why Not Selected |
|---|---|
| **Immutable provider factory** (`WithTools` returns new `Provider`) | Cleaner than `SetTools`, but requires allocating a new provider struct per turn. Passing options at the invocation boundary is more direct and generalizes to temperature, max-tokens, etc. without additional factory methods. |
| **Tools in `State`** | Would require `state` to import `provider` for the `Tool` type, creating a cycle (`provider` already depends on `state`). Moving `Tool` to `artifact` is possible but pollutes the artifact package with a provider-concern. |
| **Separate `InvokeWithTools` method on `ToolProvider`** | Bloats the interface surface; `Step.Turn` would need to know about `ToolProvider` to choose the right method, leaking tool awareness into the loop. |
| **Generic request config struct** | Would need to live in `provider/` and encompass all possible provider-specific fields, or be an `any`-typed bag. Either bloats the generic interface or loses type safety. Options keep provider-specific types in provider sub-packages. |

## Requirements

1. [explicit] Add `provider.InvokeOption` interface with an exported marker method (`IsInvokeOption()`) so sub-packages can implement it.
2. [explicit] Update `Provider.Invoke` signature to accept `...InvokeOption`.
3. [explicit] Update `StreamingProvider.InvokeStreaming` signature to accept `...InvokeOption`.
4. [explicit] Remove `ToolProvider` interface and all `SetTools` implementations.
5. [explicit] Add `loop.WithInvokeOptions` functional option and `invokeOpts` field on `Step`.
6. [explicit] Update `Step.Turn` to accept `...provider.InvokeOption`, merge pre-bound + per-call options, and pass them to `Provider.Invoke` / `StreamingProvider.InvokeStreaming`.
7. [explicit] Add `openai.WithTools(tools []provider.Tool)` option constructor returning `provider.InvokeOption`.
8. [explicit] Add `openai.WithTemperature(t float64)` option constructor (demonstrates generalization of the pattern).
9. [explicit] Remove `sync.RWMutex`, `tools` field, and `SetTools` from `openai.Provider`.
10. [explicit] Update `openai.Provider.Invoke` and `InvokeStreaming` to read tools and temperature from the options slice locally.
11. [explicit] Update all mock providers in `loop/loop_test.go` and `cognitive/react_test.go` to match new interface signatures.
12. [explicit] Update `examples/calculator/main.go` to pre-bind `openai.WithTools(tools)` to the `Step` via `loop.WithInvokeOptions`.
13. [explicit] Update `examples/single-turn-cli/main.go` commented tool example to show `Turn` with `openai.WithTools` option.
14. [explicit] Update `README.md` to remove `ToolProvider`/`SetTools` references and document the `InvokeOption` pattern.
15. [inferred] Update `provider/openai/openai_test.go` to replace `SetTools`-based tests with option-based tests.

## Task Breakdown

### Task 1: Atomic Cross-Cutting Provider Interface Refactor
- **Goal**: Add `InvokeOption`, redesign `Provider`/`StreamingProvider`, remove `ToolProvider`, update all implementations, mocks, and call sites in a single atomic set of changes so the module compiles.
- **Dependencies**: None.
- **Files Affected**:
  - `provider/provider.go`
  - `loop/loop.go`
  - `loop/loop_test.go`
  - `provider/openai/openai.go`
  - `provider/openai/openai_test.go`
  - `cognitive/react_test.go`
- **New Files**: None.
- **Interfaces**:
  ```go
  type InvokeOption interface { IsInvokeOption() }

  type Provider interface {
      Invoke(ctx context.Context, s state.State, opts ...InvokeOption) ([]artifact.Artifact, error)
  }

  type StreamingProvider interface {
      Provider
      InvokeStreaming(ctx context.Context, s state.State, deltasCh chan<- artifact.Artifact, opts ...InvokeOption) ([]artifact.Artifact, error)
  }
  ```
  ```go
  // loop/loop.go additions
  func WithInvokeOptions(opts ...provider.InvokeOption) Option
  func (s *Step) Turn(ctx context.Context, st state.State, p provider.Provider, opts ...provider.InvokeOption) (state.State, error)
  ```
  ```go
  // provider/openai/openai.go additions
  func WithTools(tools []provider.Tool) provider.InvokeOption
  func WithTemperature(t float64) provider.InvokeOption
  ```
- **Details**:
  1. In `provider/provider.go`: add `InvokeOption` interface. Update `Provider.Invoke` and `StreamingProvider.InvokeStreaming` signatures. Delete `ToolProvider` interface entirely. Keep `Tool` struct.
  2. In `loop/loop.go`: add `invokeOpts []provider.InvokeOption` field to `Step`. Add `WithInvokeOptions` option constructor. Update `Turn` signature to accept `...provider.InvokeOption`. Merge pre-bound and per-call options safely (`make` + double `append`), then pass the merged slice to `p.Invoke` and `sp.InvokeStreaming`.
  3. In `loop/loop_test.go`: update `mockProvider.Invoke`, `mockStreamingProvider.Invoke` and `mockStreamingProvider.InvokeStreaming` to match new signatures (add `opts ...provider.InvokeOption`). Remove `SetTools` methods and `ToolProvider` compile-time checks. Update every `s.Turn(...)` call site — since options are variadic, most calls won't change, but the mock definitions must.
  4. In `provider/openai/openai.go`: remove `mu sync.RWMutex` and `tools []provider.Tool` fields from `Provider`. Remove `SetTools` method. Delete `var _ provider.ToolProvider = (*Provider)(nil)` compile-time check. Add `toolOption` struct and `WithTools` constructor. Add `temperatureOption` struct and `WithTemperature` constructor. Update `Invoke` and `InvokeStreaming` to iterate the `opts` slice, type-assert against `toolOption` and `temperatureOption`, and use the extracted values locally for request construction. No field mutation.
  5. In `provider/openai/openai_test.go`: delete `TestProviderInvoke_ConcurrentSetTools` (no longer relevant). Update `TestProviderInvoke_ToolsWithDescription` to pass `WithTools(tools)` as an option to `Invoke`. Add new table-driven tests verifying that `WithTools` and `WithTemperature` are correctly read and applied. Add a test verifying concurrent invocations with different option sets on the same provider instance (should be safe by design — no mutable state).
  6. In `cognitive/react_test.go`: update `simpleProvider.Invoke`, `countingProvider.Invoke`, `cancelCheckingProvider.Invoke`, and their `InvokeStreaming` stubs to accept the new variadic option parameter. Remove any `SetTools` stubs.
- **Validation**: `go build ./...` must compile with zero errors after this task. `go test -race ./...` must pass.

### Task 2: Update Examples to Use InvokeOption Pattern
- **Goal**: Migrate all example applications from `SetTools` to the options pattern.
- **Dependencies**: Task 1.
- **Files Affected**:
  - `examples/calculator/main.go`
  - `examples/single-turn-cli/main.go`
- **New Files**: None.
- **Interfaces**: None.
- **Details**:
  1. In `examples/calculator/main.go`: remove the `prov.SetTools(tools)` call. Instead, pre-bind the tools to the `Step` at construction time: `step := loop.New(loop.WithHandlers(registry.Handler()), loop.WithInvokeOptions(openai.WithTools(tools)))`. The `ReAct` and provider wiring remain unchanged.
  2. In `examples/single-turn-cli/main.go`: update the commented tool-calling block to show passing `openai.WithTools` either to `step.Turn` directly or via `loop.WithInvokeOptions`. Remove the `p.SetTools(...)` commented code.
- **Validation**: `go build ./examples/calculator` and `go build ./examples/single-turn-cli` compile cleanly.

### Task 3: Update README Documentation
- **Goal**: Remove all documentation referencing `ToolProvider`/`SetTools` and document the new `InvokeOption` pattern.
- **Dependencies**: Task 2.
- **Files Affected**:
  - `README.md`
- **New Files**: None.
- **Interfaces**: None.
- **Details**:
  1. Remove or rewrite the "Provider adapter" bullet that references `provider.ToolProvider.SetTools()`.
  2. Remove the `prov.SetTools` code example.
  3. Remove the paragraph about "Adapters that implement `provider.ToolProvider`... expose `SetTools`".
  4. Remove or rewrite the "Dynamic tool configuration" paragraph and its `if tp, ok := prov.(provider.ToolProvider)` example.
  5. Remove the claim that "`SetTools` is safe for concurrent use".
  6. Update the `provider/` package description in the project status section to remove `ToolProvider` and mention `InvokeOption` instead.
  7. Add a brief explanation of the `InvokeOption` pattern: provider sub-packages export option constructors (e.g. `openai.WithTools`), applications pass them to `Step.Turn` or pre-bind them via `loop.WithInvokeOptions`.
- **Validation**: A manual read-through confirms no stale references to `SetTools`, `ToolProvider`, or mutex safety remain.

### Task 4: Validate Full Test Suite
- **Goal**: Ensure the entire module passes tests with race detection after all changes.
- **Dependencies**: Task 3.
- **Files Affected**: None.
- **New Files**: None.
- **Interfaces**: None.
- **Details**:
  1. Run `go test -race ./...` and verify zero failures.
  2. Run `go build ./...` and verify zero errors.
  3. Run `go vet ./...` and verify zero issues.
- **Validation**: All commands return exit code 0.

## Dependency Graph

- Task 1 → Task 2 → Task 3 → Task 4

Task 1 is a cross-cutting atomic refactor; all subsequent tasks depend on it. Tasks 2–4 are strictly sequential because each validates a layer built on the previous one.

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| `InvokeOption` interface with exported marker (`IsInvokeOption()`) allows any type to satisfy it, creating potential for accidental option collisions across provider sub-packages | Medium | Medium | Document that option types should be unexported structs in their provider sub-package; type assertions inside `Invoke`/`InvokeStreaming` only match the specific struct types defined in the same package. Collisions are harmless (unrecognized options are silently ignored). |
| Unrecognized options silently ignored by a provider, causing user confusion | Medium | Low | Document the silent-ignore behavior. Future: consider a strict-mode option or provider-specific validation. |
| Breaking change to `Provider` interface forces updating all implementations atomically; a partial implementation leaves the repo uncompilable | High | High | Task 1 is defined as a single atomic commit touching all affected files. The builder must not commit partial changes. |
| `Step.Turn` variadic options could accidentally mutate the pre-bound `invokeOpts` slice if `append` is used incorrectly | Medium | Low | Use `make` + double `append` (or `slices.Concat` if Go 1.22+) to build a fresh merged slice, never mutating `s.invokeOpts`. |
| Examples no longer demonstrate mid-session tool changes (previously done via `SetTools`) | Low | Medium | The README and example comments should clarify that per-turn dynamic options are achieved by calling `Step.Turn` directly with different option sets, bypassing `ReAct` when needed. |

## Validation Criteria

- [ ] `go build ./...` succeeds with no errors.
- [ ] `go test -race ./...` passes with no failures.
- [ ] `go vet ./...` reports no issues.
- [ ] `provider.ToolProvider` interface does not exist in `provider/provider.go`.
- [ ] `provider/openai/openai.go` does not contain `SetTools`, `sync.RWMutex`, or a mutable `tools` field.
- [ ] `openai.WithTools(tools)` returns `provider.InvokeOption` and is used in at least one test and one example.
- [ ] `loop.Step.Turn` accepts `...provider.InvokeOption` and passes merged pre-bound + per-call options to the provider.
- [ ] `cognitive.ReAct.Run` compiles without changes (it calls `Step.Turn` with no extra options).
- [ ] `README.md` contains no references to `SetTools`, `ToolProvider`, or mutex safety.
