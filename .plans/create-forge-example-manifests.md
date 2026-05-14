# Plan: Create Forge Example Manifests

## Objective

Create two forge-native example manifests under `examples/forge/http/` and `examples/forge/tui/` that exercise the existing `cmd/forge` tool against its `http` and `tui` conduit templates. These serve as a living design exercise: by comparing the generated binaries to the hand-compiled `examples/http-chat/` and `examples/tui-chat/`, the exact expressiveness gaps in the current manifest schema and templates become explicit.

## Context

The repository contains `cmd/forge`, a CLI that reads a YAML manifest and generates a compilable Go agent application. The current manifest schema is minimal: `dist.name`, `dist.output_path`, and `conduit.type` (`http` or `tui`). The existing `cmd/forge/templates/main.go.tmpl` generates a generic application wiring `openai` provider, `thread` store, `loop.Step`, `session.Manager`, and the chosen conduit.

Four hand-compiled examples exist under `examples/`:
- `examples/http-chat/` — HTTP conduit with `httpc.WithUI()`, tool registry (`add`/`multiply`), and rich usage documentation
- `examples/tui-chat/` — TUI conduit with `--thread` flag, JSON/memory store, and session management
- `examples/single-turn-cli/` — No conduit; direct `loop.Step` usage with custom artifact rendering
- `examples/calculator/` — No conduit; `cognitive.ReAct` with tool registry and custom artifact rendering

The forge tool cannot yet express tool registries, `WithUI()`, provider selection, custom artifact rendering, or "no conduit" mode. The goal of this plan is to create runnable forge equivalents for the two conduit-bearing examples and document the gaps.

## Architectural Blueprint

The solution adds a new `examples/forge/` directory parallel to the existing hand-compiled examples. Each subdirectory contains a single `forge.yaml` manifest file. A top-level `README.md` documents how to run the manifests and enumerates the gaps between generated and hand-compiled equivalents. An automated test in `cmd/forge/` ensures the manifests remain compilable as the codebase evolves.

No expansion of the manifest schema or templates is in scope — this is strictly an exercise in mapping what the *current* forge can express onto the example applications.

## Requirements

1. Create `examples/forge/http/forge.yaml` — a runnable manifest producing an HTTP chat agent.
2. Create `examples/forge/tui/forge.yaml` — a runnable manifest producing a TUI chat agent.
3. Add `.gitignore` entries to prevent committing forge-generated binaries from the example directories.
4. Write `examples/forge/README.md` documenting usage and the expressiveness gaps vs. hand-compiled examples.
5. Extend `cmd/forge/forge_test.go` so the example manifests are validated in CI.

## Task Breakdown

### Task 1: Create `examples/forge/http/forge.yaml`
- **Goal**: Write a forge manifest for an HTTP chat agent that the current `cmd/forge` can consume.
- **Dependencies**: None.
- **Files Affected**: None (new file).
- **New Files**: `examples/forge/http/forge.yaml`.
- **Interfaces**: N/A.
- **Validation**: `go run ./cmd/forge -config examples/forge/http/forge.yaml` completes without error and produces a binary.
- **Details**: The manifest must include:
  - `dist.name: http-chat`
  - `dist.output_path: ./http-chat`
  - `conduit.type: http`
  - A YAML comment block showing the command to run it.

### Task 2: Create `examples/forge/tui/forge.yaml`
- **Goal**: Write a forge manifest for a TUI chat agent that the current `cmd/forge` can consume.
- **Dependencies**: None.
- **Files Affected**: None (new file).
- **New Files**: `examples/forge/tui/forge.yaml`.
- **Interfaces**: N/A.
- **Validation**: `go run ./cmd/forge -config examples/forge/tui/forge.yaml` completes without error and produces a binary.
- **Details**: The manifest must include:
  - `dist.name: tui-chat`
  - `dist.output_path: ./tui-chat`
  - `conduit.type: tui`
  - A YAML comment block showing the command to run it.

### Task 3: Add `.gitignore` entries for forge-generated binaries
- **Goal**: Prevent accidental commits of binaries produced by running the example manifests.
- **Dependencies**: Task 1, Task 2.
- **Files Affected**: `.gitignore`.
- **New Files**: None.
- **Interfaces**: N/A.
- **Validation**: `git check-ignore -v examples/forge/http/http-chat` and `git check-ignore -v examples/forge/tui/tui-chat` both return a rule match.
- **Details**: Append the following lines to the root `.gitignore`:
  ```
  # Forge-generated example binaries
  examples/forge/http/http-chat
  examples/forge/tui/tui-chat
  ```

### Task 4: Write `examples/forge/README.md`
- **Goal**: Document the forge examples, how to build them, and the expressiveness gaps they surface.
- **Dependencies**: Task 1, Task 2.
- **Files Affected**: None (new file).
- **New Files**: `examples/forge/README.md`.
- **Interfaces**: N/A.
- **Validation**: The README exists, references both manifests and their hand-compiled equivalents, and lists at least the known gaps enumerated below.
- **Details**: The README must contain:
  - A "Quickstart" section showing how to run each manifest (noting that `output_path` is resolved relative to the current working directory, so users should `cd` into the example directory or use an absolute path).
  - A "Comparison with Hand-Compiled Examples" section documenting the following gaps:
    - `examples/http-chat/`: The generated app omits `httpc.WithUI()` and has no tool registry (`add`/`multiply`).
    - `examples/tui-chat/`: The generated app has no tool registry.
    - Both: Provider is hardcoded to OpenAI; cognitive pattern is hardcoded to `cognitive.NewTurnProcessor()` via `session.Manager`.
    - `examples/single-turn-cli/` and `examples/calculator/`: Forge currently requires a conduit type, so these "no conduit" examples cannot be expressed at all.
  - A "Future Work" section summarizing what manifest schema extensions would be needed to close the gaps.

### Task 5: Extend `cmd/forge/forge_test.go` to validate example manifests
- **Goal**: Ensure the example manifests remain compilable as the codebase evolves.
- **Dependencies**: Task 1, Task 2.
- **Files Affected**: `cmd/forge/forge_test.go`.
- **New Files**: None.
- **Interfaces**: N/A.
- **Validation**: `go test -race ./cmd/forge/...` passes.
- **Details**: In `TestForgeSmoke`, add two entries to the `tests` slice:
  - `{name: "http-example", manifestPath: "../../examples/forge/http/forge.yaml"}`
  - `{name: "tui-example", manifestPath: "../../examples/forge/tui/forge.yaml"}`
  The existing test body already opens the manifest, calls `Build` with a temp-directory output path, and asserts the binary exists — no further test logic changes are required.

## Dependency Graph

- Task 1 || Task 2 (parallel)
- Task 1, Task 2 → Task 3
- Task 1, Task 2 → Task 4
- Task 1, Task 2 → Task 5
- Task 3 || Task 4 || Task 5 (parallel after Tasks 1 and 2)

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Generated binaries from manual forge runs clutter the working tree | Low | High | `.gitignore` entries in Task 3 prevent accidental commits. |
| `cmd/forge/forge_test.go` relative paths break if package directory changes | Low | Low | The paths `../../examples/forge/...` are stable relative to `cmd/forge/`. If the package is relocated, the test will fail loudly and the path can be corrected. |
| `go run ./cmd/forge` from repo root writes binary to repo root due to relative `output_path` resolution | Low | Medium | Document the CWD-relative behavior in the README. |

## Validation Criteria

- [ ] `go run ./cmd/forge -config examples/forge/http/forge.yaml` succeeds and produces an executable.
- [ ] `go run ./cmd/forge -config examples/forge/tui/forge.yaml` succeeds and produces an executable.
- [ ] `go test -race ./cmd/forge/...` passes, including the new smoke-test cases.
- [ ] `go test -race ./...` passes for the full repository.
- [ ] `git check-ignore -v examples/forge/http/http-chat` returns a `.gitignore` rule match.
- [ ] `examples/forge/README.md` exists and documents the expressiveness gaps vs. all four hand-compiled examples.
