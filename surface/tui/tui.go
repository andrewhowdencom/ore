// Package tui implements an opinionated terminal user interface surface for
// the ore framework using Bubble Tea.
package tui

import (
	"context"

	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/surface"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// TUI is a terminal user interface surface. It satisfies surface.Surface
// and hides all Bubble Tea internals from callers.
type TUI struct {
	eventsCh chan surface.Event
	program  *tea.Program
}

// New creates a new TUI surface with an initialized events channel and
// Bubble Tea program configured with the alternate screen buffer.
// The TUI subscribes to delta and turn_complete events from the provided
// FanOut and routes them into the Bubble Tea message loop.
func New(fanOut *loop.FanOut) *TUI {
	eventsCh := make(chan surface.Event, 10)
	m := model{
		eventsCh: eventsCh,
		viewport: viewport.New(0, 0),
		md:       glamourMarkdownRenderer{},
	}
	p := tea.NewProgram(&m, tea.WithAltScreen())
	t := &TUI{
		eventsCh: eventsCh,
		program:  p,
	}

	// Subscribe to delta and turn_complete events on a single channel to
	// preserve ordering across event types.
	ch := fanOut.Subscribe("delta", "turn_complete")
	go func() {
		for event := range ch {
			switch e := event.(type) {
			case loop.DeltaEvent:
				t.program.Send(deltaMsg{delta: e.Delta})
			case loop.TurnCompleteEvent:
				t.program.Send(turnMsg{turn: e.Turn})
			}
		}
	}()

	return t
}

// Events returns a read-only channel of user-generated events.
func (t *TUI) Events() <-chan surface.Event {
	return t.eventsCh
}

// SetStatus sends a status update into the Bubble Tea message loop. The
// actual state mutation happens in model.Update.
func (t *TUI) SetStatus(ctx context.Context, status string) error {
	t.program.Send(statusMsg{status: status})
	return nil
}

// Run starts the TUI and blocks until the user quits.
func (t *TUI) Run() error {
	_, err := t.program.Run()
	return err
}
