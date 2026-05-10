# Plan: Add Markdown Rendering to TUI

## Objective
Enable the TUI surface to render assistant responses as rich Markdown using `charmbracelet/glamour`, providing syntax highlighting for code blocks, styled headings/lists, and inline formatting. Streaming text remains plain-text (incomplete Markdown renders poorly), while finalized turns are pre-rendered and cached. Re-rendering happens on terminal resize.

## Context
The TUI surface lives in `surface/tui/` with these key files:
- `surface/tui/model.go`: Defines `renderedTurn` (`role`, `text string`), `model.Update` (handles `turnMsg` by extracting `artifact.Text` into `renderedTurn.text`), and viewport management.
- `surface/tui/view.go`: `View()` iterates `turns`, calls `wrapText()` (label + indent + `cellbuf.Wrap`) for each role, and sets viewport content. `assistantStyle` applies a subtle blue foreground to the "Assistant: " label.
- `surface/tui/tui.go`: Surface wiring, creates `tea.Program` with alt screen.
- `surface/tui/model_test.go`: Comprehensive unit tests for update, view, wrapText, and viewport scrolling.
- `go.mod`: Uses `charmbracelet/bubbles`, `bubbletea`, `lipgloss`, `x/cellbuf`. No `glamour` yet.

The `Surface` interface (`surface/surface.go`) contract is unchanged — this is purely a rendering enhancement inside the TUI package.

## Architectural Blueprint

### Decision: Glamour for finalized turns only
**Evaluated paths:**
1. **Render every frame** — Pass `textStreamBuffer` through glamour in `View()`. Rejected: incomplete Markdown (e.g., unclosed code fences) produces broken or mis-styled output, and per-frame rendering would cause noticeable lag.
2. **Lightweight custom renderer** — Handle bold/italic/code with `lipgloss`. Rejected: the issue explicitly states "rich Markdown via glamour is the requirement" and "heavy dependencies are fine."
3. **Glamour for finalized turns + plain text for streaming** — Render assistant turns once at finalization, cache the ANSI output, and re-render on resize. Accepted: aligns with the issue's target experience and ideation decision.

### Design
- **`renderedTurn`** grows a `rendered string` field that holds the pre-rendered ANSI output (empty for non-assistant roles).
- **Markdown rendering** is encapsulated in a small helper (`renderMarkdown(text string, width int) (string, error)`) using `glamour.NewTermRenderer(glamour.WithAutoStyle(), glamour.WithWordWrap(...))`.
- **Error resilience**: if glamour fails, fall back to plain text so the UI never breaks.
- **`View()`** uses `wrapText()` for plain-text turns (user, tool, streaming), and a new `prefixLines()` helper for assistant turns that already contain glamour-wrapped ANSI output (to prepend the label without re-wrapping).
- **Window resize** (`tea.WindowSizeMsg`) triggers re-rendering of all assistant `renderedTurn`s so cached output respects the new terminal width.

## Requirements
1. Assistant text artifacts in finalized turns are rendered as Markdown via `charmbracelet/glamour`. [from issue]
2. Code blocks have syntax highlighting and distinct styling. [from issue]
3. Inline formatting (bold, italic, links, code spans) is rendered. [from issue]
4. Markdown output integrates cleanly with the scrollable Bubble Tea viewport. [from issue]
5. Markdown output respects terminal width and wrapping. [from issue]
6. Streaming/incomplete assistant text remains plain text. [inferred: incomplete Markdown breaks glamour output]
7. Performance is acceptable — renders once per turn, not per frame. [from issue]
8. User and tool turns remain plain text. [inferred: Markdown is only meaningful for assistant responses]
9. Window resize re-renders cached Markdown. [inferred: otherwise width is stale]

## Task Breakdown

### Task 1: Add glamour dependency
- **Goal**: Add `github.com/charmbracelet/glamour` to the module and verify the build.
- **Dependencies**: None.
- **Files Affected**: `go.mod`, `go.sum`.
- **New Files**: None.
- **Interfaces**: None.
- **Validation**:
  - `go mod tidy` exits cleanly.
  - `go build ./...` succeeds.
  - `go mod verify` passes.
- **Details**: Run `go get github.com/charmbracelet/glamour@latest`, then `go mod tidy`. Confirm glamour is listed in `go.mod`. This is a hermetic commit — no source code changes yet, just dependency resolution.

### Task 2: Create Markdown rendering utility
- **Goal**: Add a `renderMarkdown` helper and a `prefixLines` helper with unit tests.
- **Dependencies**: Task 1.
- **Files Affected**: `surface/tui/view.go` (add helpers), `surface/tui/model_test.go` (add tests).
- **New Files**: None.
- **Interfaces**:
  - `func renderMarkdown(text string, width int) (string, error)` — uses `glamour.NewTermRenderer(glamour.WithAutoStyle(), glamour.WithWordWrap(width))` then `r.Render(text)`.
  - `func prefixLines(text, label, indent string) string` — prepends `label` to the first line and `indent` to subsequent lines, without re-wrapping. Must be ANSI-aware (reuse `strings.Split` on `"\n"`, use `lipgloss.Width` for indent sizing).
- **Validation**:
  - `go test ./surface/tui/... -run TestRenderMarkdown` passes.
  - `go test ./surface/tui/... -run TestPrefixLines` passes.
  - `go test ./surface/tui/... -race` passes (all existing tests still pass).
- **Details**: In `view.go`, add `renderMarkdown` near the existing style vars. Add `prefixLines` next to `wrapText`. In `model_test.go`, add table-driven tests covering:
  - `renderMarkdown` with a simple code block produces ANSI sequences.
  - `renderMarkdown` error returns an error (mock by passing invalid width or triggering a glamour error if possible; otherwise test the fallback path in integration).
  - `prefixLines` with single-line and multi-line text, with and without ANSI sequences.

### Task 3: Integrate markdown rendering into model
- **Goal**: Modify `renderedTurn`, `Update` turnMsg handling, and window-size re-rendering.
- **Dependencies**: Task 2.
- **Files Affected**: `surface/tui/model.go`.
- **New Files**: None.
- **Interfaces**:
  - `renderedTurn` becomes:
    ```go
    type renderedTurn struct {
        role     state.Role
        text     string // original text (Markdown source for assistant)
        rendered string // pre-rendered ANSI output (only for assistant turns)
    }
    ```
  - `Update` `turnMsg` branch: after building `text.String()`, if `msg.turn.Role == state.RoleAssistant`, call `renderMarkdown(text.String(), m.viewport.Width)` and store in `rendered`.
  - `Update` `tea.WindowSizeMsg` branch: after resizing viewport, iterate `m.turns`; for each `role == state.RoleAssistant`, re-render `renderMarkdown(turn.text, m.viewport.Width)` and update `m.turns[i].rendered`.
- **Validation**:
  - `go test ./surface/tui/...` passes.
  - New tests verify:
    - `turnMsg` with `RoleAssistant` populates `rendered`.
    - `turnMsg` with `RoleUser` leaves `rendered` empty.
    - `WindowSizeMsg` updates `rendered` for assistant turns.
- **Details**: Keep the existing logic for resetting stream buffers and appending turns. Ensure the re-render loop does not mutate the slice itself (modify elements in-place via index). If `renderMarkdown` returns an error, leave `rendered` empty so `View()` falls back to plain text.

### Task 4: Update View to render assistant turns as Markdown
- **Goal**: In `View()`, use pre-rendered ANSI output for assistant turns instead of `wrapText`.
- **Dependencies**: Task 3.
- **Files Affected**: `surface/tui/view.go`.
- **New Files**: None.
- **Interfaces**: None.
- **Validation**:
  - `go test ./surface/tui/...` passes.
  - New tests verify:
    - `View()` for assistant turn with `rendered` set contains glamour ANSI sequences.
    - `View()` for assistant turn with empty `rendered` falls back to plain text.
    - `View()` for user/tool/streaming text still uses `wrapText`.
- **Details**: In the `View()` loop over `m.turns`, for `state.RoleAssistant`:
  ```go
  if turn.rendered != "" {
      b.WriteString(prefixLines(turn.rendered, assistantLabel, assistantIndent))
  } else {
      b.WriteString(wrapText(turn.text, assistantLabel, assistantIndent, width))
  }
  ```
  The streaming buffer (`m.textStreamBuffer`) continues to use `wrapText(..., assistantLabel, assistantIndent, width)` because it holds incomplete Markdown.

### Task 5: End-to-end validation
- **Goal**: Verify the complete feature works in the example application.
- **Dependencies**: Tasks 1–4.
- **Files Affected**: `examples/tui-chat/main.go` (review, possibly no changes needed).
- **New Files**: None.
- **Interfaces**: None.
- **Validation**:
  - `go test -race ./...` passes.
  - `go build ./...` succeeds.
  - Manual test: run `examples/tui-chat` and observe that a multi-line assistant response containing Markdown (e.g., `# Heading`, `**bold**`, code block) is rendered with styling.
- **Details**: The `examples/tui-chat/main.go` should not need code changes because the TUI surface is opaque. However, confirm the example still compiles and runs. If glamour introduces any runtime issues (e.g., style asset loading), diagnose and fix.

## Dependency Graph
- Task 1 → Task 2
- Task 2 → Task 3
- Task 3 → Task 4
- Task 4 → Task 5

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Glamour output exceeds terminal width due to internal padding/margins | Medium | Medium | Spike in Task 2: test actual glamour output width. If it exceeds configured width, subtract padding from the `WithWordWrap` value. |
| Glamour rendering is slow for long turns, causing lag on window resize | Medium | Low | Benchmark in Task 2. If slow, add a note in the code that resize re-render is batched; typical LLM turns are short enough. |
| Glamour fails to initialize or render (missing style assets, bad Markdown) | Low | Low | `renderMarkdown` returns error; caller in Task 3 leaves `rendered` empty so `View()` falls back to plain text. |
| Incomplete streaming Markdown is misread by users as broken rendering | Low | High | Addressed by design: streaming remains plain text. Add a code comment explaining this choice. |

## Validation Criteria
- [ ] `go test -race ./...` passes after all tasks.
- [ ] `go build ./...` succeeds after all tasks.
- [ ] Assistant turns in `model_test.go` verify `rendered` field contains ANSI sequences.
- [ ] Window resize tests verify assistant `rendered` is updated.
- [ ] View tests verify `prefixLines` is used for rendered assistant turns.
- [ ] Example `tui-chat` renders Markdown correctly in a manual test.
