package session

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/conduit"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/state"
	"github.com/andrewhowdencom/ore/thread"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// drainWithClose starts a goroutine that reads all events from ch, then calls
// closeFn to close the source channel, and waits up to 2s for the drain to
// complete. It fails the test if the drain times out.
func drainWithClose(t *testing.T, ch <-chan loop.OutputEvent, closeFn func()) []loop.OutputEvent {
	t.Helper()
	var events []loop.OutputEvent
	done := make(chan struct{})
	go func() {
		defer close(done)
		for e := range ch {
			events = append(events, e)
		}
	}()
	time.Sleep(50 * time.Millisecond)
	closeFn()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out draining channel")
	}
	return events
}

// mockProvider is a provider.Provider implementation for testing.
type mockProvider struct {
	artifacts []artifact.Artifact
	err       error
}

func (m *mockProvider) Invoke(ctx context.Context, s state.State, ch chan<- artifact.Artifact, opts ...provider.InvokeOption) error {
	for _, art := range m.artifacts {
		select {
		case ch <- art:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return m.err
}

// simpleProcessor runs a single Step.Turn with the mock provider.
func simpleProcessor() TurnProcessor {
	return func(ctx context.Context, step *loop.Step, st state.State, prov provider.Provider) (state.State, error) {
		return step.Turn(ctx, st, prov)
	}
}

// nopProcessor does nothing (used for submit-only tests).
func nopProcessor() TurnProcessor {
	return func(ctx context.Context, step *loop.Step, st state.State, prov provider.Provider) (state.State, error) {
		return st, nil
	}
}

// blockingProvider is a provider that blocks until the context is cancelled.
type blockingProvider struct{}

func (m *blockingProvider) Invoke(ctx context.Context, s state.State, ch chan<- artifact.Artifact, opts ...provider.InvokeOption) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestNewManager(t *testing.T) {
	store := thread.NewMemoryStore()
	prov := &mockProvider{}
	mgr := NewManager(store, prov, func() *loop.Step { return loop.New() }, simpleProcessor())
	require.NotNil(t, mgr)
	assert.Equal(t, store, mgr.Store())
}

func TestManager_Create(t *testing.T) {
	store := thread.NewMemoryStore()
	mgr := NewManager(store, &mockProvider{}, func() *loop.Step { return loop.New() }, simpleProcessor())

	sess, err := mgr.Create()
	require.NoError(t, err)
	require.NotNil(t, sess)
	assert.NotEmpty(t, sess.ID())

	// Thread should exist in store.
	thr, ok := store.Get(sess.ID())
	assert.True(t, ok)
	assert.Equal(t, sess.ID(), thr.ID)

	// Session should be active.
	active := mgr.List()
	require.Len(t, active, 1)
	assert.Equal(t, sess.ID(), active[0].ID())
}

func TestManager_Attach(t *testing.T) {
	store := thread.NewMemoryStore()
	mgr := NewManager(store, &mockProvider{}, func() *loop.Step { return loop.New() }, simpleProcessor())

	// Create a thread directly in the store.
	thr, err := store.Create()
	require.NoError(t, err)

	// Attach should create a new active session for the existing thread.
	sess, err := mgr.Attach(thr.ID)
	require.NoError(t, err)
	assert.Equal(t, thr.ID, sess.ID())

	// Active sessions should include it.
	active := mgr.List()
	require.Len(t, active, 1)
}

func TestManager_Attach_ExistingSession(t *testing.T) {
	store := thread.NewMemoryStore()
	mgr := NewManager(store, &mockProvider{}, func() *loop.Step { return loop.New() }, simpleProcessor())

	// Create a thread and attach once.
	thr, err := store.Create()
	require.NoError(t, err)
	sess1, err := mgr.Attach(thr.ID)
	require.NoError(t, err)

	// Second attach should return the same session, not create a new step.
	sess2, err := mgr.Attach(thr.ID)
	require.NoError(t, err)
	assert.Equal(t, sess1.ID(), sess2.ID())

	// Still only one active session.
	active := mgr.List()
	require.Len(t, active, 1)
}

func TestManager_Attach_ThreadNotFound(t *testing.T) {
	store := thread.NewMemoryStore()
	mgr := NewManager(store, &mockProvider{}, func() *loop.Step { return loop.New() }, simpleProcessor())

	_, err := mgr.Attach("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestManager_Process(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "Hello"},
			artifact.TextDelta{Content: " world"},
		},
	}
	store := thread.NewMemoryStore()
	mgr := NewManager(store, prov, func() *loop.Step { return loop.New() }, simpleProcessor())

	sess, err := mgr.Create()
	require.NoError(t, err)

	// Subscribe to output before processing.
	ch, err := sess.Subscribe("text_delta", "turn_complete")
	require.NoError(t, err)

	// Process a user message.
	err = sess.Process(context.Background(), conduit.UserMessageEvent{Content: "hi"})
	require.NoError(t, err)

	// Collect output events, then close the session to close the channel.
	events := drainWithClose(t, ch, func() { _ = sess.Close() })

	var deltas []artifact.Artifact
	var turnComplete bool
	for _, event := range events {
		switch e := event.(type) {
		case artifact.TextDelta:
			deltas = append(deltas, e)
		case loop.TurnCompleteEvent:
			turnComplete = true
		}
	}

	assert.Len(t, deltas, 2)
	assert.True(t, turnComplete)

	// Thread state should have been saved.
	thr, ok := store.Get(sess.ID())
	require.True(t, ok)
	turns := thr.State.Turns()
	require.Len(t, turns, 2) // user + assistant
	assert.Equal(t, state.RoleUser, turns[0].Role)
	assert.Equal(t, state.RoleAssistant, turns[1].Role)
}

func TestManager_Process_Busy(t *testing.T) {
	store := thread.NewMemoryStore()
	mgr := NewManager(store, &blockingProvider{}, func() *loop.Step { return loop.New() }, simpleProcessor())

	sess, err := mgr.Create()
	require.NoError(t, err)

	// Start a blocking turn in a goroutine.
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		_ = sess.Process(ctx, conduit.UserMessageEvent{Content: "block"})
	}()

	// Wait briefly for the goroutine to acquire the lock.
	time.Sleep(50 * time.Millisecond)

	// Second Process should fail with ErrSessionBusy.
	err = sess.Process(context.Background(), conduit.UserMessageEvent{Content: "concurrent"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSessionBusy)

	// Cancel to let the first goroutine complete.
	cancel()
}

func TestSession_Process_Closed(t *testing.T) {
	store := thread.NewMemoryStore()
	mgr := NewManager(store, &mockProvider{}, func() *loop.Step { return loop.New() }, simpleProcessor())

	sess, err := mgr.Create()
	require.NoError(t, err)
	err = sess.Close()
	require.NoError(t, err)

	err = sess.Process(context.Background(), conduit.UserMessageEvent{Content: "hi"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

type unsupportedEvent struct{}

func (e *unsupportedEvent) Kind() string { return "unsupported" }

func TestManager_Process_UnsupportedEvent(t *testing.T) {
	store := thread.NewMemoryStore()
	mgr := NewManager(store, &mockProvider{}, func() *loop.Step { return loop.New() }, simpleProcessor())

	sess, err := mgr.Create()
	require.NoError(t, err)

	err = sess.Process(context.Background(), &unsupportedEvent{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported event kind")
}

func TestManager_Process_ContextCancel(t *testing.T) {
	store := thread.NewMemoryStore()
	mgr := NewManager(store, &blockingProvider{}, func() *loop.Step { return loop.New() }, simpleProcessor())

	sess, err := mgr.Create()
	require.NoError(t, err)

	ch, err := sess.Subscribe("text_delta", "turn_complete", "error")
	require.NoError(t, err)

	// Start processing with a cancellable context.
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err = sess.Process(ctx, conduit.UserMessageEvent{Content: "cancel me"})
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)

	// Drain the channel. An ErrorEvent may or may not be present because
	// Turn() drops the event when the context is already cancelled.
	// The primary assertion above (Process returns context.Canceled)
	// is the behaviour under test.
	_ = drainWithClose(t, ch, func() { _ = sess.Close() })
}

func TestManager_Process_SaveError(t *testing.T) {
	prov := &mockProvider{}
	store := &errStore{}
	mgr := NewManager(store, prov, func() *loop.Step { return loop.New() }, nopProcessor())

	sess, err := mgr.Create()
	require.NoError(t, err)

	err = sess.Process(context.Background(), conduit.UserMessageEvent{Content: "hi"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "save failed")
}

func TestManager_Cancel(t *testing.T) {
	// Provider that blocks until context is cancelled.
	prov := &mockProvider{}
	store := thread.NewMemoryStore()
	mgr := NewManager(store, prov, func() *loop.Step { return loop.New() }, simpleProcessor())

	sess, err := mgr.Create()
	require.NoError(t, err)

	// Start a blocking turn.
	ctx := context.Background()
	go func() {
		_ = sess.Process(ctx, conduit.UserMessageEvent{Content: "block"})
	}()

	// Wait for lock to be acquired.
	time.Sleep(50 * time.Millisecond)

	// Cancel should abort the ongoing turn.
	err = sess.Cancel()
	require.NoError(t, err)
}

func TestSession_Cancel_Closed(t *testing.T) {
	store := thread.NewMemoryStore()
	mgr := NewManager(store, &mockProvider{}, func() *loop.Step { return loop.New() }, simpleProcessor())

	sess, err := mgr.Create()
	require.NoError(t, err)
	err = sess.Close()
	require.NoError(t, err)

	// Cancel on a closed session should return an error.
	err = sess.Cancel()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

func TestSession_Subscribe_Closed(t *testing.T) {
	store := thread.NewMemoryStore()
	mgr := NewManager(store, &mockProvider{}, func() *loop.Step { return loop.New() }, simpleProcessor())

	sess, err := mgr.Create()
	require.NoError(t, err)
	err = sess.Close()
	require.NoError(t, err)

	// Subscribe on a closed session should return an error.
	_, err = sess.Subscribe("text_delta")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

func TestManager_Close(t *testing.T) {
	store := thread.NewMemoryStore()
	mgr := NewManager(store, &mockProvider{}, func() *loop.Step { return loop.New() }, simpleProcessor())

	sess, err := mgr.Create()
	require.NoError(t, err)

	// Close should remove the session from the active map.
	err = mgr.Close(sess.ID())
	require.NoError(t, err)

	active := mgr.List()
	assert.Empty(t, active)

	// Subscribe should fail after close (via Session-level close).
	_, err = sess.Subscribe("text_delta")
	require.Error(t, err)

	// Thread should still exist in the store.
	_, ok := store.Get(sess.ID())
	assert.True(t, ok)
}

func TestManager_Close_NotFound(t *testing.T) {
	store := thread.NewMemoryStore()
	mgr := NewManager(store, &mockProvider{}, func() *loop.Step { return loop.New() }, simpleProcessor())

	err := mgr.Close("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestManager_List(t *testing.T) {
	store := thread.NewMemoryStore()
	mgr := NewManager(store, &mockProvider{}, func() *loop.Step { return loop.New() }, simpleProcessor())

	// Empty initially.
	assert.Empty(t, mgr.List())

	sess1, err := mgr.Create()
	require.NoError(t, err)
	sess2, err := mgr.Create()
	require.NoError(t, err)

	active := mgr.List()
	require.Len(t, active, 2)
	ids := make([]string, len(active))
	for i, s := range active {
		ids[i] = s.ID()
	}
	assert.Contains(t, ids, sess1.ID())
	assert.Contains(t, ids, sess2.ID())
}

func TestManager_Lock_Concurrent(t *testing.T) {
	// Use a processor that sleeps briefly to extend the lock window.
	sleepyProcessor := func() TurnProcessor {
		return func(ctx context.Context, step *loop.Step, st state.State, prov provider.Provider) (state.State, error) {
			time.Sleep(100 * time.Millisecond)
			return st, nil
		}
	}

	store := thread.NewMemoryStore()
	mgr := NewManager(store, &mockProvider{}, func() *loop.Step { return loop.New() }, sleepyProcessor())

	sess, err := mgr.Create()
	require.NoError(t, err)

	var maxConcurrent int
	var current int
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := sess.Process(context.Background(), conduit.UserMessageEvent{Content: " concurrent"})
			if errors.Is(err, ErrSessionBusy) {
				return
			}
			if err != nil {
				return // ignore other errors
			}

			mu.Lock()
			current++
			if current > maxConcurrent {
				maxConcurrent = current
			}
			mu.Unlock()

			time.Sleep(10 * time.Millisecond)

			mu.Lock()
			current--
			mu.Unlock()
		}()
	}
	wg.Wait()

	// At most one goroutine should successfully hold the lock at any time.
	assert.Equal(t, 1, maxConcurrent)
}

func TestManager_Get_NotFound(t *testing.T) {
	mgr := NewManager(thread.NewMemoryStore(), nil, nil, nil)
	_, err := mgr.Get("missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestManager_Check_NotFound(t *testing.T) {
	mgr := NewManager(thread.NewMemoryStore(), nil, nil, nil)
	err := mgr.Check("missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// errStore is a Store that always returns an error from Save.
type errStore struct{}

func (e *errStore) Create() (*thread.Thread, error)                { return thread.NewMemoryStore().Create() }
func (e *errStore) Get(id string) (*thread.Thread, bool)            { return thread.NewMemoryStore().Get(id) }
func (e *errStore) Save(*thread.Thread) error                        { return fmt.Errorf("save failed") }
func (e *errStore) Delete(string) bool                              { return false }
func (e *errStore) List() ([]*thread.Thread, error)                  { return nil, nil }
