package main

import (
	"log/slog"

	"github.com/andrewhowdencom/tack/state"
	"github.com/andrewhowdencom/tack/surface"
	tea "github.com/charmbracelet/bubbletea"
)

// deltaMsg signals that a delta artifact arrived and the view should re-render.
type deltaMsg struct{}

// turnMsg signals that a complete turn arrived and the view should re-render.
type turnMsg struct{}

// statusMsg signals that the status line changed and the view should re-render.
type statusMsg struct{}

// Init returns an initial command. No periodic ticks are needed because the
// model triggers re-renders explicitly via program.Send when deltas arrive.
func (m *Model) Init() tea.Cmd {
	return nil
}

// Update handles incoming messages: keyboard input, window resize, and
// custom messages from the orchestrator worker goroutine.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEnter:
			m.mu.Lock()
			if m.input.Len() > 0 {
				content := m.input.String()
				m.turns = append(m.turns, renderedTurn{
					role: state.RoleUser,
					text: content,
				})
				m.input.Reset()
				m.mu.Unlock()
				select {
				case m.eventsCh <- surface.UserMessageEvent{Content: content}:
				default:
					slog.Warn("event channel full, dropping user message")
				}
			} else {
				m.mu.Unlock()
			}
		case tea.KeyCtrlC:
			select {
			case m.eventsCh <- surface.InterruptEvent{}:
			default:
			}
			return m, tea.Quit
		case tea.KeyBackspace:
			m.mu.Lock()
			s := m.input.String()
			if len(s) > 0 {
				runes := []rune(s)
				m.input.Reset()
				m.input.WriteString(string(runes[:len(runes)-1]))
			}
			m.mu.Unlock()
		case tea.KeySpace:
			m.mu.Lock()
			m.input.WriteString(" ")
			m.mu.Unlock()
		case tea.KeyRunes:
			m.mu.Lock()
			m.input.WriteString(string(msg.Runes))
			m.mu.Unlock()
		}
	case tea.WindowSizeMsg:
		m.mu.Lock()
		m.width = msg.Width
		m.height = msg.Height
		m.mu.Unlock()
	case deltaMsg, turnMsg, statusMsg:
		// State was already mutated by the Surface method that sent this
		// message; we just need to return so Bubble Tea calls View() again.
	}
	return m, nil
}
