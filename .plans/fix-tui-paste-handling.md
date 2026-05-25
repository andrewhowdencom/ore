# Plan: Fix TUI Paste Handling

## Objective

Resolve GitHub issue #173 by adding `tea.PasteMsg` handling to the TUI conduit's Bubble Tea `model.Update` switch, so pasted text from bracketed-paste-capable terminals is forwarded into the textarea instead of being silently discarded.

## Context

- **File**: `x/conduit/tui/model.go` implements the Bubble Tea `model` with an `Update` switch handling `turnMsg`, `statusMsg`, `clearPendingMsg`, `tea.KeyPressMsg`, and `tea.WindowSizeMsg`.
- **Gap**: There is no `case tea.PasteMsg:` arm. Bubble Tea v2 sends `tea.PasteMsg` for bracketed paste events, but the message falls through to `return m, nil` and never reaches the textarea.
- **Fix pattern**: The existing default `tea.KeyPressMsg` handler already delegates to `m.textarea.Update(msg)` and recalculates layout. The same pattern applies to `tea.PasteMsg`.
- **Dependencies**: `charm.land/bubbletea/v2` and `charm.land/bubbles/v2` are already required in `x/conduit/tui/go.mod`.
- **Tests**: `x/conduit/tui/model_test.go` has extensive `newTestModel()`-based coverage for keyboard and window events; a paste test follows the same pattern.

## Architectural Blueprint

No architectural crossroads exist. The fix is a single missing switch case that follows the established delegation pattern to the `textarea.Model`. The `textarea` widget in Bubbles v2 natively handles `tea.PasteMsg`, so the implementation forwards the message and recalculates layout.

## Requirements

1. `model.Update` must handle `tea.PasteMsg` by delegating to `m.textarea.Update(msg)`.
2. After paste, `recalcLayout()` must run so the textarea grows/shrinks correctly.
3. A unit test must verify that sending `tea.PasteMsg{"hello world"}` results in the textarea containing `"hello world"`.

## Task Breakdown

### Task 1: Add `tea.PasteMsg` Handler to TUI Model Update
- **Goal**: Insert a `case tea.PasteMsg:` arm into `model.Update` that forwards the message to the textarea and recalculates layout.
- **Dependencies**: None.
- **Files Affected**: `x/conduit/tui/model.go`
- **New Files**: None.
- **Interfaces**: No new interfaces. The `Update` method signature is unchanged.
- **Validation**:
  - `go test ./x/conduit/tui/...` passes (no regressions).
  - `go test -race ./x/conduit/tui/...` passes.
- **Details**: Add the case immediately after the `tea.KeyPressMsg` block or alongside the other `tea.Msg` cases. The body should match the existing default keypress delegation:
  ```go
  case tea.PasteMsg:
      var cmd tea.Cmd
      m.textarea, cmd = m.textarea.Update(msg)
      m.recalcLayout()
      return m, cmd
  ```
  This leaves the repo in a valid, buildable state.

### Task 2: Add Unit Test for Paste Handling
- **Goal**: Verify that `tea.PasteMsg` inserts text into the textarea.
- **Dependencies**: Task 1.
- **Files Affected**: `x/conduit/tui/model_test.go`
- **New Files**: None.
- **Interfaces**: None.
- **Validation**:
  - `go test ./x/conduit/tui/...` passes.
  - `go test -race ./x/conduit/tui/...` passes.
- **Details**: Add a table-driven style test using `newTestModel()`:
  ```go
  func TestModel_Update_Paste(t *testing.T) {
      m := newTestModel()
      newM, _ := m.Update(tea.PasteMsg("pasted text"))
      mm := newM.(*model)
      assert.Equal(t, "pasted text", mm.textarea.Value())
  }
  ```
  This task leaves the repo fully tested and committable.

## Dependency Graph

- Task 1 → Task 2 (test depends on the handler existing)

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| `tea.PasteMsg` type signature differs in this Bubble Tea v2 patch | Low | Low | If `go test` fails to compile, inspect the actual `bubbletea/v2` `PasteMsg` definition and adjust the cast or method call. |
| Textarea widget does not handle `PasteMsg` natively in this Bubbles v2 version | Low | Low | Fall back to `m.textarea.InsertString(string(msg))` followed by `m.recalcLayout()`. |

## Validation Criteria

- [ ] `go test ./x/conduit/tui/...` passes after Task 1.
- [ ] `go test -race ./x/conduit/tui/...` passes after Task 2.
- [ ] The new `TestModel_Update_Paste` test asserts that `tea.PasteMsg("pasted text")` results in `textarea.Value() == "pasted text"`.
