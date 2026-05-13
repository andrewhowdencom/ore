package http

import (
	"fmt"
	"sync"
	"testing"

	"github.com/andrewhowdencom/ore/loop"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSessionStore(t *testing.T) {
	store := NewSessionStore()
	require.NotNil(t, store)
	assert.NotNil(t, store.sessions)
}

func TestSessionStore_CreateAndGet(t *testing.T) {
	store := NewSessionStore()
	step := loop.New()

	session, err := store.Create(step)
	require.NoError(t, err)
	require.NotNil(t, session)
	assert.NotEmpty(t, session.id)
	assert.NotNil(t, session.state)
	assert.Equal(t, step, session.step)
	assert.False(t, session.busy)

	got, ok := store.Get(session.id)
	require.True(t, ok)
	assert.Equal(t, session, got)
}

func TestSessionStore_Get_NotFound(t *testing.T) {
	store := NewSessionStore()

	got, ok := store.Get("nonexistent")
	assert.False(t, ok)
	assert.Nil(t, got)
}

func TestSessionStore_Delete(t *testing.T) {
	store := NewSessionStore()
	step := loop.New()

	session, err := store.Create(step)
	require.NoError(t, err)

	ok := store.Delete(session.id)
	assert.True(t, ok)

	_, ok = store.Get(session.id)
	assert.False(t, ok)
}

func TestSessionStore_Delete_NotFound(t *testing.T) {
	store := NewSessionStore()

	ok := store.Delete("nonexistent")
	assert.False(t, ok)
}

func TestSession_Lock(t *testing.T) {
	s := &Session{}

	assert.True(t, s.Lock())
	assert.True(t, s.busy)

	// Second lock should fail.
	assert.False(t, s.Lock())

	s.Unlock()
	assert.False(t, s.busy)

	// Can lock again after unlock.
	assert.True(t, s.Lock())
}

func TestSession_Lock_Concurrent(t *testing.T) {
	s := &Session{}

	var maxConcurrent int
	var current int
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !s.Lock() {
				return
			}

			mu.Lock()
			current++
			if current > maxConcurrent {
				maxConcurrent = current
			}
			mu.Unlock()

			// Hold the lock briefly then release.
			s.Unlock()

			mu.Lock()
			current--
			mu.Unlock()
		}()
	}
	wg.Wait()

	// At most one goroutine should hold the lock at any time.
	assert.Equal(t, 1, maxConcurrent)
}

func TestSessionStore_ConcurrentCreate(t *testing.T) {
	store := NewSessionStore()
	var ids sync.Map
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			step := loop.New()
			session, err := store.Create(step)
			require.NoError(t, err)
			_, loaded := ids.LoadOrStore(session.id, true)
			assert.False(t, loaded, "duplicate session id: %s", session.id)
		}()
	}
	wg.Wait()

	// Verify all sessions are stored.
	count := 0
	ids.Range(func(_, _ any) bool {
		count++
		return true
	})
	assert.Equal(t, 100, count)
}

func TestSessionStore_ConcurrentDelete(t *testing.T) {
	store := NewSessionStore()
	step := loop.New()
	session, err := store.Create(step)
	require.NoError(t, err)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store.Delete(session.id)
		}()
	}
	wg.Wait()

	// Session should be deleted.
	_, ok := store.Get(session.id)
	assert.False(t, ok)
}

func TestGenerateSessionID(t *testing.T) {
	id1, err := generateSessionID()
	require.NoError(t, err)
	assert.Len(t, id1, 32)

	id2, err := generateSessionID()
	require.NoError(t, err)
	assert.NotEqual(t, id1, id2)
}

// failReader is an io.Reader that always returns an error.
type failReader struct{}

func (f *failReader) Read(p []byte) (n int, err error) {
	return 0, fmt.Errorf("random source failure")
}

func TestGenerateSessionID_Error(t *testing.T) {
	old := randRead
	randRead = &failReader{}
	defer func() { randRead = old }()

	_, err := generateSessionID()
	require.Error(t, err)
}

func TestSessionStore_Create_RandFailure(t *testing.T) {
	old := randRead
	randRead = &failReader{}
	defer func() { randRead = old }()

	store := NewSessionStore()
	step := loop.New()
	_, err := store.Create(step)
	require.Error(t, err)
}
