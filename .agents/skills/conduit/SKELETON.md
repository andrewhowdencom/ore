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

    "github.com/andrewhowdencom/ore/loop"
    "github.com/andrewhowdencom/ore/session"
    "github.com/andrewhowdencom/ore/x/conduit"
)

// See Standard Conduit Contract §4 — Exported Descriptor
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
    sess   *session.Session
    events chan session.Event
}

// Option configures the conduit via functional options.
type Option func(*MyConduit)

// See Standard Conduit Contract §1 — Constructor
func New(sess *session.Session, opts ...Option) (conduit.Conduit, error) {
    if sess == nil {
        return nil, fmt.Errorf("session is required")
    }
    c := &MyConduit{sess: sess, events: make(chan session.Event, 16)}
    for _, opt := range opts {
        opt(c)
    }
    return c, nil
}

// Events returns input events for the application to pass to engine.Submit.
func (c *MyConduit) Events() <-chan session.Event { return c.events }

// See Standard Conduit Contract §5, §6 — Sink registration, Blocking Start
func (c *MyConduit) Start(ctx context.Context) error {
    defer close(c.events)

    // See Standard Conduit Contract §5 — Sink registration inside Start()
    // Subscribe to the output events your conduit renders.
    // "turn_complete" is the common choice for batched rendering.
    // For streaming, subscribe to artifact kinds directly.
    outputCh := c.sess.Subscribe("turn_complete", "error")

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

    // TODO: Set up external input -> c.events loop.
    //
    // Interactive: read input, then:
    //   c.events <- session.UserMessageEvent{Ctx: ctx, Content: text}
    // The application reads Events() and submits each event to its engine.

    // See Standard Conduit Contract §6 — Block until shutdown
    <-ctx.Done()

    // See Standard Conduit Contract §7 — Graceful shutdown
    return nil
}
```

> **Note:** Session creation, registration, and engine submission belong to the
> application. The conduit only translates external I/O.
