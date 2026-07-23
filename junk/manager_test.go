package junk

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/models"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/ledger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// drainWithClose starts a goroutine that reads all events from ch, then calls
// closeFn to close the source channel, and waits up to 2s for the drain to
// complete. It fails the test if the drain times out.
func provenance(ctx context.Context) string {
	p, _ := loop.ProvenanceFrom(ctx)
	return p
}

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

func (m *mockProvider) Invoke(ctx context.Context, s ledger.State, _ models.Spec, ch chan<- artifact.Artifact, opts ...provider.InvokeOption) error {
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
	return func(ctx context.Context, step *loop.Step, st ledger.State, prov provider.Provider, _ models.Spec) (ledger.State, error) {
		spec := models.Spec{Name: "test-model"}
		return step.Turn(ctx, st, spec, prov)
	}
}

// nopProcessor does nothing (used for submit-only tests).
func nopProcessor() TurnProcessor {
	return func(ctx context.Context, step *loop.Step, st ledger.State, prov provider.Provider, _ models.Spec) (ledger.State, error) {
		return st, nil
	}
}

// blockingProvider is a provider that blocks until the context is cancelled.
type blockingProvider struct{}

func (m *blockingProvider) Invoke(ctx context.Context, s ledger.State, _ models.Spec, ch chan<- artifact.Artifact, opts ...provider.InvokeOption) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestNewManager(t *testing.T) {
	store := NewMemoryStore()
	prov := &mockProvider{}
	mgr := NewManager(store, prov, func(stream *Stream) ([]loop.Option, error) {
		return nil, nil
	}, simpleProcessor())
	require.NotNil(t, mgr)
}

func TestManager_Create(t *testing.T) {
	store := NewMemoryStore()
	mgr := NewManager(store, &mockProvider{}, func(stream *Stream) ([]loop.Option, error) {
		return nil, nil
	}, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)
	require.NotNil(t, stream)
	assert.NotEmpty(t, stream.ID())

	// Thread should exist in store.
	thr, err := store.Get(stream.ID())
	assert.NoError(t, err)
	assert.Equal(t, stream.ID(), thr.ID)

	// Session should be active.
	active := mgr.List()
	require.Len(t, active, 1)
	assert.Equal(t, stream.ID(), active[0].ID())
}

func TestManager_Attach(t *testing.T) {
	store := NewMemoryStore()
	mgr := NewManager(store, &mockProvider{}, func(stream *Stream) ([]loop.Option, error) {
		return nil, nil
	}, simpleProcessor())

	// Create a thread directly in the store.
	thr, err := store.Create()
	require.NoError(t, err)

	// Attach should create a new active session for the existing thread.
	stream, err := mgr.Attach(thr.ID)
	require.NoError(t, err)
	assert.Equal(t, thr.ID, stream.ID())

	// Active sessions should include it.
	active := mgr.List()
	require.Len(t, active, 1)
}

func TestManager_StepFactoryError(t *testing.T) {
	store := NewMemoryStore()
	failingFactory := func(*Stream) ([]loop.Option, error) {
		return nil, fmt.Errorf("factory failure")
	}
	mgr := NewManager(store, &mockProvider{}, failingFactory, simpleProcessor())

	// Create should propagate the step factory error.
	_, err := mgr.Create()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create step")
	assert.Contains(t, err.Error(), "factory failure")

	// Attach should also propagate the step factory error.
	thr, err := store.Create()
	require.NoError(t, err)
	_, err = mgr.Attach(thr.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create step")
	assert.Contains(t, err.Error(), "factory failure")
	assert.Empty(t, mgr.List(), "factory error should not leak a session")
}

func TestManager_Create_FactoryReceivesThread(t *testing.T) {
	store := NewMemoryStore()
	var receivedThread *Thread
	factory := func(stream *Stream) ([]loop.Option, error) {
		receivedThread = stream.thread
		return nil, nil
	}
	mgr := NewManager(store, &mockProvider{}, factory, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)
	require.NotNil(t, receivedThread)
	assert.Equal(t, stream.ID(), receivedThread.ID)
}

func TestManager_Attach_FactoryReceivesThread(t *testing.T) {
	store := NewMemoryStore()
	thr, err := store.Create()
	require.NoError(t, err)

	var receivedThread *Thread
	factory := func(stream *Stream) ([]loop.Option, error) {
		receivedThread = stream.thread
		return nil, nil
	}
	mgr := NewManager(store, &mockProvider{}, factory, simpleProcessor())

	stream, err := mgr.Attach(thr.ID)
	require.NoError(t, err)
	require.NotNil(t, receivedThread)
	assert.Equal(t, thr.ID, receivedThread.ID)
	assert.Equal(t, stream.ID(), receivedThread.ID)
}

func TestManager_Attach_ExistingSession(t *testing.T) {
	store := NewMemoryStore()
	mgr := NewManager(store, &mockProvider{}, func(stream *Stream) ([]loop.Option, error) {
		return nil, nil
	}, simpleProcessor())

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
	store := NewMemoryStore()
	mgr := NewManager(store, &mockProvider{}, func(stream *Stream) ([]loop.Option, error) {
		return nil, nil
	}, simpleProcessor())

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
	store := NewMemoryStore()
	mgr := NewManager(store, prov, func(stream *Stream) ([]loop.Option, error) {
		return nil, nil
	}, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	// Subscribe to output before processing.
	ch := stream.Subscribe("text_delta", "turn_complete")

	// Process a user message.
	err = stream.Process(context.Background(), UserMessageEvent{Content: "hi"})
	require.NoError(t, err)

	// Collect output events, then close the session to close the channel.
	events := drainWithClose(t, ch, func() { _ = stream.Close() })

	var deltas []artifact.Artifact
	var turnComplete bool
	for _, event := range events {
		switch e := event.(type) {
		case loop.ArtifactEvent:
			if td, ok := e.Artifact.(artifact.TextDelta); ok {
				deltas = append(deltas, td)
			}
		case loop.TurnCompleteEvent:
			turnComplete = true
		}
	}

	assert.Len(t, deltas, 2)
	assert.True(t, turnComplete)

	// Thread state should have been saved.
	thr, err := store.Get(stream.ID())
	require.NoError(t, err)
	turns := thr.State.Turns()
	require.Len(t, turns, 2) // user + assistant
	assert.Equal(t, ledger.RoleUser, turns[0].Role)
	assert.Equal(t, ledger.RoleAssistant, turns[1].Role)
}

func TestStream_Process_Queued(t *testing.T) {
	store := NewMemoryStore()
	mgr := NewManager(store, &mockProvider{}, func(stream *Stream) ([]loop.Option, error) {
		return nil, nil
	}, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	ch := stream.Subscribe("turn_complete")

	// Submit two events concurrently via Process.
	// With the queue, both should succeed and be processed serially.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		require.NoError(t, stream.Process(context.Background(), UserMessageEvent{Content: "first"}))
	}()
	go func() {
		defer wg.Done()
		require.NoError(t, stream.Process(context.Background(), UserMessageEvent{Content: "second"}))
	}()
	wg.Wait()

	events := drainWithClose(t, ch, func() { _ = stream.Close() })
	require.GreaterOrEqual(t, len(events), 4) // 2 user + 2 assistant turns
}

func TestStream_Process_Closed(t *testing.T) {
	store := NewMemoryStore()
	mgr := NewManager(store, &mockProvider{}, func(stream *Stream) ([]loop.Option, error) {
		return nil, nil
	}, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)
	err = stream.Close()
	require.NoError(t, err)

	err = stream.Process(context.Background(), UserMessageEvent{Content: "hi"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

type unsupportedEvent struct{}

func (e *unsupportedEvent) Kind() string { return "unsupported" }

func (e *unsupportedEvent) Context() context.Context { return context.Background() }

func TestManager_Process_UnsupportedEvent(t *testing.T) {
	store := NewMemoryStore()
	mgr := NewManager(store, &mockProvider{}, func(stream *Stream) ([]loop.Option, error) {
		return nil, nil
	}, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	err = stream.Process(context.Background(), &unsupportedEvent{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported event kind")
}

func TestManager_Process_ContextCancel(t *testing.T) {
	store := NewMemoryStore()
	mgr := NewManager(store, &blockingProvider{}, func(stream *Stream) ([]loop.Option, error) {
		return nil, nil
	}, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	ch := stream.Subscribe("text_delta", "turn_complete", "error", "lifecycle")

	// Start processing with a cancellable context.
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err = stream.Process(ctx, UserMessageEvent{Content: "cancel me"})
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)

	// Drain the channel and assert cancellation emits lifecycle events.
	events := drainWithClose(t, ch, func() { _ = stream.Close() })

	var lifecycleEvents []loop.LifecycleEvent
	var errEvents []loop.ErrorEvent
	for _, e := range events {
		switch ev := e.(type) {
		case loop.LifecycleEvent:
			lifecycleEvents = append(lifecycleEvents, ev)
		case loop.ErrorEvent:
			errEvents = append(errEvents, ev)
		}
	}

	require.Len(t, lifecycleEvents, 3, "expected submitted → cancelled → done lifecycle events")
	assert.Equal(t, "submitted", lifecycleEvents[0].Phase)
	assert.Equal(t, "cancelled", lifecycleEvents[1].Phase)
	assert.Equal(t, "done", lifecycleEvents[2].Phase)
	assert.Len(t, errEvents, 0, "expected 0 ErrorEvents for context.Canceled")
}

func TestManager_Process_SaveError(t *testing.T) {
	prov := &mockProvider{}
	store := &errStore{}
	mgr := NewManager(store, prov, func(stream *Stream) ([]loop.Option, error) {
		return nil, nil
	}, nopProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	err = stream.Process(context.Background(), UserMessageEvent{Content: "hi"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "save failed")
}

func TestManager_Cancel(t *testing.T) {
	// Provider that blocks until context is cancelled.
	prov := &blockingProvider{}
	store := NewMemoryStore()
	mgr := NewManager(store, prov, func(stream *Stream) ([]loop.Option, error) {
		return nil, nil
	}, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	ch := stream.Subscribe("lifecycle", "error")

	// Start a blocking turn.
	ctx := context.Background()
	go func() {
		_ = stream.Process(ctx, UserMessageEvent{Content: "block"})
	}()

	// Wait for it to start processing.
	time.Sleep(50 * time.Millisecond)

	// Cancel should abort the ongoing turn.
	err = stream.Cancel()
	require.NoError(t, err)

	// Wait for cancellation to propagate and drain events.
	time.Sleep(100 * time.Millisecond)
	_ = stream.Close()

	events := drainWithClose(t, ch, func() {})

	var lifecycleEvents []loop.LifecycleEvent
	var errEvents []loop.ErrorEvent
	for _, e := range events {
		switch ev := e.(type) {
		case loop.LifecycleEvent:
			lifecycleEvents = append(lifecycleEvents, ev)
		case loop.ErrorEvent:
			errEvents = append(errEvents, ev)
		}
	}

	require.Len(t, lifecycleEvents, 3, "expected submitted → cancelled → done lifecycle events")
	assert.Equal(t, "submitted", lifecycleEvents[0].Phase)
	assert.Equal(t, "cancelled", lifecycleEvents[1].Phase)
	assert.Equal(t, "done", lifecycleEvents[2].Phase)
	assert.Len(t, errEvents, 0, "expected 0 ErrorEvents for context.Canceled")
}

func TestStream_Cancel_Closed(t *testing.T) {
	store := NewMemoryStore()
	mgr := NewManager(store, &mockProvider{}, func(stream *Stream) ([]loop.Option, error) {
		return nil, nil
	}, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)
	err = stream.Close()
	require.NoError(t, err)

	// Cancel on a closed session should return an error.
	err = stream.Cancel()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

func TestStream_Subscribe_Closed(t *testing.T) {
	store := NewMemoryStore()
	mgr := NewManager(store, &mockProvider{}, func(stream *Stream) ([]loop.Option, error) {
		return nil, nil
	}, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)
	err = stream.Close()
	require.NoError(t, err)

	// Subscribe on a closed session should return a closed channel.
	ch := stream.Subscribe("text_delta")
	_, ok := <-ch
	require.False(t, ok, "channel should be closed")
}

func TestManager_Close(t *testing.T) {
	store := NewMemoryStore()
	mgr := NewManager(store, &mockProvider{}, func(stream *Stream) ([]loop.Option, error) {
		return nil, nil
	}, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	// Close should remove the session from the active map.
	err = mgr.Close(stream.ID())
	require.NoError(t, err)

	active := mgr.List()
	assert.Empty(t, active)

	// Subscribe should return a closed channel after close.
	ch := stream.Subscribe("text_delta")
	_, ok := <-ch
	require.False(t, ok, "channel should be closed")

	// Thread should still exist in the store.
	_, err = store.Get(stream.ID())
	assert.NoError(t, err)
}

func TestManager_Close_NotFound(t *testing.T) {
	store := NewMemoryStore()
	mgr := NewManager(store, &mockProvider{}, func(stream *Stream) ([]loop.Option, error) {
		return nil, nil
	}, simpleProcessor())

	err := mgr.Close("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestManager_List(t *testing.T) {
	store := NewMemoryStore()
	mgr := NewManager(store, &mockProvider{}, func(stream *Stream) ([]loop.Option, error) {
		return nil, nil
	}, simpleProcessor())

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

func TestStream_Process_Serial(t *testing.T) {
	// Use a processor that sleeps briefly so we can observe serialization.
	sleepyProcessor := func() TurnProcessor {
		return func(ctx context.Context, step *loop.Step, st ledger.State, prov provider.Provider, _ models.Spec) (ledger.State, error) {
			time.Sleep(50 * time.Millisecond)
			return st, nil
		}
	}

	store := NewMemoryStore()
	mgr := NewManager(store, &mockProvider{}, func(stream *Stream) ([]loop.Option, error) {
		return nil, nil
	}, sleepyProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			err := stream.Process(context.Background(), UserMessageEvent{Content: fmt.Sprintf("msg-%d", n)})
			require.NoError(t, err)
		}(i)
	}
	wg.Wait()

	// All 10 events should have been processed and appended to ledger.
	turns := stream.thread.State.Turns()
	// sleepyProcessor does not call step.Turn(), so each event produces
	// exactly 1 user turn (from step.Submit()).
	require.GreaterOrEqual(t, len(turns), 10)

	_ = stream.Close()
}

func TestManager_Get_NotFound(t *testing.T) {
	mgr := NewManager(NewMemoryStore(), nil, nil, nil)
	_, err := mgr.Get("missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestStream_Cancel_Idle(t *testing.T) {
	store := NewMemoryStore()
	mgr := NewManager(store, &mockProvider{}, func(stream *Stream) ([]loop.Option, error) {
		return nil, nil
	}, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	// Cancel on an idle session (no active turn) should be a no-op.
	err = stream.Cancel()
	require.NoError(t, err)
}

func TestStream_Close_Idempotent(t *testing.T) {
	store := NewMemoryStore()
	mgr := NewManager(store, &mockProvider{}, func(stream *Stream) ([]loop.Option, error) {
		return nil, nil
	}, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	// First close should succeed.
	err = stream.Close()
	require.NoError(t, err)

	// Second close should also succeed (idempotent, no panic).
	err = stream.Close()
	require.NoError(t, err)

	// Subscribe should still return a closed channel after double-close.
	ch := stream.Subscribe("text_delta")
	_, ok := <-ch
	require.False(t, ok, "channel should be closed")
}

func TestStream_Process_ProviderError(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "Partial"},
		},
		err: fmt.Errorf("provider failure"),
	}
	store := NewMemoryStore()
	mgr := NewManager(store, prov, func(stream *Stream) ([]loop.Option, error) {
		return nil, nil
	}, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	err = stream.Process(context.Background(), UserMessageEvent{Content: "hi"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "process event")
}

func boomProcessor() TurnProcessor {
	return func(ctx context.Context, step *loop.Step, st ledger.State, prov provider.Provider, _ models.Spec) (ledger.State, error) {
		return st, fmt.Errorf("boom")
	}
}

func TestStream_Process_ProcessorError(t *testing.T) {
	store := NewMemoryStore()
	mgr := NewManager(store, &mockProvider{}, func(stream *Stream) ([]loop.Option, error) {
		return nil, nil
	}, boomProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	err = stream.Process(context.Background(), UserMessageEvent{Content: "hi"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "process event")
	assert.Contains(t, err.Error(), "boom")
}

func TestManager_Attach_Concurrent(t *testing.T) {
	store := NewMemoryStore()
	mgr := NewManager(store, &mockProvider{}, func(stream *Stream) ([]loop.Option, error) {
		return nil, nil
	}, simpleProcessor())

	// Create a thread directly in the store.
	thr, err := store.Create()
	require.NoError(t, err)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = mgr.Attach(thr.ID)
		}()
	}
	wg.Wait()

	// Only one active stream should exist.
	active := mgr.List()
	require.Len(t, active, 1)
	assert.Equal(t, thr.ID, active[0].ID())
}

func TestManager_RegisterSink_ReceivesEventsFromNewStream(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "Hello"},
			artifact.TextDelta{Content: " world"},
		},
	}
	store := NewMemoryStore()
	mgr := NewManager(store, prov, func(stream *Stream) ([]loop.Option, error) {
		return nil, nil
	}, simpleProcessor())

	var mu sync.Mutex
	var events []loop.OutputEvent
	var streamID string

	unregister := mgr.RegisterSink([]string{"text_delta", "turn_complete"}, func(id string, event loop.OutputEvent) {
		mu.Lock()
		defer mu.Unlock()
		streamID = id
		events = append(events, event)
	})
	defer unregister()

	stream, err := mgr.Create()
	require.NoError(t, err)

	err = stream.Process(context.Background(), UserMessageEvent{Content: "hi"})
	require.NoError(t, err)

	// Wait for events to propagate through the forwarding goroutine.
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	require.Len(t, events, 4)
	assert.Equal(t, stream.ID(), streamID)
	assert.Equal(t, "turn_complete", events[0].Kind()) // user turn from Submit
	assert.Equal(t, "text_delta", events[1].Kind())
	assert.Equal(t, "text_delta", events[2].Kind())
	assert.Equal(t, "turn_complete", events[3].Kind()) // assistant turn from Turn
}

func TestManager_RegisterSink_ReceivesEventsFromExistingStream(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "Hello"},
		},
	}
	store := NewMemoryStore()
	mgr := NewManager(store, prov, func(stream *Stream) ([]loop.Option, error) {
		return nil, nil
	}, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	var mu sync.Mutex
	var events []loop.OutputEvent

	unregister := mgr.RegisterSink([]string{"text_delta", "turn_complete"}, func(id string, event loop.OutputEvent) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, event)
	})
	defer unregister()

	err = stream.Process(context.Background(), UserMessageEvent{Content: "hi"})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	require.GreaterOrEqual(t, len(events), 2)
}

func TestManager_RegisterSink_UnregisterStopsDelivery(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "Hello"},
		},
	}
	store := NewMemoryStore()
	mgr := NewManager(store, prov, func(stream *Stream) ([]loop.Option, error) {
		return nil, nil
	}, simpleProcessor())

	var mu sync.Mutex
	var events []loop.OutputEvent

	unregister := mgr.RegisterSink([]string{"text_delta", "turn_complete"}, func(id string, event loop.OutputEvent) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, event)
	})

	stream, err := mgr.Create()
	require.NoError(t, err)

	err = stream.Process(context.Background(), UserMessageEvent{Content: "hi"})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	countAfterFirst := len(events)
	mu.Unlock()

	unregister()

	err = stream.Process(context.Background(), UserMessageEvent{Content: "again"})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, countAfterFirst, len(events))
}

func TestManager_RegisterSink_MultipleSinks(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "Hello"},
		},
	}
	store := NewMemoryStore()
	mgr := NewManager(store, prov, func(stream *Stream) ([]loop.Option, error) {
		return nil, nil
	}, simpleProcessor())

	var mu sync.Mutex
	var events1 []loop.OutputEvent
	var events2 []loop.OutputEvent

	unregister1 := mgr.RegisterSink([]string{"text_delta", "turn_complete"}, func(id string, event loop.OutputEvent) {
		mu.Lock()
		defer mu.Unlock()
		events1 = append(events1, event)
	})
	defer unregister1()

	unregister2 := mgr.RegisterSink([]string{"text_delta", "turn_complete"}, func(id string, event loop.OutputEvent) {
		mu.Lock()
		defer mu.Unlock()
		events2 = append(events2, event)
	})
	defer unregister2()

	stream, err := mgr.Create()
	require.NoError(t, err)

	err = stream.Process(context.Background(), UserMessageEvent{Content: "hi"})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	require.Len(t, events1, len(events2))
	require.GreaterOrEqual(t, len(events1), 2)
}

func TestManager_RegisterSink_KindFiltering(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "Hello"},
		},
	}
	store := NewMemoryStore()
	mgr := NewManager(store, prov, func(stream *Stream) ([]loop.Option, error) {
		return nil, nil
	}, simpleProcessor())

	var mu sync.Mutex
	var deltaEvents []loop.OutputEvent
	var turnEvents []loop.OutputEvent

	unregister1 := mgr.RegisterSink([]string{"text_delta"}, func(id string, event loop.OutputEvent) {
		mu.Lock()
		defer mu.Unlock()
		deltaEvents = append(deltaEvents, event)
	})
	defer unregister1()

	unregister2 := mgr.RegisterSink([]string{"turn_complete"}, func(id string, event loop.OutputEvent) {
		mu.Lock()
		defer mu.Unlock()
		turnEvents = append(turnEvents, event)
	})
	defer unregister2()

	stream, err := mgr.Create()
	require.NoError(t, err)

	err = stream.Process(context.Background(), UserMessageEvent{Content: "hi"})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	require.Len(t, deltaEvents, 1)
	assert.Equal(t, "text_delta", deltaEvents[0].Kind())

	require.Len(t, turnEvents, 2)
	assert.Equal(t, "turn_complete", turnEvents[0].Kind())
	assert.Equal(t, "turn_complete", turnEvents[1].Kind())
}

func TestManager_RegisterSink_ClosedStreamNoEvents(t *testing.T) {
	store := NewMemoryStore()
	mgr := NewManager(store, &mockProvider{}, func(stream *Stream) ([]loop.Option, error) {
		return nil, nil
	}, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)
	err = stream.Close()
	require.NoError(t, err)

	var mu sync.Mutex
	var events []loop.OutputEvent

	unregister := mgr.RegisterSink([]string{"text_delta"}, func(id string, event loop.OutputEvent) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, event)
	})
	defer unregister()

	err = stream.Process(context.Background(), UserMessageEvent{Content: "hi"})
	require.Error(t, err)

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	assert.Empty(t, events)
}

func TestManager_RegisterSink_WildcardKinds(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "Hello"},
		},
	}
	store := NewMemoryStore()
	mgr := NewManager(store, prov, func(stream *Stream) ([]loop.Option, error) {
		return nil, nil
	}, simpleProcessor())

	var mu sync.Mutex
	var events []loop.OutputEvent

	unregister := mgr.RegisterSink([]string{}, func(id string, event loop.OutputEvent) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, event)
	})
	defer unregister()

	stream, err := mgr.Create()
	require.NoError(t, err)

	err = stream.Process(context.Background(), UserMessageEvent{Content: "hi"})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	// Should receive: user message, submitted, streaming, text_delta,
	// text (accumulated), assistant turn_complete, done (pipeline).
	require.Len(t, events, 7)
	var kinds []string
	for _, e := range events {
		kinds = append(kinds, e.Kind())
	}
	assert.Contains(t, kinds, "turn_complete")
	assert.Contains(t, kinds, "text_delta")
	assert.Contains(t, kinds, "text")
	assert.Contains(t, kinds, "lifecycle")
	assert.Contains(t, kinds, "lifecycle")

	var phases []string
	for _, e := range events {
		if le, ok := e.(loop.LifecycleEvent); ok {
			phases = append(phases, le.Phase)
		}
	}
	assert.Equal(t, []string{"submitted", "streaming", "done"}, phases)
}

func TestManager_RegisterSink_MultipleStreams(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "Hello"},
		},
	}
	store := NewMemoryStore()
	mgr := NewManager(store, prov, func(stream *Stream) ([]loop.Option, error) {
		return nil, nil
	}, simpleProcessor())

	var mu sync.Mutex
	eventsByStream := make(map[string][]loop.OutputEvent)

	unregister := mgr.RegisterSink([]string{"text_delta"}, func(id string, event loop.OutputEvent) {
		mu.Lock()
		defer mu.Unlock()
		eventsByStream[id] = append(eventsByStream[id], event)
	})
	defer unregister()

	stream1, err := mgr.Create()
	require.NoError(t, err)

	stream2, err := mgr.Create()
	require.NoError(t, err)

	err = stream1.Process(context.Background(), UserMessageEvent{Content: "hi"})
	require.NoError(t, err)

	err = stream2.Process(context.Background(), UserMessageEvent{Content: "hello"})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	require.Len(t, eventsByStream, 2)
	require.Len(t, eventsByStream[stream1.ID()], 1)
	require.Len(t, eventsByStream[stream2.ID()], 1)
	assert.Equal(t, "text_delta", eventsByStream[stream1.ID()][0].Kind())
	assert.Equal(t, "text_delta", eventsByStream[stream2.ID()][0].Kind())
}

func TestManager_RegisterSink_ConcurrentRegisterUnregister(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "Hello"},
		},
	}
	store := NewMemoryStore()
	mgr := NewManager(store, prov, func(stream *Stream) ([]loop.Option, error) {
		return nil, nil
	}, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	var wg sync.WaitGroup

	// Goroutine that continuously processes events.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 50; j++ {
			err := stream.Process(context.Background(), UserMessageEvent{Content: "hi"})
			require.NoError(t, err)
		}
	}()

	// Goroutines that continuously register and unregister sinks.
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				unregister := mgr.RegisterSink([]string{"text_delta"}, func(id string, event loop.OutputEvent) {})
				time.Sleep(time.Millisecond)
				unregister()
			}
		}()
	}

	wg.Wait()
}

func TestManager_RegisterSink_PanicRecovery(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "Hello"},
		},
	}
	store := NewMemoryStore()
	mgr := NewManager(store, prov, func(stream *Stream) ([]loop.Option, error) {
		return nil, nil
	}, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	var mu sync.Mutex
	var normalEvents []loop.OutputEvent

	// Panicking sink.
	unregisterPanic := mgr.RegisterSink([]string{"text_delta"}, func(id string, event loop.OutputEvent) {
		panic("intentional sink panic")
	})
	defer unregisterPanic()

	// Normal sink that should still receive events.
	unregisterNormal := mgr.RegisterSink([]string{"text_delta"}, func(id string, event loop.OutputEvent) {
		mu.Lock()
		defer mu.Unlock()
		normalEvents = append(normalEvents, event)
	})
	defer unregisterNormal()

	err = stream.Process(context.Background(), UserMessageEvent{Content: "hi"})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, normalEvents, 1)
	assert.Equal(t, "text_delta", normalEvents[0].Kind())
}

func TestManager_RegisterSink_DoubleUnregister(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "Hello"},
		},
	}
	store := NewMemoryStore()
	mgr := NewManager(store, prov, func(stream *Stream) ([]loop.Option, error) {
		return nil, nil
	}, simpleProcessor())

	var mu sync.Mutex
	var events []loop.OutputEvent

	unregister := mgr.RegisterSink([]string{"text_delta"}, func(id string, event loop.OutputEvent) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, event)
	})

	stream, err := mgr.Create()
	require.NoError(t, err)

	// First process - sink should receive events.
	err = stream.Process(context.Background(), UserMessageEvent{Content: "hi"})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	countAfterFirst := len(events)
	mu.Unlock()

	// Unregister twice - should be safe and idempotent.
	unregister()
	unregister()

	// Second process - sink should NOT receive events.
	err = stream.Process(context.Background(), UserMessageEvent{Content: "again"})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, countAfterFirst, len(events))
}

// errStore is a Store that always returns an error from Save.
type errStore struct{}

func (e *errStore) Create() (*Thread, error) { return NewMemoryStore().Create() }
func (e *errStore) Get(id string) (*Thread, error) {
	return NewMemoryStore().Get(id)
}
func (e *errStore) GetBy(key, value string) (*Thread, error) {
	return NewMemoryStore().GetBy(key, value)
}
func (e *errStore) Save(*Thread) error       { return fmt.Errorf("save failed") }
func (e *errStore) Delete(string) bool       { return false }
func (e *errStore) List() ([]*Thread, error) { return nil, nil }

func TestManager_RegisterSink_ContextEchoSuppression(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "Hello"},
			artifact.TextDelta{Content: " world"},
		},
	}
	store := NewMemoryStore()
	mgr := NewManager(store, prov, func(stream *Stream) ([]loop.Option, error) {
		return nil, nil
	}, simpleProcessor())

	var mu sync.Mutex
	var provenances []string

	unregister := mgr.RegisterSink([]string{"turn_complete"}, func(id string, event loop.OutputEvent) {
		mu.Lock()
		defer mu.Unlock()
		if tc, ok := event.(loop.TurnCompleteEvent); ok {
			provenances = append(provenances, provenance(tc.Ctx))
		}
	})
	defer unregister()

	stream, err := mgr.Create()
	require.NoError(t, err)

	err = stream.Process(context.Background(), UserMessageEvent{Content: "hi", Ctx: loop.WithProvenance(context.Background(), "test-provenance")})
	require.NoError(t, err)

	// Wait for events to propagate through the forwarding goroutine.
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	// Both user turn and assistant turn should carry the input provenance.
	require.Len(t, provenances, 2)
	assert.Equal(t, "test-provenance", provenances[0])
	assert.Equal(t, "test-provenance", provenances[1])
}

func TestManager_Process_NoDuplicateTurns(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "Hello"},
			artifact.TextDelta{Content: " world"},
		},
	}
	store := NewMemoryStore()
	mgr := NewManager(store, prov, func(stream *Stream) ([]loop.Option, error) {
		return nil, nil
	}, simpleProcessor())

	stream, err := mgr.Create()
	require.NoError(t, err)

	err = stream.Process(context.Background(), UserMessageEvent{Content: "hi"})
	require.NoError(t, err)
	_ = stream.Close()

	thr, err := store.Get(stream.ID())
	require.NoError(t, err)
	turns := thr.State.Turns()

	// Exactly 2 turns: user message + assistant response.
	require.Len(t, turns, 2, "expected exactly one user turn and one assistant turn, got %d", len(turns))

	roleCounts := make(map[ledger.Role]int)
	for _, turn := range turns {
		roleCounts[turn.Role]++
	}
	assert.Equal(t, 1, roleCounts[ledger.RoleUser], "user turn should appear exactly once")
	assert.Equal(t, 1, roleCounts[ledger.RoleAssistant], "assistant turn should appear exactly once")
}

// toolLoopProvider returns a ToolCall on the first invocation and a text
// delta on the second, simulating a ReAct tool-calling loop.
type toolLoopProvider struct {
	callCount int
}

func (p *toolLoopProvider) Invoke(ctx context.Context, s ledger.State, _ models.Spec, ch chan<- artifact.Artifact, opts ...provider.InvokeOption) error {
	p.callCount++
	if p.callCount == 1 {
		ch <- artifact.ToolCall{Name: "test_tool", Arguments: `{"query":"hello"}`}
	} else {
		ch <- artifact.TextDelta{Content: "Final answer"}
	}
	return nil
}

func TestManager_Process_ToolLoop_NoDuplicateTurns(t *testing.T) {
	prov := &toolLoopProvider{}
	store := NewMemoryStore()

	// Custom processor that simulates a ReAct-like tool loop:
	// 1. Turn (assistant with tool call)
	// 2. Emit tool result (simulating tool handler)
	// 3. Turn (assistant with final answer)
	toolProc := TurnProcessor(func(ctx context.Context, step *loop.Step, st ledger.State, prov provider.Provider, _ models.Spec) (ledger.State, error) {
		// First assistant turn (tool call)
		spec := models.Spec{Name: "test-model"}
		st, err := step.Turn(ctx, st, spec, prov)
		if err != nil {
			return st, err
		}

		// Simulate tool handler emitting tool result
		step.SetEventContext(ctx)
		step.Emit(ctx, loop.TurnCompleteEvent{
			Turn: ledger.Turn{
				Role:      ledger.RoleTool,
				Artifacts: []artifact.Artifact{artifact.ToolResult{Content: "tool result"}},
			},
			Ctx: ctx,
		})

		// Second assistant turn (final answer)
		return step.Turn(ctx, st, spec, prov)
	})

	mgr := NewManager(store, prov, func(stream *Stream) ([]loop.Option, error) {
		return nil, nil
	}, toolProc)

	stream, err := mgr.Create()
	require.NoError(t, err)

	err = stream.Process(context.Background(), UserMessageEvent{Content: "hi"})
	require.NoError(t, err)
	_ = stream.Close()

	thr, err := store.Get(stream.ID())
	require.NoError(t, err)
	turns := thr.State.Turns()

	// Expected: user, assistant-tool-call, tool-result, assistant-final
	require.Len(t, turns, 4, "expected 4 turns: user + assistant(tool) + tool + assistant(final)")

	roles := make([]ledger.Role, len(turns))
	for i, turn := range turns {
		roles[i] = turn.Role
	}
	assert.Equal(t, []ledger.Role{ledger.RoleUser, ledger.RoleAssistant, ledger.RoleTool, ledger.RoleAssistant}, roles)

	// Verify no duplicates by checking each role appears the expected number of times
	roleCounts := make(map[ledger.Role]int)
	for _, turn := range turns {
		roleCounts[turn.Role]++
	}
	assert.Equal(t, 1, roleCounts[ledger.RoleUser])
	assert.Equal(t, 2, roleCounts[ledger.RoleAssistant])
	assert.Equal(t, 1, roleCounts[ledger.RoleTool])
}
