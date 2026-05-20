# Plan: Add MCP Client Support to tool.Registry

## Objective

Refactor `x/tool.Registry` into a self-describing, unified source of truth that composes local Go functions and remote tools discovered from MCP servers. Introduce a new `x/tool/mcp` module providing an MCP client with stdio and SSE transports, namespacing for multi-server scenarios, and seamless integration with the existing `loop.Handler` and `provider.Tool` contracts.

## Context

The ore framework currently separates tool concerns across three mechanisms:

1. **Provider adapter** (`provider/openai/openai.go`) — receives `[]provider.Tool` per invocation via `openai.WithTools()` and serializes them into native API parameters.
2. **Artifact handler** (`x/tool/handler.go`) — a `loop.Handler` that routes `artifact.ToolCall` to registered `ToolFunc` implementations via `x/tool/registry.go`.
3. **Application wiring** — applications manually keep `provider.Tool` metadata and `x/tool.Registry` execution in sync (e.g., `examples/calculator/main.go`, `examples/http-chat/main.go`).

The current `Registry` (`x/tool/registry.go`) is a simple `map[string]ToolFunc` with no schema awareness. `Register` takes only a name and function. Tool metadata (`provider.Tool` — name, description, JSON schema) is defined manually in the `x/tool/calculator` package and passed separately to the provider.

Key files and their current roles:
- `x/tool/tool.go` — defines `ToolFunc` signature.
- `x/tool/registry.go` — `Registry` struct, `NewRegistry()`, `Register(name, fn)`, `Handler()`.
- `x/tool/handler.go` — `Handler` implements `loop.Handler`, routes `ToolCall` to registry lookup.
- `x/tool/handler_test.go` — handler unit tests using inline `Register` calls.
- `x/tool/registry_test.go` — registry unit tests for registration, overwrite, concurrency.
- `x/tool/calculator/calculator.go` — `Add`, `Multiply` functions and `AddTool`, `MultiplyTool` `provider.Tool` descriptors.
- `x/tool/calculator/handler_integration_test.go` — integration tests registering calculator tools.
- `x/tool/calculator/doc.go` — package documentation with old API examples.
- `x/tool/doc.go` — package documentation with old API examples.
- `examples/calculator/main.go` — reference app manually wiring registry and `[]provider.Tool`.
- `examples/http-chat/main.go` — HTTP chat server manually wiring registry and `[]provider.Tool`.
- `examples/single-turn-cli/main.go` — commented-out old API example.
- `provider/provider.go` — defines `provider.Tool` struct and `InvokeOption` interface.
- `provider/openai/openai.go` — implements `WithTools([]provider.Tool)` and `serializeTools`.
- `loop/handler.go` — `loop.Handler` interface contract.
- `go.work` — workspace definition; `x/tool/calculator` is a separate module.

No MCP-related code or dependencies exist in the repository today.

## Architectural Blueprint

### Selected Architecture

The plan follows the design proposed in GitHub issue #139 with one structural decision: `x/tool/mcp` will be a **separate Go module** (following the `x/tool/calculator` and `x/conduit/*` patterns) to isolate the external MCP SDK dependency from the main module.

**Major components:**

1. **Self-describing `tool.Registry`** (`x/tool` package, main module)
   - Captures name, description, JSON schema, and function at registration time.
   - Exposes `Tools() []provider.Tool` as the single source of truth for provider adapters.
   - Defines a `RemoteSource` interface consumed by the registry but implemented externally.
   - Uses functional-options constructor `NewRegistry(opts ...Option)` with `WithMCPServer(source RemoteSource)`.
   - `Handler()` routes `ToolCall` artifacts to local functions or remote sources based on namespacing.

2. **`tool.RemoteSource` interface** (`x/tool` package)
   - Decouples the registry from MCP specifics so `x/tool` never imports `x/tool/mcp`.
   - Contract: `Name() string`, `Tools() []provider.Tool`, `Call(ctx, name, args) (any, error)`.

3. **`tool/mcp` client module** (`x/tool/mcp` package, separate module)
   - Implements `tool.RemoteSource`.
   - `mcp.NewClient(opts ...mcp.Option)` with functional options for transport, auth, and naming.
   - Transport options: `WithStdio(command, args...)`, `WithSSE(url string)`.
   - Auth options: `WithBearerToken(token)`, `WithHeader(key, value)`.
   - Name option: `WithName("filesystem")` sets the namespace prefix.
   - Performs MCP initialization handshake and `tools/list` discovery during `NewClient` so `RemoteSource.Tools()` returns cached results.

4. **Namespacing strategy**
   - Local tools registered directly on `Registry` remain **unprefixed**.
   - MCP-discovered tools are prefixed with the `RemoteSource.Name()` + `/` (e.g., `filesystem/read_file`).
   - `Registry.Tools()` prepends prefixes when merging remote tool lists.
   - `Handler` splits `ToolCall.Name` on `/` to route namespaced calls to the correct `RemoteSource`, stripping the prefix before calling `RemoteSource.Call`.

### Evaluated Alternatives

- **Alternative A: Keep old `Register` and add `RegisterTool(provider.Tool, ToolFunc)` alongside.** Rejected because the issue explicitly prefers breaking changes for structural cleanliness at this stage of the project.
- **Alternative B: Embed MCP client directly into `Registry` struct instead of `RemoteSource` interface.** Rejected because it would couple `x/tool` to MCP specifics, violating the extension model and creating future inflexibility.
- **Alternative C: Make `x/tool/mcp` a package in the main module instead of a separate module.** Rejected because it would pull the MCP SDK into the main module dependency graph, contradicting the established pattern of isolating heavy external dependencies in `x/` submodules.

## Requirements

1. `tool.Registry` must capture schema and description at registration time.
2. `tool.Registry.Tools()` must return a merged `[]provider.Tool` including namespaced MCP tools.
3. `tool.Registry.Handler()` must correctly route `ToolCall` to local functions or the appropriate MCP server.
4. `tool.NewRegistry()` must accept functional options, including `WithMCPServer(RemoteSource)`.
5. `tool.RemoteSource` interface must be defined in `x/tool` for decoupled integration.
6. `x/tool/mcp` must exist as a separate module with `NewClient`, transport options (stdio, SSE), and auth options.
7. `x/tool/mcp` client must implement `tool.RemoteSource`.
8. No import cycles between `x/tool` and `x/tool/mcp`.
9. `examples/calculator` must be updated to use the new `Register` signature and `Registry.Tools()`.
10. All tests must pass with race detection (`go test -race ./...`).
11. Package documentation (`doc.go`) must be updated for both `x/tool` and `x/tool/mcp`.

## Task Breakdown

### Task 1: Refactor x/tool into Self-Describing Registry
- **Goal**: Redesign `Registry` to store tool metadata alongside functions, add functional-options constructor, define `RemoteSource`, implement `Tools()`, and update `Handler` routing.
- **Dependencies**: None.
- **Files Affected**:
  - `x/tool/registry.go`
  - `x/tool/handler.go`
  - `x/tool/tool.go`
  - `x/tool/doc.go`
  - `x/tool/handler_test.go`
  - `x/tool/registry_test.go`
- **New Files**: None.
- **Interfaces**:
  - `Register(name, description string, schema map[string]any, fn ToolFunc)` — replaces old `Register(name, fn)`.
  - `NewRegistry(opts ...Option) *Registry` — functional-options constructor.
  - `WithMCPServer(source RemoteSource) Option` — registers a remote source.
  - `RemoteSource interface { Name() string; Tools() []provider.Tool; Call(ctx context.Context, name string, args map[string]any) (any, error) }`.
  - `Registry.Tools() []provider.Tool` — returns merged local + prefixed remote tools.
  - `Registry.Handler() *Handler` — unchanged signature, updated internals for namespaced routing.
- **Validation**:
  - `go test ./x/tool/...` passes.
  - `go vet ./x/tool/...` passes.
  - No new compiler errors in `x/tool` package.
- **Details**:
  1. Define an internal `localTool` struct holding name, description, schema, and fn.
  2. Change `Registry.tools` from `map[string]ToolFunc` to `map[string]*localTool`.
  3. Change `Register` to accept four arguments and store a `localTool`.
  4. Update `lookup` to return `ToolFunc` from the internal struct.
  5. Add `remoteSources []RemoteSource` field to `Registry`.
  6. Add `Option` type and `NewRegistry(opts ...Option)` constructor.
  7. Implement `WithMCPServer(source RemoteSource)` option appending to `remoteSources`.
  8. Implement `Tools()` iterating local tools (unprefixed) and remote sources (prefixing each with `source.Name() + "/"`).
  9. Update `Handler.Handle` to check for `/` in `ToolCall.Name`; if present, split into namespace and tool name, look up the matching `RemoteSource` by namespace, and route via `RemoteSource.Call`. If not present, route to local `lookup` as before.
  10. Update all test `Register` calls to include description and schema (can use `""` and `nil` for trivial tests).
  11. Update `x/tool/doc.go` to reflect the new API patterns.

### Task 2: Update Calculator Package and Example Applications
- **Goal**: Migrate all existing consumers of the old `Register` API to the new self-describing API, eliminating manual `[]provider.Tool` construction.
- **Dependencies**: Task 1.
- **Files Affected**:
  - `x/tool/calculator/handler_integration_test.go`
  - `x/tool/calculator/doc.go`
  - `examples/calculator/main.go`
  - `examples/http-chat/main.go`
  - `examples/single-turn-cli/main.go`
- **New Files**: None.
- **Interfaces**: Same as Task 1; consumers updated.
- **Validation**:
  - `go test ./x/tool/calculator/...` passes.
  - `go build ./examples/calculator` passes.
  - `go build ./examples/http-chat` passes.
  - `go build ./examples/single-turn-cli` passes.
- **Details**:
  1. In `x/tool/calculator/handler_integration_test.go`, update `Register` calls from `registry.Register(AddTool.Name, Add)` to `registry.Register(AddTool.Name, AddTool.Description, AddTool.Schema, Add)` (or equivalent using discrete args).
  2. Update `x/tool/calculator/doc.go` examples.
  3. In `examples/calculator/main.go`, replace manual `tools := []provider.Tool{...}` with `tools := registry.Tools()`, and update `Register` calls.
  4. In `examples/http-chat/main.go`, make the same replacements.
  5. In `examples/single-turn-cli/main.go`, update the commented-out example block to show the new API.

### Task 3: Create x/tool/mcp Module Skeleton
- **Goal**: Initialize the `x/tool/mcp` separate module, add the MCP SDK dependency, wire it into the workspace, and create a compilable skeleton.
- **Dependencies**: Task 1.
- **Files Affected**:
  - `go.work`
- **New Files**:
  - `x/tool/mcp/go.mod`
  - `x/tool/mcp/doc.go`
  - `x/tool/mcp/mcp.go` (skeleton)
- **Interfaces**:
  - `mcp.NewClient(opts ...mcp.Option) (*Client, error)` — stub returning a client implementing `tool.RemoteSource`.
  - `mcp.Option` type — functional options marker.
- **Validation**:
  - `go mod tidy` in `x/tool/mcp/` succeeds.
  - `go build ./x/tool/mcp` passes (even if stubs).
  - `go work sync` completes without errors.
- **Details**:
  1. Create `x/tool/mcp/go.mod` with module path `github.com/andrewhowdencom/ore/x/tool/mcp`, `go 1.26.2`, a `replace` directive for the main module, and `require` for `github.com/andrewhowdencom/ore` plus `testify`.
  2. Add `github.com/mark3labs/mcp-go` (or equivalent lightweight, actively maintained MCP SDK) as a dependency.
  3. Add `./x/tool/mcp` to `go.work`.
  4. Create `x/tool/mcp/doc.go` with package-level documentation.
  5. Create `x/tool/mcp/mcp.go` with a `Client` struct, `NewClient` stub, and option type definitions.

### Task 4: Implement MCP Client with Transports and Auth
- **Goal**: Build out the full MCP client supporting stdio and SSE transports, authentication options, initialization handshake, tool discovery, and remote execution.
- **Dependencies**: Task 3.
- **Files Affected**:
  - `x/tool/mcp/mcp.go`
  - `x/tool/mcp/doc.go`
- **New Files**:
  - `x/tool/mcp/client.go` (or equivalent, exact filenames depend on MCP SDK structure)
  - `x/tool/mcp/stdio.go` (or equivalent)
  - `x/tool/mcp/sse.go` (or equivalent)
  - `x/tool/mcp/auth.go` (or equivalent)
  - `x/tool/mcp/client_test.go` (or equivalent test files)
- **Interfaces**:
  - `mcp.WithStdio(command string, args ...string) mcp.Option`
  - `mcp.WithSSE(url string) mcp.Option`
  - `mcp.WithName(name string) mcp.Option`
  - `mcp.WithBearerToken(token string) mcp.Option`
  - `mcp.WithHeader(key, value string) mcp.Option`
  - `Client` implements `tool.RemoteSource`.
- **Validation**:
  - `go test ./x/tool/mcp/...` passes.
  - `go test -race ./x/tool/mcp/...` passes.
  - `go vet ./x/tool/mcp/...` passes.
- **Details**:
  1. Implement `mcp.Client` struct holding transport, name, discovered tools cache.
  2. `NewClient` performs MCP initialization and `tools/list` discovery, caching results.
  3. Implement `Client.Name() string` returning the configured namespace.
  4. Implement `Client.Tools() []provider.Tool` returning cached discovered tools (un-namespaced; `Registry` applies the prefix).
  5. Implement `Client.Call(ctx, name, args)` executing `tools/call` via the active transport.
  6. Implement stdio transport (spawning a subprocess, stdio JSON-RPC communication).
  7. Implement SSE transport (HTTP SSE connection for JSON-RPC).
  8. Implement Bearer token and custom header auth for SSE transport.
  9. Write unit tests using `httptest.Server` for SSE and mocked stdio processes (or transport-level mocks).
  10. Update `x/tool/mcp/doc.go` with usage examples.

### Task 5: Final Integration, Documentation, and Full Test Suite
- **Goal**: Run the complete test suite across all workspace modules, verify no import cycles, update top-level documentation if needed, and ensure the entire repository is healthy.
- **Dependencies**: Task 2, Task 4.
- **Files Affected**:
  - `x/tool/doc.go`
  - `x/tool/mcp/doc.go`
  - `README.md` (if tool sections exist)
- **New Files**: None.
- **Interfaces**: None new.
- **Validation**:
  - `go test -race ./...` passes across all workspace modules.
  - `go build ./...` passes across all workspace modules.
  - `go vet ./...` passes across all workspace modules.
  - No import cycles exist between `x/tool` and `x/tool/mcp` (verified by `go build` and `go test`).
- **Details**:
  1. Run `go test -race ./...` from the workspace root and each module.
  2. Run `go build ./...` to ensure all examples compile.
  3. Verify `x/tool` does not import `x/tool/mcp` anywhere (use `go list -deps` or grep).
  4. Do a final documentation pass on `x/tool/doc.go` and `x/tool/mcp/doc.go` to ensure exported symbols have godoc comments and usage examples are accurate.
  5. If `README.md` contains tool examples, update them.

## Dependency Graph

- Task 1 → Task 2 (Task 2 depends on the new Registry API)
- Task 1 → Task 3 (Task 3 needs `tool.RemoteSource` interface definition)
- Task 3 → Task 4 (Task 4 builds on module skeleton)
- Task 2 || Task 4 (Task 2 and Task 4 are parallelizable once their prerequisites are met)
- Task 2 → Task 5
- Task 4 → Task 5

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| MCP SDK choice (`mark3labs/mcp-go`) has unstable API or missing features | Medium | Low | Evaluate SDK during Task 3 skeleton creation; if unsuitable, spike an alternative before Task 4. The plan allows SDK swap because `tool.RemoteSource` decouples the rest of the framework. |
| Namespaced tool names (`filesystem/read_file`) confuse the LLM | Medium | Medium | Document the naming convention in `x/tool/doc.go` and system prompt guidance. Acceptance testing with real provider calls will validate. This is acknowledged as an open question in the issue. |
| Breaking API changes break external consumers outside this repo | Low | Low | Acceptable per AGENTS.md conventions: "This application has never been run in production... prefer aggressive refactoring." |
| Local tool registered with `/` in name conflicts with namespacing | Low | Low | Document as known edge case in package docs. Optionally add a validation in `Register` rejecting names containing `/`. |
| `x/tool/mcp` tests require complex subprocess or network mocking | Medium | Medium | Use `httptest.Server` for SSE tests and transport-level interface mocks for stdio. Do not spawn real subprocesses in unit tests. |
| Import cycle accidentally introduced between `x/tool` and `x/tool/mcp` | High | Low | Enforced by Go compiler; `go build` and `go test` will catch it immediately. The architecture explicitly keeps `RemoteSource` in `x/tool` so `x/tool/mcp` imports `x/tool`, not vice versa. |
| Race condition in concurrent `Registry.Tools()` and `Register()` calls | Medium | Low | Reuse existing `sync.RWMutex` from current `Registry` in the refactored version. Validate with `go test -race`. |

## Validation Criteria

- [ ] `tool.Registry` has functional-options constructor and captures schema/description at registration time.
- [ ] `tool.Registry.Tools()` returns a merged `[]provider.Tool` including namespaced MCP tools.
- [ ] `tool.Registry.Handler()` correctly routes `ToolCall` to local functions or the appropriate MCP server.
- [ ] `tool.RemoteSource` interface exists in `x/tool` package.
- [ ] `x/tool/mcp` sub-package exists as a separate module with `NewClient`, transport options (stdio, SSE), and auth options.
- [ ] `x/tool/mcp` client implements `tool.RemoteSource`.
- [ ] No import cycles between `x/tool` and `x/tool/mcp`.
- [ ] `examples/calculator` is updated to use the new `Register` signature and `Registry.Tools()`.
- [ ] `examples/http-chat` is updated to use the new `Register` signature and `Registry.Tools()`.
- [ ] All tests pass (`go test -race ./...`) across all workspace modules.
- [ ] Package documentation (`doc.go`) updated for both `x/tool` and `x/tool/mcp`.
