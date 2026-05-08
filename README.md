# tack

> Token Fastener — a minimal, composable agent harness for Go.

## Purpose

tack is a Go-native framework for building agentic applications. It provides a minimal core loop, a provider-agnostic LLM interface, composable I/O surfaces, and clean extension points implemented as Go interfaces.

This is a learning project and a conceptual exploration. It is inspired by [pi.dev](https://pi.dev)'s philosophy of minimal cores and aggressive extensibility, but reimagined in Go with different architectural priorities: first-class non-interactive surfaces, build-time composition via Go interfaces, and a narrower core that delegates all workflow opinions to extensions and applications.

## System Architecture

tack is organized into four layers. Each layer communicates through narrow interfaces. No layer knows more about the others than it needs to.

### Core Loop

The minimal agentic engine. It maintains state, orchestrates tool-use cycles, and manages the conversation between the system and the LLM. It is intentionally agnostic about:

- How it is triggered (interactive message, webhook, cron schedule, file system event)
- Where its outputs go (terminal, web page, Slack channel, message queue)
- Which LLM provider serves inference
- Which tools are available
- Whether memory, reflection, or other meta-cognitive patterns are employed

The Core Loop provides extension hooks for middleware and lifecycle events. It is designed to be extensible enough to support patterns like memory and reflection in the future without prescribing their implementation today.

### I/O Surfaces

I/O Surfaces are adapters that translate external events into triggers for the Core Loop and route Loop outputs to external systems. They are **not** "UIs" in the narrow sense.

An I/O Surface can be:

- **Interactive** — TUI, web interface, Telegram or Discord bot
- **Event-driven** — Webhook receiver, message queue consumer, alert processor (e.g., PagerDuty → analysis → Slack notification)
- **Scheduled** — Cron-triggered jobs, periodic report generation
- **Service-oriented** — REST or gRPC endpoint, CLI one-shot, RPC over stdio
- **Streaming** — WebSocket server, SSE endpoint, log tailer

A Surface's contract with the Core Loop is about **ingress events** and **egress actions**, not about rendering chat windows.

### Extension Points

Extension Points are clean Go interfaces for capabilities that applications compose at build time. They are not runtime plugins or shared libraries; they are packages you import and wire together.

Examples of Extension Points include:

- **Tool interfaces** — Functions the agent can invoke
- **LLM Provider interfaces** — Adapters for OpenAI, Anthropic, local models, or custom endpoints
- **Middleware interfaces** — Hooks that intercept prompts, responses, or tool calls
- **Lifecycle interfaces** — Hooks for session start, end, compaction, or error handling

Extensions compose. They do not mutate the core.

### Agents / Applications

An Agent (or Application) is a runnable assembly that wires the Core Loop, one or more I/O Surfaces, and a set of Extensions together into a concrete system.

There is no single "tack" binary that does everything. Instead, there are compositions: a coding assistant with a TUI, a PR review bot triggered by GitHub webhooks, a scheduled log analyzer that posts to Slack. Each is a Go program that imports the pieces it needs and wires them in `main`.

## Design Principles

1. **Simplicity** — The Core Loop does as little as possible. Every feature that can live outside the core does.
2. **Composability** — Components connect through narrow interfaces. A Core Loop, a TUI surface, and an OpenAI provider compose the same way as a Core Loop, a webhook surface, and a local Ollama provider.
3. **I/O Agnosticism** — The Core Loop does not know whether it is running in an interactive terminal or responding to a 3 AM PagerDuty alert. Surfaces handle the world; the core handles the loop.
4. **Build-time Extension** — Extensions are Go packages composed at build time, not runtime plugins. This keeps deployment simple and interfaces type-safe.
5. **Defer Specifics** — Patterns like memory, reflection, planning, and multi-agent orchestration are enabled by the Core Loop's extensibility but are not designed in the core. They will emerge in Extension Points as concrete needs arise.

## Relationship to pi.dev

[tack] is conceptually descended from [pi.dev](https://pi.dev), a mature TypeScript terminal coding harness. pi.dev's philosophy of "minimal core, aggressive extensibility" is the direct inspiration for this project.

Where tack diverges:

- **Language** — Go instead of TypeScript. This is a learning exercise and an exploration of Go's deployment and runtime characteristics for agent systems.
- **I/O Surfaces** — pi.dev is primarily a TUI-centric tool with other modes (print, JSON, RPC) as secondary interfaces. tack treats all ingress/egress adapters as first-class, equally valid surfaces.
- **Extension Model** — pi.dev uses TypeScript modules and runtime package loading. tack uses Go interfaces and build-time composition.
- **Scope** — pi.dev is a production coding agent. tack is a framework for building agents, not a specific agent implementation.

## Project Status

This README is a vision document. It describes the architecture and design intent that coding agents should follow when implementing tack. Concrete interfaces, provider implementations, surfaces, and example applications will be discovered and refined case-by-case during development.

The repository currently contains only this north-star document. Implementation begins by defining the Core Loop's interfaces and a reference TUI surface.
