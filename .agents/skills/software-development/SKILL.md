---
name: software-development
description: |
  Codifies the principle of centrality-weighted reasonability for Go code in
  the ore repository. Central packages (many dependents, high change impact)
  must be radically simple, while peripheral packages (few dependents, low
  change frequency) can carry more internal complexity provided they are
  self-contained. The skill guides agents to keep code simple in a
  contextual way: simplicity is risk management, not a moral virtue.
---

# Ore Software Development

## When to Use

This skill is triggered by any Go code modification in the ore repository.
It governs architectural simplicity decisions — where to keep code minimal,
where controlled complexity is acceptable, and when to refactor.

Do NOT use this skill for:
- Domain-specific conduit implementation (see `conduit/` skill instead)
- Domain-specific provider adapter implementation (no dedicated skill yet;
  apply this skill's general principles and note the provider-adapter
  gotcha below)
- Go language conventions, tooling, or syntax questions (see a future `go/`
  skill instead)
- Ore architectural philosophy or package boundary decisions at the
  repository level (see `AGENTS.md` instead)

> **Scope Boundary:** This skill guides architectural quality *within*
> the current plan's scope. If a planned change would violate a Boolean
> Guard, report the deviation and halt per the execution agent's rules.
> Do not silently refactor outside the plan to work around a guard.

## Core Principle: Centrality-Weighted Reasonability

Simplicity is **risk management**, not a moral virtue. The simpler a piece of
code is, the less likely it is to harbor bugs, the easier it is to change,
and the lower its blast radius when it does change. But simplicity is not
free — it often requires abstraction, indirection, or splitting what could
be one file into many. Whether that cost is worth paying depends on how
**central** the code is.

**Centrality** is the product of two factors:

1. **Internal dependents**: How many other ore packages import this package?
2. **Change frequency**: How often is this package modified relative to the
   repository?

A package that is imported by many others and changes frequently is
**highly central**. A package that is imported by few (or none) and changes
rarely is **peripheral**.

The principle is: **your simplicity budget scales inversely with centrality**.
Central packages must be radically simple; peripheral packages can carry
more internal complexity provided it is self-contained.

### Two Zones

| Zone | Centrality | Simplicity Target |
|---|---|---|
| **Central** | Imported by ≥3 internal packages OR sits on the core dependency chain (`artifact/` → `state/` → `provider/` → `core/`/`loop/`) | Radically simple. One public type, one responsibility, minimal surface area. |
| **Peripheral** | Imported by ≤2 internal packages, concrete leaf (e.g. `x/provider/openai/`, `x/conduit/http/`) | Self-contained complexity is acceptable. The package should still have a single responsibility, but it may contain longer functions or more types if they are all private to that package. |

### Ore Exemplars

**Radically simple central abstractions** (the target for central packages):

- `artifact/artifact.go` (~105 lines): one public interface `Kind() string`,
  concrete types, no logic.
- `provider/provider.go` (~60 lines): one interface `Invoke()`, one marker
  interface, one helper. No branching logic.
- `state/state.go` (~30 lines): one interface with two methods, one struct.
- `x/conduit/conduit.go` (~55 lines): one interface `Start()`, capability
  constants, one descriptor struct.

**Boundary cases** (acceptable self-contained complexity):

- `cognitive/react.go` (~55 lines): thin orchestration that composes
  abstractions. One public type, one public method.
- `tool/registry.go` (~200 lines): focused single responsibility (tool
  registration and lookup). Many lines, but one concept.
- `x/tool/handler.go` (~150 lines): single responsibility, slightly
  repetitive serialization logic confined to the package.

**Signals** (over the simplicity cap — candidates for refactoring):

- `loop/loop.go`: `Turn()` spans ~115 lines handling transforms, provider
  invocation, delta accumulation, event emission, and handlers. Too many
  responsibilities in one method.
- `x/provider/openai/openai.go`: `serializeMessages()` spans ~77 lines of
  complex bridging between ore state and OpenAI message params.
- `x/conduit/http/handler.go`: `sendMessage()` spans ~113 lines of NDJSON
  streaming, session lookup, and error handling.
- `junk/stream.go`: `Process()` entangles event dispatch, mutexes,
  context lifecycle, and saving in a single method.

## Assessing Centrality

When modifying a package, look it up in the **Known Centrality Map** below.
If it is not listed, assume peripheral and verify with the heuristics
after the table.

### Known Centrality Map

| Package | Zone | Rationale |
|---|---|---|
| `artifact/` | Central | Core chain leaf; every other package depends on it transitively |
| `state/` | Central | Core chain; depended on by `provider/` and everything above |
| `provider/` | Central | Core chain; depended on by `loop/` and everything above |
| `loop/` | Central | Core chain; orchestrates all provider invocations |
| `junk/` | Central | Transitive via `loop/` and `state/`; changes here affect everything downstream |
| `cognitive/` | Peripheral | Imported only by `examples/` and `cmd/` applications |
| `tool/` | Peripheral | Imported only by `x/tool/handler/` and applications |
| `x/provider/openai/` | Peripheral | Concrete adapter; one internal consumer (applications) |
| `x/conduit/http/` | Peripheral | Concrete conduit; one internal consumer (applications) |
| `x/tool/handler/` | Peripheral | Concrete handler; one internal consumer (applications) |

### Additional Heuristics

For packages not in the map, or when the map may be stale after a major
refactor:

1. **Check position on the core chain**: Packages `artifact/`, `state/`,
   `provider/`, and `core/`/`loop/` are central by definition regardless of
   importer count, because every other package depends on them
   transitively.
2. **Check transitivity**: A package imported by only one other package
   may still be central if that importer is itself central. Trace the
   dependency chain upward.
3. **Estimate change frequency**: Look at `git log --oneline -- <pkg>/` for
   the last 20 commits. If the package appears frequently, it is high
   change-frequency and thus higher centrality.

## Execution Procedure

Follow these steps in order. Do not skip or reorder.

1. **Identify the package you are modifying** and its file(s).
2. **Assess centrality** using the heuristics above. Classify the package
   as central or peripheral.
3. **Evaluate complexity against the simplicity budget**:
   - For **central** packages: every public symbol should justify its
     existence. Prefer one public type, one public method per type, and
     zero branching logic in public APIs. Extract private helpers freely.
   - For **peripheral** packages: single responsibility is still required,
     but the implementation may be longer or contain more types provided
     they are all internal to the package.
4. **Signal vs. act**:
   - If you are adding new code and it pushes a central package over the
     simplicity cap, **STOP** and find a way to keep the central package
     thin (add the complexity to a peripheral package, introduce a new
     peripheral package, or use a functional option pattern to push
     configuration outward).
   - If you are modifying peripheral code and it remains self-contained,
     proceed. Note the complexity in your reasoning log so future agents
     are aware.
   - If you encounter existing complexity in a central package that is
     not related to your current task, **signal it** (add a TODO or open
     an issue) but do not refactor it unless your task explicitly includes
     cleanup. Do not let perfect be the enemy of good.
5. **Refactor if needed**: If your task explicitly includes refactoring,
   or if the planned change cannot be made without violating a Boolean
   Guard below, decompose before proceeding. Prefer:
   - Extracting a new peripheral package (e.g. `x/<concern>/`).
   - Splitting a large function into smaller functions with narrow contracts.
   - Moving orchestration code out of central packages into application
     or cognitive layers.
6. **Validate**: Run the per-task validation criteria from the plan
   (tests, lint, build) and `task validate` if a Taskfile exists. Then
   re-assess the package: if it is now more complex and more central
   than before, reconsider the change.

## Success Criteria

After modifying ore code, verify:

- [ ] The package's centrality was assessed before complexity decisions
      were made
- [ ] Central packages contain only one public responsibility per file
- [ ] No public interface in a central package has more than 5 methods
- [ ] No function in a central package requires understanding more than 2
      other packages to read
- [ ] Peripheral complexity is self-contained (no imports of other
      peripheral implementation packages)
- [ ] The change references `AGENTS.md` for repository-level boundaries
      rather than restating them
- [ ] Go language questions were deferred to a future `go/` skill or to
      standard Go conventions, not encoded in this skill's scope

## Boolean Guards

If any of the following are true, **STOP** and reassess:

- ⚠️ **IF** a package imported by >3 other internal packages has an
  interface with >5 methods → STOP and extract the excess methods into a
  narrower interface or a new peripheral package.
- ⚠️ **IF** a core package (`artifact/`, `state/`, `provider/`, `loop/`)
  method handles both orchestration AND transformation AND emission in the
  same function → STOP and decompose into at least two of: a thin
  orchestrator (in the core package), a transformer (in a peripheral
  package), and an emitter (in a peripheral or application package).
- ⚠️ **IF** an implementation package (e.g. `x/provider/openai/`,
  `x/conduit/http/`) imports another implementation package at the same
  level of abstraction → STOP and introduce an abstraction in a core or
  shared package, or lift the shared code to a common utility package.
- ⚠️ **IF** understanding a single function requires reading >3 other
  packages to follow its logic → STOP and reassess encapsulation. The
  function likely has too many cross-package dependencies and should be
  split or its dependencies narrowed.

## Gotchas

1. **Provider adapters are inherently complex.** Bridging between ore's
   minimal abstractions and a provider's native API (`x/provider/openai/`
   is ~380 lines, `serializeMessages()` ~77 lines) requires serialization,
   deserialization, streaming delta handling, and error mapping. This is
   expected. The guard is: the complexity should be **localized** to the
   adapter and should not leak into `provider/` or `loop/`. Do not try to
   radically simplify an adapter at the cost of pushing provider-specific
   details into core packages.
2. **"Central" is transitive.** A package with only one direct internal
   importer may still be central if that importer is `loop/` or `junk/`.
   Always trace the full dependency chain upward. For example, `junk/`
   imports `loop/` and `state/`; changes in `junk/` affect everything
   downstream of `loop/`.
3. **Self-encapsulation allows dependencies, but they must be narrow stable
   contracts.** A peripheral package like `x/conduit/http/` may import
   `junk/`, `loop/`, `state/`, and `artifact/` — that is fine because
   those are narrow, stable core interfaces. What is not fine is a
   peripheral package importing another peripheral package of similar
   abstraction level (e.g. `x/conduit/http/` importing `x/provider/openai/`).
   That indicates a missing abstraction.
4. **The skill is about architectural simplicity, not Go language
   conventions.** Questions about error wrapping (`fmt.Errorf`), table-driven
   tests, `log/slog` usage, or functional options belong in a future `go/`
   skill. Do not conflate the two. Reference `AGENTS.md` for Go conventions
   that are already documented at the repository level.

## References

- `AGENTS.md` — ore architectural boundaries, package structure, cycle-free
  dependency graph, and implementation conventions.
- `README.md` — ore design principles and package overview.
- `conduit/SKILL.md` — domain-specific skill for conduit implementation.
  Use it when the task is building or modifying an I/O conduit.
- `artifact/artifact.go` — exemplar of a radically simple central package
  (~105 lines, one public interface, concrete types).
- `provider/provider.go` — exemplar of a minimal provider contract
  (~60 lines, one method interface).
- `state/state.go` — exemplar of minimal state abstraction (~30 lines,
  two-method interface).
- `x/conduit/conduit.go` — exemplar of minimal conduit interface
  (~55 lines, one method interface).
- `cognitive/react.go` — boundary case: thin orchestration that composes
  abstractions (~55 lines).
- `tool/registry.go` — boundary case: focused single responsibility
  (~200 lines, one concept).
- `x/tool/handler.go` — boundary case: single responsibility with
  slightly repetitive logic (~150 lines).
- `loop/loop.go` — signal: `Turn()` entangles multiple responsibilities
  (~115 lines).
- `x/provider/openai/openai.go` — signal: `serializeMessages()` is complex
  bridging (~77 lines).
- `x/conduit/http/handler.go` — signal: `sendMessage()` mixes streaming,
  session lookup, and error handling (~113 lines).
- `junk/stream.go` — signal: `Process()` entangles event dispatch,
  mutexes, context lifecycle, and saving (~55 lines).

> **Note:** This skill is a living document. After major refactors that
> change package centrality or move files between central and peripheral
> zones, review whether this skill's examples and heuristics should be
> updated.
