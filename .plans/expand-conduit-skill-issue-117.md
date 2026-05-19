# Plan: Expand Conduit Skill per Issue #117

## Objective

Expand and restructure the `.agents/skills/ore-conduit/SKILL.md` into `.agents/skills/conduit/SKILL.md` to close all six gaps identified in issue #117: conceptual explanation, forge compatibility, multi-conduit model, echo suppression, error contract, and sink failure handling. Extract the inlined Go skeleton into a bundled reference file within the skill directory to keep the skill lean and avoid the God Skill anti-pattern. Optimize the discovery-layer description for semantic routing and add explicit success criteria.

## Context

### Repository Topology

- `.agents/skills/ore-conduit/SKILL.md` is a 236-line prescriptive skill with an 11-step execution procedure, 4 boolean guards, a full Go skeleton, and 6 gotchas. It lacks conceptual rationale and contextual patterns.
- `x/conduit/doc.go` (post-PR #120) already documents the standard conduit contract: constructor, Descriptor, sink registration, blocking `Start(ctx)`, graceful shutdown. It is the authoritative reference for the concrete contract.
- `x/conduit/conduit.go` defines the `Conduit` interface, `Capability` constants, and `Descriptor` struct.
- `x/conduit/http/handler.go` exports a `Descriptor` variable and follows the functional-options constructor pattern `New(mgr *session.Manager, opts ...Option) (conduit.Conduit, error)`.
- `cmd/forge/blueprint.go` defines `Blueprint` with a `Conduits []ConduitConfig` array where each entry has a `module` path and optional `options` map. The generated template calls `alias.New(mgr)` with no arguments.
- `examples/forge/multi/forge.yaml` demonstrates HTTP + TUI conduits sharing the same session manager, illustrating the broadcast multi-conduit model.
- `AGENTS.md` establishes that conduits are "dumb pipes" and must not import `cognitive/` or invoke providers directly.
- `loop/loop.go` defines `EventContext` with a `Provenance string` field. `session/event_test.go` and `loop/loop_test.go` demonstrate setting and checking `Provenance` on events.

### Issue #117 Requirements

Issue #117 requests a canonical reference covering six patterns:
1. **Constructor contract:** `New(mgr *session.Manager, opts ...Option) + exported Descriptor`
2. **Lifecycle contract:** `Start(ctx)` blocks until `ctx` cancelled; sink registration before blocking
3. **Ingress patterns:** Map external events to thread IDs; `mgr.Attach()` or `mgr.Create()`; `stream.Process()`
4. **Sink patterns:** Register with `mgr` in `Start()`; receive all artifacts; render or post failure message
5. **Echo suppression:** Use framework `Provenance` metadata to skip self-submissions
6. **Error contract:** Fatal errors return from `Start()` (triggering agent shutdown); non-fatal errors log and continue

The skill must also fit the **forge generator** and the **broadcast multi-conduit model**.

## Architectural Blueprint

### Tree-of-Thought: Where Should the Extracted Skeleton Live?

| Path | Pros | Cons |
|---|---|---|
| **A. `x/conduit/doc.go`** | Already the canonical reference; `go doc` shows it; single source of truth | Would bloat an already-structured contract doc; mixes specification with implementation |
| **B. `docs/conduit-author-guide.md`** | Dedicated markdown doc; clean separation from contract; easy to reference from skill | Another file to maintain, outside the skill bundle |
| **C. `examples/conduit-skeleton/`** | Compilable, testable skeleton package | Overkill for a simple pattern reference; adds build burden |
| **D. `.agents/skills/conduit/SKELETON.md`** | Bundled within the skill directory per the Agent Skills spec; travels with the skill; no new top-level file | None |

**Decision: Path D (`.agents/skills/conduit/SKELETON.md`)** — follows the Agent Skills spec which allows bundling reference materials within the skill directory. The skeleton travels with the skill as a companion file. `x/conduit/doc.go` stays focused on the *contract* (what a conduit MUST do). The bundled `SKELETON.md` provides the *implementation reference* (how to write one). The skill's `SKILL.md` references `./SKELETON.md`.

### Structure of the Expanded Skill

The restructured skill follows the two-layer progressive disclosure model (YAML metadata + execution body) with a minimal set of sections:

1. **Discovery layer (YAML frontmatter):** Keyword-rich description for semantic routing.
2. **When to Use:** Sharpened trigger boundaries.
3. **What is a Conduit [NEW]:** 2-3 paragraph conceptual explanation (dumb pipe, ingress/egress, not a UI).
4. **Execution Procedure [AUGMENTED]:** The existing 11 steps, now with inline notes for forge, multi-conduit, echo suppression, error contract, and sink failure. The inlined Go skeleton is removed and replaced with a reference link to `./SKELETON.md`.
5. **Success Criteria [NEW]:** Short EDD-style checklist after the procedure.
6. **Boolean Guards [AUGMENTED]:** Keep existing 4, add 5th about forge mandatory-options limitation.
7. **Gotchas [AUGMENTED]:** Keep existing 6, add 7th about forge.
8. **References [UPDATED]:** Include `./SKELETON.md` and `examples/forge/README.md`.

The six issue #117 patterns are integrated into the execution procedure as inline augmentations rather than standalone sections, keeping the skill cohesive and close to its original structure.

## Requirements

1. Rename skill directory from `.agents/skills/ore-conduit/` to `.agents/skills/conduit/`; update YAML `name:` field to `conduit`; keep "ore conduit" terminology in body text.
2. Optimize YAML frontmatter `description:` for semantic routing with keywords: "new ore I/O conduit", "x/conduit/<name>", "functional-options constructor", "Descriptor", "blocking Start(ctx)", "forge YAML blueprint", "broadcast multi-conduit", "dumb pipe".
3. Add "What is a Conduit" conceptual preamble explaining architectural rationale (2-3 paragraphs).
4. Augment the 11-step execution procedure with inline notes:
   - Step 1: mention `forge.yaml` module-path declaration
   - Step 3: note that forge calls `alias.New(mgr)` with no options (zero-option constructor requirement)
   - Step 4: note multi-conduit shared session manager pattern
   - Step 5: note FanOut broadcast model (multiple concurrent subscribers)
   - Step 6: add sink failure handling (log & continue; optionally post failure message)
   - Step 7: add echo suppression via `EventContext.Provenance` (set on outbound, check on inbound)
   - Step 9: expand fatal vs. non-fatal error contract (fatal → agent shutdown; non-fatal → log & continue)
5. Add "Success Criteria" checklist (EDD-style) after the execution procedure.
6. Remove inlined Go skeleton from skill; replace with reference links to `./SKELETON.md` (bundled skeleton) and `x/conduit/doc.go` (contract).
7. Add a 5th boolean guard about conduits that require mandatory constructor options (forge incompatibility).
8. Add a 7th gotcha about forge's current inability to pass constructor options.
9. Update References section to include new docs.
10. Total skill length should stay close to the original (~240-280 lines) to avoid God Skill bloat.

## Task Breakdown

### Task 1: Rename ore-conduit Skill Directory to conduit
- **Goal:** Rename `.agents/skills/ore-conduit/` to `.agents/skills/conduit/` and update the YAML frontmatter `name:` field from `ore-conduit` to `conduit`.
- **Dependencies:** None.
- **Files Affected:** `.agents/skills/ore-conduit/SKILL.md`
- **New Files:** `.agents/skills/conduit/SKILL.md` (via `git mv`)
- **Interfaces:** YAML frontmatter `name:` field updated.
- **Validation:** `git status` shows a clean rename with no uncommitted modifications besides the single character change in YAML frontmatter. Directory path matches skill name.
- **Details:** Use `git mv` to preserve history. Update only the `name:` field in the YAML frontmatter. Keep all content references to "ore conduit" and "ore framework" unchanged per user instruction.

### Task 2: Create `.agents/skills/conduit/SKELETON.md` as the Bundled Skeleton Reference
- **Goal:** Extract the Go skeleton from the current skill into a bundled reference file within the skill directory, enhancing it with section-by-section explanatory comments that map each block to the standard contract in `x/conduit/doc.go`.
- **Dependencies:** None (parallelizable with Task 1).
- **Files Affected:** None.
- **New Files:** `.agents/skills/conduit/SKELETON.md`
- **Interfaces:** None (documentation only).
- **Validation:** File exists, contains a compilable Go skeleton with clear section headers (Descriptor, Constructor, Start & Session Attachment, Output Subscription & Sink, Input Loop & Process, Blocking & Shutdown). Each section cross-references the relevant clause in `x/conduit/doc.go`.
- **Details:** Copy the skeleton from the current skill verbatim as the base. Add a preamble explaining this is the reference implementation for new conduit authors. Add inline comments linking sections to `x/conduit/doc.go` clauses (e.g., "See Standard Conduit Contract §4"). Do NOT add forge-specific or multi-conduit complexity — keep it a minimal single-conduit skeleton. This file is bundled within the skill directory per the Agent Skills spec.

### Task 3: Expand and Restructure the Conduit Skill
- **Goal:** Rewrite `.agents/skills/conduit/SKILL.md` to close all six issue #117 gaps by augmenting the existing 11-step execution procedure with inline notes, adding a short conceptual preamble and success criteria checklist, and removing the inlined Go skeleton.
- **Dependencies:** Task 1, Task 2.
- **Files Affected:** `.agents/skills/conduit/SKILL.md`
- **New Files:** None.
- **Interfaces:** Updated YAML frontmatter with keyword-rich description optimized for semantic routing.
- **Validation:** The restructured skill contains: conceptual preamble, augmented 11-step procedure with all six issue #117 patterns integrated as inline notes, success criteria checklist, 5 boolean guards, 7 gotchas. Total line count stays close to original (~240-280 lines). All internal markdown references resolve. No Go source files are modified.
- **Details:** The builder must edit the skill with these specific changes:
  1. **Discovery layer (YAML frontmatter):** Update `description:` to be keyword-rich for semantic routing. Must include: "new ore I/O conduit", "x/conduit/<name>", "functional-options constructor", "Descriptor", "blocking Start(ctx)", "forge YAML blueprint", "broadcast multi-conduit", "dumb pipe".
  2. **When to Use:** Keep existing trigger boundaries. No changes needed.
  3. **What is a Conduit [NEW]:** Insert 2-3 paragraphs after "When to Use" and before "Execution Procedure". Explain: conduits are dumb pipes that translate external system events into ore session events and route assistant artifacts back; they are not UIs in the narrow sense; they do not import `cognitive/` or invoke providers. Reference `AGENTS.md` Conduit/Library vs. Application Boundary.
  4. **Execution Procedure [AUGMENTED]:** Keep the existing 11 steps. Remove the inlined Go skeleton block entirely; replace it with: "See `./SKELETON.md` for a compilable skeleton and `x/conduit/doc.go` for the standard contract." Add inline augmentations to specific steps:
     - **Step 1** (create package): Add note that the package must be declarable in a `forge.yaml` by its module path (e.g., `github.com/andrewhowdencom/ore/x/conduit/<name>`).
     - **Step 3** (constructor): Add note that forge calls `alias.New(mgr)` with no arguments, so the conduit MUST work with zero options. Functional options can override defaults but must not be required.
     - **Step 4** (create/attach): Add note that in multi-conduit agents, multiple conduits share the same `*session.Manager`. Each conduit calls `mgr.Create()` or `mgr.Attach()` independently.
     - **Step 5** (subscribe): Add note that the stream uses a FanOut broadcast model. Multiple conduits can subscribe concurrently; each receives all events independently.
     - **Step 6** (subscriber closure): Add note on sink failure handling. If delivery to the external system fails, log the error (non-fatal) and continue. Optionally render a failure message if the transport supports it.
     - **Step 7** (Process loop): Add echo suppression guidance. Before calling `stream.Process()`, set `EventContext.Provenance` to the conduit's identifier on outbound `UserMessageEvent`. When receiving events, check if `Provenance` matches your own identifier and skip processing to avoid echo loops.
     - **Step 9** (block/return): Expand the existing guidance. Fatal errors (startup failure, unrecoverable connection loss to the external system) MUST return non-nil from `Start()`, which triggers agent-level shutdown. Non-fatal errors (delivery failure to one recipient, transient timeout) MUST be logged and the conduit MUST continue.
  5. **Success Criteria [NEW]:** Insert a short checklist after "Execution Procedure" and before "Boolean Guards":
     - [ ] Package exports `Descriptor` with valid capabilities
     - [ ] Constructor accepts `*session.Manager` and validates non-nil
     - [ ] `Start(ctx)` blocks until `ctx.Done()`
     - [ ] Subscribes to output events before blocking
     - [ ] Maps all external inputs to `UserMessageEvent` or `InterruptEvent`
     - [ ] Passes `go test -race ./...`
     - [ ] Is declarable in a `forge.yaml` by module path
     - [ ] Handles provenance echo suppression
  6. **Boolean Guards [AUGMENTED]:** Keep existing 4 guards. Add 5th guard: "⚠️ **IF** the conduit requires mandatory constructor options (not just functional options with defaults) → STOP. Forge calls `alias.New(mgr)` with no arguments; mandatory options will break forge compatibility."
  7. **Gotchas [AUGMENTED]:** Keep existing 6 gotchas. Add 7th: "Forge calls `alias.New(mgr)` with no arguments. Conduits that require mandatory constructor options are currently incompatible with forge-generated binaries. Use functional options with sensible defaults."
  8. **References [UPDATED]:** Update to include `./SKELETON.md` and `examples/forge/README.md`.

### Task 4: Validate All References and Repository Health
- **Goal:** Verify all internal references in the expanded skill and skeleton doc resolve to existing files. Confirm the repository remains buildable and testable.
- **Dependencies:** Task 1, Task 2, Task 3.
- **Files Affected:** None (validation only).
- **New Files:** None.
- **Interfaces:** None.
- **Validation:** `go test -race ./...` passes from repository root with zero failures. All markdown hyperlinks/references in `conduit/SKILL.md` and `conduit/SKELETON.md` point to files that exist in the working tree.
- **Details:** Run `go test -race ./...` to confirm no Go files were accidentally modified. Manually verify that every file path referenced in the skill (e.g., `x/conduit/doc.go`, `./SKELETON.md`, `examples/forge/multi/forge.yaml`) exists. Confirm `git status` shows only the expected changes.

## Dependency Graph

- Task 1 || Task 2 (parallelizable)
- Task 1 → Task 3
- Task 2 → Task 3
- Task 3 → Task 4

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Skill exceeds context window and becomes a God Skill | High | Medium | Explicitly bound skill to <300 lines in plan. Move skeleton out, keep only lightweight conceptual sections and a trimmed execution procedure. |
| Forge template evolves and compatibility guidance becomes stale | Low | Medium | Add a "Living Document" note and reference specific file paths rather than prescriptive rules. |
| Echo suppression pattern depends on `EventContext.Provenance` semantics that may change | Medium | Low | Reference the current `session` package implementation. Add a note that provenance is source-only (already in current gotcha #3). |
| Bundled `SKELETON.md` duplicates content from `x/conduit/doc.go` | Low | Low | `SKELETON.md` focuses on compilable implementation; `x/conduit/doc.go` focuses on contract. Keep separation explicit. |
| `docs/` directory doesn't exist in all worktrees | Low | Low | `docs/` already exists (auto-generated by `cmd/docgen`). If missing, the builder creates it. |

## Validation Criteria

- [ ] `.agents/skills/conduit/SKILL.md` exists and is the renamed/expanded skill.
- [ ] `.agents/skills/conduit/SKELETON.md` exists and contains a compilable Go skeleton with explanatory comments.
- [ ] Skill contains "What is a Conduit" conceptual section.
- [ ] Execution Procedure Step 5 contains the broadcast multi-conduit model note (FanOut concurrent subscribers).
- [ ] Execution Procedure Steps 1 and 3 contain forge blueprint compatibility notes (module-path declaration, zero-option constructor).
- [ ] Execution Procedure Step 7 contains the echo suppression pattern using `EventContext.Provenance`.
- [ ] Execution Procedure Step 9 contains the error contract distinguishing fatal (shutdown) vs. non-fatal (log & continue).
- [ ] Execution Procedure Step 6 contains the sink failure handling note.
- [ ] Skill contains explicit "Success Criteria" checklist.
- [ ] Skill YAML frontmatter `description` is keyword-rich for semantic routing.
- [ ] Inlined Go skeleton is removed from the skill; replaced with reference links.
- [ ] All internal file references in `SKILL.md` and `SKELETON.md` resolve to existing files.
- [ ] `go test -race ./...` passes with zero failures.
