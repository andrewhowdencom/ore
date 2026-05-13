package http

import (
	"sync"
	"testing"

	"github.com/andrewhowdencom/ore/conversation"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSession(t *testing.T) {
	conv, err := conversation.NewMemoryStore().Create()
	require.NoError(t, err)
	step := loop.New()

	session := &Session{
		id:   conv.ID,
		conv: conv,
		step: step,
	}

	assert.Equal(t, conv.ID, session.ID())
	assert.Equal(t, step, session.Step())
	assert.Equal(t, conv.State, session.State())
}

func TestSession_Lock(t *testing.T) {
	conv, err := conversation.NewMemoryStore().Create()
	require.NoError(t, err)
	session := &Session{conv: conv}

	assert.True(t, session.Lock())
	assert.False(t, session.Lock(), "second lock should fail")

	session.Unlock()
	assert.True(t, session.Lock(), "lock after unlock should succeed")
	session.Unlock()
}

func TestSession_Lock_Concurrent(t *testing.T) {
	conv, err := conversation.NewMemoryStore().Create()
	require.NoError(t, err)
	session := &Session{conv: conv}

	var maxConcurrent int
	var current int
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !session.Lock() {
				return
			}

			mu.Lock()
			current++
			if current > maxConcurrent {
				maxConcurrent = current
			}
			mu.Unlock()

			// Hold the lock briefly then release.
			session.Unlock()

			mu.Lock()
			current--
			mu.Unlock()
		}()
	}
	wg.Wait()

	// At most one goroutine should hold the lock at any time.
	assert.Equal(t, 1, maxConcurrent)
}
