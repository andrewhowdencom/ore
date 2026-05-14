# Plan: Session-First Architecture Refactor

## Objective

Refactor the ore framework to make `session.Session` the narrow, conduit-facing interface between frontends and the framework, convert `session.Manager` into a factory/registry that returns `Session` handles, and remove the effectively-unused `conduit.Conduit` and `conduit.Capable` interfaces. This simplifies new conduit development by giving each frontend a small, well-defined contract instead of forcing dependence on the omnibus `*session.Manager`.

## Context

The current architecture has the following characteristics (observed from source inspection):

- **`session/session.go`** defines `Session` as a lightweight concrete struct with `ID()` and `Thread()` getters. It is not self-sufficient — all interaction logic lives on `Manager`.
- **`session/manager.go`** defines `Manager` as a ~10-method concrete type. The internal `managedSession` holds `thread`, `step`, `mu`, `busy`, and `cancel`. `Manager` owns session lookup (`map[string]*managedSession`), locking, and the full inference pipeline (`Process`, `Subscribe`, `Cancel`, `Close`, `Check`, `Get`, `List`, `Store`, `Create`, `Attach`).
- **`conduit/conduit.go`** defines `Capability`, `Capable`, `Conduit`, and `Descriptor`. `Conduit` embeds `Capable` and adds `Events() <-chan Event`. Only `conduit/tui/` implements it, and its own doc comment tells callers not to read from `Events()`.
- **`conduit/tui/tui.go`** satisfies `conduit.Conduit` and `conduit.Capable`. Its constructor is `New(mgr *session.Manager, threadID string)`. Internal goroutines call `mgr.Subscribe(threadID)`, `mgr.Process(ctx, threadID, event)`, and `mgr.Cancel(threadID)`.
- **`conduit/http/handler.go`** depends on `*session.Manager`. Per-request handlers dispatch via string session IDs (`mgr.Process(ctx, id, …)`, `mgr.Subscribe(id, …)`). Thread metadata (`Store()`, `List()`) still goes through Manager.
- **`examples/tui-chat/main.go`** creates a Manager, then passes it to `tui.New(mgr, thread.ID)`.
- **`examples/http-chat/main.go`** creates a Manager and passes it to `httpc.NewHandler(mgr)`.

Per `AGENTS.md`, aggressive refactoring is preferred over backward compatibility. No deprecation cycle is needed.

## Architectural Blueprint

### Selected Path

The issue describes a single coherent target architecture. No alternative paths were evaluated — the design is already well-specified in the issue. The implementation strategy is:

1. **Promote `managedSession` to implement `Session`** — move `Process`, `Subscribe`, `Cancel`, and `Close` from `Manager` (sessionID-string dispatch) onto `managedSession`. Add `provider`, `processor`, `store`, and `id` fields so the session is self-sufficient. Add a `closed` flag so `Subscribe` and `Process` can return errors after closure.
2. **Shrink `Manager` to factory/registry** — retain `Create`, `Attach`, `Get`, `List`, `Store`, `Check`, and `Close(id)` (registry lifecycle). Remove `Process(sessionID)`, `Subscribe(sessionID)`, `Cancel(sessionID)`. All factory methods return `Session` (the new interface).
3. **Remove `conduit.Conduit` and `conduit.Capable`** — keep `Capability` constants, `Descriptor`, and `Event` types as the lingua franca for ingress/capability discovery. Remove the `contains` helper (only used by mock `Capable` tests).
4. **Update `conduit/tui/`** — change `New(session session.Session)`. Remove `Events()`, `Capabilities()`, `Can()` methods and compile-time assertions. Internal goroutines call `session.Process()`, `session.Subscribe()`, `session.Cancel()` directly.
5. **Update `conduit/http/`** — per-request handlers (`sendMessage`, `sessionEvents`) obtain a `Session` via `mgr.Get(id)` and call `Session` methods directly. Manager remains for metadata (`Store`, `List`, `Check`, `Close(id)`).
6. **Update examples** — `examples/tui-chat` obtains a `Session` from Manager and passes it to `tui.New`. `examples/http-chat` requires no changes.

## Requirements

1. `session.Session` is a public interface with `ID()`, `Process(ctx, event)`, `Subscribe(kinds...)`, `Cancel()`, `Close()`.
2. `session.Manager` is a factory/registry: `Create()`, `Attach()`, `Get()`, `List()`, `Store()`, `Check()`, `Close(id)`.
3. `managedSession` implements `Session` and holds all state needed for a turn (step, thread, provider, processor, store, lock, cancel func, closed flag).
4. `conduit.Conduit` and `conduit.Capable` interfaces are removed from the codebase.
5. `conduit/tui/` depends on `session.Session` (not `*session.Manager`) for core interaction.
6. `conduit/http/` depends on `session.Session` for per-request interaction; Manager only for metadata/lifecycle.
7. `go test -race ./...` passes after all changes.
8. All examples compile and run.

## Task Breakdown

### Task 1: Redesign Session Package and Update HTTP Handler
- **Goal**: Promote `managedSession` to implement the new `Session` interface, shrink `Manager` to a factory/registry, and update the HTTP handler to use `Session` handles per-request.
- **Dependencies**: None.
- **Files Affected**:
  - `session/session.go` — redefine `Session` as interface, remove old struct
  - `session/manager.go` — move methods to `managedSession`, update Manager signatures
  - `session/session_test.go` — rewrite to test interface via Manager
  - `session/manager_test.go` — update all direct `mgr.Process`/`mgr.Subscribe`/`mgr.Cancel` calls to use returned `Session`
  - `conduit/http/handler.go` — use `session.Session` handles in `sendMessage`, `sessionEvents`, `createSession`
  - `conduit/http/handler_test.go` — update `TestHandler_DeleteSession` and `TestHandler_SendMessage_Concurrent` to use Session methods; update other assertions as needed
  - `conduit/http/doc.go` — update doc comments to reflect Session-first interaction
- **New Files**: None.
- **Interfaces**:
  ```go
  // session/session.go
  type Session interface {
      ID() string
      Process(ctx context.Context, event conduit.Event) error
      Subscribe(kinds ...string) (<-chan loop.OutputEvent, error)
      Cancel() error
      Close() error
  }
  ```
  ```go
  // session/manager.go — Manager factory methods return Session interface
  func (m *Manager) Create() (Session, error)
  func (m *Manager) Attach(threadID string) (Session, error)
  func (m *Manager) Get(id string) (Session, error)
  func (m *Manager) List() []Session
  ```
- **Validation**:
  - `go test -race ./session/...` passes
  - `go test -race ./conduit/http/...` passes
  - `go build ./...` passes (TUI and examples still use old signatures, which remain compatible)
- **Details**:
  1. In `session/session.go`, replace the `Session` struct with the `Session` interface (remove `Thread()` from the public contract).
  2. In `session/manager.go`:
     - Add `id string`, `provider provider.Provider`, `processor TurnProcessor`, `store thread.Store`, and `closed bool` fields to `managedSession`.
     - Move `Process`, `Subscribe`, `Cancel`, and `Close` logic from `Manager` methods onto `managedSession` methods.
     - `managedSession.Process` uses the session's own `provider`, `processor`, and `store`; includes a non-blocking busy check and turn-context cancellation.
     - `managedSession.Subscribe` checks `closed` and delegates to `step.Subscribe`.
     - `managedSession.Cancel` checks `closed` and invokes the stored cancel func.
     - `managedSession.Close` sets `closed`, then closes the step.
     - Update `Manager.Create` and `Manager.Attach` to populate the new fields on `managedSession` and return `(Session, error)`.
     - Update `Manager.Get` to return `(Session, error)`.
     - Update `Manager.List` to return `[]Session`.
     - Keep `Manager.Check` and `Manager.Close(id)`; `Manager.Close(id)` removes from map then calls `sess.Close()`.
     - Remove `Manager.Process(sessionID, …)`, `Manager.Subscribe(sessionID, …)`, and `Manager.Cancel(sessionID)`.
  3. Update `session/session_test.go` — create a Manager, get a Session from it, and assert on `ID()`, `Process()`, `Subscribe()`, `Cancel()`, `Close()`.
  4. Update `session/manager_test.go` — replace all direct `mgr.Process`/`mgr.Subscribe`/`mgr.Cancel` with calls on the `Session` handle returned by `mgr.Create()` or `mgr.Get()`. Remove `Thread()` assertions.
  5. In `conduit/http/handler.go`:
     - `createSession`: `sess` changes from `*session.Session` to `session.Session`.
     - `sendMessage`: after `h.mgr.Check(id)`, obtain session with `h.mgr.Get(id)`, then call `sess.Subscribe(req.Kinds...)` and `sess.Process(r.Context(), event)`.
     - `sessionEvents`: obtain session with `h.mgr.Get(id)`, then call `sess.Subscribe(kinds...)`.
     - `deleteSession` and `listThreads` remain unchanged (still use Manager).
  6. Update `conduit/http/handler_test.go`:
     - `TestHandler_DeleteSession`: get session before closing, then assert that `sess.Subscribe` returns an error (or closed channel) after `mgr.Close`.
     - `TestHandler_SendMessage_Concurrent`: obtain session handle, then call `sess.Process` in the goroutine.
     - Update any remaining direct `mgr.Subscribe` calls.
  7. Update `conduit/http/doc.go` to document the Session-first pattern.

### Task 2: Remove Conduit/Capable Interfaces and Refactor TUI to Session-First
- **Goal**: Remove the unused `conduit.Conduit` and `conduit.Capable` interfaces, refactor the TUI to depend on `session.Session`, and update the TUI example.
- **Dependencies**: Task 1.
- **Files Affected**:
  - `conduit/conduit.go` — remove `Capable`, `Conduit` interfaces and `contains` helper
  - `conduit/conduit_test.go` — remove `Capable`/`Conduit` tests (`TestCapable`, `TestConduitInterface`, `TestDescriptor` if it only tested those interfaces)
  - `conduit/tui/tui.go` — change `New` signature, remove `Events`/`Capabilities`/`Can`, update goroutines, update doc comments
  - `conduit/tui/tui_test.go` — remove compile-time assertions, remove `TestTUI_Events`/`TestTUI_Capabilities`/`TestTUI_Can`, update `TestNew` calls
  - `examples/tui-chat/main.go` — obtain `session.Session` from Manager before passing to `tui.New`
- **New Files**: None.
- **Interfaces**:
  ```go
  // conduit/tui/tui.go
  func New(sess session.Session) *TUI
  ```
- **Validation**:
  - `go test -race ./conduit/...` passes
  - `go build ./...` passes (including examples)
- **Details**:
  1. In `conduit/conduit.go`, delete `type Capable interface`, `type Conduit interface`, and `contains` function. Keep `Capability` constants, `Descriptor`, and `Event` types.
  2. In `conduit/conduit_test.go`, remove tests that exercise `Capable`, `Conduit`, and `contains`. Keep `Event` kind tests and `Capability` constant tests.
  3. In `conduit/tui/tui.go`:
     - Change `New(mgr *session.Manager, threadID string)` to `New(sess session.Session)`.
     - Remove `Events()`, `Capabilities()`, and `Can()` methods from the `TUI` struct.
     - Update the subscription goroutine to call `sess.Subscribe("turn_complete")` instead of `mgr.Subscribe(threadID, …)`.
     - Update the event-processing goroutine to call `sess.Process(ctx, e)` and `sess.Cancel()` instead of `mgr.Process(ctx, threadID, e)` and `mgr.Cancel(threadID)`.
     - Update package-level and struct-level doc comments to remove `conduit.Conduit` references.
  4. In `conduit/tui/tui_test.go`:
     - Remove compile-time assertions `var _ conduit.Conduit = (*TUI)(nil)` and `var _ conduit.Capable = (*TUI)(nil)`.
     - Remove `TestTUI_Events`, `TestTUI_Capabilities`, `TestTUI_Can`.
     - Update `TestNew`, `TestNew_Events`, and `TestNew_SubscribeFailure` to create a Manager, obtain a Session, and pass it to `tui.New`.
  5. In `examples/tui-chat/main.go`:
     - After creating the Manager, obtain a Session via `mgr.Attach(threadID)` (if resuming) or `mgr.Create()` (if new).
     - Pass the `Session` to `tui.New(sess)`.
     - Remove the separate `thread` variable lookup from the store (Manager's `Attach`/`Create` handles this).

### Task 3: Full Validation
- **Goal**: Run race detection and full build to confirm the refactor is complete and correct.
- **Dependencies**: Task 1, Task 2.
- **Files Affected**: None (validation only).
- **New Files**: None.
- **Validation**:
  - `go test -race ./...` passes with zero failures.
  - `go build ./...` passes with zero compilation errors.
  - `go vet ./...` is clean.
- **Details**:
  1. Run `go test -race ./...` and inspect output.
  2. Run `go build ./...` to verify all packages including examples compile.
  3. Run `go vet ./...` to catch any structural issues.
  4. If any race or compilation issue appears, debug and fix. Common risks:
     - `managedSession` lock ordering (ensure `mu` is acquired before accessing `busy`/`cancel`/`closed`).
     - `Manager` map access vs `managedSession` field access (ensure no double-locking or deadlock between `Manager.mu` and `managedSession.mu`).
     - Channel closure in `FanOut` after `Session.Close()` — ensure subscriber goroutines exit cleanly.

## Dependency Graph

- Task 1 → Task 2 (TUI refactor depends on Session interface existing)
- Task 1 || Task 2 preparation (conceptual prep can happen in parallel, but execution is sequential)
- Task 3 depends on Task 1 and Task 2

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Race conditions from moving locks from Manager to managedSession | High | Medium | Run `go test -race ./...` after every task. The current `managedSession.mu` already protects `busy`/`cancel`; extending it to `closed` and `Subscribe` is straightforward. Manager's map lock (`Manager.mu`) is only for registry access now, so the two locks are well-separated. |
| Tests asserting on `mgr.Subscribe` error behavior need semantic changes | Medium | Medium | `TestHandler_DeleteSession` previously asserted `mgr.Subscribe` returns error because the session was removed from the Manager map. After the refactor, the test must assert on `sess.Subscribe` after closure. Include explicit `closed` flag on `managedSession` so `Subscribe` returns a clear error. |
| `Thread()` getter removed from public Session contract breaks unseen callers | Low | Low | Grepped the entire tree — only `session/manager_test.go` used `sess.Thread()`. No external callers. The test assertion can be replaced with a direct store lookup. |
| `examples/tui-chat/main.go` thread lifecycle changes subtly | Low | Low | Previously the example looked up the thread from the store, then passed the ID to `tui.New(mgr, threadID)`. After the refactor, it obtains a Session directly via `mgr.Attach` or `mgr.Create`, which handles the store lookup internally. This is simpler and less error-prone. |

## Validation Criteria

- [ ] `session.Session` is an interface with `ID`, `Process`, `Subscribe`, `Cancel`, `Close`
- [ ] `session.Manager` is a factory/registry that returns `Session` handles from `Create`, `Attach`, `Get`, `List`
- [ ] `conduit/tui/` depends on `session.Session` for core interaction (not `*session.Manager`)
- [ ] `conduit/http/` depends on `session.Session` for per-request interaction; Manager only for metadata/lifecycle
- [ ] `conduit.Conduit` interface is removed from the codebase
- [ ] `conduit.Capable` interface is removed from the codebase
- [ ] `go test -race ./...` passes
- [ ] `go build ./...` passes
- [ ] Examples compile and run (`examples/tui-chat/main.go`, `examples/http-chat/main.go`)
