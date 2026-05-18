# Forge CLI

`cmd/forge` is a build-time tool that turns a YAML manifest into a runnable Go
agent binary. It generates the `main.go` and `go.mod` for an agent application,
resolves the local ore module path, and compiles the binary with `go build`.

## Commands

### `forge build`

Generates and compiles an agent binary. This is the default when no
subcommand is given.

```bash
# Build (explicit)
go run ./cmd/forge build --config forge.yaml

# Build (implicit default — backward compatible)
go run ./cmd/forge --config forge.yaml
```

### `forge generate`

Renders `main.go` and `go.mod` without compiling. Useful for debugging
templates or custom build pipelines.

```bash
# Generate to stdout
go run ./cmd/forge generate --config forge.yaml

# Generate to a directory
go run ./cmd/forge generate --config forge.yaml -o ./my-agent/
```

### `forge version`

Prints version information.

```bash
go run ./cmd/forge version
```

## Global Flags

- `--config` — path to manifest file (default: `forge.yaml`)
- `--log-level` — log level: `debug`, `info`, `warn`, `error` (default: `info`)
- `-h`, `--help` — help for any command

## Backward Compatibility

For backward compatibility, the historic single-dash `-config` flag is
automatically rewritten to `--config` internally, so existing scripts continue
to work.

## Manifest Format

The manifest is a single YAML file with two top-level sections:

```yaml
dist:
  name: my-agent          # binary name used in go.mod
  output_path: ./my-agent # destination path (relative to cwd)
conduits:
  - module: github.com/andrewhowdencom/ore/x/conduit/http
```

Multiple conduits can be declared to run concurrently:

```yaml
conduits:
  - module: github.com/andrewhowdencom/ore/x/conduit/http
  - module: github.com/andrewhowdencom/ore/x/conduit/tui
```

## Environment Variables

Generated binaries accept the following environment variables at runtime:

| Variable | Description | Default |
|---|---|---|
| `ORE_API_KEY` | API key for the LLM provider | *(required)* |
| `ORE_MODEL` | Model name | `gpt-4o` |
| `ORE_BASE_URL` | Custom OpenAI-compatible endpoint URL | *(optional)* |
| `STORE_DIR` | Enables persistent JSON thread store | *(optional)* |
| `PORT` | Listen port for HTTP conduits | `8080` |

## Resuming Threads

For TUI conduits, pass `--thread <uuid>` to resume an existing thread that was
started in another frontend (e.g., the HTTP conduit).

## Further Reading

- **[Getting Started Guide](../examples/forge/README.md)** — guided tutorial
  with example blueprints for HTTP, TUI, and multi-conduit agents.
- **[CLI Reference](../docs/reference/forge-cli.md)** — auto-generated command
  reference.
