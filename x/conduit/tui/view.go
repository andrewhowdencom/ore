package tui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/state"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/cellbuf"
)

var (
	// assistantStyle styles assistant output in a subtle blue.
	assistantStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#6C8EBF"))
	// statusStyle styles the status line faint and italic.
	statusStyle = lipgloss.NewStyle().Faint(true).Italic(true)
	// thinkingStyle styles reasoning/thinking content faint and italic.
	thinkingStyle = lipgloss.NewStyle().Faint(true).Italic(true)
	// reasoningExpandedStyle styles the full reasoning content body when
	// expanded, making it visually subdued so it does not look like normal
	// assistant text.
	reasoningExpandedStyle = lipgloss.NewStyle().Faint(true)
	// toolCallStyle styles tool call notifications faint and italic.
	toolCallStyle = lipgloss.NewStyle().Faint(true).Italic(true)
	// toolErrorStyle styles tool error output in red.
	toolErrorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5555"))
	// compactToolCallStyle styles compact tool call lines in amber.
	compactToolCallStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#D19A66"))
	// compactToolResultStyle styles compact tool result lines in muted green.
	compactToolResultStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#7EC699"))
	// compactToolErrorStyle styles compact tool error lines in red.
	compactToolErrorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5555"))
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

	width := m.viewport.Width()

	// Find the last assistant turn index.
	lastAssistantIdx := -1
	for i, turn := range m.turns {
		if turn.role == state.RoleAssistant {
			lastAssistantIdx = i
		}
	}

	// Render conversation history.
	for turnIdx, turn := range m.turns {
		isLatestAssistant := turnIdx == lastAssistantIdx
		isAfterLatestAssistant := turnIdx > lastAssistantIdx
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
					isExpanded := isLatestAssistant && m.expandLatestDetails
					if !isExpanded {
						b.WriteString(thinkingStyle.Render("Thinking..."))
					} else {
						if block.rendered != "" {
							b.WriteString(renderBlock("Thinking: ", thinkingStyle, reasoningExpandedStyle.Render(block.rendered), 0))
						} else {
							b.WriteString(renderBlock("Thinking: ", thinkingStyle, reasoningExpandedStyle.Render(block.source), width))
						}
					}
				case "tool_call":
					isExpanded := isLatestAssistant && m.expandLatestDetails
					content := block.compact
					if content == "" || isExpanded {
						content = block.source
					}
					if block.compact != "" && !isExpanded {
						b.WriteString(compactToolCallStyle.Render("→ " + content))
					} else {
						b.WriteString(renderBlock("Assistant: ", toolCallStyle, content, width))
					}
				}
				if i < len(turn.blocks)-1 {
					b.WriteString("\n\n")
				}
			}
		case state.RoleTool:
			for i, block := range turn.blocks {
				switch block.kind {
				case "text":
					b.WriteString(renderBlock("Tool: ", lipgloss.NewStyle(), block.source, width))
				case "tool_result":
					isExpanded := isAfterLatestAssistant && m.expandLatestDetails
					content := block.compact
					if content == "" || isExpanded {
						content = block.source
					}
					if block.compact != "" && !isExpanded {
						if strings.HasPrefix(block.source, "Error: ") {
							b.WriteString(compactToolErrorStyle.Render("← " + content))
						} else {
							b.WriteString(compactToolResultStyle.Render("← " + content))
						}
					} else {
						style := lipgloss.NewStyle()
						if strings.HasPrefix(block.source, "Error: ") {
							style = toolErrorStyle
						}
						b.WriteString(renderBlock("Tool: ", style, content, width))
					}
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

// compactToolCall formats a tool call into a compact single-line string.
// The compact format keeps the TUI readable within limited width by
// collapsing verbose JSON arguments into key=value pairs.
// It parses the JSON arguments and emits name · key="val" · key2=42.
// Nested objects collapse to {…} and arrays to […]. If JSON parsing
// fails, it falls back to a truncated raw representation.
// maxWidth is normally the current viewport width; callers ensure it
// reflects the available horizontal space.
func compactToolCall(tc artifact.ToolCall, maxWidth int) string {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(tc.Arguments), &args); err != nil {
		raw := tc.Name + "(" + tc.Arguments + ")"
		return truncateString(raw, maxWidth)
	}

	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := []string{tc.Name}
	for _, key := range keys {
		val := args[key]
		switch v := val.(type) {
		case string:
			parts = append(parts, fmt.Sprintf("%s=%q", key, v))
		case float64:
			if v == float64(int64(v)) {
				parts = append(parts, fmt.Sprintf("%s=%d", key, int64(v)))
			} else {
				parts = append(parts, fmt.Sprintf("%s=%v", key, v))
			}
		case bool:
			parts = append(parts, fmt.Sprintf("%s=%v", key, v))
		case map[string]interface{}:
			parts = append(parts, fmt.Sprintf("%s={…}", key))
		case []interface{}:
			parts = append(parts, fmt.Sprintf("%s=[…]", key))
		default:
			parts = append(parts, fmt.Sprintf("%s=%v", key, v))
		}
	}

	result := strings.Join(parts, " · ")
	return truncateString(result, maxWidth)
}

// compactToolResult formats a tool result into a compact single-line string,
// keeping the TUI concise by truncating at the first newline or maxWidth
// characters. If the result is an error (IsError == true), the prefix
// "Error: " is added before truncation so the compact line still signals
// a failure. maxWidth is normally the current viewport width.
func compactToolResult(tr artifact.ToolResult, maxWidth int) string {
	content := tr.Content
	if idx := strings.Index(content, "\n"); idx != -1 {
		content = content[:idx]
	}
	prefix := ""
	if tr.IsError {
		prefix = "Error: "
	}
	return truncateString(prefix+content, maxWidth)
}

// truncateString truncates s to maxWidth runes, adding "…" if truncated.
// Truncation is rune-aware, ensuring multi-byte Unicode characters are
// not split mid-character.
func truncateString(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxWidth {
		return s
	}
	if maxWidth <= 1 {
		return "…"
	}
	return string(runes[:maxWidth-1]) + "…"
}

// View renders the conversation history inside a scrollable viewport and
// anchors the input prompt at the bottom of the terminal.
func (m *model) View() tea.View {
	m.viewport.SetContent(m.buildContent())

	// Render a thin horizontal line to visually separate the conversation
	// history (viewport) from the input area at the bottom of the terminal.
	var separator string
	if m.width > 0 {
		separator = strings.Repeat("─", m.width)
	}

	view := m.viewport.View()
	if view != "" {
		v := tea.NewView(view + "\n" + separator + "\n" + m.textarea.View())
	v.AltScreen = true
	return v
	}
	v := tea.NewView(m.textarea.View())
	v.AltScreen = true
	return v
}
