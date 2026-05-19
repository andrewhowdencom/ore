# Plan: Implement Slack Socket Mode Conduit (x/conduit/slack/)

## Objective

Implement a new ore I/O conduit under `x/conduit/slack/` that integrates ore agents into Slack workspaces via Socket Mode (WebSocket). The conduit maps Slack conversation threads and DMs to persistent ore `Thread` sessions via `Thread.Metadata["slack_thread_id"]`, handles inbound message events with echo suppression, and delivers assistant text responses back into the originating Slack thread or DM. The conduit must satisfy the standard ore conduit contract, be declarable in `forge.yaml`, and work with zero constructor options so `cmd/forge` compatibility is preserved.

## Context

### Repository Topology

The `ore` framework is a Go project with the following layout:

- **Core packages** (root level): `artifact/`, `state/`, `provider/`, `loop/`, `session/`, `thread/`
- **Conduits** under `x/conduit/<name>/` with their own `go.mod` and `replace github.com/andrewhowdencom/ore => ../../..`
- **Existing conduits**: `x/conduit/http/` (REST + SSE + NDJSON), `x/conduit/tui/` (Bubble Tea terminal UI)
- **Forge CLI** at `cmd/forge/` generates agent binaries from `forge.yaml` blueprints
- **Agent orchestrator** at `agent/agent.go` starts all conduits concurrently; fatal errors from `Start()` trigger shutdown

### Key Patterns from Existing Conduits

Both `x/conduit/http/handler.go` and `x/conduit/tui/tui.go` follow the standard contract:

1. `New(mgr *session.Manager, opts ...Option) (conduit.Conduit, error)` — functional options, validates `mgr != nil`
2. Package-level `Descriptor` variable enumerating capabilities
3. `Start(ctx context.Context) error` — blocks until `ctx.Done()`, subscribes to output before blocking
4. Inbound: map external events to `session.UserMessageEvent` or `session.InterruptEvent`
5. Outbound: `stream.Subscribe("turn_complete")` → deliver artifacts to external system

### Thread Metadata and Resumption

`thread.Store` provides `GetBy(key, value) (*Thread, bool)` for metadata-based lookup (line 57, `thread/store.go`). `Thread` has `Metadata map[string]string` with `SetMetadata()`/`GetMetadata()` protected by `metaMu` (`thread/memory.go`).

`session.Manager.Create()` creates a new thread+stream. `session.Manager.Attach(threadID)` resumes an existing thread. The `Stream` type does **not** expose its underlying `*thread.Thread`; conduits must use `mgr.Store().Get(stream.ID())` to retrieve the thread object for metadata mutation.

### Forge Integration

`cmd/forge/templates/main.go.tmpl` calls each conduit as `alias.New(mgr)` with **zero arguments**. This means all options must have sensible defaults. The generated binary reads `ORE_API_KEY`, `ORE_MODEL`, `ORE_BASE_URL`, and `STORE_DIR` at runtime. Slack tokens must follow the same pattern: read from environment variables (e.g., `SLACK_BOT_TOKEN`, `SLACK_APP_TOKEN`) at `Start()` time, with functional options for override.

### Agent Orchestration

`agent.Run(ctx)` starts all conduits in goroutines and blocks until `ctx` is cancelled **or any conduit returns a non-nil error**, at which point it cancels the context to signal remaining conduits to shut down. This means:
- `Start()` must return `nil` on clean shutdown (`ctx.Done()`)
- `Start()` must return non-nil **only** for fatal startup/runtime errors
- Non-fatal errors (delivery failure, session busy) must be logged and swallowed

### Echo Suppression

`loop.EventContext.Provenance` is a `string` field. Conduits set `Provenance = "slack"` on outbound `UserMessageEvent`. The Slack conduit additionally skips any incoming Slack message where `user == bot_user_id` to prevent responding to its own messages.

## Architectural Blueprint

### Selected Transport: Socket Mode (WebSocket) via slack-go/slack

**Socket Mode is the zero-option default.** The conduit uses `github.com/slack-go/slack` with its `socketmode` subpackage to handle the WebSocket connection, event framing, ping/pong, and reconnection. This is the pragmatic choice: manual WebSocket implementation would require URL loading, handshake, heartbeat, reconnection logic, and event acknowledgement — significant complexity for an I/O conduit that adds no ore-specific value.

**Rejected alternative**: Implement Socket Mode manually with `gorilla/websocket`. Rejected due to high implementation overhead and reconnection complexity that would delay delivery without adding framework value.

### Extensibility Hook: WithEventsAPI()

A functional option `WithEventsAPI()` switches the conduit to HTTP Events API mode. For the MVP, this option is a stub that may return an error or log a warning. The actual Events API implementation is out of scope, but the hook preserves forward compatibility.

### Thread Mapping

| Slack Context | Slack Thread Identifier | Ore Metadata Key |
|---|---|---|
| Channel message (top-level) | message `ts` | `slack_thread_id` |
| Channel thread reply | `thread_ts` of parent | `slack_thread_id` |
| DM (any message) | `channel_id` | `slack_thread_id` |

Lookup uses `store.GetBy("slack_thread_id", id)` for resumption.

### Conversation Lifecycle

1. **First qualifying message** (bot @mention in channel, or any message in DM):
   - Call `mgr.Create()` for a new ore session.
   - Retrieve thread via `mgr.Store().Get(stream.ID())`.
   - Store Slack identifier: `thr.SetMetadata("slack_thread_id", slackThreadID)`.
   - Save thread: `mgr.Store().Save(thr)`.
   - Reply via `chat.postMessage` with `thread_ts` set (channels) or directly in DM.

2. **Subsequent messages** in same thread/DM:
   - Extract `thread_ts` (channel) or `channel_id` (DM).
   - Lookup: `thr, ok := store.GetBy("slack_thread_id", slackThreadID)`.
   - If found, `stream, err := mgr.Attach(thr.ID)`.
   - Submit as `session.UserMessageEvent{Content: text, Ctx: loop.EventContext{Provenance: "slack"}}`.

### Addressing and Filtering

- **Channels**: Only respond to messages where the bot is @mentioned. Parse `app_mention` events or scan `message` text for the bot's user ID (`<@BOT_USER_ID>`).
- **DMs**: All messages implicitly address the bot.
- **Echo suppression**: Skip any incoming message where `event.User == botUserID`.

### Output Delivery

- Subscribe to `"turn_complete"` events from the stream's FanOut.
- On `loop.TurnCompleteEvent` with `RoleAssistant`, extract `artifact.Text` content.
- Deliver via `chat.postMessage` back into the originating Slack thread/DM.
- Log delivery errors (non-fatal) and continue.

## Requirements

1. Package `x/conduit/slack/` with its own `go.mod` and `replace` directive. [explicit]
2. Exports `Descriptor` with `CapEventSource`, `CapRenderTurn`, `CapAcceptText`. [explicit]
3. Constructor `New(mgr *session.Manager, opts ...Option) (conduit.Conduit, error)` validates `mgr != nil`. [explicit]
4. `Start(ctx)` blocks until `ctx.Done()`, subscribes to output events before blocking. [explicit]
5. Maps Slack inbound events (`message`, `app_mention`) to `session.UserMessageEvent` or `session.InterruptEvent`. [explicit]
6. Echo suppression via `EventContext.Provenance = "slack"` and `user == bot_user_id` skip. [explicit]
7. Thread creation and resumption via `Thread.Metadata["slack_thread_id"]` + `Store.GetBy()`. [explicit]
8. Table-driven tests with mocked Slack client or `httptest`. [explicit]
9. Passes `go test -race ./...`. [explicit]
10. `README.md` with standard sections (Overview, Capabilities, Composition, Configuration, Runtime Semantics, Forge Blueprint, Error Handling). [explicit]
11. Compatible with `forge.yaml` declaration by module path. [explicit]
12. Functional option `WithEventsAPI()` for future HTTP Events API support (stub for MVP). [explicit]
13. Default token source: environment variables `SLACK_BOT_TOKEN` and `SLACK_APP_TOKEN` read at `Start()` time. [inferred]
14. Non-fatal errors (delivery failure, session busy) are logged and swallowed; only fatal errors returned from `Start()`. [inferred]

## Task Breakdown

### Task 1: Create Package Skeleton
- **Goal**: Initialize `x/conduit/slack/` with `go.mod`, core types, constructor, `Descriptor`, and minimal compilable `Start()` stub.
- **Dependencies**: None
- **Files Affected**: None (new package)
- **New Files**:
  - `x/conduit/slack/go.mod`
  - `x/conduit/slack/slack.go` (types, constructor, `Descriptor`, stub `Start()`)
  - `x/conduit/slack/slack_test.go` (constructor nil-check, `Descriptor` export)
- **Interfaces**:
  ```go
  type SlackConduit struct {
      mgr      *session.Manager
      botToken string
      appToken string
      mode     transportMode // socket (default) or eventsAPI
  }
  type Option func(*SlackConduit)
  func New(mgr *session.Manager, opts ...Option) (conduit.Conduit, error)
  var Descriptor = conduit.Descriptor{
      Name:        "Slack",
      Description: "Slack Socket Mode conduit",
      Capabilities: []conduit.Capability{
          conduit.CapEventSource,
          conduit.CapRenderTurn,
          conduit.CapAcceptText,
      },
  }
  ```
- **Validation**: `cd x/conduit/slack && go test ./...` passes. `New(nil)` returns error. `Descriptor` is non-nil.
- **Details**: The `go.mod` must include `replace github.com/andrewhowdencom/ore => ../../..`. The `Start()` stub should simply `<-ctx.Done(); return nil` so the package compiles and tests pass. No Slack SDK import yet.

### Task 2: Define Slack Client Abstraction and Thread Mapping
- **Goal**: Define interfaces around Slack SDK methods and implement thread creation/resumption logic.
- **Dependencies**: Task 1
- **Files Affected**: `x/conduit/slack/slack.go`
- **New Files**: `x/conduit/slack/client.go` (interfaces), `x/conduit/slack/thread.go` (mapping logic)
- **Interfaces**:
  ```go
  type slackPoster interface {
      PostMessage(channelID string, options ...slack.MsgOption) (string, string, error)
  }
  type slackAuthTester interface {
      AuthTest() (*slack.AuthTestResponse, error)
  }
  ```
  ```go
  func (c *SlackConduit) resolveThread(slackThreadID string) (*session.Stream, *thread.Thread, error)
  func slackThreadIDFromEvent(event *slackevents.MessageEvent) string
  ```
- **Validation**: `go test ./...` passes with mocked `thread.Store` and `slackPoster`. Tests verify:
  - New thread creation when `GetBy` returns false
  - Existing thread resumption when `GetBy` returns true
  - Correct `slack_thread_id` metadata is set and saved
- **Details**: `resolveThread` encapsulates the lookup/creation logic: call `store.GetBy("slack_thread_id", id)`. If found, `mgr.Attach(thr.ID)`. If not, `mgr.Create()`, then `store.Get(stream.ID())`, `thr.SetMetadata("slack_thread_id", id)`, `store.Save(thr)`. The `slackThreadIDFromEvent` helper extracts the identifier: for channel messages, use `thread_ts` if present else `ts`; for DMs, use `channel_id`.

### Task 3: Implement Inbound Event Handling
- **Goal**: Parse Slack events, apply addressing/filtering rules, and submit qualifying messages to the session stream.
- **Dependencies**: Task 2
- **Files Affected**: `x/conduit/slack/slack.go`, `x/conduit/slack/client.go`
- **New Files**: `x/conduit/slack/events.go`
- **Interfaces**:
  ```go
  func (c *SlackConduit) handleMessageEvent(event *slackevents.MessageEvent, botUserID string) error
  func isAddressedToBot(event *slackevents.MessageEvent, botUserID string) bool
  ```
- **Validation**: `go test ./...` passes with table-driven tests covering:
  - DM message → addressed (true)
  - Channel @mention → addressed (true)
  - Channel message without @mention → addressed (false)
  - Bot's own message → skipped
  - Echo suppression via `Provenance = "slack"`
- **Details**:
  - `isAddressedToBot` returns true for DMs (`channel[0] == 'D'`) or messages containing `<@BOT_USER_ID>`.
  - `handleMessageEvent` checks `isAddressedToBot`, skips if `event.User == botUserID`, calls `resolveThread`, and submits `session.UserMessageEvent{Content: event.Text, Ctx: loop.EventContext{Provenance: "slack"}}`.
  - For `ErrSessionBusy`, log with `slog.Error` and return `nil` (non-fatal).

### Task 4: Implement Outbound Event Delivery
- **Goal**: Subscribe to `"turn_complete"` events and deliver assistant text artifacts back to Slack.
- **Dependencies**: Task 2
- **Files Affected**: `x/conduit/slack/slack.go`
- **New Files**: `x/conduit/slack/delivery.go`
- **Interfaces**:
  ```go
  func (c *SlackConduit) deliverTurnComplete(stream *session.Stream, event loop.TurnCompleteEvent, slackThreadID string, channelID string)
  ```
- **Validation**: `go test ./...` passes with mocked `slackPoster`. Tests verify:
  - `artifact.Text` content is extracted and passed to `PostMessage`
  - `thread_ts` is set correctly for channel threads
  - Delivery errors are logged but not propagated as fatal
- **Details**:
  - Subscribe: `outputCh := stream.Subscribe("turn_complete")`.
  - For each `loop.TurnCompleteEvent` with `RoleAssistant`, iterate over `e.Turn.Artifacts`.
  - For `artifact.Text`, build a single message string (concatenate multiple text artifacts if needed) and call `PostMessage(channelID, slack.MsgOptionText(content, false), slack.MsgOptionTS(slackThreadID))`.
  - Note: for DMs, `slackThreadID == channelID`, so `MsgOptionTS` is effectively a no-op or should be omitted for DMs. Use conditional logic: if channel is a DM (`channelID[0] == 'D'`), omit `MsgOptionTS`.
  - Log delivery errors with `slog.Error` and continue.

### Task 5: Implement Start() Orchestration and Graceful Shutdown
- **Goal**: Wire inbound and outbound loops into a blocking `Start()` that handles Socket Mode connection, event dispatch, and clean shutdown.
- **Dependencies**: Task 3, Task 4
- **Files Affected**: `x/conduit/slack/slack.go`, `x/conduit/slack/events.go`, `x/conduit/slack/delivery.go`
- **Interfaces**:
  ```go
  func (c *SlackConduit) Start(ctx context.Context) error
  ```
- **Validation**: `go test ./...` passes. A test verifies `Start()` blocks until `ctx.Done()` and returns `nil`.
- **Details**:
  1. Resolve tokens: `botToken = c.botToken || os.Getenv("SLACK_BOT_TOKEN")`; same for `appToken`. Return error if empty.
  2. Create `*slack.Client` and `*socketmode.Client` (or test doubles).
  3. `AuthTest()` to get `botUserID`.
  4. Spawn goroutine reading from `socketmode.Client.Events` channel, dispatching to `handleMessageEvent`.
  5. Spawn goroutine for outbound delivery (reads from `stream.Subscribe("turn_complete")` for each active stream).
     - Actually, subscription is per-stream. The conduit needs to subscribe to each stream when it's created/attached. Maintain a map of `slackThreadID -> *session.Stream`.
  6. Block on `<-ctx.Done()`.
  7. On shutdown, close Slack connection, return `nil`.
  - **Important**: Since the conduit may manage multiple streams (one per Slack thread), outbound delivery must subscribe to each stream individually. The simplest approach: inside `resolveThread`, after obtaining the stream, start a goroutine that subscribes to `"turn_complete"` for that stream and delivers events. Alternatively, maintain a `map[string]*session.Stream` and subscribe lazily. The plan recommends subscribing inside `resolveThread` or in a dedicated per-stream goroutine spawned after `Attach`/`Create`.

### Task 6: Complete Table-Driven Tests and README
- **Goal**: Add comprehensive tests covering all acceptance criteria and write the package README.
- **Dependencies**: Task 5
- **Files Affected**: `x/conduit/slack/slack_test.go`, `x/conduit/slack/README.md`
- **Validation**:
  - `go test -race ./...` passes in `x/conduit/slack/`
  - All acceptance criteria from issue #126 are covered by tests or README
- **Details**:
  - **Tests**: Constructor validation, thread creation/resumption, event filtering (DM, mention, no-mention, bot echo), echo suppression provenance, delivery success/failure, `Start()` block-until-ctx, fatal vs non-fatal error handling.
  - **README** sections: Overview, Capabilities, Composition (constructor example), Configuration (options + env vars table), Runtime Semantics (session model, event subscription, echo suppression, shutdown), Forge Blueprint (YAML example), Error Handling (fatal vs non-fatal).
  - Follow `.agents/skills/conduit/README_EXAMPLE.md` as the template.

## Dependency Graph

- Task 1 → Task 2 → Task 3 → Task 5
- Task 2 → Task 4 → Task 5
- Task 3 || Task 4 (parallelizable after Task 2)
- Task 5 → Task 6

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| `github.com/slack-go/slack` dependency adds weight or has breaking API changes | Medium | Low | Abstract all Slack SDK interactions behind small local interfaces (`slackPoster`, `slackAuthTester`). Swapping to stdlib HTTP + `gorilla/websocket` is possible if needed. |
| `thread.Store.GetBy()` linear scan performance degrades with many threads | Low | Low | `MemoryStore` and `JSONStore` both use linear scan. This is an existing framework limitation, not specific to Slack. Future store implementations can add indexing. |
| Concurrent multi-conduit agents race on `Thread.Metadata` mutation | Medium | Medium | `Thread` already protects `Metadata` with `metaMu` (RWMutex). Conduit only calls `SetMetadata`/`Save` on thread creation. Resumption path is read-only on metadata. |
| `socketmode.Client.Run()` blocks and may not respect `ctx.Done()` promptly | Medium | Medium | Run `socketmode.Client.Run()` in a separate goroutine. The main `Start()` goroutine blocks on `<-ctx.Done()` and cancels the socket mode context independently. Test shutdown behavior. |
| `cmd/forge` compatibility broken by mandatory constructor options | High | Low | Explicitly design `New(mgr)` to work with zero options. Tokens come from env vars in `Start()`. Test `New(mgr)` with no options in unit tests. |
| Slack thread identifier collision (same `ts` in different channels) | Medium | Low | Use `channel_id + ":" + ts` as the composite identifier for channel threads. DMs use `channel_id` alone. Document this in code comments. |

## Validation Criteria

- [ ] `go test -race ./...` passes in `x/conduit/slack/`
- [ ] Package exports `Descriptor` with `CapEventSource`, `CapRenderTurn`, `CapAcceptText`
- [ ] Constructor `New(mgr)` validates `mgr != nil` and works with zero options (forge-compatible)
- [ ] `Start(ctx)` blocks until `ctx.Done()` and returns `nil` on clean shutdown
- [ ] Subscribes to `"turn_complete"` output events before blocking
- [ ] Channel @mention messages and all DM messages create/resume a thread via `Thread.Metadata["slack_thread_id"]` + `Store.GetBy()`
- [ ] Echo suppression: skips incoming messages where `user == bot_user_id`; sets `EventContext.Provenance = "slack"` on outbound events
- [ ] Assistant text artifacts delivered via `chat.postMessage` into the correct Slack thread/DM
- [ ] `README.md` present with all required sections
- [ ] Compatible with `forge.yaml` declaration: `module: github.com/andrewhowdencom/ore/x/conduit/slack`
