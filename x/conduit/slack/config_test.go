package slack

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOptionsFromMap_Valid(t *testing.T) {
	opts, err := OptionsFromMap(map[string]any{
		"bot_token":  "xoxb-test",
		"app_token":  "xapp-test",
		"events_api": true,
	})
	require.NoError(t, err)
	require.Len(t, opts, 3)

	sc := &SlackConduit{}
	for _, opt := range opts {
		opt(sc)
	}
	assert.Equal(t, "xoxb-test", sc.botToken)
	assert.Equal(t, "xapp-test", sc.appToken)
	assert.Equal(t, modeEventsAPI, sc.mode)
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
	sc := &SlackConduit{}
	opts[0](sc)
	assert.Equal(t, "tok", sc.botToken)
}

func TestFromConfig_EventsAPIDefault(t *testing.T) {
	// nil EventsAPI means socket mode (default).
	opts := FromConfig(Config{})
	require.Empty(t, opts)
}

func TestFromConfig_EventsAPIFalse(t *testing.T) {
	eventsAPI := false
	opts := FromConfig(Config{EventsAPI: &eventsAPI})
	require.Empty(t, opts)
}

func TestFromConfig_EventsAPITrue(t *testing.T) {
	eventsAPI := true
	opts := FromConfig(Config{EventsAPI: &eventsAPI})
	require.Len(t, opts, 1)
	sc := &SlackConduit{}
	opts[0](sc)
	assert.Equal(t, modeEventsAPI, sc.mode)
}

func TestFromConfig_AppToken(t *testing.T) {
	opts := FromConfig(Config{AppToken: "xapp-test"})
	require.Len(t, opts, 1)
	sc := &SlackConduit{}
	opts[0](sc)
	assert.Equal(t, "xapp-test", sc.appToken)
}
