# Plan: Merge thread/ into session/

## Objective
Consolidate the `thread/` and `session/` packages into a single unified `session/` package. Remove dead code (`Thread.Lock`/`Unlock`), fix encapsulation leaks where conduits reach through `Manager.Store()` to raw store operations, and delete the `thread/` package entirely.

## Context
- `thread/` contains `doc.go`, `store.go` (Store interface + Thread struct), `memory.go`, `json.go`, `serialize.go`, and associated test files.
- `session/` is the only core consumer of `thread/`, importing it in `session/manager.go` and `session/stream.go`.
- `Thread.Lock()` and `Thread.Unlock()` are dead code — they are only called in `thread/memory_test.go` (lines 80–85, 134–151, 238–240) and never in production code or `session/`.
- Conduits leak encapsulation:
  - `x/conduit/http/handler.go:386`: `h.mgr.Store().List()`
  - `x/conduit/slack/thread.go:19`: `store.GetBy("slack_thread_id", ...)` via `c.mgr.Store()`
  - `x/conduit/slack/thread.go:41–44`: `thr.SetMetadata(...)` + `store.Save(thr)` via raw store
  - `x/conduit/telegram/telegram.go:217–225`: constructs `&thread.Thread{}` and saves via raw store
- All `thread/` dependencies (`artifact/`, `state/`) are already imported by `session/`, so absorbing the package introduces no import cycles.

## Architectural Blueprint
Move all `thread/` types into `session/`, splitting `thread/store.go` into `session/store.go` (Store interface) and `session/thread.go` (Thread struct). Remove `Thread.Lock`, `Unlock`, `busy`, and `mu` fields. Expose store operations through `session.Manager` (`GetBy`, `GetThread`, `ListThreads`, `CreateWithID`) and `session.Stream` (`GetMetadata`, `SetMetadata`, `Save`). Remove `Manager.Store()` from the public API. Update all conduits and examples to use the new APIs. Delete the `thread/` directory once all consumers are migrated.

The only viable path is direct migration and deletion. Splitting `thread/` into a separate persistence package would reintroduce the same encapsulation leaks; keeping `thread/` as a data-only package would leave the dead code and force conduits to continue importing it directly.

## Requirements
- [ ] All `thread/` types live in `session/` with dead code removed
- [ ] `session.Manager` exposes `GetBy`, `GetThread`, `ListThreads`, `CreateWithID`
- [ ] `session.Stream` exposes `GetMetadata`, `SetMetadata`, `Save`
- [ ] `Manager.Store()` is removed from public API
- [ ] All conduits and examples compile without importing `thread/`
- [ ] `thread/` directory is deleted
- [ ] All tests pass

## Task Breakdown

### Task 1: Move thread/ types into session/ and update session internals
- **Goal**: Migrate Thread, Store, MemoryStore, JSONStore, and serialization helpers into `session/`, remove dead code, update all session files and tests.
- **Dependencies**: None
- **Files Affected**: `session/manager.go`, `session/stream.go`, `session/doc.go`, `session/manager_test.go`, `session/stream_test.go`
- **New Files**: `session/store.go`, `session/thread.go`, `session/memory.go`, `session/json.go`, `session/serialize.go`, `session/memory_test.go`, `session/json_test.go`, `session/serialize_test.go`, `session/integration_test.go`
- **Interfaces**:
  - `NewManager(store Store, prov provider.Provider, newStep func(*Thread) (*loop.Step, error), processor TurnProcessor, opts ...ManagerOption) *Manager`
  - `Stream.thread *Thread`, `Stream.store Store`
  - New `Manager` methods: `GetBy(key, value string) (*Thread, bool)`, `GetThread(id string) (*Thread, bool)`, `ListThreads() ([]*Thread, error)`, `CreateWithID(id string) (*Stream, error)`
  - New `Stream` methods: `GetMetadata(key string) (string, bool)`, `SetMetadata(key, value string)`, `Save() error`
  - `Manager.Store()` removed
- **Validation**: `go test ./session` passes
- **Details**:
  - Copy `thread/store.go` content into `session/store.go` (Store interface) and `session/thread.go` (Thread struct). Remove `Lock()`, `Unlock()`, `busy`, `mu` from Thread.
  - Copy `thread/memory.go` → `session/memory.go`, `thread/json.go` → `session/json.go`, `thread/serialize.go` → `session/serialize.go`. Update package declaration to `session`.
  - Copy test files from `thread/` into `session/` with package `session`. Remove dead-code tests: `TestThread_Lock`, `TestThread_Lock_HighContention`, and `TestMemoryStore_GetBy_ConcurrentMutation` (the latter depends on `thread.Lock()`).
  - Update `session/manager.go` to use `session.Store` and `session.Thread`. Add new Manager methods. Remove `Store()` method.
  - Update `session/stream.go` to use `session.Thread`. Add new Stream methods. `Save()` delegates to `s.store.Save(s.thread)`.
  - Update `session/manager_test.go` and `session/stream_test.go`: replace `thread.NewMemoryStore()` with `session.NewMemoryStore()`, `*thread.Thread` with `*session.Thread`, and remove the `thread` import.
  - Update `session/doc.go` to reference `session.Thread` instead of `thread.Thread`.

### Task 2: Update examples
- **Goal**: Update example applications to use `session` types instead of `thread`.
- **Dependencies**: Task 1
- **Files Affected**: `examples/http-chat/main.go`, `examples/tui-chat/main.go`
- **New Files**: None
- **Interfaces**: `session.NewMemoryStore()`, `session.NewJSONStore(dir)`, `*session.Thread` in step factory
- **Validation**: `go build ./examples/http-chat`, `go build ./examples/tui-chat`
- **Details**: Replace `thread` import with `session`. Update store construction and step factory signatures.

### Task 3: Update HTTP conduit
- **Goal**: Update HTTP conduit to use new session APIs and remove the encapsulation leak.
- **Dependencies**: Task 1
- **Files Affected**: `x/conduit/http/handler.go`, `x/conduit/http/handler_test.go`
- **New Files**: None
- **Interfaces**: `h.mgr.ListThreads()` instead of `h.mgr.Store().List()`
- **Validation**: `go test ./x/conduit/http` passes
- **Details**: Replace `thread` import. Replace `h.mgr.Store().List()` with `h.mgr.ListThreads()`. Update mock stores in tests to implement `session.Store`.

### Task 4: Update Slack conduit
- **Goal**: Update Slack conduit to use new session APIs and fix encapsulation leaks.
- **Dependencies**: Task 1
- **Files Affected**: `x/conduit/slack/thread.go`, `x/conduit/slack/slack.go`, `x/conduit/slack/events_test.go`, `x/conduit/slack/slack_test.go`, `x/conduit/slack/thread_test.go`, `x/conduit/slack/README.md`
- **New Files**: None
- **Interfaces**:
  - `resolveThread`: use `c.mgr.GetBy("slack_thread_id", slackThreadID)` instead of `store.GetBy()`, then attach via `c.mgr.Attach(thr.ID)`
  - `resolveThread`: use `stream.SetMetadata()` + `stream.Save()` instead of `thr.SetMetadata()` + `store.Save()`
  - `slack.go`: use `stream.GetMetadata("slack_thread_id")` instead of `thr.GetMetadata("slack_thread_id")`
- **Validation**: `go test ./x/conduit/slack` passes
- **Details**: Replace `thread` import. Refactor `resolveThread` to return `(*session.Stream, error)` instead of `(*session.Stream, *thread.Thread, error)`. Update all callers. Update README examples.

### Task 5: Update Telegram conduit
- **Goal**: Update Telegram conduit to use new session APIs.
- **Dependencies**: Task 1
- **Files Affected**: `x/conduit/telegram/telegram.go`, `x/conduit/telegram/telegram_test.go`, `x/conduit/telegram/README.md`
- **New Files**: None
- **Interfaces**: `c.mgr.CreateWithID(chatIDStr)` instead of manual `&thread.Thread{}` + `store.Save()`
- **Validation**: `go test ./x/conduit/telegram` passes
- **Details**: Replace `thread` import. In `getOrCreateStream`, use `c.mgr.CreateWithID(chatIDStr)` to create the thread with a deterministic ID, then call `stream.SetMetadata("telegram_chat_id", chatIDStr)` and `stream.Save()`.

### Task 6: Update stdio and TUI conduits
- **Goal**: Update stdio and TUI test files to use `session` types.
- **Dependencies**: Task 1
- **Files Affected**: `x/conduit/stdio/stdio_test.go`, `x/conduit/tui/tui_test.go`
- **New Files**: None
- **Interfaces**: `session.NewMemoryStore()`, `func(*session.Thread) (*loop.Step, error)` in step factory
- **Validation**: `go test ./x/conduit/stdio`, `go test ./x/conduit/tui` pass
- **Details**: Replace `thread` import and type references.

### Task 7: Delete thread/ package
- **Goal**: Remove the `thread/` directory and verify no references remain.
- **Dependencies**: Task 2, Task 3, Task 4, Task 5, Task 6
- **Files Affected**: All files in `thread/` (to be deleted)
- **New Files**: None
- **Interfaces**: N/A
- **Validation**: `go build ./...` passes, `go test ./...` passes, `grep -r "\"github.com/andrewhowdencom/ore/thread\"" .` returns nothing
- **Details**: Delete `thread/doc.go`, `thread/store.go`, `thread/memory.go`, `thread/json.go`, `thread/serialize.go`, `thread/integration_test.go`, `thread/json_test.go`, `thread/memory_test.go`, `thread/serialize_test.go`. Search for any remaining `thread` imports and fix.

## Dependency Graph
- Task 1 → Task 2
- Task 1 → Task 3
- Task 1 → Task 4
- Task 1 → Task 5
- Task 1 → Task 6
- Task 2, Task 3, Task 4, Task 5, Task 6 → Task 7 (Task 7 depends on all prior tasks)

## Risks & Mitigations
| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Large test refactor in session/ introduces regressions | Medium | Medium | Move thread tests verbatim first (minus dead-code tests); adapt in isolation. Run tests after every sub-change. |
| Conduit mock stores drift from Store interface | Low | Low | Compiler will catch interface mismatches; `go test` per conduit validates. |
| Package import cycles when session/ absorbs thread/ dependencies | Low | Low | thread/ only depends on `artifact/` and `state/`; session/ already imports both. No new dependencies introduced. |
| Breaking change for external consumers of `thread/` package | High | Low | This is an internal package within the ore module; no external API stability guarantee is documented. |

## Validation Criteria
- [ ] `go test ./session` passes after Task 1
- [ ] `go build ./examples/...` passes after Task 2
- [ ] `go test ./x/conduit/http` passes after Task 3
- [ ] `go test ./x/conduit/slack` passes after Task 4
- [ ] `go test ./x/conduit/telegram` passes after Task 5
- [ ] `go test ./x/conduit/stdio` and `go test ./x/conduit/tui` pass after Task 6
- [ ] `go build ./...` and `go test ./...` pass after Task 7
- [ ] No `grep -r "\"github.com/andrewhowdencom/ore/thread\"" .` results in the repo
