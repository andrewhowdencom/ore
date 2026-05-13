package tui

import (
	"testing"

	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/conduit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	ch := make(chan loop.OutputEvent, 10)
	tui := New(ch)
	require.NotNil(t, tui)
	assert.NotNil(t, tui.Events())
}

func TestTUI_Events(t *testing.T) {
	ch := make(chan loop.OutputEvent, 10)
	tui := New(ch)
	eventsCh := tui.Events()
	require.NotNil(t, eventsCh)
}

// Compile-time interface checks.
var (
	_ conduit.Conduit = (*TUI)(nil)
	_ conduit.Capable = (*TUI)(nil)
)

func TestTUI_Capabilities(t *testing.T) {
	tui := New(make(chan loop.OutputEvent, 10))
	caps := tui.Capabilities()

	assert.Equal(t, Descriptor.Capabilities, caps)

	expected := []conduit.Capability{
		conduit.CapEventSource,
		conduit.CapShowStatus,
		conduit.CapRenderDelta,
		conduit.CapRenderTurn,
		conduit.CapRenderMarkdown,
	}
	assert.Equal(t, expected, caps)

	assert.NotContains(t, caps, conduit.CapRenderImage)
	assert.NotContains(t, caps, conduit.CapAcceptVoice)
}

func TestTUI_Can(t *testing.T) {
	tui := New(make(chan loop.OutputEvent, 10))

	tests := []struct {
		name string
		cap  conduit.Capability
		want bool
	}{
		{"event-source", conduit.CapEventSource, true},
		{"show-status", conduit.CapShowStatus, true},
		{"render-delta", conduit.CapRenderDelta, true},
		{"render-turn", conduit.CapRenderTurn, true},
		{"render-markdown", conduit.CapRenderMarkdown, true},
		{"render-image", conduit.CapRenderImage, false},
		{"accept-voice", conduit.CapAcceptVoice, false},
		{"unknown", conduit.Capability("unknown"), false},
		{"empty", conduit.Capability(""), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tui.Can(tt.cap))
		})
	}
}
