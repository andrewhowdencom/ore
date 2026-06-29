package theme

import (
	"strings"
	"testing"

	"github.com/andrewhowdencom/ore/ledger"
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
	assert.Equal(t, th.AssistantStyle, th.StyleForRole(ledger.RoleAssistant))
	assert.Equal(t, th.UserStyle, th.StyleForRole(ledger.RoleUser))
	assert.Equal(t, th.ToolResultStyle, th.StyleForRole(ledger.RoleTool))
	assert.Equal(t, th.SystemStyle, th.StyleForRole(ledger.RoleSystem))
	// Unknown role falls back to the assistant style so the output is
	// still legible.
	assert.Equal(t, th.AssistantStyle, th.StyleForRole(ledger.Role("unknown")))
}

func TestTheme_Gap(t *testing.T) {
	// Gap encodes a blank-line amount as a string the renderer writes
	// between structural elements. n == 0 returns ""; n > 0 returns
	// n+1 newlines, which renders as n blank lines on a monospace
	// terminal. n < 0 is treated as 0 (defensive: a future theme that
	// misconfigures a gap should not produce a panic).
	tests := []struct {
		name string
		n    int
		want string
	}{
		{"zero produces empty string", 0, ""},
		{"one blank line is two newlines", 1, "\n\n"},
		{"two blank lines is three newlines", 2, "\n\n\n"},
		{"negative is treated as zero", -1, ""},
		{"large value is allowed", 100, strings.Repeat("\n", 101)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			th := Dark()
			assert.Equal(t, tt.want, th.Gap(tt.n))
		})
	}
}

func TestDark_DefaultGaps(t *testing.T) {
	// The dark factory must set both gap fields to 1 (one blank line
	// each) so the default behavior reproduces the previous literal
	// "\n\n" in buildContent.
	dark := Dark()
	assert.Equal(t, 1, dark.InterBlockGap, "dark.InterBlockGap must be 1")
	assert.Equal(t, 1, dark.InterTurnGap, "dark.InterTurnGap must be 1")
}

func TestLight_DefaultGaps(t *testing.T) {
	light := Light()
	assert.Equal(t, 1, light.InterBlockGap, "light.InterBlockGap must be 1")
	assert.Equal(t, 1, light.InterTurnGap, "light.InterTurnGap must be 1")
}
