# Plan: Make SummarizeStrategy Token-Aware

## Objective

Redesign `SummarizeStrategy` in `x/compaction` to be exclusively token-aware, removing the `PreserveLastN` field entirely. The strategy currently silently no-ops when `len(turns) <= PreserveLastN`, even when the accumulated conversation exceeds the provider's context budget. The new design walks backwards from the last turn, accumulating token estimates, finds the split point where the suffix fits within the budget, and summarizes the prefix into a single `RoleSystem` turn. If the entire history fits, it returns a defensive copy verbatim (no-op).

## Context

### Relevant Files
- `x/compaction/summarize.go` — `SummarizeStrategy` struct and `Compact` method
- `x/compaction/summarize_test.go` — all existing tests reference `PreserveLastN`
- `x/compaction/doc.go` — package documentation example shows `PreserveLastN: 2`
- `x/compaction/compaction.go` — `TokenUsageTrigger` (uses `MaxTokens`, provides naming precedent)
- `x/compaction/compaction_test.go` — does not reference `SummarizeStrategy` directly

### Current Behavior (Defect)
```go
func (s SummarizeStrategy) Compact(ctx context.Context, turns []state.Turn) ([]state.Turn, error) {
    if len(turns) <= s.PreserveLastN {
        result := make([]state.Turn, len(turns))
        copy(result, turns)
        return result, nil  // silently no-ops
    }
    // summarization only happens if len(turns) > PreserveLastN
}
```

A session with 10 turns of 50k tokens each never compacts when `PreserveLastN = 10`, even though the provider's context limit is exceeded.

### Project Conventions
- The `AGENTS.md` conventions state: "At this stage of the project, prefer aggressive refactoring — rename packages, move files, delete indirection, and break internal APIs when doing so produces cleaner module boundaries."
- `SummarizeStrategy` is only referenced within `x/compaction` in the current worktree (no external consumers in this repository).
- `TokenUsageTrigger` already uses `MaxTokens` as a field name for token thresholds.

### Artifact and State Types
- `state.Turn` contains `Role`, `Artifacts []artifact.Artifact`, and `Timestamp`.
- `artifact.Text` has `Content string`.
- `artifact.Usage` has `TotalTokens int`, but `TotalTokens` is cumulative for the entire conversation at the point it was emitted, not per-turn. It is unsuitable for per-turn estimation.
- `provider.Provider` interface requires `Invoke(ctx, state.State, ch chan<- artifact.Artifact, opts ...provider.InvokeOption) error`.

## Architectural Blueprint

### Selected Approach: Token-Budget Suffix Preservation

Remove `PreserveLastN` from `SummarizeStrategy`. Add `MaxTokens int` (consistent with `TokenUsageTrigger`). Implement a simple per-turn token estimation helper that sums `len(artifact.Text.Content) / 4` for all text artifacts in a turn. This is the standard rough heuristic (~4 characters per token) and requires no external dependencies.

**Algorithm:**
1. Validate `MaxTokens > 0`.
2. Compute total estimated tokens for all turns.
3. If total <= `MaxTokens`, return a defensive copy (no-op).
4. Walk backwards from the last turn, accumulating token estimates.
5. Find split point `K` such that `turns[K..N]` fits within `MaxTokens`. Always preserve at least the last turn (best effort — if the last turn alone exceeds the budget, it is still preserved; the strategy is a reducer, not a strict enforcer).
6. Summarize `turns[0..K-1]` via the provider, producing a single `RoleSystem` turn.
7. Return `[systemSummary] + turns[K..N]`.

### Why not keep `PreserveLastN` as an optional override?
The issue explicitly calls for removing it entirely: "This removes the `PreserveLastN` config field from workshop and simplifies the mental model to: 'everything that doesn't fit gets summarized.'" Adding it back as an optional field would reintroduce the same mental model complexity and the same footgun.

### Why not use `artifact.Usage` for estimation?
`Usage.TotalTokens` is cumulative across the conversation history (it includes the prompt, which is all prior turns, plus the completion). Using it per-turn would double-count. Per-turn estimation from text content is the only consistent and dependency-free approach available.

### Why not expose a `TokenEstimator` interface?
The project conventions favor minimal APIs. An unexported helper function is sufficient. Future consumers needing custom estimation can implement their own `Strategy`.

## Requirements

1. `SummarizeStrategy` must no longer have a `PreserveLastN` field.
2. `SummarizeStrategy` must gain a `MaxTokens int` field representing the token budget for the preserved suffix.
3. `Compact` must estimate tokens per turn and decide the split point based on the budget, not turn count.
4. If the entire history fits within `MaxTokens`, `Compact` must return a defensive copy without calling the provider (no-op).
5. `Compact` must always preserve at least the last turn (best effort), even if that turn alone exceeds `MaxTokens`.
6. All existing `SummarizeStrategy` tests must be rewritten to use `MaxTokens` and token-aware assertions.
7. `x/compaction/doc.go` must be updated to remove `PreserveLastN` from the example and describe the new token-aware behavior.
8. The package must remain dependency-free (no external tokenizers).

## Task Breakdown

### Task 1: Redesign SummarizeStrategy Struct and Compact Method
- **Goal**: Remove `PreserveLastN`, add `MaxTokens`, and implement token-aware split logic.
- **Dependencies**: None.
- **Files Affected**: `x/compaction/summarize.go`
- **New Files**: None.
- **Interfaces**:
  - `SummarizeStrategy` struct changes: remove `PreserveLastN int`, add `MaxTokens int`.
  - New unexported helper: `func estimateTokens(turns []state.Turn) int` — sums `len(text.Content)/4` for all `artifact.Text` in each turn.
  - `Compact` signature unchanged: `func (s SummarizeStrategy) Compact(ctx context.Context, turns []state.Turn) ([]state.Turn, error)`.
- **Validation**:
  - `go test ./x/compaction/...` passes (tests will fail until Task 2).
  - `go build ./x/compaction/...` passes.
- **Details**:
  - Validate `MaxTokens > 0` at the start of `Compact`; return `fmt.Errorf` if violated.
  - Implement `estimateTokens` as a package-level unexported function. Use integer division (`len(content) / 4`). Ignore non-`Text` artifacts for estimation (consistent with current summarization behavior, which only collects `Text` from provider responses).
  - If `len(turns) == 0`, return empty slice.
  - If `estimateTokens(turns) <= s.MaxTokens`, return defensive copy.
  - Walk backwards: start from `len(turns)-2` (always preserve last turn), accumulate estimates, stop when adding the next turn would exceed `MaxTokens`. Set `K = i + 1` at the stop point. If loop reaches `i == 0`, set `K = 0`.
  - The summarization logic (calling the provider, collecting text responses, building the `RoleSystem` turn) remains identical to the current implementation; only the split logic changes.

### Task 2: Rewrite SummarizeStrategy Tests
- **Goal**: Replace all `PreserveLastN`-based tests with `MaxTokens`-based tests that validate token-aware behavior.
- **Dependencies**: Task 1.
- **Files Affected**: `x/compaction/summarize_test.go`
- **New Files**: None.
- **Interfaces**: None new.
- **Validation**:
  - `go test -race ./x/compaction/...` passes.
  - All test cases exercise the new behavior and do not reference `PreserveLastN`.
- **Details**:
  - Use text content with predictable lengths to control token estimates. For example, `"aaaa"` (4 chars) = 1 token, `"aaaaaaaa"` (8 chars) = 2 tokens, `"aaaaaaaaaaaa"` (12 chars) = 3 tokens.
  - **Test cases to include** (table-driven where possible):
    1. `TestSummarizeStrategy_ReducesTurns` — 4 turns of 1 token each, `MaxTokens=3`. Expect summary + 3 preserved turns. (Equivalent to old `PreserveLastN=2` behavior with 4 turns, but now driven by budget.)
    2. `TestSummarizeStrategy_NoOpWhenUnderBudget` — 2 turns of 1 token each, `MaxTokens=5`. Expect defensive copy, no provider call.
    3. `TestSummarizeStrategy_NoOpExactBudget` — 3 turns of 1 token each, `MaxTokens=3`. Expect defensive copy (exact fit is a no-op).
    4. `TestSummarizeStrategy_PropagatesProviderError` — history exceeds budget, provider returns error. Expect error propagation.
    5. `TestSummarizeStrategy_IgnoresNonTextArtifacts` — non-`Text` artifacts do not affect token estimation or summary collection.
    6. `TestSummarizeStrategy_MultipleTextArtifactsConcatenated` — provider returns multiple `Text` artifacts; summary concatenates them.
    7. `TestSummarizeStrategy_ZeroMaxTokens` — `MaxTokens=0` or negative. Expect error (`MaxTokens must be > 0`).
    8. `TestSummarizeStrategy_LastTurnAloneExceedsBudget` — 2 turns: turn 0 = 1 token, turn 1 = 4 tokens. `MaxTokens=3`. Expect summary of turn 0 + preservation of turn 1 (best effort).
    9. `TestSummarizeStrategy_EmptyTurns` — empty input. Expect empty output.
    10. `TestSummarizeStrategy_SingleTurnUnderBudget` — 1 turn of 1 token, `MaxTokens=5`. Expect defensive copy.

### Task 3: Update Package Documentation
- **Goal**: Update `doc.go` to remove `PreserveLastN` from the example and describe the token-aware behavior.
- **Dependencies**: Task 1.
- **Files Affected**: `x/compaction/doc.go`
- **New Files**: None.
- **Interfaces**: None.
- **Validation**:
  - `go test ./x/compaction/...` passes (doc changes do not break code).
- **Details**:
  - Update the `SummarizeStrategy` description in the `# Built-in Strategies` section to say it is token-aware and uses `MaxTokens`.
  - Update the application wiring example:
    ```go
    compaction.WithStrategy(compaction.SummarizeStrategy{Provider: prov, MaxTokens: 8000}),
    ```
  - Remove any mention of `PreserveLastN`.

### Task 4: Verify No External Consumers Are Broken
- **Goal**: Ensure no files outside `x/compaction` reference `PreserveLastN` or `SummarizeStrategy` in the current worktree.
- **Dependencies**: Task 1, Task 2, Task 3.
- **Files Affected**: None (verification task).
- **New Files**: None.
- **Validation**:
  - `grep -r "PreserveLastN" --include="*.go" . | grep -v "\.worktrees/" | grep -v "x/compaction/"` returns no matches.
  - `grep -r "SummarizeStrategy" --include="*.go" . | grep -v "\.worktrees/" | grep -v "x/compaction/"` returns no matches.
  - `go test ./...` passes.
  - `go test -race ./...` passes.
  - `go vet ./...` is clean.
- **Details**:
  - This is a pure verification task. If any external references exist, they must be fixed in the same PR (or in a separate task if the scope expands).

## Dependency Graph

- Task 1 → Task 2 (tests depend on the new struct fields and algorithm)
- Task 1 → Task 3 (documentation depends on the new API)
- Task 2 || Task 3 (parallelizable after Task 1)
- Task 2 → Task 4
- Task 3 → Task 4

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Token estimation heuristic (`len/4`) is inaccurate for non-English text or code | Medium | Medium | Document the heuristic as a rough approximation. The strategy is a best-effort reducer, not a strict enforcer. Applications can set `MaxTokens` with a safety margin. |
| Removing `PreserveLastN` breaks external consumers in other repositories (e.g., `workshop`) | Medium | High | The issue explicitly requests this removal. Per project conventions, aggressive refactoring is preferred. Document the breaking change in the commit message. |
| A single turn with massive content exceeds `MaxTokens` alone, causing everything else to be summarized | Low | Medium | The strategy always preserves at least the last turn (best effort). Applications can chain `KeepLastN` before `SummarizeStrategy` as a safety net if they need a hard turn-count cap. |
| `estimateTokens` double-counts if `Usage` artifacts are present in turns | Low | Low | `estimateTokens` must NOT use `Usage.TotalTokens` (it is cumulative). The plan explicitly specifies text-content-only estimation. |
| Integer division in `len/4` causes zero-token estimates for very short text (<4 chars) | Low | Low | Short text is negligible in context budgets. A zero estimate just means the turn is "free" in budget terms, which is harmless. |

## Validation Criteria

- [ ] `x/compaction/summarize.go` compiles and `SummarizeStrategy` has no `PreserveLastN` field.
- [ ] `SummarizeStrategy` has a `MaxTokens int` field and `Compact` validates it is `> 0`.
- [ ] `Compact` returns a defensive copy without calling the provider when total estimated tokens are `<= MaxTokens`.
- [ ] `Compact` summarizes the prefix and preserves the suffix when the budget is exceeded.
- [ ] `Compact` always preserves at least the last turn (best effort).
- [ ] `go test -race ./x/compaction/...` passes with all rewritten tests.
- [ ] `go test ./...` passes repository-wide.
- [ ] `go vet ./...` is clean.
- [ ] `x/compaction/doc.go` contains no references to `PreserveLastN` and documents the token-aware behavior.
- [ ] No files outside `x/compaction` reference `PreserveLastN` or `SummarizeStrategy` in the current worktree.
