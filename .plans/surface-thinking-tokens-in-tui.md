# Plan: Surface Thinking Tokens in the TUI Status Bar

## Objective

Add a per-turn "thinking tokens" counter to the TUI status bar's existing
compact token segment (`↑ sent · ↓ received · Σ total`), rendered with the
`Ψ` symbol next to the other three. The data already exists on
`artifact.Usage.ThinkingTokens` (populated by the Anthropic and OpenRouter
adapters — see `x/provider/anthropic/anthropic.go:600+`), but is dropped at
two boundaries: the `x/usage` handler does not aggregate it, and the TUI
renderer does not know it is a token-family key. Both gaps close in this
plan. No new types, no new provider concepts, no model-registry work
(the max-context-window question is explicitly out of scope).

## Context

**Repository state (verified at HEAD `9ea275e` on branch `totals`):**
- Working tree is clean.
- `artifact/artifact.go:244` defines `Usage` with `PromptTokens`,
  `CompletionTokens`, `TotalTokens`, `CacheReadTokens`, `CacheWriteTokens`,
  and `ThinkingTokens` (the last three are `omitempty`).
- `x/usage/handler.go` is a `loop.Handler` that aggregates prompt/completion
  from each `Usage` artifact and emits a `PropertiesEvent` keyed by
  `"sent"`, `"received"`, `"total"`. Per-turn semantics: `prompt` and
  `completion` are overwritten with the latest `Usage` values; `total` is
  accumulated across the session. Thread-safe via `sync.Mutex`. The
  handler always emits a `PropertiesEvent` per `Usage` artifact — even
  when all counts are zero.
- `x/usage/handler_test.go` has five table-driven tests, including
  `TestHandler_AggregatesUsageAndEmitsProperties` and
  `TestHandler_TracksLastTurnValuesAndAccumulatesTotal` (the latter
  encodes the "overwrite vs. accumulate" split — thinking tokens follow
  the overwrite branch).
- `x/usage/doc.go` is a public API doc that lists the three emitted keys;
  must be updated to keep the documented contract in sync.
- `x/conduit/tui/view.go:206-265` (`buildStatusLine`) and
  `x/conduit/tui/view.go:268-291` (`compactTokenSegments`) both
  hardcode the same three keys (`sent`, `received`, `total`) and the
  same three symbols (`↑`, `↓`, `Σ`). They must be updated together or
  the two code paths will drift.
- `x/conduit/tui/tui.go:215` (`statusFromStream`) is the bootstrap that
  seeds the status bar from `session.Stream.AllMetadata()` on Start. It
  consumes the full `PropertiesEvent` map verbatim, so a new key flows
  through with no additional code.
- `x/conduit/tui/model.go:162` is the only other PropertiesEvent
  consumer in the TUI; it routes the event into `m.status` which feeds
  `buildStatusLine` — no change needed there.

**Conventions observed (from `AGENTS.md` and the existing module):**
- `x/usage/` and `x/conduit/tui/` are separate Go modules with their own
  `go.mod` and `go.sum`. Each lives under its own directory and is
  registered in the root `go.work`.
- Per-module validation is run with `task x-usage:validate` and
  `task x-conduit-tui:validate` (per `Taskfile.yml`).
- Tests are table-driven and use `github.com/stretchr/testify/assert`
  and `require`. A `mockEmitter` struct in the test file is the local
  pattern for capturing `loop.OutputEvent` values.
- Release is automated by `task release` (root) via `./cmd/release`;
  the new behavior becomes releasable as soon as the code lands on a
  clean commit on `totals`.

**Scope decision (per user, in this conversation):**
- **In scope:** surface the per-turn count with a `Ψ` symbol, show
  even when zero, in the TUI status bar. Touch `x/usage` (so the data
  is in the property bag) and `x/conduit/tui` (so it is rendered).
- **Out of scope:** max context window, % of context, cache read/write
  tokens, new symbols beyond `Ψ`, custom theming, the HTTP conduit.
  These were explicitly excluded by the user.

**Tree-of-Thought deliberation (recorded):**

| Path | Verdict | Why |
|---|---|---|
| (A) TUI consumes `artifact.Usage` directly, bypasses `x/usage` | Rejected | Violates the dumb-pipe / event-driven separation; the TUI would have to know the artifact taxonomy. Breaks the property bag, which is the only metadata surface the HTTP conduit shares with the TUI. |
| (B) Add `thinking` to the `x/usage` property bag; teach the TUI to render it | **Selected** | Two small, hermetic changes in two adjacent sub-modules. Reuses the existing `PropertiesEvent` channel. Backwards-compatible: any consumer that ignores unknown keys is unaffected. |
| (C) Move the "thinking" rendering into a new `x/usage`-emitted `ReasoningTokenEvent` artifact | Rejected | Inventing a new event type for a single integer is over-engineered for a status bar counter. |
| (D) Cumulative thinking counter (like `total`) instead of per-turn (like `received`) | Rejected | User explicitly chose per-turn. The mental model is "this turn the model thought for X tokens," which mirrors `received`. |
| (E) Hide the counter when `ThinkingTokens == 0` | Rejected | User explicitly chose to show even when zero. Symmetric with the other three counters. |
| (F) Use `…` (ellipsis) instead of `Ψ` as the symbol | Rejected | User explicitly chose `Ψ` in this conversation. |

## Requirements

1. `x/usage` must aggregate `artifact.Usage.ThinkingTokens` per turn
   (overwrite semantics — same as `prompt` and `completion`) and emit
   it as a `"thinking"` key in the `PropertiesEvent` value map,
   always (including when the count is zero).
2. The TUI's `buildStatusLine` and `compactTokenSegments` functions
   must treat `"thinking"` as a token-family key, render it with the
   symbol `Ψ`, and place it after the existing three counters in the
   compact segment: `↑ sent · ↓ received · Σ total · Ψ thinking`.
3. The TUI's "render remaining keys alphabetically" loop must skip
   `"thinking"` (otherwise it would be rendered twice — once in the
   compact segment and once as `thinking: 0`).
4. `x/usage/doc.go` must document the new key in the same list style
   as the existing three, with a one-line description noting that
   `thinking` follows per-turn (overwrite) semantics.
5. Existing tests in both modules must continue to pass unchanged
   (they don't reference the new key, so they shouldn't — but verify).
6. New unit tests must cover: (a) the handler emits `"thinking"` with
   the latest per-turn value, (b) the handler emits `"thinking": "0"`
   for a zero-usage artifact, (c) the TUI renders `Ψ 0` for a status
   map containing only the thinking key, (d) the TUI renders all four
   symbols in the correct order when all four keys are present.

## Task Breakdown

### Task 1: Aggregate and emit `thinking` in `x/usage`
- **Goal**: Extend the `x/usage` handler to track the most recent
  `ThinkingTokens` and include it in the emitted `PropertiesEvent`.
- **Dependencies**: None.
- **Files Affected**:
  - `x/usage/handler.go`
  - `x/usage/doc.go`
  - `x/usage/handler_test.go`
- **New Files**: None.
- **Interfaces**: No new exported types. Internal-only change to the
  `Handler` struct gains a `thinking int` field. The `PropertiesEvent`
  map gains one key: `"thinking"`.
- **Validation**:
  - `task x-usage:validate` (builds, vets, and runs `go test -race
    ./...` for the `x/usage` module).
  - All five existing tests still pass.
  - Two new tests pass:
    - `TestHandler_EmitsThinkingTokensPerTurn` — three sequential
      `Usage` artifacts with `ThinkingTokens` 10, 20, 30. Asserts the
      three emitted `PropertiesEvent`s have `"thinking": "10"`,
      `"thinking": "20"`, `"thinking": "30"` (overwrite, not
      accumulate).
    - `TestHandler_EmitsZeroThinking` — single `Usage{}` with
      `ThinkingTokens: 0`. Asserts the emitted event has
      `"thinking": "0"` (not absent).
  - `git status` is clean after the commit; the worktree is in a
  state where a single commit captures the change.
- **Details**:
  - Add `thinking int` to the `Handler` struct (alongside `prompt`,
    `completion`, `total`).
  - In `Handle`, under the existing mutex, read
    `u.ThinkingTokens` and overwrite the field (mirroring
    `h.prompt = u.PromptTokens`).
  - In the `PropertiesEvent` literal, add `"thinking":
    strconv.Itoa(thinking)` as a fourth entry.
  - Update `x/usage/doc.go` to add a fourth list item:
    `//   - "thinking":  per-turn output tokens consumed by the model's
    extended-thinking / reasoning phase` (overwrite semantics).
  - The comment block above the `// Update:` line should be amended
    to include thinking in the "last turn" sentence, e.g.
    `prompt, completion, and thinking track the last turn's values`.
  - Tests follow the `mockEmitter` pattern already in
    `handler_test.go`. Reuse the existing
    `TestHandler_TracksLastTurnValuesAndAccumulatesTotal` as a
    structural reference; the new tests are narrower in scope.

### Task 2: Render `Ψ thinking` in the TUI status bar
- **Goal**: Teach `buildStatusLine` and `compactTokenSegments` in
  `x/conduit/tui/view.go` to recognize `"thinking"` as a token-family
  key and render it with the `Ψ` symbol.
- **Dependencies**: None functionally, but should land *after* Task 1
  in execution order so the two are merged as a single visible
  behavior change. (If landed in the other order, the test would
  still pass but the user-visible feature would only "turn on" once
  both commits are deployed — a confusing diff to review.)
- **Files Affected**:
  - `x/conduit/tui/view.go`
  - `x/conduit/tui/view_test.go`
- **New Files**: None.
- **Interfaces**: No new exported types. The `buildStatusLine` and
  `compactTokenSegments` functions each grow by one branch in their
  respective `switch key` / `switch seg.Label` statements. The
  "skip token keys" filter in `buildStatusLine`'s "remaining keys"
  loop grows by one `||` clause.
- **Validation**:
  - `task x-conduit-tui:validate` (builds, vets, and runs
    `go test -race ./...` for the `x/conduit/tui` module).
  - All existing `TestBuildStatusLine_*` and `TestCompactTokenSegments_*`
    tests still pass.
  - Three new tests pass:
    - `TestBuildStatusLine_ThinkingKeyGrouped` — status map with all
      four token keys (`sent`, `received`, `total`, `thinking`) and a
      non-token key. Asserts the rendered string contains `↑ X · ↓ Y
      · Σ Z · Ψ T` in that order, and that the non-token key still
      appears separately.
    - `TestBuildStatusLine_ThinkingKeyOnly` — status map with only
      `thinking: "0"`. Asserts the rendered string contains `Ψ 0` and
      contains no `↑`, `↓`, or `Σ` (the function must not invent
      zero values for the other three).
    - `TestBuildStatusLine_ThinkingKeyNotDoubleRendered` — status
      map with `thinking: "42"` and no other keys. Asserts the
      rendered string contains exactly one occurrence of `Ψ 42`
      (regression guard against the
      "remaining keys alphabetically" loop accidentally re-emitting
      the thinking key as `thinking: 42`).
  - `git status` is clean after the commit.
- **Details**:
  - In `buildStatusLine` (around line 222-237), change the iteration
    list from `[]string{"sent", "received", "total"}` to include
    `"thinking"` as the fourth entry. Add a fourth `case "thinking":
    sym = "Ψ"` branch to the switch.
  - In the "render remaining keys alphabetically" filter (around
    line 245), add `|| k == "thinking"` to the skip list.
  - In `compactTokenSegments` (around line 277-285), mirror the
    same change: add a `case "thinking": sym = "Ψ"` branch.
  - Update the doc comment above `buildStatusLine` (line 209) to
    mention `Ψ thinking` alongside `↑, ↓, Σ`.
  - Note for the implementer: the symbol `Ψ` is U+03A8, a printable
    Unicode rune. It is a single cell wide in a monospaced terminal
    (verified: `go run` a one-liner that prints `Ψ` and inspect
    `runewidth` if uncertain — but `↑` and `↓` and `Σ` are already
    single-cell, so `Ψ` matches by construction).

## Dependency Graph

- Task 1 → Task 2 (Task 2 should land after Task 1 for clean diff
  review; the runtime does not strictly require this order, but a
  reviewer who lands Task 2 alone would see a test that depends on
  data no handler emits yet).

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| `Ψ` is wider than one cell in some terminals and breaks the compact line's visual alignment | Low | Low | The other three symbols are all single-cell; `Ψ` is the same Unicode class. If a regression appears, swap to `Ψ` (bold/italic) or fall back to a plain `T`. The tests assert *presence* of `Ψ X`, not column alignment, so a future fix is local. |
| Provider populates `ThinkingTokens` to a non-zero value during a normal (non-extended-thinking) turn, polluting the counter | Low | Low | Today only Anthropic and OpenRouter populate the field, and only when the upstream API reports it. Verified in `x/provider/anthropic/anthropic.go:600-610`: the field is set from `usage.output_tokens_details.thinking_tokens`, which is `0` unless extended thinking is enabled. |
| A future conduit (e.g., HTTP) renders the `"thinking"` key in a non-token context and breaks | Low | Low | The TUI's renderer is the only consumer that special-cases token keys. The HTTP conduit passes the property map through verbatim, so unknown keys are harmless. A future change is the right time to extract a shared `conduit.TokenKey` set, but that's not a current need. |
| `x/usage` mutex contention when many turns arrive concurrently | Low | Low | Existing `TestHandler_ConcurrentUpdates` covers the contention shape. The new `thinking int` field is read/written under the same lock; no change to locking discipline. |
| The `omitempty` JSON tag on `ThinkingTokens` (in `artifact.Usage`) means a future test that round-trips through JSON may lose the field | Low | Low | Out of scope for this plan; the value flows in-memory from provider to handler. If a test ever needs to assert over JSON, it should set the field explicitly. |

## Validation Criteria

- [ ] `task x-usage:validate` exits 0 (build, vet, `go test -race ./...`).
- [ ] `task x-conduit-tui:validate` exits 0 (build, vet, `go test -race ./...`).
- [ ] `task validate` (root) exits 0 — confirms nothing in the wider
      workspace regressed.
- [ ] New `TestHandler_EmitsThinkingTokensPerTurn` passes.
- [ ] New `TestHandler_EmitsZeroThinking` passes.
- [ ] New `TestBuildStatusLine_ThinkingKeyGrouped` passes.
- [ ] New `TestBuildStatusLine_ThinkingKeyOnly` passes.
- [ ] New `TestBuildStatusLine_ThinkingKeyNotDoubleRendered` passes.
- [ ] All pre-existing tests in both modules still pass.
- [ ] `x/usage/doc.go` lists the `"thinking"` key in the same list
      style as `"sent"`, `"received"`, `"total"`.
- [ ] The compact token segment renders as
      `↑ <sent> · ↓ <received> · Σ <total> · Ψ <thinking>` in both
      `buildStatusLine` and `compactTokenSegments` paths.
- [ ] `git log --oneline -3` on the `totals` branch shows two
      clean commits (one per task) with messages of the form
      `feat(usage): surface thinking tokens in properties bag` and
      `feat(tui): render thinking token counter in status bar`.
- [ ] A manual smoke test in `examples/tui-chat` with extended
      thinking enabled shows the `Ψ` segment updating per turn and
      showing `Ψ 0` for turns that did not invoke thinking. (Optional
      — not blocking — but the plan is incomplete without at least
      one manual run-through.)
