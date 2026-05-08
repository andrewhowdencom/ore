package main

import (
	"fmt"
	"strings"

	"github.com/andrewhowdencom/tack/state"
	"github.com/charmbracelet/lipgloss"
)

var (
	// assistantStyle styles assistant output in a subtle blue.
	assistantStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#6C8EBF"))
	// statusStyle styles the status line faint and italic.
	statusStyle = lipgloss.NewStyle().Faint(true).Italic(true)
)

// View renders the conversation history, streaming buffer, status line,
// and input prompt.
func (m *Model) View() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	var b strings.Builder

	// Render conversation history.
	for _, turn := range m.turns {
		switch turn.role {
		case state.RoleUser:
			b.WriteString("You: ")
			b.WriteString(turn.text)
		case state.RoleAssistant:
			b.WriteString(assistantStyle.Render("Assistant: "))
			b.WriteString(turn.text)
		case state.RoleTool:
			b.WriteString("Tool: ")
			b.WriteString(turn.text)
		}
		b.WriteString("\n\n")
	}

	// Render the in-progress streaming response.
	if m.streamBuffer.Len() > 0 {
		b.WriteString(assistantStyle.Render("Assistant: "))
		b.WriteString(m.streamBuffer.String())
		b.WriteString("\n\n")
	}

	// Render status line.
	if m.status != "" {
		b.WriteString(statusStyle.Render(fmt.Sprintf("[%s]", m.status)))
		b.WriteString("\n")
	}

	// Render input prompt with cursor.
	b.WriteString("> ")
	b.WriteString(m.input.String())
	b.WriteString("_")

	return b.String()
}
