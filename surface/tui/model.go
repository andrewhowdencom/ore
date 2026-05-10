package tui

import (
	"log/slog"
	"strings"


	"github.com/andrewhowdencom/tack/artifact"
	"github.com/andrewhowdencom/tack/state"
	"github.com/andrewhowdencom/tack/surface"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// deltaMsg carries an ephemeral delta artifact into the Bubble Tea message
// loop so model.Update can append it to the appropriate streaming buffer
// (text or reasoning).
type deltaMsg struct {
	delta artifact.Artifact
}

// turnMsg carries a complete turn into the Bubble Tea message loop so
// model.Update can finalize it in the conversation history.
type turnMsg struct {
	turn state.Turn
}

// statusMsg carries a status update into the Bubble Tea message loop so
// model.Update can update the transient status line.
type statusMsg struct {
	status string
}



// model implements tea.Model. All state mutation happens in Update,
// which runs on Bubble Tea's single goroutine, so no locks are needed.
type model struct {
	eventsCh chan surface.Event

	// Conversation history.
	turns []renderedTurn

	// textStreamBuffer holds the partial text content of the current assistant response.
	textStreamBuffer strings.Builder

	// reasoningStreamBuffer holds the partial reasoning/thinking content.
	reasoningStreamBuffer strings.Builder



	// Transient status line (e.g., "thinking...").
	status string

	// User input buffer.
	input strings.Builder

	// Terminal dimensions.
	width  int
	height int

	// Scrollable viewport for conversation history.
	viewport viewport.Model

	// md renders Markdown source into ANSI-styled terminal output. In
	// production this is a glamourMarkdownRenderer; tests may inject a mock.
	md markdownRenderer
}

// renderMarkdown delegates to the model's markdown renderer, falling back
// to a default glamourMarkdownRenderer if none was injected.
func (m *model) renderMarkdown(text string, width int) (string, error) {
	if m.md == nil {
		m.md = glamourMarkdownRenderer{}
	}
	return m.md.Render(text, width)
}

// renderedTurn represents a single turn in the conversation history.
type renderedTurn struct {
	role     state.Role
	text     string // original text (Markdown source for assistant turns)
	rendered string // pre-rendered ANSI output (only for assistant turns)
}

// Init returns an initial command. No periodic ticks are needed because
// deltas arrive via program.Send from the orchestrator goroutine.
func (m *model) Init() tea.Cmd {
	return nil
}

// Update handles incoming messages: keyboard input, window resize, and
// custom messages carrying delta/turn/status data from the surface methods.
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case deltaMsg:
		switch d := msg.delta.(type) {
		case artifact.TextDelta:
			m.textStreamBuffer.WriteString(d.Content)
		case artifact.ReasoningDelta:
			m.reasoningStreamBuffer.WriteString(d.Content)
		}
		m.viewport.GotoBottom()
	case turnMsg:
		var text strings.Builder
		for _, art := range msg.turn.Artifacts {
			if t, ok := art.(artifact.Text); ok {
				text.WriteString(t.Content)
			}
		}
		rt := renderedTurn{
			role: msg.turn.Role,
			text: text.String(),
		}
		if msg.turn.Role == state.RoleAssistant {
			rendered, err := m.renderMarkdown(text.String(), m.viewport.Width)
			if err == nil {
				rt.rendered = rendered
			}
		}
		m.turns = append(m.turns, rt)
		m.textStreamBuffer.Reset()
		m.reasoningStreamBuffer.Reset()
		m.viewport.GotoBottom()
	case statusMsg:
		m.status = msg.status
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyPgUp, tea.KeyPgDown:
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		case tea.KeyEnter:
			if m.input.Len() > 0 {
				content := m.input.String()
				m.turns = append(m.turns, renderedTurn{
					role: state.RoleUser,
					text: content,
				})
				m.input.Reset()
				select {
				case m.eventsCh <- surface.UserMessageEvent{Content: content}:
				default:
					slog.Warn("event channel full, dropping user message")
				}
			}
		case tea.KeyCtrlC:
			select {
			case m.eventsCh <- surface.InterruptEvent{}:
			default:
			}
			return m, tea.Quit
		case tea.KeyBackspace:
			s := m.input.String()
			if len(s) > 0 {
				runes := []rune(s)
				m.input.Reset()
				m.input.WriteString(string(runes[:len(runes)-1]))
			}
		case tea.KeySpace:
			m.input.WriteString(" ")
		case tea.KeyRunes:
			m.input.WriteString(string(msg.Runes))
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.Width = msg.Width
		m.viewport.Height = msg.Height - 1
		// Re-render assistant turns with the new terminal width so cached
		// Markdown output remains correctly wrapped.
		for i, turn := range m.turns {
			if turn.role == state.RoleAssistant && turn.text != "" {
				rendered, err := m.renderMarkdown(turn.text, m.viewport.Width)
				if err == nil {
					m.turns[i].rendered = rendered
				}
			}
		}
	}
	return m, nil
}
