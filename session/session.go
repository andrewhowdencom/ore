package session

import (
	"context"

	"github.com/andrewhowdencom/ore/conduit"
	"github.com/andrewhowdencom/ore/loop"
)

// Session is the narrow, conduit-facing interface between I/O frontends and
// the ore framework. It provides ingress (Process) and egress (Subscribe)
// for a single active conversation, plus lifecycle controls (Cancel, Close).
type Session interface {
	// ID returns the session's unique identifier (same as the thread ID).
	ID() string

	// Process submits the event to the session's state and runs the inference
	// pipeline. The session must not be busy. Context cancellation aborts the
	// running TurnProcessor.
	//
	// Errors:
	//   - ErrSessionBusy if the session is already processing a turn
	//   - "unsupported event kind" for unknown event types
	//   - "process event: ..." wrapping any TurnProcessor or save error
	Process(ctx context.Context, event conduit.Event) error

	// Subscribe returns a filtered output event channel for the session's
	// loop.Step FanOut. An error is returned if the session is closed.
	//
	// The returned channel is closed when the session is closed.
	// Callers should range over the channel and handle closure.
	Subscribe(kinds ...string) (<-chan loop.OutputEvent, error)

	// Cancel aborts an ongoing turn by cancelling its context.
	Cancel() error

	// Close closes the session's Step and releases its resources.
	// The underlying thread is NOT deleted from the store.
	Close() error
}
