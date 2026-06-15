# Plan: Decouple ToolCall.Display from Wire Format

## Objective

Rename `artifact.ToolCall.Value` to `artifact.ToolCall.Display` to make the field's purpose explicit (human display only), and remove the Anthropic provider's erroneous use of that field as a wire-format override. The fix structurally prevents the bug where a non-dict display value (set by `enrichToolCalls` from string-returning `DisplayHint`s) was being sent as the `tool_use.input` field, which Anthropic rejected with `Input should be a valid dictionary (2013)`.

## Context

### Root cause

`artifact.ToolCall` carries two fields that look like a single value:

```go
type ToolCall struct {
    ID        string
    Name      string
    Arguments string   // JSON the model streamed — the wire format
    Value     any      // display value set by enrichToolCalls
}
```

`Value` is populated by `enrichToolCalls` (`loop/loop.go:543`) from the tool's `DisplayHint` (`tool.Tool.DisplayHint`). For eight of nine built-in tools (`read_file`, `write_file`, `edit_file`, `list_directory`, `search_files`, `add`, `multiply`, `read_skill`), `DisplayHint` returns a `string` (`fmt.Sprintf("📁 list_directory(%s)", path)`). The bash tool is the only one that returns a struct.

The Anthropic provider's `parseToolArguments` (`x/provider/anthropic/anthropic.go:802`) consults `Value` first:

```go
if tc.Value != nil {
    return tc.Value
}
```

For non-bash tools, this returns the display string. The SDK then serializes it as the `input` field of a `tool_use` block. The wire shape becomes `"input": "📁 list_directory(/home/...)"` — a string where Anthropic requires a JSON object. The API rejects with `Input should be a valid dictionary (2013)`.

### Why the OpenAI provider is unaffected

The OpenAI provider (`x/provider/openai/openai.go:340`) uses `tc.Arguments` directly and never references `tc.Value`:

```go
Function: openai.ChatCompletionMessageToolCallFunctionParam{
    Name:      tc.Name,
    Arguments: tc.Arguments,
},
```

`Arguments` is the JSON the model streamed — always a valid JSON object in normal flow. OpenAI is correct. The Anthropic provider is the asymmetric one.

### What `Value` is actually for

It's a display-only field. Consumers:

- `MarkdownString()` (artifact) — renders for human display
- `LLMString()` (artifact) — returns the display value's JSON as a wire-size estimate (used by `x/llmbytes`)
- The TUI's `compactToolCall` actually reads `Arguments`, not `Value`

It's never used by the wire-format code path in any working provider (OpenAI uses `Arguments`; the bug is that Anthropic uses `Value`).

### Field naming precedent

`ToolCall.MarshalJSON` already uses `"display"` as the JSON field name (`artifact/artifact.go:101`). The Go field is `Value` but the JSON key is `display` — the divergence is itself a smell. Renaming the Go field to `Display` makes the JSON key match.

### Conventions to honor

From `AGENTS.md`:
- **Aggressive refactoring** preferred at this stage. Rename, move, delete indirection.
- **Cycle-free dependency graph**: this refactor is internal to the artifact package and the providers, no new dependencies.
- **Test conventions**: table-driven tests; `-race ./...` always.
- **Doc comments** on all exported identifiers explaining intent.

## Architectural Blueprint

Rename `ToolCall.Value` → `ToolCall.Display` in the artifact package. The field remains `any` and remains optional; only the name changes. `MarkdownString` and `LLMString` read from the renamed field. The Anthropic provider's `parseToolArguments` is simplified to consult only `Arguments` (matching OpenAI). `enrichToolCalls` is renamed to `applyDisplayHints` to make its single purpose explicit. Tool package `DisplayHint` doc comments are updated to clarify "for human display only; no effect on wire format."

The OpenAI provider, TUI, exporters, and `x/llmbytes` need no code changes — they read through `MarkdownString` / `LLMString` / `Arguments`, which all keep the same shape. The bug becomes structurally impossible: there is no longer a code path that consults the display field for wire format.

```
ToolCall {
    ID        string
    Name      string
    Arguments string  // JSON the model streamed; source of truth for wire format
    Display   any     // Optional typed value for human display
}
```

## Requirements

1. **No display value may reach the wire-format code path** for tool calls. `parseToolArguments` (and any future provider's equivalent) must derive the wire `input`/`arguments` from `Arguments` alone. `[inferred — the bug's fix]`
2. **The display contract stays polymorphic.** `Display` remains `any`; the contract is the `MarkdownRenderer` interface. Tool authors can return strings (the common case) or structs that implement `MarkdownRenderer` (the elaborate case) without wrapping boilerplate. `[inferred — current 8/9 tools return strings]`
3. **Existing test coverage continues to pass** with the field renamed. `[explicit]`
4. **A new regression test** proves that a `ToolCall` with a non-dict `Display` and a valid `Arguments` round-trips through serialization with a dict-shaped `input`. `[inferred — the bug needs a canary]`
5. **The `ToolCall.MarshalJSON` wire shape is preserved** — the JSON field name remains `"display"`. `[inferred — consumers may depend on the JSON shape]`
6. **The `x/llmbytes` byte-counting semantics become more accurate.** `ToolCall.LLMString()` now returns the wire-format size (from `Arguments`) rather than the display-value's JSON size. This is a behavior change, but the new value is what the LLM actually sees. `[inferred — the doc comment for `x/llmbytes` already says "the worst-case size the LLM ever sees"]`

## Task Breakdown

### Task 1: Rename `ToolCall.Value` to `ToolCall.Display` and update rendering methods in `artifact`

- **Goal**: Rename the field and update its three consumers (`LLMString`, `MarkdownString`, `MarshalJSON`) to read from the new name; improve `MarkdownString` to handle strings as-is instead of `json.Marshal`-ing them.
- **Dependencies**: None.
- **Files Affected**:
  - `artifact/artifact.go`
  - `artifact/artifact_test.go`
- **New Files**: None.
- **Interfaces**:
  - `type ToolCall struct { ..., Display any }` (renamed from `Value`)
  - `func (t ToolCall) LLMString() string` — returns `t.Arguments` directly. The previous behavior (consult `Display` then `json.Marshal`) was an estimate of the LLM-visible size that frequently diverged from reality. The wire format is what the LLM sees, and it's `Arguments`.
  - `func (t ToolCall) MarkdownString() string` — updated to: (1) check `MarkdownRenderer` on `Display`; (2) check `string` on `Display` and return as-is; (3) fall back to `json.Marshal(Display)`; (4) fall back to `Arguments` when `Display` is nil. This matches the existing `ToolResult.MarkdownString` pattern and fixes a wart where strings were returned with embedded quotes.
  - `func (t ToolCall) MarshalJSON() ([]byte, error)` — read from `Display` instead of `Value` for the `display` JSON field. The wire shape (`"display"` key) is unchanged.
  - `func (d ToolCallDelta) MergeInto(acc Artifact) Artifact` — the seeded `ToolCall` literal references `Value: nil` → `Display: nil`.
- **Validation**:
  - `go test ./artifact/... -race` passes
  - `go build ./...` passes (this catches downstream compile errors before the next task)
  - Existing `TestToolCall_LLMString` and `TestToolCall_MarkdownString` test cases are updated to the new contracts; new edge case for `string` Display value is added to `TestToolCall_MarkdownString` (expected: string rendered as-is, no JSON quoting)
  - `TestToolCall_ValueField` is renamed to `TestToolCall_DisplayField` and the field reference is updated
  - `TestAccumulable_MergeInto_EdgeCases` and `TestToolCall_MarshalJSON_WithDisplay` are updated to use `Display`
- **Details**: After this task the artifact package compiles, its tests pass, and downstream packages have compile errors that the next tasks resolve. The shape of `ToolCall` is final; no further structural changes are planned.

### Task 2: Rename `enrichToolCalls` to `applyDisplayHints` and set `Display` in `loop`

- **Goal**: Make the function name reflect its single purpose (applying display hints) and have it write to the renamed `Display` field.
- **Dependencies**: Task 1 (the field rename).
- **Files Affected**:
  - `loop/loop.go` — rename `enrichToolCalls` → `applyDisplayHints`; update doc comment from "Attaches the result to `ToolCall.Value`" to "Attaches the result to `ToolCall.Display`"; set `tc.Display = v` instead of `tc.Value = v`
  - `loop/pipeline.go` — update the call site
  - `loop/loop_test.go` — update `TestEnrichToolCalls_*` test names to `TestApplyDisplayHints_*` and field references
  - `loop/pipeline_test.go` — update `tc.Value` references in the two test cases that assert on the enriched ToolCall
- **New Files**: None.
- **Interfaces**:
  - `func applyDisplayHints(ctx context.Context, artifacts []artifact.Artifact, opts []provider.InvokeOption)` — same signature, sets `Display` instead of `Value`
- **Validation**:
  - `go test ./loop/... -race` passes
  - `go build ./...` passes
- **Details**: The function is package-private; the rename is internal. No public API surface change. The behavior is identical: take a `DisplayHint`, run it on parsed `Arguments`, attach the result to the `Display` field. Only the field name and function name change.

### Task 3: Simplify `parseToolArguments` in the Anthropic provider and add a regression test

- **Goal**: Drop the `Value` branch so a non-dict display value can never be sent as `tool_use.input`. Match the OpenAI provider's contract: derive the wire field from `Arguments` only.
- **Dependencies**: Task 1 (the field rename).
- **Files Affected**:
  - `x/provider/anthropic/anthropic.go` — `parseToolArguments` shrinks; the doc comment is updated to remove the `Value` description and clarify that `Arguments` is the source of truth
  - `x/provider/anthropic/anthropic_test.go` — add a regression test
- **New Files**: None.
- **Interfaces**:
  - `func parseToolArguments(tc artifact.ToolCall) any` — reduced to:
    ```go
    if tc.Arguments == "" {
        return map[string]any{}
    }
    var v any
    if err := json.Unmarshal([]byte(tc.Arguments), &v); err == nil {
        return v
    }
    return tc.Arguments
    ```
    The `if tc.Display != nil` branch is removed. The `LLMRenderer` and `MarkdownRenderer` references in the function are removed (they were never consulted here; the original code only checked `Value != nil`).
- **Validation**:
  - `go test ./x/provider/anthropic/... -race` passes
  - `go build ./x/provider/anthropic/...` passes
  - **New test**: `TestProviderSerialize_DisplayDoesNotAffectWireFormat` — constructs a `ToolCall` with `Display: "not a dict"` and `Arguments: `{"x":1}``, calls `parseToolArguments` (or `serializeMessages` and inspects the emitted `tool_use` block's `input` field), and asserts the result is `map[string]any{"x": 1}` (a dict), not a string. This is the canary that proves the bug class is structurally fixed.
  - Existing `TestProviderSerialize_ReplaysToolUseAndToolResult` continues to pass (validates that `{"location":"sf"}` Arguments round-trips correctly).
- **Details**: This is the smallest possible fix to the bug. After this task the Anthropic provider is symmetric with OpenAI. The `LLMRenderer` / `MarkdownRenderer` interfaces are no longer consulted in this file at all; they remain on `ToolResult` for their legitimate use case.

### Task 4: Update `DisplayHint` field documentation across tool packages

- **Goal**: Make the "display only" contract explicit at the `tool.Tool` definition site so tool authors don't repeat the same mistake.
- **Dependencies**: None (documentation only; can run in parallel with Tasks 2/3).
- **Files Affected**:
  - `x/tool/bash/bash.go` — `BashTool.DisplayHint` field doc
  - `x/tool/filesystem/filesystem.go` — `ListDirectoryTool`, `ReadFileTool`, `WriteFileTool`, `EditFileTool`, `SearchFilesTool` `DisplayHint` field docs (5 sites)
  - `x/tool/calculator/calculator.go` — `AddTool`, `MultiplyTool` `DisplayHint` field docs (2 sites)
  - `x/tool/skills/tool.go` — `ReadSkillTool` `DisplayHint` field doc
- **New Files**: None.
- **Interfaces**: No code changes; doc comments only.
- **Validation**:
  - `go build ./x/tool/...` passes
  - The new doc comment text is consistent across all sites: "for human display only; the returned value has no effect on the wire format sent to the provider."
- **Details**: No behavior change. Future tool authors reading the `tool.Tool` definition see the constraint up front. The existing `BashDisplayHint` doc comment (which already correctly describes the bash-specific case) is left intact.

### Task 5: Verify downstream consumers and run the full test matrix

- **Goal**: Confirm that no downstream consumer depended on the old `Value`-as-wire-format-override behavior, and that the full repository builds and tests cleanly.
- **Dependencies**: Tasks 1, 2, 3 (the code changes must land first).
- **Files Affected**: None (verification only, unless a regression is found).
- **New Files**: None.
- **Interfaces**: None.
- **Validation**:
  - `go build ./...` passes at the repository root
  - `go test -race ./...` passes at the repository root (covers `artifact/`, `loop/`, `provider/`, `x/provider/anthropic/`, `x/provider/openai/`, `x/tool/...`, `x/conduit/...`, `x/export/...`, `x/llmbytes/`, `x/analytics/`, `x/compaction/`, `x/telemetry/`, `state/`, `cognitive/`, `session/`, `agent/`)
  - Spot-check the consumers known to read `ToolCall`:
    - `x/export/html.go:238` — uses `a.MarkdownString()`. Works under the new contract (display still renders for human display).
    - `x/export/text.go:103` — uses `a.MarkdownString()`. Same as above.
    - `x/conduit/tui/model.go:347` — uses `a.MarkdownString()` and `compactToolCall` (which reads `Arguments`, not `Display`). Works under the new contract.
    - `x/llmbytes/llmbytes.go:42` — uses `a.LLMString()` on `ToolCall`. The new `LLMString()` returns `Arguments`, which is a more accurate wire-size estimate than the previous display-value's JSON. The `x/llmbytes` test (`llmbytes_test.go:35`) compares against `len(tc.LLMString())` so the test is robust to the new behavior.
  - Grep confirms no remaining references to `ToolCall.Value` anywhere in the repository: `rg 'ToolCall\{[^}]*Value:' --type go` returns no matches.
- **Details**: This task is the safety net. If any consumer broke under the new contract, it's caught here and rolled into a follow-up task before the plan is considered complete.

## Dependency Graph

- Task 1 (artifact rename) → Task 2 (loop rename)
- Task 1 (artifact rename) → Task 3 (anthropic provider)
- Task 1, 2, 3 → Task 5 (full verification)
- Task 4 (tool docs) || Task 1 (independent — can run in parallel)
- Task 2, 3 || Task 4 (parallelizable; only depend on Task 1)

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| External consumer (outside this repo) depends on `ToolCall.Value` by name | High — breaks their build | Low — `Value` is recent (June 2026 commit `1f0bde5`) and the field has been clearly display-flavored from the start | The AGENTS.md stance is "aggressive refactoring." If an external consumer surfaces, they can grep-and-replace. Worth a final `rg 'ToolCall\.Value\|tc\.Value\|\.Value =' --type go` before merge. |
| `x/llmbytes` byte counts change in a way that breaks a downstream telemetry dashboard | Medium — observability regression | Low — the new count is more accurate; dashboards either keep working or improve | The `llmbytes` test (which compares against `len(tc.LLMString())`) is self-consistent and will pass. Document the behavior change in the `x/llmbytes/doc.go` byte-counting comment. |
| A tool author (now or in the future) writes a `DisplayHint` that returns a Go value whose `json.Marshal` output is a dict, expecting the wire format to use it | Low — the bug is fixed and the doc comment on `DisplayHint` says "display only" | Low — Task 4 makes the constraint explicit | The wire-format path no longer consults `Display` at all, so a confused tool author gets correct behavior (Arguments is used) regardless of what `DisplayHint` returns. The doc comment is the safety net for understanding. |
| The regression test in Task 3 is flaky or doesn't actually catch the bug | High — the bug could regress | Low — the test asserts on the actual wire format, not on a side channel | The test calls `serializeMessages` and inspects the JSON of the resulting `tool_use` block's `input` field. The assertion is `map[string]any{"x": 1}`, not a string. Any future regression that consults `Display` for wire format will fail this test. |
| `x/llmbytes` or some other consumer relies on `ToolCall.LLMString()` returning the display-value's JSON | Low — only telemetry uses it | Low — see Task 5 verification | Task 5's spot-check covers this; the `llmbytes` test passes under the new contract. |

## Validation Criteria

- [ ] `rg 'ToolCall\.Value|\.Value\s*=\s*v' artifact/ loop/ x/provider/` returns no matches
- [ ] `go build ./...` passes at the repository root
- [ ] `go test -race ./...` passes at the repository root
- [ ] New test `TestProviderSerialize_DisplayDoesNotAffectWireFormat` exists in `x/provider/anthropic/anthropic_test.go` and asserts the dict-shape wire format
- [ ] `parseToolArguments` body in `x/provider/anthropic/anthropic.go` no longer references `tc.Display`
- [ ] `applyDisplayHints` in `loop/loop.go` is the only writer of `ToolCall.Display` (verified by grep)
- [ ] `MarkdownString()` on `ToolCall` returns the string Display value as-is (no JSON quoting)
- [ ] All `DisplayHint` field doc comments in `x/tool/*` contain the phrase "for human display only"
- [ ] `ToolCall.MarshalJSON` wire shape (the JSON key `display`) is unchanged
