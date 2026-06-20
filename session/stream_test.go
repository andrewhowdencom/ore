package session

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/models"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStream_Interface(t *testing.T) {
	store := NewMemoryStore()
	prov := &mockProvider{}
	mgr := NewManager(store, prov, func(*Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())

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
	_, err = store.Get(stream.ID())
	assert.NoError(t, err)
}

func TestStream_Turns(t *testing.T) {
	store := NewMemoryStore()
	prov := &mockProvider{}
	mgr := NewManager(store, prov, func(*Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	// Fresh stream has no turns.
	assert.Empty(t, stream.Turns())

	// Process a user message + assistant response.
	err = stream.Process(context.Background(), UserMessageEvent{Content: "hello"})
	require.NoError(t, err)

	turns := stream.Turns()
	require.Len(t, turns, 2)
	assert.Equal(t, state.RoleUser, turns[0].Role)
	assert.Equal(t, "hello", turns[0].Artifacts[0].(artifact.Text).Content)
	assert.Equal(t, state.RoleAssistant, turns[1].Role)

	// Modifying the returned slice must not affect the stream.
	turns[0].Role = state.RoleSystem
	_ = append(turns, state.Turn{Role: state.RoleSystem})
	freshTurns := stream.Turns()
	require.Len(t, freshTurns, 2)
	assert.Equal(t, state.RoleUser, freshTurns[0].Role)

	_ = stream.Close()
}

func TestStream_LoadTurns(t *testing.T) {
	store := NewMemoryStore()
	prov := &mockProvider{}
	mgr := NewManager(store, prov, func(*Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	// Process a user message to get initial turns.
	err = stream.Process(context.Background(), UserMessageEvent{Content: "hello"})
	require.NoError(t, err)

	initialTurns := stream.Turns()
	require.Len(t, initialTurns, 2)

	// Replace the turns with a single synthetic turn.
	replacement := []state.Turn{
		{Role: state.RoleUser, Artifacts: []artifact.Artifact{artifact.Text{Content: "replaced"}}},
	}
	stream.LoadTurns(replacement)

	got := stream.Turns()
	require.Len(t, got, 1)
	assert.Equal(t, state.RoleUser, got[0].Role)
	assert.Equal(t, "replaced", got[0].Artifacts[0].(artifact.Text).Content)

	_ = stream.Close()
}

func TestStream_AppendTurn_AppendsToState(t *testing.T) {
	store := NewMemoryStore()
	prov := &mockProvider{}
	mgr := NewManager(store, prov, func(*Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	// Pre-existing turns from a Process call.
	err = stream.Process(context.Background(), UserMessageEvent{Content: "hello"})
	require.NoError(t, err)
	require.Len(t, stream.Turns(), 2)

	// Append a synthetic RoleSystem compaction turn.
	err = stream.AppendTurn(context.Background(),
		state.RoleSystem,
		artifact.Text{Content: "summary"},
		artifact.Compaction{Strategy: "summarize"},
	)
	require.NoError(t, err)

	got := stream.Turns()
	require.Len(t, got, 3, "AppendTurn appends to the existing buffer")

	// The appended turn is at the end.
	assert.Equal(t, state.RoleSystem, got[2].Role)
	require.Len(t, got[2].Artifacts, 2)
	assert.Equal(t, "summary", got[2].Artifacts[0].(artifact.Text).Content)
	_, isCompaction := got[2].Artifacts[1].(artifact.Compaction)
	assert.True(t, isCompaction)
	assert.False(t, got[2].Timestamp.IsZero())

	_ = stream.Close()
}

func TestStream_AppendTurn_BroadcastsTurnComplete(t *testing.T) {
	store := NewMemoryStore()
	prov := &mockProvider{}
	mgr := NewManager(store, prov, func(*Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	ch := stream.Subscribe("turn_complete")

	err = stream.AppendTurn(
		loop.WithProvenance(context.Background(), "external"),
		state.RoleSystem,
		artifact.Text{Content: "summary"},
	)
	require.NoError(t, err)

	select {
	case ev := <-ch:
		tc, ok := ev.(loop.TurnCompleteEvent)
		require.True(t, ok, "expected TurnCompleteEvent, got %T", ev)
		assert.Equal(t, state.RoleSystem, tc.Turn.Role)
		require.Len(t, tc.Turn.Artifacts, 1)
		assert.Equal(t, "summary", tc.Turn.Artifacts[0].(artifact.Text).Content)
		assert.Equal(t, "external", provenance(tc.Ctx))
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for TurnCompleteEvent")
	}

	_ = stream.Close()
}

func TestStream_AppendTurn_ClosedReturnsError(t *testing.T) {
	store := NewMemoryStore()
	prov := &mockProvider{}
	mgr := NewManager(store, prov, func(*Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	require.NoError(t, stream.Close())

	err = stream.AppendTurn(context.Background(), state.RoleSystem, artifact.Text{Content: "after close"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

func TestStream_AppendTurn_NoSubscribers_NoError(t *testing.T) {
	// AppendTurn must not block or fail when there are no live
	// subscribers. The event still flows through the event bus; it
	// just has no listeners.
	store := NewMemoryStore()
	prov := &mockProvider{}
	mgr := NewManager(store, prov, func(*Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	// No subscription — AppendTurn returns nil and the state is updated.
	err = stream.AppendTurn(context.Background(), state.RoleSystem, artifact.Text{Content: "no listeners"})
	require.NoError(t, err)

	require.Len(t, stream.Turns(), 1)
	assert.Equal(t, "no listeners", stream.Turns()[0].Artifacts[0].(artifact.Text).Content)

	_ = stream.Close()
}

func TestStream_AllMetadata(t *testing.T) {
	t.Run("returns empty map for a fresh stream", func(t *testing.T) {
		store := NewMemoryStore()
		prov := &mockProvider{}
		mgr := NewManager(store, prov, func(*Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())

		stream, err := mgr.Create()
		require.NoError(t, err)
		t.Cleanup(func() { _ = stream.Close() })

		meta := stream.AllMetadata()
		require.NotNil(t, meta, "AllMetadata must return a non-nil map for safe iteration")
		assert.Empty(t, meta)
	})

	t.Run("returns seeded keys after SetMetadata", func(t *testing.T) {
		store := NewMemoryStore()
		prov := &mockProvider{}
		mgr := NewManager(store, prov, func(*Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())

		stream, err := mgr.Create()
		require.NoError(t, err)
		t.Cleanup(func() { _ = stream.Close() })

		stream.SetMetadata("thread_id", "abc")
		stream.SetMetadata("cwd", "/tmp")
		stream.SetMetadata("role", "reviewer")

		meta := stream.AllMetadata()
		assert.Equal(t, "abc", meta["thread_id"])
		assert.Equal(t, "/tmp", meta["cwd"])
		assert.Equal(t, "reviewer", meta["role"])
		assert.Len(t, meta, 3)
	})

	t.Run("returns a defensive copy that does not alias the thread's map", func(t *testing.T) {
		store := NewMemoryStore()
		prov := &mockProvider{}
		mgr := NewManager(store, prov, func(*Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())

		stream, err := mgr.Create()
		require.NoError(t, err)
		t.Cleanup(func() { _ = stream.Close() })

		stream.SetMetadata("thread_id", "abc")
		stream.SetMetadata("role", "reviewer")

		// Mutate the returned map in every way a caller could.
		meta := stream.AllMetadata()
		meta["injected"] = "evil"
		delete(meta, "thread_id")
		meta["role"] = "tampered"

		// Re-read; the stream's metadata must be unaffected. This also
		// proves the returned map is a fresh reference (aliasing would
		// have shown the mutations on re-read).
		fresh := stream.AllMetadata()
		assert.Len(t, fresh, 2, "the thread's metadata must be unchanged after mutating the returned map")
		assert.Equal(t, "abc", fresh["thread_id"])
		assert.Equal(t, "reviewer", fresh["role"])
		_, leaked := fresh["injected"]
		assert.False(t, leaked, "the injected key must not leak into the thread's metadata")
	})
}

func TestStream_Process_ContextPropagation(t *testing.T) {
	store := NewMemoryStore()
	prov := &mockProvider{}
	mgr := NewManager(store, prov, func(*Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	ch := stream.Subscribe("turn_complete")

	err = stream.Process(context.Background(), UserMessageEvent{Content: "hi", Ctx: loop.WithProvenance(context.Background(), "test-provenance")})
	require.NoError(t, err)

	events := drainWithClose(t, ch, func() { _ = stream.Close() })

	require.Len(t, events, 2) // user turn + assistant turn
	tc, ok := events[0].(loop.TurnCompleteEvent)
	require.True(t, ok)
	assert.Equal(t, "test-provenance", provenance(tc.Ctx))
	tc, ok = events[1].(loop.TurnCompleteEvent)
	require.True(t, ok)
	assert.Equal(t, "test-provenance", provenance(tc.Ctx))
}

func TestStream_ContextClearedBetweenProcesses(t *testing.T) {
	store := NewMemoryStore()
	prov := &mockProvider{}
	mgr := NewManager(store, prov, func(*Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	// First process with provenance
	ch1 := stream.Subscribe("turn_complete")
	err = stream.Process(context.Background(), UserMessageEvent{Content: "first", Ctx: loop.WithProvenance(context.Background(), "first-provenance")})
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
	assert.Equal(t, "first-provenance", provenance(events1[0].(loop.TurnCompleteEvent).Ctx))
	assert.Equal(t, "first-provenance", provenance(events1[1].(loop.TurnCompleteEvent).Ctx))

	// Second process without provenance — context should be cleared.
	// The new subscriber only receives live events (no replay buffer).
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
	// Both events are live from the second process with cleared context.
	assert.Empty(t, provenance(events2[0].(loop.TurnCompleteEvent).Ctx))
	assert.Empty(t, provenance(events2[1].(loop.TurnCompleteEvent).Ctx))
}

func TestStream_InterruptEvent_ContextPropagation(t *testing.T) {
	store := NewMemoryStore()
	prov := &mockProvider{}
	mgr := NewManager(store, prov, func(*Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	// Process an interrupt with provenance — sets then clears context
	err = stream.Process(context.Background(), InterruptEvent{Ctx: loop.WithProvenance(context.Background(), "interrupt-provenance")})
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
	assert.Empty(t, provenance(events[0].(loop.TurnCompleteEvent).Ctx))
	assert.Empty(t, provenance(events[1].(loop.TurnCompleteEvent).Ctx))
}

// testCustomEvent is a test-only OutputEvent for verifying Stream.Emit().
type testCustomEvent struct {
	Value string
	Ctx   context.Context
}

func (e testCustomEvent) Kind() string             { return "test_custom" }
func (e testCustomEvent) Context() context.Context { return e.Ctx }

func TestStream_Emit_DeliversToSubscribers(t *testing.T) {
	store := NewMemoryStore()
	prov := &mockProvider{}
	mgr := NewManager(store, prov, func(*Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	ch := stream.Subscribe("test_custom")

	err = stream.Emit(context.Background(), testCustomEvent{Value: "hello", Ctx: loop.WithProvenance(context.Background(), "emit-test")})
	require.NoError(t, err)

	select {
	case event := <-ch:
		custom, ok := event.(testCustomEvent)
		require.True(t, ok)
		assert.Equal(t, "hello", custom.Value)
		assert.Equal(t, "emit-test", provenance(custom.Ctx))
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for custom event")
	}

	_ = stream.Close()
}

func TestStream_Emit_ClosedReturnsError(t *testing.T) {
	store := NewMemoryStore()
	prov := &mockProvider{}
	mgr := NewManager(store, prov, func(*Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	err = stream.Close()
	require.NoError(t, err)

	err = stream.Emit(context.Background(), testCustomEvent{Value: "should-fail"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is closed")
}

func TestStream_Emit_AllowedWhileBusy(t *testing.T) {
	store := NewMemoryStore()
	prov := &blockingProvider{}
	mgr := NewManager(store, prov, func(*Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())

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

func TestStream_Process_EmitsLifecycleEvent(t *testing.T) {
	store := NewMemoryStore()
	prov := &mockProvider{}
	mgr := NewManager(store, prov, func(*Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	ch := stream.Subscribe("lifecycle")
	err = stream.Process(context.Background(), UserMessageEvent{Content: "hi"})
	require.NoError(t, err)

	var le loop.LifecycleEvent
	found := false
	timeout := time.After(100 * time.Millisecond)
	for !found {
		select {
		case event := <-ch:
			if e, ok := event.(loop.LifecycleEvent); ok {
				if e.Phase == "done" {
					le = e
					found = true
				}
			}
		case <-timeout:
			t.Fatal("timeout waiting for LifecycleEvent with phase 'done'")
		}
	}
	require.True(t, found)
	assert.Equal(t, "done", le.Phase)

	_ = stream.Close()
}

func TestStream_Process_EmitsLifecycleEvent_WithError(t *testing.T) {
	store := NewMemoryStore()
	mgr := NewManager(store, &mockProvider{}, func(*Stream) ([]loop.Option, error) { return nil, nil }, func(ctx context.Context, step *loop.Step, st state.State, prov provider.Provider, _ models.Spec) (state.State, error) {
		return st, errors.New("processor failed")
	})

	stream, err := mgr.Create()
	require.NoError(t, err)

	ch := stream.Subscribe("lifecycle", "error")
	err = stream.Process(context.Background(), UserMessageEvent{Content: "hi"})
	require.Error(t, err)

	var ee loop.ErrorEvent
	foundError := false
	timeout := time.After(100 * time.Millisecond)
	for !foundError {
		select {
		case event := <-ch:
			if e, ok := event.(loop.ErrorEvent); ok {
				ee = e
				foundError = true
			}
		case <-timeout:
			t.Fatal("timeout waiting for ErrorEvent")
		}
	}
	require.True(t, foundError)
	assert.NotNil(t, ee.Err)
	assert.Contains(t, ee.Err.Error(), "processor failed")

	var le loop.LifecycleEvent
	foundDone := false
	timeout = time.After(100 * time.Millisecond)
	for !foundDone {
		select {
		case event := <-ch:
			if e, ok := event.(loop.LifecycleEvent); ok {
				if e.Phase == "done" {
					le = e
					foundDone = true
				}
			}
		case <-timeout:
			t.Fatal("timeout waiting for LifecycleEvent with phase 'done'")
		}
	}
	require.True(t, foundDone)
	assert.Equal(t, "done", le.Phase)

	_ = stream.Close()
}

// saveErrStore is a Store whose Save always returns an error.
type saveErrStore struct {
	inner Store
}

func (s *saveErrStore) Create() (*Thread, error)                 { return s.inner.Create() }
func (s *saveErrStore) Get(id string) (*Thread, error)            { return s.inner.Get(id) }
func (s *saveErrStore) GetBy(key, value string) (*Thread, error) { return s.inner.GetBy(key, value) }
func (s *saveErrStore) Save(*Thread) error                      { return errors.New("save failed") }
func (s *saveErrStore) Delete(id string) bool                   { return s.inner.Delete(id) }
func (s *saveErrStore) List() ([]*Thread, error)                { return s.inner.List() }

func TestStream_Process_EmitsLifecycleEvent_WithSaveError(t *testing.T) {
	store := &saveErrStore{inner: NewMemoryStore()}
	prov := &mockProvider{}
	mgr := NewManager(store, prov, func(*Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	ch := stream.Subscribe("lifecycle", "error")
	err = stream.Process(context.Background(), UserMessageEvent{Content: "hi"})
	require.Error(t, err)

	var ee loop.ErrorEvent
	foundError := false
	timeout := time.After(100 * time.Millisecond)
	for !foundError {
		select {
		case event := <-ch:
			if e, ok := event.(loop.ErrorEvent); ok {
				ee = e
				foundError = true
			}
		case <-timeout:
			t.Fatal("timeout waiting for ErrorEvent")
		}
	}
	require.True(t, foundError)
	assert.NotNil(t, ee.Err)
	assert.Contains(t, ee.Err.Error(), "save")

	var le loop.LifecycleEvent
	foundDone := false
	timeout = time.After(100 * time.Millisecond)
	for !foundDone {
		select {
		case event := <-ch:
			if e, ok := event.(loop.LifecycleEvent); ok {
				if e.Phase == "done" {
					le = e
					foundDone = true
				}
			}
		case <-timeout:
			t.Fatal("timeout waiting for LifecycleEvent with phase 'done'")
		}
	}
	require.True(t, foundDone)
	assert.Equal(t, "done", le.Phase)

	_ = stream.Close()
}

func TestStream_Process_EmitsLifecycleEvent_PropagatesProvenance(t *testing.T) {
	store := NewMemoryStore()
	prov := &mockProvider{}
	mgr := NewManager(store, prov, func(*Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	ch := stream.Subscribe("lifecycle")
	err = stream.Process(context.Background(), UserMessageEvent{Content: "hi", Ctx: loop.WithProvenance(context.Background(), "test-provenance")})
	require.NoError(t, err)

	var le loop.LifecycleEvent
	found := false
	timeout := time.After(100 * time.Millisecond)
	for !found {
		select {
		case event := <-ch:
			if e, ok := event.(loop.LifecycleEvent); ok {
				if e.Phase == "done" {
					le = e
					found = true
				}
			}
		case <-timeout:
			t.Fatal("timeout waiting for LifecycleEvent with phase 'done'")
		}
	}
	require.True(t, found)
	assert.Equal(t, "test-provenance", provenance(le.Ctx))

	_ = stream.Close()
}

func TestStream_Process_EmitsSingleLifecycleEvent_ForMultiTurn(t *testing.T) {
	store := NewMemoryStore()
	prov := &mockProvider{}
	mgr := NewManager(store, prov, func(*Stream) ([]loop.Option, error) { return nil, nil }, func(ctx context.Context, step *loop.Step, st state.State, prov provider.Provider, _ models.Spec) (state.State, error) {
		spec := models.Spec{Name: "test-model"}
		st, err := step.Turn(ctx, st, spec, prov)
		if err != nil {
			return st, err
		}
		return step.Turn(ctx, st, spec, prov)
	})

	stream, err := mgr.Create()
	require.NoError(t, err)

	turnCh := stream.Subscribe("turn_complete")
	procCh := stream.Subscribe("lifecycle")

	err = stream.Process(context.Background(), UserMessageEvent{Content: "hi"})
	require.NoError(t, err)

	// Close stream and collect all lifecycle events
	_ = stream.Close()
	var lifecycleEvents []loop.LifecycleEvent
	for event := range procCh {
		if le, ok := event.(loop.LifecycleEvent); ok {
			lifecycleEvents = append(lifecycleEvents, le)
		}
	}

	// Verify at least one lifecycle event was received
	require.GreaterOrEqual(t, len(lifecycleEvents), 1, "expected at least 1 lifecycle event")

	// Verify at least one "done" event exists
	var foundDone bool
	for _, le := range lifecycleEvents {
		if le.Phase == "done" {
			foundDone = true
			break
		}
	}
	require.True(t, foundDone, "expected at least one 'done' lifecycle event")

	// Drain turn_complete events
	turnCount := 0
	for range turnCh {
		turnCount++
	}
	assert.GreaterOrEqual(t, turnCount, 3, "expected at least 3 turn_complete events (user + 2 assistant turns)")
}

func TestStream_Submit_NonBlocking(t *testing.T) {
	store := NewMemoryStore()
	sleepyProcessor := func() TurnProcessor {
		return func(ctx context.Context, step *loop.Step, st state.State, prov provider.Provider, _ models.Spec) (state.State, error) {
			time.Sleep(50 * time.Millisecond)
			return st, nil
		}
	}
	mgr := NewManager(store, &mockProvider{}, func(stream *Stream) ([]loop.Option, error) {
		return nil, nil
	}, sleepyProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	ch := stream.Subscribe("lifecycle")

	start := time.Now()
	err = stream.Submit(UserMessageEvent{Content: "hello"})
	elapsed := time.Since(start)
	require.NoError(t, err)
	assert.Less(t, elapsed, 10*time.Millisecond, "Submit should return immediately")

	// Wait for the event to be processed.
	select {
	case <-ch:
		// ok
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for lifecycle")
	}

	_ = stream.Close()
}

func TestStream_Submit_FIFOOrder(t *testing.T) {
	store := NewMemoryStore()
	mgr := NewManager(store, &mockProvider{}, func(stream *Stream) ([]loop.Option, error) {
		return nil, nil
	}, nopProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	require.NoError(t, stream.Submit(UserMessageEvent{Content: "first"}))
	require.NoError(t, stream.Submit(UserMessageEvent{Content: "second"}))
	require.NoError(t, stream.Submit(UserMessageEvent{Content: "third"}))

	// Flush the queue with a synchronous Process to ensure all prior
	// Submits have completed before inspecting state.
	require.NoError(t, stream.Process(context.Background(), UserMessageEvent{Content: "flush"}))

	turns := stream.thread.State.Turns()
	require.GreaterOrEqual(t, len(turns), 4)
	assert.Equal(t, "first", turns[0].Artifacts[0].(artifact.Text).Content)
	assert.Equal(t, "second", turns[1].Artifacts[0].(artifact.Text).Content)
	assert.Equal(t, "third", turns[2].Artifacts[0].(artifact.Text).Content)

	_ = stream.Close()
}

func TestStream_Submit_InterruptClearsQueue(t *testing.T) {
	store := NewMemoryStore()
	mgr := NewManager(store, &blockingProvider{}, func(stream *Stream) ([]loop.Option, error) {
		return nil, nil
	}, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	ch := stream.Subscribe("lifecycle", "error")

	// Start draining in a goroutine before emitting events.
	var events []loop.OutputEvent
	done := make(chan struct{})
	go func() {
		defer close(done)
		for e := range ch {
			events = append(events, e)
		}
	}()

	// Start a blocking turn.
	go func() {
		_ = stream.Process(context.Background(), UserMessageEvent{Content: "blocking"})
	}()

	// Wait for it to start processing.
	time.Sleep(50 * time.Millisecond)

	// Queue more events.
	require.NoError(t, stream.Submit(UserMessageEvent{Content: "queued-1"}))
	require.NoError(t, stream.Submit(UserMessageEvent{Content: "queued-2"}))

	// Interrupt clears the queue and cancels the in-flight turn.
	require.NoError(t, stream.Submit(InterruptEvent{}))

	// Wait for processing to complete.
	time.Sleep(200 * time.Millisecond)
	_ = stream.Close()
	<-done

	// We should get lifecycle events for the cancelled blocking turn
	// (submitted → cancelled → done) plus done for the interrupt itself.
	var pcEvents []loop.LifecycleEvent
	var errEvents []loop.ErrorEvent
	for _, e := range events {
		if pc, ok := e.(loop.LifecycleEvent); ok {
			pcEvents = append(pcEvents, pc)
		}
		if ee, ok := e.(loop.ErrorEvent); ok {
			errEvents = append(errEvents, ee)
		}
	}
	require.Len(t, pcEvents, 4, "expected 4 lifecycle events (submitted, cancelled, done for turn, done for interrupt)")
	require.Len(t, errEvents, 0, "expected 0 error events for context.Canceled")
}

func TestStream_ProcessAndSubmit_Mixed(t *testing.T) {
	store := NewMemoryStore()
	mgr := NewManager(store, &mockProvider{}, func(stream *Stream) ([]loop.Option, error) {
		return nil, nil
	}, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	ch := stream.Subscribe("turn_complete")

	// Mix Process (blocking) and Submit (non-blocking).
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		require.NoError(t, stream.Process(context.Background(), UserMessageEvent{Content: "process-1"}))
	}()

	require.NoError(t, stream.Submit(UserMessageEvent{Content: "submit-1"}))
	require.NoError(t, stream.Submit(UserMessageEvent{Content: "submit-2"}))

	wg.Wait()
	time.Sleep(100 * time.Millisecond)

	events := drainWithClose(t, ch, func() { _ = stream.Close() })
	require.GreaterOrEqual(t, len(events), 6) // 3 user + 3 assistant turns
}

// slowProvider sleeps for a short duration, simulating a slow turn.
type slowProvider struct{}

func (m *slowProvider) Invoke(ctx context.Context, s state.State, _ models.Spec, ch chan<- artifact.Artifact, opts ...provider.InvokeOption) error {
	select {
	case <-time.After(100 * time.Millisecond):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// serialProvider detects concurrent Invoke calls via a mutex-guarded active flag.
type serialProvider struct {
	mu       sync.Mutex
	active   bool
	detected bool
}

func (m *serialProvider) Invoke(ctx context.Context, s state.State, _ models.Spec, ch chan<- artifact.Artifact, opts ...provider.InvokeOption) error {
	m.mu.Lock()
	if m.active {
		m.detected = true
	}
	m.active = true
	m.mu.Unlock()

	time.Sleep(50 * time.Millisecond)

	m.mu.Lock()
	m.active = false
	m.mu.Unlock()
	return nil
}

func TestStream_Submit_AfterClose(t *testing.T) {
	store := NewMemoryStore()
	prov := &mockProvider{}
	mgr := NewManager(store, prov, func(*Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	err = stream.Close()
	require.NoError(t, err)

	err = stream.Submit(UserMessageEvent{Content: "should-fail"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is closed")
}

func TestStream_Close_DuringProcessing(t *testing.T) {
	store := NewMemoryStore()
	prov := &slowProvider{}
	mgr := NewManager(store, prov, func(*Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	// Start a slow turn.
	done := make(chan error)
	go func() {
		done <- stream.Process(context.Background(), UserMessageEvent{Content: "slow"})
	}()

	// Wait for processing to start.
	time.Sleep(20 * time.Millisecond)

	// Close while the turn is in-flight.
	err = stream.Close()
	require.NoError(t, err)

	// Process should return within timeout (not hang forever).
	select {
	case err := <-done:
		// Close cancels the in-flight turn, so Process should return
		// an error (typically context.Canceled).
		require.Error(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Process hung after Close during in-flight turn")
	}

	// Subsequent Submit should be rejected immediately.
	err = stream.Submit(UserMessageEvent{Content: "after-close"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is closed")
}

func TestStream_MultipleSubmit_StartsSingleWorker(t *testing.T) {
	store := NewMemoryStore()
	prov := &serialProvider{}
	mgr := NewManager(store, prov, func(*Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	ch := stream.Subscribe("lifecycle")

	// Submit 3 events rapidly from different goroutines.
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			require.NoError(t, stream.Submit(UserMessageEvent{Content: "concurrent"}))
		}()
	}
	wg.Wait()

	// Wait for all 3 events to complete.
	for i := 0; i < 3; i++ {
		select {
		case <-ch:
		case <-time.After(2 * time.Second):
			t.Fatalf("timeout waiting for lifecycle %d", i)
		}
	}

	_ = stream.Close()

	assert.False(t, prov.detected, "detected concurrent Invoke calls — multiple workers may have started")
}

func TestStream_Interceptor_Consume(t *testing.T) {
	store := NewMemoryStore()
	prov := &mockProvider{}
	consumeInterceptor := func(ctx context.Context, event Event, stream *Stream, emitter loop.Emitter) (InterceptResult, error) {
		return InterceptResult{}, nil
	}
	mgr := NewManager(store, prov, func(*Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor(), WithInterceptor(InterceptorFunc(consumeInterceptor)))

	stream, err := mgr.Create()
	require.NoError(t, err)

	ch := stream.Subscribe("turn_complete")
	err = stream.Process(context.Background(), UserMessageEvent{Content: "hello"})
	require.NoError(t, err)

	// No events should be emitted because the interceptor consumed the event.
	select {
	case event := <-ch:
		t.Fatalf("expected no events, got %T", event)
	case <-time.After(50 * time.Millisecond):
		// Expected timeout — no events.
	}

	// No turns should be added to state.
	assert.Empty(t, stream.Turns())

	_ = stream.Close()
}

func TestStream_Interceptor_PassThrough(t *testing.T) {
	store := NewMemoryStore()
	prov := &mockProvider{}
	passThroughInterceptor := func(ctx context.Context, event Event, stream *Stream, emitter loop.Emitter) (InterceptResult, error) {
		return InterceptResult{Event: event}, nil
	}
	mgr := NewManager(store, prov, func(*Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor(), WithInterceptor(InterceptorFunc(passThroughInterceptor)))

	stream, err := mgr.Create()
	require.NoError(t, err)

	ch := stream.Subscribe("turn_complete")
	err = stream.Process(context.Background(), UserMessageEvent{Content: "hello"})
	require.NoError(t, err)

	// Normal processing should occur — two turn_complete events (user + assistant).
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

	turns := stream.Turns()
	require.Len(t, turns, 2)
	assert.Equal(t, "hello", turns[0].Artifacts[0].(artifact.Text).Content)

	_ = stream.Close()
}

func TestStream_Interceptor_Rewrite(t *testing.T) {
	store := NewMemoryStore()
	prov := &mockProvider{}
	rewriteInterceptor := func(ctx context.Context, event Event, stream *Stream, emitter loop.Emitter) (InterceptResult, error) {
		if ume, ok := event.(UserMessageEvent); ok {
			ume.Content = "rewritten: " + ume.Content
			return InterceptResult{Event: ume}, nil
		}
		return InterceptResult{Event: event}, nil
	}
	mgr := NewManager(store, prov, func(*Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor(), WithInterceptor(InterceptorFunc(rewriteInterceptor)))

	stream, err := mgr.Create()
	require.NoError(t, err)

	err = stream.Process(context.Background(), UserMessageEvent{Content: "hello"})
	require.NoError(t, err)

	turns := stream.Turns()
	require.Len(t, turns, 2)
	assert.Equal(t, "rewritten: hello", turns[0].Artifacts[0].(artifact.Text).Content)

	_ = stream.Close()
}

func TestStream_Interceptor_Notice(t *testing.T) {
	store := NewMemoryStore()
	prov := &mockProvider{}
	noticeInterceptor := func(ctx context.Context, event Event, stream *Stream, emitter loop.Emitter) (InterceptResult, error) {
		return InterceptResult{
			Notice: []loop.Notice{
				{Content: "first notice", Severity: loop.SeverityInfo},
				{Content: "second notice", Severity: loop.SeverityWarn},
			},
		}, nil
	}
	mgr := NewManager(store, prov, func(*Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor(), WithInterceptor(InterceptorFunc(noticeInterceptor)))

	stream, err := mgr.Create()
	require.NoError(t, err)

	noticeCh := stream.Subscribe("notice")
	turnCh := stream.Subscribe("turn_complete")

	err = stream.Process(context.Background(), UserMessageEvent{Content: "/test"})
	require.NoError(t, err)

	// Should receive 2 notice events in order.
	var notices []loop.NoticeEvent
	for i := 0; i < 2; i++ {
		select {
		case event := <-noticeCh:
			n, ok := event.(loop.NoticeEvent)
			require.True(t, ok)
			notices = append(notices, n)
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("timeout waiting for notice event %d", i)
		}
	}
	require.Len(t, notices, 2)
	assert.Equal(t, "first notice", notices[0].Notice.Content)
	assert.Equal(t, loop.SeverityInfo, notices[0].Notice.Severity)
	assert.Equal(t, "second notice", notices[1].Notice.Content)
	assert.Equal(t, loop.SeverityWarn, notices[1].Notice.Severity)

	// No turn_complete events because the event was consumed (nil Event).
	select {
	case event := <-turnCh:
		t.Fatalf("expected no turn events, got %T", event)
	case <-time.After(50 * time.Millisecond):
		// Expected timeout — no events.
	}

	// No turns should be added to state.
	assert.Empty(t, stream.Turns())

	_ = stream.Close()
}

func TestStream_Interceptor_NoticeProvenance(t *testing.T) {
	store := NewMemoryStore()
	prov := &mockProvider{}
	noticeInterceptor := func(ctx context.Context, event Event, stream *Stream, emitter loop.Emitter) (InterceptResult, error) {
		return InterceptResult{
			Notice: []loop.Notice{{Content: "notice message", Severity: loop.SeverityInfo}},
		}, nil
	}
	mgr := NewManager(store, prov, func(*Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor(), WithInterceptor(InterceptorFunc(noticeInterceptor)))

	stream, err := mgr.Create()
	require.NoError(t, err)

	noticeCh := stream.Subscribe("notice")

	ctx := loop.WithProvenance(context.Background(), "test-provenance")
	err = stream.Process(ctx, UserMessageEvent{Content: "/test", Ctx: ctx})
	require.NoError(t, err)

	select {
	case event := <-noticeCh:
		n, ok := event.(loop.NoticeEvent)
		require.True(t, ok)
		assert.Equal(t, "notice message", n.Notice.Content)
		assert.Equal(t, loop.SeverityInfo, n.Notice.Severity)
		name, _ := loop.ProvenanceFrom(n.Ctx)
		assert.Equal(t, "test-provenance", name, "notice event should carry the original user message provenance")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for notice event")
	}

	_ = stream.Close()
}

func TestStream_Interceptor_NoticeWithReplaceAndMultipleNotice(t *testing.T) {
	store := NewMemoryStore()
	prov := &mockProvider{}
	noticeInterceptor := func(ctx context.Context, event Event, stream *Stream, emitter loop.Emitter) (InterceptResult, error) {
		ume, ok := event.(UserMessageEvent)
		require.True(t, ok)
		ume.Content = "rewritten: " + ume.Content
		return InterceptResult{
			Event: ume,
			Notice: []loop.Notice{
				{Content: "n1", Severity: loop.SeverityInfo},
				{Content: "n2", Severity: loop.SeveritySuccess},
			},
		}, nil
	}
	mgr := NewManager(store, prov, func(*Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor(), WithInterceptor(InterceptorFunc(noticeInterceptor)))

	stream, err := mgr.Create()
	require.NoError(t, err)

	noticeCh := stream.Subscribe("notice")
	turnCh := stream.Subscribe("turn_complete")

	err = stream.Process(context.Background(), UserMessageEvent{Content: "hello"})
	require.NoError(t, err)

	// Should receive 2 notice events.
	var notices []loop.NoticeEvent
	for i := 0; i < 2; i++ {
		select {
		case event := <-noticeCh:
			n, ok := event.(loop.NoticeEvent)
			require.True(t, ok)
			notices = append(notices, n)
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("timeout waiting for notice event %d", i)
		}
	}
	require.Len(t, notices, 2)
	assert.Equal(t, "n1", notices[0].Notice.Content)
	assert.Equal(t, loop.SeverityInfo, notices[0].Notice.Severity)
	assert.Equal(t, "n2", notices[1].Notice.Content)
	assert.Equal(t, loop.SeveritySuccess, notices[1].Notice.Severity)

	// Should also receive turn_complete events because the event was replaced.
	var events []loop.OutputEvent
	for i := 0; i < 2; i++ {
		select {
		case event := <-turnCh:
			events = append(events, event)
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("timeout waiting for turn event %d", i)
		}
	}
	require.Len(t, events, 2)

	// Turns should have the rewritten content.
	turns := stream.Turns()
	require.Len(t, turns, 2)
	assert.Equal(t, "rewritten: hello", turns[0].Artifacts[0].(artifact.Text).Content)

	_ = stream.Close()
}

func TestStream_Interceptor_NoticeWithReplace(t *testing.T) {
	store := NewMemoryStore()
	prov := &mockProvider{}
	noticeInterceptor := func(ctx context.Context, event Event, stream *Stream, emitter loop.Emitter) (InterceptResult, error) {
		ume, ok := event.(UserMessageEvent)
		require.True(t, ok)
		ume.Content = "rewritten: " + ume.Content
		return InterceptResult{
			Event:    ume,
			Notice: []loop.Notice{{Content: "notice message", Severity: loop.SeverityError}},
		}, nil
	}
	mgr := NewManager(store, prov, func(*Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor(), WithInterceptor(InterceptorFunc(noticeInterceptor)))

	stream, err := mgr.Create()
	require.NoError(t, err)

	noticeCh := stream.Subscribe("notice")
	turnCh := stream.Subscribe("turn_complete")

	err = stream.Process(context.Background(), UserMessageEvent{Content: "hello"})
	require.NoError(t, err)

	// Notice event should be received.
	select {
	case event := <-noticeCh:
		n, ok := event.(loop.NoticeEvent)
		require.True(t, ok)
		assert.Equal(t, "notice message", n.Notice.Content)
		assert.Equal(t, loop.SeverityError, n.Notice.Severity)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for notice event")
	}

	// Turn should complete because the event was replaced (not consumed).
	var events []loop.OutputEvent
	for i := 0; i < 2; i++ {
		select {
		case event := <-turnCh:
			events = append(events, event)
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("timeout waiting for turn event %d", i)
		}
	}
	require.Len(t, events, 2)

	turns := stream.Turns()
	require.Len(t, turns, 2)
	assert.Equal(t, "rewritten: hello", turns[0].Artifacts[0].(artifact.Text).Content)

	_ = stream.Close()
}

func TestStream_ModelOption(t *testing.T) {
	store := NewMemoryStore()
	prov := &mockProvider{}
	mgr := NewManager(store, prov, func(*Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })

	t.Run("returns false when metadata key is absent", func(t *testing.T) {
		_, ok := stream.Spec()
		assert.False(t, ok,
			"fresh stream has no ore.model.name metadata; Spec must return false so the caller uses the loop's default")
	})

	t.Run("returns Spec carrying the metadata value", func(t *testing.T) {
		stream.SetMetadata(MetadataKeyModelName, "gpt-4o-mini")

		spec, ok := stream.Spec()
		require.True(t, ok, "Spec should be present after setting ore.model.name")
		assert.Equal(t, "gpt-4o-mini", spec.Name)
	})

	t.Run("returns false when metadata is explicitly cleared", func(t *testing.T) {
		// Even if the key was previously set, an empty string is a
		// no-op signal. Spec must mirror that contract.
		stream.SetMetadata(MetadataKeyModelName, "")

		_, ok := stream.Spec()
		assert.False(t, ok,
			"empty metadata value must produce ok=false, matching the empty-Name no-op contract")
	})
}

func TestStream_Interceptor_Error(t *testing.T) {
	store := NewMemoryStore()
	prov := &mockProvider{}
	errorInterceptor := func(ctx context.Context, event Event, stream *Stream, emitter loop.Emitter) (InterceptResult, error) {
		return InterceptResult{}, errors.New("interceptor error")
	}
	mgr := NewManager(store, prov, func(*Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor(), WithInterceptor(InterceptorFunc(errorInterceptor)))

	stream, err := mgr.Create()
	require.NoError(t, err)

	err = stream.Process(context.Background(), UserMessageEvent{Content: "hello"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "interceptor error")

	_ = stream.Close()
}

func TestStream_Interceptor_NonUserMessage(t *testing.T) {
	store := NewMemoryStore()
	prov := &mockProvider{}
	var calledWith Event
	nonUserInterceptor := func(ctx context.Context, event Event, stream *Stream, emitter loop.Emitter) (InterceptResult, error) {
		calledWith = event
		return InterceptResult{Event: event}, nil
	}
	mgr := NewManager(store, prov, func(*Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor(), WithInterceptor(InterceptorFunc(nonUserInterceptor)))

	stream, err := mgr.Create()
	require.NoError(t, err)

	err = stream.Process(context.Background(), InterruptEvent{Ctx: context.Background()})
	require.NoError(t, err)

	// Interceptor should not be called for non-UserMessageEvent.
	assert.Nil(t, calledWith)

	_ = stream.Close()
}
