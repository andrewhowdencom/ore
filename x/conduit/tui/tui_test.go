package tui

import (
	"context"
	"github.com/andrewhowdencom/ore/models"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/junk"
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

func (m *mockProvider) Invoke(ctx context.Context, s state.State, _ models.Spec, ch chan<- artifact.Artifact, opts ...provider.InvokeOption) error {
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
func simpleProcessor() junk.TurnProcessor {
	return func(ctx context.Context, step *loop.Step, st state.State, prov provider.Provider, _ models.Spec) (state.State, error) {
		spec := models.Spec{Name: "test-model"}
		return step.Turn(ctx, st, spec, prov)
	}
}

func TestNew(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())

	c, err := New(mgr)
	require.NoError(t, err)
	require.NotNil(t, c)
}

func TestNew_WithThreadID(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())

	c, err := New(mgr, WithThreadID("test-thread-id"))
	require.NoError(t, err)
	require.NotNil(t, c)
}

func TestStart_AttachNotFound(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())

	c, err := New(mgr, WithThreadID("nonexistent"))
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err = c.Start(ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "nonexistent")
}

// TestStart_ReachesEventLoop pins a regression introduced by commit
// cf88e66, where TUI.Start invoked t.program.Send before p.Run(),
// blocking the main goroutine inside Program.Send and preventing the
// Bubble Tea event loop from ever starting. The test asserts that
// Start returns within a short window after the context is cancelled,
// which is only possible if the event loop reached the ctx-cancellation
// goroutine and called p.Quit().
//
// Setup notes:
//   - WithDefaultMetadata populates the stream with metadata so the
//     status-bar seed path is exercised. Without metadata, statusFromStream
//     returns nil and the broken Send is never reached.
//   - WithProgramOptions(tea.WithoutRenderer(), tea.WithoutSignals(),
//     tea.WithInput(nil)) runs the Bubble Tea program in non-interactive
//     mode so the test does not require a TTY (this environment has no
//     /dev/tty). WithoutRenderer skips the output renderer; WithoutSignals
//     suppresses OS signal handling; WithInput(nil) disables input,
//     which is what prevents Bubble Tea from trying to open /dev/tty for
//     a fallback input reader when os.Stdin is not a terminal.
//
// This is a liveness test, not a correctness test: it does not inspect
// the model's status. The seed-wiring correctness is covered by
// TestInitModel_SeedsStatusFromStream and TestInit_DispatchesSeedCmd.
func TestStart_ReachesEventLoop(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor(),
		junk.WithDefaultMetadata(func(*junk.Stream) map[string]string {
			return map[string]string{
				"thread_id":  "test-thread",
				"cwd":        "/tmp",
				"git_branch": "main",
				"tui.pid":    "9999",
			}
		}),
	)

	c, err := New(mgr, WithProgramOptions(tea.WithoutRenderer(), tea.WithoutSignals(), tea.WithInput(nil)))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- c.Start(ctx) }()

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err, "Start should return cleanly when context is cancelled")
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return within 2s after context cancellation; " +
			"the Bubble Tea event loop likely never started")
	}
}

func TestNew_WithName(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())

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
	store := junk.NewMemoryStore()
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "assistant response"},
		},
	}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())

	// Create a stream and have a conversation.
	stream, err := mgr.Create()
	require.NoError(t, err)
	err = stream.Process(context.Background(), junk.UserMessageEvent{Content: "hello"})
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
	eventsCh := make(chan junk.Event, 10)
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

func TestStatusFromStream(t *testing.T) {
	t.Run("returns a statusMsg carrying the stream's current metadata", func(t *testing.T) {
		store := junk.NewMemoryStore()
		prov := &mockProvider{}
		mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())

		stream, err := mgr.Create()
		require.NoError(t, err)
		t.Cleanup(func() { _ = stream.Close() })

		// Seed metadata that mirrors what Manager.applyDefaultMetadata
		// produces in a real session (plus a workshop-style role key).
		stream.SetMetadata("thread_id", "abc-123")
		stream.SetMetadata("cwd", "/tmp/ore")
		stream.SetMetadata("git_branch", "main")
		stream.SetMetadata("tui.pid", "9999")
		stream.SetMetadata("workshop.role", "context")

		msg := statusFromStream(stream)
		require.NotNil(t, msg, "statusFromStream must return a message when metadata is present")

		sm, ok := msg.(statusMsg)
		require.True(t, ok, "expected a statusMsg, got %T", msg)
		assert.Equal(t, "abc-123", sm.status["thread_id"])
		assert.Equal(t, "/tmp/ore", sm.status["cwd"])
		assert.Equal(t, "main", sm.status["git_branch"])
		assert.Equal(t, "9999", sm.status["tui.pid"])
		assert.Equal(t, "context", sm.status["workshop.role"])
		assert.Len(t, sm.status, 5)
	})

	t.Run("returns nil when the stream has no metadata", func(t *testing.T) {
		store := junk.NewMemoryStore()
		prov := &mockProvider{}
		mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())

		stream, err := mgr.Create()
		require.NoError(t, err)
		t.Cleanup(func() { _ = stream.Close() })

		msg := statusFromStream(stream)
		assert.Nil(t, msg, "statusFromStream must return nil so Start skips a no-op Send")
	})

	t.Run("returns a defensive copy that the caller can mutate freely", func(t *testing.T) {
		store := junk.NewMemoryStore()
		prov := &mockProvider{}
		mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())

		stream, err := mgr.Create()
		require.NoError(t, err)
		t.Cleanup(func() { _ = stream.Close() })

		stream.SetMetadata("thread_id", "abc")

		first := statusFromStream(stream).(statusMsg)
		first.status["injected"] = "evil"
		delete(first.status, "thread_id")

		// A second call must return the original keys untouched.
		second := statusFromStream(stream).(statusMsg)
		assert.Equal(t, "abc", second.status["thread_id"])
		_, leaked := second.status["injected"]
		assert.False(t, leaked)
	})
}

// TestInitModel_SeedsStatusFromStream asserts that initModel populates
// the model's initStatusMsg field from the stream's current metadata.
// Init() will later yield this message as a tea.Cmd so the existing
// statusMsg handler can merge it into m.status through the normal
// message channel after the event loop has started. The test exercises
// both the populated-metadata and empty-metadata branches of
// statusFromStream.
func TestInitModel_SeedsStatusFromStream(t *testing.T) {
	t.Run("populates initStatusMsg with a statusMsg carrying the stream's metadata", func(t *testing.T) {
		store := junk.NewMemoryStore()
		prov := &mockProvider{}
		mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor(),
			junk.WithDefaultMetadata(func(*junk.Stream) map[string]string {
				return map[string]string{
					"thread_id": "abc-123",
					"cwd":       "/tmp/ore",
				}
			}),
		)

		stream, err := mgr.Create()
		require.NoError(t, err)
		t.Cleanup(func() { _ = stream.Close() })

		tui := &TUI{mgr: mgr, name: "test"}
		eventsCh := make(chan junk.Event, 10)
		m := tui.initModel(eventsCh, stream)

		require.NotNil(t, m.initStatusMsg, "initModel must seed initStatusMsg when metadata is present")
		sm, ok := m.initStatusMsg.(statusMsg)
		require.True(t, ok, "expected a statusMsg, got %T", m.initStatusMsg)
		assert.Equal(t, "abc-123", sm.status["thread_id"])
		assert.Equal(t, "/tmp/ore", sm.status["cwd"])
	})

	t.Run("leaves initStatusMsg nil when the stream has no metadata", func(t *testing.T) {
		store := junk.NewMemoryStore()
		prov := &mockProvider{}
		mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())

		stream, err := mgr.Create()
		require.NoError(t, err)
		t.Cleanup(func() { _ = stream.Close() })

		tui := &TUI{mgr: mgr, name: "test"}
		eventsCh := make(chan junk.Event, 10)
		m := tui.initModel(eventsCh, stream)

		assert.Nil(t, m.initStatusMsg, "initModel must leave initStatusMsg nil when there is no metadata")
	})
}

// TestInit_DispatchesSeedCmd asserts that m.Init() returns a tea.Cmd
// that yields the seed statusMsg, and returns nil when no seed is
// present. This is the wiring that routes the status-bar seed through
// the event loop's normal message channel — replacing the previous
// (broken) t.program.Send call in Start that ran before p.Run().
func TestInit_DispatchesSeedCmd(t *testing.T) {
	t.Run("returns a Cmd that yields the seed statusMsg", func(t *testing.T) {
		store := junk.NewMemoryStore()
		prov := &mockProvider{}
		mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor(),
			junk.WithDefaultMetadata(func(*junk.Stream) map[string]string {
				return map[string]string{
					"thread_id": "abc-123",
					"cwd":       "/tmp/ore",
				}
			}),
		)

		stream, err := mgr.Create()
		require.NoError(t, err)
		t.Cleanup(func() { _ = stream.Close() })

		tui := &TUI{mgr: mgr, name: "test"}
		eventsCh := make(chan junk.Event, 10)
		m := tui.initModel(eventsCh, stream)

		cmd := m.Init()
		require.NotNil(t, cmd, "Init must return a non-nil Cmd when a seed is present")

		msg := cmd()
		sm, ok := msg.(statusMsg)
		require.True(t, ok, "the Cmd must yield a statusMsg, got %T", msg)
		assert.Equal(t, "abc-123", sm.status["thread_id"])
		assert.Equal(t, "/tmp/ore", sm.status["cwd"])
	})

	t.Run("returns nil when no seed is present", func(t *testing.T) {
		store := junk.NewMemoryStore()
		prov := &mockProvider{}
		mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())

		stream, err := mgr.Create()
		require.NoError(t, err)
		t.Cleanup(func() { _ = stream.Close() })

		tui := &TUI{mgr: mgr, name: "test"}
		eventsCh := make(chan junk.Event, 10)
		m := tui.initModel(eventsCh, stream)

		assert.Nil(t, m.Init(), "Init must return nil when no seed is present")
	})
}
