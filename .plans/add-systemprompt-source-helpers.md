# Plan: Add x/systemprompt/source Helpers for External Prompt Content

## Objective

Create a helper sub-package `x/systemprompt/source` that provides factory functions returning `func() string` closures compatible with `systemprompt.WithContentFunc`. These helpers enable ergonomic injection of external prompt content — specifically file reading and AGENTS.md/CLAUDE.md directory-walking discovery — into the system prompt transform, keeping the core `x/systemprompt` package minimal and free of filesystem-specific APIs.

## Context

- The repository is `github.com/andrewhowdencom/ore`, a Go framework for building agentic applications.
- The core `x/systemprompt` package (in `x/systemprompt/systemprompt.go`) provides composable content functions via `WithContentFunc` and `WithContextContentFunc`, but has no opinion about where content comes from.
- Applications currently wire prompt content manually by constructing closures. There is no ergonomic way to read files or walk directories for AGENTS.md discovery.
- An `AGENTS.md` exists at the repository root (`./AGENTS.md`), establishing the pattern this helper targets.
- Workshop (not present in this worktree) currently wires prompt content manually and should be updated in a follow-up issue.
- Existing `x/` extension packages (e.g., `x/guardrails/`) follow the pattern of a `doc.go`, implementation file, and test file, all within the main module (no separate `go.mod`).

## Architectural Blueprint

Add a new leaf sub-package `x/systemprompt/source` under the existing `x/` extension directory. This package provides factory functions that return closures compatible with `systemprompt.WithContentFunc`, bridging the gap between content acquisition (files, directory traversal) and content composition (the core transform).

**Key design decisions:**

- `File(path)` re-reads the file on every call (cheap for local files), returning the content or an empty string if the file is missing.
- `AgentsMD(startDir)` walks parent directories from `startDir` toward the filesystem root, checking each level for `AGENTS.md` and `CLAUDE.md`. All found files are concatenated in discovery order (nearest first), separated by `\n\n`.
- No error return from the closures — the `WithContentFunc` signature is `func() string`. Missing files silently produce empty strings.
- No logging for missing files in the initial implementation; debug logging can be added later if needed.
- The package is a leaf with no internal dependencies beyond the Go standard library.

**Evaluated alternatives:**

1. **Add File/AgentsMD directly to `x/systemprompt`** — Rejected because it bloats the core transform package with filesystem-specific APIs, violating the minimal-core philosophy.
2. **Add caching to `File`/`AgentsMD`** — Rejected for the initial implementation. Re-reading on every turn is cheap for local files and aligns with the dynamic system prompt pattern already supported by `systemprompt.Transform`.
3. **HTTP/Env helpers in the initial PR** — Rejected. The issue explicitly scopes the initial work to `File` and `AgentsMD` only.

## Requirements

1. `x/systemprompt/source` package created with `File` and `AgentsMD` factories.
2. `File(path string) func() string` re-reads the file on every call; returns content or empty string if missing.
3. `AgentsMD(startDir string) func() string` walks parent directories, discovers `AGENTS.md` and `CLAUDE.md`, concatenates contents in nearest-first discovery order.
4. Table-driven tests covering all acceptance criteria scenarios.
5. `go test -race ./...` passes from repo root.
6. Package doc comment (`doc.go`) explains the separation: core `systemprompt` is for composition, `source` is for content acquisition.
7. Workshop update deferred to a follow-up issue (not in this worktree).

## Task Breakdown

### Task 1: Create x/systemprompt/source Package
- **Goal**: Implement `File` and `AgentsMD` factory functions with package documentation and table-driven tests.
- **Dependencies**: None.
- **Files Affected**: None (all new files).
- **New Files**:
  - `x/systemprompt/source/doc.go`
  - `x/systemprompt/source/source.go`
  - `x/systemprompt/source/source_test.go`
- **Interfaces**:
  - `func File(path string) func() string`
  - `func AgentsMD(startDir string) func() string`
- **Validation**:
  - `go test -race ./x/systemprompt/source/...` passes.
  - `go test -race ./...` from repo root passes.
- **Details**:
  - **`doc.go`**: Package comment explaining that `x/systemprompt` is for composition (the `Transform` and `WithContentFunc` APIs) while `x/systemprompt/source` is for acquiring content from external sources (files, directory traversal). Include a short usage example showing `systemprompt.New(systemprompt.WithContentFunc(source.File("prompt.txt")))`.
  - **`source.go`**:
    - `File(path string) func() string`: Uses `os.ReadFile` on every call. If the file exists, returns its content as a string. If missing or unreadable, returns `""`. No caching.
    - `AgentsMD(startDir string) func() string`: Walks parent directories using `filepath.Dir` until reaching the filesystem root. At each directory level, checks for `AGENTS.md` then `CLAUDE.md` (in that order). Collects contents of all found files. Returns them joined with `\n\n` separators. Returns `""` if no files are found anywhere in the walk.
  - **`source_test.go`**: Table-driven tests using `t.TempDir()` to create isolated directory hierarchies. Test cases must cover:
    - `File` with existing file → content returned.
    - `File` with missing file → empty string.
    - `AgentsMD` with no files found → empty string.
    - `AgentsMD` with single `AGENTS.md` in start dir → content returned.
    - `AgentsMD` with single `AGENTS.md` in a parent dir → content returned.
    - `AgentsMD` with `AGENTS.md` and `CLAUDE.md` in the same dir → both concatenated (AGENTS.md first).
    - `AgentsMD` with multiple files in parent dirs → concatenated in nearest-first discovery order.

## Dependency Graph

- Task 1 (no dependencies; single hermetic unit).

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Workshop is in a different repo/worktree and cannot be updated in this PR | Medium | High | File a follow-up issue referencing this plan for workshop adoption of `source.AgentsMD`. |
| `AgentsMD` directory walk behavior differs across OS path separators | Low | Medium | Use `filepath` package (not hardcoded `/`) for all path construction in the implementation. |
| File reading closures cause performance issues in high-turn loops | Low | Low | Document that `File` re-reads on every turn; applications needing caching can wrap the closure themselves. |

## Validation Criteria

- [ ] `x/systemprompt/source/` directory exists with `doc.go`, `source.go`, and `source_test.go`.
- [ ] `File` factory reads files dynamically and returns empty string for missing files.
- [ ] `AgentsMD` walks parent directories, discovers `AGENTS.md` and `CLAUDE.md`, and concatenates contents in nearest-first order.
- [ ] Table-driven tests exist and pass (`go test ./x/systemprompt/source/...`).
- [ ] Race detector passes (`go test -race ./...`).
- [ ] Package doc comment clearly explains the separation between core `systemprompt` (composition) and `source` (content acquisition).
- [ ] A follow-up issue is filed (or noted in issue comments) for Workshop to adopt `source.AgentsMD`.
