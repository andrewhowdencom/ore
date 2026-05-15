# Plan: Standardize Conduit Interface and Refactor HTTP + TUI

## Objective

Add a `conduit.Conduit` interface to the root `conduit/` package and refactor both `conduit/http/` and `conduit/tui/` to implement it. This absorbs all server lifecycle (HTTP) and session lifecycle (TUI) wiring into each conduit's `Start(ctx context.Context) error` method, producing a uniform `New → Start` pattern across all ore frontends. All examples, tests, and the forge template are updated to use the new API.

## Context

Issue #99 established a clean `session.Stream` / `session.Manager` split. The `conduit/` package currently contains only `Capability`, `Descriptor`, and `Event` types — there is no common interface contract that all conduits must satisfy. Issue #96 (Forge multi-conduit orchestration) requires this stable interface to generate agent binaries from a blueprint that references arbitrary conduits.

Issue #93 (package moves to `x/conduit/`) has **not** been completed — no `x/` directory exists. All work happens in the current locations.

**Current state of relevant files:**
- `conduit/conduit.go`: Defines `Capability`, `Descriptor`, `Event`, `UserMessageEvent`, `InterruptEvent`. No `Conduit` interface.
- `conduit/http/handler.go`: `Handler` struct with `NewHandler(mgr *session.Manager, opts ...Option) *Handler` and `ServeMux()`. Application code manually creates `http.Server` and calls `ListenAndServe()`.
- `conduit/http/doc.go`: Documents `NewHandler`, `WithUI()`, and `ServeMux()`.
- `conduit/http/handler_test.go`: 30+ calls to `NewHandler()`; tests verify routing, session CRUD, NDJSON/SSE streaming, and UI static files.
- `conduit/tui/tui.go`: `TUI` struct with `New(sess session.Session) *TUI` and `Run() error`. Immediately subscribes to session output and spawns goroutines in the constructor.
- `conduit/tui/tui_test.go`: `TestNew` and `TestNew_Events` pass a `session.Session` to `tui.New()`.
- `examples/http-chat/main.go`: Manually creates `http.Server`, mounts `handler.ServeMux()`, calls `server.ListenAndServe()`.
- `examples/tui-chat/main.go`: Manually parses `--thread` flag, calls `mgr.Attach()` or `mgr.Create()`, then `tui.New(sess)` and `s.Run()`.
- `cmd/forge/templates/main.go.tmpl`: Generates the same manual composition patterns for both HTTP and TUI.
- `cmd/forge/generate_test.go` / `cmd/forge/cmd_generate_test.go`: Assert that generated HTTP code contains `"net/http"` and generated TUI code contains `"flag"`.
- `session/doc.go`: Documents old API examples (`httpc.NewHandler`, `tui.New(sess)`).

Per `AGENTS.md`, aggressive refactoring is preferred over backward compatibility at this stage.

## Architectural Blueprint

Introduce a single `Conduit` interface at the framework level:

```go
type Conduit interface {
    Start(ctx context.Context) error
}
```

Both `conduit/http/` and `conduit/tui/` refactor to implement this interface, eliminating all manual lifecycle wiring from application code:

- **HTTP Conduit**: `New(mgr *session.Manager, opts ...Option) (conduit.Conduit, error)` stores the manager and configuration. `Start(ctx)` internally creates an `http.Server`, starts `ListenAndServe()` in a goroutine, blocks until `ctx` is cancelled or the server errors, and performs graceful shutdown on context cancellation. A new `WithAddr(addr string)` functional option configures the listen address (default: `:8080`). `ServeMux()` remains exported for table-driven unit tests.

- **TUI Conduit**: `New(mgr *session.Manager, opts ...Option) (conduit.Conduit, error)` stores the manager and configuration. `Start(ctx)` internally calls `mgr.Attach()` or `mgr.Create()`, creates the Bubble Tea program and model, subscribes to the session's output stream, spawns goroutines for event processing, and blocks until the user quits or `ctx` is cancelled. A new `WithThreadID(id string)` functional option replaces the manual `--thread` flag handling in application code; when empty, `Start` calls `mgr.Create()` and logs the new thread ID.

This produces a uniform pattern for all ore frontends:

```go
// HTTP
c, err := httpc.New(mgr, httpc.WithUI(), httpc.WithAddr(":"+port))
if err != nil { return err }
return c.Start(ctx)

// TUI
c, err := tui.New(mgr, tui.WithThreadID(threadID))
if err != nil { return err }
return c.Start(ctx)
```

## Requirements

1. Add `Conduit` interface to `conduit/conduit.go` with `Start(ctx context.Context) error`.
2. Refactor `conduit/http/`: rename `NewHandler` to `New`, return `(conduit.Conduit, error)`, add `WithAddr` option, implement `Start(ctx)` with internal `http.Server` lifecycle, keep `ServeMux()` for tests.
3. Refactor `conduit/tui/`: change `New` to accept `*session.Manager`, return `(conduit.Conduit, error)`, add `WithThreadID` option, move all initialization from `New` to `Start(ctx)`, remove `Run()`.
4. Update `conduit/http/handler_test.go` and `conduit/tui/tui_test.go` for new signatures.
5. Update `conduit/http/doc.go` and `session/doc.go` to reflect new APIs.
6. Update `examples/http-chat/main.go` and `examples/tui-chat/main.go` to use `New → Start` pattern.
7. Update `cmd/forge/templates/main.go.tmpl` for new API (both HTTP and TUI branches).
8. Update `cmd/forge/generate_test.go` and `cmd/forge/cmd_generate_test.go` assertions about generated code imports.
9. All tests pass (`go test -race ./...`) and all packages build (`go build ./...`).

## Task Breakdown

### Task 1: Add `Conduit` Interface to Root Package
- **Goal**: Add the `Conduit` interface to `conduit/conduit.go` so downstream packages can target it.
- **Dependencies**: None.
- **Files Affected**: `conduit/conduit.go`
- **New Files**: None.
- **Interfaces**: 
  ```go
  type Conduit interface {
      Start(ctx context.Context) error
  }
  ```
- **Validation**: `go test ./conduit/...` passes.
- **Details**: Add `import "context"` to `conduit/conduit.go`. Define the `Conduit` interface. No other code in the repo references this interface yet, so this task is safe and independent.

### Task 2: Refactor HTTP Conduit Package and Example
- **Goal**: Make `conduit/http/` implement `conduit.Conduit` by absorbing server creation into `Start(ctx)`, and update the HTTP example.
- **Dependencies**: Task 1.
- **Files Affected**:
  - `conduit/http/handler.go`
  - `conduit/http/doc.go`
  - `conduit/http/handler_test.go`
  - `examples/http-chat/main.go`
  - `session/doc.go`
- **New Files**: None.
- **Interfaces**:
  - `New(mgr *session.Manager, opts ...Option) (conduit.Conduit, error)` — replaces `NewHandler`
  - `Start(ctx context.Context) error` — creates `http.Server`, starts `ListenAndServe()` in goroutine, blocks until `ctx.Done()` or server error, gracefully shuts down on cancellation
  - `WithAddr(addr string) Option` — configures listen address [inferred]
- **Validation**:
  - `go test ./conduit/http/...` passes
  - `go build ./examples/http-chat` passes
- **Details**:
  1. In `conduit/http/handler.go`: add `addr string` field to `Handler` struct. Add `WithAddr(addr string) Option`. Rename `NewHandler` to `New`, change return type to `(conduit.Conduit, error)`. Add `Start(ctx)` method that creates an `http.Server` with `h.addr` and `h.ServeMux()`, starts it in a goroutine, waits for `ctx.Done()` or server error, and calls `server.Shutdown()` on context cancellation. Import `"context"` as needed.
  2. In `conduit/http/doc.go`: update API documentation to reference `New`, `WithUI()`, `WithAddr()`, and `Start(ctx)`.
  3. In `conduit/http/handler_test.go`: add a `newTestHandler` helper that calls `New(mgr, opts...)` and type-asserts to `*Handler` so existing tests can continue calling `h.ServeMux()`. Update `TestNewHandler` → `TestNew` to assert the returned `conduit.Conduit` is non-nil.
  4. In `examples/http-chat/main.go`: remove `http.Server` manual creation. Use `c, err := httpc.New(mgr, httpc.WithUI(), httpc.WithAddr(":"+port))`, then `return c.Start(ctx)` with a cancellable context (`signal.NotifyContext`). Add `"os/signal"` import.
  5. In `session/doc.go`: update the HTTP conduit example in the doc comment to use `New` and `Start`.

### Task 3: Refactor TUI Conduit Package and Example
- **Goal**: Make `conduit/tui/` implement `conduit.Conduit` by absorbing session creation into `Start(ctx)`, and update the TUI example.
- **Dependencies**: Task 1.
- **Files Affected**:
  - `conduit/tui/tui.go`
  - `conduit/tui/tui_test.go`
  - `examples/tui-chat/main.go`
  - `session/doc.go`
- **New Files**: None.
- **Interfaces**:
  - `New(mgr *session.Manager, opts ...Option) (conduit.Conduit, error)` — replaces `New(sess session.Session) *TUI`
  - `Start(ctx context.Context) error` — calls `mgr.Attach(threadID)` or `mgr.Create()`, creates events channel / program / model, subscribes to session output, starts goroutines, runs Bubble Tea program, blocks until quit or `ctx` cancelled
  - `WithThreadID(id string) Option` — stores thread ID for resume; empty string means create new session
- **Validation**:
  - `go test ./conduit/tui/...` passes
  - `go build ./examples/tui-chat` passes
- **Details**:
  1. In `conduit/tui/tui.go`: add `mgr *session.Manager` and `threadID string` fields to `TUI` struct. Add `WithThreadID(id string) Option`. Change `New` signature to accept `*session.Manager` and return `(conduit.Conduit, error)`. Move all initialization logic (events channel creation, program/model creation, subscription, goroutine spawning) from `New` into `Start(ctx)`. In `Start`, call `mgr.Attach(t.threadID)` if set, else `mgr.Create()` (and log the new thread ID). Start a goroutine that sends `tea.Quit()` when `ctx.Done()` fires. Remove the old `Run()` method.
  2. In `conduit/tui/tui_test.go`: update `TestNew` and `TestNew_Events` to pass a `*session.Manager` instead of `session.Session`. Assert the returned `conduit.Conduit` is non-nil.
  3. In `examples/tui-chat/main.go`: remove manual `mgr.Attach()` / `mgr.Create()` logic. Keep `--thread` flag parsing (application-level concern), but pass the value via `tui.WithThreadID(threadID)`. Use `c, err := tui.New(mgr, tui.WithThreadID(threadID))`, then `return c.Start(ctx)` with a cancellable context. Add `"context"` and `"os/signal"` imports.
  4. In `session/doc.go`: update the TUI conduit example in the doc comment to use `New` with `*session.Manager` and `Start`.

### Task 4: Update Forge Template and Tests
- **Goal**: Update the forge code generator and its tests to emit code using the new `New → Start` pattern.
- **Dependencies**: Task 2, Task 3.
- **Files Affected**:
  - `cmd/forge/templates/main.go.tmpl`
  - `cmd/forge/generate_test.go`
  - `cmd/forge/cmd_generate_test.go`
- **New Files**: None.
- **Interfaces**: None.
- **Validation**:
  - `go test ./cmd/forge/...` passes
  - `go build ./cmd/forge` passes
- **Details**:
  1. In `cmd/forge/templates/main.go.tmpl`:
     - **HTTP branch**: remove `"net/http"` import and manual `http.Server` creation. Read `PORT` from env, pass it via `httpc.WithAddr(":"+port)`. Use `c, err := httpc.New(mgr, httpc.WithUI(), httpc.WithAddr(":"+port))` and `return c.Start(ctx)`. Add `"context"` and `"os/signal"` imports.
     - **TUI branch**: remove manual `mgr.Attach()` / `mgr.Create()` block. Keep `--thread` flag parsing. Use `c, err := tui.New(mgr, tui.WithThreadID(threadID))` and `return c.Start(ctx)`. Add `"context"` and `"os/signal"` imports.
  2. In `cmd/forge/generate_test.go`: remove the `assert.Contains(t, content, "net/http")` assertion for HTTP generated code (it no longer imports `net/http`). Keep the `flag` assertion for TUI.
  3. In `cmd/forge/cmd_generate_test.go`: update `checkOut` and `checkDir` assertions for HTTP to no longer expect `"net/http"` in generated code.

### Task 5: Full Validation
- **Goal**: Verify the entire repository compiles and passes all tests with race detection.
- **Dependencies**: Task 2, Task 3, Task 4.
- **Files Affected**: None (validation only).
- **New Files**: None.
- **Validation**:
  - `go test -race ./...` passes
  - `go build ./...` passes
- **Details**: Run `go test -race ./...` and `go build ./...`. Fix any compilation or test failures. Pay special attention to:
  - `conduit/http/handler_test.go` — all table-driven tests still pass after the `NewHandler` → `New` rename
  - `conduit/tui/tui_test.go` — `TestNew` and `TestNew_Events` still pass
  - `cmd/forge/build_test.go` — the generated code compiles successfully for both HTTP and TUI conduits

## Dependency Graph

- Task 1 → Task 2
- Task 1 → Task 3
- Task 2 || Task 3 (parallelizable — HTTP and TUI refactors are independent)
- Task 2 → Task 4
- Task 3 → Task 4
- Task 4 → Task 5

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| TUI `Start` method is hard to unit-test because Bubble Tea blocks on terminal I/O | Medium | High | Keep `TestNew` minimal (assert non-nil). The Bubble Tea model and view have their own independent unit tests (`model_test.go`, `view_test.go`) that don't require `Start`. The `Start` method's logic is a thin orchestration layer with well-tested primitives underneath. |
| HTTP `Start` method with graceful shutdown may introduce subtle goroutine or shutdown timing bugs | Medium | Medium | `Start` should use `server.Shutdown()` on `ctx.Done()` and wait for `ListenAndServe` to return before returning. Test with a cancelled context in `handler_test.go` to verify it returns cleanly. |
| Generated forge code fails to compile due to template syntax errors | High | Low | `cmd/forge/build_test.go` compiles generated code for both HTTP and TUI. This integration test will catch any template error immediately. |
| `NewHandler` renamed to `New` breaks many test call sites | Low | High | Add a `newTestHandler` helper in `handler_test.go` that wraps `New` with a type assertion to `*Handler`. This keeps the diff small and avoids rewriting 30+ test lines. |
| Examples temporarily fail to compile between Task 2 and Task 3 | Medium | Low | Tasks 2 and 3 each update their respective example in the same commit as the conduit refactor, so the repo is always buildable after each task. |

## Validation Criteria

- [ ] `conduit/conduit.go` contains the `Conduit` interface with `Start(ctx context.Context) error`.
- [ ] `conduit/http/handler.go` exports `New(mgr *session.Manager, opts ...Option) (conduit.Conduit, error)` and `Handler` implements `Start(ctx)` with internal `http.Server` lifecycle.
- [ ] `conduit/http/handler_test.go` passes (`go test ./conduit/http/...`).
- [ ] `conduit/tui/tui.go` exports `New(mgr *session.Manager, opts ...Option) (conduit.Conduit, error)` and `TUI` implements `Start(ctx)` with internal session creation/subscription.
- [ ] `conduit/tui/tui_test.go` passes (`go test ./conduit/tui/...`).
- [ ] `examples/http-chat/main.go` compiles and uses `httpc.New` + `Start(ctx)`.
- [ ] `examples/tui-chat/main.go` compiles and uses `tui.New` + `Start(ctx)`.
- [ ] `cmd/forge/templates/main.go.tmpl` generates compilable code for both HTTP and TUI conduits.
- [ ] `go test -race ./...` passes with zero failures.
- [ ] `go build ./...` passes with zero compilation errors.
