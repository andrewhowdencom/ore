// Package tui implements an opinionated terminal user interface surface for
// the ore framework using Bubble Tea.
package tui

import (
	"context"

	"github.com/andrewhowdencom/ore/artifact"
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
// The TUI reads artifacts and turn completion events from the provided
// channel and routes them into the Bubble Tea message loop.
func New(eventsCh <-chan loop.OutputEvent) *TUI {
	surfEventsCh := make(chan surface.Event, 10)
	m := model{
		eventsCh: surfEventsCh,
		viewport: viewport.New(0, 0),
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
			case artifact.Artifact:
				t.program.Send(deltaMsg{delta: e})
			case loop.TurnCompleteEvent:
				t.program.Send(turnMsg{turn: e.Turn})
			case loop.ErrorEvent:
				// Errors are surfaced via status updates rather than the
				// message loop; the application goroutine handles them.
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
