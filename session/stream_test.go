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
	ch, err := stream.Subscribe("text_delta", "turn_complete")
	require.NoError(t, err)
	require.NotNil(t, ch)

	err = stream.Process(context.Background(), UserMessageEvent{Content: "hi"})
	require.NoError(t, err)

	err = stream.Cancel()
	require.NoError(t, err)

	err = stream.Close()
	require.NoError(t, err)

	// After close, Subscribe should error.
	_, err = stream.Subscribe("text_delta")
	require.Error(t, err)

	// Thread should still exist in the store.
	_, ok := store.Get(stream.ID())
	assert.True(t, ok)
}
