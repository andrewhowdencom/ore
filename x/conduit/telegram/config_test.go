package telegram

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOptionsFromMap_Valid(t *testing.T) {
	opts, err := OptionsFromMap(map[string]any{
		"bot_token":           "test-token",
		"get_updates_timeout": 60,
	})
	require.NoError(t, err)
	require.Len(t, opts, 2)

	tc := &telegramConduit{}
	for _, opt := range opts {
		opt(tc)
	}
	assert.Equal(t, "test-token", tc.botToken)
	assert.Equal(t, 60, tc.timeout)
}

func TestOptionsFromMap_Empty(t *testing.T) {
	opts, err := OptionsFromMap(map[string]any{})
	require.NoError(t, err)
	require.Empty(t, opts)
}

func TestOptionsFromMap_InvalidType(t *testing.T) {
	_, err := OptionsFromMap(map[string]any{"bot_token": map[string]any{}})
	require.Error(t, err)
}

func TestFromConfig_BotToken(t *testing.T) {
	opts := FromConfig(Config{BotToken: "tok"})
	require.Len(t, opts, 1)
	tc := &telegramConduit{}
	opts[0](tc)
	assert.Equal(t, "tok", tc.botToken)
}

func TestFromConfig_Timeout(t *testing.T) {
	timeout := 45
	opts := FromConfig(Config{GetUpdatesTimeout: &timeout})
	require.Len(t, opts, 1)
	tc := &telegramConduit{}
	opts[0](tc)
	assert.Equal(t, 45, tc.timeout)
}

func TestFromConfig_Empty(t *testing.T) {
	opts := FromConfig(Config{})
	require.Empty(t, opts)
}
