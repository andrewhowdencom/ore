# Plan: TUI Markdown Rendering, Auto-Scroll, and Padding Fixes

## Objective

Fix three rendering and scrolling defects in the `conduit/tui` Bubble Tea surface: reasoning content bypasses the Markdown renderer, the viewport auto-scrolls against stale content height, and glamour injects unwanted document-level margin padding. The fixes must preserve all existing tests, add coverage for the new behavior, and leave `go test -race ./...` passing.

## Context

The TUI is implemented in `conduit/tui/` using the Bubble Tea framework. Key files:

- `conduit/tui/model.go` — `tea.Model` implementation. Handles `Update()` for incoming turns, status, keyboard input, and window resize. Currently renders `artifact.Text` for assistant turns through `renderMarkdown()`, but stores `artifact.Reasoning` as raw text. Calls `m.viewport.GotoBottom()` in the `turnMsg` handler, but `m.viewport.SetContent()` only happens inside `View()`.
- `conduit/tui/view.go` — `View()` implementation. Builds the full conversation string, calls `m.viewport.SetContent(b.String())`, then appends a horizontal separator and the textarea widget. Wraps raw reasoning text with `cellbuf.Wrap`.
- `conduit/tui/markdown.go` — Defines the `markdownRenderer` interface and the `glamourMarkdownRenderer` production implementation. Uses `glamour.WithAutoStyle()` and `glamour.WithWordWrap(width)`.
- `conduit/tui/tui.go` — Conduit wrapper that wires the model into a `tea.Program`. In `New()`, the model is initialized with `md: glamourMarkdownRenderer{}`.
- `conduit/tui/model_test.go` and `conduit/tui/view_test.go` — Extensive table-driven tests for model update logic and view rendering.

Glamour v1.0.0 is the dependency. Its built-in `dark.json` and `light.json` set `"document": { "margin": 2 }`, which adds a uniform left margin and blank lines around every rendered block. Glamour's `WithAutoStyle()` uses `term.IsTerminal(int(os.Stdout.Fd()))` followed by `termenv.HasDarkBackground()` to pick dark vs. light at runtime.

## Architectural Blueprint

The plan follows the issue's proposed design with a pragmatic minimal-change approach for the auto-scroll fix.

1. **Custom zero-margin styles**: Create `conduit/tui/styles/dark.json` and `conduit/tui/styles/light.json` as copies of glamour's upstream files with only `document.margin` changed from `2` to `0`. Embed them via `//go:embed`, replicate glamour's auto-detection logic in Go, and load the selected style bytes via `glamour.WithStylesFromJSONBytes()`. This replaces `WithAutoStyle()` entirely.

2. **Reasoning blocks through glamour**: Extend the `turnMsg` handler in `model.Update` so that `artifact.Reasoning` on assistant turns is routed through `m.renderMarkdown()` and cached in `renderedBlock.rendered`. In `View()`, reasoning blocks display from their rendered cache (width `0` to avoid double-wrapping), falling back to raw text on render error.

3. **Auto-scroll timing**: Extract the viewport string-building logic from `View()` into a new `buildContent() string` helper on `model`. In `Update()`'s `turnMsg` case, call `m.viewport.SetContent(m.buildContent())` **before** `m.viewport.GotoBottom()`. Keep the existing `SetContent` call in `View()` as a harmless fallback so that existing `TestModel_View_*` tests continue to work without modification.

## Requirements

1. Reasoning blocks must be rendered through glamour and display with proper terminal styling. `[derived from issue]`
2. Viewport must auto-scroll to the bottom when new assistant turns arrive, operating on the fresh content height. `[derived from issue]`
3. Markdown output must have no document-level margin padding (`document.margin = 0`). `[derived from issue]`
4. Both dark and light terminal backgrounds must render with correct colors. `[derived from issue]`
5. Existing tests must pass or be updated to reflect the changes. `[derived from issue]`
6. `go test -race ./...` must pass. `[derived from issue]`

## Task Breakdown

### Task 1: Embed Custom Zero-Margin Glamour Styles
- **Goal**: Replace `glamour.WithAutoStyle()` with embedded custom dark and light JSON styles that set `document.margin` to `0`, and add runtime dark/light detection.
- **Dependencies**: None.
- **Files Affected**: `conduit/tui/markdown.go`, `go.mod`.
- **New Files**: `conduit/tui/styles/dark.json`, `conduit/tui/styles/light.json`, `conduit/tui/styles.go` (or inline embed in `markdown.go`).
- **Interfaces**:
  - `glamourMarkdownRenderer` struct gains a `styleBytes []byte` field.
  - New constructor: `func newGlamourMarkdownRenderer() *glamourMarkdownRenderer`.
  - New helper: `func isDarkBackground() bool` (replicates glamour's `term.IsTerminal` + `termenv.HasDarkBackground` logic).
  - `markdownRenderer` interface remains unchanged.
- **Validation**:
  - `go test ./conduit/tui/...` passes.
  - `go test -race ./...` passes.
  - `go mod tidy` leaves `go.mod` and `go.sum` clean (promotes `termenv` and `golang.org/x/term` from indirect to direct if needed).
- **Details**:
  1. Create `conduit/tui/styles/dark.json` by copying glamour's upstream `dark.json` and changing `"margin": 2` to `"margin": 0`. Add a comment header noting the upstream origin and the single tweak.
  2. Do the same for `conduit/tui/styles/light.json`.
  3. In a new file `conduit/tui/styles.go` (or at the top of `markdown.go`), add:
     ```go
     //go:embed styles/dark.json
     var darkStyle []byte
     //go:embed styles/light.json
     var lightStyle []byte
     ```
  4. Implement `isDarkBackground()` using `term.IsTerminal(int(os.Stdout.Fd()))` and `termenv.HasDarkBackground()`, matching glamour's `getDefaultStyle("auto")` logic. Default to dark when not a terminal.
  5. Update `glamourMarkdownRenderer` to hold `styleBytes []byte` and use `glamour.WithStylesFromJSONBytes(r.styleBytes)` instead of `glamour.WithAutoStyle()`.
  6. Update `newGlamourMarkdownRenderer()` to select the correct style bytes at construction time.
  7. Update the `renderMarkdown` fallback in `model.go` from `m.md = glamourMarkdownRenderer{}` to `m.md = newGlamourMarkdownRenderer()`.
  8. Update `tui.New()` in `tui.go` to initialize the model with `md: newGlamourMarkdownRenderer()`.
  9. Run `go mod tidy`.

### Task 2: Route Reasoning Blocks Through the Markdown Renderer
- **Goal**: Render `artifact.Reasoning` for assistant turns through glamour, cache the output, and display the cached rendered string in `View()`.
- **Dependencies**: Task 1.
- **Files Affected**: `conduit/tui/model.go`, `conduit/tui/view.go`, `conduit/tui/model_test.go`, `conduit/tui/view_test.go`.
- **New Files**: None.
- **Interfaces**: No new interfaces; `renderedBlock.rendered` field is already present and will be populated for reasoning blocks.
- **Validation**:
  - `go test ./conduit/tui/...` passes.
  - `TestModel_Update_Turn_PreservesReasoning` asserts that `rendered` is populated.
  - `TestModel_View_AssistantTurn_WithReasoning` still passes (plain text survives inside ANSI output).
  - A new test `TestModel_View_AssistantTurn_Reasoning_Rendered` verifies that a mock-rendered reasoning string appears in the view and raw source is suppressed.
- **Details**:
  1. In `model.go`, inside the `turnMsg` handler's `artifact.Reasoning` case (which currently only stores raw source), add:
     ```go
     block := renderedBlock{kind: "reasoning", source: a.Content}
     if msg.turn.Role == state.RoleAssistant {
         rendered, err := m.renderMarkdown(a.Content, m.viewport.Width)
         if err == nil {
             block.rendered = rendered
         }
     }
     blocks = append(blocks, block)
     ```
     (Replace the current single-line `append` with this logic.)
  2. In `view.go`, inside the `case "reasoning":` block, add the same rendered-cache check used for text:
     ```go
     case "reasoning":
         if block.rendered != "" {
             b.WriteString(renderBlock("Thinking: ", thinkingStyle, block.rendered, 0))
         } else {
             b.WriteString(renderBlock("Thinking: ", thinkingStyle, block.source, width))
         }
     ```
  3. In `model_test.go`, update `TestModel_Update_Turn_PreservesReasoning` to assert `assert.NotEmpty(t, mm.turns[0].blocks[1].rendered)`.
  4. In `view_test.go`, add `TestModel_View_AssistantTurn_Reasoning_Rendered` that injects a `mockMarkdownRenderer`, sends a turn with reasoning, and asserts the mock output appears in `View()` while the raw source does not.

### Task 3: Fix Auto-Scroll Timing
- **Goal**: Ensure `GotoBottom()` operates on the freshly updated viewport content when a new turn arrives.
- **Dependencies**: Task 2.
- **Files Affected**: `conduit/tui/model.go`, `conduit/tui/view.go`.
- **New Files**: None.
- **Interfaces**: New helper method `func (m *model) buildContent() string`.
- **Validation**:
  - `go test ./conduit/tui/...` passes.
  - `TestModel_Update_Turn_AutoScrollsViewport` passes.
  - A new or updated test adds a tall assistant turn to `m.turns` before sending the message, then asserts `AtBottom()` is true after `Update`, proving the scroll used the new content height.
- **Details**:
  1. Extract the string-building logic from `View()` (everything before `m.viewport.SetContent(b.String())`) into a new unexported method `func (m *model) buildContent() string`. It should replicate the exact builder logic: iterate `m.turns`, render user/assistant/tool blocks, append pending placeholder, append status line, and return `b.String()`.
  2. In `View()`, replace the inline builder with:
     ```go
     m.viewport.SetContent(m.buildContent())
     ```
     Keep the rest of `View()` unchanged (separator and textarea assembly).
  3. In `model.go`, inside the `Update()` `turnMsg` case, after appending the new `renderedTurn` to `m.turns` and updating `m.pending`, insert:
     ```go
     m.viewport.SetContent(m.buildContent())
     m.viewport.GotoBottom()
     ```
     (Remove the old standalone `m.viewport.GotoBottom()` or keep it as a no-op if already at bottom.)
  4. Verify that `TestModel_Update_Turn_AutoScrollsViewport` still passes. Optionally strengthen it by pre-populating `m.turns` with a tall block so the new turn genuinely increases content height.

## Dependency Graph

- Task 1 → Task 2 (Task 2 relies on the updated renderer being in place)
- Task 2 → Task 3 (Task 3 touches the same `Update` and `View` methods; sequential reduces merge risk)

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Custom style JSON drifts from glamour upstream on future upgrades | Low | Medium | Add a prominent comment in each JSON file documenting the upstream origin, the exact commit/tag, and the single tweak applied. |
| `termenv.HasDarkBackground()` misbehaves in headless CI or non-TTY environments | Medium | Low | Replicate glamour's exact guard (`term.IsTerminal`) and default to dark when no terminal is detected, matching upstream behavior. |
| `buildContent()` extraction misses edge cases (empty turns, pending flag, status line) | Medium | Low | The extracted method is a pure refactor of existing `View()` logic; the unchanged `View()` fallback path continues to exercise it on every frame. Existing tests provide a safety net. |
| ANSI escape sequences in rendered output break `assert.Contains` view tests | Low | Medium | View tests for the rendered path should inject a `mockMarkdownRenderer` with predictable plain-text output, avoiding glamour's ANSI in assertions. |

## Validation Criteria

- [ ] `go test ./conduit/tui/...` passes without failures.
- [ ] `go test -race ./...` passes across the entire repo.
- [ ] `TestModel_Update_Turn_PreservesReasoning` asserts that reasoning blocks have a non-empty `rendered` cache after `Update`.
- [ ] A new or updated view test confirms that reasoning blocks display the rendered cache when present, and fall back to raw text when the rendered cache is empty.
- [ ] `TestModel_Update_Turn_AutoScrollsViewport` passes, confirming the viewport is at the bottom after a new assistant turn arrives.
- [ ] The embedded `dark.json` and `light.json` both contain `"document": { "margin": 0 }` (and no other margin values at the document level).
- [ ] `go mod tidy` does not introduce unexpected dependencies.
