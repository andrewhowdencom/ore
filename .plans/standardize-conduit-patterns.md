# Plan: Standardize Conduit Patterns

## Objective

Establish and document a canonical contract for ore conduit packages. The TUI conduit (`x/conduit/tui`) already follows the intended pattern (exported `Descriptor`, constructor signature, blocking `Start(ctx)`, graceful shutdown), but the HTTP conduit (`x/conduit/http`) is missing the `Descriptor` export and there is no single reference document that future conduit authors (including `forge`) can follow. This plan closes that gap by adding the missing HTTP `Descriptor`, updating `x/conduit/doc.go` to document the standard contract, and regenerating the capability matrix.

## Context

### Current State

- **`x/conduit/conduit.go`** defines `Conduit` interface, `Capability` constants, and `Descriptor` struct. `Descriptor` is documented with: "Each conduit package exports a Descriptor variable that enumerates the well-known capabilities it supports."
- **`x/conduit/tui/tui.go`** exports a `Descriptor` variable (`tui.Descriptor`) with capabilities: `CapEventSource`, `CapShowStatus`, `CapRenderTurn`, `CapRenderMarkdown`. It follows the constructor pattern `New(mgr *session.Manager, opts ...Option) (conduit.Conduit, error)`. Its `Start(ctx)` subscribes to the session stream inside the method, then blocks on the Bubble Tea program until `ctx` is cancelled or the user quits.
- **`x/conduit/http/handler.go`** follows the same constructor pattern (`New(mgr *session.Manager, opts ...Option) (conduit.Conduit, error)`) and `Start(ctx)` blocks until `ctx` is cancelled (with graceful `server.Shutdown`). **It does NOT export a `Descriptor` variable.** The docgen tool (`cmd/docgen/main.go`) therefore only lists the TUI in the generated capability matrix.
- **`cmd/docgen/main.go`** explicitly imports `github.com/andrewhowdencom/ore/x/conduit/tui` and references `tui.Descriptor` in the `descriptors` slice. It does not import `x/conduit/http`.
- **`x/conduit/doc.go`** describes the Conduit interface and mentions Descriptor exports, but does NOT document the concrete lifecycle contract (constructor, Start(), shutdown) that implementers must satisfy.
- **`docs/conduit-capabilities.md`** is auto-generated and currently only shows the TUI column.

### Architectural Decision

There is only one viable path: add the missing export and document the existing pattern. The TUI and HTTP conduits already share the correct constructor and lifecycle signatures; the only missing piece is the formal documentation and the HTTP `Descriptor`. No structural refactoring is required.

## Requirements

1. Add a `Descriptor` variable export to `x/conduit/http` with capabilities matching the conduit's actual features (event streaming, status display, turn rendering, markdown rendering).
2. Update `x/conduit/doc.go` to document the standard conduit contract covering: constructor signature, `Descriptor` export, sink registration inside `Start()`, blocking `Start(ctx)` until `ctx` cancelled, and graceful shutdown behavior.
3. Update `cmd/docgen/main.go` to import and include `x/conduit/http`'s `Descriptor` in the generated matrix.
4. Add unit tests verifying the HTTP `Descriptor` is exported and has valid fields.
5. Regenerate `docs/conduit-capabilities.md` to include the HTTP conduit column.
6. All existing and new tests pass with `-race`.

## Task Breakdown

### Task 1: Add HTTP Descriptor Export
- **Goal**: Export a `Descriptor` variable from `x/conduit/http` that describes the conduit's capabilities.
- **Dependencies**: None.
- **Files Affected**: `x/conduit/http/handler.go`
- **New Files**: None.
- **Interfaces**: New exported variable:
  ```go
  var Descriptor = conduit.Descriptor{
      Name:        "HTTP",
      Description: "HTTP conduit with embedded web chat UI",
      Capabilities: []conduit.Capability{
          conduit.CapEventSource,
          conduit.CapShowStatus,
          conduit.CapRenderTurn,
          conduit.CapRenderMarkdown,
      },
  }
  ```
- **Validation**: `go test ./x/conduit/http/...` passes.
- **Details**: Add the `Descriptor` variable to `x/conduit/http/handler.go` near the `Option` type and `Handler` struct (following the TUI's placement pattern). Ensure `github.com/andrewhowdencom/ore/x/conduit` is already imported (it is). The capabilities chosen match the conduit's actual behavior: it streams events via NDJSON/SSE (`CapEventSource`), shows status updates (`CapShowStatus`), renders complete turns (`CapRenderTurn`), and the web UI renders markdown via `marked.parse` (`CapRenderMarkdown`).

### Task 2: Document Standard Conduit Contract in x/conduit/doc.go
- **Goal**: Update `x/conduit/doc.go` to include a "Standard Conduit Contract" section that serves as the single reference for future conduit authors.
- **Dependencies**: None (can be done in parallel with Task 1).
- **Files Affected**: `x/conduit/doc.go`
- **New Files**: None.
- **Interfaces**: None (documentation only).
- **Validation**: `go test ./x/conduit/...` passes (doc-only change, no functional impact).
- **Details**: Append a new documentation paragraph/block to `x/conduit/doc.go` after the existing package-level comments. The contract MUST cover:
  - **Constructor**: `New(mgr *session.Manager, opts ...Option) (conduit.Conduit, error)` — functional options pattern, validates `mgr != nil`, returns a `conduit.Conduit`.
  - **Descriptor**: Each package exports `var Descriptor = conduit.Descriptor{...}` enumerating supported capabilities.
  - **Sink registration**: Conduits that maintain a persistent session connection MUST subscribe to output events (e.g., `stream.Subscribe(...)`) inside `Start()` before entering the blocking loop. Request-driven conduits MAY defer subscription to per-request handlers.
  - **Blocking Start**: `Start(ctx context.Context) error` MUST block until `ctx` is cancelled or a fatal error occurs. It MUST return a non-nil error only on fatal startup or runtime errors; clean shutdown on `ctx.Done()` should return `nil`.
  - **Graceful shutdown**: On `ctx.Done()`, the conduit MUST release resources (close channels, shutdown servers, close subscriptions) and return promptly.

### Task 3: Update docgen Tool to Include HTTP Descriptor
- **Goal**: Import `x/conduit/http` and append `http.Descriptor` to the `descriptors` slice in `cmd/docgen/main.go`.
- **Dependencies**: Task 1 (HTTP Descriptor must exist before docgen can reference it).
- **Files Affected**: `cmd/docgen/main.go`
- **New Files**: None.
- **Interfaces**: Modify the `descriptors` slice:
  ```go
  var descriptors = []conduit.Descriptor{
      tui.Descriptor,
      http.Descriptor,
  }
  ```
- **Validation**: `go build ./cmd/docgen` succeeds.
- **Details**: Add the import alias `"github.com/andrewhowdencom/ore/x/conduit/http"` and append `http.Descriptor` to the `descriptors` slice. Update the package comment if it mentions the explicit list of conduits.

### Task 4: Add HTTP Descriptor Tests
- **Goal**: Verify the HTTP `Descriptor` is exported and has valid, non-empty fields.
- **Dependencies**: Task 1.
- **Files Affected**: `x/conduit/http/handler_test.go`
- **New Files**: None.
- **Interfaces**: New test function:
  ```go
  func TestDescriptor(t *testing.T) {
      assert.NotEmpty(t, Descriptor.Name)
      assert.NotEmpty(t, Descriptor.Description)
      assert.NotEmpty(t, Descriptor.Capabilities)
  }
  ```
- **Validation**: `go test ./x/conduit/http/...` passes.
- **Details**: Add the test to the existing `handler_test.go` file. Use the existing `assert` import (already present via testify). Keep the test minimal: verify Name is non-empty, Description is non-empty, and Capabilities slice is non-empty.

### Task 5: Regenerate Capability Matrix
- **Goal**: Run `cmd/docgen` to update `docs/conduit-capabilities.md` with the HTTP conduit column.
- **Dependencies**: Task 2, Task 3.
- **Files Affected**: `docs/conduit-capabilities.md`
- **New Files**: None.
- **Interfaces**: None.
- **Validation**: The generated file contains an "HTTP" column alongside "TUI".
- **Details**: Run `go run ./cmd/docgen -out docs/conduit-capabilities.md`. Verify the output includes the HTTP column with the correct capability checkmarks.

### Task 6: Run Full Test Suite with Race Detection
- **Goal**: Ensure all changes leave the repository in a healthy state.
- **Dependencies**: Task 1, Task 2, Task 3, Task 4, Task 5.
- **Files Affected**: None (validation only).
- **New Files**: None.
- **Interfaces**: None.
- **Validation**: `go test -race ./...` passes with zero failures.
- **Details**: Execute `go test -race ./...` from the repository root. If any test fails, investigate and fix. The `-race` flag is required per project conventions (`AGENTS.md`).

## Dependency Graph

- Task 1 → Task 3
- Task 1 → Task 4
- Task 1 || Task 2 (parallelizable)
- Task 3 → Task 5
- Task 2 → Task 5
- Task 1, Task 2, Task 3, Task 4, Task 5 → Task 6

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Capability selection for HTTP Descriptor is debatable (e.g., whether to include `CapAcceptText`) | Low | Medium | Match the TUI pattern for consistency; capabilities can be revised in a follow-up issue if needed. |
| docgen fails to build after adding HTTP import | Low | Low | The import path is known and the package exists; `go build ./cmd/docgen` in Task 3 validates this. |
| Existing tests have implicit assumptions about the number of conduits | Low | Low | No tests count conduits; the only consumer is `cmd/docgen` which explicitly lists them. |
| Documentation wording in `doc.go` becomes stale as new conduits are added | Low | Medium | The contract is phrased as general guidance ("MUST"/"MAY") rather than an exhaustive list, making it resilient to new conduit types. |

## Validation Criteria

- [ ] `x/conduit/http/handler.go` exports a non-empty `Descriptor` variable.
- [ ] `x/conduit/doc.go` contains a "Standard Conduit Contract" section covering constructor, Descriptor, sink registration, blocking Start, and graceful shutdown.
- [ ] `cmd/docgen/main.go` imports `x/conduit/http` and includes `http.Descriptor` in the `descriptors` slice.
- [ ] `x/conduit/http/handler_test.go` contains a `TestDescriptor` that passes.
- [ ] `docs/conduit-capabilities.md` contains an "HTTP" column.
- [ ] `go test -race ./...` passes with zero failures.
- [ ] `go build ./cmd/docgen` succeeds.
