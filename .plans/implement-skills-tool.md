# Plan: implement-skills-tool

## Objective

Create a new `x/tool/skills/` package that implements progressive disclosure of agent skills following the agentskills.io standard. The package provides pluggable skill discovery (filesystem, embedded, well-known HTTP) and exposes three tool functions (`list_skills`, `read_skill`, `search_skills`) that an LLM can invoke to discover and load skill instructions on demand. Skills are knowledge artifacts—not executable functions—so the tool returns SKILL.md content that the LLM references in subsequent reasoning turns.

## Context

The ore framework has an extensible `x/tool/` package hierarchy with concrete tool implementations (`x/tool/bash/`, `x/tool/calculator/`, `x/tool/filesystem/`, `x/tool/mcp/`). Each tool package is a separate Go module, exports `tool.ToolFunc` implementations and `provider.Tool` descriptors, and is registered by the application into a `tool.Registry`.

The agentskills.io standard defines a `SKILL.md` format with YAML frontmatter (name, description, license, compatibility, metadata, allowed-tools) followed by Markdown instructions. Progressive disclosure has three stages: **Discovery** (LLM sees only name + description), **Activation** (LLM reads full SKILL.md), and **Execution** (LLM runs scripts from `scripts/` using other tools). This plan implements Discovery and Activation as tool functions; Execution remains the responsibility of other tools or the LLM itself.

Key patterns observed in the codebase:
- `x/tool/<name>/` packages are separate Go modules with their own `go.mod` and `replace github.com/andrewhowdencom/ore => ../../..`
- Tool functions receive `map[string]any` args and return `(any, error)`
- Package-level `provider.Tool` vars describe JSON schema for each function
- `tool.Registry` binds functions to names; `tool.Handler` executes them against `artifact.ToolCall` artifacts
- `tool.Handler` appends `artifact.ToolResult` as `state.RoleTool` turns, which the OpenAI provider serializes as `tool` messages
- `gopkg.in/yaml.v3` is already available transitively (used by `testify` and `mcp-go`)
- Table-driven tests with `stretchr/testify` are the standard
- Error wrapping uses `fmt.Errorf("...: %w", err)`

## Architectural Blueprint

The `x/tool/skills/` package is a leaf package (no internal ore dependencies beyond `provider/`, `artifact/`, `state/`, `x/tool/`). It consists of four layers:

1. **Discovery Interface** (`Discoverer`) — abstracts where skills live. Three implementations:
   - `FSDiscoverer` — walks a directory tree, finds `SKILL.md` files, parses YAML frontmatter
   - `EmbeddedDiscoverer` — reads from an `embed.FS`
   - `HTTPDiscoverer` — fetches from a `.well-known/agentskills` HTTP endpoint
2. **Catalog** — aggregates results from multiple `Discoverer`s, caches metadata in a `name → entry` map, handles deduplication (first-wins), and provides `List`, `Read`, and `Search`
3. **Toolkit** — binds a `Catalog` to three `tool.ToolFunc` implementations and exports `provider.Tool` descriptors
4. **Tool Surface** — three callable functions the LLM can invoke:
   - `list_skills()` → `[]SkillMeta` (name + description from YAML frontmatter)
   - `read_skill(name)` → full `SKILL.md` content as string
   - `search_skills(query)` → `[]SkillMeta` filtered by case-insensitive substring match on name/description

Skills are not registered as individual tools in the provider. Instead, the single `skills` tool (with its three functions) is registered in the `tool.Registry`. The application is responsible for telling the LLM about available skills (e.g., via system prompt) so the LLM knows to call `list_skills`.

## Requirements

1. Create `x/tool/skills/` as a separate Go module with `go.mod` and `replace` directive
2. Define a `Discoverer` interface with `Discover(ctx) ([]SkillMeta, error)` and `Read(ctx, name) (string, error)` methods
3. Implement `FSDiscoverer` that scans a configurable directory tree for `SKILL.md` files, parses YAML frontmatter, and reads full content on demand
4. Implement `EmbeddedDiscoverer` using `embed.FS`
5. Implement `HTTPDiscoverer` that fetches skill lists and content over HTTP (initial protocol: `GET /skills` returns JSON index, `GET /skills/{name}` returns markdown)
6. Implement `Catalog` that aggregates from multiple `Discoverer`s, caches metadata, handles deduplication (first-wins), and exposes `List`, `Read`, `Search`
7. Implement `Toolkit` that binds a `Catalog` to `tool.ToolFunc` implementations for `list_skills`, `read_skill`, and `search_skills`
8. Export `provider.Tool` descriptors for all three functions with JSON schemas
9. Parse YAML frontmatter using `gopkg.in/yaml.v3`; validate required fields (`name`, `description`)
10. Follow ore error wrapping conventions (`fmt.Errorf("...: %w", err)`)
11. Write table-driven tests for all components; mock filesystem with `testing/fstest` or temp dirs; mock HTTP with `httptest.Server`
12. Add `replace` directive in the main `go.mod`

## Task Breakdown

### Task 1: Bootstrap `x/tool/skills/` Module and Core Types
- **Goal**: Create the new module, define the `Discoverer` interface, `SkillMeta` struct, and `Catalog` skeleton with list/read/search methods (initially returning errors or empty results).
- **Dependencies**: None
- **Files Affected**:
  - `go.mod` (add `replace` directive)
- **New Files**:
  - `x/tool/skills/go.mod`
  - `x/tool/skills/doc.go`
  - `x/tool/skills/types.go`
- **Interfaces**:
  ```go
  type Discoverer interface {
      Discover(ctx context.Context) ([]SkillMeta, error)
      Read(ctx context.Context, name string) (string, error)
  }

  type SkillMeta struct {
      Name        string
      Description string
  }

  type Catalog struct { /* fields TBD */ }
  func NewCatalog(discoverers ...Discoverer) *Catalog
  func (c *Catalog) List(ctx context.Context) ([]SkillMeta, error)
  func (c *Catalog) Read(ctx context.Context, name string) (string, error)
  func (c *Catalog) Search(ctx context.Context, query string) ([]SkillMeta, error)
  ```
- **Validation**:
  - `cd x/tool/skills && go mod tidy` completes without error
  - `go build ./x/tool/skills` compiles
  - `go test ./x/tool/skills/...` runs (tests may be stubs or skipped)
- **Details**: The `go.mod` must follow the submodule pattern: `module github.com/andrewhowdencom/ore/x/tool/skills`, `go 1.26.2`, and `replace github.com/andrewhowdencom/ore => ../../..`. The `Catalog` skeleton should have the correct method signatures and a mutex for thread safety, even if implementations are stubs. `doc.go` should document the package purpose and the progressive disclosure pattern.

### Task 2: Implement Filesystem and Embedded Discoverers
- **Goal**: Build two concrete `Discoverer` implementations that read `SKILL.md` files from the local filesystem and from an embedded file system, including YAML frontmatter parsing.
- **Dependencies**: Task 1
- **Files Affected**:
  - `x/tool/skills/types.go` (refine Catalog implementation if needed)
- **New Files**:
  - `x/tool/skills/discoverer_fs.go`
  - `x/tool/skills/discoverer_embed.go`
  - `x/tool/skills/discoverer_test.go`
  - `x/tool/skills/testdata/` (embedded test fixtures)
- **Interfaces**:
  ```go
  type FSDiscoverer struct {
      Root string
  }
  func NewFSDiscoverer(root string) *FSDiscoverer

  type EmbeddedDiscoverer struct {
      FS embed.FS
      Root string // optional subpath within embed.FS
  }
  func NewEmbeddedDiscoverer(fs embed.FS, root string) *EmbeddedDiscoverer
  ```
- **Validation**:
  - `go test -race ./x/tool/skills/...` passes for all discoverer tests
  - Tests cover: happy path discovery, missing SKILL.md, invalid YAML frontmatter, read by name, read nonexistent skill
- **Details**:
  - `FSDiscoverer.Discover` walks `Root` recursively. For each directory containing `SKILL.md`, it reads the file, splits on `---\n` to isolate YAML frontmatter, parses it with `gopkg.in/yaml.v3`, validates `name` and `description` are non-empty, and stores a `name → path` mapping.
  - `FSDiscoverer.Read` looks up the path by name and returns the full file content.
  - `EmbeddedDiscoverer` follows the same pattern using `embed.FS`.
  - YAML parsing uses a private `skillFrontmatter` struct with `yaml` tags. Only `name` and `description` are required; other fields are ignored for now.
  - If a `SKILL.md` is missing required fields, it is skipped with a logged warning (use `log/slog`) rather than failing the entire discovery.
  - Include test fixtures in `testdata/fs/skills/conduit/SKILL.md` and `testdata/embed/skills/go/SKILL.md` matching the format found in `.agents/skills/conduit/SKILL.md`.

### Task 3: Implement Well-Known HTTP Discoverer
- **Goal**: Build an `HTTPDiscoverer` that fetches skill metadata and content from a remote HTTP endpoint.
- **Dependencies**: Task 1
- **Files Affected**: None
- **New Files**:
  - `x/tool/skills/discoverer_http.go`
  - `x/tool/skills/discoverer_http_test.go`
- **Interfaces**:
  ```go
  type HTTPDiscoverer struct {
      BaseURL string
      Client  *http.Client // optional; defaults to http.DefaultClient
  }
  func NewHTTPDiscoverer(baseURL string) *HTTPDiscoverer
  ```
- **Validation**:
  - `go test -race ./x/tool/skills/...` passes for HTTP discoverer tests
  - Tests cover: happy path discovery, happy path read, 404 on read, invalid JSON index, network error
- **Details**:
  - `Discover` performs `GET {BaseURL}/skills` and expects JSON: `{"skills": [{"name": "...", "description": "...", "url": "..."}]}`.
  - `Read` performs `GET {BaseURL}/skills/{name}` and returns the response body as a string.
  - Use `context.Context` for request cancellation and timeouts.
  - Wrap all errors with `fmt.Errorf`.
  - This protocol is a **provisional default** (see Risks). Document in code comments that the well-known protocol may change.

### Task 4: Implement Catalog Aggregation, Search, and Caching
- **Goal**: Complete the `Catalog` implementation so it merges results from multiple discoverers, deduplicates by name (first-wins), caches metadata, and provides substring search.
- **Dependencies**: Task 1, Task 2, Task 3
- **Files Affected**:
  - `x/tool/skills/types.go`
- **New Files**:
  - `x/tool/skills/catalog_test.go`
- **Interfaces**: No new exported interfaces; `Catalog.List`, `Catalog.Read`, `Catalog.Search` are fully implemented.
- **Validation**:
  - `go test -race ./x/tool/skills/...` passes for catalog tests
  - Tests cover: single discoverer, multiple discoverers, duplicate names (first-wins), search with matches, search with no matches, read after refresh, read nonexistent skill
- **Details**:
  - `Catalog.refresh(ctx)` calls `Discover` on all registered discoverers, builds a `map[string]discovererEntry` (name → metadata + discoverer index), and caches it under a write lock.
  - `List` refreshes the cache if empty, then returns a deterministically sorted slice of all `SkillMeta`.
  - `Read` refreshes the cache if the name is missing, looks up the discoverer index, and delegates to that discoverer’s `Read`. Returns `fmt.Errorf("skill %q not found", name)` if still missing.
  - `Search` calls `List`, then filters by case-insensitive substring match on `Name` and `Description`.
  - Use `sync.RWMutex` for cache safety. `refresh` should skip individual discoverer errors rather than failing entirely (log with `slog.Warn`), so one bad discoverer doesn't break the whole catalog.

### Task 5: Implement Tool Functions, Toolkit, and Descriptors
- **Goal**: Create the `Toolkit` type that binds a `Catalog` to three `tool.ToolFunc` implementations and exports `provider.Tool` descriptors.
- **Dependencies**: Task 4
- **Files Affected**: None
- **New Files**:
  - `x/tool/skills/tool.go`
  - `x/tool/skills/tool_test.go`
- **Interfaces**:
  ```go
  type Toolkit struct {
      catalog *Catalog
  }
  func NewToolkit(discoverers ...Discoverer) *Toolkit

  // ToolFunc implementations (methods on Toolkit)
  func (t *Toolkit) ListSkills(ctx context.Context, args map[string]any) (any, error)
  func (t *Toolkit) ReadSkill(ctx context.Context, args map[string]any) (any, error)
  func (t *Toolkit) SearchSkills(ctx context.Context, args map[string]any) (any, error)

  // Package-level provider.Tool descriptors
  var ListSkillsTool provider.Tool
  var ReadSkillTool provider.Tool
  var SearchSkillsTool provider.Tool

  // Optional convenience
  func (t *Toolkit) Register(registry *tool.Registry)
  ```
- **Validation**:
  - `go test -race ./x/tool/skills/...` passes for all tool tests
  - Tests cover: list skills, read skill by name, search skills with query, missing required args, read nonexistent skill
- **Details**:
  - `ListSkills` ignores args and returns `t.catalog.List(ctx)`.
  - `ReadSkill` extracts `"name"` from args (required), returns `t.catalog.Read(ctx, name)`.
  - `SearchSkills` extracts `"query"` from args (required), returns `t.catalog.Search(ctx, query)`.
  - `provider.Tool` descriptors must have correct JSON schemas:
    - `list_skills`: no required parameters
    - `read_skill`: required `name` (string)
    - `search_skills`: required `query` (string)
  - Include a private `toString` helper for safe `map[string]any` string extraction (same pattern as `x/tool/filesystem/` and `x/tool/bash/`).
  - `Toolkit.Register` is optional but convenient: it calls `registry.Register` for all three tools in one go.

### Task 6: Finalize Documentation and Full-Repo Validation
- **Goal**: Ensure package documentation is complete, error messages follow conventions, and the full repository test suite remains green.
- **Dependencies**: Task 5
- **Files Affected**:
  - `x/tool/skills/doc.go` (refine)
- **New Files**: None
- **Validation**:
  - `go test -race ./...` from repo root passes
  - `go mod tidy` in `x/tool/skills/` produces no changes
  - `go vet ./x/tool/skills/...` is clean
  - `doc.go` documents the progressive disclosure pattern, the three tool functions, the Discoverer interface, and how applications compose the toolkit into a `tool.Registry`
- **Details**:
  - Review all error messages for `fmt.Errorf("...: %w", err)` wrapping.
  - Ensure no unused imports or variables.
  - Verify the main `go.mod` has the `replace` directive for `x/tool/skills`.
  - Document the bootstrap problem in `doc.go`: the application must tell the LLM about the `skills` tool (e.g., via system prompt) so it knows to call `list_skills`.

## Dependency Graph

- Task 1 → Task 2
- Task 1 → Task 3
- Task 2 → Task 4
- Task 3 → Task 4
- Task 4 → Task 5
- Task 5 → Task 6

Task 2 and Task 3 are parallelizable (both depend only on Task 1).

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Well-known HTTP protocol is not standardized | Medium | High | Task 3 implements a provisional protocol (`GET /skills` for index, `GET /skills/{name}` for content) and documents it as experimental. Future iterations can adapt when the standard stabilizes. |
| YAML frontmatter parsing is fragile (malformed SKILL.md files) | Low | Medium | Skip malformed files during discovery with a warning log rather than failing the entire catalog. Document this behavior in `doc.go`. |
| Skill name collisions across discoverers | Low | Medium | Catalog uses first-wins deduplication. Document this in `doc.go` and log a warning when duplicates are detected. |
| Large SKILL.md files bloat context window | Medium | Low | Out of scope for the tool—the agentskills.io standard recommends <500 lines. The application or provider layer handles context window limits. Document the standard recommendation in `doc.go`. |
| `gopkg.in/yaml.v3` version conflicts between submodules | Low | Low | Use the same version as other submodules (v3.0.1). `go mod tidy` should resolve consistently. |

## Validation Criteria

- [ ] `x/tool/skills/go.mod` exists and follows the submodule pattern
- [ ] `go build ./x/tool/skills` compiles without errors
- [ ] `go test -race ./x/tool/skills/...` passes with >80% coverage
- [ ] `go test -race ./...` from repo root passes (no regressions)
- [ ] `go vet ./x/tool/skills/...` is clean
- [ ] `doc.go` documents the package, progressive disclosure pattern, and composition instructions
- [ ] All three tool functions have corresponding `provider.Tool` descriptors with JSON schemas
- [ ] All three discoverer implementations have unit tests
- [ ] Catalog handles deduplication, caching, and search correctly
- [ ] Error messages use `fmt.Errorf("...: %w", err)` wrapping throughout
