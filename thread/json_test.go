package thread

import (
	"encoding/json"
	"fmt"
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

	thread, err := store.Create()
	require.NoError(t, err)

	path := filepath.Join(dir, thread.ID+".json")
	_, err = os.Stat(path)
	require.NoError(t, err, "expected file to exist after Create")
}

func TestJSONStore_SaveUpdatesFile(t *testing.T) {
	dir := t.TempDir()
	store, err := NewJSONStore(dir)
	require.NoError(t, err)

	thread, err := store.Create()
	require.NoError(t, err)

	// Append a turn and save.
	thread.State.Append(state.RoleUser, artifact.Text{Content: "hello"})
	err = store.Save(thread)
	require.NoError(t, err)

	// Verify by loading into a new store (simulating restart).
	store2, err := NewJSONStore(dir)
	require.NoError(t, err)

	got, ok := store2.Get(thread.ID)
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

	thread, err := store.Create()
	require.NoError(t, err)
	thread.State.Append(state.RoleUser, artifact.Text{Content: "hello"})
	require.NoError(t, store.Save(thread))

	// Create a fresh store pointing at the same directory.
	store2, err := NewJSONStore(dir)
	require.NoError(t, err)

	got, ok := store2.Get(thread.ID)
	require.True(t, ok)
	assert.Equal(t, thread.ID, got.ID)
	assert.Len(t, got.State.Turns(), 1)
}

func TestJSONStore_DeleteRemovesFile(t *testing.T) {
	dir := t.TempDir()
	store, err := NewJSONStore(dir)
	require.NoError(t, err)

	thread, err := store.Create()
	require.NoError(t, err)

	path := filepath.Join(dir, thread.ID+".json")
	_, err = os.Stat(path)
	require.NoError(t, err)

	ok := store.Delete(thread.ID)
	assert.True(t, ok)

	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err), "expected file to be removed")

	_, ok = store.Get(thread.ID)
	assert.False(t, ok)
}

func TestJSONStore_RestartRecoversThreads(t *testing.T) {
	dir := t.TempDir()

	// First store instance.
	store1, err := NewJSONStore(dir)
	require.NoError(t, err)

	thread1, err := store1.Create()
	require.NoError(t, err)
	thread1.State.Append(state.RoleUser, artifact.Text{Content: "msg1"})
	require.NoError(t, store1.Save(thread1))

	thread2, err := store1.Create()
	require.NoError(t, err)
	thread2.State.Append(state.RoleUser, artifact.Text{Content: "msg2"})
	require.NoError(t, store1.Save(thread2))

	// Second store instance (simulating process restart).
	store2, err := NewJSONStore(dir)
	require.NoError(t, err)

	got1, ok := store2.Get(thread1.ID)
	require.True(t, ok)
	assert.Len(t, got1.State.Turns(), 1)

	got2, ok := store2.Get(thread2.ID)
	require.True(t, ok)
	assert.Len(t, got2.State.Turns(), 1)
}

func TestJSONStore_CreatedAtPreserved(t *testing.T) {
	dir := t.TempDir()
	store1, err := NewJSONStore(dir)
	require.NoError(t, err)

	thread, err := store1.Create()
	require.NoError(t, err)
	createdAt := thread.CreatedAt

	time.Sleep(1 * time.Millisecond)
	thread.State.Append(state.RoleUser, artifact.Text{Content: "hello"})
	require.NoError(t, store1.Save(thread))

	store2, err := NewJSONStore(dir)
	require.NoError(t, err)

	got, ok := store2.Get(thread.ID)
	require.True(t, ok)
	assert.True(t, createdAt.Equal(got.CreatedAt))
	assert.True(t, got.UpdatedAt.After(createdAt))
}

func TestJSONStore_List(t *testing.T) {
	dir := t.TempDir()
	store, err := NewJSONStore(dir)
	require.NoError(t, err)

	thread1, err := store.Create()
	require.NoError(t, err)
	thread2, err := store.Create()
	require.NoError(t, err)

	list, err := store.List()
	require.NoError(t, err)
	require.Len(t, list, 2)

	ids := make(map[string]bool)
	for _, thread := range list {
		ids[thread.ID] = true
	}
	assert.True(t, ids[thread1.ID])
	assert.True(t, ids[thread2.ID])
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

func TestJSONStore_CorruptedFile(t *testing.T) {
	dir := t.TempDir()

	// Write a corrupted JSON file.
	err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte("not json"), 0644)
	require.NoError(t, err)

	// Write a valid thread file.
	valid := &Thread{
		ID:        "good",
		State:     &state.Memory{},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	valid.State.Append(state.RoleUser, artifact.Text{Content: "hello"})
	data, err := json.Marshal(valid)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(dir, "good.json"), data, 0644)
	require.NoError(t, err)

	// Create store — corrupted file should be silently skipped.
	store, err := NewJSONStore(dir)
	require.NoError(t, err)

	// good should be loadable.
	got, ok := store.Get("good")
	require.True(t, ok)
	assert.Len(t, got.State.Turns(), 1)

	// bad should not be found.
	_, ok = store.Get("bad")
	assert.False(t, ok)

	// List should only include good.
	list, err := store.List()
	require.NoError(t, err)
	assert.Len(t, list, 1)
	assert.Equal(t, "good", list[0].ID)
}

func TestJSONStore_ConcurrentCreateSaveGet(t *testing.T) {
	dir := t.TempDir()
	store, err := NewJSONStore(dir)
	require.NoError(t, err)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			thread, err := store.Create()
			require.NoError(t, err)
			thread.State.Append(state.RoleUser, artifact.Text{Content: fmt.Sprintf("msg-%d", i)})
			require.NoError(t, store.Save(thread))

			got, ok := store.Get(thread.ID)
			require.True(t, ok)
			assert.Len(t, got.State.Turns(), 1)
		}(i)
	}
	wg.Wait()

	// Verify all 50 threads exist.
	list, err := store.List()
	require.NoError(t, err)
	assert.Len(t, list, 50)
}
