package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOptionsFromMap_Valid(t *testing.T) {
	opts, err := OptionsFromMap(map[string]any{"thread_id": "abc123"})
	require.NoError(t, err)
	require.Len(t, opts, 1)

	// Apply the option directly to verify it sets the field.
	tui := &TUI{}
	for _, opt := range opts {
		opt(tui)
	}
	assert.Equal(t, "abc123", tui.threadID)
}

func TestOptionsFromMap_Empty(t *testing.T) {
	opts, err := OptionsFromMap(map[string]any{})
	require.NoError(t, err)
	require.Empty(t, opts)
}

func TestOptionsFromMap_InvalidType(t *testing.T) {
	_, err := OptionsFromMap(map[string]any{"thread_id": map[string]any{}})
	require.Error(t, err)
}

func TestFromConfig_ThreadID(t *testing.T) {
	opts := FromConfig(Config{ThreadID: "xyz"})
	require.Len(t, opts, 1)

	tui := &TUI{}
	for _, opt := range opts {
		opt(tui)
	}
	assert.Equal(t, "xyz", tui.threadID)
}

func TestFromConfig_Empty(t *testing.T) {
	opts := FromConfig(Config{})
	require.Empty(t, opts)
}
