package tui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/x/conduit"
	"github.com/andrewhowdencom/ore/x/conduit/tui/theme"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/cellbuf"
)

// renderBlockUnified renders a single block with the consistent header
// format: "<Timestamp> <Title>" left-aligned with a right-aligned compact
// byte-count suffix (e.g. "1.5K B") anchored to the right edge of the
// viewport. When the viewport is too narrow to fit the title and the
// count with a single space, the count is hidden before the title is
// truncated. If the viewport width is unknown (width <= 0), the header
// falls back to "<title> <count>" joined with a single space.
//
// The expansion is controlled by the expanded parameter; when false,
// reasoning blocks render header-only (the count is sufficient), tool
// blocks use their pre-computed compact form, and generic blocks
// truncate to two lines.
func renderBlockUnified(block renderedBlock, ts time.Time, expanded bool, width int) string {
	var title string
	if ts.IsZero() {
		title = block.title
	} else {
		title = ts.Format("15:04:05") + " " + block.title
	}
	countStr := compactNumber(strconv.Itoa(len(block.source))) + " B"

	var header string
	if width > 0 {
		titleW := ansi.StringWidth(title)
		countW := ansi.StringWidth(countStr)
		switch {
		case titleW+1+countW <= width:
			header = title + strings.Repeat(" ", width-titleW-countW) + countStr
		case titleW <= width:
			header = title
		default:
			header = truncateString(title, width)
		}
	} else {
		header = title + " " + countStr
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
	// The theme (theme.Dark / theme.Light) sets Document.BlockPrefix to
	// "" so the body never carries a leading newline; no defensive
	// trim is required here. The header-to-body join is a single "\n".
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
// Callers that mutate visual state (turns, pending, expandAllDetails)
// must set contentDirty = true before the next buildContent call so the
// cache is rebuilt. In practice Update() does this via syncViewport().
func (m *model) buildContent() string {
	if !m.contentDirty && m.cachedContent != "" {
		return m.cachedContent
	}

	var b strings.Builder
	width := m.viewport.Width()

	// Render conversation history. The expandAllDetails flag applies
	// globally to all non-text blocks across every turn, so a single
	// check is enough regardless of turn position.
	for _, turn := range m.turns {
		for i, block := range turn.blocks {
			expanded := block.expandedByDefault || m.expandAllDetails
			b.WriteString(renderBlockUnified(block, turn.timestamp, expanded, width))
			if i < len(turn.blocks)-1 {
				b.WriteString(m.theme.Gap(m.theme.InterBlockGap))
			}
		}
		b.WriteString(m.theme.Gap(m.theme.InterTurnGap))
	}

	// Render the in-progress assistant turn accumulated from ArtifactEvents.
	if len(m.currentTurn.blocks) > 0 {
		for i, block := range m.currentTurn.blocks {
			expanded := block.expandedByDefault || m.expandAllDetails
			b.WriteString(renderBlockUnified(block, time.Time{}, expanded, width))
			if i < len(m.currentTurn.blocks)-1 {
				b.WriteString(m.theme.Gap(m.theme.InterBlockGap))
			}
		}
		b.WriteString(m.theme.Gap(m.theme.InterTurnGap))
	}

	// Render pending placeholder only when no artifacts have arrived yet.
	if m.pending && len(m.currentTurn.blocks) == 0 {
		b.WriteString(renderBlockUnified(renderedBlock{
			kind:              "text",
			source:            "...",
			title:             "Assistant",
			style:             m.theme.AssistantStyle,
			expandedByDefault: true,
		}, time.Time{}, true, width))
		b.WriteString(m.theme.Gap(m.theme.InterTurnGap))
	}

	m.cachedContent = b.String()
	m.contentDirty = false
	return m.cachedContent
}

// compactNumber formats a raw integer as a compact string:
// <1000 → raw integer, 1000–999999 → "1.5K", "10K", "100K",
// ≥1,000,000 → "1M", "1.5M", "10M", "100M", "1B".
func compactNumber(s string) string {
	n, err := strconv.Atoi(s)
	if err != nil {
		return s
	}
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.0fB", float64(n)/1_000_000_000)
	case n >= 100_000_000:
		return fmt.Sprintf("%.0fM", float64(n)/1_000_000)
	case n >= 10_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 100_000:
		return fmt.Sprintf("%.0fK", float64(n)/1_000)
	case n >= 10_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	default:
		return s
	}
}

// buildStatusLine renders the status map into a single wrapped line using
// "key: value · key: value" format with ANSI styling.
//
// Token-usage keys (sent, cache_read, cache_write, received, total,
// thinking) are grouped into a single compact segment with display
// symbols (↑, ↻, ⊕, ↓, Σ, Ψ) so the status bar is not flooded with
// six separate entries. Cache fields appear only when the upstream
// `usage.Handler` actually emitted them (it omits zero values), so a
// provider that doesn't report cache produces a clean four-segment bar.
//
// It returns the rendered string and the number of display lines it
// occupies at the given width. Returns 0 lines when all values are empty.
func buildStatusLine(th *theme.Theme, status map[string]string, width int) (string, int) {
	if len(status) == 0 {
		return "", 0
	}

	var parts []string

	// Group token-usage keys into one segment with display symbols.
	// Render order is documented in the package-level summary and
	// mirrored in compactTokenSegments below.
	var tokens []string
	for _, key := range []string{"sent", "cache_read", "cache_write", "received", "total", "thinking"} {
		if v, ok := status[key]; ok && v != "" {
			var sym string
			switch key {
			case "sent":
				sym = "↑"
			case "cache_read":
				sym = "↻"
			case "cache_write":
				sym = "⊕"
			case "received":
				sym = "↓"
			case "total":
				sym = "Σ"
			case "thinking":
				sym = "Ψ"
			}
			tokens = append(tokens, fmt.Sprintf("%s %s", sym, compactNumber(v)))
		}
	}
	if len(tokens) > 0 {
		parts = append(parts, strings.Join(tokens, " · "))
	}

	// Render remaining keys alphabetically.
	var keys []string
	for k := range status {
		if k == "sent" || k == "cache_read" || k == "cache_write" ||
			k == "received" || k == "total" || k == "thinking" {
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
	rendered := th.StatusStyle.Render(strings.Join(parts, " · "))
	if width <= 0 {
		return rendered, 1
	}
	wrapped := cellbuf.Wrap(rendered, width, " ")
	lines := strings.Count(wrapped, "\n") + 1
	return wrapped, lines
}

// compactTokenSegments collapses sent, cache_read, cache_write,
// received, total, and thinking into a single segment named "tokens"
// with a compact "↑ X · ↻ Y · ⊕ Z · ↓ A · Σ B · Ψ C" value.
// Segments are emitted in narrative order:
// sent → cache_read → cache_write → received → total → thinking,
// mirrored from buildStatusLine so the two renderers produce
// identical output.
//
// Cache fields appear only when `usage.Handler` emitted them (it
// omits zero values). A provider that doesn't report cache produces
// a four-segment output identical to the previous behaviour.
func compactTokenSegments(segs []conduit.StatusSegment) []conduit.StatusSegment {
	// Canonical render order, mirrored from buildStatusLine. The
	// symbol map and the byLabel lookup replace the prior
	// sort.Strings call, which produced Unicode-byte order
	// (Σ X · Ψ Y · ↑ Z · ↓ T) instead of the documented order.
	order := []string{"sent", "cache_read", "cache_write", "received", "total", "thinking"}
	symbols := map[string]string{
		"sent":        "↑",
		"cache_read":  "↻",
		"cache_write": "⊕",
		"received":    "↓",
		"total":       "Σ",
		"thinking":    "Ψ",
	}
	byLabel := make(map[string]string, len(segs))
	for _, seg := range segs {
		if _, ok := symbols[seg.Label]; ok && seg.Value != "" {
			byLabel[seg.Label] = seg.Value
		}
	}
	var values []string
	for _, key := range order {
		if v, ok := byLabel[key]; ok {
			values = append(values, fmt.Sprintf("%s %s", symbols[key], compactNumber(v)))
		}
	}
	if len(values) == 0 {
		// No recognised token keys; pass through the input unchanged so
		// non-token segments survive unmodified.
		return segs
	}
	filtered := make([]conduit.StatusSegment, 0, len(segs))
	for _, seg := range segs {
		if _, ok := symbols[seg.Label]; ok {
			continue
		}
		filtered = append(filtered, seg)
	}
	filtered = append(filtered, conduit.StatusSegment{
		Label: "tokens",
		Value: strings.Join(values, " · "),
		Zone:  segs[0].Zone,
	})
	return filtered
}

// buildStatusLineFromSegments renders zone-grouped status segments into a
// wrapped status string. Segments are grouped by zone, zones are sorted by
// priority (lower value = higher priority), and lower-priority zones are
// dropped entirely if the result exceeds maxStatusLines (3). The "default"
// zone renders without brackets for backward compatibility.
func buildStatusLineFromSegments(th *theme.Theme, segments []conduit.StatusSegment, zonePriorities map[string]int, width int) (string, int) {
	if len(segments) == 0 {
		return "", 0
	}

	const maxStatusLines = 3

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
				zoneStr = th.ZoneLabelStyle.Render(zoneLabel) + " " + zoneStr
			}
			zoneParts = append(zoneParts, zoneStr)
		}

		if len(zoneParts) == 0 {
			continue
		}

		rendered := th.StatusStyle.Render(strings.Join(zoneParts, "\n"))
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

// truncateString truncates s to maxWidth visible characters, adding "…" if
// truncated. It is ANSI-aware, ensuring invisible escape sequences from
// Glamour-rendered markdown are not counted against the width limit.
func truncateString(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return s
	}
	if ansi.StringWidth(s) <= maxWidth {
		return s
	}
	return ansi.Truncate(s, maxWidth, "…")
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
		parts = append(parts, view, separator)
		if m.working && m.workingDescription != "" {
			parts = append(parts, m.theme.SpinnerStyle.Render("⚙ "+m.workingDescription))
		}
		parts = append(parts, m.textarea.View())
		if statusLines > 0 {
			parts = append(parts, separator, statusStr)
		}
		v := tea.NewView(strings.Join(parts, "\n"))
		v.AltScreen = true
		v.MouseMode = tea.MouseModeCellMotion
		v.WindowTitle = m.windowTitle()
		return v
	}
	v := tea.NewView(m.textarea.View())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = m.windowTitle()
	return v
}
