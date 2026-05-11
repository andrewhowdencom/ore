# Plan: Add Tool Calling with BeforeTurn Extension Points

## Objective

Implement a provider-agnostic tool calling system for ore that validates the `loop.Step` extension point pattern. This involves adding a `BeforeTurn` hook interface for state preparation, extending artifacts with tool calling primitives (`ToolCall.ID`, `ToolResult`), defining provider capability interfaces (`provider.ToolProvider`), building a tool registry and handler package, updating the OpenAI adapter for native tool support, and delivering a working calculator example with cross-provider test infrastructure.

## Context

The `core/` and `step/` packages have been merged into `loop/` (completed per `.plans/merge-core-step-into-loop.md`, Issue #11). The `loop.Step` type now exists with:

- `Turn()` — single-turn execution with streaming support
- `WithSurface()` — streaming delta rendering
- `WithHandlers()` — artifact handler registration
- `Handler` interface — per-artifact processing after state append

**Current gaps for tool calling:**
- No `BeforeTurn` extension point for pre-provider state preparation
- `ToolCall` lacks an `ID` field (needed to match results to calls)
- No `ToolResult` artifact type (can't send tool results to LLM)
- No `Usage` artifact type (can't capture token consumption)
- `Provider` interface has no way to receive tool definitions
- OpenAI adapter ignores non-text artifacts, `RoleTool` turns, and tool definitions
- No tool registry or execution mechanism
- ReAct orchestrator already loops while `last.Role != RoleAssistant` — the loop structure for tool calling exists

**Design decisions already converged:**
- Two extension points in `loop.Step`: `BeforeTurn` (state prep) + `Handler` (artifact execution)
- No `AfterTurn` hook — eliminated; no compelling use case that `BeforeTurn` + `Handler` can't cover
- Handler kept as `Handler` (not renamed to `AfterTurn`)
- Tool safety embedded in the tool handler itself (allowlist/refusal)
- Provider-specific tool configuration stays in provider adapters (not generic hooks)

## Architectural Blueprint

### Extension Point Pattern

`loop.Step` gets a `BeforeTurn` interface alongside the existing `Handler`. Both follow the same lifecycle-phase naming convention:

```go
// BeforeTurn transforms state before the provider call.
type BeforeTurn interface {
    BeforeTurn(ctx context.Context, s state.State) (state.State, error)
}

// Handler (existing) — processes artifacts after they're appended to state.
type Handler interface {
    Handle(ctx context.Context, art artifact.Artifact, s state.State) error
}
```

Both are wired via functional options on `Step`:
```go
step := loop.New(
    loop.WithBeforeTurn(systemprompt.Inject("You are a calculator.")),
    loop.WithHandlers(toolHandler),
)
```

The pattern for future extension points:
1. Single-method interface named by lifecycle phase (`BeforeX`, `AfterX`, `HandleX`)
2. `ctx context.Context` always first parameter
3. Transformation or inspection semantics
4. Error propagation aborts the operation
5. No provider access in generic hooks

### Tool Calling Architecture

Tool calling uses three mechanisms:

1. **Provider adapter** — configures tools, serializes them in requests, deserializes `ToolCall` from responses, serializes `RoleTool` turns in state
2. **Artifact Handler** — executes tool calls, appends `RoleTool` turns with `ToolResult`
3. **BeforeTurn** (optional) — injects tool usage instructions into system prompt

The provider adapter owns tool configuration via a capability interface:
```go
// provider.ToolProvider is implemented by adapters that support tool calling.
type ToolProvider interface {
    Provider
    SetTools(tools []Tool) error
}
```

The application configures the provider adapter directly:
```go
prov := openai.New(apiKey, model, openai.WithTools(tools...))
step := loop.New(loop.WithSurface(s), loop.WithHandlers(toolHandler))
```

### Package Structure

```
artifact/       ← add ToolCall.ID, ToolResult, Usage
provider/       ← add Tool, ToolProvider
provider/openai/← add tool request/response serialization, RoleTool handling
loop/           ← add BeforeTurn interface, WithBeforeTurn option
tool/           ← NEW: Registry, Handler (artifact handler for tool execution)
examples/calculator/ ← NEW: calculator example with add, multiply
```

## Requirements

1. [explicit] Add `BeforeTurn` extension point to `loop.Step` with `WithBeforeTurn` functional option
2. [explicit] Add `ID string` field to `artifact.ToolCall` and `artifact.ToolCallDelta`
3. [explicit] Add `ToolResult` artifact type with `ToolCallID`, `Content`, and `IsError` fields
4. [explicit] Add `Usage` artifact type for token consumption tracking
5. [explicit] Add `provider.Tool` struct and `provider.ToolProvider` capability interface
6. [explicit] Create `tool/` package with `Registry` (maps names to functions) and `Handler` (implements `loop.Handler`)
7. [explicit] Update OpenAI adapter to support tool calling (request serialization, response deserialization, `RoleTool` state handling)
8. [explicit] Add `Usage` artifact generation to OpenAI adapter
9. [explicit] Create `examples/calculator/` with add and multiply tools
10. [inferred] Cross-provider test infrastructure: mock provider with `ToolProvider` for framework-level tool handler tests
11. [inferred] Update `examples/single-turn-cli/` and `examples/tui-chat/` to demonstrate tool calling
12. [inferred] Update `README.md` to document the new extension points and tool calling

## Task Breakdown

### Task 1: Add BeforeTurn Extension Point to loop Package
- **Goal**: Add `BeforeTurn` interface and `WithBeforeTurn` functional option to `loop.Step`, and execute before-turn hooks in `Turn()`.
- **Dependencies**: None
- **Files Affected**: `loop/loop.go`
- **New Files**: None
- **Interfaces**:
  ```go
  type BeforeTurn interface {
      BeforeTurn(ctx context.Context, s state.State) (state.State, error)
  }
  ```
  Add to `Step` struct: `beforeTurns []BeforeTurn`. Add option `WithBeforeTurn(beforeTurns ...BeforeTurn) Option`.
- **Details**: In `Turn()`, after creating `deltasCh` but before the provider call, iterate `s.beforeTurns` and transform state sequentially. On error, return the error. Add tests in `loop/loop_test.go` for:
  - BeforeTurn transforms state
  - BeforeTurn error aborts the turn
  - Multiple BeforeTurn hooks compose in order

### Task 2: Extend Artifact Types for Tool Calling
- **Goal**: Add `ID` to `ToolCall`/`ToolCallDelta`, create `ToolResult` and `Usage` artifacts.
- **Dependencies**: None
- **Files Affected**: `artifact/artifact.go`
- **New Files**: None
- **Interfaces**:
  ```go
  type ToolCall struct {
      ID        string
      Name      string
      Arguments string
  }

  type ToolCallDelta struct {
      ID        string
      Name      string
      Arguments string
  }

  type ToolResult struct {
      ToolCallID string
      Content    string
      IsError    bool
  }

  type Usage struct {
      PromptTokens     int
      CompletionTokens int
      TotalTokens      int
  }
  ```
- **Details**: Add `ID` field to existing `ToolCall` and `ToolCallDelta`. Add `ToolResult` and `Usage` types with `Kind()` methods returning `"tool_result"` and `"usage"` respectively. Keep backward compatibility — existing `ToolCall{Name, Arguments}` zero-value usage still works.

### Task 3: Add Provider Tool Capability Interfaces
- **Goal**: Define `provider.Tool` struct and `provider.ToolProvider` interface for provider adapters.
- **Dependencies**: Task 2
- **Files Affected**: `provider/provider.go`
- **New Files**: None
- **Interfaces**:
  ```go
  type Tool struct {
      Name        string
      Description string
      // Schema defines the JSON Schema for the tool's parameters.
      Schema map[string]any
  }

  // ToolProvider is implemented by adapters that support tool calling.
  type ToolProvider interface {
      Provider
      SetTools(tools []Tool) error
  }
  ```
- **Details**: The `Tool` struct is provider-agnostic — every adapter maps it to its native API. `SetTools` configures the provider for the next invocation. This is a capability interface, not an extension point on `loop.Step`.

### Task 4: Create tool Registry and Handler Package
- **Goal**: Build the `tool/` package with a function registry and an artifact handler that executes tool calls.
- **Dependencies**: Task 2, Task 3
- **Files Affected**: None
- **New Files**:
  - `tool/registry.go` — `Registry` type, `Register(name string, fn ToolFunc)` method
  - `tool/handler.go` — `Handler` type implementing `loop.Handler`
  - `tool/tool.go` — shared types (`ToolFunc`)
  - `tool/registry_test.go` — tests
  - `tool/handler_test.go` — tests
- **Interfaces**:
  ```go
  type ToolFunc func(ctx context.Context, args map[string]any) (any, error)

  type Registry struct {
      tools map[string]ToolFunc
  }

  func (r *Registry) Register(name string, fn ToolFunc)
  func (r *Registry) Handler() *Handler

  type Handler struct {
      registry *Registry
  }

  func (h *Handler) Handle(ctx context.Context, art artifact.Artifact, s state.State) error
  ```
- **Details**: `Registry` maps tool names to Go functions. `Handler` implements `loop.Handler`: on `ToolCall` artifact, looks up the tool by name in the registry, parses JSON arguments into `map[string]any`, executes the function, and appends a `RoleTool` turn with a `ToolResult` artifact. If the tool is not registered, appends an error `ToolResult` (`IsError: true`). Safety: the handler embeds its own allowlist — unknown tools are refused.

### Task 5: Update OpenAI Adapter for Tool Support
- **Goal**: Add tool request/response serialization, `RoleTool` handling, and `Usage` artifact to the OpenAI adapter.
- **Dependencies**: Task 2, Task 3
- **Files Affected**: `provider/openai/openai.go`
- **New Files**: None
- **Interfaces**:
  - Add `WithTools(tools ...provider.Tool) Option` to OpenAI provider
  - Implement `provider.ToolProvider` on `*Provider`
- **Details**:
  1. Add `tools []openai.ChatCompletionToolParamUnion` field to `Provider` (or internal config)
  2. `WithTools()` option stores tools in config; `SetTools()` updates the provider's tool list
  3. In `Invoke()` and `InvokeStreaming()`, if tools are configured, include them in the request via `openai.ChatCompletionNewParams{Tools: ...}`
  4. Deserialize `ToolCall` from response choices: `msg.ToolCalls` → `artifact.ToolCall{ID, Name, Arguments}`
  5. In `serializeMessages()`, handle `RoleTool` turns: map to `openai.ToolMessage` with `ToolCallID` from `ToolResult.ToolCallID`
  6. Capture `resp.Usage` in `Invoke()` and final chunk usage in `InvokeStreaming()`, include `artifact.Usage` in returned artifacts
  7. For streaming, tool calls may arrive as deltas. Handle `delta.ToolCalls` in `InvokeStreaming()` — this may require accumulating partial tool call deltas.

### Task 6: Add Cross-Provider Tool Tests
- **Goal**: Create a mock `ToolProvider` for framework-level tool handler validation.
- **Dependencies**: Task 3, Task 4
- **Files Affected**: `loop/loop_test.go`, `tool/handler_test.go`
- **New Files**: None (mock in test files)
- **Details**: In `loop/loop_test.go`, extend `mockProvider` and `mockStreamingProvider` to optionally support `provider.ToolProvider`. Add tests that:
  - Verify `BeforeTurn` + `Handler` + tool calling compose correctly end-to-end
  - Verify tool execution flows through the full turn → handler → state append cycle
  - Verify `Usage` artifact is processed by a metrics handler
  - In `tool/handler_test.go`, test registry registration, handler execution, error handling, and unknown tool refusal.

### Task 7: Create Calculator Example
- **Goal**: Build `examples/calculator/` demonstrating tool calling with `add` and `multiply`.
- **Dependencies**: Task 4, Task 5
- **Files Affected**: None
- **New Files**:
  - `examples/calculator/main.go`
- **Details**: A command-line calculator that:
  1. Creates a `tool.Registry` with `add` and `multiply` functions
  2. Creates a `tool.Handler` from the registry
  3. Configures the OpenAI provider with `openai.WithTools(...)` (using `provider.Tool` definitions)
  4. Creates a `loop.Step` with the tool handler
  5. Runs a single turn or a simple ReAct loop (if the response contains tool calls)
  6. Reads user input like "What is 5 plus 3 times 2?" and prints the result
  The example validates that the extension points work end-to-end.

### Task 8: Update Existing Examples for Tool Calling
- **Goal**: Update `examples/single-turn-cli/` and `examples/tui-chat/` to optionally demonstrate tool calling.
- **Dependencies**: Task 4, Task 5
- **Files Affected**: `examples/single-turn-cli/main.go`, `examples/tui-chat/main.go`
- **New Files**: None
- **Details**: For `single-turn-cli`, add an optional calculator tool setup behind an environment flag or commented code that demonstrates how to wire tools. For `tui-chat`, similarly add commented or flagged tool setup. Keep the examples working without tools as the default path.

### Task 9: Update README Documentation
- **Goal**: Document the new extension points, tool calling, and calculator example.
- **Dependencies**: Task 1–Task 8
- **Files Affected**: `README.md`
- **New Files**: None
- **Details**:
  - Update "Extension Points" section to describe `BeforeTurn` and `Handler` as the two primary hooks
  - Update "Artifact Handlers" section to include `Tool Call Handler` as a concrete example
  - Add "Tool Calling" subsection explaining the `tool/` package, registry, and handler pattern
  - Update "Project Status" to list `tool/` package and `examples/calculator/`
  - Update architecture description to remove `core/` and `step/` references (already done in loop merge, but verify)

## Dependency Graph

```
Task 1 (BeforeTurn) || Task 2 (Artifacts) || Task 3 (Provider Tool Interfaces)
  ↓
Task 4 (tool Registry/Handler)  ← depends on Task 2 + Task 3
  ↓
Task 5 (OpenAI Adapter) ← depends on Task 2 + Task 3
  ↓
Task 6 (Cross-Provider Tests) ← depends on Task 3 + Task 4
  ↓
Task 7 (Calculator Example) ← depends on Task 4 + Task 5
  ↓
Task 8 (Update Examples) ← depends on Task 4 + Task 5
  ↓
Task 9 (README) ← depends on all
```

- Task 1, Task 2, and Task 3 are parallelizable
- Task 4 and Task 5 are parallelizable (both depend on Task 2+3)
- Task 6 depends on Task 3+4
- Task 7 depends on Task 4+5
- Task 8 depends on Task 4+5
- Task 9 is sequential after all

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| OpenAI SDK streaming tool call deltas are complex | High | High | Spike: prototype tool call delta accumulation before full implementation. OpenAI streaming tool calls may arrive fragmented across multiple chunks. |
| `RoleTool` serialization differs across providers | Medium | Medium | Design `provider.Tool` to be provider-agnostic; each adapter maps to native format. Test with mock providers. |
| `BeforeTurn` ordering confusion | Low | Medium | Document clearly: BeforeTurn hooks run in registration order, sequentially. Error in any hook aborts the turn. |
| Tool call ID mismatch between response and result | High | Low | `ToolCall.ID` is required. Provider adapter must preserve IDs. Handler must echo ID in `ToolResult.ToolCallID`. Tests verify round-trip. |
| Breaking change to `ToolCall` struct | Medium | Low | Add `ID` as new field. Existing zero-value usage (`ToolCall{Name, Arguments}`) continues to work. |

## Validation Criteria

- [ ] `go test -race ./...` passes with no failures
- [ ] `go build ./...` passes with no errors
- [ ] `BeforeTurn` interface exists in `loop/` with tests verifying state transformation and error propagation
- [ ] `ToolCall` has `ID` field and `ToolResult`/`Usage` artifacts exist in `artifact/`
- [ ] `provider.Tool` and `provider.ToolProvider` exist in `provider/`
- [ ] `tool/` package exists with `Registry`, `Handler`, and comprehensive tests
- [ ] OpenAI adapter implements `ToolProvider`, serializes tools in requests, deserializes `ToolCall`, handles `RoleTool`, and emits `Usage`
- [ ] Mock `ToolProvider` exists in tests for framework-level validation
- [ ] `examples/calculator/` builds and runs successfully with real or mock OpenAI API
- [ ] `examples/single-turn-cli/` and `examples/tui-chat/` continue to work without tools (backward compatibility)
- [ ] README documents extension points, tool calling, and the calculator example
