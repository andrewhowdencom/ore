// Package tui implements an opinionated terminal user interface conduit for
// the ore framework using Bubble Tea.
//
// Two constructors are provided:
//
//	New(eventsCh)                   - use when you already have a loop.Step
//	                                  output channel and want to manage the
//	                                  event loop yourself.
//
//	NewWithManager(mgr, threadID)   - use for the common case where the TUI
//	                                  should manage its own subscription and
//	                                  send user events back through the manager.
//
// The TUI satisfies conduit.Conduit (via Capable and Events).
package tui

import (
	"context"
	"log/slog"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/conduit"
	"github.com/andrewhowdencom/ore/session"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// TUI is a terminal user interface conduit. It satisfies conduit.Conduit
// (via Capable and Events) and hides all Bubble Tea internals from callers.
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
		conduit.CapRenderDelta,
		conduit.CapRenderTurn,
		conduit.CapRenderMarkdown,
	},
}

// New creates a new TUI conduit with an initialized events channel and
// Bubble Tea program configured with the alternate screen buffer.
// The TUI reads artifacts and turn completion events from the provided
// channel and routes them into the Bubble Tea message loop.
func New(eventsCh <-chan loop.OutputEvent) *TUI {
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
		md:       glamourMarkdownRenderer{},
	}
	p := tea.NewProgram(&m, tea.WithAltScreen())
	t := &TUI{
		eventsCh: surfEventsCh,
		program:  p,
	}

	go func() {
		for event := range eventsCh {
			switch e := event.(type) {
			case loop.TurnCompleteEvent:
				t.program.Send(turnMsg{turn: e.Turn})
			case loop.ErrorEvent:
				// Errors are exposed via status updates rather than the
				// message loop; the application goroutine handles them.
			case artifact.Artifact:
				t.program.Send(deltaMsg{delta: e})
			}
		}
	}()

	return t
}

// Events returns a read-only channel of user-generated events.
func (t *TUI) Events() <-chan conduit.Event {
	return t.eventsCh
}

// Capabilities returns the full list of capabilities this TUI provides.
func (t *TUI) Capabilities() []conduit.Capability {
	return Descriptor.Capabilities
}

// Can reports whether the TUI supports a specific capability.
func (t *TUI) Can(cap conduit.Capability) bool {
	for _, c := range Descriptor.Capabilities {
		if c == cap {
			return true
		}
	}
	return false
}

// NewWithManager creates a new TUI conduit that composes with a
// session.Manager. It subscribes to the manager's output stream for the
// given thread and sends user events back through the manager. The
// application should not read from Events() when using this constructor;
// the TUI manages the event loop internally.
func NewWithManager(mgr *session.Manager, threadID string) *TUI {
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
		md:       glamourMarkdownRenderer{},
	}
	p := tea.NewProgram(&m, tea.WithAltScreen())
	t := &TUI{
		eventsCh: surfEventsCh,
		program:  p,
	}

	// Subscribe to the manager's output stream for this thread.
	outputCh, err := mgr.Subscribe(threadID, "text_delta", "reasoning_delta", "tool_call_delta", "turn_complete")
	if err != nil {
		// Session may not exist yet; attach and retry.
		_, _ = mgr.Attach(threadID)
		outputCh, err = mgr.Subscribe(threadID, "text_delta", "reasoning_delta", "tool_call_delta", "turn_complete")
		if err != nil {
			slog.Error("failed to subscribe to manager output", "err", err)
		}
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
				case artifact.Artifact:
					t.program.Send(deltaMsg{delta: e})
				}
			}
		}()
	}

	// Goroutine to process user events through the manager.
	go func() {
		for event := range t.eventsCh {
			switch e := event.(type) {
			case conduit.UserMessageEvent:
				if err := t.SetStatus(context.Background(), "thinking..."); err != nil {
					slog.Error("set status failed", "err", err)
				}
				if err := mgr.Process(context.Background(), threadID, e); err != nil {
					slog.Error("process failed", "err", err)
				}
				if err := t.SetStatus(context.Background(), ""); err != nil {
					slog.Error("set status failed", "err", err)
				}
			case conduit.InterruptEvent:
				if err := mgr.Cancel(threadID); err != nil {
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
