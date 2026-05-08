package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	tui := New()
	require.NotNil(t, tui)
	assert.NotNil(t, tui.Events())
}

func TestTUI_Events(t *testing.T) {
	tui := New()
	ch := tui.Events()
	require.NotNil(t, ch)
}
