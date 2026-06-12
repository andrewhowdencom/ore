# Plan: Rewrite x/tool with Default Truncation and Streaming Bash

## Objective

Make the tool layer **default to safe** by introducing a structured `Format` contract on `tool.Tool`, a head/tail byte+line truncator with UTF-8 safety, and an integration point in the framework `Handler` that bounds every LLM-facing result unless a tool explicitly opts out via `LLMRenderer`. Replace `bytes.Buffer`-based output capture in `x/tool/bash` with a streaming, bounded-memory accumulator that spills full output to a temp file. Migrate every built-in tool to the new contract, rewrite tool descriptions to expose limits and recovery hints, and ship unit + integration tests that prove a 10 MB result and a multi-GB subprocess cannot OOM the host or bloat per-turn cost.

## Context

**Repository state** (verified via `git status` and `git log` on branch `421`):
- HEAD: `7078600 chore(release): bump github.com/andrewhowdencom/ore/examples to v0.5.0`. Tree is clean.
- Branch: `421` (worktree-isolated work on issue #421).
- `go.work` lists 15 workspace modules; the relevant ones for this plan are:
  - `tool/` (core, leaf) — defines `Tool`, `ToolFunc`, `Sandbox`, `Example`.
  - `x/tool/` (framework-level) — defines `Handler`, `Registry`, `ToolFilter`.
  - `x/tool/{bash,filesystem,skills,calculator,set_title,mcp}/` (concrete tools).

**Current pain points observed in code** (per the issue body):
- `x/tool/bash/result.go` — `Result{Stdout, Stderr, ExitCode}` has no cap. JSON-marshaled into `ToolResult.Content` and sent on every turn.
- `x/tool/bash/bash_unix.go:35-37` (and `bash_windows.go`) — `var stdoutBuf, stderrBuf bytes.Buffer` accumulates the full subprocess output in the host heap.
- `x/tool/filesystem/filesystem.go` — `ReadFile` (line ~85) reads the whole file before formatting; `SearchFiles` (line ~290) and `ListDirectory` (line ~245) have no row caps.
- `x/tool/skills/tool.go` — `ReadSkill` returns full `SKILL.md` content with no cap.
- `artifact/artifact.go:154` — `LLMRenderer` interface exists as the only escape hatch; `x/provider/openai/` uses it; no built-in tool implements it.
- All tool `Description` strings are prose without limits or recovery hints.

**Architectural conventions from `AGENTS.md`**:
- Core leaf packages (`artifact/`, `state/`, `provider/`, `tool/`) hold contracts only; framework-level defaults live outside (e.g., `x/tool/handler.go`, `x/tool/filter.go`).
- No backwards compatibility is required (project is pre-production, in-development). Prefer aggressive refactoring.
- Functional options pattern for constructor configuration.
- Table-driven tests; `go test -race ./...` is the validation gate.
- `MUST NOT` add new dependencies to core packages; provider adapters use only `net/http` and `encoding/json` from stdlib. The truncator will be stdlib-only.
- `tool.ToolFunc` returns `any`; the framework's responsibility is to serialize the result for the LLM. This is the seam the new contract lives on.
- `StatusContributor` interface in `artifact` is the existing precedent for value-carried observability metadata.

**Reference patterns from prior plans** (e.g., `.plans/add-filesystem-tools.md`):
- Workspace module setup (add entry to root `go.mod` `require` + `replace`, to `go.work`).
- `Taskfile.lib.yml` includes in root `Taskfile.yml` for `task validate`.
- Tool submodules each have their own `go.mod` and follow the `x/tool/calculator/` pattern: `tool.go`, `*_test.go`, `doc.go`, `go.mod`, `go.sum`.

## Architectural Blueprint

**Three-layer change:**

1. **Contract layer (`tool/`, `artifact/`)** — Extend `tool.Tool` with a `Format Format` field. Extend `artifact.ToolResult` with a `Truncation *Truncation` field. Add the supporting types (`TruncateConfig`, `TruncationStyle`, `Truncation`). The contract is opt-in at the field level: zero values mean "use framework defaults."

2. **Default implementation layer (`x/tool/truncate/`)** — A new sibling package to `x/tool/handler.go` that implements the default truncation algorithm. Head + tail truncation, byte + line caps, UTF-8 boundary safe, produces a `Truncation` struct. Includes a simple `{name}` template substitution for `RecoveryHint`. No external deps.

3. **Integration layer (`x/tool/handler.go`)** — Update the handler so that, after a `ToolFunc` returns:
   - If the value implements `artifact.LLMRenderer`, use `MarshalLLM()` as-is (explicit opt-out).
   - Otherwise, `json.Marshal` the value, then apply `Format.Truncate` (falling back to framework defaults of 50 KB / 2000 lines, tail style) using the tool's registered `Format`.
   - If truncation occurred, render the tool's `Format.RecoveryHint` template against the truncation metadata, append the standard "X lines shown of Y total" notice, and set `Truncation` on the emitted `ToolResult`.

**Tool migration (in this order):**
- `x/tool/bash/` — `Bash` returns a `Result` carrying `Truncation`, `StdoutPath`, `StderrPath`. `runCommand` (Unix + Windows) is rewritten to use a `BoundedBuffer` (rolling 2× tail + temp file spill). `Result` gains `MarshalLLM() string`. **This subsumes the work tracked in #424.**
- `x/tool/filesystem/` — `ReadFile` byte cap with temp-file fallback; `SearchFiles` and `ListDirectory` row caps.
- `x/tool/skills/` — `read_skill` byte cap with temp-file fallback.
- `x/tool/calculator/`, `x/tool/set_title/`, `x/tool/{filter,filter_test}.go` — adopt the `Format` field shape with a `NoTruncate` declaration (zero caps = framework no-op, but the field is populated for documentation). Descriptions rewritten to structured form.

**Single PR, internally consistent.** Per the issue: "we don't ship a half-rewritten tool layer."

## Requirements

1. **`tool.Tool` gains a `Format Format` field** with zero-value defaulting to framework behavior. [explicit, issue §"New contract (sketch)"]
2. **`Format` carries `Truncate TruncateConfig`, `Style TruncationStyle`, `RecoveryHint string`.** `TruncateConfig` is `{MaxBytes int, MaxLines int}` with zero values meaning "use framework defaults." `TruncationStyle` is an enum (`StyleTail` default, `StyleHead`). [explicit, issue §"New contract (sketch)"]
3. **Framework defaults: 50 KB byte cap, 2000 line cap, tail style.** Mirrors pi's `truncate.ts`. [explicit, issue §"Goals" #1]
4. **`artifact.ToolResult` gains `Truncation *Truncation`** (pointer, `omitempty`). `Truncation` has `OriginalBytes int`, `OriginalLines int`, `ShownBytes int`, `ShownLines int`, `Style string`, `RecoveryHint string`. [explicit, issue §"Framework"]
5. **`x/tool/handler.go` `Handler.Handle` applies the format** in this order: check `LLMRenderer` → marshal JSON → truncate (head or tail) → render `RecoveryHint` template → set `Truncation` on result. [explicit, issue §"Framework"]
6. **`x/tool/truncate/` package** with: `Truncate(s string, cfg TruncateConfig, style TruncationStyle) (string, Truncation)`, `RenderHint(template string, meta Truncation) string`. UTF-8 boundary safe; byte cap cannot split a multi-byte rune; line cap counts newlines correctly when no `\n` terminator. [explicit, issue §"Framework"]
7. **`x/tool/bash` streaming accumulator:** a `BoundedBuffer` type with `Write([]byte) (int, error)`, `Bytes() []byte`, `String() string`, `Path() string` methods. Maintains a rolling 2× cap tail; spills the full byte stream to a temp file (created on first overflow). `runCommand` (Unix + Windows) is updated to use `BoundedBuffer` instead of `bytes.Buffer`. [explicit, issue §"Built-in tools to migrate"]
8. **`x/tool/bash/result.go` gains `Truncation *Truncation`, `StdoutPath string`, `StderrPath string`, and `MarshalLLM() string`** that returns the truncated tail plus a recovery hint. [explicit, issue §"Built-in tools to migrate"]
9. **`x/tool/filesystem/read_file` byte cap** (50 KB default) with temp-file fallback. `ReadFileTool.Format.RecoveryHint = "Use offset={next_offset} to continue reading."` [explicit, issue §"Built-in tools to migrate"]
10. **`x/tool/filesystem/search_files` row cap** (1000 default) + `Format.RecoveryHint = "Use limit=2N to see more results."` [explicit, issue §"Built-in tools to migrate"]
11. **`x/tool/filesystem/list_directory` row cap** (500 default) + same recovery hint. [explicit, issue §"Built-in tools to migrate"]
12. **`x/tool/skills/read_skill` byte cap** (50 KB default) with temp-file fallback. [explicit, issue §"Built-in tools to migrate"]
13. **All tool `Description` strings rewritten** to a structured form: one-line summary, output limits, recovery hint. [explicit, issue §"Tool descriptions"]
14. **Unit tests for the truncator:** head/tail, byte cap, line cap, UTF-8 boundary safety (multi-byte rune at cut site), no-truncation path, empty input, zero-cap defaults. [explicit, issue §"Tests"]
15. **Handler tests** demonstrating: 10 MB string result is truncated to ≤ 50 KB; a tool returning a value that implements `LLMRenderer` is NOT truncated; a tool with explicit `Format.Truncate` overrides the framework default; namespaced (MCP) results are also subject to truncation. [explicit, issue §"Tests"]
16. **Per-tool tests** for new `Format` declarations and behavior. Existing tests must continue to pass. [explicit, issue §"Tests"]
17. **Integration smoke test** (under `x/tool/bash/` or `x/tool/`) that runs a known large-output command (e.g., `dd if=/dev/zero bs=1M count=100 2>/dev/null | base64`) and asserts the resulting `ToolResult` is bounded and `Truncation` is non-nil. [explicit, issue §"Tests", inferred for OSS context since "workshop" is internal]

## Task Breakdown

### Task 1: Framework contract — `Format`, `TruncateConfig`, `TruncationStyle`, `Truncation`

- **Goal**: Extend the core `tool` and `artifact` packages with the types needed by the new contract.
- **Dependencies**: None
- **Files Affected**:
  - `tool/tool.go` — add `Format` field to `Tool` struct
  - `artifact/artifact.go` — add `Truncation` field to `ToolResult`, add `Truncation` struct
- **New Files**:
  - `tool/format.go` — defines `Format`, `TruncateConfig`, `TruncationStyle` (constants `StyleHead`, `StyleTail`), `DefaultTruncateConfig()` returning `{MaxBytes: 50_000, MaxLines: 2000}`
  - `artifact/truncation.go` — defines the `Truncation` struct (`OriginalBytes int`, `OriginalLines int`, `ShownBytes int`, `ShownLines int`, `Style string`, `RecoveryHint string`, plus `MarshalJSON` with `omitempty` semantics)
- **Interfaces**:
  ```go
  // In tool/format.go
  type Format struct {
      Truncate     TruncateConfig
      Style        TruncationStyle
      RecoveryHint string
  }
  type TruncateConfig struct {
      MaxBytes int
      MaxLines int
  }
  type TruncationStyle int
  const (
      StyleTail TruncationStyle = iota
      StyleHead
  )
  func DefaultTruncateConfig() TruncateConfig {
      return TruncateConfig{MaxBytes: 50_000, MaxLines: 2000}
  }

  // In artifact/truncation.go
  type Truncation struct {
      OriginalBytes int    `json:"original_bytes,omitempty"`
      OriginalLines int    `json:"original_lines,omitempty"`
      ShownBytes    int    `json:"shown_bytes"`
      ShownLines    int    `json:"shown_lines"`
      Style         string `json:"style,omitempty"`
      RecoveryHint  string `json:"recovery_hint,omitempty"`
  }
  ```
- **Validation**:
  - `go build ./...` from the root succeeds.
  - `go test -race ./... ./tool/... ./artifact/...` passes (existing tests unaffected).
  - `task validate` succeeds.
- **Details**:
  - Adding fields to existing structs is a contract extension, not a break. Existing tools with no `Format` declaration get zero values, which the handler will treat as "use framework defaults" in Task 2.
  - Place `Truncation` in `artifact/` (not `tool/`) because the artifact is the data model; the contract for producing truncation metadata belongs in the data package.
  - `Truncation.MarshalJSON` should use `omitempty` for zero-value fields to keep the wire format clean.
  - The `RecoveryHint` field on `Truncation` is the **rendered** version (post-template-substitution). This keeps the rendered hint co-located with the data the consumer needs.

### Task 2: Framework default — `x/tool/truncate/` truncator package

- **Goal**: Implement the default head/tail byte+line+UTF-8-safe truncator and the recovery-hint template substitution.
- **Dependencies**: Task 1
- **Files Affected**:
  - `go.work` — add `./x/tool/truncate`
  - `go.mod` (root) — add `require` and `replace` for the new submodule
  - `Taskfile.yml` — add `x-tool-truncate: {taskfile: Taskfile.lib.yml, dir: x/tool/truncate}` include
- **New Files**:
  - `x/tool/truncate/go.mod` — module `github.com/andrewhowdencom/ore/x/tool/truncate`, Go 1.26.2, `replace github.com/andrewhowdencom/ore => ../../..`, requires `github.com/andrewhowdencom/ore` and `github.com/stretchr/testify`
  - `x/tool/truncate/go.sum` — generated by `go mod tidy`
  - `x/tool/truncate/doc.go` — package documentation with usage snippet
  - `x/tool/truncate/truncate.go` — core `Truncate` and `RenderHint` functions
  - `x/tool/truncate/truncate_test.go` — table-driven tests
  - `x/tool/truncate/hint.go` — `RenderHint` template substitution
  - `x/tool/truncate/hint_test.go` — `RenderHint` tests
- **Interfaces**:
  ```go
  // Truncate returns the truncated string and a Truncation descriptor
  // reporting what was removed. It is a no-op when s is within the
  // configured caps. Style determines whether the kept portion is the
  // head (start) or tail (end) of the input.
  func Truncate(s string, cfg TruncateConfig, style TruncationStyle) (string, artifact.Truncation)

  // RenderHint substitutes {name} placeholders in tmpl with values from
  // meta. Unknown placeholders are left as-is. The set of supported
  // names is the union of Truncation's fields (e.g. {original_bytes},
  // {shown_lines}, {style}) plus the keys present in meta.RecoveryHint
  // extras; tool authors may add arbitrary fields to the metadata map
  // for tool-specific recovery hints.
  func RenderHint(tmpl string, meta artifact.Truncation) string
  ```
- **Validation**:
  - `go test -race ./x/tool/truncate/...` passes.
  - Tests cover: byte cap respected, line cap respected, no truncation when under both caps, UTF-8 boundary at cut site (multi-byte rune at end is preserved, never split), empty string, style=head returns first N bytes/lines, style=tail returns last N bytes/lines, `RenderHint` substitutes known fields, unknown placeholders left as-is, no placeholders in template returns template unchanged.
  - `go work sync` succeeds.
  - `task validate` succeeds.
- **Details**:
  - **UTF-8 safety**: when cutting at a byte index, scan backward to the start of the next rune. If the cut would split a rune, back up to before the rune. This guarantees the returned string is valid UTF-8.
  - **Line cap semantics**: count `\n` characters in the kept portion. If the cap is reached, find the last `\n` at or before the cap, cut there, and discard the partial trailing line. For tail-style, preserve the last complete line.
  - **Combined cap**: apply byte cap and line cap independently, take the smaller of the two kept lengths.
  - **Hint substitution**: use `strings.NewReplacer` constructed from a `[]string` of `{name} → value` pairs. Pre-compute the pairs in a single pass over the Truncation struct to keep the hot path fast.
  - The package must compile in isolation against `github.com/andrewhowdencom/ore` (for `tool.Format`, `tool.TruncateConfig`, `artifact.Truncation`).
  - This is **not** a hot path (called once per tool result), so template substitution simplicity beats micro-optimization.

### Task 3: Handler integration — apply `Format` in `x/tool/handler.go`

- **Goal**: Wire the truncator into the handler so every tool result is bounded by default.
- **Dependencies**: Tasks 1, 2
- **Files Affected**:
  - `x/tool/handler.go` — extract result-serialization into a helper; apply `Format` after tool execution (both local and namespaced paths)
  - `x/tool/handler_test.go` — new test cases
- **New Files**: None (extend `x/tool/handler.go` and add new test functions to the existing test file)
- **Interfaces** (internal, not exported):
  ```go
  // applyFormat runs the post-execution pipeline: LLMRenderer check,
  // JSON marshal, truncate, render hint, set Truncation on the result.
  func (h *Handler) applyFormat(
      ctx context.Context,
      tool toolpkg.Tool,
      result any,
  ) (content string, truncation *artifact.Truncation)
  ```
- **Validation**:
  - `go test -race ./x/tool/...` passes.
  - New test cases (in `x/tool/handler_test.go`):
    - `TestHandler_AppliesDefaultTruncation`: register a tool that returns a 10 MB string; assert emitted `ToolResult.Content` length ≤ 50 KB and `Truncation` is non-nil with the right `OriginalBytes`.
    - `TestHandler_RespectsLLMRenderer`: register a tool that returns a value implementing `LLMRenderer` with a 10 MB string; assert the full string is preserved verbatim and `Truncation` is nil.
    - `TestHandler_AppliesToolSpecificFormat`: register a tool with `Format.Truncate.MaxBytes = 100`; assert truncation to ≤ 100 bytes.
    - `TestHandler_AppliesRecoveryHint`: register a tool with `Format.RecoveryHint = "Use offset={next_offset} to continue."`; after truncation, assert the emitted content contains the rendered hint.
    - `TestHandler_TruncatesNamespacedResults`: same as default truncation, but for an MCP-style namespaced tool — verify the framework default applies to remote results too.
    - `TestHandler_NoTruncationUnderCap`: small result → no truncation, `Truncation` is nil.
    - `TestHandler_TruncationMetadataPreservedOnError`: a tool that returns a 10 MB result AND an error; assert the result content is truncated and `IsError=true`.
  - Existing tests (`TestHandler_ExecutesRegisteredTool`, `TestHandler_ArrayReturnValue`, etc.) continue to pass.
  - `task validate` succeeds.
- **Details**:
  - **Refactor** the four duplicate emit-on-success/error blocks in `Handle` into a single `emitResult(ctx, e, toolCallID, content, value, isError, truncation)` helper to avoid triplicating the truncation-aware emit logic. This is the kind of aggressive refactor the AGENTS.md encourages.
  - **Order of operations** in `applyFormat`:
    1. If `result` is nil, return empty content and nil truncation.
    2. If `result` implements `artifact.LLMRenderer`, call `MarshalLLM()` and return as-is (no truncation).
    3. `b, err := json.Marshal(result)`; on error, return the marshal error string (matching existing behavior) and nil truncation.
    4. `truncated, trunc := truncate.Truncate(string(b), format.Truncate, format.Style)`.
    5. If `trunc.ShownBytes < trunc.OriginalBytes` AND `format.RecoveryHint != ""`, render the hint and append: `"\n\n" + rendered + "\n" + fmt.Sprintf("[%d lines shown of %d total]", trunc.ShownLines, trunc.OriginalLines)`.
    6. Return `(truncated, &trunc)`.
  - **Where the `Format` comes from**: the `Handler` needs to look up the tool's `Format` from the registry. The local path has access to the `Tool` struct via `registry.Lookup`. The namespaced path needs to look up the tool in the `RemoteSource.Tools()` list. Add a helper `h.formatFor(name string) toolpkg.Format` that handles both paths and returns zero-value `Format` (triggers framework defaults) when the tool is not found.
  - **Trace attributes**: add `tool.truncated bool` and `tool.truncation.original_bytes int` to the existing `tool.execute` span when truncation occurs, for observability.

### Task 4: bash — streaming accumulator + truncated `Result`

- **Goal**: Replace `bytes.Buffer` in `runCommand` with a bounded, streaming accumulator. Extend `Result` with truncation metadata and a temp file path. Implement `MarshalLLM` on `Result`.
- **Dependencies**: Task 3 (handler integration is required before Result.MarshalLLM can be exercised)
- **Files Affected**:
  - `x/tool/bash/bash_unix.go` — replace `bytes.Buffer` with `BoundedBuffer`
  - `x/tool/bash/bash_windows.go` — same
  - `x/tool/bash/result.go` — extend `Result` with `Truncation`, `StdoutPath`, `StderrPath`; add `MarshalLLM() string`; keep `MarshalMarkdown() string`
  - `x/tool/bash/bash.go` — add `Format` to `BashTool`; update `Description`; pass `Bash` tool's `Format` defaults to `runCommand`; tidy result construction to populate new fields
  - `x/tool/bash/result_test.go` — tests for `MarshalLLM`, `MarshalMarkdown` with truncation
  - `x/tool/bash/bash_test.go` — integration test for large output (covered in Task 7)
- **New Files**:
  - `x/tool/bash/bounded_buffer.go` — `BoundedBuffer` type with `Write([]byte) (int, error)`, `Bytes() []byte`, `String() string`, `Path() string`, `Close() error` methods
  - `x/tool/bash/bounded_buffer_test.go` — tests for the buffer
- **Interfaces**:
  ```go
  // BoundedBuffer is an io.Writer that retains only a rolling 2*cap tail
  // in memory and optionally spills the full byte stream to a temp file
  // (created on first overflow). The Write method never blocks and
  // never returns an error (other than from the underlying temp file).
  type BoundedBuffer struct {
      cap   int
      tail  []byte
      file  *os.File
      path  string
      wrote int64
  }
  func NewBoundedBuffer(cap int) *BoundedBuffer
  func (b *BoundedBuffer) Write(p []byte) (int, error)
  func (b *BoundedBuffer) String() string
  func (b *BoundedBuffer) Path() string  // "" if no spill
  func (b *BoundedBuffer) Close() error

  // In result.go
  type Result struct {
      Stdout      string         `json:"stdout"`
      Stderr      string         `json:"stderr"`
      ExitCode    int            `json:"exit_code"`
      StdoutPath  string         `json:"stdout_path,omitempty"`
      StderrPath  string         `json:"stderr_path,omitempty"`
      Truncation  *Truncation    `json:"truncation,omitempty"`
  }
  // Where Truncation is imported from x/tool/truncate or defined locally
  // as a re-export of artifact.Truncation.

  // MarshalLLM returns the LLM-facing string representation. When
  // truncated, the truncated tail is followed by a recovery hint
  // pointing to the temp file.
  func (r *Result) MarshalLLM() string
  ```
- **Validation**:
  - `go test -race ./x/tool/bash/...` passes.
  - New tests for `BoundedBuffer`:
    - `TestBoundedBuffer_UnderCap`: write less than `cap` bytes; `String()` returns full input; `Path()` returns "".
    - `TestBoundedBuffer_OverCap`: write more than `cap` bytes; `String()` returns last `2*cap` bytes; `Path()` returns temp file path; temp file contains the full input.
    - `TestBoundedBuffer_ConcurrentSafe` is **not** required (per AGENTS.md, the bash tool is synchronous within a single call). Skip goroutine tests.
    - `TestBoundedBuffer_Empty`: zero writes, `String()` returns "".
  - New tests for `Result`:
    - `TestResult_MarshalLLM_NoTruncation`: untruncated result renders the full stdout/stderr/exit code.
    - `TestResult_MarshalLLM_Truncated`: result with `Truncation != nil` and `StdoutPath != ""` renders the truncated content + recovery hint naming the temp file.
  - Existing `TestResult_MarshalMarkdown` cases continue to pass (extend the `Result` struct is backwards compatible at the JSON tag level — old fields stay; new fields have `omitempty`).
  - `go test -race ./x/tool/bash/...` passes.
  - `task validate` succeeds.
- **Details**:
  - **BoundedBuffer mechanics**:
    - Internal `tail []byte` of length `2*cap` (kept as a circular slice).
    - On each `Write(p)`: append `p` to `tail`; if `len(tail) > 2*cap`, slice off the front. Concurrent-safe is not required; the bash tool writes from a single goroutine via `cmd.Stdout`.
    - On the **first** write that exceeds `cap`: lazily open a temp file (`os.CreateTemp("", "ore-bash-*.log")`), write the cumulative `tail` content to it, and continue spilling. The temp file is closed by `Close()`.
    - `String()` returns `string(tail)`.
    - `Path()` returns the temp file path (or "" if no spill occurred).
  - **Spill threshold**: trigger file creation when `len(tail) >= cap`, not on the first byte. This avoids creating an empty file for commands that produce small output.
  - **Result construction**: `runCommand` returns `stdoutBuf, stderrBuf, err`. Update it to return `stdoutBuf, stderrBuf, stdoutPath, stderrPath, err`. The caller in `Bash` constructs the `Result` and populates the truncation metadata.
  - **Truncation construction**: after the run, compute `originalBytes := len(fullStdout)`, `originalLines := strings.Count(fullStdout, "\n")`. The `truncate.Truncate` function is invoked here if the tool's `Format` specifies non-zero caps, OR by the framework handler if the value goes through `json.Marshal` of the whole `Result`. Two layers of truncation (Result-level for `MarshalLLM`, handler-level for the JSON-marshaled fallback) is acceptable; the Bash tool's `Result` should provide a pre-truncated `Stdout` field so the JSON marshal produces a small result. **Recommendation**: do the truncation in `Bash` itself, not in the handler, so the `Result.Stdout` is already bounded. The framework's handler-level truncation remains as a safety net.
  - **Description rewrite**:
    ```
    Execute a shell command. Returns stdout, stderr, and exit code.

    Output limits: stdout and stderr are each capped at 50 KB / 2000 lines
    (whichever is reached first). When truncated, the full output is
    written to a temp file (path included in the result).

    Recovery: read the temp file with read_file, or use grep/tail/head
    on it to extract the relevant lines. Use working_directory to scope.
    ```
  - **Why streaming matters**: today, `bytes.Buffer` is sized by the subprocess. A `dd if=/dev/zero bs=1M count=1024` command produces 1 GB of zeros. The host process's heap balloons. The `BoundedBuffer` caps retention at `2*cap` = 100 KB regardless of subprocess size.

### Task 5: filesystem — bounded reads + row caps + `Format` declarations

- **Goal**: Cap `read_file` bytes, `search_files` rows, `list_directory` rows. Add `Format` to all five filesystem tool descriptors. Rewrite descriptions.
- **Dependencies**: Task 3
- **Files Affected**:
  - `x/tool/filesystem/filesystem.go` — add byte cap to `ReadFile`, row cap to `SearchFiles` and `ListDirectory`; add `Format` to all five `*Tool` descriptors; rewrite `Description` strings
  - `x/tool/filesystem/filesystem_test.go` — new tests for caps and Format
- **New Files**: None
- **Interfaces** (signature changes; tools continue to satisfy `tool.ToolFunc`):
  ```go
  // ReadFile adds temp-file fallback when output is truncated.
  // Existing args: path, offset, limit. The limit arg is now an upper
  // bound on lines retained in the LLM-facing result; if the file is
  // larger, the full content spills to a temp file.
  func ReadFile(ctx context.Context, sb tool.Sandbox, args map[string]any) (any, error)

  // ReadFileTool gains Format: Format{Truncate: {MaxBytes: 50000, MaxLines: 2000},
  //                                   Style: tool.StyleHead,
  //                                   RecoveryHint: "Output truncated at {shown_lines} of {original_lines} lines. Use offset={next_offset} to continue reading."}
  ```
- **Validation**:
  - `go test -race ./x/tool/filesystem/...` passes.
  - New tests:
    - `TestReadFile_ByteCapRespected`: write a 200 KB file; call `ReadFile`; assert returned string ≤ 50 KB.
    - `TestReadFile_TruncationInResult`: same as above, but assert the result includes a recovery hint and the temp file path.
    - `TestReadFile_OffsetRespected`: existing offset/limit semantics still work.
    - `TestSearchFiles_RowCap`: synthesize a directory with 1500 matching files; assert result has exactly 1000 rows and the recovery hint.
    - `TestListDirectory_RowCap`: synthesize a directory with 600 entries; assert result has 500 rows and the recovery hint.
    - `TestWriteFile_NoTruncation`: write a small file; assert `Format.RecoveryHint` is empty and the result is short.
  - Existing tests continue to pass.
  - `task validate` succeeds.
- **Details**:
  - **ReadFile byte cap**: use `io.LimitReader` to bound the read at the cap, then `truncate.Truncate` the resulting string. If truncated, write the full file content to a temp file (via `os.CreateTemp`) and include the path in the result. The recovery hint instructs the model to use `offset={next_offset}` to continue.
  - **next_offset calculation**: the truncated tail/head preserves a certain number of lines; `next_offset` is the line number immediately after the last shown line. For head-style, `next_offset = shown_lines + 1`. For tail-style, it's still `shown_lines + 1` (the model can pass that as the new `offset` to read further).
  - **SearchFiles row cap**: cap the `results` slice at `MaxRows` (default 1000). If truncated, return the cap rows plus a `Truncation` in the result. The result type becomes `SearchFilesResult{Results []SearchResult, Truncation *Truncation}`. **Schema change**: the JSON shape of the result changes. This is a breaking change for any consumer that parses the result as `[]SearchResult`. The migration path is the `MarshalLLM` method on the new struct, which renders the truncated slice plus the recovery hint.
  - **ListDirectory row cap**: same as SearchFiles. New struct `ListDirectoryResult{Entries []string, Truncation *Truncation}`.
  - **Schema update**: add a `limit` parameter to `search_files` and `list_directory` schemas, defaulting to 1000 and 500 respectively. The model can override the cap downward.
  - **Description rewrites** (per the issue's structured form):
    - `read_file`: "Read the contents of a file. Returns line-number-prefixed content. Output is capped at 50 KB / 2000 lines; full content is written to a temp file when truncated. Use offset=N to continue reading, or read the temp file."
    - `search_files`: "Search files for a regex query. Returns matching lines with file path and line number. Output is capped at 1000 rows. Use limit=2N to see more, or limit=N to reduce."
    - `list_directory`: "List immediate non-hidden entries. Output is capped at 500 entries. Use limit=2N to see more, or limit=N to reduce."
    - `write_file` / `edit_file`: descriptions remain short (acknowledgements only) but adopt the structured form with an explicit `Format{Truncate: TruncateConfig{}}` declaration.

### Task 6: skills, calculator, set_title, filter — adopt the contract

- **Goal**: Migrate the remaining built-in tools to the new contract. Most are small-output and need no truncation; the value is consistency.
- **Dependencies**: Task 3
- **Files Affected**:
  - `x/tool/skills/tool.go` — add byte cap to `ReadSkill`; add `Format` to `ReadSkillTool`; rewrite description
  - `x/tool/skills/catalog_test.go` (or wherever `Catalog.Read` is tested) — add a cap test
  - `x/tool/calculator/calculator.go` — add `Format` to `AddTool` and `MultiplyTool` with `TruncateConfig{MaxBytes: 0, MaxLines: 0}` to declare "no truncation needed"; rewrite descriptions
  - `x/tool/set_title/settitle.go` — add `Format` to the `set_title` tool descriptor; rewrite description
  - `x/tool/filter.go`, `x/tool/filter_test.go` — filter is not a tool; verify the file is unaffected (no `tool.Tool` descriptors live there)
- **New Files**: None
- **Interfaces** (declarations on existing tool descriptors):
  ```go
  // x/tool/calculator/calculator.go
  var AddTool = tool.Tool{
      Name: "add",
      Description: "Add two numbers. Output is the numeric sum (no truncation).",
      Schema: ...,
      Format: tool.Format{Truncate: tool.TruncateConfig{}}, // zero caps = no truncation
      DisplayHint: ...,
  }

  // x/tool/skills/tool.go
  var ReadSkillTool = tool.Tool{
      Name: "read_skill",
      Description: "Read the full SKILL.md content for a named skill. Output is capped at 50 KB; full content is written to a temp file when truncated.",
      Schema: ...,
      Format: tool.Format{
          Truncate:     tool.TruncateConfig{MaxBytes: 50_000, MaxLines: 2000},
          Style:        tool.StyleHead,
          RecoveryHint: "Output truncated. Use read_file on the temp file path to read more, or invoke a more specific skill.",
      },
      DisplayHint: ReadSkillDisplayHint,
  }
  ```
- **Validation**:
  - `go test -race ./x/tool/{calculator,set_title,skills}/...` passes.
  - New tests:
    - `TestReadSkill_ByteCapRespected`: synthesize a > 50 KB skill content; assert returned string is truncated and includes the temp file path.
    - `TestCalculator_FormatNoTruncate`: small-output tool, assert that returning a 1 KB string is unchanged by the handler.
  - `task validate` succeeds.
- **Details**:
  - **`Format` with zero caps**: the truncator's `DefaultTruncateConfig` is invoked when caps are zero. Calculator results are small (a single number), so passing through the framework default is fine — 50 KB is large enough that a `float64` is never truncated. But the plan should specify that calculator adopts a **declarative** Format (zero caps) so future readers understand the intent: "this tool's output is intentionally small."
  - **`ReadSkill` byte cap**: similar to `ReadFile`, use `io.LimitReader` and spill to temp file.
  - **set_title's `Tool()` function** is a `tool.ToolFunc` factory; verify whether the resulting `tool.Tool` descriptor exposes `Format`. Looking at `settitle.go`, the descriptor is constructed by the caller (search for `set_title`'s registration in `examples/` or `agent/`). The plan should add `Format` to wherever the descriptor lives. **Investigation needed in the implementer phase** to locate the descriptor construction site.
  - **`x/tool/filter.go`**: not a tool; this is a `ToolFilter` function type. No changes needed.

### Task 7: Integration smoke test — bounded large-output bash invocation

- **Goal**: Prove that a multi-GB subprocess output does not OOM the host and that the resulting `ToolResult` is bounded.
- **Dependencies**: Task 4
- **Files Affected**:
  - `x/tool/bash/bash_test.go` — add new test functions
- **New Files**: None (extend the existing test file)
- **Interfaces**: None (test code only)
- **Validation**:
  - `go test -race -run TestBashLargeOutput ./x/tool/bash/...` passes.
  - The test:
    - Constructs a `Bash` `ToolFunc` (or invokes `Bash` directly with a synthetic sandbox that has a temp working directory).
    - Runs a command that produces large output, e.g., `bash -c 'for i in $(seq 1 100000); do echo "line $i with some padding to make it bigger"; done'` (produces ~5 MB) or `dd if=/dev/zero bs=1M count=50 2>/dev/null | base64` (produces ~67 MB).
    - Asserts `result.Truncation != nil` and `result.Truncation.OriginalBytes > result.Truncation.ShownBytes`.
    - Asserts the final `toolResult` produced by the handler has `len(Content) <= 50_000 + len(recoveryHint) + len(noticeLine)`.
    - Asserts `result.StdoutPath != ""` and the file exists and contains the full output.
    - Skip the test on Windows (where `dd` is not available) or use a portable command.
- **Details**:
  - **Test scaffold**: build a synthetic `tool.Sandbox` that satisfies `tool.FileSandbox` (returns a temp `WorkingDirectory`) and `tool.ExecSandbox` (delegates to `Bash`'s internal command-execution path). Or, more simply, exercise `Bash` against a real `exec.Cmd` via a custom sandbox — see `x/tool/sandbox/unsafe/unsafe.go` for a working `ExecSandbox` implementation.
  - **Test timeout**: 30s. If the test hangs, the underlying command has leaked.
  - **Memory check** (optional, may be flaky in CI): `runtime.MemStats` before and after the call; assert heap growth ≤ some bound (e.g., 10 MB). Mark as `t.Skip` if `testing.Short()`.
  - **Cleanup**: `t.Cleanup(func() { os.Remove(result.StdoutPath) })`.

### Task 8: Test sweep + observability + documentation

- **Goal**: Run the full validation suite, add trace attributes for truncation events, update `AGENTS.md` if needed.
- **Dependencies**: Tasks 1–7
- **Files Affected**:
  - `AGENTS.md` — add a note in the "Tool" section explaining the `Format` contract and the "framework defaults to safe" property
  - Any `*.md` doc files under `x/tool/{bash,filesystem,skills}/` — update if they mention output handling
- **New Files**:
  - `x/tool/truncate/README.md` (if a separate doc file is preferred over `doc.go`)
  - `x/tool/doc.go` — add a top-level note about default truncation behavior (already exists; check for needed updates)
- **Validation**:
  - `task validate` succeeds across all workspace modules.
  - `go test -race ./...` passes.
  - `go vet ./...` clean.
  - `golangci-lint run ./...` clean.
  - `go build ./...` succeeds.
  - Manual: `git grep "truncate" -- 'x/tool/'` shows the new truncator and updated tools.
- **Details**:
  - **Trace attributes** (added in Task 3): `tool.truncated bool`, `tool.truncation.original_bytes int`, `tool.truncation.shown_bytes int` on the `tool.execute` span. Verify in a `TestHandler_TruncationSpanAttributes` test.
  - **AGENTS.md update**: add a paragraph under the "Tool" / "Implementation Conventions" section describing:
    - The `Format` field on `tool.Tool` (zero value = framework defaults).
    - The default 50 KB / 2000 line cap.
    - The `LLMRenderer` opt-out.
    - The recovery-hint template substitution.
  - **Cost measurement** (informal, not a CI gate): run a workshop session with a representative large-output scenario (e.g., `cat package-lock.json`); assert the per-turn token contribution from the tool result is bounded. Document the before/after in the PR description.

## Dependency Graph

```
Task 1 (contract types)
  ↓
Task 2 (truncator package) ──→ Task 3 (handler integration)
                                    ↓
                  ┌─────────────────┼─────────────────┐
                  ↓                 ↓                 ↓
              Task 4 (bash)     Task 5 (filesystem)  Task 6 (skills, calc, set_title)
                  ↓                                       ↓
                  └──────────────────┬────────────────────┘
                                     ↓
                                Task 7 (smoke test)
                                     ↓
                                Task 8 (test sweep + docs)
```

- **Task 1 → Task 2** (Task 2 imports the types from Task 1)
- **Task 1, 2 → Task 3** (Task 3 wires the truncator into the handler)
- **Task 3 → Tasks 4, 5, 6** (each tool migration depends on the handler being able to apply `Format`)
- **Task 4 → Task 7** (the smoke test exercises the bash tool)
- **Task 7 → Task 8** (final validation runs after all migrations land)

**Parallelizable**: Tasks 4, 5, 6 can land in parallel branches once Task 3 lands. For a single PR, they're sequential commits but conceptually independent.

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| `BoundedBuffer` Write semantics differ from `bytes.Buffer` in edge cases (e.g., partial UTF-8 across writes) | High | Medium | Add explicit tests for multi-byte runes split across `Write` calls; ensure `BoundedBuffer` accumulates bytes first, then truncates on `String()` |
| `ReadFile` byte cap interacts poorly with the existing `offset`/`limit` line parameters | Medium | High | Treat the byte cap as an outer bound; existing `offset`/`limit` still apply within the bounded region. Add an integration test for `read_file` with both `offset=100, limit=50` and a 200 KB file |
| `SearchFiles` result shape change (from `[]SearchResult` to a struct with `Truncation`) breaks existing callers | Medium | High | Define a new `SearchFilesResult` type with `MarshalLLM()` that produces a string; the framework uses `MarshalLLM` when present; raw JSON is unchanged for non-LLMRenderer-aware consumers. Document the JSON shape change in the PR description |
| `Truncation` field on `artifact.ToolResult` bloats the artifact with tool-specific concerns | Low | Medium | `omitempty` on all fields; `Truncation` is a small struct (5 ints + 2 strings); justify the addition in the PR description by analogy with `IsError` and `Value` |
| `RecoveryHint` template substitution produces unexpected output for edge cases (empty template, unknown placeholders, multiple occurrences) | Low | Medium | Document the substitution rules; add unit tests for each case; default to leaving unknown placeholders as-is rather than failing |
| `dd if=/dev/zero` smoke test is flaky in CI (sandboxing, resource limits) | Medium | Medium | Mark as `t.Skip` under `testing.Short()`; use a smaller command (`seq 1 100000`) as the primary test; the `dd` case is a separate, explicitly-tagged integration test |
| `BoundedBuffer` temp file cleanup leaks if `Close()` is not called | Low | High | `t.Cleanup` in tests; `defer bb.Close()` in `runCommand`; consider a `Finalize()` method that's idempotent and safe to skip on normal completion |
| Workshop integration test cannot be authored in OSS (no workshop binary in this repo) | Low | Certain | Substitute with a Go-level integration test in `x/tool/bash/bash_test.go` that runs the bash tool end-to-end and asserts the `ToolResult` |
| Temp file path is returned to the LLM without sandbox scoping, exposing host paths | Medium | High | The tool's existing `FileSandbox.ResolvePath` must be used for the temp file path; if no sandbox is configured, the path is returned as-is (matching today's behavior for absolute paths). Document this in the tool description |

## Validation Criteria

- [ ] `go test -race ./...` passes across all 16 workspace modules.
- [ ] `task validate` succeeds at the repo root.
- [ ] `golangci-lint run ./...` produces no findings.
- [ ] `go vet ./...` produces no findings.
- [ ] A new test in `x/tool/bash/bash_test.go` runs a command producing ≥ 50 MB of output and asserts: (a) the function returns within 30 s, (b) `result.Truncation != nil`, (c) `result.Truncation.OriginalBytes ≥ 50_000_000`, (d) `result.Truncation.ShownBytes ≤ 50_000`, (e) `result.StdoutPath != ""` and the file at that path contains the full output.
- [ ] A new test in `x/tool/handler_test.go` registers a tool that returns a 10 MB string and asserts the emitted `ToolResult.Content` is ≤ 50 KB and `Truncation` is non-nil.
- [ ] A new test in `x/tool/handler_test.go` registers a tool that returns a value implementing `artifact.LLMRenderer` with a 10 MB string and asserts the full string is preserved (opt-out works).
- [ ] A new test in `x/tool/filesystem/filesystem_test.go` synthesizes a 10 MB file, calls `read_file`, and asserts the result is bounded and the temp file is created.
- [ ] A new test in `x/tool/filesystem/filesystem_test.go` synthesizes a directory with 1500 entries containing a regex match, calls `search_files`, and asserts the result has exactly 1000 rows.
- [ ] Every built-in tool (`bash`, `filesystem.read_file/write_file/edit_file/list_directory/search_files`, `skills.read_skill`, `calculator.add/multiply`, `set_title`) has a populated `Format` field and a rewritten `Description` that includes output limits and a recovery hint.
- [ ] `git grep "bytes.Buffer"` in `x/tool/bash/` returns no matches (the unbounded buffers are gone).
- [ ] `go doc github.com/andrewhowdencom/ore/tool.Format` and `go doc github.com/andrewhowdencom/ore/x/tool/truncate` produce the expected documentation.
- [ ] `AGENTS.md` is updated with a paragraph describing the `Format` contract and the "framework defaults to safe" property.
