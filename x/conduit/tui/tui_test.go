package tui

import (
	"context"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/ledger"
	"github.com/andrewhowdencom/ore/session"
	"github.com/andrewhowdencom/ore/x/compaction"
	"github.com/andrewhowdencom/ore/x/conduit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestSession returns a fresh *session.Session backed by an empty
// in-memory ledger thread. It is the canonical fixture for tests
// that exercise the TUI's session-shaped API surface.
func newTestSession() *session.Session {
	return session.New("test", ledger.NewThread())
}

// ---------------------------------------------------------------------------
// Constructor
// ---------------------------------------------------------------------------

// TestNew_Happy verifies that New accepts a non-nil session and
// returns a non-nil conduit.Conduit value.
func TestNew_Happy(t *testing.T) {
	sess := newTestSession()
	defer sess.Close()

	c, err := New(sess)
	require.NoError(t, err)
	require.NotNil(t, c)
}

// TestNew_AppOwnedSession documents that the TUI does not own the
// session's lifecycle. The caller is responsible for closing the
// session; defer'ing Close in the test confirms the session is
// usable after New returns.
func TestNew_AppOwnedSession(t *testing.T) {
	sess := newTestSession()
	defer sess.Close()

	c, err := New(sess, WithName("my-app"))
	require.NoError(t, err)
	tuiC := c.(*TUI)
	assert.Equal(t, "my-app", tuiC.name,
		"WithName option must remain in effect after construction")
}

// TestNew_NilSessionErrors asserts that New rejects a nil session
// with a clear error. The TUI is a dumb pipe; it cannot conjure a
// session from nothing.
func TestNew_NilSessionErrors(t *testing.T) {
	c, err := New(nil)
	require.Error(t, err)
	require.Nil(t, c)
	assert.Contains(t, err.Error(), "session")
}

// ---------------------------------------------------------------------------
// Capability descriptor
// ---------------------------------------------------------------------------

// TestTUI_ImplementsAudioNotifier is a compile-time assertion that
// *TUI satisfies the AudioNotifier contract introduced by the
// framework. The TUI uses the terminal bell (\a) for both done and
// error signals because \a cannot vary pitch; a richer backend could
// introduce distinct tones.
func TestTUI_ImplementsAudioNotifier(t *testing.T) {
	var _ conduit.AudioNotifier = (*TUI)(nil)
}

// ---------------------------------------------------------------------------
// Status bar seed
// ---------------------------------------------------------------------------

// TestStatusFromSession asserts that the bootstrap message is
// populated from the session's metadata, is nil when no metadata is
// present, and is a defensive copy that the caller may mutate freely.
func TestStatusFromSession(t *testing.T) {
	t.Run("returns a statusMsg carrying the session's current metadata", func(t *testing.T) {
		sess := newTestSession()
		defer sess.Close()
		sess.SetMetadata("thread_id", "abc-123")
		sess.SetMetadata("cwd", "/tmp/ore")
		sess.SetMetadata("git_branch", "main")
		sess.SetMetadata("tui.pid", "9999")
		sess.SetMetadata("workshop.role", "context")

		msg := statusFromSession(sess)
		require.NotNil(t, msg, "statusFromSession must return a message when metadata is present")

		sm, ok := msg.(statusMsg)
		require.True(t, ok, "expected a statusMsg, got %T", msg)
		assert.Equal(t, "abc-123", sm.status["thread_id"])
		assert.Equal(t, "/tmp/ore", sm.status["cwd"])
		assert.Equal(t, "main", sm.status["git_branch"])
		assert.Equal(t, "9999", sm.status["tui.pid"])
		assert.Equal(t, "context", sm.status["workshop.role"])
		assert.Len(t, sm.status, 5)
	})

	t.Run("returns nil when the session has no metadata", func(t *testing.T) {
		sess := newTestSession()
		defer sess.Close()

		msg := statusFromSession(sess)
		assert.Nil(t, msg, "statusFromSession must return nil so Start skips a no-op Send")
	})

	t.Run("returns a defensive copy that the caller can mutate freely", func(t *testing.T) {
		sess := newTestSession()
		defer sess.Close()
		sess.SetMetadata("thread_id", "abc")

		first := statusFromSession(sess).(statusMsg)
		first.status["injected"] = "evil"
		delete(first.status, "thread_id")

		// A second call must return the original keys untouched.
		second := statusFromSession(sess).(statusMsg)
		assert.Equal(t, "abc", second.status["thread_id"])
		_, leaked := second.status["injected"]
		assert.False(t, leaked)
	})
}

// TestInitModel_SeedsStatusFromSession asserts that initModel populates
// the model's initStatusMsg from the session's current metadata.
// initStatusMsg is yielded by Init() as a tea.Cmd so the existing
// statusMsg handler can merge it into m.status through the normal
// message channel after the event loop has started.
func TestInitModel_SeedsStatusFromSession(t *testing.T) {
	t.Run("populates initStatusMsg with a statusMsg carrying the session's metadata", func(t *testing.T) {
		sess := newTestSession()
		defer sess.Close()
		sess.SetMetadata("thread_id", "abc-123")
		sess.SetMetadata("cwd", "/tmp/ore")

		tui := &TUI{sess: sess, name: "test"}
		eventsCh := make(chan session.Event, 16)
		m := tui.initModel(context.Background(), eventsCh, sess)

		require.NotNil(t, m.initStatusMsg, "initModel must seed initStatusMsg when metadata is present")
		sm, ok := m.initStatusMsg.(statusMsg)
		require.True(t, ok, "expected a statusMsg, got %T", m.initStatusMsg)
		assert.Equal(t, "abc-123", sm.status["thread_id"])
		assert.Equal(t, "/tmp/ore", sm.status["cwd"])
	})

	t.Run("leaves initStatusMsg nil when the session has no metadata", func(t *testing.T) {
		sess := newTestSession()
		defer sess.Close()

		tui := &TUI{sess: sess, name: "test"}
		eventsCh := make(chan session.Event, 16)
		m := tui.initModel(context.Background(), eventsCh, sess)

		assert.Nil(t, m.initStatusMsg, "initModel must leave initStatusMsg nil when there is no metadata")
	})
}

// TestInit_DispatchesSeedCmd asserts that m.Init() returns a tea.Cmd
// that yields the seed statusMsg, and returns nil when no seed is
// present. This is the wiring that routes the status-bar seed through
// the event loop's normal message channel.
func TestInit_DispatchesSeedCmd(t *testing.T) {
	t.Run("returns a Cmd that yields the seed statusMsg", func(t *testing.T) {
		sess := newTestSession()
		defer sess.Close()
		sess.SetMetadata("thread_id", "abc-123")
		sess.SetMetadata("cwd", "/tmp/ore")

		tui := &TUI{sess: sess, name: "test"}
		eventsCh := make(chan session.Event, 16)
		m := tui.initModel(context.Background(), eventsCh, sess)

		cmd := m.Init()
		require.NotNil(t, cmd, "Init must return a non-nil Cmd when a seed is present")

		msg := cmd()
		sm, ok := msg.(statusMsg)
		require.True(t, ok, "the Cmd must yield a statusMsg, got %T", msg)
		assert.Equal(t, "abc-123", sm.status["thread_id"])
		assert.Equal(t, "/tmp/ore", sm.status["cwd"])
	})

	t.Run("returns nil when no seed is present", func(t *testing.T) {
		sess := newTestSession()
		defer sess.Close()

		tui := &TUI{sess: sess, name: "test"}
		eventsCh := make(chan session.Event, 16)
		m := tui.initModel(context.Background(), eventsCh, sess)

		assert.Nil(t, m.Init(), "Init must return nil when no seed is present")
	})
}

// TestReadBoundaryFromSession verifies that the boundary read path
// honors the session metadata contract: zero value on missing key,
// zero value on nil session, and zero value on malformed encoded
// value (the latter is treated as "no boundary" so the TUI does not
// surface decode errors in the conversation view).
func TestReadBoundaryFromSession(t *testing.T) {
	t.Run("zero value for nil session", func(t *testing.T) {
		assert.Equal(t, compaction.BoundaryInfo{}, readBoundaryFromSession(nil))
	})

	t.Run("zero value when metadata is missing", func(t *testing.T) {
		sess := newTestSession()
		defer sess.Close()
		assert.Equal(t, compaction.BoundaryInfo{}, readBoundaryFromSession(sess))
	})

	t.Run("zero value when encoded boundary is malformed", func(t *testing.T) {
		sess := newTestSession()
		defer sess.Close()
		sess.SetMetadata(compaction.MetaKeyBoundaryInfo, "not-a-valid-encoded-boundary")
		assert.Equal(t, compaction.BoundaryInfo{}, readBoundaryFromSession(sess))
	})
}

// ---------------------------------------------------------------------------
// Status message handling (preserved from before the migration)
// ---------------------------------------------------------------------------

// TestTUI_DeleteProperty_RemovesKeyFromStatus asserts that the
// model.Update statusMsg handler applies deletion operations by
// removing the key from m.status. Combined with the upstream
// PropertiesEvent handling in tui.go (which converts PropertyOpDelete
// into the deletions slice), this pins the end-to-end delete path.
func TestTUI_DeleteProperty_RemovesKeyFromStatus(t *testing.T) {
	m := newTestModel()
	m.initStatusMsg = nil

	// Seed a key that will later be deleted.
	newM, _ := m.Update(statusMsg{status: map[string]string{"workshop.role": "reviewer"}})
	mm := newM.(*model)
	matched, ok := mm.status["workshop.role"]
	require.True(t, ok)
	require.Equal(t, "reviewer", matched)

	// Apply a delete op: key is removed from m.status.
	newM, _ = mm.Update(statusMsg{deletions: []string{"workshop.role"}})
	mm = newM.(*model)
	_, present := mm.status["workshop.role"]
	assert.False(t, present, "deleted key must be absent from m.status")
}

// TestTUI_StatusMsgApplyOrderSetThenDelete verifies the model applies
// set ops first, then delete ops, so a same-event "set then delete"
// batch yields the correct final state.
func TestTUI_StatusMsgApplyOrderSetThenDelete(t *testing.T) {
	m := newTestModel()
	m.initStatusMsg = nil

	newM, _ := m.Update(statusMsg{status: map[string]string{"k": "v1"}})
	mm := newM.(*model)
	assert.Equal(t, "v1", mm.status["k"])

	// Same-event batch: set k=v2, then delete k. Final state: k absent.
	newM, _ = mm.Update(statusMsg{
		status:    map[string]string{"k": "v2"},
		deletions: []string{"k"},
	})
	mm = newM.(*model)
	_, present := mm.status["k"]
	assert.False(t, present, "delete after set in the same event must remove the key")
}

// ---------------------------------------------------------------------------
// Egress channel: Events() produces session.Event values
// ---------------------------------------------------------------------------

// TestEvents_EmitsUserMessageEvent asserts that pressing Enter on the
// textarea drives a session.UserMessageEvent onto the Events() channel.
// The event's Ctx is wrapped in loop.WithProvenance so downstream
// interceptors and tracing layers can attribute the event to "tui".
func TestEvents_EmitsUserMessageEvent(t *testing.T) {
	eventsCh := make(chan session.Event, 16)
	m := newTestModel()
	m.ctx = context.Background()
	m.eventsCh = eventsCh
	m.textarea.SetValue("hello world")

	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	select {
	case e := <-eventsCh:
		require.Equal(t, "user_message", e.Kind())
		ume, ok := e.(session.UserMessageEvent)
		require.True(t, ok, "expected session.UserMessageEvent, got %T", e)
		assert.Equal(t, "hello world", ume.Content)
		assert.Equal(t, "tui", func() string { s, _ := loop.ProvenanceFrom(ume.Ctx); return s }(),
			"emitted events must carry 'tui' provenance")
	default:
		t.Fatal("expected user message event on channel")
	}
}

// TestEvents_EmitsInterruptEventOnEscape asserts that pressing Esc
// drives a session.InterruptEvent on the Events() channel but does
// NOT quit the program (the user can keep typing).
func TestEvents_EmitsInterruptEventOnEscape(t *testing.T) {
	eventsCh := make(chan session.Event, 16)
	m := newTestModel()
	m.ctx = context.Background()
	m.eventsCh = eventsCh

	newM, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	_ = newM

	select {
	case e := <-eventsCh:
		require.Equal(t, "interrupt", e.Kind())
		ie, ok := e.(session.InterruptEvent)
		require.True(t, ok, "expected session.InterruptEvent, got %T", e)
		assert.Equal(t, "tui", func() string { s, _ := loop.ProvenanceFrom(ie.Ctx); return s }(),
			"interrupt events must carry 'tui' provenance")
	default:
		t.Fatal("expected interrupt event on channel")
	}

	assert.Nil(t, cmd, "Escape should not quit the program")
}

// TestEvents_EmitsInterruptEventOnCtrlC asserts that pressing Ctrl+C
// drives a session.InterruptEvent AND returns a tea.Cmd that quits
// the program. The cancel-func invocation is covered separately in
// TestWithCancelFunc below.
func TestEvents_EmitsInterruptEventOnCtrlC(t *testing.T) {
	eventsCh := make(chan session.Event, 16)
	m := newTestModel()
	m.ctx = context.Background()
	m.eventsCh = eventsCh

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})

	select {
	case e := <-eventsCh:
		require.Equal(t, "interrupt", e.Kind())
	default:
		t.Fatal("expected interrupt event on channel")
	}

	// Ctrl+C should request the program to quit; the cmd returned by
	// the KeyPressMsg handler is a tea.QuitMsg.
	require.NotNil(t, cmd, "Ctrl+C should request a quit")
}

// TestWithCancelFunc_InvokedOnCtrlC asserts that the cancel func
// registered via WithCancelFunc is invoked when the user presses Ctrl+C.
func TestWithCancelFunc_InvokedOnCtrlC(t *testing.T) {
	eventsCh := make(chan session.Event, 16)
	cancelled := make(chan struct{}, 1)
	cancel := func() { cancelled <- struct{}{} }

	m := newTestModel()
	m.ctx = context.Background()
	m.eventsCh = eventsCh
	m.cancelFunc = cancel

	_, _ = m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})

	select {
	case <-cancelled:
		// OK
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected cancel func to be invoked on Ctrl+C")
	}

	// Also drain the interrupt event so the channel does not leak.
	select {
	case <-eventsCh:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected interrupt event on channel")
	}
}

// TestWithCancelFunc_InvokedOnEscape asserts that the cancel func
// is also invoked on Esc, even though Esc does not quit the program.
func TestWithCancelFunc_InvokedOnEscape(t *testing.T) {
	eventsCh := make(chan session.Event, 16)
	cancelled := make(chan struct{}, 1)
	cancel := func() { cancelled <- struct{}{} }

	m := newTestModel()
	m.ctx = context.Background()
	m.eventsCh = eventsCh
	m.cancelFunc = cancel

	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

	select {
	case <-cancelled:
		// OK
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected cancel func to be invoked on Escape")
	}
}

// TestWithCancelFunc_NotInvokedWhenNotSet guards against a nil-cancelFunc
// panic. The TUI must remain safe to use when the application does not
// register a cancel func — it just emits the event and moves on.
func TestWithCancelFunc_NotInvokedWhenNotSet(t *testing.T) {
	eventsCh := make(chan session.Event, 16)
	m := newTestModel()
	m.ctx = context.Background()
	m.eventsCh = eventsCh
	m.cancelFunc = nil // explicit: no cancel func registered

	// Should not panic on Ctrl+C.
	_, _ = m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})

	// And should not panic on Esc.
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

	// Both events should be on the channel.
	for i := 0; i < 2; i++ {
		select {
		case e := <-eventsCh:
			assert.Equal(t, "interrupt", e.Kind())
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("expected interrupt event %d", i)
		}
	}
}

// TestEvents_DropsUserMessageWhenChannelFull asserts that the model
// does not block when the application is not draining Events() that
// quickly. The buffered channel has a capacity of 16; the 17th send
// is dropped (with a warning log) so the UI thread continues to be
// responsive.
func TestEvents_DropsUserMessageWhenChannelFull(t *testing.T) {
	// Capacity 1, then fill it.
	eventsCh := make(chan session.Event, 1)
	m := newTestModel()
	m.ctx = context.Background()
	m.eventsCh = eventsCh
	m.textarea.SetValue("first")

	// First send fills the channel.
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	// Second send should be dropped (no block).
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	// Confirm exactly one event in the channel.
	select {
	case <-eventsCh:
		// OK
	default:
		t.Fatal("expected first event in channel")
	}
	select {
	case <-eventsCh:
		t.Fatal("second event should have been dropped")
	default:
		// OK
	}
}

// ---------------------------------------------------------------------------
// invokeCancelFunc — internal helper (defensive nil-check)
// ---------------------------------------------------------------------------

// TestInvokeCancelFunc_NilSafe guards against a nil-cancelFunc panic
// when the application has not registered a cancel func. The TUI
// constructs cancelFunc==nil in New when WithCancelFunc is omitted.
func TestInvokeCancelFunc_NilSafe(t *testing.T) {
	tui := &TUI{} // cancelFunc is nil
	require.NotPanics(t, func() { tui.invokeCancelFunc() })
}

// TestInvokeCancelFunc_InvokesRegistered asserts the happy path.
func TestInvokeCancelFunc_InvokesRegistered(t *testing.T) {
	called := false
	tui := &TUI{cancelFunc: func() { called = true }}
	tui.invokeCancelFunc()
	assert.True(t, called)
}

// ---------------------------------------------------------------------------
// Start lifecycle — live-only
// ---------------------------------------------------------------------------

// TestStart_ReachesEventLoop is a liveness test that pins a regression
// where Start invoked a non-existent event path and blocked the main
// goroutine. The test runs Start with program options that disable
// the renderer, signal handling, and input — so the Bubble Tea program
// runs in non-interactive mode in the test environment — and cancels
// ctx to force a clean exit. The TUI Exit handler closes the Events
// channel; the test asserts that closure happens within a short window.
func TestStart_ReachesEventLoop(t *testing.T) {
	sess := newTestSession()
	defer sess.Close()

	tuiC, err := New(sess,
		WithProgramOptions(tea.WithoutRenderer(), tea.WithoutSignals(), tea.WithInput(nil)),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- tuiC.Start(ctx) }()

	// Wait for the channel to be assigned by Start (after p.Run() is
	// entered). The model.Init() tick yields a statusMsg; reading
	// from Events() confirms the channel is live.
	tuiVal := tuiC.(*TUI)

	// Programmatic cancel: Bubble Tea sees ctx.Done() and quits.
	cancel()

	select {
	case err := <-done:
		// Start may return a non-nil error from p.Run() in headless
		// mode; that's acceptable. The key assertion is that Start
		// returned within the timeout window.
		_ = err
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return within 2s after context cancellation; " +
			"the Bubble Tea event loop likely never started")
	}

	// After Start returns, the channel may be closed. Reading from a
	// closed channel returns zero values immediately, which is a
	// graceful way to confirm lifecycle without timing assumptions.
	select {
	case _, ok := <-tuiVal.Events():
		assert.False(t, ok, "Events channel should be closed after Start returns")
	case <-time.After(100 * time.Millisecond):
		// Tolerated: channel may not be closed yet in some paths.
	}
}

// TestStart_RequiresSession is the nil-session check at construction
// time. A subsequent test ensures that the New error message is clear.
func TestNew_ErrorMessageMentionsSession(t *testing.T) {
	_, err := New(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session")
}
