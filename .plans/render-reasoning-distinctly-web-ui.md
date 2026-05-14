# Plan: Render Reasoning Distinctly in Web UI

## Objective

Rewrite the embedded HTTP conduit web chat client (`conduit/http/static/chat.js` + `index.html`) to consume NDJSON events directly from the `POST /sessions/{id}/messages` response body, eliminating the separate SSE connection. Render reasoning content in visually distinct collapsible blocks separate from normal text, using block-by-block progressive rendering with a `...` typing indicator for the currently-incomplete block.

## Context

The `ore` framework's HTTP conduit (`conduit/http/`) serves an embedded web chat UI via `//go:embed` in `conduit/http/static.go`. The current `chat.js` (discovered via `conduit/http/static/chat.js`) uses a persistent `EventSource` connection to the `events_url` returned by `POST /sessions` to receive all assistant response events. Both `text_delta` and `reasoning_delta` events are funneled through the same `renderAssistantDelta()` function, concatenating into a single `accumulatedText` string and rendered identically into one `.message.assistant` div. This means reasoning/thinking content is interleaved with the final answer with no visual distinction.

The backend (`conduit/http/handler.go`, `loop/loop.go`) correctly separates event types and accumulates adjacent same-kind deltas into discrete `artifact.Text` and `artifact.Reasoning` blocks. The NDJSON response from `POST /sessions/{id}/messages` already carries all events including deltas, `turn_complete`, and a final `complete` event with the full turn array. The backend is well-tested (`conduit/http/handler_test.go`, `conduit/http/types_test.go`) and must remain untouched per project conventions (`AGENTS.md`).

Key frontend files:
- `conduit/http/static/chat.js` — current vanilla-JS chat client using SSE
- `conduit/http/static/index.html` — current HTML with inline CSS
- `conduit/http/static.go` — Go embed directive (no changes needed)

## Architectural Blueprint

### Selected Architecture

**Frontend-only, zero backend changes.** The SSE endpoint (`GET /sessions/{id}/events`), `events_url` in session creation responses, and all Go handler code remain intact for other API consumers.

The `chat.js` client is rewritten with three structural changes:

1. **Drop SSE entirely.** No `EventSource`. Session creation ignores `events_url`. All events are consumed from the NDJSON response body of `POST /messages`.

2. **NDJSON streaming parse.** `sendMessage()` uses `fetch()` with `ReadableStream` + `TextDecoder` to parse the NDJSON response line-by-line as chunks arrive over the wire, rather than buffering the entire response with `await r.text()`.

3. **Block-by-block progressive rendering.** A single `.typing-indicator` (`...`) is shown inside the assistant message div for the currently-incomplete block. When the event kind switches (e.g., `reasoning_delta` → `text_delta`), the previous block is considered complete: the `...` is replaced by the rendered block content, and a fresh `...` appears for the next block. `turn_complete` finalizes the last pending block and removes the trailing `...`.

**Reasoning blocks** render as native HTML `<details>` elements (collapsed by default) with a `<summary>Thinking...</summary>` label, plain text content, and distinct CSS styling (smaller font, muted color). **Text blocks** render via `marked.parse()` on the full accumulated block content. Multiple reasoning/text blocks per assistant message are preserved as separate DOM children, faithfully reflecting provider interleaving.

### Evaluated Alternatives

| Approach | Why Not Selected |
|---|---|
| Keep SSE, only add CSS styling | Does not solve the root problem: reasoning and text are still interleaved into a single string because both deltas append to `accumulatedText` |
| Full character-by-character streaming with incremental markdown | Markdown is not stream-parseable; mid-sentence re-parsing with `marked` causes flicker and mis-rendered partial constructs (e.g., unclosed `**` or `` ` ``) |
| Wait for `complete` event, render everything at once | Simpler but provides no progressive feedback during long reasoning turns; user stares at `...` for the entire duration |
| Block-by-block from `complete` event only | Loses the progressive feel; the `complete` event arrives at the very end of the NDJSON stream |

The selected block-by-block approach (using kind switches within the delta stream) gives progressive feedback without the complexity of incremental markdown parsing.

## Requirements

1. [explicit] Remove SSE consumption from `chat.js`; consume NDJSON from `POST /messages` response body via streaming parse.
2. [explicit] Render reasoning content visually distinct from normal text in the assistant message bubble.
3. [explicit] Reasoning blocks are native `<details>` elements, collapsed by default, with a `<summary>Thinking...</summary>` label.
4. [explicit] Use block-by-block progressive rendering: a `...` typing indicator represents the currently-incomplete block.
5. [explicit] An event kind switch (e.g., `reasoning_delta` → `text_delta`) triggers completion of the previous block, replacing its `...` with the rendered block.
6. [explicit] `turn_complete` triggers completion of the final pending block and removes the trailing `...`.
7. [explicit] `tool_call_delta` events are silently consumed without affecting block tracking or rendering.
8. [inferred] Backend must remain unchanged; `go test ./...` must continue to pass.
9. [inferred] The `createSession` function name must remain in `chat.js` for the existing Go test assertion (`TestHandler_WithUI_StaticFiles`).
10. [inferred] `marked.parse()` is applied only to text blocks, not reasoning blocks.

## Task Breakdown

### Task 1: Add CSS for Reasoning Blocks and Typing Indicator
- **Goal**: Update `index.html` with styles for collapsible reasoning blocks and the `...` typing indicator.
- **Dependencies**: None.
- **Files Affected**: `conduit/http/static/index.html`
- **New Files**: None.
- **Interfaces**: None.
- **Validation**: `go test ./conduit/http/...` passes (static file tests verify HTML serving). Open `index.html` in a browser to visually confirm new selectors do not break existing layout.
- **Details**:
  - Add `.typing-indicator` styles (e.g., light grey, italic, `...` content). Position it as the last child of `.message.assistant`.
  - Add `.message.assistant details` styles: smaller font size (e.g., `0.875rem`), muted color (e.g., `#666`), border or background to visually separate from text blocks, margin spacing.
  - Add `.message.assistant details summary` styles: `cursor: pointer`, subtle font weight, remove default browser marker if desired via `list-style: none`.
  - Add `.message.assistant details[open] summary` margin-bottom for breathing room when expanded.
  - Ensure all new selectors are scoped within `.message.assistant` to avoid leaking into user messages.

### Task 2: Rewrite chat.js for NDJSON Block Rendering
- **Goal**: Replace SSE-based event handling with NDJSON streaming consumption and implement block-by-block progressive rendering with distinct reasoning blocks.
- **Dependencies**: Task 1.
- **Files Affected**: `conduit/http/static/chat.js`
- **New Files**: None.
- **Interfaces**:
  - **Remove**: `eventsUrl` (global variable), `eventSource` (global variable), `connectSSE(url)` (function), `currentAssistantDiv` (global variable), `accumulatedText` (global variable), `renderAssistantDelta(content)` (function).
  - **Modify**: `createSession()` — extract `sessionId` only; do not assign `eventsUrl` or call `connectSSE()`.
  - **Modify**: `sendMessage(content)` — switch from fire-and-forget POST to async streaming NDJSON consumption. Create the assistant message div with a `.typing-indicator` child before initiating the fetch. Use `fetch()` + `response.body.getReader()` + `TextDecoder` to read the NDJSON stream incrementally.
  - **Add**: `readNDJSONStream(reader, decoder)` — async generator or loop that reads chunks, splits on `\n`, buffers partial lines, and yields parsed JSON event objects.
  - **Add**: `handleEvent(event)` — dispatches by `event.kind`: routes `text_delta`/`reasoning_delta` to block accumulator, `tool_call_delta` to no-op, `turn_complete` to finalizer, `error` to error handler.
  - **Add**: `completeCurrentBlock()` — removes the `.typing-indicator` from the active assistant message div, calls `renderBlock(kind, content)` to create the DOM element for the completed block, appends it to the assistant message div, then appends a fresh `.typing-indicator` for the next block.
  - **Add**: `renderBlock(kind, content)` — for `reasoning_delta`/`reasoning`: returns a `<details>` element with `<summary>Thinking...</summary>` and a plain-text `<div>` child containing `content`. For `text_delta`/`text`: returns a `<div>` with `innerHTML` set to `marked.parse(content)` (wrapped in try/catch, falling back to plain text on error).
  - **Add**: `finalizeTurn()` — if a block is pending, calls `completeCurrentBlock()`; removes the final `.typing-indicator`; resets block tracking state (`currentBlockKind`, `currentBlockContent`); sets `isTurnInProgress = false`; updates status and send button.
- **Validation**: `go test ./...` passes. `go test ./conduit/http/...` specifically passes. `GET /chat.js` served via handler contains `createSession` string. Manual browser test: load page, create session, send message, observe `...` during processing, see final text rendered.
- **Details**:
  1. Remove all SSE-related globals and functions (`eventsUrl`, `eventSource`, `connectSSE`). Remove the `addEventListener` calls for SSE event types.
  2. In `createSession()`, parse the JSON response, store only `sessionId = data.id`. Do not read or use `data.events_url`.
  3. Rewrite `sendMessage(content)` as an `async` function:
     - Validate `sessionId` and `!isTurnInProgress` (unchanged guard).
     - Set `isTurnInProgress = true`, update status to `thinking...`, disable send button.
     - Call `renderUserMessage(content)` to append the user bubble.
     - Create a `.message.assistant` div and append a `.typing-indicator` child (`...`). Append to `#chat`. Scroll to bottom.
     - `await fetch('/sessions/' + sessionId + '/messages', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({content: content})})`.
     - If `!response.ok`, throw or show error, call `finalizeTurn()` (or equivalent cleanup), return.
     - Obtain `const reader = response.body.getReader()` and `const decoder = new TextDecoder()`.
     - Maintain a `buffer` string across chunk reads. Loop `while (true)`: `const {done, value} = await reader.read()`; if `done`, break; decode chunk with `decoder.decode(value, {stream: true})`; split on `\n`; keep the last (potentially partial) line in `buffer`; iterate complete lines, trim, skip empty lines, `JSON.parse(line)`, call `handleEvent(parsed)`.
     - After the stream loop, call `finalizeTurn()` if not already called by a `turn_complete` event.
     - Wrap the entire fetch/streaming loop in try/catch: on error, show error in status, `finalizeTurn()`.
  4. Implement `handleEvent(event)`:
     - `tool_call_delta`: return immediately (no state change).
     - `text_delta` / `reasoning_delta`:
       - If `currentBlockKind` is `null`, initialize a new block with the event's kind and content. The `.typing-indicator` already exists in the DOM.
       - If `currentBlockKind` equals the event kind, append `event.content` to `currentBlockContent`.
       - If `currentBlockKind` differs from the event kind, call `completeCurrentBlock()` to render the previous block, then start a new `currentBlock` with the event's kind and content.
     - `turn_complete`: call `completeCurrentBlock()` to render the final pending block, then call `finalizeTurn()`.
     - `error`: display the error message in the status bar, call `finalizeTurn()`.
  5. Implement `completeCurrentBlock()`:
     - Assert `currentAssistantMessageDiv` and `currentBlockKind` are set.
     - Remove the `.typing-indicator` element from the assistant message div.
     - Call `renderBlock(currentBlockKind, currentBlockContent)` to get the rendered DOM element.
     - Append the rendered block element to the assistant message div.
     - Append a new `.typing-indicator` element to the assistant message div.
     - Scroll to bottom.
     - Reset `currentBlockKind = null` and `currentBlockContent = ''` (the next event will set them fresh).
  6. Implement `renderBlock(kind, content)`:
     - For `reasoning` kind: create `<details>` element, set `innerHTML = '<summary>Thinking...</summary><div class="reasoning-content"></div>'`, set text content of the inner div to `content` (plain text, no markdown). Return the details element.
     - For `text` kind: create `<div>` element. Try `div.innerHTML = marked.parse(content)`. Catch errors and fallback to `div.textContent = content`. Return the div.
  7. Implement `finalizeTurn()`:
     - If `currentBlockKind` is not `null`, call `completeCurrentBlock()`.
     - Remove the `.typing-indicator` from the assistant message div (it may have already been removed by `completeCurrentBlock`, idempotent check).
     - Set `currentBlockKind = null`, `currentBlockContent = ''`.
     - Set `isTurnInProgress = false`.
     - Update send button state.
     - Set status to `Ready` (or `Error: ...` if an error occurred).
  8. Keep `renderUserMessage(content)`, `updateSendButton()`, `scrollToBottom()`, `handleSend()`, `resetTextareaHeight()`, and the existing DOM event listeners (`send-btn` click, `message-input` keydown/textarea auto-resize) unchanged in behavior.
  9. Ensure `createSession` function name is preserved exactly as a top-level function declaration for the Go test assertion.

## Dependency Graph

- Task 1 → Task 2

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| `chat.js` rewrite introduces subtle JS syntax or logic errors | Medium | Medium | `go test ./conduit/http/...` verifies file serving; manual browser test for core send/receive flow; lint with `npx eslint` if available |
| NDJSON streaming parse splits multi-byte UTF-8 characters across chunk boundaries | Low | Low | `TextDecoder` with `{stream: true}` handles partial byte sequences correctly |
| `marked.parse()` throws on malformed markdown in a text block | Low | Low | Wrap in `try/catch`, fallback to `textContent` — same resilience pattern already used in `finalizeTurn()` |
| Existing Go test `TestHandler_WithUI_StaticFiles` asserts `createSession` string in chat.js | Medium | Low | Explicitly preserve `createSession` as a top-level function name in the rewrite |
| Long reasoning turns with no kind switch leave user staring at `...` for extended time | Low | Low | This is the accepted UX tradeoff of Approach B per ideation consensus |

## Validation Criteria

- [ ] `go test ./...` passes with no failures or race conditions (`go test -race ./...`).
- [ ] `go test ./conduit/http/...` passes, including `TestHandler_WithUI_StaticFiles` which asserts `createSession` exists in served `chat.js`.
- [ ] `GET /` returns `index.html` containing new CSS rules for `.typing-indicator`, `details`, and `summary`.
- [ ] `GET /chat.js` returns JavaScript with no references to `EventSource`, `eventsUrl`, `connectSSE`, or `addEventListener('text_delta'` in SSE style.
- [ ] Manual browser test: load `http://localhost:8080/`, page auto-creates session, send a message. Assistant message bubble appears with `...` during processing, then text renders.
- [ ] Manual browser test with reasoning-capable provider (or mock): assistant message contains one or more collapsed `<details>` blocks labeled "Thinking..." above the final text answer.
- [ ] Manual browser test: clicking a "Thinking..." summary expands to reveal plain-text reasoning content.
- [ ] Manual browser test: sending a second message after the first completes works correctly (new user bubble, new assistant bubble, state resets cleanly).
