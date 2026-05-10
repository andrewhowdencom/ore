package tui

import (
	"strings"
	"testing"

	"github.com/andrewhowdencom/tack/artifact"
	"github.com/andrewhowdencom/tack/state"
	"github.com/andrewhowdencom/tack/surface"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModel_Update_Delta_TextDelta(t *testing.T) {
	m := model{}
	newM, _ := m.Update(deltaMsg{delta: artifact.TextDelta{Content: "hello"}})
	mm := newM.(*model)
	assert.Equal(t, "hello", mm.textStreamBuffer.String())
}

func TestModel_Update_Delta_ReasoningDelta(t *testing.T) {
	m := model{}
	newM, _ := m.Update(deltaMsg{delta: artifact.ReasoningDelta{Content: "thinking"}})
	mm := newM.(*model)
	assert.Equal(t, "thinking", mm.reasoningStreamBuffer.String())
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
	assert.Equal(t, "hello world", mm.turns[0].text)
	assert.Empty(t, mm.textStreamBuffer.String())
	assert.Empty(t, mm.reasoningStreamBuffer.String())
}

func TestModel_Update_Turn_ResetsStreamBuffer(t *testing.T) {
	m := model{}
	m.textStreamBuffer.WriteString("partial")

	turn := state.Turn{
		Role: state.RoleAssistant,
		Artifacts: []artifact.Artifact{
			artifact.Text{Content: "complete"},
		},
	}
	newM, _ := m.Update(turnMsg{turn: turn})
	mm := newM.(*model)
	assert.Empty(t, mm.textStreamBuffer.String())
	assert.Empty(t, mm.reasoningStreamBuffer.String())
}

func TestModel_Update_Status(t *testing.T) {
	m := model{}
	newM, _ := m.Update(statusMsg{status: "thinking..."})
	mm := newM.(*model)
	assert.Equal(t, "thinking...", mm.status)
}

func TestModel_Update_KeyRunes(t *testing.T) {
	m := model{}
	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello")})
	mm := newM.(*model)
	assert.Equal(t, "hello", mm.input.String())
}

func TestModel_Update_KeySpace(t *testing.T) {
	m := model{}
	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	mm := newM.(*model)
	assert.Equal(t, " ", mm.input.String())
}

func TestModel_Update_KeyBackspace(t *testing.T) {
	m := model{}
	m.input.WriteString("hi")
	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	mm := newM.(*model)
	assert.Equal(t, "h", mm.input.String())
}

func TestModel_Update_KeyBackspace_Empty(t *testing.T) {
	m := model{}
	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	mm := newM.(*model)
	assert.Empty(t, mm.input.String())
}

func TestModel_Update_KeyEnter_WithInput(t *testing.T) {
	eventsCh := make(chan surface.Event, 10)
	m := model{eventsCh: eventsCh}
	m.input.WriteString("hello")

	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm := newM.(*model)

	require.Len(t, mm.turns, 1)
	assert.Equal(t, state.RoleUser, mm.turns[0].role)
	assert.Equal(t, "hello", mm.turns[0].text)
	assert.Empty(t, mm.input.String())

	select {
	case e := <-eventsCh:
		require.Equal(t, "user_message", e.Kind())
		ume, ok := e.(surface.UserMessageEvent)
		require.True(t, ok)
		assert.Equal(t, "hello", ume.Content)
	default:
		t.Fatal("expected event on channel")
	}
}

func TestModel_Update_KeyEnter_EmptyInput(t *testing.T) {
	eventsCh := make(chan surface.Event, 10)
	m := model{eventsCh: eventsCh}

	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm := newM.(*model)

	assert.Empty(t, mm.turns)
	assert.Empty(t, mm.input.String())

	select {
	case <-eventsCh:
		t.Fatal("expected no event on empty input")
	default:
	}
}

func TestModel_Update_WindowSize(t *testing.T) {
	m := model{}
	newM, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	mm := newM.(*model)
	assert.Equal(t, 80, mm.width)
	assert.Equal(t, 24, mm.height)
}

func TestModel_Update_WindowSize_ResizesViewport(t *testing.T) {
	m := model{}
	newM, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	mm := newM.(*model)
	assert.Equal(t, 80, mm.viewport.Width)
	assert.Equal(t, 23, mm.viewport.Height)
}

func TestModel_Update_KeyCtrlC(t *testing.T) {
	eventsCh := make(chan surface.Event, 10)
	m := model{eventsCh: eventsCh}

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
	m := model{
		viewport: viewport.New(80, 20),
		turns: []renderedTurn{
			{role: state.RoleUser, text: "hello"},
		},
	}
	output := m.View()
	assert.Contains(t, output, "You: ")
	assert.Contains(t, output, "hello")
}

func TestModel_View_ContainsAssistantTurn(t *testing.T) {
	m := model{
		viewport: viewport.New(80, 20),
		turns: []renderedTurn{
			{role: state.RoleAssistant, text: "world"},
		},
	}
	output := m.View()
	assert.Contains(t, output, "Assistant: ")
	assert.Contains(t, output, "world")
}

func TestModel_View_ContainsToolTurn(t *testing.T) {
	m := model{
		viewport: viewport.New(80, 20),
		turns: []renderedTurn{
			{role: state.RoleTool, text: "result"},
		},
	}
	output := m.View()
	assert.Contains(t, output, "Tool: ")
	assert.Contains(t, output, "result")
}

func TestModel_View_ContainsStreaming(t *testing.T) {
	m := model{
		viewport: viewport.New(80, 20),
	}
	m.textStreamBuffer.WriteString("partial")
	output := m.View()
	assert.Contains(t, output, "Assistant: ")
	assert.Contains(t, output, "partial")
}

func TestModel_View_ContainsStatus(t *testing.T) {
	m := model{
		viewport: viewport.New(80, 20),
		status: "thinking...",
	}
	output := m.View()
	assert.Contains(t, output, "thinking...")
}

func TestModel_View_ContainsPrompt(t *testing.T) {
	m := model{
		viewport: viewport.New(80, 20),
	}
	m.input.WriteString("hi")
	output := m.View()
	assert.Contains(t, output, "> ")
	assert.Contains(t, output, "hi_")
}

func TestModel_View_Empty(t *testing.T) {
	m := model{
		viewport: viewport.New(80, 20),
	}
	output := m.View()
	assert.True(t, strings.HasSuffix(output, "> _"))
}

func TestModel_View_ContainsInputAtBottom(t *testing.T) {
	m := model{
		viewport: viewport.New(80, 20),
		turns: []renderedTurn{
			{role: state.RoleUser, text: "hello"},
		},
	}
	output := m.View()
	lines := strings.Split(output, "\n")
	lastLine := lines[len(lines)-1]
	assert.Equal(t, "> _", lastLine)
}

func TestModel_Update_PgUp_ScrollsViewport(t *testing.T) {
	m := model{
		viewport: viewport.New(80, 5),
	}
	m.viewport.SetContent(strings.Repeat("line\n", 20))
	m.viewport.GotoBottom()
	initialYOffset := m.viewport.YOffset

	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	mm := newM.(*model)

	assert.Less(t, mm.viewport.YOffset, initialYOffset, "PgUp should scroll viewport up")
}

func TestModel_Update_PgDown_ScrollsViewport(t *testing.T) {
	m := model{
		viewport: viewport.New(80, 5),
	}
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
	m := model{
		viewport: viewport.New(80, 5),
	}
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
	m := model{
		viewport: viewport.New(80, 5),
	}
	m.viewport.SetContent(strings.Repeat("line\n", 20))
	m.viewport.GotoBottom()

	// Scroll up to simulate user reading history
	m.viewport.HalfPageUp()
	assert.False(t, m.viewport.AtBottom(), "should not be at bottom after scrolling up")

	newM, _ := m.Update(deltaMsg{delta: artifact.TextDelta{Content: "new token"}})
	mm := newM.(*model)

	assert.True(t, mm.viewport.AtBottom(), "delta should auto-scroll viewport to bottom")
	assert.Equal(t, "new token", mm.textStreamBuffer.String())
}

func TestModel_View_LongHistory_InputAtBottom(t *testing.T) {
	m := model{
		viewport: viewport.New(80, 5),
	}
	// Add enough turns to exceed viewport height
	for i := 0; i < 10; i++ {
		m.turns = append(m.turns, renderedTurn{
			role: state.RoleUser,
			text: strings.Repeat("word ", 20),
		})
	}
	output := m.View()
	lines := strings.Split(output, "\n")
	lastLine := lines[len(lines)-1]
	assert.Equal(t, "> _", lastLine)
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
	m := model{
		viewport: viewport.New(20, 5),
		turns: []renderedTurn{
			{role: state.RoleUser, text: strings.Repeat("word ", 10)},
		},
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

func TestModel_Update_Delta_StartsBlinking(t *testing.T) {
	m := model{}
	_, cmd := m.Update(deltaMsg{delta: artifact.TextDelta{Content: "hello"}})
	require.NotNil(t, cmd, "first delta should start the blinking cursor")
}

func TestModel_Update_CursorTickMsg_TogglesCursor(t *testing.T) {
	m := model{streaming: true}

	// First tick: should toggle to true
	newM, cmd := m.Update(cursorTickMsg{})
	mm := newM.(*model)
	assert.True(t, mm.cursorVisible, "cursor should be visible after first tick")
	require.NotNil(t, cmd, "should return next tick command")

	// Second tick: should toggle to false
	newM2, cmd2 := mm.Update(cursorTickMsg{})
	mm2 := newM2.(*model)
	assert.False(t, mm2.cursorVisible, "cursor should be hidden after second tick")
	require.NotNil(t, cmd2, "should return next tick command")
}

func TestModel_Update_Turn_StopsBlinking(t *testing.T) {
	m := model{}
	m.streaming = true
	m.cursorVisible = true
	m.textStreamBuffer.WriteString("partial")

	turn := state.Turn{
		Role: state.RoleAssistant,
		Artifacts: []artifact.Artifact{
			artifact.Text{Content: "complete"},
		},
	}
	newM, _ := m.Update(turnMsg{turn: turn})
	mm := newM.(*model)
	assert.False(t, mm.streaming, "streaming should be false after turn")
	assert.False(t, mm.cursorVisible, "cursor should be hidden after turn")
	assert.Empty(t, mm.textStreamBuffer.String())
	assert.Empty(t, mm.reasoningStreamBuffer.String())
}

func TestModel_View_ContainsReasoningStream(t *testing.T) {
	m := model{
		viewport: viewport.New(80, 20),
	}
	m.reasoningStreamBuffer.WriteString("analyzing patterns")
	output := m.View()
	assert.Contains(t, output, "Thinking: ")
	assert.Contains(t, output, "analyzing patterns")
}

func TestModel_View_BlinkingCursorVisible(t *testing.T) {
	m := model{
		viewport: viewport.New(80, 20),
	}
	m.streaming = true
	m.cursorVisible = true
	m.textStreamBuffer.WriteString("partial")
	output := m.View()
	assert.Contains(t, output, "partial")
	assert.Contains(t, output, "▌")
}

func TestModel_View_BlinkingCursorHidden(t *testing.T) {
	m := model{
		viewport: viewport.New(80, 20),
	}
	m.streaming = true
	m.cursorVisible = false
	m.textStreamBuffer.WriteString("partial")
	output := m.View()
	assert.Contains(t, output, "partial")
	assert.NotContains(t, output, "▌")
}

func TestModel_View_NoCursorWhenNotStreaming(t *testing.T) {
	m := model{
		viewport: viewport.New(80, 20),
	}
	m.streaming = false
	m.cursorVisible = true
	m.textStreamBuffer.WriteString("partial")
	output := m.View()
	assert.Contains(t, output, "partial")
	assert.NotContains(t, output, "▌")
}
