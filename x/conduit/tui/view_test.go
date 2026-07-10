package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/viewport"
	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/ledger"
	"github.com/andrewhowdencom/ore/x/conduit"
	"github.com/andrewhowdencom/ore/x/conduit/tui/theme"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderMarkdown(t *testing.T) {
	input := "# Hello\n\nSome **bold** text and `code`."
	output, err := newGlamourMarkdownRenderer(theme.Dark()).Render(input, 80)
	require.NoError(t, err)
	assert.NotEmpty(t, output)
	// Output should differ from input (glamour processes the markdown).
	assert.NotEqual(t, input, output)
}

func TestRenderMarkdown_CodeBlock(t *testing.T) {
	input := "```go\nfunc main() {\n    fmt.Println(\"hi\")\n}\n```"
	output, err := newGlamourMarkdownRenderer(theme.Dark()).Render(input, 80)
	require.NoError(t, err)
	assert.NotEmpty(t, output)
	// Verify glamour processed the code block (output differs from input).
	assert.NotEqual(t, input, output)
}

func TestRenderMarkdown_NegativeWidth(t *testing.T) {
	// glamour.NewTermRenderer may accept any width; ensure we handle
	// a negative width without panic.
	input := "hello"
	output, err := newGlamourMarkdownRenderer(theme.Dark()).Render(input, -1)
	// We allow either success or error; the caller handles errors.
	_ = output
	_ = err
}

func TestModel_View_AssistantTurn_WithRendered(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.turns = []renderedTurn{
		{role: ledger.RoleAssistant, blocks: []renderedBlock{{title: "Assistant", style: m.theme.AssistantStyle, expandedByDefault: true, kind: "text", source: "# Hello", rendered: "pre-rendered glamour output"}}},
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
		{role: ledger.RoleAssistant, blocks: []renderedBlock{{title: "Assistant", style: m.theme.AssistantStyle, expandedByDefault: true, kind: "text", source: "plain text"}}},
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
	m.expandAllDetails = true
	m.turns = []renderedTurn{
		{role: ledger.RoleAssistant, blocks: []renderedBlock{
			{title: "Assistant", style: m.theme.AssistantStyle, expandedByDefault: true, kind: "text", source: "the answer"},
			{title: "Thinking", style: m.theme.ThinkingStyle, expandedByDefault: false, kind: "reasoning", source: "because 2+2=4"},
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
	m.expandAllDetails = true
	m.turns = []renderedTurn{
		{role: ledger.RoleAssistant, blocks: []renderedBlock{
			{title: "Thinking", style: m.theme.ThinkingStyle, expandedByDefault: false, kind: "reasoning", source: "let me think..."},
			{title: "Assistant", style: m.theme.AssistantStyle, expandedByDefault: true, kind: "text", source: "the answer"},
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
	turn := ledger.Turn{
		Role: ledger.RoleAssistant,
		Artifacts: []artifact.Artifact{
			artifact.Reasoning{Content: "let me think..."},
		},
	}
	newM2, _ := mm.Update(turnMsg{turn: turn})
	mm2 := newM2.(*model)
	mm2.expandAllDetails = true
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
		{role: ledger.RoleUser, blocks: []renderedBlock{{title: "You", style: lipgloss.NewStyle(), expandedByDefault: true, kind: "text", source: "hello"}}},
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
		{role: ledger.RoleAssistant, blocks: []renderedBlock{
			{title: "Assistant", style: m.theme.AssistantStyle, expandedByDefault: true, kind: "text", source: "the answer", rendered: "the answer"},
			{title: "Thinking", style: m.theme.ThinkingStyle, expandedByDefault: false, kind: "reasoning", source: "because 2+2=4"},
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
		{role: ledger.RoleAssistant, blocks: []renderedBlock{
			{title: "Assistant", style: m.theme.AssistantStyle, expandedByDefault: true, kind: "text", source: "the answer", rendered: "the answer"},
			{title: "Thinking", style: m.theme.ThinkingStyle, expandedByDefault: false, kind: "reasoning", source: "because 2+2=4", rendered: "rendered-reasoning"},
		}},
	}
	m.expandAllDetails = true
	output := m.buildContent()
	assert.Contains(t, output, "Assistant")
	assert.NotContains(t, output, "· |s|")
	assert.Contains(t, output, "the answer")
	assert.Contains(t, output, "Thinking")
	assert.Contains(t, output, "rendered-reasoning")
	assert.NotContains(t, output, "Thinking...")
}

func TestBuildContent_Reasoning_ExpandAllDetails_GlobalScope(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.turns = []renderedTurn{
		{role: ledger.RoleAssistant, blocks: []renderedBlock{
			{title: "Assistant", style: m.theme.AssistantStyle, expandedByDefault: true, kind: "text", source: "first answer", rendered: "first answer"},
			{title: "Thinking", style: m.theme.ThinkingStyle, expandedByDefault: false, kind: "reasoning", source: "first reasoning", rendered: "first-reasoning"},
		}},
		{role: ledger.RoleAssistant, blocks: []renderedBlock{
			{title: "Assistant", style: m.theme.AssistantStyle, expandedByDefault: true, kind: "text", source: "latest answer", rendered: "latest answer"},
			{title: "Thinking", style: m.theme.ThinkingStyle, expandedByDefault: false, kind: "reasoning", source: "latest reasoning", rendered: "latest-reasoning"},
		}},
	}
	// With global semantics, expandAllDetails = true must expand reasoning
	// blocks in BOTH historical turns, not just the latest.
	m.expandAllDetails = true
	output := m.buildContent()
	assert.Contains(t, output, "latest-reasoning", "latest turn's reasoning should be expanded")
	assert.Contains(t, output, "first-reasoning", "historical turn's reasoning should be expanded under global scope")

	// And with the flag off, neither should be expanded.
	m.expandAllDetails = false
	m.contentDirty = true
	output = m.buildContent()
	assert.NotContains(t, output, "latest-reasoning")
	assert.NotContains(t, output, "first-reasoning")
}

func TestModel_Update_KeyCtrlO_TogglesReasoningExpansion(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.md = mockMarkdownRenderer{output: "rendered-reasoning"}

	// Simulate incremental artifact event arriving before TurnCompleteEvent.
	newM, _ := m.Update(artifactMsg{artifact: artifact.ReasoningDelta{Content: "let me think..."}})
	mm := newM.(*model)
	turn := ledger.Turn{
		Role: ledger.RoleAssistant,
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
		output, err := newGlamourMarkdownRenderer(theme.Dark()).Render(input, 80)
		assert.NoError(t, err, "malformed markdown %q should not error", input)
		assert.NotEmpty(t, output)
	}
}

func TestRenderMarkdown_NarrowWidth(t *testing.T) {
	for _, width := range []int{1, 2, 5} {
		output, err := newGlamourMarkdownRenderer(theme.Dark()).Render("hello world", width)
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
	// Even with an empty body the block must terminate with the single
	// trailing newline required by the buildContent/Gap contract. The
	// only newline in the output is that terminator — no header-to-body
	// blank line is introduced (because there is no body).
	assert.Equal(t, 1, strings.Count(output, "\n"),
		"empty body should produce header + a single trailing newline; got %q", output)
	assert.True(t, strings.HasSuffix(output, "\n"),
		"empty body output must end with \"\\n\"; got %q", output)
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

// TestRenderBlockUnified_NarrowViewport_HidesCount verifies that when the
// viewport is too narrow to fit both the title and the count with a
// single space, the count is dropped entirely (preserving the title).
func TestRenderBlockUnified_NarrowViewport_HidesCount(t *testing.T) {
	block := renderedBlock{kind: "text", source: "hello", title: "Assistant", style: lipgloss.NewStyle(), expandedByDefault: true}
	// width=10: titleW=9, countW=3, titleW+1+countW=13 > 10, but titleW=9 < 10,
	// so the count is hidden and the title is preserved.
	output := renderBlockUnified(block, time.Time{}, true, 10)
	assert.Contains(t, output, "Assistant")
	assert.NotContains(t, output, "5 B")
}

// TestRenderBlockUnified_NarrowViewport_TruncatesTitle verifies that when
// the viewport is narrower than the title itself, the title is truncated
// with the ellipsis suffix and the count is not shown.
func TestRenderBlockUnified_NarrowViewport_TruncatesTitle(t *testing.T) {
	block := renderedBlock{kind: "text", source: "x", title: "VeryLongTitle", style: lipgloss.NewStyle(), expandedByDefault: true}
	// width=8: titleW=13 > 8, so the title is truncated via truncateString.
	output := renderBlockUnified(block, time.Time{}, true, 8)
	assert.Contains(t, output, "…")
	assert.NotContains(t, output, "VeryLongTitle")
	assert.NotContains(t, output, "1 B")
}

// TestRenderBlockUnified_LargeByteCount_Compact verifies that sources
// larger than 1000 bytes are rendered with the compact "<n> B" suffix
// (e.g. "1.5K B") and remain right-aligned in wide viewports.
func TestRenderBlockUnified_LargeByteCount_Compact(t *testing.T) {
	source := strings.Repeat("a", 1500)
	block := renderedBlock{kind: "text", source: source, title: "Assistant", style: lipgloss.NewStyle(), expandedByDefault: true}
	output := renderBlockUnified(block, time.Time{}, true, 80)
	assert.Contains(t, output, "Assistant")
	assert.Contains(t, output, "1.5K B")
	// The count should be right-aligned: "Assistant" is at the start, and
	// "1.5K B" is at the end of the header line.
	idxTitle := strings.Index(output, "Assistant")
	idxCount := strings.Index(output, "1.5K B")
	assert.Greater(t, idxCount, idxTitle, "count should be right of the title on the same line")
	// The header line should be exactly 80 runes wide.
	lines := strings.Split(output, "\n")
	headerLine := lines[0]
	assert.Equal(t, 80, ansi.StringWidth(headerLine), "header should be padded to viewport width")
}

func TestRenderBlockUnified_ToolResultErrorStyle(t *testing.T) {
	block := renderedBlock{kind: "tool_result", source: "Error: failed", title: "Tool Result", style: theme.Dark().ErrorStyle, expandedByDefault: false}
	ts := time.Date(2024, 1, 1, 12, 30, 45, 0, time.UTC)
	output := renderBlockUnified(block, ts, false, 80)
	assert.Contains(t, output, "Tool Result")
}

func TestEmbeddedStyles_MarginZero(t *testing.T) {
	// After the theme consolidation, the glamour document overrides live
	// on the *theme.Theme value (constructed in Go from glamour's upstream
	// DarkStyleConfig / LightStyleConfig with three document-level
	// overrides applied). Verify the overrides are present on both
	// themes so the production renderer never re-introduces the frame
	// padding or the leading/trailing newline that compounded with the
	// buildContent separator to produce two blank lines between messages.
	dark := theme.Dark()
	require.NotNil(t, dark.GlamourStyle.Document.Margin, "dark theme document.margin must be a non-nil pointer")
	assert.Equal(t, uint(0), *dark.GlamourStyle.Document.Margin, "dark theme document.margin should be 0")
	assert.Equal(t, "", dark.GlamourStyle.Document.BlockPrefix, "dark theme document.block_prefix should be empty")
	assert.Equal(t, "", dark.GlamourStyle.Document.BlockSuffix, "dark theme document.block_suffix should be empty")

	light := theme.Light()
	require.NotNil(t, light.GlamourStyle.Document.Margin, "light theme document.margin must be a non-nil pointer")
	assert.Equal(t, uint(0), *light.GlamourStyle.Document.Margin, "light theme document.margin should be 0")
	assert.Equal(t, "", light.GlamourStyle.Document.BlockPrefix, "light theme document.block_prefix should be empty")
	assert.Equal(t, "", light.GlamourStyle.Document.BlockSuffix, "light theme document.block_suffix should be empty")
}

func TestRenderReasoning_ErrorFallback(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.md = mockMarkdownRenderer{err: errors.New("render failed")}
	// Simulate incremental artifact event arriving before TurnCompleteEvent.
	newM, _ := m.Update(artifactMsg{artifact: artifact.ReasoningDelta{Content: "let me think..."}})
	mm := newM.(*model)
	turn := ledger.Turn{
		Role: ledger.RoleAssistant,
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

	mm2.expandAllDetails = true
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
	// After the theme consolidation, mode selection lives in theme.Auto()
	// (or in the explicit Dark / Light factory calls). The production
	// renderer takes a *theme.Theme and applies the theme's glamour style
	// verbatim. Verify the dark theme carries the expected document-level
	// overrides, which is what the production renderer will use.
	th := theme.Dark()
	require.NotNil(t, th.GlamourStyle.Document.Margin, "dark theme document.margin must be a non-nil pointer")
	assert.Equal(t, uint(0), *th.GlamourStyle.Document.Margin, "dark theme document.margin should be 0")
	assert.Equal(t, "", th.GlamourStyle.Document.BlockPrefix, "dark theme document.block_prefix should be empty")
	assert.Equal(t, "", th.GlamourStyle.Document.BlockSuffix, "dark theme document.block_suffix should be empty")
}

func TestRenderer_SelectsLightStyle(t *testing.T) {
	th := theme.Light()
	require.NotNil(t, th.GlamourStyle.Document.Margin, "light theme document.margin must be a non-nil pointer")
	assert.Equal(t, uint(0), *th.GlamourStyle.Document.Margin, "light theme document.margin should be 0")
	assert.Equal(t, "", th.GlamourStyle.Document.BlockPrefix, "light theme document.block_prefix should be empty")
	assert.Equal(t, "", th.GlamourStyle.Document.BlockSuffix, "light theme document.block_suffix should be empty")
}

func TestRenderer_SelectsNoTTY(t *testing.T) {
	// Non-TTY mode selection is now performed by theme.Auto(), which
	// defaults to the dark theme. Verify the dark theme still carries
	// the expected overrides, which is what the non-TTY code path will
	// hand to the production renderer.
	th := theme.Dark()
	require.NotNil(t, th.GlamourStyle.Document.Margin, "non-TTY (dark) theme document.margin must be a non-nil pointer")
	assert.Equal(t, uint(0), *th.GlamourStyle.Document.Margin, "non-TTY (dark) theme document.margin should be 0")
	assert.Equal(t, "", th.GlamourStyle.Document.BlockPrefix, "non-TTY (dark) theme document.block_prefix should be empty")
	assert.Equal(t, "", th.GlamourStyle.Document.BlockSuffix, "non-TTY (dark) theme document.block_suffix should be empty")
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
			role: ledger.RoleAssistant,
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
			role: ledger.RoleTool,
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
	m.expandAllDetails = false
	compactOutput := m.buildContent()
	assert.Contains(t, compactOutput, "search_files")
	assert.Contains(t, compactOutput, "result data")

	// Expanded mode: shows full source content.
	m.expandAllDetails = true
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
			role: ledger.RoleAssistant,
			blocks: []renderedBlock{
				{title: "Tool", style: m.theme.AssistantStyle, expandedByDefault: false, kind: "tool_call", source: "Calling: foo({})", compact: "foo", toolCallID: "call_1"},
			},
		},
		{
			role: ledger.RoleTool,
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

	// Compact mode: error block should be wrapped in the error style.
	m.expandAllDetails = false
	output := m.buildContent()
	assert.Contains(t, output, "Error: failed")

	// Expanded mode: error block should be wrapped in the error style.
	m.expandAllDetails = true
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
			role: ledger.RoleAssistant,
			blocks: []renderedBlock{
				{title: "Tool", style: m.theme.AssistantStyle, expandedByDefault: false, kind: "tool_call", source: "Calling: foo({})", compact: "foo", toolCallID: "call_1"},
				{title: "Tool", style: m.theme.AssistantStyle, expandedByDefault: false, kind: "tool_call", source: "Calling: bar({})", compact: "bar", toolCallID: "call_2"},
			},
		},
		{
			role: ledger.RoleTool,
			blocks: []renderedBlock{
				{title: "Tool Result", style: m.theme.ToolResultStyle, expandedByDefault: false, kind: "tool_result", source: "result1", compact: "result1", toolCallID: "call_1"},
				{title: "Tool Result", style: m.theme.ToolResultStyle, expandedByDefault: false, kind: "tool_result", source: "result2", compact: "result2", toolCallID: "call_2"},
			},
		},
	}

	// Compact mode: blocks use borderless header styles.
	m.expandAllDetails = false
	output := m.buildContent()
	assert.Contains(t, output, "foo")
	assert.Contains(t, output, "bar")
	assert.Contains(t, output, "result1")
	assert.Contains(t, output, "result2")

	// Expanded mode: shows full source content.
	m.expandAllDetails = true
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
	m.expandAllDetails = true

	// A single assistant turn can contain text, tool_call, and reasoning
	// blocks interleaved. tool_result blocks belong in separate RoleTool turns.
	m.turns = []renderedTurn{
		{
			role: ledger.RoleAssistant,
			blocks: []renderedBlock{
				{title: "Tool", style: lipgloss.NewStyle(), expandedByDefault: true, kind: "text", source: "intro", rendered: "intro"},
				{title: "Tool", style: m.theme.AssistantStyle, expandedByDefault: false, kind: "tool_call", source: "Calling: foo({})", compact: "foo", toolCallID: "call_1"},
				{title: "Thinking", style: m.theme.ThinkingStyle, expandedByDefault: false, kind: "reasoning", source: "think", rendered: "think"},
				{title: "Tool", style: lipgloss.NewStyle(), expandedByDefault: true, kind: "text", source: "outro", rendered: "outro"},
			},
		},
		{
			role: ledger.RoleTool,
			blocks: []renderedBlock{
				{title: "Tool Result", style: m.theme.ToolResultStyle, expandedByDefault: false, kind: "tool_result", source: "result", compact: "result", toolCallID: "call_1"},
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
		{role: ledger.RoleUser, blocks: []renderedBlock{{title: "You", style: lipgloss.NewStyle(), expandedByDefault: true, kind: "text", source: "hello world"}}},
	}

	// Should not panic with zero-width viewport.
	_ = m.View().Content
}

func TestBuildContent_ToggleNoToolBlocks(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))

	m.turns = []renderedTurn{
		{role: ledger.RoleAssistant, blocks: []renderedBlock{{title: "Assistant", style: m.theme.AssistantStyle, expandedByDefault: true, kind: "text", source: "hello", rendered: "hello"}}},
	}

	// Toggle on — no tool blocks, view should be unchanged
	m.expandAllDetails = true
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
			role: ledger.RoleAssistant,
			blocks: []renderedBlock{
				{title: "Tool", style: m.theme.AssistantStyle, expandedByDefault: false, kind: "tool_call", source: "Calling: foo({})", compact: "foo", toolCallID: "call_1"},
			},
		},
	}

	m.expandAllDetails = false
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
	mm.expandAllDetails = true
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
	mm.expandAllDetails = true
	mm.contentDirty = true
	mm.syncViewport()
	output2 := mm.View().Content
	assert.Contains(t, output2, "Thinking")
	assert.NotContains(t, output2, "· |s|")
	assert.Contains(t, output2, "rendered-reasoning")
	assert.NotContains(t, output2, "Thinking...")

	// Toggle collapsed again.
	mm.expandAllDetails = false
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
		{title: "Thinking", style: m.theme.ThinkingStyle, expandedByDefault: false, kind: "reasoning", source: "abc"},
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
		{title: "Thinking", style: m.theme.ThinkingStyle, expandedByDefault: false, kind: "reasoning", source: "日本語"},
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
		{title: "Thinking", style: m.theme.ThinkingStyle, expandedByDefault: false, kind: "reasoning", source: "let me think..."},
		{title: "Assistant", style: m.theme.AssistantStyle, expandedByDefault: true, kind: "text", source: "the answer"},
	}
	output := m.buildContent()
	assert.Contains(t, output, "Thinking")
	assert.Contains(t, output, "15 B")
}

func TestBuildContent_Reasoning_Expanded_NoCounter(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.expandAllDetails = true
	m.currentTurn.blocks = []renderedBlock{
		{title: "Thinking", style: m.theme.ThinkingStyle, expandedByDefault: false, kind: "reasoning", source: "let me think...", rendered: "rendered-reasoning"},
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
		{role: ledger.RoleAssistant, blocks: []renderedBlock{
			{title: "Thinking", style: m.theme.ThinkingStyle, expandedByDefault: false, kind: "reasoning", source: "historical reasoning"},
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
		Display:   mockMarkdownValue{output: "```bash\n$ go test ./...\n```"},
	}
	block := m.renderArtifact(tc, ledger.RoleAssistant)
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
	block := m.renderArtifact(tc, ledger.RoleAssistant)
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
	block := m.renderArtifact(tr, ledger.RoleTool)
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
	block := m.renderArtifact(tr, ledger.RoleTool)
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
	block := m.renderArtifact(tr, ledger.RoleTool)
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
	block := m.renderArtifact(tr, ledger.RoleTool)
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
	block := m.renderArtifact(tr, ledger.RoleTool)
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
	rendered, lines := buildStatusLine(theme.Dark(), status, 200)
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
	rendered, lines := buildStatusLine(theme.Dark(), status, 200)
	assert.Equal(t, 1, lines)
	assert.Contains(t, rendered, "Σ 42")
	assert.NotContains(t, rendered, "↑")
	assert.NotContains(t, rendered, "↓")
}

func TestBuildStatusLine_NoTokenKeys(t *testing.T) {
	status := map[string]string{
		"phase": "streaming",
	}
	rendered, lines := buildStatusLine(theme.Dark(), status, 200)
	assert.Equal(t, 1, lines)
	assert.Contains(t, rendered, "phase: streaming")
	assert.NotContains(t, rendered, "tokens")
}

// TestBuildStatusLine_ThinkingKeyGrouped asserts that when all four
// token-family keys are present, the renderer produces the compact
// "↑ X · ↓ Y · Σ Z · Ψ T" segment in the documented order, and that
// non-token keys continue to appear separately.
func TestBuildStatusLine_ThinkingKeyGrouped(t *testing.T) {
	status := map[string]string{
		"sent":      "100",
		"received":  "50",
		"total":     "150",
		"thinking":  "7",
		"phase":     "done",
		"thread_id": "abc-123",
	}
	rendered, lines := buildStatusLine(theme.Dark(), status, 200)
	assert.Equal(t, 1, lines)
	assert.Contains(t, rendered, "↑ 100")
	assert.Contains(t, rendered, "↓ 50")
	assert.Contains(t, rendered, "Σ 150")
	assert.Contains(t, rendered, "Ψ 7")
	// Verify the four symbols appear in the documented order, joined by " · ".
	symIdx := []int{
		strings.Index(rendered, "↑"),
		strings.Index(rendered, "↓"),
		strings.Index(rendered, "Σ"),
		strings.Index(rendered, "Ψ"),
	}
	for i := 1; i < len(symIdx); i++ {
		assert.Greater(t, symIdx[i], symIdx[i-1],
			"symbol %d must appear after symbol %d in the compact segment", i, i-1)
	}
	// Non-token keys must still appear.
	assert.Contains(t, rendered, "phase: done")
	assert.Contains(t, rendered, "thread_id: abc-123")
	// The thinking key must not be re-emitted as "thinking: 7" by the
	// "remaining keys alphabetically" loop.
	assert.NotContains(t, rendered, "thinking: 7")
}

// TestBuildStatusLine_ThinkingKeyOnly asserts that a status map containing
// only the thinking key renders "Ψ 0" and does not invent zero values for
// the other three counters (which would only be present if the handler had
// emitted them).
func TestBuildStatusLine_ThinkingKeyOnly(t *testing.T) {
	status := map[string]string{
		"thinking": "0",
	}
	rendered, lines := buildStatusLine(theme.Dark(), status, 200)
	assert.Equal(t, 1, lines)
	assert.Contains(t, rendered, "Ψ 0")
	assert.NotContains(t, rendered, "↑")
	assert.NotContains(t, rendered, "↓")
	assert.NotContains(t, rendered, "Σ")
	// And the key must not be re-emitted by the "remaining keys" loop.
	assert.NotContains(t, rendered, "thinking: 0")
}

// TestBuildStatusLine_ThinkingKeyNotDoubleRendered is a regression guard:
// buildStatusLine groups token-family keys into the compact segment AND
// skips them in the alphabetical "remaining keys" loop. If the skip
// filter forgets the new key, "Ψ 42" would appear in the compact segment
// AND "thinking: 42" would appear after it. This test asserts the
// double-render cannot happen.
func TestBuildStatusLine_ThinkingKeyNotDoubleRendered(t *testing.T) {
	status := map[string]string{
		"thinking": "42",
	}
	rendered, lines := buildStatusLine(theme.Dark(), status, 200)
	assert.Equal(t, 1, lines)
	assert.Equal(t, 1, strings.Count(rendered, "Ψ"),
		"Ψ must appear exactly once in the rendered status line")
	assert.Equal(t, 1, strings.Count(rendered, "42"),
		"42 must appear exactly once in the rendered status line")
	assert.NotContains(t, rendered, "thinking:")
}

// TestCompactTokenSegments_NarrativeOrder asserts that compactTokenSegments
// emits the four-token segment in the documented order
// "↑ sent · ↓ received · Σ total · Ψ thinking" regardless of the order
// of the input segments. Prior to the fix, sort.Strings produced
// Unicode-byte order (Σ Ψ ↑ ↓), which grouped the Greek letters together
// and the arrows together, splitting the cluster visually.
func TestCompactTokenSegments_NarrativeOrder(t *testing.T) {
	segs := []conduit.StatusSegment{
		{Label: "total", Value: "150", Zone: "lifecycle"},
		{Label: "thinking", Value: "7", Zone: "lifecycle"},
		{Label: "received", Value: "50", Zone: "lifecycle"},
		{Label: "sent", Value: "100", Zone: "lifecycle"},
	}
	got := compactTokenSegments(segs)
	require.Len(t, got, 1, "expected exactly one tokens segment")
	assert.Equal(t, "tokens", got[0].Label)
	assert.Equal(t, "lifecycle", got[0].Zone)
	assert.Equal(t, "↑ 100 · ↓ 50 · Σ 150 · Ψ 7", got[0].Value,
		"tokens must render in narrative order regardless of input order")
}

// TestCompactTokenSegments_PartialKeys asserts that missing keys in the
// input are simply skipped (not rendered as empty placeholders) and that
// the surviving keys still appear in narrative order.
func TestCompactTokenSegments_PartialKeys(t *testing.T) {
	segs := []conduit.StatusSegment{
		{Label: "thinking", Value: "7", Zone: "lifecycle"},
		{Label: "sent", Value: "100", Zone: "lifecycle"},
	}
	got := compactTokenSegments(segs)
	require.Len(t, got, 1)
	assert.Equal(t, "↑ 100 · Ψ 7", got[0].Value,
		"partial key set must not introduce placeholder gaps")
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
	str, lines := buildStatusLineFromSegments(theme.Dark(), segments, priorities, 80)
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
	str, _ := buildStatusLineFromSegments(theme.Dark(), segments, priorities, 40)
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
	str, _ := buildStatusLineFromSegments(theme.Dark(), segments, priorities, 80)
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
	str, lines := buildStatusLine(theme.Dark(), status, width)
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
	rendered, lines := buildStatusLine(theme.Dark(), status, -1)
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

// TestBuildContent_InterMessageSpacing_OneBlankLine is a regression test
// for the inter-message spacing bug. Glamour's default document style
// emits a trailing "\n" on every rendered markdown body, and a previous
// buildContent separator added "\n\n" after every turn. With both
// sources of newlines active, two consecutive assistant turns rendered
// with two blank lines between them rather than one. The fix layered:
// (a) the theme sets Document.BlockSuffix = "" so glamour no longer
// adds a trailing newline, (b) renderBlockUnified now appends a
// single trailing "\n" itself so the block owns its line terminator,
// and (c) Theme.Gap(n) returns exactly n newlines so the boundary is
// "1 (block-end) + 1 (gap)" = 2 newlines = 1 blank line.
//
// This test uses a real glamourMarkdownRenderer (not a mock) so it
// exercises the full glamour pipeline and would catch a regression
// where glamour's behaviour changes.
func TestBuildContent_InterMessageSpacing_OneBlankLine(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.md = newGlamourMarkdownRenderer(theme.Dark())

	// Two simple assistant text turns. Plain text keeps glamour's
	// rendered output close to the source so the newline accounting
	// is easy to reason about.
	m.turns = []renderedTurn{
		{
			role:      ledger.RoleAssistant,
			timestamp: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
			blocks: []renderedBlock{
				{title: "Assistant", style: m.theme.AssistantStyle, expandedByDefault: true, kind: "text", source: "hello"},
			},
		},
		{
			role:      ledger.RoleAssistant,
			timestamp: time.Date(2024, 1, 1, 12, 0, 5, 0, time.UTC),
			blocks: []renderedBlock{
				{title: "Assistant", style: m.theme.AssistantStyle, expandedByDefault: true, kind: "text", source: "world"},
			},
		},
	}

	output := m.buildContent()

	// The exact pattern between the last body line of the first turn
	// and the header of the second turn must contain exactly two
	// newlines (one blank line), not three (two blank lines, the
	// original bug). Glamour may wrap the second header in ANSI
	// escape codes, so we count newlines in the segment rather than
	// asserting on the exact string.
	//
	// Under the new model: the first turn's body is glamour's
	// rendering of "hello" (no trailing newline, because the theme
	// sets Document.BlockSuffix = ""). renderBlockUnified appends
	// exactly one "\n" so the block ends in "hello\n". Then
	// buildContent appends Gap(InterTurnGap) = Gap(1) = "\n". So
	// the segment between the end of "hello" and the start of the
	// next header contains exactly two newlines: 1 (block-end) +
	// 1 (gap).
	idx1 := strings.Index(output, "hello")
	idx2 := strings.Index(output, "12:00:05")
	require.GreaterOrEqual(t, idx1, 0, "first body should be in output")
	require.Greater(t, idx2, idx1, "second header timestamp should appear after first body")

	segment := output[idx1+len("hello") : idx2]
	newlineCount := strings.Count(segment, "\n")
	assert.Equal(t, 2, newlineCount,
		"inter-message spacing should be exactly one blank line (2 newlines), got %d newlines in segment %q (full output:\n%s)",
		newlineCount, segment, output)
}

// TestBuildContent_UniformSpacing_AllBoundaries locks in the
// "every block ends in '\\n' + Gap(n) = n newlines" invariant by
// walking three boundary types in a single buildContent output and
// asserting each contains exactly two newlines (one blank line):
// text→text within a turn, text→reasoning (compact) within a turn,
// and text→text across turns.
//
// Each segment between an end token and a start token must contain
// exactly two newlines; any value other than 2 signals a regression
// in either renderBlockUnified's trailing-newline contract or in
// Theme.Gap's blank-line count.
func TestBuildContent_UniformSpacing_AllBoundaries(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.expandAllDetails = false // keep reasoning compact so the "header-only" path is exercised

	// Use unique marker tokens so the substring lookups don't collide
	// across the three boundaries.
	m.turns = []renderedTurn{
		{
			role:      ledger.RoleAssistant,
			timestamp: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
			blocks: []renderedBlock{
				// (1) text → text within turn: end token "AAA", start token "BBB"
				{title: "Assistant", style: m.theme.AssistantStyle, expandedByDefault: true, kind: "text", source: "AAA"},
				{title: "Assistant", style: m.theme.AssistantStyle, expandedByDefault: true, kind: "text", source: "BBB"},
			},
		},
		{
			role:      ledger.RoleAssistant,
			timestamp: time.Date(2024, 1, 1, 12, 0, 5, 0, time.UTC),
			blocks: []renderedBlock{
				// (2) text → reasoning (compact) within turn: end token "CCC", start token "Thinking"
				{title: "Assistant", style: m.theme.AssistantStyle, expandedByDefault: true, kind: "text", source: "CCC"},
				{title: "Thinking", style: m.theme.ThinkingStyle, expandedByDefault: false, kind: "reasoning", source: "DDD"},
				// (no third block — keeps boundary (2) unambiguous)
			},
		},
		{
			role:      ledger.RoleAssistant,
			timestamp: time.Date(2024, 1, 1, 12, 0, 10, 0, time.UTC),
			blocks: []renderedBlock{
				// (3) text → text across turn boundary: end token "EEE", start token "FFF"
				{title: "Assistant", style: m.theme.AssistantStyle, expandedByDefault: true, kind: "text", source: "EEE"},
			},
		},
		{
			role:      ledger.RoleAssistant,
			timestamp: time.Date(2024, 1, 1, 12, 0, 15, 0, time.UTC),
			blocks: []renderedBlock{
				{title: "Assistant", style: m.theme.AssistantStyle, expandedByDefault: true, kind: "text", source: "FFF"},
			},
		},
	}

	output := m.buildContent()

	// Each boundary: take the substring between the end of the
	// previous marker and the start of the next block's styled header
	// and assert exactly two newlines (one blank line). The next
	// block's styled header is identified by its timestamp; the
	// timestamp is unique within the test data, so the lookup is
	// unambiguous.
	//
	// Note that the styled header is preceded by an ANSI escape
	// sequence (e.g. \x1b[38;2;...m), which contains no newlines
	// and is therefore invisible to the count. The segment ends
	// just before the timestamp of the next block's header.
	type boundary struct {
		name     string
		endToken string
		startTok string
	}
	boundaries := []boundary{
		// Turn 1: AAA (block 1) → BBB (block 2). The next block's
		// header is the second occurrence of "12:00:00" in the
		// output; the first occurrence precedes "AAA" and is
		// therefore not in the search range.
		{"text→text within turn", "AAA", "12:00:00"},
		// Turn 2: CCC (block 1) → reasoning (block 2). The next
		// block's header is the second occurrence of "12:00:05".
		{"text→reasoning (compact) within turn", "CCC", "12:00:05"},
		// Turn 3 → turn 4: EEE → next turn's block. The next
		// block's header is "12:00:15", which only appears once.
		{"text→text across turn boundary", "EEE", "12:00:15"},
	}

	for _, b := range boundaries {
		b := b
		t.Run(b.name, func(t *testing.T) {
			endIdx := strings.Index(output, b.endToken)
			require.GreaterOrEqual(t, endIdx, 0, "end token %q should be in output", b.endToken)
			startIdx := strings.Index(output[endIdx+len(b.endToken):], b.startTok)
			require.GreaterOrEqual(t, startIdx, 0, "start token %q should be in output after %q", b.startTok, b.endToken)
			absStart := endIdx + len(b.endToken) + startIdx
			segment := output[endIdx+len(b.endToken) : absStart]
			newlineCount := strings.Count(segment, "\n")
			assert.Equal(t, 2, newlineCount,
				"boundary %q should be exactly one blank line (2 newlines), got %d newlines in segment %q (full output:\n%s)",
				b.name, newlineCount, segment, output)
		})
	}
}

// TestRenderBlockUnified_AlwaysEndsWithNewline locks in the block
// terminator invariant: every configuration of renderBlockUnified
// returns a string ending in exactly one "\n", regardless of body
// content, expand/compact state, or block kind. The trailing newline
// is the contract that lets Theme.Gap(n) produce n blank lines at
// every boundary without leaking or doubling.
func TestRenderBlockUnified_AlwaysEndsWithNewline(t *testing.T) {
	ts := time.Date(2024, 1, 1, 12, 30, 45, 0, time.UTC)

	cases := []struct {
		name   string
		block  renderedBlock
		expand bool
	}{
		{
			name:   "text with body, expanded",
			block:  renderedBlock{kind: "text", source: "hello world", title: "Assistant", style: lipgloss.NewStyle(), expandedByDefault: true},
			expand: true,
		},
		{
			name:   "text with empty body, expanded",
			block:  renderedBlock{kind: "text", source: "", title: "Assistant", style: lipgloss.NewStyle(), expandedByDefault: true},
			expand: true,
		},
		{
			name:   "reasoning, compact",
			block:  renderedBlock{kind: "reasoning", source: "deep thought", title: "Thinking", style: lipgloss.NewStyle(), compact: "Thinking 12 B", expandedByDefault: false},
			expand: false,
		},
		{
			name:   "reasoning, expanded",
			block:  renderedBlock{kind: "reasoning", source: "deep thought", title: "Thinking", style: lipgloss.NewStyle(), compact: "Thinking 12 B", expandedByDefault: false},
			expand: true,
		},
		{
			name:   "tool_call, compact",
			block:  renderedBlock{kind: "tool_call", source: "{}", compact: "bash · command=\"test\"", title: "Assistant · Call bash", style: lipgloss.NewStyle(), expandedByDefault: false},
			expand: false,
		},
		{
			name:   "tool_result, compact",
			block:  renderedBlock{kind: "tool_result", source: "out", compact: "result line 1", title: "Tool Result", style: lipgloss.NewStyle(), expandedByDefault: false},
			expand: false,
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			output := renderBlockUnified(c.block, ts, c.expand, 80)
			require.NotEmpty(t, output, "renderBlockUnified must always return non-empty output")
			assert.True(t, strings.HasSuffix(output, "\n"),
				"output must end with \"\\n\"; got %q", output)
			// The header-to-body join is a single "\n" — count consecutive
			// newlines elsewhere in the output. A multi-line body might
			// contain internal "\n\n" (e.g. a paragraph break in glamour
			// output), so we only assert the trailing-newline invariant
			// here; the header/body tightness is covered by
			// TestRenderBlockUnified_NoBlankLineBetweenHeaderAndBody for
			// the simple body case.
		})
	}
}
