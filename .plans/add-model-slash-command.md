# Add `/model <name>` slash command to set per-thread model override

## Context

PR #436 ([#435](https://github.com/andrewhowdencom/ore/issues/435)) introduced
the per-invocation model override primitive:

- `provider.ModelOption` — a `provider.InvokeOption` consumed by adapters
  (currently only the OpenAI adapter) to override the constructor model.
- `Stream.ModelOption()` — a helper on `*session.Stream` that reads
  `Thread.Metadata["provider.model"]` and returns a `provider.InvokeOption`
  (or `nil` when absent/empty).
- `Stream.SetMetadata(key, value)` — writes the key and emits a
  `loop.PropertiesEvent` so UI conduits can react.

The PR explicitly states that **slash commands** are the intended integration
point for writing the metadata key. There is currently no slash command to do
so; users must hard-code the model at provider construction. This plan adds a
`/model <name>` slash command so the model can be changed interactively
mid-session.

### Design Constraints

The slash handler signature is:

```go
type Handler func(ctx context.Context, emitter loop.Emitter, cmd Command) (Result, error)
```

The handler is invoked by `Stream.processOne` with `s.step` as the emitter
(loop step). The current `*session.Stream` is **not** in the handler's
signature. To call `stream.SetMetadata("provider.model", name)`, the handler
must have a way to reach the stream.

In the existing codebase, only one slash command captures mutable per-thread
state — the `/name` slash command from `x/tool/set_title` — but that command
intentionally does **not** persist; it only emits a transient
`PropertiesEvent`. The title is therefore lost on session resume. The same
deficiency cannot apply to `/model`, because the whole point of the override
is to survive a reload of the JSON store.

### Approach

Add a new method on the `session.Interceptor` interface that hands the
active `*session.Stream` to the slash handler. The blast radius is small
(12 test sites in `session/stream_test.go` and `session/manager_test.go`,
plus 1 production site in `x/slash/slash.go`) and the change is mechanical.

**Why not extend `loop.Emitter`?** Adding a method to `loop.Emitter` would
force every existing implementation of `loop.Emitter` (artifact handlers in
`loop.Step`, the `mockEmitter` in test files, and any third-party
implementations) to be updated. `loop.Emitter` is a hot interface used by
every artifact handler; the blast radius is too large for a single new slash
command.

**Why not pass through `Command`?** The slash registry doesn't have a
reference to the stream either — it only receives the `loop.Emitter`
(=`s.step`). Threading the stream through the registry requires a separate
plumbing fix anyway, so doing it at the interceptor level (one layer up)
removes the duplication.

**Why not widen `Command` instead?** The `Command` struct is constructed by
the registry in `Intercept`. The registry would need the stream in its
closure, which means changing the `NewRegistry()` signature. The current
"zero-config" pattern (`slash.NewRegistry()` then `Bind(...)` then
`session.WithInterceptor(slashReg)`) is convenient; adding a stream
parameter would force the manager (which already has the stream) to create
the registry, inverting the current binding order used by both `tui-chat`
and `http-chat` examples. The interceptor-level fix preserves the current
binding order.

### The change

Add the active `*session.Stream` as a fourth parameter to the
`session.Interceptor` interface. Update the registry's `Intercept` method
to thread it through to the `Command` value passed to handlers, via an
unexported field. Expose it via a public `func (c Command) Stream() *session.Stream`
accessor. Handlers that don't need it (e.g. `/name`, `/compact`) are
unaffected; only `set_model.Slash()` calls the accessor.

### Why a new package, not a method on `x/tool/set_title`

`set_title` is an artifact-handler tool. It defines a `ToolFunc` and a
`ToolDescriptor` so an LLM can call it. A model override is **not** an
artifact the LLM should ever call — it is a per-session configuration set by
the user, not a tool. Bundling it into `set_title` would conflate "thing the
LLM calls" with "thing the user calls via slash". They have different
security profiles (LLM tool calls are subject to HITL guardrails; slash
commands are direct user input), different persistence semantics (tool
results become `ToolResult` artifacts; slash commands set metadata), and
different test coverage requirements.

The new package is `x/tool/set_model` — it sits next to `x/tool/set_title` in
the same workspace module (`x/tool/`) and reuses the slash-only pattern from
`set_title.Slash()`.

## Plan

### Task 1: Extend `session.Interceptor` and `x/slash.Command` to expose the stream

- **Files Affected**:
  - `session/event.go` — add `*session.Stream` as a fourth parameter to the
    `Interceptor` interface and `InterceptorFunc` type.
  - `session/stream.go` — pass `s` to `s.interceptor.Intercept(...)` on
    line 166.
  - `x/slash/slash.go` — extend the `Registry` interface, extend the
    `Command` struct with an unexported `stream *session.Stream` field, add
    a `func (c Command) Stream() *session.Stream` accessor, and update the
    compile-time `var _ session.Interceptor = (*registry)(nil)` assertion.
  - `x/slash/slash_test.go` — update the 3 test sites that construct
    `InterceptorFunc` (in `session/stream_test.go` and
    `session/manager_test.go`) to match the new signature.
  - `session/stream_test.go`, `session/manager_test.go` — update the
    ~9 `InterceptorFunc` test sites.
- **Interfaces**:
  - `session.Interceptor` — adds `stream *session.Stream` parameter.
  - `x/slash.Registry` — `Intercept` adds `stream *session.Stream` parameter.
  - `x/slash.Command` — gains an unexported `stream *session.Stream` field
    and a public `Stream()` accessor.
- **Details**:
  - The `Command.stream` field is unexported. Test code in other packages
    that wants to build a `Command` with a stream uses a test-only helper
    in the `x/slash` package (or constructs it via the registry's
    `Intercept`).
  - Handlers that don't need the stream ignore the new field — every
    existing test in `x/slash/slash_test.go` continues to pass without
    modification, because the stream is zero-valued when not exercised.
- **Validation**: `go test -race ./session/... ./x/slash/...` passes.

### Task 2: Add `x/tool/set_model` package

- **Files Affected**:
  - `x/tool/set_model/go.mod` (new)
  - `x/tool/set_model/setmodel.go` (new)
  - `x/tool/set_model/setmodel_test.go` (new)
  - `Taskfile.yml` (register the new module in `includes:` and `validate:`)
- **Interfaces**: `func Slash() slash.Handler` returns a handler that
  - parses `cmd.Input` (trimmed)
  - if empty, returns `slash.Result{Feedback: artifact.Text{Content: "Usage: /model <name>"}}`
  - otherwise calls `cmd.Stream().SetMetadata("provider.model", name)` and
    returns `slash.Result{}` (the `PropertiesEvent` for `provider.model` is
    emitted by `SetMetadata` itself; we do not double-emit).
- **Details**:
  - Reuse the validation pattern from `set_title.emitTitle`: trim, check
    non-empty, no other normalization. The user is trusted; we don't second
    guess what a "model name" looks like (e.g. `gpt-4o`, `gpt-4o-mini`,
    `o1-preview`, `minimax/minimax-m3`).
  - The `MetadataKey` constant `"provider.model"` is re-declared locally
    rather than exported from `session/`. The `session` package intentionally
    keeps `ModelOption`'s implementation detail private (see PR #436 commit
    message: "Metadata key is 'provider.model', a domain-prefixed framework
    contract key"). Re-declaring avoids widening the `session/` API surface.
  - No `Tool()` function or `ToolDescriptor` — slash-only. This matches the
    security posture: the LLM cannot set its own model.
- **Validation**:
  - `go test -race ./x/tool/...` passes.
  - `TestSlash_EmptyInput_ReturnsFeedback` — empty input → feedback, no
    `SetMetadata` call, no event emitted.
  - `TestSlash_WhitespaceInput_ReturnsFeedback` — whitespace-only input →
    feedback, no `SetMetadata` call, no event emitted.
  - `TestSlash_ValidInput_SetsMetadata` — calls the handler with a stub
    stream (via the new `Stream()` accessor wired through `x/slash`) and
    asserts the metadata is set.
  - `TestSlash_TrimsInput` — leading/trailing whitespace stripped before
    being stored.
  - `TestSlash_ImplementsSlashHandler` — compile-time interface check.

### Task 3: Wire `/model` into `examples/tui-chat`

- **Files Affected**: `examples/tui-chat/main.go`
- **Details**:
  - Add `slashReg.Bind("model", "Set the model for this session", set_model.Slash())`
  - after the existing `Bind("name", ...)` call.
  - The TUI already subscribes to `"properties"` events and renders them via
    `statusMsg`; the new `provider.model` key will appear automatically in
    the status bar (if the TUI is configured to display the `model` zone,
    which the current `examples/tui-chat/main.go` already does — see line
    199: `"model": "context"`).
- **Validation**: `go build ./examples/tui-chat` succeeds; no tests require
  changes (the example has no test file).

### Task 4: Wire `/model` into `examples/http-chat`

- **Files Affected**: `examples/http-chat/main.go`
- **Details**:
  - Add the same `slashReg.Bind("model", ...)` call next to the
    `Bind("new", ...)` and `Bind("compact", ...)` calls.
  - The HTTP conduit already serializes `PropertiesEvent` to its event
    stream; the `provider.model` key will flow through to the web UI
    automatically once the web UI renders it.
- **Validation**: `go build ./examples/http-chat` succeeds.

## Tradeoffs

- **Persistence of the slash command itself**: The `/model` slash command
  only writes to thread metadata. It does **not** register a slash handler
  in the global slash registry. The user is expected to bind it in their
  application code, just like `/name`, `/new`, and `/compact`. This matches
  the existing pattern in `set_title` and avoids module-loading order
  issues with slash handler discovery.
- **No `Tool()` function**: Model override is not exposed as an LLM-callable
  tool. The LLM cannot change its own model. This is intentional: a
  self-modifying model can be a security concern, and the override is a
  user-controlled setting. If a future use case arises (e.g., routing
  queries to different models based on content), it can be added as a
  separate `route_model` tool without touching this one.
- **`Command` accessor is public, field is private**: We export
  `func (c Command) Stream() *session.Stream` rather than the field. This
  keeps the struct field unexported (so test code in other packages can't
  fabricate a `Command` with a stream and skip the registry plumbing), while
  making the read API available to anyone who needs it.
- **No automatic `/reset`/clear**: There is no `/model default` or
  `/model clear` command. To clear the override, the user closes and
  reopens the session. A `clear` subcommand can be added later if the need
  arises; this plan keeps the surface minimal.
- **Model validation is intentionally absent**: We don't validate that
  `name` is a known model for the configured provider. The provider will
  return a 4xx error from the API if the model is unknown, which is the
  right place for that check (a model the API knows about is by definition
  a model we can use). Adding a client-side allowlist would require
  per-provider knowledge and is out of scope.

## Risk

- **Single new field on `Command`**: every package that constructs a
  `Command` literal in tests (only `x/slash/slash_test.go` does this, and
  the `set_title` test in `x/tool/set_title/settitle_test.go`) must continue
  to compile. The new field is unexported and zero-valued by default, so
  these continue to work without modification. Verified by inspection.
- **Module graph**: `x/tool/set_model` imports `x/slash` and `session`.
  `x/slash` imports `session` and `loop`. No new cycles are introduced.
  `x/tool/set_model` does not import any provider adapter.
- **Taskfile registration**: forgetting to add the new module to
  `Taskfile.yml` would silently exclude it from `task validate` (see the
  `conduit` skill's gotcha #7). Mitigated by Task 3's explicit
  `go test ./x/tool/...` validation.

## Validation Checklist

- [x] `go test -race ./session/...` passes
- [x] `go test -race ./x/slash/...` passes
- [x] `go test -race ./x/tool/...` passes
- [x] `go build ./examples/tui-chat` succeeds
- [x] `go build ./examples/http-chat` succeeds
- [x] `go test -race ./...` at the repo root passes (i.e., new module
      registered in `Taskfile.yml`)
- [x] New: `TestSlash_EmptyInput_ReturnsFeedback`
- [x] New: `TestSlash_WhitespaceInput_ReturnsFeedback`
- [x] New: `TestSlash_ValidInput_SetsMetadata`
- [x] New: `TestSlash_TrimsInput`
- [x] New: `TestSlash_ImplementsSlashHandler`
- [x] New: `TestSlash_NilStream_ReturnsFeedback` (added beyond the original
      five; covers the case where the registry was bypassed in a test)
