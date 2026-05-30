# Plan: http-chat-shareable-urls

## Objective

Move the ore HTTP-chat web UI entry point from `/` to `/chat`, using a `?thread=<uuid>` query parameter as the shareable conversation handle. A blank `/chat` defers thread creation until the user's first message, at which point the URL updates via `pushState`. Revisiting `/chat?thread=<uuid>` attaches to the live stream of that thread. The thread/session distinction is hidden from the user — the URL is the single conceptual handle. Conversation history loading on attach or reload is explicitly out of scope.

## Context

The ore HTTP conduit (`x/conduit/http`) serves an embedded web chat UI from `static/index.html` and `static/chat.js`. Currently:

- The UI is served at `GET /` and `GET /chat.js`.
- `chat.js` unconditionally calls `POST /sessions` on page load to create a new ephemeral session.
- The API supports `POST /sessions` with JSON body `{"thread_id": "<uuid>"}` to attach to an existing thread, but the UI never uses this.
- The `session.Manager` already conflates stream IDs with thread IDs: `Stream.id` is always set to `Thread.ID` in both `Manager.Create()` and `Manager.Attach()`. Multiple clients attaching to the same thread ID receive the same `*Stream` handle, enabling broadcast.
- The existing API routes (`/sessions/{id}/messages`, `/sessions/{id}/events`) will remain unchanged internally. The web UI transparently manages session lifecycle behind the thread-centric URL.

## Architectural Blueprint

The change is a **UI-layer and routing-layer refactor** — no backend data model changes.

1. **Routing**: Register UI at `/chat` and `/chat.js`. Redirect `/` → `/chat`.
2. **UI Boot Logic**: Parse `window.location.search` for `?thread=`. If present, attach via `POST /sessions {"thread_id": "..."}`. If absent, skip session creation and show a ready state.
3. **Deferred Creation**: On first `sendMessage`, if no session exists, create one via `POST /sessions`, extract the returned ID (which is the thread ID), call `history.pushState(null, "", "/chat?thread=" + id)`, then proceed to send the message.
4. **Broadcast preserved**: Reusing `Manager.Attach()` means multiple tabs on the same `?thread=<uuid>` share the same `*Stream` and its `FanOut`.

## Requirements

1. `GET /` must return a 307/302 redirect to `/chat`.
2. `GET /chat` serves the web chat UI (previously at `/`).
3. `GET /chat.js` continues to serve the bundled JavaScript.
4. Visiting `/chat?thread=<uuid>` attaches the UI to the existing thread; 404 if thread not found.
5. Visiting `/chat` (no query param) shows a ready UI with no active session.
6. The first user message on a blank `/chat` creates a new thread/session and mutates the browser URL to `/chat?thread=<uuid>` via `history.pushState`.
7. The URL can be copied and opened in another browser/tab to join the same live stream.
8. No conversation history is loaded on attach or reload (out of scope).
9. All existing API routes (`POST /sessions`, `POST /sessions/{id}/messages`, etc.) remain unchanged.

## Task Breakdown

### Task 1: Update HTTP handler routes and redirect
- **Goal**: Move web UI serving from `/` to `/chat`, add redirect from `/` to `/chat`.
- **Dependencies**: None.
- **Files Affected**: `x/conduit/http/handler.go`, `x/conduit/http/handler_test.go`.
- **New Files**: None.
- **Interfaces**: `ServeMux()` route registrations change; `serveUI` handler renamed or path-switch updated.
- **Validation**: `go test ./x/conduit/http/...` passes; `go vet ./...` clean.
- **Details**:
  - In `ServeMux()`: remove `GET /` UI registration; add `GET /chat` for `index.html` and `GET /chat.js` for `chat.js`.
  - Add `GET /` redirect handler (`http.Redirect(w, r, "/chat", http.StatusTemporaryRedirect)`).
  - Update `ServeMux()` doc comment to reflect new routes.
  - Update any handler tests that assert on `GET /` returning HTML; change to `GET /chat`.

### Task 2: Update web UI boot logic for thread attachment
- **Goal**: Parse `?thread=` on page load and attach to existing thread, or defer creation.
- **Dependencies**: Task 1.
- **Files Affected**: `x/conduit/http/static/chat.js`.
- **New Files**: None.
- **Interfaces**: JavaScript internal functions `createSession()`, `attachToThread(threadId)`.
- **Validation**: Manual browser test: open `/chat?thread=<valid-uuid>` → status shows "Ready" and events stream correctly. Open `/chat?thread=<invalid-uuid>` → shows error.
- **Details**:
  - Remove unconditional `createSession()` call at bottom of `chat.js`.
  - Add URL query parsing: `new URLSearchParams(window.location.search).get('thread')`.
  - If `thread` param present: call `fetch('/sessions', {method: 'POST', body: JSON.stringify({thread_id: thread})})`, set `sessionId` from response, set status "Ready".
  - If no `thread` param: set `sessionId = null`, set status "Ready — type a message to start".
  - Handle 404 on attach: show error status and keep `sessionId = null`.

### Task 3: Deferred thread creation on first message with URL pushState
- **Goal**: Create a new thread/session on first user message and update the browser URL.
- **Dependencies**: Task 2.
- **Files Affected**: `x/conduit/http/static/chat.js`.
- **New Files**: None.
- **Interfaces**: `sendMessage()` function behavior extended.
- **Validation**: Manual browser test: open `/chat` → type first message → URL updates to `/chat?thread=<uuid>` → assistant responds. Open same URL in second tab → both see live stream.
- **Details**:
  - In `sendMessage()`, if `!sessionId`:
    - Await `fetch('/sessions', {method: 'POST'})`.
    - Extract `id` from response (this is the thread ID since `Stream.id == Thread.id`).
    - Set `sessionId = id`.
    - Call `history.pushState(null, "", "/chat?thread=" + id)`.
  - Then proceed with the existing `fetch('/sessions/' + sessionId + '/messages', ...)` logic.
  - Ensure the UI handles the race where a user sends multiple messages rapidly before the first `createSession` resolves (disable send button until session is established).

### Task 4: Update example documentation and README references
- **Goal**: Update `examples/http-chat/main.go` doc comments and any README/web docs that reference the old `/` root page.
- **Dependencies**: Task 1.
- **Files Affected**: `examples/http-chat/main.go`.
- **New Files**: None.
- **Interfaces**: None.
- **Validation**: `go test ./examples/http-chat/...` passes (if any tests exist); `go build ./examples/http-chat` succeeds.
- **Details**:
  - Update `main.go` doc comment: "A built-in web chat UI is served at http://localhost:8080/chat when the application starts."
  - Review for any other hardcoded references to `localhost:8080/` instead of `localhost:8080/chat`.

## Dependency Graph

- Task 1 → Task 2 (UI can't attach to threads at `/chat` until route exists)
- Task 2 → Task 3 (deferred creation depends on boot logic)
- Task 1 || Task 4 (doc updates can happen in parallel with route changes, but referencing `/chat` before the route exists is confusing; sequential is cleaner)

Simplified critical path: Task 1 → Task 2 → Task 3. Task 4 can follow Task 1.

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Browser `history.pushState` with query param causes unexpected reload behavior on back/forward | Low | Medium | Use `history.replaceState` instead if navigation feels weird during manual QA. |
| Multiple rapid messages from blank `/chat` create multiple threads | Medium | Low | Disable send button until `sessionId` is resolved after first message. |
| `index.html` uses absolute `/chat.js` which still works at `/chat`, but relative assets would break | Low | Low | Verify in manual QA; no relative assets are currently used. |
| Tests assert on `GET /` returning 200 + HTML | Low | High | Update handler tests in Task 1; `go test` validation catches this. |
| Thread-not-found (`404`) on `/chat?thread=bad-uuid` has poor UX | Medium | Medium | Show a clear error in status bar; user can navigate to `/chat` to start fresh. |

## Validation Criteria

- [ ] `GET /` returns HTTP 307/302 redirect to `/chat`.
- [ ] `GET /chat` returns `index.html` with `text/html` content type.
- [ ] `GET /chat.js` returns `chat.js` with `application/javascript` content type.
- [ ] `go test ./x/conduit/http/...` passes.
- [ ] `go test -race ./...` passes.
- [ ] `go build ./examples/http-chat` succeeds.
- [ ] Manual browser test: `/chat` → type message → URL updates to `/chat?thread=<uuid>`.
- [ ] Manual browser test: copy updated URL to new tab → attaches to same thread, sees live assistant response.
- [ ] Manual browser test: `/chat?thread=<nonexistent>` → shows error, does not crash.
