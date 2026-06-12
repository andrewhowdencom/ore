package theme

import (
	"charm.land/lipgloss/v2"
	glamouransi "github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
)

// darkThemeBase returns the upstream glamour DarkStyleConfig with the
// ore-specific overrides applied: document.margin = 0 (so the glamour
// renderer does not pad the output with a frame), document.block_prefix
// and document.block_suffix = "" (so the explicit "\n\n" separator in
// buildContent is the sole authority for inter-message spacing).
func darkThemeBase() glamouransi.StyleConfig {
	base := styles.DarkStyleConfig
	zero := uint(0)
	empty := ""
	base.Document.Margin = &zero
	base.Document.BlockPrefix = empty
	base.Document.BlockSuffix = empty
	return base
}

// Dark returns a Theme configured for terminals with a dark background.
// The glamour StyleConfig is built from glamour's upstream
// DarkStyleConfig with the ore-specific overrides applied; the lipgloss
// styles mirror the project's existing dark palette (see
// x/conduit/tui/view.go before the consolidation).
func Dark() *Theme {
	return &Theme{
		GlamourStyle:           darkThemeBase(),
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
