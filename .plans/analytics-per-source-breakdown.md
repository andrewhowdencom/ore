# Plan: Per-Source Breakdown for x/analytics

## Objective

Extend `x/analytics` to report context-window cost on a second axis — the artifact's `Source` — so a cost-hunting operator can see *which* tool is responsible for context bloat, not just *that* tool results are bloat. The current report groups by `Kind()` only; the new report groups by `(Kind, Source)` in a single flat table, with `Source` populated from `artifact.ToolCall.Name` for tool calls and joined from the same turn's originating `ToolCall` for tool results. Orphan tool results (whose `ToolCallID` does not resolve within the same turn) bucket under `"(unknown)"` for visibility. The API is broken cleanly — no shadow functions, no deprecation aliases.

## Context

Findings from repository inspection (worktree `feat/analytics-per-source-breakdown`, base `0a6899d` on `main`):

- `x/analytics/analytics.go` is the only source file (~115 lines). It exports `KindStats{Kind, Count, Bytes}` and three functions: `AnalyzeTurns`, `AnalyzeThread`, `AnalyzeStore`. All three aggregate by `art.Kind()` only. The `Source` field is not present anywhere.
- `x/analytics/analytics_test.go` (~350 lines) covers empty input, mixed kinds, custom (unknown) artifacts, nil/empty thread, and store-wide aggregation. The `TestAnalyzeThread_AfterJSONRoundTrip` and `TestAnalyzeStore_AfterJSONRoundTrip` tests guard against the regression in issue #416 by asserting that bytes counted for round-tripped artifacts match the in-memory baseline. Those guards must continue to hold in the new shape.
- `x/analytics/doc.go` is a one-line package comment that currently advertises "per-artifact-kind counts and byte sizes". This is the only place the docstring needs to change.
- `x/analytics` is its own Go module under the workspace (`go.work` lists `./x/analytics`). It is **not** registered in the root `Taskfile.yml` includes, so `task validate` does not reach it. Validation is via direct `go test`, `go vet`, and `go build` from inside `x/analytics/`.
- `x/analytics` is a peripheral package: zero internal importers, leaf in the dependency graph. Per the `software-development` skill, peripheral complexity is acceptable provided it is self-contained, which this change is.
- `artifact.ToolCall` exposes `Name string` directly. `artifact.ToolResult` exposes `ToolCallID string` and **no** name field — same-turn join is the agreed mechanism. No schema change to `artifact` is in scope.
- `x/llmbytes.Of(art)` is the canonical byte counter and continues to be used unchanged. The bug-class guard from issue #416 (no JSON-envelope leak) is preserved by the existing round-trip tests, which will be updated to assert the new struct shape.
- The package currently has no production consumers in the main worktree, so the API break is clean. No CLI or application code in the main checkout imports `x/analytics`; worktrees that have a copy of the package (`feat/add-analytics-library`, `chore/llmbytes-simplify-after-merge`, `423`, `421`, `fix/tui-remove-blank-line-between-header-and-body`, `feat/openrouter-include-reasoning`) have not been inspected because they are out of scope per AGENTS.md.

## Architectural Blueprint

Replace the single-axis stats type with a two-axis stats type. The package remains a flat aggregator — no new layers, no new files. The `artifact` package is not modified.

```
                   ┌───────────────────────┐
                   │ AnalyzeTurns(turns)   │ ── produces ──> []Stats
                   │ AnalyzeThread(thread) │
                   │ AnalyzeStore(store)   │
                   └───────────────────────┘
                              │
                              │ for each turn:
                              │   1. pre-pass: nameByID = map of toolCallID → name
                              │      built from that turn's ToolCall artifacts
                              │   2. main pass: for each artifact,
                              │        resolve Source
                              │        aggregate (Kind, Source) → Count, Bytes
                              │
                              ▼
              ┌────────────────────────────────┐
              │ Stats                          │
              │   Kind   string                │
              │   Source string                │
              │   Count  int                   │
              │   Bytes  int64                 │
              └────────────────────────────────┘
```

Source resolution per artifact kind:

| Kind         | Source resolution                                    |
|--------------|------------------------------------------------------|
| `tool_call`  | `art.Name` (always populated for a valid ToolCall)  |
| `tool_result`| Lookup `ToolCallID` in the same turn's `nameByID` map. If absent, `"(unknown)"`. |
| `text`, `reasoning`, `image`, `usage`, custom kinds | `""` |

Tree-of-Thought deliberation, restated briefly from the ideation phase: three output shapes were considered (flat, two-tier, opt-in) and flat was chosen. Two resolution scopes were considered (same-turn, whole-thread) and same-turn was chosen. Two orphan strategies were considered (silent, `(unknown)` bucket) and `(unknown)` was chosen. No `ToolResult` schema change was considered; denormalizing a `Name` field on `ToolResult` was rejected as out of proportion to the problem.

## Requirements

1. Export a stats type `Stats` (replacing `KindStats`) with fields `Kind string`, `Source string`, `Count int`, `Bytes int64`. The struct must replace `KindStats` outright; no alias type.
2. `AnalyzeTurns`, `AnalyzeThread`, and `AnalyzeStore` return `[]Stats` (replacing `[]KindStats`).
3. For `tool_call` artifacts, the row's `Source` is the artifact's `Name`.
4. For `tool_result` artifacts, the row's `Source` is the `Name` of the `ToolCall` artifact in the same turn whose `ID` equals the result's `ToolCallID`. If no such `ToolCall` exists in the same turn, the `Source` is the literal string `"(unknown)"`.
5. For all other kinds (`text`, `reasoning`, `image`, `usage`, and any custom kind), the row's `Source` is the empty string.
6. Output rows are sorted lexicographically by `(Kind, Source)`. Empty `Source` clusters first within each `Kind`.
7. The byte counter is `x/llmbytes.Of(art)`, unchanged.
8. The package doc comment is updated to describe the per-source breakdown.
9. Tests cover: every existing test continues to pass with the new struct shape; tool_call bucketing by `Name` is exercised; tool_result same-turn resolution is exercised; tool_result orphan → `(unknown)` is exercised; the JSON round-trip invariants from issue #416 are preserved end-to-end.
10. `[inferred]` The `"(unknown)"` sentinel is implemented as a package-private constant, not an exported symbol — it is a presentation choice internal to the package.

## Task Breakdown

### Task 1: Rename `KindStats` to `Stats` and add `Source` field

- **Goal**: Replace `KindStats` with `Stats{Kind, Source, Count, Bytes}`; populate `Source` as empty string for every artifact for now; update existing tests to the new shape.
- **Dependencies**: None.
- **Files Affected**: `x/analytics/analytics.go`, `x/analytics/analytics_test.go`, `x/analytics/doc.go`.
- **New Files**: None.
- **Interfaces**:
  - `type Stats struct { Kind string; Source string; Count int; Bytes int64 }` — replaces `KindStats`.
  - `func AnalyzeTurns(turns []state.Turn) []Stats` — return type changed.
  - `func AnalyzeThread(t *session.Thread) []Stats` — return type changed.
  - `func AnalyzeStore(store session.Store) ([]Stats, error)` — return type changed.
- **Validation**:
  - `cd x/analytics && go vet ./...` clean.
  - `cd x/analytics && go test -race ./...` passes.
  - `cd x/analytics && go build ./...` clean.
  - Every existing test that referenced `KindStats` is updated to `Stats` and continues to pass. The aggregated byte counts for every kind are unchanged from the pre-rename values.
- **Details**: This is a pure mechanical rename plus the addition of an empty `Source` field. Aggregation logic in all three functions is unchanged. Sort order in all three functions is updated to use `out[i].Kind < out[j].Kind` as the primary key and `out[i].Source < out[j].Source` as the secondary key. Tests that asserted specific bytes for the `tool_call` kind now assert those bytes for the `(tool_call, "")` row instead. The `customArtifact` test fixture produces a `(custom, "")` row. The package doc comment in `doc.go` is updated to advertise per-source counts and byte sizes. This task leaves the repository in a committable state: the API is renamed, all existing tests pass, no behavior has changed beyond the field shape and the new sort tiebreaker.

### Task 2: Populate `Source` for `tool_call` artifacts

- **Goal**: When the inner aggregation loop encounters a `artifact.ToolCall`, set the row's `Source` to `art.Name` rather than the empty string.
- **Dependencies**: Task 1.
- **Files Affected**: `x/analytics/analytics.go`, `x/analytics/analytics_test.go`.
- **New Files**: None.
- **Interfaces**: No exported type changes. Internal aggregation logic extended.
- **Validation**:
  - `cd x/analytics && go test -race ./...` passes.
  - New test `TestAnalyzeTurns_ToolCallBucketedByName` constructs a turn with multiple `ToolCall` artifacts carrying distinct `Name` values (e.g. `bash`, `file_read`, `bash` again) and asserts the output contains exactly two rows for the `tool_call` kind, one `(tool_call, bash, ...)` and one `(tool_call, file_read, ...)`, with the correct `Count` and `Bytes` aggregates.
  - The pre-existing `TestAnalyzeTurns_MixedArtifacts` and `TestAnalyzeStore_AfterJSONRoundTrip` tests are updated to assert `(tool_call, bash, 1, 12)` (or equivalent) instead of `(tool_call, "", 1, 12)`.
- **Details**: In the inner loop, type-assert the artifact to `artifact.ToolCall`; if it matches, use `art.Name` as the `Source`. Other kinds continue to produce `Source == ""` for now (the `tool_result` resolution comes in Task 3). The pre-pass that builds `nameByID` for tool results (Task 3) does not need to be introduced yet, but a stub comment marking the next task may be useful. The sort key change in Task 1 already accommodates the new `Source` values. This task leaves the repository in a committable state: tool calls are bucketed by name, all other kinds unchanged, all tests green.

### Task 3: Resolve `Source` for `tool_result` artifacts via same-turn join

- **Goal**: For each `tool_result` artifact, resolve `Source` to the same turn's originating `ToolCall.Name` via `ToolCallID`; bucket orphans under `"(unknown)"`.
- **Dependencies**: Task 2.
- **Files Affected**: `x/analytics/analytics.go`, `x/analytics/analytics_test.go`.
- **New Files**: None.
- **Interfaces**: A package-private helper, e.g. `func resolveToolSource(art artifact.Artifact, nameByID map[string]string) (source string, ok bool)`, is acceptable for testability but not required. No exported type or function changes.
- **Validation**:
  - `cd x/analytics && go test -race ./...` passes.
  - New test `TestAnalyzeTurns_ToolResultResolvedByToolCall` constructs a turn with one `ToolCall{Name: "bash", ID: "1"}` and a matching `ToolResult{ToolCallID: "1", Content: "ok"}`, and asserts the output contains a `(tool_result, bash, 1, 2)` row.
  - New test `TestAnalyzeTurns_ToolResultOrphan` constructs a turn with a `ToolResult{ToolCallID: "missing", Content: "ok"}` and no matching `ToolCall`. Asserts the output contains a `(tool_result, "(unknown)", 1, 2)` row.
  - The pre-existing `TestAnalyzeTurns_MixedArtifacts` and `TestAnalyzeStore_AfterJSONRoundTrip` tests are updated to assert `(tool_result, bash, 1, 2)` instead of `(tool_result, "", 1, 2)`.
  - The `x/analytics/doc.go` package comment is updated to mention that orphan tool results are bucketed under `"(unknown)"` — this is the second half of the docstring update started in Task 1. (If preferred, this can be deferred to Task 4; either is fine.)
- **Details**: In `AnalyzeTurns`, before the main aggregation loop, build a `map[string]string` from the current turn's `ToolCall` artifacts keyed by `ID` and valued by `Name`. In the main loop, type-assert to `artifact.ToolResult`; if matched, look up `art.ToolCallID` in the map. If the lookup hits, use the corresponding `Name`; if it misses, use the package-private constant `"(unknown)"`. The same pre-pass and lookup pattern is applied in `AnalyzeStore`'s per-turn inner loop, where each turn is processed independently (no cross-turn resolution). The `AnalyzeThread` path delegates to `AnalyzeTurns` and inherits the new behavior automatically. The map build is O(n) per turn and runs once before the per-artifact loop. This task leaves the repository in a committable state: tool results are bucketed by source from the same turn, orphans are visible, all tests green.

### Task 4: Lock in the round-trip invariants in the new shape

- **Goal**: Confirm that the issue #416 regression guard (no JSON-envelope bytes leak for round-tripped artifacts) still holds end-to-end after the struct shape change, and that `AnalyzeStore` applies same-turn resolution per-thread correctly.
- **Dependencies**: Task 3.
- **Files Affected**: `x/analytics/analytics_test.go`.
- **New Files**: None.
- **Interfaces**: None (test-only).
- **Validation**:
  - `cd x/analytics && go test -race ./...` passes.
  - `TestAnalyzeThread_AfterJSONRoundTrip` is updated to assert the new `Stats` shape (replacing its `KindStats` assertions) and continues to fail-loudly if a future change reintroduces pointer artifacts at the session round-trip boundary.
  - `TestAnalyzeStore_AfterJSONRoundTrip` is updated to assert the new shape, including `(tool_call, bash, 1, 12)` and `(tool_result, bash, 1, 2)` rows after the round trip.
  - `TestAnalyzeThread_NoJSONEnvelopeLeak` is updated to assert the new shape and continues to guard against JSON-envelope-length regression for any future artifact kind.
  - New test `TestAnalyzeStore_ToolResultOrphanPerThread` constructs a `mockStore` with two threads, one containing a `ToolResult` whose `ToolCallID` is present in the same turn (resolves to a name) and one containing a `ToolResult` whose `ToolCallID` is not present anywhere in the same thread (orphans). Asserts the store-wide output contains both `(tool_result, <name>, 1, ...)` and `(tool_result, "(unknown)", 1, ...)` rows, and that resolution in one thread does not affect the other.
- **Details**: No production code changes in this task. The purpose is to (a) refresh the existing round-trip guard tests against the new struct shape so the issue #416 invariant is not silently dropped, and (b) add a store-level orphan test that confirms the per-thread scope of the resolution. The `mockStore` test double from `analytics_test.go` is reused. This task leaves the repository in a committable state: the regression guards are ported to the new schema, and store-wide aggregation is explicitly exercised for the orphan path.

## Dependency Graph

- Task 1 → Task 2 → Task 3 → Task 4 (strictly sequential; each task builds on the prior task's struct/field state and the test suite grows incrementally).
- No parallelizable tasks. The change is small and linear, and each task's commit message is most coherent when it follows the previous one in order.

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Same-turn join misses tool results whose originating `ToolCall` was emitted in a prior turn (cross-turn flow) | Medium — those results bucket as `(unknown)`, which is visible but could be misread as a data corruption bug | Low (the dominant flow places call and result in the same turn) | Document the scope in `doc.go`; the `(unknown)` bucket is itself the mitigation — the user can see how many of their results fall into it and decide whether to investigate |
| Compaction drops the `ToolCall` but keeps the `ToolResult`, producing false `(unknown)` rows | Low — any compaction policy that drops calls but keeps results is already a data-loss bug; analytics just surfaces it | Low | Out of scope. The `(unknown)` bucket is the signal that the data layer has lost information. |
| Worktrees with a stale `x/analytics` copy (`feat/add-analytics-library`, `chore/llmbytes-simplify-after-merge`, `423`, `421`, `fix/tui-remove-blank-line-between-header-and-body`, `feat/openrouter-include-reasoning`) may merge this change and produce merge conflicts in `analytics.go`/`analytics_test.go` | Low — those are still unmerged feature branches; the maintainer will handle conflict resolution at merge time | Low | Noted in Context. No action in this plan; the maintainer can rebase or merge cleanly. |
| Existing consumers (in any worktree) that import `KindStats` by name will fail to compile after the rename | Medium at the worktree level, zero in main (no consumers) | Certain in any worktree that imports it | Documented in Requirements as a clean break. Per AGENTS.md, no backwards-compat scaffolding. Worktree consumers rebase and adopt `Stats`. |
| `llmbytes.Of(art)` does not handle a future artifact type that is added between Tasks 1 and 3 | Low — peripheral package, no other consumers are likely to land in the window | Very low | The `TestAnalyzeThread_NoJSONEnvelopeLeak` test pattern (assert bytes are strictly less than the JSON envelope) is the existing guard. Task 4's update keeps that guard in place. |
| `"(unknown)"` collision with a real tool named `"(unknown)"` | Negligible — tool names containing parens are not idiomatic in any ore provider adapter; the value is a presentation label, not a structural identifier | Very low | Accept the negligible risk. If a future tool actually needs this name, the convention can be changed to `__unknown__` or similar in a one-line edit. |

## Validation Criteria

- [ ] `x/analytics/analytics.go` exports `Stats` (not `KindStats`) with fields `Kind`, `Source`, `Count`, `Bytes`.
- [ ] `AnalyzeTurns`, `AnalyzeThread`, and `AnalyzeStore` return `[]Stats`.
- [ ] A `tool_call` artifact with `Name: "bash"` produces a row `(tool_call, "bash", 1, <bytes>)`.
- [ ] Two `tool_call` artifacts with `Name: "bash"` and `Name: "file_read"` in the same turn produce two distinct rows: `(tool_call, "bash", 1, ...)` and `(tool_call, "file_read", 1, ...)`.
- [ ] A `tool_result` whose `ToolCallID` matches a same-turn `ToolCall` produces a row with the `ToolCall`'s `Name` as the `Source`.
- [ ] A `tool_result` whose `ToolCallID` does not match any same-turn `ToolCall` produces a row `(tool_result, "(unknown)", 1, <bytes>)`.
- [ ] A `text` artifact produces a row `(text, "", 1, <bytes>)`. Same for `reasoning`, `image`, `usage`, and any custom kind.
- [ ] Output rows are sorted lexicographically by `(Kind, Source)`. Empty `Source` sorts before any non-empty `Source` within the same `Kind`.
- [ ] `x/analytics/doc.go` describes the per-source breakdown and the `(unknown)` orphan convention.
- [ ] `cd x/analytics && go vet ./...` is clean.
- [ ] `cd x/analytics && go test -race ./...` passes.
- [ ] `cd x/analytics && go build ./...` is clean.
- [ ] The pre-existing `TestAnalyzeThread_AfterJSONRoundTrip` and `TestAnalyzeStore_AfterJSONRoundTrip` tests have been ported to the new `Stats` shape and still detect a regression in `session/serialize.go`'s pointer-handling.
- [ ] The `(unknown)` orphan path is exercised by at least one dedicated test.
- [ ] The store-wide aggregation's per-thread orphan handling is exercised by at least one dedicated test.
