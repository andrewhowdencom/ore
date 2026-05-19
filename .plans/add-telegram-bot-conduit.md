# Plan: Add Telegram Bot API Conduit

## Objective

Implement a Telegram Bot API I/O conduit under `x/conduit/telegram/` so users can interact with ore agents via Telegram text messages. The conduit is a dumb pipe: it long-polls the Telegram `getUpdates` endpoint, maps incoming text messages into `session.UserMessageEvent` per unique `chat_id`, and replies with assistant text artifacts by subscribing to `"turn_complete"` events. It follows the standardized ore conduit contract (functional-options constructor, exported `Descriptor`, blocking `Start(ctx)`, forge compatibility).

## Context

The ore framework defines a minimal `Conduit` interface (`Start(ctx context.Context) error`) and capability vocabulary in `x/conduit/`. Concrete conduits live under `x/conduit/<name>/` as Go modules with `replace` directives to the core module.

Existing reference implementations:
- `x/conduit/http/` — request-driven, per-request `stream.Subscribe()`, NDJSON/SSE streaming
- `x/conduit/tui/` — single-session, `stream.Subscribe("turn_complete")`, Bubble Tea rendering

Both use `session.Manager` to create/attach `session.Stream` handles and call `stream.Process()` for input with `loop.EventContext{Provenance: "..."}` for echo suppression.

Key session primitives:
- `session.Manager.Create()` / `Attach(threadID)` — create or resume a `session.Stream`
- `stream.Process(ctx, event)` — accepts `UserMessageEvent` or `InterruptEvent`
- `stream.Subscribe(kinds...)` — per-stream filtered channel from the `loop.Step` FanOut
- `mgr.RegisterSink(kinds, fn)` — manager-level callback receiving events from ALL active and future streams, with the stream ID as a parameter
- `loop.TurnCompleteEvent{Turn state.Turn, Ctx loop.EventContext}` — emitted after a turn completes
- `loop.EventContext{Provenance string}` — carries source metadata through the pipeline

The `conduit` skill (`.agents/skills/conduit/SKILL.md`) mandates: own `go.mod`, exported `Descriptor`, `New(mgr, opts...)`, blocking `Start()`, provenance-based echo suppression, table-driven tests, and a `README.md`.

## Architectural Blueprint

### Selected Architecture

The Telegram conduit is a **polling-driven, multi-session conduit** where each Telegram `chat_id` maps to a distinct ore `session.Thread`. Unlike HTTP (request-per-session) or TUI (single-session), Telegram requires handling an unbounded set of chats concurrently. The cleanest pattern is:

1. **Ingress (polling loop)**: A single goroutine long-polls `getUpdates` with `offset` tracking. Each text message from a non-bot user triggers `mgr.Attach(strconv.FormatInt(chatID, 10))` to get or create a stream, then `stream.Process(ctx, UserMessageEvent{...})` with `Provenance: "telegram"`.

2. **Egress (manager sink)**: Use `mgr.RegisterSink(["turn_complete"], fn)` to receive turn-complete events from ALL streams (existing and future). In the sink callback, check `event.Context().Provenance == "telegram"` to skip events from other conduits in multi-conduit setups. Extract text artifacts from `TurnCompleteEvent.Turn` and call `sendMessage` back to the originating `chat_id` (which equals the `streamID`).

3. **Echo suppression**: Two layers:
   - **Input side**: Skip messages where `message.From.ID == botUserID` (the bot's own messages in groups).
   - **Output side**: The sink callback filters by `Provenance == "telegram"` so it doesn't reply to HTTP or TUI events.

4. **Fatal vs. non-fatal errors**:
   - Fatal (returned from `Start()`): missing bot token at startup, `getMe` failure (invalid token), unrecoverable network loss after retries.
   - Non-fatal (logged, swallowed): single `sendMessage` delivery failure, transient `getUpdates` timeout.

### Evaluated Alternatives

- **Per-stream subscription goroutine**: For each chat, spawn a goroutine that calls `stream.Subscribe("turn_complete")` and replies. This leaks goroutines if chats are unbounded and requires managing subscription lifetimes. Rejected in favor of `RegisterSink` which is centralized and handles all streams.

- **Webhook mode**: The issue explicitly defers webhook mode. Long-polling is simpler (no reverse proxy/HTTPS required) and sufficient for the MVP.

- **Telegram Bot SDK**: The project convention (AGENTS.md) prohibits external SDKs for adapters. Rejected in favor of `net/http` + `encoding/json` from the standard library.

## Requirements

1. Create `x/conduit/telegram/` as a separate Go module with `replace github.com/andrewhowdencom/ore => ../../..`.
2. Export `Descriptor` with `CapEventSource`, `CapAcceptText`, `CapRenderTurn`.
3. Constructor: `New(mgr *session.Manager, opts ...Option) (conduit.Conduit, error)`. Validates `mgr != nil`. Accepts zero options (forge-compatible).
4. Functional options: `WithBotToken(token string)`, `WithHTTPClient(client *http.Client)`, `WithGetUpdatesTimeout(seconds int)`.
5. `Start(ctx)`:
   - Validates bot token is non-empty (error if missing).
   - Calls `getMe` to validate token and cache `botUserID`.
   - Registers a `"turn_complete"` sink on `mgr`.
   - Starts long-polling `getUpdates` loop.
   - Blocks until `ctx.Done()`.
   - Returns nil on clean shutdown; non-nil only on fatal errors.
6. Maps incoming text to `session.UserMessageEvent{Content: text, Ctx: {Provenance: "telegram"}}`.
7. Skips bot's own messages and non-text updates.
8. Replies to `"turn_complete"` events with `sendMessage`.
9. Add table-driven tests using `httptest.Server` to mock the Telegram API.
10. Write `x/conduit/telegram/README.md` per `README_EXAMPLE.md` template.
11. Run `go test -race ./...` from the package directory.

## Task Breakdown

### Task 1: Scaffold the `x/conduit/telegram/` Package
- **Goal**: Create the package directory and `go.mod` as a standalone module linked to the core ore module.
- **Dependencies**: None.
- **Files Affected**: None (all new).
- **New Files**:
  - `x/conduit/telegram/go.mod`
- **Interfaces**: N/A.
- **Validation**: `cd x/conduit/telegram && go mod tidy` succeeds without errors. The module path must be `github.com/andrewhowdencom/ore/x/conduit/telegram` with `replace github.com/andrewhowdencom/ore => ../../..`.
- **Details**: Copy the `go.mod` pattern from `x/conduit/http/go.mod`, adjusting the module path. The file should include `github.com/andrewhowdencom/ore v0.0.0` as a require and `github.com/stretchr/testify v1.11.1` for testing. After writing, run `go mod tidy` to resolve indirects.

### Task 2: Implement the Core Conduit (`telegram.go`)
- **Goal**: Implement the full Telegram conduit with constructor, functional options, `Start()` lifecycle, polling loop, sink registration, and API client.
- **Dependencies**: Task 1.
- **Files Affected**: None (all new).
- **New Files**:
  - `x/conduit/telegram/telegram.go`
- **Interfaces**:
  ```go
  func New(mgr *session.Manager, opts ...Option) (conduit.Conduit, error)
  func (c *telegramConduit) Start(ctx context.Context) error
  func WithBotToken(token string) Option
  func WithHTTPClient(client *http.Client) Option
  func WithGetUpdatesTimeout(seconds int) Option
  ```
- **Validation**: `cd x/conduit/telegram && go build ./...` compiles without errors.
- **Details**:
  - Define internal structs matching Telegram Bot API JSON for `Update`, `Message`, `Chat`, `User`, `getUpdatesReq`, `getUpdatesResp`, `sendMessageReq`, `sendMessageResp`, `getMeResp`.
  - `Start()` sequence:
    1. If `c.botToken == ""`, return `fmt.Errorf("bot token is required")`.
    2. Call `getMe` to validate token. If `!resp.OK`, return fatal error.
    3. Cache `resp.Result.ID` as `botUserID`.
    4. Register sink: `cleanup := c.mgr.RegisterSink([]string{"turn_complete"}, func(streamID string, event loop.OutputEvent) { ... })`. Defer `cleanup()`.
    5. In sink callback: check `event.Context().Provenance == "telegram"`. If false, return. If `TurnCompleteEvent`, iterate `e.Turn.Artifacts`, find `artifact.Text`, and call `sendMessage` to `chatID = streamID` (parse with `strconv.ParseInt`). Log delivery errors but do not return them.
    6. Start polling loop in a goroutine:
       - Call `getUpdates` with `offset` starting at 0, `limit: 100`, `timeout: c.timeout` (default 30).
       - For each `update.Message` where `Text != ""` and `update.Message.From.ID != botUserID`:
         - `chatIDStr := strconv.FormatInt(update.Message.Chat.ID, 10)`
         - `stream, err := c.mgr.Attach(chatIDStr)`
         - `stream.Process(ctx, session.UserMessageEvent{Content: update.Message.Text, Ctx: loop.EventContext{Provenance: "telegram"}})`
       - Update `offset = max(update.UpdateID + 1, offset)`.
       - On transient errors (timeout, network), log and continue. On `ctx.Done()`, return.
    7. Block on `<-ctx.Done()`.
    8. Return nil.
  - The polling goroutine should respect `ctx.Done()` by passing `ctx` into the HTTP request so `getUpdates` can be interrupted.

### Task 3: Add Table-Driven Tests (`telegram_test.go`)
- **Goal**: Test the constructor validation, startup failure modes, message processing, reply delivery, echo suppression, and graceful shutdown.
- **Dependencies**: Task 2.
- **Files Affected**: None (all new).
- **New Files**:
  - `x/conduit/telegram/telegram_test.go`
- **Interfaces**: N/A (tests exercise public API).
- **Validation**: `cd x/conduit/telegram && go test -race ./...` passes.
- **Details**:
  - Use `httptest.Server` to mock the Telegram Bot API (`/bot<token>/getMe`, `/bot<token>/getUpdates`, `/bot<token>/sendMessage`).
  - Use a real `session.Manager` with:
    - `thread.NewMemoryStore()` (or equivalent in-memory store)
    - A no-op `provider.Provider` mock
    - `func() *loop.Step { return loop.New() }`
    - A `TurnProcessor` that submits a test reply: `func(ctx, step, st, prov) (state.State, error) { return step.Submit(ctx, st, state.RoleAssistant, artifact.Text{Content: "Test reply"}) }`
  - Test cases:
    1. `New(nil)` → error.
    2. `New(mgr)` (zero options) → no error.
    3. `Start(ctx)` with empty token → error.
    4. `Start(ctx)` with invalid token (getMe returns `{"ok":false}`) → error.
    5. Incoming message triggers `Attach` + `Process` + mocked `sendMessage` with the assistant reply.
    6. Bot's own message is skipped (no `Process` call).
    7. `TurnCompleteEvent` with `Provenance != "telegram"` is skipped (no `sendMessage`).
    8. `Start()` returns nil after `ctx.Cancel()`.
  - For the mock server, maintain an `offset` counter and return updates when requested. Verify `sendMessage` receives the correct `chat_id` and `text`.
  - Ensure no data races: `go test -race ./...` must pass.

### Task 4: Write `README.md`
- **Goal**: Document the conduit for composers following the standardized README template.
- **Dependencies**: Task 2.
- **Files Affected**: None (all new).
- **New Files**:
  - `x/conduit/telegram/README.md`
- **Interfaces**: N/A.
- **Validation**: `README.md` is present and contains all required sections: Overview, Capabilities, Composition, Configuration, Runtime Semantics, Forge Blueprint, Error Handling.
- **Details**:
  - Copy the structure from `.agents/skills/conduit/README_EXAMPLE.md`.
  - Adapt all placeholders:
    - Name: "Telegram"
    - Description: "Telegram Bot API conduit via long-polling"
    - Capabilities: `event-source`, `accept-text`, `render-turn`
    - Options table: `WithBotToken`, `WithHTTPClient`, `WithGetUpdatesTimeout`
    - Session model: per-chat `Attach(chat_id)`
    - Echo suppression: both bot-message skipping and provenance filtering
    - Forge blueprint: `module: github.com/andrewhowdencom/ore/x/conduit/telegram`
    - Error handling: fatal vs. non-fatal sections

### Task 5: Integrate with Root Module and Documentation Generator [Optional]
- **Goal**: Optionally make the new conduit discoverable by `cmd/docgen` and usable from the root module workspace.
- **Dependencies**: Tasks 1–4.
- **Files Affected**:
  - `go.mod`
  - `cmd/docgen/main.go`
  - `cmd/docgen/main_test.go`
- **New Files**: None.
- **Interfaces**: N/A.
- **Validation**: `go mod tidy` from root succeeds; `go test ./...` from root passes.
- **Details**:
  - Add `require github.com/andrewhowdencom/ore/x/conduit/telegram v0.0.0-00010101000000-000000000000` to root `go.mod`.
  - Add `replace github.com/andrewhowdencom/ore/x/conduit/telegram => ./x/conduit/telegram` to root `go.mod`.
  - Import the package in `cmd/docgen/main.go` so it appears in generated docs.
  - Update `cmd/docgen/main_test.go` if it asserts on the list of conduits.
  - **Note**: This task is optional. The core requirement (forge-compatible module) is satisfied without it. Only perform if the builder wants root-level discoverability.

## Dependency Graph

- Task 1 → Task 2 → Task 3 → Task 4
- Task 5 || Task 4 (Task 5 can be done in parallel with Task 4, but depends on Task 2)

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| `thread.NewMemoryStore()` does not exist (name mismatch) | Medium | Low | Builder agent checks `thread/` package for actual in-memory store constructor name before writing tests. |
| `RegisterSink` does not expose `EventContext` on `OutputEvent` for provenance filtering | High | Low | Already verified: `loop.OutputEvent` has `Context() EventContext`. If somehow changed, fallback to always replying (no multi-conduit filtering). |
| Telegram `getUpdates` long-polling blocks `ctx.Done()` because `http.Client` does not support request cancellation with timeout | Medium | Medium | Pass `ctx` into the `http.NewRequestWithContext` call. Ensure `http.Client` has a timeout slightly longer than `getUpdates` timeout. Test with `ctx.Cancel()`. |
| Bot token validated at `Start()` instead of `New()` may confuse unit tests | Low | Medium | Document clearly in README and code comments. Tests should call `Start()` to verify token rejection. |
| Race condition between polling goroutine and sink callback accessing shared state | High | Low | All shared state (botUserID, offset) is read-only after `Start()` setup. `go test -race` validates. |

## Validation Criteria

- [ ] `x/conduit/telegram/go.mod` exists with correct module path and replace directive.
- [ ] `go mod tidy` succeeds in `x/conduit/telegram/`.
- [ ] `go build ./...` succeeds in `x/conduit/telegram/`.
- [ ] `go test -race ./...` passes in `x/conduit/telegram/`.
- [ ] Package exports `Descriptor` with `CapEventSource`, `CapAcceptText`, `CapRenderTurn`.
- [ ] `New(nil)` returns a non-nil error.
- [ ] `New(mgr)` (zero options) returns a non-nil `conduit.Conduit` without error.
- [ ] `Start(ctx)` with missing bot token returns a non-nil error.
- [ ] `Start(ctx)` calls `getMe` and returns error on invalid token.
- [ ] Incoming text message triggers `mgr.Attach(chatID)` and `stream.Process()` with `Provenance: "telegram"`.
- [ ] Bot's own messages are skipped in the polling loop.
- [ ] `TurnCompleteEvent` with `Provenance == "telegram"` triggers `sendMessage`.
- [ ] `TurnCompleteEvent` with `Provenance != "telegram"` does NOT trigger `sendMessage`.
- [ ] `Start(ctx)` blocks until `ctx.Done()` and returns nil on clean shutdown.
- [ ] `README.md` exists with all required sections per `README_EXAMPLE.md`.
