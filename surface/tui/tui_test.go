package tui

import (
	"testing"

	"github.com/andrewhowdencom/ore/loop"
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
