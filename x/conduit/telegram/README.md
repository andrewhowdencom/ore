# Telegram Conduit

Telegram is a polling-driven ore conduit that connects to the Telegram Bot API
via long-polling, maps incoming text messages into `session.UserMessageEvent`s,
and replies to the originating chat with assistant text artifacts.

## Capabilities

This conduit exports the following capabilities (see `Descriptor.Capabilities`):

- **`event-source`** — long-polls the Telegram `getUpdates` endpoint and pushes
  inbound text messages into the ore session stream.
- **`accept-text`** — maps each `message.text` to
  `session.UserMessageEvent{Content: ...}`.
- **`render-turn`** — subscribes to `"turn_complete"` events via
  `session.Manager.RegisterSink` and replies to the originating `chat_id` with
  the assistant's text content.

## Composition

The constructor signature follows the standard ore conduit contract:

```go
func New(mgr *session.Manager, opts ...Option) (conduit.Conduit, error)
```

Instantiate the conduit with a `*session.Manager` and functional options:

```go
package main

import (
    "context"
    "log/slog"

    "github.com/andrewhowdencom/ore/session"
    "github.com/andrewhowdencom/ore/x/conduit/telegram"
)

func main() {
    mgr := session.NewManager(...)

    c, err := telegram.New(mgr,
        telegram.WithBotToken(os.Getenv("TELEGRAM_BOT_TOKEN")),
    )
    if err != nil {
        slog.Error("create conduit", "err", err)
        return
    }

    if err := c.Start(context.Background()); err != nil {
        slog.Error("conduit exited", "err", err)
    }
}
```

## Configuration / Options

| Option | Type | Default | Description |
|---|---|---|---|
| `WithBotToken(token string)` | `string` | *(none)* | Telegram bot token from BotFather. Required at `Start()` time; zero options are accepted at construction time so the conduit composes easily in hand-written main.go files. |
| `WithHTTPClient(client *http.Client)` | `*http.Client` | `60s` timeout | Custom HTTP client for Bot API requests. |
| `WithGetUpdatesTimeout(seconds int)` | `int` | `30` | Long-polling timeout in seconds passed to `getUpdates`. |

Environment variables read at runtime (not at construction time):

| Variable | Default | Description |
|---|---|---|
| `TELEGRAM_BOT_TOKEN` | *(none)* | Bot token used when `WithBotToken` is omitted. |

## Runtime Semantics

### Session Model

Each unique Telegram `chat_id` gets its own isolated ore `session.Thread`. The
first time a message arrives from a given chat, the conduit creates a
`thread.Thread` with the `chat_id` as its deterministic thread ID via
`mgr.Store().Save()` and then attaches to it. Subsequent messages from the same
chat resume the existing thread via `mgr.Attach(chat_id)`.

Sessions are closed when the conduit shuts down (`ctx.Done()`). Threads are
**not** deleted; they remain in the store for future resumption across
conduit restarts.

### Event Subscription

The conduit registers a `"turn_complete"` sink on the `session.Manager` inside
`Start()`:

```go
cleanup := mgr.RegisterSink([]string{"turn_complete"}, func(streamID string, event loop.OutputEvent) {
    // streamID == chat_id
})
defer cleanup()
```

Each received `loop.TurnCompleteEvent` whose `Role` is `assistant` and whose
`Provenance` matches `"telegram"` (or is empty) is converted to a text message
and sent back to the originating `chat_id` via the Bot API `sendMessage`
endpoint. The conduit does **not** stream deltas; it waits for the complete turn.

### Echo Suppression

Two mechanisms prevent feedback loops:

1. **Bot-message skipping**: During `Start()`, the conduit calls `getMe` to
   cache the bot's own user ID. In the polling loop, any `Update` whose
   `message.from.id` matches the bot ID is silently skipped.

2. **Provenance filtering**: Before calling `stream.Process(ctx, event)`, the
   conduit sets `EventContext.Provenance` to `"telegram"`. The sink callback
   checks `event.Context().Provenance`; events from other conduits (e.g., HTTP
   or TUI) are ignored so that the Telegram conduit does not reply to turns
   initiated by other frontends.

### Shutdown Behavior

On `ctx.Done()`:

1. The long-polling goroutine detects `ctx.Err()` and exits after the current
   `getUpdates` request is cancelled by the HTTP client.
2. The `RegisterSink` cleanup function is called via `defer`, removing the
   Telegram callback from the manager's sink list.
3. `Start()` returns `nil` for clean shutdown.

## Error Handling

### Fatal errors (returned from `Start()`)

These trigger agent-level shutdown:

- Missing bot token (`WithBotToken` was never called and no runtime token was
  supplied).
- Invalid bot token (the `getMe` validation call returns `ok=false` or a
  non-200 HTTP status).
- Unrecoverable network loss preventing all Bot API communication after
  transient retries are exhausted.

### Non-fatal errors (logged, conduit continues)

These are logged with `slog.Error` and the conduit keeps running:

- Single `sendMessage` delivery failure (the turn is dropped for this chat;
  subsequent turns are still attempted).
- Transient `getUpdates` timeout or HTTP error (the poll loop backs off for
  one second and retries).
- `stream.Process()` failure for a single message (e.g., session busy).
