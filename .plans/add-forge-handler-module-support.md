# Plan: Add Generic Handler Module Support to Forge Blueprints

## Objective

Enable `cmd/forge` blueprints to declare `loop.Handler` modules (e.g. tool handlers) that are instantiated per-stream and wired into the generated agent application. This requires a breaking but mechanical change to `session.Manager` to allow the step factory to return errors, followed by blueprint schema, template generation, build pipeline, and documentation updates.

## Context

### Repository Topology

The ore framework follows a cycle-free dependency graph:

```
artifact/ ← leaf
state/    ← depends on artifact/
provider/ ← depends on state/, artifact/
loop/     ← depends on artifact/, state/, provider/
core/     ← depends on artifact/, state/, provider/, loop/
session/  ← depends on loop/, provider/, state/, thread/
```

Key packages and files relevant to this plan:

- **`session/manager.go`** — `Manager` struct with `newStep func() *loop.Step` field. `Create()` and `Attach()` call the factory once per stream. This is the core blocker: the factory cannot propagate errors.
- **`loop/loop.go`** — `Step` struct, `New(opts ...Option) *Step`, `WithHandlers(handlers ...Handler) Option`. The `Option` pattern is already in place for wiring handlers.
- **`loop/handler.go`** — `Handler` interface: `Handle(ctx context.Context, art artifact.Artifact, s state.State) error`.
- **`cmd/forge/blueprint.go`** — `Blueprint`, `Dist`, `ConduitConfig` structs. `ParseBlueprint` validates required fields.
- **`cmd/forge/generate.go`** — `GenerateMainGo`, `buildTemplateData`, `deriveImportAlias`, `formatGoMapStringAny`. Embeds `templates/main.go.tmpl`.
- **`cmd/forge/build.go`** — `Build` generates a temp module, runs `go mod tidy`, compiles.
- **`cmd/forge/templates/main.go.tmpl`** — The Go template that generates agent `main.go`. Currently hardcodes `stepFactory := func() *loop.Step { return loop.New() }`.
- **`examples/http-chat/main.go`** and **`examples/tui-chat/main.go`** — Reference applications that call `session.NewManager` directly with a `func() *loop.Step` step factory.
- **`examples/forge/README.md`** — Documents "Common Gaps" including "Custom artifact handlers" and "Tool definitions" as future work.
- **`cmd/forge/README.md`** — Documents the current manifest format (dist + conduits only).

### Call Sites Requiring Mechanical Updates

The `func() *loop.Step` signature appears in these files and must all be updated to `func() (*loop.Step, error)`:

- `session/manager.go` — field declaration, `NewManager`, `Create`, `Attach`
- `session/doc.go` — example code in package doc comment
- `session/manager_test.go` — ~36 lambdas (e.g. `func() *loop.Step { return loop.New() }`)
- `session/stream_test.go` — ~8 lambdas
- `examples/http-chat/main.go` — `stepFactory` lambda
- `examples/tui-chat/main.go` — `stepFactory` lambda
- `x/conduit/http/config_test.go` — ~4 lambdas
- `x/conduit/http/handler_test.go` — ~40+ lambdas
- `x/conduit/slack/events_test.go` — ~7 lambdas
- `x/conduit/slack/slack_test.go` — ~8 lambdas
- `x/conduit/slack/thread_test.go` — ~2 lambdas
- `x/conduit/telegram/telegram_test.go` — ~9 lambdas
- `x/conduit/tui/tui_test.go` — ~3 lambdas
- `cmd/forge/templates/main.go.tmpl` — generated `stepFactory` lambda

## Architectural Blueprint

### Selected Path: Error-Propagating Step Factory + Handler Module Contract

This is the only viable path identified in the issue. The core change is mechanical: every `func() *loop.Step` becomes `func() (*loop.Step, error)`, and every `return loop.New()` becomes `return loop.New(), nil`. This unlocks handler constructors (which may return `error`) to be called inside the step factory closure.

**Handler Module Contract** (analogous to conduits but without `session.Manager` dependency):

```go
package somehandler

// Option configures the handler.
type Option func(*Handler)

// New creates a handler implementing loop.Handler.
func New(opts ...Option) (loop.Handler, error)

// OptionsFromMap bridges a YAML-decoded options map to functional options.
func OptionsFromMap(m map[string]any) ([]Option, error)
```

**Blueprint Schema** mirrors the existing `conduits` list:

```yaml
dist:
  name: my-agent
  output_path: ./my-agent
conduits:
  - module: github.com/andrewhowdencom/ore/x/conduit/http
handlers:
  - module: github.com/andrewhowdencom/ore/x/handler/somehandler
    options:
      key: value
```

**Generated `main.go`** instantiates handlers inside the step factory closure and passes them to `loop.New(loop.WithHandlers(...))`:

```go
stepFactory := func() (*loop.Step, error) {
    h0, err := somehandler.New()
    if err != nil {
        return nil, fmt.Errorf("create handler: %w", err)
    }
    return loop.New(loop.WithHandlers(h0)), nil
}
```

**No Tree-of-Thought deliberation required** — the issue explicitly defines the required architecture and there is only one viable path. The `func() (*loop.Step, error)` signature change is the minimal, correct design given that handler constructors return errors and the factory runs inside `Create`/`Attach` where errors can be propagated.

## Requirements

1. `session.NewManager` accepts `func() (*loop.Step, error)` instead of `func() *loop.Step`.
2. `session.Manager.Create()` and `Attach()` propagate step creation errors.
3. Blueprint schema supports an optional `handlers` list with `module` (required) and `options` (optional).
4. Generated `main.go` imports handler modules, instantiates them per-stream, and wires them via `loop.WithHandlers(...)`.
5. `cmd/forge/build.go` resolves external handler modules via `go get` (same pattern as external conduits).
6. All existing tests pass (`go test -race ./...`) after mechanical signature updates.
7. New tests cover handler module parsing, generation, and build resolution.
8. Documentation updated (`cmd/forge/README.md`, `examples/forge/README.md`).

## Task Breakdown

### Task 1: Core session.Manager Signature Change and Mechanical Updates
- **Goal**: Change `newStep` from `func() *loop.Step` to `func() (*loop.Step, error)` across the entire repo, including all call sites and tests.
- **Dependencies**: None.
- **Files Affected**:
  - `session/manager.go` — `Manager.newStep` field type, `NewManager` signature, `Create` error propagation, `Attach` error propagation
  - `session/doc.go` — package doc example code
  - `examples/http-chat/main.go` — `stepFactory` lambda
  - `examples/tui-chat/main.go` — `stepFactory` lambda
  - `session/manager_test.go` — all `NewManager` call lambdas (`return loop.New()` → `return loop.New(), nil`)
  - `session/stream_test.go` — all `NewManager` call lambdas
  - `x/conduit/http/config_test.go` — all `NewManager` call lambdas
  - `x/conduit/http/handler_test.go` — all `NewManager` call lambdas
  - `x/conduit/slack/events_test.go` — all `NewManager` call lambdas
  - `x/conduit/slack/slack_test.go` — all `NewManager` call lambdas
  - `x/conduit/slack/thread_test.go` — all `NewManager` call lambdas
  - `x/conduit/telegram/telegram_test.go` — all `NewManager` call lambdas
  - `x/conduit/tui/tui_test.go` — all `NewManager` call lambdas
  - `cmd/forge/templates/main.go.tmpl` — generated `stepFactory` signature and return
- **New Files**: None.
- **Interfaces**:
  - `func NewManager(store thread.Store, prov provider.Provider, newStep func() (*loop.Step, error), processor TurnProcessor, opts ...ManagerOption) *Manager`
  - `func (m *Manager) Create() (*Stream, error)` — already returns error, now also propagates `m.newStep()` error
  - `func (m *Manager) Attach(threadID string) (*Stream, error)` — already returns error, now also propagates `m.newStep()` error
- **Validation**: `go test -race ./...` passes. This is the definitive check for this task — the mechanical change touches ~15 files across multiple packages.
- **Details**: 
  1. In `session/manager.go`, change `newStep func() *loop.Step` to `newStep func() (*loop.Step, error)` on the `Manager` struct.
  2. Update `NewManager` parameter signature.
  3. In `Create()`, change `step := m.newStep()` to `step, err := m.newStep(); if err != nil { return nil, fmt.Errorf("create step: %w", err) }`.
  4. In `Attach()`, apply the same pattern.
  5. In `session/doc.go`, update the example `stepFactory` comment.
  6. In `examples/http-chat/main.go` and `examples/tui-chat/main.go`, update `stepFactory` to return `(*loop.Step, error)`.
  7. Globally replace `func() *loop.Step { return loop.New() }` with `func() (*loop.Step, error) { return loop.New(), nil }` in all test files. Use `sed` or a bulk edit — this is 100% mechanical and must not change any other logic.
  8. In `cmd/forge/templates/main.go.tmpl`, change `stepFactory := func() *loop.Step { return loop.New() }` to `stepFactory := func() (*loop.Step, error) { return loop.New(), nil }`.

### Task 2: Add HandlerConfig to Blueprint Schema
- **Goal**: Extend the `Blueprint` struct and `ParseBlueprint` to support an optional `handlers` list.
- **Dependencies**: None (parallel with Task 1 — no shared files).
- **Files Affected**:
  - `cmd/forge/blueprint.go` — `Blueprint` struct, `ParseBlueprint` validation
- **New Files**: None.
- **Interfaces**:
  - New type: `type HandlerConfig struct { Module string; Options map[string]any }`
  - `Blueprint` gains field: `Handlers []HandlerConfig \`yaml:"handlers,omitempty"\``
  - `ParseBlueprint` gains validation: if `len(b.Handlers) > 0`, each entry must have non-empty `Module`
- **Validation**: `go test -race ./cmd/forge/...` passes. Specifically, `TestParseBlueprint` and new handler tests must pass.
- **Details**:
  1. Add `HandlerConfig` struct mirroring `ConduitConfig`.
  2. Add `Handlers []HandlerConfig` to `Blueprint` with `yaml:"handlers,omitempty"` tag.
  3. In `ParseBlueprint`, after conduit validation, add handler validation: iterate `b.Handlers`, require `Module != ""`, return formatted error with index.
  4. Update `cmd/forge/blueprint_test.go` with new test cases: valid blueprint with handlers, missing handler module, handlers with options, empty handlers (optional so should be valid).

### Task 3: Generate Handler Wiring in Templates
- **Goal**: Update `GenerateMainGo` and the template to import, instantiate, and wire handler modules inside the per-stream `stepFactory` closure.
- **Dependencies**: Task 2 (needs `HandlerConfig` type).
- **Files Affected**:
  - `cmd/forge/generate.go` — `HandlerTemplateData`, `MainGoTemplateData`, `buildTemplateData`
  - `cmd/forge/templates/main.go.tmpl` — handler imports, stepFactory handler instantiation
  - `cmd/forge/generate_test.go` — new test cases for handler generation
- **New Files**: None.
- **Interfaces**:
  - New type: `HandlerTemplateData struct { Index int; ImportAlias string; ModulePath string; HasOptions bool; OptionsLiteral string }`
  - `MainGoTemplateData` gains field: `Handlers []HandlerTemplateData`
  - `buildTemplateData` processes `blueprint.Handlers` using the same alias derivation and option formatting as conduits, but with a separate alias namespace (handlers and conduits may share the same module path segment, so aliases must be derived from a combined used-aliases map to prevent collision).
- **Validation**: `go test -race ./cmd/forge/...` passes.
- **Details**:
  1. In `cmd/forge/generate.go`, add `HandlerTemplateData` struct (same shape as `ConduitTemplateData`).
  2. Update `MainGoTemplateData` to include `Handlers []HandlerTemplateData`.
  3. Update `buildTemplateData` to:
     - Process handlers after conduits (or interleaved, but alias derivation must use the same `usedAliases` map so that a handler and conduit with the same last path segment get disambiguated, e.g. `http` conduit and `http` handler → `httpc` and `http` or similar).
     - For each handler, create `HandlerTemplateData` with `ImportAlias`, `ModulePath`, `HasOptions`, `OptionsLiteral`.
  4. In `cmd/forge/templates/main.go.tmpl`:
     - Add `{{- range .Handlers }}` import block alongside conduits.
     - Inside `stepFactory`, before `return loop.New(...)`, add handler instantiation:
       ```
       {{- range .Handlers }}
       {{- if .HasOptions }}
           {{.ImportAlias}}OptsMap := {{.OptionsLiteral}}
           {{.ImportAlias}}Opts, err := {{.ImportAlias}}.OptionsFromMap({{.ImportAlias}}OptsMap)
           if err != nil {
               return nil, fmt.Errorf("create handler {{.ImportAlias}}: %w", err)
           }
           h{{.Index}}, err := {{.ImportAlias}}.New({{.ImportAlias}}Opts...)
       {{- else }}
           h{{.Index}}, err := {{.ImportAlias}}.New()
       {{- end }}
           if err != nil {
               return nil, fmt.Errorf("create handler: %w", err)
           }
       {{- end }}
       ```
     - Change the `return loop.New(), nil` to pass handlers:
       ```
       return loop.New(
       {{- range .Handlers }}
           loop.WithHandlers(h{{.Index}}),
       {{- end }}
       ), nil
       ```
     - Note: if there are no handlers, `loop.New()` is called with no options, which is correct.
  5. In `cmd/forge/generate_test.go`, add test cases:
     - Blueprint with single handler (no options)
     - Blueprint with handler options
     - Blueprint with conduits and handlers together
     - Blueprint with handler+conduit alias collision (e.g. both end in `http`)
     - Verify generated code contains handler imports, instantiation, and `loop.WithHandlers` call.

### Task 4: Build Pipeline Resolves External Handler Modules
- **Goal**: Update `Build` to `go get` external handler module paths before `go mod tidy`, using the same pattern as external conduits.
- **Dependencies**: Task 2 (needs `HandlerConfig` to iterate modules).
- **Files Affected**:
  - `cmd/forge/build.go` — `Build` function
  - `cmd/forge/build_test.go` — new tests for external handler module resolution
- **New Files**: None.
- **Interfaces**:
  - `Build` function gains logic: before `go mod tidy`, iterate `blueprint.Handlers` and `blueprint.Conduits` together, collect external module paths (those not under `github.com/andrewhowdencom/ore/`), run `go get <module>` for each unique external module.
- **Validation**: `go test -race ./cmd/forge/...` passes.
- **Details**:
  1. In `cmd/forge/build.go`, before the `go mod tidy` call, collect all unique external modules from both `blueprint.Conduits` and `blueprint.Handlers`.
  2. For each unique external module, run `go get <module>` in `tmpDir`.
  3. In `cmd/forge/build_test.go`, add `TestBuildExternalHandlerModule` (mocks `execCommand`, verifies `go get` is called for handler modules) and `TestBuildExternalHandlerModuleFailure`.

### Task 5: Integration Tests and Testdata
- **Goal**: Add handler testdata blueprints and update integration tests to cover end-to-end handler generation.
- **Dependencies**: Task 3, Task 4.
- **Files Affected**:
  - `cmd/forge/cmd_generate_test.go` — new test cases for handler blueprints
- **New Files**:
  - `cmd/forge/testdata/handler-forge.yaml` — minimal blueprint with a handler module
  - `cmd/forge/testdata/handler-options-forge.yaml` — blueprint with handler options
- **Interfaces**: None.
- **Validation**: `go test -race ./cmd/forge/...` passes.
- **Details**:
  1. Create `cmd/forge/testdata/handler-forge.yaml`:
     ```yaml
     dist:
       name: handler-smoke-agent
       output_path: ./handler-smoke-agent
     conduits:
       - module: github.com/andrewhowdencom/ore/x/conduit/http
     handlers:
       - module: github.com/andrewhowdencom/ore/tool
     ```
  2. Create `cmd/forge/testdata/handler-options-forge.yaml` with handler options.
  3. In `cmd/forge/cmd_generate_test.go`, add test cases using the new testdata:
     - Verify stdout output contains handler import
     - Verify directory output contains handler instantiation and `loop.WithHandlers`

### Task 6: Update Documentation
- **Goal**: Update `cmd/forge/README.md` and `examples/forge/README.md` to document the new `handlers` blueprint section.
- **Dependencies**: Task 2, Task 3, Task 4, Task 5 (functional work must be complete before documenting).
- **Files Affected**:
  - `cmd/forge/README.md`
  - `examples/forge/README.md`
- **New Files**: None.
- **Interfaces**: None.
- **Validation**: Manual review of documentation for accuracy and completeness.
- **Details**:
  1. In `cmd/forge/README.md`:
     - Update the "Manifest Format" section to show the optional `handlers` list.
     - Add a handler example alongside the conduit example.
     - Document that handlers follow the same `module`/`options` pattern as conduits.
  2. In `examples/forge/README.md`:
     - Update the "Common Gaps" comparison tables: mark "Custom artifact handlers" and "Tool definitions" as now supported via handler modules.
     - Update the "Future Work" section: remove or demote the handler-related items, keep provider selection and cognitive pattern selection as remaining gaps.
     - Add a handler example to the "Blueprint Format" section.

## Dependency Graph

- Task 1 || Task 2 (parallel — no shared files)
- Task 2 → Task 3 (Task 3 needs `HandlerConfig` from Task 2)
- Task 2 → Task 4 (Task 4 needs `HandlerConfig` from Task 2)
- Task 3 → Task 5 (Task 5 needs template generation with handlers from Task 3)
- Task 4 → Task 5 (Task 5 needs build pipeline support from Task 4)
- Task 1, Task 3, Task 4, Task 5 → Task 6 (docs depend on all functional work)

Full dependency chain for critical path:
```
Task 1 || Task 2
Task 2 → Task 3 → Task 5
Task 2 → Task 4 → Task 5
Task 5 → Task 6
```

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Missing a test lambda during mechanical signature update | Medium | Medium | Use `grep` to find all occurrences, then bulk `sed`. Run `go test -race ./...` to catch compilation errors. |
| Template generates invalid Go when handlers + conduits share import alias | High | Low | `buildTemplateData` uses a single `usedAliases` map across both conduits and handlers. Covered by generate tests with collision scenarios. |
| External handler module `go get` fails during build | Medium | Low | Same pattern as conduits — already tested. Build test mocks `execCommand` to verify the call is made. |
| Generated code fails to compile due to `loop.WithHandlers` type mismatch | High | Low | `parser.ParseFile` in `GenerateMainGo` catches syntax errors. Full `go build` in `Build` catches type errors. Integration tests cover this. |
| Blueprint validation rejects empty handlers list | Low | Low | Explicitly make handlers optional in `ParseBlueprint` — only validate if `len(Handlers) > 0`. Test with empty handlers. |

## Validation Criteria

- [ ] `go test -race ./...` passes after Task 1.
- [ ] `go test -race ./cmd/forge/...` passes after Task 2.
- [ ] `go test -race ./cmd/forge/...` passes after Task 3.
- [ ] `go test -race ./cmd/forge/...` passes after Task 4.
- [ ] `go test -race ./cmd/forge/...` passes after Task 5.
- [ ] `cmd/forge/README.md` documents the `handlers` section with at least one example.
- [ ] `examples/forge/README.md` updates the "Common Gaps" table to reflect handler support.
- [ ] A blueprint with handlers can be generated and built end-to-end (integration test in Task 5).
