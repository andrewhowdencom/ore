# Plan: Add Runtime Configuration for Forge-Generated Binaries

## Objective

Make Forge-generated binaries distributable artifacts that end users can configure and run without recompiling. Introduce a reusable `app` package that provides Cobra/Viper-backed CLI scaffolding, a three-layer runtime configuration system (CLI flags → environment variables → config file → compile-time defaults), and named conduit instances in Forge blueprints.

## Context

### Current State

- `cmd/forge/blueprint.go` defines `Blueprint`, `ConduitConfig`, and `HandlerConfig`. Neither config type has a `name` field; conduits are identified only by module path.
- `cmd/forge/templates/main.go.tmpl` generates a thick `main.go` that hardcodes env var lookups (`ORE_API_KEY`, `ORE_MODEL`, `ORE_BASE_URL`, `STORE_DIR`), creates the OpenAI provider, session manager, agent, and conduits inline. There is no CLI surface beyond the implicit binary invocation.
- `cmd/forge/generate.go` builds template data from the blueprint, including import aliases and `OptionsLiteral` maps for compile-time conduit/handler options.
- Existing conduits (`x/conduit/http`, `x/conduit/tui`, `x/conduit/slack`, `x/conduit/telegram`) already export `OptionsFromMap(m map[string]any) ([]Option, error)` using `github.com/mitchellh/mapstructure` with `yaml` tags. This is the universal contract the generated binary relies on.
- The `x/conduit/conduit.go` package defines the `Conduit` interface (`Start(ctx context.Context) error`) and `Descriptor` struct. It has no `go.mod`, so it is part of the root `github.com/andrewhowdencom/ore` module.
- `loop/loop.go` defines `Step`, `Option`, `WithHandlers`, and the `Handler` interface contract (used by artifact handlers).
- `go.mod` already includes `github.com/spf13/cobra` but does not include `github.com/spf13/viper`.

### Project Conventions

Per `AGENTS.md`:
- Core/framework packages live at the root level so external applications can import them. The new `app` package will be placed at `app/` (root level).
- Prefer aggressive refactoring over backwards compatibility.
- Use functional options pattern for constructors.
- Wrap errors with `fmt.Errorf("...: %w", err)`.
- Table-driven tests and `go test -race ./...` are mandatory.
- Conduit/library packages must NOT embed cognitive patterns or manage turn loops; they are dumb pipes. The `app` package is an application scaffold, so it correctly belongs above the conduit layer.

### Evaluated Alternatives

| Approach | Evaluation | Decision |
|---|---|---|
| **A. Generated binary stays thick; add Cobra/Viper inline** | Duplicated CLI scaffolding in every generated binary; violates DRY; harder to maintain | ❌ Rejected |
| **B. Thin template + reusable `app` package at root** | Centralized CLI/config logic; generated code is minimal; consistent UX across all binaries | ✅ Selected |
| **C. `app` package under `cmd/forge/app/`** | Not importable by generated binaries without replace directives; harder to discover | ❌ Rejected |

The selected approach (B) aligns with the issue design and project conventions: a reusable `app` package imported by the generated binary, with the template reduced to conduit/handler registration closures.

## Requirements

1. Add a `name` field to `ConduitConfig` and `HandlerConfig` in the Forge blueprint schema. Names must be unique across conduits and handlers. If omitted, derive a default from the module path (last element, with numeric disambiguation for duplicates).
2. Create a reusable `app` package at `app/` that provides Cobra/Viper scaffolding for all generated binaries.
3. Support three-layer runtime configuration with precedence: CLI flags > environment variables > config file > compile-time defaults from `forge.yaml` `options`.
4. Standardize `OptionsFromMap` as the required conduit/handler contract and document it.
5. Rewrite `main.go.tmpl` to be extremely thin: import conduits, register them with names and compile-time defaults via `app` package closures, then delegate to `app.Run(...)`.
6. Preserve existing environment variable names (`ORE_API_KEY`, `ORE_MODEL`, `ORE_BASE_URL`, `STORE_DIR`) via Viper binding for backwards-compatible secret injection.
7. Update all example blueprints (`examples/forge/http/`, `examples/forge/multi/`, `examples/forge/tui/`) to use explicit names.
8. Add comprehensive tests for the `app` package, template generation, and updated blueprint parsing.

## Task Breakdown

### Task 1: Add `Name` Field to Blueprint Schema
- **Goal**: Extend `ConduitConfig` and `HandlerConfig` with an optional `name` field, derive defaults when omitted, and enforce uniqueness.
- **Dependencies**: None.
- **Files Affected**:
  - `cmd/forge/blueprint.go`
  - `cmd/forge/blueprint_test.go`
- **New Files**: None.
- **Interfaces**:
  - `ConduitConfig.Name string` (new field)
  - `HandlerConfig.Name string` (new field)
  - `ParseBlueprint` updated to derive `Name` from module path if empty, appending numeric suffix for collisions.
  - `ParseBlueprint` updated to validate uniqueness of names across conduits and handlers.
- **Validation**:
  - `go test -race ./cmd/forge/...` passes.
  - Blueprints with duplicate names return a validation error.
  - Blueprints without explicit names get derived names matching expected defaults.
- **Details**:
  1. Add `Name string` to `ConduitConfig` and `HandlerConfig` structs.
  2. In `ParseBlueprint`, after parsing, iterate conduits and handlers. For each with empty `Name`, derive from last path element of `Module` (same logic as `deriveImportAlias` but without stdlib collision avoidance). If collision, append `1`, `2`, etc.
  3. Validate that all names are unique across the combined set of conduits and handlers.
  4. Add/update table-driven tests for name derivation, collision handling, and uniqueness validation.

### Task 2: Update Template Data Structures
- **Goal**: Thread the new `Name` field through template data so the generator can emit named conduit/handler registrations.
- **Dependencies**: Task 1.
- **Files Affected**:
  - `cmd/forge/generate.go`
  - `cmd/forge/generate_test.go`
  - `cmd/forge/build_test.go`
  - `cmd/forge/cmd_generate_test.go`
- **New Files**: None.
- **Interfaces**:
  - `ConduitTemplateData.Name string` (new field)
  - `HandlerTemplateData.Name string` (new field)
  - `buildTemplateData` updated to populate `Name` from blueprint config.
- **Validation**:
  - `go test -race ./cmd/forge/...` passes.
  - Generated template data includes expected names for all conduits/handlers.
- **Details**:
  1. Add `Name string` to `ConduitTemplateData` and `HandlerTemplateData`.
  2. Update `buildTemplateData` to set `Name` from `c.Name` / `h.Name`.
  3. Update all tests that assert template data structure.

### Task 3: Add Viper Dependency
- **Goal**: Add `github.com/spf13/viper` to the root `go.mod` so the `app` package can use it.
- **Dependencies**: None (can be done in parallel with Task 1).
- **Files Affected**:
  - `go.mod`
  - `go.sum`
- **New Files**: None.
- **Interfaces**: None.
- **Validation**:
  - `go mod tidy` succeeds.
  - `go test -race ./...` still passes (no compilation errors from new dependency).
- **Details**:
  1. Run `go get github.com/spf13/viper` in the root module.
  2. Run `go mod tidy`.

### Task 4: Create `app` Package — Config Loading
- **Goal**: Implement the `app` package's configuration layer using Viper, supporting flags, env vars, config files, and compile-time defaults.
- **Dependencies**: Task 3.
- **Files Affected**: None (all new files).
- **New Files**:
  - `app/config.go`
  - `app/config_test.go`
  - `app/doc.go`
- **Interfaces**:
  - `type Config struct { ... }` — holds parsed runtime configuration (log level, provider settings, conduit configs).
  - `func LoadConfig(conduits []ConduitRegistration) (*Config, error)` — initializes Viper, registers CLI flags, binds env vars, reads config file, merges defaults.
  - `type ConduitRegistration struct { Name string; Defaults map[string]any }` — compile-time registration used to seed Viper defaults.
  - CLI flags: `--config`, `--log-level`, `--api-key`, `--model`, `--base-url`, `--store-dir`.
  - Env vars: `ORE_CONFIG`, `ORE_LOG_LEVEL`, `ORE_API_KEY`, `ORE_MODEL`, `ORE_BASE_URL`, `ORE_STORE_DIR`, plus `ORE_CONDUIT_<NAME>_<KEY>` for conduit-specific settings (via `viper.SetEnvKeyReplacer` with `.` and `-` → `_`).
  - Config file keys: `log_level`, `api_key`, `model`, `base_url`, `store_dir`, `conduits.<name>.<key>`.
- **Validation**:
  - `go test -race ./app/...` passes.
  - Tests cover: flag override, env var override, config file override, compile-time defaults as fallback, missing required values (e.g., api-key), and invalid config file paths.
- **Details**:
  1. Create `app/doc.go` with package documentation.
  2. Create `app/config.go`:
     - Define `Config` struct with fields for global settings and per-conduit `map[string]any`.
     - Implement `LoadConfig` that:
       - Uses `pflag` (via Cobra's flag set) to define flags.
       - Uses Viper to set defaults from `ConduitRegistration.Defaults` under `conduits.<name>.<key>`.
       - Reads config file from path specified by `--config` (default `./config.yaml`).
       - Binds env vars with `ORE_` prefix; uses key replacer for nested/dashed keys.
       - Merges layers: flags > env > config > defaults.
     - Extract per-conduit config maps from Viper via `viper.GetStringMap("conduits." + name)`.
  3. Create `app/config_test.go` with table-driven tests mocking Viper state or using temporary files.

### Task 5: Create `app` Package — Agent Lifecycle
- **Goal**: Implement the `app` package's runtime orchestration: create provider, session manager, agent, instantiate conduits with merged runtime config, and run the agent.
- **Dependencies**: Task 4.
- **Files Affected**: None (all new files).
- **New Files**:
  - `app/app.go`
  - `app/provider.go`
  - `app/conduits.go`
  - `app/app_test.go`
- **Interfaces**:
  - `func Run(opts ...Option) error` — entry point for generated binaries. Sets up Cobra command internally, parses config, builds agent, and blocks on `agent.Run`.
  - `func WithConduit(name string, factory ConduitFactory, defaults map[string]any) Option` — registers a conduit.
  - `func WithHandler(name string, factory HandlerFactory, defaults map[string]any) Option` — registers a handler.
  - `type ConduitFactory func(mgr session.Manager, opts map[string]any) (conduit.Conduit, error)` — generated binary provides closure bridging `OptionsFromMap`.
  - `type HandlerFactory func(opts map[string]any) (loop.Handler, error)` — generated binary provides closure bridging `OptionsFromMap`.
  - `type Option func(*appConfig)` — functional option for `Run`.
- **Validation**:
  - `go test -race ./app/...` passes.
  - Tests use local mock implementations of `conduit.Conduit`, `provider.Provider`, `loop.Handler`, and `thread.Store` to verify orchestration without network calls.
- **Details**:
  1. Create `app/provider.go`:
     - Hardcode OpenAI provider setup for now (provider runtime config is out of scope per issue).
     - Read `api_key`, `model`, `base_url` from runtime config.
     - Return `openai.New(apiKey, model, opts...)`.
  2. Create `app/conduits.go`:
     - Define `ConduitRegistration` and `HandlerRegistration` structs.
     - Define `mergeOpts(defaults, runtime map[string]any) map[string]any` — runtime overrides defaults.
  3. Create `app/app.go`:
     - Define `appConfig` struct holding registrations.
     - `Run` sets up `slog` with configured log level.
     - Creates thread store (`thread.NewJSONStore` or `thread.NewMemoryStore`).
     - Creates OpenAI provider via `app/provider.go`.
     - Creates `loop.Step` with handlers (using `loop.WithHandlers`).
     - Creates `session.NewManager` and `agent.New`.
     - Instantiates each registered conduit by calling its factory with merged runtime opts and `mgr`.
     - Adds conduits to agent via `a.Add(...)`.
     - Runs `agent.Run(ctx)` with signal-interrupt context.
  4. Create `app/app_test.go` with mocks:
     - Mock `conduit.Conduit` (Start returns nil or blocks on context).
     - Mock `loop.Handler` (Handle returns nil).
     - Mock `provider.Provider` (Invoke returns nil or empty channel).
     - Test that `Run` correctly wires mocks together and exits on context cancellation.

### Task 6: Rewrite `main.go.tmpl` to Use `app` Package
- **Goal**: Reduce the generated binary to an extremely thin shell that imports conduits, registers them with the `app` package, and calls `app.Run(...)`.
- **Dependencies**: Task 2, Task 5.
- **Files Affected**:
  - `cmd/forge/templates/main.go.tmpl`
- **New Files**: None.
- **Interfaces**: The generated `main()` function signature remains `func main()`, but the body becomes ~20 lines.
- **Validation**:
  - `go test -race ./cmd/forge/...` passes.
  - A generated binary from `examples/forge/http/forge.yaml` compiles and produces `--help` output showing the new CLI flags.
- **Details**:
  1. Rewrite `main.go.tmpl`:
     - Keep imports for `app`, each conduit, and each handler.
     - `func main() { app.Run( ... ) }`.
     - For each conduit, emit: `app.WithConduit("{{.Name}}", func(mgr session.Manager, opts map[string]any) (conduit.Conduit, error) { {{.ImportAlias}}Opts, err := {{.ImportAlias}}.OptionsFromMap(opts); ... return {{.ImportAlias}}.New(mgr, {{.ImportAlias}}Opts...) }, {{.OptionsLiteral}})`.
     - For each handler, emit similar `app.WithHandler` registration.
  2. The template still imports `github.com/andrewhowdencom/ore/app` and `github.com/andrewhowdencom/ore/x/conduit`.
  3. Update `cmd/forge/generate_test.go` assertions to match new template output.

### Task 7: Verify and Document `OptionsFromMap` Contract
- **Goal**: Ensure all existing conduits and handlers export `OptionsFromMap` with the exact signature `func OptionsFromMap(m map[string]any) ([]Option, error)`, and add package-level documentation about the contract.
- **Dependencies**: None (can be done in parallel with Tasks 1–3).
- **Files Affected**:
  - `x/conduit/doc.go`
  - `x/conduit/http/config.go` (verify only)
  - `x/conduit/tui/config.go` (verify only)
  - `x/conduit/slack/config.go` (verify only)
  - `x/conduit/telegram/config.go` (verify only)
- **New Files**: None.
- **Interfaces**: Confirm all existing packages already implement `func OptionsFromMap(m map[string]any) ([]Option, error)`.
- **Validation**:
  - `go test -race ./x/conduit/...` passes.
  - No compilation errors from missing `OptionsFromMap`.
- **Details**:
  1. Read each conduit's `config.go` to verify `OptionsFromMap` exists with the expected signature.
  2. If any conduit is missing it, add it using the same `mapstructure` + `yaml` tag pattern.
  3. Update `x/conduit/doc.go` to document that all conduit packages MUST export `OptionsFromMap(m map[string]any) ([]Option, error)` for Forge compatibility.

### Task 8: Update Example Blueprints
- **Goal**: Add explicit `name` fields to all Forge example blueprints so they serve as correct references.
- **Dependencies**: Task 1.
- **Files Affected**:
  - `examples/forge/http/forge.yaml`
  - `examples/forge/multi/forge.yaml`
  - `examples/forge/tui/forge.yaml`
  - `examples/forge/README.md` (if it documents the blueprint schema)
- **New Files**: None.
- **Interfaces**: None.
- **Validation**:
  - `go run ./cmd/forge -config examples/forge/http/forge.yaml` succeeds and the generated binary compiles.
  - Same for `multi` and `tui` examples.
- **Details**:
  1. Add `name: http` to `examples/forge/http/forge.yaml`.
  2. Add `name: http` and `name: tui` to `examples/forge/multi/forge.yaml`.
  3. Add `name: tui` to `examples/forge/tui/forge.yaml`.
  4. Update any README documentation that shows blueprint snippets.

### Task 9: Integration Validation
- **Goal**: End-to-end validation that the full pipeline works: blueprint parsing → code generation → compilation → CLI surface → config override.
- **Dependencies**: Tasks 1–8.
- **Files Affected**: None (validation only).
- **New Files**: None.
- **Interfaces**: None.
- **Validation**:
  - `go test -race ./...` passes across all packages.
  - `go run ./cmd/forge -config examples/forge/http/forge.yaml` succeeds.
  - The generated `./http-chat` binary responds to `--help` with documented flags.
  - Running `./http-chat --config ./custom-config.yaml` with a config file overriding `conduits.http.addr` starts the server on the overridden port.
- **Details**:
  1. Run unit tests: `go test -race ./...`.
  2. Run Forge build for each example and verify compilation.
  3. Run generated binary `--help` to verify CLI flags are present.
  4. Create a temporary `custom-config.yaml` with a different port, run the binary, and verify it listens on the overridden port (or at least parses the config without error).
  5. Verify env var override: `ORE_LOG_LEVEL=debug ./http-chat` logs at debug level.

## Dependency Graph

- Task 1 → Task 2 (Template data needs names from blueprint)
- Task 1 → Task 8 (Examples need the new name field)
- Task 3 → Task 4 (Config loading needs Viper)
- Task 4 → Task 5 (Agent lifecycle needs config loading)
- Task 2 → Task 6 (Template rewrite needs name field in data)
- Task 5 → Task 6 (Template uses `app` package API)
- Task 6 → Task 9 (Integration test needs generated binary)
- Task 7 || Task 8 (Can proceed in parallel with other tasks)
- Task 1 || Task 3 || Task 7 (Independent foundational work)

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| **Viper env var mapping for dashed conduit names is brittle** | Medium | Medium | Spike: verify `viper.SetEnvKeyReplacer` behavior with `-` in keys; add unit tests for env var binding in `app/config_test.go`. |
| **`app` package becomes a "god package" importing too many modules** | Medium | Medium | Keep `app` focused on scaffolding only; defer provider-agnostic abstraction to the separate provider runtime config workstream. Document that `app` is temporary scaffolding. |
| **Generated binary still requires generated closures, so "extremely thin" is relative** | Low | Low | Acceptable per issue: "Closure-based type bridging in generated code — Necessary because Go's type system prevents generic OptionsFromMap invocation." Document this in template comments. |
| **Conduit or handler missing `OptionsFromMap`** | High | Low | Task 7 explicitly verifies all existing packages; add compile-time validation in Forge if feasible (e.g., attempt to parse generated code, which already happens). |
| **Config file schema changes later when provider runtime config arrives** | Medium | Medium | Keep provider settings flat at top level for now; document that nested `providers.<name>` is reserved for future work. |

## Validation Criteria

- [ ] `go test -race ./...` passes without failures.
- [ ] `cmd/forge/blueprint_test.go` validates name derivation, collision handling, and uniqueness.
- [ ] `cmd/forge/generate_test.go` validates new template output structure.
- [ ] `app/config_test.go` validates all four config layers (flag, env, file, default) independently and in combination.
- [ ] `app/app_test.go` validates agent lifecycle with mocked dependencies.
- [ ] A generated binary from any `examples/forge/` blueprint compiles and produces `--help` output.
- [ ] The generated binary accepts `--config`, `--log-level`, `--api-key`, `--model`, `--base-url`, and `--store-dir` flags.
- [ ] The generated binary respects a config file overriding a conduit compile-time default.
- [ ] The generated binary respects an env var overriding a config file setting.
- [ ] All example blueprints include explicit `name` fields.
