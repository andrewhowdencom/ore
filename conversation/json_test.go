package conversation

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONStore_CreateCreatesFile(t *testing.T) {
	dir := t.TempDir()
	store, err := NewJSONStore(dir)
	require.NoError(t, err)

	conv, err := store.Create()
	require.NoError(t, err)

	path := filepath.Join(dir, conv.ID+".json")
	_, err = os.Stat(path)
	require.NoError(t, err, "expected file to exist after Create")
}

func TestJSONStore_SaveUpdatesFile(t *testing.T) {
	dir := t.TempDir()
	store, err := NewJSONStore(dir)
	require.NoError(t, err)

	conv, err := store.Create()
	require.NoError(t, err)

	// Append a turn and save.
	conv.State.Append(state.RoleUser, artifact.Text{Content: "hello"})
	err = store.Save(conv)
	require.NoError(t, err)

	// Verify by loading into a new store (simulating restart).
	store2, err := NewJSONStore(dir)
	require.NoError(t, err)

	got, ok := store2.Get(conv.ID)
	require.True(t, ok)
	turns := got.State.Turns()
	require.Len(t, turns, 1)
	assert.Equal(t, state.RoleUser, turns[0].Role)
	require.Len(t, turns[0].Artifacts, 1)
	assert.Equal(t, "text", turns[0].Artifacts[0].Kind())
}

func TestJSONStore_GetLoadsFromDisk(t *testing.T) {
	dir := t.TempDir()
	store, err := NewJSONStore(dir)
	require.NoError(t, err)

	conv, err := store.Create()
	require.NoError(t, err)
	conv.State.Append(state.RoleUser, artifact.Text{Content: "hello"})
	require.NoError(t, store.Save(conv))

	// Create a fresh store pointing at the same directory.
	store2, err := NewJSONStore(dir)
	require.NoError(t, err)

	got, ok := store2.Get(conv.ID)
	require.True(t, ok)
	assert.Equal(t, conv.ID, got.ID)
	assert.Len(t, got.State.Turns(), 1)
}

func TestJSONStore_DeleteRemovesFile(t *testing.T) {
	dir := t.TempDir()
	store, err := NewJSONStore(dir)
	require.NoError(t, err)

	conv, err := store.Create()
	require.NoError(t, err)

	path := filepath.Join(dir, conv.ID+".json")
	_, err = os.Stat(path)
	require.NoError(t, err)

	ok := store.Delete(conv.ID)
	assert.True(t, ok)

	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err), "expected file to be removed")

	_, ok = store.Get(conv.ID)
	assert.False(t, ok)
}

func TestJSONStore_RestartRecoversConversations(t *testing.T) {
	dir := t.TempDir()

	// First store instance.
	store1, err := NewJSONStore(dir)
	require.NoError(t, err)

	conv1, err := store1.Create()
	require.NoError(t, err)
	conv1.State.Append(state.RoleUser, artifact.Text{Content: "msg1"})
	require.NoError(t, store1.Save(conv1))

	conv2, err := store1.Create()
	require.NoError(t, err)
	conv2.State.Append(state.RoleUser, artifact.Text{Content: "msg2"})
	require.NoError(t, store1.Save(conv2))

	// Second store instance (simulating process restart).
	store2, err := NewJSONStore(dir)
	require.NoError(t, err)

	got1, ok := store2.Get(conv1.ID)
	require.True(t, ok)
	assert.Len(t, got1.State.Turns(), 1)

	got2, ok := store2.Get(conv2.ID)
	require.True(t, ok)
	assert.Len(t, got2.State.Turns(), 1)
}

func TestJSONStore_CreatedAtPreserved(t *testing.T) {
	dir := t.TempDir()
	store1, err := NewJSONStore(dir)
	require.NoError(t, err)

	conv, err := store1.Create()
	require.NoError(t, err)
	createdAt := conv.CreatedAt

	time.Sleep(1 * time.Millisecond)
	conv.State.Append(state.RoleUser, artifact.Text{Content: "hello"})
	require.NoError(t, store1.Save(conv))

	store2, err := NewJSONStore(dir)
	require.NoError(t, err)

	got, ok := store2.Get(conv.ID)
	require.True(t, ok)
	assert.True(t, createdAt.Equal(got.CreatedAt))
	assert.True(t, got.UpdatedAt.After(createdAt))
}

func TestJSONStore_ConcurrentCreate(t *testing.T) {
	dir := t.TempDir()
	store, err := NewJSONStore(dir)
	require.NoError(t, err)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.Create()
			require.NoError(t, err)
		}()
	}
	wg.Wait()
}
