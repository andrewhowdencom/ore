package conduit

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUserMessageEvent_Kind(t *testing.T) {
	e := UserMessageEvent{Content: "hello"}
	assert.Equal(t, "user_message", e.Kind())
}

func TestInterruptEvent_Kind(t *testing.T) {
	e := InterruptEvent{}
	assert.Equal(t, "interrupt", e.Kind())
}

func TestEventInterface(t *testing.T) {
	// Verify both types satisfy the Event interface.
	var _ Event = UserMessageEvent{}
	var _ Event = InterruptEvent{}
}

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

func TestContains(t *testing.T) {
	caps := []Capability{CapEventSource, CapShowStatus}

	assert.True(t, contains(caps, CapEventSource))
	assert.True(t, contains(caps, CapShowStatus))
	assert.False(t, contains(caps, CapRenderDelta))
	assert.False(t, contains(caps, Capability("unknown")))
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

type mockCapable struct {
	caps []Capability
}

func (m *mockCapable) Capabilities() []Capability { return m.caps }
func (m *mockCapable) Can(cap Capability) bool      { return contains(m.caps, cap) }

func TestCapable(t *testing.T) {
	m := &mockCapable{caps: []Capability{CapEventSource, CapShowStatus}}
	var _ Capable = m

	assert.Equal(t, []Capability{CapEventSource, CapShowStatus}, m.Capabilities())
	assert.True(t, m.Can(CapEventSource))
	assert.True(t, m.Can(CapShowStatus))
	assert.False(t, m.Can(CapRenderDelta))
	assert.False(t, m.Can(Capability("")))
}

type mockConduit struct {
	mockCapable
}

func (m *mockConduit) Events() <-chan Event { return nil }

func TestConduitInterface(t *testing.T) {
	var _ Conduit = (*mockConduit)(nil)
}
