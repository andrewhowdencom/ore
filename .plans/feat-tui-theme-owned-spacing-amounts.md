# Plan: Theme-Owned Inter-Block and Inter-Turn Gaps in TUI

## Objective

Replace the five literal `"\n\n"` strings in `x/conduit/tui/view.go::buildContent` with named, theme-owned gap amounts. The renderer reads the gap from `m.theme` and encodes it once. The theme is the single source of truth for the value; the renderer is the sole writer of inter-block and inter-turn newlines.

This preserves the existing default behavior (one blank line between blocks within a turn; one blank line between turns) and makes the gap a typed amount the theme owns, so future per-theme variations (compact mode, accessibility mode) become a one-line change to the theme rather than a renderer edit.

## Context

- `x/conduit/tui/view.go:142-170` writes `"\n\n"` five times: two inter-block boundaries (within `m.turns`, within `m.currentTurn`) and three inter-turn boundaries (after `m.turns`, after `m.currentTurn`, and after the pending placeholder). The literal is the only signal of intent at the call site; a reader has to count `if i < len(turn.blocks)-1` to know which is which.
- `x/conduit/tui/theme/theme.go` defines `Theme` with lipgloss styles and the glamour `StyleConfig`. The `theme` package is the consolidated style authority following `.plans/consolidate-tui-theme.md`; it already owns `Document.Margin`, `Document.BlockPrefix`, and `Document.BlockSuffix` (glamour-side spacing knobs).
- `x/conduit/tui/theme/dark.go` and `light.go` are the two factories; `theme.Dark()` is what `newTestModel` uses (`model_test.go:38`).
- `x/conduit/tui/view_test.go:1347-1397` (`TestBuildContent_InterMessageSpacing_OneBlankLine`) is the existing regression test for the inter-turn gap. It asserts that the segment between the end of one assistant body's `"hello"` and the start of the next header's `12:00:05` timestamp contains exactly two newlines (one blank line).
- `x/conduit/tui/model_test.go:284-340` (`TestModel_View_Contains{Turn,AssistantTurn,ToolTurn}`) asserts that the segment between a block's label and its body contains exactly one newline (the header→body join from ore#413, unaffected by this change).
- The TUI is the only consumer of the theme. `examples/tui-chat/main.go` is the only external consumer of `tui.New(...)` and uses functional options; no breaking change to its public surface.
- Project conventions (`AGENTS.md`): prefer aggressive refactoring, no backwards compatibility concerns, the TUI is a peripheral package that may carry internal complexity if the public surface stays clean. Theme-owns-styling is the established direction; this change extends the same pattern from glamour's knobs (already theme-owned) to the gap the renderer writes.

## Architectural Blueprint

Two new fields on `Theme`, a single method on `Theme` that encodes the amount as a string, and five call-site substitutions in `buildContent`. No new packages, no new files.

```
x/conduit/tui/
├── theme/
│   ├── theme.go    (add 2 fields, add Gap method)
│   ├── dark.go     (set the 2 fields in Dark())
│   ├── light.go    (set the 2 fields in Light())
│   └── theme_test.go (add Gap tests; assert Dark/Light defaults)
├── view.go         (replace 6 "\n\n" literals with theme.Gap(...) calls)
├── view_test.go    (update TestBuildContent_InterMessageSpacing to assert against theme value)
└── model_test.go   (no changes; newTestModel already uses theme.Dark())
```

The defaults in `Dark()` and `Light()` are `InterBlockGap: 1` and `InterTurnGap: 1` — one blank line each — which reproduce the current `"\n\n"` behavior exactly. The existing regression test passes against the new code without modification, because the default gap = 1 → 2 newlines (the existing test's expected count).

### The new types and method

```go
// theme/theme.go
type Theme struct {
    // ... existing fields ...
    InterBlockGap int // blank lines between blocks within a turn
    InterTurnGap  int // blank lines between turns
}

// Gap encodes a blank-line amount as a string the renderer can write.
// n == 0 produces ""; n == 1 produces "\n\n" (one blank line); n == 2
// produces "\n\n\n" (two blank lines). The formula is n+1 newlines
// because adjacent content always has a line break between it; "n
// blank lines" means "n newlines in addition to the line break that's
// always there."
func (t *Theme) Gap(n int) string {
    if n <= 0 {
        return ""
    }
    return strings.Repeat("\n", n+1)
}
```

### The renderer call sites

In `buildContent`, the five gap writes change from:

```go
b.WriteString("\n\n")   // ×5
```

to:

```go
b.WriteString(m.theme.Gap(m.theme.InterBlockGap))   // ×2 (inter-block)
b.WriteString(m.theme.Gap(m.theme.InterTurnGap))    // ×3 (inter-turn)
```

The pattern at each call site is: "render a block, then write the named gap before the next structural element." Reading the function top-to-bottom, the *name* of the gap (not the literal character count) tells the reader what's happening.

### Why this shape and not `*uint` / `Spacing` struct / `TrimTrailingNL` flag

The earlier ideation explored three richer designs:

- **`*uint` for nil-default semantics:** rejected. There is no use case for a "gap is unset, fall back to renderer default" sentinel. The default is the value in the theme factory; the renderer has no default of its own.
- **`theme.Spacing` struct:** rejected. Wrapping two related integers in a struct adds a layer of indirection without buying anything the bare fields don't. If a third gap appears (e.g. inter-section), promoting the fields to a struct is a mechanical refactor at that point.
- **`TrimTrailingNL` flag:** rejected. The audit of glamour's per-block trailing-newline behavior showed text and list blocks emit 1 trailing `\n` and others emit 0. The current behavior is already what the new design produces (the composer's `"\n\n"` overpowers glamour's incidental trailing `\n` for the cases that have one). A trim flag would make the contract more rigid without fixing a real bug. Skip until there is a real second consumer of the trim behavior.

The two-integers-and-one-method design is the smallest unit of code that:

1. Names the boundary at the call site (`InterBlockGap`, `InterTurnGap`).
2. Names the encoding in one place (`Gap`).
3. Lets the value live in the theme (`Dark()`, `Light()`).
4. Reproduces the current default behavior exactly.

## Requirements

1. `Theme.InterBlockGap` and `Theme.InterTurnGap` must be `int` (not `*uint`, not a struct), named for the boundary they govern.
2. `Theme.Gap(n int) string` must encode n blank lines as n+1 newlines for n > 0, and as `""` for n ≤ 0.
3. `theme.Dark()` and `theme.Light()` must set both fields to `1` (one blank line) so the default behavior is unchanged.
4. `buildContent` must use `m.theme.Gap(m.theme.InterBlockGap)` and `m.theme.Gap(m.theme.InterTurnGap)` at all five call sites that currently write `"\n\n"`. The literals `"\n\n"` must not appear in `buildContent` after the change.
5. The existing `TestBuildContent_InterMessageSpacing_OneBlankLine` regression test must pass without modification to its assertion (the default gap of 1 produces 2 newlines, which the test expects).
6. `TestModel_View_Contains{Turn,AssistantTurn,ToolTurn}` must continue to pass; the header→body join is unaffected.
7. New unit tests on the `theme` package: `TestTheme_Gap` (encoding for n=0,1,2,negative) and `Test{Dark,Light}_DefaultGaps` (asserting the default values).
8. `go build ./...`, `go vet ./...`, `gofmt -l ./x/conduit/tui/...`, and `go test -race ./...` must all pass.
9. The renderer (`buildContent`) must be the only place that writes inter-block and inter-turn newlines. Per-block renderers (glamour, `renderBlockUnified`) must not change.

## Task Breakdown

### Task 1: Add `Gap` to `theme.Theme`

- **Goal:** Introduce `InterBlockGap`, `InterTurnGap`, and the `Gap` method on `*Theme`. The package builds; no caller uses them yet.
- **Dependencies:** None.
- **Files Affected:** `x/conduit/tui/theme/theme.go`.
- **New Files:** None.
- **Interfaces:** Three new symbols on `*Theme`.
- **Validation:** `go build ./x/conduit/tui/theme/...` succeeds.
- **Details:** Place the two new fields at the end of the `Theme` struct with doc comments matching the existing field-comment style. The `Gap` method takes its receiver as `*Theme` (consistent with `StyleForRole`) but does not use any field on the receiver — the method is on `*Theme` so it lives next to its caller in `view.go::buildContent`. Alternative: make it a free function `gap(n int) string` in the `view` package. Pick one: prefer the method on `*Theme` because it co-locates the encoding with the values the theme owns; future theme-side variations (e.g. `t.GapOSC(n)` for terminal-OSC-based spacing) have a natural home there. The `strings` import is added.

### Task 2: Set defaults in `Dark()` and `Light()`

- **Goal:** Populate `InterBlockGap: 1, InterTurnGap: 1` in both factory functions.
- **Dependencies:** Task 1.
- **Files Affected:** `x/conduit/tui/theme/dark.go`, `x/conduit/tui/theme/light.go`.
- **New Files:** None.
- **Interfaces:** None (struct literal field additions).
- **Validation:** `go build ./x/conduit/tui/...` succeeds.
- **Details:** Add the two lines to the existing struct literal in each factory. Place them at the end of the literal, after `ZoneLabelStyle`, with no extra comment — the field name is self-documenting.

### Task 3: Replace the five `"\n\n"` literals in `buildContent`

- **Goal:** `buildContent` writes the inter-block gap via `m.theme.Gap(m.theme.InterBlockGap)` at the two inter-block sites, and the inter-turn gap via `m.theme.Gap(m.theme.InterTurnGap)` at the three inter-turn sites. The literal `"\n\n"` no longer appears in `buildContent`.
- **Dependencies:** Tasks 1 and 2.
- **Files Affected:** `x/conduit/tui/view.go` (lines 142-146, 154-158, 170).
- **New Files:** None.
- **Interfaces:** None (internal call site changes).
- **Validation:** `go test -race ./x/conduit/tui/...` passes. `gofmt -l ./x/conduit/tui` produces no output.
- **Details:** Five substitutions, one per `"\n\n"` call. Two become `m.theme.Gap(m.theme.InterBlockGap)`, three become `m.theme.Gap(m.theme.InterTurnGap)`. The boundaries (which are inter-block vs inter-turn) are:
  - **Inter-block:** line 143 (within `m.turns`), line 155 (within `m.currentTurn`).
  - **Inter-turn:** line 146 (after `m.turns`), line 158 (after `m.currentTurn`), line 170 (after the pending placeholder).
  - Note: the "inter-block" gap inside a single-block turn is a no-op (the `if i < len(turn.blocks)-1` guard), so the inter-block call site is only one per turn, not two. Two inter-block *and* three inter-turn calls because there are three places that produce a turn boundary.

### Task 4: Add unit tests for `theme.Gap` and the factory defaults

- **Goal:** Lock in the encoding (n=0,1,2,negative) and the default values (`Dark()` and `Light()` both set `InterBlockGap=1, InterTurnGap=1`).
- **Dependencies:** Tasks 1 and 2.
- **Files Affected:** `x/conduit/tui/theme/theme_test.go`.
- **New Files:** None.
- **Interfaces:** None.
- **Validation:** `go test -race ./x/conduit/tui/theme/...` passes.
- **Details:** Add three test functions:
  - `TestTheme_Gap` — table-driven, covers `n=0` (empty string), `n=1` (`"\n\n"`), `n=2` (`"\n\n\n"`), `n=-1` (empty string, defensive), `n=100` (100+1 newlines, sanity check). Uses `assert.Equal` on the literal output.
  - `TestDark_DefaultGaps` — `assert.Equal(t, 1, dark.InterBlockGap)` and `assert.Equal(t, 1, dark.InterTurnGap)`.
  - `TestLight_DefaultGaps` — same for `Light()`.
- These are the tests that would have caught a regression where the default gap was changed silently, or where the encoding formula was misimplemented (e.g. `n` instead of `n+1`).

### Task 5: Verify and commit

- **Goal:** Confirm the full Go toolchain is clean and the diff is reviewable as a single "theme-owned TUI gaps" change.
- **Dependencies:** Tasks 1-4.
- **Files Affected:** None.
- **New Files:** None.
- **Interfaces:** None.
- **Validation:**
  - `go build ./...` exits 0.
  - `go vet ./...` exits 0.
  - `gofmt -l ./x/conduit/tui/...` produces no output.
  - `go test -race ./x/conduit/tui/...` exits 0.
  - `go test -race ./...` exits 0 across the full repo.
  - `git status` shows exactly the four files this plan touches: `theme/theme.go`, `theme/dark.go`, `theme/light.go`, `theme/theme_test.go`, `view.go`. (Five, not four — I miscounted in the header.)
  - `git grep -n '\\n\\n' x/conduit/tui/view.go` returns zero matches inside `buildContent`.
- **Details:** Final commit message subject: `feat(tui): theme-owned inter-block and inter-turn gaps`. Body should reference the existing `consolidate-tui-theme.md` plan and the gap that the consolidation left in place (five literal `"\n\n"` strings). Co-author trailer per project convention.

## Dependency Graph

Tasks 1 → 2 → 3 → 4 → 5. All sequential. Tasks 1 and 2 are both strictly additive; the call-site changes in Task 3 are the only ones that alter existing behavior, and the default values chosen in Task 2 make the behavior change a no-op (default gap of 1 → 2 newlines, same as the current literal).

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| `Gap` is on `*Theme` but doesn't use the receiver; readers find it surprising | Low | Low | Add a one-line doc comment to `Gap` clarifying that it's a method for namespacing the encoding under the theme, not because it reads theme state. If reviewers push back, refactor to a free function `gap(n int) string` in the `view` package — five-line change, no API impact. |
| A consumer of the `Theme` struct literal (in tests or examples) forgets to set the new fields, getting Go's zero value (0,0) and a tighter layout than expected | Low | Medium | `newTestModel` already uses `theme.Dark()` which sets the defaults; no test helper changes. The `examples/tui-chat/main.go` consumer uses `tui.New(...)` which goes through `theme.Auto()` → `theme.Dark()` → defaults set. The only consumer that could break is one that constructs a `*Theme` literal directly; a `go vet`-style lint could catch this but is out of scope. |
| The `strings` import in `theme.go` is unused if `Gap` is later moved out of the theme package | Negligible | Low | Trivial revert. |
| Future glamour version changes its per-block trailing-newline behavior, breaking the gap-encoding assumption that "n gap = n+1 newlines after the previous block's last line" | Medium | Low | The default gap of 1 produces 2 newlines regardless of glamour's behavior, because `buildContent` writes the full gap unconditionally; it does not depend on glamour emitting (or not emitting) a trailing newline. The "n+1" formula assumes there is always a line break between adjacent content, which is invariant of glamour. No mitigation needed. |
| The `TestBuildContent_InterMessageSpacing_OneBlankLine` test's count of 2 newlines is sensitive to the default gap; a future "tighter default" change to the theme would break it | Low | Low | The test asserts the *default* behavior; if the default changes, the test's expected count should be updated as part of the same change. The test doc comment should be updated to say "default gap of 1 → 2 newlines" rather than the current wording. |

## Validation Criteria

- [ ] `x/conduit/tui/theme/theme.go` defines `InterBlockGap int`, `InterTurnGap int`, and `func (t *Theme) Gap(n int) string`.
- [ ] `theme.Dark()` and `theme.Light()` set `InterBlockGap: 1, InterTurnGap: 1`.
- [ ] `theme.Gap(0)` returns `""`. `theme.Gap(1)` returns `"\n\n"`. `theme.Gap(2)` returns `"\n\n\n"`. `theme.Gap(-1)` returns `""`.
- [ ] `x/conduit/tui/view.go::buildContent` contains no literal `"\n\n"`. Five call sites use `m.theme.Gap(m.theme.InterBlockGap)` or `m.theme.Gap(m.theme.InterTurnGap)`.
- [ ] `go test -race ./x/conduit/tui/...` passes.
- [ ] `go test -race ./...` passes across the full repo.
- [ ] `go vet ./...` clean. `gofmt -l ./x/conduit/tui/...` clean.
- [ ] `TestBuildContent_InterMessageSpacing_OneBlankLine` passes without modification.
- [ ] `TestModel_View_Contains{Turn,AssistantTurn,ToolTurn}` passes without modification.
- [ ] New tests `TestTheme_Gap`, `TestDark_DefaultGaps`, `TestLight_DefaultGaps` pass.
