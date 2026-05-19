---
name: conduit
description: |
  Implements I/O conduits for the ore framework. Creates packages under
  x/conduit/<name>/ with independent go.mod. Translates external events into
  session.UserMessageEvent via session.Manager, subscribes to loop.OutputEvent
  FanOut streams, and routes text/reasoning/image artifacts back to external
  systems using the broadcast subscriber-closure pattern. Does NOT handle
  cognitive orchestration, provider invocation, or turn-loop management.
---

# Ore Conduit

## When to Use

This skill is triggered **ONLY** when implementing a **NEW** I/O conduit package
under `x/conduit/<name>/` or modifying an existing conduit implementation.

Do NOT use this skill for:
- Core package work (`artifact/`, `loop/`, `session/`, `provider/`, `state/`, `thread/`, `cognitive/`)
- Modifying ore `AGENTS.md` or repository-level documentation
- Go language or tooling questions (see `go/` skill instead)
- Ore architectural philosophy or package boundary decisions (see `AGENTS.md`)

## Execution Procedure

Follow these steps in order. Do not skip or reorder.

1. **Create package `x/conduit/<name>/`** with its own `go.mod`. Use
   `replace github.com/andrewhowdencom/ore => ../../..` to link the core module.
2. **Implement `conduit.Conduit`** — a type with exactly one method:
   `Start(ctx context.Context) error`.
3. **Accept `*session.Manager`** via the constructor using the functional options
   pattern:
   ```go
   func New(mgr *session.Manager, opts ...Option) (conduit.Conduit, error)
   ```
   Validate `mgr != nil`; return an error if nil.
4. **In `Start()`**: create or attach a session:
   - `stream, err := mgr.Create()` for a new session
   - `stream, err := mgr.Attach(threadID)` to resume an existing thread
5. **Subscribe to output events** from the stream:
   ```go
   outputCh := stream.Subscribe("turn_complete")
   ```
   For streaming conduits, subscribe to artifact kinds directly
   (`"text_delta"`, `"reasoning_delta"`, etc.).
6. **Capture your delivery mechanism in the subscriber closure.** The subscriber
   goroutine must close over the external-system client (HTTP writer, Slack
   client, TUI program, etc.). Destination routing is **NOT** carried in
   `EventContext.Provenance`.
7. **Set up the external input → `stream.Process()` loop.** Map all external
   events to `session.UserMessageEvent{Content: ...}` and call
   `stream.Process(ctx, event)`. For cancellation, use
   `session.InterruptEvent{}`.
8. **Export a `Descriptor` variable** at the package level enumerating the
   well-known capabilities this conduit supports:
   ```go
   var Descriptor = conduit.Descriptor{
       Name:        "MyConduit",
       Description: "One-line description",
       Capabilities: []conduit.Capability{
           conduit.CapEventSource,
           conduit.CapRenderTurn,
       },
   }
   ```
9. **Block in `Start()`** until `ctx.Done()` signals shutdown. Return `nil` on
   clean shutdown; return non-nil only on fatal startup or runtime errors.
10. **Add table-driven tests** with a mock `session.Manager` or
    `httptest.Server`. Verify `Start()` blocks, `Descriptor` is exported, and
    the constructor rejects nil manager.
11. **Run `go test -race ./...`** from the package directory. All tests must
    pass.

## Boolean Guards

If any of the following are true, **STOP** and reassess:

- ⚠️ **IF** importing `cognitive/` → STOP. Conduits are dumb pipes. Cognitive
  patterns (ReAct, chain-of-thought) belong in the application layer, not the
  conduit.
- ⚠️ **IF** calling `provider.Invoke()` directly → STOP. Use
  `stream.Process()` which delegates to the session manager and turn processor.
- ⚠️ **IF** managing turn loops, tool execution loops, or ReAct logic → STOP.
  That is the `cognitive/` package's responsibility or the application's
  `TurnProcessor`.
- ⚠️ **IF** putting destination routing metadata (channel ID, thread ID, email
  address) into `EventContext.Provenance` → STOP. Capture the delivery mechanism
  in the subscriber closure. `Provenance` is for source metadata only.

## Inlined Expertise: The Conduit Skeleton

Copy and adapt this skeleton. Replace `<name>` with your conduit identifier.

```go
package myconduit

import (
    "context"
    "fmt"

    "github.com/andrewhowdencom/ore/x/conduit"
    "github.com/andrewhowdencom/ore/loop"
    "github.com/andrewhowdencom/ore/session"
)

// Descriptor enumerates the capabilities this conduit provides.
var Descriptor = conduit.Descriptor{
    Name:        "MyConduit",
    Description: "One-line description of what this conduit does",
    Capabilities: []conduit.Capability{
        conduit.CapEventSource,
        conduit.CapRenderTurn,
    },
}

// MyConduit is the conduit implementation. Keep it minimal.
type MyConduit struct {
    mgr      *session.Manager
    threadID string // optional; for resuming an existing thread
}

// Option configures the conduit via functional options.
type Option func(*MyConduit)

// WithThreadID sets the thread ID to resume on Start.
func WithThreadID(id string) Option {
    return func(c *MyConduit) {
        c.threadID = id
    }
}

// New creates the conduit. It does NOT start I/O.
func New(mgr *session.Manager, opts ...Option) (conduit.Conduit, error) {
    if mgr == nil {
        return nil, fmt.Errorf("session manager is required")
    }
    c := &MyConduit{mgr: mgr}
    for _, opt := range opts {
        opt(c)
    }
    return c, nil
}

// Start creates or attaches to a session, sets up the output subscriber,
// initializes external I/O, and blocks until ctx is cancelled.
func (c *MyConduit) Start(ctx context.Context) error {
    var stream *session.Stream
    var err error
    if c.threadID != "" {
        stream, err = c.mgr.Attach(c.threadID)
    } else {
        stream, err = c.mgr.Create()
    }
    if err != nil {
        return err
    }

    // Subscribe to the output events your conduit renders.
    // "turn_complete" is the common choice for batched rendering.
    // For streaming, subscribe to artifact kinds directly.
    outputCh := stream.Subscribe("turn_complete")

    // Capture your delivery mechanism in this closure.
    // Examples: http.ResponseWriter, Slack API client, tea.Program.
    go func() {
        for event := range outputCh {
            switch e := event.(type) {
            case loop.TurnCompleteEvent:
                // TODO: deliver e.Turn to the external system
                _ = e
            case loop.ErrorEvent:
                // TODO: handle or log delivery errors
                _ = e
            }
        }
    }()

    // TODO: Set up external input → stream.Process() loop.
    //
    // Interactive: read input, then:
    //   stream.Process(ctx, session.UserMessageEvent{Content: text})
    //
    // Webhook/polling: in your receiver goroutine:
    //   stream.Process(ctx, session.UserMessageEvent{Content: payload})

    // Block until the framework signals shutdown.
    <-ctx.Done()
    return nil
}
```

## Gotchas

1. **Subscriber backpressure.** `stream.Subscribe()` channels have a fixed buffer
   of 100 events. Slow subscribers silently drop events. Design for idempotency
   or tolerate missing deltas. Do not assume reliable delivery.
2. **Hardcoded event types.** `Stream.Process()` only accepts
   `session.UserMessageEvent` and `session.InterruptEvent`. All external inputs
   must map to one of these. Custom event types require changes to the
   `session` package.
3. **Provenance is source-only.** `EventContext.Provenance` is a `string` for
   echo suppression and audit trails. It is NOT for destination routing metadata
   (channel IDs, thread timestamps, email addresses). Capture the delivery
   mechanism in the subscriber closure.
4. **MarshalArtifact is hardcoded.** `x/conduit/http/types.go` has a fixed switch
   for core artifact kinds. If your conduit defines custom artifact types and
   uses HTTP transport, you must extend the marshal/unmarshal functions or
   handle serialization yourself.
5. **Closed subscription on dead session.** If a session is closed before a
   subscriber is created, `stream.Subscribe()` returns an already-closed
   channel. Range over it safely; it will exit immediately.
6. **NDJSON vs SSE vs turn_complete.** The HTTP conduit demonstrates two valid
   patterns: NDJSON streaming over a request/response connection, and SSE over a
   persistent ambient connection. The TUI subscribes to `"turn_complete"` for
   batched rendering. Choose the pattern that matches your transport.

## References

- `AGENTS.md` — ore architectural boundaries and the Conduit/Library vs.
  Application contract.
- `x/conduit/doc.go` — standard conduit contract documentation (constructor,
  Descriptor, blocking Start, graceful shutdown).
- `x/conduit/http/` — HTTP conduit reference (NDJSON streaming, SSE,
  embedded web UI, RESTful session endpoints).
- `x/conduit/tui/` — TUI conduit reference (Bubble Tea, turn_complete
  subscription, channel-based Process loop).
- `go/` skill — Go conventions (functional options, table-driven tests,
  error wrapping with `fmt.Errorf`, `log/slog`).
- `.plans/standardize-conduit-patterns.md` — repo-internal plan that
  standardized the HTTP `Descriptor` export and the `x/conduit/doc.go`
  conduit contract.

> **Note:** This skill is a living document. After implementing a new conduit,
> review whether any pattern you discovered should be added here.
