# Plan: Add Tool Output Byte Budget

## Objective

Add a hard, documented, non-overridable byte-budget to the ore tool framework, enforced at the framework level. When a tool's serialized output exceeds its budget, the framework truncates the result and appends a deterministic signal containing the total bytes and static per-tool usage examples. This prevents unbounded token consumption, especially from tools like `search_files` that recursively search directories and can return arbitrarily large JSON arrays.

## Context

The ore framework defines tool contracts in the `tool/` package and implements concrete tools in `x/tool/` extensions. Tool execution is orchestrated by the `Handler` in `x/tool/handler.go`, which is the single chokepoint where all tool results are serialized to JSON and emitted as `artifact.ToolResult`.

**Relevant files discovered:**

- `tool/tool.go` — defines `Tool` struct (Name, Description, Schema, DisplayHint) and `ToolFunc` signature.
- `tool/registry.go` — defines `Registry` interface and `Lookup(name string) (ToolFunc, bool)`.
- `tool/registry_test.go` — tests the registry, including `Lookup` usage.
- `x/tool/handler.go` — the single place where all tool results are serialized (`json.Marshal`) and emitted.
- `x/tool/handler_test.go` — tests for the handler.
- `x/tool/filesystem/filesystem.go` — `ReadFile` (already has internal `maxChars=100_000` guardrail), `SearchFiles` (recursive, unbounded `[]SearchResult` output), `ListDirectory`, `WriteFile`, `EditFile`.
- `x/tool/filesystem/filesystem_test.go` — tests for filesystem tools.
- `x/tool/bash/bash.go` — `Bash` tool returns `*Result` with stdout/stderr, potentially large.
- `x/tool/skills/tool.go` — `ReadSkill` tool returns full SKILL.md content, potentially large.
- `examples/filesystem/main.go` — registers all filesystem tools via `registry.Register(tool, fn)`.
- `tool/doc.go` and `x/tool/doc.go` — package documentation.

**Key observations:**
- `search_files` is recursive by default and has no output limit. A broad regex on a large codebase can produce a JSON array of thousands of `SearchResult` objects.
- `read_file` has an internal `maxChars` guardrail but it is ad-hoc, not framework-level.
- The `Handler` serializes all results via `json.Marshal(result)` and stores them in `ToolResult.Content`. This is the ideal place to enforce a universal budget.
- `Registry.Lookup` currently returns only the `ToolFunc`, not the `Tool` descriptor, so the `Handler` cannot access per-tool metadata like a budget.
- The project convention (AGENTS.md) explicitly prefers aggressive refactoring: renaming, moving, breaking internal APIs is encouraged at this stage.

## Architectural Blueprint

**Selected approach: Two-layer budget enforcement with framework-level safety net.**

1. **Budget fields on `Tool` descriptor**: Add `MaxBytes int` and `TruncationHint string` to `tool.Tool`. `MaxBytes` is the hard ceiling in bytes. `TruncationHint` is a template string (e.g. `"...truncated (total ${N} bytes). Make it more efficient by using the tool like: search_files(path=\"src\", query=\"func ReadFile\")"`) that the framework formats with the actual total bytes.

2. **Registry interface change**: Change `Registry.Lookup` to return both the `Tool` descriptor and the `ToolFunc` (or add a new method). This allows the `Handler` to know each tool's budget at invocation time. This is a breaking but low-impact change: only `x/tool/handler.go` and `tool/registry_test.go` call `Lookup`.

3. **Framework safety net in `Handler`**: After `json.Marshal(result)`, the `Handler` checks `len(content)` against `t.MaxBytes`. If exceeded, it truncates the `Content` string to a valid UTF-8 boundary, appends the formatted `TruncationHint`, and emits the truncated `ToolResult`. This covers both local and remote tools. The budget is applied to the serialized JSON string form — the actual bytes the LLM sees. This is the universal, unavoidable safety net.

4. **Cooperative truncation in tools**: Tools that produce large structured output (especially `search_files`) should respect `MaxBytes` during their own execution to avoid wasted work and invalid JSON. The framework provides helper utilities in `tool/` (e.g., `tool.FormatTruncated(result any, maxBytes int, hint string) (string, bool)`) so tool authors can implement this easily. For `search_files`, this means stopping the `WalkDir` early when accumulated match bytes would exceed the budget. For `read_file`, the internal `maxChars` can be replaced or aligned with the framework budget.

5. **Tool description updates**: Each tool's `Description` is updated to mention the byte limit (e.g., `"Returns at most 50000 bytes."`). This makes the LLM aware of the constraint without giving it control over it.

**Evaluated alternatives:**

- *Per-tool wrapper at registration*: Wrapping `ToolFunc` at `registry.Register` would only cover local tools. Remote tools (`RemoteSource.Call`) bypass the registry entirely. Rejected because the `Handler` is the only universal chokepoint.
- *LLM-configurable `max_bytes` parameter*: The user explicitly rejected this during deliberation. The budget is a hard safety rail, not a dial.
- *Pre-serialization truncation only*: Relying solely on tool cooperation would leave gaps (e.g., remote tools, tools that forget to implement it). Rejected. The framework safety net is mandatory; cooperative truncation is an optimization.

## Requirements

1. `tool.Tool` must expose a `MaxBytes` field (0 = no limit, positive = hard ceiling) and a `TruncationHint` template string field.
2. The `Registry` interface must allow the `Handler` to retrieve the `Tool` descriptor alongside its `ToolFunc` at lookup time.
3. The `Handler` must enforce the budget after JSON serialization, truncating the `Content` string and appending the formatted hint at the end.
4. All content-returning tools in `x/tool/` must have `MaxBytes` and `TruncationHint` set: `read_file`, `search_files`, `list_directory`, `bash`, `read_skill`.
5. Tool descriptions must mention the byte limit so the LLM is aware of it.
6. `search_files` must stop collecting matches during execution when it approaches the budget (cooperative truncation), avoiding wasted work on massive recursive searches.
7. The framework must provide a helper or utility for tools that want to measure their own output against the budget before returning.
8. Truncation must be UTF-8 safe: never split a multi-byte rune.
9. The mechanism must work for both local tools and remote tools (MCP sources) that pass through the `Handler`.

## Task Breakdown

### Task 1: Extend Tool Descriptor and Registry Interface
- **Goal**: Add `MaxBytes` and `TruncationHint` to `tool.Tool`, and change `Registry.Lookup` to return the `Tool` descriptor alongside the `ToolFunc`.
- **Dependencies**: None.
- **Files Affected**: `tool/tool.go`, `tool/registry.go`, `tool/registry_test.go`, `x/tool/handler.go`, `x/tool/handler_test.go`, `x/tool/skills/tool.go` (indirectly, may need to adapt).
- **New Files**: None.
- **Interfaces**:
  - `Tool` struct gains `MaxBytes int` and `TruncationHint string`.
  - `Registry.Lookup(name string) (Tool, ToolFunc, bool)` — returns the descriptor, the function, and the found bool. (Or add `LookupTool(name string) (Tool, bool)` if the implementer prefers a non-breaking additive change; the planner recommends the breaking change for clarity.)
- **Validation**: `go test ./tool/...` passes. `go build ./x/tool/...` passes (Handler updated to use new signature).
- **Details**: Update `localTool` in `registry.go` to already hold both `tool` and `fn`; the change is mostly surfacing it. Update the single non-test caller in `x/tool/handler.go` to accept the new return value. Update `registry_test.go` to destructure the new return. Update `x/tool/handler_test.go` mocks if any implement the `Registry` interface.

### Task 2: Implement Framework Safety Net in Handler
- **Goal**: After serializing a tool result to JSON, enforce `t.MaxBytes` by truncating `Content` and appending the formatted `TruncationHint`.
- **Dependencies**: Task 1.
- **Files Affected**: `x/tool/handler.go`, `x/tool/handler_test.go`.
- **New Files**: None.
- **Interfaces**:
  - `Handler` internal serialization path now checks `len(content) > t.MaxBytes` before emitting.
  - If exceeded, `Content` is truncated to `t.MaxBytes - len(hint)` bytes at a valid UTF-8 rune boundary, then the formatted hint is appended.
  - For remote tools, the Handler must look up the `Tool` descriptor from the remote source's `Tools()` list to find the budget.
- **Validation**: `go test ./x/tool/...` passes. New tests verify: (a) tool within budget returns unchanged, (b) tool exceeding budget is truncated with hint, (c) hint includes actual total bytes, (d) UTF-8 safety is preserved, (e) remote tool budgets are respected.
- **Details**: The Handler has two code paths (local and remote). Both must enforce the budget. For local tools, the `Tool` descriptor is available from `Lookup`. For remote tools, the Handler already iterates `rs.Tools()` to build the namespace prefix; it can build a lookup map by name to find the budget. If `MaxBytes` is 0, no budget is enforced (backward-compatible default). The truncation must happen on the `Content` string (the JSON representation), not the `Value` field, because `Content` is what the LLM sees.

### Task 3: Apply Budgets and Cooperative Truncation to Filesystem Tools
- **Goal**: Set `MaxBytes` and `TruncationHint` on all filesystem tools, update descriptions, and make `search_files` budget-aware during execution.
- **Dependencies**: Task 1.
- **Files Affected**: `x/tool/filesystem/filesystem.go`, `x/tool/filesystem/filesystem_test.go`.
- **New Files**: None.
- **Interfaces**:
  - `ReadFileTool`: `MaxBytes: 100000`, `TruncationHint: "...truncated (total ${N} bytes). Make it more efficient by using the tool like: read_file(path=\"file.go\", offset=1, limit=100)"`. Description updated to mention the limit.
  - `SearchFilesTool`: `MaxBytes: 50000`, `TruncationHint: "...truncated (total ${N} bytes). Make it more efficient by using the tool like: search_files(path=\"src\", query=\"func ReadFile\") or list_directory(path=\"src\")"`. Description updated. `search_files` stops accumulating `SearchResult` entries when the estimated serialized size would exceed the budget.
  - `ListDirectoryTool`: `MaxBytes: 10000`, `TruncationHint: "...truncated (total ${N} bytes). Make it more efficient by using the tool like: list_directory(path=\"src\")"`.
  - `WriteFileTool` and `EditFileTool`: return small status strings, so `MaxBytes: 0` (no limit needed) or a small limit.
  - `read_file`: internal `maxChars` can be removed or aligned with `MaxBytes` (the framework safety net now covers it). If removed, `read_file` returns full content and trusts the framework; if kept, it acts as an early optimization.
- **Validation**: `go test ./x/tool/filesystem/...` passes. New tests verify `search_files` stops early when budget is exceeded, and that truncated results include the hint.
- **Details**: For `search_files`, the cooperative truncation means tracking the accumulated JSON bytes during `WalkDir`. When adding a new `SearchResult`, estimate the serialized size (e.g., `len(path) + len(content) + 50` overhead). If the running total would exceed `MaxBytes`, stop walking and return the partial results with the truncation flag. The `Handler` safety net will still catch any edge case where the estimate is wrong.

### Task 4: Apply Budgets to Bash and Skills Tools
- **Goal**: Set `MaxBytes` and `TruncationHint` on `BashTool` and `ReadSkillTool`, and update descriptions.
- **Dependencies**: Task 1.
- **Files Affected**: `x/tool/bash/bash.go`, `x/tool/skills/tool.go`, `x/tool/skills/tool_test.go` (if any), `x/tool/bash/bash_test.go` (if any).
- **New Files**: None.
- **Interfaces**:
  - `BashTool`: `MaxBytes: 50000`, `TruncationHint: "...truncated (total ${N} bytes). Make it more efficient by using the tool like: bash(command=\"grep -n 'pattern' *.go | head -20\")"`. Description updated.
  - `ReadSkillTool`: `MaxBytes: 30000`, `TruncationHint: "...truncated (total ${N} bytes). Make it more efficient by using the tool like: read_skill(name=\"go\")"`. Description updated.
- **Validation**: `go build ./x/tool/bash/...` and `go build ./x/tool/skills/...` pass. `go test ./x/tool/...` passes.
- **Details**: These tools currently return their full output and rely on the framework safety net. No cooperative truncation is required for these unless the implementer wants to optimize (e.g., truncating bash stdout before returning). The framework safety net from Task 2 is sufficient for the initial rollout.

### Task 5: Update Documentation and Examples
- **Goal**: Document the budget mechanism in package docs and ensure example applications compile cleanly.
- **Dependencies**: Task 2, Task 3, Task 4.
- **Files Affected**: `tool/doc.go`, `x/tool/doc.go`, `examples/filesystem/main.go`, `examples/verifier-chat/main.go` (if it uses filesystem tools).
- **New Files**: None.
- **Interfaces**: None.
- **Validation**: `go build ./examples/...` passes. `go test ./...` passes.
- **Details**: Update `tool/doc.go` to explain that `Tool.MaxBytes` and `Tool.TruncationHint` define a hard output budget enforced by the framework. Update `x/tool/doc.go` to describe how the `Handler` applies the budget after serialization. In `examples/filesystem/main.go`, no code changes are likely needed because the `Tool` descriptors are already passed to `Register`, but verify the example compiles with the new fields. Also verify `examples/verifier-chat/main.go` if it registers tools.

## Dependency Graph

- Task 1 → Task 2 (Task 2 depends on the new Tool fields and Lookup signature)
- Task 1 → Task 3 (Task 3 depends on MaxBytes/TruncationHint existing on Tool)
- Task 1 → Task 4 (Task 4 depends on MaxBytes/TruncationHint existing on Tool)
- Task 2 || Task 3 || Task 4 (Task 2, 3, and 4 are parallelizable after Task 1)
- Task 2 → Task 5 (Task 5 documents the handler enforcement mechanism)
- Task 3 → Task 5 (Task 5 documents the filesystem tool budgets)
- Task 4 → Task 5 (Task 5 documents the bash/skills tool budgets)

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Changing `Registry.Lookup` signature breaks unknown callers in other worktrees | Medium | Low | Only 2 callers exist in the current branch (handler and test). The plan includes updating both. If other branches have additional callers, they will need to adapt during merge. |
| Truncating JSON mid-structure produces invalid JSON that confuses the LLM | Medium | Medium | Mitigated by two-layer approach: cooperative truncation in `search_files` keeps JSON valid for the primary blow-up case. The framework safety net is a fallback; the plan requires UTF-8 safety but does not guarantee JSON validity after truncation. This is acceptable for a safety net. |
| `search_files` byte estimation during cooperative truncation is inaccurate | Low | Medium | The estimate can be conservative (over-estimate). The framework safety net catches any overflow. Tests should verify both paths. |
| Remote tools (MCP) have no `MaxBytes` field, causing inconsistent behavior | Medium | Low | The `Handler` looks up the `Tool` descriptor from the remote source's `Tools()` list. If the remote source does not populate `MaxBytes`, it defaults to 0 (no budget), which is backward-compatible. Future work can add MCP negotiation. |
| LLM misinterprets truncation as a tool error and retries with same parameters | Medium | Medium | The truncation message explicitly says "truncated" and gives a usage example. It is not marked as an error (`IsError: false`). The description also warns the LLM about the limit. |

## Validation Criteria

- [ ] `go test ./tool/...` passes with the new Lookup signature.
- [ ] `go test ./x/tool/...` passes, including new handler budget-enforcement tests.
- [ ] `go test ./x/tool/filesystem/...` passes, including new `search_files` early-stopping tests.
- [ ] `go build ./examples/filesystem` and `go build ./examples/verifier-chat` compile successfully.
- [ ] `go test -race ./...` passes across the entire repository.
- [ ] The `ReadFileTool` descriptor includes `MaxBytes > 0` and `TruncationHint != ""`.
- [ ] The `SearchFilesTool` descriptor includes `MaxBytes > 0` and `TruncationHint != ""`, and `search_files` stops accumulating matches before the budget is exceeded.
- [ ] A handler test demonstrates that a tool result exceeding `MaxBytes` is truncated and the `Content` string ends with the formatted hint containing the actual total bytes.
- [ ] All tool descriptions mention the byte limit in plain language.
