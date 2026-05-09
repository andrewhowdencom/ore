# tack

> Token Fastener — a minimal, composable agent harness for Go.

## Purpose

tack is a Go-native framework for building agentic applications. It provides a minimal core inference primitive, provider-agnostic LLM adapters, composable I/O surfaces, and clean extension points implemented as Go interfaces.

This is a learning project and a conceptual exploration. It is inspired by [pi.dev](https://pi.dev)'s philosophy of minimal cores and aggressive extensibility, but reimagined in Go with different architectural priorities: first-class non-interactive surfaces, build-time composition via Go interfaces, and a narrower core that delegates all workflow opinions to extensions and applications.

## System Architecture

tack is organized into layers. Each layer communicates through narrow interfaces. No layer knows more about the others than it needs to.

### Loop / Step

The minimal inference primitive. A single Step turn is:

1. Read current state
2. Hand state to a provider adapter, which serializes it for a specific LLM API
3. Invoke the LLM
4. Receive the raw response (a heterogeneous bag of artifacts: text, tool calls, images, reasoning blocks, or future formats)
5. Append the response to state
6. Return updated state

That is all it does. It does not loop. It does not execute tools. It does not parse ReAct text. It does not know what an image is.

Step is intentionally agnostic about:

- How it is triggered (interactive message, webhook, cron schedule, file system event)
- Where its outputs go (terminal, web page, Slack channel, message queue)
- Which LLM provider serves inference
- Which tools, image generators, or other external capabilities are available
- Whether the response contains text, tool calls, images, audio, structured data, or future artifact types
- **Which reasoning pattern drives the conversation** — ReAct, Chain-of-Thought, Tree-of-Thought, reflection, planning, multi-agent debate, or any other meta-cognitive strategy

### Provider Adapters

Provider Adapters are the bridge between Step and specific LLM APIs. They understand the native protocol of their provider: OpenAI's chat completions, Anthropic's Messages API, Google's Gemini, local Ollama endpoints, or custom enterprise gateways.

A provider adapter's job is:

- **Serialize** tack's generic state into the provider's native request format
- **Invoke** the LLM through the provider's SDK or HTTP API
- **Deserialize** the provider's native response into a generic, provider-agnostic artifact format that Step can append to state

Step does not know whether it is talking to GPT-4, Claude, Gemini, or a local model. It only knows: hand state to adapter, receive state back.

### Extension Points

Extension Points are clean Go interfaces for capabilities that applications compose at build time. They are not runtime plugins or shared libraries; they are packages you import and wire together.

#### Artifact Handlers

The most important extension pattern. LLM responses are heterogeneous bags of artifacts. An Artifact Handler processes specific artifact types it understands and ignores the rest.

Examples:

- **Tool Call Handler** — Detects `tool_call` artifacts, executes the corresponding functions, and appends results to state. This is deliberately an extension, not core behavior. It is the primary stress test for whether the extension model actually works.
- **Image Handler** — Detects `image` artifacts (URLs or base64 blobs), stores or renders them, and appends references to state
- **Structured Output Handler** — Validates `json_schema` artifacts against a declared schema
- **Streaming Handler** — Intercepts `text_delta` or `reasoning_delta` artifacts and routes them to a TUI or SSE stream in real time. The framework supports a dual-stream artifact model: ephemeral delta artifacts flow through a "hot" channel for real-time rendering, while complete buffered artifacts are appended to state via the "cold" path. Provider adapters own delta→complete buffering internally.

Multiple artifact handlers can fire on the same response. A response containing text, a tool call, and a reasoning block will be processed by three different handlers, each doing its own work.

#### Other Extension Points

- **Middleware interfaces** — Hooks that intercept prompts, responses, or state transitions
- **Lifecycle interfaces** — Hooks for session start, end, compaction, or error handling
- **Output Parser interfaces** — Swappable parsers for reasoning formats (e.g., ReAct's `Thought: ... Action: ...` for models without native tool support)

Extensions compose. They do not mutate the core.

### I/O Surfaces

I/O Surfaces are adapters that translate external events into triggers for the application layer and route outputs to external systems. They are **not** "UIs" in the narrow sense.

An I/O Surface can be:

- **Interactive** — TUI, web interface, Telegram or Discord bot
- **Event-driven** — Webhook receiver, message queue consumer, alert processor (e.g., PagerDuty → analysis → Slack notification)
- **Scheduled** — Cron-triggered jobs, periodic report generation
- **Service-oriented** — REST or gRPC endpoint, CLI one-shot, RPC over stdio
- **Streaming** — WebSocket server, SSE endpoint, log tailer

A Surface's contract with the application layer is about **ingress events** and **egress actions**, not about rendering chat windows.

The framework defines a `surface.Surface` interface with four egress actions and one ingress source:

- `Events() <-chan Event` — read-only channel of user-generated events (`UserMessageEvent`, `InterruptEvent`, etc.)
- `RenderDelta(ctx, artifact.Artifact) error` — render an ephemeral delta artifact incrementally (e.g., `TextDelta` chunks)
- `RenderTurn(ctx, state.Turn) error` — render a complete turn that has been appended to state
- `SetStatus(ctx, string) error` — update a transient status indicator (e.g., "thinking...", "calling tool...")

Implementations (TUI, web, Telegram, etc.) satisfy this interface at build time. The framework does not assume any specific rendering mechanism.

### Orchestration

Above the Loop / Step, the framework provides two composable abstractions that separate **single-turn execution** from **multi-turn strategy**:

- **`loop.Step`** — executes one complete inference turn. It invokes the provider, optionally routes streaming deltas to the surface via a background goroutine, and then runs registered artifact handlers synchronously on the complete response. Handlers may mutate state (e.g., append `RoleTool` turns with tool results).
- **`orchestrate.ReAct`** — an `Orchestrator` implementation that loops on `Step` while state changes. It reads events from a `Surface`, appends `RoleUser` turns, calls `Step.Turn()`, renders new turns via `RenderTurn()`, and loops again if a handler appended a `RoleTool` turn. Status updates ("thinking...", "calling tool...") are sent to the surface between iterations.

This separation means single-turn applications (e.g., a REST API endpoint) can use `Step` directly, while multi-turn agents (e.g., a TUI coding assistant) compose `Step` with `ReAct` or a custom `Orchestrator`.

### Agents / Applications

An Agent (or Application) is a runnable assembly that composes loop.Step, a Provider Adapter, a set of Artifact Handlers and Extensions, one or more I/O Surfaces, and an Orchestrator into a concrete system.

Crucially, the application layer is also where **strategy** happens. Step does not loop on its own. The application (via an Orchestrator) decides:

- When to call `Step.Turn()`
- Whether to call it once (single-shot Q&A) or repeatedly (tool-calling agent)
- Whether to fork state and run multiple loops in parallel (Tree-of-Thought)
- Whether to insert reflection messages between turns (Reflexion)
- When to stop and return a result to the I/O Surface

There is no single "tack" binary that does everything. Instead, there are compositions: a coding assistant with a TUI that loops on tool calls, a PR review bot that runs a single turn and posts to Slack, a scheduled log analyzer that chains three single-turn prompts together. Each is a Go program that imports the pieces it needs and wires them in `main`.

## Design Principles

1. **Simplicity** — Step does as little as possible. It is a stateful inference primitive. Every feature that can live outside the core does.
2. **Composability** — Components connect through narrow interfaces. A Step, an OpenAI adapter, a tool handler, and a TUI surface compose the same way as a Step, an Anthropic adapter, an image handler, and a webhook surface.
3. **I/O Agnosticism** — Step does not know whether it is running in an interactive terminal or responding to a 3 AM PagerDuty alert. Surfaces handle the world; Step handles one inference turn.
4. **Build-time Extension** — Extensions are Go packages composed at build time, not runtime plugins. This keeps deployment simple and interfaces type-safe.
5. **Defer Specifics** — Patterns like memory, reflection, planning, reasoning strategies (ReAct, ToT, CoT), multi-agent orchestration, and tool calling are enabled by Step's extensibility but are not designed in the core. They emerge as artifact handlers, orchestrators, and applications that control how turns are invoked, not as alternative core implementations.
6. **Treat Tool Calling as an Extension** — Tool calling is a common and important capability, but it is not privileged. It is one artifact handler among many. This ensures the architecture can absorb future LLM capabilities (images, audio, video, structured output) without core changes.

## Relationship to pi.dev

[tack] is conceptually descended from [pi.dev](https://pi.dev), a mature TypeScript terminal coding harness. pi.dev's philosophy of "minimal core, aggressive extensibility" is the direct inspiration for this project.

Where tack diverges:

- **Language** — Go instead of TypeScript. This is a learning exercise and an exploration of Go's deployment and runtime characteristics for agent systems.
- **I/O Surfaces** — pi.dev is primarily a TUI-centric tool with other modes (print, JSON, RPC) as secondary interfaces. tack treats all ingress/egress adapters as first-class, equally valid surfaces.
- **Extension Model** — pi.dev uses TypeScript modules and runtime package loading. tack uses Go interfaces and build-time composition.
- **Scope** — pi.dev is a production coding agent. tack is a framework for building agents, not a specific agent implementation.

## Project Status

This README remains a vision document, but the framework is now partially implemented. The following packages and interfaces are available:

- `artifact/` — `Artifact` interface with `Text`, `ToolCall`, `Image`, `Reasoning`, and streaming delta types (`TextDelta`, `ReasoningDelta`, `ToolCallDelta`)
- `state/` — `State` interface with `Turns()` and `Append()`, and an in-memory `Memory` implementation
- `provider/` — `Provider` interface with `Invoke()`, and `StreamingProvider` for channel-based delta emission
- `loop/` — `Step` with `Turn()` method, optional streaming via surface, and artifact `Handler` interface for single-turn execution
- `orchestrate/` — `Orchestrator` interface and `ReAct` implementation for multi-turn looping
- `surface/` — `Surface` interface with ingress events and egress delta/turn/status rendering
- `provider/openai/` — OpenAI-compatible adapter with streaming chat completions support
- `examples/single-turn-cli/` — Reference one-shot CLI application
- `examples/tui-chat/` — Reference streaming chat REPL using Bubble Tea

Remaining work: additional provider adapters (Anthropic, Gemini), additional artifact handlers (image, structured output), middleware and lifecycle hooks, and more I/O surface implementations (web, Telegram, webhook).
