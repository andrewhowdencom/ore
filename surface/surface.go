// Package surface defines the Surface interface, the contract between I/O
// frontends and the tack framework. Implementations (TUI, web, Telegram,
// etc.) provide ingress events and consume egress actions (delta rendering,
// turn finalization, status updates).
package surface

import (
	"context"

	"github.com/andrewhowdencom/tack/artifact"
	"github.com/andrewhowdencom/tack/state"
)

// Surface is the contract between an I/O frontend and the tack framework.
// Concrete implementations are composed at build time; the framework does not
// assume any specific rendering mechanism (terminal, HTML, chat messages).
type Surface interface {
	// Events returns a read-only channel of user-generated events.
	// The channel is owned by the surface; it may be closed to signal
	// shutdown. The consumer should read until the channel is closed or
	// the context is cancelled.
	Events() <-chan Event

	// RenderDelta renders an ephemeral delta artifact incrementally.
	// The surface should type-assert the artifact to the concrete delta
	// type it understands (e.g., artifact.TextDelta) and update its
	// display accordingly. Deltas are not persisted to state.
	RenderDelta(ctx context.Context, delta artifact.Artifact) error

	// RenderTurn renders a complete turn that has been appended to state.
	// This is called once per turn, after all deltas for that turn have
	// been emitted, giving the surface a chance to finalize the display
	// of that turn in its conversation history.
	RenderTurn(ctx context.Context, turn state.Turn) error

	// SetStatus updates a transient status indicator.
	// Typical values: "thinking...", "calling tool...", "error: ...".
	SetStatus(ctx context.Context, status string) error
}
