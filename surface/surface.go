// Package surface defines the Surface interface, the contract between I/O
// frontends and the ore framework. Implementations (TUI, web, Telegram,
// etc.) provide ingress events and consume egress events via subscription
// to the framework's event stream (e.g., loop.FanOut). Status updates remain
// explicit method calls. Concrete implementations are composed at build time;
// the framework does not assume any specific rendering mechanism.
package surface

import (
	"context"
)

// Surface is the contract between an I/O frontend and the ore framework.
// Concrete implementations are composed at build time; the framework does not
// assume any specific rendering mechanism (terminal, HTML, chat messages).
type Surface interface {
	// Events returns a read-only channel of user-generated events.
	// The channel is owned by the surface; it may be closed to signal
	// shutdown. The consumer should read until the channel is closed or
	// the context is cancelled.
	Events() <-chan Event

	// SetStatus updates a transient status indicator.
	// Typical values: "thinking...", "calling tool...", "error: ...".
	SetStatus(ctx context.Context, status string) error
}
