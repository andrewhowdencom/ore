// Package theme owns the visual styling of the TUI conduit. A Theme value
// carries the full glamour StyleConfig used to render markdown bodies plus
// the set of lipgloss styles used for chrome (headers, role labels, status
// line, activity indicator). The role-to-style mapping is expressed as a
// method on the theme, so renderers and the model do not need to know which
// role maps to which colour.
//
// Two factory functions, Dark and Light, return the canonical instances.
// Auto selects between them based on the terminal's reported background.
// Application code that needs to override the default can pass a custom
// *Theme to the TUI via tui.WithTheme(...).
package theme

import (
	"charm.land/lipgloss/v2"
	"github.com/andrewhowdencom/ore/state"
	glamouransi "github.com/charmbracelet/glamour/ansi"
)

// Theme is the consolidated style configuration for the TUI. It owns the
// glamour StyleConfig used by the markdown renderer and the lipgloss styles
// used by the chrome (headers, status, role labels). The struct is a value
// (not an interface) so tests can construct one with a literal and so the
// theme can be embedded in other structs without indirection.
type Theme struct {
	// GlamourStyle is the full glamour/ansi.StyleConfig passed to
	// glamour.WithStyles when constructing the markdown renderer. Populating
	// it makes the theme the single source of truth for both the structural
	// rules of markdown rendering (block prefixes, margins, indent tokens)
	// and the colour palette.
	GlamourStyle glamouransi.StyleConfig

	// AssistantStyle styles the "Assistant" header for assistant text blocks.
	AssistantStyle lipgloss.Style
	// UserStyle styles the "You" header for user-input blocks.
	UserStyle lipgloss.Style
	// ToolResultStyle styles the "Tool Result" header for successful tool
	// results. Error results use ErrorStyle.
	ToolResultStyle lipgloss.Style
	// ErrorStyle styles error turns and error tool results.
	ErrorStyle lipgloss.Style
	// SystemStyle styles the "System" header for system-level messages.
	SystemStyle lipgloss.Style
	// StatusStyle styles the status line.
	StatusStyle lipgloss.Style
	// ThinkingStyle styles the "Thinking" header for reasoning blocks.
	ThinkingStyle lipgloss.Style
	// ReasoningExpandedStyle styles the body of an expanded reasoning
	// block, so it is visually subdued and does not look like normal
	// assistant text.
	ReasoningExpandedStyle lipgloss.Style
	// SpinnerStyle styles the activity indicator line shown when
	// long-running work is active (e.g. /compact).
	SpinnerStyle lipgloss.Style
	// ZoneLabelStyle styles zone names (Lifecycle, Context) in the status
	// bar.
	ZoneLabelStyle lipgloss.Style
}

// StyleForRole returns the lipgloss style appropriate for a conversation
// role. The mapping is a theme concern, not a renderer concern: a custom
// theme may emphasise different roles (e.g. a high-contrast theme may
// colour user text more prominently).
func (t *Theme) StyleForRole(role state.Role) lipgloss.Style {
	switch role {
	case state.RoleAssistant:
		return t.AssistantStyle
	case state.RoleUser:
		return t.UserStyle
	case state.RoleTool:
		return t.ToolResultStyle
	case state.RoleSystem:
		return t.SystemStyle
	default:
		// Unknown roles fall back to the assistant style so unrecognised
		// state (including the empty role) still renders legibly.
		return t.AssistantStyle
	}
}
