package session

import (
	"context"
	"testing"

	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/thread"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSession_Interface(t *testing.T) {
	store := thread.NewMemoryStore()
	prov := &mockProvider{}
	mgr := NewManager(store, prov, func() *loop.Step { return loop.New() }, simpleProcessor())

	sess, err := mgr.Create()
	require.NoError(t, err)
	require.NotNil(t, sess)
	assert.NotEmpty(t, sess.ID())

	// Verify all Session interface methods are callable.
	ch, err := sess.Subscribe("text_delta", "turn_complete")
	require.NoError(t, err)
	require.NotNil(t, ch)

	err = sess.Process(context.Background(), UserMessageEvent{Content: "hi"})
	require.NoError(t, err)

	err = sess.Cancel()
	require.NoError(t, err)

	err = sess.Close()
	require.NoError(t, err)

	// After close, Subscribe should error.
	_, err = sess.Subscribe("text_delta")
	require.Error(t, err)

	// Thread should still exist in the store.
	_, ok := store.Get(sess.ID())
	assert.True(t, ok)
}
