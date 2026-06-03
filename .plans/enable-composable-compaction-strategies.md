# Plan: Enable Composable Compaction Strategies

## Objective

Fix `Compactor.WithStrategy` so that multiple calls compose into a sequential pipeline rather than silently overwriting each other. Add a `ChainStrategy` type as an explicit composition primitive. This enables downstream consumers to build defensive compaction pipelines (e.g., summarize first, then truncate as a fallback) without losing earlier strategies.

## Context

The `x/compaction` package provides a `Compactor` that coordinates a `Trigger` (when to compact) and a `Strategy` (how to compact). The current implementation stores only a single `Strategy`:

```go
type Compactor struct {
    trigger  Trigger
    strategy Strategy
}
```

`WithStrategy` sets `c.strategy = s` directly, so the last call wins. The `doc.go` already encourages "chaining strategies" for defensive composition, but the API provides no mechanism to do so. Issue #333 was discovered when `workshop` configured:

```go
compaction.New(
    WithStrategy(SummarizeStrategy{...}),
    WithStrategy(KeepLastN{...}), // silently overwrites SummarizeStrategy
)
```

Relevant files:
- `x/compaction/compaction.go` — `Compactor`, `WithStrategy`, `MaybeCompact`, built-in triggers/strategies
- `x/compaction/compaction_test.go` — existing tests for `Compactor` and built-in strategies
- `x/compaction/summarize.go` — `SummarizeStrategy` (already has internal `PreserveLastN`)
- `x/compaction/summarize_test.go` — tests for summarization
- `x/compaction/doc.go` — package documentation referencing strategy chaining

## Architectural Blueprint

### Selected Approach: Hybrid (Auto-accumulation + Explicit `ChainStrategy`)

1. **Add `ChainStrategy`** — a new `Strategy` implementation that holds `[]Strategy` and runs them in sequence, piping the output of strategy *N* into the input of strategy *N+1*.
2. **Update `Compactor`** — change the internal strategy field to a slice `[]Strategy`. `WithStrategy` appends rather than overwrites. `MaybeCompact` uses `ChainStrategy` internally when multiple strategies are registered, or runs the single strategy directly when only one is present.
3. **Error behavior** — if any strategy in the chain fails, the chain stops and returns the error. No partial results are emitted.
4. **Backward compatibility** — zero or one `WithStrategy` call behaves exactly as before.

This mirrors patterns used elsewhere in the codebase (e.g., `WithTransforms` in `loop.Step`) where options accumulate, and provides an explicit `ChainStrategy` for programmatic composition outside of `Compactor`.

### Why not pure `ChainStrategy` (Approach B)?
It would not fix the footgun: users would still silently lose strategies when calling `WithStrategy` multiple times. Documentation is insufficient for a behavior that contradicts the API name.

### Why not pure slice (Approach A)?
It works but provides no reusable composition primitive. `ChainStrategy` is useful when building strategy chains programmatically or when a chain needs to be passed as a single strategy to other components.

## Requirements

1. Multiple `WithStrategy` calls in `compaction.New(...)` must compose all registered strategies into a sequential pipeline.
2. The pipeline must execute strategies in registration order, piping output from strategy *N* into input for strategy *N+1*.
3. If any strategy in the chain returns an error, the entire `MaybeCompact` call must fail with that error.
4. A public `ChainStrategy` type must be available for explicit composition outside `Compactor`.
5. Existing single-strategy configurations must remain backward compatible.
6. All existing tests must continue to pass without modification.
7. Package documentation (`doc.go`) must be updated to describe chaining behavior.

## Task Breakdown

### Task 1: Add `ChainStrategy` Type and Tests
- **Goal**: Implement a reusable strategy chain that runs multiple strategies sequentially.
- **Dependencies**: None.
- **Files Affected**: `x/compaction/compaction.go`, `x/compaction/compaction_test.go`
- **New Files**: None.
- **Interfaces**:
  - New type `ChainStrategy struct { strategies []Strategy }`
  - New method `(c ChainStrategy) Compact(ctx context.Context, turns []state.Turn) ([]state.Turn, error)`
- **Validation**:
  - `go test ./x/compaction/...` passes
  - New tests cover: empty chain (no-op), single strategy in chain, multiple strategies in chain, error propagation mid-chain, defensive copy behavior
- **Details**:
  - Add `ChainStrategy` to `x/compaction/compaction.go` near the existing `Strategy` interface.
  - `Compact` iterates over `c.strategies`, passing the output slice of each iteration as the input to the next.
  - Return a defensive copy even when no strategies run (to match existing behavior).
  - Add table-driven tests in `x/compaction/compaction_test.go`.

### Task 2: Update `Compactor` to Accumulate Strategies
- **Goal**: Change `Compactor` internals so `WithStrategy` appends and `MaybeCompact` auto-composes multiple strategies.
- **Dependencies**: Task 1.
- **Files Affected**: `x/compaction/compaction.go`
- **New Files**: None.
- **Interfaces**:
  - `Compactor` field changes from `strategy Strategy` to `strategies []Strategy`
  - `WithStrategy(s Strategy) Option` now appends: `c.strategies = append(c.strategies, s)`
  - `MaybeCompact` logic:
    - If `len(strategies) == 0`: no-op (return original turns, false, nil)
    - If `len(strategies) == 1`: call `strategies[0].Compact` directly
    - If `len(strategies) > 1`: create `ChainStrategy{strategies: c.strategies}` and call `Compact`
- **Validation**:
  - All existing tests in `x/compaction/compaction_test.go` pass unchanged
  - `go test -race ./x/compaction/...` passes
- **Details**:
  - Do not rename exported API. `WithStrategy` keeps its signature.
  - Preserve the existing `MaybeCompact` return semantics: returns `true` when trigger fires and strategy execution succeeds (even if the final turn count is unchanged).
  - Ensure the error wrapping message `"compaction strategy failed: %w"` is still applied at the `Compactor` level.

### Task 3: Add Integration Tests for Composed Strategies
- **Goal**: Verify that multiple `WithStrategy` calls in `compaction.New` produce correct pipelined behavior.
- **Dependencies**: Task 2.
- **Files Affected**: `x/compaction/compaction_test.go`
- **New Files**: None.
- **Interfaces**: None new.
- **Validation**:
  - `go test ./x/compaction/...` passes
- **Details**:
  - Test case: `compaction.New(WithTrigger(...), WithStrategy(KeepLastN{N: 5}), WithStrategy(KeepLastN{N: 2}))` — expect that 6 turns are first reduced to 5, then to 2.
  - Test case: chaining with a strategy that returns an error — expect the error propagates and downstream strategies do not run.
  - Test case: single `WithStrategy` behaves identically to before (backward compatibility).

### Task 4: Update Package Documentation
- **Goal**: Document `ChainStrategy` and clarify that `WithStrategy` accumulates.
- **Dependencies**: Task 2.
- **Files Affected**: `x/compaction/doc.go`
- **New Files**: None.
- **Interfaces**: None.
- **Validation**:
  - `go test ./x/compaction/...` passes (doc.go changes should not break tests)
  - Documentation is consistent with code behavior
- **Details**:
  - Add a `ChainStrategy` section under `# Built-in Strategies` (or a new `# Strategy Composition` section).
  - Update the `Application wiring` example to show chaining:
    ```go
    compactor := compaction.New(
        compaction.WithTrigger(compaction.TurnCountTrigger{N: 20}),
        compaction.WithStrategy(compaction.KeepLastN{N: 10}),
        compaction.WithStrategy(compaction.SummarizeStrategy{Provider: prov, PreserveLastN: 2}),
    )
    ```
  - Clarify that `WithStrategy` appends; use `compaction.New(compaction.WithStrategy(...))` for a single strategy.

### Task 5: Run Full Test Suite and Race Detector
- **Goal**: Ensure the change does not introduce regressions in the broader codebase.
- **Dependencies**: Task 3, Task 4.
- **Files Affected**: None (verification task).
- **New Files**: None.
- **Interfaces**: None.
- **Validation**:
  - `go test ./...` passes
  - `go test -race ./...` passes
  - `go vet ./...` is clean
- **Details**:
  - This is a pure verification task. No code changes. If any tests fail, investigate and fix in the relevant task above.

## Dependency Graph

- Task 1 → Task 2 (Task 2 depends on `ChainStrategy` existing)
- Task 2 → Task 3 (Task 3 tests the updated `Compactor` behavior)
- Task 2 → Task 4 (Task 4 documents the updated behavior)
- Task 3 || Task 4 (parallelizable after Task 2)
- Task 3 → Task 5
- Task 4 → Task 5

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Changing `Compactor` internal field type breaks external consumers that type-assert or reflect on `Compactor` | Low | Low | `Compactor` fields are unexported; external consumers cannot access them. No exported API changes. |
| Strategy chain errors leave state in an inconsistent partial-compaction state | Medium | Low | Chain stops on first error and returns original `turns` slice unmodified (the error path in `MaybeCompact` returns `nil, false, err`). |
| Existing tests rely on `WithStrategy` overwrite behavior (unlikely) | Low | Very Low | All existing tests use a single `WithStrategy` call; Task 3 adds multi-strategy tests. If any test fails, it will be caught in Task 2 validation. |
| Performance overhead from defensive copying in `ChainStrategy` | Low | Low | Existing strategies already make defensive copies. Chain just adds O(N) slice copies where N = number of strategies. Acceptable for compaction frequency. |

## Validation Criteria

- [ ] `go test ./x/compaction/...` passes with all existing tests unchanged.
- [ ] New tests exist for `ChainStrategy` (empty, single, multiple, error propagation).
- [ ] New tests exist for `Compactor` with multiple `WithStrategy` calls (accumulation and piping).
- [ ] `go test -race ./x/compaction/...` passes.
- [ ] `go test ./...` passes repository-wide.
- [ ] `go vet ./...` is clean.
- [ ] `x/compaction/doc.go` documents `ChainStrategy` and the accumulation behavior of `WithStrategy`.
- [ ] Calling `WithStrategy` multiple times does not produce a compiler or runtime error, and all strategies execute in order.
