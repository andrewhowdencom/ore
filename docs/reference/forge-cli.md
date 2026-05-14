# Forge CLI Reference

The `forge` CLI generates compilable Go agent binaries from YAML manifests.

## Commands

### `forge build`

Generates `main.go` and `go.mod` from a manifest, runs `go mod tidy`, and compiles a binary.

```bash
forge build --config forge.yaml
```

Flags:
- `--config` — path to manifest file (default: `forge.yaml`, inherited from root)

### `forge generate`

Renders `main.go` and `go.mod` without compiling. Useful for debugging templates or integrating into custom build pipelines.

```bash
# Print to stdout
forge generate --config forge.yaml

# Write to a directory
forge generate --config forge.yaml -o ./my-agent/
```

Flags:
- `--config` — path to manifest file (default: `forge.yaml`, inherited from root)
- `-o`, `--output` — output directory (default: stdout)

### `forge version`

Prints version information.

```bash
forge version
```

## Global Flags

- `--config` — path to manifest file (default: `forge.yaml`)
- `--log-level` — log level: `debug`, `info`, `warn`, `error` (default: `info`)
- `-h`, `--help` — help for any command

## Backward Compatibility

When no subcommand is provided, `forge` defaults to `build` for backward compatibility:

```bash
forge --config forge.yaml   # equivalent to forge build --config forge.yaml
```

## Manifest Format

```yaml
dist:
  name: my-agent          # binary name used in go.mod
  output_path: ./my-agent # destination path (relative to cwd)
conduit:
  type: http              # "http" or "tui"
```

## Environment Variables

Generated binaries accept:
- `ORE_API_KEY` — required
- `ORE_MODEL` — defaults to `gpt-4o`
- `ORE_BASE_URL` — optional, for custom OpenAI-compatible endpoints
- `STORE_DIR` — optional, enables persistent JSON thread store
- `PORT` — for HTTP agents, defaults to `8080`
