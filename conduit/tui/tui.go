// Package tui implements an opinionated terminal user interface conduit for
// the ore framework using Bubble Tea.
//
// Use New(sess) to create a TUI that composes with a session.Session. The
// TUI subscribes to the session's output stream and sends user events back
// through it.
package tui

import (
	"context"
	"log/slog"

	"github.com/andrewhowdencom/ore/conduit"
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
	eventsCh chan conduit.Event
	program  *tea.Program
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

// New creates a new TUI conduit that composes with a session.Session.
// It subscribes to the session's output stream and sends user events back
// through the session. The application should not read from the internal
// events channel; the TUI manages the event loop internally.
func New(sess session.Session) *TUI {
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
	t := &TUI{
		eventsCh: surfEventsCh,
		program:  p,
	}

	// Subscribe to the session's output stream.
	outputCh, err := sess.Subscribe("turn_complete")
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
			case conduit.UserMessageEvent:
				if err := t.SetStatus(context.Background(), "thinking..."); err != nil {
					slog.Error("set status failed", "err", err)
				}
				if err := sess.Process(context.Background(), e); err != nil {
					slog.Error("process failed", "err", err)
					t.program.Send(clearPendingMsg{})
				}
				if err := t.SetStatus(context.Background(), ""); err != nil {
					slog.Error("set status failed", "err", err)
				}
			case conduit.InterruptEvent:
				if err := sess.Cancel(); err != nil {
					slog.Error("cancel failed", "err", err)
				}
			}
		}
	}()

	return t
}

// SetStatus sends a status update into the Bubble Tea message loop. The
// actual state mutation happens in model.Update.
func (t *TUI) SetStatus(ctx context.Context, status string) error {
	t.program.Send(statusMsg{status: status})
	return nil
}

// Run starts the TUI and blocks until the user quits.
// It closes the events channel on return so that background goroutines
// can exit cleanly.
func (t *TUI) Run() error {
	_, err := t.program.Run()
	close(t.eventsCh)
	return err
}
