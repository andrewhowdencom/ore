# Getting Started with Forge Blueprint Agents

This directory contains forge-native example blueprints that exercise the
current capabilities of `cmd/forge`. Each subdirectory holds a single
`forge.yaml` blueprint that the forge CLI consumes to generate a compilable
Go agent application.

These examples serve as a design exercise: by comparing the generated
binaries to the hand-compiled equivalents under `examples/http-chat/` and
`examples/tui-chat/`, the expressiveness gaps in the current blueprint schema
and templates become explicit.

## Learning Objectives

By the end of this guide you will be able to:

- Write a forge blueprint YAML file that declares one or more conduits.
- Generate a compilable Go agent application using `cmd/forge`.
- Build and run a multi-conduit agent that serves HTTP and TUI simultaneously.

## Prerequisites

- Go toolchain matching the repository's `go.mod`.
- A valid `ORE_API_KEY` environment variable for the OpenAI-compatible provider.
- (Optional) `git` for cloning the repository.

## Quickstart

### HTTP Agent

```bash
cd examples/forge/http
go run ../../../cmd/forge build --config forge.yaml
./http-chat
```

> **Build succeeded** — the binary is written to `./http-chat` (as configured
> by `dist.output_path` in the blueprint). Run it to start the agent.

### TUI Agent

```bash
cd examples/forge/tui
go run ../../../cmd/forge build --config forge.yaml
./tui-chat
```

> **Build succeeded** — the binary is written to `./tui-chat`.

### Multi-Conduit Agent (HTTP + TUI)

```bash
cd examples/forge/multi
go run ../../../cmd/forge build --config forge.yaml
./multi-agent
```

> **Build succeeded** — the binary is written to `./multi-agent`.

> **Note on `output_path`**: Forge resolves `dist.output_path` relative to the
current working directory at the time `cmd/forge` runs, not relative to the
blueprint file. Run forge from the example directory (as shown above) or use an
absolute path to control where the binary is written.

The generated agent binaries read the following environment variables at
runtime:

- `ORE_API_KEY` — required
- `ORE_MODEL` — defaults to `gpt-4o`
- `ORE_BASE_URL` — optional, for custom OpenAI-compatible endpoints
- `ORE_STORE_DIR` — optional, enables persistent JSON thread store

Conduit-specific options can also be overridden via environment variables
using the `ORE_CONDUIT_<NAME>_<KEY>` convention. For example, the HTTP
conduit's listen address can be set with `ORE_CONDUIT_HTTP_ADDR=:9090`.
Handler options follow the same pattern: `ORE_HANDLER_<NAME>_<KEY>`.
Dots and hyphens in names are normalised to underscores.

Multi-conduit agents expose all capabilities of their constituent conduits.
For example, an HTTP+TUI agent listens on HTTP and also presents a TUI,
both sharing the same session manager.

## Blueprint Format

A minimal blueprint declares a distribution name and output path, plus one
or more conduits:

```yaml
dist:
  name: my-agent
  output_path: ./my-agent
conduits:
  - name: http
    module: github.com/andrewhowdencom/ore/x/conduit/http
```

Multiple conduits can be declared to run concurrently. Each entry must
have a unique `name` so that runtime configuration can target specific
instances. If `name` is omitted, it is derived from the last path element
of `module` (e.g. `http` for `.../x/conduit/http`). Duplicate modules
receive numeric suffixes (`http1`, `http2`, ...):

```yaml
dist:
  name: my-agent
  output_path: ./my-agent
conduits:
  - name: public-api
    module: github.com/andrewhowdencom/ore/x/conduit/http
  - name: internal-admin
    module: github.com/andrewhowdencom/ore/x/conduit/http
  - name: tui
    module: github.com/andrewhowdencom/ore/x/conduit/tui
```

Each conduit entry can optionally include an `options` map for
conduit-specific configuration. Options are translated into Go functional
option calls in the generated `main.go` via the conduit's `OptionsFromMap`
bridge:

```yaml
conduits:
  - module: github.com/andrewhowdencom/ore/x/conduit/http
    options:
      addr: ":8080"
      ui: false
```

Handler entries follow the same pattern, with the same `name` field
semantics. Handlers are instantiated inside the per-stream `stepFactory`
closure and wired into `loop.Step` via `loop.WithHandlers`:

```yaml
handlers:
  - name: tools
    module: github.com/andrewhowdencom/ore/tool
    options:
      verbose: true
```

### Conduit Options Reference

| Conduit | Option Key | Type | Description |
|---|---|---|---|
| HTTP | `addr` | string | TCP listen address (default: `:7654`) |
| HTTP | `ui` | bool | Enable the built-in web chat UI (default: `true`) |
| TUI | `thread_id` | string | Resume an existing thread instead of creating a new session |
| Slack | `bot_token` | string | Slack bot token (`xoxb-...`) |
| Slack | `app_token` | string | Slack app-level token (`xapp-...`) |
| Slack | `events_api` | bool | Use HTTP Events API instead of Socket Mode (default: `false`) |
| Telegram | `bot_token` | string | Telegram bot token |
| Telegram | `get_updates_timeout` | int | Long-polling timeout in seconds (default: `30`) |

## Comparison with Hand-Compiled Examples

The forge-generated applications closely mirror the runtime behavior of the
hand-compiled examples, but several features are currently impossible to
express in the blueprint schema.

### `examples/http-chat/`

| Feature | Hand-Compiled | Forge-Generated |
|---|---|---|
| HTTP conduit | ✅ | ✅ |
| `httpc.WithUI()` — built-in web chat UI | ✅ | ✅ |
| Conduit options (`addr`, `ui`) | ✅ | ✅ |
| Tool registry (`add` / `multiply`) | ✅ | ⚠️ (via handler modules) |
| Rich package documentation / usage guide | ✅ | ❌ (generic template) |

### `examples/tui-chat/`

| Feature | Hand-Compiled | Forge-Generated |
|---|---|---|
| TUI conduit | ✅ | ✅ |
| `--thread` flag for resuming sessions | ✅ | ❌ |
| Conduit options (`thread_id`) | ✅ | ✅ |
| JSON / memory thread store via `ORE_STORE_DIR` | ✅ | ✅ |
| Tool registry | ✅ | ⚠️ (via handler modules) |
| Rich package documentation / usage guide | ✅ | ❌ (generic template) |

### `examples/single-turn-cli/` and `examples/calculator/`

These two examples **cannot be expressed at all** in the current blueprint
schema because they do not use a conduit. Forge requires at least one
conduit module in the `conduits` array.

| Feature | Hand-Compiled | Forge-Generated |
|---|---|---|
| No conduit (direct `loop.Step` usage) | ✅ | ❌ |
| `cognitive.ReAct` pattern | ✅ (calculator) | ❌ |
| Custom artifact rendering / output formatting | ✅ | ❌ |
| Tool registry | ✅ (calculator) | ❌ |

### Common Gaps (All Examples)

- **Provider selection**: The template hardcodes `provider/openai`. No blueprint
  field exists to select a different provider adapter.
- **Cognitive pattern**: The template always wires `cognitive.NewTurnProcessor()`
  through `session.NewManager`. There is no way to request `cognitive.ReAct`
  or a custom cognitive loop.
- **Tool definitions**: There is no blueprint section for declaring tools,
  function implementations, or JSON schemas in YAML. Handler modules can
  implement tool registries in Go, but the tool schemas and function
  implementations themselves must still be written in Go code.
- **Custom artifact handlers**: ✅ Supported via the `handlers` list. Handler
  modules implementing `loop.Handler` are instantiated per-stream and
  wired into `loop.Step` automatically.

## Future Work

To close the gaps above, the blueprint schema and `cmd/forge` templates would
need to grow the following dimensions:

1. **No-conduit mode**: Support agent applications that do not need any
   conduit (direct `loop.Step` usage), such as CLI or batch jobs.
2. **Provider selection**: A `provider` stanza (e.g. `provider: {type: openai,
   model: gpt-4o, base_url: ...}`) to choose and configure provider adapters.
3. **Tool declarations** (with YAML-level function implementations): A `tools`
   list where each entry provides a name, description, JSON schema, and a
   reference to a Go function implementation. This likely requires a
   companion plugin or code-generation mechanism, since tool *implementations*
   cannot be expressed in YAML alone. Handler modules can bridge this gap
   by implementing the tool registry in Go.
4. **Conduit options translation** ✅ — Translate the YAML `options` map into
   Go functional options in the generated template (e.g. `http: {ui: true}`
   → `httpc.WithUI()`).
5. **Cognitive pattern selection**: A `cognitive` stanza to choose between
   `TurnProcessor`, `ReAct`, or future patterns.

These extensions would move forge from a simple scaffold toward a declarative
DSL for agent composition, while still keeping the framework's core
principle that complex logic belongs in Go code, not YAML.
