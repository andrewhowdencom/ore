# Plan: Add Single-Shot stdio Conduit

## Objective

Implement a new `x/conduit/stdio/` package that provides a single-shot, unix-filter-style conduit for the ore framework. It reads from an `io.Reader`, submits a single user message, streams assistant artifacts as Markdown blocks to an `io.Writer`, and returns after the turn completes. This is a deliberate exception to the standard conduit blocking-contract so the conduit can be used in CLI pipelines.

## Context

- The repository is on branch `190` tracking GitHub issue #190.
- No existing `x/conduit/stdio/` or `x/conduit/stream/` package exists.
- Existing conduits (`x/conduit/tui/`, `x/conduit/http/`, `x/conduit/slack/`, `x/conduit/telegram/`) all follow the functional-options constructor pattern, export a `Descriptor`, and block in `Start()` until `ctx.Done()`.
- The TUI conduit (`x/conduit/tui/tui.go`) is the reference for `WithThreadID` and `session.Manager` composition. It uses `mgr.Create()` / `mgr.Attach()` and subscribes to `turn_complete`.
- `examples/single-turn-cli/main.go` demonstrates single-turn stdin/stdout I/O but bypasses `session.Manager` and talks directly to `loop.Step` and `provider.Provider`. The new conduit must use `session.Manager`.
- The conduit skill (`.agents/skills/conduit/SKILL.md`, `SKELETON.md`, `README_EXAMPLE.md`) defines the exact contract for new conduits.
- `x/conduit/conduit.go` defines `Conduit` interface, `Descriptor`, and capability constants.
- `x/conduit/doc.go` documents the standard contract: constructor, Descriptor, sink registration, blocking Start, graceful shutdown.
- `session/manager.go` and `session/stream.go` provide `Manager.Create()`, `Manager.Attach()`, `Stream.Process()`, `Stream.Subscribe()`, and `Stream.Cancel()`.
- `loop/loop.go` defines `OutputEvent`, `TurnCompleteEvent`, `ErrorEvent`, and `ArtifactEvent`. `Turn()` emits `ArtifactEvent` for every streaming delta (`text_delta`, `reasoning_delta`, `tool_call_delta`) and for accumulated blocks on kind switches.
- `artifact/artifact.go` defines artifact kinds: `text_delta`, `reasoning_delta`, `tool_call_delta`, `text`, `reasoning`, `tool_call`, `tool_result`, `usage`, `image`.
- `go.work` currently lists all workspace modules including `x/conduit/tui`, `x/conduit/http`, etc. The new module must be added there.

## Architectural Blueprint

- **Package**: `x/conduit/stdio/` with its own `go.mod` (module `github.com/andrewhowdencom/ore/x/conduit/stdio`).
- **Type**: `stdio` struct holding `*session.Manager`, `io.Reader`, `io.Writer`, and optional `threadID`.
- **Constructor**: `New(mgr *session.Manager, opts ...Option) (conduit.Conduit, error)` using functional options. Validates `mgr != nil`.
- **Options**:
  - `WithInput(r io.Reader)` — defaults to `os.Stdin`
  - `WithOutput(w io.Writer)` — defaults to `os.Stdout`
  - `WithThreadID(id string)` — empty means `mgr.Create()`, non-empty means `mgr.Attach()`
- **Descriptor**: exports `CapEventSource`, `CapRenderMarkdown`, `CapAcceptText`.
- **`Start(ctx)` lifecycle** (exception to blocking contract):
  1. Create or attach a `*session.Stream`.
  2. Subscribe to `text_delta`, `reasoning_delta`, `tool_call_delta`, `turn_complete`, `error`.
  3. Launch a goroutine that consumes output events and writes Markdown blocks to `io.Writer`.
  4. Read all bytes from `io.Reader` (`io.ReadAll`).
  5. Build `session.UserMessageEvent{Content: string(data)}` with `EventContext.Provenance` set to the conduit's identifier (e.g., `"stdio"`).
  6. Call `stream.Process(ctx, event)`.
  7. Wait for the turn to finish (`turn_complete` or `error`) in the main goroutine.
  8. Return cleanly on success, or return the error on failure.
- **Markdown rendering rules**:
  - `text_delta`: write content directly (no wrapper).
  - `reasoning_delta`: open `\`\`\`reasoning\n` on first delta, write content, close with `\n\`\`\`\n` when the kind switches or turn completes.
  - `tool_call_delta`: open `\`\`\`tool-call\n` on first delta, write content, close when switching or turn completes.
  - Other artifact kinds: write a generic Markdown block or skip if unsupported.
  - `ErrorEvent`: write a formatted error block or message to `io.Writer`, then return the error from `Start()`.
- **Provenance echo suppression**: outbound events set `Provenance = "stdio"`; the subscriber skips any event whose `Context().Provenance` matches `"stdio"`.

### Package Name Deliberation

The issue proposed `x/conduit/stream/` or `x/conduit/stdio/`. `stream` was rejected because the term is overloaded with the existing `session.Stream`, `Stream.Subscribe`, and delta-streaming concepts. `stdio` clearly signals the Unix filter / stdin-stdout use case, even though the package supports arbitrary `io.Reader`/`io.Writer`.

## Requirements

1. Package exports `Descriptor` with valid capabilities (`CapEventSource`, `CapRenderMarkdown`, `CapAcceptText`).
2. Constructor accepts `*session.Manager` and validates non-nil.
3. `Start(ctx)` returns after the single turn completes or on error (documented exception to blocking-contract).
4. Subscribes to streaming output events (`text_delta`, `reasoning_delta`, `turn_complete`, `error`) before processing.
5. Maps all external input to `session.UserMessageEvent` with provenance set.
6. Passes `go test -race ./...`.
7. Handles provenance echo suppression.
8. `README.md` present with all required sections (Overview, Capabilities, Composition, Configuration, Runtime Semantics, Error Handling).
9. Supports generic I/O via `WithInput`/`WithOutput` with `os.Stdin`/`os.Stdout` defaults.
10. Supports thread resumption via `WithThreadID`.

## Task Breakdown

### Task 1: Bootstrap Package Directory and Module
- **Goal**: Create `x/conduit/stdio/` with `go.mod`, `doc.go`, and placeholder source files that compile.
- **Dependencies**: None.
- **Files Affected**:
  - `go.work` (add `./x/conduit/stdio` to the `use` block)
- **New Files**:
  - `x/conduit/stdio/go.mod`
  - `x/conduit/stdio/doc.go`
  - `x/conduit/stdio/stdio.go` (placeholder struct and constructor)
- **Interfaces**: None.
- **Validation**:
  - `cd x/conduit/stdio && go mod tidy` succeeds.
  - `go build ./...` from the package directory succeeds.
  - `go.work` includes the new module path.
- **Details**: The `go.mod` must declare `module github.com/andrewhowdencom/ore/x/conduit/stdio` and include `replace github.com/andrewhowdencom/ore => ../../..`. `doc.go` must document the package purpose and the single-shot lifecycle exception.

### Task 2: Implement Constructor, Functional Options, and Descriptor
- **Goal**: Implement the full constructor, options, and exported `Descriptor`.
- **Dependencies**: Task 1.
- **Files Affected**:
  - `x/conduit/stdio/stdio.go`
- **New Files**:
  - `x/conduit/stdio/stdio_test.go`
- **Interfaces**:
  ```go
  func New(mgr *session.Manager, opts ...Option) (conduit.Conduit, error)
  type Option func(*stdio)
  func WithInput(r io.Reader) Option
  func WithOutput(w io.Writer) Option
  func WithThreadID(id string) Option
  var Descriptor = conduit.Descriptor{...}
  ```
- **Validation**:
  - `go test -race ./...` passes.
  - Tests cover: nil manager rejection, default input/output (non-nil), `WithThreadID` sets field correctly, `Descriptor` is exported and non-zero.
- **Details**: The `stdio` struct holds `mgr *session.Manager`, `in io.Reader`, `out io.Writer`, `threadID string`. Default `in` to `os.Stdin` and `out` to `os.Stdout` in `New`. Do NOT implement `Start()` yet; leave a stub that returns `nil`.

### Task 3: Implement Start() Single-Turn Lifecycle and Streaming Markdown Output
- **Goal**: Implement `Start(ctx)` that reads input, processes one turn, streams Markdown blocks, and returns.
- **Dependencies**: Task 2.
- **Files Affected**:
  - `x/conduit/stdio/stdio.go`
- **New Files**: None.
- **Interfaces**:
  ```go
  func (s *stdio) Start(ctx context.Context) error
  ```
- **Validation**:
  - `go test -race ./...` passes with tests covering:
    - Happy path: single turn completes, Start returns, output contains streamed text.
    - Reasoning deltas are wrapped in ` ```reasoning` blocks.
    - Error event causes Start to return a non-nil error after writing to output.
    - Thread attachment with `WithThreadID` works (mock store returns a thread).
    - Provenance echo suppression: events with `Provenance == "stdio"` are not written to output.
    - Context cancellation during processing is handled gracefully.
- **Details**:
  1. Create/attach stream using `mgr.Create()` or `mgr.Attach(s.threadID)`.
  2. Subscribe to `text_delta`, `reasoning_delta`, `tool_call_delta`, `turn_complete`, `error`.
  3. Use a `sync.WaitGroup` or a `done` channel to coordinate between the subscriber goroutine and the main goroutine.
  4. Read all input with `io.ReadAll(s.in)`.
  5. Send `UserMessageEvent` with `EventContext{Provenance: "stdio"}`.
  6. Call `stream.Process(ctx, event)`.
  7. Wait for `turn_complete` or `error` in the main goroutine (the subscriber signals completion).
  8. Return `nil` on `turn_complete`, or the error on `error`.
  9. The subscriber goroutine must track the current artifact kind to open/close Markdown code blocks correctly.
  10. Use `fmt.Fprintf` or `io.WriteString` to write to `s.out`. Flush if the writer implements `io.Flusher`.
  11. Do NOT block on `<-ctx.Done()` after the turn completes; return promptly.

### Task 4: Write README.md
- **Goal**: Document the stdio conduit following the standardized README template.
- **Dependencies**: Task 3.
- **Files Affected**: None.
- **New Files**:
  - `x/conduit/stdio/README.md`
- **Interfaces**: None.
- **Validation**: README contains all required sections with accurate content.
- **Details**: Follow the structure in `.agents/skills/conduit/README_EXAMPLE.md`:
  - **Overview**: What the conduit does, single-shot Unix-filter style, exception to blocking Start contract.
  - **Capabilities**: `event-source`, `render-markdown`, `accept-text`.
  - **Composition**: Code snippet showing `stdio.New(mgr, stdio.WithInput(...), stdio.WithOutput(...))`.
  - **Configuration / Options**: Table of `WithInput`, `WithOutput`, `WithThreadID` with types, defaults, and descriptions.
  - **Runtime Semantics**: Session model (Create vs Attach), event subscription, Markdown block rendering, echo suppression, single-shot return behavior.
  - **Error Handling**: Fatal errors (returned from Start) vs non-fatal errors (logged, though in a single-shot conduit most turn errors are fatal because they end the run).

## Dependency Graph

- Task 1 → Task 2 → Task 3 → Task 4

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| `io.ReadAll` on stdin in tests causes hangs or deadlocks | Medium | Medium | Inject `strings.NewReader` or `bytes.NewBuffer` in all tests; never use `os.Stdin` in tests. |
| Delta-to-block tracking for Markdown open/close is off-by-one | Medium | Medium | Write focused tests that emit mixed `text_delta` and `reasoning_delta` sequences and assert exact output strings. |
| Single-shot `Start()` returning early breaks multi-conduit composition assumptions | Low | Low | Document the exception explicitly in `doc.go` and `README.md`; the issue already acknowledges this deviation. |
| `go.work` or module replace directives drift after bootstrap | Low | High | Include `go.work` update in Task 1 and validate with `go build ./...` from repo root. |
| Race between subscriber goroutine and `stream.Process()` completion | Medium | Low | Use a channel or `sync.WaitGroup` to ensure Start does not return before the subscriber has processed the final `turn_complete` or `error` event. |

## Validation Criteria

- [ ] `x/conduit/stdio/go.mod` exists with correct module name and replace directive.
- [ ] `go.work` lists `./x/conduit/stdio`.
- [ ] `go test -race ./...` passes in `x/conduit/stdio/`.
- [ ] `Descriptor` is exported with capabilities `CapEventSource`, `CapRenderMarkdown`, `CapAcceptText`.
- [ ] Constructor `New` rejects nil `*session.Manager`.
- [ ] `Start(ctx)` returns after a single turn on the happy path.
- [ ] Streaming `text_delta` content is written directly to output.
- [ ] Streaming `reasoning_delta` content is wrapped in ` ```reasoning` Markdown blocks.
- [ ] Provenance echo suppression is implemented (events with `Provenance == "stdio"` are ignored by the subscriber).
- [ ] `README.md` exists in `x/conduit/stdio/` with all required sections.
