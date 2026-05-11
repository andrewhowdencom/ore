# Plan: Rename Project from "tack" to "ore"

## Objective

Rename the entire Go module, all import paths, documentation, and environment variables from "tack" to "ore", ensuring the codebase compiles and all tests pass after the rename.

## Context

The project is currently named **tack** with Go module path `github.com/andrewhowdencom/tack`. The string "tack" appears in three broad categories:

1. **Go module system**: `go.mod` module declaration and import paths in 23+ `.go` and `_test.go` files across all packages (`artifact/`, `state/`, `provider/`, `loop/`, `tool/`, `surface/`, `cognitive/`, `examples/*/`).
2. **Human-readable references**: `README.md`, `AGENTS.md`, package doc comments (`doc.go` files and inline comments), and example file header comments.
3. **Runtime configuration**: Environment variables `TACK_API_KEY`, `TACK_MODEL`, and `TACK_BASE_URL` in the three example applications.

Additionally, four stale compiled binaries (`calculator`, `main`, `single-turn-cli`, `tui-chat`) exist in the working tree but are untracked by git. Seven existing `.plans/*.md` historical plan files also contain "tack" references and should be updated for consistency.

## Architectural Blueprint

This is a mechanical, non-architectural rename. There is only one viable path: perform a global, coordinated replacement of "tack" → "ore" and `TACK_` → `ORE_` across all relevant files, then verify compilation and tests. The `go.mod` module declaration and all import paths must be changed together — neither compiles without the other.

## Requirements

- [R1] The Go module path must change from `github.com/andrewhowdencom/tack` to `github.com/andrewhowdencom/ore`.
- [R2] All import paths must be updated accordingly across all `.go` and `_test.go` files.
- [R3] All package documentation (`doc.go` and inline comments) referencing "tack" must be updated to "ore".
- [R4] `README.md` and `AGENTS.md` must be updated to reference "ore".
- [R5] Environment variables must change from `TACK_*` to `ORE_*` in example applications.
- [R6] Stale compiled binaries must be removed from the working tree.
- [R7] Existing `.plans/*.md` historical plan files must be updated for consistency [inferred].
- [R8] The repository must compile and pass all tests after the rename.
- [R9] The `README.md` must include a new conceptual framing: **ore** as the inputs to an agentic system, and **forge** as the agentic developer that builds with ore (and others, for that matter).

## Task Breakdown

### Task 1: Rename Go Module and All Import Paths
- **Goal**: Update `go.mod` module declaration and every `github.com/andrewhowdencom/tack` import path to `github.com/andrewhowdencom/ore` across all Go source and test files.
- **Dependencies**: None
- **Files Affected**:
  - `go.mod`
  - `examples/single-turn-cli/main.go`
  - `examples/tui-chat/main.go`
  - `examples/calculator/main.go`
  - `provider/openai/openai.go`
  - `provider/openai/openai_test.go`
  - `provider/provider.go`
  - `state/memory.go`
  - `state/memory_test.go`
  - `state/state.go`
  - `surface/surface.go`
  - `surface/tui/model.go`
  - `surface/tui/model_test.go`
  - `surface/tui/tui.go`
  - `surface/tui/view.go`
  - `surface/tui/view_test.go`
  - `loop/handler.go`
  - `loop/loop.go`
  - `loop/loop_test.go`
  - `tool/handler.go`
  - `tool/handler_test.go`
  - `cognitive/react.go`
  - `cognitive/react_test.go`
- **New Files**: None
- **Interfaces**: No interface changes; only import path strings
- **Validation**:
  - `go mod tidy` runs cleanly
  - `go build ./...` compiles successfully
- **Details**:
  1. Edit `go.mod` line 1: `module github.com/andrewhowdencom/tack` → `module github.com/andrewhowdencom/ore`
  2. Globally replace `github.com/andrewhowdencom/tack` with `github.com/andrewhowdencom/ore` in every `.go` and `_test.go` file
  3. Run `go mod tidy` to regenerate `go.sum`
  4. Run `go build ./...` to verify compilation

### Task 2: Update Documentation and Package Comments
- **Goal**: Replace all human-readable "tack" references with "ore" in `README.md`, `AGENTS.md`, package doc comments, and inline comments.
- **Dependencies**: Task 1
- **Files Affected**:
  - `README.md`
  - `AGENTS.md`
  - `artifact/doc.go`
  - `artifact/artifact.go`
  - `state/doc.go`
  - `state/state.go`
  - `loop/doc.go`
  - `surface/surface.go`
  - `surface/tui/tui.go`
  - `tool/tool.go`
  - `provider/openai/openai.go` (inline comments: "converts tack state", "maps tack roles")
  - `examples/single-turn-cli/main.go` (header comment)
  - `examples/tui-chat/main.go` (header comment)
  - `examples/calculator/main.go` (header comment)
- **New Files**: None
- **Interfaces**: No interface changes
- **Validation**:
  - `go vet ./...` is clean
  - `go build ./...` still compiles
- **Details**:
  1. Update `README.md`:
     - Title: `# tack` → `# ore`
     - Tagline: replace the current "Token Fastener" tagline with a conceptual framing that explains: **ore** are the inputs to an agentic system, and **forge** is the agentic developer that builds with ore (and with other materials too). Add this as a short conceptual preamble near the top of the README, e.g. between the title and the Purpose section.
     - Update all remaining body references from "tack" to "ore"
  2. Update `AGENTS.md`: title `# tack Agent Conventions` → `# ore Agent Conventions`, update body references
  3. Update package doc comments in `artifact/doc.go`, `artifact/artifact.go`, `state/doc.go`, `state/state.go`, `loop/doc.go`
  4. Update comments in `surface/surface.go` ("the tack framework"), `surface/tui/tui.go` ("the tack framework"), `tool/tool.go` ("for tack")
  5. Update inline comments in `provider/openai/openai.go`
  6. Update example header comments in all three `examples/*/main.go` files

### Task 3: Rename Environment Variables
- **Goal**: Change all `TACK_*` environment variables to `ORE_*` in the three example applications.
- **Dependencies**: Task 1
- **Files Affected**:
  - `examples/single-turn-cli/main.go`
  - `examples/tui-chat/main.go`
  - `examples/calculator/main.go`
- **New Files**: None
- **Interfaces**: No interface changes
- **Validation**:
  - `go build ./...` compiles
  - `go vet ./...` is clean
- **Details**:
  1. Replace `TACK_API_KEY` with `ORE_API_KEY` (6 occurrences across 3 files, including error messages)
  2. Replace `TACK_MODEL` with `ORE_MODEL` (6 occurrences across 3 files)
  3. Replace `TACK_BASE_URL` with `ORE_BASE_URL` (6 occurrences across 3 files)

### Task 4: Update Historical Plan Files
- **Goal**: Update existing `.plans/*.md` files to reference "ore" instead of "tack" for consistency.
- **Dependencies**: Task 2
- **Files Affected**:
  - `.plans/add-tool-calling-with-extension-points.md`
  - `.plans/add-tui-scrollable-viewport.md`
  - `.plans/add-tui-with-streaming.md`
  - `.plans/extract-tui-surface-package.md`
  - `.plans/merge-core-step-into-loop.md`
  - `.plans/separate-cognitive-patterns-from-io-wiring.md`
  - `.plans/add-tui-streaming-feedback.md`
- **New Files**: None
- **Interfaces**: No interface changes
- **Validation**: No build validation; verify by reading or grep
- **Details**:
  1. Globally replace "tack" with "ore" in each affected `.plans/*.md` file
  2. Update any `github.com/andrewhowdencom/tack` module references in code blocks within the plans to `github.com/andrewhowdencom/ore`
  3. Leave `.plans/add-openai-streaming-reasoning-delta.md` and `.plans/add-tui-markdown-rendering.md` alone if they do not contain "tack" (verify with grep)

### Task 5: Remove Stale Binaries and Final Verification
- **Goal**: Remove untracked compiled binaries and run the full test suite to confirm the rename is complete and the repository is healthy.
- **Dependencies**: Task 1, Task 2, Task 3
- **Files Affected** (removal only):
  - `calculator`
  - `main`
  - `single-turn-cli`
  - `tui-chat`
- **New Files**: None
- **Interfaces**: No interface changes
- **Validation**:
  - `go build ./...` passes
  - `go test -race ./...` passes
  - `go vet ./...` passes
  - `grep -ri "tack\|TACK" --not-match-dir=.git --not-match-dir=.pi .` returns no matches in source files
- **Details**:
  1. Remove untracked binaries: `rm calculator main single-turn-cli tui-chat`
  2. Run `go build ./...`
  3. Run `go test -race ./...`
  4. Run `go vet ./...`
  5. Run a final grep sweep to confirm no "tack" or "TACK" references remain in any tracked source, documentation, or example files

## Dependency Graph

- Task 1 → Task 2
- Task 1 → Task 3
- Task 2 → Task 4
- Task 1, Task 2, Task 3 → Task 5 (parallel after Task 1 completes)

```
Task 1 ─┬─→ Task 2 ──→ Task 4
        ├─→ Task 3 ─────┘
        └──────────────→ Task 5
```

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Missing hidden references in unread files | Medium | Medium | Final grep sweep in Task 5 validation catches residual references |
| Go module cache issues after rename | Low | Low | Run `go clean -modcache` and `go mod tidy` in Task 1 |
| External consumers break (importers of the module) | High | N/A | Expected and intentional; this is a breaking rename |
| Stale binaries accidentally committed in future | Low | Low | They were already untracked; no action needed beyond Task 5 removal |
| Code examples inside `.plans/*.md` markdown blocks break during text replacement | Low | Medium | Use targeted edits in code blocks rather than global replace within those files |

## Validation Criteria

- [ ] `go.mod` declares `module github.com/andrewhowdencom/ore`
- [ ] `go build ./...` compiles without errors
- [ ] `go test -race ./...` passes
- [ ] `go vet ./...` is clean
- [ ] No `TACK_` environment variable references remain in any `.go` file
- [ ] No `github.com/andrewhowdencom/tack` import paths remain in any `.go` file
- [ ] No human-readable "tack" references remain in `README.md`, `AGENTS.md`, or package `doc.go` comments
- [ ] `README.md` contains the conceptual framing: ore as inputs to an agentic system, forge as the agentic developer that builds with ore (and others)
- [ ] Stale binaries (`calculator`, `main`, `single-turn-cli`, `tui-chat`) are removed from the working tree
- [ ] All `.plans/*.md` files are updated for consistency (where they previously contained "tack")
