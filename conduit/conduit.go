// Package conduit defines the Conduit interface, the contract between I/O
// frontends and the ore framework. Implementations (TUI, web, Telegram,
// etc.) provide ingress events and consume egress events via subscription
// to the framework's event stream (e.g., loop.FanOut). Status updates remain
// explicit method calls. Concrete implementations are composed at build time;
// the framework does not assume any specific rendering mechanism.
package conduit

// Capability is a well-known conduit capability.
type Capability string

// Well-known conduit capabilities.
const (
	CapEventSource          Capability = "event-source"
	CapShowStatus           Capability = "show-status"
	CapRenderDelta          Capability = "render-delta"
	CapRenderTurn           Capability = "render-turn"
	CapRenderMarkdown       Capability = "render-markdown"
	CapRenderImage          Capability = "render-image"
	CapRenderAudio          Capability = "render-audio"
	CapAcceptText           Capability = "accept-text"
	CapAcceptImage          Capability = "accept-image"
	CapAcceptVoice          Capability = "accept-voice"
	CapAcceptFile           Capability = "accept-file"
	CapShowTypingIndicator  Capability = "show-typing-indicator"
	CapRenderInlineButtons  Capability = "render-inline-buttons"
	CapRequestUserConfirm   Capability = "request-user-confirmation"
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

// Capable is implemented by conduits that declare their capabilities.
type Capable interface {
	// Capabilities returns the full list of capabilities this conduit provides.
	Capabilities() []Capability
	// Can reports whether the conduit supports a specific capability.
	Can(cap Capability) bool
}

// Conduit is the contract between an I/O frontend and the ore framework.
// It declares ingress capabilities (event production) via the embedded
// Capable interface. Concrete implementations are composed at build time;
// the framework does not assume any specific rendering mechanism.
type Conduit interface {
	Capable
	// Events returns a read-only channel of user-generated events.
	// The channel is owned by the conduit; it may be closed to signal
	// shutdown. The consumer should read until the channel is closed or
	// the context is cancelled.
	Events() <-chan Event
}

// contains reports whether cap is present in caps.
func contains(caps []Capability, cap Capability) bool {
	for _, c := range caps {
		if c == cap {
			return true
		}
	}
	return false
}
