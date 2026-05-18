# Plan: Restructure README and Move Package Detail to Go Docs

## Objective

Replace the monolithic README with a concise landing page focused on project overview, philosophy, and orientation links. Move all package-level architectural detail, API examples, and usage documentation into the respective Go package docs (godoc) and dedicated Forge documentation. This eliminates stale duplication, ensures documentation lives with the code it describes, and creates a single source of truth for each concern.

## Context

The current README (`README.md`, ~300 lines) is a hybrid vision document and package-by-package API reference. It extensively documents `artifact/`, `state/`, `provider/`, `loop/`, `tool/`, `cognitive/`, `thread/`, `conduit/`, `session/`, and `cmd/forge/` concepts inline. Several problems stem from this structure:

- **Stale references**: The README still refers to `conduit/` at root level, but the package moved to `x/conduit/` in a prior refactor (`move-conduit-to-x` plan).
- **Content duplication**: The "Loop / Step", "Provider Adapters", "Extension Points", "I/O Conduits", "Threads", and "Three-Layer Architecture" sections mirror content already present in each package's `doc.go`.
- **Missing package docs**: `tool/` has no `doc.go` at all, and `x/conduit/` only has a brief package comment in `conduit.go`.
- **Forge docs trapped in README**: Detailed CLI usage, manifest format, and backward-compatibility notes for `cmd/forge` belong in a dedicated `cmd/forge/README.md` where users of the tool can find them.
- **Project status checklist**: The user has explicitly stated this is no longer relevant and should be removed.

Most packages (`artifact/`, `state/`, `provider/`, `loop/`, `cognitive/`, `thread/`, `session/`) already have well-written `doc.go` files that largely match the README's descriptions. The main gaps are `tool/` (no doc.go), `x/conduit/` (minimal package comment), and `cmd/forge/` (no dedicated README).

`examples/forge/README.md` already serves as a comprehensive getting-started guide for Forge blueprints. `docs/reference/forge-cli.md` is auto-generated CLI reference documentation.

## Architectural Blueprint

### Target README Structure

The new README becomes a **landing page** with four sections:

1. **Overview** — 1-2 paragraphs: what ore is, its purpose, and its philosophy (minimal core, build-time composition, provider-agnostic).
2. **Design Principles** — succinct bullet list (simplicity, composability, I/O agnosticism, build-time extension, defer specifics, tool calling as extension).
3. **Relationship to pi.dev** — brief paragraph explaining lineage and divergence points (language, I/O conduits first-class, build-time composition, scope as framework not agent).
4. **Component Map** — a table of packages with one-line descriptions and links to `pkg.go.dev` documentation.
5. **Forge Quick Reference** — brief pointer to Forge as the build-time agent generator, with a link to `cmd/forge/README.md` for full usage.

Everything else — architecture deep-dives, package-level descriptions, code examples, manifest format, project status checklist — is removed from the README.

### Documentation Destinations

| README Content | Destination |
|---|---|
| Tool calling & registry examples | `tool/doc.go` |
| I/O Conduit architecture & types | Enriched `x/conduit/conduit.go` package comment |
| Forge CLI commands, flags, manifest | `cmd/forge/README.md` |
| Three-Layer Architecture (Step → ReAct → App) | Distributed: `loop/doc.go`, `cognitive/doc.go`, `session/doc.go` (already well-covered) |
| Design Principles | Retained briefly in README |
| Relationship to pi.dev | Retained briefly in README |
| Project Status | **Deleted** |

## Requirements

1. README must not exceed ~80-100 lines of prose (excluding the component map table).
2. Every package mentioned in the component map must have a working `pkg.go.dev` link.
3. No information from the current README may be lost; all package-level detail must be discoverable in godoc or dedicated docs.
4. `tool/` must gain a `doc.go` with Registry, Handler, and tool-calling concepts.
5. `x/conduit/` package comment must be enriched with I/O conduit concepts from README.
6. `cmd/forge/README.md` must contain all Forge CLI detail removed from README.
7. `examples/forge/README.md` and `docs/reference/forge-cli.md` must be linked from `cmd/forge/README.md`.
8. The `conduit/` root-level reference in README must be corrected to `x/conduit/`.

## Task Breakdown

### Task 1: Add `tool/` Package Godoc
- **Goal**: Create `tool/doc.go` capturing the tool-calling architecture, Registry, Handler, and examples currently in the README.
- **Dependencies**: None.
- **Files Affected**: `tool/` (currently no doc.go).
- **New Files**: `tool/doc.go`.
- **Interfaces**: No new interfaces; purely documentation.
- **Validation**: `go doc ./tool` renders the package comment. `go test ./tool/...` passes. No compilation errors introduced.
- **Details**: Include: (a) what `tool.Registry` does, (b) what `tool.Handler` implements and how it satisfies `loop.Handler`, (c) the dynamic tool configuration concept (different tools per turn via `openai.WithTools`), (d) a concise code example showing registration and handler wiring. Draw from the "Tool Calling" and "Extension Points → Artifact Handlers" sections of the current README.

### Task 2: Enrich `x/conduit/` Package Godoc
- **Goal**: Expand the package comment in `x/conduit/conduit.go` (or create `x/conduit/doc.go`) to cover I/O conduit architecture from README.
- **Dependencies**: None.
- **Files Affected**: `x/conduit/conduit.go`.
- **New Files**: `x/conduit/doc.go` (preferred if `conduit.go` currently contains the package comment — moving it to `doc.go` is cleaner).
- **Interfaces**: No new interfaces; purely documentation.
- **Validation**: `go doc ./x/conduit` renders the enriched package comment. `go test ./x/conduit/...` passes.
- **Details**: Include: (a) what a Conduit is (ingress/egress adapter, not just a UI), (b) the types of conduits (interactive, event-driven, scheduled, service-oriented, streaming), (c) the `Conduit` interface with `Start(ctx)`, (d) the `Capability` and `Descriptor` types for capability discovery, (e) mention of concrete implementations in `x/conduit/http` and `x/conduit/tui`. Remove any stale references to root-level `conduit/`.

### Task 3: Create `cmd/forge/README.md`
- **Goal**: Extract all Forge CLI detail from the current README into a dedicated `cmd/forge/README.md`.
- **Dependencies**: None.
- **Files Affected**: Current `README.md` (source of content to extract).
- **New Files**: `cmd/forge/README.md`.
- **Interfaces**: No new interfaces; purely documentation.
- **Validation**: Review that all Forge content from README is present: commands (`build`, `generate`, `version`), global flags (`--config`, `--log-level`), backward compatibility note, manifest format YAML block, environment variables, and `--thread` resume note. Internal links to `examples/forge/README.md` and `docs/reference/forge-cli.md` must be valid.
- **Details**: Structure the document as: (1) What Forge is (one paragraph), (2) Commands section with usage examples, (3) Manifest format YAML block, (4) Environment variables table, (5) Links to getting started guide (`examples/forge/README.md`) and generated reference (`docs/reference/forge-cli.md`).

### Task 4: Rewrite `README.md`
- **Goal**: Replace the monolithic README with the concise landing page specified in the blueprint.
- **Dependencies**: Task 1, Task 2, Task 3.
- **Files Affected**: `README.md`.
- **New Files**: None (overwrite existing).
- **Interfaces**: No new interfaces.
- **Validation**: (a) `go build ./...` passes — README changes are markdown-only, but verify no accidental file corruption. (b) Read through the old README side-by-side with the new one; confirm no content is orphaned (all package detail exists in godoc or `cmd/forge/README.md`). (c) All `pkg.go.dev` links use the correct module path `github.com/andrewhowdencom/ore`. (d) No references to root-level `conduit/` remain. (e) Project status section is fully removed.
- **Details**: Draft sections in this order:
  1. **Purpose** — 1-2 paragraphs (reuse current opening but trim).
  2. **Design Principles** — 6 bullets (trimmed from current section).
  3. **Relationship to pi.dev** — 1 paragraph (trimmed).
  4. **Packages** — a markdown table with columns: Package | Description | Docs. Link each package name to `https://pkg.go.dev/github.com/andrewhowdencom/ore/<pkg>`. Packages: `artifact`, `state`, `provider`, `loop`, `tool`, `cognitive`, `thread`, `session`, `x/conduit`, `provider/openai`, `cmd/forge`.
  5. **Getting Started** — brief Forge pointer: "The fastest way to build an agent is with Forge. See `cmd/forge/README.md` for CLI usage and `examples/forge/README.md` for a guided tutorial."
  6. (Optional) **License** — if one exists, keep the reference. If not, omit.

## Dependency Graph

- Task 1 || Task 2 || Task 3 (parallel — independent package and tool docs)
- Task 1 → Task 4
- Task 2 → Task 4
- Task 3 → Task 4

Task 4 must wait until Tasks 1-3 are complete so that (a) all content removed from README has a confirmed new home, and (b) the README can link to the newly created/updated docs.

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| `pkg.go.dev` links are dead because module is not indexed | Low | Medium | Use `https://pkg.go.dev/github.com/andrewhowdencom/ore@latest/<pkg>` which resolves even without semver tags. Add a note that links go live once the module is fetched. |
| Information accidentally lost during README rewrite | Medium | Medium | Task 4 validation explicitly requires side-by-side audit of old vs new README. Builder must verify every removed paragraph exists in godoc or Forge docs. |
| godoc examples contain syntax errors if copied from README | Low | Low | Task 1 and 2 validation runs `go test ./...` which compiles all packages including doc.go comments. |
| `x/conduit/doc.go` conflicts with existing package comment in `conduit.go` | Low | Low | If creating `doc.go`, remove the package comment from `conduit.go` to avoid duplication. |
| Stale `conduit/` root references elsewhere in repo | Medium | Medium | After README rewrite, run `grep -r "\"conduit/" --include="*.go" --include="*.md" .` to find remaining stale references. Create follow-up task if found. |

## Validation Criteria

- [ ] `tool/doc.go` exists and `go doc ./tool` renders meaningful package-level documentation.
- [ ] `x/conduit/doc.go` exists (or `conduit.go` enriched) and `go doc ./x/conduit` renders I/O conduit architecture.
- [ ] `cmd/forge/README.md` exists and contains all Forge CLI content from the old README.
- [ ] `README.md` is under ~100 lines of prose and contains no package-level API detail.
- [ ] `README.md` contains a component map table linking to `pkg.go.dev` for all core packages.
- [ ] No references to root-level `conduit/` package remain in README.
- [ ] The "Project Status" section is fully removed from README.
- [ ] `go test ./...` passes after all changes.
- [ ] Side-by-side review confirms no content from old README is orphaned.
