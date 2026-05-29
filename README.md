# ore

> ore are the inputs to an agentic system.

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
| `loop` | Single-turn execution primitive: Step with Turn(), handlers, and inference assembly transforms | [pkg.go.dev](https://pkg.go.dev/github.com/andrewhowdencom/ore@latest/loop) |
| `tool` | Tool interface, registry, and sandbox contracts for LLM tool calling | [pkg.go.dev](https://pkg.go.dev/github.com/andrewhowdencom/ore@latest/tool) |
| `cognitive` | Cognitive patterns (ReAct) for multi-turn looping | [pkg.go.dev](https://pkg.go.dev/github.com/andrewhowdencom/ore@latest/cognitive) |
| `session` | Stream and Manager primitives for per-session inference orchestration | [pkg.go.dev](https://pkg.go.dev/github.com/andrewhowdencom/ore@latest/session) |
| `agent` | Multi-conduit orchestration container and application scaffold | [pkg.go.dev](https://pkg.go.dev/github.com/andrewhowdencom/ore@latest/agent) |

Extensions (provider adapters, conduits, and tool implementations) live under `x/`.


## Getting Started

The fastest way to understand ore is to run one of the hand-written examples.
Each is a complete, compilable Go program that wires the framework together
without any YAML or code generation layer.

- [`examples/http-chat/`](examples/http-chat/) — Stateful HTTP chat server with
  NDJSON streaming, SSE events, and an optional web UI.
- [`examples/tui-chat/`](examples/tui-chat/) — Interactive terminal chat with
  Markdown rendering and persistent thread store.
- [`examples/calculator/`](examples/calculator/) — Single-turn CLI demo with
  tool calling (add, multiply) via the ReAct cognitive pattern.
- [`examples/filesystem/`](examples/filesystem/) — File system tool demo with
  path resolution and working directory constraints.
- [`examples/single-turn-cli/`](examples/single-turn-cli/) — Minimal single-turn
  CLI example with direct provider invocation.

For a more fully fledged coding agent built on `ore`, see
[`andrewhowdencom/workshop`](https://github.com/andrewhowdencom/workshop).

All examples read `ORE_API_KEY` from the environment. Set `ORE_MODEL` to choose
a different model (default: `gpt-4o`). Set `STORE_DIR` for persistent JSON
thread storage.

See [Development / Validation](#development--validation) for how to validate
the entire codebase before submitting changes.

## Development / Validation

The repository uses **Taskfile** for common developer commands. The root task
`validate` runs the full lint–test–build pipeline for **all 16 workspace
modules** (root, `examples/`, and every `x/*` package):

```bash
task validate
```

Validate a single module only (e.g. the TUI conduit):

```bash
task x-conduit-tui:validate
```

Running `task validate` from the repo root therefore guarantees that the
entire codebase stays healthy.
