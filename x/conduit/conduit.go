// Package conduit defines capability constants and descriptors that are
// the lingua franca for capability discovery across ore frontends (TUI,
// web, Telegram, etc.). Concrete implementations are composed at build time;
// the framework does not assume any specific rendering mechanism.
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

// Well-known conduit capabilities.
const (
	CapEventSource         Capability = "event-source"
	CapShowStatus          Capability = "show-status"
	CapRenderDelta         Capability = "render-delta"
	CapRenderTurn          Capability = "render-turn"
	CapRenderMarkdown      Capability = "render-markdown"
	CapRenderImage         Capability = "render-image"
	CapRenderAudio         Capability = "render-audio"
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
