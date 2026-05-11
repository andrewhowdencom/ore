// Package tui implements an opinionated terminal user interface surface for
// the ore framework using Bubble Tea.
package tui

import (
	"context"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/state"
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
func New() *TUI {
	eventsCh := make(chan surface.Event, 10)
	m := model{
		eventsCh: eventsCh,
		viewport: viewport.New(0, 0),
		md:       glamourMarkdownRenderer{},
	}
	p := tea.NewProgram(&m, tea.WithAltScreen())
	return &TUI{
		eventsCh: eventsCh,
		program:  p,
	}
}

// Events returns a read-only channel of user-generated events.
func (t *TUI) Events() <-chan surface.Event {
	return t.eventsCh
}

// RenderDelta sends a delta artifact into the Bubble Tea message loop for
// incremental rendering. The actual state mutation happens in model.Update.
func (t *TUI) RenderDelta(ctx context.Context, delta artifact.Artifact) error {
	t.program.Send(deltaMsg{delta: delta})
	return nil
}

// RenderTurn sends a complete turn into the Bubble Tea message loop for
// finalization in the conversation history. The actual state mutation happens
// in model.Update.
func (t *TUI) RenderTurn(ctx context.Context, turn state.Turn) error {
	t.program.Send(turnMsg{turn: turn})
	return nil
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
