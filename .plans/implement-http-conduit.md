# Plan: Implement HTTP Conduit for Streaming Chat over HTTP

## Objective

Implement an HTTP-based conduit library (`conduit/http/`) and a reference example application (`examples/http-chat/`) that expose the ore framework's conversation primitives (`loop.Step`, `cognitive.ReAct`, `state.Memory`) over HTTP. The server maintains ephemeral per-session state, runs the full ReAct loop (inference + tool resolution) server-side, and supports both synchronous NDJSON streaming responses and an optional session-wide SSE ambient channel for real-time events.

## Context

The ore framework (`github.com/andrewhowdencom/ore`) is a minimal, composable framework for building agentic applications. It has the following package structure and conventions (from `AGENTS.md`):

- **artifact/** — extensible Artifact interface (`Kind() string`) with concrete types: `Text`, `ToolCall`, `ToolResult`, `Image`, `Usage`, `Reasoning`, plus streaming deltas (`TextDelta`, `ReasoningDelta`, `ToolCallDelta`). Artifacts have **no JSON serialization support** — this is a critical finding for the HTTP API.
- **state/** — `State` interface (`Turns()`, `Append()`) with in-memory `Memory` implementation. Not goroutine-safe by design.
- **provider/** — `Provider` interface (`Invoke(ctx, State, chan<- Artifact, ...Option) error`) with `Tool` descriptor.
- **provider/openai/** — Concrete OpenAI adapter using stdlib `net/http` and `encoding/json`.
- **loop/** — `Step` orchestrates inference turns with embedded `FanOut` for event distribution. `FanOut.Subscribe(kinds ...string)` filters events by `Kind()`. `Handler` processes assistant-turn artifacts (e.g., `tool.Handler` executes tool calls and appends `RoleTool` turns).
- **cognitive/** — `ReAct` pattern loops `Step.Turn()` while the last turn is not from the assistant, driving tool-call resolution.
- **conduit/** — `Conduit` interface (`Capable`, `Events() <-chan Event`) with `Descriptor` capability model. The TUI (`conduit/tui/`) is the only existing implementation.
- **tool/** — `Registry` maps tool names to `ToolFunc`; `Handler` implements `loop.Handler` to execute tool calls.
- **examples/tui-chat/** — Reference application composing TUI, ReAct, OpenAI provider, and tool calling.
- **examples/calculator/** — Reference application demonstrating tool calling with `loop.Handler` and `cognitive.ReAct`.

Key conventions observed:
- Table-driven tests; `go test -race ./...` always required.
- `httptest.Server` for mocking HTTP APIs.
- Minimal external dependencies — stdlib preferred.
- `fmt.Errorf("...: %w", err)` for error wrapping.
- `log/slog` for lifecycle events.
- Functional options pattern for constructors.

## Architectural Blueprint

The `conduit/http/` package is **not** a `conduit.Conduit` implementation — the `Conduit` interface requires a Go channel (`Events() <-chan Event`), which is incompatible with HTTP request/response semantics. Instead, it is an HTTP handler library that directly composes ore framework primitives.

**Selected architecture:**

1. **Library (`conduit/http/`)** exports a `Handler` struct with methods mountable on any `http.ServeMux`. It accepts a `provider.Provider` and a `func() *loop.Step` factory (each session needs its own `Step` with isolated `FanOut`).
2. **Session store** is an in-memory, mutex-protected map. Each session holds a `*state.Memory`, a `*loop.Step`, and a busy flag to prevent concurrent turns (HTTP 409 Conflict).
3. **Message endpoint** (`POST /sessions/{id}/messages`) runs the full `cognitive.ReAct` loop server-side. It appends the user message via `Step.Submit()`, then loops `Step.Turn()` + tool execution until the assistant responds without pending tool calls.
4. **Response format** is always NDJSON (`application/x-ndjson`) — one JSON object per line, flushed incrementally. Clients choose to stream or buffer.
5. **Event filtering** via `?kinds=` query parameter on both POST and SSE endpoints, mapping directly to `FanOut.Subscribe(kinds ...string)`.
6. **SSE ambient channel** (`GET /sessions/{id}/events`) provides a persistent, session-wide event stream independent of the per-request NDJSON response.
7. **Example application** (`examples/http-chat/`) demonstrates composition: OpenAI provider → tool registry → `loop.Step` factory → HTTP handler → `http.Server`.

**No Tree-of-Thought deliberation required** — the RFC (GitHub issue #60) converged the design through prior ideation.

## Requirements

1. Create `conduit/http/` package with HTTP handler methods and session store.
2. Implement NDJSON streaming response writer for the message endpoint.
3. Implement SSE event stream writer for the ambient channel endpoint.
4. Define JSON DTOs and serialization for all artifact and event types (framework artifacts lack JSON support).
5. Implement ephemeral session lifecycle: create, lookup, delete, busy-state tracking.
6. Run server-side ReAct loop within the message handler; client does not execute tools.
7. Support event filtering via `?kinds=` query parameter on both endpoints.
8. Create `examples/http-chat/` reference application demonstrating composition.
9. All code must have table-driven tests; `go test -race ./...` must pass.
10. Use only stdlib dependencies (no new external deps).

## Task Breakdown

### Task 1: Define JSON DTOs and serialization for artifacts and events
- **Goal**: Create JSON-serializable data types and marshal/unmarshal functions for all artifact types and loop output events, since the framework artifacts lack JSON tags.
- **Dependencies**: None.
- **Files Affected**: `artifact/artifact.go` (read for type definitions), `loop/loop.go` (read for event types), `conduit/event.go` (read for conduit event types).
- **New Files**: `conduit/http/types.go`, `conduit/http/types_test.go`.
- **Interfaces**:
  - `func MarshalArtifact(art artifact.Artifact) ([]byte, error)` — polymorphic marshal based on `Kind()`.
  - `func UnmarshalArtifact(data []byte) (artifact.Artifact, error)` — demarshal based on `kind` field.
  - `func MarshalOutputEvent(event loop.OutputEvent) ([]byte, error)` — marshal `TurnCompleteEvent`, `ErrorEvent`, and artifact events.
  - Corresponding JSON DTO structs for each artifact kind (e.g., `jsonText`, `jsonToolCall`, `jsonTextDelta`).
- **Validation**: `go test -race ./conduit/http/` passes with table-driven tests covering all artifact types, delta types, and event types.
- **Details**: The DTOs must exactly mirror the fields of the framework types (e.g., `Text.Content`, `ToolCall.ID/Name/Arguments`, `TurnCompleteEvent.Turn`). Implement type-switching in marshal/unmarshal based on `Kind()`. Test round-trip serialization for every artifact and event kind.

### Task 2: Implement ephemeral session store
- **Goal**: Create a thread-safe, in-memory session store with create, lookup, and delete, and a Session struct that tracks busy state to prevent concurrent turns.
- **Dependencies**: None (can proceed in parallel with Task 1).
- **Files Affected**: `state/memory.go` (read for `Memory` struct), `state/state.go` (read for `State` interface), `loop/loop.go` (read for `Step` struct).
- **New Files**: `conduit/http/session.go`, `conduit/http/session_test.go`.
- **Interfaces**:
  - `type Session struct { id string; state *state.Memory; step *loop.Step; mu sync.Mutex; busy bool }`
  - `type SessionStore struct { sessions map[string]*Session; mu sync.RWMutex }`
  - `func (s *SessionStore) Create(id string, step *loop.Step) *Session`
  - `func (s *SessionStore) Get(id string) (*Session, bool)`
  - `func (s *SessionStore) Delete(id string) bool`
  - `func (s *Session) Lock() bool` — returns false if already busy; true if acquired.
  - `func (s *Session) Unlock()`
- **Validation**: `go test -race ./conduit/http/` passes with tests for concurrent create/get/delete, busy-state contention, and session lifecycle.
- **Details**: Use `crypto/rand` + `encoding/hex` for session ID generation (stdlib only, no UUID library). The `Session` struct owns a `*loop.Step` created by the handler's factory. The busy flag prevents a second `POST /messages` from starting while a ReAct loop is in progress (returns HTTP 409). `state.Memory` is not goroutine-safe, so all state mutations during a turn must hold the session mutex.

### Task 3: Implement HTTP handler skeleton with routing and documentation
- **Goal**: Create the `Handler` struct, constructor, `ServeMux` convenience method, and stub route handlers. Add package documentation (`doc.go`).
- **Dependencies**: Task 2 (session store).
- **Files Affected**: `provider/provider.go` (read for `Provider` interface), `loop/loop.go` (read for `Step` and `New`), `conduit/tui/tui.go` (read for TUI conduit patterns), `examples/tui-chat/main.go` (read for application wiring patterns).
- **New Files**: `conduit/http/handler.go`, `conduit/http/handler_test.go`, `conduit/http/doc.go`.
- **Interfaces**:
  - `type Handler struct { provider provider.Provider; newStep func() *loop.Step; store *SessionStore }`
  - `func NewHandler(p provider.Provider, newStep func() *loop.Step) *Handler`
  - `func (h *Handler) ServeMux() *http.ServeMux`
  - Stub handlers: `CreateSession`, `DeleteSession`, `SendMessage`, `SessionEvents` — all returning 501 Not Implemented.
- **Validation**: `go test -race ./conduit/http/` passes with routing tests using `httptest.Server` to verify all endpoints are registered and return 501.
- **Details**: Follow the functional options pattern convention for `NewHandler` if needed (e.g., `With...` options for future extensibility). The `doc.go` should follow the pattern of `conduit/tui/tui.go` package documentation. Register routes with exact paths: `POST /sessions`, `DELETE /sessions/{id}/`, `POST /sessions/{id}/messages`, `GET /sessions/{id}/events`.

### Task 4: Implement session management endpoints (Create / Delete)
- **Goal**: Wire `CreateSession` and `DeleteSession` handlers with JSON request/response, session store operations, and proper HTTP status codes.
- **Dependencies**: Task 3 (handler skeleton), Task 2 (session store), Task 1 (JSON types for response serialization — `CreateSession` returns JSON).
- **Files Affected**: `conduit/http/handler.go`.
- **New Files**: (tests added to `conduit/http/handler_test.go`).
- **Interfaces**: (no new interfaces; existing stub handlers are fleshed out).
- **Validation**: `go test -race ./conduit/http/` passes with tests for creating a session (201 Created with JSON body), deleting a session (204 No Content), and accessing a non-existent session (404 Not Found).
- **Details**: `CreateSession` generates an ID, calls the `newStep` factory to create a per-session `*loop.Step`, initializes `state.Memory`, stores the session, and returns `{"id": "...", "events_url": "/sessions/{id}/events"}`. `DeleteSession` looks up the session, closes its `Step` (`step.Close()`), removes it from the store, and returns 204.

### Task 5: Implement message endpoint with NDJSON streaming and ReAct loop
- **Goal**: Implement `SendMessage` handler that parses the user message, submits it to session state, runs the full `cognitive.ReAct` loop, and streams all new turns as NDJSON.
- **Dependencies**: Task 4 (session endpoints), Task 1 (JSON serialization).
- **Files Affected**: `conduit/http/handler.go`, `cognitive/react.go` (read for ReAct loop logic).
- **New Files**: `conduit/http/stream.go`, `conduit/http/handler_test.go` (add tests).
- **Interfaces**:
  - `type ndjsonWriter struct { w http.ResponseWriter; enc *json.Encoder; flusher http.Flusher }`
  - `func newNDJSONWriter(w http.ResponseWriter) *ndjsonWriter`
  - `func (nw *ndjsonWriter) WriteEvent(v interface{}) error` — encodes one JSON object and flushes.
- **Validation**: `go test -race ./conduit/http/` passes with tests using a mock `provider.Provider` (local struct implementing `Invoke`) and `httptest.ResponseRecorder` (which implements `http.Flusher`). Verify NDJSON line count, event ordering, and final `complete` object.
- **Details**: The handler must: (1) parse request JSON (`{"content": "...", "kinds": [...]}`), (2) lock session (return 409 if busy), (3) `Step.Submit(RoleUser, Text)`, (4) create `cognitive.ReAct{Step: session.step, Provider: h.provider}`, (5) subscribe to the Step's FanOut for requested kinds, (6) run `react.Run(ctx, session.state)`, (7) stream all FanOut events via `ndjsonWriter`, (8) append a final `complete` object with all new turns, (9) unlock session. The `ndjsonWriter` uses `json.NewEncoder` + `w.(http.Flusher).Flush()` after each line. Set `Content-Type: application/x-ndjson` and `Transfer-Encoding: chunked` implicitly via flushing.

### Task 6: Implement SSE ambient channel endpoint
- **Goal**: Implement `SessionEvents` handler that establishes a persistent SSE connection, subscribes to the session's `Step.FanOut`, and streams filtered events in SSE format.
- **Dependencies**: Task 4 (session endpoints), Task 1 (JSON serialization).
- **Files Affected**: `conduit/http/handler.go`, `loop/fanout.go` (read for FanOut subscription API).
- **New Files**: `conduit/http/sse.go`, `conduit/http/handler_test.go` (add tests).
- **Interfaces**:
  - `type sseWriter struct { w http.ResponseWriter; flusher http.Flusher }`
  - `func newSSEWriter(w http.ResponseWriter) *sseWriter`
  - `func (sw *sseWriter) WriteEvent(kind string, data []byte) error` — writes `event: {kind}\ndata: {data}\n\n` and flushes.
- **Validation**: `go test -race ./conduit/http/` passes with tests for SSE event formatting, connection lifecycle, and disconnection cleanup (subscriber channel closed).
- **Details**: The handler must: (1) accept `?kinds=` query parameter, (2) look up session (404 if missing), (3) subscribe to `session.step.Subscribe(kinds...)`, (4) set `Content-Type: text/event-stream`, (5) read from subscriber channel in a loop, (6) marshal each event to JSON and write via `sseWriter`, (7) detect client disconnect via `r.Context().Done()`, (8) unsubscribe / close subscription on disconnect. If the session's Step is closed (session deleted), the subscriber channel closes and the handler returns cleanly.

### Task 7: Create example application `examples/http-chat/`
- **Goal**: Create a reference application demonstrating composition of the HTTP conduit with an OpenAI provider, tool registry, and `http.Server`.
- **Dependencies**: Task 5, Task 6.
- **Files Affected**: `examples/calculator/main.go` (read for tool wiring patterns), `examples/tui-chat/main.go` (read for ReAct wiring patterns).
- **New Files**: `examples/http-chat/main.go`.
- **Interfaces**: (application-level wiring; no new exported interfaces).
- **Validation**: `go build ./examples/http-chat/` succeeds.
- **Details**: The example should mirror `examples/tui-chat/main.go` and `examples/calculator/main.go`. It must: (1) read `ORE_API_KEY`, `ORE_MODEL`, `ORE_BASE_URL`, and `PORT` from environment, (2) create an OpenAI provider, (3) optionally create a tool registry with demo tools (e.g., calculator), (4) create a `loop.Step` factory that includes tool handlers and provider tool options, (5) create the HTTP handler via `httpc.NewHandler(prov, stepFactory)`, (6) mount the handler's `ServeMux()` on an `http.Server`, (7) start the server and log the listening address. Use `slog` for lifecycle logging.

## Dependency Graph

- Task 1 → Task 5 (Task 5 needs JSON serialization)
- Task 1 → Task 6 (Task 6 needs JSON serialization)
- Task 2 → Task 3 (Task 3 needs session store)
- Task 3 → Task 4 (Task 4 needs handler skeleton)
- Task 4 → Task 5 (Task 5 needs session endpoints)
- Task 4 → Task 6 (Task 6 needs session endpoints)
- Task 5 || Task 6 (Task 5 and Task 6 are parallelizable)
- Task 5 + Task 6 → Task 7 (Task 7 needs working message and SSE endpoints)

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| JSON DTOs drift from framework artifact types | High | Medium | Comprehensive table-driven round-trip tests for every artifact kind; test failures are early warning. |
| Concurrent session access causes data races | High | Medium | Session-level mutex + busy flag; always run `go test -race ./...`; use `sync.RWMutex` for store. |
| HTTP response flushing fails silently | Medium | Low | Use `http.Flusher` interface assertion; test with `httptest.ResponseRecorder` (implements Flusher). |
| SSE client disconnect leaks goroutines | Medium | Medium | Subscribe to `r.Context().Done()`; close FanOut subscription on disconnect; defer cleanup. |
| Long ReAct loops block HTTP connections | Medium | Medium | Document expected behavior; client uses NDJSON streaming to observe progress; no timeout in MVP. |
| Session Step FanOut drops events before SSE subscribes | Low | High | Documented limitation: SSE is best-effort ambient; NDJSON POST response is canonical ground truth. |

## Validation Criteria

- [ ] `go test -race ./...` passes with zero failures.
- [ ] `go build ./examples/http-chat/` succeeds without errors.
- [ ] `go vet ./...` is clean.
- [ ] All new files in `conduit/http/` have corresponding `*_test.go` files with table-driven tests.
- [ ] HTTP handler tests use `httptest.Server` or `httptest.ResponseRecorder` for all endpoint testing.
- [ ] Example application starts and responds to `curl -X POST /sessions` with a valid JSON session object.
- [ ] Example application accepts `POST /sessions/{id}/messages` and returns NDJSON lines.
- [ ] Example application's `GET /sessions/{id}/events` endpoint returns properly formatted SSE events when accessed with `Accept: text/event-stream`.
- [ ] No new external dependencies added to `go.mod` (stdlib only).
- [ ] Package documentation in `conduit/http/doc.go` follows existing `doc.go` conventions.
