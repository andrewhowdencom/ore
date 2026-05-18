# Forge Examples

This directory contains forge-native example blueprints that exercise the
current capabilities of `cmd/forge`. Each subdirectory holds a single
`forge.yaml` blueprint that the forge CLI consumes to generate a compilable
Go agent application.

These examples serve as a design exercise: by comparing the generated
binaries to the hand-compiled equivalents under `examples/http-chat/` and
`examples/tui-chat/`, the expressiveness gaps in the current blueprint schema
and templates become explicit.

## Quickstart

### HTTP Agent

```bash
cd examples/forge/http
go run ../../../cmd/forge build --config forge.yaml
./http-chat
```

### TUI Agent

```bash
cd examples/forge/tui
go run ../../../cmd/forge build --config forge.yaml
./tui-chat
```

### Multi-Conduit Agent (HTTP + TUI)

```bash
cd examples/forge/multi
go run ../../../cmd/forge build --config forge.yaml
./multi-agent
```

> **Note on `output_path`**: Forge resolves `dist.output_path` relative to the
current working directory at the time `cmd/forge` runs, not relative to the
blueprint file. Run forge from the example directory (as shown above) or use an
absolute path to control where the binary is written.

All agents require the same environment variables as their hand-compiled
counterparts:

- `ORE_API_KEY` — required
- `ORE_MODEL` — defaults to `gpt-4o`
- `ORE_BASE_URL` — optional, for custom OpenAI-compatible endpoints
- `STORE_DIR` — optional, enables persistent JSON thread store
- `PORT` — for the HTTP conduit only, defaults to `8080`

The TUI agent accepts an optional flag:

- `--thread` — resume an existing session by thread UUID

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
  - module: github.com/andrewhowdencom/ore/x/conduit/http
```

Multiple conduits can be declared to run concurrently:

```yaml
dist:
  name: my-agent
  output_path: ./my-agent
conduits:
  - module: github.com/andrewhowdencom/ore/x/conduit/http
  - module: github.com/andrewhowdencom/ore/x/conduit/tui
```

Each conduit entry can optionally include an `options` map for future
conduit-specific configuration:

```yaml
conduits:
  - module: github.com/andrewhowdencom/ore/x/conduit/http
    options:
      ui: true
      port: 8080
```

> **Note**: The `options` field is parsed and stored but not yet translated
> into Go constructor options in the generated template. Built-in conduits
> receive their default options (e.g. `WithUI()`, `WithAddr()` for HTTP).

## Comparison with Hand-Compiled Examples

The forge-generated applications closely mirror the runtime behavior of the
hand-compiled examples, but several features are currently impossible to
express in the blueprint schema.

### `examples/http-chat/`

| Feature | Hand-Compiled | Forge-Generated |
|---|---|---|
| HTTP conduit | ✅ | ✅ |
| `httpc.WithUI()` — built-in web chat UI | ✅ | ✅ |
| Tool registry (`add` / `multiply`) | ✅ | ❌ |
| Rich package documentation / usage guide | ✅ | ❌ (generic template) |

### `examples/tui-chat/`

| Feature | Hand-Compiled | Forge-Generated |
|---|---|---|
| TUI conduit | ✅ | ✅ |
| `--thread` flag for resuming sessions | ✅ | ✅ |
| JSON / memory thread store via `STORE_DIR` | ✅ | ✅ |
| Tool registry | ✅ | ❌ |
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
  function implementations, or JSON schemas.
- **Artifact rendering**: The template uses generic conduit rendering. Custom
  artifact handling (e.g. printing `Usage` tokens, formatting `Reasoning`
  blocks) must be hand-coded.

## Future Work

To close the gaps above, the blueprint schema and `cmd/forge` templates would
need to grow the following dimensions:

1. **No-conduit mode**: Support agent applications that do not need any
   conduit (direct `loop.Step` usage), such as CLI or batch jobs.
2. **Provider selection**: A `provider` stanza (e.g. `provider: {type: openai,
   model: gpt-4o, base_url: ...}`) to choose and configure provider adapters.
3. **Tool declarations**: A `tools` list where each entry provides a name,
   description, JSON schema, and a reference to a Go function implementation.
   This likely requires a companion plugin or code-generation mechanism,
   since tool *implementations* cannot be expressed in YAML alone.
4. **Conduit options translation**: Translate the YAML `options` map into
   Go functional options in the generated template (e.g. `http: {ui: true}`
   → `httpc.WithUI()`).
5. **Cognitive pattern selection**: A `cognitive` stanza to choose between
   `TurnProcessor`, `ReAct`, or future patterns.
6. **Custom artifact handlers**: A hook or template override for rendering
   artifact types that the built-in conduits do not handle natively.

These extensions would move forge from a simple scaffold toward a declarative
DSL for agent composition, while still keeping the framework's core
principle that complex logic belongs in Go code, not YAML.
