package tui

import (
	"strings"
	"testing"

	"github.com/andrewhowdencom/ore/state"
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
			{role: state.RoleAssistant, blocks: []renderedBlock{{kind: "text", source: "# Hello", rendered: "pre-rendered glamour output"}}},
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
			{role: state.RoleAssistant, blocks: []renderedBlock{{kind: "text", source: "plain text"}}},
		},
	}
	output := m.View()
	assert.Contains(t, output, "Assistant: ")
	assert.Contains(t, output, "plain text")
}

func TestModel_View_StreamingText_PlainText(t *testing.T) {
	m := model{
		viewport: viewport.New(80, 20),
		streamBlocks: []streamBlock{{kind: "text", content: "streaming text"}},
	}
	output := m.View()
	assert.Contains(t, output, "Assistant: ")
	assert.Contains(t, output, "streaming text")
}

func TestModel_View_StreamingReasoning(t *testing.T) {
	m := model{
		viewport: viewport.New(80, 20),
		streamBlocks: []streamBlock{{kind: "reasoning", content: "thinking..."}},
	}
	output := m.View()
	assert.Contains(t, output, "Thinking: ")
	assert.Contains(t, output, "thinking...")
}

func TestModel_View_InterleavedStreaming(t *testing.T) {
	m := model{
		viewport: viewport.New(80, 20),
		streamBlocks: []streamBlock{
			{kind: "text", content: "first"},
			{kind: "reasoning", content: "think"},
			{kind: "text", content: "second"},
		},
	}
	output := m.View()
	// Verify text and reasoning appear interleaved in order.
	idxFirst := strings.Index(output, "first")
	idxThink := strings.Index(output, "think")
	idxSecond := strings.Index(output, "second")
	assert.Greater(t, idxThink, idxFirst, "reasoning should appear after first text")
	assert.Greater(t, idxSecond, idxThink, "second text should appear after reasoning")
}

func TestModel_View_AssistantTurn_WithReasoning(t *testing.T) {
	m := model{
		viewport: viewport.New(80, 20),
		turns: []renderedTurn{
			{role: state.RoleAssistant, blocks: []renderedBlock{
				{kind: "text", source: "the answer"},
				{kind: "reasoning", source: "because 2+2=4"},
			}},
		},
	}
	output := m.View()
	assert.Contains(t, output, "Assistant: ")
	assert.Contains(t, output, "the answer")
	assert.Contains(t, output, "Thinking: ")
	assert.Contains(t, output, "because 2+2=4")
	// Verify order: text appears before reasoning.
	idxAnswer := strings.Index(output, "the answer")
	idxReason := strings.Index(output, "because 2+2=4")
	assert.Greater(t, idxReason, idxAnswer, "reasoning should appear after text")
}

func TestRenderMarkdown_MalformedInput(t *testing.T) {
	cases := []string{
		"[link](<unfinished",
		"**bold",
		"```unclosed",
	}
	for _, input := range cases {
		output, err := glamourMarkdownRenderer{}.Render(input, 80)
		assert.NoError(t, err, "malformed markdown %q should not error", input)
		assert.NotEmpty(t, output)
	}
}

func TestRenderMarkdown_NarrowWidth(t *testing.T) {
	for _, width := range []int{1, 2, 5} {
		output, err := glamourMarkdownRenderer{}.Render("hello world", width)
		assert.NoError(t, err, "narrow width %d should not panic", width)
		assert.NotEmpty(t, output)
	}
}
