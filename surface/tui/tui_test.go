package tui

import (
	"testing"

	"github.com/andrewhowdencom/ore/loop"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	src := make(chan loop.OutputEvent, 10)
	fanOut := loop.NewFanOut(src)
	defer fanOut.Close()

	tui := New(fanOut)
	require.NotNil(t, tui)
	assert.NotNil(t, tui.Events())
}

func TestTUI_Events(t *testing.T) {
	src := make(chan loop.OutputEvent, 10)
	fanOut := loop.NewFanOut(src)
	defer fanOut.Close()

	tui := New(fanOut)
	ch := tui.Events()
	require.NotNil(t, ch)
}
