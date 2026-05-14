# ore

> ore are the inputs to an agentic system.
> forge is the agentic developer that builds with ore (and others, for that matter).

## Purpose

ore is a Go-native framework for building agentic applications. It provides a minimal core inference primitive, provider-agnostic LLM adapters, composable I/O conduits, and clean extension points implemented as Go interfaces.

This is a learning project and a conceptual exploration. It is inspired by [pi.dev](https://pi.dev)'s philosophy of minimal cores and aggressive extensibility, but reimagined in Go with different architectural priorities: first-class non-interactive conduits, build-time composition via Go interfaces, and a narrower core that delegates all workflow opinions to extensions and applications.

## System Architecture

ore is organized into layers. Each layer communicates through narrow interfaces. No layer knows more about the others than it needs to.

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

- **Serialize** ore's generic state into the provider's native request format
- **Invoke** the LLM through the provider's SDK or HTTP API
- **Deserialize** the provider's native response into a generic, provider-agnostic artifact format that Step can append to state

Step does not know whether it is talking to GPT-4, Claude, Gemini, or a local model. It only knows: hand state to adapter, receive state back.

### Extension Points

Extension Points are clean Go interfaces for capabilities that applications compose at build time. They follow a lifecycle naming convention (`BeforeX`, `AfterX`, `HandleX`) and are not runtime plugins or shared libraries; they are packages you import and wire together.

#### Artifact Handlers

The most important extension pattern. LLM responses are heterogeneous bags of artifacts. An Artifact Handler processes specific artifact types it understands and ignores the rest. Handlers implement `loop.Handler` and are registered via `loop.WithHandlers()`.

The primary concrete handler is the **Tool Call Handler** (in `tool/`). It detects `tool_call` artifacts, looks up the tool in a `tool.Registry` by name, executes the corresponding Go function, and appends `RoleTool` turns with `ToolResult` artifacts. Unknown tools are refused. This is deliberately an extension, not core behavior — it validates that the extension model actually works.

Other handlers:

- **Image Handler** (planned) — Detects `image` artifacts (URLs or base64 blobs), stores or renders them, and appends references to state
- **Structured Output Handler** (planned) — Validates `json_schema` artifacts against a declared schema
- **Streaming Support** — `Step` can render partial delta artifacts in real time when the provider implements `provider.StreamingProvider`. No separate streaming handler is required; provider adapters own delta→complete buffering internally.

Multiple artifact handlers can fire on the same response. A response containing text, a tool call, and a reasoning block will be processed by three different handlers, each doing its own work.

#### Tool Calling

Tool calling uses three mechanisms that compose through the extension point pattern:

1. **Provider adapter** (`provider/openai/`) — accepts tool configuration per-invocation via `openai.WithTools()`, serializes them in requests, deserializes `ToolCall` from responses, and serializes `RoleTool` turns with `ToolResult` back to the provider
2. **Artifact Handler** (`tool/`) — a `tool.Registry` maps names to Go functions, and a `tool.Handler` implements `loop.Handler` to execute them
3. **`BeforeTurn` hook** (optional) — a `loop.BeforeTurn` implementation can inject system prompts or tool usage instructions before the provider call

The application wires them together:

```go
registry := tool.NewRegistry()
registry.Register("add", func(ctx context.Context, args map[string]any) (any, error) {
    a, _ := args["a"].(float64)
    b, _ := args["b"].(float64)
    return a + b, nil
})

prov := openai.New(apiKey, model)

tools := []provider.Tool{
    {Name: "add", Description: "Add two numbers", Schema: schema},
}

// The concrete type tool.Handler implements loop.Handler.
// Pre-bind tool options to the Step so ReAct remains provider-agnostic.
step := loop.New(
    loop.WithHandlers(registry.Handler()),
    loop.WithInvokeOptions(openai.WithTools(tools)),
)

// The cognitive.ReAct pattern automatically loops while tool calls are in flight.
```

The `provider.Tool` struct is provider-agnostic — each adapter maps it to its native API. Provider sub-packages expose option constructors such as `openai.WithTools` that return `provider.InvokeOption` values; these are passed to `Step.Turn` or pre-bound to the Step via `loop.WithInvokeOptions`.

**Dynamic tool configuration.** The tool list can be evolved during a session by passing different `openai.WithTools` options to each `Step.Turn` call. This allows the application to prune, expand, or replace tools based on context, user permissions, or discovered capabilities:

```go
// Pass different tool sets per-turn.
tools := selectToolsForContext(ctx, state)
_, err := step.Turn(ctx, state, prov, openai.WithTools(tools))
```

Because tools are passed per-invocation through `InvokeOption`, there is no mutable provider state and no need for synchronization.

#### Other Extension Points

- **`BeforeTurn` hook** (`loop.BeforeTurn`) — transforms state before the provider call. Multiple hooks compose in registration order. Errors abort the turn. Register via `loop.WithBeforeTurn(...)`:

  ```go
  type systemPromptInjector struct{}

  func (i systemPromptInjector) BeforeTurn(ctx context.Context, s state.State) (state.State, error) {
      s.Append(state.RoleSystem, artifact.Text{Content: "You are a helpful assistant."})
      return s, nil
  }

  step := loop.New(
      loop.WithBeforeTurn(systemPromptInjector{}),
      loop.WithHandlers(registry.Handler()),
  )
  ```
- **Lifecycle interfaces** (planned) — Hooks for session start, end, compaction, or error handling
- **Output Parser interfaces** (planned) — Swappable parsers for reasoning formats (e.g., ReAct's `Thought: ... Action: ...` for models without native tool support)

Extensions compose. They do not mutate the core.

### I/O Conduits

I/O Conduits are adapters that translate external events into triggers for the application layer and route outputs to external systems. They are **not** "UIs" in the narrow sense.

An I/O Conduit can be:

- **Interactive** — TUI, web interface, Telegram or Discord bot
- **Event-driven** — Webhook receiver, message queue consumer, alert processor (e.g., PagerDuty → analysis → Slack notification)
- **Scheduled** — Cron-triggered jobs, periodic report generation
- **Service-oriented** — REST or gRPC endpoint, CLI one-shot, RPC over stdio
- **Streaming** — WebSocket server, SSE endpoint, log tailer

A Conduit's contract with the application layer is about **ingress events** and **egress actions**, not about rendering chat windows.

The framework defines a `conduit.Conduit` interface with four egress actions and one ingress source:

- `Events() <-chan Event` — read-only channel of user-generated events (`UserMessageEvent`, `InterruptEvent`, etc.)
- `RenderDelta(ctx, artifact.Artifact) error` — render an ephemeral delta artifact incrementally (e.g., `TextDelta` chunks)
- `RenderTurn(ctx, state.Turn) error` — render a complete turn that has been appended to state
- `SetStatus(ctx, string) error` — update a transient status indicator (e.g., "thinking...", "calling tool...")

Implementations (TUI, web, Telegram, etc.) satisfy this interface at build time. The framework does not assume any specific rendering mechanism.

### Threads

Threads are first-class, persistent entities identified by stable UUIDs.
The `thread/` package defines a `Store` interface with in-memory and
JSON-on-disk implementations. A `Thread` holds a `*state.Buffer`,
timestamps, and a per-thread lock so multiple conduits can safely
append turns to the same thread.

This design shifts conduits from thread owners into thin I/O
frontends: HTTP, TUI, and future frontends all attach to the same
`Thread` via a shared `Store`. A thread started in the HTTP
example can be resumed in the TUI by passing its UUID via `--thread`.

Only non-delta artifacts can be persisted; delta streaming fragments
(`TextDelta`, `ReasoningDelta`, `ToolCallDelta`) are rejected during
serialization to prevent ephemeral data from entering stored state.

### Three-Layer Architecture

Above the Loop / Step, the framework separates concerns into three layers:

- **`loop.Step`** — the transform layer. Executes one complete inference turn: invokes the provider, optionally emits streaming deltas as `OutputEvent` to a configured channel, and runs registered artifact handlers synchronously on the complete response. Handlers may mutate state (e.g., append `RoleTool` turns with tool results).
- **`cognitive.ReAct`** — a pure cognitive pattern that implements the ReAct feedback loop. It repeatedly calls `Step.Turn()`, inspects the resulting state, and loops again if the last turn is not from the assistant (indicating pending tool results). It is conduit-agnostic and stateless — it receives `state.State` as a parameter and returns it.
- **Application-layer IO wiring** — the application (typically in `main()`) owns the `Conduit`, reads `Conduit.Events()`, appends user messages to state, invokes the cognitive pattern, subscribes to `Step`'s output events to route delta and turn events back to the conduit, and manages status and interrupts.

This three-layer separation means single-turn applications can use `Step` directly, multi-turn agents compose `Step` with `cognitive.ReAct`, and the application layer handles all conduit-specific concerns.

### Agents / Applications

An Agent (or Application) is a runnable assembly that composes `loop.Step`, a Provider Adapter, a set of Artifact Handlers and Extensions, one or more I/O Conduits, and a cognitive pattern into a concrete system.

Crucially, the application layer is also where **strategy** happens. Step does not loop on its own. The application (via a cognitive pattern or directly) decides:

- When to call `Step.Turn()`
- Whether to call it once (single-shot Q&A) or repeatedly (tool-calling agent)
- Whether to fork state and run multiple loops in parallel (Tree-of-Thought)
- Whether to insert reflection messages between turns (Reflexion)
- When to stop and return a result to the I/O Conduit

There is no single "ore" binary that does everything. Instead, there are compositions: a coding assistant with a TUI that loops on tool calls, a PR review bot that runs a single turn and posts to Slack, a scheduled log analyzer that chains three single-turn prompts together. Each is a Go program that imports the pieces it needs and wires them in `main`.

## Design Principles

1. **Simplicity** — Step does as little as possible. It is a stateful inference primitive. Every feature that can live outside the core does.
2. **Composability** — Components connect through narrow interfaces. A Step, an OpenAI adapter, a tool handler, and a TUI conduit compose the same way as a Step, an Anthropic adapter, an image handler, and a webhook conduit.
3. **I/O Agnosticism** — Step does not know whether it is running in an interactive terminal or responding to a 3 AM PagerDuty alert. Conduits handle the world; Step handles one inference turn.
4. **Build-time Extension** — Extensions are Go packages composed at build time, not runtime plugins. This keeps deployment simple and interfaces type-safe.
5. **Defer Specifics** — Patterns like memory, reflection, planning, reasoning strategies (ReAct, ToT, CoT), multi-agent orchestration, and tool calling are enabled by Step's extensibility but are not designed in the core. They emerge as artifact handlers, orchestrators, and applications that control how turns are invoked, not as alternative core implementations.
6. **Treat Tool Calling as an Extension** — Tool calling is a common and important capability, but it is not privileged. It is one artifact handler among many. This ensures the architecture can absorb future LLM capabilities (images, audio, video, structured output) without core changes.

## Relationship to pi.dev

[ore] is conceptually descended from [pi.dev](https://pi.dev), a mature TypeScript terminal coding harness. pi.dev's philosophy of "minimal core, aggressive extensibility" is the direct inspiration for this project.

Where ore diverges:

- **Language** — Go instead of TypeScript. This is a learning exercise and an exploration of Go's deployment and runtime characteristics for agent systems.
- **I/O Conduits** — pi.dev is primarily a TUI-centric tool with other modes (print, JSON, RPC) as secondary interfaces. ore treats all ingress/egress adapters as first-class, equally valid conduits.
- **Extension Model** — pi.dev uses TypeScript modules and runtime package loading. ore uses Go interfaces and build-time composition.
- **Scope** — pi.dev is a production coding agent. ore is a framework for building agents, not a specific agent implementation.

## Project Status

This README remains a vision document, but the framework is now partially implemented. The following packages and interfaces are available:

- `artifact/` — `Artifact` interface with `Text`, `ToolCall`, `ToolResult`, `Usage`, `Image`, `Reasoning`, and streaming delta types (`TextDelta`, `ReasoningDelta`, `ToolCallDelta`)
- `state/` — `State` interface with `Turns()` and `Append()`, and an in-memory `Memory` implementation
- `thread/` — `Store` interface with `Create`, `Get`, `Save`, `Delete`, and `List`. `Thread` struct with UUID, `*state.Buffer`, timestamps, and per-thread locking. `MemoryStore` (ephemeral) and `JSONStore` (persisted to `{uuid}.json` files) implementations. Delta artifacts cannot be persisted; serialization rejects them.
- `provider/` — `Provider` interface with `Invoke()`, `StreamingProvider` for channel-based delta emission, and `InvokeOption` for per-invocation configuration (tools, temperature, etc.)
- `loop/` — `Step` with `Turn()` method, `BeforeTurn` hook, optional streaming via `OutputEvent` channel, and artifact `Handler` interface for single-turn execution
- `tool/` — `Registry` for mapping tool names to Go functions, and `Handler` implementing `loop.Handler` for tool execution
- `cognitive/` — `ReAct` cognitive pattern for multi-turn looping, conduit-agnostic and stateless
- `conduit/` — `Conduit` interface with ingress events and egress delta/turn/status rendering. The TUI conduit renders assistant turns as rich Markdown via `charmbracelet/glamour` (syntax-highlighted code blocks, headings, bold/italic); streaming text stays plain text so incomplete Markdown never breaks.
- `provider/openai/` — OpenAI-compatible adapter with streaming chat completions and tool calling support
- `examples/single-turn-cli/` — Reference one-shot CLI application
- `examples/tui-chat/` — Reference streaming chat REPL using Bubble Tea
- `examples/calculator/` — Reference tool-calling application with add and multiply
- `cmd/forge/` — CLI that reads a YAML manifest and generates a compilable Go agent binary for HTTP or TUI conduits

Remaining work: additional provider adapters (Anthropic, Gemini), additional artifact handlers (image, structured output), additional lifecycle hooks, and more I/O conduit implementations (web, Telegram, webhook).

## Forge CLI

`cmd/forge` is a build-time tool that turns a YAML manifest into a runnable Go binary. It generates the `main.go` and `go.mod` for an agent application, resolves the local ore module path, and compiles the binary with `go build`.

### Usage

```bash
go run ./cmd/forge -config forge.yaml
```

### Manifest format

The manifest is a single YAML file with three top-level sections:

```yaml
dist:
  name: my-agent          # binary name used in go.mod
  output_path: ./my-agent # destination path (relative to cwd)
conduit:
  type: http              # "http" or "tui"
```

### Generated binary

The compiled binary accepts the same environment variables as the reference applications (`ORE_API_KEY`, `ORE_MODEL`, `ORE_BASE_URL`, `STORE_DIR`, `PORT`) and behaves identically. For the TUI conduit, pass `--thread <uuid>` to resume an existing thread.
