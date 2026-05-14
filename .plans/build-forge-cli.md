# Plan: Build forge CLI for Generating ore Agent Binaries

## Objective

Build `forge`, a minimal CLI binary at `cmd/forge/` that reads a YAML manifest and generates a standalone, compilable Go agent application. The manifest selects a built-in conduit (`http` or `tui`), and `forge` scaffolds explicit wiring code — OpenAI provider, ReAct cognitive pattern, loop, session manager, and thread store — into a temporary Go module, then shells out to `go build` to produce the final binary. There is no global mutable state or `init()` registration in generated code.

## Context

The ore framework (`github.com/andrewhowdencom/ore`) is a minimal, composable framework for building agentic applications. Key findings from repository inspection:

- **Module**: `github.com/andrewhowdencom/ore` (Go 1.26.2).
- **Conduit packages**: `conduit/tui/` (Bubble Tea terminal UI) and `conduit/http/` (HTTP handler library with NDJSON/SSE streaming).
- **Conduit constructors**:
  - `tui.New(mgr *session.Manager, threadID string) *TUI`
  - `http.NewHandler(mgr *session.Manager, opts ...http.Option) *Handler`
  - `http.WithUI() http.Option` (optional; omitted in MVP generated code to keep minimal).
- **Session manager**: `session.NewManager(store thread.Store, prov provider.Provider, newStep func() *loop.Step, processor session.TurnProcessor, opts ...ManagerOption) *Manager`.
- **Loop Step**: `loop.New(opts ...loop.Option) *loop.Step` with `loop.WithHandlers(...)` and `loop.WithInvokeOptions(...)`.
- **Cognitive pattern**: `cognitive.NewTurnProcessor() session.TurnProcessor` implements ReAct.
- **OpenAI provider**: `openai.New(apiKey, model string, opts ...openai.Option) *Provider` with `openai.WithBaseURL(url string)`.
- **Thread stores**: `thread.NewMemoryStore() *MemoryStore` and `thread.NewJSONStore(dir string) (*JSONStore, error)`.
- **Existing CLI pattern**: `cmd/docgen/main.go` uses `flag`, `slog`, and a `run()` function returning `error`.
- **Dependencies**: `gopkg.in/yaml.v3` is already an indirect dependency in `go.mod` (from testify); it will be promoted to direct when `cmd/forge` imports it.
- **AGENTS.md conventions**: Table-driven tests; `go test -race ./...`; `fmt.Errorf("...: %w", err)` for error wrapping; `log/slog` for lifecycle events; functional options for constructors; stdlib preferred.

## Architectural Blueprint

**Selected architecture: Template-based code generation with local module replacement.**

`forge` is a single CLI binary under `cmd/forge/` that performs four phases:

1. **Parse**: Read and validate a `forge.yaml` manifest.
2. **Generate**: Use `text/template` (with `//go:embed` template files) to emit `main.go` and `go.mod` into a temporary directory. The template for `main.go` branches on `conduit.type` (`http` vs `tui`) to produce the correct imports, initialization, and blocking call (`s.Run()` or `server.ListenAndServe()`). The `go.mod` includes a `replace github.com/andrewhowdencom/ore => <local-absolute-path>` directive so the generated module resolves the ore dependency from the local repository.
3. **Discover**: Find the ore module root by walking up from the current working directory looking for a `go.mod` whose module line matches `github.com/andrewhowdencom/ore`.
4. **Build**: Run `go mod tidy` and `go build -o <output_path>` in the temp directory via `os/exec.Command`. Clean up the temp directory on success or failure.

**Tree-of-Thought deliberation:**

- *Path A: Programmatic AST generation (`go/ast` + `go/format`).* Produces perfectly formatted, type-safe Go code and allows dynamic composition. However, it is dramatically more complex than needed for a first cut with only two fixed conduit templates. Rejected.
- *Path B: String concatenation / `fmt.Sprintf`.* Simple and zero-dependency, but quickly becomes unmaintainable for conditional imports and divergent `main()` bodies between `http` and `tui`. Rejected.
- *Path C: `text/template` with `//go:embed`.* Standard library, handles conditionals cleanly, keeps templates as separate readable files, and is maintainable as the number of conduits grows. Selected.

## Requirements

1. `forge` lives at `cmd/forge/` and compiles as a standalone CLI.
2. `forge` reads a `forge.yaml` (default path) or a file specified via `-config` CLI flag.
3. YAML manifest shape: `dist.name`, `dist.output_path`, `conduit.type` (`http` or `tui`).
4. `forge` generates a temporary Go module with `main.go` and `go.mod`.
5. Generated `main.go` contains hardcoded wiring: OpenAI provider (env-var config), ReAct cognitive pattern, loop Step, session Manager, thread store.
6. Generated `go.mod` depends on `github.com/andrewhowdencom/ore` with a `replace` directive pointing to the local ore repository root.
7. `forge` shells out to `go mod tidy` and `go build -o <dist.output_path>` in the temp directory.
8. Generated binaries start successfully (validated by smoke test).
9. No global mutable state or `init()` in generated code.
10. All code has table-driven tests; `go test -race ./...` passes.

## Task Breakdown

### Task 1: Scaffold `cmd/forge/` CLI Entrypoint
- **Goal**: Create the `cmd/forge/` package with CLI flag parsing, logging, and a `run()` skeleton.
- **Dependencies**: None.
- **Files Affected**: None (new package).
- **New Files**: `cmd/forge/main.go`.
- **Interfaces**: `func run() error` pattern; `flag.String("config", "forge.yaml", "path to manifest file")`.
- **Validation**: `go build ./cmd/forge` compiles cleanly.
- **Details**: Follow the `cmd/docgen/main.go` pattern. Use `slog` with `TextHandler`. Parse a `-config` flag. The `run()` function reads the manifest file, invokes the generator, and triggers the build. Leave the manifest parsing and build calls as unimplemented stubs returning descriptive errors so the package compiles.

### Task 2: Define Manifest Schema and YAML Parser
- **Goal**: Define Go structs matching the YAML manifest and implement parsing with validation.
- **Dependencies**: Task 1.
- **Files Affected**: `cmd/forge/main.go` (add import).
- **New Files**: `cmd/forge/manifest.go`, `cmd/forge/manifest_test.go`.
- **Interfaces**:
  ```go
  type Manifest struct {
      Dist    Dist    `yaml:"dist"`
      Conduit Conduit `yaml:"conduit"`
  }
  type Dist struct {
      Name       string `yaml:"name"`
      OutputPath string `yaml:"output_path"`
  }
  type Conduit struct {
      Type string `yaml:"type"`
  }
  func ParseManifest(r io.Reader) (*Manifest, error)
  ```
- **Validation**: `go test -race ./cmd/forge/...` passes with table-driven tests covering valid manifests, missing fields, and unknown conduit types.
- **Details**: Use `gopkg.in/yaml.v3` (already present in `go.mod` as indirect). Validate that `conduit.type` is exactly `"http"` or `"tui"`. Return clear errors for unsupported types. Ensure `dist.output_path` is non-empty.

### Task 3: Implement `main.go` Template Generation
- **Goal**: Generate compilable `main.go` for both `http` and `tui` conduits using `text/template`.
- **Dependencies**: Task 2.
- **Files Affected**: None.
- **New Files**: `cmd/forge/generate.go`, `cmd/forge/templates/main.go.tmpl`, `cmd/forge/generate_test.go`.
- **Interfaces**:
  ```go
  func GenerateMainGo(manifest *Manifest) ([]byte, error)
  ```
- **Validation**: `go test -race ./cmd/forge/...` passes. Generated `main.go` for both conduits must be valid Go (verified by `go/parser.ParseFile` in tests or by compiling in a temp module in a later task).
- **Details**: Embed `templates/main.go.tmpl` via `//go:embed`. The template produces:
  - Common preamble: `slog` setup, env var reading (`ORE_API_KEY`, `ORE_MODEL`, `ORE_BASE_URL`, `STORE_DIR`), thread store creation (memory or JSON), OpenAI provider construction, step factory, session manager with ReAct.
  - For `tui`: create/resume thread via `--thread` flag, instantiate `tui.New(mgr, thread.ID)`, call `s.Run()`.
  - For `http`: read `PORT` env var, instantiate `http.NewHandler(mgr)`, create `&http.Server{Addr: ":" + port, Handler: handler.ServeMux()}`, call `server.ListenAndServe()`.
  - No tool registry; no `WithUI()` for http MVP.

### Task 4: Implement `go.mod` Template Generation
- **Goal**: Generate a `go.mod` that depends on the local ore module via a `replace` directive.
- **Dependencies**: Task 2.
- **Files Affected**: None.
- **New Files**: `cmd/forge/templates/go.mod.tmpl` (or inline if trivial), `cmd/forge/generate.go` (extend).
- **Interfaces**:
  ```go
  func GenerateGoMod(manifest *Manifest, oreModulePath string) ([]byte, error)
  ```
- **Validation**: `go test -race ./cmd/forge/...` passes. Unit test verifies the replace directive points to the provided absolute path and the module name matches `dist.name`.
- **Details**: The generated `go.mod` uses `dist.name` as its module name, `go 1.26.2`, a `require github.com/andrewhowdencom/ore v0.0.0`, and `replace github.com/andrewhowdencom/ore => <oreModulePath>` where `<oreModulePath>` is an absolute filesystem path.

### Task 5: Implement Ore Module Root Discovery
- **Goal**: Find the local ore repository root by walking up from CWD looking for the correct `go.mod`.
- **Dependencies**: None.
- **Files Affected**: None.
- **New Files**: `cmd/forge/module.go`, `cmd/forge/module_test.go`.
- **Interfaces**:
  ```go
  func FindOreModuleRoot(startDir string) (string, error)
  ```
- **Validation**: `go test -race ./cmd/forge/...` passes. Table-driven tests with temp directories simulating found and not-found scenarios.
- **Details**: Walk parent directories from `startDir` until a `go.mod` is found whose first non-comment, non-blank line starts with `module github.com/andrewhowdencom/ore`. Return the absolute path of the directory containing that `go.mod`. If the filesystem root is reached without a match, return an error instructing the user to run `forge` from within the ore repository.

### Task 6: Implement Build Orchestration
- **Goal**: Wire generation, module discovery, and `go build` together into the full `forge` pipeline.
- **Dependencies**: Tasks 1, 2, 3, 4, 5.
- **Files Affected**: `cmd/forge/main.go` (replace stub with real call), `cmd/forge/generate.go`.
- **New Files**: `cmd/forge/build.go`, `cmd/forge/build_test.go`.
- **Interfaces**:
  ```go
  func Build(manifest *Manifest, oreModulePath string, outputPath string) error
  ```
- **Validation**: `go test -race ./cmd/forge/...` passes. An integration-style test creates a temp manifest, mocks or executes the generation and build steps, and verifies the binary file exists.
- **Details**: The `Build` function:
  1. Creates a temp dir (`os.MkdirTemp("", "forge-*")`).
  2. Writes `main.go` and `go.mod` into the temp dir.
  3. Runs `go mod tidy` in the temp dir via `exec.Command`.
  4. Resolves `outputPath`: if relative, resolve against the CWD where `forge` was invoked; pass the absolute path to `go build -o <absPath> .`.
  5. Runs `go build -o <absPath> .` in the temp dir.
  6. Cleans up the temp dir on success or failure (via `defer`).
  7. Returns any error with wrapped context (`fmt.Errorf("go build: %w", err)`).
  Wire `Build` into `run()` in `main.go`.

### Task 7: Integration Smoke Tests
- **Goal**: Validate that `forge` produces runnable binaries for both `http` and `tui` conduits.
- **Dependencies**: Task 6.
- **Files Affected**: None.
- **New Files**: `cmd/forge/testdata/http-forge.yaml`, `cmd/forge/testdata/tui-forge.yaml`, `cmd/forge/forge_test.go`.
- **Interfaces**: None.
- **Validation**: `go test -race ./cmd/forge/...` passes. The generated binaries compile and can be executed (e.g., `--help` or a no-op startup check).
- **Details**: Write two YAML manifests under `testdata/`. In `forge_test.go`, run the full pipeline for each: parse → generate → build. Use `t.TempDir()` as the output directory. Verify the binary file exists and is executable. Because the generated binaries require `ORE_API_KEY` to start properly, the test should validate compilation and optionally check that the binary can print a help message or exit gracefully when env vars are missing. Do not require a live OpenAI API key for the test suite.

### Task 8: Update Dependencies and Final Validation
- **Goal**: Promote `gopkg.in/yaml.v3` to a direct dependency and ensure the entire repository builds cleanly.
- **Dependencies**: Task 7.
- **Files Affected**: `go.mod`, `go.sum`.
- **New Files**: None.
- **Interfaces**: None.
- **Validation**:
  - `go mod tidy` at repo root produces a clean diff.
  - `go build ./...` passes.
  - `go test -race ./...` passes.
  - `go vet ./...` is clean.
- **Details**: Run `go mod tidy` at the repository root. The import of `gopkg.in/yaml.v3` in `cmd/forge/manifest.go` will automatically promote it from indirect to direct. Verify no new external dependencies are introduced (only yaml.v3, already present). Run the full test suite to confirm no regressions.

## Dependency Graph

- Task 1 → Task 2
- Task 2 → Task 3
- Task 2 → Task 4
- Task 3 || Task 4 || Task 5 (parallel after Task 2)
- Task 3, Task 4, Task 5 → Task 6
- Task 6 → Task 7
- Task 7 → Task 8

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| `go build` in temp dir fails because `replace` path is wrong or ore module has uncommitted changes | High | Medium | Validate `FindOreModuleRoot` with tests. If `go mod tidy` or `go build` fails, surface the full stderr in the error message so the user can diagnose path issues. |
| Template-generated Go code has syntax errors (imports, missing brackets) | High | Low | Unit tests parse generated code with `go/parser.ParseFile`. Integration tests compile it. |
| `forge` run outside ore repo cannot find module root | Medium | Medium | Clear error message in `FindOreModuleRoot`. Document that `forge` must be invoked from within the ore repository. Future iteration can support a `--module-path` flag. |
| Generated binary requires `ORE_API_KEY` even for smoke tests | Low | High | Smoke test only validates compilation and file existence; do not require a live API key. Optionally test `--help` if the generated CLI adds one in the future. |
| `go mod tidy` pulls unexpected dependencies or changes go.sum | Low | Low | Lock Go version in generated `go.mod` to match ore (1.26.2). The `replace` directive ensures resolution uses the local ore module's full dependency graph. |

## Validation Criteria

- [ ] `go build ./cmd/forge` compiles the CLI binary.
- [ ] `go test -race ./cmd/forge/...` passes with all unit and integration tests.
- [ ] `forge` successfully parses `testdata/http-forge.yaml` and `testdata/tui-forge.yaml`.
- [ ] `forge` generates `main.go` and `go.mod` that are syntactically valid Go and module files.
- [ ] `forge` shells out to `go build` and produces an executable binary for both `http` and `tui` conduits.
- [ ] `go test -race ./...` passes repository-wide with no regressions.
- [ ] `go mod tidy` is clean at the repository root.
