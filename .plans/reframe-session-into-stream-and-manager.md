# Plan: Reframe Session Package into Stream and Manager Primitives

## Objective
Reframe the `session` package to separate the per-session interaction primitive (`Stream`) from the session registry/factory (`Manager`). This breaks the `session → conduit` dependency by moving ingress event types into `session`, unblocking the planned move of `conduit` to `x/conduit` (#93). All consumers — TUI, HTTP handler, examples, and code-generation templates — are updated to use the new `*session.Stream` concrete type.

## Context

The `session.Manager` type currently conflates two distinct responsibilities in a single struct:

1. **Per-session interaction model** — `Process`, `Subscribe`, `Cancel`, `Close` (methods on the unexported `managedSession` type, exposed through the `Session` interface).
2. **Session registry/factory** — `Create`, `Attach`, `List`, `Get`, `Store`, `Check`, `Close(sessionID)`.

This conflation forces `session` to import `conduit` solely for `conduit.Event`, `conduit.UserMessageEvent`, and `conduit.InterruptEvent`. The event vocabulary belongs to the session interaction model, not to I/O frontends.

### Files Discovered and Read

| File | Role | Change Required |
|---|---|---|
| `session/manager.go` | Contains `Manager` struct + unexported `managedSession` | Rename `managedSession` → `Stream`; update `Manager` return types |
| `session/session.go` | Defines `Session` interface | **Delete** (replaced by `*Stream`) |
| `session/doc.go` | Package documentation | Rewrite to document `Stream` + `Manager` |
| `session/manager_test.go` | Tests for `Manager` and `managedSession` methods | Update variable types; rename test functions |
| `session/session_test.go` | Tests for `Session` interface | **Rename** → `session/stream_test.go`; update to test `*Stream` |
| `conduit/event.go` | Defines `Event`, `UserMessageEvent`, `InterruptEvent` | **Delete** (moved to `session`) |
| `conduit/conduit.go` | Capability metadata (`Capability`, `Descriptor`) | Update package doc comment |
| `conduit/tui/tui.go` | TUI constructor (accepts `session.Session`) | Accept `*session.Stream`; update event types |
| `conduit/tui/model.go` | TUI Bubble Tea model | Use `session.Event` for event channel |
| `conduit/tui/model_test.go` | TUI model tests | Use `session.Event` / `session.UserMessageEvent` |
| `conduit/tui/tui_test.go` | TUI tests | Use `session.Event` |
| `conduit/http/handler.go` | HTTP handler (uses `conduit.UserMessageEvent`) | Use `session.UserMessageEvent`; update session variables to `*Stream` |
| `conduit/http/handler_test.go` | HTTP handler tests | Use `session.UserMessageEvent`; update session variables |
| `conduit/http/doc.go` | HTTP package docs | Update references to `session.Session` |
| `examples/tui-chat/main.go` | TUI example application | Use `*session.Stream` |
| `examples/http-chat/main.go` | HTTP example application | **No changes** (passes `*session.Manager` unchanged) |
| `cmd/forge/templates/main.go.tmpl` | Code-generation template for `forge` CLI | Use `*session.Stream` in TUI template path |
| `cmd/docgen/main.go` | Capability matrix generator | **No changes** (only uses `conduit.Descriptor` / `conduit.Capability`) |

### Architectural Decision

There is essentially one viable path: follow the issue proposal exactly. The `managedSession` struct already encapsulates all per-session state (`loop.Step`, `thread.Thread`, `TurnProcessor`, `provider.Provider`). Exporting it as `session.Stream` and deleting the `Session` interface is a direct structural rename with minimal logic changes. No alternative architecture (e.g., keeping the interface, adding a wrapper) is justified because the issue already selected the concrete-type approach for simplicity and to eliminate an unnecessary abstraction layer.

## Requirements

1. `session.Stream` is an exported concrete type owning `loop.Step`, `thread.Thread`, `TurnProcessor`, and `provider.Provider`.
2. `session.Stream` provides `Process(ctx, Event)`, `Subscribe(kinds...)`, `Cancel()`, `Close()`, and `ID()`.
3. `session.Manager` retains only factory/registry methods: `Create()`, `Attach()`, `List()`, `Get()`, `Store()`, `Check()`, `Close(sessionID)`.
4. `Manager.Create()`, `Manager.Attach()`, and `Manager.Get()` return `*session.Stream`.
5. `Manager.List()` returns `[]*session.Stream`.
6. Event types `Event`, `UserMessageEvent`, `InterruptEvent` live in `session/` and are imported by frontends, not exported by `conduit`.
7. The `session.Session` interface is removed entirely.
8. `conduit` package retains only capability metadata (`Capability`, `Descriptor`).
9. All tests pass after each task.
10. `go test -race ./...` passes after each task.

## Task Breakdown

### Task 1: Move Event Types from `conduit` to `session` and Update All References
- **Goal**: Relocate `Event`, `UserMessageEvent`, and `InterruptEvent` from `conduit/event.go` to `session/event.go`, then update every consumer reference so the `session → conduit` dependency is broken.
- **Dependencies**: None.
- **Files Affected**:
  - `session/manager.go` — change `conduit.Event` → `session.Event`, `conduit.UserMessageEvent` → `session.UserMessageEvent`, `conduit.InterruptEvent` → `session.InterruptEvent`; remove `github.com/andrewhowdencom/ore/conduit` import
  - `session/session.go` — change `conduit.Event` → `session.Event`; remove `conduit` import
  - `session/manager_test.go` — change `conduit.UserMessageEvent` → `session.UserMessageEvent`; remove `conduit` import
  - `session/session_test.go` — change `conduit.UserMessageEvent` → `session.UserMessageEvent`; remove `conduit` import
  - `session/doc.go` — change `conduit.UserMessageEvent` → `session.UserMessageEvent`
  - `conduit/tui/tui.go` — change `conduit.Event` → `session.Event`, `conduit.UserMessageEvent` → `session.UserMessageEvent`, `conduit.InterruptEvent` → `session.InterruptEvent`; remove `conduit` import if no longer needed (it is still needed for `conduit.Descriptor`)
  - `conduit/tui/model.go` — change `conduit.Event` → `session.Event`, `conduit.UserMessageEvent` → `session.UserMessageEvent`, `conduit.InterruptEvent` → `session.InterruptEvent`; remove `conduit` import
  - `conduit/tui/model_test.go` — change all `conduit.Event` → `session.Event`, `conduit.UserMessageEvent` → `session.UserMessageEvent`; remove `conduit` import
  - `conduit/tui/tui_test.go` — change `conduit.Event` → `session.Event`; remove `conduit` import
  - `conduit/http/handler.go` — change `conduit.UserMessageEvent` → `session.UserMessageEvent`; remove `conduit` import
  - `conduit/http/handler_test.go` — change `conduit.UserMessageEvent` → `session.UserMessageEvent`; remove `conduit` import
  - `conduit/conduit.go` — update package doc comment to remove mention of "event types" (e.g., change "defines the event types and capability constants" to "defines capability constants")
- **New Files**:
  - `session/event.go` — copy of current `conduit/event.go` with package changed to `session`
- **Deleted Files**:
  - `conduit/event.go`
- **Interfaces**: No new interfaces; existing `Session.Process(ctx, event)` signature changes from `conduit.Event` to `session.Event`.
- **Validation**: `go test ./...` passes; `go test -race ./...` passes. The `Session` interface and all consumer signatures remain structurally identical — only the event type's package location changes, so this task is fully hermetic.
- **Details**: After this task, `session` no longer imports `conduit`. The `conduit` package still imports nothing from `session`, so there is no import cycle. The `Session` interface still exists and all consumers still use it.

### Task 2: Introduce `session.Stream`, Remove `Session` Interface, and Update All Consumers
- **Goal**: Export the per-session primitive as `session.Stream`, delete the `Session` interface, update `Manager` return types, and migrate all frontends and examples to `*session.Stream`.
- **Dependencies**: Task 1.
- **Files Affected**:
  - `session/manager.go` — rename `managedSession` → `Stream` (exported); change `Manager.sessions` from `map[string]*managedSession` to `map[string]*Stream`; update `Create()`, `Attach()`, `List()`, `Get()`, `Check()`, `Close()` to return or operate on `*Stream`; remove per-session methods from this file (they move to `stream.go`)
  - `session/stream.go` — **new file** containing the exported `Stream` struct and its methods (`Process`, `Subscribe`, `Cancel`, `Close`, `ID`); these are currently the methods on `managedSession` in `manager.go`
  - `session/session.go` — **delete** this file (the `Session` interface is obsolete)
  - `session/manager_test.go` — update all tests to work with `*Stream` return values; rename test functions referencing `Session` to `Stream` (e.g., `TestSession_Process_Closed` → `TestStream_Process_Closed`)
  - `session/session_test.go` — **rename** to `session/stream_test.go`; update `TestSession_Interface` to `TestStream_Interface` or equivalent; test the concrete `*Stream` type directly
  - `session/doc.go` — rewrite package documentation to describe `Stream` as the per-session primitive and `Manager` as the factory/registry
  - `conduit/tui/tui.go` — change constructor signature from `func New(sess session.Session) *TUI` to `func New(stream *session.Stream) *TUI`; update internal references from `sess` to `stream`; update package and function doc comments that mention `session.Session`
  - `conduit/tui/model.go` — no structural changes needed (event types already updated in Task 1), but verify `eventsCh` channel type is `session.Event`
  - `conduit/http/handler.go` — change all local `session.Session` variable declarations to `*session.Stream` (`createSession`, `sendMessage`, `sessionEvents` handlers); update method calls from `sess.Process` / `sess.Subscribe` / `sess.Cancel` to `stream.Process` / `stream.Subscribe` / `stream.Cancel`
  - `conduit/http/doc.go` — update doc comments that mention `session.Session` to mention `*session.Stream` (e.g., "exposing session.Session conversation primitives" → "exposing session.Stream conversation primitives")
  - `conduit/http/handler_test.go` — update `session.Session` variable declarations to `*session.Stream`
  - `examples/tui-chat/main.go` — change `var sess session.Session` to `var stream *session.Stream`; update `mgr.Create()` / `mgr.Attach()` result handling; update `tui.New(sess)` to `tui.New(stream)`
  - `cmd/forge/templates/main.go.tmpl` — in the TUI template path (`{{else}}` branch), change `var sess session.Session` to `var stream *session.Stream`; update `tui.New(sess)` to `tui.New(stream)`
- **New Files**:
  - `session/stream.go`
- **Deleted Files**:
  - `session/session.go`
- **Interfaces**: The `Session` interface is **deleted**. Its method set is now the public API surface of the concrete `*session.Stream` type.
- **Validation**: `go test ./...` passes; `go test -race ./...` passes; `go build ./examples/...` passes.
- **Details**: This is an atomic refactor across multiple packages. The `Session` interface cannot be removed incrementally because it is the common contract between `session` and `conduit`. By renaming `managedSession` to `Stream`, exporting it, and updating every consumer in a single commit, the codebase remains buildable throughout. The HTTP example (`examples/http-chat/main.go`) requires no changes because it passes `*session.Manager` to `httpc.NewHandler`, and the handler internally obtains `*session.Stream` instances per-request — that is already the intended design.

## Dependency Graph

- Task 1 → Task 2

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Import cycle created between `session` and `conduit` if event aliases are kept in `conduit` | High | Low | Task 1 deletes `conduit/event.go` entirely and updates all consumers to import `session` for events. No aliases are retained. |
| `conduit/tui` or `conduit/http` tests break due to subtle type-assertion differences between interface and concrete pointer | Medium | Low | Task 2 includes updating all test files. `go test -race ./...` is required in validation. The `Stream` methods are identical to the old `managedSession` methods, so behavior should be unchanged. |
| `cmd/forge` template generation breaks silently because `main.go.tmpl` compiles into generated code that may not be exercised in CI | Medium | Medium | Add `go build` of a generated project to validation criteria, or at minimum verify that `go run ./cmd/forge` + building the output produces no compile errors. |
| External consumers of the `session.Session` interface (outside this repo) break | Low | Low | Not applicable — this is a framework under active development. The AGENTS.md explicitly states: "prefer aggressive refactoring". No backwards-compatibility guarantees are made at this stage. |

## Validation Criteria

- [ ] `go test ./...` passes after Task 1.
- [ ] `go test -race ./...` passes after Task 1.
- [ ] `go test ./...` passes after Task 2.
- [ ] `go test -race ./...` passes after Task 2.
- [ ] `go build ./examples/...` passes after Task 2.
- [ ] `session` package no longer imports `github.com/andrewhowdencom/ore/conduit` (verified by `grep` or `go list -deps ./session/...`).
- [ ] `conduit` package no longer defines event types (verified by `grep` in `conduit/event.go` — file should not exist).
- [ ] `cmd/docgen/main.go` still compiles and runs without changes (it only depends on `conduit.Descriptor` / `conduit.Capability`).
