package loop

import (
	"sync"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFanOut_SingleSubscriber(t *testing.T) {
	src := make(chan OutputEvent, 10)
	f := NewFanOut(src)
	defer f.Close()

	ch := f.Subscribe("delta")

	src <- DeltaEvent{Delta: artifact.TextDelta{Content: "hello"}}
	src <- DeltaEvent{Delta: artifact.TextDelta{Content: "world"}}
	close(src)

	var events []OutputEvent
	for e := range ch {
		events = append(events, e)
	}

	require.Len(t, events, 2)
	assert.Equal(t, "delta", events[0].Kind())
	assert.Equal(t, "hello", events[0].(DeltaEvent).Delta.(artifact.TextDelta).Content)
	assert.Equal(t, "world", events[1].(DeltaEvent).Delta.(artifact.TextDelta).Content)
}

func TestFanOut_MultipleSubscribersDifferentKinds(t *testing.T) {
	src := make(chan OutputEvent, 10)
	f := NewFanOut(src)
	defer f.Close()

	deltaCh := f.Subscribe("delta")
	turnCh := f.Subscribe("turn_complete")

	src <- DeltaEvent{Delta: artifact.TextDelta{Content: "hello"}}
	src <- TurnCompleteEvent{Turn: state.Turn{Role: state.RoleAssistant}}
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
	assert.Equal(t, "delta", deltas[0].Kind())

	require.Len(t, turns, 1)
	assert.Equal(t, "turn_complete", turns[0].Kind())
}

func TestFanOut_NoMatchingEvents(t *testing.T) {
	src := make(chan OutputEvent, 10)
	f := NewFanOut(src)
	defer f.Close()

	ch := f.Subscribe("delta")

	src <- TurnCompleteEvent{Turn: state.Turn{Role: state.RoleAssistant}}
	close(src)

	var events []OutputEvent
	for e := range ch {
		events = append(events, e)
	}

	assert.Empty(t, events)
}

func TestFanOut_Close(t *testing.T) {
	src := make(chan OutputEvent, 10)
	f := NewFanOut(src)

	deltaCh := f.Subscribe("delta")
	turnCh := f.Subscribe("turn_complete")

	require.NoError(t, f.Close())

	_, deltaOpen := <-deltaCh
	assert.False(t, deltaOpen, "delta channel should be closed")

	_, turnOpen := <-turnCh
	assert.False(t, turnOpen, "turn_complete channel should be closed")
}

func TestFanOut_CloseIdempotent(t *testing.T) {
	src := make(chan OutputEvent, 10)
	f := NewFanOut(src)

	require.NoError(t, f.Close())
	require.NoError(t, f.Close()) // should not panic
}

func TestFanOut_LateSubscribe(t *testing.T) {
	src := make(chan OutputEvent, 10)
	f := NewFanOut(src)
	defer f.Close()

	// Send events before subscribing. Because src is buffered, these events
	// sit in the channel until run() reads them. If a subscription is created
	// before run() drains the buffer, the late subscriber may receive buffered
	// events. This is acceptable — subscribers should be created before events
	// are produced.
	src <- DeltaEvent{Delta: artifact.TextDelta{Content: "early"}}

	ch := f.Subscribe("delta")

	// Send events after subscribing.
	src <- DeltaEvent{Delta: artifact.TextDelta{Content: "late"}}
	close(src)

	var events []OutputEvent
	for e := range ch {
		events = append(events, e)
	}

	// The late subscriber receives at least the event sent after subscription.
	require.GreaterOrEqual(t, len(events), 1)
	assert.Equal(t, "late", events[len(events)-1].(DeltaEvent).Delta.(artifact.TextDelta).Content)
}

func TestFanOut_ConcurrentSubscribeAndSend(t *testing.T) {
	src := make(chan OutputEvent, 100)
	f := NewFanOut(src)
	defer f.Close()

	// Start sending events concurrently.
	var sendWg sync.WaitGroup
	sendWg.Add(1)
	go func() {
		defer sendWg.Done()
		for i := 0; i < 50; i++ {
			src <- DeltaEvent{Delta: artifact.TextDelta{Content: "msg"}}
		}
	}()

	// Subscribe concurrently while events are being sent.
	var subWg sync.WaitGroup
	for i := 0; i < 5; i++ {
		subWg.Add(1)
		go func() {
			defer subWg.Done()
			ch := f.Subscribe("delta")
			// Drain the channel.
			for range ch {
			}
		}()
	}

	sendWg.Wait()
	close(src)
	subWg.Wait()
}

func TestFanOut_MultipleSubscribersSameKind(t *testing.T) {
	src := make(chan OutputEvent, 10)
	f := NewFanOut(src)
	defer f.Close()

	ch1 := f.Subscribe("delta")
	ch2 := f.Subscribe("delta")

	src <- DeltaEvent{Delta: artifact.TextDelta{Content: "hello"}}
	close(src)

	e1 := <-ch1
	e2 := <-ch2

	assert.Equal(t, "hello", e1.(DeltaEvent).Delta.(artifact.TextDelta).Content)
	assert.Equal(t, "hello", e2.(DeltaEvent).Delta.(artifact.TextDelta).Content)

	_, open1 := <-ch1
	_, open2 := <-ch2
	assert.False(t, open1)
	assert.False(t, open2)
}
