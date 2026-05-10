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

// renderStreamWithCursor wraps stream text with its label and indent, then
// appends a blinking cursor to the last non-empty line when active.
func renderStreamWithCursor(text, label, indent, cursor string, width int) string {
	wrapped := wrapText(text, label, indent, width)
	if cursor == "" {
		return wrapped
	}
	lines := strings.Split(wrapped, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			if lipgloss.Width(lines[i])+lipgloss.Width(cursor) <= width {
				lines[i] = lines[i] + cursor
			} else {
				lines = append(lines, indent+cursor)
			}
			break
		}
	}
	return strings.Join(lines, "\n")
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
			b.WriteString(wrapText(turn.text, assistantLabel, assistantIndent, width))
		case state.RoleTool:
			b.WriteString(wrapText(turn.text, toolLabel, toolIndent, width))
		}
		b.WriteString("\n\n")
	}

	// Render the in-progress streaming response.
	cursor := ""
	if m.streaming && m.cursorVisible {
		cursor = "▌"
	}

	if m.textStreamBuffer.Len() > 0 {
		b.WriteString(renderStreamWithCursor(
			m.textStreamBuffer.String(),
			assistantLabel,
			assistantIndent,
			cursor,
			width,
		))
		b.WriteString("\n\n")
	}

	if m.reasoningStreamBuffer.Len() > 0 {
		thinkingLabel := thinkingStyle.Render("Thinking: ")
		thinkingIndent := strings.Repeat(" ", lipgloss.Width(thinkingLabel))
		b.WriteString(renderStreamWithCursor(
			m.reasoningStreamBuffer.String(),
			thinkingLabel,
			thinkingIndent,
			cursor,
			width,
		))
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
