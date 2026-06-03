package session

import "context"

// Event is the base interface for all ingress events to a session Stream.
// Custom event types can be defined in other packages by implementing
// the public Kind() and Context() methods.
type Event interface {
	Kind() string
	Context() context.Context
}

// UserMessageEvent represents the user submitting a text message.
type UserMessageEvent struct {
	Content string

	// Ctx carries the provenance/context metadata for the user message.
	Ctx context.Context
}

// Kind returns the event kind identifier.
func (e UserMessageEvent) Kind() string { return "user_message" }

// Context returns the event's context.Context metadata.
func (e UserMessageEvent) Context() context.Context { return e.Ctx }

// InterruptEvent represents the user interrupting the current operation.
type InterruptEvent struct {
	// Ctx carries the provenance/context metadata for the interrupt event.
	Ctx context.Context
}

// Kind returns the event kind identifier.
func (e InterruptEvent) Kind() string { return "interrupt" }

// Context returns the event's context.Context metadata.
func (e InterruptEvent) Context() context.Context { return e.Ctx }
