# Plan: Refactor forge CLI onto Cobra with build and generate Subcommands

## Objective

Rebuild `cmd/forge` from a flat `flag`-based CLI onto the [Cobra](https://github.com/spf13/cobra) framework. Introduce two subcommands — `forge build` (the existing pipeline, invoked implicitly when no subcommand is given) and `forge generate` (renders `main.go` and `go.mod` without invoking `go mod tidy` or `go build`). Split the conflated generation and compilation logic in `Build()` into a reusable `Generate()` function so that both commands share the same template rendering but only `build` compiles.

## Context

The current `cmd/forge` entrypoint (`cmd/forge/main.go`) uses the standard `flag` package to parse a single `-config` flag and then calls `runWithPath()`, which always runs the full pipeline: parse manifest → generate files in a temp dir → `go mod tidy` → `go build`. The generation helpers (`GenerateMainGo`, `GenerateGoMod`) live in `cmd/forge/generate.go`, and the orchestration logic lives in `cmd/forge/build.go`. The package has comprehensive table-driven tests (`build_test.go`, `generate_test.go`, `forge_test.go`, `main_test.go`) and smoke tests that compile real binaries for `http` and `tui` conduits.

Key files and their current responsibilities:
- `cmd/forge/main.go` — CLI entrypoint, flag parsing, `runWithPath()`
- `cmd/forge/build.go` — `Build()` creates a temp dir, generates files, runs `go mod tidy`, runs `go build`, cleans up temp dir
- `cmd/forge/generate.go` — `GenerateMainGo()` and `GenerateGoMod()` (pure functions that return `[]byte`)
- `cmd/forge/manifest.go` — `Manifest` struct and `ParseManifest()`
- `cmd/forge/module.go` — `FindOreModuleRoot()`
- `cmd/forge/build_test.go`, `cmd/forge/generate_test.go`, `cmd/forge/forge_test.go`, `cmd/forge/main_test.go` — existing test coverage
- `go.mod` — currently does **not** include `github.com/spf13/cobra`; uses `gopkg.in/yaml.v3` and `github.com/stretchr/testify`

A related prior plan (`build-forge-cli.md`) implemented the initial flat CLI; this plan is the second-phase refactor requested in GitHub issue #84.

## Architectural Blueprint

**Selected architecture: Cobra subcommands with a shared `Generate()` primitive.**

1. **Dependency**: Add `github.com/spf13/cobra` to the module. It is the Go ecosystem standard for multi-command CLIs and is explicitly requested in the issue.
2. **Split generation from compilation**: Extract a `Generate(manifest, oreModulePath, targetDir)` function that writes `main.go` and `go.mod` into `targetDir`. `Build()` becomes a thin wrapper: create temp dir → `Generate(..., tmpDir)` → `go mod tidy` → `go build` → clean up.
3. **Command structure**:
   - Root command `forge` with `RunE` wired to the `build` handler so that `forge -config ...` behaves identically to `forge build -config ...`.
   - `build` subcommand with the same flags and behavior as today's flat CLI.
   - `generate` subcommand with a `-config` flag and an `-o` flag. When `-o` is omitted, the command prints the generated files to stdout with clear separators. When `-o <dir>` is provided, it writes files into that directory.
4. **Test strategy**: Existing `Build` and smoke tests must continue to pass without behavioral regression. New table-driven tests will cover `Generate()` file writing, `generate` stdout output, and `generate` directory output.

**Tree-of-Thought deliberation:**

- *Path A: Keep `flag` and manually parse `os.Args[1]` for subcommands.* Minimal dependency change, but reinvents help text, subcommand routing, and flag parsing that Cobra provides for free. Rejected because the issue explicitly asks for Cobra.
- *Path B: Use `urfave/cli/v2` or another CLI framework.* Lighter weight than Cobra in some cases, but deviates from the stated requirement and from Go community conventions. Rejected.
- *Path C: Adopt Cobra.* Adds one well-known dependency, satisfies all acceptance criteria (`--help` with subcommands, easy future extensibility), and aligns with the issue. Selected.

## Requirements

1. `cmd/forge` uses Cobra for command routing and help generation.
2. `forge build` exists and behaves identically to the current `forge -config ...` flat command.
3. `forge` with no subcommand defaults to `build` (backward compatibility).
4. `forge generate` renders `main.go` and `go.mod` without invoking `go mod tidy` or `go build`.
5. `forge generate` outputs to stdout by default (both files, with separators) and writes to a directory when `-o` is provided.
6. `forge --help` lists available subcommands with descriptions.
7. All existing tests in `cmd/forge/` pass without behavioral regression.
8. New table-driven tests cover the `generate` command output (stdout and directory variants).
9. [inferred] Error wrapping continues to use `fmt.Errorf("...: %w", err)` per project convention.
10. [inferred] Lifecycle logging continues to use `log/slog` per project convention.
11. [inferred] `forge version` subcommand prints version information (CLI skill convention).
12. [inferred] Root command exposes `--log-level` persistent flag to configure `slog` (CLI skill convention).
13. [inferred] All commands include `Example` fields so `--help` is self-documenting (CLI skill convention).

## Task Breakdown

### Task 1: Add Cobra Dependency
- **Goal**: Add `github.com/spf13/cobra` to the ore module and tidy dependencies.
- **Dependencies**: None.
- **Files Affected**: `go.mod`, `go.sum`.
- **New Files**: None.
- **Interfaces**: None.
- **Validation**:
  - `go mod tidy` produces a clean diff.
  - `go build ./...` passes.
  - `go test -race ./...` passes (no code changes yet, just dependency addition).
- **Details**: Run `go get github.com/spf13/cobra@latest` at the repository root. Ensure `go.sum` is updated. Verify no unintended indirect dependency upgrades beyond what Cobra requires.

### Task 2: Extract Directory-Based `Generate()` from `Build()`
- **Goal**: Split the file-writing phase out of `Build()` into a standalone `Generate()` function so both `build` and `generate` commands can reuse it.
- **Dependencies**: None (can be done in parallel with Task 1, but must finish before Task 3).
- **Files Affected**: `cmd/forge/build.go`, `cmd/forge/generate.go`.
- **New Files**: None.
- **Interfaces**:
  ```go
  // Generate writes main.go and go.mod into targetDir.
  func Generate(manifest *Manifest, oreModulePath string, targetDir string) error

  // Build remains but internally calls Generate into a temp dir,
  // then runs go mod tidy and go build.
  func Build(manifest *Manifest, oreModulePath string, outputPath string) error
  ```
- **Validation**:
  - `go test -race ./cmd/forge/...` passes with all existing tests still green.
  - `TestBuild`, `TestBuild_RelativeOutputPath`, and `TestForgeSmoke` continue to compile binaries successfully.
- **Details**:
  - Move the `GenerateMainGo`/`GenerateGoMod` execution and `os.WriteFile` calls from `Build()` into a new `Generate()` function that accepts a `targetDir` string.
  - `Generate()` must resolve `targetDir` to an absolute path (or accept an absolute path) and write `main.go` and `go.mod` there.
  - `Build()` should create a temp dir, call `Generate(manifest, oreModulePath, tmpDir)`, then run `go mod tidy` and `go build` exactly as it does today, and finally clean up the temp dir via `defer os.RemoveAll(tmpDir)`.
  - Do **not** change the signatures of `GenerateMainGo` or `GenerateGoMod`.

### Task 3: Implement Cobra Command Structure
- **Goal**: Rewrite `cmd/forge/main.go` to use Cobra, wire `build` as the default and explicit subcommand, add `generate`, and add `version` per CLI skill conventions.
- **Dependencies**: Task 1, Task 2.
- **Files Affected**: `cmd/forge/main.go`, `cmd/forge/main_test.go`.
- **New Files**: None (or optionally `cmd/forge/commands.go` if the implementer prefers to separate command definitions from `main.go`).
- **Interfaces**:
  ```go
  // Existing function signatures updated to use Cobra command handlers.
  // run() returns error and is wired into cobra.Command{RunE: ...}.
  // runWithPath is updated or replaced by build command handler.
  ```
- **Validation**:
  - `go test -race ./cmd/forge/...` passes.
  - `TestRunWithPath` is updated to exercise the `build` command path and still covers missing file, malformed YAML, and missing required fields.
  - `go build ./cmd/forge` compiles cleanly.
- **Details**:
  - Create a root `cobra.Command` named `forge` whose `RunE` delegates to the `build` handler (so `forge -config foo.yaml` behaves like `forge build -config foo.yaml`).
  - Add a `--log-level` persistent flag on the root command (accepted values: `debug`, `info`, `warn`, `error`). Use it to set the `slog` level in `main()` before subcommands run.
  - Create a `build` subcommand that accepts `--config` (or `-c`) flag, parses the manifest, discovers the ore module root, and calls `Build()`. Include an `Example` field in the Cobra command definition.
  - Create a `generate` subcommand that accepts `--config` and `-o` flags. When `-o` is omitted, print `main.go` and `go.mod` to stdout with a separator line (`// --- FILE: go.mod ---`). When `-o <dir>` is provided, ensure the directory exists (create if needed) and call `Generate(manifest, oreModulePath, dir)`. Include an `Example` field.
  - Create a `version` subcommand that prints version information. Use Go build info (`debug.ReadBuildInfo`) if available; fall back to a hardcoded string or "dev" if not. Include an `Example` field.
  - Keep `slog` for lifecycle events (e.g., "build complete", "generate complete").
  - Wrap errors with `fmt.Errorf("...: %w", err)`.
  - The root command should show help when `--help` is passed.

### Task 4: Add `generate` Command Tests
- **Goal**: Provide table-driven tests verifying `generate` stdout and directory output.
- **Dependencies**: Task 3.
- **Files Affected**: None (new test file).
- **New Files**: `cmd/forge/cmd_generate_test.go` (or extend an existing test file).
- **Interfaces**: None.
- **Validation**:
  - `go test -race ./cmd/forge/...` passes with new tests.
  - New tests cover:
    - Default stdout output: both `main.go` and `go.mod` are present in stdout.
    - Directory output: files are written to the specified directory and are readable.
    - Invalid manifest: appropriate error is returned.
- **Details**:
  - Use `t.TempDir()` for directory output tests.
  - For stdout tests, capture `os.Stdout` (or invoke the command handler directly and inspect a `bytes.Buffer`).
  - Assert that generated content contains expected markers (e.g., `module <name>` in `go.mod`, conduit-specific imports in `main.go`).
  - Follow the existing table-driven test style in `build_test.go` and `generate_test.go`.

### Task 5: Final Validation and Documentation Update
- **Goal**: Ensure the entire repository remains healthy and update CLI documentation following the Diátaxis framework (Documentation skill).
- **Dependencies**: Task 4.
- **Files Affected**: `cmd/forge/README.md` (if it exists), `README.md` (if forge is documented there), `docs/reference/` (if forge CLI is documented there).
- **New Files**: None (or `docs/reference/forge-cli.md` if no CLI reference exists yet).
- **Interfaces**: None.
- **Validation**:
  - `go test -race ./...` passes repository-wide.
  - `go vet ./...` is clean.
  - `go build ./cmd/forge` compiles.
  - Manual sanity checks:
    - `./forge --help` lists `build`, `generate`, and `version`.
    - `./forge build --help` shows the build flag help with examples.
    - `./forge generate --help` shows the generate flag help with examples.
    - `./forge version` prints a version string.
    - `./forge -config cmd/forge/testdata/http-forge.yaml` still compiles a binary.
    - `./forge generate -config cmd/forge/testdata/http-forge.yaml` prints to stdout.
    - `./forge generate -config cmd/forge/testdata/http-forge.yaml -o /tmp/my-agent` writes files.
- **Details**:
  - Search the repository for any documentation strings or README sections that reference the old flat `forge -config ...` usage and update them to show the new subcommand style while noting the implicit default.
  - Update or create `docs/reference/forge-cli.md` (or equivalent) to document the CLI commands, flags, and subcommands. This is **Reference**-style documentation (information-oriented, dry, accurate) per the Diátaxis framework.
  - Check `examples/forge/` for any README or comments that need updating.
  - If `mkdocs` is in use, verify the reference page is linked in the navigation.

## Dependency Graph

- Task 1 || Task 2 (parallel)
- Task 1, Task 2 → Task 3
- Task 3 → Task 4
- Task 4 → Task 5

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Cobra dependency conflicts with existing flag usage or changes expected `-config` behavior | Medium | Low | Cobra's `pflag` supports both `-config` and `--config`. Existing scripts using `-config` will continue to work. Validate with `main_test.go` error-path tests. |
| `Generate()` temp-dir logic leaks if refactored incorrectly | Medium | Low | Keep `defer os.RemoveAll(tmpDir)` inside `Build()` only; `Generate()` writes to a caller-provided directory and does **not** create or delete directories. |
| Smoke tests fail because generated binary paths or env-var requirements changed | High | Low | The `Build()` signature and compilation steps are intentionally unchanged. Smoke tests validate compilation and file existence, not runtime env vars. |
| `forge` with no subcommand does not default to `build` correctly | High | Low | Wire `rootCmd.RunE = buildCmd.RunE`. Write an explicit test for the no-subcommand case. |
| Two-file stdout output is confusing for piping / parsing | Low | Medium | Use a clear, predictable separator (`// --- FILE: go.mod ---`) and document it in the `--help` text for `generate`. Future iteration can add `--format tar` or similar if needed. |
| Comment from #84: naming generated output based on folder/dist | Low | Low | Out of scope for this plan. The `-o` flag gives users explicit control. A future issue can add auto-naming heuristics. |
| Configuration skill recommends Viper and XDG paths, but current manifest is input data (not app config) | Low | Medium | Acknowledge in plan notes. Viper migration for the manifest parser is deferred to avoid breaking existing tests and expanding scope. The application layer (`cmd/forge`) may adopt Viper later for its own flags/env/file unification without changing the manifest schema. |

## Validation Criteria

- [ ] `go mod tidy` is clean and `github.com/spf13/cobra` is present in `go.mod`.
- [ ] `go test -race ./...` passes repository-wide.
- [ ] `go vet ./...` is clean.
- [ ] `go build ./cmd/forge` compiles successfully.
- [ ] `forge build -config cmd/forge/testdata/http-forge.yaml` compiles a binary identical to the old flat command.
- [ ] `forge -config cmd/forge/testdata/http-forge.yaml` (no subcommand) compiles a binary.
- [ ] `forge generate -config cmd/forge/testdata/http-forge.yaml` prints `main.go` and `go.mod` to stdout.
- [ ] `forge generate -config cmd/forge/testdata/http-forge.yaml -o <dir>` writes both files to `<dir>`.
- [ ] `forge --help` shows `build`, `generate`, and `version` subcommands with descriptions and examples.
- [ ] `forge version` prints a version string.
- [ ] `forge --log-level debug` changes log output verbosity.
- [ ] All existing tests in `cmd/forge/` pass without behavioral regression.
- [ ] New tests cover `generate` stdout and directory output.
