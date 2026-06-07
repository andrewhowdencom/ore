package tui

import (
	"context"
	"testing"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/session"
	"github.com/andrewhowdencom/ore/state"
	"github.com/andrewhowdencom/ore/x/conduit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
func simpleProcessor() session.TurnProcessor {
	return func(ctx context.Context, executor loop.TurnExecutor, st state.State, prov provider.Provider) (state.State, error) {
		return executor.Turn(ctx, st, prov)
	}
}

func TestNew(t *testing.T) {
	store := session.NewMemoryStore()
	prov := &mockProvider{}
	mgr := session.NewManager(store, prov, func(*session.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())

	c, err := New(mgr)
	require.NoError(t, err)
	require.NotNil(t, c)
}

func TestNew_WithThreadID(t *testing.T) {
	store := session.NewMemoryStore()
	prov := &mockProvider{}
	mgr := session.NewManager(store, prov, func(*session.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())

	c, err := New(mgr, WithThreadID("test-thread-id"))
	require.NoError(t, err)
	require.NotNil(t, c)
}

func TestStart_AttachNotFound(t *testing.T) {
	store := session.NewMemoryStore()
	prov := &mockProvider{}
	mgr := session.NewManager(store, prov, func(*session.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())

	c, err := New(mgr, WithThreadID("nonexistent"))
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err = c.Start(ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "nonexistent")
}

func TestNew_WithName(t *testing.T) {
	store := session.NewMemoryStore()
	prov := &mockProvider{}
	mgr := session.NewManager(store, prov, func(*session.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())

	c, err := New(mgr, WithName("my-app"))
	require.NoError(t, err)
	require.NotNil(t, c)

	// Verify the name was stored by accessing it through the concrete type.
	tui := c.(*TUI)
	assert.Equal(t, "my-app", tui.name)
}

func TestTUI_ImplementsAudioNotifier(t *testing.T) {
	var _ conduit.AudioNotifier = (*TUI)(nil)
}

func TestTUI_InitModel_ResumesThreadWithHistory(t *testing.T) {
	store := session.NewMemoryStore()
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "assistant response"},
		},
	}
	mgr := session.NewManager(store, prov, func(*session.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())

	// Create a stream and have a conversation.
	stream, err := mgr.Create()
	require.NoError(t, err)
	err = stream.Process(context.Background(), session.UserMessageEvent{Content: "hello"})
	require.NoError(t, err)

	// Close the stream so we can re-attach.
	threadID := stream.ID()
	err = stream.Close()
	require.NoError(t, err)

	// Create a new TUI targeting the existing thread.
	c, err := New(mgr, WithThreadID(threadID))
	require.NoError(t, err)
	tui := c.(*TUI)

	// Re-attach to get a fresh stream handle backed by the same thread.
	stream2, err := mgr.Attach(threadID)
	require.NoError(t, err)
	defer stream2.Close()

	// Verify the thread has historical turns.
	turns := stream2.Turns()
	require.Len(t, turns, 2)

	// Call initModel directly to verify history pre-population.
	eventsCh := make(chan session.Event, 10)
	m := tui.initModel(eventsCh, stream2)

	// The model should have both turns pre-populated.
	require.Len(t, m.turns, 2)
	assert.Equal(t, state.RoleUser, m.turns[0].role)
	require.Len(t, m.turns[0].blocks, 1)
	assert.Equal(t, "hello", m.turns[0].blocks[0].source)

	assert.Equal(t, state.RoleAssistant, m.turns[1].role)
	require.Len(t, m.turns[1].blocks, 1)
	assert.Equal(t, "assistant response", m.turns[1].blocks[0].source)
	assert.NotEmpty(t, m.turns[1].blocks[0].rendered, "assistant turn should be markdown rendered")
}
