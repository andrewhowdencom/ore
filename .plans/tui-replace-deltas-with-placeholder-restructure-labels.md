# Plan: TUI Replace Deltas with Pending Placeholder and Restructure Labels

## Objective
Refactor the ore TUI conduit (`conduit/tui/`) to stop consuming streaming delta artifacts and instead display a calm `"..."` placeholder while an assistant response is pending. Simultaneously restructure the conversation rendering so that all role labels (`You:`, `Assistant:`, `Thinking:`, `Tool:`) appear on their own line above their content, with content starting at column 0 and no indent padding. Factor the ad-hoc per-role wrapping/prefixing logic into a single generic block renderer.

## Context
The TUI is implemented in `conduit/tui/` with four source files:
- `tui.go`: Constructor and Bubble Tea program wiring. Subscribes to manager output stream for delta and turn events, routes them into the model via `deltaMsg` and `turnMsg`.
- `model.go`: Bubble Tea model state and `Update()` loop. Holds `streamBlocks []streamBlock` for accumulating streaming delta artifacts, and `turns []renderedTurn` for finalized conversation history.
- `view.go`: `View()` method and text-wrapping helpers (`wrapText`, `prefixLines`). Currently prefixes labels to the first content line and indents continuation lines.
- `markdown.go`: `markdownRenderer` interface and `glamourMarkdownRenderer` production implementation.

The issue accepts that `examples/tui-chat` composition remains unchanged (it only wires `tui.New`).

## Architectural Blueprint
There is only one viable path: remove delta-specific code paths, introduce a boolean `pending` flag in the model, replace `wrapText`/`prefixLines` with a generic `renderBlock` helper, and restructure `View()` to render label-then-content for every block. The subscription list in `tui.go` shrinks to `"turn_complete"` only. The model no longer mutates on `deltaMsg`; it sets `pending=true` on user submission and clears it on `RoleAssistant` turn receipt. `View()` renders an `Assistant: \n...` block when `pending` is true, replacing the previous `streamBlocks` rendering.

No Tree-of-Thought deliberation is needed because the issue provides an explicit, unambiguous design: remove streaming, add placeholder, move labels above content, factor renderer.

## Requirements
1. Remove `conduit.CapRenderDelta` from the TUI descriptor.
2. Stop subscribing to `"text_delta"`, `"reasoning_delta"`, `"tool_call_delta"` in `tui.go`.
3. Stop routing `artifact.Artifact` events into the Bubble Tea message loop.
4. Remove `deltaMsg` and `streamBlock` types from `model.go`; remove `streamBlocks` field.
5. Add `pending bool` to `model`; set on `KeyEnter` submission, clear on `RoleAssistant` `turnMsg`.
6. While `pending` is true, `View()` renders `"Assistant:"` label plus `"..."` content.
7. All role labels appear on their own line; content starts at column 0 with no indent padding.
8. Blank lines continue to separate turns; no blank line between label and content within a block.
9. Replace `wrapText` and `prefixLines` with `renderBlock(label, labelStyle, content, width)`.
10. `renderBlock` wraps plain text via `cellbuf.Wrap` when `width > 0`; passes pre-rendered content through unwrapped when `width == 0`.
11. All existing tests pass; `go test -race ./...` clean; `go build ./examples/tui-chat` succeeds.

## Task Breakdown

### Task 1: Remove delta streaming from TUI descriptor and event routing
- **Goal**: Stop advertising, subscribing to, and routing delta artifacts in the TUI constructor.
- **Dependencies**: None.
- **Files Affected**: `conduit/tui/tui.go`, `conduit/tui/tui_test.go`
- **New Files**: None.
- **Interfaces**: `Descriptor.Capabilities` loses `conduit.CapRenderDelta`. `mgr.Subscribe` loses delta kind arguments.
- **Validation**: `go test ./conduit/tui/...` passes.
- **Details**:
  1. In `tui.go`, remove `conduit.CapRenderDelta` from the `Descriptor.Capabilities` slice.
  2. In `tui.go`, remove `"text_delta"`, `"reasoning_delta"`, `"tool_call_delta"` from both `mgr.Subscribe` calls (the initial call and the retry-after-Attach call).
  3. In `tui.go`, delete the `artifact.Artifact` case inside the output event routing goroutine. The `loop.TurnCompleteEvent` and `loop.ErrorEvent` cases remain.
  4. In `tui_test.go`, remove `conduit.CapRenderDelta` from the expected capabilities list in `TestTUI_Capabilities`.
  5. In `tui_test.go`, remove the `{"render-delta", conduit.CapRenderDelta, true}` entry from the `TestTUI_Can` table.
  6. Run `go test ./conduit/tui/...` and confirm it passes. The model and view still contain dead delta/stream code, but it is no longer exercised in production; tests that exercise it directly still pass.

### Task 2: Add pending state, generic block renderer, and label-above-content layout
- **Goal**: Replace delta accumulation with a pending flag, factor wrapping logic into `renderBlock`, and restructure all labels to sit above content at column 0.
- **Dependencies**: Task 1.
- **Files Affected**: `conduit/tui/model.go`, `conduit/tui/view.go`, `conduit/tui/model_test.go`, `conduit/tui/view_test.go`
- **New Files**: None.
- **Interfaces**:
  - `renderBlock(label string, labelStyle lipgloss.Style, content string, width int) string` (replaces `wrapText` and `prefixLines`)
  - `model` loses `streamBlocks []streamBlock`, gains `pending bool`
  - `Update()` loses `deltaMsg` case; `turnMsg` case clears `pending` for assistant turns; `KeyEnter` case sets `pending = true`
- **Validation**: `go test ./conduit/tui/...` passes.
- **Details**:
  1. In `model.go`:
     - Delete the `deltaMsg` struct.
     - Delete the `streamBlock` struct.
     - Delete the `streamBlocks []streamBlock` field from `model`.
     - Add `pending bool` to `model`.
     - Remove the entire `case deltaMsg:` block from `Update()`.
     - In the `case tea.KeyMsg:` `KeyEnter` branch (when `!msg.Alt` and textarea is non-empty), after sending the `UserMessageEvent` to `eventsCh`, set `m.pending = true`.
     - In the `case turnMsg:` block, after appending the turn, clear `m.pending = false` **only when `msg.turn.Role == state.RoleAssistant`**. User and tool turns must not clear the flag.
     - Remove `m.streamBlocks = nil` from the `turnMsg` case (the field no longer exists).
  2. In `view.go`:
     - Delete `wrapText` and `prefixLines`.
     - Implement `renderBlock(label string, labelStyle lipgloss.Style, content string, width int) string`:
       - If `content == ""`, return `labelStyle.Render(label)` only.
       - If `width > 0`, wrap `content` with `cellbuf.Wrap(content, width, " ")`.
       - Return `labelStyle.Render(label) + "\n" + content`.
     - In `View()`, delete all `*_indent` variables (`userIndent`, `assistantIndent`, `toolIndent`, `thinkingIndent`).
     - Rewrite the turn-rendering loops to use `renderBlock` for every block:
       - User text: `renderBlock("You: ", lipgloss.NewStyle(), block.source, width)`
       - Assistant text with rendered cache: `renderBlock("Assistant: ", assistantStyle, block.rendered, 0)` (width `0` skips re-wrapping of pre-rendered ANSI content)
       - Assistant text fallback: `renderBlock("Assistant: ", assistantStyle, block.source, width)`
       - Thinking/reasoning: `renderBlock("Thinking: ", thinkingStyle, block.source, width)`
       - Tool text: `renderBlock("Tool: ", lipgloss.NewStyle(), block.source, width)`
     - Delete the entire `for _, block := range m.streamBlocks { ... }` streaming block rendering section.
     - After the `m.turns` loop and before the status line, add:
       ```go
       if m.pending {
           b.WriteString(renderBlock("Assistant: ", assistantStyle, "...", width))
           b.WriteString("\n\n")
       }
       ```
     - Preserve `\n\n` spacing between blocks within a turn, and `\n\n` after each turn.
  3. In `model_test.go`:
     - Remove all tests prefixed with `TestModel_Update_Delta_*`.
     - Remove `TestModel_Update_Delta_AutoScrollsViewport`.
     - Rename `TestModel_Update_Turn_ResetsStreamBuffer` to `TestModel_Update_Turn_ClearsPending` and assert `pending == false` instead of `streamBlocks` empty.
     - Add `TestModel_Update_KeyEnter_SetsPending` that sends `KeyEnter` with input and asserts `mm.pending == true`.
     - Add `TestModel_Update_Turn_Assistant_ClearsPending` that sends a `turnMsg` with `RoleAssistant` and asserts `mm.pending == false`.
     - Add `TestModel_Update_Turn_User_DoesNotClearPending` that sends a `turnMsg` with `RoleUser` after setting `pending = true` and asserts `mm.pending == true`.
  4. In `view_test.go`:
     - Remove all `TestPrefixLines_*` tests.
     - Remove all `TestWrapText_*` tests.
     - Remove `TestModel_View_StreamingText_PlainText`, `TestModel_View_StreamingReasoning`, and `TestModel_View_InterleavedStreaming`.
     - Update `TestModel_View_AssistantTurn_WithRendered` to assert that the rendered content appears on the line below `"Assistant: "`, not prefixed to the same line.
     - Update `TestModel_View_AssistantTurn_FallbackToPlainText` similarly.
     - Update `TestModel_View_ContainsTurn`, `TestModel_View_ContainsAssistantTurn`, `TestModel_View_ContainsToolTurn` to assert the label-above-content layout.
     - Add `TestRenderBlock_LabelAboveContent` that verifies `renderBlock("You: ", noStyle, "hello", 80)` produces `"You: \nhello"`.
     - Add `TestRenderBlock_WrapsContent` that passes a long string and narrow width, asserting the wrapped lines start at column 0 with no indent prefix.
     - Add `TestRenderBlock_StyledLabel` that passes a `lipgloss.Style` and asserts the label is styled.
     - Add `TestRenderBlock_EmptyContent` that passes empty content and asserts only the label is returned.
     - Add `TestRenderBlock_PreRenderedWidthZero` that passes width `0` and asserts `cellbuf.Wrap` is not called (content returned unchanged).
     - Add `TestModel_View_PendingPlaceholder` that sets `m.pending = true` and asserts the view contains `"Assistant: "` on one line and `"..."` on the next.
     - Update `TestModel_View_WrapsLongTurn` to verify wrapping without indent prefix.
  5. Run `go test ./conduit/tui/...` and fix any failures.

### Task 3: Full repository verification
- **Goal**: Confirm the entire codebase compiles and all tests pass with race detection.
- **Dependencies**: Task 2.
- **Files Affected**: None (verification only).
- **New Files**: None.
- **Interfaces**: None.
- **Validation**: `go test -race ./...` passes and `go build ./examples/tui-chat` succeeds.
- **Details**:
  1. Run `go test -race ./...` from the repository root.
  2. Run `go build ./examples/tui-chat`.
  3. If either fails, debug and fix. No code changes should be needed outside `conduit/tui/` if Tasks 1–2 were done correctly.

## Dependency Graph
- Task 1 → Task 2 → Task 3

## Risks & Mitigations
| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| `cellbuf.Wrap` with width=0 or negative behaves unexpectedly | Medium | Low | `renderBlock` guards with `width > 0` before calling `cellbuf.Wrap`; falls back to returning content unchanged |
| `mgr.Process()` fails before emitting assistant `TurnCompleteEvent`, leaving `pending=true` forever | Medium | Low | Note as known limitation; future work can add an error/clear-pending message from `tui.go` to the model |
| Pre-rendered glamour output contains lines wider than viewport after resize | Low | Low | `tea.WindowSizeMsg` handler already re-renders Markdown at new width before `View()` is called |
| `lipgloss.NewStyle()` as zero-value style in `renderBlock` might not render identically to raw string | Low | Low | Verify in tests that `lipgloss.NewStyle().Render("foo") == "foo"`; the library guarantees this |
| Removing `deltaMsg` and `streamBlocks` breaks tests outside `conduit/tui/` | Low | Low | Grep confirmed no external references; run `go test ./...` in Task 3 |

## Validation Criteria
- [ ] `conduit/tui/tui_test.go` passes and no longer asserts `CapRenderDelta`
- [ ] `conduit/tui/model_test.go` passes with new pending-state tests and no delta tests
- [ ] `conduit/tui/view_test.go` passes with new `renderBlock` tests and no `wrapText`/`prefixLines` tests
- [ ] `go test -race ./...` passes from repository root
- [ ] `go build ./examples/tui-chat` succeeds
- [ ] `examples/tui-chat/main.go` requires no changes (backward compatible composition)
