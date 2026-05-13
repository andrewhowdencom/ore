package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/state"
	"github.com/andrewhowdencom/ore/conduit"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestModel returns a model with a properly initialized textarea widget.
// Tests that send key messages or call View() must use this helper to avoid
// panics from the zero-value textarea.
func newTestModel() model {
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.Prompt = "> "
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("alt+enter"))
	ta.Focus()
	return model{
		textarea: ta,
	}
}

func TestModel_Update_Delta_TextDelta(t *testing.T) {
	m := model{}
	newM, _ := m.Update(deltaMsg{delta: artifact.TextDelta{Content: "hello"}})
	mm := newM.(*model)
	require.Len(t, mm.streamBlocks, 1)
	assert.Equal(t, "text", mm.streamBlocks[0].kind)
	assert.Equal(t, "hello", mm.streamBlocks[0].content)
}

func TestModel_Update_Delta_ReasoningDelta(t *testing.T) {
	m := model{}
	newM, _ := m.Update(deltaMsg{delta: artifact.ReasoningDelta{Content: "thinking"}})
	mm := newM.(*model)
	require.Len(t, mm.streamBlocks, 1)
	assert.Equal(t, "reasoning", mm.streamBlocks[0].kind)
	assert.Equal(t, "thinking", mm.streamBlocks[0].content)
}

func TestModel_Update_Delta_Interleaved(t *testing.T) {
	m := model{}
	newM, _ := m.Update(deltaMsg{delta: artifact.TextDelta{Content: "first"}})
	newM, _ = newM.Update(deltaMsg{delta: artifact.ReasoningDelta{Content: "think"}})
	newM, _ = newM.Update(deltaMsg{delta: artifact.TextDelta{Content: "second"}})
	mm := newM.(*model)
	require.Len(t, mm.streamBlocks, 3)
	assert.Equal(t, "text", mm.streamBlocks[0].kind)
	assert.Equal(t, "first", mm.streamBlocks[0].content)
	assert.Equal(t, "reasoning", mm.streamBlocks[1].kind)
	assert.Equal(t, "think", mm.streamBlocks[1].content)
	assert.Equal(t, "text", mm.streamBlocks[2].kind)
	assert.Equal(t, "second", mm.streamBlocks[2].content)
}

func TestModel_Update_Delta_AdjacentTextMerges(t *testing.T) {
	m := model{}
	newM, _ := m.Update(deltaMsg{delta: artifact.TextDelta{Content: "Hello"}})
	newM, _ = newM.Update(deltaMsg{delta: artifact.TextDelta{Content: " world"}})
	mm := newM.(*model)
	require.Len(t, mm.streamBlocks, 1)
	assert.Equal(t, "text", mm.streamBlocks[0].kind)
	assert.Equal(t, "Hello world", mm.streamBlocks[0].content)
}

func TestModel_Update_Delta_AdjacentReasoningMerges(t *testing.T) {
	m := model{}
	newM, _ := m.Update(deltaMsg{delta: artifact.ReasoningDelta{Content: "think"}})
	newM, _ = newM.Update(deltaMsg{delta: artifact.ReasoningDelta{Content: "...done"}})
	mm := newM.(*model)
	require.Len(t, mm.streamBlocks, 1)
	assert.Equal(t, "reasoning", mm.streamBlocks[0].kind)
	assert.Equal(t, "think...done", mm.streamBlocks[0].content)
}

func TestModel_Update_Turn(t *testing.T) {
	m := model{}
	turn := state.Turn{
		Role: state.RoleAssistant,
		Artifacts: []artifact.Artifact{
			artifact.Text{Content: "hello world"},
		},
	}
	newM, _ := m.Update(turnMsg{turn: turn})
	mm := newM.(*model)
	require.Len(t, mm.turns, 1)
	assert.Equal(t, state.RoleAssistant, mm.turns[0].role)
	require.Len(t, mm.turns[0].blocks, 1)
	assert.Equal(t, "text", mm.turns[0].blocks[0].kind)
	assert.Equal(t, "hello world", mm.turns[0].blocks[0].source)
	assert.Empty(t, mm.streamBlocks)
}

func TestModel_Update_Turn_PreservesReasoning(t *testing.T) {
	m := model{
		viewport: viewport.New(80, 20),
	}
	turn := state.Turn{
		Role: state.RoleAssistant,
		Artifacts: []artifact.Artifact{
			artifact.Text{Content: "the answer is 42"},
			artifact.Reasoning{Content: "let me think..."},
		},
	}
	newM, _ := m.Update(turnMsg{turn: turn})
	mm := newM.(*model)
	require.Len(t, mm.turns, 1)
	require.Len(t, mm.turns[0].blocks, 2)
	assert.Equal(t, "text", mm.turns[0].blocks[0].kind)
	assert.Equal(t, "the answer is 42", mm.turns[0].blocks[0].source)
	assert.Equal(t, "reasoning", mm.turns[0].blocks[1].kind)
	assert.Equal(t, "let me think...", mm.turns[0].blocks[1].source)
}

func TestModel_Update_Turn_ResetsStreamBuffer(t *testing.T) {
	m := model{}
	m.streamBlocks = []streamBlock{{kind: "text", content: "partial"}}

	turn := state.Turn{
		Role: state.RoleAssistant,
		Artifacts: []artifact.Artifact{
			artifact.Text{Content: "complete"},
		},
	}
	newM, _ := m.Update(turnMsg{turn: turn})
	mm := newM.(*model)
	assert.Empty(t, mm.streamBlocks)
}

func TestModel_Update_Turn_Interleaved(t *testing.T) {
	m := model{}
	turn := state.Turn{
		Role: state.RoleAssistant,
		Artifacts: []artifact.Artifact{
			artifact.Text{Content: "Hello"},
			artifact.Reasoning{Content: "think"},
			artifact.Text{Content: " world"},
		},
	}
	newM, _ := m.Update(turnMsg{turn: turn})
	mm := newM.(*model)
	require.Len(t, mm.turns, 1)
	require.Len(t, mm.turns[0].blocks, 3)
	assert.Equal(t, "text", mm.turns[0].blocks[0].kind)
	assert.Equal(t, "Hello", mm.turns[0].blocks[0].source)
	assert.Equal(t, "reasoning", mm.turns[0].blocks[1].kind)
	assert.Equal(t, "think", mm.turns[0].blocks[1].source)
	assert.Equal(t, "text", mm.turns[0].blocks[2].kind)
	assert.Equal(t, " world", mm.turns[0].blocks[2].source)
}

func TestModel_Update_Status(t *testing.T) {
	m := model{}
	newM, _ := m.Update(statusMsg{status: "thinking..."})
	mm := newM.(*model)
	assert.Equal(t, "thinking...", mm.status)
}

func TestModel_Update_KeyRunes(t *testing.T) {
	m := newTestModel()
	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello")})
	mm := newM.(*model)
	assert.Equal(t, "hello", mm.textarea.Value())
}

func TestModel_Update_KeySpace(t *testing.T) {
	m := newTestModel()
	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	mm := newM.(*model)
	assert.Equal(t, " ", mm.textarea.Value())
}

func TestModel_Update_KeyBackspace(t *testing.T) {
	m := newTestModel()
	m.textarea.SetValue("hi")
	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	mm := newM.(*model)
	assert.Equal(t, "h", mm.textarea.Value())
}

func TestModel_Update_KeyBackspace_Empty(t *testing.T) {
	m := newTestModel()
	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	mm := newM.(*model)
	assert.Empty(t, mm.textarea.Value())
}

func TestModel_Update_KeyEnter_WithInput(t *testing.T) {
	eventsCh := make(chan conduit.Event, 10)
	m := newTestModel()
	m.eventsCh = eventsCh
	m.textarea.SetValue("hello")

	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm := newM.(*model)

	// User turns no longer render directly on KeyEnter; they arrive via
	// turnMsg from the loop's FanOut.
	assert.Empty(t, mm.turns)
	assert.Empty(t, mm.textarea.Value())

	select {
	case e := <-eventsCh:
		require.Equal(t, "user_message", e.Kind())
		ume, ok := e.(conduit.UserMessageEvent)
		require.True(t, ok)
		assert.Equal(t, "hello", ume.Content)
	default:
		t.Fatal("expected event on channel")
	}
}

func TestModel_Update_KeyEnter_EmptyInput(t *testing.T) {
	eventsCh := make(chan conduit.Event, 10)
	m := newTestModel()
	m.eventsCh = eventsCh

	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm := newM.(*model)

	assert.Empty(t, mm.turns)
	assert.Empty(t, mm.textarea.Value())

	select {
	case <-eventsCh:
		t.Fatal("expected no event on empty input")
	default:
	}
}

func TestModel_Update_WindowSize(t *testing.T) {
	m := newTestModel()
	newM, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	mm := newM.(*model)
	assert.Equal(t, 80, mm.width)
	assert.Equal(t, 24, mm.height)
}

func TestModel_Update_WindowSize_ResizesViewport(t *testing.T) {
	m := newTestModel()
	newM, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	mm := newM.(*model)
	assert.Equal(t, 80, mm.viewport.Width)
	assert.Equal(t, 22, mm.viewport.Height)
}

func TestModel_Update_KeyCtrlC(t *testing.T) {
	eventsCh := make(chan conduit.Event, 10)
	m := newTestModel()
	m.eventsCh = eventsCh

	newM, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	mm := newM.(*model)

	select {
	case e := <-eventsCh:
		require.Equal(t, "interrupt", e.Kind())
	default:
		t.Fatal("expected interrupt event on channel")
	}

	require.NotNil(t, cmd)
	_ = mm // suppress unused if we don't assert on mm
}

func TestModel_View_ContainsTurn(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(80, 20)
	m.turns = []renderedTurn{
		{role: state.RoleUser, blocks: []renderedBlock{{kind: "text", source: "hello"}}},
	}
	output := m.View()
	assert.Contains(t, output, "You: ")
	assert.Contains(t, output, "hello")
}

func TestModel_View_ContainsAssistantTurn(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(80, 20)
	m.turns = []renderedTurn{
		{role: state.RoleAssistant, blocks: []renderedBlock{{kind: "text", source: "world"}}},
	}
	output := m.View()
	assert.Contains(t, output, "Assistant: ")
	assert.Contains(t, output, "world")
}

func TestModel_View_ContainsToolTurn(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(80, 20)
	m.turns = []renderedTurn{
		{role: state.RoleTool, blocks: []renderedBlock{{kind: "text", source: "result"}}},
	}
	output := m.View()
	assert.Contains(t, output, "Tool: ")
	assert.Contains(t, output, "result")
}

func TestModel_View_ContainsStreaming(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(80, 20)
	m.streamBlocks = []streamBlock{{kind: "text", content: "partial"}}
	output := m.View()
	assert.Contains(t, output, "Assistant: ")
	assert.Contains(t, output, "partial")
}

func TestModel_View_ContainsStatus(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(80, 20)
	m.status = "thinking..."
	output := m.View()
	assert.Contains(t, output, "thinking...")
}

func TestModel_View_ContainsPrompt(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(80, 20)
	m.textarea.SetValue("hi")
	output := m.View()
	assert.Contains(t, output, "> ")
	assert.Contains(t, output, "hi")
}

func TestModel_View_Empty(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(80, 20)
	output := m.View()
	assert.Contains(t, output, "> ")
}

func TestModel_View_ContainsInputAtBottom(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(80, 20)
	m.turns = []renderedTurn{
		{role: state.RoleUser, blocks: []renderedBlock{{kind: "text", source: "hello"}}},
	}
	output := m.View()
	lines := strings.Split(output, "\n")
	lastLine := lines[len(lines)-1]
	assert.Contains(t, lastLine, "> ")
}

func TestModel_Update_PgUp_ScrollsViewport(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(80, 5)
	m.viewport.SetContent(strings.Repeat("line\n", 20))
	m.viewport.GotoBottom()
	initialYOffset := m.viewport.YOffset

	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	mm := newM.(*model)

	assert.Less(t, mm.viewport.YOffset, initialYOffset, "PgUp should scroll viewport up")
}

func TestModel_Update_PgDown_ScrollsViewport(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(80, 5)
	m.viewport.SetContent(strings.Repeat("line\n", 20))
	m.viewport.GotoBottom()

	// Scroll up first so PgDown has room to scroll back down
	m.viewport.HalfPageUp()
	initialYOffset := m.viewport.YOffset

	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	mm := newM.(*model)

	assert.Greater(t, mm.viewport.YOffset, initialYOffset, "PgDown should scroll viewport down")
}

func TestModel_Update_Turn_AutoScrollsViewport(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(80, 5)
	m.viewport.SetContent(strings.Repeat("line\n", 20))
	m.viewport.GotoBottom()

	// Scroll up to simulate user reading history
	m.viewport.HalfPageUp()
	assert.False(t, m.viewport.AtBottom(), "should not be at bottom after scrolling up")

	turn := state.Turn{
		Role: state.RoleAssistant,
		Artifacts: []artifact.Artifact{
			artifact.Text{Content: "hello world"},
		},
	}
	newM, _ := m.Update(turnMsg{turn: turn})
	mm := newM.(*model)

	assert.True(t, mm.viewport.AtBottom(), "turn should auto-scroll viewport to bottom")
}

func TestModel_Update_Delta_AutoScrollsViewport(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(80, 5)
	m.viewport.SetContent(strings.Repeat("line\n", 20))
	m.viewport.GotoBottom()

	// Scroll up to simulate user reading history
	m.viewport.HalfPageUp()
	assert.False(t, m.viewport.AtBottom(), "should not be at bottom after scrolling up")

	newM, _ := m.Update(deltaMsg{delta: artifact.TextDelta{Content: "new token"}})
	mm := newM.(*model)

	assert.True(t, mm.viewport.AtBottom(), "delta should auto-scroll viewport to bottom")
	require.Len(t, mm.streamBlocks, 1)
	assert.Equal(t, "text", mm.streamBlocks[0].kind)
	assert.Equal(t, "new token", mm.streamBlocks[0].content)
}

func TestModel_View_LongHistory_InputAtBottom(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(80, 5)
	// Add enough turns to exceed viewport height
	for i := 0; i < 10; i++ {
		m.turns = append(m.turns, renderedTurn{
			role: state.RoleUser,
			blocks: []renderedBlock{{kind: "text", source: strings.Repeat("word ", 20)}},
		})
	}
	output := m.View()
	lines := strings.Split(output, "\n")
	lastLine := lines[len(lines)-1]
	assert.Contains(t, lastLine, "> ")
}

func TestWrapText_NoWrap(t *testing.T) {
	output := wrapText("hello", "You: ", "     ", 80)
	assert.Equal(t, "You: hello", output)
}

func TestWrapText_WrapsLongLine(t *testing.T) {
	text := strings.Repeat("a", 100)
	output := wrapText(text, "You: ", "     ", 20)
	lines := strings.Split(output, "\n")
	assert.Greater(t, len(lines), 1, "long text should wrap to multiple lines")
	assert.True(t, strings.HasPrefix(lines[0], "You: "), "first line should have label")
	for i := 1; i < len(lines); i++ {
		assert.True(t, strings.HasPrefix(lines[i], "     "), "continuation lines should have indent")
	}
}

func TestWrapText_WidthZero(t *testing.T) {
	output := wrapText("hello", "You: ", "     ", 0)
	assert.Equal(t, "You: hello", output)
}

func TestWrapText_EmptyText(t *testing.T) {
	output := wrapText("", "You: ", "     ", 80)
	assert.Equal(t, "You: ", output)
}

func TestWrapText_Unicode(t *testing.T) {
	// Japanese characters are typically 2 cells wide.
	output := wrapText("こんにちは世界", "You: ", "     ", 12)
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		assert.LessOrEqual(t, lipgloss.Width(line), 12, "line %q exceeds width", line)
	}
}

func TestWrapText_AnsiAware(t *testing.T) {
	styledLabel := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Render("Label: ")
	indent := strings.Repeat(" ", lipgloss.Width(styledLabel))
	text := strings.Repeat("x", 100)
	output := wrapText(text, styledLabel, indent, 30)
	lines := strings.Split(output, "\n")
	assert.Greater(t, len(lines), 1, "long text should wrap to multiple lines")
	for _, line := range lines {
		assert.LessOrEqual(t, lipgloss.Width(line), 30, "line %q exceeds width", line)
	}
}

func TestModel_View_WrapsLongTurn(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(20, 5)
	m.turns = []renderedTurn{
		{role: state.RoleUser, blocks: []renderedBlock{{kind: "text", source: strings.Repeat("word ", 10)}}},
	}
	output := m.View()
	lines := strings.Split(output, "\n")
	hasContinuation := false
	for _, line := range lines {
		if strings.HasPrefix(line, "     ") {
			hasContinuation = true
			break
		}
	}
	assert.True(t, hasContinuation, "long turn should wrap with continuation lines")
}

// unknownArtifact is an artifact type not handled by the TUI model.
type unknownArtifact struct{}

func (unknownArtifact) Kind() string { return "unknown" }

func TestModel_Update_Delta_UnknownArtifact(t *testing.T) {
	m := model{}
	newM, cmd := m.Update(deltaMsg{delta: unknownArtifact{}})
	mm := newM.(*model)
	assert.Empty(t, mm.streamBlocks)
	assert.Nil(t, cmd)
}

func TestWrapText_AvailableLEOne(t *testing.T) {
	// "You: " has width 5, so width=6 gives available=1.
	output := wrapText("hello", "You: ", "     ", 6)
	assert.Equal(t, "You: hello", output)
}

func TestModel_Update_Turn_Assistant_PopulatesRendered(t *testing.T) {
	m := model{
		viewport: viewport.New(80, 20),
	}
	turn := state.Turn{
		Role: state.RoleAssistant,
		Artifacts: []artifact.Artifact{
			artifact.Text{Content: "# Hello\n\n**bold** text"},
		},
	}
	newM, _ := m.Update(turnMsg{turn: turn})
	mm := newM.(*model)
	require.Len(t, mm.turns, 1)
	require.Len(t, mm.turns[0].blocks, 1)
	assert.NotEmpty(t, mm.turns[0].blocks[0].source)
	assert.NotEmpty(t, mm.turns[0].blocks[0].rendered, "assistant turn should have rendered Markdown")
}

func TestModel_Update_Turn_User_LeavesRenderedEmpty(t *testing.T) {
	m := model{
		viewport: viewport.New(80, 20),
	}
	turn := state.Turn{
		Role: state.RoleUser,
		Artifacts: []artifact.Artifact{
			artifact.Text{Content: "hello world"},
		},
	}
	newM, _ := m.Update(turnMsg{turn: turn})
	mm := newM.(*model)
	require.Len(t, mm.turns, 1)
	require.Len(t, mm.turns[0].blocks, 1)
	assert.Empty(t, mm.turns[0].blocks[0].rendered, "user turn should not have rendered Markdown")
}

func TestModel_Update_WindowSize_RerendersAssistantTurns(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(80, 20)
	turn := state.Turn{
		Role: state.RoleAssistant,
		Artifacts: []artifact.Artifact{
			artifact.Text{Content: "# Title\n\nThis is a longer paragraph that should definitely wrap differently at width forty versus width eighty."},
		},
	}
	newM, _ := m.Update(turnMsg{turn: turn})
	mm := newM.(*model)
	require.Len(t, mm.turns, 1)
	initialRendered := mm.turns[0].blocks[0].rendered
	assert.NotEmpty(t, initialRendered)

	// Resize to a narrower width
	newM2, _ := mm.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
	mm2 := newM2.(*model)
	assert.NotEmpty(t, mm2.turns[0].blocks[0].rendered)
	assert.NotEqual(t, initialRendered, mm2.turns[0].blocks[0].rendered,
		"re-rendered output should differ after width change")
}

// mockMarkdownRenderer is a test double that returns fixed output or errors.
type mockMarkdownRenderer struct {
	output string
	err    error
}

func (m mockMarkdownRenderer) Render(text string, width int) (string, error) {
	return m.output, m.err
}

func TestModel_Update_Turn_Assistant_RenderError_Fallback(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(80, 20)
	m.md = mockMarkdownRenderer{err: errors.New("render failed")}
	turn := state.Turn{
		Role: state.RoleAssistant,
		Artifacts: []artifact.Artifact{
			artifact.Text{Content: "# Hello"},
		},
	}
	newM, _ := m.Update(turnMsg{turn: turn})
	mm := newM.(*model)
	require.Len(t, mm.turns, 1)
	require.Len(t, mm.turns[0].blocks, 1)
	assert.Empty(t, mm.turns[0].blocks[0].rendered, "render error should leave rendered empty")
	assert.Equal(t, "# Hello", mm.turns[0].blocks[0].source, "raw text should still be stored")
}

func TestModel_View_AssistantTurn_RenderError_FallbackToPlainText(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(80, 20)
	m.md = mockMarkdownRenderer{err: errors.New("render failed")}
	turn := state.Turn{
		Role: state.RoleAssistant,
		Artifacts: []artifact.Artifact{
			artifact.Text{Content: "plain fallback text"},
		},
	}
	newM, _ := m.Update(turnMsg{turn: turn})
	mm := newM.(*model)
	output := mm.View()
	assert.Contains(t, output, "Assistant: ")
	assert.Contains(t, output, "plain fallback text")
}

func TestModel_Update_WindowSize_RerenderError_KeepsOldCache(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(80, 20)
	m.md = mockMarkdownRenderer{output: "initial-render"}
	turn := state.Turn{
		Role: state.RoleAssistant,
		Artifacts: []artifact.Artifact{
			artifact.Text{Content: "text"},
		},
	}
	newM, _ := m.Update(turnMsg{turn: turn})
	mm := newM.(*model)
	assert.Equal(t, "initial-render", mm.turns[0].blocks[0].rendered)

	// Swap to an error-returning renderer and resize.
	mm.md = mockMarkdownRenderer{err: errors.New("resize render failed")}
	newM2, _ := mm.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
	mm2 := newM2.(*model)
	assert.Equal(t, "initial-render", mm2.turns[0].blocks[0].rendered,
		"old cache should be kept on re-render error")
}

func TestModel_Update_AltEnter_InsertsNewline(t *testing.T) {
	m := newTestModel()
	m.textarea.SetValue("hello")
	m.recalcLayout()

	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	mm := newM.(*model)

	assert.Contains(t, mm.textarea.Value(), "\n")
}

func TestModel_Update_Enter_SubmitsMultiLine(t *testing.T) {
	eventsCh := make(chan conduit.Event, 10)
	m := newTestModel()
	m.eventsCh = eventsCh
	m.textarea.SetValue("line1\nline2")
	m.recalcLayout()

	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm := newM.(*model)

	assert.Empty(t, mm.textarea.Value())

	select {
	case e := <-eventsCh:
		require.Equal(t, "user_message", e.Kind())
		ume, ok := e.(conduit.UserMessageEvent)
		require.True(t, ok)
		assert.Equal(t, "line1\nline2", ume.Content)
	default:
		t.Fatal("expected event on channel")
	}
}

func TestModel_View_ContainsSeparator(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(80, 20)
	m.width = 80
	output := m.View()
	assert.Contains(t, output, strings.Repeat("─", 80))
}

func TestModel_Update_Turn_Assistant_EmptyText(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(80, 20)
	m.md = mockMarkdownRenderer{output: "mock-empty-output"}
	turn := state.Turn{
		Role: state.RoleAssistant,
		Artifacts: []artifact.Artifact{
			artifact.Text{Content: ""},
		},
	}
	newM, _ := m.Update(turnMsg{turn: turn})
	mm := newM.(*model)
	require.Len(t, mm.turns, 1)
	require.Len(t, mm.turns[0].blocks, 1)
	assert.Empty(t, mm.turns[0].blocks[0].source)
	assert.Equal(t, "mock-empty-output", mm.turns[0].blocks[0].rendered)
	// View should not crash with empty text.
	output := mm.View()
	assert.Contains(t, output, "Assistant: ")
}
