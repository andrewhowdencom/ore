# Plan: Validate Tool JSON Schema at Registration Time

## Objective
Add structural JSON Schema validation to `tool.Registry.Register` so that malformed tool parameter schemas fail fast at registration time rather than surfacing as opaque provider API errors at runtime (see issue #162).

## Context
The `tool.Registry.Register` method in `x/tool/registry.go` currently accepts a `schema map[string]any` parameter without any validation. Malformed schemas—such as a missing root `type: "object"` or parameter definitions placed at the top level instead of under `properties`—only fail when the provider adapter serializes them and the remote API rejects the request. This wastes API calls and produces poor developer experience.

The project follows a **minimal dependency** philosophy (`go.mod` has only `stretchr/testify` outside the standard library). All provider adapters use `net/http` and `encoding/json` from the standard library. The AGENTS.md conventions encourage **aggressive refactoring** and breaking internal APIs for cleaner boundaries.

Existing tool schemas in the codebase (calculator, filesystem, skills, bash) are already well-formed and will pass validation. Only test fixtures use intentionally malformed schemas like `{"type": "string"}`.

## Architectural Blueprint

### Selected Approach
Add an exported `ValidateSchema(schema map[string]any) error` helper in `x/tool` and integrate it into `Register`. Change `Register` to return `error`, making validation a mandatory, fail-fast gate.

**Why this path:**
- **Fail-fast alignment**: Catches developer mistakes during `go test` or application startup, not at runtime.
- **Minimal dependencies**: Uses only `encoding/json` from the standard library; no external JSON Schema library needed.
- **Downstream safety**: Exported `ValidateSchema` lets projects using `ore` pre-validate schemas without constructing a full `Registry`.
- **Clean API**: Returning `error` from `Register` is idiomatic Go and matches the project's error-wrapping conventions.

**Rejected alternatives:**
- *Validate in `Tools()` instead of `Register`*: Less fail-fast; doesn't catch errors at the point of definition.
- *Add a `WithSkipValidation` option*: Adds API surface for a case that doesn't yet exist; can be added later if genuinely needed.
- *Import a heavy JSON Schema library*: Violates the minimal-dependency principle and is overkill for the structural checks requested.

### Validation Rules
`ValidateSchema` will enforce the following, in order:
1. **Nil or empty schema** → valid (tool takes no parameters).
2. **JSON serializability** → `json.Marshal` the schema; fail if it contains cycles, functions, channels, or other non-JSON types.
3. **Root type check** → If the schema has any keys, the root `type` must be the string `"object"`.
4. **Known-keyword whitelist** → Any top-level key that is not a recognized JSON Schema keyword is rejected with a message directing the developer to nest parameter definitions under `properties`.

The known-keyword set covers common draft-07 and draft-2020-12 keywords (`type`, `properties`, `required`, `additionalProperties`, `patternProperties`, `minProperties`, `maxProperties`, `propertyNames`, `unevaluatedProperties`, `items`, `prefixItems`, `contains`, `minContains`, `maxContains`, `minItems`, `maxItems`, `uniqueItems`, `enum`, `const`, `format`, `multipleOf`, `maximum`, `exclusiveMaximum`, `minimum`, `exclusiveMinimum`, `maxLength`, `minLength`, `pattern`, `allOf`, `anyOf`, `oneOf`, `not`, `if`, `then`, `else`, `dependentSchemas`, `dependentRequired`, `title`, `description`, `default`, `examples`, `deprecated`, `readOnly`, `writeOnly`, `$schema`, `$id`, `$ref`, `$defs`, `definitions`, `$anchor`, `$dynamicRef`, `$dynamicAnchor`, `$vocabulary`, `$comment`, `contentMediaType`, `contentEncoding`).

## Requirements
1. `tool.Registry.Register` must return `error` and validate the schema before storing the tool.
2. A reusable `tool.ValidateSchema` function must be exported so downstream projects can pre-validate.
3. Validation must use only the Go standard library.
4. All existing callers of `Register` in `x/tool/`, `examples/`, and tests must be updated to handle the new error return.
5. Existing well-formed schemas must continue to pass; malformed test fixtures must be corrected.
6. The full test suite, including race detection, must pass after all changes.

## Task Breakdown

### Task 1: Add ValidateSchema helper and unit tests
- **Goal**: Create `ValidateSchema` with structural JSON Schema checks and comprehensive table-driven tests.
- **Dependencies**: None.
- **Files Affected**: None (new files only).
- **New Files**: `x/tool/schema.go`, `x/tool/schema_test.go`.
- **Interfaces**:
  ```go
  func ValidateSchema(schema map[string]any) error
  ```
- **Validation**: `go test ./x/tool/...` passes.
- **Details**:
  - Implement `ValidateSchema` with the four validation rules described in the Blueprint.
  - Wrap errors with context using `fmt.Errorf("...: %w", err)` per project conventions.
  - In `schema_test.go`, write table-driven tests covering:
    - `nil` schema (valid)
    - empty map `{}` (valid)
    - valid `{"type": "object"}`
    - valid schema with `properties` and `required`
    - invalid: non-JSON-serializable value (e.g., a channel)
    - invalid: root `type` missing
    - invalid: root `type` is `"string"`
    - invalid: misplaced property definition at top level (e.g., `{"name": {"type": "string"}}`)
    - invalid: unknown top-level key that is not a standard keyword
    - valid: schema with all known JSON Schema keywords at top level

### Task 2: Integrate validation into Registry.Register and update Registry tests
- **Goal**: Change `Register` to return `error`, call `ValidateSchema`, and update all tests in `registry_test.go`.
- **Dependencies**: Task 1.
- **Files Affected**: `x/tool/registry.go`, `x/tool/registry_test.go`.
- **New Files**: None.
- **Interfaces**:
  ```go
  func (r *Registry) Register(name, description string, schema map[string]any, fn ToolFunc) error
  ```
- **Validation**: `go test ./x/tool/...` passes.
- **Details**:
  - In `registry.go`, modify `Register` to validate the schema before storing. Return an error if validation fails. Retain the existing overwrite behavior on success.
  - In `registry_test.go`, update every `r.Register(...)` call to `require.NoError(t, r.Register(...))`.
  - Fix `TestRegistry_Register_Overwrite_Tools`: replace the invalid schemas `{"type": "string"}` and `{"type": "number"}` with distinct valid schemas (e.g., `{"type": "object", "title": "first"}` and `{"type": "object", "title": "second"}`) and update the corresponding assertion.
  - Add a new test `TestRegistry_Register_InvalidSchema` that asserts `Register` returns a non-nil error for a malformed schema and does not store the tool.
  - Update `TestRegistry_ConcurrentRegistration` to handle the error return from `Register` inside goroutines (use `require.NoError` in the goroutine or collect errors).

### Task 3: Update Handler tests
- **Goal**: Update `handler_test.go` to handle the new `Register` error return.
- **Dependencies**: Task 2.
- **Files Affected**: `x/tool/handler_test.go`.
- **New Files**: None.
- **Interfaces**: None.
- **Validation**: `go test ./x/tool/...` passes.
- **Details**:
  - Update every `r.Register(...)` call to `require.NoError(t, r.Register(...))`.
  - The `TestHandler_ExecutesRegisteredTool` schema `{"type": "object"}` is already valid and passes; only the call site needs updating.

### Task 4: Update Skills toolkit and Calculator integration tests
- **Goal**: Update `skills.Toolkit.Register` to propagate errors and update calculator handler integration tests.
- **Dependencies**: Task 2.
- **Files Affected**: `x/tool/skills/tool.go`, `x/tool/calculator/handler_integration_test.go`.
- **New Files**: None.
- **Interfaces**:
  ```go
  func (t *Toolkit) Register(registry *tool.Registry) error
  ```
- **Validation**: `go test ./x/tool/...` passes.
- **Details**:
  - In `skills/tool.go`, change `Toolkit.Register` to return `error`. Wrap each `registry.Register` error with context (e.g., `fmt.Errorf("register list_skills: %w", err)`).
  - In `calculator/handler_integration_test.go`, update every `registry.Register(...)` call to `require.NoError(t, registry.Register(...))`.

### Task 5: Update all example applications
- **Goal**: Update `examples/calculator`, `examples/filesystem`, and `examples/http-chat` to handle `Register` errors.
- **Dependencies**: Task 2.
- **Files Affected**: `examples/calculator/main.go`, `examples/filesystem/main.go`, `examples/http-chat/main.go`.
- **New Files**: None.
- **Interfaces**: None.
- **Validation**: `go build ./examples/...` passes.
- **Details**:
  - In each example, wrap `registry.Register(...)` calls with error handling. Since these are in `run() error` functions, propagate the error:
    ```go
    if err := registry.Register(...); err != nil {
        return fmt.Errorf("register <tool>: %w", err)
    }
    ```
  - The calculator and filesystem examples register multiple tools; each must be wrapped individually.

### Task 6: Update doc comments across affected packages
- **Goal**: Update `doc.go` files and commented examples to reflect the new `Register` signature.
- **Dependencies**: Task 2, Task 4, Task 5.
- **Files Affected**: `x/tool/doc.go`, `x/tool/calculator/doc.go`, `x/tool/filesystem/doc.go`, `x/tool/bash/doc.go`, `x/tool/skills/doc.go`, `examples/single-turn-cli/main.go`.
- **New Files**: None.
- **Interfaces**: None.
- **Validation**: `go test ./...` passes (comments do not affect compilation, but the full suite verifies no regressions).
- **Details**:
  - Update every `// registry.Register(...)` example comment to show the returned error being handled (e.g., `if err := registry.Register(...); err != nil { ... }`).
  - Update `examples/single-turn-cli/main.go` commented block similarly.

### Task 7: Full validation and race detection
- **Goal**: Run the complete test suite with race detection to confirm no regressions.
- **Dependencies**: Task 3, Task 4, Task 5, Task 6.
- **Files Affected**: None.
- **New Files**: None.
- **Interfaces**: None.
- **Validation**: `go test -race ./...` passes with zero failures.
- **Details**:
  - Execute `go test -race ./...` from the repository root.
  - If any package fails, trace the failure back to the originating task and fix it.

## Dependency Graph
- Task 1 → Task 2
- Task 2 → Task 3
- Task 2 → Task 4
- Task 2 → Task 5
- Task 3 || Task 4 || Task 5 (parallelizable after Task 2)
- Task 3, Task 4, Task 5 → Task 6
- Task 6 → Task 7

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Known-keyword whitelist is too restrictive and flags a legitimate JSON Schema keyword | Medium | Low | The whitelist is comprehensive (covers draft-07 and draft-2020-12). If a legitimate keyword is missing, the fix is a one-line addition to the whitelist map. A spike test against all existing tool schemas in the repo confirms none are flagged. |
| Changing `Register` return type breaks unknown downstream consumers outside the repo | Medium | Low | The repo conventions explicitly encourage breaking internal APIs at this stage. All in-repo callers are enumerated and updated in Tasks 3–6. Downstream consumers are expected to adapt; the exported `ValidateSchema` makes migration easier. |
| Remote tool schemas from MCP servers are not validated | Low | Medium | Out of scope for this plan. Remote schemas come from external servers and are assumed valid. If needed, a future task can add `Tools()`-time validation for remote tools. |
| `json.Marshal` silently drops unsupported types rather than erroring | Low | Low | `json.Marshal` returns an error for channels, functions, and cyclic references. This is exactly the behavior we want. We test this explicitly in Task 1. |

## Validation Criteria
- [ ] `go test ./x/tool/...` passes after Task 1.
- [ ] `go test ./x/tool/...` passes after Task 2.
- [ ] `go test ./x/tool/...` passes after Task 3.
- [ ] `go test ./x/tool/...` passes after Task 4.
- [ ] `go build ./examples/...` passes after Task 5.
- [ ] `go test ./...` passes after Task 6.
- [ ] `go test -race ./...` passes with zero failures after Task 7.
- [ ] `tool.ValidateSchema(map[string]any{"name": map[string]any{"type": "string"}})` returns a non-nil error (reproduces the exact bug from issue #162).
- [ ] `tool.ValidateSchema(nil)` returns nil.
- [ ] `tool.ValidateSchema(map[string]any{"type": "object", "properties": map[string]any{}})` returns nil.
