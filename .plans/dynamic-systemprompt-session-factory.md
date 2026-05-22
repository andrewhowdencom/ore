# Plan: Dynamic System Prompt with Session-Aware Step Factory

## Objective

Enable dynamic system prompts that can change mid-session (e.g., via tool calls) and persist across session restarts. Achieve this by making the `systemprompt` transform evaluate content lazily via a function, and by giving the `session.Manager` step factory access to the `*thread.Thread` so transforms can bind to session-scoped persistent state (stored in `thread.Metadata`).

## Context

- `x/systemprompt/systemprompt.go` currently stores a static `content string` and injects it as a `RoleSystem` virtual turn on every `Transform`. There is no way to re-evaluate the content between turns.
- `session.Manager` (root module) creates `loop.Step` instances via a zero-argument factory `newStep func() (*loop.Step, error)`. The factory has no knowledge of which `thread.Thread` the step will serve, so it cannot create thread-specific transforms.
- `thread.Thread` has a `Metadata map[string]string` field that is serialized by both `MemoryStore` and `JSONStore`, making it the only session-scoped persistent state bag in the framework.
- The project conventions favor aggressive refactoring over backward compatibility. `WithContent` will be replaced entirely with `WithContentFunc`.
- All `x/conduit/*` submodules (`http`, `slack`, `telegram`, `tui`) import the root module via `replace` directives, so a flag-day signature change across all modules is feasible.

## Architectural Blueprint

Selected architecture (Framing B-lite):

1. Replace `x/systemprompt`'s static `WithContent` with `WithContentFunc(func() string)`. The func is evaluated on every `Transform` call, enabling mid-session persona changes.
2. Change `session.Manager`'s step factory signature from `func() (*loop.Step, error)` to `func(*thread.Thread) (*loop.Step, error)`. This gives the factory the thread identity needed to create session-specific transforms (e.g., closing over `thread.Metadata` in a `WithContentFunc` closure).
3. `thread.Metadata` is the canonical location for session-scoped persistent configuration. Applications like workshop can store `thread.Metadata["workshop.role"]` and have the factory bind a `WithContentFunc` that reads it.

Rejected alternatives:

- **Framing A** (application-managed state with unchanged Manager): fails for multi-session apps because the zero-argument factory cannot create per-session closures.
- **Changing the `Transform` interface** to receive thread metadata: would bloat the interface for all transforms, most of which don't need session awareness.
- **Adding a separate `SessionContext` abstraction**: over-engineered for the current need; `thread.Thread` already carries the required identity and persistence.

## Requirements

1. `x/systemprompt` must support dynamic content evaluation via `WithContentFunc(func() string)`.
2. `x/systemprompt.WithContent` must be removed (replaced entirely by `WithContentFunc`).
3. `session.Manager`'s step factory must receive `*thread.Thread` so it can create session-aware steps.
4. All call sites of `NewManager` and inline factory definitions across the root module and all `x/conduit/*` submodules must be updated.
5. Existing tests and examples must continue to compile and pass.

## Task Breakdown

### Task 1: Replace WithContent with WithContentFunc in x/systemprompt
- **Goal**: Replace the static `WithContent` option with `WithContentFunc(func() string)` and ensure content is re-evaluated on every `Transform`.
- **Dependencies**: None.
- **Files Affected**:
  - `x/systemprompt/systemprompt.go`
  - `x/systemprompt/systemprompt_test.go`
  - `x/systemprompt/doc.go`
- **New Files**: None.
- **Interfaces**:
  - Remove: `func WithContent(content string) Option`
  - Add: `func WithContentFunc(fn func() string) Option`
  - `Transform` struct field changes from `content string` to `contentFunc func() string`
  - `Transform.Transform` calls `t.contentFunc()`; if no option provided, default to `func() string { return "" }`
- **Validation**:
  - `go test -race ./x/systemprompt/...` passes in root module.
- **Details**:
  1. Update `config` struct to hold `contentFunc func() string`.
  2. Remove `WithContent`. Add `WithContentFunc`.
  3. In `New()`, if `contentFunc` is nil, set it to a func returning `""`.
  4. Update `Transform.Transform` to call `t.contentFunc()`.
  5. Update `x/systemprompt/systemprompt_test.go`: replace `WithContent("...")` with `WithContentFunc(func() string { return "..." })` in existing tests. Add a new test `TestTransform_DynamicContent` that uses a closure capturing a mutable variable, calls `Transform` twice, and asserts the content changes between invocations.
  6. Update `x/systemprompt/doc.go` example to use `WithContentFunc`.

### Task 2: Change session.Manager step factory signature and update all call sites
- **Goal**: Change `newStep` from `func() (*loop.Step, error)` to `func(*thread.Thread) (*loop.Step, error)`, update `NewManager` and all call sites across root module and all submodules.
- **Dependencies**: None (mechanically independent from Task 1).
- **Files Affected**:
  - Root: `session/manager.go`, `session/manager_test.go`, `session/stream_test.go`, `session/doc.go`
  - Root: `examples/http-chat/main.go`, `examples/tui-chat/main.go`, `examples/single-turn-cli/main.go`
  - Submodule: `x/conduit/http/handler_test.go`
  - Submodule: `x/conduit/slack/events_test.go`, `x/conduit/slack/slack_test.go`, `x/conduit/slack/thread_test.go`
  - Submodule: `x/conduit/telegram/telegram_test.go`
  - Submodule: `x/conduit/tui/tui_test.go`
- **New Files**: None.
- **Interfaces**:
  - `session.Manager.newStep` field type: `func(*thread.Thread) (*loop.Step, error)`
  - `session.NewManager` parameter: `newStep func(*thread.Thread) (*loop.Step, error)`
  - `session.Create()` calls `m.newStep(thr)`
  - `session.Attach()` calls `m.newStep(thr)`
- **Validation**:
  - `go test -race ./session/...` passes in root module.
  - `go build ./examples/http-chat` and `go build ./examples/tui-chat` succeed in root module.
  - `go test -race ./...` passes in `x/conduit/http/`
  - `go test -race ./...` passes in `x/conduit/slack/`
  - `go test -race ./...` passes in `x/conduit/telegram/`
  - `go test -race ./...` passes in `x/conduit/tui/`
- **Details**:
  1. In `session/manager.go`: update `newStep` field type, `NewManager` parameter, and both `Create()` and `Attach()` call sites to pass `thr`.
  2. In `session/doc.go`: update example factory signature.
  3. In `session/manager_test.go` and `session/stream_test.go`: bulk-replace all `func() (*loop.Step, error) { return loop.New(), nil }` with `func(*thread.Thread) (*loop.Step, error) { return loop.New(), nil }`. Also update named factory variables (e.g., `failingFactory`).
  4. In `examples/http-chat/main.go`: update `stepFactory` to `func(thr *thread.Thread) (*loop.Step, error)`.
  5. In `examples/tui-chat/main.go`: update `stepFactory` similarly.
  6. In `examples/single-turn-cli/main.go`: update the commented `systemprompt` example to use `WithContentFunc`.
  7. In each submodule test file (`x/conduit/http/handler_test.go`, `x/conduit/slack/*_test.go`, `x/conduit/telegram/telegram_test.go`, `x/conduit/tui/tui_test.go`): apply the same mechanical replacement. Ensure `thread` package is imported where needed (most files already import it via `session.NewManager`'s `thread.Store` parameter).

## Dependency Graph

- Task 1 || Task 2 (Tasks 1 and 2 are parallelizable — they touch disjoint file sets and are mechanically independent)

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Missing a call site in submodule tests | High | Medium | Use `grep -rn "func() (\*loop.Step, error)"` across the entire repo before committing to verify no stale signatures remain. |
| `thread` package not imported in some submodule test files after signature change | Medium | Low | Go compiler will catch missing imports immediately; fix any that surface during `go test`. |
| `WithContentFunc` nil pointer panic if no option provided | Medium | Low | Default `contentFunc` to `func() string { return "" }` in `New()`. Covered by existing `TestTransform_EmptyContent`. |
| Large mechanical refactor in submodule tests | Low | High | Use bulk find/replace (`sed` or editor multi-cursor) for the repetitive `func() (*loop.Step, error)` → `func(*thread.Thread) (*loop.Step, error)` change. |

## Validation Criteria

- [ ] `go test -race ./x/systemprompt/...` passes in root module.
- [ ] `go test -race ./session/...` passes in root module.
- [ ] `go build ./examples/http-chat` and `go build ./examples/tui-chat` succeed in root module.
- [ ] `go test -race ./...` passes in `x/conduit/http/`, `x/conduit/slack/`, `x/conduit/telegram/`, `x/conduit/tui/`.
- [ ] `grep -rn "WithContent(" --include="*.go"` returns no matches in `x/systemprompt/` (except plan/historical docs).
- [ ] `grep -rn "func() (\*loop.Step, error)" --include="*.go"` returns no matches in `session/`, `examples/`, or `x/conduit/`.
