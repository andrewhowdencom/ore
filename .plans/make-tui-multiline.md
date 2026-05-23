# Plan: Make TUI Multi-line (Shift+Enter)

## Objective

Migrate the ore TUI conduit (`x/conduit/tui/`) from Bubble Tea v1 (v1.3.10) to Bubble Tea v2 (v2.0.6) to gain reliable Shift modifier detection on keyboard input, then enable `Shift+Enter` as the multi-line input shortcut (replacing the current `Alt+Enter` workaround). This is the technical mechanism; the user-visible outcome is intuitive multi-line chat input.

## Context

The TUI package lives at `x/conduit/tui/` with its own `go.mod`, making it a self-contained migration boundary. It currently depends on:

- `github.com/charmbracelet/bubbletea v1.3.10`
- `github.com/charmbracelet/bubbles v1.0.0`
- `github.com/charmbracelet/lipgloss v1.1.1-0.20250404203927-76690c660834`
- `github.com/charmbracelet/glamour v1.0.0`
- `github.com/charmbracelet/x/cellbuf v0.0.15`

The code explicitly documents: *"bubbletea v1.3.10 does not support Shift modifier detection"* and binds `Alt+Enter` for newline insertion via `ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("alt+enter"))`.

Bubble Tea v2 is published under vanity module paths (`charm.land/bubbletea/v2`, `charm.land/bubbles/v2`, `charm.land/lipgloss/v2`) with stable releases. v2 restructures key handling: `tea.KeyMsg` becomes an interface implemented by `tea.KeyPressMsg`/`tea.KeyReleaseMsg`; `Key` exposes `Mod KeyMod` with `.Contains()` for modifier detection (e.g., `msg.Key().Mod.Contains(tea.ModShift)`); and `Model.View()` returns `tea.View` (a struct wrapping a styled string) instead of `string`.

Bubbles v2 API surface (`textarea`, `viewport`, `key` packages) remains largely compatible with v1: same struct fields, same `New()`, `View() string`, `SetContent`, `GotoBottom`, `Value`, etc. The breakage is concentrated in the core Bubble Tea `tea` package.

The `examples/tui-chat` example only imports `github.com/andrewhowdencom/ore/x/conduit/tui` and does not directly import any `charmbracelet` packages, so it requires no changes.

## Architectural Blueprint

The migration is a contained, single-package dependency upgrade. No architectural patterns in ore change. The TUI conduit remains a "dumb pipe" per ore conventions — it translates external terminal events into ore session events and routes ore artifacts back to the terminal. The change is purely in the terminal framework version.

**Selected path:** Migrate the entire `x/conduit/tui/` package atomically to v2 (Task 1), then add the Shift+Enter binding on the migrated base (Task 2). Splitting the migration further is impossible because v1 and v2 `tea` types (Model, Msg, KeyMsg) cannot coexist in the same package.

**Alternative considered:** Wait for `github.com/charmbracelet/bubbletea` v1.x to gain shift detection — rejected because v1.3.10 is the latest published v1 release and the Charm team has moved active development to the `charm.land` vanity paths.

## Requirements

1. `x/conduit/tui/` compiles and tests pass with Bubble Tea v2.
2. The TUI behaves identically to pre-migration for all existing shortcuts (`Ctrl+O`, `Ctrl+C`, `Alt+Enter`, `PgUp`/`PgDown`, plain `Enter`).
3. `Shift+Enter` inserts a newline in the textarea input.
4. Plain `Enter` (without Shift) submits the message.
5. The README shortcut table reflects the new binding.
6. The v1 limitation comment is removed.
7. All tests run with `-race` and pass.

## Task Breakdown

### Task 1: Migrate TUI Package to Bubble Tea v2 with Equivalent Behavior
- **Goal**: Update `x/conduit/tui/` to compile, run, and pass all tests on Bubble Tea v2 with zero behavioral changes.
- **Dependencies**: None.
- **Files Affected**:
  - `x/conduit/tui/go.mod`
  - `x/conduit/tui/tui.go`
  - `x/conduit/tui/model.go`
  - `x/conduit/tui/view.go`
  - `x/conduit/tui/markdown.go`
  - `x/conduit/tui/model_test.go`
  - `x/conduit/tui/view_test.go`
  - `x/conduit/tui/tui_test.go`
- **New Files**: None.
- **Interfaces / API Changes**:
  - Import paths flip from `github.com/charmbracelet/...` to `charm.land/.../v2`:
    - `tea "charm.land/bubbletea/v2"`
    - `"charm.land/bubbles/v2/textarea"`
    - `"charm.land/bubbles/v2/viewport"`
    - `"charm.land/bubbles/v2/key"`
    - `"charm.land/lipgloss/v2"`
    - `glamour` may or may not have a v2 path; keep `github.com/charmbracelet/glamour` if no v2 vanity path exists. Run `go mod tidy` to resolve.
  - `model.View()` return type: `string` → `tea.View` (use `tea.NewView(...)` to wrap the final concatenated string).
  - `model.Update(msg tea.Msg)` type switch on `tea.KeyMsg` (struct) → `tea.KeyPressMsg` (concrete type implementing `tea.KeyMsg` interface). Use `msg.Key().Code` for special-key constants (`tea.KeyEnter`, `tea.KeyPgUp`, `tea.KeyPgDown`, `tea.KeyBackspace`, `tea.KeySpace`) and `msg.Key().Mod.Contains(tea.ModAlt)` / `tea.ModCtrl` for modifiers.
  - `tea.KeyRunes` constant likely does not exist in v2. Replace `tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}` synthesis with the v2 equivalent (construct a `tea.KeyPressMsg` with `Key{Text: " ", Code: ' '}` or use `tea.KeySpace` directly; verify exact constructor at build time).
  - `tea.KeyCtrlC` and `tea.KeyCtrlO` constants may not exist. Detect via `msg.String() == "ctrl+c"` / `"ctrl+o"` or via `msg.Key().Code == 'c' && msg.Key().Mod.Contains(tea.ModCtrl)`.
  - `tea.WithAltScreen()` option: verify existence in v2 `options.go`; if renamed or moved, update `tui.go` accordingly.
  - Remove the `// Note: bubbletea v1.3.10 does not support Shift modifier detection.` comment in `tui.go`.
  - `viewport.New(0, 0)` initialization in `model.go`: verify if v2 uses `viewport.New()` with `viewport.WithWidth/WithHeight` options instead of positional args.
  - `m.viewport.Update(msg)` and `m.textarea.Update(msg)` calls: these pass `tea.Msg` which is a type alias in v2 (`type Msg = uv.Event`), so they should remain compatible.
- **Validation**:
  - `cd x/conduit/tui && go mod tidy`
  - `go build ./...`
  - `go test -race ./...` passes (all existing tests must pass with mechanical adjustments to their key message constructions).

### Task 2: Enable Shift+Enter for Multi-line Input
- **Goal**: Bind `Shift+Enter` to newline insertion, keep plain `Enter` for message submission, and update documentation.
- **Dependencies**: Task 1.
- **Files Affected**:
  - `x/conduit/tui/tui.go`
  - `x/conduit/tui/model.go`
  - `x/conduit/tui/model_test.go`
  - `x/conduit/tui/README.md`
- **New Files**: None.
- **Interfaces / API Changes**:
  - In `tui.go`, change `ta.KeyMap.InsertNewline` binding from `"alt+enter"` to `"shift+enter"` (or add both bindings if the v2 `key.Binding` API supports multiple keys).
  - In `model.go` `Update()`, change the `tea.KeyEnter` handling branch:
    - If `msg.Key().Mod.Contains(tea.ModShift)` → pass to `textarea.Update()` for newline insertion (same as current `msg.Alt` path).
    - Else (plain Enter without Shift) → submit the message (same as current non-Alt path).
    - Remove the `!msg.Alt` check (v2 uses `Mod` on `Key`, not a boolean on `KeyMsg`).
  - Update `model_test.go`: replace `tea.KeyMsg{Type: tea.KeyEnter, Alt: true}` tests with `tea.KeyPressMsg{Key: tea.Key{Code: tea.KeyEnter, Mod: tea.ModShift}}` (or equivalent v2 constructor). Add a new test for plain `tea.KeyEnter` without Shift submitting, and `Shift+Enter` inserting a newline.
  - Update `README.md` shortcut table: replace `Alt+Enter` with `Shift+Enter` for "Insert newline in the input box".
- **Validation**:
  - `go test -race ./...` passes, including new/modified key event tests.
  - `go build ./...` passes.

## Dependency Graph

- Task 1 → Task 2 (Task 2 depends on Task 1)

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| `tea.WithAltScreen()` removed/renamed in v2 | Medium | Low | Inspect `charm.land/bubbletea/v2/options.go` at build time. Use the v2 equivalent option or `tea.WithoutRenderer()` + manual alt-screen if necessary. |
| `glamour` has no v2 vanity path and conflicts with lipgloss v2 styles | Medium | Low | glamour is only used in `markdown.go` for Markdown→ANSI rendering. It does not depend on Bubble Tea core. If style incompatibilities arise, keep glamour at v1 or pin a compatible version. Spikes in Task 1. |
| v2 `KeyPressMsg` construction in tests is syntactically different from v1 `KeyMsg` struct | Low | High | Mechanical refactor. The plan already flags this; tests need `tea.KeyPressMsg{Key: tea.Key{...}}` instead of `tea.KeyMsg{Type: ..., Runes: ..., Alt: ...}`. |
| `cellbuf.Wrap` from `charmbracelet/x/cellbuf` breaks with lipgloss v2 | Low | Low | `cellbuf` is a low-level utility. If it breaks, replace with lipgloss v2 width-wrapping or inline a simple wrap function. Spikes in Task 1 if build fails. |
| Terminal emulator does not send Shift+Enter escape sequence (e.g., basic `xterm` without kitty protocol) | Medium | Medium | v2 uses the Kitty Keyboard Protocol where available and falls back gracefully. If Shift+Enter is still not detected on a given terminal, users can fall back to plain Enter + manual newlines. Document this limitation. |

## Validation Criteria

- [ ] `cd x/conduit/tui && go mod tidy` resolves all dependencies without errors.
- [ ] `go build ./...` in `x/conduit/tui` succeeds.
- [ ] `go test -race ./...` in `x/conduit/tui` passes with no failures.
- [ ] The TUI example (`go run ./examples/tui-chat`) starts without panic.
- [ ] `Alt+Enter` is no longer mentioned in code comments or README as a limitation.
- [ ] `Shift+Enter` is documented in `README.md` as the newline shortcut.
