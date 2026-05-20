# Plan: Move tool/ Package to x/tool/ to Align with Extension Model

## Objective

Relocate the `tool/` package from the repository root to `x/tool/` so it sits alongside the `x/conduit/` extension model. Extract the inline calculator tools from `examples/calculator/main.go` and `examples/http-chat/main.go` into a reusable `x/tool/calculator/` submodule. Update all import paths, module configuration, and documentation to reflect the new structure. No remaining references to the old `github.com/andrewhowdencom/ore/tool` root import path should exist after the move.

## Context

The ore project treats core primitives (`artifact/`, `state/`, `provider/`, `loop/`) as root-level packages and extensions (`x/conduit/`) as composable add-ons under `x/`. The `tool/` package was created at root in an earlier iteration but is explicitly documented as an extension ("deliberately an extension, not core behavior"). The issue mandates moving it to `x/tool/` to align with this architectural boundary.

Key findings from repository mapping:

- **`tool/`** currently contains six files: `doc.go`, `tool.go`, `handler.go`, `registry.go`, `handler_test.go`, `registry_test.go`. Package `tool` implements `loop.Handler` and provides a `Registry` that maps tool names to `ToolFunc` implementations.
- **Import consumers:** Only `examples/calculator/main.go` and `examples/http-chat/main.go` import `github.com/andrewhowdencom/ore/tool`. Both define identical inline `add` and `multiply` calculator tools, duplicating logic and JSON schemas.
- **Extension model precedent:** `x/conduit/conduit.go` (base interface, part of main module) and `x/conduit/http/`, `x/conduit/tui/`, `x/conduit/slack/`, `x/conduit/telegram/` (concrete implementations, each with their own `go.mod` and `go.work` entry).
- **Module configuration:** The main `go.mod` declares dependencies on `x/conduit/http` and `x/conduit/tui` with `replace` directives pointing to local paths. `go.work` lists all x/conduit submodules.
- **`loop/handler.go`:** Defines the `loop.Handler` interface that `tool.Handler` implements. This stays in `loop/` per the issue's explicit out-of-scope rule.

## Architectural Blueprint

The selected architecture follows the existing `x/conduit/` extension model exactly:

1. **Base extension package (`x/tool/`)** — Moved from `tool/`, part of the main Go module. Contains the `Registry`, `Handler`, `ToolFunc` types and their tests. No separate `go.mod`.
2. **Concrete implementation (`x/tool/calculator/`)** — A new submodule with its own `go.mod`, added to `go.work`. Exports reusable `Add`/`Multiply` `tool.ToolFunc` implementations and their corresponding `provider.Tool` JSON schema descriptors (`AddTool`, `MultiplyTool`). Also exports `ToFloat64`, a shared helper for parsing JSON number arguments.
3. **Example apps (`examples/calculator/`, `examples/http-chat/`)** — Become thin composition layers. They import `github.com/andrewhowdencom/ore/x/tool/calculator` and wire pre-built tools into a registry instead of defining them inline.

**Evaluated alternatives:**

- *Path A (selected):* `x/tool/calculator/` as a separate Go module. Matches `x/conduit/http/`, `x/conduit/tui/` pattern. Slightly more boilerplate (extra `go.mod`, `go.work` entry) but structurally consistent and enables external consumers to depend only on the calculator package.
- *Path B:* `x/tool/calculator/` as a package inside the main module (no `go.mod`). Simpler build graph but breaks the extension model precedent and makes it impossible for external consumers to depend on just the calculator tools.
- *Path C:* Keep calculator tools in `examples/` and only move `tool/` to `x/tool/`. Rejected because it leaves duplicated inline tool definitions across two examples and fails the acceptance criterion that `examples/calculator/` must import from `x/tool/calculator/`.

## Requirements

1. `x/tool/` contains `Registry`, `Handler`, `ToolFunc`, and all original tests, moved from `tool/`.
2. `x/tool/calculator/` contains reusable `Add`, `Multiply` tool functions and their `provider.Tool` JSON schema descriptors.
3. `examples/calculator/` and `examples/http-chat/` import from `x/tool/calculator/` and no longer define tools inline.
4. `go.mod` and `go.work` are updated so the workspace builds cleanly.
5. `go test -race ./...` passes after the move.
6. No remaining import references to `github.com/andrewhowdencom/ore/tool` (root path) in `.go`, `.mod`, or `.work` files.
7. `README.md` package table updated to reflect the new `x/tool` path. [inferred]

## Task Breakdown

### Task 1: Move `tool/` Package to `x/tool/`
- **Goal**: Relocate all `tool/` source and test files to `x/tool/` and update package doc to reflect the new path.
- **Dependencies**: None.
- **Files Affected**: `tool/doc.go`, `tool/tool.go`, `tool/handler.go`, `tool/registry.go`, `tool/handler_test.go`, `tool/registry_test.go`
- **New Files**: `x/tool/doc.go`, `x/tool/tool.go`, `x/tool/handler.go`, `x/tool/registry.go`, `x/tool/handler_test.go`, `x/tool/registry_test.go`
- **Interfaces**: No API changes. Package remains `package tool`; import path changes from `github.com/andrewhowdencom/ore/tool` to `github.com/andrewhowdencom/ore/x/tool`.
- **Validation**: `go test ./x/tool/...` passes. Old `tool/` directory is fully removed.
- **Details**:
  1. Move each file from `tool/` to `x/tool/`.
  2. In `x/tool/doc.go`, update the package comment to reference the new path where appropriate (e.g., any self-referential import examples).
  3. In test files, remove the now-unnecessary `github.com/andrewhowdencom/ore/tool` import since tests are in-package.
  4. Delete the old `tool/` directory entirely.
  5. Run `go test ./x/tool/...` to confirm compilation.

### Task 2: Create `x/tool/calculator/` Submodule
- **Goal**: Extract reusable calculator tool implementations and JSON schemas into a new submodule under `x/tool/calculator/`.
- **Dependencies**: Task 1.
- **Files Affected**: None (new submodule).
- **New Files**:
  - `x/tool/calculator/doc.go`
  - `x/tool/calculator/calculator.go`
  - `x/tool/calculator/calculator_test.go`
  - `x/tool/calculator/go.mod`
- **Interfaces**:
  - `func Add(ctx context.Context, args map[string]any) (any, error)` — tool function for addition.
  - `func Multiply(ctx context.Context, args map[string]any) (any, error)` — tool function for multiplication.
  - `var AddTool provider.Tool` — JSON schema descriptor for `Add`.
  - `var MultiplyTool provider.Tool` — JSON schema descriptor for `Multiply`.
  - `func ToFloat64(v any) float64` — shared helper for parsing JSON-decoded numbers.
- **Validation**: `go test ./x/tool/calculator/...` passes. The package compiles independently.
- **Details**:
  1. Create `x/tool/calculator/doc.go` with package documentation explaining the reusable calculator tools.
  2. Create `x/tool/calculator/calculator.go`:
     - Implement `Add` and `Multiply` as `tool.ToolFunc`-compatible functions.
     - Define `AddTool` and `MultiplyTool` as exported `provider.Tool` vars with the same JSON schemas currently inlined in `examples/calculator/main.go` and `examples/http-chat/main.go`.
     - Export `ToFloat64` (lifted from the duplicated helper in both example files).
  3. Create `x/tool/calculator/calculator_test.go` with basic coverage: verify `Add` and `Multiply` compute correct results, `ToFloat64` handles `float64`, `int`, and `string` inputs.
  4. Create `x/tool/calculator/go.mod`:
     - Module path: `github.com/andrewhowdencom/ore/x/tool/calculator`
     - Require `github.com/andrewhowdencom/ore v0.0.0`
     - Add `replace github.com/andrewhowdencom/ore => ../../..`
  5. Add `./x/tool/calculator` to `go.work`.

### Task 3: Refactor `examples/calculator/main.go`
- **Goal**: Remove inline tool definitions and import reusable calculator exports from `x/tool/calculator/`.
- **Dependencies**: Task 2.
- **Files Affected**: `examples/calculator/main.go`
- **New Files**: None.
- **Interfaces**: No new interfaces.
- **Validation**: `go build ./examples/calculator/` succeeds.
- **Details**:
  1. Replace `"github.com/andrewhowdencom/ore/tool"` import with `"github.com/andrewhowdencom/ore/x/tool"` and `"github.com/andrewhowdencom/ore/x/tool/calculator"`.
  2. Remove inline `add`, `multiply`, and `toFloat64` definitions.
  3. Register tools from the calculator package:
     ```go
     registry.Register(calculator.AddTool.Name, calculator.Add)
     registry.Register(calculator.MultiplyTool.Name, calculator.Multiply)
     ```
  4. Use `calculator.AddTool` and `calculator.MultiplyTool` for the `tools` slice passed to `openai.WithTools(tools)`.

### Task 4: Refactor `examples/http-chat/main.go`
- **Goal**: Remove inline tool definitions from the HTTP chat example and import reusable calculator exports.
- **Dependencies**: Task 2.
- **Files Affected**: `examples/http-chat/main.go`
- **New Files**: None.
- **Interfaces**: No new interfaces.
- **Validation**: `go build ./examples/http-chat/` succeeds.
- **Details**:
  1. Replace `"github.com/andrewhowdencom/ore/tool"` import with `"github.com/andrewhowdencom/ore/x/tool"` and `"github.com/andrewhowdencom/ore/x/tool/calculator"`.
  2. Remove inline `add`, `multiply`, and `toFloat64` definitions.
  3. Update the package-level comment: replace "See package tool for details" with "See package x/tool for details".
  4. Register tools and build the `tools` slice using `calculator.Add`, `calculator.Multiply`, `calculator.AddTool`, `calculator.MultiplyTool` (same pattern as Task 3).

### Task 5: Update Root Module Configuration
- **Goal**: Wire `x/tool/calculator/` into the root module and workspace so all packages resolve correctly.
- **Dependencies**: Task 2.
- **Files Affected**: `go.mod`, `go.work`
- **New Files**: None.
- **Interfaces**: No new interfaces.
- **Validation**: `go mod tidy` in the root module completes without errors. `go mod tidy` in `x/tool/calculator/` completes without errors.
- **Details**:
  1. In the root `go.mod`:
     - Add `require github.com/andrewhowdencom/ore/x/tool/calculator v0.0.0-00010101000000-000000000000` (following the existing `x/conduit/http` pattern).
     - Add `replace github.com/andrewhowdencom/ore/x/tool/calculator => ./x/tool/calculator`.
  2. In `go.work`:
     - Add `./x/tool/calculator` to the `use` block.
  3. Run `go mod tidy` in the root module.
  4. Run `go mod tidy` in `x/tool/calculator/`.

### Task 6: Update README.md Package Table
- **Goal**: Update documentation to reflect the new `x/tool` package path.
- **Dependencies**: Task 1.
- **Files Affected**: `README.md`
- **New Files**: None.
- **Interfaces**: No new interfaces.
- **Validation**: README renders correctly; the `tool` row points to `x/tool`.
- **Details**:
  1. In the package table, change the `tool` row to `x/tool`.
  2. Update the pkg.go.dev link from `.../tool` to `.../x/tool`.

### Task 7: Final Verification
- **Goal**: Confirm the entire repository compiles, tests pass, and no stale import paths remain.
- **Dependencies**: Tasks 1–6.
- **Files Affected**: None (verification only).
- **New Files**: None.
- **Validation**:
  - `go test -race ./...` passes.
  - `grep -r "github.com/andrewhowdencom/ore/tool" --include="*.go" --include="*.mod" --include="*.work" .` returns zero matches.
- **Details**:
  1. Run `go test -race ./...` from the repository root.
  2. Run the grep command above to confirm no stale `github.com/andrewhowdencom/ore/tool` imports remain.
  3. Run `go build ./examples/calculator/` and `go build ./examples/http-chat/` as smoke tests.

## Dependency Graph

- Task 1 → Task 2 (Task 2 imports `x/tool/`)
- Task 2 → Task 3 (Task 3 uses calculator exports)
- Task 2 → Task 4 (Task 4 uses calculator exports)
- Task 2 → Task 5 (Task 5 wires the new submodule)
- Task 1 → Task 6 (Task 6 references the new `x/tool` path)
- Tasks 3, 4, 5, 6 → Task 7 (final verification)
- Task 3 || Task 4 (parallelizable once Task 2 is complete)

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| `go.work` / `go.mod` replace cycles cause build failures | High | Medium | Follow existing `x/conduit/http` pattern exactly; run `go mod tidy` in both root and submodule after each change. |
| Tests in `x/tool/` fail after move due to stale in-package imports | Low | Low | Test files are in-package; they never import `github.com/andrewhowdencom/ore/tool` explicitly. Only `handler_test.go` imports `artifact` and `state`, which are unchanged. |
| Example build failures due to missing `ToFloat64` after inline removal | Medium | Low | Ensure `calculator.ToFloat64` is exported and both examples reference it. Smoke-build each example after refactoring. |
| README or historical plan files contain outdated `tool/` references | Low | Low | The acceptance criteria only requires no `.go`/`.mod`/`.work` references. Historical `.plans/` files are documentation artifacts and do not need updating. README is covered by Task 6. |

## Validation Criteria

- [ ] `x/tool/doc.go`, `x/tool/tool.go`, `x/tool/handler.go`, `x/tool/registry.go`, `x/tool/handler_test.go`, `x/tool/registry_test.go` exist and compile.
- [ ] Old `tool/` directory is fully removed.
- [ ] `x/tool/calculator/doc.go`, `x/tool/calculator/calculator.go`, `x/tool/calculator/calculator_test.go`, `x/tool/calculator/go.mod` exist and compile.
- [ ] `examples/calculator/main.go` imports `github.com/andrewhowdencom/ore/x/tool/calculator` and has no inline tool definitions.
- [ ] `examples/http-chat/main.go` imports `github.com/andrewhowdencom/ore/x/tool/calculator` and has no inline tool definitions.
- [ ] `go.work` contains `./x/tool/calculator`.
- [ ] Root `go.mod` contains a `require` and `replace` entry for `github.com/andrewhowdencom/ore/x/tool/calculator`.
- [ ] `go test -race ./...` passes.
- [ ] `grep -r "github.com/andrewhowdencom/ore/tool" --include="*.go" --include="*.mod" --include="*.work" .` returns zero matches.
