# Conduit Skeleton

Reference implementation for a single ore I/O conduit. Copy and adapt this
skeleton, replacing `<name>` with your conduit identifier. See `x/conduit/doc.go`
for the standard contract (constructor, Descriptor, sink registration, blocking
Start, graceful shutdown).

```go
package myconduit

import (
    "context"
    "fmt"

    "github.com/andrewhowdencom/ore/x/conduit"
    "github.com/andrewhowdencom/ore/loop"
    "github.com/andrewhowdencom/ore/session"
)

// See Standard Conduit Contract §2 — Exported Descriptor
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

// See Standard Conduit Contract §1 — Constructor
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

// See Standard Conduit Contract §3, §4 — Sink registration, Blocking Start
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

    // See Standard Conduit Contract §3 — Sink registration inside Start()
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

    // See Standard Conduit Contract §4 — Block until shutdown
    <-ctx.Done()

    // See Standard Conduit Contract §5 — Graceful shutdown
    return nil
}
```

> **Note:** This skeleton omits forge and multi-conduit patterns. For those,
> see the `conduit` skill and `examples/forge/README.md`.
