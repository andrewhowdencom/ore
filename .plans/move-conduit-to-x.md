# Plan: Move Conduit Packages to x/ Extension Directory

> **Blocked by:** [#99 — Reframe session package into Stream and Manager primitives](https://github.com/andrewhowdencom/ore/issues/99)
>
> Do not execute this plan until #99 is complete. After the session reframe, `session` will own its own event vocabulary and will no longer import `conduit`, making this move a pure mechanical relocation with no cross-import from a root package into `x/`.

## Objective
Establish the `x/` package convention for all non-core extensions by moving the `conduit/` package tree — the capability-metadata package (`conduit/conduit.go`), `conduit/tui/`, and `conduit/http/` — to `x/conduit/`. Update all downstream imports, templates, and tests to reference the new paths. This is the first concrete step toward the architectural boundary described in GitHub issue #93.

## Context
The ore repository currently co-locates core and extension packages at the root. After #99 lands, the architecture will be:

- **`session/`** — owns the per-session `Stream` primitive (events in, output events out) and the `Manager` registry. Defines `session.Event`, `session.UserMessageEvent`, `session.InterruptEvent`.
- **`conduit/`** — reduced to capability metadata only: `Capability`, `Descriptor`, `Capable`, `Conduit` interface. No event types.
- **`conduit/tui/`** and **`conduit/http/`** — I/O frontend implementations that import `session` for event types and the `Stream` API, and import `conduit` (or `x/conduit` after this move) for capability descriptors.

With `session` no longer importing `conduit`, the move to `x/conduit/` becomes a clean boundary: no root-level package will import `x/conduit`. The only importers of `x/conduit` will be:
- `x/conduit/tui` and `x/conduit/http` (self-references within the extension tree)
- `cmd/docgen` (documentation generator)
- `cmd/forge` templates (generated code)
- `examples/` (reference applications)

Key files and packages identified:
- **Source to move**: `conduit/conduit.go` (capability metadata only — `event.go` will have been deleted by #99), `conduit/tui/` (Bubble Tea terminal UI), `conduit/http/` (HTTP handler library with SSE/NDJSON streaming, embedded web UI)
- **Consumers of `github.com/andrewhowdencom/ore/conduit` (after #99)**: `cmd/docgen/main.go`, `cmd/forge/templates/main.go.tmpl`, `cmd/forge/generate_test.go`, `cmd/forge/cmd_generate_test.go`, plus self-references in `conduit/tui/*.go` and `conduit/http/*.go`
- **Consumers of `github.com/andrewhowdencom/ore/conduit/tui`**: `examples/tui-chat/main.go`, `cmd/docgen/main.go`, `cmd/forge/templates/main.go.tmpl`, `cmd/forge/generate_test.go`, `cmd/forge/cmd_generate_test.go`, plus self-references in `conduit/tui/*.go`
- **Consumers of `github.com/andrewhowdencom/ore/conduit/http`**: `examples/http-chat/main.go`, `cmd/forge/templates/main.go.tmpl`, `cmd/forge/generate_test.go`, plus self-references in `conduit/http/*.go`
- The module path is `github.com/andrewhowdencom/ore`; no `go.mod` changes are required because this is an intra-module move.
- The `//go:embed static/*` directive in `conduit/http/static.go` is relative to the source file and will continue to work after the directory move.

## Architectural Blueprint

The post-#99, post-move layout:

```
session/
  event.go            — Event, UserMessageEvent, InterruptEvent (from #99)
  stream.go           — Stream primitive (from #99)
  manager.go          — slimmed Manager (registry only, from #99)
x/
  conduit/
    conduit.go        — Capability, Descriptor, Capable, Conduit interface
    conduit_test.go   — tests for capability model
    http/
      doc.go, handler.go, handler_test.go, sse.go, static.go, stream.go, types.go, types_test.go
      static/         — embedded web UI assets
    tui/
      markdown.go, model.go, model_test.go, styles.go, styles/, tui.go, tui_test.go, view.go, view_test.go
```

All importers update their import paths from `github.com/andrewhowdencom/ore/conduit` to `github.com/andrewhowdencom/ore/x/conduit`, and similarly for `conduit/http` and `conduit/tui`.

## Requirements
1. Move `conduit/` directory tree to `x/conduit/` preserving git history (`git mv`). Note: `conduit/event.go` will not exist (deleted by #99).
2. Update all self-referencing imports within the moved `x/conduit/tui/` and `x/conduit/http/` packages.
3. Update `cmd/docgen/` imports to reference `x/conduit` and `x/conduit/tui`.
4. Update `cmd/forge/templates/main.go.tmpl` generated import paths to `x/conduit/http` and `x/conduit/tui`.
5. Update `cmd/forge/` test assertions that check for the old import paths in generated code.
6. Update `examples/tui-chat/main.go` and `examples/http-chat/main.go` imports.
7. Ensure `go test -race ./...` passes after all changes.

## Task Breakdown

### Task 1: Move conduit/ to x/conduit/ and Fix Internal Imports
- **Goal**: Physically move the directory tree and update imports inside the moved packages so they reference each other via the new `x/` prefix.
- **Dependencies**: #99 complete.
- **Files Affected**:
  - `conduit/conduit.go` → `x/conduit/conduit.go`
  - `conduit/conduit_test.go` → `x/conduit/conduit_test.go`
  - `conduit/http/doc.go` → `x/conduit/http/doc.go`
  - `conduit/http/handler.go` → `x/conduit/http/handler.go`
  - `conduit/http/handler_test.go` → `x/conduit/http/handler_test.go`
  - `conduit/http/sse.go` → `x/conduit/http/sse.go`
  - `conduit/http/static.go` → `x/conduit/http/static.go`
  - `conduit/http/stream.go` → `x/conduit/http/stream.go`
  - `conduit/http/types.go` → `x/conduit/http/types.go`
  - `conduit/http/types_test.go` → `x/conduit/http/types_test.go`
  - `conduit/tui/markdown.go` → `x/conduit/tui/markdown.go`
  - `conduit/tui/model.go` → `x/conduit/tui/model.go`
  - `conduit/tui/model_test.go` → `x/conduit/tui/model_test.go`
  - `conduit/tui/styles.go` → `x/conduit/tui/styles.go`
  - `conduit/tui/tui.go` → `x/conduit/tui/tui.go`
  - `conduit/tui/tui_test.go` → `x/conduit/tui/tui_test.go`
  - `conduit/tui/view.go` → `x/conduit/tui/view.go`
  - `conduit/tui/view_test.go` → `x/conduit/tui/view_test.go`
  - Embedded asset directories: `conduit/http/static/` → `x/conduit/http/static/`, `conduit/tui/styles/` → `x/conduit/tui/styles/`
- **New Files**: None (all are moves).
- **Interfaces**: No interface changes; only import path updates.
- **Validation**:
  - `go build ./x/conduit/...` must succeed.
  - `go test ./x/conduit/...` must pass.
- **Details**: Use `git mv` to move the entire `conduit/` directory to `x/conduit/`. Note: `conduit/event.go` will have been deleted by #99 and should not exist. Update every `.go` file inside `x/conduit/tui/` and `x/conduit/http/` that imports `github.com/andrewhowdencom/ore/conduit` to import `github.com/andrewhowdencom/ore/x/conduit` instead.

### Task 2: Update cmd/docgen/ Imports
- **Goal**: Update the docgen tool to import conduit descriptors from the new `x/` paths.
- **Dependencies**: Task 1.
- **Files Affected**: `cmd/docgen/main.go`
- **New Files**: None.
- **Interfaces**: No changes.
- **Validation**: `go build ./cmd/docgen` and `go test ./cmd/docgen/...` pass.
- **Details**: In `cmd/docgen/main.go`, change:
  - `github.com/andrewhowdencom/ore/conduit` → `github.com/andrewhowdencom/ore/x/conduit`
  - `github.com/andrewhowdencom/ore/conduit/tui` → `github.com/andrewhowdencom/ore/x/conduit/tui`

### Task 3: Update cmd/forge/ Templates and Tests
- **Goal**: Update the forge CLI code generator and its tests to emit and assert the new `x/conduit` import paths.
- **Dependencies**: Task 1.
- **Files Affected**:
  - `cmd/forge/templates/main.go.tmpl`
  - `cmd/forge/generate_test.go`
  - `cmd/forge/cmd_generate_test.go`
- **New Files**: None.
- **Interfaces**: No changes.
- **Validation**: `go test ./cmd/forge/...` passes.
- **Details**:
  1. In `cmd/forge/templates/main.go.tmpl`, update the two generated import lines:
     - `httpc "github.com/andrewhowdencom/ore/conduit/http"` → `httpc "github.com/andrewhowdencom/ore/x/conduit/http"`
     - `"github.com/andrewhowdencom/ore/conduit/tui"` → `"github.com/andrewhowdencom/ore/x/conduit/tui"`
  2. In `cmd/forge/generate_test.go`, update assertions:
     - `assert.Contains(t, content, "github.com/andrewhowdencom/ore/conduit/http")` → `...ore/x/conduit/http`
     - `assert.Contains(t, content, "github.com/andrewhowdencom/ore/conduit/tui")` → `...ore/x/conduit/tui`
  3. In `cmd/forge/cmd_generate_test.go`, update assertions:
     - `assert.Contains(t, out, "github.com/andrewhowdencom/ore/conduit/tui")` → `...ore/x/conduit/tui`
     - `assert.Contains(t, string(mainGo), "github.com/andrewhowdencom/ore/conduit/tui")` → `...ore/x/conduit/tui`

### Task 4: Update Examples
- **Goal**: Update example applications to import conduits from the new `x/` paths.
- **Dependencies**: Task 1.
- **Files Affected**: `examples/tui-chat/main.go`, `examples/http-chat/main.go`
- **New Files**: None.
- **Interfaces**: No changes.
- **Validation**: `go build ./examples/tui-chat` and `go build ./examples/http-chat` pass.
- **Details**:
  - In `examples/tui-chat/main.go`, change `"github.com/andrewhowdencom/ore/conduit/tui"` to `"github.com/andrewhowdencom/ore/x/conduit/tui"`.
  - In `examples/http-chat/main.go`, change `httpc "github.com/andrewhowdencom/ore/conduit/http"` to `httpc "github.com/andrewhowdencom/ore/x/conduit/http"`.

### Task 5: Final Validation
- **Goal**: Verify the entire repository builds and tests cleanly after all moves.
- **Dependencies**: Task 1, Task 2, Task 3, Task 4.
- **Files Affected**: None (validation only).
- **New Files**: None.
- **Interfaces**: No changes.
- **Validation**:
  - `go build ./...` passes with zero errors.
  - `go test -race ./...` passes with zero failures.
  - `go vet ./...` is clean.
- **Details**: Run the full build and test suite. If any remaining import references to the old `ore/conduit` (without `/x/`) paths exist outside of prose comments, fix them. The old `conduit/` directory should no longer exist at the repository root. Commit the entire change set as a single commit with a message like `refactor: Move conduit packages to x/conduit/`.

## Dependency Graph
- #99 → Task 1 → Task 2 → Task 5
- Task 1 → Task 3 → Task 5
- Task 1 → Task 4 → Task 5
- Task 2 || Task 3 || Task 4 (all are parallelizable after Task 1)

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| #99 changes scope after this plan is written | Medium | Low | Review this plan against the actual #99 implementation before executing. The core assumption is that `session` no longer imports `conduit` after #99. |
| Missing an import reference in a file not scanned during planning | Medium | Medium | Task 5 runs `go build ./...` which will catch any unresolved imports immediately. |
| Forge template tests assert on exact import path strings and are missed | Medium | Low | Task 3 explicitly lists `generate_test.go` and `cmd_generate_test.go`; `go test ./cmd/forge/...` will catch any remaining old-path assertions. |
| `git mv` is not used and history is lost | Low | Low | The task instructions explicitly specify `git mv`. |
| External consumers outside this repo break | Low | Low | This is acceptable per the project's aggressive-refactoring convention (AGENTS.md). No backwards-compatibility guarantee at this stage. |

## Validation Criteria
- [ ] `conduit/` directory no longer exists at repository root.
- [ ] `x/conduit/` directory exists with all former `conduit/` contents (minus `event.go`, removed by #99).
- [ ] No `.go` file in the repository imports `github.com/andrewhowdencom/ore/conduit` (without `/x/`), except possibly in prose doc comments.
- [ ] `go build ./...` succeeds with zero errors.
- [ ] `go test -race ./...` passes with zero failures.
- [ ] `go vet ./...` is clean.
