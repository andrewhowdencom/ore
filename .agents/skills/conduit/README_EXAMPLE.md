# AcmeWebhook Conduit

> **Reference Template** — This file is an example README for the `conduit` skill.
> Copy it into `x/conduit/<name>/README.md` and adapt all `<placeholders>` to your
> conduit's actual behavior. See `.agents/skills/conduit/SKILL.md` step 12.

## Overview

AcmeWebhook is an event-driven ore conduit that exposes an HTTP POST endpoint
for receiving external webhooks, maps each payload to a `session.UserMessageEvent`,
and streams assistant responses back to a configured callback URL.

## Capabilities

This conduit exports the following capabilities (see `Descriptor.Capabilities`):

- **`event-source`** — receives inbound events via HTTP POST and pushes them
  into the ore session stream.
- **`render-turn`** — subscribes to `"turn_complete"` events and delivers the
  full assistant turn to the callback URL.
- **`accept-text`** — maps webhook JSON payloads containing a `message` field
  to `session.UserMessageEvent{Content: ...}`.

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

    "github.com/andrewhowdencom/ore/x/conduit/acmewebhook"
    "github.com/andrewhowdencom/ore/session"
)

func main() {
    mgr := session.NewManager(...)

    c, err := acmewebhook.New(mgr,
        acmewebhook.WithAddr(":8080"),
        acmewebhook.WithCallbackURL("https://example.com/callback"),
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

For advanced use cases (e.g., embedding the handler in an existing
`http.Server`), type-assert the returned `conduit.Conduit` to `*acmewebhook.Handler`
and call `ServeMux()`.

## Configuration / Options

| Option | Type | Default | Description |
|---|---|---|---|
| `WithAddr(addr string)` | `string` | `":8080"` | TCP address for the HTTP listener. |
| `WithCallbackURL(url string)` | `string` | *(required)* | URL to which assistant turns are POSTed as JSON. |
| `WithThreadID(id string)` | `string` | `""` | Resume an existing thread on start. Empty string creates a new session. |
| `WithTimeout(d time.Duration)` | `duration` | `30s` | HTTP client timeout for callback delivery. |

Environment variables read at runtime (not at construction time):

| Variable | Default | Description |
|---|---|---|
| `ACME_WEBHOOK_SECRET` | *(none)* | HMAC secret for verifying webhook signatures. |

## Runtime Semantics

### Session Model

- On the first inbound webhook delivery, the conduit calls `mgr.Create()` to
  obtain a new ephemeral session.
- If the webhook payload contains a `thread_id` field, the conduit calls
  `mgr.Attach(threadID)` instead, resuming the existing thread.
- Sessions are closed when the conduit shuts down (`ctx.Done()`). Threads are
  **not** deleted; they remain in the store for future resumption.

### Event Subscription

The conduit subscribes to `"turn_complete"` events inside `Start()`:

```go
outputCh := stream.Subscribe("turn_complete")
```

Each received `loop.TurnCompleteEvent` is serialized to JSON and POSTed to
`WithCallbackURL`. The conduit does **not** stream deltas; it waits for the
complete turn.

### Echo Suppression

Before calling `stream.Process(ctx, event)`, the conduit sets
`EventContext.Provenance` to `"acmewebhook"`. When receiving events, it checks
whether `Provenance` matches its own identifier and skips processing to avoid
processing its own callback deliveries as new input.

### Shutdown Behavior

On `ctx.Done()`, the conduit:

1. Stops accepting new webhook deliveries (`server.Shutdown`).
2. Waits for in-flight callbacks to complete (up to `WithTimeout`).
3. Closes the session stream.
4. Returns `nil` for clean shutdown, or a non-nil error for fatal runtime errors
   (e.g., listener failure).

## Forge Blueprint

Declare the conduit in a `forge.yaml` by its Go module path:

```yaml
dist:
  name: acme-agent
  output_path: ./acme-agent
conduits:
  - module: github.com/andrewhowdencom/ore/x/conduit/acmewebhook
```

> **Note:** `cmd/forge` instantiates the conduit as `acmewebhook.New(mgr)` with
> no arguments. All options must have sensible defaults so the forge-generated
> binary starts without additional configuration. Runtime options (callback URL,
> secret) can be supplied via environment variables.

## Error Handling

### Fatal errors (returned from `Start()`)

These trigger agent-level shutdown:

- Failure to bind the HTTP listener (e.g., port already in use).
- Unrecoverable connection loss to the external callback service
  (after retry exhaustion).

### Non-fatal errors (logged, conduit continues)

These are logged with `slog.Error` and the conduit keeps running:

- Malformed webhook payload (returns HTTP 400 to the caller).
- Callback delivery timeout or HTTP error (the turn is dropped for this
  delivery; subsequent turns are still attempted).
- Session busy (HTTP 409 Conflict returned to the caller).
