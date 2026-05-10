package tui

import (
	"fmt"
	"strings"

	"github.com/andrewhowdencom/tack/state"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/cellbuf"
)

var (
	// assistantStyle styles assistant output in a subtle blue.
	assistantStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#6C8EBF"))
	// statusStyle styles the status line faint and italic.
	statusStyle = lipgloss.NewStyle().Faint(true).Italic(true)
	// thinkingStyle styles reasoning/thinking content faint and italic.
	thinkingStyle = lipgloss.NewStyle().Faint(true).Italic(true)
)

// wrapText wraps text to fit within the given terminal width, prefixing the
// first line with label and subsequent lines with indent. It is Unicode and
// ANSI aware.
func wrapText(text, label, indent string, width int) string {
	if width <= 0 || text == "" {
		return label + text
	}
	labelWidth := lipgloss.Width(label)
	available := width - labelWidth
	if available <= 1 {
		return label + text
	}
	wrapped := cellbuf.Wrap(text, available, " ")
	lines := strings.Split(wrapped, "\n")
	var b strings.Builder
	for i, line := range lines {
		if i == 0 {
			b.WriteString(label)
		} else {
			b.WriteString("\n")
			b.WriteString(indent)
		}
		b.WriteString(line)
	}
	return b.String()
}

// prefixLines prepends label to the first line and indent to every
// subsequent line of text. It does not re-wrap text; the caller is
// responsible for ensuring each line already fits within the desired width.
func prefixLines(text, label, indent string) string {
	if text == "" {
		return label + text
	}
	lines := strings.Split(text, "\n")
	var b strings.Builder
	for i, line := range lines {
		if i == 0 {
			b.WriteString(label)
		} else {
			b.WriteString("\n")
			b.WriteString(indent)
		}
		b.WriteString(line)
	}
	return b.String()
}

// View renders the conversation history inside a scrollable viewport and
// anchors the input prompt at the bottom of the terminal.
func (m *model) View() string {
	var b strings.Builder

	width := m.viewport.Width

	userLabel := "You: "
	userIndent := strings.Repeat(" ", lipgloss.Width(userLabel))

	assistantLabel := assistantStyle.Render("Assistant: ")
	assistantIndent := strings.Repeat(" ", lipgloss.Width(assistantLabel))

	toolLabel := "Tool: "
	toolIndent := strings.Repeat(" ", lipgloss.Width(toolLabel))

	// Render conversation history.
	for _, turn := range m.turns {
		switch turn.role {
		case state.RoleUser:
			b.WriteString(wrapText(turn.text, userLabel, userIndent, width))
		case state.RoleAssistant:
			if turn.rendered != "" {
				b.WriteString(prefixLines(turn.rendered, assistantLabel, assistantIndent))
			} else {
				b.WriteString(wrapText(turn.text, assistantLabel, assistantIndent, width))
			}
		case state.RoleTool:
			b.WriteString(wrapText(turn.text, toolLabel, toolIndent, width))
		}
		b.WriteString("\n\n")
	}

	// Render the in-progress text stream.
	if m.textStreamBuffer.Len() > 0 {
		b.WriteString(wrapText(m.textStreamBuffer.String(), assistantLabel, assistantIndent, width))
		b.WriteString("\n\n")
	}

	// Render the in-progress reasoning stream.
	if m.reasoningStreamBuffer.Len() > 0 {
		thinkingLabel := thinkingStyle.Render("Thinking: ")
		thinkingIndent := strings.Repeat(" ", lipgloss.Width(thinkingLabel))
		b.WriteString(wrapText(m.reasoningStreamBuffer.String(), thinkingLabel, thinkingIndent, width))
		b.WriteString("\n\n")
	}

	// Render status line.
	if m.status != "" {
		b.WriteString(statusStyle.Render(fmt.Sprintf("[%s]", m.status)))
		b.WriteString("\n")
	}

	m.viewport.SetContent(b.String())

	// Render input prompt as a fixed line at the bottom.
	inputLine := "> " + m.input.String() + "_"

	view := m.viewport.View()
	if view != "" {
		return view + "\n" + inputLine
	}
	return inputLine
}
