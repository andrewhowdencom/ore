// tui-chat is a reference application demonstrating a streaming chat REPL
// using the tack framework with Bubble Tea.
package main

import (
	"context"
	"strings"
	"sync"

	"github.com/andrewhowdencom/tack/artifact"
	"github.com/andrewhowdencom/tack/state"
	"github.com/andrewhowdencom/tack/surface"
	tea "github.com/charmbracelet/bubbletea"
)

// Model implements tea.Model and surface.Surface. It manages the conversation
// state, input buffer, streaming display, and status line. All state mutations
// are protected by a mutex because RenderDelta/RenderTurn/SetStatus are called
// from the orchestrator's worker goroutine, while View/Update are called from
// Bubble Tea's goroutine.
type Model struct {
	program *tea.Program
	mu      sync.Mutex

	// Conversation history.
	turns []renderedTurn

	// Streaming buffer for the current assistant response.
	streamBuffer strings.Builder

	// Transient status line (e.g., "thinking...").
	status string

	// User input buffer.
	input strings.Builder

	// Events channel consumed by the orchestrator.
	eventsCh chan surface.Event

	// Terminal dimensions.
	width  int
	height int
}

// renderedTurn represents a single turn in the conversation history.
type renderedTurn struct {
	role state.Role
	text string
}

// newModel creates a new TUI model with an initialized events channel.
func newModel() *Model {
	return &Model{
		eventsCh: make(chan surface.Event, 10),
	}
}

// SetProgram stores the Bubble Tea program reference so the model can trigger
// re-renders when deltas arrive from the orchestrator worker goroutine.
func (m *Model) SetProgram(p *tea.Program) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.program = p
}

// Events returns a read-only channel of user-generated events.
// The channel is owned by the model.
func (m *Model) Events() <-chan surface.Event {
	return m.eventsCh
}

// RenderDelta appends an ephemeral delta artifact to the streaming buffer
// and triggers a re-render via the Bubble Tea program.
func (m *Model) RenderDelta(ctx context.Context, delta artifact.Artifact) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch d := delta.(type) {
	case artifact.TextDelta:
		m.streamBuffer.WriteString(d.Content)
	case artifact.ReasoningDelta:
		m.streamBuffer.WriteString(d.Content)
	}

	if m.program != nil {
		m.program.Send(deltaMsg{})
	}
	return nil
}

// RenderTurn appends a complete turn to the conversation history, resets the
// streaming buffer, and triggers a re-render.
func (m *Model) RenderTurn(ctx context.Context, turn state.Turn) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var text strings.Builder
	for _, art := range turn.Artifacts {
		if t, ok := art.(artifact.Text); ok {
			text.WriteString(t.Content)
		}
	}

	m.turns = append(m.turns, renderedTurn{
		role: turn.Role,
		text: text.String(),
	})
	m.streamBuffer.Reset()

	if m.program != nil {
		m.program.Send(turnMsg{})
	}
	return nil
}

// SetStatus updates the transient status line and triggers a re-render.
func (m *Model) SetStatus(ctx context.Context, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status = status

	if m.program != nil {
		m.program.Send(statusMsg{})
	}
	return nil
}
