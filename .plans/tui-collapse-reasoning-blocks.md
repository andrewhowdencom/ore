# Plan: TUI Collapse Reasoning Blocks by Default and Grey Them Out When Expanded

## Objective

Wire reasoning artifacts in the TUI conduit into the same expand/collapse toggle as tool calls (`Ctrl+O`), so that reasoning blocks are collapsed to a compact `Thinking...` indicator by default in the latest assistant turn, and render with a subdued/dimmed style when expanded. Historical reasoning blocks always remain compact. The `Ctrl+O` binding becomes a single "show/hide latest assistant turn details" toggle that applies to both tool calls and reasoning blocks.

## Context

The ore TUI conduit lives in `x/conduit/tui/` and is built on Bubble Tea v2 with lipgloss v2 styling. Key files:

- **`x/conduit/tui/model.go`** — Bubble Tea model implementing `tea.Model`. Receives `turnMsg` carrying complete `state.Turn` values. Already renders reasoning through the same Markdown/glamour pipeline as text (`renderMarkdown`), caching the ANSI output in `renderedBlock.rendered`. The `expandLatestTools bool` field controls whether the latest assistant turn's tool calls and results are shown expanded or compact. `Ctrl+O` toggles this flag; it resets to `false` after each new assistant turn.
- **`x/conduit/tui/view.go`** — `buildContent()` constructs the viewport string. Reasoning blocks currently always render fully with a `Thinking:` label styled by `thinkingStyle` (`Faint(true).Italic(true)`). The label style is applied, but the Markdown-rendered body receives no additional styling, making it visually identical to normal text.
- **`x/conduit/tui/styles.go`** — Embeds tweaked glamour dark/light JSON style files with `document.margin` set to 0. No lipgloss styles are defined here.
- **`x/conduit/tui/README.md`** — Documents `Ctrl+O` as toggling "tool blocks".
- **`artifact/artifact.go`** — Defines `artifact.Reasoning{Content string}` with `Kind() "reasoning"`.

Existing toggle pattern: tool calls compute a `compact` string in `Update()`, then `buildContent()` checks `isLatestAssistant && m.expandLatestTools` to decide between compact (`→ foo`) and expanded (`Assistant: Calling: foo({})`) rendering. Tool results use `isAfterLatestAssistant && m.expandLatestTools`. Historical tool blocks are always compact.

## Architectural Blueprint

Extend the existing `expandLatestTools` toggle to cover reasoning blocks as well, renaming it to `expandLatestDetails` for clarity. The change is a mechanical extension of the established compact/expanded pattern:

1. **Model layer (`model.go`)**: When processing an `artifact.Reasoning` in `Update()`, set `renderedBlock.compact = "Thinking..."` so the block carries its compact representation just like tool calls. Rename `expandLatestTools` → `expandLatestDetails` and update all references (turn reset, `Ctrl+O` toggle).
2. **View layer (`view.go`)**: In `buildContent()`, for reasoning blocks in the latest assistant turn, check `m.expandLatestDetails` instead of always rendering full content. Collapsed: emit `thinkingStyle.Render("Thinking...")`. Expanded: emit the `Thinking:` label plus the rendered/source content wrapped in a new `reasoningExpandedStyle` (`Faint(true)`) so the entire Markdown body is visually subdued. For historical turns, reasoning always stays compact.
3. **Styling (`view.go` inline)**: Add `reasoningExpandedStyle = lipgloss.NewStyle().Faint(true)`. This is applied to the already-ANSI-rendered Markdown output; lipgloss v2 handles styling over existing ANSI sequences.
4. **Tests**: Update all existing reasoning-related view tests (which currently expect full content) to either set `expandLatestDetails = true` or assert the new compact indicator. Add new tests verifying compact mode, expanded mode with subdued styling, and historical-turn compaction.
5. **Documentation**: Update `README.md` to describe `Ctrl+O` as toggling "latest assistant turn details (tool calls and reasoning)".

No Tree-of-Thought deliberation was needed: the issue explicitly demands a single unified toggle, and the existing tool-call compact/expanded pattern is the natural extension point.

## Requirements

1. Reasoning blocks in the **latest assistant turn** render as a compact `Thinking...` indicator by default (collapsed).
2. `Ctrl+O` toggles expansion of **both** reasoning blocks and tool blocks in the latest assistant turn. The toggle state resets automatically on each new assistant turn.
3. When expanded, the full reasoning content (including the Markdown-rendered body) is visually subdued/dimmed via a `Faint(true)` lipgloss style applied over the ANSI output.
4. Reasoning blocks in **historical** (non-latest) assistant turns always remain compact.
5. The compact indicator only appears when a reasoning artifact has actually been received (it is not a general pending spinner).
6. Existing tool-call compact/expanded behavior must remain unchanged.
7. All tests pass with race detection enabled (`go test -race ./x/conduit/tui/...`).

## Task Breakdown

### Task 1: Extend Model to Support Reasoning Compact Representation
- **Goal**: Rename `expandLatestTools` to `expandLatestDetails`, set `compact` on reasoning blocks during turn processing, and update all model-level references.
- **Dependencies**: None.
- **Files Affected**: `x/conduit/tui/model.go`
- **New Files**: None.
- **Interfaces**:
  - Field rename: `model.expandLatestTools bool` → `model.expandLatestDetails bool`
  - `Update()` `turnMsg` handler: `m.expandLatestTools = false` → `m.expandLatestDetails = false`
  - `Update()` `tea.KeyPressMsg` `Ctrl+O` handler: `m.expandLatestTools = !m.expandLatestTools` → `m.expandLatestDetails = !m.expandLatestDetails`
  - `Update()` `artifact.Reasoning` branch: set `block.compact = "Thinking..."`
- **Validation**: `go test -race ./x/conduit/tui/...` compiles and existing model tests still pass (tests using the old field name will fail until Task 3).
- **Details**:
  1. Rename the field and update its comment to describe it as controlling "latest assistant turn details (tool calls and reasoning)".
  2. In the `turnMsg` handler's `artifact.Reasoning` case, after creating the `renderedBlock` with `kind: "reasoning"`, `source: a.Content`, and the rendered cache, also set `compact: "Thinking..."`.
  3. Update the two places in `Update()` that read/write the boolean flag (turn reset and `Ctrl+O` toggle).
  4. Leave `WindowSizeMsg` re-rendering loop untouched; it already re-renders text blocks and does not need to handle reasoning separately (the cached `rendered` string is still valid, and the width-0 path skips re-wrapping).

### Task 2: Update buildContent() for Reasoning Collapse/Expand and Subdued Styling
- **Goal**: Render reasoning blocks collapsed by default in the latest turn, expanded with dimmed styling when toggled, and always compact in historical turns.
- **Dependencies**: Task 1.
- **Files Affected**: `x/conduit/tui/view.go`
- **New Files**: None.
- **Interfaces**:
  - New style variable: `reasoningExpandedStyle = lipgloss.NewStyle().Faint(true)`
  - Updated `buildContent()` `case "reasoning":` logic:
    - `isExpanded := isLatestAssistant && m.expandLatestDetails`
    - Collapsed: `b.WriteString(thinkingStyle.Render("Thinking..."))`
    - Expanded with rendered cache: `b.WriteString(renderBlock("Thinking: ", thinkingStyle, reasoningExpandedStyle.Render(block.rendered), 0))`
    - Expanded without rendered cache: `b.WriteString(renderBlock("Thinking: ", thinkingStyle, reasoningExpandedStyle.Render(block.source), width))`
  - Update `tool_call` `isExpanded` check to use `m.expandLatestDetails`
  - Update `tool_result` `isExpanded` check to use `m.expandLatestDetails`
- **Validation**: `go test -race ./x/conduit/tui/...` compiles.
- **Details**:
  1. Add `reasoningExpandedStyle` in the style block near the top of `view.go`.
  2. Replace the existing `case "reasoning":` branch in `buildContent()` with the compact/expanded logic described above.
  3. Update the two existing `m.expandLatestTools` references in `buildContent()` (`tool_call` and `tool_result` branches) to `m.expandLatestDetails`.
  4. Ensure the compact indicator (`Thinking...`) is a single-line string with no trailing newline beyond what the inter-block spacing logic already provides.

### Task 3: Update and Add Tests
- **Goal**: Fix all test references to the renamed field, update reasoning-related view tests to account for compact-by-default behavior, and add new tests for the new compact/expanded reasoning rendering.
- **Dependencies**: Task 1, Task 2.
- **Files Affected**: `x/conduit/tui/model_test.go`, `x/conduit/tui/view_test.go`
- **New Files**: None.
- **Interfaces**:
  - Renamed test: `TestModel_Update_KeyCtrlO_TogglesExpandLatestTools` → `TestModel_Update_KeyCtrlO_TogglesExpandLatestDetails`
  - Renamed test: `TestModel_Update_Turn_Assistant_ResetsExpandLatestTools` → `TestModel_Update_Turn_Assistant_ResetsExpandLatestDetails`
  - Updated test: `TestModel_Update_UserAfterTool_DoesNotResetExpand` — rename field references in assertions/comments
  - Updated tests: reasoning view tests that expect full content must set `m.expandLatestDetails = true`
  - New test: `TestBuildContent_Reasoning_Compact` — latest assistant turn with reasoning, `expandLatestDetails = false`, asserts `Thinking...` indicator present and full reasoning content absent
  - New test: `TestBuildContent_Reasoning_Expanded` — latest assistant turn with reasoning, `expandLatestDetails = true`, asserts full content present and styled with `reasoningExpandedStyle`
  - New test: `TestBuildContent_Reasoning_OldTurn_AlwaysCompact` — reasoning in a non-latest assistant turn, `expandLatestDetails = true`, asserts compact indicator (historical reasoning stays compact)
  - New test: `TestModel_Update_KeyCtrlO_TogglesReasoningExpansion` — sends an assistant turn with reasoning, toggles `Ctrl+O`, verifies `buildContent()` reflects the toggle
- **Validation**: `go test -race ./x/conduit/tui/...` passes.
- **Details**:
  1. Global search/replace `expandLatestTools` → `expandLatestDetails` across both test files. Update test names where they explicitly reference the old name.
  2. For `TestModel_View_AssistantTurn_WithReasoning`: set `m.expandLatestDetails = true` before calling `View()` so the test can continue verifying full content rendering.
  3. For `TestModel_View_AssistantTurn_MultiBlockSpacing`: set `m.expandLatestDetails = true` so the full reasoning content is available for order verification.
  4. For `TestModel_View_AssistantTurn_Reasoning_Rendered`: after `Update(turnMsg)`, set `mm.expandLatestDetails = true` before calling `View()`.
  5. For `TestBuildContent_MixedBlocks`: set `m.expandLatestDetails = true` so all block types are visible for order verification.
  6. For new compact tests, construct `renderedTurn` with `kind: "reasoning"` and a `compact` field value (set manually on the test struct; the actual compact is assigned by `Update()` in production).

### Task 4: Update README Documentation
- **Goal**: Document that `Ctrl+O` now toggles both tool calls and reasoning blocks.
- **Dependencies**: None (can run in parallel with Task 3, but logically follows Task 2).
- **Files Affected**: `x/conduit/tui/README.md`
- **New Files**: None.
- **Interfaces**: Updated keyboard shortcuts table entry for `Ctrl+O`.
- **Validation**: Manual review of the README renders correctly.
- **Details**:
  1. Update the `Ctrl+O` row: change description from "Toggle expansion of the latest assistant turn's tool blocks" to "Toggle expansion of the latest assistant turn's details (tool calls and reasoning)".
  2. Update the Design paragraph to mention reasoning alongside tool calls as content that is compact by default.

### Task 5: Verify All Tests Pass
- **Goal**: Run the full TUI test suite with race detection to confirm no regressions.
- **Dependencies**: Task 3, Task 4.
- **Files Affected**: None.
- **New Files**: None.
- **Interfaces**: None.
- **Validation**: `go test -race ./x/conduit/tui/...` exits with status 0.
- **Details**:
  1. Run `go test -race ./x/conduit/tui/...`.
  2. If any tests fail, triage and fix. Common issues will be:
     - Missing `expandLatestDetails = true` in tests that previously relied on default-expanded reasoning.
     - Compact indicator string mismatches (`Thinking...` vs `Thinking: `).
     - Style assertion mismatches (ANSI escape sequence differences).

## Dependency Graph

- Task 1 → Task 2
- Task 1 → Task 3
- Task 2 → Task 3
- Task 3 → Task 5
- Task 4 || Task 3 (Task 4 is independent and can be done in parallel, but commit order should place it before or with Task 3)

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Lipgloss `Faint(true)` does not visually dim glamour-rendered ANSI output as expected on some terminals | Medium | Low | Verify in common terminals (iTerm2, GNOME Terminal, Windows Terminal). If `Faint` is insufficient, switch to an explicit grey foreground color (`Foreground(lipgloss.Color("#666666"))`) which overrides all underlying colors. The implementation allows swapping the style definition without changing structural logic. |
| Tests that assert on exact ANSI escape sequences break due to style additions | Medium | Medium | Avoid asserting on raw ANSI bytes. Assert on content presence/absence and use `assert.Contains`/`assert.NotContains`. Where style-specific assertions are needed, use the existing pattern of `style.Render(expected)` for comparison rather than hardcoding escape sequences. |
| Renaming `expandLatestTools` to `expandLatestDetails` breaks external consumers (if any) | Low | Low | The field is unexported on an unexported `model` struct, so no external consumers exist. The rename is safe. |
| Compact reasoning indicator in historical turns may be confused with the pending spinner | Low | Low | The compact indicator uses `thinkingStyle` (faint+italic) while the pending placeholder uses `assistantStyle` (blue) plus `...` after `Assistant: `. They are visually distinct. Ensure tests verify this distinction. |

## Validation Criteria

- [ ] `go test -race ./x/conduit/tui/...` passes with zero failures.
- [ ] `x/conduit/tui/model.go` compiles: `go build ./x/conduit/tui/...` succeeds.
- [ ] Reasoning blocks in the latest assistant turn render as `Thinking...` (single line, faint+italic) when `expandLatestDetails` is `false`.
- [ ] Reasoning blocks in the latest assistant turn render as `Thinking:` label + full content when `expandLatestDetails` is `true`, and the content body is styled with `Faint(true)`.
- [ ] Reasoning blocks in historical assistant turns always render as `Thinking...` regardless of `expandLatestDetails`.
- [ ] Tool-call and tool-result compact/expanded behavior is unchanged (only the field name changed).
- [ ] `Ctrl+O` toggles `expandLatestDetails` and refreshes the viewport.
- [ ] New assistant turns reset `expandLatestDetails` to `false`.
- [ ] `x/conduit/tui/README.md` accurately describes `Ctrl+O` as toggling both tool calls and reasoning.
