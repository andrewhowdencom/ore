package conversation

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
	conv, err := store.Create()
	require.NoError(t, err)
	assert.NotEmpty(t, conv.ID)
	assert.NotNil(t, conv.State)
	assert.False(t, conv.CreatedAt.IsZero())
	assert.False(t, conv.UpdatedAt.IsZero())

	// Second creation should have a different ID.
	conv2, err := store.Create()
	require.NoError(t, err)
	assert.NotEqual(t, conv.ID, conv2.ID)
}

func TestMemoryStore_Get(t *testing.T) {
	store := NewMemoryStore()
	conv, err := store.Create()
	require.NoError(t, err)

	got, ok := store.Get(conv.ID)
	assert.True(t, ok)
	assert.Equal(t, conv.ID, got.ID)

	_, ok = store.Get("nonexistent")
	assert.False(t, ok)
}

func TestMemoryStore_Save(t *testing.T) {
	store := NewMemoryStore()
	conv, err := store.Create()
	require.NoError(t, err)

	originalUpdatedAt := conv.UpdatedAt
	time.Sleep(1 * time.Millisecond) // ensure time advances

	// Append a turn and save.
	conv.State.Append(state.RoleUser, artifact.Text{Content: "hello"})
	err = store.Save(conv)
	require.NoError(t, err)

	got, ok := store.Get(conv.ID)
	require.True(t, ok)
	assert.True(t, got.UpdatedAt.After(originalUpdatedAt), "UpdatedAt should advance after Save")
	assert.Len(t, got.State.Turns(), 1)
}

func TestMemoryStore_Delete(t *testing.T) {
	store := NewMemoryStore()
	conv, err := store.Create()
	require.NoError(t, err)

	ok := store.Delete(conv.ID)
	assert.True(t, ok)

	_, ok = store.Get(conv.ID)
	assert.False(t, ok)

	ok = store.Delete(conv.ID)
	assert.False(t, ok)
}

func TestConversation_Lock(t *testing.T) {
	conv := &Conversation{}

	assert.True(t, conv.Lock())
	assert.False(t, conv.Lock(), "second lock should fail")

	conv.Unlock()
	assert.True(t, conv.Lock(), "lock after unlock should succeed")
	conv.Unlock()
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
