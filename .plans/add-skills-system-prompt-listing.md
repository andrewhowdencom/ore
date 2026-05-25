# Plan: Add System Prompt Skills Listing

## Objective

Add an ore-native mechanism that injects discovered skill metadata (name + description) from a `skills.Catalog` into the system prompt at step-creation time, so the LLM knows what skills exist without requiring an extra `list_skills` tool call. The feature must be compatible with the composable prompt fragment model already implemented in `x/systemprompt`.

## Context

- `x/systemprompt` (post-#178) supports composable prompt fragments via `WithContentFunc` and `WithContentFuncs`. Multiple fragments accumulate and are concatenated with `\n\n` separators on `Transform`. Empty and nil fragments are skipped.
- `x/tool/skills` provides a `Catalog` that aggregates `Discoverer` results, caches them, and exposes `List(ctx) ([]SkillMeta, error)` — deterministically sorted by name. `SkillMeta` contains `Name` and `Description`.
- `x/tool/skills` also provides a `Toolkit` that wraps a `Catalog` with tool functions (`list_skills`, `read_skill`, `search_skills`).
- Currently, applications must manually mention the skills tool in their system prompt or accept an extra round-trip for discovery.
- Issue #177 is blocked by #178, which is now **closed** and merged (commit `19adff7`).

## Architectural Blueprint

The implementation follows a **two-package, zero-dependency design**:

1. **`x/systemprompt`** gains a new `WithContextContentFunc(fn func(context.Context) string)` option. During `Transform`, context-aware fragments are evaluated alongside regular fragments, all feeding into the same `parts` slice that is joined with `\n\n`. This is a generic extension point — skills is the first consumer, but any dynamic fragment that needs the transform context can use it.

2. **`x/tool/skills`** gains:
   - `Catalog.SystemPromptFragment(ctx context.Context) string` — queries `List(ctx)`, formats the result as a Markdown bullet list with a header, and returns `""` if no skills exist or an error occurs (no empty-list noise).
   - `Toolkit.SystemPromptFragment() func(context.Context) string` — a convenience method returning the catalog's fragment function, ready for `systemprompt.WithContextContentFunc`.

This keeps `x/tool/skills` independent of `x/systemprompt` (no import cycles), and `x/systemprompt` independent of `x/tool/skills`.

## Requirements

1. A `skills.Catalog` can produce a formatted prompt fragment suitable for injection into `x/systemprompt`.
2. The system prompt includes a deterministic, formatted list of all discovered skill names and descriptions.
3. If no skills are discovered, the section is omitted entirely (no empty list noise).
4. The implementation is compatible with the composable fragment model from #178 (works alongside other `WithContentFunc` / `WithContentFuncs` calls).
5. The fragment function receives the `Transform` context for proper cancellation and timeout propagation.
6. All changes include unit tests and package documentation updates.

## Task Breakdown

### Task 1: Add Context-Aware Content Func Option to systemprompt
- **Goal**: Extend `x/systemprompt` to accept prompt fragments that require a `context.Context`.
- **Dependencies**: None.
- **Files Affected**: `x/systemprompt/systemprompt.go`, `x/systemprompt/systemprompt_test.go`
- **New Files**: None.
- **Interfaces**:
  - New option: `func WithContextContentFunc(fn func(context.Context) string) Option`
  - Updated `config` struct with `ctxContentFuncs []func(context.Context) string`
  - Updated `Transform` to evaluate `ctxContentFuncs` in order, appending non-empty results to `parts`
- **Validation**: `go test ./x/systemprompt/...` passes. New tests cover:
  - Single context-aware fragment
  - Mixed regular + context-aware fragments in correct order
  - Empty/nil context-aware fragments are skipped
  - Context is actually passed to the function (verified via a test that reads from context)
- **Details**: The new option must follow the same accumulation semantics as `WithContentFunc` — nil functions skipped, empty string results omitted. Both `contentFuncs` and `ctxContentFuncs` contribute to the same `parts` slice in registration order. Regular `contentFuncs` are evaluated first, then `ctxContentFuncs`, maintaining backward compatibility.

### Task 2: Add SystemPromptFragment to skills.Catalog
- **Goal**: Add a method to `Catalog` that returns a formatted prompt fragment listing all discovered skills.
- **Dependencies**: None (can be done in parallel with Task 1).
- **Files Affected**: `x/tool/skills/types.go`, `x/tool/skills/catalog_test.go`
- **New Files**: None.
- **Interfaces**:
  - New method on `Catalog`: `func (c *Catalog) SystemPromptFragment(ctx context.Context) string`
- **Validation**: `go test ./x/tool/skills/...` passes. New tests cover:
  - Fragment with multiple skills (deterministic order, correct formatting)
  - Fragment with no skills returns `""`
  - Fragment when `List` errors returns `""`
- **Details**: The method calls `c.List(ctx)`. If the result is empty or an error occurs, it returns `""`. Otherwise, it formats:
  ```
  You have access to the following specialized skills. Use read_skill(name=<skill>) to load detailed instructions when needed:

  - <name>: <description>
  - <name>: <description>
  ```
  The list is deterministic because `List` sorts by name. Use `strings.Builder` for efficient formatting.

### Task 3: Add SystemPromptFragment Convenience to skills.Toolkit
- **Goal**: Provide a one-liner on `Toolkit` that returns the fragment function for use with `systemprompt.WithContextContentFunc`.
- **Dependencies**: Task 2.
- **Files Affected**: `x/tool/skills/tool.go`, `x/tool/skills/tool_test.go`
- **New Files**: None.
- **Interfaces**:
  - New method on `Toolkit`: `func (t *Toolkit) SystemPromptFragment() func(context.Context) string`
- **Validation**: `go test ./x/tool/skills/...` passes. Test verifies the returned function delegates to the catalog and produces the expected output.
- **Details**: The method simply returns `t.catalog.SystemPromptFragment`. This gives applications a clean composition:
  ```go
  tk := skills.NewToolkit(...)
  sp, _ := systemprompt.New(
      systemprompt.WithContextContentFunc(tk.SystemPromptFragment()),
  )
  ```

### Task 4: Update Package Documentation
- **Goal**: Document the new system prompt fragment feature in `x/tool/skills`.
- **Dependencies**: Task 2, Task 3.
- **Files Affected**: `x/tool/skills/doc.go`
- **New Files**: None.
- **Interfaces**: None.
- **Validation**: `go build ./x/tool/skills/...` passes (no syntax errors in doc comments).
- **Details**: Update `doc.go` to include a "System Prompt Integration" section showing how to wire `Toolkit.SystemPromptFragment()` into `x/systemprompt`. Also update the "Composition" section to mention this as an alternative to relying solely on `list_skills`.

## Dependency Graph

- Task 1 || Task 2 (parallelizable; no cross-dependencies)
- Task 1 → Task 4 (doc update references context-aware fragments)
- Task 2 → Task 3 (Toolkit method delegates to Catalog method)
- Task 3 → Task 4 (doc example references Toolkit method)

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| `Catalog.List(ctx)` can fail during fragment evaluation (e.g., filesystem read error) | Medium | Low | `SystemPromptFragment` silently returns `""` on error, so the system prompt simply omits the section rather than failing the entire transform. This is intentional — skills are a convenience, not a hard requirement. |
| Adding `WithContextContentFunc` to systemprompt expands API surface | Low | High | The option follows the exact same pattern as `WithContentFunc`. It is a small, consistent addition. Future alternative: a single unified `WithContentFunc` that takes `func(context.Context) string` and deprecates the old one, but that breaks backward compatibility which AGENTS.md says to prefer at this stage. |
| Cross-package test dependencies if we add an integration test | Low | Medium | Do not add integration tests between `x/systemprompt` and `x/tool/skills`. Each package is tested independently with mocks/closures. Integration is validated at the application level (e.g., `examples/` or `cmd/`). |

## Validation Criteria

- [ ] `go test -race ./x/systemprompt/...` passes with new context-aware fragment tests
- [ ] `go test -race ./x/tool/skills/...` passes with new `SystemPromptFragment` tests
- [ ] `go test -race ./...` passes for the entire repository
- [ ] `x/tool/skills/doc.go` includes updated composition documentation
- [ ] A `skills.Catalog` with zero discoverers returns `""` from `SystemPromptFragment` (no empty list noise)
- [ ] A `skills.Catalog` with skills returns a deterministic, formatted Markdown list
- [ ] `systemprompt.Transform` correctly interleaves regular and context-aware fragments in registration order
