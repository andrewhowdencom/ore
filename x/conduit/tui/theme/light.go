package theme

import (
	"charm.land/lipgloss/v2"
	glamouransi "github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
)

// lightThemeBase returns the upstream glamour LightStyleConfig with the
// same ore-specific overrides as darkThemeBase: document.margin = 0 and
// document.block_prefix / block_suffix = "".
func lightThemeBase() glamouransi.StyleConfig {
	base := styles.LightStyleConfig
	zero := uint(0)
	empty := ""
	base.Document.Margin = &zero
	base.Document.BlockPrefix = empty
	base.Document.BlockSuffix = empty
	return base
}

// Light returns a Theme configured for terminals with a light background.
// The lipgloss styles use the same hex values as Dark for now; a future
// task may introduce a divergent light palette if user feedback warrants
// it. The current choice keeps the theme predictable across modes.
func Light() *Theme {
	return &Theme{
		GlamourStyle:           lightThemeBase(),
		AssistantStyle:         lipgloss.NewStyle().Foreground(lipgloss.Color("#6C8EBF")),
		UserStyle:              lipgloss.NewStyle().Foreground(lipgloss.Color("#E5C07B")),
		ToolResultStyle:        lipgloss.NewStyle().Foreground(lipgloss.Color("#98C379")),
		ErrorStyle:             lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5555")),
		SystemStyle:            lipgloss.NewStyle().Foreground(lipgloss.Color("#C678DD")),
		StatusStyle:            lipgloss.NewStyle().Faint(true).Italic(true),
		ThinkingStyle:          lipgloss.NewStyle().Faint(true).Italic(true),
		ReasoningExpandedStyle: lipgloss.NewStyle().Faint(true),
		SpinnerStyle:           lipgloss.NewStyle().Faint(true).Italic(true),
		ZoneLabelStyle:         lipgloss.NewStyle().Bold(true),
	}
}
