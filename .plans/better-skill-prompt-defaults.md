# Plan: Better Skill Prompt Defaults

## Objective

Replace the passive skill activation instruction in `x/tool/skills` with a strong, conditional behavioral directive that tells the LLM exactly when to load skills. Add `SetDirective(string)` to both `Catalog` and `Toolkit` so applications can customize the directive text without hand-crafting prompt engineering.

## Context

- `x/tool/skills/types.go` — `Catalog.SystemPromptFragment(ctx)` currently hardcodes a passive header: `"You have access to the following specialized skills. Use read_skill(name=<skill>) to load detailed instructions when needed:"`. The phrase `"when needed"` is too vague; LLMs have no concrete trigger condition.
- `x/tool/skills/tool.go` — `Toolkit` wraps `Catalog` and exposes `SystemPromptFragment()` returning `func(context.Context) string`. `Toolkit` delegates directly to `Catalog.SystemPromptFragment`; there is no configuration layer.
- `x/tool/skills/doc.go` — Documents the current passive default in the "System Prompt Integration" section.
- Tests in `x/tool/skills/catalog_test.go` and `x/tool/skills/tool_test.go` assert the exact hardcoded output.
- No external consumers of `SystemPromptFragment()` or `NewToolkit()` exist outside `x/tool/skills/` (confirmed via grep across the repo).
- The `systemprompt` package (`x/systemprompt/systemprompt.go`) uses functional options (`WithContentFunc`, `WithContextContentFunc`). The skills package should follow a similar configurability pattern, but `NewCatalog` and `NewToolkit` take variadic `Discoverer` args — functional options would break their signatures. Setter methods are the non-breaking alternative.

## Architectural Blueprint

Add a `directive string` field to `Catalog` initialized with a strong default behavioral directive. Expose `SetDirective(string)` on both `Catalog` and `Toolkit`. Update `SystemPromptFragment()` to interpolate the configurable directive. Update tests and documentation.

**Selected approach**: Setter methods on both `Catalog` and `Toolkit`.
- Non-breaking: `NewCatalog(discoverers ...Discoverer)` and `NewToolkit(discoverers ...Discoverer)` signatures remain unchanged.
- Clean separation: `Catalog` owns the data and fragment generation; `Toolkit` is the application-facing wrapper that delegates configuration.
- Minimal surface: a single `directive` string field, not a full template engine or formatter interface.

**Rejected alternatives**:
- Functional options on `NewCatalog`/`NewToolkit`: would break the variadic `Discoverer` parameter signatures.
- Separate `FragmentFormatter` type: over-engineered for a single directive string change.
- Moving fragment generation entirely to `Toolkit`: would duplicate `Catalog.SystemPromptFragment()` logic and abandon the existing public API.

## Requirements

1. `SystemPromptFragment()` produces a deterministic, sorted skill listing (already true; preserve this).
2. The default fragment includes a behavioral directive that tells the LLM **when** to load skills (not just that it **can**).
3. Applications can customize or override the directive text via a `Toolkit` option [inferred: `SetDirective` method on `Toolkit`].
4. Empty skill catalogs still produce a clean empty string (graceful degradation).
5. The change does not break existing consumers of `SystemPromptFragment()` (signature unchanged; output changes but behavior is the same: return a string).

## Task Breakdown

### Task 1: Add Configurable Directive to Catalog
- **Goal**: Add a `directive` field to `Catalog` with a strong default, update `SystemPromptFragment()` to use it, and add a `SetDirective` method.
- **Dependencies**: None.
- **Files Affected**: `x/tool/skills/types.go`
- **New Files**: None.
- **Interfaces**:
  - `func (c *Catalog) SetDirective(directive string)` — setter for custom directive text
  - Updated `SystemPromptFragment(ctx context.Context) string` — interpolates `c.directive` instead of hardcoded string
- **Validation**: `go test ./x/tool/skills/...` passes (tests updated in Task 2).
- **Details**:
  1. Add `directive string` field to `Catalog` struct.
  2. In `NewCatalog`, initialize `directive` to the strong default: `"When your task matches a skill description below, call read_skill(name=<skill>) to load its detailed instructions before proceeding."`
  3. Update `SystemPromptFragment` to use `c.directive` as the header line instead of the hardcoded string.
  4. Add `func (c *Catalog) SetDirective(directive string)` method.
  5. Ensure empty/error cases still return `""` (graceful degradation).

### Task 2: Update Tests for New Default Directive
- **Goal**: Update all test expectations in `catalog_test.go` and `tool_test.go` to match the new default directive.
- **Dependencies**: Task 1.
- **Files Affected**:
  - `x/tool/skills/catalog_test.go`
  - `x/tool/skills/tool_test.go`
- **New Files**: None.
- **Interfaces**: None.
- **Validation**: `go test -race ./x/tool/skills/...` passes.
- **Details**:
  1. In `catalog_test.go`: update `TestCatalog_SystemPromptFragment` expected string to use the new default directive.
  2. In `tool_test.go`: update `TestToolkit_SystemPromptFragment` expected string to use the new default directive.
  3. Verify `TestCatalog_SystemPromptFragment_Empty`, `TestCatalog_SystemPromptFragment_Error`, `TestToolkit_SystemPromptFragment_ErrorFallback` still return empty strings as before.
  4. Add a new test `TestCatalog_SetDirective` that calls `SetDirective` with custom text and asserts the fragment uses the custom directive.
  5. Add a new test `TestToolkit_SetDirective` that calls `SetDirective` on `Toolkit` and asserts the fragment uses the custom directive.

### Task 3: Add Directive Configuration to Toolkit
- **Goal**: Expose `SetDirective` on `Toolkit` so applications can customize the directive without accessing the unexported `catalog` field.
- **Dependencies**: Task 1.
- **Files Affected**: `x/tool/skills/tool.go`
- **New Files**: None.
- **Interfaces**:
  - `func (t *Toolkit) SetDirective(directive string)` — delegates to `t.catalog.SetDirective(directive)`
- **Validation**: `go test -race ./x/tool/skills/...` passes.
- **Details**:
  1. Add `func (t *Toolkit) SetDirective(directive string)` method that calls `t.catalog.SetDirective(directive)`.
  2. This is the application-facing configuration API; `Catalog.SetDirective` remains available for direct `Catalog` users.

### Task 4: Update Package Documentation
- **Goal**: Update `doc.go` to document the new default directive and `SetDirective` usage.
- **Dependencies**: Task 1, Task 2, Task 3.
- **Files Affected**: `x/tool/skills/doc.go`
- **New Files**: None.
- **Interfaces**: None.
- **Validation**: Manual review of `doc.go` for accuracy and completeness.
- **Details**:
  1. Update the "System Prompt Integration" section in `doc.go` to show the new default directive in example output.
  2. Add documentation for `SetDirective` usage on both `Catalog` and `Toolkit`.
  3. Ensure the prose accurately describes the behavior: the default is a strong conditional directive, and applications can override it.

### Task 5: Full Repository Validation
- **Goal**: Verify the entire repository compiles and passes all tests.
- **Dependencies**: Task 2, Task 3, Task 4.
- **Files Affected**: None (validation only).
- **New Files**: None.
- **Validation**:
  - `go test -race ./...` passes
  - `go build ./...` passes
- **Details**: Run `go test -race ./...` and `go build ./...`. Since no external packages consume `x/tool/skills`, only tests within `x/tool/skills/` should be affected. Fix any compilation or test failures.

## Dependency Graph

- Task 1 → Task 2
- Task 1 → Task 3
- Task 2 || Task 3 (parallelizable after Task 1)
- Task 2 → Task 5
- Task 3 → Task 4
- Task 4 → Task 5

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Changing default output breaks tests outside x/tool/skills | Low | Low | Verified via grep: no external consumers of `SystemPromptFragment()` or `NewToolkit()` exist outside the package. Only tests within `x/tool/skills/` need updating. |
| New directive is still too weak for LLM adherence | Medium | Medium | The proposed default follows Pi's proven pattern: `"When your task matches X, call Y"`. If real-world usage shows poor adherence, applications can override via `SetDirective` without further code changes. |
| Setter methods feel non-idiomatic vs functional options | Low | Low | `NewCatalog`/`NewToolkit` take variadic `Discoverer` args; functional options would break signatures. Setter methods are a standard non-breaking alternative in Go. |

## Validation Criteria

- [ ] `Catalog.SystemPromptFragment()` returns the new strong default directive when no custom directive is set.
- [ ] `Catalog.SetDirective("custom")` causes `SystemPromptFragment()` to use `"custom"` as the header.
- [ ] `Toolkit.SetDirective("custom")` causes `SystemPromptFragment()` to use `"custom"` as the header.
- [ ] Empty/error skill catalogs still return `""` from `SystemPromptFragment()`.
- [ ] `go test -race ./x/tool/skills/...` passes.
- [ ] `go test -race ./...` passes with zero failures.
- [ ] `go build ./...` passes with zero compilation errors.
- [ ] `doc.go` accurately documents the new default directive and `SetDirective` API.
