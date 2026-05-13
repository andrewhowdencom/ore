# Plan: Make Conversations First-Class Entities Accessible from Multiple Conduits

## Objective

Refactor conduits from conversation owners into thin I/O frontends that attach to a shared conversation store. Introduce a new `conversation/` package defining a `Store` interface (adapter pattern) with in-memory and JSON-on-disk implementations. Conversations receive stable UUIDs, hold `*state.Memory`, and enforce per-conversation locking so multiple conduits (HTTP, TUI, and future frontends) can load, continue, and persist the same conversation. Update `examples/tui-chat/` and `examples/http-chat/` to demonstrate cross-conduit continuity: a conversation created via HTTP can be resumed in the TUI via `--conversation <uuid>`.

## Context

### Current Architecture

- **`conduit.Conduit` interface** (`conduit/conduit.go`): defines `Events() <-chan Event` and capability enumeration. Designed for persistent-connection frontends (TUI). HTTP does **not** implement `Conduit` — it is a session-based request/response handler (`conduit/http/handler.go`).
- **`conduit/http/`**: `Handler` maintains an in-memory `SessionStore`. Each `POST /sessions` creates an isolated session with a random ID, its own `*loop.Step`, and its own `*state.Memory`. Sessions are ephemeral and lost on process exit.
- **`conduit/tui/`**: `TUI` creates one anonymous `*state.Memory` and one `*loop.Step`. No session identity. Conversation ends when the TUI quits.
- **`loop.Step`** (`loop/loop.go`): runtime turn orchestrator with embedded `FanOut`. Not persistent — it owns goroutines and channels.
- **`state.Memory`** (`state/memory.go`): simple slice-backed `State` implementation. Explicitly **not goroutine-safe** per `AGENTS.md`.
- **`artifact.Artifact`** (`artifact/artifact.go`): interface with `Kind() string`. Concrete types: `Text`, `ToolCall`, `ToolResult`, `Usage`, `Image`, `Reasoning`. Streaming deltas (`TextDelta`, `ReasoningDelta`, `ToolCallDelta`) implement `Delta` and are accumulated into complete artifacts by `Step.Turn()` — they never appear in persisted state.
- **Examples**:
  - `examples/tui-chat/main.go`: composes TUI, `loop.Step`, `cognitive.ReAct`, OpenAI provider. One anonymous conversation.
  - `examples/http-chat/main.go`: composes HTTP handler with `stepFactory` per session. Each session runs its own `ReAct` loop.

### Observed Patterns

- Functional options pattern (`New(opts ...Option)`) is standard across the codebase.
- `log/slog` for lifecycle logging.
- `fmt.Errorf("...: %w", err)` for error wrapping.
- Table-driven tests; `go test -race ./...` required.
- `httptest.Server` for HTTP handler tests.
- Root-level packages are framework primitives; `internal/` is not used for framework contracts.

## Architectural Blueprint

### Tree-of-Thought Deliberation

**Option A: Extend `conduit.Conduit` interface**
- Add `Attach(id string)` or similar method to `Conduit`.
- *Rejected*: HTTP does not implement `Conduit` today. The interface is scoped to persistent-connection frontends. Extending it would force session-based frontends into a channel-shaped contract, breaking the existing separation of concerns.

**Option B: Create a new `conversation/` root-level package**
- A framework primitive sitting above conduits. Defines `Store` interface, `Conversation` entity with locking, and pluggable persistence.
- HTTP handler and TUI example both receive a `conversation.Store` and attach/load conversations by UUID.
- *Selected*: Clean separation. `conversation/` depends only on `state/` and `artifact/` — no coupling to `loop/`, `provider/`, or `conduit/`. Conduits become I/O adapters; the store owns identity and persistence.

**Option C: Add persistence to `state/` package**
- Make `state.Memory` serializable or add a `state.Store`.
- *Rejected*: `state/` is intentionally minimal — just the `State` interface and one in-memory implementation. Persistence is an application-layer concern. Bloating `state/` would violate the cycle-free, minimal-dependency design.

**Option D: Add persistence to `loop/` package**
- `loop.Step` already manages turns and FanOut.
- *Rejected*: `loop/` is a runtime orchestration primitive (goroutines, channels). Persistence and multi-conduit identity are orthogonal concerns. Mixing them would violate single responsibility.

### Selected Architecture

A new **`conversation/`** package at root level defines the store abstraction. It imports only `artifact/` and `state/`, keeping the dependency graph clean:

```
artifact/       ← no internal deps
state/          ← depends on artifact/
conversation/   ← depends on artifact/, state/
provider/       ← depends on state/, artifact/
loop/           ← depends on artifact/, provider/, state/
conduit/        ← depends on ... (varies by impl)
```

**Key design decisions:**

1. **`Conversation` struct**: holds `ID string`, `State *state.Memory`, `CreatedAt/UpdatedAt time.Time`, and a `sync.Mutex` with `Lock()`/`Unlock()` methods. Provides per-conversation serialization of turns.
2. **`Store` interface**: `Create() (*Conversation, error)`, `Get(id string) (*Conversation, bool)`, `Save(conv *Conversation) error`, `Delete(id string) bool`.
3. **Serialization in `conversation/`**: A private registry maps artifact `Kind() string` to factory functions. Only non-delta artifact types are registered (`text`, `tool_call`, `tool_result`, `usage`, `image`, `reasoning`). JSON format stores turns as `{"role": "...", "artifacts": [{"kind": "...", "data": {...}}]}`.
4. **`MemoryStore`**: in-memory `map[string]*Conversation` protected by `sync.RWMutex`.
5. **`JSONStore`**: reads/writes `{uuid}.json` files in a directory. Atomically writes to a temp file and renames for crash safety.
6. **HTTP handler evolution**: `Handler` accepts a `conversation.Store`. `POST /sessions` creates a new `Conversation` (via `store.Create()`). Optionally accepts `conversation_id` in request body to attach to an existing conversation. `Session` holds a `*Conversation` reference; `Session.State()` returns `conv.State`; `Session.Lock()`/`Unlock()` delegates to the conversation. After each `messageHandler` invocation, `store.Save(session.conv)` is called.
7. **TUI example evolution**: parses `--conversation <uuid>` flag. Creates `conversation.MemoryStore` (or `JSONStore` if `STORE_DIR` env var is set). If `--conversation` provided, loads from store; if absent, creates new conversation. Prints the conversation UUID on startup. Saves state after each turn via `store.Save()`.
8. **HTTP example evolution**: uses `conversation.JSONStore` when `STORE_DIR` env var is set; defaults to `MemoryStore`. Demonstrates persistence across restarts.

## Requirements

1. Create `conversation/` package with `Store` interface, `Conversation` struct, and in-memory implementation (`MemoryStore`).
2. Implement JSON serialization for `[]state.Turn` and `[]artifact.Artifact` (non-delta types only) within `conversation/` package.
3. Implement `JSONStore` that persists conversations as individual JSON files in a directory.
4. `Conversation` provides `Lock()`/`Unlock()` for per-conversation turn serialization (since `state.Memory` is not goroutine-safe).
5. HTTP handler (`conduit/http/`) is updated to accept `conversation.Store`; sessions attach to conversations by UUID.
6. `POST /sessions` supports optional `conversation_id` to attach to an existing conversation.
7. `GET /conversations` endpoint lists all conversation IDs.
8. TUI example (`examples/tui-chat/`) supports `--conversation <uuid>` flag to attach to existing conversation.
9. [inferred] TUI and HTTP examples print the conversation UUID on creation so users can note it for cross-conduit attachment.
10. [inferred] Examples support `STORE_DIR` env var to opt into `JSONStore` persistence.
11. All existing tests continue to pass; new tests use `go test -race ./...`.
12. [inferred] No changes to `artifact.Artifact` interface or `state.State` interface — serialization is isolated in `conversation/`.

## Task Breakdown

### Task 1: Add Artifact and State JSON Serialization in conversation Package
- **Goal**: Implement round-trip JSON serialization for `[]state.Turn` and `[]artifact.Artifact` (non-delta types) within `conversation/` package.
- **Dependencies**: None.
- **Files Affected**: None (new package).
- **New Files**:
  - `conversation/serialize.go` — registry of artifact kind → factory function; `marshalTurns`, `unmarshalTurns` helpers
  - `conversation/serialize_test.go` — table-driven tests for each artifact type and full turn sequences
- **Interfaces**:
  ```go
  var artifactRegistry = map[string]func() artifact.Artifact{
      "text":        func() artifact.Artifact { return &artifact.Text{} },
      "tool_call":   func() artifact.Artifact { return &artifact.ToolCall{} },
      "tool_result": func() artifact.Artifact { return &artifact.ToolResult{} },
      "usage":       func() artifact.Artifact { return &artifact.Usage{} },
      "image":       func() artifact.Artifact { return &artifact.Image{} },
      "reasoning":   func() artifact.Artifact { return &artifact.Reasoning{} },
  }
  ```
- **Validation**: `go test -race ./conversation/...` passes. Serialization tests cover empty state, single turn, multiple turns, and all artifact types.
- **Details**: The JSON format mirrors `state.Turn` with `role` (string) and `artifacts` (array of `{kind, data}` objects). The `data` field contains the artifact-specific JSON. Delta artifacts are rejected during serialization with a clear error. The registry is package-private; future custom artifact types can extend it.

### Task 2: Create conversation Package with Store Interface and MemoryStore
- **Goal**: Define the `Store` interface, `Conversation` struct with locking, and an in-memory implementation.
- **Dependencies**: Task 1 (for `Conversation` JSON methods, though `MemoryStore` itself does not use them).
- **Files Affected**: None.
- **New Files**:
  - `conversation/doc.go` — package documentation
  - `conversation/store.go` — `Store` interface, `Conversation` struct
  - `conversation/memory.go` — `MemoryStore` implementation
  - `conversation/memory_test.go` — table-driven tests for `Create`, `Get`, `Save`, `Delete`
- **Interfaces**:
  ```go
  type Store interface {
      Create() (*Conversation, error)
      Get(id string) (*Conversation, bool)
      Save(conv *Conversation) error
      Delete(id string) bool
  }

  type Conversation struct {
      ID        string
      State     *state.Memory
      CreatedAt time.Time
      UpdatedAt time.Time
      mu        sync.Mutex
      busy      bool
  }

  func (c *Conversation) Lock() bool
  func (c *Conversation) Unlock()
  ```
- **Validation**: `go test -race ./conversation/...` passes. `MemoryStore` tests verify concurrent access, lock behavior, and UUID generation.
- **Details**: `Store.Create()` generates a UUID (random 128-bit hex, same pattern as HTTP session IDs). `Conversation.Lock()` returns `false` if already busy. `Save()` for `MemoryStore` updates `UpdatedAt` and stores the pointer. The `Conversation` struct is placed in `store.go` alongside the `Store` interface.

### Task 3: Implement JSON-on-Disk Store
- **Goal**: Persist conversations as individual `.json` files in a directory using atomic write-and-rename.
- **Dependencies**: Task 1 (serialization), Task 2 (Store interface and Conversation struct).
- **Files Affected**: None.
- **New Files**:
  - `conversation/json.go` — `JSONStore` implementation
  - `conversation/json_test.go` — tests using `os.MkdirTemp` for isolation
- **Interfaces**:
  ```go
  type JSONStore struct {
      dir string
      mu  sync.RWMutex
      // in-memory cache of loaded conversations
      cache map[string]*Conversation
  }

  func NewJSONStore(dir string) (*JSONStore, error)
  ```
- **Validation**: `go test -race ./conversation/...` passes. Tests verify: create → file exists → save → file updated → get loads from file → delete removes file → restart (new store instance) recovers all conversations.
- **Details**: File naming: `{uuid}.json`. Write atomically using temp file + `os.Rename`. On `Get()`, if not in cache, attempt to load from disk. On `Save()`, write to disk and update cache. On `Delete()`, remove file and delete from cache. Return errors from file I/O with `fmt.Errorf` wrapping.

### Task 4: Update HTTP Handler to Use Conversation Store
- **Goal**: Wire `conversation.Store` into HTTP handler; sessions attach to conversations; state persists per turn.
- **Dependencies**: Task 2 (Store interface and MemoryStore).
- **Files Affected**:
  - `conduit/http/handler.go` — add `convStore conversation.Store` field; update `NewHandler`; update `createSession` to create/attach conversations; update `sendMessage` to `Save` after turn; add `listConversations` handler
  - `conduit/http/session.go` — `Session` holds `*conversation.Conversation` instead of `*state.Memory`; `State()` returns `conv.State`; `Lock()`/`Unlock()` delegate to conversation
  - `conduit/http/handler_test.go` — update tests to provide `conversation.MemoryStore`; add test for attaching to existing conversation
- **New Files**: None.
- **Interfaces**:
  ```go
  func NewHandler(
      store conversation.Store,
      newStep func() *loop.Step,
      messageHandler MessageHandler,
  ) *Handler
  ```
- **Validation**: `go test -race ./conduit/http/...` passes. All existing handler tests still pass with minimal changes.
- **Details**: `POST /sessions` body now accepts optional `{"conversation_id": "..."}`. If provided and found, attaches to existing conversation; if not found, returns 404. If omitted, creates new conversation via `store.Create()`. Response body includes both `id` (session/conversation ID) and `events_url`. `GET /conversations` returns JSON array of `{"id": "...", "created_at": "...", "updated_at": "..."}`. `sendMessage` saves conversation state after the message handler completes (while lock is still held, before `defer Unlock()` returns).

### Task 5: Update TUI Example with --conversation Flag
- **Goal**: TUI can create new or attach to existing conversations; prints UUID on startup; saves after each turn.
- **Dependencies**: Task 2 (Store interface and MemoryStore).
- **Files Affected**:
  - `examples/tui-chat/main.go` — add `flag` parsing for `--conversation`; create/load conversation; print UUID; save after each turn
- **New Files**: None.
- **Interfaces**: No new exported interfaces.
- **Validation**: `go build ./examples/tui-chat` succeeds.
- **Details**: Parse `--conversation` using stdlib `flag` package. Create `conversation.MemoryStore` (or `JSONStore` if `STORE_DIR` env var set). If `--conversation` provided: `store.Get(id)`; if not found, `log.Fatal` with clear message. If not provided: `store.Create()`, print the new UUID to stderr via `slog.Info`. After each `react.Run()` completes, call `store.Save(conv)`. The TUI's `loop.Step` remains local to the TUI process — shared state, not shared runtime.

### Task 6: Update HTTP Example with JSON Store Support
- **Goal**: HTTP example demonstrates persistent JSON store via `STORE_DIR` env var.
- **Dependencies**: Task 3 (JSONStore), Task 4 (HTTP handler updated).
- **Files Affected**:
  - `examples/http-chat/main.go` — create `conversation.JSONStore` or `conversation.MemoryStore` based on `STORE_DIR`; pass to `httpc.NewHandler`
- **New Files**: None.
- **Interfaces**: No new exported interfaces.
- **Validation**: `go build ./examples/http-chat` succeeds.
- **Details**: If `STORE_DIR` is set and directory exists (or can be created), use `conversation.NewJSONStore`. Otherwise use `conversation.MemoryStore`. Pass the store as the first argument to `httpc.NewHandler`. The step factory and message handler remain unchanged.

### Task 7: Integration Test for Cross-Conduit Continuity
- **Goal**: Automated test verifying a conversation created via HTTP can be loaded and continued via the store abstraction.
- **Dependencies**: Tasks 1–6.
- **Files Affected**: None.
- **New Files**:
  - `conversation/integration_test.go` — or `examples/integration_test.go` if package boundaries allow
- **Interfaces**: No new exported interfaces.
- **Validation**: `go test -race ./conversation/...` (or relevant package) passes.
- **Details**: Test procedure:
  1. Create `JSONStore` in a temp directory.
  2. Create a `Conversation` via `store.Create()`.
  3. Append a user turn and an assistant turn to `conv.State`.
  4. `store.Save(conv)`.
  5. Create a **new** `JSONStore` instance pointing to the same directory (simulating process restart).
  6. `store.Get(id)` and verify turns match.
  7. Verify `CreatedAt` is preserved; `UpdatedAt` reflects the save.
  This validates persistence and round-trip serialization without requiring a running HTTP server or TUI.

### Task 8: Documentation and Example README Updates
- **Goal**: Update package docs and example usage to reflect the new conversation store pattern.
- **Dependencies**: Tasks 4, 5, 6 (so examples are stable before documenting).
- **Files Affected**:
  - `README.md` — add `conversation/` to architecture diagram; describe cross-conduit continuity
  - `examples/tui-chat/main.go` — top-level comment block updated with `--conversation` usage
  - `examples/http-chat/main.go` — top-level comment block updated with `STORE_DIR` usage and conversation listing
- **New Files**: None.
- **Interfaces**: No new exported interfaces.
- **Validation**: `go build ./...` passes; documentation is consistent with code.
- **Details**: Document the store adapter pattern (in-memory vs JSON). Explain that conduits attach to conversations rather than owning them. Provide a concrete walkthrough: create via `curl`, note UUID, continue in TUI. Keep the TUI and HTTP example comment blocks up to date with the new flags and env vars.

## Dependency Graph

- Task 1 (serialization) || Task 2 (conversation package + MemoryStore)
- Task 1 + Task 2 → Task 3 (JSONStore)
- Task 2 → Task 4 (HTTP handler update)
- Task 2 → Task 5 (TUI example update)
- Task 3 + Task 4 → Task 6 (HTTP example update)
- Task 4 || Task 5 || Task 6 → Task 7 (integration test)
- Task 4 + Task 5 + Task 6 → Task 8 (documentation)

**Critical path**: Task 1 → Task 3 → Task 6 → Task 7 (serialization must work before JSON store; JSON store must work before HTTP example; HTTP example must be stable before integration test).

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| `artifact.Artifact` interface makes JSON serialization fragile | High | Medium | Registry pattern with explicit kind-to-type mapping; serialization isolated in `conversation/` so changes are localized. Tests must cover all known artifact types. |
| `state.Memory` is not goroutine-safe; concurrent conduits on same conversation race | High | Medium | Per-conversation `Lock()`/`Unlock()` in `Conversation` struct. HTTP handler already has session locking pattern — migrate to conversation-level. Document that `Store.Save()` should be called while lock is held. |
| HTTP handler tests break due to `NewHandler` signature change | Medium | High | Update `handler_test.go` to pass `conversation.MemoryStore`. All existing tests should require only one-line changes. Verify with `go test -race ./conduit/http/...`. |
| JSON store file corruption on crash | Medium | Low | Atomic write-and-rename pattern. Temp file in same directory, then `os.Rename`. |
| TUI example becomes harder to run for quick demos | Low | Medium | Default behavior (no `--conversation`) still creates a new anonymous conversation and prints the UUID. No extra setup required. |
| Custom artifact types from external packages cannot be persisted | Medium | Low | Out of scope for this plan. Document limitation. Future work can expose `RegisterKind` from `conversation/` or move registry to `artifact/`. |
| Circular dependencies if `conversation/` imports too much | Medium | Low | Strictly limit imports to `artifact/` and `state/`. No imports of `loop/`, `provider/`, `conduit/`, `cognitive/`. |

## Validation Criteria

- [ ] `go test -race ./...` passes with no new race conditions.
- [ ] `go build ./examples/tui-chat` and `go build ./examples/http-chat` succeed.
- [ ] A new conversation created via `curl -X POST http://localhost:8080/sessions` returns a UUID.
- [ ] The same UUID can be passed to `go run ./examples/tui-chat --conversation <uuid>` and the TUI loads the conversation state.
- [ ] With `STORE_DIR=/tmp/ore-store go run ./examples/http-chat`, restarting the process preserves all previously created conversations.
- [ ] The `conversation/` package has `>80%` test coverage for serialization and both store implementations.
- [ ] HTTP handler tests cover: creating new conversation, attaching to existing conversation, listing conversations, saving after message.
- [ ] No changes to `artifact.Artifact`, `state.State`, `provider.Provider`, or `loop.Step` interfaces.
- [ ] `README.md` and example comment blocks accurately describe the new flags and env vars.
