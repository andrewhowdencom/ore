package tui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

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
	// errorStyle styles error turns from the harness in red.
	errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5555"))
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

// buildContent constructs the full conversation string for the viewport.
// It includes all turns and the pending placeholder, but NOT the status
// line (which is now rendered as a fixed bar below the input area).
//
// The result is memoized: when contentDirty is false and cachedContent is
// non-empty, the cached string is returned immediately without recomputing.
// Callers that mutate visual state (turns, pending, expandLatestDetails)
// must set contentDirty = true before the next buildContent call so the
// cache is rebuilt. In practice Update() does this via syncViewport().
func (m *model) buildContent() string {
	if !m.contentDirty && m.cachedContent != "" {
		return m.cachedContent
	}

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
						b.WriteString(thinkingStyle.Render(thinkingCompact(block.source)))
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
		case state.RoleSystem:
			for i, block := range turn.blocks {
				if block.kind == "error" {
					b.WriteString(renderBlock("From: System, Message: ", errorStyle, block.source, width))
				}
				if i < len(turn.blocks)-1 {
					b.WriteString("\n\n")
				}
			}
		}
		b.WriteString("\n\n")
	}

	// Render the in-progress assistant turn accumulated from ArtifactEvents.
	if len(m.currentTurn.blocks) > 0 {
		for i, block := range m.currentTurn.blocks {
			switch block.kind {
			case "text":
				if block.rendered != "" {
					b.WriteString(renderBlock("Assistant: ", assistantStyle, block.rendered, 0))
				} else {
					b.WriteString(renderBlock("Assistant: ", assistantStyle, block.source, width))
				}
			case "reasoning":
				if !m.expandLatestDetails {
					isActive := (i == len(m.currentTurn.blocks)-1 && block.kind == "reasoning")
					if isActive {
						// Use rune count for user-visible "chars" metric.
						b.WriteString(thinkingStyle.Render(fmt.Sprintf("Thinking · %d Chars", utf8.RuneCountInString(block.source))))
					} else {
						b.WriteString(thinkingStyle.Render(thinkingCompact(block.source)))
					}
				} else {
					if block.rendered != "" {
						b.WriteString(renderBlock("Thinking: ", thinkingStyle, reasoningExpandedStyle.Render(block.rendered), 0))
					} else {
						b.WriteString(renderBlock("Thinking: ", thinkingStyle, reasoningExpandedStyle.Render(block.source), width))
					}
				}
			case "tool_call":
				content := block.compact
				if content == "" || m.expandLatestDetails {
					content = block.source
				}
				if block.compact != "" && !m.expandLatestDetails {
					b.WriteString(compactToolCallStyle.Render("→ " + content))
				} else {
					b.WriteString(renderBlock("Assistant: ", toolCallStyle, content, width))
				}
			}
			if i < len(m.currentTurn.blocks)-1 {
				b.WriteString("\n\n")
			}
		}
		b.WriteString("\n\n")
	}

	// Render pending placeholder only when no artifacts have arrived yet.
	if m.pending && len(m.currentTurn.blocks) == 0 {
		b.WriteString(renderBlock("Assistant: ", assistantStyle, "...", width))
		b.WriteString("\n\n")
	}

	m.cachedContent = b.String()
	m.contentDirty = false
	return m.cachedContent
}

// buildStatusLine renders the status map into a single wrapped line using
// "key: value · key: value" format with ANSI styling.
//
// It returns the rendered string and the number of display lines it
// occupies at the given width. Returns 0 lines when all values are empty.
func buildStatusLine(status map[string]string, width int) (string, int) {
	if len(status) == 0 {
		return "", 0
	}
	var keys []string
	for k := range status {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		if v := status[k]; v != "" {
			parts = append(parts, fmt.Sprintf("%s: %s", k, v))
		}
	}
	if len(parts) == 0 {
		return "", 0
	}
	rendered := statusStyle.Render(strings.Join(parts, " · "))
	if width <= 0 {
		return rendered, 1
	}
	wrapped := cellbuf.Wrap(rendered, width, " ")
	lines := strings.Count(wrapped, "\n") + 1
	return wrapped, lines
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

// compactToolResult formats a pre-rendered tool result string into a compact
// single-line representation, keeping the TUI concise by truncating at the
// first newline or maxWidth characters. The caller is responsible for
// pre-rendering (e.g. via MarkdownString) and adding an "Error: " prefix
// when appropriate. maxWidth is normally the current viewport width.
func compactToolResult(content string, maxWidth int) string {
	if idx := strings.Index(content, "\n"); idx != -1 {
		content = content[:idx]
	}
	return truncateString(content, maxWidth)
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

// thinkingCompact returns the compact single-line representation for a
// reasoning block, showing the rune count so users can gauge how much
// reasoning was performed.
func thinkingCompact(source string) string {
	return fmt.Sprintf("Thinking · %d Chars", utf8.RuneCountInString(source))
}

// windowTitle returns a dynamic terminal window title based on the
// current lifecycle phase. It uses ASCII bracket indicators so users
// can distinguish session state when multiple ore clients are open in
// tmux or a terminal multiplexer.
func (m *model) windowTitle() string {
	phase := m.status["phase"]
	base := m.name
	if t := m.status["title"]; t != "" {
		base = t
	}
	switch phase {
	case "submitted", "streaming":
		return base + " [...]"
	case "error":
		return base + " [err]"
	default:
		return base + " [ok]"
	}
}

// View renders the conversation history inside a scrollable viewport and
// anchors the input prompt at the bottom of the terminal.
func (m *model) View() tea.View {
	// Render thin horizontal lines to visually separate the conversation
	// history (viewport), the input area, and the fixed status bar.
	var separator string
	if m.width > 0 {
		separator = strings.Repeat("─", m.width)
	}

	statusStr, statusLines := buildStatusLine(m.status, m.width)

	view := m.viewport.View()
	if view != "" {
		var parts []string
		parts = append(parts, view, separator, m.textarea.View())
		if statusLines > 0 {
			parts = append(parts, separator, statusStr)
		}
		v := tea.NewView(strings.Join(parts, "\n"))
		v.AltScreen = true
		v.WindowTitle = m.windowTitle()
		return v
	}
	v := tea.NewView(m.textarea.View())
	v.AltScreen = true
	v.WindowTitle = m.windowTitle()
	return v
}
