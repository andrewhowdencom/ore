package conduit

import "context"

// Conduit is the common interface implemented by all ore frontends.
// Start initializes and runs the conduit, blocking until the context
// is cancelled or a fatal error occurs.
type Conduit interface {
	Start(ctx context.Context) error
}

// Capability is a well-known conduit capability.
type Capability string

// AudioNotifier is implemented by conduits that provide audible feedback
// for turn lifecycle events. PlayDone signals a successful assistant
// turn; PlayError signals a turn failure. Implementations may use
// distinct tones or the same sound, depending on platform constraints.
type AudioNotifier interface {
	PlayDone(ctx context.Context) error
	PlayError(ctx context.Context) error
}

// Well-known conduit capabilities.
const (
	CapEventSource Capability = "event-source"
	// CapShowStatus signals that the conduit can display a structured
	// status line fed by "properties" OutputEvents carrying map[string]string
	// key-value pairs (e.g. thread_id, token counts, model name).
	CapShowStatus     Capability = "show-status"
	CapRenderDelta    Capability = "render-delta"
	CapRenderTurn     Capability = "render-turn"
	CapRenderMarkdown Capability = "render-markdown"
	CapRenderImage    Capability = "render-image"
	// CapAudioNotification signals that the conduit can emit sound cues
	// for turn completion and error events. Clients check this to know
	// whether to wire AudioNotifier callbacks.
	CapAudioNotification   Capability = "audio-notification"
	CapAcceptText          Capability = "accept-text"
	CapAcceptImage         Capability = "accept-image"
	CapAcceptVoice         Capability = "accept-voice"
	CapAcceptFile          Capability = "accept-file"
	CapShowTypingIndicator Capability = "show-typing-indicator"
	CapRenderInlineButtons Capability = "render-inline-buttons"
	CapRequestUserConfirm  Capability = "request-user-confirmation"
)

// Descriptor describes a conduit implementation for documentation and
// static discovery. Each conduit package exports a Descriptor variable
// that enumerates the capabilities it provides.
type Descriptor struct {
	// Name is the human-readable conduit name (e.g., "TUI").
	Name string
	// Description is a short summary of the conduit.
	Description string
	// Capabilities lists the well-known capabilities this conduit supports.
	Capabilities []Capability
}

// StatusSegment represents a single key-value pair in a conduit's status
// line, grouped by an opaque Zone string. Conduits define their own
// rendering for zones; well-known zone names like "lifecycle" and
// "context" are conventions, not enforced constants.
type StatusSegment struct {
	// Label is the property key, e.g. "phase" or "model".
	Label string
	// Value is the property value, e.g. "streaming" or "gpt-4o".
	Value string
	// Zone is an opaque grouping identifier, e.g. "lifecycle" or
	// "context". The conduit interprets this string for rendering.
	Zone string
}

// StatusFormatter maps a flat map[string]string status into structured
// StatusSegment values. Each conduit (or application) provides a formatter
// that assigns keys to semantic zones for priority-based rendering.
type StatusFormatter func(map[string]string) []StatusSegment
