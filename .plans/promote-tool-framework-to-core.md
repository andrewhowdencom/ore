# Plan: Promote Tool Framework to Core

## Objective

Elevate tool execution from an optional extension (`x/tool/`) to a first-class framework primitive (`tool/`). All reference agents (GitHub issue #100) depend on tools, and treating them as a core concern enables cleaner composition today and lays the architectural groundwork for future agent self-modification (sub-agents, workflow changes, cognitive model switching) via a richer tool execution context.

## Context

### Current State
- `artifact.ToolCall`, `artifact.ToolResult`, `provider.Tool` are already in core packages
- Tool *execution framework* lives under `x/tool/`: `Registry` (concrete, mutex-protected), `Handler` (implements `loop.Handler`), `ToolFunc`, `RemoteSource`, schema validation, MCP integration, skills discovery, and concrete tool implementations (bash, calculator, filesystem)
- Every application manually wires tool execution:
  ```go
  registry := tool.NewRegistry()
  registry.Register(...)
  step := loop.New(
      loop.WithHandlers(registry.Handler()),
      loop.WithInvokeOptions(openai.WithTools(registry.Tools())),
  )
  ```
- The `loop.Step` and `cognitive.ReAct` treat tool execution as an optional artifact handler, not a standard turn lifecycle phase
- Core packages (`cognitive/`, `session/`) cannot reference tool concepts without importing `x/tool/`, which violates the framework's dependency graph conventions

### Design Principle
Universal framework concerns belong in core root packages; agent-specific or swappable implementations stay in `x/`. Skills discovery (`x/tool/skills/`) and concrete tools (`x/tool/calculator/`, `x/tool/bash/`, `x/tool/filesystem/`) are not universal. The tool *registration*, *execution contract*, and *schema validation* are universal.

### Architectural Decision
Adopt the **canonical handler** approach (minimal changes). Extract the tool framework contract to `tool/` (root) while keeping `loop.Step` thin. Tool execution continues through the existing `loop.Handler` extension point, bridged by a canonical handler in `x/tool/handler.go`. This preserves composability and leaves the door open for future native loop integration without a breaking structural migration.

## Requirements

1. Root `tool/` package defines the tool execution contract (`ToolFunc`), registration abstraction (`Registry` interface with default implementation), remote source interface (`RemoteSource`), and schema validation (`ValidateSchema`)
2. Root `tool/` must be importable by core packages (`cognitive/`, `session/`, `loop/` if needed) without creating dependency cycles
3. `x/tool/` retains: concrete tool implementations (`bash/`, `calculator/`, `filesystem/`), MCP client (`mcp/`), skills discovery (`skills/`), and the `loop.Handler` bridge (`handler.go`)
4. Existing examples continue to work with minimal wiring changes (import path update)
5. `go test -race ./...` passes after each task

## Task Breakdown

### Task 1: Extract Core Tool Types to Root `tool/` Package
- **Goal**: Create a root `tool/` package containing the universal tool framework contracts extracted from `x/tool/`
- **Dependencies**: None
- **Files Affected**:
  - `x/tool/tool.go` → `tool/tool.go` (move `ToolFunc`)
  - `x/tool/registry.go` → `tool/registry.go` (extract `Registry` interface + minimal default implementation)
  - `x/tool/schema.go` → `tool/schema.go` (move `ValidateSchema`)
- **New Files**:
  - `tool/tool.go`
  - `tool/registry.go`
  - `tool/schema.go`
  - `tool/doc.go`
- **Interfaces**:
  ```go
  package tool

  type Registry interface {
      Register(name, description string, schema map[string]any, fn ToolFunc) error
      Tools() []provider.Tool
  }

  type ToolFunc func(ctx context.Context, args map[string]any) (any, error)
  ```
  The default in-memory registry implements this interface. `RemoteSource` interface moves here unchanged.
- **Validation**: `go test ./tool/...` passes. The package compiles and exports its types.
- **Details**: Move `ToolFunc` to `tool/tool.go`. Extract a `Registry` interface from the current concrete `Registry` struct. Move the concrete `Registry` implementation (with `sync.RWMutex`, `localTool`, `WithMCPServer` option) to `tool/registry.go` as the default implementation, analogous to `state.Buffer`. Move `ValidateSchema` and related types to `tool/schema.go`. Update all imports within the moved code. Add `tool/doc.go` with package-level documentation explaining that this is the core tool framework and that concrete implementations and discovery mechanisms live in `x/tool/`.

### Task 2: Refactor `x/tool/` to Import Root `tool/` Package
- **Goal**: Update `x/tool/` to import the root `tool/` package, removing duplicate definitions and updating the handler to use the core registry interface
- **Dependencies**: Task 1
- **Files Affected**:
  - `x/tool/registry.go` — remove or reduce to re-exports if needed; concrete registry implementation is now in root
  - `x/tool/handler.go` — update to accept `tool.Registry` interface instead of concrete `*Registry`
  - `x/tool/tool.go` — remove (type moved to root)
  - `x/tool/schema.go` — remove (moved to root)
  - `x/tool/handler_test.go` — update imports
  - `x/tool/registry_test.go` — update imports
  - `x/tool/schema_test.go` — update imports
- **New Files**: None
- **Interfaces**: `Handler` struct now holds `registry tool.Registry` instead of `*Registry`
- **Validation**: `go test ./x/tool/...` passes. All `x/tool/` tests and concrete tool implementations compile.
- **Details**: Delete or stub `x/tool/tool.go`, `x/tool/schema.go`. Update `x/tool/handler.go` to import `github.com/andrewhowdencom/ore/tool` and use `tool.Registry` in `Handler` struct and `NewHandler` constructor. Update `x/tool/registry.go` — if the concrete implementation moved to root, this file may become a thin re-export or be deleted entirely. Update all test files to import `tool` for registry construction. Verify that `x/tool/mcp/`, `x/tool/skills/`, `x/tool/bash/`, `x/tool/calculator/`, `x/tool/filesystem/` compile with updated imports.

### Task 3: Update Examples and Documentation
- **Goal**: Update all applications and examples that import `github.com/andrewhowdencom/ore/x/tool` to use `github.com/andrewhowdencom/ore/tool`
- **Dependencies**: Task 2
- **Files Affected**:
  - `examples/single-turn-cli/main.go`
  - `examples/calculator/main.go`
  - `examples/http-chat/main.go`
  - `examples/filesystem/main.go`
  - `x/tool/doc.go` (update to reflect new package boundaries)
- **New Files**: None
- **Interfaces**: No new interfaces; import paths change
- **Validation**: `go build ./examples/...` succeeds. `go test ./...` passes.
- **Details**: Replace `github.com/andrewhowdencom/ore/x/tool` with `github.com/andrewhowdencom/ore/tool` in all example imports. Update `x/tool/doc.go` to clarify that `x/tool/` now contains concrete tool implementations, discovery mechanisms, and the `loop.Handler` bridge, while the core contracts live in `tool/`. Verify that `examples/http-chat`, `examples/calculator`, `examples/filesystem`, and `examples/single-turn-cli` compile and their wiring patterns still work. No logic changes — only import paths.

### Task 4: (Optional) Add Generic `provider.WithTools` Invoke Option
- **Goal**: Add a provider-agnostic `WithTools` invoke option to reduce provider-specific wiring in applications
- **Dependencies**: Task 1
- **Files Affected**:
  - `provider/provider.go`
  - `x/provider/openai/openai.go`
- **New Files**: None
- **Interfaces**:
  ```go
  package provider

  type ToolsOption struct {
      Tools []Tool
  }
  func (ToolsOption) IsInvokeOption() {}
  func WithTools(tools []Tool) InvokeOption { return ToolsOption{Tools: tools} }
  ```
- **Validation**: `go test ./provider/...` and `go test ./x/provider/openai/...` pass.
- **Details**: Add `ToolsOption` to `provider/provider.go`. Update `x/provider/openai/openai.go` to type-assert `provider.ToolsOption` in its `Invoke` method, alongside its existing `toolOption`. Keep `openai.WithTools` as a convenience wrapper that delegates to `provider.WithTools`. This is a backward-compatible change that makes application wiring slightly more provider-agnostic: `loop.WithInvokeOptions(provider.WithTools(registry.Tools()))` works for any adapter that supports it.

### Task 5: (Optional) Make `cognitive.ReAct` Tool-Aware
- **Goal**: Provide a convenience constructor or option for `cognitive.ReAct` that auto-wires the canonical tool handler when a registry is configured
- **Dependencies**: Task 2, Task 3
- **Files Affected**:
  - `cognitive/react.go`
  - `cognitive/doc.go`
- **New Files**: None
- **Interfaces**:
  ```go
  func NewReActWithTools(step *loop.Step, prov provider.Provider, registry tool.Registry) *ReAct
  ```
  Or alternatively, a `TurnProcessor` variant.
- **Validation**: `go test ./cognitive/...` passes.
- **Details**: This is optional sugar. A `cognitive.NewReActWithTools` constructor would create a `loop.Step` with the canonical tool handler already registered, reducing application boilerplate. The constructor should use `tool.NewHandler` (from `x/tool/handler.go`, imported via `x/tool`) to bridge the registry to the handler. This keeps `cognitive/` core-importable (it imports `tool/` root, not `x/tool/`) but the convenience constructor may import `x/tool` for the handler bridge. Evaluate whether this creates an acceptable dependency or if it should live in `x/cognitive/` instead.

## Dependency Graph

- Task 1 → Task 2 (Task 2 depends on Task 1)
- Task 2 → Task 3 (Task 3 depends on Task 2)
- Task 1 || Task 4 (Task 4 is independent and can be done in parallel with Task 1, but must be completed before Task 5 if Task 5 is attempted)
- Task 2 || Task 5 (Task 5 depends on Task 2)

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Import cycle between `tool/` and `loop/` if `tool/` tries to implement `loop.Handler` | High | Medium | Prohibit `tool/` from importing `loop/`. The handler bridge stays in `x/tool/handler.go` which may import both packages. |
| Tests in `x/tool/` break due to moved types | Medium | High | Run `go test ./x/tool/...` after Task 2. Update test imports before committing. |
| Examples fail to compile due to import path changes | Medium | High | Run `go build ./examples/...` after Task 3. |
| Concrete `Registry` with `sync.RWMutex` feels too opinionated for root package | Low | Medium | Document that the root `Registry` is the default in-memory implementation, analogous to `state.Buffer`. Extract an interface later if needed. |
| `provider.WithTools` generic option not adopted by all adapters | Low | Low | The option is additive and backward-compatible. Adapters can adopt it incrementally. |
| Future self-modification API requires breaking the `ToolFunc` signature | Medium | Low | The `Registry` interface design leaves room for a richer execution context later. `ToolFunc` can be deprecated in favor of a context-rich variant without breaking the interface immediately. |

## Validation Criteria

- [ ] `go test -race ./...` passes after every task
- [ ] `go build ./examples/...` succeeds after Task 3
- [ ] Root `tool/` package is importable by `cognitive/` without creating import cycles
- [ ] `x/tool/skills/`, `x/tool/mcp/`, `x/tool/bash/`, `x/tool/calculator/`, `x/tool/filesystem/` all compile and their tests pass
- [ ] Examples demonstrate the same tool-calling behavior with updated import paths
- [ ] Package documentation (`tool/doc.go`, `x/tool/doc.go`) accurately describes the new boundary between core and extensions
- [ ] No references to `github.com/andrewhowdencom/ore/x/tool` remain in core packages (`cognitive/`, `session/`, `loop/`, `provider/`, `state/`, `artifact/`)
