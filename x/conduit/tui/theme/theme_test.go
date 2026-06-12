package theme

import (
	"testing"

	"github.com/andrewhowdencom/ore/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDarkThemeBase_DocumentOverrides(t *testing.T) {
	// The dark theme base must zero out document.margin (so glamour does
	// not pad the output) and strip document.block_prefix /
	// document.block_suffix (so the buildContent separator is the sole
	// authority for inter-message spacing).
	base := darkThemeBase()
	require.NotNil(t, base.Document.Margin, "document.margin must be a non-nil pointer")
	assert.Equal(t, uint(0), *base.Document.Margin, "document.margin must be 0")
	assert.Equal(t, "", base.Document.BlockPrefix, "document.block_prefix must be empty")
	assert.Equal(t, "", base.Document.BlockSuffix, "document.block_suffix must be empty")
}

func TestLightThemeBase_DocumentOverrides(t *testing.T) {
	base := lightThemeBase()
	require.NotNil(t, base.Document.Margin, "document.margin must be a non-nil pointer")
	assert.Equal(t, uint(0), *base.Document.Margin, "document.margin must be 0")
	assert.Equal(t, "", base.Document.BlockPrefix, "document.block_prefix must be empty")
	assert.Equal(t, "", base.Document.BlockSuffix, "document.block_suffix must be empty")
}

func TestDark_Light_ReturnNonNilThemes(t *testing.T) {
	// The factories must return non-nil themes with the glamour style
	// already populated, so callers can pass the result straight into
	// tui.WithTheme(...) without a nil check.
	dark := Dark()
	require.NotNil(t, dark)
	assert.NotEmpty(t, dark.AssistantStyle.Render("x"), "assistant style must render something")

	light := Light()
	require.NotNil(t, light)
	assert.NotEmpty(t, light.AssistantStyle.Render("x"), "assistant style must render something")
}

func TestStyleForRole_Mapping(t *testing.T) {
	// StyleForRole must return the right lipgloss style for each role,
	// and a sensible fallback for unknown roles.
	th := Dark()
	assert.Equal(t, th.AssistantStyle, th.StyleForRole(state.RoleAssistant))
	assert.Equal(t, th.UserStyle, th.StyleForRole(state.RoleUser))
	assert.Equal(t, th.ToolResultStyle, th.StyleForRole(state.RoleTool))
	assert.Equal(t, th.SystemStyle, th.StyleForRole(state.RoleSystem))
	// Unknown role falls back to the assistant style so the output is
	// still legible.
	assert.Equal(t, th.AssistantStyle, th.StyleForRole(state.Role("unknown")))
}
