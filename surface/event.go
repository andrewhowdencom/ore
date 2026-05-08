package surface

// Event is the base interface for all ingress events from a Surface.
// Custom event types can be defined in other packages by implementing
// the public Kind() method, following the same pattern as artifact.Artifact.
type Event interface {
	Kind() string
}

// UserMessageEvent represents the user submitting a text message.
type UserMessageEvent struct {
	Content string
}

// Kind returns the event kind identifier.
func (e UserMessageEvent) Kind() string { return "user_message" }

// InterruptEvent represents the user interrupting the current operation.
type InterruptEvent struct{}

// Kind returns the event kind identifier.
func (e InterruptEvent) Kind() string { return "interrupt" }
