package tui

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/state"
	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"
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
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.turns = []renderedTurn{
		{role: state.RoleAssistant, blocks: []renderedBlock{{kind: "text", source: "# Hello", rendered: "pre-rendered glamour output"}}},
	}
	output := m.View().Content
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
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.turns = []renderedTurn{
		{role: state.RoleAssistant, blocks: []renderedBlock{{kind: "text", source: "plain text"}}},
	}
	output := m.View().Content
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
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.turns = []renderedTurn{
		{role: state.RoleAssistant, blocks: []renderedBlock{
			{kind: "text", source: "the answer"},
			{kind: "reasoning", source: "because 2+2=4"},
		}},
	}
	output := m.View().Content
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
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.turns = []renderedTurn{
		{role: state.RoleAssistant, blocks: []renderedBlock{
			{kind: "reasoning", source: "let me think..."},
			{kind: "text", source: "the answer"},
		}},
	}
	output := m.View().Content
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

func TestModel_View_AssistantTurn_Reasoning_Rendered(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.md = mockMarkdownRenderer{output: "rendered-reasoning"}
	turn := state.Turn{
		Role: state.RoleAssistant,
		Artifacts: []artifact.Artifact{
			artifact.Reasoning{Content: "let me think..."},
		},
	}
	newM, _ := m.Update(turnMsg{turn: turn})
	mm := newM.(*model)
	output := mm.View().Content
	assert.Contains(t, output, "Thinking: ")
	assert.Contains(t, output, "rendered-reasoning")
	assert.NotContains(t, output, "let me think...")
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
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.pending = true
	output := m.View().Content
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

func TestEmbeddedStyles_MarginZero(t *testing.T) {
	var dark map[string]interface{}
	require.NoError(t, json.Unmarshal(darkStyle, &dark))
	doc, ok := dark["document"].(map[string]interface{})
	require.True(t, ok, "dark style should have document key")
	margin, ok := doc["margin"].(float64)
	require.True(t, ok, "document should have margin key")
	assert.Equal(t, 0.0, margin, "dark style document margin should be 0")

	var light map[string]interface{}
	require.NoError(t, json.Unmarshal(lightStyle, &light))
	doc2, ok := light["document"].(map[string]interface{})
	require.True(t, ok, "light style should have document key")
	margin2, ok := doc2["margin"].(float64)
	require.True(t, ok, "document should have margin key")
	assert.Equal(t, 0.0, margin2, "light style document margin should be 0")
}

func TestRenderReasoning_ErrorFallback(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.md = mockMarkdownRenderer{err: errors.New("render failed")}
	turn := state.Turn{
		Role: state.RoleAssistant,
		Artifacts: []artifact.Artifact{
			artifact.Reasoning{Content: "let me think..."},
		},
	}
	newM, _ := m.Update(turnMsg{turn: turn})
	mm := newM.(*model)
	require.Len(t, mm.turns, 1)
	require.Len(t, mm.turns[0].blocks, 1)
	assert.Empty(t, mm.turns[0].blocks[0].rendered, "render error should leave rendered empty")
	assert.Equal(t, "let me think...", mm.turns[0].blocks[0].source, "raw text should still be stored")

	output := mm.View().Content
	assert.Contains(t, output, "Thinking: ")
	assert.Contains(t, output, "let me think...")
}

func TestRenderer_SelectsDarkStyle(t *testing.T) {
	r := newGlamourMarkdownRendererWithDetectors(
		func() bool { return true },
		func() bool { return true },
	)
	assert.Equal(t, darkStyle, r.styleBytes, "terminal + dark background should select dark style")
}

func TestRenderer_SelectsLightStyle(t *testing.T) {
	r := newGlamourMarkdownRendererWithDetectors(
		func() bool { return true },
		func() bool { return false },
	)
	assert.Equal(t, lightStyle, r.styleBytes, "terminal + light background should select light style")
}

func TestRenderer_SelectsNoTTY(t *testing.T) {
	r := newGlamourMarkdownRendererWithDetectors(
		func() bool { return false },
		func() bool { return false },
	)
	assert.Equal(t, darkStyle, r.styleBytes, "non-terminal should default to dark style")
}

func TestCompactToolCall_FlatJSON(t *testing.T) {
	tc := artifact.ToolCall{Name: "search_files", Arguments: `{"path": ".", "query": "hello"}`}
	output := compactToolCall(tc, 80)
	assert.Equal(t, `search_files · path="." · query="hello"`, output)
}

func TestCompactToolCall_NestedObject(t *testing.T) {
	tc := artifact.ToolCall{Name: "foo", Arguments: `{"nested": {"a": 1}}`}
	output := compactToolCall(tc, 80)
	assert.Equal(t, `foo · nested={…}`, output)
}

func TestCompactToolCall_Array(t *testing.T) {
	tc := artifact.ToolCall{Name: "foo", Arguments: `{"items": [1, 2, 3]}`}
	output := compactToolCall(tc, 80)
	assert.Equal(t, `foo · items=[…]`, output)
}

func TestCompactToolCall_InvalidJSON(t *testing.T) {
	tc := artifact.ToolCall{Name: "foo", Arguments: "not json"}
	output := compactToolCall(tc, 80)
	assert.Equal(t, "foo(not json)", output)
}

func TestCompactToolCall_Truncation(t *testing.T) {
	tc := artifact.ToolCall{Name: "foo", Arguments: `{"key": "very long value that exceeds the width"}`}
	output := compactToolCall(tc, 20)
	assert.True(t, strings.HasSuffix(output, "…"), "truncated output should end with ellipsis")
	assert.LessOrEqual(t, lipgloss.Width(output), 20, "output display width should not exceed maxWidth")
}

func TestCompactToolCall_Boolean(t *testing.T) {
	tc := artifact.ToolCall{Name: "foo", Arguments: `{"flag": true}`}
	output := compactToolCall(tc, 80)
	assert.Equal(t, `foo · flag=true`, output)
}

func TestCompactToolCall_Integer(t *testing.T) {
	tc := artifact.ToolCall{Name: "foo", Arguments: `{"count": 42}`}
	output := compactToolCall(tc, 80)
	assert.Equal(t, `foo · count=42`, output)
}

func TestCompactToolCall_EmptyArguments(t *testing.T) {
	tc := artifact.ToolCall{Name: "foo", Arguments: ""}
	output := compactToolCall(tc, 80)
	assert.Equal(t, "foo()", output)
}

func TestCompactToolCall_EmptyObject(t *testing.T) {
	tc := artifact.ToolCall{Name: "foo", Arguments: "{}"}
	output := compactToolCall(tc, 80)
	assert.Equal(t, "foo", output)
}

func TestCompactToolResult_Normal(t *testing.T) {
	tr := artifact.ToolResult{Content: "result data"}
	output := compactToolResult(tr, 80)
	assert.Equal(t, "result data", output)
}

func TestCompactToolResult_Multiline(t *testing.T) {
	tr := artifact.ToolResult{Content: "line1\nline2"}
	output := compactToolResult(tr, 80)
	assert.Equal(t, "line1", output)
}

func TestCompactToolResult_Error(t *testing.T) {
	tr := artifact.ToolResult{Content: "failed", IsError: true}
	output := compactToolResult(tr, 80)
	assert.Equal(t, "Error: failed", output)
}

func TestCompactToolResult_Truncation(t *testing.T) {
	tr := artifact.ToolResult{Content: strings.Repeat("a", 100)}
	output := compactToolResult(tr, 10)
	assert.Equal(t, "aaaaaaaaa…", output)
}

func TestTruncateString(t *testing.T) {
	assert.Equal(t, "hello", truncateString("hello", 10))
	assert.Equal(t, "hell…", truncateString("hello world", 5))
	assert.Equal(t, "hello", truncateString("hello", 0))
	assert.Equal(t, "…", truncateString("hello", 1))
}

func TestBuildContent_ExpandLatestTools_Toggle(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))

	m.turns = []renderedTurn{
		{
			role: state.RoleAssistant,
			blocks: []renderedBlock{
				{
					kind:    "tool_call",
					source:  "Calling: search_files({\"path\": \".\", \"query\": \"hello\"})",
					compact: "search_files · path=\".\" · query=\"hello\"",
				},
			},
		},
		{
			role: state.RoleTool,
			blocks: []renderedBlock{
				{
					kind:    "tool_result",
					source:  "result data",
					compact: "result data",
				},
			},
		},
	}

	// Compact mode (default): single-line with arrow indicator.
	m.expandLatestTools = false
	compactOutput := m.buildContent()
	assert.Contains(t, compactOutput, "→ search_files")
	assert.NotContains(t, compactOutput, "Calling: search_files")
	assert.Contains(t, compactOutput, "← result data")

	// Expanded mode: two-line label+content layout.
	m.expandLatestTools = true
	expandedOutput := m.buildContent()
	assert.Contains(t, expandedOutput, "Calling: search_files")
	assert.NotContains(t, expandedOutput, "→ search_files")
	assert.Contains(t, expandedOutput, "Tool: ")
	assert.Contains(t, expandedOutput, "result data")
}

func TestBuildContent_CompactToolError_RedStyling(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))

	m.turns = []renderedTurn{
		{
			role: state.RoleAssistant,
			blocks: []renderedBlock{
				{kind: "tool_call", source: "Calling: foo({})", compact: "foo"},
			},
		},
		{
			role: state.RoleTool,
			blocks: []renderedBlock{
				{
					kind:    "tool_result",
					source:  "Error: failed",
					compact: "Error: failed",
				},
			},
		},
	}

	// Compact mode: red styling for errors via compactToolErrorStyle.
	m.expandLatestTools = false
	output := m.buildContent()
	expectedCompact := compactToolErrorStyle.Render("← Error: failed")
	assert.Contains(t, output, expectedCompact)

	// Expanded mode: red label styling for errors via toolErrorStyle.
	m.expandLatestTools = true
	output = m.buildContent()
	assert.Contains(t, output, toolErrorStyle.Render("Tool: "))
	assert.Contains(t, output, "Error: failed")
}

func TestBuildContent_MultipleToolCalls(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))

	m.turns = []renderedTurn{
		{
			role: state.RoleAssistant,
			blocks: []renderedBlock{
				{kind: "tool_call", source: "Calling: foo({})", compact: "foo"},
				{kind: "tool_call", source: "Calling: bar({})", compact: "bar"},
			},
		},
		{
			role: state.RoleTool,
			blocks: []renderedBlock{
				{kind: "tool_result", source: "result1", compact: "result1"},
				{kind: "tool_result", source: "result2", compact: "result2"},
			},
		},
	}

	// Compact mode
	m.expandLatestTools = false
	output := m.buildContent()
	assert.Contains(t, output, "→ foo")
	assert.Contains(t, output, "→ bar")
	assert.Contains(t, output, "← result1")
	assert.Contains(t, output, "← result2")

	// Expanded mode
	m.expandLatestTools = true
	output = m.buildContent()
	assert.Contains(t, output, "Calling: foo({})")
	assert.Contains(t, output, "Calling: bar({})")
	assert.Contains(t, output, "Tool: ")
	assert.Contains(t, output, "result1")
	assert.Contains(t, output, "result2")
}

func TestBuildContent_MixedBlocks(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))

	// A single assistant turn can contain text, tool_call, and reasoning
	// blocks interleaved. tool_result blocks belong in separate RoleTool turns.
	m.turns = []renderedTurn{
		{
			role: state.RoleAssistant,
			blocks: []renderedBlock{
				{kind: "text", source: "intro", rendered: "intro"},
				{kind: "tool_call", source: "Calling: foo({})", compact: "foo"},
				{kind: "reasoning", source: "think", rendered: "think"},
				{kind: "text", source: "outro", rendered: "outro"},
			},
		},
		{
			role: state.RoleTool,
			blocks: []renderedBlock{
				{kind: "tool_result", source: "result", compact: "result"},
			},
		},
	}

	output := m.buildContent()

	// Verify order: text → tool_call → reasoning → text → tool_result
	idxIntro := strings.Index(output, "intro")
	idxFoo := strings.Index(output, "→ foo")
	idxThink := strings.Index(output, "Thinking: ")
	idxOutro := strings.Index(output, "outro")
	idxResult := strings.Index(output, "← result")

	require.GreaterOrEqual(t, idxIntro, 0, "intro should be found")
	require.GreaterOrEqual(t, idxFoo, 0, "foo should be found")
	require.GreaterOrEqual(t, idxThink, 0, "Thinking should be found")
	require.GreaterOrEqual(t, idxOutro, 0, "outro should be found")
	require.GreaterOrEqual(t, idxResult, 0, "result should be found")

	assert.Greater(t, idxFoo, idxIntro, "tool_call should follow text")
	assert.Greater(t, idxThink, idxFoo, "reasoning should follow tool_call")
	assert.Greater(t, idxOutro, idxThink, "text should follow reasoning")
	assert.Greater(t, idxResult, idxOutro, "tool_result should follow text")
}

func TestView_ZeroWidthViewport_NoPanic(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(0), viewport.WithHeight(10))
	m.turns = []renderedTurn{
		{role: state.RoleUser, blocks: []renderedBlock{{kind: "text", source: "hello world"}}},
	}

	// Should not panic with zero-width viewport.
	_ = m.View().Content
}

func TestBuildContent_ToggleNoToolBlocks(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))

	m.turns = []renderedTurn{
		{role: state.RoleAssistant, blocks: []renderedBlock{{kind: "text", source: "hello", rendered: "hello"}}},
	}

	// Toggle on — no tool blocks, view should be unchanged
	m.expandLatestTools = true
	output := m.buildContent()
	assert.Contains(t, output, "Assistant: ")
	assert.Contains(t, output, "hello")
	assert.NotContains(t, output, "→")
	assert.NotContains(t, output, "←")
}

func TestBuildContent_CompactToolCall_AmberStyling(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))

	m.turns = []renderedTurn{
		{
			role: state.RoleAssistant,
			blocks: []renderedBlock{
				{kind: "tool_call", source: "Calling: foo({})", compact: "foo"},
			},
		},
	}

	m.expandLatestTools = false
	output := m.buildContent()
	expected := compactToolCallStyle.Render("→ foo")
	assert.Contains(t, output, expected)
}
