package tui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/state"
	"github.com/andrewhowdencom/ore/x/conduit"
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
	// errorStyle styles error turns from the harness in red.
	errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5555"))
	// userStyle styles user input in yellow.
	userStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#E5C07B"))
	// systemStyle styles system-level messages in purple.
	systemStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#C678DD"))
	// toolResultStyle styles successful tool results in green.
	toolResultStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#98C379"))
	// zoneLabelStyle styles zone names (Lifecycle, Context) bold in the status bar.
	zoneLabelStyle = lipgloss.NewStyle().Bold(true)
)

// renderBlockUnified renders a single block with the consistent header format:
// "<Timestamp> <Title> · |s| <rune_count>" followed by compact or expanded body.
// The expansion is controlled by the expanded parameter; when false, reasoning
// blocks render header-only (the count is sufficient), tool blocks use their
// pre-computed compact form, and generic blocks truncate to two lines.
func renderBlockUnified(block renderedBlock, ts time.Time, expanded bool, width int) string {
	var header string
	count := utf8.RuneCountInString(block.source)
	if ts.IsZero() {
		header = fmt.Sprintf("%s · |s| %d", block.title, count)
	} else {
		header = fmt.Sprintf("%s %s · |s| %d", ts.Format("15:04:05"), block.title, count)
	}

	styledHeader := block.style.Render(header)

	var body string
	if expanded {
		if block.rendered != "" {
			body = block.rendered
		} else {
			body = block.source
		}
		if width > 0 && body != "" {
			body = cellbuf.Wrap(body, width, " ")
		}
	} else {
		switch block.kind {
		case "reasoning":
			// Header already conveys the count; no body needed in compact mode.
			return styledHeader
		case "tool_call", "tool_result":
			if block.compact != "" {
				body = block.compact
			} else {
				body = compactGeneric(block.source, width)
			}
		default:
			body = compactGeneric(block.source, width)
		}
	}

	if body == "" {
		return styledHeader
	}
	return styledHeader + "\n" + body
}

// compactGeneric truncates content to at most two lines and maxWidth runes,
// appending "…" when truncated.
func compactGeneric(content string, width int) string {
	lines := strings.Split(content, "\n")
	if len(lines) > 2 {
		content = strings.Join(lines[:2], "\n") + "…"
	}
	if width > 0 {
		content = cellbuf.Wrap(content, width, " ")
	}
	return truncateString(content, width)
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
		isLatestAssistant := turn.role == state.RoleAssistant && turnIdx == lastAssistantIdx
		isAfterLatestAssistant := turnIdx > lastAssistantIdx
		for i, block := range turn.blocks {
			expanded := block.expandedByDefault
			if !block.expandedByDefault && (isLatestAssistant || isAfterLatestAssistant) {
				expanded = m.expandLatestDetails
			}
			b.WriteString(renderBlockUnified(block, turn.timestamp, expanded, width))
			if i < len(turn.blocks)-1 {
				b.WriteString("\n\n")
			}
		}
		b.WriteString("\n\n")
	}

	// Render the in-progress assistant turn accumulated from ArtifactEvents.
	if len(m.currentTurn.blocks) > 0 {
		for i, block := range m.currentTurn.blocks {
			expanded := m.expandLatestDetails || block.expandedByDefault
			b.WriteString(renderBlockUnified(block, time.Time{}, expanded, width))
			if i < len(m.currentTurn.blocks)-1 {
				b.WriteString("\n\n")
			}
		}
		b.WriteString("\n\n")
	}

	// Render pending placeholder only when no artifacts have arrived yet.
	if m.pending && len(m.currentTurn.blocks) == 0 {
		b.WriteString(renderBlockUnified(renderedBlock{
			kind:              "text",
			source:            "...",
			title:             "Assistant",
			style:             assistantStyle,
			expandedByDefault: true,
		}, time.Time{}, true, width))
		b.WriteString("\n\n")
	}

	m.cachedContent = b.String()
	m.contentDirty = false
	return m.cachedContent
}

// buildStatusLine renders the status map into a single wrapped line using
// "key: value · key: value" format with ANSI styling.
//
// Token-usage keys (sent, received, total) are grouped into a single compact
// segment with display symbols (↑, ↓, Σ) so the status bar is not flooded
// with three separate entries.
//
// It returns the rendered string and the number of display lines it
// occupies at the given width. Returns 0 lines when all values are empty.
func buildStatusLine(status map[string]string, width int) (string, int) {
	if len(status) == 0 {
		return "", 0
	}

	var parts []string

	// Group token-usage keys into one segment with display symbols.
	var tokens []string
	for _, key := range []string{"sent", "received", "total"} {
		if v, ok := status[key]; ok && v != "" {
			var sym string
			switch key {
			case "sent":
				sym = "↑"
			case "received":
				sym = "↓"
			case "total":
				sym = "Σ"
			}
			tokens = append(tokens, fmt.Sprintf("%s %s", sym, v))
		}
	}
	if len(tokens) > 0 {
		parts = append(parts, strings.Join(tokens, " · "))
	}

	// Render remaining keys alphabetically.
	var keys []string
	for k := range status {
		if k == "sent" || k == "received" || k == "total" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
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

// compactTokenSegments collapses sent, received, and total into a single
// segment named "tokens" with a compact "↑ X · ↓ Y · Σ Z" value.
// Segments are sorted by label for deterministic output.
func compactTokenSegments(segs []conduit.StatusSegment) []conduit.StatusSegment {
	var values []string
	var filtered []conduit.StatusSegment
	for _, seg := range segs {
		var sym string
		switch seg.Label {
		case "sent":
			sym = "↑"
		case "received":
			sym = "↓"
		case "total":
			sym = "Σ"
		default:
			filtered = append(filtered, seg)
			continue
		}
		values = append(values, fmt.Sprintf("%s %s", sym, seg.Value))
	}
	if len(values) > 0 {
		sort.Strings(values)
		filtered = append(filtered, conduit.StatusSegment{
			Label: "tokens",
			Value: strings.Join(values, " · "),
			Zone:  segs[0].Zone,
		})
	}
	return filtered
}

// buildStatusLineFromSegments renders zone-grouped status segments into a
// wrapped status string. Segments are grouped by zone, zones are sorted by
// priority (lower value = higher priority), and lower-priority zones are
// dropped entirely if the result exceeds maxStatusLines (2). The "default"
// zone renders without brackets for backward compatibility.
func buildStatusLineFromSegments(segments []conduit.StatusSegment, zonePriorities map[string]int, width int) (string, int) {
	if len(segments) == 0 {
		return "", 0
	}

	const maxStatusLines = 2

	// Group segments by zone, filtering out empty values.
	zones := make(map[string][]conduit.StatusSegment)
	for _, seg := range segments {
		if seg.Value == "" {
			continue
		}
		zones[seg.Zone] = append(zones[seg.Zone], seg)
	}
	// Compact token-usage segments per zone so they consume one slot.
	for z, segs := range zones {
		zones[z] = compactTokenSegments(segs)
	}
	if len(zones) == 0 {
		return "", 0
	}

	// Sort zone names by priority ascending.
	zoneNames := make([]string, 0, len(zones))
	for z := range zones {
		zoneNames = append(zoneNames, z)
	}
	sort.Slice(zoneNames, func(i, j int) bool {
		pi, ok := zonePriorities[zoneNames[i]]
		if !ok {
			pi = 99
		}
		pj, ok := zonePriorities[zoneNames[j]]
		if !ok {
			pj = 99
		}
		return pi < pj
	})

	var wrapped string
	var lines int

	// Try progressively fewer zones (dropping lowest-priority first) until
	// the wrapped output fits within maxStatusLines.
	for numZones := len(zoneNames); numZones > 0; numZones-- {
		var zoneParts []string
		for i := 0; i < numZones; i++ {
			zone := zoneNames[i]
			parts := zones[zone]
			// Sort segments by label for deterministic output.
			sort.Slice(parts, func(a, b int) bool {
				return parts[a].Label < parts[b].Label
			})
			var kvParts []string
			for _, seg := range parts {
				kvParts = append(kvParts, fmt.Sprintf("%s: %s", seg.Label, seg.Value))
			}
			if len(kvParts) == 0 {
				continue
			}
			zoneStr := strings.Join(kvParts, " · ")
			if zone != "default" {
				zoneLabel := strings.ToUpper(zone[:1]) + zone[1:] + ":"
				zoneStr = zoneLabelStyle.Render(zoneLabel) + " " + zoneStr
			}
			zoneParts = append(zoneParts, zoneStr)
		}

		if len(zoneParts) == 0 {
			continue
		}

		rendered := statusStyle.Render(strings.Join(zoneParts, "\n"))
		if width <= 0 {
			return rendered, 1
		}

		wrapped = cellbuf.Wrap(rendered, width, " ")
		lines = strings.Count(wrapped, "\n") + 1

		if lines <= maxStatusLines {
			return wrapped, lines
		}
	}

	// Even the highest-priority zone alone exceeds maxStatusLines.
	// Return it anyway so status is never fully hidden.
	if wrapped != "" {
		return wrapped, lines
	}

	return "", 0
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
// representation, keeping the TUI concise by limiting to the first 3 lines
// or maxWidth characters. The caller is responsible for pre-rendering
// (e.g. via MarkdownString) and adding an "Error: " prefix when appropriate.
// maxWidth is normally the current viewport width.
func compactToolResult(content string, maxWidth int) string {
	lines := strings.Split(content, "\n")
	if len(lines) > 3 {
		content = strings.Join(lines[:3], "\n") + "…"
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
	case "cancelled":
		return base + " [cancelled]"
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

	statusStr, statusLines := m.statusLine()

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
