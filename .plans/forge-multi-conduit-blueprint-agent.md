# Plan: Forge Multi-Conduit Blueprint Agent Integration

## Objective

Transform `cmd/forge` from a single-conduit code generator into a multi-conduit blueprint system. Forge will generate agent applications that use a shared `agent.Agent` orchestrator to run multiple conduits concurrently (e.g., HTTP + TUI simultaneously). This requires creating a new `agent` package, extending the YAML configuration format from a single `conduit.type` to a `conduits` array with module paths, rewriting the Go code generation template to use dynamic imports and agent orchestration, supporting external conduit module resolution, and renaming the configuration concept from "manifest" to "blueprint" per #95.

## Context

The current Forge implementation lives under `cmd/forge/` and consists of:

- **`cmd/forge/manifest.go`**: Defines `Manifest` struct with `Dist` and single `Conduit` (with `Type string`) fields. `ParseManifest` validates that `conduit.type` is either `"http"` or `"tui"`.
- **`cmd/forge/templates/main.go.tmpl`**: Hardcoded Go template with `{{if eq .ConduitType "http"}}` / `{{else}}` branches that import and initialize exactly one conduit.
- **`cmd/forge/generate.go`**: `GenerateMainGo` passes `{ConduitType: manifest.Conduit.Type}` to the template. `Generate` writes `main.go` and `go.mod`.
- **`cmd/forge/build.go`**: `Build` creates a temp Go module, runs `go mod tidy`, then `go build -o <output>`.
- **`cmd/forge/main.go`**: Cobra CLI with `build`, `generate`, and `version` commands.
- **`x/conduit/conduit.go`**: Defines `Conduit` interface with single method `Start(ctx context.Context) error`.
- **`x/conduit/http/` and `x/conduit/tui/`**: Both implement `conduit.Conduit` via `New(mgr *session.Manager, opts ...Option) (conduit.Conduit, error)`.
- **`session/manager.go`**: `NewManager(store, prov, newStep, processor, opts...)` creates the shared session manager.
- **`cognitive/react.go`**: `NewTurnProcessor()` returns a `session.TurnProcessor` for the ReAct cognitive loop.
- **No `agent/` package exists** — this is a dependency that must be created.
- **`examples/forge/`**: Contains example manifests (`http/forge.yaml`, `tui/forge.yaml`) used by smoke tests.
- **No "recipe" terminology exists in code** — #95 intends the rename to apply to the `Manifest` type and the concept of predefined agent configurations.

All tests are table-driven and located in `cmd/forge/*_test.go`. The build pipeline is exercised by `TestForgeSmoke` which compiles real binaries from testdata and example manifests.

## Architectural Blueprint

### Tree-of-Thought: Agent Package Location

| Path | Location | Rationale | Verdict |
|---|---|---|---|
| A | `agent/` at root | First-class framework primitive; generated applications must import it. Matches core package convention (all root-level). | **Selected** |
| B | `x/agent/` | Follows extension pattern, but agent orchestration is not an extension — it is a core composition primitive. | Rejected |
| C | `core/agent/` | Nested under core, but agent depends on `session/` and `x/conduit/` which are outside `core/`. Violates cycle-free dependency graph. | Rejected |

### Tree-of-Thought: Template Options Handling

| Path | Approach | Rationale | Verdict |
|---|---|---|---|
| A | Full YAML-to-Go options translation | Each conduit's YAML `options` map maps to Go functional options. Requires per-conduit knowledge in generator. Complex for first iteration. | Deferred |
| B | No options; env vars only | Simplest. Generated code reads env vars for configuration (e.g., `PORT`, `THREAD`). Matches current template behavior. | **Selected as baseline** |
| C | Hybrid: built-in conduits get env-var options, external get none | Pragmatic. Well-known conduits retain their existing env-var configuration patterns. External conduits receive no options until a generic options mechanism is designed. | **Selected for first version** |

### Component Diagram

```
┌─────────────────────────────────────────┐
│  cmd/forge (CLI)                        │
│  ├── ParseBlueprint (YAML → struct)   │
│  ├── GenerateMainGo (struct → main.go)│
│  ├── GenerateGoMod (struct → go.mod)  │
│  └── Build (compile binary)           │
└─────────────────────────────────────────┘
                   │
                   ▼
┌─────────────────────────────────────────┐
│  agent/ (new package)                 │
│  ├── New(mgr *session.Manager)         │
│  ├── Add(c conduit.Conduit)            │
│  └── Run(ctx) error                    │
└─────────────────────────────────────────┘
                   │
       ┌───────────┼───────────┐
       ▼           ▼           ▼
  x/conduit/http  x/conduit/tui  external/...
       │           │           │
       └───────────┴───────────┘
                   │
                   ▼
           session.Manager
```

### Key Design Decisions

1. **`Blueprint` replaces `Manifest`** — aggressive refactoring per AGENTS.md. All struct, function, variable, and comment references are renamed. No backward compatibility.
2. **`agent.Agent` is a thin orchestrator** — it holds a `*session.Manager` and a slice of `conduit.Conduit`. `Run(ctx)` starts all conduits concurrently and blocks until `ctx` is cancelled or any conduit returns a non-nil error. It mirrors `core.Loop` philosophy: minimal, composable, application-layer concerns handled by callers.
3. **`Conduits []ConduitConfig` replaces `Conduit Conduit`** — each entry has `Module string` (Go module path) and `Options map[string]any` (reserved for future YAML-to-Go option translation).
4. **Dynamic import aliases** — the generator derives import aliases from the last path element of the module path. Built-in conduits use well-known aliases (`httpc`, `tui`) to avoid stdlib conflicts. External conduits use the last path element; conflicts are disambiguated with numeric suffixes.
5. **External module resolution** — for each conduit module not under `github.com/andrewhowdencom/ore/x/conduit/`, the build process runs `go get <module>` in the temp directory before `go mod tidy` to ensure the dependency is resolvable.

## Requirements

1. Create a new `agent` package at repository root that orchestrates multiple `conduit.Conduit` instances with a shared `session.Manager`.
2. Rename the `Manifest` type and all related functions/variables to `Blueprint` across `cmd/forge/` per #95.
3. Extend the YAML blueprint format to support a `conduits` array with `module` and `options` fields, replacing the single `conduit.type` field.
4. Rewrite `cmd/forge/templates/main.go.tmpl` to generate code that:
   - Creates a shared `session.Manager`
   - Creates an `agent.Agent`
   - Dynamically imports each conduit module
   - Calls each conduit's `New(mgr, opts...)` constructor
   - Adds each conduit to the agent
   - Calls `agent.Run(ctx)`
5. Support `go get` resolution for external conduit module paths in the build pipeline.
6. Update all tests, testdata, and examples to use the new blueprint format.
7. Add a multi-conduit smoke test (e.g., HTTP + TUI in one blueprint).
8. All existing tests continue to pass after the refactor.

## Task Breakdown

### Task 1: Create the `agent` Package

- **Goal**: Implement a minimal agent orchestration package that runs multiple conduits concurrently.
- **Dependencies**: None.
- **Files Affected**: None (all new files).
- **New Files**:
  - `agent/doc.go` — package documentation
  - `agent/agent.go` — Agent struct and methods
  - `agent/agent_test.go` — table-driven tests
- **Interfaces**:
  - `func New(mgr *session.Manager) *Agent`
  - `func (a *Agent) Add(c conduit.Conduit)`
  - `func (a *Agent) Run(ctx context.Context) error` — starts all conduits in goroutines, blocks until `ctx` cancelled or any conduit errors
- **Validation**:
  - `go test ./agent/...` passes
  - `go test -race ./agent/...` passes
  - Package compiles with no lint errors
- **Details**: The `Agent` struct holds `*session.Manager` and `[]conduit.Conduit`. `Run` uses a `sync.WaitGroup` and an error channel. Each conduit is started in its own goroutine; the first error triggers `context.Cancel` and returns. Include tests with mock conduits (local structs implementing `conduit.Conduit`) to verify concurrent startup, cancellation, and error propagation.

### Task 2: Rename `Manifest` to `Blueprint` and Extend Format

- **Goal**: Rename all manifest-related identifiers to blueprint, replace the single conduit field with a conduits array, and update the parser.
- **Dependencies**: None.
- **Files Affected**:
  - `cmd/forge/manifest.go` → rename to `cmd/forge/blueprint.go`
  - `cmd/forge/manifest_test.go` → rename to `cmd/forge/blueprint_test.go`
  - `cmd/forge/generate.go` — update function signatures and data structures
  - `cmd/forge/generate_test.go` — update test cases
  - `cmd/forge/build.go` — update function signatures
  - `cmd/forge/build_test.go` — update test cases
  - `cmd/forge/main.go` — update variable names and function calls
  - `cmd/forge/forge_test.go` — update test cases
  - `cmd/forge/cmd_generate_test.go` — update test cases
- **New Files**: None.
- **Interfaces**:
  - `type Blueprint struct { Dist Dist; Conduits []ConduitConfig }`
  - `type ConduitConfig struct { Module string; Options map[string]any }`
  - `func ParseBlueprint(r io.Reader) (*Blueprint, error)`
  - `func GenerateMainGo(blueprint *Blueprint) ([]byte, error)`
  - `func GenerateGoMod(blueprint *Blueprint, oreModulePath string) ([]byte, error)`
  - `func Build(blueprint *Blueprint, oreModulePath string, outputPath string) error`
  - `func Generate(blueprint *Blueprint, oreModulePath string, targetDir string) error`
- **Validation**:
  - `go test ./cmd/forge/...` passes with all renamed references
  - `go test -race ./cmd/forge/...` passes
  - No compilation errors in `cmd/forge`
- **Details**: Rename `Manifest` → `Blueprint`, `ParseManifest` → `ParseBlueprint`, and all `manifest` variable names → `blueprint`. Change the struct to use `Conduits []ConduitConfig` instead of `Conduit Conduit`. Validation rules: `dist.name` and `dist.output_path` remain required; `conduits` must be non-empty; each `ConduitConfig.Module` must be non-empty. Update all tests to use `Blueprint` and `Conduits`. The template data structure in `GenerateMainGo` should be updated to pass a `Conduits` slice (the template itself will be rewritten in Task 3).

### Task 3: Rewrite `main.go.tmpl` for Multi-Conduit Agent Generation

- **Goal**: Rewrite the Go template to generate multi-conduit applications using `agent.Agent`.
- **Dependencies**: Task 1 (agent package must exist), Task 2 (blueprint format must support conduits array).
- **Files Affected**:
  - `cmd/forge/templates/main.go.tmpl`
  - `cmd/forge/generate.go` — update `GenerateMainGo` to build `ConduitTemplateData`
  - `cmd/forge/generate_test.go`
  - `cmd/forge/forge_test.go`
  - `cmd/forge/cmd_generate_test.go`
- **New Files**: None.
- **Interfaces**:
  - Template data structure: `type ConduitTemplateData struct { VarName string; ImportAlias string; ModulePath string; Options []string }`
  - Alias generation: derive from last path element; built-in conduits use hardcoded aliases (`httpc`, `tui`); disambiguate conflicts
  - Options generation: built-in HTTP includes `httpc.WithUI()` and `httpc.WithAddr(":")` with port from `PORT` env var; built-in TUI includes `tui.WithThreadID(threadID)` with threadID from `--thread` flag if present; external conduits have no options
- **Validation**:
  - `go test ./cmd/forge/...` passes
  - Generated `main.go` is valid Go syntax (already verified by `parser.ParseFile` in `GenerateMainGo`)
  - Generated `main.go` compiles successfully in smoke tests
- **Details**: The template must:
  1. Generate a dynamic import block with one import per conduit using the generated alias.
  2. Conditionally include `"flag"` and `"net/http"` (or their stdlib equivalents) only if needed by built-in conduits.
  3. Parse the `--thread` flag if any TUI conduit is present.
  4. Create `session.Manager` with the same shared dependencies as the current template.
  5. Create `agent.New(mgr)`.
  6. Loop over conduits: for each, generate `c, err := <alias>.New(mgr, <opts>...)` then `a.Add(c)`.
  7. Generate `ctx, stop := signal.NotifyContext(...)` and `return a.Run(ctx)`.
  Update tests to verify generated output contains correct imports, agent usage, and conduit constructors for both single and multi-conduit blueprints.

### Task 4: Support External Conduit Module Resolution

- **Goal**: Ensure the build pipeline can resolve external conduit module paths.
- **Dependencies**: Task 3 (template generates imports for external modules).
- **Files Affected**:
  - `cmd/forge/build.go`
  - `cmd/forge/build_test.go`
- **New Files**: None.
- **Interfaces**:
  - Build function enhancement: after writing generated files and before `go mod tidy`, run `go get <module>` for each external conduit module path
  - External module definition: any module path not matching `github.com/andrewhowdencom/ore/x/conduit/*`
- **Validation**:
  - `go test ./cmd/forge/...` passes
  - Smoke tests with built-in conduits still pass (no regression)
- **Details**: In `Build`, after generating `main.go` and `go.mod`, iterate over `blueprint.Conduits`. For each module not in the ore module, run `go get <modulePath>` in the temp directory. This ensures the module is resolvable before `go mod tidy` runs. If `go get` fails, propagate the error with context. For testing, add a test case that verifies the build process handles an external module path correctly (use a well-known public Go module or mock the exec command). If network-dependent tests are unreliable in CI, mock `exec.Command` for the test.

### Task 5: Update Examples, Testdata, and Documentation

- **Goal**: Migrate all manifests to the new multi-conduit blueprint format and add multi-conduit examples.
- **Dependencies**: Task 3 (new format and template must exist).
- **Files Affected**:
  - `cmd/forge/testdata/http-forge.yaml`
  - `cmd/forge/testdata/tui-forge.yaml`
  - `examples/forge/http/forge.yaml`
  - `examples/forge/tui/forge.yaml`
  - `examples/forge/README.md`
- **New Files**:
  - `cmd/forge/testdata/multi-forge.yaml` — example blueprint with both HTTP and TUI conduits
  - `examples/forge/multi/forge.yaml` — example multi-conduit blueprint
- **Interfaces**: None.
- **Validation**:
  - `go test ./cmd/forge/...` passes (smoke tests use updated testdata)
  - `TestForgeSmoke` includes the `multi-forge.yaml` test case and the compiled binary is produced
  - Example builds succeed when run from their directories
- **Details**: Update all YAML files from:
  ```yaml
  conduit:
    type: http
  ```
  to:
  ```yaml
  conduits:
    - module: github.com/andrewhowdencom/ore/x/conduit/http
  ```
  Add `multi-forge.yaml` with both HTTP and TUI conduits. Update `examples/forge/README.md` to document the new blueprint format, multi-conduit capabilities, and any new env vars or flags. Rename references from "manifest" to "blueprint" in documentation.

### Task 6: Finalize and Run Full Test Suite

- **Goal**: Ensure the entire repository is healthy after all changes.
- **Dependencies**: Task 4, Task 5.
- **Files Affected**: None.
- **New Files**: None.
- **Interfaces**: None.
- **Validation**:
  - `go test -race ./...` passes across the entire repository
  - `go vet ./...` is clean
  - No compilation errors in any package
  - All generated example binaries compile successfully
- **Details**: Run the full test suite with race detection. Verify `agent/`, `cmd/forge/`, and all existing packages pass. Check that the generated multi-conduit binary can be built and starts without errors (runtime guard test validates exit behavior). Address any race conditions or test failures discovered.

## Dependency Graph

- Task 1 → Task 3 (agent package required by template)
- Task 2 → Task 3 (blueprint format required by template)
- Task 3 → Task 4 (template must generate external imports before build can resolve them)
- Task 3 → Task 5 (examples need new format)
- Task 4 || Task 5 (parallelizable after Task 3)
- Task 4 → Task 6
- Task 5 → Task 6

```
Task 1 ─┐
        ├──→ Task 3 ──→ Task 4 ──→ Task 6
Task 2 ─┘         └─→ Task 5 ──→ Task 6
```

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Agent package design disagrees with future multi-conduit orchestration requirements | High | Medium | Keep agent package minimal (only `New`, `Add`, `Run`). Future requirements (e.g., graceful shutdown, conduit health checks) can be added without breaking the existing API. Document the design as intentionally thin. |
| External conduit modules fail `go get` in tests (network issues, private repos) | Medium | Medium | For tests, use a well-known public module (e.g., `github.com/spf13/cobra` as a dummy) or mock `exec.Command`. Document that real external conduits must be publicly resolvable or configured with Go module proxies. |
| Template import alias conflicts between conduits | Medium | Low | Implement alias disambiguation in generator: track used aliases, append numeric suffix on collision. Built-in conduits use hardcoded aliases (`httpc`, `tui`) that avoid stdlib conflicts. |
| YAML options parsing is underspecified for first version | Low | High | Explicitly defer full YAML-to-Go options translation. Parse and store options in `ConduitConfig.Options` but do not translate them in the template. This reserves the field for future enhancement without blocking the multi-conduit feature. |
| Large rename (Manifest→Blueprint) breaks unmerged branches or worktrees | Medium | High | The rename is mechanical and grep-replaceable. Document the rename in the plan. Other branches will need to rebase. This is acceptable per AGENTS.md aggressive refactoring convention. |
| Generated multi-conduit binary has race conditions during concurrent conduit startup | Medium | Medium | The agent package must use proper synchronization (`sync.WaitGroup`, `context.WithCancel`). Verify with `go test -race ./agent/...`. The smoke test should validate that the binary compiles and runs without data races. |

## Validation Criteria

- [ ] `go test -race ./agent/...` passes with mock conduit tests for concurrent startup, cancellation, and error propagation.
- [ ] `go test -race ./cmd/forge/...` passes with all renamed references and updated test cases.
- [ ] All smoke tests (`TestForgeSmoke`) pass for single-conduit blueprints (HTTP, TUI) and multi-conduit blueprints.
- [ ] `go test -race ./...` passes across the entire repository with no regressions.
- [ ] `go vet ./...` is clean.
- [ ] The generated multi-conduit `main.go` is syntactically valid Go (verified by `parser.ParseFile`).
- [ ] The generated multi-conduit binary compiles and starts; the runtime guard test confirms it exits cleanly when `ORE_API_KEY` is missing.
- [ ] Example blueprints in `examples/forge/` compile successfully with `go run ../../../cmd/forge build --config forge.yaml`.
- [ ] No references to "Manifest" or "manifest" remain in `cmd/forge/` code or comments (except where referring to the old format for historical context in docs).
