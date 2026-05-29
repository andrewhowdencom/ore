# Slack Conduit

> **Ore I/O Conduit** — Slack Socket Mode integration for ore agents.

## Overview

The Slack conduit connects ore agents to Slack workspaces via [Socket Mode](https://api.slack.com/apis/socket-mode),
a WebSocket-based transport that does not require a public HTTP endpoint.
Each Slack conversation thread (or DM channel) maps to a persistent ore `Thread`
via `Thread.Metadata["slack_thread_id"]`, enabling seamless conversation
resumption across agent restarts.

The conduit is a **dumb pipe**: it translates Slack message events into
`session.UserMessageEvent`, subscribes to the session manager's broadcast
`"turn_complete"` events, and delivers assistant text artifacts back into Slack
via `chat.postMessage`. It does not manage cognitive loops, tool execution, or
provider invocation — those are application-layer concerns composed in
`examples/` or `cmd/` packages.

## Capabilities

This conduit exports the following capabilities (see `Descriptor.Capabilities`):

- **`event-source`** — receives inbound Slack message events via Socket Mode
  and pushes them into the ore session stream.
- **`render-turn`** — subscribes to `"turn_complete"` events and delivers the
  full assistant text turn back to the originating Slack thread or DM.
- **`accept-text`** — maps Slack message text to `session.UserMessageEvent{Content: ...}`.

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

    "github.com/andrewhowdencom/ore/x/conduit/slack"
    "github.com/andrewhowdencom/ore/session"
)

func main() {
    mgr := session.NewManager(...)

    c, err := slack.New(mgr)
    if err != nil {
        slog.Error("create slack conduit", "err", err)
        return
    }

    if err := c.Start(context.Background()); err != nil {
        slog.Error("slack conduit exited", "err", err)
    }
}
```

For testing or advanced use cases, type-assert the returned `conduit.Conduit` to
`*slack.SlackConduit` and access package-specific options.

## Configuration / Options

| Option | Type | Default | Description |
|---|---|---|---|
| `WithBotToken(token string)` | `string` | *(env)* | Slack bot token (`xoxb-...`). |
| `WithAppToken(token string)` | `string` | *(env)* | Slack app-level token (`xapp-...`) for Socket Mode. |
| `WithEventsAPI()` | — | — | Switch to HTTP Events API mode (stub; not yet implemented). |

Environment variables read at runtime (by `Start()`, not at construction time):

| Variable | Default | Description |
|---|---|---|
| `SLACK_BOT_TOKEN` | *(none)* | Slack bot token (`xoxb-...`). |
| `SLACK_APP_TOKEN` | *(none)* | Slack app-level token (`xapp-...`) for Socket Mode. |

> **Note**: The constructor accepts zero options; tokens must be supplied at
> runtime (e.g., via environment variables).

## Runtime Semantics

### Session Model

- **First qualifying message** (bot `@mention` in a channel, or any message in
  a DM):
  - `mgr.Create()` creates a new ore thread and stream.
  - The Slack thread identifier is stored in `Thread.Metadata["slack_thread_id"]`.
  - The Slack channel ID is stored via `stream.SetMetadata("slack_channel_id", ...)`.
  - `stream.Save()` persists the metadata.
- **Subsequent messages** in the same thread/DM:
  - Extract `thread_ts` (channel) or `channel_id` (DM).
  - `mgr.GetBy("slack_thread_id", id)` finds the existing thread.
  - `mgr.Attach(thr.ID)` resumes the active stream.

| Slack Context | Slack Thread Identifier | Ore Metadata Key |
|---|---|---|
| Channel thread | `thread_ts` (or top-level `ts`) | `slack_thread_id` |
| DM | `channel_id` | `slack_thread_id` |

### Event Subscription

The conduit registers a `session.Manager` sink for `"turn_complete"` events.
The sink callback filters by `Provenance == "slack"` and `RoleAssistant`, then
looks up `slack_channel_id` and `slack_thread_id` from the thread metadata to
construct the `chat.postMessage` delivery.

### Echo Suppression

- **Inbound**: Any Slack message where `event.User == bot_user_id` is skipped.
- **Outbound**: `session.UserMessageEvent` carries `Ctx.Provenance = "slack"`.
  The sink callback checks this to avoid delivering turns from other conduits
  (e.g. HTTP or TUI) into Slack.

### Shutdown Behavior

On `ctx.Done()`:

1. The `Start()` function returns `nil` (clean shutdown).
2. All active streams tracked in `activeStreams` are closed.
3. The manager sink is unregistered.
4. The Socket Mode client goroutines exit naturally.

## Error Handling

### Fatal errors (returned from `Start()`)

These trigger agent-level shutdown via `agent.Run()`:

- Missing `SLACK_BOT_TOKEN` or `SLACK_APP_TOKEN` environment variables.
- Slack `AuthTest` failure (invalid or revoked token).
- Unrecoverable Socket Mode WebSocket connection loss (after retry exhaustion).

### Non-fatal errors (logged, conduit continues)

These are logged with `slog.Error` and do not stop the conduit:

- Malformed or unsupported Slack event types (skipped).
- `chat.postMessage` delivery failure (the turn is dropped for this delivery;
  subsequent turns are still attempted).
