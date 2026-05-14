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
	output, err := newGlamourMarkdownRenderer().Render(input, 80)
	require.NoError(t, err)
	assert.NotEmpty(t, output)
	// Output should differ from input (glamour processes the markdown).
	assert.NotEqual(t, input, output)
}

func TestRenderMarkdown_CodeBlock(t *testing.T) {
	input := "```go\nfunc main() {\n    fmt.Println(\"hi\")\n}\n```"
	output, err := newGlamourMarkdownRenderer().Render(input, 80)
	require.NoError(t, err)
	assert.NotEmpty(t, output)
	// Verify glamour processed the code block (output differs from input).
	assert.NotEqual(t, input, output)
}

func TestRenderMarkdown_NegativeWidth(t *testing.T) {
	// glamour.NewTermRenderer may accept any width; ensure we handle
	// a negative width without panic.
	input := "hello"
	output, err := newGlamourMarkdownRenderer().Render(input, -1)
	// We allow either success or error; the caller handles errors.
	_ = output
	_ = err
}

func TestModel_View_AssistantTurn_WithRendered(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(80, 20)
	m.turns = []renderedTurn{
		{role: state.RoleAssistant, blocks: []renderedBlock{{kind: "text", source: "# Hello", rendered: "pre-rendered glamour output"}}},
	}
	output := m.View()
	assert.Contains(t, output, "Assistant: ")
	assert.Contains(t, output, "pre-rendered glamour output")
	// Should not contain the raw Markdown source.
	assert.NotContains(t, output, "# Hello")
	idxLabel := strings.Index(output, "Assistant: ")
	idxContent := strings.Index(output, "pre-rendered glamour output")
	assert.Greater(t, idxContent, idxLabel, "content should appear after label")
	segment := output[idxLabel:idxContent]
	assert.Contains(t, segment, "\n", "label and content should be on separate lines")
}

func TestModel_View_AssistantTurn_FallbackToPlainText(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(80, 20)
	m.turns = []renderedTurn{
		{role: state.RoleAssistant, blocks: []renderedBlock{{kind: "text", source: "plain text"}}},
	}
	output := m.View()
	assert.Contains(t, output, "Assistant: ")
	assert.Contains(t, output, "plain text")
	idxLabel := strings.Index(output, "Assistant: ")
	idxContent := strings.Index(output, "plain text")
	assert.Greater(t, idxContent, idxLabel, "content should appear after label")
	segment := output[idxLabel:idxContent]
	assert.Contains(t, segment, "\n", "label and content should be on separate lines")
}

func TestModel_View_AssistantTurn_WithReasoning(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(80, 20)
	m.turns = []renderedTurn{
		{role: state.RoleAssistant, blocks: []renderedBlock{
			{kind: "text", source: "the answer"},
			{kind: "reasoning", source: "because 2+2=4"},
		}},
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

func TestModel_View_AssistantTurn_MultiBlockSpacing(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(80, 20)
	m.turns = []renderedTurn{
		{role: state.RoleAssistant, blocks: []renderedBlock{
			{kind: "reasoning", source: "let me think..."},
			{kind: "text", source: "the answer"},
		}},
	}
	output := m.View()
	assert.Contains(t, output, "Thinking: ")
	assert.Contains(t, output, "let me think...")
	assert.Contains(t, output, "Assistant: ")
	assert.Contains(t, output, "the answer")
	// Verify order: reasoning precedes the answer (typical provider ordering).
	idxThink := strings.Index(output, "let me think...")
	idxAnswer := strings.Index(output, "the answer")
	require.Greater(t, idxAnswer, idxThink, "answer should appear after reasoning")
	// Verify that the blocks are on separate lines (not adjacent as in the
	// buggy behavior where turn-level rendering omitted intra-turn separators).
	segment := output[idxThink+len("let me think...") : idxAnswer]
	assert.Contains(t, segment, "\n", "reasoning and answer blocks should be on separate lines")
}

func TestRenderMarkdown_MalformedInput(t *testing.T) {
	cases := []string{
		"[link](<unfinished",
		"**bold",
		"```unclosed",
	}
	for _, input := range cases {
		output, err := newGlamourMarkdownRenderer().Render(input, 80)
		assert.NoError(t, err, "malformed markdown %q should not error", input)
		assert.NotEmpty(t, output)
	}
}

func TestRenderMarkdown_NarrowWidth(t *testing.T) {
	for _, width := range []int{1, 2, 5} {
		output, err := newGlamourMarkdownRenderer().Render("hello world", width)
		assert.NoError(t, err, "narrow width %d should not panic", width)
		assert.NotEmpty(t, output)
	}
}

func TestRenderBlock_LabelAboveContent(t *testing.T) {
	output := renderBlock("You: ", lipgloss.NewStyle(), "hello", 80)
	assert.Equal(t, "You: \nhello", output)
}

func TestRenderBlock_WrapsContent(t *testing.T) {
	text := strings.Repeat("a", 100)
	output := renderBlock("You: ", lipgloss.NewStyle(), text, 20)
	lines := strings.Split(output, "\n")
	assert.Greater(t, len(lines), 2, "long text should wrap to multiple lines")
	// First line is label, remaining lines are content starting at column 0
	assert.Equal(t, "You: ", lines[0])
	for i := 1; i < len(lines); i++ {
		assert.False(t, strings.HasPrefix(lines[i], " "), "content should start at column 0")
	}
}

func TestRenderBlock_StyledLabel(t *testing.T) {
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000"))
	output := renderBlock("Label: ", style, "hello", 80)
	assert.True(t, strings.HasPrefix(output, style.Render("Label: ")))
}

func TestRenderBlock_EmptyContent(t *testing.T) {
	output := renderBlock("You: ", lipgloss.NewStyle(), "", 80)
	assert.Equal(t, "You: ", output)
}

func TestRenderBlock_PreRenderedWidthZero(t *testing.T) {
	content := "line1\nline2\nline3"
	output := renderBlock("Assistant: ", lipgloss.NewStyle(), content, 0)
	lines := strings.Split(output, "\n")
	require.Len(t, lines, 4)
	assert.Equal(t, "Assistant: ", lines[0])
	assert.Equal(t, "line1", lines[1])
	assert.Equal(t, "line2", lines[2])
	assert.Equal(t, "line3", lines[3])
}

func TestModel_View_PendingPlaceholder(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(80, 20)
	m.pending = true
	output := m.View()
	assert.Contains(t, output, "Assistant: ")
	assert.Contains(t, output, "...")
	idxLabel := strings.Index(output, "Assistant: ")
	idxContent := strings.Index(output, "...")
	assert.Greater(t, idxContent, idxLabel, "placeholder content should appear after label")
	segment := output[idxLabel:idxContent]
	assert.Contains(t, segment, "\n", "label and placeholder should be on separate lines")
}

func TestRenderBlock_Unicode(t *testing.T) {
	// Japanese characters are typically 2 cells wide.
	text := "こんにちは世界"
	output := renderBlock("You: ", lipgloss.NewStyle(), text, 12)
	lines := strings.Split(output, "\n")
	// First line is label
	assert.Equal(t, "You: ", lines[0])
	// Content should be wrapped considering cell width
	for i := 1; i < len(lines); i++ {
		assert.LessOrEqual(t, lipgloss.Width(lines[i]), 12, "line %q exceeds width", lines[i])
	}
}

func TestRenderBlock_NegativeWidth(t *testing.T) {
	// Negative width should skip wrapping and not panic.
	output := renderBlock("You: ", lipgloss.NewStyle(), "hello", -1)
	assert.Equal(t, "You: \nhello", output)
}

func TestRenderBlock_ExactFit(t *testing.T) {
	// Content whose length exactly matches width should not produce
	// an extra wrapped line.
	content := strings.Repeat("a", 20)
	output := renderBlock("You: ", lipgloss.NewStyle(), content, 20)
	lines := strings.Split(output, "\n")
	// Label + one content line
	assert.Equal(t, 2, len(lines), "exact-fit content should not wrap to extra line")
	assert.Equal(t, "You: ", lines[0])
	assert.Equal(t, content, lines[1])
}
