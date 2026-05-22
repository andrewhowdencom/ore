# Plan: Move OpenAI Provider to x/provider/openai Submodule

## Objective
Relocate the OpenAI provider adapter from `provider/openai/` in the core module to `x/provider/openai/` as a standalone submodule, refactor its constructor to pure functional options with error return (matching existing `x/` extension patterns), and update all consumers. This removes the `github.com/openai/openai-go` dependency from the root module and establishes `x/provider/` as the namespace for future provider adapters (Anthropic, Ollama, etc.).

## Context
- `provider/openai/` currently contains `doc.go`, `openai.go`, `openai_test.go` in the root module.
- Root `go.mod` directly depends on `github.com/openai/openai-go v1.12.0`.
- Six examples import `github.com/andrewhowdencom/ore/provider/openai` and call `openai.New(apiKey, model, opts...)`.
- `x/tool/doc.go` references `provider/openai/` in documentation comments.
- Existing `x/` submodules (`guardrails`, `systemprompt`, `tool/mcp`) use `New(opts ...Option) (*T, error)` with validation of required configuration.
- Examples are part of the root module (no `go.mod` in `examples/`), so root `go.mod` will need a `replace` directive for the new submodule.
- Forge has been removed from the project; all Forge-specific configuration patterns (`Config`, `OptionsFromMap`, blueprints, descriptors) are out of scope.

## Architectural Blueprint
- **`provider/`** at root keeps the core `provider.Provider` interface, `Tool`, and `InvokeOption`. Concrete adapters branch off `provider/` and never import `core/`.
- **`x/provider/openai/`** becomes the first concrete provider adapter in a new `x/provider/` extension namespace. Future adapters follow the exact same submodule pattern.
- **Submodule `go.mod`** depends on the root ore module (for `artifact/`, `state/`, `provider/`), `github.com/openai/openai-go`, and `stretchr/testify`. It uses a `replace` directive pointing to the root module, matching all existing `x/` submodules.
- **Root `go.mod`** gains a `replace` directive for `x/provider/openai` and drops `github.com/openai/openai-go`.
- **Constructor pattern** matches `x/guardrails.New`, `x/systemprompt.New`, and `x/tool/mcp.NewClient`: pure functional options with `(*T, error)` return, validating that required options (`apiKey`, `model`) were actually provided.

Only one architectural path is viable because the project already has an established `x/` extension pattern with `go.mod`, `replace` directives, and functional-options constructors. Diverging from this pattern would create inconsistency.

## Requirements
1. Create `x/provider/openai/` as a submodule with its own `go.mod`.
2. Move and refactor `provider/openai/` files: constructor becomes `New(opts ...Option) (*Provider, error)` with `WithAPIKey`, `WithModel`, `WithBaseURL`, `WithHTTPClient`.
3. Update all example imports and constructor calls to use the new path and signature.
4. Update `x/tool/doc.go` documentation reference from `provider/openai/` to `x/provider/openai/`.
5. Remove `provider/openai/` from root, update root `go.mod` with replace directive and dependency cleanup.
6. Verify all tests pass and all examples compile.

## Task Breakdown

### Task 1: Create x/provider/openai Submodule and Move Files
- **Goal**: Establish `x/provider/openai/` as a standalone submodule with moved and refactored provider code.
- **Dependencies**: None
- **Files Affected**: `provider/openai/doc.go`, `provider/openai/openai.go`, `provider/openai/openai_test.go`
- **New Files**: `x/provider/openai/doc.go`, `x/provider/openai/openai.go`, `x/provider/openai/openai_test.go`, `x/provider/openai/go.mod`
- **Interfaces**:
  - `New(opts ...Option) (*Provider, error)` — returns error, validates `apiKey != ""` and `model != ""`
  - `WithAPIKey(key string) Option`
  - `WithModel(model string) Option`
  - `WithBaseURL(url string) Option` (preserved)
  - `WithHTTPClient(client option.HTTPClient) Option` (preserved)
- **Validation**:
  - `cd x/provider/openai && go mod tidy`
  - `cd x/provider/openai && go test -race ./...` passes
- **Details**:
  - In `openai.go`:
    - Change `config` struct to not pre-populate `apiKey` or `model` from positional args.
    - Add `WithAPIKey(key string) Option` and `WithModel(model string) Option`.
    - Change `New(apiKey, model string, opts ...Option) *Provider` to `New(opts ...Option) (*Provider, error)`.
    - In `New`, after applying opts, validate `cfg.apiKey != ""` and `cfg.model != ""`. Return `fmt.Errorf("missing required option: apiKey")` or `fmt.Errorf("missing required option: model")` if absent.
    - Keep `WithBaseURL` and `WithHTTPClient` unchanged.
  - In `openai_test.go`:
    - Update all `New("test-key", "gpt-4", ...)` calls to `New(WithAPIKey("test-key"), WithModel("gpt-4"), ...)`.
    - Handle error return: `p, err := New(...); require.NoError(t, err)`.
  - In `doc.go`:
    - Update package comment to reflect the new module path.
  - Create `x/provider/openai/go.mod`:
    - `module github.com/andrewhowdencom/ore/x/provider/openai`
    - `go 1.26.2`
    - `require github.com/andrewhowdencom/ore v0.0.0`
    - `require github.com/openai/openai-go v1.12.0`
    - `require github.com/stretchr/testify v1.11.1`
    - `replace github.com/andrewhowdencom/ore => ../../..`
    - Run `go mod tidy` to resolve indirects.

### Task 2: Update Root go.mod and Remove Old Directory
- **Goal**: Remove `provider/openai/` from root module and adjust root dependencies.
- **Dependencies**: Task 1
- **Files Affected**: `go.mod`
- **New Files**: None
- **Interfaces**: None
- **Validation**:
  - `go mod tidy` in root completes successfully.
  - Root `go.mod` no longer lists `github.com/openai/openai-go` as a direct dependency.
  - `go test -race ./...` from root passes (openai tests are gone from root).
- **Details**:
  - Delete `provider/openai/doc.go`, `provider/openai/openai.go`, `provider/openai/openai_test.go`.
  - Add `replace github.com/andrewhowdencom/ore/x/provider/openai => ./x/provider/openai` to root `go.mod`.
  - Remove `github.com/openai/openai-go v1.12.0` from root `go.mod` (or let `go mod tidy` remove it after files are deleted).
  - Run `go mod tidy` in root.

### Task 3: Update Example Consumers
- **Goal**: Update all 6 examples to import from `x/provider/openai` and use the new error-returning constructor.
- **Dependencies**: Task 1
- **Files Affected**:
  - `examples/single-turn-cli/main.go`
  - `examples/tui-chat/main.go`
  - `examples/calculator/main.go`
  - `examples/http-chat/main.go`
  - `examples/filesystem/main.go`
  - `examples/workshop/main.go`
- **New Files**: None
- **Interfaces**: None
- **Validation**:
  - `go build ./examples/single-turn-cli`, `go build ./examples/tui-chat`, `go build ./examples/calculator`, `go build ./examples/http-chat`, `go build ./examples/filesystem`, `go build ./examples/workshop` all compile successfully.
- **Details**:
  - In each file, change the import path from `github.com/andrewhowdencom/ore/provider/openai` to `github.com/andrewhowdencom/ore/x/provider/openai`.
  - Change every `prov := openai.New(apiKey, modelName, opts...)` to:
    ```go
    prov, err := openai.New(openai.WithAPIKey(apiKey), openai.WithModel(modelName), opts...)
    if err != nil {
        return fmt.Errorf("create openai provider: %w", err)
    }
    ```
  - All examples already return `error` from their `run()` functions, so this pattern fits naturally.

### Task 4: Update Documentation Reference
- **Goal**: Update `x/tool/doc.go` to reference the new provider path.
- **Dependencies**: Task 1
- **Files Affected**: `x/tool/doc.go`
- **New Files**: None
- **Interfaces**: None
- **Validation**:
  - `go build ./x/tool` passes.
  - No remaining references to `provider/openai/` in documentation comments.
- **Details**:
  - Find the text `provider/openai/` in `x/tool/doc.go` comments and change to `x/provider/openai/`.

### Task 5: Verify Full Build and Tests
- **Goal**: Confirm everything compiles and tests pass across root and submodule.
- **Dependencies**: Task 2, Task 3, Task 4
- **Files Affected**: None
- **New Files**: None
- **Interfaces**: None
- **Validation**:
  - `go test -race ./...` from root passes.
  - `go test -race ./...` from `x/provider/openai/` passes.
  - `go build ./examples/...` passes.
  - `go vet ./...` clean.
  - No remaining references to `github.com/andrewhowdencom/ore/provider/openai` anywhere in the codebase.
- **Details**:
  - Run `grep -r "github.com/andrewhowdencom/ore/provider/openai" . --include="*.go" --exclude-dir=.git --exclude-dir=.worktrees` and confirm zero results.
  - Run `grep -r "provider/openai" . --include="*.md" --exclude-dir=.git --exclude-dir=.worktrees` and update any stray references.

## Dependency Graph
- Task 1 → Task 2 (root cleanup depends on files being moved)
- Task 1 → Task 3 (examples need new import path to exist)
- Task 1 → Task 4 (doc reference needs new path to exist)
- Task 2 || Task 3 || Task 4 (parallelizable after Task 1)
- Task 2 → Task 5
- Task 3 → Task 5
- Task 4 → Task 5

## Risks & Mitigations
| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Submodule `go.mod` missing `replace` directive or root `go.mod` missing replace | High | Medium | Explicitly verify both `go.mod` files have correct `replace` directives; validation step builds both modules. |
| Test file has hidden dependency on root module paths | Medium | Low | Review `openai_test.go` imports carefully; all imports are root-level packages (`artifact`, `provider`, `state`) which remain valid from the submodule. |
| Example compilation fails due to missing error handling in new constructor | Medium | Low | Each example needs explicit `if err != nil` check; validation step builds all examples. |
| Root `go.mod` still retains `openai-go` because other indirect dependency pulls it in | Low | Low | Run `go mod tidy` and verify; if retained, trace the dependency and exclude if necessary. |

## Validation Criteria
- [ ] `x/provider/openai/` exists as a submodule with `go.mod`, `doc.go`, `openai.go`, `openai_test.go`
- [ ] `provider/openai/` no longer exists in the core module
- [ ] `openai.New` signature is `New(opts ...Option) (*Provider, error)` with `WithAPIKey` and `WithModel`
- [ ] `New` returns an error when `apiKey` or `model` is empty
- [ ] All tests in `x/provider/openai/` pass with `-race`
- [ ] All 6 examples compile successfully
- [ ] Root `go.mod` does not list `github.com/openai/openai-go` as a direct dependency
- [ ] Root `go.mod` has a `replace` directive for `github.com/andrewhowdencom/ore/x/provider/openai`
- [ ] `x/tool/doc.go` references `x/provider/openai/` instead of `provider/openai/`
- [ ] `go test -race ./...` passes from root
- [ ] No remaining references to `github.com/andrewhowdencom/ore/provider/openai` in the codebase
