package tui

import (
	"fmt"
	"strings"

	"github.com/andrewhowdencom/ore/state"
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

// renderBlock renders a labeled content block with the label on its own line
// and content starting at column 0. If width > 0, content is wrapped to fit.
func renderBlock(label string, labelStyle lipgloss.Style, content string, width int) string {
	styledLabel := labelStyle.Render(label)
	if content == "" {
		return styledLabel
	}
	if width > 0 {
		content = cellbuf.Wrap(content, width, " ")
	}
	return styledLabel + "\n" + content
}

// buildContent constructs the full conversation string for the viewport,
// including all turns, the pending placeholder, and the status line.
//
// This helper was extracted from View() so that Update() can refresh the
// viewport content before calling GotoBottom(), fixing a timing bug where
// auto-scroll operated on stale content height and hid newly-rendered output.
func (m *model) buildContent() string {
	var b strings.Builder

	width := m.viewport.Width

	// Render conversation history.
	for _, turn := range m.turns {
		switch turn.role {
		case state.RoleUser:
			for i, block := range turn.blocks {
				if block.kind == "text" {
					b.WriteString(renderBlock("You: ", lipgloss.NewStyle(), block.source, width))
				}
				if i < len(turn.blocks)-1 {
					b.WriteString("\n\n")
				}
			}
		case state.RoleAssistant:
			for i, block := range turn.blocks {
				switch block.kind {
				case "text":
					if block.rendered != "" {
						b.WriteString(renderBlock("Assistant: ", assistantStyle, block.rendered, 0))
					} else {
						b.WriteString(renderBlock("Assistant: ", assistantStyle, block.source, width))
					}
				// Reasoning blocks are rendered through the same Markdown pipeline
				// as text blocks; the rendered ANSI is cached in renderedBlock.rendered.
				case "reasoning":
					if block.rendered != "" {
						b.WriteString(renderBlock("Thinking: ", thinkingStyle, block.rendered, 0))
					} else {
						b.WriteString(renderBlock("Thinking: ", thinkingStyle, block.source, width))
					}
				}
				if i < len(turn.blocks)-1 {
					b.WriteString("\n\n")
				}
			}
		case state.RoleTool:
			for i, block := range turn.blocks {
				if block.kind == "text" {
					b.WriteString(renderBlock("Tool: ", lipgloss.NewStyle(), block.source, width))
				}
				if i < len(turn.blocks)-1 {
					b.WriteString("\n\n")
				}
			}
		}
		b.WriteString("\n\n")
	}

	// Render pending placeholder.
	if m.pending {
		b.WriteString(renderBlock("Assistant: ", assistantStyle, "...", width))
		b.WriteString("\n\n")
	}

	// Render status line.
	if m.status != "" {
		b.WriteString(statusStyle.Render(fmt.Sprintf("[%s]", m.status)))
		b.WriteString("\n")
	}

	return b.String()
}

// View renders the conversation history inside a scrollable viewport and
// anchors the input prompt at the bottom of the terminal.
func (m *model) View() string {
	m.viewport.SetContent(m.buildContent())

	// Render a thin horizontal line to visually separate the conversation
	// history (viewport) from the input area at the bottom of the terminal.
	var separator string
	if m.width > 0 {
		separator = strings.Repeat("─", m.width)
	}

	view := m.viewport.View()
	if view != "" {
		return view + "\n" + separator + "\n" + m.textarea.View()
	}
	return m.textarea.View()
}
