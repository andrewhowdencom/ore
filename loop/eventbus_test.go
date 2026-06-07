package loop

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEventBus_Emit_AppendsToStateBeforeOnEmit verifies that when a state is
// bound to the EventBus, TurnCompleteEvent is automatically appended to that
// state before OnEmit callbacks run.
func TestEventBus_Emit_AppendsToStateBeforeOnEmit(t *testing.T) {
	mem := &state.Buffer{}
	eb := newEventBus()
	eb.state = mem

	var onEmitCalled bool
	var turnsAtOnEmit int
	eb.onEmit = []OnEmit{func(ctx context.Context, event OutputEvent) {
		onEmitCalled = true
		turnsAtOnEmit = len(mem.Turns())
	}}

	eb.Emit(context.Background(), TurnCompleteEvent{
		Turn: state.Turn{Role: state.RoleAssistant, Artifacts: []artifact.Artifact{artifact.Text{Content: "hello"}}},
	})

	require.True(t, onEmitCalled, "OnEmit should have been called")
	assert.Equal(t, 1, turnsAtOnEmit, "state should have been appended before OnEmit callback")
	turns := mem.Turns()
	require.Len(t, turns, 1)
	assert.Equal(t, state.RoleAssistant, turns[0].Role)
}

// TestEventBus_Emit_OnEmitOrdering verifies that OnEmit callbacks run in
// registration order.
func TestEventBus_Emit_OnEmitOrdering(t *testing.T) {
	eb := newEventBus()
	var order []int
	eb.onEmit = []OnEmit{
		func(ctx context.Context, event OutputEvent) { order = append(order, 1) },
		func(ctx context.Context, event OutputEvent) { order = append(order, 2) },
		func(ctx context.Context, event OutputEvent) { order = append(order, 3) },
	}

	eb.Emit(context.Background(), LifecycleEvent{Phase: "submitted"})

	assert.Equal(t, []int{1, 2, 3}, order)
}

// TestEventBus_Emit_NonTurnCompleteEvent_DoesNotAppendToState verifies that
// only TurnCompleteEvent triggers state auto-append.
func TestEventBus_Emit_NonTurnCompleteEvent_DoesNotAppendToState(t *testing.T) {
	mem := &state.Buffer{}
	eb := newEventBus()
	eb.state = mem

	eb.Emit(context.Background(), ArtifactEvent{Artifact: artifact.Text{Content: "hello"}})
	eb.Emit(context.Background(), LifecycleEvent{Phase: "submitted"})
	eb.Emit(context.Background(), ErrorEvent{Err: errors.New("boom")})

	assert.Empty(t, mem.Turns())
}

// TestEventBus_Emit_WithoutState does not panic when Emit is called with no
// bound state.
func TestEventBus_Emit_WithoutState(t *testing.T) {
	eb := newEventBus()
	eb.Emit(context.Background(), TurnCompleteEvent{
		Turn: state.Turn{Role: state.RoleAssistant, Artifacts: []artifact.Artifact{artifact.Text{Content: "hello"}}},
	})
	// Should not panic.
}

// TestEventBus_Subscribe_ReturnsChannel verifies that Subscribe returns a
// receive-only channel that receives events matching the given kinds.
func TestEventBus_Subscribe_ReturnsChannel(t *testing.T) {
	eb := newEventBus()
	ch := eb.Subscribe("text_delta", "turn_complete")

	go func() {
		eb.Emit(context.Background(), ArtifactEvent{Artifact: artifact.TextDelta{Content: "hello"}})
		eb.Emit(context.Background(), TurnCompleteEvent{Turn: state.Turn{Role: state.RoleAssistant}})
	}()

	events := collectEvents(ch, 100*time.Millisecond)

	require.Len(t, events, 2)
	assert.Equal(t, "text_delta", events[0].Kind())
	assert.Equal(t, "turn_complete", events[1].Kind())
}

// TestEventBus_Close_ClosesChannels verifies that Close closes all subscriber
// channels.
func TestEventBus_Close_ClosesChannels(t *testing.T) {
	eb := newEventBus()
	ch := eb.Subscribe("text_delta")

	err := eb.Close()
	require.NoError(t, err)

	_, ok := <-ch
	assert.False(t, ok, "channel should be closed")
}

// TestEventBus_Close_Idempotent verifies that calling Close multiple times
// does not panic.
func TestEventBus_Close_Idempotent(t *testing.T) {
	eb := newEventBus()
	ch := eb.Subscribe("text_delta")

	err := eb.Close()
	require.NoError(t, err)

	// Second close should not panic.
	err = eb.Close()
	require.NoError(t, err)

	_, ok := <-ch
	assert.False(t, ok, "channel should still be closed")
}

// TestEventBus_Emit_ContextCancellation verifies that Emit respects context
// cancellation and does not block indefinitely when the context is already
// cancelled.
func TestEventBus_Emit_ContextCancellation(t *testing.T) {
	eb := newEventBus()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		eb.Emit(ctx, LifecycleEvent{Phase: "submitted"})
		close(done)
	}()

	select {
	case <-done:
		// Expected — Emit returned without blocking.
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Emit should have returned immediately for cancelled context")
	}
}

// TestEventBus_Emit_MultipleEvents verifies that multiple events can be
// emitted and received in order.
func TestEventBus_Emit_MultipleEvents(t *testing.T) {
	eb := newEventBus()
	ch := eb.Subscribe("lifecycle", "error")

	go func() {
		eb.Emit(context.Background(), LifecycleEvent{Phase: "submitted"})
		eb.Emit(context.Background(), ErrorEvent{Err: errors.New("boom")})
		eb.Emit(context.Background(), LifecycleEvent{Phase: "done"})
	}()

	events := collectEvents(ch, 100*time.Millisecond)

	require.Len(t, events, 3)
	assert.Equal(t, "lifecycle", events[0].Kind())
	assert.Equal(t, "error", events[1].Kind())
	assert.Equal(t, "lifecycle", events[2].Kind())
}
