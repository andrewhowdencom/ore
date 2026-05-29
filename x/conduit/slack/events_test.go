package slack

import (
	"context"
	"testing"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/session"
	"github.com/andrewhowdencom/ore/state"
	"github.com/slack-go/slack/slackevents"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// blockingProvider blocks until the context is cancelled.
type blockingProvider struct{}

func (m *blockingProvider) Invoke(ctx context.Context, s state.State, ch chan<- artifact.Artifact, opts ...provider.InvokeOption) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestIsAddressedToBot_DM(t *testing.T) {
	event := &slackevents.MessageEvent{Channel: "D123", Text: "hello"}
	assert.True(t, isAddressedToBot(event, "B123"))
}

func TestIsAddressedToBot_ChannelMention(t *testing.T) {
	event := &slackevents.MessageEvent{Channel: "C123", Text: "hello <@B123>"}
	assert.True(t, isAddressedToBot(event, "B123"))
}

func TestIsAddressedToBot_ChannelNoMention(t *testing.T) {
	event := &slackevents.MessageEvent{Channel: "C123", Text: "hello"}
	assert.False(t, isAddressedToBot(event, "B123"))
}

func TestHandleMessageEvent_BotEcho(t *testing.T) {
	store := session.NewMemoryStore()
	prov := &mockProvider{}
	mgr := session.NewManager(store, prov, func(*session.Thread) (*loop.Step, error) { return loop.New(), nil }, simpleProcessor())

	c, err := New(mgr)
	require.NoError(t, err)

	sc := c.(*SlackConduit)
	event := &slackevents.MessageEvent{
		Channel: "C123",
		User:    "B123", // same as botUserID
		Text:    "hello",
	}

	err = sc.handleMessageEvent(context.Background(), event, "B123")
	require.NoError(t, err)

	// Verify no thread was created.
	threads, err := store.List()
	require.NoError(t, err)
	assert.Empty(t, threads)
}

func TestHandleMessageEvent_ChannelNotAddressed(t *testing.T) {
	store := session.NewMemoryStore()
	prov := &mockProvider{}
	mgr := session.NewManager(store, prov, func(*session.Thread) (*loop.Step, error) { return loop.New(), nil }, simpleProcessor())

	c, err := New(mgr)
	require.NoError(t, err)

	sc := c.(*SlackConduit)
	event := &slackevents.MessageEvent{
		Channel: "C123",
		User:    "U456",
		Text:    "hello without mention",
	}

	err = sc.handleMessageEvent(context.Background(), event, "B123")
	require.NoError(t, err)

	threads, err := store.List()
	require.NoError(t, err)
	assert.Empty(t, threads)
}

func TestHandleMessageEvent_DM(t *testing.T) {
	store := session.NewMemoryStore()
	prov := &mockProvider{}
	mgr := session.NewManager(store, prov, func(thr *session.Thread) (*loop.Step, error) {
		return loop.New(loop.WithOnEmit(func(ctx context.Context, event loop.OutputEvent) {
			if tc, ok := event.(loop.TurnCompleteEvent); ok {
				thr.State.Append(tc.Turn.Role, tc.Turn.Artifacts...)
			}
		})), nil
	}, simpleProcessor())

	c, err := New(mgr)
	require.NoError(t, err)

	sc := c.(*SlackConduit)
	event := &slackevents.MessageEvent{
		Channel:   "D123",
		User:      "U456",
		Text:      "hello bot",
		TimeStamp: "1234567890.123456",
	}

	err = sc.handleMessageEvent(context.Background(), event, "B123")
	require.NoError(t, err)

	threads, err := store.List()
	require.NoError(t, err)
	require.Len(t, threads, 1)

	thr := threads[0]
	val, ok := thr.GetMetadata("slack_thread_id")
	require.True(t, ok)
	assert.Equal(t, "D123", val)

	channelID, ok := thr.GetMetadata("slack_channel_id")
	require.True(t, ok)
	assert.Equal(t, "D123", channelID)

}

func TestHandleMessageEvent_ChannelMention(t *testing.T) {
	store := session.NewMemoryStore()
	prov := &mockProvider{}
	mgr := session.NewManager(store, prov, func(*session.Thread) (*loop.Step, error) { return loop.New(), nil }, simpleProcessor())

	c, err := New(mgr)
	require.NoError(t, err)

	sc := c.(*SlackConduit)
	event := &slackevents.MessageEvent{
		Channel:   "C123",
		User:      "U456",
		Text:      "hello <@B123>",
		TimeStamp: "1234567890.123456",
	}

	err = sc.handleMessageEvent(context.Background(), event, "B123")
	require.NoError(t, err)

	threads, err := store.List()
	require.NoError(t, err)
	require.Len(t, threads, 1)

	val, ok := threads[0].GetMetadata("slack_thread_id")
	require.True(t, ok)
	assert.Equal(t, "1234567890.123456", val)

	channelID, ok := threads[0].GetMetadata("slack_channel_id")
	require.True(t, ok)
	assert.Equal(t, "C123", channelID)
}

func TestHandleMessageEvent_Concurrent(t *testing.T) {
	store := session.NewMemoryStore()
	prov := &blockingProvider{}
	mgr := session.NewManager(store, prov, func(*session.Thread) (*loop.Step, error) { return loop.New(), nil }, simpleProcessor())

	c, err := New(mgr)
	require.NoError(t, err)

	sc := c.(*SlackConduit)

	// Create the thread first so both messages resolve to the same stream.
	_, err = sc.resolveThread("D123", "D123")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		event := &slackevents.MessageEvent{
			Channel:   "D123",
			User:      "U456",
			Text:      "first message",
			TimeStamp: "1234567890.111111",
		}
		_ = sc.handleMessageEvent(ctx, event, "B123")
	}()

	// Wait for the first goroutine to start processing.
	time.Sleep(50 * time.Millisecond)

	// Second message should be enqueued without error.
	event2 := &slackevents.MessageEvent{
		Channel:   "D123",
		User:      "U456",
		Text:      "second message",
		TimeStamp: "1234567890.222222",
	}
	err = sc.handleMessageEvent(ctx, event2, "B123")
	require.NoError(t, err)

	cancel()
	<-done
}
