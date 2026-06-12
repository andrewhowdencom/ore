# Plan: Consolidate TUI Theme System

## Objective

Replace the TUI's two unrelated style systems — lipgloss Go variables in `view.go` for chrome and embedded glamour JSON files in `styles/` for markdown body content — with a single `theme/` sub-package whose `Theme` struct owns both. The fix for the inter-message spacing bug (two blank lines instead of one) becomes a property of the theme rather than a separate defensive `TrimLeft` in `renderBlockUnified`. Two factories (`Dark()`, `Light()`) replace the runtime JSON auto-detection; auto-detect becomes a thin wrapper. The JSON files, the `//go:embed` machinery, and the `TrimLeft` defense are all deleted.

## Context

- `x/conduit/tui/view.go:20-40` defines ten package-level `lipgloss.NewStyle()` variables (`assistantStyle`, `userStyle`, `toolResultStyle`, `errorStyle`, `systemStyle`, `statusStyle`, `thinkingStyle`, `reasoningExpandedStyle`, `spinnerStyle`, `zoneLabelStyle`) with hardcoded hex colors. They are referenced 25 times inside `view.go`, 12 times inside `model.go`, 33 times in `view_test.go`, and 4 times in `model_test.go`.
- `x/conduit/tui/styles.go:13-17` embeds `x/conduit/tui/styles/dark.json` and `x/conduit/tui/styles/light.json` via `//go:embed` and exports them as `darkStyle` / `lightStyle` byte slices.
- `x/conduit/tui/markdown.go:42-58` selects one of those byte slices based on `term.IsTerminal` and `termenv.HasDarkBackground`, then passes it to `glamour.WithStylesFromJSONBytes`.
- `x/conduit/tui/view.go:95` does `return styledHeader + "\n" + strings.TrimLeft(body, "\n")`, with a comment explaining it is defending against glamour's `block_prefix: "\n"`. There is no symmetric `TrimRight` for glamour's `block_suffix: "\n"`.
- `x/conduit/tui/view.go:138-149` writes `"\n\n"` after every turn in `buildContent`. Combined with glamour's trailing `"\n"`, this produces three newlines (two blank lines) between messages rather than one.
- `glamour/ansi.StyleConfig` is a fully exported Go struct (`glamour@v1.0.0/ansi/style.go:98-141`) covering every field the JSON supports, and glamour already exports the reference implementations as `glamour/styles.DarkStyleConfig` and `glamour/styles.LightStyleConfig` (`glamour@v1.0.0/styles/styles.go:137, 347`). I diffed the embedded JSON against the upstream defaults; they are byte-identical except for `document.margin: 2 → 0`. So the theme can be constructed in Go with no JSON indirection, using the upstream configs as the starting point and overriding only `Document.Margin = 0`, `Document.BlockPrefix = ""`, `Document.BlockSuffix = ""`.
- The TUI is the only consumer of the JSON files and the package-level style vars; `examples/tui-chat/main.go` is the only external consumer of `tui.New(...)` and uses functional options (`WithName`, `WithThreadID`, etc.). Adding a new `WithTheme(...)` option is fully backward compatible.
- The project conventions (`AGENTS.md`) call for aggressive refactoring when the architecture has not stabilised; backwards compatibility is a liability. The TUI is a peripheral package (a conduit), so it may carry internal complexity provided the public surface stays clean.

## Architectural Blueprint

The TUI gets a new `theme/` sub-package with the following shape:

```
x/conduit/tui/
├── theme/
│   ├── theme.go    # Theme struct (GlamourStyle, AssistantStyle, UserStyle, …)
│   │               # StyleForRole(state.Role) lipgloss.Style
│   │               # Auto() *Theme — wraps terminal detection
│   ├── dark.go     # func Dark() *Theme — GlamourStyle starts from
│   │               #   glamour.DarkStyleConfig, sets Document.Margin=0,
│   │               #   Document.BlockPrefix="", Document.BlockSuffix="";
│   │               # lipgloss styles populated from named tokens
│   └── light.go    # func Light() *Theme — same shape, light palette
├── styles/         # DELETED — JSON files removed
├── styles.go       # DELETED — embed machinery removed
├── markdown.go     # glamourMarkdownRenderer takes *theme.Theme (not styleBytes)
│                   # renderMarkdown uses theme.GlamourStyle
├── view.go         # Package-level style vars REMOVED; every reference goes
│                   #   through m.theme.<Style>; TrimLeft defense REMOVED
├── model.go        # renderArtifact uses m.theme.StyleForRole(turn.Role)
├── tui.go          # TUI struct gains a *theme.Theme field; constructor
│                   #   gains WithTheme(...) option; default is theme.Auto()
└── …               # tests rewritten to access styles through the model
```

Key design decisions (recap of ideation, in case the reader has not been following):

- **Option B for glamour styling**: build the `ansi.StyleConfig` in Go using `glamour.DarkStyleConfig` / `LightStyleConfig` as the starting point. Delete the JSON files entirely. No `text/template` indirection, no `//go:embed`, no runtime JSON parsing.
- **Sub-package, not top-level**: `x/conduit/tui/theme/`. TUI-specific, no premature extraction.
- **Struct, not interface**: `Theme` is a value. Two factory functions return canonical instances. Tests construct themes by literal.
- **Role→style mapping as a method on Theme**: `theme.StyleForRole(role) lipgloss.Style`. The mapping is a theme concern, not a renderer concern.
- **Auto-detect becomes `theme.Auto()`**: wraps `term.IsTerminal` + `termenv.HasDarkBackground` and returns either `Dark()` or `Light()`. The TUI constructor calls this when no `WithTheme` is supplied.
- **Bug fix lives in the theme factories**: `dark.go` and `light.go` set `Document.BlockPrefix = ""` and `Document.BlockSuffix = ""`. The defensive `TrimLeft` in `renderBlockUnified` is deleted because the theme is now the single source of truth for "no extra whitespace around rendered markdown."

## Requirements

1. A single `theme.Theme` value must own both the glamour `StyleConfig` and every lipgloss style previously held in package-level variables.
2. `Theme` must expose `StyleForRole(state.Role) lipgloss.Style` so the role→style mapping is data-driven, not a switch statement in `renderArtifact`.
3. Two factory functions, `theme.Dark()` and `theme.Light()`, must return fully populated `*Theme` values. The current JSON files' glamour output must be reproduced exactly, except that `Document.Margin = 0`, `Document.BlockPrefix = ""`, and `Document.BlockSuffix = ""` (the spacing fix and the existing margin override).
4. `theme.Auto() *Theme` must wrap the existing terminal-detection logic (`term.IsTerminal` + `termenv.HasDarkBackground`) and return `Dark()` or `Light()` accordingly. The current behavior (non-terminal defaults to dark; terminal + dark background → dark; otherwise light) is preserved.
5. The TUI constructor must gain a `WithTheme(*theme.Theme) Option`. When omitted, `theme.Auto()` is used. The functional-option pattern matches the existing options (`WithName`, `WithThreadID`, `WithTracer`, etc.).
6. The glamour `StyleConfig` is constructed in Go, not loaded from JSON. The files `x/conduit/tui/styles/dark.json`, `x/conduit/tui/styles/light.json`, and the `//go:embed` machinery in `x/conduit/tui/styles.go` are all deleted.
7. The package-level lipgloss style variables in `view.go` are deleted. Every reference in `view.go`, `model.go`, `view_test.go`, and `model_test.go` is updated to access the style through the `Theme`.
8. The defensive `strings.TrimLeft(body, "\n")` in `renderBlockUnified` is removed. The theme's `Document.BlockPrefix = ""` is the only contract governing leading whitespace in rendered markdown.
9. The inter-message gap in `buildContent` must be exactly one blank line (one `\n` between the last body line and the next header), verified by a new regression test.
10. `go build ./...`, `go vet ./...`, `gofmt`, and `go test -race ./...` must all pass after every task.

## Task Breakdown

### Task 1: Scaffold the `theme/` sub-package

- **Goal**: Create `x/conduit/tui/theme/theme.go` with the `Theme` struct, a `StyleForRole(state.Role)` method, and an `Auto()` factory that selects dark/light. The factories `Dark()` and `Light()` exist as stubs returning zero-value themes. The package builds; the theme is unused.
- **Dependencies**: None.
- **Files Affected**: None.
- **New Files**:
  - `x/conduit/tui/theme/theme.go` — `Theme` struct, `StyleForRole`, `Auto`, stub `Dark` / `Light`.
- **Interfaces**:
  ```go
  package theme

  type Theme struct {
      GlamourStyle    ansi.StyleConfig
      AssistantStyle  lipgloss.Style
      UserStyle       lipgloss.Style
      ToolResultStyle lipgloss.Style
      ErrorStyle      lipgloss.Style
      SystemStyle     lipgloss.Style
      StatusStyle     lipgloss.Style
      ThinkingStyle   lipgloss.Style
      ReasoningExpandedStyle lipgloss.Style
      SpinnerStyle    lipgloss.Style
      ZoneLabelStyle  lipgloss.Style
  }

  func Dark() *Theme  { return &Theme{} }
  func Light() *Theme { return &Theme{} }
  func Auto() *Theme  { return Dark() }  // stub; replaced in Task 2 once the factories are real

  func (t *Theme) StyleForRole(role state.Role) lipgloss.Style {
      switch role {
      case state.RoleAssistant: return t.AssistantStyle
      case state.RoleUser:      return t.UserStyle
      case state.RoleTool:      return t.ToolResultStyle
      case state.RoleSystem:    return t.SystemStyle
      default:                  return t.AssistantStyle
      }
  }
  ```
- **Validation**: `go build ./x/conduit/tui/...` succeeds; the package is not yet imported by anything.
- **Details**: Keep the struct minimal — exactly the ten lipgloss styles currently in `view.go:20-40`, plus the glamour `StyleConfig` field. `Auto()` is a stub returning `Dark()` and will be replaced when `Dark()`/`Light()` are real in Task 2. `StyleForRole` covers the four roles currently handled by `renderArtifact`; the error-role fallback is mapped to assistant for now and refined in a later task when error styles are unified. This task is purely additive — no existing code is modified.

### Task 2: Implement `Dark()` and `Light()` factories

- **Goal**: Populate `GlamourStyle`, `AssistantStyle`, and the other nine lipgloss fields inside both factories, using `glamour.DarkStyleConfig` / `LightStyleConfig` as the starting point for the glamour style. `Document.Margin = 0`, `Document.BlockPrefix = ""`, `Document.BlockSuffix = ""` are set. `Auto()` is wired to the existing terminal-detection logic. The package builds; the theme is still unused by `view.go` / `model.go`.
- **Dependencies**: Task 1.
- **Files Affected**: `x/conduit/tui/theme/theme.go` (split into `theme/dark.go` and `theme/light.go` if the file grows past ~150 lines).
- **New Files**:
  - `x/conduit/tui/theme/dark.go` — `func Dark() *Theme` returns a fully populated dark theme.
  - `x/conduit/tui/theme/light.go` — `func Light() *Theme` returns a fully populated light theme.
  - `x/conduit/tui/theme/auto.go` (optional) — `func Auto() *Theme` wraps the existing detection.
- **Interfaces**:
  ```go
  package theme

  import (
      "os"
      glamouransi "github.com/charmbracelet/glamour/ansi"
      "github.com/charmbracelet/glamour/styles"
      "github.com/charmbracelet/lipgloss/v2"
      "github.com/muesli/termenv"
      "golang.org/x/term"
  )

  // darkThemeBase returns the upstream glamour DarkStyleConfig with the
  // ore-specific overrides applied: document.margin = 0 and the leading/
  // trailing newlines stripped (the spacing-bug fix).
  func darkThemeBase() glamouransi.StyleConfig {
      base := styles.DarkStyleConfig
      zero, empty := uint(0), ""
      base.Document.Margin = &zero
      base.Document.BlockPrefix = &empty
      base.Document.BlockSuffix = &empty
      return base
  }

  func Dark() *Theme {
      return &Theme{
          GlamourStyle:           darkThemeBase(),
          AssistantStyle:         lipgloss.NewStyle().Foreground(lipgloss.Color("#6C8EBF")),
          UserStyle:              lipgloss.NewStyle().Foreground(lipgloss.Color("#E5C07B")),
          ToolResultStyle:        lipgloss.NewStyle().Foreground(lipgloss.Color("#98C379")),
          ErrorStyle:             lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5555")),
          SystemStyle:            lipgloss.NewStyle().Foreground(lipgloss.Color("#C678DD")),
          StatusStyle:            lipgloss.NewStyle().Faint(true).Italic(true),
          ThinkingStyle:          lipgloss.NewStyle().Faint(true).Italic(true),
          ReasoningExpandedStyle: lipgloss.NewStyle().Faint(true),
          SpinnerStyle:           lipgloss.NewStyle().Faint(true).Italic(true),
          ZoneLabelStyle:         lipgloss.NewStyle().Bold(true),
      }
  }
  ```
  `Light()` mirrors this with `styles.LightStyleConfig` and the same hex values (or a parallel light palette if the user wants the option of divergent light-mode colors — flag for the builder; the simplest path is identical hex values since the existing lipgloss vars do not vary by theme).
- **Validation**: `go build ./x/conduit/tui/...` succeeds. A trivial `theme_test.go` asserts that `darkThemeBase().Document.Margin != nil && *darkThemeBase().Document.Margin == 0` and the same for the block-prefix/suffix empty strings. The factories are not yet wired into any production code.
- **Details**: Splitting the file is up to the builder; the threshold mentioned in `AGENTS.md` (~150 lines) is a soft signal. The hex values must be byte-identical to the current `view.go:20-40` definitions so the visual output is unchanged. `Auto()` should use the *exact* same detection logic as `markdown.go:42-51` (currently `term.IsTerminal(int(os.Stdout.Fd()))` and `termenv.HasDarkBackground`) so behaviour is preserved.

### Task 3: Wire the theme into the markdown renderer

- **Goal**: Replace `glamourMarkdownRenderer.styleBytes []byte` with a `*theme.Theme` field. `renderMarkdown` calls `glamour.WithStyles(md.theme.GlamourStyle)` instead of `glamour.WithStylesFromJSONBytes(r.styleBytes)`. The JSON files are still embedded (still needed by `TestEmbeddedStyles_MarginZero` and the `TestRenderer_Selects*` tests, which are rewritten in this task).
- **Dependencies**: Task 2.
- **Files Affected**:
  - `x/conduit/tui/markdown.go` — `glamourMarkdownRenderer` gains a `theme *theme.Theme` field; `newGlamourMarkdownRenderer` becomes `newGlamourMarkdownRenderer(theme *theme.Theme)`; `Render` calls `glamour.WithStyles(r.theme.GlamourStyle)`. The internal `isTerminal` / `hasDarkBackground` detectors are no longer needed here (they live in `theme.Auto()`).
  - `x/conduit/tui/model.go` — `model.md` field changes from `markdownRenderer` (no theme) to a `markdownRenderer` constructed with a theme; `m.renderMarkdown` uses the theme's `GlamourStyle`. The `model` struct gains a `theme *theme.Theme` field.
  - `x/conduit/tui/view_test.go` — `TestRenderer_SelectsDarkStyle`, `TestRenderer_SelectsLightStyle`, `TestRenderer_SelectsNoTTY` are rewritten to assert against the theme's `GlamourStyle.Document` field (margin, block_prefix, block_suffix) and the right `Theme` instance (dark vs light) rather than byte slices. `TestEmbeddedStyles_MarginZero` is rewritten to assert against `theme.Dark().GlamourStyle.Document.Margin` and the same for light; the JSON unmarshal is replaced with direct field access.
- **New Files**: None.
- **Interfaces**:
  ```go
  // markdown.go
  type glamourMarkdownRenderer struct {
      theme *theme.Theme
  }

  func newGlamourMarkdownRenderer(th *theme.Theme) *glamourMarkdownRenderer {
      return &glamourMarkdownRenderer{theme: th}
  }

  func (r glamourMarkdownRenderer) Render(text string, width int) (string, error) {
      rnd, err := glamour.NewTermRenderer(
          glamour.WithStyles(r.theme.GlamourStyle),
          glamour.WithWordWrap(width),
      )
      if err != nil {
          return "", err
      }
      return rnd.Render(text)
  }
  ```
- **Validation**: `go build ./x/conduit/tui/...` and `go test ./x/conduit/tui/...` pass. The `TestRenderer_Selects*` tests are rewritten before they break (i.e. the rewrite is in this same commit, not a follow-up).
- **Details**: `model` gains a `theme *theme.Theme` field. `newTestModel()` in the tests constructs a model with `theme: theme.Dark()` as a default; existing test assertions that exercise `m.md.Render(...)` continue to work because the renderer's `Render` signature is unchanged. The `glamour` import in `markdown.go` is now used for `NewTermRenderer` plus `WithStyles`; the `os` and `term` imports are removed (the detector logic moved to `theme.Auto()`).

### Task 4: Remove package-level lipgloss style vars and update all references

- **Goal**: Delete the ten `lipgloss.NewStyle()` package-level variables in `view.go:20-40`. Replace every reference in `view.go` and `model.go` with the corresponding field on `m.theme` / `mm.theme`. Update the test files (`view_test.go` ~33 references, `model_test.go` ~4 references) to access styles through the model's `theme` field.
- **Dependencies**: Task 3.
- **Files Affected**:
  - `x/conduit/tui/view.go` — delete the `var (...)` block at lines 20-40; replace every `assistantStyle` etc. with `m.theme.AssistantStyle` (or the appropriate field).
  - `x/conduit/tui/model.go` — same substitution inside `renderArtifact`, `renderPlainBlock`, and any other function that uses the package-level vars.
  - `x/conduit/tui/view_test.go` — every literal `style: assistantStyle` becomes `style: mm.theme.AssistantStyle` (or whatever the test setup uses). The `newTestModel` helper is updated to inject `theme.Dark()` so the `AssistantStyle` etc. fields are populated. Specifically, the references at `view_test.go:52, 71, 90, 91, 112, 113, 176, 177, 192, 193, 210, 211, 214, 215, 392, 643, 659, 664, 680, 681, 687, 688, 723, 724, 731, 775` are touched.
  - `x/conduit/tui/model_test.go` — the references at lines 126, 305, 1154, 1159 are touched.
- **New Files**: None.
- **Interfaces**: No new public types; the lipgloss style vars go from `package tui` to `*theme.Theme`.
- **Validation**: `go build ./...`, `go vet ./...`, and `go test -race ./...` all pass.
- **Details**: This is a large but mechanical change. The builder should grep for each removed identifier to ensure no stragglers remain. `gofmt -w ./x/conduit/tui` should produce no output. The test files' `newTestModel` helper becomes the injection point for the theme, so production code does not need any new wiring in this task — the `*Theme` field on `model` is set in `initModel` (in Task 5) and tests set it directly in their helper.

### Task 5: Wire `WithTheme(...)` into the TUI constructor

- **Goal**: Add a `*theme.Theme` field to the `TUI` struct in `tui.go`, a `WithTheme(*theme.Theme) Option` functional option, and pass the configured theme into `initModel`. The default is `theme.Auto()`. The `examples/tui-chat/main.go` consumer continues to work without changes (the default kicks in).
- **Dependencies**: Task 4.
- **Files Affected**:
  - `x/conduit/tui/tui.go` — `TUI` struct gains a `theme *theme.Theme` field; new `WithTheme` option; `initModel` passes `t.theme` (or `theme.Auto()` if nil) into the model.
  - `x/conduit/tui/model.go` — `initModel` no longer needs to call `newGlamourMarkdownRenderer()` without arguments; it calls `newGlamourMarkdownRenderer(m.theme)`.
- **New Files**: None.
- **Interfaces**:
  ```go
  // tui.go
  type TUI struct {
      // … existing fields …
      theme *theme.Theme
  }

  // WithTheme configures a custom theme for the TUI. When omitted,
  // theme.Auto() is used (dark/light selected from the terminal).
  func WithTheme(th *theme.Theme) Option {
      return func(t *TUI) { t.theme = th }
  }

  // In initModel:
  if t.theme == nil {
      t.theme = theme.Auto()
  }
  m := model{ /* … */, theme: t.theme }
  ```
- **Validation**: `go build ./...`, `go vet ./...`, `go test -race ./x/conduit/tui/...` pass. `examples/tui-chat/main.go` continues to build (`go build ./examples/tui-chat/...`).
- **Details**: The default is computed in `initModel`, not in `New`, to keep `New` cheap. `theme.Auto()` is cheap itself (it just calls `term.IsTerminal` and `termenv.HasDarkBackground`); computing it eagerly in `New` would be acceptable too — the choice is the builder's, but the spec is "default in `initModel` is fine, default in `New` is also fine."

### Task 6: Add a regression test for the spacing fix

- **Goal**: Assert that `buildContent` produces exactly one blank line between two assistant text blocks (not two). The test must exercise the real theme — `theme.Dark()` and a real `glamourMarkdownRenderer` — to catch the regression. This is the test that would have caught the original bug.
- **Dependencies**: Task 5.
- **Files Affected**: `x/conduit/tui/view_test.go`.
- **New Files**: None.
- **Interfaces**: None.
- **Validation**: The new test passes after Task 4 (current state, with the bug) and continues to pass after Tasks 7-9 (after the fix). The test should be authored in this task and intentionally fail against the current buggy code, then pass once the TrimLeft removal lands. If the builder prefers a green-first approach, they can author the test in this task without running it, then re-run after Task 7.
- **Details**: Construct a model with `theme: theme.Dark()`, populate two assistant turns with simple text blocks (e.g. `"hello"` and `"world"`), call `m.buildContent()`, and assert that the string contains exactly `"\n\n"` between the end of the first block's body and the second block's header, not `"\n\n\n"`. Suggested assertion: `assert.Equal(t, 1, strings.Count(output, "\n\n"))` between two blocks, or use a regex to count the exact pattern. The test must use a real (non-mock) markdown renderer — that is the only way the test catches the bug. Place it next to the other `buildContent` tests in `view_test.go`.

### Task 7: Remove the `TrimLeft` defense in `renderBlockUnified`

- **Goal**: Delete the `strings.TrimLeft(body, "\n")` and its three-line explanatory comment from `view.go:93-95`. The `Theme` is now the single source of truth for `Document.BlockPrefix = ""`, so the defense is dead code. The line simplifies to `return styledHeader + "\n" + body`.
- **Dependencies**: Task 6.
- **Files Affected**: `x/conduit/tui/view.go`.
- **New Files**: None.
- **Interfaces**: No signature change.
- **Validation**: `go build ./...`, `go vet ./...`, `go test -race ./x/conduit/tui/...` pass. The new regression test from Task 6 must pass against the new code. The existing `TestRenderBlockUnified_NoDoubleNewline` tests around `view_test.go:307-321` and `:371` must continue to pass (they assert exactly what this task enforces).
- **Details**: The change is three lines of code plus a removed three-line comment. The new line is `return styledHeader + "\n" + body`. `strings` is still imported (it is used elsewhere in the file).

### Task 8: Delete the JSON files and the embed machinery

- **Goal**: Remove `x/conduit/tui/styles/dark.json`, `x/conduit/tui/styles/light.json`, and the entire `x/conduit/tui/styles.go` file. The glamour rendering is now entirely in Go via `theme.GlamourStyle`. The test file `TestEmbeddedStyles_MarginZero` was already rewritten in Task 3 to assert against the Go theme.
- **Dependencies**: Task 7.
- **Files Affected**:
  - `x/conduit/tui/styles.go` — deleted.
  - `x/conduit/tui/styles/dark.json` — deleted.
  - `x/conduit/tui/styles/light.json` — deleted.
- **New Files**: None.
- **Interfaces**: None.
- **Validation**: `go build ./...` succeeds (no other file imports the embed package or the JSON files; the `//go:embed` directive was the only consumer). `go test -race ./...` passes. `git grep darkStyle lightStyle styles/dark.json styles/light.json` returns zero matches.
- **Details**: Use `git rm` to preserve history. Verify no test or production file still references the removed names by grepping for `darkStyle`, `lightStyle`, `styles/dark`, `styles/light`. The `//go:embed` import `_ "embed"` in `styles.go` was the only consumer; no other file uses it.

### Task 9: Final validation

- **Goal**: Run the full Go toolchain against the TUI conduit and the example consumer. Confirm the repository is in a clean, committable state.
- **Dependencies**: Task 8.
- **Files Affected**: None (validation only).
- **New Files**: None.
- **Interfaces**: None.
- **Validation** (run from the repository root):
  - `go build ./...` exits 0.
  - `go vet ./...` exits 0.
  - `gofmt -l ./x/conduit/tui/...` produces no output.
  - `go test -race ./...` exits 0.
  - `go test ./examples/tui-chat/...` exits 0.
  - `git grep -n "darkStyle\|lightStyle\|styles/dark\.json\|styles/light\.json" -- 'x/' 'examples/' 'cmd/'` returns zero matches.
- **Details**: If any check fails, fix the smallest possible thing and re-run the whole sequence. Do not amend previous tasks' commits to fix new failures — land a Task-9 fixup commit if needed. The plan is complete when all six checks pass and the diff is reviewable as a single "consolidate TUI theme" change spanning nine commits.

## Dependency Graph

- Task 1 → Task 2 (Task 2 implements the factories scaffolded in Task 1)
- Task 2 → Task 3 (Task 3 needs real factories to construct a real `glamourMarkdownRenderer`)
- Task 3 → Task 4 (Task 4 removes the package-level lipgloss vars; tests that reference them must already be wired to `m.theme` via the model set up in Task 3)
- Task 4 → Task 5 (Task 5 adds the public `WithTheme` option; it requires the `*theme.Theme` field on the model that Task 4 finalises)
- Task 5 → Task 6 (Task 6's regression test needs the full theme + renderer + model wiring)
- Task 6 → Task 7 (Task 7's removal is verified by the test from Task 6)
- Task 7 → Task 8 (Task 8 deletes the JSON files; nothing may depend on them)
- Task 8 → Task 9 (Task 9 is the final validation)

All tasks are sequential. No parallelism is safe because every task builds on the same code surface (the `model` and `view` packages) and the Go compiler enforces ordering through the symbol table.

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| `StyleForRole` does not cover the "error" role the way the existing inline switch in `renderArtifact` does | Medium | Medium | The builder should diff the existing switch (`model.go:316-360`) against the new `StyleForRole` and ensure all four roles are covered with the same mapping. Add a unit test for `StyleForRole` covering assistant, user, tool, system, and a hypothetical error role. |
| `glamour.DarkStyleConfig` / `LightStyleConfig` are large structs; copying them by value into the theme wastes a few hundred bytes | Low | Low | Use a pointer or a sync.Once-init pattern if profiling shows it matters. The structs are < 4KB each; the duplication is not a real concern. |
| `view_test.go` has 33 references to package-level style vars; the mechanical rewrite is error-prone | Medium | High | Run `gofmt -w ./x/conduit/tui/...` and `go vet ./...` after the rewrite. Use a single `gofmt` + `go vet` cycle per file edit, not per character. The tests themselves exercise the same code paths as production, so any missed reference will show up as a test failure or a vet warning. |
| The new regression test (Task 6) does not catch the bug because the builder accidentally uses a mock renderer | High | Low | The test description explicitly mandates a real (non-mock) `glamourMarkdownRenderer` constructed with `theme.Dark()`. Reviewer should verify the test does not import or use `mockMarkdownRenderer`. |
| `theme.Auto()` detection logic does not match the existing `markdown.go:42-51` behavior (e.g. no-TTY path, dark/light split) | Medium | Medium | Copy the detection logic byte-for-byte. Add a unit test for `theme.Auto()` that runs the detectors with known inputs and asserts the right `*Theme` is returned. |
| The `//go:embed` machinery or the `darkStyle` / `lightStyle` package vars are referenced by a file the plan did not enumerate (e.g. an example, a test helper) | Medium | Low | Task 9 includes a `git grep` check for `darkStyle`, `lightStyle`, `styles/dark.json`, `styles/light.json` across the whole repo. The check is the safety net. |
| The 13-line `//go:embed` styles file is left orphaned because the builder forgets to `git rm` it | Low | Medium | The deletion is a single `git rm` per file; the build and `git grep` checks in Task 9 will surface it. |

## Validation Criteria

- [ ] `x/conduit/tui/theme/theme.go` (and the optional `dark.go`, `light.go`, `auto.go`) exist and contain the `Theme` struct, `StyleForRole`, `Dark`, `Light`, `Auto`.
- [ ] `theme.Dark().GlamourStyle.Document.Margin` is `*0` (i.e. not nil, points to 0).
- [ ] `theme.Dark().GlamourStyle.Document.BlockPrefix` is `*""` (i.e. not nil, points to empty string).
- [ ] `theme.Dark().GlamourStyle.Document.BlockSuffix` is `*""`.
- [ ] Same three checks pass for `theme.Light()`.
- [ ] `theme.Auto()` returns `theme.Dark()` when the terminal is non-TTY or has a dark background; `theme.Light()` otherwise.
- [ ] `x/conduit/tui/markdown.go` uses `glamour.WithStyles(theme.GlamourStyle)`, not `glamour.WithStylesFromJSONBytes`.
- [ ] `x/conduit/tui/view.go` defines no package-level `lipgloss.NewStyle()` variables.
- [ ] `x/conduit/tui/view.go:renderBlockUnified` does not call `strings.TrimLeft` on the body.
- [ ] `x/conduit/tui/tui.go` exposes `WithTheme(*theme.Theme) Option` and the `TUI` struct holds a `*theme.Theme` field.
- [ ] `x/conduit/tui/styles/dark.json`, `x/conduit/tui/styles/light.json`, and `x/conduit/tui/styles.go` no longer exist (verified by `git ls-files`).
- [ ] The new regression test in Task 6 passes.
- [ ] `go build ./...` exits 0.
- [ ] `go vet ./...` exits 0.
- [ ] `gofmt -l ./x/conduit/tui/...` produces no output.
- [ ] `go test -race ./...` passes with zero failures.
- [ ] `examples/tui-chat/main.go` builds and its tests pass.
