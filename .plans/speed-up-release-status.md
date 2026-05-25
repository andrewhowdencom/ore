# Plan: Speed Up Release Status Checks

## Objective

Eliminate the ~10–50× redundant `git log`/`git diff-tree` subprocess calls that make `release status` (and `release all`) slow. Introduce a lightweight SHA-keyed commit cache so that shared history is fetched once and reused across all module iterations, cutting total subprocess calls from O(modules × commits) to O(total-unique-commits).

## Context

The `cmd/release` tool (1,088 lines of non-test Go) supports three commands: `status`, `all`, and `<module-path>`. All three share the same underlying pattern in `commits.go`:

1. `git rev-list <tag>..HEAD` → list commit SHAs
2. For **each** SHA, spawn `git log -1 --format=%s` to get the message
3. For **each** SHA, spawn `git diff-tree --no-commit-id --name-only -r` to get changed files

With ~13 modules and a shared monorepo history, `runStatus` and `runAll` call `commitsSinceTag` once per module. A modest 20-commit delta per module translates to 13 `rev-list` + ~520 per-commit subprocess calls. The per-commit calls are the dominant cost and are almost entirely redundant because the same SHAs appear in multiple modules' histories.

**Project conventions (from `AGENTS.md`):**
- Prefer aggressive refactoring; backwards compatibility is not a goal for internal tools.
- Table-driven tests with `httptest` (or real subprocesses, as already used here).
- `go test -race ./...` is the standard validation gate.

**Relevant files discovered:**
- `cmd/release/commits.go` — `commitsSinceTag`, `commitMessage`, `commitFiles`
- `cmd/release/status.go` — `runStatus` (sequential per-module loop)
- `cmd/release/all.go` — `runAll` (same redundant pattern)
- `cmd/release/single.go` — `runRelease` (one module, but uses same primitive)
- `cmd/release/commits_test.go` — `TestCommitsSinceTag` variants (real git repos)
- No orchestrator-level tests for `runStatus`/`runAll`/`runRelease` exist, so caller refactoring is low-risk.

## Architectural Blueprint

The root cause is not CPU-bound work — it is I/O-bound subprocess spawning. The cheapest, simplest fix is a **commit-data cache** keyed by SHA, shared across the per-module loops in `status`, `all`, and `single`.

### Tree-of-Thought Deliberation

| Path | Description | Pros | Cons | Verdict |
|------|-------------|------|------|---------|
| **A: Parallelise with goroutines** | Spawn a goroutine per module in `runStatus`/`runAll` | Easy to add; overlaps waiting git calls | Does not eliminate redundant work; harder to test deterministically; git subprocesses may contend on the repo lock | Rejected: user wants simple / easy to test |
| **B: SHA-keyed commit cache** | Cache `git log` and `git diff-tree` results in a map, share across module iterations | Eliminates dominant cost; ~10× speedup; preserves existing structure; trivial to test | Still does one `git rev-list` per module | **Selected**: best simplicity / impact trade-off |
| **C: Single broad-range walk** | Walk from the earliest tag to HEAD once, then filter per-module | Minimum subprocess count (~14 total) | Requires per-module filtering / ancestry logic; more parsing complexity | Rejected for now; can be layered on top of B if profiling later shows `rev-list` is slow |

**Selected architecture: Path B.**

A `commitCache` struct holds two maps (`messages` and `files` keyed by SHA). `commitsSinceTag` is refactored to accept `*commitCache`; on a cache miss it falls back to the existing `git` subprocess, on a hit it returns the stored value. Callers in `status.go`, `all.go`, and `single.go` instantiate one cache before their loop and pass it through. Tests use a fresh `newCommitCache()`; no observable behavior changes.

## Requirements

1. `release status` and `release all` must complete significantly faster (target: order-of-magnitude reduction in subprocess calls).
2. Output of `release status` and `release all` must remain byte-for-byte identical.
3. All existing tests in `cmd/release/...` must continue to pass.
4. The change must be simple and easy to test (no goroutines, no external dependencies, no complex filtering logic).

## Task Breakdown

### Task 1: Introduce `commitCache` and refactor `commitsSinceTag`
- **Goal**: Create a SHA-keyed cache for commit messages and file lists, and refactor `commitsSinceTag` to consume it, eliminating redundant `git log`/`git diff-tree` calls.
- **Dependencies**: None.
- **Files Affected**: `cmd/release/commits.go`, `cmd/release/commits_test.go`
- **New Files**: None.
- **Interfaces**:
  - New type: `type commitCache struct { messages map[string]string; files map[string][]string }`
  - New function: `func newCommitCache() *commitCache`
  - New methods (or internal helpers) on `commitCache` to fetch message and files by SHA, populating the map on miss.
  - Modified signature: `func commitsSinceTag(root, tag string, cache *commitCache) ([]Commit, error)`
- **Validation**: `go test ./cmd/release/...` passes (including race detector).
- **Details**:
  - The `commitCache` methods must handle `nil` receiver gracefully (fallback to direct git call), or callers must always pass a non-nil cache. Prefer non-nil to keep the surface small; update `commits_test.go` to pass `newCommitCache()` in the three existing `TestCommitsSinceTag*` calls.
  - Preserve exact behavior of `commitMessage` and `git diff-tree` flags; do not introduce `-z` or other parsing changes yet.
  - This task is hermetic: after it, `commitsSinceTag` compiles and tests pass, even though callers in `status.go`/`all.go`/`single.go` will temporarily fail to compile because their signatures are not yet updated.

### Task 2: Update `runStatus` to share a cache across modules
- **Goal**: Wire the new `commitCache` into `runStatus` so commit data is loaded once and reused for every module.
- **Dependencies**: Task 1.
- **Files Affected**: `cmd/release/status.go`
- **New Files**: None.
- **Interfaces**: `runStatus` creates `cache := newCommitCache()` before the `for _, m := range modules` loop and passes it to `commitsSinceTag`.
- **Validation**:
  - `go build ./cmd/release` succeeds.
  - `go test ./cmd/release/...` passes.
  - `go run ./cmd/release status` produces output identical to the pre-refactor version.
- **Details**:
  - Mechanical change: instantiate cache before the loop, pass pointer into the loop body.
  - No logic changes to version computation, bump detection, or formatting.

### Task 3: Update `runAll` and `runRelease` to share a cache
- **Goal**: Apply the same caching pattern to `runAll` and `runRelease` for consistency and speed.
- **Dependencies**: Task 2.
- **Files Affected**: `cmd/release/all.go`, `cmd/release/single.go`
- **New Files**: None.
- **Interfaces**: Both functions create a `newCommitCache()` before the `commitsSinceTag` call(s) and pass it through.
- **Validation**:
  - `go build ./cmd/release` succeeds.
  - `go test ./cmd/release/...` passes.
  - `go run ./cmd/release -dry-run all` runs without error and produces identical preview output.
- **Details**:
  - `runAll` has two phases: target discovery (needs commit history) and release execution (does not). The cache should be created before the target-discovery loop and discarded afterward; the pre-flight and tag loops do not need it.
  - `runRelease` processes a single module, so the benefit is small, but using the same signature keeps the codebase consistent.

### Task 4: Verify end-to-end speedup and output parity
- **Goal**: Confirm the refactor delivers the expected performance improvement and does not change observable behavior.
- **Dependencies**: Task 3.
- **Files Affected**: None (validation only).
- **New Files**: None.
- **Interfaces**: N/A.
- **Validation**:
  - `go test -race ./cmd/release/...` passes.
  - `go vet ./cmd/release/...` is clean.
  - Compare `go run ./cmd/release status` output before and after: must be identical.
  - (Optional but recommended) Run `time go run ./cmd/release status` on a real repo with many modules; confirm wall-clock time is measurably reduced.
- **Details**:
  - If any test fails, debug whether it is due to cache ordering, nil handling, or git flag changes. The cache must be transparent.
  - If `runStatus` output differs, bisect by comparing intermediate data (commit lists, bump types, version strings) to isolate the regression.

## Dependency Graph

- Task 1 → Task 2 (Task 2 depends on the new `commitsSinceTag` signature)
- Task 2 → Task 3 (Task 3 depends on the caller pattern established in Task 2)
- Task 3 → Task 4 (Task 4 validates the complete stack)

No parallelisable work — the tasks are strictly sequential because each one builds on the API changes of the previous.

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Changing `commitsSinceTag` signature breaks tests or callers we missed | Medium | Low | `go build ./cmd/release` will catch any missed callers immediately; tests cover the primitive directly. |
| Cache hides a git state change during execution (e.g. tags are created between module iterations) | Low | Low | `status` and `all` already assume a static repo state during execution; the cache makes this assumption explicit, not new. |
| `go test -race` fails due to concurrent map access if someone later adds goroutines | Low | Low | The cache uses a plain map and is accessed from a single goroutine today. Document this assumption; if parallelisation is added later, switch to `sync.Map` or add a mutex. |
| User expected goroutines / parallelisation as the fix | Medium | Low | The ideation phase already explored this and converged on caching as the simpler, higher-impact change. Document the rationale in the plan. |

## Validation Criteria

- [ ] `go test -race ./cmd/release/...` passes on every task boundary.
- [ ] `go build ./cmd/release` succeeds on every task boundary.
- [ ] `go run ./cmd/release status` output is byte-for-byte identical before and after the full refactor.
- [ ] `go run ./cmd/release -dry-run all` output is byte-for-byte identical before and after the full refactor.
- [ ] Wall-clock time for `release status` is measurably reduced (target: order-of-magnitude fewer subprocess calls).
