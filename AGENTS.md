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
models/        ← leaf value type carried through state, loop, provider
provider/      ← depends on state/, artifact/, models/
loop/          ← depends on artifact/, state/, provider/, models/
x/wire/...     ← wire-format adapters (e.g. x/wire/anthropic/, x/wire/openai/);
                  depend only on provider/, models/, state/, artifact/
x/provider/... ← first-party, third-party, and gateway provider packages;
                  depend on x/wire/... and models/; never import loop/
x/catalog/...  ← generated model catalogs (e.g. x/catalog/models/);
                  depend only on root models/
cmd/modelsdev-gen/  ← generator binary; stdlib-only; produces x/catalog/models/
                       and x/provider/{openrouter,vercel}/lookup.go
```

- **Core packages** (`artifact/`, `state/`, `provider/`, `loop/`, `models/`) live at the root level so external applications can import them. Do not place framework contracts under `internal/`.
- **Wire adapters** live under `x/wire/<vendor>/` (e.g., `x/wire/anthropic/`). They translate a `models.Spec` to a vendor-specific wire format and implement `provider.Provider`. They never import `loop/`. Wire adapters are *transport*, not "the Anthropic provider" or "the OpenAI provider" — there are several distinct ways to expose each vendor (first-party, third-party mirrors, gateways), all built on top of the wires.
- **Provider packages** live under `x/provider/<name>/` (e.g., `x/provider/openai/`, `x/provider/anthropic/`, `x/provider/minimax/`, `x/provider/openrouter/`, `x/provider/vercel/`). They are thin wrappers that compose a wire adapter with vendor-specific defaults (base URL, name resolver, auth selection) and re-export the wire's options under their own package name. Three sub-shapes coexist:
  - **First-party** (`x/provider/anthropic/`, `x/provider/openai/`): identity resolution, no base URL, no auth overrides. The canonical entry point for direct vendor calls.
  - **Third-party** (`x/provider/minimax/`): identity resolution, fixed base URL targeting a vendor's API mirror, two constructors (`NewAnthropic`, `NewOpenAI`) for the two wire surfaces the mirror accepts.
  - **Gateway** (`x/provider/openrouter/`, `x/provider/vercel/`): identity resolution with a generated lookup table, fixed base URL targeting the gateway host.
  Provider packages implement `provider.Provider` (via composition with a wire) but never import `loop/`.
- **Catalog packages** live under `x/catalog/...` (currently `x/catalog/models/`) and contain generated `models.Spec` values keyed by canonical `Name`. The package exposes one var per upstream model (e.g. `ClaudeOpus45`, `GPT4o`); only the root `models` package is imported. The generator at `cmd/modelsdev-gen/` is the only producer; its output is committed and regenerated via `task generate`.
- **Generator binaries** live under `cmd/<name>/` (currently `cmd/modelsdev-gen/`). They are stdlib-only CLIs that produce committed artifacts. Re-running them is part of `task generate`.
- **Example/reference applications** live under `examples/<name>/` (e.g., `examples/single-turn-cli/`). These validate the framework and demonstrate composition patterns.
- **Maintained applications** with longer lifespans live under `cmd/<name>/` following the Standard Go Project Layout.

## Interface Design Principles

### Artifacts

The `Artifact` interface must expose a **public** method (e.g., `Kind() string`) to allow cross-package extensibility. Private marker methods (e.g., `artifact()`) prevent custom artifact types from being defined in other packages because Go does not allow implementing unexported methods across package boundaries.

Common artifact types (`Text`, `ToolCall`, `Image`) are defined in the `artifact/` package. Future custom types implement the same public interface from their own packages.

### State

State is a **mutable** interface. `Append()` mutates in place. `Turns()` returns a defensive copy of the internal slice so providers can safely iterate without synchronization. The in-memory implementation (`state.Buffer`) is intentionally not goroutine-safe — concurrency control is a future middleware concern.

### Provider

The provider contract is intentionally minimal: a single `Invoke(ctx, state, spec, ch, opts...)` method that takes a `models.Spec` value describing the model identity and inference configuration. The Spec is the canonical argument; per-call option types (ToolsOption, MaxTokensOption) cover only data-within-the-call. Metadata (token usage, finish reason) can be attached as custom artifact types or inspected by type-asserting the concrete provider adapter in the application layer. Do not bloat the interface with provider-specific fields.

### Model

The `models.ModelSpec` value type lives at the root level (`models/`, stdlib-only) and carries the model identity (`Name`) plus inference configuration (`Window`, `MaxOutputTokens`, `Temperature`, `ThinkingLevel`, `TopP`, `TopK`, `Seed`, `StopSequences`, `FrequencyPenalty`, `PresencePenalty`). The Spec is what flows through the loop, the session, and the provider; the application constructs it (or derives it from session metadata) and the framework propagates it. Per-vendor catalogs of well-known Specs live under `x/catalog/models/` as a generated sub-module; one var per upstream model, keyed by canonical `Name`.

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

### Tool Format and Default Truncation

The `tool.Tool` struct carries a `Format` field that controls how the
LLM-facing string for the tool's result is rendered. The default
behavior is **"framework defaults to safe"**: a tool that returns a
10 MB string is truncated to a bounded size unless the tool
explicitly opts out. Third-party tools inherit this property.

- `Format.Truncate` is a `TruncateConfig{MaxBytes, MaxLines}`. Zero
  values are filled in by the handler with the framework defaults
  (50 KB byte cap, 2000 line cap) at application time via
  `Format.ResolvedTruncateConfig()`.
- `Format.Style` selects head or tail truncation. The zero value is
  `StyleTail`, which matches terminal-output conventions. File reads
  typically use `StyleHead`.
- `Format.RecoveryHint` is a template string with `{name}`
  placeholders. The handler substitutes known placeholders
  (`{original_bytes}`, `{original_lines}`, `{shown_bytes}`,
  `{shown_lines}`, `{style}`, `{next_offset}`) plus any keys
  provided as tool-specific extras (e.g. `{path}` for the temp
  file path). Unknown placeholders are left as-is so typos are
  visible rather than failing the render.
- Tools that want full control over their LLM-facing string
  implement `artifact.LLMRenderer`. The handler respects this
  output verbatim; no further truncation is applied. This is the
  explicit opt-out path. The bash tool uses it to attach the temp
  file path and recovery hint without double-truncation.
- `artifact.ToolResult` gains a `Truncation *Truncation` field
  when truncation occurred. The struct reports
  Original/Shown bytes and lines, the style, and the rendered
  recovery hint. A nil Truncation means no truncation.

The default truncator lives in `x/tool/truncate` (alongside the
framework `Handler`). It is UTF-8 boundary safe and respects both
byte and line caps; the smaller of the two kept lengths wins.

The bash tool additionally uses a streaming `BoundedBuffer` to
cap the host process's heap regardless of subprocess output size.
A 60 MB `dd if=/dev/zero` no longer allocates 60 MB in the host;
the buffer holds a rolling 2× tail in memory and spills the full
byte stream to a temp file when the cap is exceeded. The
`BoundedBuffer.TotalBytes` method reports the full subprocess
output size; this is what populates `Truncation.OriginalBytes`
so the metadata reflects what the LLM is missing, not just the
size of the bounded tail.

Tool descriptions follow a structured form so the model sees a
consistent pattern: one-line summary, output limits, recovery
hint. The `BashTool` description and friends are reference
examples.

## Implementation Conventions

### Dependencies

The repository is a Go workspace (see `go.work`) with the root module and one
sub-module per directory under `x/<area>/<name>/` and `examples/<name>/`.
Each sub-module has its own `go.mod` and its own transitive dependency graph
so the cost of a heavy upstream SDK is contained to the single module that
needs it. The `Package Structure` section above documents the placement
rules; this section documents the dependency rules that flow from them.

- **Root module's `go.mod` must stay stdlib-only.** It contains the core
  primitives (`artifact/`, `state/`, `provider/`, `loop/`, `tool/`, etc.) and
  is imported by every other module. A transitive SDK in the root would
  force every downstream module to pay for it.
- **External SDKs are accepted inside a per-extension `go.mod`** when the
  alternative is hand-rolling a long-tail wire protocol. Examples: the
  openai module uses `github.com/openai/openai-go`; the anthropic module
  uses `github.com/anthropics/anthropic-sdk-go`. The acceptance criterion
  is *containment*: the SDK must not appear in the root module's
  `go.sum`, and it must not be a transitive dep of any other module that
  does not need it. Sub-modules may use `encoding/json` and `net/http` from
  the standard library for the parts of the wire protocol that the SDK
  does not model (e.g., non-standard extensions surfaced as JSON
  extra-fields).
- **Tests for a sub-module live inside the sub-module's `go.mod`** (as is
  the convention for Go modules). External test-only deps (e.g.,
  `github.com/stretchr/testify`) are acceptable in any module.

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
- `x/provider/retry.Provider.Invoke()` — `retry.invoke` —
  `SpanKindInternal` (parent of the inner `provider.invoke` span when
  the decorator is in the call chain)
- `x/provider/openai.Provider.Invoke()` — `provider.invoke` — `SpanKindClient`
- `x/tool.Handler.Handle()` — `tool.execute` — `SpanKindInternal`

When a tracer is configured, the `provider.invoke` span also records
granular HTTP lifecycle events (DNS, connection, TLS handshake, first-byte)
via an attached `httptrace.ClientTrace`, enriching the span without
creating child sub-spans.

HTTP-level retries (5xx, 429 with `Retry-After`) belong in
`x/provider/retry`, not in the provider adapters. The decorator owns the
backoff schedule, the streaming backstop, and the tracing shape; the
adapters only need to wrap their SDK errors in a type that implements
`retry.HTTPError`.

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
