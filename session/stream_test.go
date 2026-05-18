package session

import (
	"context"
	"testing"
	"time"

	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/thread"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStream_Interface(t *testing.T) {
	store := thread.NewMemoryStore()
	prov := &mockProvider{}
	mgr := NewManager(store, prov, func() *loop.Step { return loop.New() }, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)
	require.NotNil(t, stream)
	assert.NotEmpty(t, stream.ID())

	// Verify all Stream methods are callable.
	ch := stream.Subscribe("text_delta", "turn_complete")
	require.NotNil(t, ch)

	err = stream.Process(context.Background(), UserMessageEvent{Content: "hi"})
	require.NoError(t, err)

	err = stream.Cancel()
	require.NoError(t, err)

	err = stream.Close()
	require.NoError(t, err)

	// After close, Subscribe should return a closed channel.
	ch = stream.Subscribe("text_delta")
	_, ok := <-ch
	require.False(t, ok, "channel should be closed")

	// Thread should still exist in the store.
	_, ok = store.Get(stream.ID())
	assert.True(t, ok)
}

func TestStream_Process_ContextPropagation(t *testing.T) {
	store := thread.NewMemoryStore()
	prov := &mockProvider{}
	mgr := NewManager(store, prov, func() *loop.Step { return loop.New() }, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	ch := stream.Subscribe("turn_complete")

	err = stream.Process(context.Background(), UserMessageEvent{Content: "hi", Ctx: loop.EventContext{Provenance: "test-provenance"}})
	require.NoError(t, err)

	events := drainWithClose(t, ch, func() { _ = stream.Close() })

	require.Len(t, events, 2) // user turn + assistant turn
	tc, ok := events[0].(loop.TurnCompleteEvent)
	require.True(t, ok)
	assert.Equal(t, "test-provenance", tc.Ctx.Provenance)
	tc, ok = events[1].(loop.TurnCompleteEvent)
	require.True(t, ok)
	assert.Equal(t, "test-provenance", tc.Ctx.Provenance)
}

func TestStream_ContextClearedBetweenProcesses(t *testing.T) {
	store := thread.NewMemoryStore()
	prov := &mockProvider{}
	mgr := NewManager(store, prov, func() *loop.Step { return loop.New() }, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	// First process with provenance
	ch1 := stream.Subscribe("turn_complete")
	err = stream.Process(context.Background(), UserMessageEvent{Content: "first", Ctx: loop.EventContext{Provenance: "first-provenance"}})
	require.NoError(t, err)

	var events1 []loop.OutputEvent
	for i := 0; i < 2; i++ {
		select {
		case event := <-ch1:
			events1 = append(events1, event)
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("timeout waiting for event %d", i)
		}
	}
	require.Len(t, events1, 2)
	assert.Equal(t, "first-provenance", events1[0].(loop.TurnCompleteEvent).Ctx.Provenance)
	assert.Equal(t, "first-provenance", events1[1].(loop.TurnCompleteEvent).Ctx.Provenance)

	// Second process without provenance — context should be cleared
	ch2 := stream.Subscribe("turn_complete")
	err = stream.Process(context.Background(), UserMessageEvent{Content: "second"})
	require.NoError(t, err)

	var events2 []loop.OutputEvent
	for i := 0; i < 2; i++ {
		select {
		case event := <-ch2:
			events2 = append(events2, event)
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("timeout waiting for event %d", i)
		}
	}
	require.Len(t, events2, 2)
	assert.Empty(t, events2[0].(loop.TurnCompleteEvent).Ctx.Provenance)
	assert.Empty(t, events2[1].(loop.TurnCompleteEvent).Ctx.Provenance)
}

func TestStream_InterruptEvent_ContextPropagation(t *testing.T) {
	store := thread.NewMemoryStore()
	prov := &mockProvider{}
	mgr := NewManager(store, prov, func() *loop.Step { return loop.New() }, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	// Process an interrupt with provenance — sets then clears context
	err = stream.Process(context.Background(), InterruptEvent{Ctx: loop.EventContext{Provenance: "interrupt-provenance"}})
	require.NoError(t, err)

	// Subsequent process without provenance should have empty context
	ch := stream.Subscribe("turn_complete")
	err = stream.Process(context.Background(), UserMessageEvent{Content: "after interrupt"})
	require.NoError(t, err)

	var events []loop.OutputEvent
	for i := 0; i < 2; i++ {
		select {
		case event := <-ch:
			events = append(events, event)
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("timeout waiting for event %d", i)
		}
	}
	require.Len(t, events, 2)
	assert.Empty(t, events[0].(loop.TurnCompleteEvent).Ctx.Provenance)
	assert.Empty(t, events[1].(loop.TurnCompleteEvent).Ctx.Provenance)
}
