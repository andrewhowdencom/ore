package session

import (
	"context"
	"testing"

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
