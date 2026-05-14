# ore Agent Conventions

This file captures architectural conventions and design intent for agents working on the ore codebase. It complements the README's vision document with practical rules discovered during design.

## Project Philosophy

ore is a **framework for building agentic applications**, not a specific agent implementation. The core is a minimal, provider-agnostic inference primitive. Everything else — provider adapters, artifact handlers, I/O conduits, orchestration strategies — lives outside the core as composable, build-time extensions.

## Refactoring vs. Backwards Compatibility

Agents have a tendency to preserve backwards compatibility when modifying code. **Do not do this.** At this stage of the project, prefer **aggressive refactoring** — rename packages, move files, delete indirection, and break internal APIs when doing so produces cleaner module boundaries. Backwards compatibility is a liability until the architecture has stabilised.

## Package Structure

Follow a **cycle-free dependency graph**:

```
artifact/      ← leaf package, no internal dependencies
state/         ← depends on artifact/
provider/      ← depends on state/, artifact/
core/          ← depends on artifact/, state/, provider/
provider/...    ← concrete adapters branch off provider/, never import core/
```

- **Core packages** (`artifact/`, `state/`, `provider/`, `core/`) live at the root level so external applications can import them. Do not place framework contracts under `internal/`.
- **Concrete provider adapters** live under `provider/<name>/` (e.g., `provider/openai/`). They implement `provider.Provider` but never import `core/`.
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

### Core Loop

The `core.Loop` is a thin orchestrator. A `Turn()` method calls the provider, appends returned artifacts to state with `RoleAssistant`, and returns the mutated state. It does not handle retries, tool execution, or multi-turn looping — those are application-layer concerns.

## Implementation Conventions

### Dependencies

Keep the dependency graph minimal. Provider adapters use `net/http` and `encoding/json` from the standard library. Avoid importing external SDKs for LLM providers — the adapter's job is to serialize/deserialize, and an SDK adds unnecessary weight and abstraction.

### Error Handling

Wrap errors with context using `fmt.Errorf("...: %w", err)`. The core propagates provider errors unchanged.

### Logging

Use `log/slog` with `TextHandler` for lifecycle events (startup, shutdown, errors). Do not use logs for access tracking — that belongs in tracing.

### Testing

- **Table-driven tests** are the standard for all unit tests.
- **Race detection**: always run `go test -race ./...`.
- Mock interfaces using local struct implementations in test files.
- Use `httptest.Server` to mock HTTP APIs in provider adapter tests.

### Functional Options

Use the functional options pattern for constructors with optional parameters (e.g., `New(apiKey, model string, opts ...Option)`).

## Application Boundaries

- **Examples** (`examples/`) are reference implementations demonstrating how to compose the framework. They may be minimal, hardcoded, or environment-variable-driven.
- **Commands** (`cmd/`) are maintained, first-class applications with longer lifespans and stronger operational requirements.

Do not conflate the two. If a binary is a validation tool or tutorial, it belongs in `examples/`. If it is a product or service, it belongs in `cmd/`.

## Conduit/Library vs. Application Boundary

Conduit and handler libraries (`conduit/tui/`, `conduit/http/`, and future I/O adapters) provide **infrastructure only**:

- Transport adaptation (HTTP request/response, terminal rendering)
- Event streaming (channels, NDJSON, SSE)
- Session management (when the transport requires it, e.g. HTTP)

They **MUST NOT**:
- Import `cognitive/` or embed specific cognitive patterns (ReAct, Chain-of-Thought, etc.)
- Invoke the provider directly
- Manage the conversation turn loop

Cognitive patterns, provider invocation, and conversation orchestration are **application-level concerns**, composed in `examples/` or `cmd/` packages. The library exposes its `Session`, `Step`, and `State` via exported accessors so the application can call `Step.Submit()`, `Step.Turn()`, or run a full `cognitive.ReAct` loop as needed.

This mirrors the TUI pattern: `conduit/tui/` is a dumb pipe; `examples/tui-chat/main.go` composes the ReAct loop. The HTTP conduit must follow the same separation.

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
