# Plan: Add Streaming Read, UTF-8 Validation, and Size Cap to ReadFile

## Objective

Replace the current `os.ReadFile` + `strings.Split` implementation of `ReadFile` in `x/tool/filesystem` with a streaming, guarded reader that validates UTF-8 to reject binary files, enforces a 100,000-character total output cap, and preserves existing `offset`/`limit` semantics. This prevents unbounded memory consumption and context-window pollution when the tool is accidentally pointed at large binaries.

## Context

The current `ReadFile` tool (`x/tool/filesystem/filesystem.go:49-90`) loads the entire file into memory with `os.ReadFile`, splits on newlines, then applies `offset`/`limit` windowing. Issue #207 describes a real incident where a ~3.7 MB ELF binary was read in full, tokenised as noise, and rejected by the provider API.

Key observations from the codebase:

- `ReadFile` lives in `x/tool/filesystem/filesystem.go` and is registered alongside `WriteFile`, `EditFile`, `ListDirectory`, and `SearchFiles`.
- `SearchFiles` already uses a line-by-line `bufio.Scanner` (`searchFile`), but `ReadFile` does not.
- The `x/tool/filesystem` package is a separate Go module (`go.mod` at `x/tool/filesystem/go.mod`).
- Existing tests for `ReadFile` cover happy-path, offset/limit combinations, edge cases (empty file, no trailing newline, offset beyond EOF), sandbox integration, and error cases. All must continue to pass.
- Project conventions (`AGENTS.md`) mandate table-driven tests, `go test -race ./...`, and error wrapping with `fmt.Errorf("...: %w", err)`.

## Architectural Blueprint

The redesign is a local, surgical refactor inside `ReadFile`. No new packages or interfaces are required.

**Selected approach: `bufio.Reader` + `ReadString('\n')` line loop**

- Open the file with `os.Open` and wrap it in a `bufio.Reader`.
- Read line-by-line using `ReadString('\n')`, which naturally handles both newline-terminated and non-newline-terminated files.
- Validate each line (minus the trailing newline delimiter) with `utf8.ValidString`. If any chunk fails validation, immediately return an informative error that includes the file size already obtained from the earlier `os.Stat`.
- Maintain line-number state to honour `offset` (1-based, lines before it are skipped) and `limit` (maximum lines to emit).
- Accumulate formatted output (`"<n>|<text>\n"`) in a `strings.Builder`.
- Stop accumulating when the builder length would exceed 100,000 characters; return the truncated result.
- Skip the empty final element produced by a trailing newline, preserving the existing `strings.Split` behaviour.

**Why not `bufio.Scanner`?**
`Scanner` has a hard 64 KB line limit and its split function is less ergonomic for preserving exact line content while also trimming the delimiter. `ReadString('\n')` gives us full control and naturally supports arbitrarily long lines.

**Why UTF-8 as the text gate?**
As argued in the issue: Go source is UTF-8 by spec, Markdown and JSON in this repo are UTF-8, and compiled binaries immediately present null bytes or invalid sequences. Rejection on invalid UTF-8 is a cheap, accurate allowlist that requires no magic-number tables or extension blocklists.

**Why 100,000 characters?**
Large enough for substantial documentation and generated code, small enough to protect the LLM context window. The cap applies to the total formatted output string length.

## Requirements

1. `ReadFile` must stream line-by-line instead of loading the entire file into memory (`os.ReadFile` must be removed from `ReadFile`).
2. Any file containing invalid UTF-8 bytes must be rejected immediately with an informative error that includes the exact file size.
3. Valid UTF-8 files must be capped at a total formatted output of 100,000 characters; truncation may occur at line boundaries.
4. Existing `offset` and `limit` semantics (1-based offset, 0 = unlimited lines) must be preserved for text files.
5. All existing tests in `x/tool/filesystem/filesystem_test.go` must continue to pass.
6. New tests must be added for:
   - Binary file rejection (synthetic invalid UTF-8 / null bytes).
   - Total character cap enforcement on a large text file.
   - Streaming behaviour on a file larger than the cap.

## Task Breakdown

### Task 1: Refactor ReadFile to Stream, Validate UTF-8, and Enforce 100k Cap
- **Goal**: Rewrite the `ReadFile` body in `x/tool/filesystem/filesystem.go` to use `os.Open` + `bufio.Reader` line streaming, `utf8.ValidString` gating, and a 100,000-character output limit while keeping `offset`/`limit` behaviour identical.
- **Dependencies**: None.
- **Files Affected**: `x/tool/filesystem/filesystem.go`
- **New Files**: None.
- **Interfaces**: No new interfaces. `ReadFile` signature remains `func ReadFile(ctx context.Context, sb tool.Sandbox, args map[string]any) (any, error)`.
- **Validation**:
  - `cd x/tool/filesystem && go test ./...` passes (all existing tests).
  - `go test -race ./...` passes.
- **Details**:
  1. Add `io` and `unicode/utf8` to imports.
  2. Keep the initial `resolvePath`, `os.Stat`, and directory check unchanged.
  3. Replace `os.ReadFile` + `strings.Split` with:
     - `f, err := os.Open(path)`; defer close.
     - `reader := bufio.NewReader(f)`.
     - Loop with `line, err := reader.ReadString('\n')`.
     - Trim trailing `\n` (and `\r` if present, to match current CRLF handling).
     - Validate trimmed line with `utf8.ValidString`. On failure, return an error such as `fmt.Errorf("cannot read binary file %s (%.1f MB): invalid UTF-8 detected", path, float64(info.Size()) / (1024*1024))`.
     - Track line number (`lineNum` starting at 1).
     - Skip lines while `lineNum < offset`.
     - If `limit > 0` and `linesEmitted >= limit`, break.
     - If `err == io.EOF` and the trimmed line is empty, break (skip trailing newline artifact).
     - Format each accepted line as `"%d|%s\n"`.
     - Before appending to `strings.Builder`, check whether `builder.Len() + len(formattedLine) > 100_000`. If so, break the loop (truncate at line boundary).
     - Append and increment counters.
     - After the loop, return `builder.String()`, nil.
  4. Ensure no change to `ReadFileTool` schema (the existing `offset`/`limit` properties remain).

### Task 2: Add Guardrail Tests for Binary Rejection and Character Cap
- **Goal**: Extend `x/tool/filesystem/filesystem_test.go` with tests that prove the new UTF-8 gate and 100k cap behave correctly.
- **Dependencies**: Task 1.
- **Files Affected**: `x/tool/filesystem/filesystem_test.go`
- **New Files**: None.
- **Interfaces**: None.
- **Validation**:
  - `cd x/tool/filesystem && go test ./...` passes (all tests, old and new).
  - `go test -race ./...` passes.
- **Details**:
  1. Add `TestReadFile_BinaryRejection`:
     - Write a small file starting with `\x7fELF` or simply containing null bytes (`\x00`) to a temp file.
     - Call `ReadFile` with `offset=1`, `limit=0`.
     - Assert error is non-nil and error message contains `"cannot read binary file"` and the file size.
  2. Add `TestReadFile_TotalCharacterCap`:
     - Write a text file with >100,000 characters (e.g. 2,000 lines of 60 characters = 120,000 chars).
     - Call `ReadFile` with `offset=1`, `limit=0`.
     - Assert result string length is ≤ 100,000.
     - Assert the result ends with a valid line boundary (no mid-line truncation).
  3. Add `TestReadFile_StreamingTruncationWithOffset`:
     - Write a large text file.
     - Call `ReadFile` with a large `offset` (e.g. 500) and no `limit`.
     - Assert the result starts at the correct line number and still respects the 100k cap.
  4. Add `TestReadFile_CapWithLimit`:
     - Write a text file.
     - Call `ReadFile` with a `limit` that would exceed the cap.
     - Assert the cap wins (result length ≤ 100k).
  5. Ensure all new tests use `t.Parallel()` and table-driven style where appropriate, following existing test conventions.

## Dependency Graph

- Task 1 → Task 2 (Task 2 depends on the refactored `ReadFile` implementation)

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| `ReadString('\n')` on a file with no newlines allocates a single huge string that exceeds memory before UTF-8 validation | Medium | Low | `bufio.Reader.ReadString` grows its buffer dynamically; for files with no newlines it will read the entire file. Mitigation: add a per-read size guard (e.g. if a single read exceeds 1 MB, treat as binary/skip). However, the issue explicitly says "No per-line limit". If this becomes a problem, a follow-up issue can introduce a per-read cap. |
| Existing tests rely on exact string matching of line prefixes; a subtle change in newline trimming (CRLF, trailing newline) could break them | Medium | Medium | Carefully replicate the current `strings.Split` semantics. The plan above specifies exact handling of `\r`, `\n`, and EOF. Validation is the full existing test suite. |
| `utf8.ValidString` rejects legitimate non-UTF-8 text encodings (e.g. Latin-1) | Low | Low | Acceptable per issue rationale: this is a Go codebase where all source and docs are UTF-8. Aggressive rejection is preferred over silent noise. |
| 100k cap truncates within a line prefix, producing malformed output | Low | Low | Cap is checked before appending the *entire* formatted line, so truncation always occurs at line boundaries. |

## Validation Criteria

- [ ] `ReadFile` no longer calls `os.ReadFile` or `strings.Split` on the file content.
- [ ] A file containing `\x00` or otherwise invalid UTF-8 is rejected with an error that includes the file path and exact byte size.
- [ ] A valid UTF-8 text file whose total formatted output exceeds 100,000 characters is truncated and the returned string length is ≤ 100,000.
- [ ] Existing `offset`/`limit` semantics are unchanged: `TestReadFile_OffsetAndLimit`, `TestReadFile_OffsetZero`, `TestReadFile_OffsetBeyondEOF`, `TestReadFile_LimitZero`, `TestReadFile_NegativeOffset`, `TestReadFile_ZeroLimit` all pass without modification.
- [ ] All tests in `x/tool/filesystem` pass under `go test ./...` and `go test -race ./...`.
- [ ] New tests exist and pass for: binary rejection, total character cap, streaming truncation with offset, and cap interaction with limit.
