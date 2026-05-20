# Plan: Add Filesystem Tools for Coding Agents

## Objective
Create a new `x/tool/filesystem/` package that exports five filesystem interaction tools (`read_file`, `write_file`, `edit_file`, `list_directory`, `search_files`) following the existing `x/tool/calculator/` pattern. Each tool is an independently registered `tool.ToolFunc` with a `provider.Tool` JSON-schema descriptor. Add an integration example under `examples/filesystem/` demonstrating registration in a `cognitive.ReAct` loop.

## Context
- **Reference pattern**: `x/tool/calculator/` (`calculator.go`, `calculator_test.go`, `doc.go`, `go.mod`) defines two `tool.ToolFunc` implementations (`Add`, `Multiply`) and their `provider.Tool` descriptors (`AddTool`, `MultiplyTool`).
- **Tool contract**: `tool.ToolFunc` is `func(ctx context.Context, args map[string]any) (any, error)` — parsed JSON arguments in, any JSON-serializable result out.
- **Descriptor contract**: `provider.Tool` is `{Name, Description, Schema map[string]any}` — each concrete provider adapter maps this to its native API.
- **Handler execution**: `tool.Handler` (from `x/tool/handler.go`) implements `loop.Handler`. It detects `artifact.ToolCall`, looks up the tool by name, executes the `ToolFunc`, and appends a `state.RoleTool` turn with `artifact.ToolResult`.
- **Module setup**: The root `go.mod` uses `replace` directives for workspace submodules; `go.work` lists all workspace modules. The calculator submodule is at `github.com/andrewhowdencom/ore/x/tool/calculator` with `replace => ./x/tool/calculator`.
- **Example pattern**: `examples/calculator/main.go` wires tools into a `cognitive.ReAct` loop with an OpenAI provider, reading user input from CLI args or stdin.
- **AGENTS.md conventions**: Prefer standard library only. Table-driven tests. Functional options pattern where applicable. Wrap errors with `fmt.Errorf("...: %w", err)`.

## Architectural Blueprint
A new leaf submodule `x/tool/filesystem/` provides five `tool.ToolFunc` implementations and matching `provider.Tool` descriptors. The package uses only the Go standard library (`os`, `bufio`, `regexp`, `path/filepath`, `fmt`, `context`). Each tool returns a plain Go value that JSON-serializes cleanly for LLM consumption:
- `read_file` → `string` (line-number-prefixed content)
- `write_file` → `string` (success confirmation)
- `edit_file` → `string` (success confirmation)
- `list_directory` → `[]string` (entry names)
- `search_files` → `[]SearchResult` (`{Path string, LineNumber int, Content string}`)

An integration example at `examples/filesystem/` copies the calculator example pattern, registering all five tools in a `tool.Registry` and running a `cognitive.ReAct` loop.

## Requirements
1. Create `x/tool/filesystem/` Go submodule and add it to the workspace (`go.work`, root `go.mod`).
2. Implement `read_file` with optional `offset` (1-based) and `limit`, returning line-number-prefixed content.
3. Implement `write_file` that creates a new file and **fails if the path already exists** (forces `edit_file` for modifications).
4. Implement `edit_file` with exact-match `old_string` → `new_string` replacement; replace the **first** occurrence for predictable behavior.
5. Implement `list_directory` with shallow, non-hidden entry listing.
6. Implement `search_files` with regex search across file(s), returning path + line number + matching line content.
7. Export a `provider.Tool` descriptor for each tool.
8. Provide table-driven unit tests for all tools using `t.TempDir()` for filesystem isolation.
9. Provide `doc.go` with package documentation and usage example.
10. Provide an integration example at `examples/filesystem/main.go` demonstrating tool registration with a `cognitive.ReAct` loop.

## Task Breakdown

### Task 1: Package Scaffold and read_file
- **Goal**: Create the `x/tool/filesystem/` module and implement the `read_file` tool.
- **Dependencies**: None
- **Files Affected**: `go.mod` (root), `go.work`
- **New Files**: `x/tool/filesystem/go.mod`, `x/tool/filesystem/doc.go`, `x/tool/filesystem/filesystem.go`, `x/tool/filesystem/filesystem_test.go`
- **Interfaces**:
  - `ReadFile(ctx context.Context, args map[string]any) (any, error)` — `tool.ToolFunc`
  - `ReadFileTool provider.Tool` — descriptor
  - Helper: `toInt(v any, def int) int` — safe numeric arg extraction with default
- **Validation**:
  - `go test ./x/tool/filesystem/...` passes
  - `go test -race ./x/tool/filesystem/...` passes
  - `go work sync` succeeds
- **Details**:
  - Add `require github.com/andrewhowdencom/ore/x/tool/filesystem v0.0.0-00010101000000-000000000000` and `replace github.com/andrewhowdencom/ore/x/tool/filesystem => ./x/tool/filesystem` in root `go.mod`.
  - Add `./x/tool/filesystem` to `go.work`.
  - `x/tool/filesystem/go.mod`: module path `github.com/andrewhowdencom/ore/x/tool/filesystem`, Go 1.26.2, `replace github.com/andrewhowdencom/ore => ../../..`, `require github.com/andrewhowdencom/ore` and `github.com/stretchr/testify`.
  - `doc.go`: follow `x/tool/calculator/doc.go` pattern — package docs + usage snippet showing registry registration.
  - `read_file` parameters:
    - `path` (string, required) — relative or absolute file path.
    - `offset` (number, optional, default `1`) — 1-based starting line.
    - `limit` (number, optional, default `0` meaning no limit) — maximum lines to return.
  - Read file with `os.ReadFile`, split into lines, prefix each returned line with `fmt.Sprintf("%d|%s", lineNum, content)`.
  - Return clear errors for: missing file, path is a directory, permission denied.
  - Table-driven tests in `filesystem_test.go` using `t.TempDir()`:
    - Happy path: read entire file, verify line prefixes.
    - Offset + limit: read subset, verify correct lines.
    - Missing file: expect error.
    - Directory path: expect error.
    - Empty file: returns empty string.

### Task 2: write_file
- **Goal**: Implement the `write_file` tool with "fail if exists" safety.
- **Dependencies**: Task 1
- **Files Affected**: `x/tool/filesystem/filesystem.go`, `x/tool/filesystem/filesystem_test.go`
- **New Files**: None
- **Interfaces**:
  - `WriteFile(ctx context.Context, args map[string]any) (any, error)`
  - `WriteFileTool provider.Tool`
- **Validation**: `go test -race ./x/tool/filesystem/...` passes
- **Details**:
  - Parameters: `path` (string, required), `content` (string, required).
  - Check existence with `os.Stat`; if exists (file or directory), return error: `fmt.Errorf("path %q already exists", path)`.
  - Create parent directories with `os.MkdirAll`.
  - Write content with `os.WriteFile`.
  - Return success confirmation string, e.g., `fmt.Sprintf("wrote %d bytes to %q", len(content), path)`.
  - Tests:
    - Create new file in existing directory.
    - Create nested path (parent directories created).
    - Fail when file already exists.
    - Fail when directory already exists at path.
    - Write empty content.

### Task 3: edit_file
- **Goal**: Implement `edit_file` with exact-match search-and-replace.
- **Dependencies**: Task 2
- **Files Affected**: `x/tool/filesystem/filesystem.go`, `x/tool/filesystem/filesystem_test.go`
- **New Files**: None
- **Interfaces**:
  - `EditFile(ctx context.Context, args map[string]any) (any, error)`
  - `EditFileTool provider.Tool`
- **Validation**: `go test -race ./x/tool/filesystem/...` passes
- **Details**:
  - Parameters: `path` (string, required), `old_string` (string, required), `new_string` (string, required).
  - Read entire file content with `os.ReadFile`.
  - If `old_string` is empty, return error: `fmt.Errorf("old_string cannot be empty")`.
  - Find the **first** exact occurrence of `old_string` using `strings.Index`.
  - If not found, return error: `fmt.Errorf("old_string not found in %q", path)`.
  - Replace the first occurrence with `new_string`.
  - Write back with `os.WriteFile`.
  - Return success confirmation string, e.g., `fmt.Sprintf("edited %q", path)`.
  - Tests:
    - Simple single-line replacement.
    - Multi-line string replacement (old_string spans lines).
    - Error when `old_string` is empty.
    - Error when `old_string` is not found.
    - Error when file does not exist.
    - Verify only first occurrence is replaced when multiple identical occurrences exist.

### Task 4: list_directory
- **Goal**: Implement `list_directory` with shallow, non-hidden entry listing.
- **Dependencies**: Task 3
- **Files Affected**: `x/tool/filesystem/filesystem.go`, `x/tool/filesystem/filesystem_test.go`
- **New Files**: None
- **Interfaces**:
  - `ListDirectory(ctx context.Context, args map[string]any) (any, error)`
  - `ListDirectoryTool provider.Tool`
- **Validation**: `go test -race ./x/tool/filesystem/...` passes
- **Details**:
  - Parameters: `path` (string, required).
  - Use `os.ReadDir` for shallow listing (immediate children only).
  - Filter out hidden entries: `strings.HasPrefix(entry.Name(), ".")`.
  - Return `[]string` of entry names sorted alphabetically.
  - Return clear errors for: missing path, path is a file.
  - Tests:
    - List directory with mixed files and subdirectories.
    - Hidden entries (`.git`, `.hidden`) are excluded.
    - Empty directory returns empty slice.
    - Missing path returns error.
    - File-as-path returns error.

### Task 5: search_files
- **Goal**: Implement `search_files` with regex search across file(s).
- **Dependencies**: Task 4
- **Files Affected**: `x/tool/filesystem/filesystem.go`, `x/tool/filesystem/filesystem_test.go`
- **New Files**: None
- **Interfaces**:
  - `SearchFiles(ctx context.Context, args map[string]any) (any, error)`
  - `SearchFilesTool provider.Tool`
  - `type SearchResult struct { Path string; LineNumber int; Content string }`
- **Validation**: `go test -race ./x/tool/filesystem/...` passes
- **Details**:
  - Parameters: `path` (string, required), `query` (string, required).
  - Compile regex with `regexp.Compile(query)`; if invalid, return error: `fmt.Errorf("invalid regex: %w", err)`.
  - If `path` is a file, search only that file.
  - If `path` is a directory, walk recursively with `filepath.WalkDir`, skipping hidden directories and files (names starting with `.`).
  - For each file, read line-by-line and test each line against the regex.
  - Return `[]SearchResult` where each match includes the file path, 1-based line number, and the matching line content.
  - Limit result count? Not required for initial implementation; return all matches.
  - Tests:
    - Single file search with simple regex.
    - Directory recursive search.
    - No matches returns empty slice (not error).
    - Invalid regex returns error.
    - Hidden files/directories are skipped.

### Task 6: Integration Example
- **Goal**: Add an example application demonstrating filesystem tool registration in a coding agent loop.
- **Dependencies**: Task 5 (all tools exported)
- **Files Affected**: None
- **New Files**: `examples/filesystem/main.go`
- **Interfaces**: None new
- **Validation**: `go build ./examples/filesystem/` succeeds
- **Details**:
  - Copy the structure from `examples/calculator/main.go`.
  - Import `github.com/andrewhowdencom/ore/x/tool/filesystem`.
  - Create `tool.NewRegistry()` and register all five tools using their descriptors and functions:
    - `registry.Register(filesystem.ReadFileTool.Name, filesystem.ReadFileTool.Description, filesystem.ReadFileTool.Schema, filesystem.ReadFile)`
    - Repeat for `WriteFile`, `EditFile`, `ListDirectory`, `SearchFiles`.
  - Wire the registry into `loop.New(loop.WithHandlers(registry.Handler()), loop.WithInvokeOptions(openai.WithTools(registry.Tools())))`.
  - Use `cognitive.ReAct` to run the loop, reading user input from CLI args or stdin.
  - The example binary does not need its own `go.mod`; it is part of the root module (same pattern as `examples/calculator/`).

## Dependency Graph
- Task 1 → Task 2 → Task 3 → Task 4 → Task 5
- Task 5 → Task 6

## Risks & Mitigations
| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Exact-match fragility confuses LLM | Medium | Medium | Document limitation in tool descriptions; clear error messages guide LLM to correct usage |
| Invalid regex from LLM | Low | Low | Validate with `regexp.Compile`; return descriptive error so LLM can retry |
| Agent overwrites critical files | Low | Low | `write_file` fails on existing files; `edit_file` requires explicit `old_string` |
| Path traversal outside intended workspace | Medium | Low | Document that paths are relative to working directory; future iteration may add chroot/allowlist |
| Large directory trees cause slow search | Low | Low | Acceptable for MVP; future iteration may add file-type filters or depth limits |

## Validation Criteria
- [ ] `go test -race ./x/tool/filesystem/...` passes
- [ ] `go test ./x/tool/filesystem/...` passes with 100% of tool functions covered
- [ ] `go work sync` succeeds after adding the new module
- [ ] Root `go.mod` contains require + replace for `github.com/andrewhowdencom/ore/x/tool/filesystem`
- [ ] `go.work` includes `./x/tool/filesystem`
- [ ] `go build ./examples/filesystem/` succeeds
- [ ] Package `doc.go` includes usage snippet matching calculator pattern
- [ ] All five tools have exported `provider.Tool` descriptors and `tool.ToolFunc` implementations
