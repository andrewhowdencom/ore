# Plan: Add Composable Prompt Fragments to systemprompt

## Objective

Enable `x/systemprompt` to accept and concatenate multiple content functions into a single `RoleSystem` turn, replacing the current "last write wins" limitation of `WithContentFunc`. This unblocks downstream packages (e.g., `x/tool/skills`) from injecting their own prompt fragments without wrapping the application's content function. The implementation follows the user's expectation of a simple variadic `WithContentFuncs(cf, cf, cf)` API rather than the more complex prefix/suffix or named fragment designs proposed in the issue description.

## Context

**Repository:** `andrewhowdencom/ore` — a Go framework for building agentic applications.

**Current `x/systemprompt` implementation (`x/systemprompt/systemprompt.go`):**
- `config` struct stores a single `contentFunc func() string`
- `WithContentFunc` overwrites the previous value on each call
- `Transform` produces exactly one `RoleSystem` turn with one `artifact.Text`

> **Safety check:** A full-repo `grep` confirms `WithContentFunc` is only ever called once per `New()` invocation (in a commented example and in tests). No multi-call patterns exist — there is no `../workshop` or other downstream package that relies on last-write-wins overwrite semantics. The change to append is safe.

**Current `x/guardrails` implementation (`x/guardrails/guardrails.go`):**
- Separate `loop.Transform` that injects each rule as an individual `RoleUser` turn
- Doc explicitly states: "Using RoleUser (rather than RoleSystem) gives the guardrails the weight of user instructions, distinct from the persona set by x/systemprompt"
- Applications compose both: `loop.WithTransforms(sp, gr)`

**Downstream need (`x/tool/skills`):**
- Issue #177 (blocked by #178) wants a system prompt transform that lists discovered skills
- Currently impossible without the skills package wrapping the user's prompt function

**Project conventions (`AGENTS.md`):**
- Prefer aggressive refactoring; do not preserve backwards compatibility for internal APIs
- Use table-driven tests; always run `go test -race ./...`
- Use `fmt.Errorf("...: %w", err)` for error wrapping

## Architectural Blueprint

The selected approach is a **unified fragment slice** with simple variadic convenience:

1. **Internal storage**: `config` holds `contentFuncs []func() string` instead of a single field.
2. **`WithContentFunc`**: Changes from overwrite to **append**. This is technically a behavior change, but "last write wins" was a limitation, not a feature. The project convention explicitly permits breaking internal APIs.
3. **`WithContentFuncs(fns ...func() string)`**: New variadic convenience that appends all given functions to the internal slice in order.
4. **`Transform`**: Evaluates all functions in registration order, filters out empty results, and concatenates the remainder with `\n\n` separators into a single `RoleSystem` turn.
5. **Guardrails**: Evaluated separately. The intentional `RoleUser` semantics documented in `x/guardrails/doc.go` provide distinct value — guardrails as user instructions carry different conversational weight than system persona. Therefore `x/guardrails` remains a standalone `loop.Transform`. No consolidation into `systemprompt`.

**Rejected alternatives:**
- *Prefix/Suffix (Option A from issue)*: Over-engineered for the actual use case. The order of `Option` application already determines ordering; explicit prefix/suffix slots add unnecessary API surface.
- *Named Fragments (Option B from issue)*: Adds a map and name-based lookup that nobody requested. Complicates the API without clear benefit.

## Requirements

1. `systemprompt.New` accepts multiple content functions via `WithContentFunc` or `WithContentFuncs`
2. Fragments are evaluated lazily on each `Transform` call (preserving existing dynamic behavior)
3. Fragments concatenate deterministically with `\n\n` separators
4. Empty or nil fragment results are omitted (no trailing/leading separators)
5. `Transform` continues to produce exactly one `RoleSystem` turn
6. Existing tests continue to pass after adjusting for append semantics
7. Evaluate `x/guardrails` consolidation and document the decision to keep it separate

## Task Breakdown

### Task 1: Add Composable Content Fragments to systemprompt
- **Goal**: Refactor `x/systemprompt` to store and concatenate multiple content functions.
- **Dependencies**: None.
- **Files Affected**:
  - `x/systemprompt/systemprompt.go`
  - `x/systemprompt/systemprompt_test.go`
  - `x/systemprompt/doc.go`
- **New Files**: None.
- **Interfaces**:
  - `WithContentFunc(fn func() string) Option` — semantics changed from overwrite to append
  - `WithContentFuncs(fns ...func() string) Option` — new variadic option
  - `Transform` concatenation logic updated
- **Validation**:
  - `go test ./x/systemprompt/...` passes
  - `go test -race ./x/systemprompt/...` passes
  - All new tests exercise multiple fragments, empty fragment skipping, and `\n\n` separators
- **Details**:
  1. Change `config.contentFunc func() string` to `contentFuncs []func() string`.
  2. Change `WithContentFunc` to append `fn` to `contentFuncs`.
  3. Add `WithContentFuncs(fns ...func() string) Option` that iterates and appends each non-nil function.
  4. Update `Transform` to:
     - Iterate `contentFuncs`
     - Evaluate each function
     - Skip empty string results
     - Join non-empty results with `\n\n`
     - Produce a single `RoleSystem` turn with the joined text (or empty string if all fragments empty)
  5. Update `doc.go` to document the new variadic option and fragment concatenation behavior.
  6. Update tests:
     - `TestTransform_PrependSystemPrompt` — should still pass (single fragment)
     - `TestTransform_EmptyContent` — should still pass (no fragments, nil default)
     - `TestTransform_NilContentFunc` — may need adjustment; nil func should be skipped
     - Add `TestTransform_MultipleContentFuncs` — two fragments, verifies `\n\n` separator
     - Add `TestTransform_EmptyFragmentSkipped` — mixed empty and non-empty fragments
     - Add `TestTransform_AllEmptyFragments` — all empty, produces empty system turn
     - Add `TestTransform_MultipleWithContentFuncCalls` — two separate `WithContentFunc` calls

### Task 2: Evaluate Guardrails Consolidation
- **Goal**: Determine whether `x/guardrails` should be merged into `x/systemprompt` or remain a standalone transform.
- **Dependencies**: Task 1 (to reason about the interaction with composable fragments).
- **Files Affected**:
  - `x/guardrails/doc.go`
  - `x/guardrails/guardrails.go`
- **New Files**: None.
- **Interfaces**: No new interfaces. Decision: keep `x/guardrails` as a standalone `loop.Transform`.
- **Validation**:
  - Read `x/guardrails/doc.go` rationale for `RoleUser` semantics
  - Confirm that moving guardrails to `RoleSystem` would change conversational weight
  - Document conclusion in the commit message or as a comment in `x/guardrails/doc.go`
  - `go test ./x/guardrails/...` passes (no code changes expected)
- **Details**:
  1. Read the current `x/guardrails` documentation and implementation.
  2. Confirm the intentional distinction: guardrails as `RoleUser` instructions vs. system prompt as `RoleSystem` persona.
  3. Conclude that consolidation into `systemprompt` would erase this semantic distinction.
  4. Decide **not** to deprecate or remove `x/guardrails`.
  5. Optionally add a brief comment in `x/guardrails/doc.go` clarifying why it remains a separate transform even with composable systemprompt fragments (e.g., "Guardrails are intentionally injected as RoleUser turns so they carry the weight of user instructions, distinct from the system persona. This remains true even when systemprompt supports multiple fragments.")
  6. Reject the issue's proposed `guardrails.Fragment()` helper — it would encourage loss of `RoleUser` semantics.

## Dependency Graph

- Task 1 → Task 2 (Task 2 depends on understanding the fragment model to evaluate consolidation)

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|------|--------|------------|------------|
| Changing `WithContentFunc` from overwrite to append breaks downstream code that relied on last-write-wins | Medium | Low | Search codebase for multiple `WithContentFunc` calls; project conventions explicitly permit breaking internal APIs at this stage |
| `Transform` concatenation logic introduces separator bugs (double separators, leading/trailing) | Low | Low | Write dedicated tests for edge cases: all empty, mixed empty/non-empty, single fragment |
| Guardrails consolidation debated during implementation, slowing progress | Low | Medium | Pre-document the decision in this plan; Task 2 is a lightweight evaluation, not a rewrite |
| Nil content funcs cause panics if not filtered during `WithContentFuncs` | High | Low | Defensively skip nil functions in both `WithContentFunc` and `WithContentFuncs` (preserve existing nil-safe behavior) |

## Validation Criteria

- [ ] `go test ./...` passes
- [ ] `go test -race ./...` passes
- [ ] `x/systemprompt` tests cover: single fragment, multiple fragments via `WithContentFunc`, multiple fragments via `WithContentFuncs`, empty fragment skipping, all-empty fragments, dynamic content evaluation across multiple fragments
- [ ] `Transform` produces exactly one `RoleSystem` turn regardless of fragment count
- [ ] Fragments concatenate with `\n\n` separator when non-empty
- [ ] Empty fragment results produce no stray separators
- [ ] `x/guardrails` tests continue to pass with no changes
- [ ] Guardrails consolidation decision is documented (commit message or code comment)
