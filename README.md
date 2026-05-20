# ore

> ore are the inputs to an agentic system.
> forge is the agentic developer that builds with ore (and others, for that matter).

ore is a Go-native framework for building agentic applications. It provides a
minimal core inference primitive, provider-agnostic LLM adapters, composable I/O
conduits, and clean extension points implemented as Go interfaces.

This is a learning project and a conceptual exploration. It is inspired by
[pi.dev](https://pi.dev)'s philosophy of minimal cores and aggressive
extensibility, but reimagined in Go with different architectural priorities:
first-class non-interactive conduits, build-time composition via Go interfaces,
and a narrower core that delegates all workflow opinions to extensions and
applications.

## Design Principles

1. **Simplicity** — The core does as little as possible. Every feature that can
   live outside the core does.
2. **Composability** — Components connect through narrow interfaces. A Step,
   an OpenAI adapter, a tool handler, and a TUI conduit compose the same way as
   a Step, an Anthropic adapter, an image handler, and a webhook conduit.
3. **I/O Agnosticism** — The core does not know whether it is running in an
   interactive terminal or responding to a 3 AM PagerDuty alert.
4. **Build-time Extension** — Extensions are Go packages composed at build time,
   not runtime plugins. This keeps deployment simple and interfaces type-safe.
5. **Defer Specifics** — Patterns like memory, reflection, planning, reasoning
   strategies, multi-agent orchestration, and tool calling emerge as artifact
   handlers and orchestrators, not as core implementations.
6. **Treat Tool Calling as an Extension** — Tool calling is one artifact handler
   among many. This ensures the architecture can absorb future LLM capabilities
   without core changes.

## Relationship to pi.dev

[ore] is conceptually descended from [pi.dev](https://pi.dev), a mature
TypeScript terminal coding harness. Where ore diverges: **Language** (Go
instead of TypeScript), **I/O Conduits** (all ingress/egress adapters are
first-class, not just TUI), **Extension Model** (Go interfaces and build-time
composition instead of runtime module loading), and **Scope** (a framework for
building agents, not a specific agent implementation).

## Packages

| Package | Description | Docs |
|---|---|---|
| `artifact` | Extensible Artifact interface and common types (Text, ToolCall, Image, Reasoning, deltas) | [pkg.go.dev](https://pkg.go.dev/github.com/andrewhowdencom/ore@latest/artifact) |
| `state` | Conversation history model: State interface with Turns() and Append() | [pkg.go.dev](https://pkg.go.dev/github.com/andrewhowdencom/ore@latest/state) |
| `provider` | Provider interface and InvokeOption for LLM adapter contracts | [pkg.go.dev](https://pkg.go.dev/github.com/andrewhowdencom/ore@latest/provider) |
| `loop` | Single-turn execution primitive: Step with Turn(), handlers, and BeforeTurn hooks | [pkg.go.dev](https://pkg.go.dev/github.com/andrewhowdencom/ore@latest/loop) |
| `x/tool` | Provider-agnostic tool registry and artifact handler for executing LLM tool calls | [pkg.go.dev](https://pkg.go.dev/github.com/andrewhowdencom/ore@latest/x/tool) |
| `x/tool/calculator` | Reusable calculator tool implementations (Add, Multiply) | [pkg.go.dev](https://pkg.go.dev/github.com/andrewhowdencom/ore@latest/x/tool/calculator) |
| `x/tool/mcp` | MCP client implementing tool.RemoteSource for composing remote tools | [pkg.go.dev](https://pkg.go.dev/github.com/andrewhowdencom/ore@latest/x/tool/mcp) |
| `cognitive` | Cognitive patterns (ReAct) for multi-turn looping | [pkg.go.dev](https://pkg.go.dev/github.com/andrewhowdencom/ore@latest/cognitive) |
| `thread` | Persistent thread Store with UUID-based sessions and JSON persistence | [pkg.go.dev](https://pkg.go.dev/github.com/andrewhowdencom/ore@latest/thread) |
| `session` | Stream and Manager primitives for per-session inference orchestration | [pkg.go.dev](https://pkg.go.dev/github.com/andrewhowdencom/ore@latest/session) |
| `x/conduit` | I/O conduit interface and capability discovery for frontends | [pkg.go.dev](https://pkg.go.dev/github.com/andrewhowdencom/ore@latest/x/conduit) |
| `provider/openai` | OpenAI-compatible provider adapter with streaming and tool calling | [pkg.go.dev](https://pkg.go.dev/github.com/andrewhowdencom/ore@latest/provider/openai) |
| `cmd/forge` | Build-time agent generator from YAML blueprints | [README](cmd/forge/README.md) |

## Getting Started

The fastest way to build an agent is with **Forge**. See
[`cmd/forge/README.md`](cmd/forge/README.md) for CLI usage and
[`examples/forge/README.md`](examples/forge/README.md) for a guided tutorial
with HTTP, TUI, and multi-conduit blueprints.
