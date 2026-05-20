# Plan: Generate Conduit Options from Blueprint YAML

## Objective

Make `cmd/forge` translate per-conduit `options` maps declared in blueprint YAML into functional-option calls in generated `main.go`. Each conduit package gains a typed `Config` struct, a `FromConfig` helper, and an `OptionsFromMap(m map[string]any) ([]Option, error)` bridge that uses `mapstructure` with `yaml` tag support. Forge's template data and `main.go.tmpl` are updated to emit `OptionsFromMap` calls when a conduit declares options, while conduits without options continue to generate the existing bare `New(mgr)` call.

## Context

### Repository Topology

The project is a Go workspace with a root module (`github.com/andrewhowdencom/ore`) and four conduit submodules under `x/conduit/`:

- `x/conduit/http/` — HTTP server conduit (`WithAddr`, `WithoutUI`, `WithUI`)
- `x/conduit/tui/` — Bubble Tea TUI conduit (`WithThreadID`)
- `x/conduit/slack/` — Slack Socket Mode conduit (`WithBotToken`, `WithAppToken`, `WithEventsAPI`)
- `x/conduit/telegram/` — Telegram Bot API conduit (`WithBotToken`, `WithGetUpdatesTimeout`)

Each submodule has its own `go.mod` and `replace` directive pointing back to the root module.

### Current State

- `cmd/forge/blueprint.go` defines `ConduitConfig.Options map[string]any` but the field is never consumed by code generation.
- `cmd/forge/generate.go` `buildTemplateData` only captures `Index`, `ImportAlias`, and `ModulePath`.
- `cmd/forge/templates/main.go.tmpl` generates `cN, err := alias.New(mgr)` with no option spreading.
- All four conduits already use functional options internally, but none expose a `Config` struct, `FromConfig`, or `OptionsFromMap` helper.
- `mapstructure` is not a dependency anywhere in the project.
- Blueprint examples in `examples/forge/` declare bare conduits; `examples/forge/README.md` explicitly notes that `options` are "parsed and stored but not yet translated."

### Project Conventions (from `AGENTS.md`)

- Aggressive refactoring preferred; no backward-compatibility constraints.
- Conduit packages are leaf infrastructure (dumb pipes); they must not import `cognitive/` or manage turn loops.
- Table-driven tests with `go test -race ./...` are the standard.
- Functional-options pattern is already in use for all conduits.

### Issue-Driven Design

GitHub issue #128 pre-selected **Path B: typed config struct per conduit with a bridge to functional options**. Key design constraints from the issue:
- `Config` fields use `yaml` tags (not `mapstructure` tags).
- `OptionsFromMap` uses `mapstructure` but configures it to read `yaml` tags via `DecoderConfig{TagName: "yaml"}`.
- Forge emits bare `New(mgr)` when no options are present; it emits `OptionsFromMap` + variadic spread when options are present.
- Compile-time failure for bad types is acceptable.

## Architectural Blueprint

1. **Conduit Config Layer** — Each conduit package gains two new files:
   - `config.go`: exports `Config`, `FromConfig(cfg Config) []Option`, and `OptionsFromMap(m map[string]any) ([]Option, error)`.
   - `config_test.go`: table-driven tests for `OptionsFromMap` covering valid maps, invalid types, and integration with `New`.

2. **Forge Template Layer** — `cmd/forge/generate.go` gains a `formatGoMapStringAny` helper that recursively formats a `map[string]any` into a valid Go composite literal string (handling strings, bools, ints, float64s, nil, nested maps, and slices). `ConduitTemplateData` gains `HasOptions bool` and `OptionsLiteral string`. `buildTemplateData` populates these from the blueprint. `main.go.tmpl` conditionally emits the `OptionsFromMap` call.

3. **Forge Test Layer** — `cmd/forge/generate_test.go` gains test cases that assert generated code contains the `OptionsFromMap` call and valid map literals. `cmd/forge/build_test.go` gains a build-integration test that compiles generated code with options.

4. **Documentation Layer** — `examples/forge/http/forge.yaml` and `examples/forge/multi/forge.yaml` are updated to show options in action. `examples/forge/README.md` removes the "not yet translated" note and documents the new capability.

## Requirements

1. Each conduit in `x/conduit/` exports a `Config` struct with `yaml` tags matching the functional options it exposes.
2. Each conduit exports `FromConfig(cfg Config) []Option` that translates non-zero fields into their corresponding functional options.
3. Each conduit exports `OptionsFromMap(m map[string]any) ([]Option, error)` that bridges YAML-decoded maps to `[]Option` via `mapstructure` configured with `TagName: "yaml"`.
4. Forge's `GenerateMainGo` produces compilable code that calls `OptionsFromMap` when a blueprint conduit entry has a non-empty `options` map.
5. Forge continues to emit bare `New(mgr)` for conduits without options.
6. Generated code passes `parser.ParseFile` validation (already enforced by `GenerateMainGo`).
7. All new code has table-driven tests; `go test -race ./...` passes in every affected module.
8. Blueprint examples and README are updated to reflect the new capability.

## Task Breakdown

### Task 1: Implement Config Bridge for HTTP Conduit
- **Goal**: Add `Config`, `FromConfig`, and `OptionsFromMap` to `x/conduit/http/`.
- **Dependencies**: None.
- **Files Affected**: `x/conduit/http/go.mod`, `x/conduit/http/handler.go` (read-only reference), `x/conduit/http/handler_test.go` (read-only reference).
- **New Files**: `x/conduit/http/config.go`, `x/conduit/http/config_test.go`.
- **Interfaces**:
  - `type Config struct { Addr string \`yaml:"addr"\`; UI bool \`yaml:"ui"\` }`
  - `func FromConfig(cfg Config) []Option`
  - `func OptionsFromMap(m map[string]any) ([]Option, error)`
- **Validation**:
  - `cd x/conduit/http && go test -race ./...` passes.
- **Details**:
  - `config.go` imports `github.com/mitchellh/mapstructure`.
  - `FromConfig` calls `WithAddr` only when `cfg.Addr != ""`; calls `WithoutUI` only when `!cfg.UI`. Omitted/zero fields produce no options, preserving constructor defaults (`addr: ":7654"`, `withUI: true`).
  - `OptionsFromMap` creates a `mapstructure.DecoderConfig{TagName: "yaml", Result: &cfg}` and decodes the map.
  - `config_test.go` tests:
    - `OptionsFromMap` with `{"addr": ":0", "ui": false}` returns options that can be applied via `New` and verified by starting a server on `:0` and cancelling the context (reuse `TestNew_WithAddr` pattern).
    - `OptionsFromMap` with `{"addr": map[string]any{}}` returns a decode error.

### Task 2: Implement Config Bridge for TUI Conduit
- **Goal**: Add `Config`, `FromConfig`, and `OptionsFromMap` to `x/conduit/tui/`.
- **Dependencies**: None.
- **Files Affected**: `x/conduit/tui/go.mod`, `x/conduit/tui/tui.go` (read-only reference).
- **New Files**: `x/conduit/tui/config.go`, `x/conduit/tui/config_test.go`.
- **Interfaces**:
  - `type Config struct { ThreadID string \`yaml:"thread_id"\` }`
  - `func FromConfig(cfg Config) []Option`
  - `func OptionsFromMap(m map[string]any) ([]Option, error)`
- **Validation**:
  - `cd x/conduit/tui && go test -race ./...` passes.
- **Details**:
  - `FromConfig` calls `WithThreadID` only when `cfg.ThreadID != ""`.
  - `config_test.go` tests:
    - `OptionsFromMap` with `{"thread_id": "abc"}` returns options that can be passed to `New(mgr, opts...)` without error.
    - `OptionsFromMap` with invalid type returns error.

### Task 3: Implement Config Bridge for Slack Conduit
- **Goal**: Add `Config`, `FromConfig`, and `OptionsFromMap` to `x/conduit/slack/`.
- **Dependencies**: None.
- **Files Affected**: `x/conduit/slack/go.mod`, `x/conduit/slack/slack.go` (read-only reference).
- **New Files**: `x/conduit/slack/config.go`, `x/conduit/slack/config_test.go`.
- **Interfaces**:
  - `type Config struct { BotToken string \`yaml:"bot_token"\`; AppToken string \`yaml:"app_token"\`; EventsAPI bool \`yaml:"events_api"\` }`
  - `func FromConfig(cfg Config) []Option`
  - `func OptionsFromMap(m map[string]any) ([]Option, error)`
- **Validation**:
  - `cd x/conduit/slack && go test -race ./...` passes.
- **Details**:
  - `FromConfig` calls `WithBotToken`, `WithAppToken`, and `WithEventsAPI` when their fields are non-zero / true. Test-only injection options (`WithSlackClient`, `WithSocketModeClient`) are intentionally omitted from `Config`.
  - `config_test.go` tests `OptionsFromMap` with valid bot/app tokens and with invalid type maps.

### Task 4: Implement Config Bridge for Telegram Conduit
- **Goal**: Add `Config`, `FromConfig`, and `OptionsFromMap` to `x/conduit/telegram/`.
- **Dependencies**: None.
- **Files Affected**: `x/conduit/telegram/go.mod`, `x/conduit/telegram/telegram.go` (read-only reference).
- **New Files**: `x/conduit/telegram/config.go`, `x/conduit/telegram/config_test.go`.
- **Interfaces**:
  - `type Config struct { BotToken string \`yaml:"bot_token"\`; GetUpdatesTimeout int \`yaml:"get_updates_timeout"\` }`
  - `func FromConfig(cfg Config) []Option`
  - `func OptionsFromMap(m map[string]any) ([]Option, error)`
- **Validation**:
  - `cd x/conduit/telegram && go test -race ./...` passes.
- **Details**:
  - `FromConfig` calls `WithBotToken` when non-empty and `WithGetUpdatesTimeout` when non-zero. Test-only options (`WithHTTPClient`, `withBaseURL`) are omitted from `Config`.
  - `config_test.go` tests `OptionsFromMap` with valid and invalid inputs.

### Task 5: Update Forge Code Generation for Options
- **Goal**: Extend `cmd/forge` template data and `main.go.tmpl` to conditionally generate `OptionsFromMap` calls.
- **Dependencies**: Tasks 1–4 (conduits must export `OptionsFromMap` so generated code compiles; however, template logic can be written and tested independently since `parser.ParseFile` only validates syntax).
- **Files Affected**: `cmd/forge/generate.go`, `cmd/forge/templates/main.go.tmpl`.
- **New Files**: None.
- **Interfaces**:
  - `ConduitTemplateData` gains `HasOptions bool` and `OptionsLiteral string`.
  - `buildTemplateData` populates `HasOptions` (true when `len(c.Options) > 0`) and `OptionsLiteral` (via new `formatGoMapStringAny` helper).
  - `formatGoMapStringAny(m map[string]any) string` recursively formats map values into valid Go literals (strings quoted, bools/numbers bare, nil as `nil`, nested maps/slices handled deterministically with sorted keys).
- **Validation**:
  - `go test ./cmd/forge/...` passes from the repository root.
- **Details**:
  - In `generate.go`, add `sort` import (needed for deterministic map literal output). Add `formatGoMapStringAny` and a recursive `goValue` helper.
  - In `main.go.tmpl`, wrap each conduit instantiation in `{{if .HasOptions}}...{{else}}...{{end}}`:
    ```go
    {{if .HasOptions}}
    {{.ImportAlias}}OptsMap := {{.OptionsLiteral}}
    {{.ImportAlias}}Opts, err := {{.ImportAlias}}.OptionsFromMap({{.ImportAlias}}OptsMap)
    if err != nil {
        return err
    }
    c{{.Index}}, err := {{.ImportAlias}}.New(mgr, {{.ImportAlias}}Opts...)
    {{else}}
    c{{.Index}}, err := {{.ImportAlias}}.New(mgr)
    {{end}}
    ```
  - The existing `parser.ParseFile` call in `GenerateMainGo` will validate that generated syntax is valid Go.

### Task 6: Update Forge Tests for Options
- **Goal**: Add test coverage for generated code that includes conduit options.
- **Dependencies**: Task 5.
- **Files Affected**: `cmd/forge/generate_test.go`, `cmd/forge/build_test.go`.
- **New Files**: None.
- **Interfaces**: No new exported interfaces.
- **Validation**:
  - `go test ./cmd/forge/...` passes.
- **Details**:
  - In `generate_test.go`, add a test case (e.g., "http conduit with options") that provides a blueprint with `ConduitConfig{Module: ".../http", Options: map[string]any{"addr": ":8080", "ui": false}}` and asserts:
    - Generated code contains `httpc.OptionsFromMap(map[string]any{"addr": ":8080", "ui": false})`
    - Generated code contains `httpc.New(mgr, httpcOpts...)`
  - Add a test case for a multi-conduit blueprint where only one conduit has options, asserting the mixed pattern (bare `New` for one, options for the other).
  - In `build_test.go`, add a `Build` test case that builds a binary from a blueprint with HTTP options, proving the generated code compiles end-to-end.

### Task 7: Update Blueprint Examples and Documentation
- **Goal**: Demonstrate the new options capability in example blueprints and README.
- **Dependencies**: Tasks 1–6.
- **Files Affected**: `examples/forge/http/forge.yaml`, `examples/forge/multi/forge.yaml`, `examples/forge/README.md`.
- **New Files**: None.
- **Interfaces**: No new exported interfaces.
- **Validation**:
  - `cd examples/forge/http && go run ../../../cmd/forge build --config forge.yaml` succeeds and produces a binary.
  - `cd examples/forge/multi && go run ../../../cmd/forge build --config forge.yaml` succeeds.
- **Details**:
  - Update `examples/forge/http/forge.yaml` to include:
    ```yaml
    conduits:
      - module: github.com/andrewhowdencom/ore/x/conduit/http
        options:
          addr: ":8080"
    ```
  - Update `examples/forge/multi/forge.yaml` to include options on the HTTP conduit:
    ```yaml
    conduits:
      - module: github.com/andrewhowdencom/ore/x/conduit/http
        options:
          addr: ":8080"
      - module: github.com/andrewhowdencom/ore/x/conduit/tui
    ```
  - Update `examples/forge/README.md`:
    - Remove the "not yet translated" note under the Blueprint Format section.
    - Replace it with a working example showing `options` wiring.
    - Update the comparison tables: mark "Conduit options translation" as ✅ where applicable.

### Task 8: Full Validation and Cleanup
- **Goal**: Run the full test suite across all modules and verify no regressions.
- **Dependencies**: Tasks 1–7.
- **Files Affected**: None (read-only validation).
- **New Files**: None.
- **Validation**:
  - `go test -race ./...` passes in the root module.
  - `cd x/conduit/http && go test -race ./...` passes.
  - `cd x/conduit/tui && go test -race ./...` passes.
  - `cd x/conduit/slack && go test -race ./...` passes.
  - `cd x/conduit/telegram && go test -race ./...` passes.
  - `go mod tidy` in each submodule leaves no changes.
- **Details**:
  - If any `go.mod` files drifted during implementation (e.g., `mapstructure` was not added cleanly), run `go mod tidy` in each submodule and the root module, then commit any resulting `go.sum` updates.

## Dependency Graph

- Task 1 || Task 2 || Task 3 || Task 4 (all conduit config bridges are parallelizable)
- Task 5 → Task 6 (tests depend on template changes)
- Task 5 || Task 1–4 (template logic can be written while conduit bridges are in progress, but Task 6 build tests need Task 1–5 complete)
- Task 7 depends on Tasks 1–6
- Task 8 depends on Tasks 1–7

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| `mapstructure` does not decode `yaml`-tagged structs by default | High | Medium | Use `mapstructure.DecoderConfig{TagName: "yaml"}` explicitly in every `OptionsFromMap`. Verify with unit tests. |
| `formatGoMapStringAny` produces non-deterministic output due to map iteration order | Medium | High | Sort keys alphabetically before rendering pairs in `formatGoMapStringAny`; this ensures generated code is deterministic and test assertions are stable. |
| Generated `map[string]any` literal contains unrepresentable types (e.g., `[]byte`) | Low | Low | Document that blueprint options are scalar/bool/string/int only. `goValue` helper falls back to `%v` for unknown types; if an unrepresentable type is encountered, `parser.ParseFile` in `GenerateMainGo` will catch the syntax error. |
| Forge build test with options fails because submodule is not published | Medium | Low | The `Build` test runs in a temp module with `replace` directives pointing to local paths, so unpublished dependencies are fine. Ensure `go mod tidy` in the temp dir resolves `mapstructure`. |
| `yaml.v3` decodes numbers as `float64` instead of `int`, breaking `goValue` formatting | Low | Medium | `goValue` handles both `int` and `float64`. `mapstructure` will coerce `float64` to `int` during decode if the target field is `int`, so the end-to-end behavior is safe. |

## Validation Criteria

- [ ] All four conduits export `Config`, `FromConfig`, and `OptionsFromMap`.
- [ ] `OptionsFromMap` in each conduit correctly bridges `map[string]any` to `[]Option` using `mapstructure` with `TagName: "yaml"`.
- [ ] Forge generates compilable `main.go` that includes `OptionsFromMap` calls and map literals for conduits with options.
- [ ] Forge still generates bare `New(mgr)` for conduits without options.
- [ ] `go test -race ./...` passes in the root module and all four conduit submodules.
- [ ] Updated blueprint examples (`examples/forge/http/forge.yaml`, `examples/forge/multi/forge.yaml`) compile successfully via `cmd/forge build`.
- [ ] `examples/forge/README.md` no longer contains the "not yet translated" note and accurately documents the options feature.
