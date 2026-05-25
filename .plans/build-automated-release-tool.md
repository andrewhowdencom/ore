# Plan: Build Automated Release Tool for Multi-Module Go Monorepo

## Objective

Implement `cmd/release`, a custom CLI tool that automates independent semver releases for the ore multi-module Go monorepo. The tool discovers modules from `go.mod` files, determines version bumps via conventional commit analysis, maps commits to modules through a file-path heuristic, lazily updates cross-module `go.mod` dependencies, and performs end-to-end release orchestration: `go mod tidy`, git commit, annotated tag creation, and push to `origin/main`.

## Context

### Repository State

- **Branch**: `179` (clean working tree).
- **Module layout**: 13 modules total. Root module at repo root; submodules under `x/` and `examples/`.
- **`go.work`**: Already exists and lists all 13 modules (`examples`, `x/conduit`, `x/conduit/http`, `x/conduit/slack`, `x/conduit/telegram`, `x/conduit/tui`, `x/provider/openai`, `x/tool`, `x/tool/bash`, `x/tool/calculator`, `x/tool/filesystem`, `x/tool/mcp`, `x/tool/skills`).
- **Root `go.mod`**: Clean — no `replace` or `require` entries for `x/` paths.
- **Submodule `go.mod` files**: Most are clean of `replace` directives. One exception remains: `x/conduit/tui/go.mod` still contains two `replace` directives (`github.com/andrewhowdencom/ore` and `github.com/andrewhowdencom/ore/x/conduit`). This is lingering structural debt from the earlier module-cleanup plan.
- **Git tags**: Only one tag exists: `0.0.1` — invalid because it lacks the required `v` prefix. Go tooling ignores it.
- **`cmd/` directory**: Does not exist.
- **`Taskfile.yml`**: Exists with `setup`, `build`, `test`, `lint`, `generate`, and `validate` targets. No release task yet.
- **`examples/go.mod`**: Currently has no explicit ore `require` entries because `go mod tidy` in workspace mode resolved everything through the workspace. For published modules, `GOWORK=off go mod tidy` will be required to write clean, external-consumable `go.mod` files.

### Structural Predecessor

The plan `.plans/break-module-cycles-and-enable-release.md` addressed module restructuring (`go.work`, `replace` removal, parent-package extraction). That work is largely complete except for the two remaining `replace` directives in `x/conduit/tui/go.mod`. This release plan assumes completion of that structural work and includes a small cleanup task to close the gap.

## Architectural Blueprint

### Selected Architecture

The release tool is a single Go binary under `cmd/release/` within the **root module**. It uses minimal external dependencies and standard-library-heavy patterns consistent with the ore project philosophy.

| Concern | Decision | Rationale |
|---|---|---|
| **Git operations** | `os/exec` shell-out to `git` | Simpler, more reliable for annotated tags, push with auth, and remote operations than a pure-Go library. Git is a given in any release environment. |
| **`go.mod` manipulation** | `golang.org/x/mod/modfile` | Official Go-team library for parsing and editing `go.mod` files. Robust against formatting edge cases. |
| **CLI framework** | Lightweight `flag` + `os.Args` subcommand parser | Avoids importing Cobra or similar heavy CLI frameworks. Only three subcommands are needed. |
| **Conventional commit parsing** | Small inline regex/parser | We only need `fix` → patch, `feat` → minor, `!` or `BREAKING CHANGE` → major. No need for an external parser dependency. |
| **Commit-to-module mapping** | File-path prefix heuristic | A commit affects a module if any changed file path starts with that module's directory. Root module captures files not under any submodule directory. Simple, fast, and deterministic. |
| **Release ordering** | Topological sort over module dependency graph | Dependencies must be tagged before consumers so that consumer `go.mod` updates can reference real, existing tags. |
| **`go mod tidy` isolation** | `GOWORK=off` | Ensures each module's `go.mod` is valid for external consumers, not just within the local workspace. |

### Release Workflow (`release all`)

1. **Discover** all modules by scanning for `go.mod` files.
2. **Resolve latest tag** per module (root: `v*`, submodules: `<path>/v*`).
3. **Map commits** since each tag to affected modules via file paths.
4. **Parse** conventional commits for those mapped commits → bump type (patch/minor/major).
5. **Filter** to modules with unreleased changes.
6. **Build dependency graph** from `go.mod` `require` blocks; topologically sort.
7. **For each module in topological order**:
   a. Update its `go.mod` to reference the latest tagged versions of its ore dependencies (lazy: only if a newer tag exists).
   b. Run `GOWORK=off go mod tidy`.
   c. If tidy fails, abort the entire release.
   d. Stage `go.mod` + `go.sum`.
   e. Commit with message `chore(release): bump <module> to <version>`.
   f. Create annotated tag: root `vX.Y.Z`, submodule `<path>/vX.Y.Z`.
8. **Push** commits and tags to `origin/main`.

### Tag Format

- Root module: `vX.Y.Z` (e.g., `v0.1.0`)
- Submodules: `<module-import-path-suffix>/vX.Y.Z` (e.g., `x/provider/openai/v0.1.0`, `x/tool/skills/v0.1.0`)

### First Release Bootstrap

The root module has never had a valid tag. Because `cmd/release` is part of the root module, it cannot release itself before it exists. The implementer must handle the bootstrap manually: either tag the root module by hand before using the tool, or run the tool via `go run ./cmd/release` on its first invocation and let the "no previous tag" logic start at `v0.1.0` (or `v0.0.1` if only fixes). This plan notes the bootstrap as an implementer discretion item rather than mandating a specific approach.

## Requirements

1. Remove the invalid `0.0.1` git tag (local and remote). [from issue]
2. Remove remaining `replace` directives from `x/conduit/tui/go.mod` so all published `go.mod` files are clean. [inferred: prerequisite for reliable `go mod tidy`]
3. Implement `cmd/release` as a Go binary in the root module.
4. Support three CLI entry points: `release status`, `release all`, `release <module-path>`.
5. Discover all modules automatically by scanning the repository for `go.mod` files.
6. Resolve the latest semver tag per module using prefix matching (`v*` for root, `<path>/v*` for submodules).
7. Map commits to modules via file-path prefix heuristic.
8. Parse conventional commits (Angular style) to determine patch/minor/major bumps.
9. Update `go.mod` dependencies on other ore modules lazily — only when the consuming module itself is being released, and only if a newer tag exists.
10. Run `GOWORK=off go mod tidy` for each released module; treat failure as a hard abort.
11. Commit `go.mod`/`go.sum` changes directly to `main` with a conventional commit message.
12. Create annotated git tags and push them automatically to `origin/main`.
13. Release modules in dependency order (topological sort) so downstream modules can reference upstream tags that already exist.
14. Add a `task release` target to `Taskfile.yml`. [from issue]

## Task Breakdown

### Task 1: Clean Up Remaining Structural Debt in `x/conduit/tui/go.mod`
- **Goal**: Remove the two lingering `replace` directives from `x/conduit/tui/go.mod` so the file is valid for external consumers.
- **Dependencies**: None.
- **Files Affected**: `x/conduit/tui/go.mod`
- **New Files**: None.
- **Interfaces**: None.
- **Validation**: `go work sync` completes without errors. `go build ./...` and `go test ./...` inside `x/conduit/tui/` pass. `grep -r "replace " x/conduit/tui/go.mod` returns nothing.
- **Details**: Delete the `replace github.com/andrewhowdencom/ore => ../../../` and `replace github.com/andrewhowdencom/ore/x/conduit => ../../conduit` lines. Run `go work sync` from the repo root, then `cd x/conduit/tui && go mod tidy`. Verify the module still compiles and tests pass.

### Task 2: Remove Invalid `0.0.1` Git Tag
- **Goal**: Delete the malformed `0.0.1` tag locally and from the remote.
- **Dependencies**: None.
- **Files Affected**: None (git operation only).
- **New Files**: None.
- **Interfaces**: None.
- **Validation**: `git tag -l` in the local repo returns nothing. The remote no longer lists `0.0.1`.
- **Details**: Run `git tag -d 0.0.1` and `git push origin :refs/tags/0.0.1`. This tag is invalid because Go requires the `v` prefix; its presence is harmless but noisy.

### Task 3: Scaffold `cmd/release` Package and CLI Entrypoint
- **Goal**: Create the `cmd/release` directory, main entrypoint, and lightweight subcommand dispatcher.
- **Dependencies**: None.
- **Files Affected**: `go.mod` (to add `golang.org/x/mod` dependency)
- **New Files**: `cmd/release/main.go`
- **Interfaces**: CLI contract: `release status`, `release all`, `release <path>`.
- **Validation**: `go build ./cmd/release` succeeds. `go run ./cmd/release --help` (or equivalent) prints usage.
- **Details**: Use `os.Args` and the standard `flag` package for subcommand parsing. Do not import Cobra or other CLI frameworks. Add `golang.org/x/mod` to the root module via `go get golang.org/x/mod`. The entrypoint should dispatch to placeholder functions for `status`, `all`, and `path` subcommands. Keep the code simple and testable by delegating to internal packages/functions rather than doing everything in `main`.

### Task 4: Implement Module Discovery, Tag Resolution, and Commit-to-Module Mapping
- **Goal**: Build the core data-gathering engine: find modules, find their latest tags, and map commits to affected modules.
- **Dependencies**: Task 3.
- **Files Affected**: `cmd/release/main.go` (or new helper files in `cmd/release/`)
- **New Files**: New helper `.go` files under `cmd/release/` as needed (e.g., `discover.go`, `tags.go`, `commits.go`).
- **Interfaces**:
  - `func discoverModules(root string) ([]Module, error)` — scans for `go.mod` files.
  - `func latestTag(modulePath string) (string, error)` — runs `git tag --list <pattern>` and parses semver.
  - `func commitsSinceTag(tag string) ([]Commit, error)` — runs `git log <tag>..HEAD` with file info.
  - `func mapCommitsToModules(commits []Commit, modules []Module) map[string][]Commit` — file-prefix heuristic.
- **Validation**: Unit tests pass (`go test ./cmd/release/...`). Tests should cover:
  - Tag parsing (`v0.1.0`, `x/foo/v0.2.3`, invalid tags).
  - Commit-to-module mapping with mock commit data.
  - Module discovery from a mock filesystem.
- **Details**:
  - Module discovery: recursively find `go.mod`, extract module path from the `module` directive.
  - Latest tag: use `git tag --list '<prefix>v*' --sort=-v:refname` to get the highest semver. Handle "no tags" as a nil/empty result.
  - Commit mapping: for each commit, inspect changed file paths (`git diff-tree --no-commit-id --name-only -r <sha>`). A file path starting with a module's directory means the commit affects that module. Root module is affected by files that do not belong to any submodule directory.

### Task 5: Implement Conventional Commit Parsing and `release status` Subcommand
- **Goal**: Determine version bumps from commit messages and expose them via the `status` command.
- **Dependencies**: Task 4.
- **Files Affected**: `cmd/release/` source files.
- **New Files**: New helper `.go` file for commit parsing (e.g., `bump.go`).
- **Interfaces**:
  - `func bumpType(msgs []string) Bump` — returns `Patch`, `Minor`, `Major`, or `None`.
  - `func nextVersion(current string, bump Bump) (string, error)` — increments semver.
- **Validation**: `go run ./cmd/release status` prints a table/list showing each module, its latest tag, number of unreleased commits, and the proposed bump + new version. Unit tests for commit parsing pass.
- **Details**:
  - Conventional commit rules:
    - `fix:` or any non-breaking non-feat → patch.
    - `feat:` without `!` → minor.
    - Any type with `!` (e.g., `feat!:`) or any commit containing `BREAKING CHANGE:` in the body → major.
    - Commits not matching conventional format: treat as patch (since any commit implies a change) **or** treat as no-bump. The issue implies conventional commits drive bumps; for non-conventional commits, patch is the safer default so nothing gets silently dropped.
  - If a module has no unreleased commits, show `None`.
  - For a module with no previous tag, the first release defaults to `v0.1.0` if any commits exist (implementer discretion per issue).

### Task 6: Implement `go.mod` Dependency Updates and Topological Release Ordering
- **Goal**: Update ore dependencies in a module's `go.mod` before release, and determine the correct release order.
- **Dependencies**: Task 4, Task 3.
- **Files Affected**: `cmd/release/` source files.
- **New Files**: New helper `.go` files (e.g., `gomod.go`, `graph.go`).
- **Interfaces**:
  - `func updateModuleDeps(mod *modfile.File, latestTags map[string]string) error` — updates `require` versions.
  - `func topologicalSort(modules []Module) ([]Module, error)` — dependency-order release.
- **Validation**: Unit tests for topological sort pass. Manual test: running the update logic on one submodule writes correct `require` versions and `go mod tidy` (with `GOWORK=off`) succeeds.
- **Details**:
  - Parse each module's `go.mod` with `golang.org/x/mod/modfile`.
  - For each `require` that is another ore module (prefix `github.com/andrewhowdencom/ore`), check `latestTags`. If the latest tag is newer than the current `require` version, update it.
  - Build a dependency graph where edges go from a module to its ore dependencies. Topologically sort so dependencies release first.
  - If a cycle is detected (should not happen after structural cleanup), fail fast with a clear error.

### Task 7: Implement Commit, Tag, Push Workflow and `release all` / `release <path>`
- **Goal**: Wire together the full release pipeline: tidy, commit, tag, push.
- **Dependencies**: Task 5, Task 6.
- **Files Affected**: `cmd/release/` source files.
- **New Files**: New helper `.go` file for git workflow (e.g., `gitops.go`).
- **Interfaces**:
  - `func releaseModule(module Module, newVersion string) error` — runs tidy, stages, commits, tags.
  - `func push() error` — pushes commits and tags to `origin/main`.
- **Validation**: `go test ./cmd/release/...` passes. Manual test on an isolated branch: `go run ./cmd/release all --dry-run` (or without push) shows the intended sequence without mutating `main`.
- **Details**:
  - For `release all`:
    1. Run the `status` logic to identify changed modules.
    2. Topologically sort them.
    3. For each module in order:
       a. Call `updateModuleDeps`.
       b. Write updated `go.mod`.
       c. Run `GOWORK=off go mod tidy` in the module directory.
       d. On tidy failure, stop immediately and do not proceed.
       e. Stage `go.mod` and `go.sum`.
       f. Commit: `chore(release): bump <module-path> to <version>`.
       g. Tag: root `vX.Y.Z`, submodule `<path>/vX.Y.Z`. Use annotated tags with a message.
    4. Push all commits: `git push origin main`.
    5. Push all tags: `git push origin --tags` (or push each tag individually).
  - For `release <path>`:
    - Target a single module even if it has no changes (issue implies explicit path means explicit release). Skip status filtering for the target module.
    - Run the same tidy → commit → tag → push sequence for that module only.
  - Safety: consider adding a `--dry-run` flag that prints what would be done without executing git mutations. This is not explicitly in the issue scope but is strongly recommended for a tool that commits to `main`.

### Task 8: Add `task release` to Taskfile.yml and Bootstrap First Release
- **Goal**: Integrate the release tool into the project's task runner and perform the first release.
- **Dependencies**: Task 7.
- **Files Affected**: `Taskfile.yml`
- **New Files**: None.
- **Interfaces**: New Taskfile task `release`.
- **Validation**: `task --list` shows the new `release` task. `task release:status` (or equivalent) runs successfully.
- **Details**:
  - Add to `Taskfile.yml`:
    ```yaml
    release:
      desc: Run the monorepo release tool
      cmds:
        - go run ./cmd/release all
    release-status:
      desc: Show pending module releases
      cmds:
        - go run ./cmd/release status
    ```
  - For the first release bootstrap: since no valid tags exist, `release status` will show all 13 modules as unreleased. The implementer should decide whether to:
    - Manually create the first root tag (e.g., `git tag -a v0.1.0 -m "Bootstrap release" && git push origin v0.1.0`), then use the tool for subsequent releases, **or**
    - Run `go run ./cmd/release all` directly and let the "no previous tag" logic assign initial versions (e.g., `v0.1.0` for all modules with commits).
  - Document the chosen bootstrap strategy in the tool's source comments or a `cmd/release/README.md`.

## Dependency Graph

```
Task 1 ──┐
         │
Task 2 ──┼──→ Task 3 ──→ Task 4 ──┬──→ Task 5 ──┐
                                   │             │
                                   └──→ Task 6 ──┼──→ Task 7 ──→ Task 8
```

- Task 1 and Task 2 are parallelizable (no dependency between them).
- Task 3 depends on nothing.
- Task 4 depends on Task 3.
- Task 5 depends on Task 4.
- Task 6 depends on Task 4 and Task 3.
- Task 7 depends on Task 5 and Task 6.
- Task 8 depends on Task 7.

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| `x/conduit/tui/go.mod` still has `replace` directives, causing `GOWORK=off go mod tidy` to behave unexpectedly or fail during release | High | Medium | Addressed by Task 1. Validate by running `cd x/conduit/tui && GOWORK=off go mod tidy` before any release logic is written. |
| `go mod tidy` hard failure mid-release leaves partial commits/tags in local history | High | Medium | Implement pre-flight validation: run `GOWORK=off go mod tidy` for all target modules **before** creating any commits or tags. Only proceed to git mutations if all tidies pass. |
| Submodule tag format collision (e.g., `x/tool/v0.1.0` vs `x/tool/bash/v0.1.0`) | Medium | Low | Tag pattern must include the full module suffix. Unit tests for `latestTag` should cover nested paths like `x/tool/bash/v0.1.0` and ensure `x/tool/v0.1.0` is not matched for `x/tool/bash`. |
| Commit-to-module heuristic misattributes root-level changes (e.g., `.github/workflows/`) to the root module | Medium | Medium | Acceptable — root module is the natural catch-all. Document the heuristic. If desired, add an ignore list for CI/config files. |
| `git push origin main` fails due to branch protection, diverged history, or missing auth | High | Low | Document that the tool must be run by a user with direct push rights to `main`. Add `--dry-run` flag for safe testing. Catch push errors and abort cleanly. |
| Circular module dependency prevents topological sort | High | Low | Structural plan already broke cycles. Add a fast-fail check: if cycles are detected, print the cycle and exit with an error before any git mutations. |
| First release bootstrap: root module has no tag, so the tool cannot reference a prior version | Medium | High | Documented as implementer discretion. Suggest manual first tag for the root module, or treat "no prior tag" as `v0.1.0` (or `v0.0.1` if only fixes) in the tool logic. |
| `examples/go.mod` has no explicit ore requires because workspace resolution stripped them | Medium | Medium | `GOWORK=off go mod tidy` in `examples/` will restore the correct `require` lines during the first release. This is expected workspace behavior and not a bug. |

## Validation Criteria

- [ ] `x/conduit/tui/go.mod` contains no `replace` directives.
- [ ] Invalid tag `0.0.1` does not exist locally or on the remote.
- [ ] `go build ./cmd/release` succeeds from the repo root.
- [ ] `go run ./cmd/release status` runs without panic and shows a status line for all 13 modules.
- [ ] Unit tests in `cmd/release/` pass (`go test ./cmd/release/...`).
- [ ] Conventional commit parsing correctly identifies patch, minor, and major bumps in unit tests.
- [ ] Topological sort test validates correct dependency ordering (e.g., `x/tool` before `x/tool/bash`).
- [ ] `GOWORK=off go mod tidy` passes in every module after the tool updates dependencies.
- [ ] `task release-status` is available and functional in `Taskfile.yml`.
- [ ] The tool correctly creates root tags (`vX.Y.Z`) and submodule tags (`<path>/vX.Y.Z`).
- [ ] A manual test on a throwaway branch demonstrates the full `release all` workflow (tidy → commit → tag) without errors. The push step can be verified separately or with `--dry-run`.
