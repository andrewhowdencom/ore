package tui

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/viewport"
	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/state"
	"github.com/andrewhowdencom/ore/x/conduit"
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
		{role: state.RoleAssistant, blocks: []renderedBlock{{title: "Assistant", style: assistantStyle, expandedByDefault: true, kind: "text", source: "# Hello", rendered: "pre-rendered glamour output"}}},
	}
	m.syncViewport()
	output := m.View().Content
	assert.Contains(t, output, "Assistant")
	assert.Contains(t, output, "7 B")
	assert.NotContains(t, output, "· |s|")
	assert.Contains(t, output, "pre-rendered glamour output")
	// Should not contain the raw Markdown source.
	assert.NotContains(t, output, "# Hello")
	idxLabel := strings.Index(output, "Assistant")
	idxContent := strings.Index(output, "pre-rendered glamour output")
	assert.Greater(t, idxContent, idxLabel, "content should appear after label")
	segment := output[idxLabel:idxContent]
	assert.Contains(t, segment, "\n", "label and content should be on separate lines")
}

func TestModel_View_AssistantTurn_FallbackToPlainText(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.turns = []renderedTurn{
		{role: state.RoleAssistant, blocks: []renderedBlock{{title: "Assistant", style: assistantStyle, expandedByDefault: true, kind: "text", source: "plain text"}}},
	}
	m.syncViewport()
	output := m.View().Content
	assert.Contains(t, output, "Assistant")
	assert.NotContains(t, output, "· |s|")
	assert.Contains(t, output, "plain text")
	idxLabel := strings.Index(output, "Assistant")
	idxContent := strings.Index(output, "plain text")
	assert.Greater(t, idxContent, idxLabel, "content should appear after label")
	segment := output[idxLabel:idxContent]
	assert.Contains(t, segment, "\n", "label and content should be on separate lines")
}

func TestModel_View_AssistantTurn_WithReasoning(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.expandLatestDetails = true
	m.turns = []renderedTurn{
		{role: state.RoleAssistant, blocks: []renderedBlock{
			{title: "Assistant", style: assistantStyle, expandedByDefault: true, kind: "text", source: "the answer"},
			{title: "Thinking", style: thinkingStyle, expandedByDefault: false, kind: "reasoning", source: "because 2+2=4"},
		}},
	}
	m.syncViewport()
	output := m.View().Content
	assert.Contains(t, output, "Assistant")
	assert.NotContains(t, output, "· |s|")
	assert.Contains(t, output, "the answer")
	assert.Contains(t, output, "Thinking")
	assert.Contains(t, output, "13 B")
	assert.Contains(t, output, "because 2+2=4")
	// Verify order: text appears before reasoning.
	idxAnswer := strings.Index(output, "the answer")
	idxReason := strings.Index(output, "because 2+2=4")
	assert.Greater(t, idxReason, idxAnswer, "reasoning should appear after text")
}

func TestModel_View_AssistantTurn_MultiBlockSpacing(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.expandLatestDetails = true
	m.turns = []renderedTurn{
		{role: state.RoleAssistant, blocks: []renderedBlock{
			{title: "Thinking", style: thinkingStyle, expandedByDefault: false, kind: "reasoning", source: "let me think..."},
			{title: "Assistant", style: assistantStyle, expandedByDefault: true, kind: "text", source: "the answer"},
		}},
	}
	m.syncViewport()
	output := m.View().Content
	assert.Contains(t, output, "Thinking")
	assert.Contains(t, output, "let me think...")
	assert.Contains(t, output, "Assistant")
	assert.NotContains(t, output, "· |s|")
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
	// Simulate incremental artifact event arriving before TurnCompleteEvent.
	newM, _ := m.Update(artifactMsg{artifact: artifact.ReasoningDelta{Content: "let me think..."}})
	mm := newM.(*model)
	turn := state.Turn{
		Role: state.RoleAssistant,
		Artifacts: []artifact.Artifact{
			artifact.Reasoning{Content: "let me think..."},
		},
	}
	newM2, _ := mm.Update(turnMsg{turn: turn})
	mm2 := newM2.(*model)
	mm2.expandLatestDetails = true
	mm2.contentDirty = true
	mm2.syncViewport()
	output := mm2.View().Content
	assert.Contains(t, output, "Thinking")
	assert.NotContains(t, output, "· |s|")
	assert.Contains(t, output, "rendered-reasoning")
	assert.NotContains(t, output, "let me think...")
}

func TestBuildContent_CacheHit(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.turns = []renderedTurn{
		{role: state.RoleUser, blocks: []renderedBlock{{title: "You", style: lipgloss.NewStyle(), expandedByDefault: true, kind: "text", source: "hello"}}},
	}
	first := m.buildContent()
	require.False(t, m.contentDirty, "buildContent should clear dirty flag")
	require.NotEmpty(t, m.cachedContent, "buildContent should populate cache")

	second := m.buildContent()
	assert.Equal(t, first, second, "second call should return cached content without recomputing")
	assert.False(t, m.contentDirty, "dirty flag should remain false on cache hit")
}

func TestBuildContent_Reasoning_Compact(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.turns = []renderedTurn{
		{role: state.RoleAssistant, blocks: []renderedBlock{
			{title: "Assistant", style: assistantStyle, expandedByDefault: true, kind: "text", source: "the answer", rendered: "the answer"},
			{title: "Thinking", style: thinkingStyle, expandedByDefault: false, kind: "reasoning", source: "because 2+2=4"},
		}},
	}
	output := m.buildContent()
	assert.Contains(t, output, "Assistant")
	assert.NotContains(t, output, "· |s|")
	assert.Contains(t, output, "the answer")
	assert.Contains(t, output, "Thinking")
	assert.Contains(t, output, "13 B")
	assert.NotContains(t, output, "because 2+2=4")
}

func TestBuildContent_Reasoning_Expanded(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.turns = []renderedTurn{
		{role: state.RoleAssistant, blocks: []renderedBlock{
			{title: "Assistant", style: assistantStyle, expandedByDefault: true, kind: "text", source: "the answer", rendered: "the answer"},
			{title: "Thinking", style: thinkingStyle, expandedByDefault: false, kind: "reasoning", source: "because 2+2=4", rendered: "rendered-reasoning"},
		}},
	}
	m.expandLatestDetails = true
	output := m.buildContent()
	assert.Contains(t, output, "Assistant")
	assert.NotContains(t, output, "· |s|")
	assert.Contains(t, output, "the answer")
	assert.Contains(t, output, "Thinking")
	assert.Contains(t, output, "rendered-reasoning")
	assert.NotContains(t, output, "Thinking...")
}

func TestBuildContent_Reasoning_OldTurn_AlwaysCompact(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.turns = []renderedTurn{
		{role: state.RoleAssistant, blocks: []renderedBlock{
			{title: "Assistant", style: assistantStyle, expandedByDefault: true, kind: "text", source: "first answer", rendered: "first answer"},
			{title: "Thinking", style: thinkingStyle, expandedByDefault: false, kind: "reasoning", source: "first reasoning", rendered: "first-reasoning"},
		}},
		{role: state.RoleAssistant, blocks: []renderedBlock{
			{title: "Assistant", style: assistantStyle, expandedByDefault: true, kind: "text", source: "latest answer", rendered: "latest answer"},
			{title: "Thinking", style: thinkingStyle, expandedByDefault: false, kind: "reasoning", source: "latest reasoning", rendered: "latest-reasoning"},
		}},
	}
	m.expandLatestDetails = true
	output := m.buildContent()
	// The latest reasoning should be expanded
	assert.Contains(t, output, "latest-reasoning")
	// The old reasoning should stay compact
	assert.NotContains(t, output, "first-reasoning")
}

func TestModel_Update_KeyCtrlO_TogglesReasoningExpansion(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.md = mockMarkdownRenderer{output: "rendered-reasoning"}

	// Simulate incremental artifact event arriving before TurnCompleteEvent.
	newM, _ := m.Update(artifactMsg{artifact: artifact.ReasoningDelta{Content: "let me think..."}})
	mm := newM.(*model)
	turn := state.Turn{
		Role: state.RoleAssistant,
		Artifacts: []artifact.Artifact{
			artifact.Reasoning{Content: "let me think..."},
		},
	}
	newM2, _ := mm.Update(turnMsg{turn: turn})
	mm2 := newM2.(*model)

	// Default: collapsed — completed reasoning shows byte count
	output := mm2.buildContent()
	assert.Contains(t, output, "Thinking")
	assert.Contains(t, output, "15 B")
	assert.NotContains(t, output, "rendered-reasoning")

	// Toggle open
	newM3, _ := mm2.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	mm3 := newM3.(*model)
	output2 := mm3.buildContent()
	assert.Contains(t, output2, "Thinking")
	assert.NotContains(t, output2, "· |s|")
	assert.Contains(t, output2, "rendered-reasoning")
	assert.NotContains(t, output2, "Thinking...")

	// Toggle closed again
	newM4, _ := mm3.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	mm4 := newM4.(*model)
	output3 := mm4.buildContent()
	assert.Contains(t, output3, "Thinking")
	assert.Contains(t, output3, "15 B")
	assert.NotContains(t, output3, "rendered-reasoning")
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

func TestRenderBlockUnified_HeaderWithTimestamp(t *testing.T) {
	block := renderedBlock{kind: "text", source: "hello", title: "Assistant", style: lipgloss.NewStyle(), expandedByDefault: true}
	ts := time.Date(2024, 1, 1, 12, 30, 45, 0, time.UTC)
	output := renderBlockUnified(block, ts, true, 80)
	assert.Contains(t, output, "12:30:45")
	assert.Contains(t, output, "Assistant")
	assert.Contains(t, output, "5 B")
	assert.NotContains(t, output, "· |s|")
	assert.Contains(t, output, "hello")
	// No blank line between header and body.
	assert.NotContains(t, output, "\n\n", "header and body must not be separated by a blank line")
}

func TestRenderBlockUnified_HeaderWithoutTimestamp(t *testing.T) {
	block := renderedBlock{kind: "text", source: "hello", title: "Assistant", style: lipgloss.NewStyle(), expandedByDefault: true}
	output := renderBlockUnified(block, time.Time{}, true, 80)
	assert.Contains(t, output, "Assistant")
	assert.Contains(t, output, "5 B")
	assert.NotContains(t, output, "· |s|")
	assert.Contains(t, output, "hello")
	// No blank line between header and body.
	assert.NotContains(t, output, "\n\n", "header and body must not be separated by a blank line")
}

// TestRenderBlockUnified_NoBlankLineBetweenHeaderAndBody locks in the
// header-to-body spacing: the line immediately after the header is body
// content, and no double-newline appears anywhere in the output.
func TestRenderBlockUnified_NoBlankLineBetweenHeaderAndBody(t *testing.T) {
	block := renderedBlock{kind: "text", source: "hello", title: "You", style: lipgloss.NewStyle(), expandedByDefault: true}
	ts := time.Date(2024, 1, 1, 12, 30, 45, 0, time.UTC)
	output := renderBlockUnified(block, ts, true, 80)

	// (a) The line immediately after the header must be body content —
	// i.e. "hello", not the empty string left by a blank row.
	lines := strings.Split(output, "\n")
	require.GreaterOrEqual(t, len(lines), 2, "output should have at least header + body line")
	assert.Equal(t, "hello", lines[1], "first body line must be body content, not a blank line")

	// (b) No double-newline anywhere — header-to-body must be a single "\n".
	assert.Equal(t, 0, strings.Count(output, "\n\n"), "no double-newline may appear in the block output")
}

func TestRenderBlockUnified_CompactReasoning(t *testing.T) {
	block := renderedBlock{kind: "reasoning", source: "deep thought", title: "Thinking", style: lipgloss.NewStyle(), compact: "Thinking 12 B", expandedByDefault: false}
	ts := time.Date(2024, 1, 1, 12, 30, 45, 0, time.UTC)
	output := renderBlockUnified(block, ts, false, 80)
	assert.Contains(t, output, "Thinking")
	assert.Contains(t, output, "12 B")
	assert.NotContains(t, output, "· |s|")
	// Reasoning compact should NOT include body
	assert.NotContains(t, output, "deep thought")
}

func TestRenderBlockUnified_ExpandedReasoning(t *testing.T) {
	block := renderedBlock{kind: "reasoning", source: "deep thought", title: "Thinking", style: lipgloss.NewStyle(), compact: "Thinking 12 B", expandedByDefault: false}
	ts := time.Date(2024, 1, 1, 12, 30, 45, 0, time.UTC)
	output := renderBlockUnified(block, ts, true, 80)
	assert.Contains(t, output, "Thinking")
	assert.Contains(t, output, "12 B")
	assert.NotContains(t, output, "· |s|")
	assert.Contains(t, output, "deep thought")
}

func TestRenderBlockUnified_CompactToolCall(t *testing.T) {
	block := renderedBlock{kind: "tool_call", source: "{}", compact: "bash · command=\"test\"", title: "Assistant · Call bash", style: lipgloss.NewStyle(), expandedByDefault: false}
	ts := time.Date(2024, 1, 1, 12, 30, 45, 0, time.UTC)
	output := renderBlockUnified(block, ts, false, 80)
	assert.Contains(t, output, "Assistant · Call bash")
	assert.Contains(t, output, "bash · command=\"test\"")
}

func TestRenderBlockUnified_EmptyBody(t *testing.T) {
	block := renderedBlock{kind: "text", source: "", title: "Assistant", style: lipgloss.NewStyle(), expandedByDefault: true}
	ts := time.Date(2024, 1, 1, 12, 30, 45, 0, time.UTC)
	output := renderBlockUnified(block, ts, true, 80)
	assert.Contains(t, output, "Assistant")
	// Empty body early-returns the header alone — no trailing newline.
	assert.NotContains(t, output, "\n", "empty body should produce header-only output")
	// The header (with timestamp) and the zero-byte count are both present.
	assert.Contains(t, output, "12:30:45 Assistant")
	assert.Contains(t, output, "0 B")
	assert.NotContains(t, output, "· |s|")
}

func TestRenderBlockUnified_WrapsContent(t *testing.T) {
	text := strings.Repeat("a", 100)
	block := renderedBlock{kind: "text", source: text, title: "Assistant", style: lipgloss.NewStyle(), expandedByDefault: true}
	ts := time.Date(2024, 1, 1, 12, 30, 45, 0, time.UTC)
	output := renderBlockUnified(block, ts, true, 20)
	lines := strings.Split(output, "\n")
	assert.Greater(t, len(lines), 2, "long text should wrap to multiple lines")
	// First line is the header, second line is body content (not blank).
	assert.Contains(t, lines[0], "Assistant")
	assert.NotEmpty(t, lines[1], "first body line after header should be non-blank")
	// No double-newlines anywhere — header-to-body must be tight.
	assert.NotContains(t, output, "\n\n", "header and body must not be separated by a blank line")
}

func TestRenderBlockUnified_Unicode(t *testing.T) {
	text := "こんにちは世界"
	block := renderedBlock{kind: "text", source: text, title: "You", style: lipgloss.NewStyle(), expandedByDefault: true}
	ts := time.Date(2024, 1, 1, 12, 30, 45, 0, time.UTC)
	output := renderBlockUnified(block, ts, true, 12)
	lines := strings.Split(output, "\n")
	// First line is header
	assert.Contains(t, lines[0], "You")
}

func TestRenderBlockUnified_NegativeWidth(t *testing.T) {
	block := renderedBlock{kind: "text", source: "hello", title: "Assistant", style: lipgloss.NewStyle(), expandedByDefault: true}
	output := renderBlockUnified(block, time.Time{}, true, -1)
	assert.Contains(t, output, "hello")
}

func TestRenderBlockUnified_ToolResultErrorStyle(t *testing.T) {
	block := renderedBlock{kind: "tool_result", source: "Error: failed", title: "Tool Result", style: errorStyle, expandedByDefault: false}
	ts := time.Date(2024, 1, 1, 12, 30, 45, 0, time.UTC)
	output := renderBlockUnified(block, ts, false, 80)
	assert.Contains(t, output, "Tool Result")
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
	// Simulate incremental artifact event arriving before TurnCompleteEvent.
	newM, _ := m.Update(artifactMsg{artifact: artifact.ReasoningDelta{Content: "let me think..."}})
	mm := newM.(*model)
	turn := state.Turn{
		Role: state.RoleAssistant,
		Artifacts: []artifact.Artifact{
			artifact.Reasoning{Content: "let me think..."},
		},
	}
	newM2, _ := mm.Update(turnMsg{turn: turn})
	mm2 := newM2.(*model)
	require.Len(t, mm2.turns, 1)
	require.Len(t, mm2.turns[0].blocks, 1)
	assert.Empty(t, mm2.turns[0].blocks[0].rendered, "render error should leave rendered empty")
	assert.Equal(t, "let me think...", mm2.turns[0].blocks[0].source, "raw text should still be stored")

	mm2.expandLatestDetails = true
	mm2.contentDirty = true
	mm2.syncViewport()
	output := mm2.View().Content
	assert.Contains(t, output, "Thinking")
	assert.NotContains(t, output, "· |s|")
	assert.Contains(t, output, "let me think...")
}

func TestModel_View_WindowTitle(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.name = "TestApp"
	m.status = map[string]string{"phase": "submitted"}
	v := m.View()
	assert.Equal(t, "TestApp [...]", v.WindowTitle)
}

func TestModel_View_WindowTitle_ErrorPhase(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.name = "TestApp"
	m.status = map[string]string{"phase": "error"}
	v := m.View()
	assert.Equal(t, "TestApp [err]", v.WindowTitle)
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
	output := compactToolResult("result data", 80)
	assert.Equal(t, "result data", output)
}

func TestCompactToolResult_Multiline(t *testing.T) {
	output := compactToolResult("line1\nline2\nline3", 80)
	assert.Equal(t, "line1\nline2\nline3", output)
}

func TestCompactToolResult_MultilineOverflow(t *testing.T) {
	output := compactToolResult("line1\nline2\nline3\nline4", 80)
	assert.Equal(t, "line1\nline2\nline3…", output)
}

func TestCompactToolResult_Error(t *testing.T) {
	output := compactToolResult("Error: failed", 80)
	assert.Equal(t, "Error: failed", output)
}

func TestCompactToolResult_Truncation(t *testing.T) {
	output := compactToolResult(strings.Repeat("a", 100), 10)
	assert.Equal(t, "aaaaaaaaa…", output)
}

func TestTruncateString(t *testing.T) {
	assert.Equal(t, "hello", truncateString("hello", 10))
	assert.Equal(t, "hell…", truncateString("hello world", 5))
	assert.Equal(t, "hello", truncateString("hello", 0))
	assert.Equal(t, "…", truncateString("hello", 1))
}

func TestTruncateString_ANSIAware(t *testing.T) {
	// ANSI-styled string: raw rune count is 14, visible width is 5.
	ansiHello := "\x1b[31mhello\x1b[0m"

	// Old implementation would see 14 runes > 10 and truncate to hel….
	// New implementation sees 5 visible chars < 10 and returns full string.
	assert.Equal(t, ansiHello, truncateString(ansiHello, 10),
		"should not truncate when visible width fits")

	// When truncation is needed, visible characters should be preserved
	// with ANSI codes intact.
	result := truncateString(ansiHello, 3)
	assert.Contains(t, result, "he…",
		"should truncate to visible width")
	// Verify ANSI codes are preserved.
	assert.Contains(t, result, "\x1b[31m")
	assert.Contains(t, result, "\x1b[0m")
}

func TestBuildContent_ExpandLatestTools_Toggle(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))

	m.turns = []renderedTurn{
		{
			role: state.RoleAssistant,
			blocks: []renderedBlock{
				{
					kind:       "tool_call",
					source:     "Calling: search_files({\"path\": \".\", \"query\": \"hello\"})",
					compact:    "search_files · path=\".\" · query=\"hello\"",
					toolCallID: "call_1",
				},
			},
		},
		{
			role: state.RoleTool,
			blocks: []renderedBlock{
				{
					kind:       "tool_result",
					source:     "result data",
					compact:    "result data",
					toolCallID: "call_1",
				},
			},
		},
	}

	// Compact mode (default): blocks use borderless header styles.
	m.expandLatestDetails = false
	compactOutput := m.buildContent()
	assert.Contains(t, compactOutput, "search_files")
	assert.Contains(t, compactOutput, "result data")

	// Expanded mode: shows full source content.
	m.expandLatestDetails = true
	m.contentDirty = true
	expandedOutput := m.buildContent()
	assert.Contains(t, expandedOutput, "Calling: search_files")
	assert.Contains(t, expandedOutput, "result data")
}

func TestBuildContent_CompactToolError_RedStyling(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))

	m.turns = []renderedTurn{
		{
			role: state.RoleAssistant,
			blocks: []renderedBlock{
				{title: "Tool", style: assistantStyle, expandedByDefault: false, kind: "tool_call", source: "Calling: foo({})", compact: "foo", toolCallID: "call_1"},
			},
		},
		{
			role: state.RoleTool,
			blocks: []renderedBlock{
				{
					kind:       "tool_result",
					source:     "Error: failed",
					compact:    "Error: failed",
					toolCallID: "call_1",
				},
			},
		},
	}

	// Compact mode: error block should be wrapped in errorStyle.
	m.expandLatestDetails = false
	output := m.buildContent()
	assert.Contains(t, output, "Error: failed")

	// Expanded mode: error block should be wrapped in errorStyle.
	m.expandLatestDetails = true
	m.contentDirty = true
	output = m.buildContent()
	assert.Contains(t, output, "Error: failed")
	assert.Contains(t, output, "Tool") // unified header present
	assert.NotContains(t, output, "· |s|")
}

func TestBuildContent_MultipleToolCalls(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))

	m.turns = []renderedTurn{
		{
			role: state.RoleAssistant,
			blocks: []renderedBlock{
				{title: "Tool", style: assistantStyle, expandedByDefault: false, kind: "tool_call", source: "Calling: foo({})", compact: "foo", toolCallID: "call_1"},
				{title: "Tool", style: assistantStyle, expandedByDefault: false, kind: "tool_call", source: "Calling: bar({})", compact: "bar", toolCallID: "call_2"},
			},
		},
		{
			role: state.RoleTool,
			blocks: []renderedBlock{
				{title: "Tool Result", style: toolResultStyle, expandedByDefault: false, kind: "tool_result", source: "result1", compact: "result1", toolCallID: "call_1"},
				{title: "Tool Result", style: toolResultStyle, expandedByDefault: false, kind: "tool_result", source: "result2", compact: "result2", toolCallID: "call_2"},
			},
		},
	}

	// Compact mode: blocks use borderless header styles.
	m.expandLatestDetails = false
	output := m.buildContent()
	assert.Contains(t, output, "foo")
	assert.Contains(t, output, "bar")
	assert.Contains(t, output, "result1")
	assert.Contains(t, output, "result2")

	// Expanded mode: shows full source content.
	m.expandLatestDetails = true
	m.contentDirty = true
	output = m.buildContent()
	assert.Contains(t, output, "Calling: foo({})")
	assert.Contains(t, output, "Calling: bar({})")
	assert.Contains(t, output, "result1")
	assert.Contains(t, output, "result2")
}

func TestBuildContent_MixedBlocks(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.expandLatestDetails = true

	// A single assistant turn can contain text, tool_call, and reasoning
	// blocks interleaved. tool_result blocks belong in separate RoleTool turns.
	m.turns = []renderedTurn{
		{
			role: state.RoleAssistant,
			blocks: []renderedBlock{
				{title: "Tool", style: lipgloss.NewStyle(), expandedByDefault: true, kind: "text", source: "intro", rendered: "intro"},
				{title: "Tool", style: assistantStyle, expandedByDefault: false, kind: "tool_call", source: "Calling: foo({})", compact: "foo", toolCallID: "call_1"},
				{title: "Thinking", style: thinkingStyle, expandedByDefault: false, kind: "reasoning", source: "think", rendered: "think"},
				{title: "Tool", style: lipgloss.NewStyle(), expandedByDefault: true, kind: "text", source: "outro", rendered: "outro"},
			},
		},
		{
			role: state.RoleTool,
			blocks: []renderedBlock{
				{title: "Tool Result", style: toolResultStyle, expandedByDefault: false, kind: "tool_result", source: "result", compact: "result", toolCallID: "call_1"},
			},
		},
	}

	output := m.buildContent()

	// Verify order: text → tool_call → reasoning → text → tool_result
	// All blocks are expanded, so tool_call shows "Calling:" and tool_result
	// shows "Tool:" instead of the compact arrows.
	idxIntro := strings.Index(output, "intro")
	idxFoo := strings.Index(output, "Calling: foo({})")
	idxThink := strings.Index(output, "Thinking")
	idxOutro := strings.Index(output, "outro")
	idxResult := strings.Index(output, "result")

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
		{role: state.RoleUser, blocks: []renderedBlock{{title: "You", style: lipgloss.NewStyle(), expandedByDefault: true, kind: "text", source: "hello world"}}},
	}

	// Should not panic with zero-width viewport.
	_ = m.View().Content
}

func TestBuildContent_ToggleNoToolBlocks(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))

	m.turns = []renderedTurn{
		{role: state.RoleAssistant, blocks: []renderedBlock{{title: "Assistant", style: assistantStyle, expandedByDefault: true, kind: "text", source: "hello", rendered: "hello"}}},
	}

	// Toggle on — no tool blocks, view should be unchanged
	m.expandLatestDetails = true
	output := m.buildContent()
	assert.Contains(t, output, "Assistant")
	assert.NotContains(t, output, "· |s|")
	assert.Contains(t, output, "hello")
}

func TestBuildContent_CompactToolCall_BlockStyling(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))

	m.turns = []renderedTurn{
		{
			role: state.RoleAssistant,
			blocks: []renderedBlock{
				{title: "Tool", style: assistantStyle, expandedByDefault: false, kind: "tool_call", source: "Calling: foo({})", compact: "foo", toolCallID: "call_1"},
			},
		},
	}

	m.expandLatestDetails = false
	output := m.buildContent()
	assert.Contains(t, output, "foo")
}

func TestModel_View_MixedArtifacts_Rendered(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.md = mockMarkdownRenderer{output: "rendered"}

	// Simulate incremental artifact events arriving before TurnCompleteEvent.
	newM, _ := m.Update(artifactMsg{artifact: artifact.TextDelta{Content: "hello"}})
	mm := newM.(*model)
	newM2, _ := mm.Update(artifactMsg{artifact: artifact.ReasoningDelta{Content: "think"}})
	mm2 := newM2.(*model)
	newM3, _ := mm2.Update(artifactMsg{artifact: artifact.ToolCall{ID: "call_1", Name: "foo", Arguments: "{}"}})
	mm3 := newM3.(*model)

	newM3, _ = mm3.Update(renderTickMsg{})
	mm3 = newM3.(*model)

	mm3.syncViewport()
	output := mm3.View().Content
	assert.Contains(t, output, "Assistant")
	assert.NotContains(t, output, "· |s|")
	assert.Contains(t, output, "rendered") // text block
	assert.Contains(t, output, "Thinking")
	assert.Contains(t, output, "5 B") // reasoning is completed (not last block)
}

func TestModel_View_IncrementalToolCall_CompactAndExpanded(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.md = mockMarkdownRenderer{output: "rendered"}

	// Simulate incremental artifact event arriving before TurnCompleteEvent.
	newM, _ := m.Update(artifactMsg{artifact: artifact.ToolCall{ID: "call_1", Name: "foo", Arguments: "{}"}})
	mm := newM.(*model)
	mm.syncViewport()
	output1 := mm.View().Content
	assert.Contains(t, output1, "foo") // compact content shown in styled block

	// Toggle expanded.
	mm.expandLatestDetails = true
	mm.contentDirty = true
	mm.syncViewport()
	output2 := mm.View().Content
	assert.Contains(t, output2, "rendered") // rendered content shown in styled block
}

func TestModel_View_IncrementalReasoning_ExpandCollapse(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.md = mockMarkdownRenderer{output: "rendered-reasoning"}

	// Simulate incremental artifact event arriving before TurnCompleteEvent.
	newM, _ := m.Update(artifactMsg{artifact: artifact.ReasoningDelta{Content: "let me think..."}})
	mm := newM.(*model)
	newM, _ = mm.Update(renderTickMsg{})
	mm = newM.(*model)
	mm.syncViewport()
	output1 := mm.View().Content
	assert.Contains(t, output1, "Thinking")
	assert.Contains(t, output1, "15 B")
	assert.NotContains(t, output1, "rendered-reasoning")

	// Toggle expanded.
	mm.expandLatestDetails = true
	mm.contentDirty = true
	mm.syncViewport()
	output2 := mm.View().Content
	assert.Contains(t, output2, "Thinking")
	assert.NotContains(t, output2, "· |s|")
	assert.Contains(t, output2, "rendered-reasoning")
	assert.NotContains(t, output2, "Thinking...")

	// Toggle collapsed again.
	mm.expandLatestDetails = false
	mm.contentDirty = true
	mm.syncViewport()
	output3 := mm.View().Content
	assert.Contains(t, output3, "Thinking")
	assert.Contains(t, output3, "15 B")
	assert.NotContains(t, output3, "rendered-reasoning")
}

func TestBuildContent_ActiveReasoning_Counter(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.currentTurn.blocks = []renderedBlock{
		{title: "Thinking", style: thinkingStyle, expandedByDefault: false, kind: "reasoning", source: "abc"},
	}
	output := m.buildContent()
	assert.Contains(t, output, "Thinking")
	assert.Contains(t, output, "3 B")
	assert.NotContains(t, output, "Thinking...")
}

func TestBuildContent_ActiveReasoning_UnicodeCounter(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.currentTurn.blocks = []renderedBlock{
		{title: "Thinking", style: thinkingStyle, expandedByDefault: false, kind: "reasoning", source: "日本語"},
	}
	output := m.buildContent()
	assert.Contains(t, output, "Thinking")
	assert.Contains(t, output, "9 B")
	assert.NotContains(t, output, "Thinking...")
}

func TestBuildContent_CompletedReasoning_CharCount(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	// Reasoning followed by text means reasoning is no longer the active (last) block.
	m.currentTurn.blocks = []renderedBlock{
		{title: "Thinking", style: thinkingStyle, expandedByDefault: false, kind: "reasoning", source: "let me think..."},
		{title: "Assistant", style: assistantStyle, expandedByDefault: true, kind: "text", source: "the answer"},
	}
	output := m.buildContent()
	assert.Contains(t, output, "Thinking")
	assert.Contains(t, output, "15 B")
}

func TestBuildContent_Reasoning_Expanded_NoCounter(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.expandLatestDetails = true
	m.currentTurn.blocks = []renderedBlock{
		{title: "Thinking", style: thinkingStyle, expandedByDefault: false, kind: "reasoning", source: "let me think...", rendered: "rendered-reasoning"},
	}
	output := m.buildContent()
	assert.Contains(t, output, "Thinking")
	assert.NotContains(t, output, "· |s|")
	assert.Contains(t, output, "rendered-reasoning")
	assert.NotContains(t, output, "Thinking...")
}

func TestBuildContent_HistoricalReasoning_CharCount(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.turns = []renderedTurn{
		{role: state.RoleAssistant, blocks: []renderedBlock{
			{title: "Thinking", style: thinkingStyle, expandedByDefault: false, kind: "reasoning", source: "historical reasoning"},
		}},
	}
	output := m.buildContent()
	assert.Contains(t, output, "Thinking")
	assert.Contains(t, output, "20 B")
}

// mockMarkdownValue is a test double that implements artifact.MarkdownRenderer.
type mockMarkdownValue struct {
	output string
}

func (m mockMarkdownValue) MarshalMarkdown() string {
	return m.output
}

func TestRenderArtifact_ToolCall_MarkdownRenderer(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	tc := artifact.ToolCall{
		ID:        "call_1",
		Name:      "bash",
		Arguments: `{"command":"go test ./..."}`,
		Value:     mockMarkdownValue{output: "```bash\n$ go test ./...\n```"},
	}
	block := m.renderArtifact(tc, state.RoleAssistant)
	assert.Equal(t, "tool_call", block.kind)
	assert.Equal(t, "call_1", block.toolCallID)
	assert.Equal(t, "```bash\n$ go test ./...\n```", block.source)
	// compact should still use compactToolCall (which ignores Value for compact view)
	assert.NotEmpty(t, block.compact)
}

func TestRenderArtifact_ToolCall_FallbackToArguments(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	tc := artifact.ToolCall{
		ID:        "call_1",
		Name:      "search",
		Arguments: `{"q":"hello"}`,
	}
	block := m.renderArtifact(tc, state.RoleAssistant)
	assert.Equal(t, "tool_call", block.kind)
	assert.Equal(t, "call_1", block.toolCallID)
	assert.Equal(t, "Calling: search({\"q\":\"hello\"})", block.source)
}

func TestRenderArtifact_ToolResult_MarkdownRenderer(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.md = mockMarkdownRenderer{output: "# Custom Markdown"}
	tr := artifact.ToolResult{
		ToolCallID: "call_1",
		Content:    `{"raw":"json"}`,
		Value:      mockMarkdownValue{output: "# Custom Markdown"},
	}
	block := m.renderArtifact(tr, state.RoleTool)
	assert.Equal(t, "tool_result", block.kind)
	assert.Equal(t, "call_1", block.toolCallID)
	assert.Equal(t, "# Custom Markdown", block.source)
	assert.Equal(t, "# Custom Markdown", block.rendered)
	assert.Equal(t, "# Custom Markdown", block.compact)
}

func TestRenderArtifact_ToolResult_JSONFallback(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.md = mockMarkdownRenderer{output: "```json\n\"json value\"\n```"}
	tr := artifact.ToolResult{
		ToolCallID: "call_1",
		Content:    "fallback",
		Value:      "json value",
	}
	block := m.renderArtifact(tr, state.RoleTool)
	assert.Equal(t, "tool_result", block.kind)
	assert.Equal(t, "call_1", block.toolCallID)
	assert.Equal(t, "```json\n\"json value\"\n```", block.source)
	assert.Equal(t, "```json\n\"json value\"\n```", block.rendered)
	assert.Equal(t, "```json\n\"json value\"\n```", block.compact)
}

func TestRenderArtifact_ToolResult_Error(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.md = mockMarkdownRenderer{output: "Error: failed"}
	tr := artifact.ToolResult{
		ToolCallID: "call_1",
		Content:    "failed",
		IsError:    true,
	}
	block := m.renderArtifact(tr, state.RoleTool)
	assert.Equal(t, "tool_result", block.kind)
	assert.Equal(t, "call_1", block.toolCallID)
	assert.Equal(t, "Error: failed", block.source)
	assert.Equal(t, "Error: failed", block.rendered)
	assert.Equal(t, "Error: failed", block.compact)
}

func TestRenderArtifact_ToolResult_ContentFallback(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.md = mockMarkdownRenderer{output: "plain content"}
	tr := artifact.ToolResult{
		ToolCallID: "call_1",
		Content:    "plain content",
	}
	block := m.renderArtifact(tr, state.RoleTool)
	assert.Equal(t, "tool_result", block.kind)
	assert.Equal(t, "call_1", block.toolCallID)
	assert.Equal(t, "plain content", block.source)
	assert.Equal(t, "plain content", block.rendered)
	assert.Equal(t, "plain content", block.compact)
}

func TestRenderArtifact_ToolResult_CompactFromRendered(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.md = mockMarkdownRenderer{output: "**styled result**"}
	tr := artifact.ToolResult{
		ToolCallID: "call_1",
		Content:    "raw result",
	}
	block := m.renderArtifact(tr, state.RoleTool)
	assert.Equal(t, "tool_result", block.kind)
	assert.Equal(t, "call_1", block.toolCallID)
	assert.Equal(t, "raw result", block.source)
	assert.Equal(t, "**styled result**", block.rendered)
	assert.Equal(t, "**styled result**", block.compact)
}
func TestBuildStatusLine_TokenKeysGrouped(t *testing.T) {
	status := map[string]string{
		"phase":     "done",
		"sent":      "100",
		"received":  "50",
		"total":     "150",
		"thread_id": "abc-123",
	}
	rendered, lines := buildStatusLine(status, 200)
	assert.Equal(t, 1, lines)
	// Token keys should appear grouped as a single segment with display symbols.
	assert.Contains(t, rendered, "↑ 100")
	assert.Contains(t, rendered, "↓ 50")
	assert.Contains(t, rendered, "Σ 150")
	// Other keys should still appear.
	assert.Contains(t, rendered, "phase: done")
	assert.Contains(t, rendered, "thread_id: abc-123")
}

func TestBuildStatusLine_TokenKeysPartial(t *testing.T) {
	// Only total present; the others should not appear.
	status := map[string]string{
		"total": "42",
	}
	rendered, lines := buildStatusLine(status, 200)
	assert.Equal(t, 1, lines)
	assert.Contains(t, rendered, "Σ 42")
	assert.NotContains(t, rendered, "↑")
	assert.NotContains(t, rendered, "↓")
}

func TestBuildStatusLine_NoTokenKeys(t *testing.T) {
	status := map[string]string{
		"phase": "streaming",
	}
	rendered, lines := buildStatusLine(status, 200)
	assert.Equal(t, 1, lines)
	assert.Contains(t, rendered, "phase: streaming")
	assert.NotContains(t, rendered, "tokens")
}

func TestBuildStatusLine_ZoneGrouping(t *testing.T) {
	segments := []conduit.StatusSegment{
		{Label: "phase", Value: "streaming", Zone: "lifecycle"},
		{Label: "title", Value: "chat", Zone: "lifecycle"},
		{Label: "model", Value: "gpt-4o", Zone: "context"},
	}
	priorities := map[string]int{
		"lifecycle": 0,
		"context":   1,
		"default":   99,
	}
	str, lines := buildStatusLineFromSegments(segments, priorities, 80)
	assert.Equal(t, 2, lines, "zones render on separate lines")
	// Lifecycle zone comes first (priority 0), then context (priority 1).
	assert.Contains(t, str, "Lifecycle:")
	assert.Contains(t, str, "phase: streaming")
	assert.Contains(t, str, "title: chat")
	assert.Contains(t, str, "Context:")
	assert.Contains(t, str, "model: gpt-4o")
}

func TestBuildStatusLine_PriorityTruncation(t *testing.T) {
	segments := []conduit.StatusSegment{
		{Label: "phase", Value: "streaming very deeply and considering all possibilities", Zone: "lifecycle"},
		{Label: "model", Value: "gpt-4", Zone: "context"},
	}
	priorities := map[string]int{
		"lifecycle": 0,
		"context":   1,
	}
	// At width 40, both zones together fit within maxStatusLines (3). The lower-
	// priority "context" zone should now be included.
	str, _ := buildStatusLineFromSegments(segments, priorities, 40)
	assert.Contains(t, str, "phase: streaming")
	// Context zone should be included now that maxStatusLines is 3.
	assert.Contains(t, str, "model:")
}

func TestBuildStatusLine_UnmappedKeysDefaultZone(t *testing.T) {
	segments := []conduit.StatusSegment{
		{Label: "phase", Value: "streaming", Zone: "lifecycle"},
		{Label: "unmapped", Value: "value", Zone: "default"},
	}
	priorities := map[string]int{
		"lifecycle": 0,
		"default":   99,
	}
	str, _ := buildStatusLineFromSegments(segments, priorities, 80)
	// Lifecycle zone label is bold and capitalized.
	assert.Contains(t, str, "Lifecycle:")
	// Default zone should NOT have a zone label (backward compatibility).
	assert.Contains(t, str, "unmapped: value")
	assert.NotContains(t, str, "Default:")
}

func TestBuildStatusLine_WrapsAtWidth(t *testing.T) {
	status := map[string]string{
		"phase": "thinking very deeply about the problem at hand and considering all possibilities",
	}
	width := 30
	str, lines := buildStatusLine(status, width)
	assert.Greater(t, lines, 1, "long status should wrap to multiple lines")
	assert.Contains(t, str, "\n", "wrapped string should contain newlines")

	// Verify ANSI escape sequences are not split across line boundaries by
	// checking that no line ends with a partial escape sequence.
	for _, line := range strings.Split(str, "\n") {
		assert.False(t, strings.HasSuffix(line, "\x1b["), "line should not end with partial ANSI escape: %q", line)
	}
}

func TestBuildStatusLine_NegativeWidth(t *testing.T) {
	status := map[string]string{
		"phase": "submitted",
	}
	rendered, lines := buildStatusLine(status, -1)
	assert.NotEmpty(t, rendered)
	assert.Equal(t, 1, lines)
}

func TestCompactNumber(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"0", "0"},
		{"1", "1"},
		{"12", "12"},
		{"123", "123"},
		{"999", "999"},
		{"1000", "1.0K"},
		{"1234", "1.2K"},
		{"9999", "10.0K"},
		{"10000", "10.0K"},
		{"12345", "12.3K"},
		{"99999", "100.0K"},
		{"100000", "100K"},
		{"123456", "123K"},
		{"999999", "1000K"},
		{"1000000", "1.0M"},
		{"1234567", "1.2M"},
		{"10000000", "10.0M"},
		{"12345678", "12.3M"},
		{"100000000", "100M"},
		{"123456789", "123M"},
		{"1000000000", "1B"},
		{"1234567890", "1B"},
		{"10000000000", "10B"},
		{"invalid", "invalid"},
		{"", ""},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.expected, compactNumber(tc.input))
		})
	}
}

func TestModel_View_ActivitySpinner(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.width = 80

	// No activity: spinner should not appear.
	output := m.View().Content
	assert.NotContains(t, output, "⚙")
	assert.NotContains(t, output, "compacting")

	// Activity active: spinner should appear with description.
	newM, _ := m.Update(activityMsg{active: true, description: "compacting"})
	mm := newM.(*model)
	output = mm.View().Content
	assert.Contains(t, output, "⚙ compacting")

	// Activity inactive: spinner should disappear.
	newM2, _ := mm.Update(activityMsg{active: false, description: ""})
	mm2 := newM2.(*model)
	output = mm2.View().Content
	assert.NotContains(t, output, "⚙ compacting")
}
