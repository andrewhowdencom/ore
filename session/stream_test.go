package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/state"
	"github.com/andrewhowdencom/ore/thread"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStream_Interface(t *testing.T) {
	store := thread.NewMemoryStore()
	prov := &mockProvider{}
	mgr := NewManager(store, prov, func(*thread.Thread) (*loop.Step, error) { return loop.New(), nil }, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)
	require.NotNil(t, stream)
	assert.NotEmpty(t, stream.ID())

	// Verify all Stream methods are callable.
	ch := stream.Subscribe("text_delta", "turn_complete")
	require.NotNil(t, ch)

	err = stream.Process(context.Background(), UserMessageEvent{Content: "hi"})
	require.NoError(t, err)

	err = stream.Cancel()
	require.NoError(t, err)

	err = stream.Close()
	require.NoError(t, err)

	// After close, Subscribe should return a closed channel.
	ch = stream.Subscribe("text_delta")
	_, ok := <-ch
	require.False(t, ok, "channel should be closed")

	// Thread should still exist in the store.
	_, ok = store.Get(stream.ID())
	assert.True(t, ok)
}

func TestStream_Process_ContextPropagation(t *testing.T) {
	store := thread.NewMemoryStore()
	prov := &mockProvider{}
	mgr := NewManager(store, prov, func(*thread.Thread) (*loop.Step, error) { return loop.New(), nil }, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	ch := stream.Subscribe("turn_complete")

	err = stream.Process(context.Background(), UserMessageEvent{Content: "hi", Ctx: loop.EventContext{Provenance: "test-provenance"}})
	require.NoError(t, err)

	events := drainWithClose(t, ch, func() { _ = stream.Close() })

	require.Len(t, events, 2) // user turn + assistant turn
	tc, ok := events[0].(loop.TurnCompleteEvent)
	require.True(t, ok)
	assert.Equal(t, "test-provenance", tc.Ctx.Provenance)
	tc, ok = events[1].(loop.TurnCompleteEvent)
	require.True(t, ok)
	assert.Equal(t, "test-provenance", tc.Ctx.Provenance)
}

func TestStream_ContextClearedBetweenProcesses(t *testing.T) {
	store := thread.NewMemoryStore()
	prov := &mockProvider{}
	mgr := NewManager(store, prov, func(*thread.Thread) (*loop.Step, error) { return loop.New(), nil }, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	// First process with provenance
	ch1 := stream.Subscribe("turn_complete")
	err = stream.Process(context.Background(), UserMessageEvent{Content: "first", Ctx: loop.EventContext{Provenance: "first-provenance"}})
	require.NoError(t, err)

	var events1 []loop.OutputEvent
	for i := 0; i < 2; i++ {
		select {
		case event := <-ch1:
			events1 = append(events1, event)
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("timeout waiting for event %d", i)
		}
	}
	require.Len(t, events1, 2)
	assert.Equal(t, "first-provenance", events1[0].(loop.TurnCompleteEvent).Ctx.Provenance)
	assert.Equal(t, "first-provenance", events1[1].(loop.TurnCompleteEvent).Ctx.Provenance)

	// Second process without provenance — context should be cleared
	ch2 := stream.Subscribe("turn_complete")
	err = stream.Process(context.Background(), UserMessageEvent{Content: "second"})
	require.NoError(t, err)

	var events2 []loop.OutputEvent
	for i := 0; i < 2; i++ {
		select {
		case event := <-ch2:
			events2 = append(events2, event)
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("timeout waiting for event %d", i)
		}
	}
	require.Len(t, events2, 2)
	assert.Empty(t, events2[0].(loop.TurnCompleteEvent).Ctx.Provenance)
	assert.Empty(t, events2[1].(loop.TurnCompleteEvent).Ctx.Provenance)
}

func TestStream_InterruptEvent_ContextPropagation(t *testing.T) {
	store := thread.NewMemoryStore()
	prov := &mockProvider{}
	mgr := NewManager(store, prov, func(*thread.Thread) (*loop.Step, error) { return loop.New(), nil }, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	// Process an interrupt with provenance — sets then clears context
	err = stream.Process(context.Background(), InterruptEvent{Ctx: loop.EventContext{Provenance: "interrupt-provenance"}})
	require.NoError(t, err)

	// Subsequent process without provenance should have empty context
	ch := stream.Subscribe("turn_complete")
	err = stream.Process(context.Background(), UserMessageEvent{Content: "after interrupt"})
	require.NoError(t, err)

	var events []loop.OutputEvent
	for i := 0; i < 2; i++ {
		select {
		case event := <-ch:
			events = append(events, event)
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("timeout waiting for event %d", i)
		}
	}
	require.Len(t, events, 2)
	assert.Empty(t, events[0].(loop.TurnCompleteEvent).Ctx.Provenance)
	assert.Empty(t, events[1].(loop.TurnCompleteEvent).Ctx.Provenance)
}

// testCustomEvent is a test-only OutputEvent for verifying Stream.Emit().
type testCustomEvent struct {
	Value string
	Ctx   loop.EventContext
}

func (e testCustomEvent) Kind() string          { return "test_custom" }
func (e testCustomEvent) Context() loop.EventContext { return e.Ctx }

func TestStream_Emit_DeliversToSubscribers(t *testing.T) {
	store := thread.NewMemoryStore()
	prov := &mockProvider{}
	mgr := NewManager(store, prov, func(*thread.Thread) (*loop.Step, error) { return loop.New(), nil }, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	ch := stream.Subscribe("test_custom")

	err = stream.Emit(context.Background(), testCustomEvent{Value: "hello", Ctx: loop.EventContext{Provenance: "emit-test"}})
	require.NoError(t, err)

	select {
	case event := <-ch:
		custom, ok := event.(testCustomEvent)
		require.True(t, ok)
		assert.Equal(t, "hello", custom.Value)
		assert.Equal(t, "emit-test", custom.Ctx.Provenance)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for custom event")
	}

	_ = stream.Close()
}

func TestStream_Emit_ClosedReturnsError(t *testing.T) {
	store := thread.NewMemoryStore()
	prov := &mockProvider{}
	mgr := NewManager(store, prov, func(*thread.Thread) (*loop.Step, error) { return loop.New(), nil }, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	err = stream.Close()
	require.NoError(t, err)

	err = stream.Emit(context.Background(), testCustomEvent{Value: "should-fail"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is closed")
}

func TestStream_Emit_AllowedWhileBusy(t *testing.T) {
	store := thread.NewMemoryStore()
	prov := &blockingProvider{}
	mgr := NewManager(store, prov, func(*thread.Thread) (*loop.Step, error) { return loop.New(), nil }, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	ch := stream.Subscribe("test_custom")

	// Start processing — this will block on the provider.
	done := make(chan error)
	go func() {
		done <- stream.Process(context.Background(), UserMessageEvent{Content: "hi"})
	}()

	// Wait for Process to acquire the busy lock.
	time.Sleep(50 * time.Millisecond)

	// Emit should succeed even though the stream is busy.
	err = stream.Emit(context.Background(), testCustomEvent{Value: "during-turn"})
	require.NoError(t, err)

	// The custom event should be delivered through the subscription.
	select {
	case event := <-ch:
		custom, ok := event.(testCustomEvent)
		require.True(t, ok)
		assert.Equal(t, "during-turn", custom.Value)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for custom event")
	}

	// Cancel to unblock Process.
	_ = stream.Cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for Process to return")
	}

	_ = stream.Close()
}

func TestStream_Process_EmitsProcessCompleteEvent(t *testing.T) {
	store := thread.NewMemoryStore()
	prov := &mockProvider{}
	mgr := NewManager(store, prov, func(*thread.Thread) (*loop.Step, error) { return loop.New(), nil }, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	ch := stream.Subscribe("process_complete")
	err = stream.Process(context.Background(), UserMessageEvent{Content: "hi"})
	require.NoError(t, err)

	select {
	case event := <-ch:
		pce, ok := event.(loop.ProcessCompleteEvent)
		require.True(t, ok)
		assert.Nil(t, pce.Err)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for ProcessCompleteEvent")
	}

	_ = stream.Close()
}

func TestStream_Process_EmitsProcessCompleteEvent_WithError(t *testing.T) {
	store := thread.NewMemoryStore()
	mgr := NewManager(store, &mockProvider{}, func(*thread.Thread) (*loop.Step, error) { return loop.New(), nil }, func(ctx context.Context, step *loop.Step, st state.State, prov provider.Provider) (state.State, error) {
		return st, errors.New("processor failed")
	})

	stream, err := mgr.Create()
	require.NoError(t, err)

	ch := stream.Subscribe("process_complete")
	err = stream.Process(context.Background(), UserMessageEvent{Content: "hi"})
	require.Error(t, err)

	select {
	case event := <-ch:
		pce, ok := event.(loop.ProcessCompleteEvent)
		require.True(t, ok)
		assert.NotNil(t, pce.Err)
		assert.Contains(t, pce.Err.Error(), "processor failed")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for ProcessCompleteEvent")
	}

	_ = stream.Close()
}

// saveErrStore is a thread.Store whose Save always returns an error.
type saveErrStore struct {
	inner thread.Store
}

func (s *saveErrStore) Create() (*thread.Thread, error)                    { return s.inner.Create() }
func (s *saveErrStore) Get(id string) (*thread.Thread, bool)               { return s.inner.Get(id) }
func (s *saveErrStore) GetBy(key, value string) (*thread.Thread, bool)    { return s.inner.GetBy(key, value) }
func (s *saveErrStore) Save(*thread.Thread) error                         { return errors.New("save failed") }
func (s *saveErrStore) Delete(id string) bool                             { return s.inner.Delete(id) }
func (s *saveErrStore) List() ([]*thread.Thread, error)                   { return s.inner.List() }

func TestStream_Process_EmitsProcessCompleteEvent_WithSaveError(t *testing.T) {
	store := &saveErrStore{inner: thread.NewMemoryStore()}
	prov := &mockProvider{}
	mgr := NewManager(store, prov, func(*thread.Thread) (*loop.Step, error) { return loop.New(), nil }, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	ch := stream.Subscribe("process_complete")
	err = stream.Process(context.Background(), UserMessageEvent{Content: "hi"})
	require.Error(t, err)

	select {
	case event := <-ch:
		pce, ok := event.(loop.ProcessCompleteEvent)
		require.True(t, ok)
		assert.NotNil(t, pce.Err)
		assert.Contains(t, pce.Err.Error(), "save")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for ProcessCompleteEvent")
	}

	_ = stream.Close()
}

func TestStream_Process_EmitsProcessCompleteEvent_PropagatesProvenance(t *testing.T) {
	store := thread.NewMemoryStore()
	prov := &mockProvider{}
	mgr := NewManager(store, prov, func(*thread.Thread) (*loop.Step, error) { return loop.New(), nil }, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	ch := stream.Subscribe("process_complete")
	err = stream.Process(context.Background(), UserMessageEvent{Content: "hi", Ctx: loop.EventContext{Provenance: "test-provenance"}})
	require.NoError(t, err)

	select {
	case event := <-ch:
		pce, ok := event.(loop.ProcessCompleteEvent)
		require.True(t, ok)
		assert.Equal(t, "test-provenance", pce.Ctx.Provenance)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for ProcessCompleteEvent")
	}

	_ = stream.Close()
}

func TestStream_Process_EmitsSingleProcessCompleteEvent_ForMultiTurn(t *testing.T) {
	store := thread.NewMemoryStore()
	prov := &mockProvider{}
	mgr := NewManager(store, prov, func(*thread.Thread) (*loop.Step, error) { return loop.New(), nil }, func(ctx context.Context, step *loop.Step, st state.State, prov provider.Provider) (state.State, error) {
		st, err := step.Turn(ctx, st, prov)
		if err != nil {
			return st, err
		}
		return step.Turn(ctx, st, prov)
	})

	stream, err := mgr.Create()
	require.NoError(t, err)

	turnCh := stream.Subscribe("turn_complete")
	procCh := stream.Subscribe("process_complete")

	err = stream.Process(context.Background(), UserMessageEvent{Content: "hi"})
	require.NoError(t, err)

	// Should receive exactly one ProcessCompleteEvent
	select {
	case event := <-procCh:
		pce, ok := event.(loop.ProcessCompleteEvent)
		require.True(t, ok)
		assert.Nil(t, pce.Err)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for ProcessCompleteEvent")
	}

	// Should NOT receive a second ProcessCompleteEvent
	select {
	case <-procCh:
		t.Fatal("expected exactly one ProcessCompleteEvent, got a second")
	case <-time.After(50 * time.Millisecond):
		// Expected - no second event
	}

	// Close stream and drain turn_complete events
	_ = stream.Close()
	turnCount := 0
	for range turnCh {
		turnCount++
	}
	assert.GreaterOrEqual(t, turnCount, 3, "expected at least 3 turn_complete events (user + 2 assistant turns)")
}
