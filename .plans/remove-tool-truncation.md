# Plan: Remove Tool Result Truncation Framework

## Objective

Remove the entire tool result truncation framework from the ore codebase. The framework is broken — it does not effectively prevent oversized tool outputs from exceeding provider context limits. Rather than fix it, we strip it out entirely and let the provider error naturally when context limits are exceeded. A proper replacement will be designed and built later.

## Context

The truncation framework spans multiple packages and consists of:

1. **Core `tool/` package** (`tool/tool.go`, `tool/doc.go`, `tool/registry.go`, `tool/registry_test.go`):
   - `tool.Tool` struct has `MaxBytes int` and `TruncationHint string` fields
   - `TruncateContent` and `truncateToValidUTF8` helper functions
   - `tool.registry.go` copies `MaxBytes` and `TruncationHint` from remote tool sources
   - Extensive documentation in `tool/doc.go` under the "Tool Output Budget" section
   - Tests in `tool/registry_test.go` for `TruncateContent` behavior

2. **Extension `x/tool/` package** (`x/tool/handler.go`, `x/tool/doc.go`, `x/tool/handler_test.go`):
   - `Handler` applies `TruncateContent` after JSON serialization for both local and remote tool results
   - Documentation in `x/tool/doc.go` describing handler-level truncation
   - Tests in `x/tool/handler_test.go` for budget within/exceeded scenarios

3. **Concrete tool implementations**:
   - `x/tool/filesystem/filesystem.go`: "Cooperative truncation" — `readFile` caps at `readFileMaxBytes`, `searchFiles` tracks `estimatedBytes` against `searchFilesMaxBytes` and returns `errBudgetExceeded`, `listDirectory` has `listDirectoryMaxBytes`
   - `x/tool/bash/bash.go`: `MaxBytes: 50000` with `TruncationHint`
   - `x/tool/skills/tool.go`: `MaxBytes: 30000` with `TruncationHint`

4. **Test coverage**:
   - `tool/registry_test.go`: `TestTruncateContent_NoBudget`, `TestTruncateContent_WithinBudget`, `TestTruncateContent_ExceedsBudget`, `TestTruncateContent_UTF8Boundary`
   - `x/tool/handler_test.go`: `TestHandler_BudgetWithinLimit`, `TestHandler_BudgetExceeded`, and additional truncation scenarios
   - `x/tool/filesystem/filesystem_test.go`: `TestReadFile_StreamingTruncationWithOffset`, `TestReadFile_CapWithLimit`, `TestSearchFiles_BudgetWithinLimit`, `TestSearchFiles_BudgetExceeded`

Provider adapters (`x/provider/openai/`) do **not** serialize `MaxBytes` or `TruncationHint` into tool schemas, so no provider changes are needed.

## Architectural Blueprint

The selected approach is **aggressive removal**: delete all truncation-related code, fields, constants, tests, and documentation. No replacement is introduced now. The `Tool` struct becomes smaller, the handler becomes simpler, and concrete tools stop tracking their own output budgets. Oversized results flow through to the provider, which will error naturally if its context window is exceeded.

This aligns with the ore convention of aggressive refactoring at this stage — there are no users to break, no persisted state, and no legacy data to preserve.

## Requirements

1. Remove `MaxBytes` and `TruncationHint` fields from `tool.Tool` struct
2. Remove `TruncateContent` and `truncateToValidUTF8` functions from `tool/tool.go`
3. Remove truncation-related documentation from `tool/doc.go` and `x/tool/doc.go`
4. Remove `MaxBytes`/`TruncationHint` propagation from `tool/registry.go`
5. Remove truncation application from `x/tool/handler.go`
6. Remove cooperative truncation from `x/tool/filesystem/filesystem.go` (constants, `errBudgetExceeded`, budget checks)
7. Remove `MaxBytes`/`TruncationHint` from all concrete tool descriptors (`x/tool/filesystem/`, `x/tool/bash/`, `x/tool/skills/`)
8. Update tool descriptions to remove "Returns at most N bytes" language
9. Remove all truncation-related tests
10. Ensure `go test -race ./...` and `go build ./...` pass after each task

## Task Breakdown

### Task 1: Remove Truncation Framework from Core `tool/` Package
- **Goal**: Remove `MaxBytes`, `TruncationHint`, `TruncateContent`, and related docs/tests from the core tool package.
- **Dependencies**: None.
- **Files Affected**:
  - `tool/tool.go`
  - `tool/doc.go`
  - `tool/registry.go`
  - `tool/registry_test.go`
- **New Files**: None.
- **Interfaces**: `tool.Tool` struct loses `MaxBytes int` and `TruncationHint string` fields. `TruncateContent` and `truncateToValidUTF8` functions are deleted. `tool.registry.go` stops copying `MaxBytes` and `TruncationHint` from remote tools.
- **Validation**:
  - `go test ./tool/...` passes
  - `go build ./tool/...` passes
- **Details**:
  1. In `tool/tool.go`, remove the `MaxBytes` and `TruncationHint` fields from the `Tool` struct. Remove the `TruncateContent` and `truncateToValidUTF8` functions. Remove any unused imports (`fmt`, `strings`, `unicode/utf8` if they become unused).
  2. In `tool/doc.go`, remove the entire "Tool Output Budget" section and all references to `MaxBytes`, `TruncationHint`, and `TruncateContent`.
  3. In `tool/registry.go`, remove `MaxBytes` and `TruncationHint` from the remote tool conversion logic.
  4. In `tool/registry_test.go`, remove `TestTruncateContent_NoBudget`, `TestTruncateContent_WithinBudget`, `TestTruncateContent_ExceedsBudget`, and `TestTruncateContent_UTF8Boundary`.

### Task 2: Remove Truncation from `x/tool/` Handler
- **Goal**: Remove `TruncateContent` calls from the handler and update handler docs/tests.
- **Dependencies**: Task 1 (handler references `toolpkg.TruncateContent` and `toolDescriptor.MaxBytes`/`TruncationHint` which are deleted in Task 1).
- **Files Affected**:
  - `x/tool/handler.go`
  - `x/tool/doc.go`
  - `x/tool/handler_test.go`
- **New Files**: None.
- **Interfaces**: Handler no longer truncates tool results. The `Content` field of `artifact.ToolResult` is set to the raw (possibly error) content string directly without truncation.
- **Validation**:
  - `go test ./x/tool/...` passes
  - `go build ./x/tool/...` passes
- **Details**:
  1. In `x/tool/handler.go`, replace all four `toolpkg.TruncateContent(...)` calls with the raw `content` string (or `string(content)` for the JSON-marshaled case). The remote tool error path, remote tool success path, local tool error path, and local tool success path all become straight assignments.
  2. In `x/tool/doc.go`, remove the paragraph describing handler-level truncation enforcement.
  3. In `x/tool/handler_test.go`, remove all truncation-related tests (`TestHandler_BudgetWithinLimit`, `TestHandler_BudgetExceeded`, and any other tests that set `MaxBytes`/`TruncationHint` on tool descriptors to test truncation behavior). Remove any `MaxBytes`/`TruncationHint` field assignments in remaining test tool descriptors if they were only there for truncation tests.

### Task 3: Remove Cooperative Truncation from `x/tool/filesystem/`
- **Goal**: Remove the "cooperative truncation" constants, logic, and tests from the filesystem tool package.
- **Dependencies**: Task 1 (tool descriptors lose `MaxBytes`/`TruncationHint` fields).
- **Files Affected**:
  - `x/tool/filesystem/filesystem.go`
  - `x/tool/filesystem/filesystem_test.go`
- **New Files**: None.
- **Interfaces**: `read_file` no longer has an internal byte cap; `search_files` no longer tracks `estimatedBytes` or returns `errBudgetExceeded`. Tool descriptors no longer have `MaxBytes` or `TruncationHint`. The `limit` parameter on `read_file` remains — it is a user-facing parameter, not part of the truncation framework.
- **Validation**:
  - `go test ./x/tool/filesystem/...` passes
  - `go build ./x/tool/filesystem/...` passes
- **Details**:
  1. In `x/tool/filesystem/filesystem.go`:
     - Remove the constants `readFileMaxBytes`, `searchFilesMaxBytes`, `listDirectoryMaxBytes`
     - Remove `var errBudgetExceeded`
     - In `read_file`: remove the `const maxBytes = readFileMaxBytes` declaration and the `if result.Len()+len(formatted) > maxBytes` check
     - In `search_files`: remove `estimatedBytes` tracking, remove the `if estimatedBytes+matchBytes > searchFilesMaxBytes` checks, remove `errBudgetExceeded` return, remove the `errors.Is(err, errBudgetExceeded)` special case at the end
     - Remove `MaxBytes` and `TruncationHint` from all tool descriptors (`read_file`, `write_file`, `edit_file`, `list_directory`, `search_files`)
     - Update `read_file` description to remove "Returns at most 100000 bytes."
     - Update `list_directory` description to remove "Returns at most 10000 bytes."
     - Update `search_files` description to remove "Returns at most 50000 bytes."
  2. In `x/tool/filesystem/filesystem_test.go`:
     - Remove `TestReadFile_StreamingTruncationWithOffset`
     - Remove `TestReadFile_CapWithLimit`
     - Remove `TestSearchFiles_BudgetWithinLimit`
     - Remove `TestSearchFiles_BudgetExceeded`
     - Remove any test assertions that reference truncation behavior (e.g., line 706 `strings.HasSuffix(resultStr, "\n")` in a truncation-related context)

### Task 4: Remove Truncation Metadata from `x/tool/bash/` and `x/tool/skills/`
- **Goal**: Remove `MaxBytes`/`TruncationHint` from bash and skills tool descriptors.
- **Dependencies**: Task 1.
- **Files Affected**:
  - `x/tool/bash/bash.go`
  - `x/tool/skills/tool.go`
- **New Files**: None.
- **Interfaces**: None changed — only descriptor field removals.
- **Validation**:
  - `go test ./x/tool/bash/... ./x/tool/skills/...` passes
  - `go build ./x/tool/bash/... ./x/tool/skills/...` passes
- **Details**:
  1. In `x/tool/bash/bash.go`, remove `MaxBytes: 50000` and `TruncationHint` from the bash tool descriptor. Update the description to remove "Returns at most 50000 bytes."
  2. In `x/tool/skills/tool.go`, remove `MaxBytes: 30000` and `TruncationHint` from the `read_skill` tool descriptor. Update the description to remove "Returns at most 30000 bytes."

### Task 5: Full Repository Validation
- **Goal**: Ensure the entire repository builds and tests cleanly after all truncation removals.
- **Dependencies**: Task 2, Task 3, Task 4.
- **Files Affected**: None.
- **New Files**: None.
- **Interfaces**: None.
- **Validation**:
  - `go test -race ./...` passes
  - `go build ./...` passes
- **Details**: Run the full test suite with race detection. If any package outside the tool hierarchy references `MaxBytes`, `TruncationHint`, or `TruncateContent`, fix or flag it. (No such references were found during discovery, but this is the final safety check.)

## Dependency Graph

- Task 1 → Task 2, Task 3, Task 4 (Task 2, 3, and 4 are parallelizable after Task 1 completes)
- Task 2, Task 3, Task 4 → Task 5

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Provider errors from oversized tool outputs become user-facing | Medium | High | This is the intended behavior per requirements. Users will see provider errors when context limits are exceeded, which is more honest than silently truncated data. |
| `unicode/utf8` or other imports become unused in `tool/tool.go` after removing functions | Low | High | Go compiler will flag this; remove unused imports during Task 1. |
| Tests outside the identified files reference truncation behavior | Low | Medium | Task 5 full validation catches this. If found, add a quick fix task. |
| `limit` parameter on `read_file` is accidentally removed alongside `maxBytes` | Low | Medium | Be explicit in Task 3: the `limit` parameter is a user-facing feature and stays. Only the `maxBytes` cap is removed. |
| `write_file` and `edit_file` have `MaxBytes: 0` — removing the field is fine, but verify no logic depended on it | Low | Low | `MaxBytes: 0` meant "no limit" in the old framework. Removing the field has the same effect. No logic change needed. |

## Validation Criteria

- [ ] `tool.Tool` struct has no `MaxBytes` or `TruncationHint` fields
- [ ] `tool.TruncateContent` and `tool.truncateToValidUTF8` functions do not exist
- [ ] `tool/registry.go` does not copy `MaxBytes` or `TruncationHint` from remote tools
- [ ] `x/tool/handler.go` does not call `TruncateContent`
- [ ] `x/tool/filesystem/filesystem.go` has no `readFileMaxBytes`, `searchFilesMaxBytes`, `listDirectoryMaxBytes`, or `errBudgetExceeded`
- [ ] `x/tool/filesystem/filesystem.go` `search_files` does not track `estimatedBytes`
- [ ] No tool descriptors contain `MaxBytes` or `TruncationHint` fields
- [ ] No tool descriptions mention byte limits
- [ ] `go test -race ./...` passes
- [ ] `go build ./...` passes
