package thread

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

func TestThread_Lock(t *testing.T) {
	thread := &Thread{}

	assert.True(t, thread.Lock())
	assert.False(t, thread.Lock(), "second lock should fail")

	thread.Unlock()
	assert.True(t, thread.Lock(), "lock after unlock should succeed")
	thread.Unlock()
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

func TestThread_Lock_HighContention(t *testing.T) {
	thread, err := NewMemoryStore().Create()
	require.NoError(t, err)

	var maxConcurrent int
	var current int
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !thread.Lock() {
				return
			}

			mu.Lock()
			current++
			if current > maxConcurrent {
				maxConcurrent = current
			}
			mu.Unlock()

			// Hold briefly to increase contention window.
			time.Sleep(1 * time.Millisecond)

			mu.Lock()
			current--
			mu.Unlock()
			thread.Unlock()
		}()
	}
	wg.Wait()

	assert.Equal(t, 1, maxConcurrent, "at most one goroutine should hold the lock at any time")
}
