# Plan: Add TUI Streaming Visual Feedback

## Objective

Implement visual feedback during active streaming in the `surface/tui` package so that users can see when the assistant is generating a response, and so that reasoning/thinking deltas are visually distinguished from final text output.

## Context

Issue [#8](https://github.com/andrewhowdencom/tack/issues/8) (“TUI: Add streaming visual feedback”) identifies that during streaming responses, text appears character-by-character with no visual cue that generation is in progress. Additionally, `artifact.TextDelta` and `artifact.ReasoningDelta` were handled identically in the Bubble Tea model — both were appended to a single `streamBuffer` and rendered with the same `Assistant:` label.

**Relevant files discovered:**

- `surface/tui/model.go` — Bubble Tea `tea.Model` implementation. `Update` handled `deltaMsg` by writing both `TextDelta` and `ReasoningDelta` into `m.streamBuffer`.
- `surface/tui/view.go` — Renders conversation history and the in-progress stream buffer with `assistantLabel` (subtle blue) for all assistant content. No animated or static indicator is appended.
- `surface/tui/model_test.go` — Table-driven tests covering delta handling, turn finalization, viewport scrolling, and view output. All streaming tests referenced the single `streamBuffer` field.
- `surface/tui/tui.go` — Thin wrapper; no changes needed.
- `artifact/artifact.go` — Defines `TextDelta` and `ReasoningDelta` as distinct artifact kinds.
- `go.mod` — Already depends on `github.com/charmbracelet/bubbles v1.0.0`, which includes `spinner` and `viewport` bubbles.

**Key observations:**

1. The `streamBuffer` field was split into `textStreamBuffer` and `reasoningStreamBuffer`.
2. A blinking cursor (`▌`) toggled via `tea.Tick` is the simplest, most robust indicator because it is a single-width character with no ANSI sequences, so it does not interact with `cellbuf.Wrap`.
3. Reasoning content should be rendered with a `Thinking:` label and a faint + italic `lipgloss.Style` (same family as the existing `statusStyle`).
4. The indicator must disappear when `turnMsg` is received (`RenderTurn`), which resets both stream buffers.
5. The OpenAI provider’s `InvokeStreaming` currently emits only `TextDelta` and `ToolCallDelta`; it does **not** emit `ReasoningDelta` in the stream. The TUI change is provider-agnostic — it will display reasoning deltas correctly when any provider emits them.

## Architectural Blueprint

**Selected approach:** Split the monolithic `streamBuffer` into two dedicated builders (`textStreamBuffer` and `reasoningStreamBuffer`), add a lightweight streaming-state flag and a `tea.Tick`-driven blinking cursor, and update `View` to render each buffer with its own label and style.

**Why not a `bubbles/spinner` bubble?** The spinner bubble is available, but its `View()` output contains ANSI style codes that complicate width calculations inside `cellbuf.Wrap`. A single-character blinking cursor is simpler, equally visible, and satisfies the acceptance criteria without risking wrap-edge flicker.

**Component interactions:**

```
+----------------------------------+
|  deltaMsg (TextDelta)            |
|  → m.textStreamBuffer.WriteString |
+----------------------------------+
            |
            v
+----------------------------------+
|  deltaMsg (ReasoningDelta)       |
|  → m.reasoningStreamBuffer.Write  |
+----------------------------------+
            |
            v
+----------------------------------+
|  cursorTickMsg (every 500 ms)    |
|  → toggle m.cursorVisible          |
+----------------------------------+
            |
            v
+----------------------------------+
|  turnMsg (RenderTurn)            |
|  → reset both buffers              |
|  → m.streaming = false             |
|  → m.cursorVisible = false       |
+----------------------------------+
            |
            v
+----------------------------------+
|  View()                            |
|  → wrap text buffer + cursor       |
|  → wrap reasoning buffer (faint)   |
+----------------------------------+
```

## Requirements

1. A visual indicator (blinking cursor `▌`) appears at the end of the active text stream while deltas are arriving.
2. `artifact.ReasoningDelta` content is accumulated in a separate buffer and rendered with a `Thinking:` label in faint/italic style.
3. The indicator disappears cleanly when `turnMsg` finalizes the turn.
4. The indicator must not interfere with `cellbuf.Wrap` text wrapping or viewport scrolling.
5. All existing tests in `surface/tui` continue to pass; new tests cover the blinking cursor and reasoning stream rendering.

## Task Breakdown

### Task 1: Split stream buffers and add blinking cursor indicator
- **Goal**: Separate `TextDelta` and `ReasoningDelta` into distinct model buffers, add streaming-state tracking, and implement a `tea.Tick`-driven blinking cursor that renders during active streaming.
- **Dependencies**: None.
- **Files Affected**:
  - `surface/tui/model.go`
  - `surface/tui/view.go`
  - `surface/tui/model_test.go`
- **New Files**: None.
- **Interfaces**:
  - `model` struct changes:
    - `streamBuffer` → `textStreamBuffer`
    - Add `reasoningStreamBuffer strings.Builder`
    - Add `streaming bool`
    - Add `cursorVisible bool`
  - New message type: `cursorTickMsg struct{}`
  - `model.Update` changes:
    - `deltaMsg` case: write to the correct buffer; set `m.streaming = true`; if buffers were empty before writing, return a `tea.Tick(500ms, …)` command to start blinking.
    - New `cursorTickMsg` case: if `m.streaming`, toggle `m.cursorVisible` and return the next tick command.
    - `turnMsg` case: reset both buffers; set `m.streaming = false`; set `m.cursorVisible = false`.
  - `model.View` changes:
    - Render `textStreamBuffer` with `assistantLabel` and, when `m.streaming && m.cursorVisible`, append `▌` after the wrapped text (post-wrap, on the last non-empty line, respecting terminal width).
    - Render `reasoningStreamBuffer` with a new `thinkingStyle` (faint + italic) and `Thinking:` label, applying the same cursor rule.
- **Validation**:
  - `go test -race ./surface/tui/…` passes.
  - `go vet ./…` is clean.
- **Details**:
  1. In `model.go`, rename `streamBuffer` to `textStreamBuffer`, add `reasoningStreamBuffer`, `streaming`, and `cursorVisible`.
  2. In the `deltaMsg` handler, type-switch on `artifact.TextDelta` (write to `textStreamBuffer`) and `artifact.ReasoningDelta` (write to `reasoningStreamBuffer`). Set `streaming = true`. If both buffers were empty before the write, return `tea.Tick(500ms, func(t time.Time) tea.Msg { return cursorTickMsg{} })` to start the blink loop.
  3. Add a new `case cursorTickMsg:` in `Update`. When `m.streaming`, toggle `m.cursorVisible` and return the next `tea.Tick` command. When not streaming, swallow the message and return `nil`.
  4. In the `turnMsg` handler, call `m.textStreamBuffer.Reset()`, `m.reasoningStreamBuffer.Reset()`, and set both `streaming` and `cursorVisible` to `false`.
  5. In `view.go`, add a package-level `thinkingStyle = lipgloss.NewStyle().Faint(true).Italic(true)`.
  6. Replace the single `if m.streamBuffer.Len() > 0` block with two blocks: one for `textStreamBuffer` (using `assistantLabel`) and one for `reasoningStreamBuffer` (using `thinkingStyle.Render("Thinking: ")`).
  7. For each active stream block, wrap the buffer text normally via `wrapText`, then post-process the result to append `▌` to the last non-empty line only when `m.streaming && m.cursorVisible`, ensuring `lipgloss.Width(lastLine)+1 <= width` or falling back to an indented continuation line.
  8. Update `model_test.go`:
     - Rename all `m.streamBuffer` references to `m.textStreamBuffer`.
     - `TestModel_Update_Delta_ReasoningDelta` should assert `m.reasoningStreamBuffer.String()`.
     - `TestModel_Update_Turn_ResetsStreamBuffer` should assert both buffers are empty.
     - Add `TestModel_Update_Delta_StartsBlinking` — assert that the first delta returns a non-nil `tea.Cmd`.
     - Add `TestModel_Update_CursorTickMsg_TogglesCursor` — send two ticks and assert `cursorVisible` toggles.
     - Add `TestModel_Update_Turn_StopsBlinking` — send a delta, send a turn, assert `streaming == false` and `cursorVisible == false`.
     - Add `TestModel_View_ContainsReasoningStream` — populate `reasoningStreamBuffer`, assert view contains `Thinking:` and reasoning text.
     - Add `TestModel_View_BlinkingCursorVisible` — set `streaming = true`, `cursorVisible = true`, populate `textStreamBuffer`, assert view contains `▌`.
     - Add `TestModel_View_BlinkingCursorHidden` — set `streaming = true`, `cursorVisible = false`, populate `textStreamBuffer`, assert view does **not** contain `▌`.
  9. Run `go test -race ./surface/tui/…` and fix any width-calculation or race issues.

## Dependency Graph

- Task 1 has no dependencies and is the only task (all changes are localized to the `surface/tui` package).

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| `▌` (U+258C) is not rendered correctly in some terminals | Medium | Low | Fallback to `_` (ASCII underscore) if width detection shows `lipgloss.Width("▌") != 1`; documented in code comment. |
| Appending cursor after `cellbuf.Wrap` still exceeds terminal width on very narrow terminals | Low | Medium | Clamp to `width - 1` before appending; if insufficient, place cursor on a new indented continuation line. |
| OpenAI provider does not emit `ReasoningDelta` in streaming mode, making the reasoning distinction hard to test end-to-end | Low | High | Add a unit test that manually sends `ReasoningDelta` to the model; the feature is provider-agnostic and will work when any provider emits reasoning deltas. |
| `tea.Tick` command returned from `Update` might leak if the user quits while streaming is active | Low | Low | Bubble Tea’s message loop stops processing on `tea.Quit`, so tick messages are naturally discarded. |

## Validation Criteria

- [ ] `go test -race ./surface/tui/…` passes with zero failures.
- [ ] `go vet ./…` reports no issues.
- [ ] `surface/tui/model.go` compiles with the new `textStreamBuffer`, `reasoningStreamBuffer`, `streaming`, and `cursorVisible` fields.
- [ ] `surface/tui/view.go` renders `textStreamBuffer` with `Assistant:` label, `reasoningStreamBuffer` with `Thinking:` label in faint/italic style, and appends a blinking `▌` cursor when `streaming && cursorVisible`.
- [ ] `turnMsg` resets both buffers and disables the cursor.
- [ ] The OpenAI provider is **not** modified in this plan; the TUI change is surface-only and provider-agnostic.
