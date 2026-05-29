package session

import (
	"sync"
	"testing"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryStore_Create(t *testing.T) {
	store := NewMemoryStore()
	thread, err := store.Create()
	require.NoError(t, err)
	assert.NotEmpty(t, thread.ID)
	assert.NotNil(t, thread.State)
	assert.False(t, thread.CreatedAt.IsZero())
	assert.False(t, thread.UpdatedAt.IsZero())

	// Second creation should have a different ID.
	thread2, err := store.Create()
	require.NoError(t, err)
	assert.NotEqual(t, thread.ID, thread2.ID)
}

func TestMemoryStore_Get(t *testing.T) {
	store := NewMemoryStore()
	thread, err := store.Create()
	require.NoError(t, err)

	got, ok := store.Get(thread.ID)
	assert.True(t, ok)
	assert.Equal(t, thread.ID, got.ID)

	_, ok = store.Get("nonexistent")
	assert.False(t, ok)
}

func TestMemoryStore_Save(t *testing.T) {
	store := NewMemoryStore()
	thread, err := store.Create()
	require.NoError(t, err)

	originalUpdatedAt := thread.UpdatedAt
	time.Sleep(1 * time.Millisecond) // ensure time advances

	// Append a turn and save.
	thread.State.Append(state.RoleUser, artifact.Text{Content: "hello"})
	err = store.Save(thread)
	require.NoError(t, err)

	got, ok := store.Get(thread.ID)
	require.True(t, ok)
	assert.True(t, got.UpdatedAt.After(originalUpdatedAt), "UpdatedAt should advance after Save")
	assert.Len(t, got.State.Turns(), 1)
}

func TestMemoryStore_Delete(t *testing.T) {
	store := NewMemoryStore()
	thread, err := store.Create()
	require.NoError(t, err)

	ok := store.Delete(thread.ID)
	assert.True(t, ok)

	_, ok = store.Get(thread.ID)
	assert.False(t, ok)

	ok = store.Delete(thread.ID)
	assert.False(t, ok)
}

func TestMemoryStore_List(t *testing.T) {
	store := NewMemoryStore()
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

func TestMemoryStore_ConcurrentCreate(t *testing.T) {
	store := NewMemoryStore()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.Create()
			require.NoError(t, err)
		}()
	}
	wg.Wait()
}

func TestMemoryStore_GetBy(t *testing.T) {
	store := NewMemoryStore()
	thread1, err := store.Create()
	require.NoError(t, err)
	_, err = store.Create()
	require.NoError(t, err)

	thread1.SetMetadata("slack.thread_ts", "1234567890.123456")
	err = store.Save(thread1)
	require.NoError(t, err)

	got, ok := store.GetBy("slack.thread_ts", "1234567890.123456")
	require.True(t, ok)
	assert.Equal(t, thread1.ID, got.ID)

	_, ok = store.GetBy("slack.thread_ts", "999")
	assert.False(t, ok)
}

func TestMemoryStore_GetBy_NotFound(t *testing.T) {
	store := NewMemoryStore()
	_, err := store.Create()
	require.NoError(t, err)

	_, ok := store.GetBy("channel_id", "999")
	assert.False(t, ok)
}

func TestMemoryStore_GetBy_Duplicate(t *testing.T) {
	store := NewMemoryStore()
	thread1, err := store.Create()
	require.NoError(t, err)
	thread2, err := store.Create()
	require.NoError(t, err)

	thread1.SetMetadata("channel_id", "same")
	thread2.SetMetadata("channel_id", "same")
	require.NoError(t, store.Save(thread1))
	require.NoError(t, store.Save(thread2))

	got, ok := store.GetBy("channel_id", "same")
	require.True(t, ok)
	assert.True(t, got.ID == thread1.ID || got.ID == thread2.ID)
}

func TestMemoryStore_GetBy_AfterDelete(t *testing.T) {
	store := NewMemoryStore()
	thread, err := store.Create()
	require.NoError(t, err)

	thread.SetMetadata("channel_id", "123")
	require.NoError(t, store.Save(thread))

	ok := store.Delete(thread.ID)
	assert.True(t, ok)

	_, ok = store.GetBy("channel_id", "123")
	assert.False(t, ok)
}

func TestMemoryStore_GetBy_EmptyMetadata(t *testing.T) {
	store := NewMemoryStore()
	_, err := store.Create()
	require.NoError(t, err)

	_, ok := store.GetBy("any_key", "any_value")
	assert.False(t, ok)
}
