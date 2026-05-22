# Plan: Extract Workshop to Standalone Repository

## Objective

Extract the `examples/workshop/` terminal coding assistant from the `ore` repository into a standalone Go repository (`github.com/andrewhowdencom/workshop`). The new repository will import `ore` and its sub-modules as external Go module dependencies, demonstrating how to build agentic applications outside the `ore` examples directory. Bidirectional README references will link the two projects.

## Context

- `ore` is a Go-native agentic framework at `github.com/andrewhowdencom/ore`, structured as a Go workspace (`go.work`) with a root module and multiple sub-modules (`x/conduit/tui`, `x/provider/openai`, `x/tool/filesystem`, `x/tool/bash`, etc.).
- `examples/workshop/main.go` is a complete terminal-based coding assistant that composes: TUI conduit (`x/conduit/tui`), OpenAI provider (`x/provider/openai`), system prompt and guardrails transforms, filesystem/bash tools, ReAct cognitive pattern, and persistent thread storage.
- The workshop imports from both the root `ore` module (`cognitive`, `loop`, `session`, `thread`, `x/guardrails`, `x/systemprompt`) and sub-modules (`x/conduit/tui`, `x/provider/openai`, `x/tool`, `x/tool/filesystem`, `x/tool/bash`).
- `ore/README.md` currently lists `examples/workshop/` in its "Getting Started" examples section.
- The workshop directory contains only `main.go` (no tests or auxiliary files), making extraction clean.
- Historical plans (e.g., `.plans/enhance-workshop-with-filesystem-tools.md`) reference `examples/workshop/` but are archived documentation and do not need modification.

## Architectural Blueprint

### Selected Approach

Create a new standalone Go module `github.com/andrewhowdencom/workshop` that imports `ore` and its sub-modules as external dependencies. Copy `main.go` with minimal modifications (update package comment to reflect the new location). Remove the example directory from `ore`, update both READMEs to cross-reference each other.

### Rationale

This is the only viable path because the user explicitly requested a separate repository, and the workshop is already architecturally independent (no hardcoded filesystem paths, only package imports). Alternative approaches ruled out:

- **Git submodule**: Would keep the code physically inside `ore` while delegating version control, which contradicts the goal of a fully independent repository.
- **Workspace member / separate module within ore repo**: Would not create a truly standalone project that others can fork independently.
- **Monorepo with multiple roots**: Would not satisfy the requirement to reference the workshop as an external implementation.

### Dependency Resolution Strategy

The workshop imports both the root `ore` module and its sub-modules. In the new repository, these will be resolved via `go get`:

- `github.com/andrewhowdencom/ore` (root module)
- `github.com/andrewhowdencom/ore/x/conduit/tui`
- `github.com/andrewhowdencom/ore/x/provider/openai`
- `github.com/andrewhowdencom/ore/x/tool`
- `github.com/andrewhowdencom/ore/x/tool/filesystem`
- `github.com/andrewhowdencom/ore/x/tool/bash`

Because `ore` sub-modules use `replace` directives in the workspace, they may not be immediately resolvable from a module proxy during early development. The plan includes a mitigation: temporary `replace` directives in the workshop `go.mod` pointing to local ore paths, with a note to remove them once ore tags are published.

## Requirements

1. Create a new `github.com/andrewhowdencom/workshop` repository with the workshop code.
2. The new repository must have a `go.mod` that imports `github.com/andrewhowdencom/ore` and its sub-modules as dependencies.
3. `main.go` should be copied with minimal modifications (update package comment to reflect the new location and usage instructions).
4. Completely remove `ore/examples/workshop/` directory.
5. Update `ore/README.md` to remove the local `examples/workshop/` reference and add a link to the external repository.
6. Create a `README.md` in the new workshop repository that describes the project and links back to `ore`.
7. Ensure both repositories remain in a buildable state.

## Task Breakdown

### Task 1: Create the Standalone Workshop Repository

- **Goal**: Initialize `github.com/andrewhowdencom/workshop` with the workshop code, `go.mod`, and `README.md`.
- **Dependencies**: None
- **Files Affected**: None (new repository)
- **New Files**:
  - `workshop/main.go` (extracted from `ore/examples/workshop/main.go`)
  - `workshop/go.mod`
  - `workshop/go.sum`
  - `workshop/README.md`
- **Interfaces**: No new interfaces
- **Validation**: `go mod tidy` succeeds; `go build .` succeeds in the new repository
- **Details**:
  1. Create a new directory `workshop/` as the root of the new repository.
  2. Copy `ore/examples/workshop/main.go` to `workshop/main.go`.
  3. Update the package comment in `main.go`:
     - Change `go run ./examples/workshop` to `go run .`
     - Change `go run ./examples/workshop --thread <uuid>` to `go run . --thread <uuid>`
     - Change `STORE_DIR=/tmp/ore-store go run ./examples/workshop` to `STORE_DIR=/tmp/ore-store go run .`
  4. Initialize Go module: `go mod init github.com/andrewhowdencom/workshop`.
  5. Add ore dependencies. In the new repo directory, run:
     - `go get github.com/andrewhowdencom/ore@latest`
     - `go get github.com/andrewhowdencom/ore/x/conduit/tui@latest`
     - `go get github.com/andrewhowdencom/ore/x/provider/openai@latest`
     - `go get github.com/andrewhowdencom/ore/x/tool@latest`
     - `go get github.com/andrewhowdencom/ore/x/tool/filesystem@latest`
     - `go get github.com/andrewhowdencom/ore/x/tool/bash@latest`
  6. If `go get` fails for sub-modules (because they are not yet published or tagged), add temporary `replace` directives in `workshop/go.mod` pointing to the local ore paths (e.g., `replace github.com/andrewhowdencom/ore => ../ore`), run `go mod tidy`, verify the build, and document the temporary nature of the replace directives in the README.
  7. Create `README.md` with:
     - Project title and description (terminal coding assistant built on `ore`)
     - Prerequisites (`go`, OpenAI API key)
     - Usage instructions (same env vars and flags: `ORE_API_KEY`, `ORE_MODEL`, `STORE_DIR`, `--thread`)
     - Available tools summary (filesystem: read/write/edit/list/search, bash: execute shell commands)
     - "Built With" section linking to `github.com/andrewhowdencom/ore`
     - A note about the `replace` directives if they are needed during initial development
  8. Verify `go build .` compiles cleanly.

### Task 2: Remove Workshop from Ore Repository

- **Goal**: Completely remove `examples/workshop/` from `ore` and verify the repository remains healthy.
- **Dependencies**: None (parallelizable with Task 1)
- **Files Affected**: `ore/examples/workshop/main.go` (deleted)
- **New Files**: None
- **Interfaces**: None
- **Validation**: `go test ./...` passes in `ore`; no references to `examples/workshop` remain in live code
- **Details**:
  1. Delete the entire `ore/examples/workshop/` directory.
  2. Run `go test ./...` in `ore` to ensure no test or code depends on the workshop.
  3. Run `go build ./...` in `ore` to ensure all remaining examples and packages compile.
  4. Run `go work sync` to ensure the workspace is consistent.
  5. Search the `ore` codebase (excluding `.plans/`, `.worktrees/`, `.git/`) for any remaining references to `examples/workshop` in code or configuration and remove them.

### Task 3: Update READMEs with Bidirectional References

- **Goal**: Update both project READMEs to cross-reference each other.
- **Dependencies**: Task 1, Task 2
- **Files Affected**: `ore/README.md`
- **New Files**: `workshop/README.md` (content refined in Task 1, ore reference verified here)
- **Interfaces**: None
- **Validation**: Both READMEs render correctly as Markdown; `ore/README.md` no longer references local `examples/workshop`
- **Details**:
  1. In `ore/README.md`, in the "Getting Started" section:
     - Remove the bullet:
       ```markdown
       - [`examples/workshop/`](examples/workshop/) — Terminal coding assistant that
         composes `x/systemprompt` and `x/guardrails` transforms for persona injection
         and formatting rules.
       ```
     - Add a new paragraph after the examples list:
       ```markdown
       For a more fully fledged coding agent built on `ore`, see
       [`andrewhowdencom/workshop`](https://github.com/andrewhowdencom/workshop).
       ```
  2. In `workshop/README.md`:
     - Ensure a "Built With" or "Framework" section exists that links to `github.com/andrewhowdencom/ore`.
     - Include the description: "This project is built on [`ore`](https://github.com/andrewhowdencom/ore), a Go-native framework for building agentic applications."

## Dependency Graph

- Task 1 || Task 2 (parallelizable)
- Task 3 → Task 1, Task 2

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Ore sub-modules not resolvable by Go module proxy | High | Medium | During initial development, use temporary `replace` directives in workshop's `go.mod` pointing to local ore paths; document this in the workshop README as a temporary setup step; publish ore tags for sub-modules when ready |
| Workshop code drifts from ore API changes | Medium | Medium | Pin ore to a specific commit or tag in workshop's `go.mod`; set up CI in workshop repo to build against ore latest |
| Users confused by example relocation | Low | Medium | Prominent bidirectional README links; the `ore/README.md` explicitly points to the new external location |
| Workshop `main.go` package comment references outdated paths | Low | High | Explicitly included in Task 1 to update all `go run ./examples/workshop` references to `go run .` |

## Validation Criteria

- [ ] `github.com/andrewhowdencom/workshop` repository contains `main.go`, `go.mod`, and `README.md`
- [ ] `go build .` succeeds in the workshop repository
- [ ] `ore/examples/workshop/` directory no longer exists
- [ ] `go test ./...` passes in `ore` after removal
- [ ] `go build ./...` passes in `ore` after removal
- [ ] `ore/README.md` no longer lists `examples/workshop/` as a local example
- [ ] `ore/README.md` links to the external workshop repository
- [ ] `workshop/README.md` links back to `github.com/andrewhowdencom/ore`
- [ ] No remaining references to `examples/workshop` in live `ore` code (excluding historical plans)
- [ ] Workshop `main.go` package comment uses `go run .` instead of `go run ./examples/workshop`
