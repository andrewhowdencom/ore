# Plan: Replace TUI Input Buffer with `textarea`

## Objective

Replace the TUI's bare single-line `strings.Builder` input with a `charmbracelet/bubbles/textarea` widget, enabling multi-line editing, richer keyboard navigation, and a visually separated input area anchored at the bottom of the terminal.

## Context

- The TUI lives in `conduit/tui/`. It uses Bubble Tea (`bubbletea`) with a `viewport` for scrollable conversation history and a hand-rolled `strings.Builder` input buffer rendered as `> text_` at the bottom.
- Current key handling in `model.go` manually implements `KeyRunes`, `KeySpace`, `KeyBackspace`, `KeyEnter` (submit), and `KeyCtrlC` (quit). `PgUp`/`PgDown` scroll the viewport.
- `charmbracelet/bubbles` is already a direct dependency (`v1.0.0` in `go.mod`), so `bubbles/textarea` is available without new module imports.
- `textarea`'s default height is 6 lines (`defaultHeight = 6`). It does not auto-grow; height must be set explicitly via `SetHeight()`.
- `textarea.View()` renders prompt, line numbers (if enabled), and text with soft word-wrapping to its internal content width (`m.width`).
- The `SetWidth(w)` API automatically subtracts prompt width, line-number gutter, and border padding from the wrapping width, so `m.textarea.Width()` returns the *content* width usable for wrapping estimates.
- Status is rendered inside the viewport content string (unchanged from current behavior). Status lines *below* the input are explicitly out of scope for this task.

## Architectural Blueprint

### Selected approach

Use `bubbles/textarea.Model` as the input widget. Intercept `tea.KeyMsg` in `model.Update` before passing to `textarea` for:
- `KeyEnter` (no modifiers) → submit message, clear input.
- `KeyEnter` with `Alt` → pass to `textarea` to insert a newline.  
  *(Note: `bubbletea` v1.3.10 does not detect the `Shift` modifier on special keys, so `Alt+Enter` is used as the practical alternative.)*
- `KeyPgUp` / `KeyPgDown` → pass to `viewport` to scroll conversation history.
- `KeyCtrlC` → send interrupt event, return `tea.Quit`.
- All other keys → pass to `textarea.Update()`.

After any `textarea.Update()` call, recalculate the textarea's desired height from its current value and content width, clamp to a terminal-fraction cap, update `SetHeight()`, and resize the conversation `viewport` to fill the remaining space above a 1-line horizontal separator.

### Why `textarea` over custom

- `textarea` provides multi-line input, cursor navigation, Home/End, Ctrl+A/E, and backspace/delete out of the box.
- The project already depends on `bubbles` (for `viewport`), so adding `textarea` adds zero new module dependencies.
- Custom key handling on `strings.Builder` would require re-implementing cursor tracking, rune-aware deletion, and wrapping — all of which `textarea` already solves.

### Alternatives considered

- **Custom multi-line input on `strings.Builder`** — Rejected. Would duplicate a large fraction of `textarea`'s logic and be error-prone with Unicode widths.
- **`bubbles/textinput`** — Rejected. Single-line only; does not satisfy the multi-line requirement.
- **Keep single-line, improve styling only** — Rejected. Issue #10 explicitly requires multi-line input (Alt+Enter for newline).

## Requirements

1. Replace `model.input strings.Builder` with `textarea textarea.Model`.
2. `Enter` submits the current input as a `UserMessageEvent`; `Alt+Enter` inserts a newline into the textarea.  
   *(Note: `bubbletea` v1.3.10 cannot detect `Shift+Enter`, so `Alt+Enter` is the practical alternative.)*
3. `PgUp`/`PgDown` scroll the conversation `viewport`, never the textarea cursor.
4. `Ctrl+C` sends `InterruptEvent` and quits cleanly.
5. Arrow keys, Home/End, and standard `textarea` shortcuts (Ctrl+A/E, etc.) navigate within the input.
6. The textarea height grows dynamically with content, capped so it never consumes more than ~40% of the terminal.
7. A horizontal rule (`─`) visually separates the conversation viewport from the input area.
8. Terminal resize recalculates textarea width, textarea height, and viewport height correctly.
9. All existing tests continue to pass after updating assertions to match the new implementation.

## Task Breakdown

### Task 1: Switch model input to `textarea.Model` and wire behavior
- **Goal**: Replace the `strings.Builder` input with `textarea`, implement key routing, dynamic height, visual separator, and update all package tests.
- **Dependencies**: None.
- **Files Affected**:
  - `conduit/tui/model.go`
  - `conduit/tui/view.go`
  - `conduit/tui/tui.go`
  - `conduit/tui/model_test.go`
  - `conduit/tui/view_test.go`
  - `conduit/tui/tui_test.go`
- **New Files**: None.
- **Interfaces**:
  - `model.input` field type changes from `strings.Builder` to `textarea.Model`.
  - `tui.New()` must initialize `textarea.Model`, call `.Focus()`, set `ShowLineNumbers = false`, configure a subtle `Prompt` (e.g., `> ` or empty), and set an initial width/height.
  - `model.Update(tea.KeyMsg)` routing changes: intercept `Enter`, `Alt+Enter`, `PgUp`, `PgDown`, `CtrlC`; route everything else through `m.textarea.Update()`.
  - Add `recalcLayout()` helper: after any `textarea` state change, compute desired height from `m.textarea.Value()` and `m.textarea.Width()`, call `m.textarea.SetHeight(desired)`, then set `m.viewport.Height = m.height - m.textarea.Height() - 1` (subtracting 1 for the horizontal separator).
  - `model.View()` changes: render `m.viewport.View()` + newline + horizontal separator (`strings.Repeat("─", m.width)`) + newline + `m.textarea.View()`.
  - `model.Update(tea.WindowSizeMsg)`: set `m.textarea.SetWidth(msg.Width)`, then call `recalcLayout()`.
- **Validation**:
  - `go test -race ./conduit/tui/...` passes.
  - No compilation errors in `examples/tui-chat/main.go`.
- **Details**:
  - Update tests that reference `m.input` (`strings.Builder`) to reference `m.textarea.Value()` instead.
  - Update view tests that assert on the old `> _` prompt suffix to assert on `m.textarea.View()` presence or on the separator line instead.
  - Add new test: `Alt+Enter` inserts a newline (textarea value contains `\n`).
  - Add new test: `Enter` on multi-line input submits and clears the textarea.
  - Add new test: `PgUp`/`PgDown` still scroll the viewport even when the textarea has multi-line content.
  - Add new test: horizontal separator appears in `View()` output.
  - Height estimation can start simple: `strings.Count(value, "\n") + 1` plus a small buffer for word-wrapping (e.g., `sum(len(line) / contentWidth)`). Cap at `max(5, m.height/3)`.

### Task 2: Full project integration validation
- **Goal**: Verify the entire project compiles and passes tests with the `conduit/tui` changes.
- **Dependencies**: Task 1.
- **Files Affected**: None (read-only validation).
- **New Files**: None.
- **Interfaces**: None.
- **Validation**:
  - `go test -race ./...` passes.
  - `go build ./...` passes.
- **Details**:
  - Run the full test suite with race detection.
  - Confirm `examples/tui-chat` compiles (`go build ./examples/tui-chat/...`).
  - If any cross-package tests fail due to the TUI changes, diagnose and fix (likely none, since the TUI package has a narrow public API).

## Dependency Graph

- Task 1 → Task 2

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| `textarea` word-wrap line count differs from our dynamic-height estimate, causing visual jitter or truncated text | Medium | Medium | Keep height estimate conservative (slightly over-count). The builder can refine the estimate in Task 1; exact pixel-perfect matching is not required for a chat input. |
| Key routing between `textarea` and `viewport` fights (e.g., arrow keys unexpectedly scroll viewport instead of moving cursor) | Medium | Low | Explicitly intercept only `PgUp`/`PgDown` and `Enter`/`Alt+Enter`. All other keys pass to `textarea`. Add targeted tests for cursor movement vs. viewport scrolling. |
| `textarea` default styling (line numbers, borders) clashes with existing app styling | Low | Low | Explicitly disable line numbers and minimize border/padding in `tui.New()`. The visual separator is rendered by our `View()` code, not by `textarea`. |
| Tests asserting exact `View()` output break due to `textarea` injecting ANSI cursor/styles | Medium | Medium | Update assertions to check for content presence rather than exact string equality. Use `strings.Contains` or `lipgloss.Width` checks. |

## Validation Criteria

- [ ] `go test -race ./conduit/tui/...` passes after Task 1.
- [ ] `go test -race ./...` passes after Task 2.
- [ ] `go build ./...` passes after Task 2.
- [ ] `Alt+Enter` inserts a newline in the textarea (verified by test).
- [ ] `Enter` submits the message and clears the textarea (verified by test).
- [ ] `PgUp`/`PgDown` scroll the conversation viewport even when the textarea contains multi-line text (verified by test).
- [ ] The rendered view contains a horizontal separator line between the conversation history and the input area (verified by test).
- [ ] Terminal resize updates the textarea width and viewport height without panic (verified by existing `WindowSize` test updated for new layout).
