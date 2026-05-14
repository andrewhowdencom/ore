# Plan: Rename Conversation to Thread

## Objective

Rename the `conversation/` package, its exported `Conversation` type, and all
references across the codebase to `thread/` and `Thread`. The current name
"Conversation" is too human-centric and misleading for autonomous, non-interactive
conduits (webhooks, batch jobs, scheduled agents). "Thread" — analogous to a
Slack thread — captures a neutral, turn-ordered sequence without implying social
dialogue.

This is a pure mechanical refactor: zero behavior changes, only identifier and
path renames.

## Context

The codebase was mapped via `find` and `grep`. The `conversation/` package
contains 10 `.go` files (~1,100 lines including tests). It is imported by:

- `conduit/http/` (handler, session, tests)
- `examples/http-chat/main.go`
- `examples/tui-chat/main.go`

Generic uses of "conversation" as an abstract concept in `state/`, `cognitive/`,
`provider/`, `AGENTS.md`, and TUI view comments are **not** part of this rename.
They describe the turn-based interaction pattern, not the `Conversation` entity.

The `conversation/` package is fully implemented and tested (85.2% coverage).
All tests pass (`go test -race ./...`). The rename must preserve that.

## Architectural Blueprint

No architecture changes. The rename follows a simple rule:

| Old | New |
|---|---|
| `conversation/` | `thread/` |
| `package conversation` | `package thread` |
| `Conversation` struct | `Thread` struct |
| `*Conversation` | `*Thread` |
| `Store` interface method signatures (parameter/return types only) | same method names, `Thread` types |
| `jsonConversation` helper | `jsonThread` |
| `conversations` (map key / variable) | `threads` |
| `conv` (variable / field) | `thread` |
| `convStore` | `threadStore` |
| `/conversations` | `/threads` |
| `conversation_id` | `thread_id` |
| `--conversation` | `--thread` |
| Test names containing `Conversation` | `Thread` |

Import paths update from `github.com/andrewhowdencom/ore/conversation` to
`github.com/andrewhowdencom/ore/thread`.

## Requirements

1. [R1] Rename directory `conversation/` → `thread/`.
2. [R2] Update package declarations, type names, variable names, field names, and
   comments inside `thread/`.
3. [R3] Update all imports and references in `conduit/http/`, `examples/`, and
   `README.md`.
4. [R4] Update HTTP API route `/conversations` → `/threads` and JSON field
   `conversation_id` → `thread_id`.
5. [R5] Update TUI example flag `--conversation` → `--thread`.
6. [R6] Update test names (`TestConversation_*` → `TestThread_*`, etc.).
7. [R7] Preserve `go test -race ./...` PASS and `thread/` coverage ≥ 80%.
8. [R8] Generic "conversation" concept references in `state/`, `cognitive/`,
   `provider/`, `AGENTS.md`, and TUI view comments remain unchanged.

## Task Breakdown

### Task 1: Rename Package and All Code References
- **Goal**: Move `conversation/` → `thread/`, rename all types/variables/imports/
  comments across Go source, and verify `go build ./...` compiles.
- **Dependencies**: None.
- **Files Affected**:
  - `conversation/doc.go` → `thread/doc.go`
  - `conversation/store.go` → `thread/store.go`
  - `conversation/memory.go` → `thread/memory.go`
  - `conversation/memory_test.go` → `thread/memory_test.go`
  - `conversation/json.go` → `thread/json.go`
  - `conversation/json_test.go` → `thread/json_test.go`
  - `conversation/serialize.go` → `thread/serialize.go`
  - `conversation/serialize_test.go` → `thread/serialize_test.go`
  - `conversation/integration_test.go` → `thread/integration_test.go`
  - `conduit/http/handler.go`
  - `conduit/http/handler_test.go`
  - `conduit/http/session.go`
  - `conduit/http/session_test.go`
  - `examples/http-chat/main.go`
  - `examples/tui-chat/main.go`
- **New Files**: None (all are renames/edits).
- **Interfaces**:
  - `thread.Store` method signatures: `Create() (*Thread, error)`,
    `Get(id string) (*Thread, bool)`, `Save(th *Thread) error`,
    `Delete(id string) bool`, `List() ([]*Thread, error)`.
  - `thread.Thread` struct (formerly `Conversation`) — fields unchanged.
  - `http.Handler` field `convStore` → `threadStore`.
  - `http.Session` field `conv *conversation.Conversation` → `thread *thread.Thread`.
  - HTTP request body struct field `ConversationID string` → `ThreadID string`
    with JSON tag `thread_id`.
  - HTTP route `GET /conversations` → `GET /threads`.
- **Validation**:
  - `go build ./...` passes with zero errors.
  - `go test -race ./thread/...` passes.
- **Details**:
  1. Move all files from `conversation/` to `thread/` (or copy and delete old
     directory).
  2. In every moved file, change `package conversation` → `package thread`.
  3. Rename `Conversation` struct → `Thread` everywhere (type definitions,
     receiver methods, interface signatures, variable declarations, comments,
     test names).
  4. Rename `jsonConversation` → `jsonThread` in `store.go`.
  5. Rename `conversations` map/key names → `threads` in `memory.go` and
     `json.go`.
  6. Update error message strings: `"marshal conversation:"` →
     `"marshal thread:"`, `"unmarshal conversation:"` → `"unmarshal thread:"`,
     etc.
  7. In `conduit/http/handler.go`:
     - Update import path to `github.com/andrewhowdencom/ore/thread`.
     - Rename field `convStore` → `threadStore`.
     - Rename handler `listConversations` → `listThreads`.
     - Update route registration `GET /conversations` → `GET /threads`.
     - Rename struct field `ConversationID` → `ThreadID` (JSON tag `thread_id`).
     - Update local variable `conv` → `thread`.
     - Update comments referencing the entity.
  8. In `conduit/http/session.go`:
     - Update import path.
     - Rename field `conv` → `thread`.
     - Update comments.
  9. In `conduit/http/handler_test.go` and `session_test.go`:
     - Update import path.
     - Update all `*conversation.Conversation` → `*thread.Thread` in mock store
       method signatures.
     - Update test names (`TestConversation_*` → `TestThread_*`).
     - Update request paths and JSON bodies (`/conversations` → `/threads`,
       `conversation_id` → `thread_id`).
     - Update variable names (`conv` → `thread`).
  10. In `examples/http-chat/main.go`:
      - Update import path.
      - Rename `convStore` → `threadStore`.
      - Update `conversation.NewMemoryStore()` → `thread.NewMemoryStore()` and
        `conversation.NewJSONStore()` → `thread.NewJSONStore()`.
      - Update comments and sample `curl` commands.
  11. In `examples/tui-chat/main.go`:
      - Update import path.
      - Rename flag `--conversation` → `--thread` and variable
        `conversationID` → `threadID`.
      - Rename local variable `conv` → `thread`.
      - Update `conversation.NewMemoryStore()` → `thread.NewMemoryStore()` and
        `conversation.NewJSONStore()` → `thread.NewJSONStore()`.
      - Update error/log messages and comments.
  12. Run `go build ./...` and fix any remaining compilation errors.
  13. Run `go test -race ./thread/...` to confirm package tests pass.

### Task 2: Update README.md Documentation
- **Goal**: Rename the "Conversations" section to "Threads" and update all
  package/type/flag references in README.md.
- **Dependencies**: Task 1 (so links and type names match the code).
- **Files Affected**: `README.md`
- **New Files**: None.
- **Interfaces**: None.
- **Validation**: Visual review of README.md shows no remaining `Conversation` or
  `conversation/` references outside of generic "conversation" concept mentions.
- **Details**:
  1. Change section heading `### Conversations` → `### Threads`.
  2. Update body text: `conversation/` → `thread/`, `Conversation` → `Thread`,
     `conversations` → `threads` where referring to the entity.
  3. Update CLI example: `--conversation <uuid>` → `--thread <uuid>`.
  4. Update Project Status bullet for `conversation/` → `thread/`.
  5. Leave generic uses of "conversation" (e.g., "conversation history model",
     "conversation turn loop") unchanged.

### Task 3: Full Verification
- **Goal**: Ensure the entire repository builds, all tests pass, and coverage is
  preserved after the rename.
- **Dependencies**: Task 1, Task 2.
- **Files Affected**: None (read-only verification).
- **New Files**: None.
- **Interfaces**: None.
- **Validation**:
  - `go build ./...` passes.
  - `go test -race ./...` passes.
  - `go test -race -cover ./thread/...` ≥ 80%.
- **Details**:
  1. Run `go build ./...`.
  2. Run `go test -race ./...`.
  3. Run `go test -race -cover ./thread/...` and confirm coverage ≥ 80%.
  4. Do a final `grep -rn 'conversation' --include='*.go' .` and
     `grep -rn 'Conversation' --include='*.go' .` (excluding generic-concept
     packages like `state/`, `cognitive/`, `provider/`, `AGENTS.md`) to catch
     any missed occurrences.

## Dependency Graph

- Task 1 → Task 2 (docs should reference the renamed code)
- Task 1 → Task 3 (full build/test verification)
- Task 2 → Task 3 (docs verification can be combined with code verification)
- Task 2 || Task 3 cannot run fully until Task 1 completes

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Missed import path or variable name causes compilation failure | High | Medium | `go build ./...` in Task 1 catches all compile errors immediately |
| Missed string literal in error message or log output | Low | Medium | Final grep sweep in Task 3 catches remaining references |
| Test coverage drops because test names changed but tests weren't updated | Low | Low | `go test -race -cover ./thread/...` in Task 3 validates coverage |
| `go mod` or build tooling caches old package path | Medium | Low | `go clean -cache` and `rm -rf conversation/` early in Task 1 |

## Validation Criteria

- [ ] `go build ./...` passes with zero errors after Task 1.
- [ ] `go test -race ./...` passes after Task 1.
- [ ] `go test -race -cover ./thread/...` reports ≥ 80% coverage after Task 3.
- [ ] README.md contains no `Conversation` or `conversation/` references that
      should have been renamed (generic concept references are allowed).
- [ ] No `conversation/` directory remains in the repository after Task 1.
