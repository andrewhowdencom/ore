# ore Agent Conventions

This file captures architectural conventions and design intent for agents working on the ore codebase. It complements the README's vision document with practical rules discovered during design.

## Project Philosophy

ore is a **framework for building agentic applications**, not a specific agent implementation. The core is a minimal, provider-agnostic inference primitive. Everything else — provider adapters, artifact handlers, I/O conduits, orchestration strategies — lives outside the core as composable, build-time extensions.

## Refactoring vs. Backwards Compatibility

Agents have a tendency to preserve backwards compatibility when modifying code. **Do not do this.** At this stage of the project, prefer **aggressive refactoring** — rename packages, move files, delete indirection, and break internal APIs when doing so produces cleaner module boundaries. Backwards compatibility is a liability until the architecture has stabilised.

This application has never been run in production and has no persisted state. There are no users to break, no migrations to write, and no legacy data to preserve. This is exactly the time to be ruthless about structural cleanliness.

## Package Structure

Follow a **cycle-free dependency graph**:

```
artifact/      ← leaf package, no internal dependencies
state/         ← depends on artifact/
provider/      ← depends on state/, artifact/
loop/          ← depends on artifact/, state/, provider/
x/provider/... ← concrete adapters branch off provider/, never import loop/
```

- **Core packages** (`artifact/`, `state/`, `provider/`, `loop/`) live at the root level so external applications can import them. Do not place framework contracts under `internal/`.
- **Concrete provider adapters** live under `x/provider/<name>/` (e.g., `x/provider/openai/`). They implement `provider.Provider` but never import `loop/`.
- **Example/reference applications** live under `examples/<name>/` (e.g., `examples/single-turn-cli/`). These validate the framework and demonstrate composition patterns.
- **Maintained applications** with longer lifespans live under `cmd/<name>/` following the Standard Go Project Layout.

## Interface Design Principles

### Artifacts

The `Artifact` interface must expose a **public** method (e.g., `Kind() string`) to allow cross-package extensibility. Private marker methods (e.g., `artifact()`) prevent custom artifact types from being defined in other packages because Go does not allow implementing unexported methods across package boundaries.

Common artifact types (`Text`, `ToolCall`, `Image`) are defined in the `artifact/` package. Future custom types implement the same public interface from their own packages.

### State

State is a **mutable** interface. `Append()` mutates in place. `Turns()` returns a defensive copy of the internal slice so providers can safely iterate without synchronization. The in-memory implementation (`state.Buffer`) is intentionally not goroutine-safe — concurrency control is a future middleware concern.

### Provider

The provider contract is intentionally minimal: a single `Invoke(ctx, State) ([]Artifact, error)` method. Metadata (token usage, finish reason) can be attached as custom artifact types or inspected by type-asserting the concrete provider adapter in the application layer. Do not bloat the interface with provider-specific fields.

### Loop

The `loop.Step` is a thin orchestrator. A `Turn()` method calls the provider,
emits returned artifacts as a TurnCompleteEvent via Emit. When a state is
bound via loop.WithState, the turn is automatically appended before OnEmit
callbacks run. It does not handle retries, tool execution, or multi-turn
looping — those are application-layer concerns.

### Event Emission and State Persistence

`Step.Emit` is the single gateway for all observable mutations. A
synchronous `OnEmit` callback tier runs before the async `FanOut`,
providing a lossless, ordered, zero-drop path for custom side-effects
(logging, metrics) while preserving the intentionally lossy FanOut for
conduits. The canonical state persistence mechanism is `loop.WithState`,
which causes `Emit` to automatically append `TurnCompleteEvent` to the
bound state before running `OnEmit` callbacks.

Conduits that maintain local cached views (e.g. the TUI conduit) must
expose a reload hook (e.g. `ReloadHistory([]state.Turn)`) so application
code can refresh the view after external state mutations such as compaction
via `stream.LoadTurns`.

- `OnEmit` callbacks receive every `OutputEvent` and are invoked in
  registration order before the event reaches subscribers.
- `Emitter` is the interface exposed to artifact handlers for emitting
  events back into the stream.
- `Handler.Handle` receives an `Emitter` instead of `state.State`,
  keeping tool results visible to UI conduits via the same event stream.

### Transform

`loop.Transform` is an extension point that injects context into a turn without mutating the persistent buffer. Transforms receive the current `state.State`, return a modified copy, and the `loop.Step` executes the provider against the transformed state. This enables system prompts, guardrails, and other per-turn augmentations to live outside the core.

Examples: `x/systemprompt` injects a system message; `x/guardrails` validates or blocks content before it reaches the provider.

### Tool

The `tool/` package defines the core tool execution framework. A `ToolFunc` receives a `Sandbox` interface that provides isolation capabilities. Tools opt into available sandbox features via type assertions:

- `Sandbox` — base interface with `Name()`. A nil sandbox means no isolation; tools execute against the host filesystem and process space.
- `FileSandbox` — extends `Sandbox` with `ResolvePath()` and `WorkingDirectory()` for filesystem constraints.
- `ExecSandbox` — extends `Sandbox` with `Run()` for process isolation.

## Implementation Conventions

### Dependencies

Keep the dependency graph minimal. Provider adapters use `net/http` and `encoding/json` from the standard library. Avoid importing external SDKs for LLM providers — the adapter's job is to serialize/deserialize, and an SDK adds unnecessary weight and abstraction.

### Error Handling

Wrap errors with context using `fmt.Errorf("...: %w", err)`. Provider errors are propagated unchanged.

### Logging

Use `log/slog` with `TextHandler` for lifecycle events (startup, shutdown, errors). Do not use logs for access tracking — that belongs in tracing.

### Testing

- **Table-driven tests** are the standard for all unit tests.
- **Race detection**: always run `go test -race ./...`.
- Mock interfaces using local struct implementations in test files.
- Use `httptest.Server` to mock HTTP APIs in provider adapter tests.

### Functional Options

Use the functional options pattern for constructors with optional parameters
(e.g., `New(apiKey, model string, opts ...Option)`).
Common options include `WithTransforms`, `WithHandlers`, `WithOnEmit`,
and `WithInvokeOptions`.

### Observability and Tracing

OpenTelemetry tracing is woven through the framework as a **build-time
opt-in** via functional options. Every instrumented component accepts a
`trace.Tracer` through a `WithTracer(...)` option; when no tracer is
configured the instrumentation is a no-op.

**Span hierarchy:**
- Conduit server spans (e.g. `http.send_message`, `tui.turn`, `slack.turn`,
  `telegram.turn`, `stdio.turn`) — `SpanKindServer`
- `cognitive.ReAct.Run()` — `react.run` — `SpanKindInternal`
- `loop.Step.Turn()` / `Submit()` — `loop.turn` — `SpanKindInternal`
- `x/provider/openai.Provider.Invoke()` — `provider.invoke` — `SpanKindClient`
- `x/tool.Handler.Handle()` — `tool.execute` — `SpanKindInternal`

When a tracer is configured, the `provider.invoke` span also records
granular HTTP lifecycle events (DNS, connection, TLS handshake, first-byte)
via an attached `httptrace.ClientTrace`, enriching the span without
creating child sub-spans.

All spans carry `thread_id` as a `go.opentelemetry.io/otel/attribute.String`
attribute, extracted from the context via `loop.ThreadIDFrom(ctx)`.

Context propagation:
- Conduits inject/extract W3C `traceparent` via
  `go.opentelemetry.io/otel/propagation.TraceContext`
- The HTTP conduit serializes the active span's traceparent into NDJSON/SSE
  responses (field `traceparent` on `eventContextJSON`) so web clients can
  link client-side spans.
- `context.WithoutCancel` is used at the `loop.Step` event-envelope
  boundary so async FanOut subscribers do not inherit request cancellation.

## Application Boundaries

- **Examples** (`examples/`) are reference implementations demonstrating how to compose the framework. They may be minimal, hardcoded, or environment-variable-driven.
- **Commands** (`cmd/`) are maintained, first-class applications with longer lifespans and stronger operational requirements.
- **Agent** (`agent/`) is a reusable multi-conduit orchestration container that wires multiple conduits to a shared `session.Manager`. It sits between core framework and application as a composable runtime scaffold.

Do not conflate the two. If a binary is a validation tool or tutorial, it belongs in `examples/`. If it is a product or service, it belongs in `cmd/`.

## Conduit/Library vs. Application Boundary

Conduit and handler libraries (`x/conduit/tui/`, `x/conduit/http/`, and future I/O adapters) provide **infrastructure only**:

- Transport adaptation (HTTP request/response, terminal rendering)
- Event streaming (channels, NDJSON, SSE)
- Session management (when the transport requires it, e.g. HTTP)

They **MUST NOT**:
- Import `cognitive/` or embed specific cognitive patterns (ReAct, Chain-of-Thought, etc.)
- Invoke the provider directly
- Manage the conversation turn loop

Cognitive patterns, provider invocation, and conversation orchestration are **application-level concerns**, composed in `examples/` or `cmd/` packages. The library exposes its `Session`, `Step`, and `State` via exported accessors so the application can call `Step.Submit()`, `Step.Turn()`, or run a full `cognitive.ReAct` loop as needed.

This mirrors the TUI pattern: `x/conduit/tui/` is a dumb pipe that
subscribes to `*_delta` artifact events and renders them incrementally
using a short-interval debounce; `examples/tui-chat/main.go` composes the
ReAct loop. The HTTP conduit must follow the same separation.

### TUI Rendering Expectations

The TUI conduit renders all assistant artifacts (including `Reasoning`)
through a Markdown renderer with embedded zero-margin styles. Agents
generating content for the TUI should format output accordingly, as
raw text and Markdown syntax will be styled by glamour at display time.

## Agent Workspace Conventions

### Verify Freshness Before Reasoning

Before reading files or building a mental model of the codebase, verify the
repository state:

- `git status` — check for uncommitted changes that may affect file contents.
- `git log --oneline -5` — confirm the HEAD commit and recent history.
- `git branch` — confirm which branch is checked out.

Do not assume files in sibling directories (e.g. `.worktrees/`) reflect the
current branch. Each worktree is an isolated checkout; only the current working
directory is the source of truth.

### Scope to the Current Worktree

Agents are opened within a specific git worktree or branch checkout. Treat that
working directory as the sole scope of operation. Do not read from, reason about,
or modify files in other worktrees unless explicitly directed.

### Main Branch Default

When opened in the main worktree on the default branch, default to ideation and
discussion. Do not propose or execute file changes unless explicitly asked.
