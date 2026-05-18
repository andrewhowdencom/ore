package session

import "github.com/andrewhowdencom/ore/loop"

// Event is the base interface for all ingress events to a session Stream.
// Custom event types can be defined in other packages by implementing
// the public Kind() and Context() methods.
type Event interface {
	Kind() string
	Context() loop.EventContext
}

// UserMessageEvent represents the user submitting a text message.
type UserMessageEvent struct {
	Content string

	// Ctx carries the provenance/context metadata for the user message.
	Ctx loop.EventContext
}

// Kind returns the event kind identifier.
func (e UserMessageEvent) Kind() string { return "user_message" }

// Context returns the event's loop.EventContext metadata.
func (e UserMessageEvent) Context() loop.EventContext { return e.Ctx }

// InterruptEvent represents the user interrupting the current operation.
type InterruptEvent struct {
	// Ctx carries the provenance/context metadata for the interrupt event.
	Ctx loop.EventContext
}

// Kind returns the event kind identifier.
func (e InterruptEvent) Kind() string { return "interrupt" }

// Context returns the event's loop.EventContext metadata.
func (e InterruptEvent) Context() loop.EventContext { return e.Ctx }
