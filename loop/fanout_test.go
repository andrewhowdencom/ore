package loop

import (
	"sync"
	"testing"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFanOut_SingleSubscriber(t *testing.T) {
	src := make(chan outputEventEnvelope, 10)
	f := NewFanOut(src)
	defer f.Close()

	ch := f.Subscribe("text_delta")

	src <- outputEventEnvelope{event: artifact.TextDelta{Content: "hello"}, done: make(chan struct{})}
	src <- outputEventEnvelope{event: artifact.TextDelta{Content: "world"}, done: make(chan struct{})}
	close(src)

	var events []OutputEvent
	for e := range ch {
		events = append(events, e)
	}

	require.Len(t, events, 2)
	assert.Equal(t, "text_delta", events[0].Kind())
	assert.Equal(t, "hello", events[0].(artifact.TextDelta).Content)
	assert.Equal(t, "world", events[1].(artifact.TextDelta).Content)
}

func TestFanOut_MultipleSubscribersDifferentKinds(t *testing.T) {
	src := make(chan outputEventEnvelope, 10)
	f := NewFanOut(src)
	defer f.Close()

	deltaCh := f.Subscribe("text_delta")
	turnCh := f.Subscribe("turn_complete")

	src <- outputEventEnvelope{event: artifact.TextDelta{Content: "hello"}, done: make(chan struct{})}
	src <- outputEventEnvelope{event: TurnCompleteEvent{Turn: state.Turn{Role: state.RoleAssistant}}, done: make(chan struct{})}
	close(src)

	var deltas []OutputEvent
	for e := range deltaCh {
		deltas = append(deltas, e)
	}
	var turns []OutputEvent
	for e := range turnCh {
		turns = append(turns, e)
	}

	require.Len(t, deltas, 1)
	assert.Equal(t, "text_delta", deltas[0].Kind())

	require.Len(t, turns, 1)
	assert.Equal(t, "turn_complete", turns[0].Kind())
}

func TestFanOut_NoMatchingEvents(t *testing.T) {
	src := make(chan outputEventEnvelope, 10)
	f := NewFanOut(src)
	defer f.Close()

	ch := f.Subscribe("text_delta")

	src <- outputEventEnvelope{event: TurnCompleteEvent{Turn: state.Turn{Role: state.RoleAssistant}}, done: make(chan struct{})}
	close(src)

	var events []OutputEvent
	for e := range ch {
		events = append(events, e)
	}

	assert.Empty(t, events)
}

func TestFanOut_Close(t *testing.T) {
	src := make(chan outputEventEnvelope, 10)
	f := NewFanOut(src)

	deltaCh := f.Subscribe("text_delta")
	turnCh := f.Subscribe("turn_complete")

	require.NoError(t, f.Close())

	_, deltaOpen := <-deltaCh
	assert.False(t, deltaOpen, "delta channel should be closed")

	_, turnOpen := <-turnCh
	assert.False(t, turnOpen, "turn_complete channel should be closed")
}

func TestFanOut_CloseIdempotent(t *testing.T) {
	src := make(chan outputEventEnvelope, 10)
	f := NewFanOut(src)

	require.NoError(t, f.Close())
	require.NoError(t, f.Close()) // should not panic
}

func TestFanOut_LateSubscribe(t *testing.T) {
	src := make(chan outputEventEnvelope, 10)
	f := NewFanOut(src)
	defer f.Close()

	// Send events before subscribing. Because src is buffered, these events
	// sit in the channel until run() reads them. If a subscription is created
	// before run() drains the buffer, the late subscriber may receive buffered
	// events. This is acceptable — subscribers should be created before events
	// are produced.
	src <- outputEventEnvelope{event: artifact.TextDelta{Content: "early"}, done: make(chan struct{})}

	ch := f.Subscribe("text_delta")

	// Send events after subscribing.
	src <- outputEventEnvelope{event: artifact.TextDelta{Content: "late"}, done: make(chan struct{})}
	close(src)

	var events []OutputEvent
	for e := range ch {
		events = append(events, e)
	}

	// The late subscriber receives at least the event sent after subscription.
	require.GreaterOrEqual(t, len(events), 1)
	assert.Equal(t, "late", events[len(events)-1].(artifact.TextDelta).Content)
}

func TestFanOut_ConcurrentSubscribeAndSend(t *testing.T) {
	src := make(chan outputEventEnvelope, 100)
	f := NewFanOut(src)
	defer f.Close()

	// Start sending events concurrently.
	var sendWg sync.WaitGroup
	sendWg.Add(1)
	go func() {
		defer sendWg.Done()
		for i := 0; i < 50; i++ {
			src <- outputEventEnvelope{event: artifact.TextDelta{Content: "msg"}, done: make(chan struct{})}
		}
	}()

	// Subscribe concurrently while events are being sent.
	var subWg sync.WaitGroup
	for i := 0; i < 5; i++ {
		subWg.Add(1)
		go func() {
			defer subWg.Done()
			ch := f.Subscribe("text_delta")
			// Drain the channel.
			for range ch {
			}
		}()
	}

	sendWg.Wait()
	close(src)
	subWg.Wait()
}

func TestFanOut_MultipleKindsOneSubscriber(t *testing.T) {
	src := make(chan outputEventEnvelope, 10)
	f := NewFanOut(src)
	defer f.Close()

	ch := f.Subscribe("text_delta", "turn_complete")

	src <- outputEventEnvelope{event: artifact.TextDelta{Content: "hello"}, done: make(chan struct{})}
	src <- outputEventEnvelope{event: TurnCompleteEvent{Turn: state.Turn{Role: state.RoleAssistant}}, done: make(chan struct{})}
	close(src)

	var events []OutputEvent
	for e := range ch {
		events = append(events, e)
	}

	require.Len(t, events, 2)
	assert.Equal(t, "text_delta", events[0].Kind())
	assert.Equal(t, "turn_complete", events[1].Kind())
}

func TestFanOut_SlowSubscriberDoesNotBlock(t *testing.T) {
	src := make(chan outputEventEnvelope, 200)
	f := NewFanOut(src)
	defer f.Close()

	// Slow subscriber — never reads
	_ = f.Subscribe("text_delta")

	// Fill its buffer (100 events)
	for i := 0; i < 100; i++ {
		src <- outputEventEnvelope{event: artifact.TextDelta{Content: "filler"}, done: make(chan struct{})}
	}

	// Send 50 more events — these should be dropped without blocking.
	// If send() blocked on the full channel, the FanOut's run() goroutine
	// would deadlock and f.Close() (via defer) would hang, failing the test.
	for i := 0; i < 50; i++ {
		src <- outputEventEnvelope{event: artifact.TextDelta{Content: "msg"}, done: make(chan struct{})}
	}

	close(src)
}

func TestFanOut_SlowSubscriberDoesNotBlockOthers(t *testing.T) {
	src := make(chan outputEventEnvelope, 200)
	f := NewFanOut(src)
	defer f.Close()

	_ = f.Subscribe("text_delta")

	// Send 100 events to fill the slow subscriber's buffer
	for i := 0; i < 100; i++ {
		src <- outputEventEnvelope{event: artifact.TextDelta{Content: "filler"}, done: make(chan struct{})}
	}

	// Give the FanOut time to process the initial batch before creating
	// the fast subscriber, minimizing the chance fastCh receives filler.
	time.Sleep(10 * time.Millisecond)

	// Create fast subscriber
	fastCh := f.Subscribe("text_delta")

	// Send distinctive event
	src <- outputEventEnvelope{event: artifact.TextDelta{Content: "after-full"}, done: make(chan struct{})}
	close(src)

	// Fast subscriber should receive the distinctive event
	found := false
	for event := range fastCh {
		if event.(artifact.TextDelta).Content == "after-full" {
			found = true
		}
	}
	assert.True(t, found, "fast subscriber should receive event sent after slowCh was full")
}

func TestFanOut_MultipleSubscribersSameKind(t *testing.T) {
	src := make(chan outputEventEnvelope, 10)
	f := NewFanOut(src)
	defer f.Close()

	ch1 := f.Subscribe("text_delta")
	ch2 := f.Subscribe("text_delta")

	src <- outputEventEnvelope{event: artifact.TextDelta{Content: "hello"}, done: make(chan struct{})}
	close(src)

	e1 := <-ch1
	e2 := <-ch2

	assert.Equal(t, "hello", e1.(artifact.TextDelta).Content)
	assert.Equal(t, "hello", e2.(artifact.TextDelta).Content)

	_, open1 := <-ch1
	_, open2 := <-ch2
	assert.False(t, open1)
	assert.False(t, open2)
}
