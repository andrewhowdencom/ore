# Plan: Replace SetTools with Immutable Provider Clones

## Objective

Eliminate all shared mutable state from the OpenAI provider adapter by removing `SetTools`, the `sync.RWMutex`, and the `provider.ToolProvider` capability interface. Replace them with immutable clone methods on the concrete provider type (`openai.Provider.WithTools`, `openai.Provider.WithTemperature`). The base `Provider` and `StreamingProvider` interfaces remain unchanged; `loop.Step.Turn` and `cognitive.ReAct` require no modifications. Each provider instance becomes a lightweight, immutable value object sharing only the underlying HTTP client infrastructure.

## Context

The repository is an active Go module (`github.com/andrewhowdencom/ore`) with the following relevant structure:

- **`provider/provider.go`** defines `Provider`, `StreamingProvider`, and `ToolProvider` interfaces. `ToolProvider` adds `SetTools(tools []Tool) error`.
- **`provider/openai/openai.go`** implements all three interfaces. It stores a mutable `tools []provider.Tool` slice protected by a `sync.RWMutex`. `SetTools` acquires a write lock; `Invoke` and `InvokeStreaming` acquire read locks around access.
- **`loop/loop.go`** defines `Step.Turn(ctx, state, provider.Provider)` which calls `Provider.Invoke` or `StreamingProvider.InvokeStreaming`. It never references `ToolProvider`.
- **`cognitive/react.go`** defines `ReAct.Run(ctx, state)` which loops calling `Step.Turn`. It never references `ToolProvider`.
- **`examples/calculator/main.go`** calls `prov.SetTools(tools)` before constructing `ReAct`.
- **`examples/single-turn-cli/main.go`** contains a commented block showing `SetTools` usage.
- **Conventions (from `AGENTS.md`)**: functional options pattern for constructors, `fmt.Errorf("...: %w", err)` wrapping, table-driven tests, `go test -race ./...`.

The `SetTools`/`mutex` pattern was introduced to allow dynamic tool configuration, but it creates a logical race in multi-session workloads: two goroutines sharing a provider instance can clobber each other's tool configuration. The selected design removes all mutable provider state, making each configuration change produce a new provider instance.

## Architectural Blueprint

### Selected Architecture: Immutable Provider Value Objects

The `openai.Provider` struct becomes a pure value object: all fields are set at construction or clone time and never mutated afterward.

```go
type Provider struct {
    client      openai.Client
    model       string
    tools       []provider.Tool      // immutable after construction/clone
    temperature float64              // immutable after construction/clone
}
```

Clone methods derive a new provider sharing the expensive `client` while replacing configuration fields:

```go
func (p *Provider) WithTools(tools []provider.Tool) *Provider
func (p *Provider) WithTemperature(t float64) *Provider
```

`Invoke` and `InvokeStreaming` read `p.tools` and `p.temperature` directly — no synchronization needed because the fields are immutable.

The base interfaces remain unchanged:

```go
type Provider interface {
    Invoke(ctx context.Context, s state.State) ([]artifact.Artifact, error)
}

type StreamingProvider interface {
    Provider
    InvokeStreaming(ctx context.Context, s state.State, deltasCh chan<- artifact.Artifact) ([]artifact.Artifact, error)
}
```

### Evaluated Alternatives

| Alternative | Why Not Selected |
|---|---|
| **`InvokeOption` variadic parameter on `Provider.Invoke`** | Over-abstracts provider-specific configuration (temperature, tools) through a generic interface. Forces `loop.Step.Turn` and all mock providers to accept and pass through opaque options. Adds indirection without clear benefit over direct clone methods. |
| **Generic request config struct** | Would need to live in `provider/` and encompass all possible provider-specific fields, or be an `any`-typed bag. Either bloats the generic interface or loses type safety. Clone methods keep provider-specific types in the provider package where they belong. |
| **`ToolProvider.WithTools` returning `provider.Provider` (Issue #18's deeper insight)** | Cleaner than `SetTools`, but still requires a capability interface (`ToolProvider`) that `loop` and `ReAct` don't need. With clone methods on the concrete type, no capability interface is needed at all. |

## Requirements

1. [explicit] Remove the `provider.ToolProvider` interface from `provider/provider.go`.
2. [explicit] Remove `SetTools` method, `sync.RWMutex` field, and `var _ provider.ToolProvider = (*Provider)(nil)` compile-time check from `provider/openai/openai.go`.
3. [explicit] Add `openai.Provider.WithTools(tools []provider.Tool) *Provider` clone method.
4. [explicit] Add `openai.Provider.WithTemperature(t float64) *Provider` clone method.
5. [explicit] Update `openai.Provider.Invoke` and `InvokeStreaming` to read `p.tools` and `p.temperature` directly (no locking).
6. [explicit] Apply `p.temperature` to the chat completion request parameters when non-zero.
7. [explicit] Update `provider/openai/openai_test.go`: remove `TestProviderInvoke_ConcurrentSetTools`, update tool tests to use `WithTools`, add temperature test, add test verifying cloned providers are independent.
8. [explicit] Update `loop/loop_test.go`: remove `SetTools` stub methods and `provider.ToolProvider` compile-time checks from `mockProvider` and `mockStreamingProvider`.
9. [explicit] Update `examples/calculator/main.go`: replace `prov.SetTools(tools)` with `prov.WithTools(tools)` before passing to `ReAct`.
10. [explicit] Update `examples/single-turn-cli/main.go`: update commented tool example to show `WithTools` clone method.
11. [explicit] Update `README.md`: remove all `ToolProvider`, `SetTools`, and mutex safety references; document the immutable clone pattern.
12. [inferred] Verify `cognitive/react_test.go` compiles without changes (no `ToolProvider` references expected).

## Task Breakdown

### Task 1: Atomic Cross-Cutting Refactor — Remove Mutable Provider State
- **Goal**: Remove `ToolProvider`, `SetTools`, and the mutex; add immutable clone methods; update all implementations and mocks atomically so the module compiles.
- **Dependencies**: None.
- **Files Affected**:
  - `provider/provider.go`
  - `provider/openai/openai.go`
  - `provider/openai/openai_test.go`
  - `loop/loop_test.go`
- **New Files**: None.
- **Interfaces**:
  - Deleted: `provider.ToolProvider` interface.
  - Added: `func (p *Provider) WithTools(tools []provider.Tool) *Provider`
  - Added: `func (p *Provider) WithTemperature(t float64) *Provider`
- **Details**:
  1. In `provider/provider.go`: delete the `ToolProvider` interface definition and its `SetTools` method.
  2. In `provider/openai/openai.go`: remove the `mu sync.RWMutex` field from `Provider`. Add `temperature float64` field. Keep `tools []provider.Tool` (now immutable). Delete `SetTools` method and the `var _ provider.ToolProvider = (*Provider)(nil)` compile-time check. Add `WithTools` returning `&Provider{client: p.client, model: p.model, tools: tools, temperature: p.temperature}`. Add `WithTemperature` returning `&Provider{client: p.client, model: p.model, tools: p.tools, temperature: t}`. Update `Invoke` and `InvokeStreaming` to read `p.tools` directly instead of `p.mu.RLock(); tools := p.tools; p.mu.RUnlock()`. When `p.temperature != 0`, set the `Temperature` field on `openai.ChatCompletionNewParams`.
  3. In `provider/openai/openai_test.go`: delete `TestProviderInvoke_ConcurrentSetTools` entirely (it tests mutex behavior that no longer exists). Update `TestProviderInvoke_ToolsWithDescription` to construct the provider normally then call `WithTools(tools)` before invoking. Add `TestProviderWithTemperature` verifying the temperature parameter is serialized into the request body. Add `TestProviderWithTools_Isolation` verifying that two providers cloned from the same base with different tool sets produce different request bodies (no shared mutable state).
  4. In `loop/loop_test.go`: delete `SetTools` stub methods from `mockProvider` and `mockStreamingProvider`. Delete `var _ provider.ToolProvider = (*mockProvider)(nil)` and `var _ provider.ToolProvider = (*mockStreamingProvider)(nil)`.
- **Validation**: `go build ./...` compiles with zero errors. `go test ./provider/openai` and `go test ./loop` pass.

### Task 2: Update Examples to Use Immutable Clone Methods
- **Goal**: Migrate example applications from `SetTools` to `WithTools`.
- **Dependencies**: Task 1.
- **Files Affected**:
  - `examples/calculator/main.go`
  - `examples/single-turn-cli/main.go`
- **New Files**: None.
- **Interfaces**: None.
- **Details**:
  1. In `examples/calculator/main.go`: after `prov := openai.New(apiKey, model, opts...)`, replace `if err := prov.SetTools(tools); err != nil { return err }` with `prov = prov.WithTools(tools)`. Pass `prov` to `ReAct` as before.
  2. In `examples/single-turn-cli/main.go`: update the commented tool-calling block to show `p := openai.New(apiKey, model, opts...).WithTools([]provider.Tool{...})` instead of `p.SetTools`.
- **Validation**: `go build ./examples/calculator` and `go build ./examples/single-turn-cli` compile cleanly.

### Task 3: Update README Documentation
- **Goal**: Remove all references to `ToolProvider`, `SetTools`, and mutex safety; document the immutable clone pattern.
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
  6. Update the `provider/` package description in the project status section to remove `ToolProvider`.
  7. Add a brief explanation of the immutable clone pattern: provider sub-packages expose clone methods (e.g. `openai.Provider.WithTools`) that return new provider instances sharing the underlying HTTP client. No mutex, no races, no cross-session leakage.
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
| Breaking change to provider interface (removing `ToolProvider`) forces atomic update across multiple packages | High | Certain | Task 1 is defined as a single atomic commit touching all affected files. The builder must not commit partial changes. |
| `tools` slice header is copied between clones, not deep-copied; external mutation of the underlying array could affect all clones | Low | Low | Document that callers should treat the passed `[]provider.Tool` as immutable after cloning. The `Tool` struct itself contains only value types (string, map) so the risk is minimal. |
| `temperature` uses `0` as "unset" sentinel, preventing explicit `temperature: 0` (deterministic) from being sent to the API | Low | Medium | Document the convention. If needed in the future, change the field to `*float64` (pointer) where `nil` means unset. |
| Existing external code importing `provider.ToolProvider` will break on upgrade | Medium | Low | This is a learning/experimental project; breaking changes are acceptable. The commit message should clearly flag the breaking change. |

## Validation Criteria

- [ ] `go build ./...` succeeds with no errors.
- [ ] `go test -race ./...` passes with no failures.
- [ ] `go vet ./...` reports no issues.
- [ ] `provider.ToolProvider` interface does not exist in `provider/provider.go`.
- [ ] `provider/openai/openai.go` does not contain `SetTools`, `sync.RWMutex`, `mu`, or `ToolProvider` compile-time checks.
- [ ] `openai.Provider.WithTools(tools)` exists and returns `*Provider`.
- [ ] `openai.Provider.WithTemperature(t)` exists and returns `*Provider`.
- [ ] `openai.Provider.Invoke` reads `p.tools` directly without locking.
- [ ] `loop/loop_test.go` mock providers do not implement `SetTools` or reference `ToolProvider`.
- [ ] `examples/calculator/main.go` compiles and uses `WithTools` instead of `SetTools`.
- [ ] `README.md` contains no references to `SetTools`, `ToolProvider`, or mutex safety.
