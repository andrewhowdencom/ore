package session

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
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

func TestStream_Submit_NonBlocking(t *testing.T) {
	store := thread.NewMemoryStore()
	sleepyProcessor := func() TurnProcessor {
		return func(ctx context.Context, step *loop.Step, st state.State, prov provider.Provider) (state.State, error) {
			time.Sleep(50 * time.Millisecond)
			return st, nil
		}
	}
	mgr := NewManager(store, &mockProvider{}, func(thr *thread.Thread) (*loop.Step, error) {
		return loop.New(loop.WithOnEmit(func(ctx context.Context, event loop.OutputEvent) {
			if tc, ok := event.(loop.TurnCompleteEvent); ok {
				thr.State.Append(tc.Turn.Role, tc.Turn.Artifacts...)
			}
		})), nil
	}, sleepyProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	ch := stream.Subscribe("process_complete")

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
		t.Fatal("timeout waiting for process_complete")
	}

	_ = stream.Close()
}

func TestStream_Submit_FIFOOrder(t *testing.T) {
	store := thread.NewMemoryStore()
	mgr := NewManager(store, &mockProvider{}, func(thr *thread.Thread) (*loop.Step, error) {
		return loop.New(loop.WithOnEmit(func(ctx context.Context, event loop.OutputEvent) {
			if tc, ok := event.(loop.TurnCompleteEvent); ok {
				thr.State.Append(tc.Turn.Role, tc.Turn.Artifacts...)
			}
		})), nil
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
	store := thread.NewMemoryStore()
	mgr := NewManager(store, &blockingProvider{}, func(thr *thread.Thread) (*loop.Step, error) {
		return loop.New(loop.WithOnEmit(func(ctx context.Context, event loop.OutputEvent) {
			if tc, ok := event.(loop.TurnCompleteEvent); ok {
				thr.State.Append(tc.Turn.Role, tc.Turn.Artifacts...)
			}
		})), nil
	}, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	ch := stream.Subscribe("process_complete")

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

	// We should get process_complete for the cancelled blocking event and the interrupt.
	// The queued events should have been dropped.
	var pcEvents []loop.ProcessCompleteEvent
	for _, e := range events {
		if pc, ok := e.(loop.ProcessCompleteEvent); ok {
			pcEvents = append(pcEvents, pc)
		}
	}
	require.Len(t, pcEvents, 2, "expected 2 process_complete events (cancelled + interrupt)")

	// First event was cancelled.
	require.NotNil(t, pcEvents[0].Err)

	// Interrupt succeeded.
	require.Nil(t, pcEvents[1].Err)
}

func TestStream_ProcessAndSubmit_Mixed(t *testing.T) {
	store := thread.NewMemoryStore()
	mgr := NewManager(store, &mockProvider{}, func(thr *thread.Thread) (*loop.Step, error) {
		return loop.New(loop.WithOnEmit(func(ctx context.Context, event loop.OutputEvent) {
			if tc, ok := event.(loop.TurnCompleteEvent); ok {
				thr.State.Append(tc.Turn.Role, tc.Turn.Artifacts...)
			}
		})), nil
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

func (m *slowProvider) Invoke(ctx context.Context, s state.State, ch chan<- artifact.Artifact, opts ...provider.InvokeOption) error {
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

func (m *serialProvider) Invoke(ctx context.Context, s state.State, ch chan<- artifact.Artifact, opts ...provider.InvokeOption) error {
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
	store := thread.NewMemoryStore()
	prov := &mockProvider{}
	mgr := NewManager(store, prov, func(*thread.Thread) (*loop.Step, error) { return loop.New(), nil }, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	err = stream.Close()
	require.NoError(t, err)

	err = stream.Submit(UserMessageEvent{Content: "should-fail"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is closed")
}

func TestStream_Close_DuringProcessing(t *testing.T) {
	store := thread.NewMemoryStore()
	prov := &slowProvider{}
	mgr := NewManager(store, prov, func(*thread.Thread) (*loop.Step, error) { return loop.New(), nil }, simpleProcessor())

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
	store := thread.NewMemoryStore()
	prov := &serialProvider{}
	mgr := NewManager(store, prov, func(*thread.Thread) (*loop.Step, error) { return loop.New(), nil }, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	ch := stream.Subscribe("process_complete")

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
			t.Fatalf("timeout waiting for process_complete %d", i)
		}
	}

	_ = stream.Close()

	assert.False(t, prov.detected, "detected concurrent Invoke calls — multiple workers may have started")
}
