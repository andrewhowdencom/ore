package tui

import (
	"strings"
	"testing"

	"github.com/andrewhowdencom/tack/state"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderMarkdown(t *testing.T) {
	input := "# Hello\n\nSome **bold** text and `code`."
	output, err := glamourMarkdownRenderer{}.Render(input, 80)
	require.NoError(t, err)
	assert.NotEmpty(t, output)
	// Output should differ from input (glamour processes the markdown).
	assert.NotEqual(t, input, output)
}

func TestRenderMarkdown_CodeBlock(t *testing.T) {
	input := "```go\nfunc main() {\n    fmt.Println(\"hi\")\n}\n```"
	output, err := glamourMarkdownRenderer{}.Render(input, 80)
	require.NoError(t, err)
	assert.NotEmpty(t, output)
	// Verify glamour processed the code block (output differs from input).
	assert.NotEqual(t, input, output)
}

func TestRenderMarkdown_NegativeWidth(t *testing.T) {
	// glamour.NewTermRenderer may accept any width; ensure we handle
	// a negative width without panic.
	input := "hello"
	output, err := glamourMarkdownRenderer{}.Render(input, -1)
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

func TestModel_View_AssistantTurn_WithRendered(t *testing.T) {
	m := model{
		viewport: viewport.New(80, 20),
		turns: []renderedTurn{
			{role: state.RoleAssistant, text: "# Hello", rendered: "pre-rendered glamour output"},
		},
	}
	output := m.View()
	assert.Contains(t, output, "Assistant: ")
	assert.Contains(t, output, "pre-rendered glamour output")
	// Should not contain the raw Markdown source.
	assert.NotContains(t, output, "# Hello")
}

func TestModel_View_AssistantTurn_FallbackToPlainText(t *testing.T) {
	m := model{
		viewport: viewport.New(80, 20),
		turns: []renderedTurn{
			{role: state.RoleAssistant, text: "plain text", rendered: ""},
		},
	}
	output := m.View()
	assert.Contains(t, output, "Assistant: ")
	assert.Contains(t, output, "plain text")
}

func TestModel_View_StreamingText_PlainText(t *testing.T) {
	m := model{
		viewport: viewport.New(80, 20),
	}
	m.textStreamBuffer.WriteString("streaming text")
	output := m.View()
	assert.Contains(t, output, "Assistant: ")
	assert.Contains(t, output, "streaming text")
}
