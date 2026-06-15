# Plan: Paginate and Sort the Threads Listing

## Objective

Replace the unbounded, unsorted `GET /threads` JSON endpoint and the embedded HTML landing page (`GET /`) with versions that return threads in `updated_at` descending order, paginated using a cursor-based protocol. The JSON endpoint returns an object envelope `{"threads": [...], "next_cursor": "..."}`; the HTML page renders only the first page server-side and progressively loads subsequent pages via a small inline script. The change is a library-level refactor in `x/conduit/http/` with no backwards compatibility.

## Context

The HTTP conduit currently exposes:

- `x/conduit/http/handler.go:511` — `listThreads` returns a bare JSON array of every thread in the store. Order is whatever the underlying `session.Store.List()` produces (map iteration for `MemoryStore`, directory iteration for `JSONStore`). Each entry is an inline struct `{id, created_at, updated_at}` with no preview.
- `x/conduit/http/handler.go:125` — `serveLanding` walks the same store, computes a 120-char preview for each thread by walking its full turn history (`previewSnippet`, line 153), and renders all of them via `x/conduit/http/static/landing.html`. The "slow first load" the user described is the consequence of this code: on a `JSONStore` cold cache, `List()` reads every thread file in full, and `serveLanding` then walks every thread's full turn history a second time to produce previews. Pagination to a small page size reduces the cold-cache first-page cost from O(N) reads to O(limit) reads.
- `x/conduit/http/openapi.yaml:135` — documents the JSON endpoint as a bare array with no query parameters.
- `x/conduit/http/handler_test.go:786-823` — three table-driven tests assert the bare-array shape.
- `session/thread.go:17` — `Thread` carries `UpdatedAt time.Time`; this is the natural sort key.
- `session/memory.go:80` and `session/json.go:106` — `Store.List()` returns an unsorted slice. The handler is the right place to sort; sorting is a presentation concern, not a storage one.

Project conventions honoured:

- Per `AGENTS.md`, "Do not preserve backwards compatibility… prefer aggressive refactoring." The response shape changes wholesale.
- Per `AGENTS.md`, this is a **library change in `x/conduit/http/`**. `examples/http-chat/main.go` and any other consumer picks up the new behaviour automatically.
- Per `AGENTS.md`, OpenTelemetry tracing is woven through the framework as build-time opt-in; this work touches no tracing paths.

Other consumers of `/threads`: a repo-wide grep for the path returns only `handler.go`, `openapi.yaml`, and `handler_test.go`. No JS, no HTML, no other Go code consumes the endpoint. The blast radius is small.

## Architectural Blueprint

**Sort and paginate in the handler, not the store.** The store returns a flat slice; the handler sorts, slices, and encodes. This keeps `MemoryStore` and `JSONStore` ignorant of pagination concerns. If the `JSONStore` later grows very large, a follow-up can teach the store to do per-page disk reads — but the handler-level helper can be reused unchanged.

**Cursor model.** Opaque, base64-encoded JSON. Wire format: `base64.URLEncoding` (no padding) of `json.Marshal({"v":1, "u":<rfc3339Nano>, "i":<id>})`. The `v` field lets the format evolve. Clients MUST treat the cursor as opaque.

**Sort key.** `updated_at` descending, with `id` ascending as a deterministic tiebreaker. The tiebreaker is required because two threads can share a timestamp; without it, pagination across identical timestamps is unstable.

**Page size.** Default 20, max 100, min 1. Out-of-range values are silently clamped. `?limit=N` overrides the default; absent means default.

**Response envelope.** `{"threads": [...], "next_cursor": "<opaque>"}`. `next_cursor` is omitted (empty string) when the returned page is the last one. The `threads` array is always present, even if empty.

**HTML page strategy.** Server-render only the first page. Include a "Load more" button (`<button id="load-more" data-cursor="<opaque>">Load more</button>`) below the thread list when a `next_cursor` is present. A small inline `<script>` listens for clicks, fetches `/threads?cursor=<data-cursor>&limit=20`, appends the new thread cards to the existing list using the same markup as the server-rendered cards, updates the button's `data-cursor` from the response's `next_cursor`, and removes the button if the response has no `next_cursor`. A `<noscript>` block hides the button for users without JS — they see only the first page.

**Why inline script and not a new `.js` file?** The current `chat.js` is loaded only on the `/chat` page; the landing page has no JS today. Adding a `landing.js` would require a new `static/landing.js` file, a new route registration, and a new template path. For ~30 lines of script, the inline approach is proportional. If the script grows past ~100 lines, extract to a separate file in a follow-up.

## Requirements

1. Threads MUST be returned in `updated_at` descending order, with `id` ascending as a tiebreaker.
2. The JSON endpoint MUST accept `?limit=N` and `?cursor=<opaque>` query parameters.
3. `?limit=N` is silently clamped to `[1, 100]`; default is 20 when absent.
4. An invalid `?cursor=` value MUST return `400 Bad Request` with a clear error message.
5. The JSON response MUST be an object envelope `{"threads": [...], "next_cursor": "<opaque>"}`; `next_cursor` is omitted on the last page.
6. The HTML landing page MUST also return only the first page server-side and progressively load more via JS, using the same cursor protocol.
7. The OpenAPI spec MUST be updated to reflect the new contract.
8. The change MUST be a library change in `x/conduit/http/`; `examples/http-chat/main.go` and other consumers pick it up automatically.
9. `go test -race ./...` MUST pass in both the root module and the `x/conduit/http/` sub-module. [inferred]

## Task Breakdown

### Task 1: Add cursor codec and threads response DTOs

- **Goal**: Introduce the data types and constants needed for the new contract, with no handler changes.
- **Dependencies**: None.
- **Files Affected**:
  - `x/conduit/http/handler.go` (read-only reference for the existing `listThreads` summary struct and `serveLanding` data struct)
  - `x/conduit/http/openapi.yaml` (read-only reference for the `ThreadSummary` schema)
- **New Files**:
  - `x/conduit/http/threads.go` — defines `threadSummaryJSON`, `threadsListResponseJSON`, internal `threadCursor` (with `encode`/`decode` methods), and the package-private constants `defaultThreadPageSize = 20`, `maxThreadPageSize = 100`. Also defines the sentinel `errInvalidCursor` returned by the decoder and used by the handler to translate to `400`.
  - `x/conduit/http/threads_test.go` — round-trip tests for the cursor codec and a sanity test that the response DTO marshals to the expected JSON shape.
- **Interfaces**:
  - `threadSummaryJSON{ID string `json:"id"`; CreatedAt string `json:"created_at"`; UpdatedAt string `json:"updated_at"`}` — timestamps as RFC3339Nano strings, matching the existing `ThreadSummary` shape in the OpenAPI spec.
  - `threadsListResponseJSON{Threads []threadSummaryJSON `json:"threads"`; NextCursor string `json:"next_cursor,omitempty"`}` — `next_cursor` is omitted via `omitempty` when empty.
  - `threadCursor{Version int; UpdatedAt time.Time; ID string}` plus `encode() (string, error)` and `decode(string) (threadCursor, error)`.
- **Validation**:
  - `cd x/conduit/http && go build ./...` succeeds.
  - `cd x/conduit/http && go test ./...` passes (no handler behaviour change yet).
  - `cd x/conduit/http && go test -race ./...` passes for the new file.
- **Details**: Cursor encoding uses `base64.URLEncoding` (no padding) on `json.Marshal(threadCursor)`. The decoder rejects unknown `Version` values with the sentinel `errInvalidCursor`. Tests must cover: round-trip, malformed base64, malformed JSON, unknown version.

### Task 2: Add a pure sort-and-paginate function with unit tests

- **Goal**: Implement the algorithm that sorts a slice of threads and returns one page, isolated from the HTTP layer.
- **Dependencies**: Task 1.
- **Files Affected**:
  - `x/conduit/http/handler.go` (read-only reference for the existing `listThreads` body and `previewSnippet` signature)
- **New Files**: None beyond extending `x/conduit/http/threads.go` and `x/conduit/http/threads_test.go`.
- **Interfaces**:
  - `paginateAndSortThreads(threads []*session.Thread, limit int, cursor string) (page []*session.Thread, nextCursor string, err error)` — input is an unsorted slice of `*session.Thread`, a clamped `limit`, a raw cursor string (may be empty for the first page). Output: the page slice (a sub-slice of the input), the opaque `next_cursor` string (empty if the page is the last), or an error (`errInvalidCursor` for a bad cursor).
  - Behaviour: sort by `updated_at` desc, then `id` asc; locate the cursor position; return at most `limit` items; if more remain after the returned page, encode the cursor pointing to the first item *after* the returned page; otherwise return an empty next cursor.
- **Validation**:
  - `cd x/conduit/http && go test -run TestPaginate ./...` passes.
  - `cd x/conduit/http && go test -race ./...` passes.
- **Details**: A sentinel error (`errInvalidCursor`) lets the handler translate cleanly to `400`. Tests must cover: empty input; single page (next cursor empty); multi-page (cursor progression lands on consecutive items); tiebreak (two threads with identical `updated_at` paginate deterministically); invalid cursor returns the sentinel error; `limit` of 1 still works; `limit` greater than total returns everything with empty next cursor.

### Task 3: Refactor `listThreads` and `serveLanding` to use the new helper

- **Goal**: Wire the helper into the HTTP handlers and update the existing tests.
- **Dependencies**: Task 1, Task 2.
- **Files Affected**:
  - `x/conduit/http/handler.go` — replace the body of `listThreads` (line 511) and `serveLanding` (line 125). The inline `summary` struct in `listThreads` is removed in favour of `threadSummaryJSON`. `serveLanding` no longer iterates all threads; it iterates the first page only and passes the `next_cursor` (if any) to the template via a new `NextCursor` field on the data struct.
  - `x/conduit/http/handler_test.go` — the three `TestHandler_ListThreads_*` tests (lines 786, 805, 823) are updated to assert the envelope shape. The `TestHandler_ServeMux_*` table (line 212) is unchanged (status code stays 200).
- **New Files**: None.
- **Interfaces**:
  - `listThreads` now parses `r.URL.Query().Get("limit")` and `r.URL.Query().Get("cursor")`, clamps the limit, returns `400` on invalid cursor, calls `paginateAndSortThreads`, and writes the envelope via `json.NewEncoder(w).Encode(...)`.
  - `serveLanding` calls the same helper with the default limit; the template data struct gains a `NextCursor string` field.
- **Validation**:
  - `cd x/conduit/http && go test -race ./...` passes.
  - New tests added: `TestHandler_ListThreads_DefaultLimit`, `TestHandler_ListThreads_SortedByUpdatedAtDesc`, `TestHandler_ListThreads_LimitRespected`, `TestHandler_ListThreads_CursorProgression`, `TestHandler_ListThreads_LastPageHasEmptyCursor`, `TestHandler_ListThreads_InvalidCursor400`, `TestHandler_ListThreads_LimitClamped`.
- **Details**: The order of operations in `listThreads` is critical: clamp the limit first, then attempt to decode the cursor. An invalid cursor must short-circuit to `400` even when the limit is valid. The 500 path for a store error is preserved. The `serveLanding` template change is the only data-struct change in the template.

### Task 4: Add "Load more" JS to the landing page

- **Goal**: Make the HTML landing page's "Load more" button functional.
- **Dependencies**: Task 3.
- **Files Affected**:
  - `x/conduit/http/static/landing.html` — the template adds a `<button id="load-more" data-cursor="{{.NextCursor}}">Load more</button>` below the thread list when `.NextCursor` is non-empty, plus an inline `<script>` (in a `{{if .NextCursor}}...{{end}}` block) implementing the click handler. A `<noscript><style>#load-more { display: none; }</style></noscript>` block hides the button when JS is disabled.
  - `x/conduit/http/handler_test.go` — add `TestHandler_LandingPage_IncludesLoadMoreWhenMorePagesExist` and `TestHandler_LandingPage_OmitsLoadMoreOnLastPage`.
- **New Files**: None.
- **Interfaces**: None on the Go side; the script consumes `/threads?cursor=...&limit=20` and the same JSON envelope.
- **Validation**:
  - `cd x/conduit/http && go test -race ./...` passes.
  - The smoke tests assert the rendered HTML contains `id="load-more"` and `data-cursor="<base64>"` when more pages exist, and that the button is absent on the last page.
- **Details**: The inline script is a `<script>...</script>` block, ~30 lines, that:
  1. On `DOMContentLoaded`, finds the button.
  2. On click, fetches `/threads?cursor=<button.dataset.cursor>&limit=20`.
  3. Parses the JSON envelope, appends each thread's card markup to the `.thread-list` element.
  4. If the response has a `next_cursor`, updates `button.dataset.cursor`; otherwise removes the button.
  The card markup is generated server-side and replicated in JS — duplication is acceptable given the script's small size. A `<noscript>` block sets `display: none` on the button when JS is disabled, so the user simply sees the first page. Card markup must match `landing.html`'s current `.thread-card` structure exactly (thread-id, thread-preview, thread-meta).

### Task 5: Update the OpenAPI spec

- **Goal**: Document the new contract.
- **Dependencies**: Task 3.
- **Files Affected**:
  - `x/conduit/http/openapi.yaml` — the `/threads` GET entry (line 135) gains `?limit` (integer, default 20, min 1, max 100) and `?cursor` (string) query parameters. The response schema switches from a bare array to a reference to a new `ThreadsListResponse` schema. A `400` response is added. A new schema entry `ThreadsListResponse` is added under `components.schemas` (alongside `ThreadSummary` at line 230), with `threads` (array of `ThreadSummary`) and `next_cursor` (string, optional). If an `ErrorResponse` schema does not already exist in the file, add a minimal one and reference it from the 400 response.
- **New Files**: None.
- **Validation**:
  - `cd x/conduit/http && go test -race ./...` passes. The `openapi_test.go` suite calls `spec.Validate(loader.Context)` and will reject the spec if the new schema is malformed.
  - Manual: the new schema is consistent with the Go response DTO.
- **Details**: The 400 response for an invalid cursor should be added to the spec. Confirm via a quick read of the existing spec at the start of this task whether `ErrorResponse` is defined; if not, add a minimal `{message: string}` schema.

### Task 6: End-to-end validation

- **Goal**: Confirm the change is clean across the whole repo and the example app still builds.
- **Dependencies**: All previous tasks.
- **Files Affected**: None (read-only validation).
- **New Files**: None.
- **Validation**:
  - `go test -race ./...` from the repo root passes.
  - `cd x/conduit/http && go test -race ./...` passes.
  - `cd examples/http-chat && go build ./...` succeeds.
  - Manual smoke (if a `STORE_DIR` is set up locally): start the example, hit `GET /threads?limit=2` and confirm the envelope shape; hit the second page with the returned `next_cursor`; confirm the HTML landing page renders the first page with a "Load more" button.
- **Details**: This task exists to catch cross-module breakage — e.g., a stray consumer of the old shape elsewhere in the repo. Earlier grep confirmed no other consumer; this task is a final safety net.

## Dependency Graph

- Task 1 → Task 2 (Task 2 uses the cursor types from Task 1)
- Task 1, Task 2 → Task 3 (Task 3 wires the helper into the handler)
- Task 3 → Task 4 (Task 4 reads `NextCursor` from the template data struct)
- Task 3 → Task 5 (Task 5 documents the contract from Task 3)
- Tasks 1-5 → Task 6 (Task 6 is the end-to-end sweep)

Tasks 4 and 5 are parallelizable after Task 3 completes.

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Existing tests break (handler_test.go asserts the old bare-array shape) | Medium | High | Task 3 explicitly updates the three `TestHandler_ListThreads_*` tests as part of the same change. The new tests are listed in the task's Validation section. |
| OpenAPI spec validator rejects the new schema | Medium | Low | The new schema is structurally simple. `openapi_test.go` calls `spec.Validate(loader.Context)` and Task 5 runs the full test suite to catch this. |
| `JSONStore` cold-cache first-page load is still slow for very large directories | Low | Low | The user explicitly accepted that pagination "mostly fixes" the slow load; the first page of a cold cache still reads one page of files. If this becomes a problem later, teach `JSONStore` to keep a sidecar metadata index. |
| `previewSnippet` still walks the full turn history of each visible thread | Low | Medium | For threads with thousands of turns, the preview walk remains a per-page cost. This is the same code path the user already pays, now bounded by `limit`. Out of scope for this work; can be addressed by caching the preview on `Thread` or by storing it in a sidecar. |
| Cursor instability under concurrent writes (a thread added between page requests can shift positions) | Low | Medium | This is an inherent property of cursor pagination on a mutable list. The `(updated_at, id)` composite cursor minimises it. Documented in the API contract. |
| Inline JS in `landing.html` could grow over time | Low | Low | ~30 lines today. If it grows past ~100 lines, extract to `static/landing.js` and serve via the existing `static.go` and `serveUI` patterns. |
| Existing OpenAPI spec is missing an `ErrorResponse` schema; adding the 400 path requires introducing one | Low | Low | Read the existing `openapi.yaml` at the start of Task 5; add the schema if absent. |

## Validation Criteria

- [ ] `go test -race ./...` passes in the root module.
- [ ] `go test -race ./...` passes in `x/conduit/http/`.
- [ ] `go build ./...` succeeds in `examples/http-chat/`.
- [ ] `GET /threads` (no params) returns `{"threads": [...20 items...], "next_cursor": "<opaque>"}` when the store holds more than 20 threads; `next_cursor` is omitted on the last page.
- [ ] Threads are ordered by `updated_at` descending, with `id` ascending as a tiebreaker.
- [ ] `GET /threads?limit=N` returns at most `N` items; `?limit=0`, `?limit=-5`, `?limit=99999` are all clamped to `[1, 100]`.
- [ ] `GET /threads?cursor=<bad>` returns `400 Bad Request`.
- [ ] The HTML landing page at `GET /` renders the first page only and includes a "Load more" button with `data-cursor` when more pages exist; the button is absent on the last page.
- [ ] The OpenAPI spec at `x/conduit/http/openapi.yaml` documents the new contract and `openapi_test.go` still passes.
- [ ] No other consumer of `GET /threads` in the repo is broken (verified by `go test ./...` across modules).
