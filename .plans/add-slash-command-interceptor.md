# Plan: Add Slash Command Interceptor

## Objective

Add a slash command interception system that intercepts user messages in `session.Stream` before they enter the LLM inference pipeline. Commands like `/new` and `/compact` consume the event entirely (no LLM call), while unrecognized text passes through unchanged. A new `x/slash/` package provides a command registry analogous to `tool/`, and a first-class `session.SessionSwitchEvent` signals cross-session navigation to all conduits via the existing event emission API.

## Context

The codebase has the following relevant structure:

- **`session/stream.go`** defines `session.Stream`, which owns `Process()` and `processOne()`. The latter handles `UserMessageEvent` and `InterruptEvent` directly in a hardcoded switch. There is no pre-processing hook.
- **`session/manager.go`** creates `Stream` instances via functional options (`ManagerOption`). It wires `provider`, `processor`, `newStep`, and `defaultMeta` into every stream. Adding an interceptor follows the same pattern.
- **`session/event.go`** defines the `Event` interface (`Kind()`, `Context()`) and two concrete types: `UserMessageEvent` and `InterruptEvent`.
- **`loop/loop.go`** defines `loop.OutputEvent` (`Kind()`, `Context()`), `loop.Step`, and all built-in event types: `TurnCompleteEvent`, `ErrorEvent`, `LifecycleEvent`, `PropertiesEvent`, `ArtifactEvent`. The `Step.Emit()` method was exported by a previous plan (add-event-emission-api.md), which resolved the dependency on event emission.
- **`loop/handler.go`** defines the `Handler` interface for artifact handlers.
- **`x/conduit/http/types.go`** implements `MarshalOutputEvent` with a hardcoded type switch over built-in event kinds. It already has a `json.Marshaler` fallback in the `default` case, which handles custom events like `SessionSwitchEvent`.
- **`artifact/artifact.go`** defines `Artifact` and all concrete artifact types: `Text`, `TextDelta`, `Reasoning`, `ReasoningDelta`, `ToolCall`, `ToolCallDelta`, `ToolResult`, `Usage`, `Image`. None implement `json.Marshaler`.
- **`state/state.go`** defines `Turn` and `State`. `Turn` has no `json` tags.
- **`tool/tool.go`** and **`tool/registry.go`** define the `Tool`, `ToolFunc`, and `Registry` pattern — the blueprint for the new `x/slash/` package.
- **`examples/http-chat/main.go`** and **`examples/tui-chat/main.go`** compose `session.Manager`, `loop.Step`, and conduits. They are the reference applications for demonstrating slash command integration.

Project conventions from `AGENTS.md`:
- Prefer aggressive refactoring over backwards compatibility.
- Core packages (`artifact/`, `state/`, `provider/`, `loop/`) live at root level; never place framework contracts under `internal/`.
- Use `log/slog`, table-driven tests, `go test -race ./...`.
- Concrete extensions live under `x/<name>/` and implement core interfaces without importing `loop/` (unless they are conduits or handlers).

## Architectural Blueprint

### Selected Architecture

1. **`x/slash/` package** — analogous to `tool/`, provides a `Registry` with `Bind(name, handler)` and an `Intercept` method that implements `session.Interceptor`. The package is a leaf: it imports `session` but is not imported by any core package.

2. **`session.Interceptor` interface** — defined in `session` package. Receives a `session.Event` and returns `(session.Event, bool, error)` where `bool` means "consumed". Only `UserMessageEvent` is intercepted; other event kinds pass through unchanged.

3. **`session.Stream` interceptor hook** — `Stream` stores an `interceptor` field. In `processOne()`, before the event switch, the interceptor is called. If it consumes the event, `processOne` returns early with no error. If it rewrites the event, the modified event is passed to the original switch.

4. **`session.Manager` functional option** — `WithInterceptor(interceptor session.Interceptor)` is added to `ManagerOption`. The interceptor is passed to every `Stream` created by `Create()`, `Attach()`, and `CreateWithID()`.

5. **`session.SessionSwitchEvent`** — a new `loop.OutputEvent` emitted by slash command handlers (e.g., `/new`) to signal cross-session navigation. It implements `json.Marshaler` so the HTTP conduit's existing `json.Marshaler` fallback serializes it correctly. The `Ctx` field carries routing metadata (provenance, thread ID).

6. **Self-serializing framework types** — `artifact/`, `state/`, and `loop/` types are refactored to implement `json.Marshaler`. This makes the framework internally consistent: all data types know their own JSON representation, and the HTTP conduit's `MarshalOutputEvent` and `MarshalArtifact` become thin dispatchers rather than hardcoded switches.

7. **Simplified HTTP serialization** — `x/conduit/http/types.go` is refactored to dispatch all event and artifact serialization to `json.Marshaler`. The hardcoded `artifactToJSON` and event switch are removed.

### Evaluated Alternatives

| Approach | Rationale | Why Rejected |
|---|---|---|
| Interceptor at conduit level | Each conduit (TUI, HTTP) implements its own slash parsing | Violates DRY; conduits are "dumb pipes" per `AGENTS.md`. The framework should intercept at the session boundary, not the transport boundary. |
| Per-conversation command registries | Debug sessions get `/trace`, normal chats don't | Adds complexity to `Manager` (needs to decide which registry per stream). The user confirmed a single global registry is sufficient. |
| Registry pattern for HTTP marshaling | Custom events register a serializer with the HTTP conduit | More explicit than `json.Marshaler`, but introduces coupling between `x/slash/` and `x/conduit/http/`. The `json.Marshaler` fallback is already implemented and works. |
| Skip artifact/loop self-serialization | Only `SessionSwitchEvent` needs `json.Marshaler` | Leaves the architecture inconsistent. The user expressed interest in refactoring existing types for extensibility. |

## Requirements

1. A new `x/slash/` package must provide a `Registry` with `Bind(name, handler)` and `Intercept(ctx, event)`. [explicit]
2. The `Registry` must be analogous to `tool.Registry` in design (functional options, compile-time assertions, test coverage). [inferred]
3. `session.Stream` must support an `Interceptor` that runs before the LLM pipeline. [explicit]
4. `session.Manager` must accept an interceptor via `WithInterceptor()` functional option. [explicit]
5. The interceptor must only process `UserMessageEvent`; other events pass through unchanged. [inferred]
6. The interceptor must support two outcomes: consume (no LLM processing) and pass-through (continue with original or modified event). [explicit]
7. `session.SessionSwitchEvent` must be a first-class `loop.OutputEvent` with `Kind() == "session_switch"`. [explicit]
8. `session.SessionSwitchEvent` must implement `json.Marshaler` for HTTP conduit serialization. [inferred]
9. All `artifact.Artifact` types must implement `json.Marshaler` with output matching the current `x/conduit/http` wire format. [inferred from refactoring goal]
10. All `loop.OutputEvent` types must implement `json.Marshaler` with output matching the current HTTP wire format. [inferred]
11. `state.Turn` must implement `json.Marshaler` (or gain `json` tags) to support `TurnCompleteEvent` serialization. [inferred]
12. `x/conduit/http/types.go` must be refactored to dispatch serialization via `json.Marshaler` rather than hardcoded switches. [inferred]
13. `examples/http-chat` must demonstrate slash command registration (`/new`) and `session.WithInterceptor` wiring. [inferred]
14. All changes must be covered by unit tests and pass `go test -race ./...`. [convention]

## Task Breakdown

### Task 1: Add `session.Interceptor` interface and wire into `Stream`/`Manager`
- **Goal**: Define the interception primitive in `session` and hook it into the stream processing pipeline.
- **Dependencies**: None.
- **Files Affected**: `session/event.go`, `session/stream.go`, `session/manager.go`, `session/stream_test.go`, `session/manager_test.go`
- **New Files**: None.
- **Interfaces**:
  - `session.Interceptor` interface with `Intercept(ctx context.Context, event Event) (Event, bool, error)`
  - `ManagerOption` addition: `WithInterceptor(interceptor Interceptor)`
  - `Stream` field: `interceptor Interceptor`
- **Validation**:
  - `go test ./session/...` passes
  - `go test -race ./session/...` passes
  - `go build ./...` passes
- **Details**:
  - Add `Interceptor` interface to `session/event.go` (alongside `Event` interface).
  - Add `interceptor` field to `session.Stream` in `session/stream.go`.
  - In `processOne()`, before the `switch e := event.(type)` block, check `if s.interceptor != nil`. If the event is `UserMessageEvent`, call `s.interceptor.Intercept()`. If consumed (`bool == true`), return `nil` immediately. If not consumed, use the returned event in the switch.
  - Add `WithInterceptor` to `session/manager.go` as a `ManagerOption`. Pass the interceptor to `Stream` in `Create()`, `Attach()`, and `CreateWithID()`.
  - Add tests in `session/stream_test.go`:
    1. `TestStream_Interceptor_Consume` — interceptor returns `(event, true, nil)`, assert no LLM call is made.
    2. `TestStream_Interceptor_PassThrough` — interceptor returns `(event, false, nil)`, assert normal LLM pipeline runs.
    3. `TestStream_Interceptor_Rewrite` — interceptor returns a modified `UserMessageEvent`, assert the modified content is processed.
    4. `TestStream_Interceptor_Error` — interceptor returns error, assert error is propagated.
    5. `TestStream_Interceptor_NonUserMessage` — interceptor is called with `InterruptEvent`, assert it passes through unchanged.
  - Add test in `session/manager_test.go` verifying `WithInterceptor` is passed to created streams.

### Task 2: Add `session.SessionSwitchEvent`
- **Goal**: Define the first framework-level meta-event for cross-session navigation.
- **Dependencies**: None (parallelizable with Task 1).
- **Files Affected**: `session/event.go`
- **New Files**: None.
- **Interfaces**:
  - `SessionSwitchEvent struct { SessionID string; Ctx context.Context }`
  - `Kind() string` → `"session_switch"`
  - `Context() context.Context`
  - `MarshalJSON() ([]byte, error)` — produces `{"kind":"session_switch","session_id":"...","context":{...}}`
- **Validation**:
  - `go test ./session/...` passes
  - `go build ./...` passes
- **Details**:
  - Add `SessionSwitchEvent` to `session/event.go` (alongside `UserMessageEvent` and `InterruptEvent`).
  - Implement `loop.OutputEvent` via `Kind()` and `Context()`.
  - Implement `json.Marshaler`:
    - Base JSON: `{"kind":"session_switch","session_id":e.SessionID}`
    - Include context envelope if provenance exists: `{"provenance":"..."}` (use `loop.ProvenanceFrom(e.Ctx)`).
    - Do NOT include `traceparent` in `loop` — that is HTTP-specific and added by the transport layer if needed. Wait, but `MarshalOutputEvent` in HTTP will call `MarshalJSON()` and won't add traceparent. So `SessionSwitchEvent` needs to include traceparent if the context has an active span.
    - Actually, `loop` already imports `go.opentelemetry.io/otel/trace` and `go.opentelemetry.io/otel/propagation`. So `SessionSwitchEvent.MarshalJSON` can extract the traceparent and include it in the context envelope, matching the current HTTP format.
  - Add tests in `session/event_test.go` (or a new test file) verifying JSON serialization with and without context.
  - Verify that `MarshalOutputEvent` in `x/conduit/http` correctly serializes `SessionSwitchEvent` via the existing `json.Marshaler` fallback (add a test in `x/conduit/http/types_test.go` if not already covered).

### Task 3: Create `x/slash/` package with Registry and command dispatch
- **Goal**: Provide the slash command framework package, analogous to `tool/`.
- **Dependencies**: Task 1 (needs `session.Interceptor` interface), Task 2 (optional but recommended for handler examples).
- **Files Affected**: None (new package).
- **New Files**: `x/slash/slash.go`, `x/slash/slash_test.go`, `x/slash/doc.go`
- **Interfaces**:
  - `type Handler func(ctx context.Context, args []string) error`
  - `type Registry struct` with `commands map[string]Handler`
  - `func NewRegistry() *Registry`
  - `func (r *Registry) Bind(name string, handler Handler)`
  - `func (r *Registry) Intercept(ctx context.Context, event session.Event) (session.Event, bool, error)` — implements `session.Interceptor`
- **Validation**:
  - `go test ./x/slash/...` passes
  - `go test -race ./x/slash/...` passes
  - `go build ./...` passes
- **Details**:
  - `Intercept` checks if the event is `UserMessageEvent`. If not, returns `(event, false, nil)`.
  - For `UserMessageEvent`, it splits the content on whitespace to find the command. If the first token starts with `/` and matches a bound command, it calls the handler with the remaining tokens as `args`.
  - If the handler returns `nil`, the event is consumed (`bool == true`).
  - If the handler returns an error, the error is propagated.
  - If no command matches, the event passes through unchanged (`bool == false`).
  - Commands are stored without the leading `/` (e.g., `Bind("new", handler)` matches `/new`).
  - Add tests:
    1. `TestRegistry_BindAndMatch` — bind a command, intercept a matching event, assert consumed.
    2. `TestRegistry_NoMatch` — intercept a non-matching event, assert pass-through.
    3. `TestRegistry_HandlerError` — handler returns error, assert error propagated.
    4. `TestRegistry_ArgsParsing` — `/new arg1 arg2`, assert handler receives `[]string{"arg1", "arg2"}`.
    5. `TestRegistry_NonUserMessage` — intercept `InterruptEvent`, assert pass-through.
  - Follow `tool/` package style: use `sync.RWMutex` for thread safety, compile-time assertions where appropriate, and table-driven tests.

### Task 4: Refactor `artifact/`, `state/`, and `loop/` types to implement `json.Marshaler`
- **Goal**: Make all framework data types self-serializing to JSON, producing output identical to the current `x/conduit/http` wire format.
- **Dependencies**: None (parallelizable with Tasks 1-3).
- **Files Affected**: `artifact/artifact.go`, `artifact/artifact_test.go`, `state/state.go`, `state/state_test.go` (if exists), `loop/loop.go`, `loop/loop_test.go`
- **New Files**: None.
- **Interfaces**: Add `MarshalJSON() ([]byte, error)` to all artifact types, `state.Turn`, and all `loop.OutputEvent` types.
- **Validation**:
  - `go test ./artifact/... ./state/... ./loop/...` passes
  - `go test -race ./artifact/... ./state/... ./loop/...` passes
  - `go build ./...` passes
- **Details**:
  - **Artifact types** (`artifact/artifact.go`): Each type implements `MarshalJSON` producing the same JSON structure as the current `artifactToJSON` in `x/conduit/http/types.go`:
    - `Text` → `{"kind":"text","content":"..."}`
    - `TextDelta` → `{"kind":"text_delta","content":"..."}`
    - `Reasoning` → `{"kind":"reasoning","content":"..."}`
    - `ReasoningDelta` → `{"kind":"reasoning_delta","content":"..."}`
    - `ToolCall` → `{"kind":"tool_call","id":"...","name":"...","arguments":"...","display":"..."}` (display only if different from arguments)
    - `ToolCallDelta` → `{"kind":"tool_call_delta","id":"...","name":"...","arguments":"...","index":N}`
    - `ToolResult` → `{"kind":"tool_result","tool_call_id":"...","content":"...","is_error":true/false}` (use `MarkdownString()` for content)
    - `Usage` → `{"kind":"usage","prompt_tokens":N,"completion_tokens":N,"total_tokens":N}`
    - `Image` → `{"kind":"image","url":"..."}`
  - **State types** (`state/state.go`): Add `json` tags to `Turn` for lowercase field names: `Role` → `role`, `Artifacts` → `artifacts`, `Timestamp` → `timestamp,omitempty`. Alternatively, implement `MarshalJSON` on `Turn` for full control. Tags are simpler.
  - **Loop events** (`loop/loop.go`):
    - `TurnCompleteEvent` → `{"kind":"turn_complete","turn":{...},"context":{...}}` (turn serialized via `json.Marshal(e.Turn)`, context via `loop` context helper)
    - `ErrorEvent` → `{"kind":"error","message":"...","context":{...}}`
    - `LifecycleEvent` → `{"kind":"lifecycle","phase":"...","context":{...}}`
    - `PropertiesEvent` → `{"kind":"properties","properties":{...},"context":{...}}`
    - `ArtifactEvent` → merge artifact JSON with context envelope. Since `ArtifactEvent` is in `loop` and the context envelope is also generic (provenance), implement `MarshalJSON` that serializes the artifact and wraps with context.
  - **Context serialization**: Create a `loop` helper (e.g., `marshalEventContext(ctx)`) that extracts provenance and returns a JSON-compatible map or struct. Do not include HTTP-specific `traceparent` in `loop` — but `loop` already imports OTel trace packages, so it can include `traceparent` if the context has an active span. This matches the current `eventContextToJSON` behavior.
  - Add `json.Marshaler` tests for each artifact type verifying exact JSON output matches the current HTTP format.
  - Add `json.Marshaler` tests for each loop event type.
  - Ensure the `Display` field on `ToolCall` is only included when it differs from `Arguments` (matching current behavior).
  - Ensure `Timestamp` on `Turn` uses `time.RFC3339Nano` format (matching current `turnToJSON` behavior). Note: `time.Time.MarshalJSON` already uses `RFC3339Nano`, so `json` tags on `Turn` may be sufficient.

### Task 5: Simplify HTTP conduit's `MarshalOutputEvent` and `MarshalArtifact` to dispatch to `json.Marshaler`
- **Goal**: Remove hardcoded serialization switches and delegate to self-serializing types.
- **Dependencies**: Task 4 (needs artifact/loop/state types to implement `json.Marshaler`).
- **Files Affected**: `x/conduit/http/types.go`, `x/conduit/http/types_test.go`
- **New Files**: None.
- **Interfaces**: `MarshalOutputEvent` and `MarshalArtifact` become thin dispatchers.
- **Validation**:
  - `go test ./x/conduit/http/...` passes
  - `go test -race ./x/conduit/http/...` passes
  - `go build ./...` passes
- **Details**:
  - Refactor `MarshalArtifact` to:
    ```go
    func MarshalArtifact(art artifact.Artifact) ([]byte, error) {
        if m, ok := art.(json.Marshaler); ok {
            return m.MarshalJSON()
        }
        return nil, fmt.Errorf("unsupported artifact kind: %s", art.Kind())
    }
    ```
  - Refactor `MarshalOutputEvent` to:
    ```go
    func MarshalOutputEvent(event loop.OutputEvent) ([]byte, error) {
        if m, ok := event.(json.Marshaler); ok {
            return m.MarshalJSON()
        }
        return nil, fmt.Errorf("unsupported event kind: %s", event.Kind())
    }
    ```
  - Remove `artifactToJSON`, `artifactFromJSON`, `turnToJSON`, `turnFromJSON`, and all event-specific JSON DTO structs (`artifactJSON`, `turnJSON`, `eventContextJSON`, `turnCompleteEventJSON`, `errorEventJSON`, `artifactEventJSON`, `propertiesEventJSON`, `lifecycleEventJSON`) if they are no longer needed.
  - Keep `eventContextToJSON` and `eventContextFromJSON` if they are used by `UnmarshalOutputEvent` and `UnmarshalArtifact`. Wait — `UnmarshalOutputEvent` and `UnmarshalArtifact` still need to know the exact JSON structure to reconstruct the correct types. They cannot use `json.Marshaler` because unmarshaling requires a registry or type switch. So the DTO structs and helper functions may still be needed for unmarshaling.
  - **Decision**: Only remove the DTOs used for *marshaling* (the outbound path). Keep the DTOs and helper functions for *unmarshaling* (the inbound path). The inbound path is still a closed switch because `UnmarshalOutputEvent` needs to know which concrete type to instantiate. This is acceptable because the HTTP UI never sends events (confirmed by user).
  - Verify that the existing `types_test.go` tests still pass, or update them if the JSON output format has changed (it should not have changed).
  - Add a test verifying `MarshalOutputEvent` correctly serializes a custom `OutputEvent` that implements `json.Marshaler` (e.g., a test-only type).

### Task 6: Add slash command example to `examples/http-chat`
- **Goal**: Demonstrate slash command integration in a reference application.
- **Dependencies**: Task 1, Task 2, Task 3.
- **Files Affected**: `examples/http-chat/main.go`
- **New Files**: None.
- **Interfaces**: None.
- **Validation**:
  - `go build ./examples/http-chat/...` passes
- **Details**:
  - In `examples/http-chat/main.go`, after creating the `session.Manager`, create a `slash.Registry`:
    ```go
    slashRegistry := slash.NewRegistry()
    slashRegistry.Bind("new", func(ctx context.Context, args []string) error {
        newStream, err := mgr.Create()
        if err != nil {
            return err
        }
        // The handler needs access to the stream to emit SessionSwitchEvent.
        // This is application-level wiring: the handler captures the stream
        // or uses a closure that emits via the manager or a known stream.
        // For the example, demonstrate that the handler can create a new
        // session and the application logic handles the switch.
        return nil
    })
    ```
  - Pass the registry to the Manager via `session.WithInterceptor(slashRegistry)`.
  - The example should be minimal: show registry creation, one bound command (`/new`), and the `WithInterceptor` option. The handler for `/new` can simply log that it was called (the full session-switch wiring is conduit-dependent and out of scope for a minimal example).
  - Ensure the example compiles and the `go build` passes.
  - Note: The `slash.Registry.Intercept` method receives `session.Event` but the handler in the example only needs to create a new session. The exact session-switch emission is handled by the application closure capturing `mgr`.

## Dependency Graph

- Task 1 → Task 3 (Task 3 needs `session.Interceptor` interface defined in Task 1)
- Task 2 || Task 1 (Task 2 is independent)
- Task 4 → Task 5 (Task 5 needs self-serializing types)
- Task 3 || Task 4 (Task 3 and Task 4 are parallelizable)
- Task 1, Task 2, Task 3 → Task 6 (Task 6 needs interceptor, SessionSwitchEvent, and slash registry)
- Task 5 || Task 6 (Task 5 is independent of the example)

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Self-serializing artifact/loop types produce JSON different from the current HTTP wire format | High | Medium | Before Task 5, run `x/conduit/http/types_test.go` tests with the new `MarshalJSON` implementations. The tests must pass without modification. If they fail, the `MarshalJSON` implementation is incorrect. |
| `ArtifactEvent` or `TurnCompleteEvent` context serialization in `loop` includes HTTP-specific fields incorrectly | Medium | Low | Only include `provenance` and `traceparent` in `loop` context serialization. These are the only two fields currently in `eventContextJSON`. `traceparent` extraction uses `trace.SpanFromContext` which is already imported in `loop`. |
| `state.Turn` `json` tags break non-HTTP serialization (e.g., JSON store) | Medium | Low | `json` tags are struct metadata and do not affect Go struct behavior. If `session.NewJSONStore` uses `json.Marshal` on `state.Turn`, the tags will change the stored JSON keys. Verify that the store can still load old data (backward compatibility) or document that the store format changes. Given the project convention of aggressive refactoring, this is acceptable. |
| `json.Marshaler` on `artifact.ToolCall` with `Value any` fails to serialize custom values | Medium | Low | `ToolCall.Value` is `any`. `json.Marshal` on `any` uses reflection. If `Value` implements `json.Marshaler` or is a basic type, it works. If `Value` contains unexported fields, `json.Marshal` fails. Add a test with `ToolCall.Value` set to a custom type. The current `artifactToJSON` ignores `Value` and uses `MarkdownString()` for `display`; self-serializing should preserve this logic. |
| Interceptor called during active turn causes race condition with `Stream` state | Medium | Low | The interceptor is called in `processOne` which holds no locks but is the single worker goroutine. The `Stream` state (`closed`, `cancel`) is only accessed in `processOne` and the public API methods with `mu`. The interceptor runs after the stream state is checked but before the provider is invoked. No additional synchronization is needed beyond the existing worker queue. |
| Slash command handler captures stream in closure, causing circular reference / leak | Low | Low | Handlers are user-provided closures. The application is responsible for closure design. The framework does not hold strong references to the handler beyond the registry. |

## Validation Criteria

- [ ] `session.Interceptor` interface is defined and documented.
- [ ] `session.Stream` calls the interceptor before the LLM pipeline and respects consume/pass-through semantics.
- [ ] `session.Manager` accepts `WithInterceptor` and passes it to all created streams.
- [ ] `session.SessionSwitchEvent` implements `loop.OutputEvent` and `json.Marshaler`.
- [ ] `x/slash/` package provides `Registry`, `Bind`, and `Intercept` with tests.
- [ ] All artifact types implement `json.Marshaler` with output matching the current HTTP wire format.
- [ ] All `loop.OutputEvent` types implement `json.Marshaler` with output matching the current HTTP wire format.
- [ ] `x/conduit/http/types.go` `MarshalOutputEvent` and `MarshalArtifact` dispatch to `json.Marshaler`.
- [ ] `x/conduit/http/types_test.go` tests pass without modification (or are updated to match the exact same JSON output).
- [ ] `examples/http-chat` compiles with a slash registry and `session.WithInterceptor`.
- [ ] `go test -race ./...` passes with no failures or data races.
- [ ] `go build ./...` passes.
- [ ] `go vet ./...` passes.
