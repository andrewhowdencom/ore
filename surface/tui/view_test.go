package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderMarkdown(t *testing.T) {
	input := "# Hello\n\nSome **bold** text and `code`."
	output, err := renderMarkdown(input, 80)
	require.NoError(t, err)
	assert.NotEmpty(t, output)
	// Output should differ from input (glamour adds formatting).
	assert.NotEqual(t, input, output)
}

func TestRenderMarkdown_CodeBlock(t *testing.T) {
	input := "```go\nfunc main() {\n    fmt.Println(\"hi\")\n}\n```"
	output, err := renderMarkdown(input, 80)
	require.NoError(t, err)
	assert.NotEmpty(t, output)
}

func TestRenderMarkdown_NegativeWidth(t *testing.T) {
	// glamour.NewTermRenderer may accept any width; ensure we handle
	// a negative width without panic.
	input := "hello"
	output, err := renderMarkdown(input, -1)
	// We allow either success or error; the caller handles errors.
	_ = output
	_ = err
}

func TestPrefixLines_SingleLine(t *testing.T) {
	output := prefixLines("hello", "Label: ", "       ")
	assert.Equal(t, "Label: hello", output)
}

func TestPrefixLines_MultiLine(t *testing.T) {
	output := prefixLines("line1\nline2\nline3", "L: ", "   ")
	lines := strings.Split(output, "\n")
	require.Len(t, lines, 3)
	assert.Equal(t, "L: line1", lines[0])
	assert.Equal(t, "   line2", lines[1])
	assert.Equal(t, "   line3", lines[2])
}

func TestPrefixLines_EmptyText(t *testing.T) {
	output := prefixLines("", "Label: ", "       ")
	assert.Equal(t, "Label: ", output)
}

func TestPrefixLines_WithANSI(t *testing.T) {
	styled := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Render("red")
	input := styled + "\nplain"
	output := prefixLines(input, "L: ", "   ")
	assert.True(t, strings.HasPrefix(output, "L: "))
	lines := strings.Split(output, "\n")
	require.Len(t, lines, 2)
	assert.True(t, strings.HasPrefix(lines[1], "   "))
}
