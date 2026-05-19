# Plan: Document Conduit Usage for Composers

## Objective

Add a standardized, human-facing composition guide to every ore conduit package so that developers wiring conduits into their own agent binaries can discover configuration options, runtime semantics, and forge blueprints without reading source code. This is achieved by: (1) creating a filled-out reference README example in the skill directory, and (2) updating the `conduit` skill to mandate a `README.md` as a procedural step.

## Context

### Repository State

- **`.agents/skills/conduit/SKILL.md`** (10066 bytes) — the ore conduit skill. It has an 11-step execution procedure covering package creation, constructor implementation, `Start()` lifecycle, `Descriptor` export, tests, and `go test -race ./...`. It has 8 success-criteria checkboxes. It references `./SKELETON.md` (compilable Go skeleton) and `x/conduit/doc.go` (standard contract). It does **not** mention any README or usage documentation.
- **`.agents/skills/conduit/SKELETON.md`** (3312 bytes) — a compilable Go reference skeleton. It is code-focused; it does not address prose documentation.
- **`x/conduit/doc.go`** (3413 bytes) — Go package documentation defining the `Conduit` interface, `Capability` constants, and `Descriptor` struct. It documents the *contract* but not *how to use* a specific conduit.
- **`x/conduit/http/handler.go`** — HTTP conduit implementation. Constructor: `New(mgr, opts...)`. Options: `WithAddr`, `WithUI`, `WithoutUI`. Session model: `POST /sessions` (creates or attaches via `thread_id`), `GET /sessions/{id}/events` (SSE), `POST /sessions/{id}/messages` (NDJSON), `DELETE /sessions/{id}`. No `README.md` exists.
- **`x/conduit/tui/tui.go`** — TUI conduit implementation. Constructor: `New(mgr, opts...)`. Options: `WithThreadID`. Session model: creates/attaches in `Start()`, subscribes to `"turn_complete"`, runs Bubble Tea program. No `README.md` exists.
- **`examples/forge/http/forge.yaml`** and **`examples/forge/tui/forge.yaml`** — minimal forge blueprints declaring the conduit by module path.
- **`examples/forge/README.md`** — comprehensive forge getting-started guide.

### Gap

The skill tells implementers *how to write* a conduit but never tells them to document it for consumers. A developer who wants to compose `x/conduit/http` into their binary must read `handler.go` to discover:
- What functional options exist (`WithAddr`, `WithUI`, `WithoutUI`)
- How sessions are created vs. resumed (`POST /sessions` with optional `thread_id`)
- The wire format (NDJSON vs. SSE)
- How to declare it in a `forge.yaml`
- What errors are fatal vs. logged

This friction scales with every new conduit.

### Precedent

The `standardize-conduit-patterns.md` plan (already executed) added the HTTP `Descriptor` export and documented the standard contract in `x/conduit/doc.go`. It established that the skill is a living document that references repo-internal plans. This plan follows the same pattern.

## Architectural Blueprint

### Selected Path

Create a **single reference file** (`.agents/skills/conduit/README_EXAMPLE.md`) containing a filled-out README for a fictional conduit, plus a **skill update** that mandates the README as step 12.

This path was chosen because:
- A filled-out example (not a literal `{{Template}}`) lets implementers pattern-match by reading realistic prose.
- Living the reference file inside the skill directory keeps it versioned with the skill and discoverable by agents.
- A procedural step (not just a success-criteria checkbox) ensures implementers do not skip it.

### Alternative Considered and Rejected

- **Centralized catalog** (one top-level doc listing all conduits): Rejected because conduits are build-time extensions; a centralized catalog would require updating every time a new conduit is added, creating a maintenance bottleneck. Per-package READMEs scale with the code.

### Standardized README Sections

Every conduit `README.md` must contain these sections in order:

1. **Overview** — one-line description matching `Descriptor.Description`
2. **Capabilities** — human-readable expansion of `Descriptor.Capabilities`
3. **Composition** — minimal instantiation snippet with `New(mgr, opts...)`
4. **Configuration / Options** — all functional options, env vars, defaults (table)
5. **Runtime Semantics** — session model, event types, threading, shutdown, echo suppression
6. **Forge Blueprint** — copy-pasteable `forge.yaml` snippet
7. **Error Handling** — fatal vs. logged-and-continued behavior

## Requirements

1. Create a reference file `.agents/skills/conduit/README_EXAMPLE.md` containing a filled-out example README for a fictional conduit (e.g., a "Slack Bot" or generic "Webhook" conduit), demonstrating all 7 standardized sections with realistic content.
2. Update `.agents/skills/conduit/SKILL.md` to add a step 12 mandating the README, add a success-criteria checkbox for the README, and reference `README_EXAMPLE.md` in the References section.
3. The fictional example must be grounded in real ore patterns observed in `x/conduit/http` and `x/conduit/tui` (constructor signature, `session.Manager` usage, `stream.Subscribe`, `EventContext.Provenance`, forge blueprints).

## Task Breakdown

### Task 1: Create Conduit README Reference Example
- **Goal**: Write `.agents/skills/conduit/README_EXAMPLE.md` — a filled-out example README for a fictional conduit that implementers copy and adapt.
- **Dependencies**: None.
- **Files Affected**: None.
- **New Files**: `.agents/skills/conduit/README_EXAMPLE.md`
- **Interfaces**: None (prose documentation only).
- **Validation**:
  - File renders correctly as GitHub-flavored Markdown (no broken links, no malformed tables).
  - All 7 standardized sections are present and non-empty.
  - The fictional conduit's constructor signature matches the standard contract: `New(mgr *session.Manager, opts ...Option) (conduit.Conduit, error)`.
  - The forge blueprint snippet uses a realistic module path (e.g., `github.com/andrewhowdencom/ore/x/conduit/example`).
- **Details**:
  - Choose a fictional but realistic conduit name (e.g., "AcmeWebhook" or "SlackBot"). Do not invent an actual implementation; this is purely a prose template.
  - Section 1 (Overview): one paragraph, echoes `Descriptor.Description`.
  - Section 2 (Capabilities): bullet list with short rationale for each capability (e.g., "`event-source` — the conduit streams events via HTTP POST webhook").
  - Section 3 (Composition): Go code snippet showing `New(mgr)` with at least one functional option.
  - Section 4 (Configuration / Options): table with columns "Option", "Type", "Default", "Description". Include env vars if applicable.
  - Section 5 (Runtime Semantics): subsections for Session Model, Event Subscription, Echo Suppression, Shutdown Behavior. Ground these in real ore semantics (e.g., "Sessions are created via `mgr.Create()` on first webhook delivery; resumption uses `mgr.Attach(threadID)`").
  - Section 6 (Forge Blueprint): YAML snippet with `dist` and `conduits` stanzas.
  - Section 7 (Error Handling): two sub-bullets: "Fatal errors (returned from `Start()`)" and "Non-fatal errors (logged, conduit continues)".
  - Add a header comment noting this is a reference template for the `conduit` skill.

### Task 2: Update Conduit Skill to Mandate READMEs
- **Goal**: Modify `.agents/skills/conduit/SKILL.md` to make the README a required deliverable.
- **Dependencies**: Task 1 (the skill must reference the new file).
- **Files Affected**: `.agents/skills/conduit/SKILL.md`
- **New Files**: None.
- **Interfaces**: None (prose updates only).
- **Validation**:
  - `grep -n "README.md" .agents/skills/conduit/SKILL.md` shows at least three matches (step 12, success criteria, references).
  - `grep -n "README_EXAMPLE.md" .agents/skills/conduit/SKILL.md` shows at least one match (references).
  - No broken Markdown formatting in the skill file.
- **Details**:
  - **Step 12 insertion**: After the existing step 11 (`Run go test -race ./...`), add step 12:
    > 12. **Write `README.md`** in the package root (`x/conduit/<name>/README.md`) documenting how to compose the conduit. Follow the structure in `./README_EXAMPLE.md`. Include: Overview, Capabilities, Composition, Configuration, Runtime Semantics, Forge Blueprint, and Error Handling.
  - **Success Criteria update**: Add a new checkbox:
    > - [ ] `README.md` is present with all required sections (see `./README_EXAMPLE.md`)
  - **References update**: Add to the References list:
    > - `./README_EXAMPLE.md` — filled-out reference README demonstrating the standardized sections.
  - **Note update**: Append to the living-document note:
    > After implementing a new conduit, review whether any pattern you discovered should be added here, and whether the `README_EXAMPLE.md` template should be updated.
  - Verify the skill still passes a basic Markdown lint (no broken tables, no dangling references).

## Dependency Graph

- Task 1 → Task 2 (Task 2 references the new file created in Task 1)

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Reference example diverges from real conduit patterns over time | Medium | Medium | The skill's living-document note already calls for reviewing discovered patterns; extend it to explicitly mention `README_EXAMPLE.md`. |
| Implementers treat the fictional example as a real package | Low | Low | Use a clearly fictional name (e.g., "AcmeWebhook") and add a prominent header comment stating it is a template. |
| Skill file length grows unwieldy | Low | Low | The addition is one step, one checkbox, and one reference line — minimal overhead. |
| Existing conduits (HTTP, TUI) remain undocumented | Medium | High | Explicitly out of scope for this plan; schedule a follow-up plan to backfill `x/conduit/http/README.md` and `x/conduit/tui/README.md`. |

## Validation Criteria

- [ ] `.agents/skills/conduit/README_EXAMPLE.md` exists and contains all 7 standardized sections.
- [ ] `.agents/skills/conduit/SKILL.md` contains a step 12 mandating the README.
- [ ] `.agents/skills/conduit/SKILL.md` success criteria include a README checkbox.
- [ ] `.agents/skills/conduit/SKILL.md` references section includes `./README_EXAMPLE.md`.
- [ ] No broken internal references in either Markdown file.
