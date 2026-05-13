# Plan: Add Optional Web Chat UI to HTTP Conduit

## Objective

Extend `conduit/http/` with an optional embedded HTML/JS chat client so that running the HTTP example (`examples/http-chat`) gives users an out-of-the-box interactive chat interface at `http://localhost:8080/`. This achieves feature parity with the TUI example (`examples/tui-chat`), makes the HTTP conduit easier to validate and demo, and closes the UX gap between the terminal and web surfaces.

## Context

The `ore` framework exposes conversation primitives through a minimal HTTP handler library in `conduit/http/`. The current `Handler` struct (in `conduit/http/handler.go`) provides four endpoints:

- `POST /sessions` — creates an ephemeral session, returns `{id, events_url}`
- `DELETE /sessions/{id}` — removes a session
- `POST /sessions/{id}/messages` — sends a message, returns an NDJSON stream of delta/complete events
- `GET /sessions/{id}/events` — SSE ambient event stream

Sessions are in-memory (`SessionStore` in `conduit/http/session.go`), locked per-turn to prevent concurrent message processing. JSON serialization of artifacts and events is handled in `conduit/http/types.go`. Streaming uses `sseWriter` (`conduit/http/sse.go`) for SSE and `ndjsonWriter` (`conduit/http/stream.go`) for NDJSON.

The HTTP example (`examples/http-chat/main.go`) composes the handler with an OpenAI provider, calculator tools, and the `cognitive.ReAct` pattern. Users must interact via `curl` or build a custom client — there is no built-in UI.

In contrast, the TUI example (`examples/tui-chat/main.go`) gives an out-of-the-box interactive experience: a scrollable chat history, a text input area, streaming response display, status indicators ("thinking..."), interrupt support (Ctrl+C), and server-side Markdown rendering via `glamour`.

The TUI is implemented as a reusable library in `conduit/tui/`. The HTTP conduit is also a reusable library, but it has no UI layer. Adding an optional embedded web client to the HTTP handler (not a separate package) is the most pragmatic approach because the handler already owns sessions, messages, and events — the UI is just a thin browser-based client that consumes the existing API.

Key project conventions from `AGENTS.md`:
- Core packages at root level; examples under `examples/<name>/`
- Standard library preferred; functional options pattern for constructors
- Table-driven tests; `httptest.Server` for handler tests
- `go test -race ./...` required

## Architectural Blueprint

### Selected Architecture

Add a `WithUI()` functional option to `conduit/http.NewHandler`. When enabled, the `ServeMux()` registers a `GET /` route that serves embedded static files (`index.html` + `chat.js`) via `//go:embed`. The HTML/JS client is a vanilla, dependency-free browser application (except `marked.js` from CDN for Markdown rendering) that:

1. Auto-creates a session on page load via `POST /sessions`
2. Opens an `EventSource` to the session's SSE endpoint for streaming assistant responses
3. Sends user messages via `POST /sessions/{id}/messages`
4. Renders `text_delta` events into a scrollable chat log
5. Uses `marked.parse()` for client-side Markdown→HTML on assistant messages

The UI is served at `GET /` only when `WithUI()` is passed. Without the option, the handler behaves exactly as before — no routes are added, no static files are served.

### Why Not a Separate `conduit/web/` Package?

A separate web surface package (mirroring `conduit/tui/`) was considered but rejected. The TUI implements the `conduit.Surface` interface and provides a full event loop. The web UI is fundamentally a thin client over the existing HTTP API; it does not need its own event model or orchestration. Adding it to `conduit/http/` as an opt-in feature avoids:
- Duplicating session/message logic already in the HTTP handler
- Creating a new package with only thin wrapper code
- Breaking the composability of the existing handler

### Evaluated Alternatives

| Approach | Why Not Selected |
|---|---|
| Separate `conduit/web/` package | Unnecessary indirection; the web client is a consumer of the HTTP API, not a new abstraction layer |
| WebSocket instead of SSE | Overkill; SSE is simpler, natively supported by browsers, and the existing handler already provides SSE endpoints |
| Server-side Markdown rendering | Would require adding a Markdown library dependency (e.g., `blackfriday`) to the HTTP package; client-side `marked.js` is simpler and appropriate for a web frontend |
| Inline HTML/JS in Go strings | Harder to maintain and review; `//go:embed` keeps frontend code in its own files |

### Component Interaction

```
Browser loads http://localhost:8080/
  → GET / → serves embedded index.html
  → index.html loads chat.js
  → chat.js:
      POST /sessions → receives {id, events_url}
      EventSource → GET /sessions/{id}/events (SSE)
      Send button → POST /sessions/{id}/messages
```

## Requirements

1. `WithUI()` functional option shall exist and toggle static file serving in `conduit/http.Handler`
2. Static files (`index.html`, `chat.js`) shall be embedded via `//go:embed` and served at `GET /`
3. The web UI shall auto-create a session on page load via `POST /sessions`
4. The web UI shall stream assistant responses via SSE (`GET /sessions/{id}/events`)
5. The web UI shall send user messages via `POST /sessions/{id}/messages`
6. The web UI shall render streamed `text_delta` events into a scrollable chat log
7. Assistant messages shall render Markdown client-side via `marked.js` CDN
8. The HTML/JS client shall be vanilla (no frontend frameworks)
9. No new Go module dependencies shall be introduced
10. The `examples/http-chat/main.go` example shall be updated to pass `http.WithUI()` to demonstrate the feature
11. All existing tests shall continue to pass

## Task Breakdown

### Task 1: Add UI Option Infrastructure and Static File Embedding
- **Goal**: Add `WithUI()` functional option to `conduit/http.NewHandler`, create `//go:embed` infrastructure, and register a `GET /` route in `ServeMux()` when enabled.
- **Dependencies**: None.
- **Files Affected**:
  - `conduit/http/handler.go` — add `withUI bool` field, `WithUI()` option, `GET /` route in `ServeMux()`, `serveUI` handler method
  - `conduit/http/handler_test.go` — add tests for `WithUI()` option, static file serving, route registration
- **New Files**:
  - `conduit/http/static.go` — `//go:embed static/*` directive and embed.FS variable
  - `conduit/http/static/index.html` — placeholder HTML (minimal structure: `<html><body>ore chat</body></html>`)
  - `conduit/http/static/chat.js` — placeholder JS (`console.log('ore chat loaded')`)
- **Interfaces**:
  ```go
  type Option func(*Handler)
  func WithUI() Option
  func NewHandler(newStep func() *loop.Step, messageHandler MessageHandler, opts ...Option) *Handler
  // Handler gains: withUI bool
  // ServeMux gains: GET / route (conditional)
  // New method: func (h *Handler) serveUI(w stdhttp.ResponseWriter, r *stdhttp.Request)
  ```
- **Validation**:
  - `go test ./conduit/http/...` passes (including new tests)
  - `go build ./...` succeeds
  - `go vet ./...` clean
  - New tests verify:
    - `WithUI()` adds `GET /` route to ServeMux
    - `GET /` returns `text/html` content type
    - `GET /chat.js` returns `application/javascript` content type
    - Without `WithUI()`, `GET /` returns 404
- **Details**: The `NewHandler` signature changes to accept variadic `opts ...Option` at the end. This is backward-compatible because existing two-argument calls still compile. The `serveUI` method reads embedded files from `embed.FS` and serves them with correct content types. Placeholder static files ensure the infrastructure is testable before the full client is implemented.

### Task 2: Implement HTML/JS Chat Client
- **Goal**: Write the full `index.html` and `chat.js` implementing the chat interface.
- **Dependencies**: Task 1.
- **Files Affected**:
  - `conduit/http/static/index.html` — replace placeholder with full chat UI
  - `conduit/http/static/chat.js` — replace placeholder with full client logic
- **New Files**: None.
- **Interfaces**: No Go interfaces. JS functions:
  - `createSession()` — `fetch` POST `/sessions`, store `id` and `events_url`
  - `connectSSE(eventsUrl)` — create `EventSource`, handle `text_delta`, `reasoning_delta`, `tool_call_delta`, `turn_complete`, `error` events
  - `sendMessage(content)` — `fetch` POST `/sessions/{id}/messages`
  - `renderUserMessage(content)` — append user bubble to chat log
  - `renderAssistantDelta(content)` — append or extend assistant bubble
  - `finalizeTurn(turn)` — commit assistant content, run `marked.parse()`
  - `setStatus(status)` — update status indicator
- **Validation**:
  - `go test ./conduit/http/...` passes (Task 1 tests still cover serving)
  - `go build ./...` succeeds
  - Manual validation: run `examples/http-chat` with `WithUI()`, open browser, verify:
    - Page loads without errors
    - POST `/sessions` is called on load
    - EventSource connects to SSE endpoint
    - User can type a message and send it
    - Assistant response streams in
    - Markdown is rendered correctly
- **Details**: `index.html` uses a flexbox layout with a scrollable `#chat` div, a `<textarea>` + Send button in `#input-area`, and a `#status` span. `chat.js` uses vanilla JS (no frameworks). `marked.js` is loaded from CDN (`https://cdn.jsdelivr.net/npm/marked/marked.min.js`). The client handles SSE `text_delta` events by appending content to the current assistant message bubble. On `turn_complete`, it runs `marked.parse()` on the accumulated content. The status indicator shows "thinking..." while a turn is in progress and clears when complete.

### Task 3: Update HTTP Example to Demonstrate WithUI()
- **Goal**: Update `examples/http-chat/main.go` to pass `http.WithUI()` so the example serves the web UI by default.
- **Dependencies**: Task 1, Task 2.
- **Files Affected**:
  - `examples/http-chat/main.go` — add `http.WithUI()` to `NewHandler` call
- **New Files**: None.
- **Interfaces**: No new interfaces. Existing call: `handler := httpc.NewHandler(stepFactory, messageHandler)` becomes `handler := httpc.NewHandler(stepFactory, messageHandler, httpc.WithUI())`.
- **Validation**:
  - `go build ./examples/http-chat` succeeds
  - `go test ./...` passes
- **Details**: The example is updated to always enable the UI, making the out-of-the-box experience match the TUI example. The `README.md` or example comments may be updated to mention the web UI (optional, not required for this task).

### Task 4: Integration Validation
- **Goal**: Run full repository validation to ensure no regressions and confirm the feature works end-to-end.
- **Dependencies**: Task 1, Task 2, Task 3.
- **Files Affected**: None (read-only validation).
- **New Files**: None.
- **Interfaces**: None.
- **Validation**:
  - `go test -race ./...` passes
  - `go vet ./...` clean
  - `go build ./examples/http-chat` succeeds
  - No new Go module dependencies introduced (`go mod tidy` produces no changes)
  - Manual end-to-end test: run `ORE_API_KEY=... go run ./examples/http-chat`, open `http://localhost:8080/`, verify chat works
- **Details**: This is a validation-only task. Run the full test suite with race detection. Verify the example builds. Perform a manual smoke test with a real API key if available, or with the mock provider pattern from handler tests. Document any issues found and loop back to earlier tasks if needed.

## Dependency Graph

- Task 1 → Task 2 (client depends on serving infrastructure)
- Task 1 → Task 3 (example depends on `WithUI()` existing)
- Task 2 → Task 3 (example makes most sense with full client)
- Task 1, Task 2, Task 3 → Task 4 (integration validation)
- **Parallelizable**: None of the core tasks are parallelizable because each builds on the previous. Task 2 (HTML/JS) could theoretically be authored in parallel with the Go infrastructure in Task 1, but the JS needs to know the exact endpoint paths and response formats defined in Task 1. Sequential is safer and clearer.

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| `GET /` conflicts if caller mounts `ServeMux()` at root | Medium (breaks caller's own root handler) | Medium (when `WithUI()` is used with root mount) | UI is **opt-in** via `WithUI()` — caller consciously opts into root path consumption. Documented in handler godoc. |
| CDN dependency (`marked.js`) unavailable | Low (UI fails to render Markdown) | Low | Acceptable for a reference/demo example. The chat still works without Markdown rendering. Can vendor a minified copy in a future iteration. |
| No automated tests for JS functionality | Medium (regressions in client logic go undetected) | High | JS functionality is validated manually in Task 2 and Task 4. The Go serving layer is fully unit-tested in Task 1. |
| `//go:embed` requires files at build time | Low (build fails if static files missing) | Low | Static files are committed to the repo. CI and `go build` will catch missing files. |
| Session auto-creation on every page load creates dangling sessions | Low (memory leak under many refreshes) | Medium | Ephemeral single-session model matches TUI parity. Sessions are deleted on `DELETE` or server restart. Documented as demo behavior. |
| `NewHandler` signature change breaks existing callers | Low | Low | Variadic `opts ...Option` at end is backward-compatible. Existing two-argument calls compile unchanged. Verified by existing tests. |

## Validation Criteria

- [ ] `WithUI()` option exists and correctly toggles static file serving
- [ ] `GET /` returns `text/html` with the chat UI when `WithUI()` is enabled
- [ ] `GET /` returns 404 when `WithUI()` is not enabled
- [ ] `go test -race ./conduit/http/...` passes with new tests
- [ ] `go test -race ./...` passes across the entire repository
- [ ] `go vet ./...` is clean
- [ ] `go build ./examples/http-chat` succeeds
- [ ] No new Go module dependencies introduced
- [ ] `examples/http-chat` serves a working chat UI at `http://localhost:8080/` when `WithUI()` is used
- [ ] UI auto-creates a session on page load
- [ ] User can send a message and see streaming assistant responses
- [ ] Assistant messages render Markdown via `marked.js`
- [ ] Static file serving is covered by unit tests (content types, 404 when disabled)
