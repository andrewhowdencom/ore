package tui

import (
	"testing"

	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/surface"
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
	_ surface.Surface = (*TUI)(nil)
	_ surface.Capable = (*TUI)(nil)
)

func TestTUI_Capabilities(t *testing.T) {
	tui := New(make(chan loop.OutputEvent, 10))
	caps := tui.Capabilities()

	assert.Equal(t, Descriptor.Capabilities, caps)

	expected := []surface.Capability{
		surface.CapEventSource,
		surface.CapShowStatus,
		surface.CapRenderDelta,
		surface.CapRenderTurn,
		surface.CapRenderMarkdown,
	}
	assert.Equal(t, expected, caps)

	assert.NotContains(t, caps, surface.CapRenderImage)
	assert.NotContains(t, caps, surface.CapAcceptVoice)
}

func TestTUI_Can(t *testing.T) {
	tui := New(make(chan loop.OutputEvent, 10))

	tests := []struct {
		name string
		cap  surface.Capability
		want bool
	}{
		{"event-source", surface.CapEventSource, true},
		{"show-status", surface.CapShowStatus, true},
		{"render-delta", surface.CapRenderDelta, true},
		{"render-turn", surface.CapRenderTurn, true},
		{"render-markdown", surface.CapRenderMarkdown, true},
		{"render-image", surface.CapRenderImage, false},
		{"accept-voice", surface.CapAcceptVoice, false},
		{"unknown", surface.Capability("unknown"), false},
		{"empty", surface.Capability(""), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tui.Can(tt.cap))
		})
	}
}
