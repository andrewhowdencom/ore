# Plan: Create software-development Skill with Centrality-Weighted Reasonability

## Objective

Create a new `.agents/skills/software-development/SKILL.md` that codifies the principle of **centrality-weighted reasonability** for Go code in the ore repository. The skill guides agents to keep code simple in a contextual way: central packages (many dependents, high change impact) must be radically simple, while peripheral packages (few dependents, low change frequency) can carry more internal complexity provided they are self-contained. The skill must follow the established ore skill format, be compatible with the existing skill ecosystem, and not duplicate language-level Go conventions.

## Context

### Repository State

- **Branch**: `main`, up to date with `origin/main`
- **Existing skills**: Only `.agents/skills/conduit/SKILL.md` (plus `README_EXAMPLE.md`, `SKELETON.md`)
- **No `go/` skill exists yet** — so no overlap risk with language conventions
- **Plans directory**: `.plans/` exists with 50+ existing plans following a standard format

### Architectural Conventions from `AGENTS.md`

- ore is a **framework for building agentic applications**, not a specific implementation
- Core packages (`artifact/`, `state/`, `provider/`, `core/`) live at root level
- Concrete adapters live under `x/provider/<name>/`, `x/conduit/<name>/`
- **Cycle-free dependency graph**: `artifact/` → `state/` → `provider/` → `core/`, with concrete branches off `provider/` and `conduit/` that never import `core/`
- **Aggressive refactoring preferred** over backwards compatibility at this stage
- Core abstractions should be minimal: `Artifact` has one method (`Kind()`), `Provider` has one method (`Invoke()`), `State` has two methods (`Turns()`, `Append()`)

### Existing Skill Format Reference

The `conduit/SKILL.md` establishes the ore skill format:
- YAML frontmatter with `name:` and `description:`
- `# <Name>` heading
- `## When to Use` — trigger conditions and exclusions
- `## <Concept Name>` — explanatory section
- `## Execution Procedure` — numbered, ordered steps
- `## Success Criteria` — checkboxes
- `## Boolean Guards` — `⚠️ **IF** ... → STOP` format
- `## Gotchas` — numbered list of pitfalls
- `## References` — links to relevant files and other skills

### Codebase Examples for the Skill

**Exemplars of minimal central abstractions:**
- `artifact/artifact.go` (~100 lines): one interface `Kind() string`, concrete types
- `provider/provider.go` (~70 lines): one interface `Invoke()`, one marker interface
- `state/state.go` (~40 lines): one interface `State`, one struct `Turn`
- `x/conduit/conduit.go` (~70 lines): one interface `Start()`, capability constants

**Boundary cases (acceptable complexity):**
- `cognitive/react.go` (~50 lines): thin orchestration, composes abstractions
- `tool/registry.go` (~200 lines): focused single responsibility
- `x/tool/handler.go` (~150 lines): single responsibility, slightly repetitive

**Signals (over the cap):**
- `loop/loop.go`: `Turn()` is ~200 lines handling transforms, provider invocation, delta accumulation, event emission, handlers
- `x/provider/openai/openai.go`: `serializeMessages()` ~120 lines of complex bridging
- `x/conduit/http/handler.go`: `sendMessage()` ~100 lines of NDJSON streaming
- `session/stream.go`: `Process()` entangles event dispatch, mutexes, context lifecycle, saving

## Architectural Blueprint

The skill follows the **established ore skill format** with these sections:

1. **Frontmatter**: `name: software-development`, `description:` covering centrality-weighted reasonability
2. **When to Use**: Triggered by any Go code modification; excludes domain-specific tasks covered by other skills (conduit, provider adapter)
3. **Core Principle**: Centrality-weighted reasonability — simplicity budget scales inversely with package centrality
4. **Assessing Centrality**: Heuristics for agents to evaluate a package's position in the graph
5. **Execution Procedure**: Concrete steps to assess, signal, and act on complexity
6. **Boolean Guards**: STOP conditions for central package violations
7. **Success Criteria**: Checklist for validating a change against the principle
8. **Gotchas**: Common misapplications of the principle
9. **References**: Links to `AGENTS.md`, `README.md`, existing skills, and ore exemplars

The skill is **not** a uniform "lines of code" rulebook. It is a set of signals and heuristics that help an agent decide where simplification matters most.

## Requirements

1. Skill file created at `.agents/skills/software-development/SKILL.md`
2. Follows the ore skill format (YAML frontmatter, matching section structure to `conduit/SKILL.md`)
3. Codifies the centrality-weighted reasonability principle with concrete heuristics
4. Includes ore-specific examples (files referenced above) as grounding
5. Explicitly scopes itself to architectural simplicity, not Go language conventions
6. Does not duplicate the `conduit/` skill's domain-specific instructions
7. Includes at least 4 Boolean Guards in the `⚠️ **IF** ... → STOP` format
8. Includes at least 3 Gotchas
9. Compatible with the existing skill ecosystem (can be triggered alongside other skills)

## Task Breakdown

### Task 1: Draft the software-development skill file

- **Goal**: Create `.agents/skills/software-development/SKILL.md` with the centrality-weighted reasonability principle, following the established ore skill format.
- **Dependencies**: None.
- **Files Affected**: None (new file only).
- **New Files**:
  - `.agents/skills/software-development/SKILL.md`
- **Interfaces**: None (this is a documentation/skill artifact).
- **Validation**:
  - File exists at the correct path
  - Contains YAML frontmatter with `name:` and `description:`
  - Contains all required sections: `When to Use`, core principle section, `Execution Procedure`, `Success Criteria`, `Boolean Guards`, `Gotchas`, `References`
  - References only real files discovered during context ingestion
  - Does not duplicate content from `conduit/SKILL.md`
- **Details**:
  1. Create directory `.agents/skills/software-development/` if it does not exist.
  2. Write `SKILL.md` following the format of `.agents/skills/conduit/SKILL.md`.
  3. Frontmatter `description:` should capture the centrality-weighted principle in one paragraph.
  4. `## When to Use` section: triggered by any Go code change in the ore repo; explicitly excludes tasks covered by domain-specific skills (e.g., conduit implementation).
  5. Core principle section: explain that simplicity is risk management, not a moral virtue. Define centrality (number of internal dependents × change frequency). Define the two zones: central (radically simple) vs. peripheral (self-contained complexity acceptable).
  6. `## Execution Procedure`: numbered steps for an agent to (a) assess centrality, (b) evaluate complexity against budget, (c) signal vs. act, (d) refactor if needed.
  7. `## Boolean Guards`: at least 4 guards using the `⚠️ **IF** ... → STOP` format. Examples:
     - IF a package imported by >3 other internal packages has an interface with >5 methods → STOP and extract
     - IF a core package method handles both orchestration AND transformation AND emission → STOP and decompose
     - IF an implementation package imports another implementation package → STOP and introduce abstraction
     - IF understanding a single function requires reading >3 other packages → STOP and reassess encapsulation
  8. `## Gotchas`: at least 3. Examples:
     - Provider adapters are inherently complex (bridging two models); higher threshold is acceptable
     - "Central" is transitive — check the full dependency chain
     - Self-encapsulation allows dependencies, but they must be narrow stable contracts
  9. `## References`: link to `AGENTS.md`, `README.md`, `conduit/SKILL.md`, and specific ore exemplar files.

### Task 2: Validate against existing skill ecosystem

- **Goal**: Verify the new skill does not conflict with existing skills, follows the correct format, and is discoverable.
- **Dependencies**: Task 1.
- **Files Affected**:
  - `.agents/skills/conduit/SKILL.md` (read for format comparison)
  - `.agents/skills/software-development/SKILL.md` (validate)
- **New Files**: None.
- **Interfaces**: None.
- **Validation**:
  - Read `conduit/SKILL.md` and confirm the new skill follows the same structural conventions (frontmatter, section ordering, guard format, gotcha numbering)
  - Confirm the new skill's `When to Use` exclusions do not overlap with `conduit/SKILL.md`'s scope
  - Confirm the new skill does not duplicate content from `AGENTS.md` (it should reference it, not restate it)
  - All file paths referenced in the skill exist in the repository
  - The skill can be parsed as valid Markdown
- **Details**:
  1. Read `.agents/skills/conduit/SKILL.md` and compare section structure.
  2. Verify `.agents/skills/software-development/SKILL.md` has matching structure.
  3. Spot-check that all referenced file paths (`artifact/artifact.go`, `provider/provider.go`, `state/state.go`, `loop/loop.go`, etc.) exist.
  4. Confirm no duplicate content with `AGENTS.md` — the skill should reference AGENTS.md rather than restating package structure rules.
  5. Confirm the skill's scope does not claim ownership of conduit-specific or provider-adapter-specific tasks.

## Dependency Graph

- Task 1 → Task 2

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Skill is too abstract to be actionable | Medium | Medium | Include concrete ore file references and ore-specific examples in the skill body |
| Overlaps with future `go/` skill (language conventions) | Low | Medium | Explicitly scope to architectural simplicity; add note that Go language questions belong in a future `go/` skill |
| Conflicts with domain-specific skills (conduit) | Low | Low | Reference domain skills in `When to Use` exclusions; do not restate domain rules |
| Skill becomes stale as architecture evolves | Medium | High | Include note that skill is a living document; add success criteria prompting updates after major refactors |

## Validation Criteria

- [ ] `.agents/skills/software-development/SKILL.md` exists and is valid Markdown
- [ ] File contains YAML frontmatter with `name: software-development` and a `description:` field
- [ ] File contains all required sections matching the ore skill format: `When to Use`, core principle, `Execution Procedure`, `Success Criteria`, `Boolean Guards`, `Gotchas`, `References`
- [ ] At least 4 Boolean Guards in the `⚠️ **IF** ... → STOP` format
- [ ] At least 3 Gotchas with concrete ore examples
- [ ] All file paths referenced in the skill exist in the repository
- [ ] Skill explicitly excludes domain-specific tasks covered by other skills (conduit)
- [ ] Skill references `AGENTS.md` and `README.md` rather than restating their content
- [ ] Skill is scoped to architectural simplicity, not Go language conventions
