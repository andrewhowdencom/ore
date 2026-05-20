package slack

import (
	"testing"

	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/session"
	"github.com/andrewhowdencom/ore/thread"
	"github.com/slack-go/slack/slackevents"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSlackThreadIDFromEvent_DM(t *testing.T) {
	event := &slackevents.MessageEvent{
		Channel:   "D123456",
		TimeStamp: "1234567890.123456",
	}
	assert.Equal(t, "D123456", slackThreadIDFromEvent(event))
}

func TestSlackThreadIDFromEvent_ChannelReply(t *testing.T) {
	event := &slackevents.MessageEvent{
		Channel:         "C123456",
		ThreadTimeStamp: "1234567890.123456",
		TimeStamp:       "1234567890.234567",
	}
	assert.Equal(t, "1234567890.123456", slackThreadIDFromEvent(event))
}

func TestSlackThreadIDFromEvent_ChannelTopLevel(t *testing.T) {
	event := &slackevents.MessageEvent{
		Channel:   "C123456",
		TimeStamp: "1234567890.123456",
	}
	assert.Equal(t, "1234567890.123456", slackThreadIDFromEvent(event))
}

func TestIsDM(t *testing.T) {
	tests := []struct {
		channelID string
		want      bool
	}{
		{"D123456", true},
		{"C123456", false},
		{"G123456", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.channelID, func(t *testing.T) {
			assert.Equal(t, tt.want, isDM(tt.channelID))
		})
	}
}

func TestResolveThread_Create(t *testing.T) {
	store := thread.NewMemoryStore()
	prov := &mockProvider{}
	mgr := session.NewManager(store, prov, func() *loop.Step { return loop.New() }, simpleProcessor())

	c, err := New(mgr)
	require.NoError(t, err)

	sc, ok := c.(*SlackConduit)
	require.True(t, ok)

	stream, thr, err := sc.resolveThread("test-slack-thread-1", "C999")
	require.NoError(t, err)
	require.NotNil(t, stream)
	require.NotNil(t, thr)

	// Verify metadata was set.
	val, ok := thr.GetMetadata("slack_thread_id")
	require.True(t, ok)
	assert.Equal(t, "test-slack-thread-1", val)

	channelID, ok := thr.GetMetadata("slack_channel_id")
	require.True(t, ok)
	assert.Equal(t, "C999", channelID)
}

func TestResolveThread_Resume(t *testing.T) {
	store := thread.NewMemoryStore()
	prov := &mockProvider{}
	mgr := session.NewManager(store, prov, func() *loop.Step { return loop.New() }, simpleProcessor())

	c, err := New(mgr)
	require.NoError(t, err)

	sc, ok := c.(*SlackConduit)
	require.True(t, ok)

	// Create a thread first.
	stream1, thr1, err := sc.resolveThread("test-slack-thread-2", "C999")
	require.NoError(t, err)
	require.NotNil(t, stream1)
	require.NotNil(t, thr1)

	// Verify channel metadata was stored.
	channelID, ok := thr1.GetMetadata("slack_channel_id")
	require.True(t, ok)
	assert.Equal(t, "C999", channelID)

	// Resolve again — should attach to the same thread.
	stream2, thr2, err := sc.resolveThread("test-slack-thread-2", "C999")
	require.NoError(t, err)
	require.NotNil(t, stream2)
	require.NotNil(t, thr2)

	// Both should map to the same thread ID.
	assert.Equal(t, thr1.ID, thr2.ID)
}
