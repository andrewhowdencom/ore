// Package tui implements an opinionated terminal user interface conduit for
// the ore framework using Bubble Tea.
//
// Use New(mgr, opts...) to create a TUI that composes with a session.Manager.
// The TUI creates or attaches to a session on Start, subscribes to the
// session's output stream, and sends user events back through it.
// Available options include WithThreadID to resume an existing thread.
package tui

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/andrewhowdencom/ore/x/conduit"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/session"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// TUI is a terminal user interface conduit. It hides all Bubble Tea internals
// from callers.
type TUI struct {
	mgr      *session.Manager
	threadID string
	eventsCh chan conduit.Event
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
var Descriptor = conduit.Descriptor{
	Name:        "TUI",
	Description: "Terminal user interface via Bubble Tea",
	Capabilities: []conduit.Capability{
		conduit.CapEventSource,
		conduit.CapShowStatus,
		conduit.CapRenderTurn,
		conduit.CapRenderMarkdown,
	},
}

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

	surfEventsCh := make(chan conduit.Event, 10)

	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.Prompt = "> "
	// Note: bubbletea v1.3.10 does not support Shift modifier detection.
	// Alt+Enter is used as the practical alternative for newline insertion.
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("alt+enter"))
	ta.Focus()

	m := model{
		eventsCh: surfEventsCh,
		viewport: viewport.New(0, 0),
		textarea: ta,
		md:       newGlamourMarkdownRenderer(),
	}
	p := tea.NewProgram(&m, tea.WithAltScreen())
	t.eventsCh = surfEventsCh
	t.program = p

	// Subscribe to the stream's output.
	outputCh, err := stream.Subscribe("turn_complete")
	if err != nil {
		slog.Error("failed to subscribe to session output", "err", err)
	}

	// Goroutine to stream output events into the Bubble Tea message loop.
	if err == nil {
		go func() {
			for event := range outputCh {
				switch e := event.(type) {
				case loop.TurnCompleteEvent:
					t.program.Send(turnMsg{turn: e.Turn})
				case loop.ErrorEvent:
					// Errors are exposed via status updates rather than the
					// message loop; the application goroutine handles them.
				}
			}
		}()
	}

	// Goroutine to process user events through the session.
	go func() {
		for event := range t.eventsCh {
			switch e := event.(type) {
			case session.UserMessageEvent:
				if err := t.SetStatus(context.Background(), "thinking..."); err != nil {
					slog.Error("set status failed", "err", err)
				}
				if err := stream.Process(context.Background(), e); err != nil {
					slog.Error("process failed", "err", err)
					t.program.Send(clearPendingMsg{})
				}
				if err := t.SetStatus(context.Background(), ""); err != nil {
					slog.Error("set status failed", "err", err)
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

// SetStatus sends a status update into the Bubble Tea message loop. The
// actual state mutation happens in model.Update.
func (t *TUI) SetStatus(ctx context.Context, status string) error {
	t.program.Send(statusMsg{status: status})
	return nil
}
