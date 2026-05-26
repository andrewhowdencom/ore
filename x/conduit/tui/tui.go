// Package tui implements an opinionated terminal user interface conduit for
// the ore framework using Bubble Tea.
//
// Use New(mgr, opts...) to create a TUI that composes with a session.Manager.
// The TUI creates or attaches to a session on Start, subscribes to the
// session's output stream, and sends user events back through it.
// Available options include WithThreadID to resume an existing thread.
//
// Keyboard shortcuts:
//   Ctrl+O — toggle expansion of latest assistant turn's tool blocks
//            (compact by default; resets after each new turn)
//   Ctrl+C — quit
//   Shift+Enter — insert newline in the input box
package tui

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/andrewhowdencom/ore/x/conduit"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/session"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
)

// TUI is a terminal user interface conduit. It hides all Bubble Tea internals
// from callers.
type TUI struct {
	mgr      *session.Manager
	threadID string
	eventsCh chan session.Event
	program  *tea.Program
}

// Option configures a TUI.
type Option func(*TUI)

// WithThreadID sets the thread ID to resume when starting the TUI.
// An empty string means create a new session.
func WithThreadID(id string) Option {
	return func(t *TUI) {
		t.threadID = id
	}
}

// Descriptor enumerates the capabilities of the TUI conduit.
// CapAudioNotification is included because the TUI satisfies the
// AudioNotifier contract using the terminal bell (\a), the only
// universally-available sound in terminal environments.
var Descriptor = conduit.Descriptor{
	Name:        "TUI",
	Description: "Terminal user interface via Bubble Tea",
	Capabilities: []conduit.Capability{
		conduit.CapEventSource,
		conduit.CapShowStatus,
		conduit.CapRenderTurn,
		conduit.CapRenderMarkdown,
		conduit.CapAudioNotification,
	},
}

// Compile-time assertion that *TUI implements conduit.AudioNotifier.
var _ conduit.AudioNotifier = (*TUI)(nil)

// New creates a new TUI conduit that implements conduit.Conduit.
// The returned value must be started with Start(ctx) to run the interface.
// Available options: WithThreadID(id) to resume an existing thread.
func New(mgr *session.Manager, opts ...Option) (conduit.Conduit, error) {
	if mgr == nil {
		return nil, fmt.Errorf("session manager is required")
	}
	t := &TUI{mgr: mgr}
	for _, opt := range opts {
		opt(t)
	}
	return t, nil
}

// Start creates or attaches to a session, initializes the Bubble Tea program,
// subscribes to the session output stream, and blocks until the user quits
// (Ctrl+C) or ctx is cancelled. On context cancellation the program exits
// gracefully.
func (t *TUI) Start(ctx context.Context) error {
	var stream *session.Stream
	var err error
	if t.threadID != "" {
		stream, err = t.mgr.Attach(t.threadID)
		if err != nil {
			return fmt.Errorf("attach to thread %q: %w", t.threadID, err)
		}
	} else {
		stream, err = t.mgr.Create()
		if err != nil {
			return fmt.Errorf("create session: %w", err)
		}
		slog.Info("thread started", "id", stream.ID())
	}

	_ = stream.Emit(ctx, loop.StatusEvent{
		Status: map[string]string{"thread_id": stream.ID()},
		Ctx:    loop.EventContext{Provenance: "tui"},
	})

	surfEventsCh := make(chan session.Event, 10)

	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.Prompt = "> "
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("shift+enter"))
	ta.Focus()

	m := model{
		eventsCh: surfEventsCh,
		viewport: viewport.New(),
		textarea: ta,
		md:       newGlamourMarkdownRenderer(),
	}
	p := tea.NewProgram(&m)
	t.eventsCh = surfEventsCh
	t.program = p

	// Subscribe to the stream's output, including complete artifact kinds so
	// the TUI can render assistant content incrementally as each artifact
	// arrives, rather than waiting for TurnCompleteEvent.
	outputCh := stream.Subscribe("text", "reasoning", "tool_call", "tool_result", "turn_complete", "error", "process_complete", "status")

	// Goroutine to stream output events into the Bubble Tea message loop.
	go func() {
		for event := range outputCh {
			switch e := event.(type) {
			case loop.ArtifactEvent:
				t.program.Send(artifactMsg{artifact: e.Artifact})
			case loop.TurnCompleteEvent:
				t.program.Send(turnMsg{turn: e.Turn})
			case loop.ErrorEvent:
				t.program.Send(errorMsg{err: e.Err})
				_ = t.PlayError(ctx)
			case loop.ProcessCompleteEvent:
				if e.Err != nil {
					_ = t.PlayError(ctx)
				} else {
					_ = t.PlayDone(ctx)
				}
				t.program.Send(statusMsg{status: map[string]string{"state": ""}})
			case loop.StatusEvent:
				t.program.Send(statusMsg{status: e.Status})
			}
		}
	}()

	// Goroutine to process user events through the session.
	go func() {
		for event := range t.eventsCh {
			switch e := event.(type) {
			case session.UserMessageEvent:
				_ = stream.Emit(context.Background(), loop.StatusEvent{
					Status: map[string]string{"state": "thinking..."},
					Ctx:    loop.EventContext{Provenance: "tui"},
				})
				if err := stream.Submit(e); err != nil {
					slog.Error("submit failed", "err", err)
					t.program.Send(clearPendingMsg{})
				}
			case session.InterruptEvent:
				if err := stream.Cancel(); err != nil {
					slog.Error("cancel failed", "err", err)
				}
			}
		}
	}()

	// Goroutine to quit the program when the context is cancelled.
	go func() {
		<-ctx.Done()
		p.Quit()
	}()

	_, err = p.Run()
	close(t.eventsCh)
	return err
}

// PlayDone forwards an audio notification to the Bubble Tea model so the
// terminal bell is emitted on the UI goroutine. This preserves the
// single-threaded model and avoids race conditions with the event loop.
// The TUI uses the same bell for both done and error because \a cannot
// vary pitch; distinct tones require a richer audio backend.
func (t *TUI) PlayDone(ctx context.Context) error {
	t.program.Send(audioMsg{})
	return nil
}

// PlayError forwards an audio notification to the Bubble Tea model so the
// terminal bell is emitted on the UI goroutine. Because the terminal
// bell (\a) cannot vary pitch, the TUI produces the same sound for
// errors as for successful turns. A future richer backend could introduce
// distinct error tones.
func (t *TUI) PlayError(ctx context.Context) error {
	t.program.Send(audioMsg{})
	return nil
}
