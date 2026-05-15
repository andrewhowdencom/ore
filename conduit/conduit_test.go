package conduit

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Capability model tests.

func TestCapabilityConstants_NonEmpty(t *testing.T) {
	caps := []Capability{
		CapEventSource,
		CapShowStatus,
		CapRenderDelta,
		CapRenderTurn,
		CapRenderMarkdown,
		CapRenderImage,
		CapRenderAudio,
		CapAcceptText,
		CapAcceptImage,
		CapAcceptVoice,
		CapAcceptFile,
		CapShowTypingIndicator,
		CapRenderInlineButtons,
		CapRequestUserConfirm,
	}
	for _, c := range caps {
		assert.NotEmpty(t, string(c), "capability constant must not be empty")
	}
}

func TestDescriptor(t *testing.T) {
	d := Descriptor{
		Name:         "Test",
		Description:  "Test conduit",
		Capabilities: []Capability{CapEventSource},
	}
	assert.Equal(t, "Test", d.Name)
	assert.Equal(t, "Test conduit", d.Description)
	assert.Equal(t, []Capability{CapEventSource}, d.Capabilities)
}
