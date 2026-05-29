# Contributing to ore

We welcome contributions! Before opening a PR, please make sure the
repository validates cleanly.

## Validation

- **Full repository validation** — checks linting, unit tests (with race
detector), and builds **all** modules:

  ```bash
  task validate
  ```

- **Validate a single module** — useful when iterating on a specific conduit,
provider, or tool:

  ```bash
  # Example: only the OpenAI provider adapter
  task x-provider-openai:validate
  ```

  This runs the shared `validate` task defined in `Taskfile.lib.yml` for the
  selected workspace.

## Quick-start

```bash
# Install required tools (golangci-lint, etc.)
brew install golangci/tap/golangci-lint   # macOS example

# Or use the setup task which runs `go mod download`
task setup
```

For more details on the overall architecture, see the project README.
